# 02: 冻结公开 OpenAPI 与 Operation Registry

**What to build:** 冻结目标公开 IAM 路径及其唯一 Owner、认证分类、Permission、错误和 typed obligation，使 Gateway 能从契约生成 registry 并 fail closed。

**Blocked by:** 01 / 冻结 Direct P2 来源与替换基线

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-0 / public contract

**Baseline:** 01 人工接受的来源/调用面/删除清单、当前 Direct P2 规格和目标设计。

**Scope:** ANI 公网 OpenAPI、`x-ani-authz`/exposure 标注、operation/owner/authn/authz/obligation、生成 registry/policy revision、稳定公开错误、SDK breaking 影响和 replacement gates。

**Out of scope:** IAM 业务实现、Core runtime、调用方切流、旧接口删除或兼容 fallback。

**Allowed paths:** `../ANI/repo/api/openapi/**`、`../ANI/repo/services/ani-gateway/**` 中生成器/registry/契约测试、`../ANI/repo/pkg/generated/**`、`../ANI/repo/frontends/console/src/api/**`、`../ANI/repo/frontends/boss/src/api/**`，以及本事项和证据目录。

**Approved Allowed-path extension (2026-09-04):** 用户在 DP2-02 breaking checkpoint 审核后，精确批准加入 `repo/sdks/core/go/anisdk/client.go`、`repo/sdks/core/java/src/main/java/com/kubercloud/ani/core/ApiClient.java`、`repo/sdks/core/python/kubercloud_ani_core/client.py`、`repo/sdks/core/sdk-metadata.json`、`repo/sdks/core/typescript/src/index.mjs`、`repo/sdks/core/typescript/src/index.ts`，以及 Feature-batch 文档 `repo/development-records/DP2-02-public-iam-operation-registry.md`、`repo/development-records/README.md`、`repo/CURRENT-SPRINT.md`、`ANI-06-开发计划.md`。该扩展只用于同源生成物与 ANI 强制文档闭环，不授权 Gateway 运行时接线、调用方切换、部署或发布。

