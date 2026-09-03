#!/usr/bin/env python3
"""Read-only replacement assertions for ANI IAM issue 01."""

from __future__ import annotations

import hashlib
import json
import re
import subprocess
from pathlib import Path


COMMIT = "963bc88836c54a1b09cf100b37eb2f2cb2a5a4be"
EXPECTED_RPCS = [
    "Login",
    "PlatformPasswordLogin",
    "BeginOIDCLogin",
    "CompleteOIDCLogin",
    "RefreshToken",
    "RevokeToken",
    "ValidateToken",
    "ValidatePrincipal",
    "IssueServiceToken",
    "CheckPermission",
    "CheckPermissionV2",
    "CreateAPIKey",
    "ListAPIKeys",
    "RevokeAPIKey",
]
EXPECTED_DIGESTS = {
    "api/proto/auth/v1/auth_service.proto": "aabcc72b10bd2b89591eaf706b4cf2659b98a8b5b4e3dbf92b3e387938bc33ec",
    "pkg/generated/pb/auth/v1/auth_service_grpc.pb.go": "d6912aeab75d01f837a94e4c936454af1b32dc4bc64853d5fe3c851e97348acc",
    "api/openapi/v1.yaml": "9b2237706da6bdbe54e73f02e192bf87bc46151f8f652d43f693c4580846426c",
    "api/core-v1-compatibility-baseline.yaml": "64c438277b673c6a2db5126c7030169a6be714ee6139f6b02ef1cca8c19ec341",
    "deploy/migrations/atlas.sum": "175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af",
    "deploy/docker/config/dex-dev.yaml": "3e6df562afa062f6e5f5b18060f2c6ab2ad1472f44b4a3cb4485484919513153",
}


def run(*args: str) -> bytes:
    return subprocess.check_output(args, stderr=subprocess.STDOUT)


def git_show(repo: Path, path: str) -> bytes:
    return run("git", "-C", str(repo), "show", f"{COMMIT}:./{path}")


def main() -> int:
    iam_root = Path(__file__).resolve().parents[4]
    ani_repo = (iam_root.parent / "ANI" / "repo").resolve()
    assertions: dict[str, object] = {}

    assertions["commit_type"] = run(
        "git", "-C", str(ani_repo), "cat-file", "-t", COMMIT
    ).decode().strip()
    if assertions["commit_type"] != "commit":
        raise SystemExit("fixed baseline is not a commit")

    for path, expected in EXPECTED_DIGESTS.items():
        actual = hashlib.sha256(git_show(ani_repo, path)).hexdigest()
        if actual != expected:
            raise SystemExit(f"digest mismatch: {path}: {actual}")
    assertions["artifact_digests"] = "pass"

    proto = git_show(ani_repo, "api/proto/auth/v1/auth_service.proto").decode()
    generated = git_show(
        ani_repo, "pkg/generated/pb/auth/v1/auth_service_grpc.pb.go"
    ).decode()
    service_body = re.search(r"service AuthService \{(.*?)\n\}", proto, re.S)
    if service_body is None:
        raise SystemExit("AuthService block missing")
    proto_rpcs = re.findall(r"^\s*rpc\s+(\w+)\(", service_body.group(1), re.M)
    generated_rpcs = re.findall(
        r"AuthService_(\w+)_FullMethodName\s*=", generated
    )
    if proto_rpcs != EXPECTED_RPCS or generated_rpcs != EXPECTED_RPCS:
        raise SystemExit(
            f"RPC inventory mismatch: proto={proto_rpcs} generated={generated_rpcs}"
        )
    assertions["rpc_inventory"] = EXPECTED_RPCS

    openapi = git_show(ani_repo, "api/openapi/v1.yaml").decode()
    old_path = "/admin/tenants/{tenant_id}/transfer-ownership:"
    if old_path not in openapi:
        raise SystemExit("CP0 frozen OpenAPI lost transfer-ownership oracle")
    assertions["cp0_old_protected_path_preserved"] = "pass"

    target_plan = (iam_root / "docs/plans/plan-iam-service-refactor.md").read_text()
    phased_plan = (iam_root / "docs/plans/plan-iam-kratos-phased.md").read_text()
    required_target_phrases = [
        "`TransferOwnership` 和 `tenant-owner` 不进入目标契约",
        "| Authorized | 一次 `CheckPermission(raw credential, operation_id, policy_revision, target attributes)` |",
        "没有默认 allow 或字符串推导 fallback",
    ]
    missing = [phrase for phrase in required_target_phrases if phrase not in target_plan]
    if missing:
        raise SystemExit(f"target replacement assertions missing: {missing}")
    if "只有基线事项的证据得到人工明确接受" not in phased_plan:
        raise SystemExit("human checkpoint assertion missing")
    assertions["target_replacement_contract"] = "pass"
    assertions["human_checkpoint"] = "required"

    print(
        json.dumps(
            {"baseline": COMMIT, "result": "pass", "assertions": assertions},
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
