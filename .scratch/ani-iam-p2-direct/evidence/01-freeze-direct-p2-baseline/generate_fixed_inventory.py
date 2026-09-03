#!/usr/bin/env python3
"""Generate DP2-01 inventories strictly from the pinned ANI Git object."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

import yaml


ANI_REPOSITORY = Path("/home/chabking/workspace/ANI")
COMMIT = "0cedae825a489d936cf41815dc27f278f6d3213c"
TREE = "552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8"

OPENAPI_PATH = "repo/api/openapi/v1.yaml"
SERVICES_OPENAPI_PATH = "repo/api/openapi/services/v1.yaml"
AUTH_PROTO_PATH = "repo/api/proto/auth/v1/auth_service.proto"

EXPECTED_RPCS = (
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
)

DERIVED_OPERATION_IDS = {
    ("POST", "/auth/refresh"): "refreshToken",
    ("GET", "/branding"): "getBranding",
    ("GET", "/tasks/{task_id}"): "getTask",
}

EXPECTED_ACTUAL_DERIVED_OPERATION_IDS = {
    ("POST", "/auth/refresh"): "refreshToken",
    ("GET", "/branding"): "getBranding",
}

HTTP_METHODS = ("get", "post", "put", "patch", "delete", "head", "options")

KEY_PATHS = (
    OPENAPI_PATH,
    SERVICES_OPENAPI_PATH,
    AUTH_PROTO_PATH,
    "repo/pkg/generated/pb/auth/v1/auth_service.pb.go",
    "repo/pkg/generated/pb/auth/v1/auth_service_grpc.pb.go",
    "repo/scripts/generate_gateway_authz.py",
    "repo/scripts/validate_gateway_authz_drift.py",
    "repo/scripts/validate_core_gateway_authz_routes.py",
    "repo/services/ani-gateway/internal/authz/zz_generated_core_policies.go",
    "repo/services/ani-gateway/internal/authz/config.go",
    "repo/services/ani-gateway/internal/middleware/chain.go",
    "repo/services/ani-gateway/internal/middleware/policy.go",
    "repo/services/ani-gateway/internal/middleware/auth_client.go",
    "repo/services/ani-gateway/internal/middleware/auth.go",
    "repo/services/ani-gateway/internal/middleware/rbac.go",
    "repo/services/ani-gateway/internal/middleware/generated_authz.go",
    "repo/services/ani-gateway/internal/router/auth.go",
    "repo/services/ani-gateway/main.go",
    "repo/services/auth-service/internal/config/config.go",
    "repo/services/auth-service/main.go",
    "repo/services/auth-service/internal/service/auth_service.go",
    "repo/services/auth-service/internal/service/password_login.go",
    "repo/services/auth-service/internal/service/platform_login.go",
    "repo/services/auth-service/internal/service/refresh_tokens.go",
    "repo/services/auth-service/internal/service/token_blocklist.go",
    "repo/services/auth-service/internal/service/api_keys.go",
    "repo/services/auth-service/internal/service/oidc.go",
    "repo/services/auth-service/internal/service/oidc_sessions.go",
    "repo/services/auth-service/internal/service/jwt.go",
    "repo/services/auth-service/internal/service/token_issuer.go",
    "repo/services/envoy-authz-adapter/internal/config/config.go",
    "repo/services/envoy-authz-adapter/internal/authclient/client.go",
    "repo/services/envoy-authz-adapter/internal/extauth/server.go",
    "repo/services/envoy-authz-adapter/main.go",
    "repo/services/inference-service/internal/config/config.go",
    "repo/services/inference-service/internal/runtime/coresdk/minter.go",
    "repo/services/inference-service/internal/runtime/coresdk/adapter.go",
    "repo/services/inference-service/main.go",
    "repo/frontends/console/src/api/auth.ts",
    "repo/frontends/console/src/auth/session.ts",
    "repo/frontends/console/src/routes/login.tsx",
    "repo/frontends/console/src/routes/auth/callback.tsx",
    "repo/frontends/console/src/routes/_authenticated/settings/api-keys.tsx",
    "repo/frontends/boss/src/api/auth.ts",
    "repo/frontends/boss/src/auth/session.ts",
    "repo/frontends/boss/src/auth/permissions.ts",
    "repo/frontends/boss/src/routes/login.tsx",
    "repo/frontends/boss/src/routes/auth/callback.tsx",
    "repo/deploy/migrations/20260501000100_init_schema.sql",
    "repo/deploy/migrations/20260707001400_platform_users.sql",
    "repo/deploy/migrations/20260707001401_platform_refresh_tokens.sql",
    "repo/deploy/migrations/20260827000200_users_display_name_soft_delete.sql",
    "repo/deploy/migrations/20260827_001_user_roles_single_role.sql",
    "repo/deploy/migrations/20260828000100_database_roles_hardening.sql",
    "repo/deploy/migrations/20260828000200_app_role_privileges.sql",
    "repo/deploy/migrations/20260901_001_gpu_chain_remaining_rls_fix.sql",
    "repo/deploy/migrations/atlas.sum",
    "repo/deploy/docker/docker-compose.yml",
    "repo/deploy/docker/config/dex-dev.yaml",
    "repo/deploy/real-k8s-lab/sprint13-production-auth-dex.yaml",
    "repo/deploy/real-k8s-lab/sprint13-production-shaped-gateway-deployment.yaml",
    "repo/deploy/real-k8s-lab/inference-envoy-ai-gateway-c40.yaml",
    "repo/deploy/real-k8s-lab/inference-incluster-e2e.yaml",
    "repo/Makefile",
    "repo/go.work",
    "repo/installer/ani-installer/profiles/baremetal.yaml",
    "repo/installer/ani-installer/profiles/existing-k8s.yaml",
    "repo/installer/ani-installer/profiles/vm.yaml",
)

EXPECTED_UNCHECKED_MIGRATIONS = (
    "20260821_001_tenant_admin_invitation.sql",
    "20260825_001_tenant_admin_invitation_pending_unique.sql",
    "20260827_001_async_tasks_list_index.sql",
    "20260827_001_user_roles_single_role.sql",
    "20260828_001_instance_resource_rls_fix.sql",
    "20260831_001_async_tasks_rls_fix.sql",
    "20260901_001_gpu_chain_remaining_rls_fix.sql",
)


def git_text(*args: str) -> str:
    completed = subprocess.run(
        ["git", "-C", str(ANI_REPOSITORY), *args],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return completed.stdout


def git_bytes(*args: str) -> bytes:
    completed = subprocess.run(
        ["git", "-C", str(ANI_REPOSITORY), *args],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return completed.stdout


def pinned_blob(path: str) -> bytes:
    return git_bytes("show", f"{COMMIT}:{path}")


def verify_identity() -> None:
    git_text("cat-file", "-e", f"{COMMIT}^{{commit}}")
    actual_tree = git_text("rev-parse", f"{COMMIT}^{{tree}}").strip()
    if actual_tree != TREE:
        raise SystemExit(f"pinned tree mismatch: {actual_tree} != {TREE}")


def render_source_manifest() -> str:
    rows = ["path\tgit_blob\tsha256\tbytes"]
    for path in KEY_PATHS:
        content = pinned_blob(path)
        blob = git_text("rev-parse", f"{COMMIT}:{path}").strip()
        rows.append(
            "\t".join(
                (
                    path,
                    blob,
                    hashlib.sha256(content).hexdigest(),
                    str(len(content)),
                )
            )
        )
    return "\n".join(rows) + "\n"


def render_migration_inventory() -> str:
    migration_prefix = "repo/deploy/migrations/"
    paths = tuple(
        path
        for path in git_text(
            "ls-tree",
            "-r",
            "--name-only",
            COMMIT,
            "--",
            migration_prefix,
        ).splitlines()
        if path.startswith(migration_prefix) and path.endswith(".sql")
    )
    atlas_entries = {
        line.split(" ", 1)[0]
        for line in pinned_blob(f"{migration_prefix}atlas.sum").decode("utf-8").splitlines()
        if line and not line.startswith("h1:")
    }
    unchecked = tuple(sorted(Path(path).name for path in paths if Path(path).name not in atlas_entries))
    if len(paths) != 38 or len(atlas_entries) != 31 or unchecked != EXPECTED_UNCHECKED_MIGRATIONS:
        raise SystemExit(
            "migration inventory mismatch: "
            f"files={len(paths)} atlas_entries={len(atlas_entries)} unchecked={unchecked!r}"
        )

    rows = ["path\tgit_blob\tsha256\tbytes\tatlas_listed"]
    for path in paths:
        content = pinned_blob(path)
        rows.append(
            "\t".join(
                (
                    path,
                    git_text("rev-parse", f"{COMMIT}:{path}").strip(),
                    hashlib.sha256(content).hexdigest(),
                    str(len(content)),
                    "yes" if Path(path).name in atlas_entries else "no",
                )
            )
        )
    return "\n".join(rows) + "\n"


def load_openapi() -> dict[str, Any]:
    document = yaml.safe_load(pinned_blob(OPENAPI_PATH))
    if not isinstance(document, dict) or not isinstance(document.get("paths"), dict):
        raise SystemExit("pinned OpenAPI does not contain a paths object")
    return document


def load_yaml_object(path: str) -> dict[str, Any]:
    document = yaml.safe_load(pinned_blob(path))
    if not isinstance(document, dict) or not isinstance(document.get("paths"), dict):
        raise SystemExit(f"pinned OpenAPI {path} does not contain a paths object")
    return document


def effective_security(document: dict[str, Any], operation: dict[str, Any]) -> Any:
    if "security" in operation:
        return operation["security"]
    return document.get("security", [])


def compact_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def render_openapi_inventory() -> tuple[str, dict[str, int]]:
    document = load_openapi()
    rows = [
        "method\tpath\toperation_id\toperation_id_source\told_policy_source\tsecurity"
        "\tauthz_version\tresource\taction\tboundary\tprincipal_kinds"
    ]
    counts = {"total": 0, "public": 0, "generated": 0, "legacy": 0}
    seen_operations: set[str] = set()
    seen_routes: set[tuple[str, str]] = set()
    derived_seen: dict[tuple[str, str], str] = {}

    for path in sorted(document["paths"]):
        item = document["paths"][path]
        if not isinstance(item, dict):
            continue
        for method in HTTP_METHODS:
            operation = item.get(method)
            if not isinstance(operation, dict):
                continue
            upper_method = method.upper()
            explicit = operation.get("operationId")
            if isinstance(explicit, str) and explicit:
                operation_id = explicit
                operation_id_source = "explicit"
            else:
                operation_id = DERIVED_OPERATION_IDS.get((upper_method, path), "")
                operation_id_source = "derived"
                if not operation_id:
                    raise SystemExit(f"unapproved missing operationId: {upper_method} {path}")
                derived_seen[(upper_method, path)] = operation_id

            route = (upper_method, path)
            if route in seen_routes:
                raise SystemExit(f"duplicate OpenAPI route: {upper_method} {path}")
            if operation_id in seen_operations:
                raise SystemExit(f"duplicate operationId: {operation_id}")
            seen_routes.add(route)
            seen_operations.add(operation_id)

            security = effective_security(document, operation)
            extension = operation.get("x-ani-authz")
            if security == []:
                source = "public"
            elif extension is not None:
                source = "generated"
            else:
                source = "legacy"

            if not isinstance(extension, dict):
                extension = {}
            kinds = extension.get("principal_kinds", [])
            counts["total"] += 1
            counts[source] += 1
            rows.append(
                "\t".join(
                    (
                        upper_method,
                        path,
                        operation_id,
                        operation_id_source,
                        source,
                        compact_json(security),
                        str(extension.get("version", "")),
                        str(extension.get("resource", "")),
                        str(extension.get("action", "")),
                        str(extension.get("boundary", "")),
                        compact_json(kinds),
                    )
                )
            )

    if derived_seen != EXPECTED_ACTUAL_DERIVED_OPERATION_IDS:
        raise SystemExit(f"derived operation set mismatch: {derived_seen!r}")
    expected_counts = {"total": 236, "public": 8, "generated": 5, "legacy": 223}
    if counts != expected_counts:
        raise SystemExit(f"OpenAPI classification count mismatch: {counts!r}")
    return "\n".join(rows) + "\n", counts


def render_rpc_inventory() -> str:
    import re

    proto = pinned_blob(AUTH_PROTO_PATH).decode("utf-8")
    service_match = re.search(r"service\s+AuthService\s*\{(?P<body>.*?)^\}", proto, re.MULTILINE | re.DOTALL)
    if not service_match:
        raise SystemExit("auth.v1.AuthService block not found")
    rpc_pattern = re.compile(
        r"^\s*rpc\s+(?P<name>[A-Za-z0-9_]+)\s*\(\s*(?P<request>[^)]+)\s*\)\s*"
        r"returns\s*\(\s*(?P<response>[^)]+)\s*\)\s*;",
        re.MULTILINE,
    )
    parsed = tuple(match.group("name") for match in rpc_pattern.finditer(service_match.group("body")))
    if parsed != EXPECTED_RPCS:
        raise SystemExit(f"AuthService RPC inventory mismatch: {parsed!r}")
    rows = ["ordinal\trpc\trequest\tresponse\tfull_method"]
    for ordinal, match in enumerate(rpc_pattern.finditer(service_match.group("body")), start=1):
        name = match.group("name")
        rows.append(
            f"{ordinal}\t{name}\t{match.group('request').strip()}\t{match.group('response').strip()}"
            f"\t/auth.v1.AuthService/{name}"
        )
    return "\n".join(rows) + "\n"


def is_related_iam_operation(contract: str, path: str) -> bool:
    if contract == "core-v1":
        return path.startswith("/auth/") or path.startswith("/admin/platform-users") or path in {
            "/admin/tenant-admins/available-tenants",
            "/admin/tenant-users",
        } or path.startswith("/admin/tenants/{tenant_id}/user") or path == "/admin/tenants/{tenant_id}/roles"
    return (
        path.startswith("/tenant/members")
        or path.startswith("/tenant/roles")
        or path == "/tenant/sso"
        or path.startswith("/platform-admins")
        or path.startswith("/tenant-admins")
        or path.startswith("/tenants/{tenantId}/admins")
        or path == "/tenants/{tenantId}/roles"
    )


def render_related_iam_operations() -> str:
    rows = ["contract\tmethod\tpath\tgateway_path\toperation_id\tsecurity\ttags"]
    contracts = (
        ("core-v1", OPENAPI_PATH, "/api/v1"),
        ("services-v1", SERVICES_OPENAPI_PATH, "/api/v1/svc"),
    )
    seen: set[tuple[str, str, str]] = set()
    for contract, source_path, gateway_prefix in contracts:
        document = load_yaml_object(source_path)
        for path in sorted(document["paths"]):
            if not is_related_iam_operation(contract, path):
                continue
            item = document["paths"][path]
            if not isinstance(item, dict):
                continue
            for method in HTTP_METHODS:
                operation = item.get(method)
                if not isinstance(operation, dict):
                    continue
                operation_id = operation.get("operationId")
                if not isinstance(operation_id, str) or not operation_id:
                    operation_id = DERIVED_OPERATION_IDS.get((method.upper(), path), "")
                if not operation_id:
                    raise SystemExit(f"related IAM operation missing operationId: {contract} {method.upper()} {path}")
                key = (contract, method.upper(), path)
                if key in seen:
                    raise SystemExit(f"duplicate related IAM operation: {key!r}")
                seen.add(key)
                gateway_path = gateway_prefix + path
                rows.append(
                    "\t".join(
                        (
                            contract,
                            method.upper(),
                            path,
                            gateway_path,
                            operation_id,
                            compact_json(effective_security(document, operation)),
                            compact_json(operation.get("tags", [])),
                        )
                    )
                )
    if len(seen) != 59:
        raise SystemExit(f"related IAM operation count mismatch: {len(seen)}")
    return "\n".join(rows) + "\n"


def write_output(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8", newline="\n")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output-dir", type=Path, default=Path(__file__).resolve().parent)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)

    verify_identity()
    openapi_inventory, counts = render_openapi_inventory()
    write_output(args.output_dir / "source-manifest.tsv", render_source_manifest())
    write_output(args.output_dir / "migration-files.tsv", render_migration_inventory())
    write_output(args.output_dir / "openapi-operations.tsv", openapi_inventory)
    write_output(args.output_dir / "auth-service-rpcs.tsv", render_rpc_inventory())
    write_output(args.output_dir / "related-iam-operations.tsv", render_related_iam_operations())
    print(
        "fixed inventory generated: "
        f"commit={COMMIT} tree={TREE} paths={len(KEY_PATHS)} migration_files=38 atlas_listed=31 "
        f"operations={counts['total']} related_iam_operations=59 public={counts['public']} "
        f"generated={counts['generated']} legacy={counts['legacy']} rpcs={len(EXPECTED_RPCS)}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