**Forbidden paths:** 本仓库 `internal/**`、`migrations/**`、`deploy/**`；`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/deploy/**`；不得实现业务、切流或删除运行能力。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/02-freeze-public-openapi-registry/`

- [x] 每个目标 operation 只有一个 Gateway Handler 和一个后端 Owner。
- [x] Public、Authenticated-only、Authorized 分类完整，未知/缺失/版本不匹配 fail closed。
- [x] Permission 和 typed obligation 可生成；缺少 obligation Handler 的 operation 不可注册。
- [x] `401/403/409/429/503/504` 映射和 stable ErrorResponse 明确。
- [x] OpenAPI/SDK breaking 与删除清单可独立审查。

**Verification:** OpenAPI lint/generation/breaking、registry completeness、owner/obligation、stable error 和 clean generated diff 检查通过。

**Stop conditions:** Owner、Permission 或 obligation 不唯一；需要在契约冻结时提前删除运行能力；生成物不能固定到精确摘要。

**Recovery:** 回退未发布契约和生成物；不改变当前调用方路由。

**Human checkpoint:** 任何 breaking 契约的提交、发布或调用方消费前，需要针对精确 diff 的人工确认。

## Claim record

- Claimed at: 2026-09-03 (Asia/Shanghai)
- IAM start: branch `main`, commit `da3a7af2fb1ffa1d86f167666cccc951f2ddbdcd`, clean worktree/index
- ANI worktree: initially `/tmp/ani-direct-p2-01-05`, moved with explicit user authorization to durable `/home/chabking/workspace/ANI-direct-p2-01-05`; branch `codex/direct-p2-01-05`, commit `0cedae825a489d936cf41815dc27f278f6d3213c`, tree `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8`. Native cross-filesystem `git worktree move` was unavailable, so the exact directory was moved and repaired with `git worktree repair`; pre/post staged-diff SHA-256 remained `867dccce0ac648967e5ee2188fc46ee6e08b059bb37f3d8fb3fd0a6600508993`.
- Toolchain: Go `go1.26.7-X:nodwarf5 linux/amd64`; Python `3.14.7`; PyYAML `6.0.3`; `openapi-typescript` lockfile version `7.13.0`
- Fixed generator inputs: `api/openapi/v1.yaml` SHA-256 `b6a2dc1f9c596555fcc164240d7a5533a04a128b8e19a4d7af1c53e41ab9415b`; existing Gateway registry generator SHA-256 `b1c6ca039de158fa36f7aaa644e5c3b5df31c87ffaf49db159e779aaa19cf006`; SDK generator SHA-256 `f9d35b5f8ac60bcbcf04a5640e379b96ea1bb24915cd2869e50c9b9fe603b056`
- Allowed paths: ANI `repo/api/openapi/**`; only generator/registry/contract-test changes under `repo/services/ani-gateway/**`; `repo/pkg/generated/**`; `repo/frontends/console/src/api/**`; `repo/frontends/boss/src/api/**`; the six exact Core SDK outputs and four exact Feature-batch documentation files listed in the approved extension above; this ticket and `.scratch/ani-iam-p2-direct/evidence/02-freeze-public-openapi-registry/**`

## Execution plan

1. Freeze the accepted DP2-01 operation/replacement inventory and the exact OpenAPI/generator inputs.
2. Add contract-first lint, breaking, registry/owner/classification/obligation/error and request-chain gates, and save the initial failing results.
3. Change the OpenAPI source, regenerate the immutable operation registry and SDK artifacts with pinned commands, then verify a second generation has no drift.
4. Run focused and repository regressions, inspect the complete staged diff, record hashes and SDK/caller impacts, and pause at the breaking-contract checkpoint before any commit.

## Test plan

- OpenAPI YAML/lint/generation and exact breaking comparison against `0cedae825a489d936cf41815dc27f278f6d3213c`.
- Registry completeness, unique handler/owner, complete Public/Authenticated-only/Authorized classification, Permission/scope and typed-obligation handler coverage.
- Fail-closed unknown/missing/revision-mismatch behavior; stable `401/403/409/429/503/504` `ErrorResponse`; forged `x-ani-*` stripping; Public zero-IAM-call and protected at-most-one-decision contract checks.
- SDK regeneration and impact diff for Gateway, Console and BOSS; clean second generation; relevant Go/Python tests; `git diff --check` and staged-path audit.

## Recovery plan

No current caller, deployment, registry or remote artifact is changed. Before acceptance, recovery is to preserve the evidence and remove only this ticket's uncommitted allowed-path changes with an explicitly reviewed reverse patch; after an accepted local commit, recovery is a new revert commit against its exact SHA. No reset, stash, force operation, push, deploy or cutover is permitted.

## Checkpoint result

- Target OpenAPI/registry/generator/browser-schema gates: `pass`.
- Exact fixed-object breaking report: `pass` with 263 machine-readable breaking rows, 59 added operations, no removed operation, six operationId changes, 45 added schemas and no removed or structurally changed existing schema.
- Four-language SDK isolated generation: `pass`; Java compile/run: `not_verified`.
- Target Gateway runtime wiring and concrete new handler implementation: `not_verified`, intentionally deferred to implementation/DP2-05.
- Legacy `validate-gateway-authz`, `validate-core-api-compatibility` and `validate-auth-contract`: `fail` against accepted target semantics; replacement gates and exact assertions are recorded in evidence.
- Human acceptance of the exact breaking diff: `pass` (2026-09-04 checkpoint response: “我已审核” and instruction to continue the Goal).
- Exact Allowed-path expansion for six `repo/sdks/core/**` generated files and four ANI Feature-batch documentation paths: `pass` (same checkpoint response).
- Accepted ANI local commit: `a221a7b50c2cfdb13f04c13f154338d836a48af3` (`feat(dp2-02): freeze public iam operation registry`); post-commit ANI worktree status: clean.
- Resolved at: 2026-09-04 (Asia/Shanghai), after the accepted breaking/path checkpoint and successful final commit gates.

Evidence: `.scratch/ani-iam-p2-direct/evidence/02-freeze-public-openapi-registry/`.
