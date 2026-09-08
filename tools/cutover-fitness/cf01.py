#!/usr/bin/env python3
"""Small, dependency-free guards for CUTOVER-FITNESS-01 artifacts."""

from __future__ import annotations

import json
import os
import re
import argparse
from pathlib import Path
from typing import Any


ALLOWED_NAMESPACES = {"ani-cutover-current", "ani-cutover-target"}
HEX40 = re.compile(r"^[0-9a-f]{40}$")
DIGEST_IMAGE = re.compile(r"^[^\s]+@sha256:[0-9a-f]{64}$")
FORBIDDEN_EVIDENCE_KEYS = {
    "access_token",
    "refresh_token",
    "id_token",
    "authorization",
    "cookie",
    "password",
    "private_key",
    "client_secret",
    "connection_string",
    "database_url",
    "dsn",
}


def render_template(template: str, values: dict[str, str]) -> str:
    rendered = template
    for name, value in values.items():
        rendered = rendered.replace(f"@@{name}@@", value)
    missing = sorted(set(re.findall(r"@@([A-Z0-9_]+)@@", rendered)))
    if missing:
        raise ValueError("missing template values: " + ", ".join(missing))
    return rendered


def validate_manifest(document: str, run_id: str) -> list[str]:
    errors: list[str] = []
    if "imagePullSecrets:" in document:
        errors.append("imagePullSecrets are forbidden")
    if re.search(r"^\s*type:\s*(NodePort|LoadBalancer)\s*$", document, re.MULTILINE):
        errors.append("NodePort and LoadBalancer Services are forbidden")
    for namespace in re.findall(r"^\s*namespace:\s*([^\s#]+)", document, re.MULTILINE):
        if namespace not in ALLOWED_NAMESPACES:
            errors.append(f"namespace {namespace} is outside the CF-01 boundary")
    for image in re.findall(r"^\s*image:\s*[\"']?([^\s\"']+)", document, re.MULTILINE):
        if not DIGEST_IMAGE.fullmatch(image):
            errors.append(f"bare image tag or invalid digest image: {image}")
    for index, resource in enumerate(re.split(r"^---\s*$", document, flags=re.MULTILINE), 1):
        if not re.search(r"^kind:\s*\S+", resource, re.MULTILINE):
            continue
        if not re.search(r"^\s{4}cutover-fitness-id:\s*CF-01\s*$", resource, re.MULTILINE):
            errors.append(f"resource {index} lacks cutover-fitness-id label")
        if not re.search(rf"^\s{{4}}run-id:\s*{re.escape(run_id)}\s*$", resource, re.MULTILINE):
            errors.append(f"resource {index} lacks run-id label")
    return errors


def _walk(value: Any, path: str = "$") -> list[str]:
    errors: list[str] = []
    if isinstance(value, dict):
        for key, child in value.items():
            normalized = str(key).lower().replace("-", "_")
            if normalized in FORBIDDEN_EVIDENCE_KEYS:
                errors.append(f"forbidden evidence key at {path}.{key}")
            errors.extend(_walk(child, f"{path}.{key}"))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            errors.extend(_walk(child, f"{path}[{index}]"))
    return errors


def validate_plan(plan: dict[str, Any]) -> list[str]:
    errors = _walk(plan)
    for lane in ("current", "target"):
        lane_value = plan.get(lane)
        if not isinstance(lane_value, dict):
            continue
        commit = lane_value.get("commit")
        if commit is not None and (not isinstance(commit, str) or not HEX40.fullmatch(commit)):
            errors.append(f"{lane}.commit must be an immutable 40-hex commit")
        tree = lane_value.get("tree")
        if tree is not None and (not isinstance(tree, str) or not HEX40.fullmatch(tree)):
            errors.append(f"{lane}.tree must be an immutable 40-hex tree")
    return errors


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(temporary, 0o600)
    temporary.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    render = sub.add_parser("render")
    render.add_argument("--template", type=Path, required=True)
    render.add_argument("--output", type=Path, required=True)
    render.add_argument("--run-id", required=True)
    render.add_argument("--set", action="append", default=[])
    validate = sub.add_parser("validate")
    validate.add_argument("--manifest", type=Path, required=True)
    validate.add_argument("--run-id", required=True)
    args = parser.parse_args()
    if args.command == "render":
        values = {"RUN_ID": args.run_id}
        for assignment in args.set:
            name, separator, value = assignment.partition("=")
            if not separator or not name:
                raise SystemExit(f"invalid --set: {assignment}")
            values[name] = value
        result = render_template(args.template.read_text(encoding="utf-8"), values)
        errors = validate_manifest(result, args.run_id)
        if errors:
            raise SystemExit("\n".join(errors))
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(result, encoding="utf-8")
        os.chmod(args.output, 0o600)
        return 0
    errors = validate_manifest(args.manifest.read_text(encoding="utf-8"), args.run_id)
    if errors:
        raise SystemExit("\n".join(errors))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
