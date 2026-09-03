#!/usr/bin/env python3
"""Verify the DP2-01 evidence without reading ANI's mutable checkout."""

from __future__ import annotations

import argparse
import csv
import hashlib
import re
import subprocess
import tempfile
from pathlib import Path


IAM_REPOSITORY = Path(__file__).resolve().parents[4]
EVIDENCE_DIRECTORY = Path(__file__).resolve().parent
ANI_REPOSITORY = Path("/home/chabking/workspace/ANI")
ANI_COMMIT = "0cedae825a489d936cf41815dc27f278f6d3213c"
ANI_TREE = "552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8"
IAM_HEAD = "c27ae04c3837e0886773ba016c9cded4169bf1ed"
KRATOS_BASELINE = "05ba302661d593b608df070dd51cc063fc9f8023"
TICKET = ".scratch/ani-iam-p2-direct/issues/01-freeze-direct-p2-baseline.md"
EVIDENCE_PREFIX = ".scratch/ani-iam-p2-direct/evidence/01-freeze-direct-p2-baseline/"

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

EXPECTED_INVENTORY_HASHES = {
    "source-manifest.tsv": "3c0df2fd53d91a7d91930d72aa1bf847d4158f3724885b7cbd5d9ccf53477849",
    "migration-files.tsv": "e863ffc96f86679dae90bbee7db1bf24e4f1b75b8eb9f25e4ca02e2fa78da7ea",
    "openapi-operations.tsv": "e5f1a2025df723705d0aa37eb09c88379a0a589e724490915076b438195a6bfc",
    "related-iam-operations.tsv": "5f8cd36dd12ed79db7903b10ce2747e14eef60fe268d83347b797342a870e217",
    "auth-service-rpcs.tsv": "281fcdc00dab5f5193148a5df219ab747cc2bb650627d37f73d25a92427353ef",
}

INITIAL_REFERENCE_COUNTS = {
    r"auth\.v1\.AuthService": 48,
    "AUTH_SERVICE_ADDR": 17,
    "AUTH_SERVICE_GRPC_ADDR": 18,
    "AUTH_SERVICE_MINT_SECRET": 3,
    "X-API-Key": 119,
    "ANI_AUTH_MODE": 206,
    "jwt:blocklist:": 3,
    "tenant-owner": 15,
}


