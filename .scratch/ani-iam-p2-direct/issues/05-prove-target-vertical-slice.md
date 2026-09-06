# 05: 证明目标最小纵向链路

**What to build:** 使用目标契约和无 RLS 数据库跑通 `Password Login → Session → CheckPermission → 目标 Gateway → 一个受保护 API`，尽早判断目标架构是否闭环。

**Blocked by:** 02 / 冻结公开 OpenAPI 与 Operation Registry；03 / 冻结 IAM 与 Core 集成契约；04 / 建立无 RLS 持久化基础

**Status:** claimed

**Type:** enhancement

**Plan mapping:** DP2-1 / Go-No-Go A

**Baseline:** 02/03 的固定契约与 registry、04 的空库/受限 role 基础、固定目标 Gateway operation。

**Scope:** 最小 Human/Password/Session/Grant/Permission 数据与 use case、目标 gRPC service、Gateway 一次决策、一个受保护 Handler、稳定错误、同事务 Audit 和真实依赖 E2E。

**Out of scope:** OIDC、完整 Refresh/browser、Invitation、完整 Role 管理、API Key、Service Token、Core/NATS、五类调用方整体切换或 legacy fallback。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`internal/server/**`、`migrations/**`、`configs/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**` 中固定目标 operation/registry/测试，以及本事项和证据目录。

