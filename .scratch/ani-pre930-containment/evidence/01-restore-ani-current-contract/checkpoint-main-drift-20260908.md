# ANI pre-9/30 containment checkpoint — main drift

Date: 2026-09-08

## Published candidate

- Branch: `codex/ani-pre930-containment`
- Original accepted base: `caa2a5e72fad98215a5ea26696e453e5c2ef6523`
- Original base tree: `839d673535f828f446d3fb8129b5874da24df00d`
- Candidate commit: `fd4ede316f3734300381884908ebaa8bc8aaa925`
- Candidate tree: `f02e7d62e4523400a49eb40c329d92ff55287401`
- Staged binary diff SHA-256 before commit: `c813904b1346aafe34b15fb33591482ba77a03322603eca8549f44e2f1b990f9`
- Path audit: 80 paths, exactly the PR #145 78-path set plus `.github/workflows/ci.yml` and `repo/development-records/ANI-IAM-PRE930-CONTAINMENT.md`.
- Pull request: `https://github.com/e92nf872rp/ANI/pull/152`

## Verification on exact candidate

GitHub Actions run `34202658984` (`https://github.com/e92nf872rp/ANI/actions/runs/34202658984`) completed with `success` on exact head `fd4ede316f3734300381884908ebaa8bc8aaa925`:

- `pass`: Core Compatibility / Gateway Authz, including both `make validate-core-api-compatibility` and `make validate-gateway-authz`.
- `pass`: Go Build & Test.
- `pass`: Python AI Services.
- `pass`: Services Boundary / API / Docs Gate.
- `pass`: OpenAPI Spec Lint.
- `pass`: Dependency CVE Scan.
- `pass`: Required PR Gates.

Local test gates were not run, per user instruction. Local generation completed twice with zero drift; `git diff --check`, path audit, and added-line credential-pattern audit passed.

## Drift stop

While the run was executing, ANI main advanced to:

- New main: `804db51a5f93605f9bbd4ac407f0489ecb1d187c`
- New tree: `28eb0508c93aab28e86e044a88ada9e19bf16830`
- Commit: `feat(observability): 平台组件状态与组件诊断（指标/日志）只读接口 (#148)`

The new commit overlaps the containment candidate in 18 paths: planning/current-state docs, Core OpenAPI, target registry artifacts, Core SDKs/docs, generated Gateway policy/target registry, router and Gateway main. GitHub now reports PR #152 as `CONFLICTING` and `REVIEW_REQUIRED`.

No merge, rebase, new commit, deployment, cutover, data change, credential action, or PR merge was performed after detecting the drift. Human acceptance of exact main `804db51a5f93605f9bbd4ac407f0489ecb1d187c` is required before reconciliation.
