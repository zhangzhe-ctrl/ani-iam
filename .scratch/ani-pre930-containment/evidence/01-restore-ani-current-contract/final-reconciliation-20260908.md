# ANI pre-9/30 containment final reconciliation

Date: 2026-09-08

## Accepted graph

- Branch: `codex/ani-pre930-containment`
- Initial containment commit: `fd4ede316f3734300381884908ebaa8bc8aaa925`
- Accepted main: `804db51a5f93605f9bbd4ac407f0489ecb1d187c`
- Accepted main tree: `28eb0508c93aab28e86e044a88ada9e19bf16830`
- Final merge commit: `19cc06832e2dd4d1b56fe448f31c21d77055e24d`
- Final tree: `4ba6a15ad0cddf0db66a25d695b082d47346aff1`
- Final parents: `fd4ede316f3734300381884908ebaa8bc8aaa925 804db51a5f93605f9bbd4ac407f0489ecb1d187c`

The merge preserved the platform component status and diagnostics implementation from accepted main. The four Observability operations map to current Gateway authz as `observability/read/platform` for `user` and `service`; the nine Tenant operations map to current `tenants/{create|read|update}/platform` for `user`. Direct P2 `x-ani-handler`, `x-ani-owner`, `x-ani-auth-classification`, and target authn/authz shapes were not retained in current ANI.

Core SDK, static API documentation, and Gateway authorization policy were regenerated twice without drift. Conflict markers, unmerged entries, out-of-union paths, and active Direct P2 target-runtime residue were absent before commit. No local test gate was run, per user instruction.

## Exact-head verification

- Pull request: `https://github.com/e92nf872rp/ANI/pull/152`
- Actions run: `https://github.com/e92nf872rp/ANI/actions/runs/34204529598`
- Exact head: `19cc06832e2dd4d1b56fe448f31c21d77055e24d`
- Result: `success`
- `pass`: Core Compatibility / Gateway Authz.
- `pass`: Go Build & Test.
- `pass`: Python AI Services.
- `pass`: Services Boundary / API / Docs Gate.
- `pass`: OpenAPI Spec Lint.
- `pass`: Dependency CVE Scan.
- `pass`: Required PR Gates.

At final verification, remote main remained exactly `804db51a5f93605f9bbd4ac407f0489ecb1d187c`; PR #152 was `OPEN`, `MERGEABLE`, and `REVIEW_REQUIRED`. Node.js 20 deprecation messages and the existing accepted Services import allowlist were non-blocking annotations. The PR was not merged, and no deployment, cutover, data mutation, or credential action was performed.
