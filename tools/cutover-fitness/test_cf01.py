import json
import tempfile
import threading
import unittest
from unittest import mock
from pathlib import Path

import cf01
import probe

REPO_ROOT = Path(__file__).resolve().parents[2]


class ManifestValidationTests(unittest.TestCase):
    def test_accepts_only_digest_images_task_labels_and_allowed_namespaces(self):
        manifest = """
apiVersion: v1
kind: Pod
metadata:
  name: probe
  namespace: ani-cutover-current
  labels:
    cutover-fitness-id: CF-01
    run-id: 20260908t105253z
spec:
  containers:
    - name: probe
      image: docker.changqingyun.cn/ani/cf01-probe@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
"""
        self.assertEqual(cf01.validate_manifest(manifest, "20260908t105253z"), [])

    def test_rejects_tag_pull_secret_cross_namespace_and_nodeport(self):
        manifest = """
apiVersion: v1
kind: Service
metadata:
  name: bad
  namespace: ani-system
spec:
  type: NodePort
  imagePullSecrets:
    - name: copied-login
  selector: {}
---
apiVersion: v1
kind: Pod
metadata:
  name: bad
  namespace: ani-cutover-target
spec:
  containers:
    - name: bad
      image: docker.changqingyun.cn/ani/cf01-probe:latest
"""
        errors = "\n".join(cf01.validate_manifest(manifest, "20260908t105253z"))
        for expected in ("ani-system", "NodePort", "imagePullSecrets", "bare image tag", "cutover-fitness-id", "run-id"):
            self.assertIn(expected, errors)

    def test_render_rejects_missing_tokens_and_validates_result(self):
        template = "run-id: @@RUN_ID@@\nimage: @@PROBE_IMAGE@@\n"
        with self.assertRaisesRegex(ValueError, "PROBE_IMAGE"):
            cf01.render_template(template, {"RUN_ID": "20260908t105253z"})
        rendered = cf01.render_template(
            template,
            {
                "RUN_ID": "20260908t105253z",
                "PROBE_IMAGE": "docker.changqingyun.cn/ani/cf01-probe@sha256:" + "a" * 64,
            },
        )
        self.assertNotIn("@@", rendered)


class CurrentAuthDockerfileTests(unittest.TestCase):
    def test_uses_workspace_without_mutating_module_files(self):
        dockerfile = (REPO_ROOT / "deploy/cutover-fitness/current-auth.Dockerfile").read_text()
        self.assertIn("go work init ./runtimeadmin ./pkg ./services/auth-service", dockerfile)
        self.assertNotIn("GOWORK=off", dockerfile)
        self.assertNotIn("go mod tidy", dockerfile)
        self.assertRegex(dockerfile, r"FROM docker\.io/library/golang@sha256:[0-9a-f]{64} AS build")
        self.assertRegex(dockerfile, r"FROM docker\.io/library/alpine@sha256:[0-9a-f]{64}")


class EvidenceTests(unittest.TestCase):
    def test_plan_rejects_dynamic_refs_and_secret_shaped_values(self):
        plan = {
            "run_id": "20260908t105253z",
            "current": {"commit": "main", "tree": "4ba6a15ad0cddf0db66a25d695b082d47346aff1"},
            "password": "must-not-be-recorded",
        }
        errors = "\n".join(cf01.validate_plan(plan))
        self.assertIn("immutable 40-hex commit", errors)
        self.assertIn("forbidden evidence key", errors)

    def test_write_json_is_canonical_and_private(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            cf01.write_json(path, {"result": "pass", "run_id": "20260908t105253z"})
            self.assertEqual(json.loads(path.read_text()), {"result": "pass", "run_id": "20260908t105253z"})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)


class ProbeTests(unittest.TestCase):
    def test_storage_round_trip_uses_known_literal(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "canary"
            self.assertTrue(probe.storage_round_trip(path, "CF01-STORAGE-V1"))
            self.assertEqual(path.read_text(), "CF01-STORAGE-V1\n")

    def test_tcp_and_udp_echo_are_observable(self):
        server = probe.EchoServer("127.0.0.1", 0, 0)
        thread = threading.Thread(target=server.serve, daemon=True)
        thread.start()
        self.assertTrue(server.ready.wait(2))
        try:
            self.assertEqual(probe.tcp_echo("127.0.0.1", server.tcp_port, 1), b"CF01-TCP-V1")
            self.assertEqual(probe.udp_echo("127.0.0.1", server.udp_port, 1), b"CF01-UDP-V1")
        finally:
            server.close()
            thread.join(2)

    def test_current_http_smoke_uses_fixed_gateway_chain(self):
        calls = []

        def request(method, url, body=None, token="", headers=None):
            calls.append((method, url, body, token, headers or {}))
            if url.endswith("/auth/password/login"):
                return 200, {"access_token": "opaque-current-token"}
            return 200, {}

        with mock.patch.object(probe, "_json_request", side_effect=request):
            result = probe.http_smoke(
                "http://gateway:9200/readyz",
                "http://gateway:8080/api/v1",
                "current",
                "user@example.com",
                "correct horse",
                "tenant-a",
                "0198f062-b76d-7f2a-b0ad-50a417bf1f70",
            )

        self.assertEqual(result, {
            "ready_status": 200,
            "login_status": 200,
            "protected_status": 200,
            "token_obtained": True,
        })
        self.assertEqual(calls[0][:2], ("GET", "http://gateway:9200/readyz"))
        self.assertEqual(calls[1][2], {
            "tenant_name": "tenant-a",
            "username": "user@example.com",
            "password": "correct horse",
        })
        self.assertEqual(calls[2][:2], ("GET", "http://gateway:8080/api/v1/instances"))
        self.assertEqual(calls[2][3], "opaque-current-token")

    def test_target_http_smoke_uses_frozen_contract_and_one_idempotency_key(self):
        calls = []

        def request(method, url, body=None, token="", headers=None):
            calls.append((method, url, body, token, headers or {}))
            if url.endswith("/auth/password/login"):
                return 200, {"access_token": "opaque-target-token"}
            return 200, {}

        with mock.patch.object(probe, "_json_request", side_effect=request):
            result = probe.http_smoke(
                "http://gateway:9200/readyz",
                "http://gateway:8080/api/v1",
                "target",
                "user@example.com",
                "correct horse",
                "tenant-a",
                "0198f062-b76d-7f2a-b0ad-50a417bf1f70",
            )

        self.assertEqual(result["protected_status"], 200)
        self.assertEqual(calls[1][2], {
            "account": "user@example.com",
            "password": "correct horse",
            "audience": "console",
            "boundary": {
                "type": "tenant",
                "tenant_id": "0198f062-b76d-7f2a-b0ad-50a417bf1f70",
            },
            "device_name": "cf01-fixed-smoke",
        })
        self.assertEqual(calls[1][4], {"Idempotency-Key": "cf01-target-password-login-v1"})


if __name__ == "__main__":
    unittest.main()