def run(*args: str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(args),
        cwd=cwd,
        check=check,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def read_tsv(name: str) -> list[dict[str, str]]:
    with (EVIDENCE_DIRECTORY / name).open(encoding="utf-8", newline="") as handle:
        return list(csv.DictReader(handle, delimiter="\t"))


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(f"result: fail\ngate: {message}")


def verify_git_identity(accepted: bool) -> None:
    run("git", "-C", str(ANI_REPOSITORY), "cat-file", "-e", f"{ANI_COMMIT}^{{commit}}")
    tree = run("git", "-C", str(ANI_REPOSITORY), "rev-parse", f"{ANI_COMMIT}^{{tree}}").stdout.strip()
    require(tree == ANI_TREE, f"ANI tree mismatch: {tree}")

    branch = run("git", "branch", "--show-current", cwd=IAM_REPOSITORY).stdout.strip()
    head = run("git", "rev-parse", "HEAD", cwd=IAM_REPOSITORY).stdout.strip()
    require(branch == "main", f"authoritative IAM branch changed: {branch}")
    if accepted:
        run("git", "merge-base", "--is-ancestor", IAM_HEAD, head, cwd=IAM_REPOSITORY)
    else:
        require(head == IAM_HEAD, f"authoritative IAM HEAD changed: {head}")
    run(
        "git",
        "diff",
        "--exit-code",
        KRATOS_BASELINE,
        "--",
        "go.mod",
        "go.sum",
        "api",
        "cmd",
        "configs",
        "internal",
        "tests",
        cwd=IAM_REPOSITORY,
    )


def verify_regeneration() -> None:
    with tempfile.TemporaryDirectory(prefix="dp2-01-regenerate-") as directory:
        run(
            "python3",
            str(EVIDENCE_DIRECTORY / "generate_fixed_inventory.py"),
            "--output-dir",
            directory,
            cwd=IAM_REPOSITORY,
        )
        for name, expected_hash in EXPECTED_INVENTORY_HASHES.items():
            committed = EVIDENCE_DIRECTORY / name
            regenerated = Path(directory) / name
            require(committed.read_bytes() == regenerated.read_bytes(), f"generated inventory drift: {name}")
            require(sha256(committed) == expected_hash, f"inventory hash mismatch: {name}")


def verify_inventories() -> None:
    sources = read_tsv("source-manifest.tsv")
    require(len(sources) == 68, f"source manifest count is {len(sources)}, want 68")
    require(len({row["path"] for row in sources}) == 68, "duplicate source path")
    for row in sources:
        require(bool(re.fullmatch(r"[0-9a-f]{40}", row["git_blob"])), f"bad Git blob: {row['path']}")
        require(bool(re.fullmatch(r"[0-9a-f]{64}", row["sha256"])), f"bad SHA-256: {row['path']}")
        require(int(row["bytes"]) > 0, f"empty fixed source: {row['path']}")

    migrations = read_tsv("migration-files.tsv")
    require(len(migrations) == 38, f"migration count is {len(migrations)}, want 38")
    require(sum(row["atlas_listed"] == "yes" for row in migrations) == 31, "atlas-listed migration count is not 31")
    unchecked = [Path(row["path"]).name for row in migrations if row["atlas_listed"] == "no"]
    require(
        unchecked
        == [
            "20260821_001_tenant_admin_invitation.sql",
            "20260825_001_tenant_admin_invitation_pending_unique.sql",
            "20260827_001_async_tasks_list_index.sql",
            "20260827_001_user_roles_single_role.sql",
            "20260828_001_instance_resource_rls_fix.sql",
            "20260831_001_async_tasks_rls_fix.sql",
            "20260901_001_gpu_chain_remaining_rls_fix.sql",
        ],
        f"unchecked migrations: {unchecked}",
    )

    operations = read_tsv("openapi-operations.tsv")
    require(len(operations) == 236, f"OpenAPI operation count is {len(operations)}, want 236")
    require(len({(row["method"], row["path"]) for row in operations}) == 236, "duplicate OpenAPI route")
    require(len({row["operation_id"] for row in operations}) == 236, "duplicate OpenAPI operationId")
    counts = {kind: sum(row["old_policy_source"] == kind for row in operations) for kind in ("public", "generated", "legacy")}
    require(counts == {"public": 8, "generated": 5, "legacy": 223}, f"OpenAPI classification counts: {counts}")
    derived = {(row["method"], row["path"], row["operation_id"]) for row in operations if row["operation_id_source"] == "derived"}
    require(derived == {("POST", "/auth/refresh", "refreshToken"), ("GET", "/branding", "getBranding")}, f"derived operations: {derived}")

    related = read_tsv("related-iam-operations.tsv")
    require(len(related) == 59, f"related IAM operation count is {len(related)}, want 59")
    related_counts = {kind: sum(row["contract"] == kind for row in related) for kind in ("core-v1", "services-v1")}
    require(related_counts == {"core-v1": 28, "services-v1": 31}, f"related operation counts: {related_counts}")
    require(len({(row["contract"], row["method"], row["path"]) for row in related}) == 59, "duplicate related route")

    rpcs = read_tsv("auth-service-rpcs.tsv")
    require([row["rpc"] for row in rpcs] == EXPECTED_RPCS, "14-RPC order or membership drift")
    require([int(row["ordinal"]) for row in rpcs] == list(range(1, 15)), "14-RPC ordinal drift")


def parse_markdown_rows(path: Path, prefix: str, columns: int) -> list[list[str]]:
    rows: list[list[str]] = []
    pattern = re.compile(rf"^\| ({prefix}\d{{3}}) \|")
    for line in path.read_text(encoding="utf-8").splitlines():
        if pattern.match(line):
            fields = [field.strip() for field in line.strip().strip("|").split("|")]
            require(len(fields) == columns, f"{path.name} {fields[0]} has {len(fields)} columns, want {columns}")
            require(all(fields), f"{path.name} {fields[0]} has an empty field")
            rows.append(fields)
    return rows


def verify_classification_and_deletion() -> None:
    matrix_path = EVIDENCE_DIRECTORY / "replacement-matrix.md"
    rows = parse_markdown_rows(matrix_path, "B", 9)
    require([row[0] for row in rows] == [f"B{number:03d}" for number in range(1, 61)], "replacement IDs are not B001-B060")
    allowed = {"保留语义", "目标实现替换", "允许 breaking 漂移", "最终删除"}
    require(all(row[2] in allowed for row in rows), "replacement matrix has an invalid classification")
    classification_counts = {kind: sum(row[2] == kind for row in rows) for kind in allowed}
    require(
        classification_counts == {"保留语义": 8, "目标实现替换": 24, "允许 breaking 漂移": 3, "最终删除": 25},
        f"classification counts: {classification_counts}",
    )
    impact_pattern = re.compile(r"02=(yes|no);03=(yes|no);04=(yes|no);05=(yes|no)")
    require(all(impact_pattern.fullmatch(row[8]) for row in rows), "replacement matrix has an invalid DP2 impact field")
    matrix = matrix_path.read_text(encoding="utf-8")
    for rpc in EXPECTED_RPCS:
        require(f"AuthService.{rpc}" in matrix, f"replacement matrix misses RPC {rpc}")

    deletions = parse_markdown_rows(EVIDENCE_DIRECTORY / "deletion-manifest.md", "D", 6)
    require([row[0] for row in deletions] == [f"D{number:03d}" for number in range(1, 29)], "deletion IDs are not D001-D028")
    require(all("DP2-" in row[3] or "PR0" in row[3] for row in deletions), "deletion row lacks a replacement gate")


def verify_initial_references() -> None:
    for pattern, expected_count in INITIAL_REFERENCE_COUNTS.items():
        result = run(
            "git",
            "-C",
            str(ANI_REPOSITORY),
            "grep",
            "-n",
            pattern,
            ANI_COMMIT,
            "--",
            "repo",
            check=False,
        )
        require(result.returncode == 0, f"initial reference missing: {pattern}")
        actual_count = len(result.stdout.splitlines())
        require(actual_count == expected_count, f"initial reference count {pattern}: {actual_count}, want {expected_count}")


def verify_markdown_links_and_text() -> None:
    markdown_files = sorted(EVIDENCE_DIRECTORY.glob("*.md"))
    require(markdown_files, "no Markdown evidence")
    link_pattern = re.compile(r"\[[^\]]+\]\(([^)]+)\)")
    for path in markdown_files:
        text = path.read_text(encoding="utf-8")
        require(text.endswith("\n"), f"missing final newline: {path.name}")
        for line_number, line in enumerate(text.splitlines(), start=1):
            require(line.rstrip() == line, f"trailing whitespace: {path.name}:{line_number}")
        for target in link_pattern.findall(text):
            if target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            target_path = target.split("#", 1)[0]
            require((path.parent / target_path).resolve().exists(), f"broken link in {path.name}: {target}")

    historical = (EVIDENCE_DIRECTORY / "historical-rls-negative.md").read_text(encoding="utf-8")
    require("FAIL / BLOCKED" in historical, "historical RLS status changed")
    require("SQLSTATE 42501" in historical, "historical RLS error missing")
    require("not_verified" in historical, "historical unverified results missing")
    require("not a runnable compatibility oracle" in (EVIDENCE_DIRECTORY / "README.md").read_text(encoding="utf-8"), "Oracle warning missing")


def verify_workspace_scope(accepted: bool) -> None:
    ticket_text = (IAM_REPOSITORY / TICKET).read_text(encoding="utf-8")
    expected_status = "resolved" if accepted else "claimed"
    require(f"**Status:** {expected_status}" in ticket_text, f"DP2-01 is not {expected_status}")

    claimed = run(
        "rg",
        "-l",
        r"^\*\*Status:\*\* claimed$",
        ".scratch/ani-iam-p2-direct/issues",
        cwd=IAM_REPOSITORY,
        check=False,
    ).stdout.splitlines()
    expected_claimed = [] if accepted else [TICKET]
    require(claimed == expected_claimed, f"claimed issue set: {claimed}")

    status_lines = run(
        "git",
        "status",
        "--short",
        "--untracked-files=all",
        cwd=IAM_REPOSITORY,
    ).stdout.splitlines()
    for line in status_lines:
        path = line[3:]
        require(path == TICKET or path.startswith(EVIDENCE_PREFIX), f"out-of-scope workspace change: {path}")

    staged = run("git", "diff", "--cached", "--name-only", cwd=IAM_REPOSITORY).stdout.splitlines()
    for path in staged:
        require(path == TICKET or path.startswith(EVIDENCE_PREFIX), f"out-of-scope staged path: {path}")

    run("git", "diff", "--check", cwd=IAM_REPOSITORY)
    run("git", "diff", "--cached", "--check", cwd=IAM_REPOSITORY)

    private_key_marker = b"-----" + b"BEGIN "
    for path in sorted(EVIDENCE_DIRECTORY.iterdir()):
        if path.is_file():
            require(private_key_marker not in path.read_bytes(), f"possible credential/private key in {path.name}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--accepted",
        action="store_true",
        help="verify the human-accepted resolved state before the DP2-01 commit",
    )
    args = parser.parse_args()

    verify_git_identity(args.accepted)
    print("gate: fixed object and IAM baseline\nresult: pass")
    verify_regeneration()
    print("gate: fixed inventory regeneration and hashes\nresult: pass")
    verify_inventories()
    print("gate: 236 operations, 59 related operations, and 14 RPCs\nresult: pass")
    verify_classification_and_deletion()
    print("gate: replacement and deletion completeness\nresult: pass")
    verify_initial_references()
    print("gate: initial zero-reference search baseline\nresult: pass")
    verify_markdown_links_and_text()
    print("gate: Markdown links and evidence text\nresult: pass")
    verify_workspace_scope(args.accepted)
    print("gate: workspace and staged-path scope\nresult: pass")
    print("overall: pass")
    print(f"human_checkpoint: {'accepted' if args.accepted else 'required'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