**Forbidden paths:** 已冻结的 `api/**` 和 `../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/services/**`、旧 Auth runtime；不得代码 fallback、默认 allow、RLS 或 superuser 业务查询。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/05-prove-target-vertical-slice/`

- [x] Password 成功与无效 Credential、Session/Grant 和 Access Token 可验证。
- [x] CheckPermission allow/deny、policy revision mismatch 和 Gateway `401/403/503/504` 稳定。
- [x] Authorized 路由只调用 IAM 一次，客户端 `x-ani-*` 被删除后才注入可信上下文。
- [x] mutation 与 Audit 原子；两 Tenant 负向和 query mutation 通过。
- [x] 使用真实独立 PostgreSQL/Redis、受限 runtime role 和目标 Gateway，不用 fake 代替 gate。

**Verification:** unit、contract、真实依赖 integration、Gateway E2E、故障注入、两 Tenant/query mutation 和空库 replay 全部通过；Artifact/配置固定。

**Stop conditions:** 目标链路无法在无 RLS/受限 role 下闭环；需要 legacy fallback、多次 IAM 决策、默认 allow 或修改已冻结契约。

**Recovery:** 删除隔离目标 route/config、数据库 fixture 和 Credential；主调用路径保持旧系统。

**Human checkpoint:** 证据完成后输出 Go/No-Go A 等待人工接受；不得自动领取 06、07 或 09。

## Claim record

- Claimed at: `2026-09-04T13:09:53+08:00`.
- Dependency status: DP2-02, DP2-03 and DP2-04 are `resolved`; no other Direct P2 ticket is `claimed`.
- IAM start: branch `main`, commit `e7ac8556196b2d0884a5686e18fe2473e68544c0`, clean worktree and index. DP2-04's real PostgreSQL, Atlas/sqlc and restricted-role foundation is the persistence baseline.
- ANI start: durable worktree `/home/chabking/workspace/ANI-direct-p2-01-05`, branch `codex/direct-p2-01-05`, commit `573d3735934f74f9f1eb78818cddefefd9f575eb`, clean worktree and index. It contains DP2-02 commit `a221a7b` and DP2-03 commit `573d373`.
- Fixed contracts: ANI OpenAPI/operation registry from DP2-02; IAM descriptor SHA-256 `df863beb3b095d1f01350c5334d80daf10cdf48083ce0e5663781171aa99a001`; Core descriptor SHA-256 `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a`; cross-project pins SHA-256 `33376182b2bcd2f0dd7c84bdf9790d492b6a643560a169e80c0fe63e9113c3b9`.
- Initial runtime dependencies: Go `go1.26.7-X:nodwarf5 linux/amd64`; Docker client/server `29.7.2`; PostgreSQL `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`; Redis `redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf`.
- Effective ANI path mapping: the ticket's `../ANI/repo/services/ani-gateway/**` scope is applied only to `/home/chabking/workspace/ANI-direct-p2-01-05/repo/services/ani-gateway/**`. The existing `/home/chabking/workspace/ANI/repo` checkout remains forbidden and untouched.
- Allowed paths remain the ticket's IAM scopes and the mapped target Gateway scope only. Frozen `api/**`, ANI `api/openapi/**`, deploy paths, other ANI services and legacy Auth remain forbidden. A required ANI documentation-closure path outside this scope is a stop condition requiring an exact path decision.
- Human-approved dependency-path extension (2026-09-04): `go.mod` and `go.sum` may change only to pin `github.com/alexedwards/argon2id v1.0.0`, `github.com/lestrrat-go/jwx/v3 v3.2.0`, and `github.com/redis/go-redis/v9 v9.22.0`, plus their resolver-required transitive module checksums. This approval does not extend `cmd/**`, ANI documentation paths, deployment paths, or any other ticket boundary.
- Human-confirmed external ANI checkout change (2026-09-04): `/home/chabking/workspace/ANI/repo/go.work.sum` belongs to another task. This Goal may ignore it but must not modify, move, stage, delete, stash, commit, or otherwise touch it.
- Human-approved composition-root path extension (2026-09-04): DP2-05 may additionally modify only `cmd/server/app.go`, `cmd/server/app_test.go`, `cmd/server/main.go`, and `cmd/server/main_test.go`. No other `cmd/**`, ANI documentation, deployment, or ticket path is added by this approval.
- Human-approved runtime-configuration and ANI documentation-closure extension (2026-09-05): DP2-05 may additionally modify only `internal/conf/conf.proto`, `internal/conf/conf.pb.go`, `internal/conf/validate.go`, `internal/conf/validate_test.go`, and the dedicated ANI worktree paths `repo/development-records/DP2-05-target-iam-vertical-slice.md`, `repo/development-records/README.md`, `repo/CURRENT-SPRINT.md`, and `ANI-06-开发计划.md`. This approval does not extend any other `internal/conf/**`, ANI code, deployment, service, API, or documentation path.

## Confirmed TDD seams

The Goal pre-agrees the target IAM gRPC services, framework-independent biz use cases, data ports backed by the real DP2-04 PostgreSQL/Redis dependencies, and the fixed ANI Gateway HTTP handler/operation-registry boundary. Tests observe behavior at those seams: no private call assertions and no fake substituted for the real-dependency or Gateway E2E gates.

## Execution plan

1. Inventory the fixed DP2-02 protected operation/registry and DP2-03 service methods, then read the ANI Gateway's controlling instructions, sprint/API/generator/test/development records before changing ANI.
2. Add one failing vertical test at a time for Password Login, Session/Grant/Access Token, permission allow/deny/revision mismatch, and atomic Audit; implement only the minimum target service/biz/data behavior needed by the tracer bullet.
3. Add Gateway RED tests for Public zero-decision, protected one-decision, hostile `x-ani-*` removal/trusted injection, and stable `401/403/503/504`; connect exactly one fixed protected operation without fallback.
4. Run the complete chain against task-owned PostgreSQL and Redis containers using the restricted runtime role, including dependency failures, two-Tenant negatives, query mutation, empty migration/seed replay, race/concurrency scope and clean generated diff.
5. Review Standards and ticket Spec independently, repair every blocker, stage only Allowed paths in both repositories, and produce the Go/No-Go A review package. Do not commit either DP2-05 change or mark the ticket resolved before human acceptance.

## Test plan

- IAM unit/table tests: valid and invalid Password, minimal Principal/Identity/Password state, Session/Grant creation, token verification, permission allow/deny and policy revision mismatch.
- Contract/service tests: only the three frozen services, stable gRPC/ErrorInfo mapping, no legacy Auth registration, and thin DTO/domain mappings.
- Real data tests: empty PostgreSQL replay/seed, restricted runtime role, mutation+Audit commit/rollback, PostgreSQL and Redis failures, two-Tenant negatives, Tenant predicate mutation, version/concurrency behavior.
- Gateway tests/E2E: fixed Public route performs zero IAM calls; one fixed protected operation performs exactly one decision; client `x-ani-*` is stripped before trusted context injection; deterministic `401/403/503/504` failure injection; no default allow or legacy fallback.
- Regression: targeted and full relevant Go suites in both repositories, race scope, Atlas/sqlc and frozen contract/registry generation without drift, `git diff --check`, staged-path, secret and build-artifact audits.

## Recovery plan

Before the Go/No-Go checkpoint, keep both repositories as explicit uncommitted/staged Allowed-path diffs. Every PostgreSQL/Redis dependency uses a task-owned disposable container, database/role or namespace and is removed after the run. Recovery removes only the isolated fixtures and reverts the two local diffs; it does not touch the existing ANI checkout, shared databases, legacy Auth, deployment, traffic, credentials or remote Git. After an accepted Go and local commits, recovery uses new revert commits against the exact DP2-05 IAM and ANI SHAs; never reset, stash, amend, force, push or deploy.
