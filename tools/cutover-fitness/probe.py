#!/usr/bin/env python3
"""CF-01 probe image entrypoint; never prints supplied credentials or tokens."""

from __future__ import annotations

import argparse
import json
import os
import selectors
import signal
import socket
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


TCP_MARKER = b"CF01-TCP-V1"
UDP_MARKER = b"CF01-UDP-V1"


def storage_round_trip(path: Path, marker: str) -> bool:
    path.write_text(marker + "\n", encoding="utf-8")
    with path.open("rb") as handle:
        os.fsync(handle.fileno())
    return path.read_text(encoding="utf-8") == marker + "\n"


def tcp_echo(host: str, port: int, timeout: float) -> bytes:
    with socket.create_connection((host, port), timeout=timeout) as connection:
        connection.sendall(TCP_MARKER)
        return connection.recv(64)


def udp_echo(host: str, port: int, timeout: float) -> bytes:
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
        connection.settimeout(timeout)
        connection.sendto(UDP_MARKER, (host, port))
        return connection.recvfrom(64)[0]


class EchoServer:
    def __init__(self, host: str, tcp_port: int, udp_port: int) -> None:
        self.tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.tcp.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.tcp.bind((host, tcp_port))
        self.tcp.listen()
        self.tcp.setblocking(False)
        self.udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.udp.bind((host, udp_port))
        self.udp.setblocking(False)
        self.tcp_port = self.tcp.getsockname()[1]
        self.udp_port = self.udp.getsockname()[1]
        self.ready = __import__("threading").Event()
        self._closed = False

    def serve(self) -> None:
        selector = selectors.DefaultSelector()
        selector.register(self.tcp, selectors.EVENT_READ, "tcp")
        selector.register(self.udp, selectors.EVENT_READ, "udp")
        self.ready.set()
        while not self._closed:
            for key, _ in selector.select(0.2):
                try:
                    if key.data == "tcp":
                        connection, _ = self.tcp.accept()
                        with connection:
                            connection.settimeout(1)
                            connection.sendall(connection.recv(64))
                    else:
                        payload, address = self.udp.recvfrom(64)
                        self.udp.sendto(payload, address)
                except (OSError, TimeoutError):
                    if not self._closed:
                        raise
        selector.close()

    def close(self) -> None:
        self._closed = True
        self.tcp.close()
        self.udp.close()


def _json_request(
    method: str,
    url: str,
    body: dict | None = None,
    token: str = "",
    headers: dict[str, str] | None = None,
) -> tuple[int, dict]:
    request_headers = {"Accept": "application/json", **(headers or {})}
    payload = None
    if body is not None:
        payload = json.dumps(body).encode()
        request_headers["Content-Type"] = "application/json"
    if token:
        request_headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(url, data=payload, headers=request_headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            raw = response.read()
            return response.status, json.loads(raw or b"{}")
    except urllib.error.HTTPError as error:
        raw = error.read()
        try:
            parsed = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            parsed = {}
        return error.code, parsed


def _find_access_token(value: object) -> str:
    if isinstance(value, dict):
        for key, child in value.items():
            if key in {"access_token", "accessToken"} and isinstance(child, str):
                return child
            found = _find_access_token(child)
            if found:
                return found
    if isinstance(value, list):
        for child in value:
            found = _find_access_token(child)
            if found:
                return found
    return ""


def http_smoke(
    ready_url: str,
    api_base_url: str,
    lane: str,
    account: str,
    password: str,
    tenant_name: str,
    tenant_id: str,
) -> dict:
    ready_status, _ = _json_request("GET", ready_url)
    if lane == "current":
        login_body = {"tenant_name": tenant_name, "username": account, "password": password}
        login_headers = None
    elif lane == "target":
        login_body = {
            "account": account,
            "password": password,
            "audience": "console",
            "boundary": {"type": "tenant", "tenant_id": tenant_id},
            "device_name": "cf01-fixed-smoke",
        }
        login_headers = {"Idempotency-Key": "cf01-target-password-login-v1"}
    else:
        raise ValueError("lane must be current or target")
    login_status, login = _json_request(
        "POST",
        api_base_url.rstrip("/") + "/auth/password/login",
        login_body,
        headers=login_headers,
    )
    token = _find_access_token(login)
    protected_status = 0
    if token:
        protected_status, _ = _json_request("GET", api_base_url.rstrip("/") + "/instances", token=token)
    return {
        "ready_status": ready_status,
        "login_status": login_status,
        "protected_status": protected_status,
        "token_obtained": bool(token),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    serve = sub.add_parser("serve")
    serve.add_argument("--host", default="0.0.0.0")
    serve.add_argument("--tcp-port", type=int, default=18080)
    serve.add_argument("--udp-port", type=int, default=18081)
    storage = sub.add_parser("storage")
    storage.add_argument("--path", required=True)
    storage.add_argument("--marker", required=True)
    storage.add_argument("--hold-seconds", type=int, default=3600)
    check = sub.add_parser("check")
    check.add_argument("--protocol", choices=("tcp", "udp"), required=True)
    check.add_argument("--host", required=True)
    check.add_argument("--port", type=int, required=True)
    check.add_argument("--timeout", type=float, default=2)
    check.add_argument("--expect", choices=("allow", "deny"), required=True)
    dns = sub.add_parser("dns")
    dns.add_argument("--host", required=True)
    smoke = sub.add_parser("http-smoke")
    smoke.add_argument("--ready-url", required=True)
    smoke.add_argument("--api-base-url", required=True)
    smoke.add_argument("--lane", choices=("current", "target"), required=True)
    smoke.add_argument("--account", required=True)
    smoke.add_argument("--tenant-name", required=True)
    smoke.add_argument("--tenant-id", required=True)
    sleep = sub.add_parser("sleep")
    sleep.add_argument("--seconds", type=int, default=3600)
    args = parser.parse_args()

    if args.command == "serve":
        server = EchoServer(args.host, args.tcp_port, args.udp_port)
        signal.signal(signal.SIGTERM, lambda *_: server.close())
        signal.signal(signal.SIGINT, lambda *_: server.close())
        server.serve()
        return 0
    if args.command == "storage":
        if not storage_round_trip(Path(args.path), args.marker):
            return 1
        print(json.dumps({"storage_round_trip": "pass"}))
        time.sleep(args.hold_seconds)
        return 0
    if args.command == "check":
        try:
            response = tcp_echo(args.host, args.port, args.timeout) if args.protocol == "tcp" else udp_echo(args.host, args.port, args.timeout)
            allowed = response == (TCP_MARKER if args.protocol == "tcp" else UDP_MARKER)
        except (OSError, TimeoutError):
            allowed = False
        passed = allowed == (args.expect == "allow")
        print(json.dumps({"protocol": args.protocol, "expect": args.expect, "result": "pass" if passed else "fail"}))
        return 0 if passed else 1
    if args.command == "dns":
        socket.getaddrinfo(args.host, None)
        print(json.dumps({"dns": "pass", "host": args.host}))
        return 0
    if args.command == "http-smoke":
        password = os.environ.get("CF01_PASSWORD", "")
        if not password:
            raise SystemExit("CF01_PASSWORD is required")
        result = http_smoke(
            args.ready_url,
            args.api_base_url,
            args.lane,
            args.account,
            password,
            args.tenant_name,
            args.tenant_id,
        )
        print(json.dumps(result, sort_keys=True))
        return 0 if all((result["ready_status"] == 200, result["login_status"] == 200, result["protected_status"] == 200)) else 1
    time.sleep(args.seconds)
    return 0


if __name__ == "__main__":
    sys.exit(main())
