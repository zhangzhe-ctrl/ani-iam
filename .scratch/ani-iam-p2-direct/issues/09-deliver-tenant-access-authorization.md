# 09: 交付 Tenant Access、Membership 与目标授权

**What to build:** 交付切换所需的 Tenant Access、Membership、基础 Role/Binding、Permission Catalog 和 Gateway 单次授权，使目标 TenantScope 与 typed obligation 可验证。

**Blocked by:** 05 / 证明目标最小纵向链路（Go/No-Go A 已人工接受）

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-2 / tenant authorization

**Baseline:** 02 registry/policy revision、03 IAM/Core 契约、04 persistence、05 一次决策纵向链路。

**Scope:** Tenant Access 基础状态、Human Membership、System Role/Binding、Permission Catalog、last-admin 基础保护、TenantScope/Platform Capability repository、ValidatePrincipal/CheckPermission、Gateway trusted context、typed obligation 和 Audit。

**Out of scope:** 完整 Invitation/Custom Role/Platform Admin/Recovery、Core/NATS 真链路、Service Principal/API Key、默认 allow 或 legacy fallback。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**` 中目标授权路径，以及本事项和证据目录。用户于 2026-09-08 另外精确授权 generator-first 修复 `../ANI/repo/api/openapi/operation-registry.v1.json` 和 `../ANI/repo/services/ani-gateway/internal/authz/zz_generated_target_operation_registry.go`；随后精确授权 IAM `cmd/server/app.go`、`cmd/server/app_test.go`、`internal/server/workload_identity.go`、`internal/server/workload_identity_test.go`，以及 ANI `../ANI/repo/pkg/ports/target_iam.go`、`../ANI/repo/pkg/adapters/iam/client.go`、`../ANI/repo/pkg/adapters/iam/client_test.go`。用户又明确批准 generator-first 同步 ANI 的 `../ANI/repo/pkg/generated/pb/iam/v1/authentication_service.pb.go`、`../ANI/repo/pkg/generated/pb/iam/v1/authentication_service_grpc.pb.go`、`../ANI/repo/pkg/generated/pb/iam/v1/iam_descriptor.pb`、`../ANI/repo/api/proto/tenant/integration/v1/contract_pins.json`、`../ANI/repo/api/proto/tenant/integration/v1/testdata/iam_password_login.v1.json`，以及 Feature-batch 闭环文档 `../ANI/repo/development-records/DP2-09-tenant-access-authorization.md`、`../ANI/repo/development-records/README.md`、`../ANI/repo/CURRENT-SPRINT.md`、`../ANI/ANI-06-开发计划.md`。这不授权修改 OpenAPI/policy/replacement 输入、部署或其他 ANI/IAM 路径。

**Forbidden paths:** `api/**`、`../ANI/repo/api/openapi/**` 中除上述单一精确生成文件之外的所有路径、`deploy/**`、其他 `../ANI/repo/**`；不得 Principal 直接授权、NULL Tenant、Boolean bypass、同步回调 Core、字符串推导 Permission 或默认 allow。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/09-deliver-tenant-access-authorization/`

- [x] 普通租户授权验证 Principal、Tenant Access、Membership、Lifecycle projection freshness 和 Permission。
- [x] Gateway Authorized 路由只调用 CheckPermission 一次并注入最小可信上下文。
- [x] Permission/operation/policy mismatch fail closed，稳定映射 `401/403/503/504`。
- [x] TenantScope 非空且 repository 无 unscoped 查询；两 Tenant/obligation/last-admin 负向成立。

**Verification:** unit、真实数据库、registry 静态门禁、Gateway E2E、两 Tenant/query mutation、obligation、并发 last-admin 和错误映射通过。

**Stop conditions:** 需要多次 IAM 决策、同步 Core、全局 Role、Principal 直接绑定、平台 bypass 或修改冻结契约。

**Recovery:** 清理隔离 Tenant Access/Membership/Binding 和目标 Gateway route；不改变主调用路径。

## Claim record

- Claim time: `2026-09-08`.
- IAM start identity: `codex/direct-p2-06-14@56ab0fcc7c6117f11e0231e8d37a127d0c2f0a57`; worktree/index clean and `git diff --check` passed.
- Dependency state: DP2-05 Go/No-Go A is accepted and `resolved`; DP2-06, DP2-07 and DP2-08 are also `resolved`; no other Direct P2 ticket is `claimed`.
- Human-accepted ANI baseline: `9bfedfd04c75533e01fa3d88419e7a3454f79404`, the direct successor of PR #145 merge commit `50f7b422707c2ab78462bd9bb8186bae018a14fe` by PR #149. Dedicated worktree `/home/chabking/workspace/ANI-direct-p2-06-14` is `codex/direct-p2-06-14@9bfedfd04c75533e01fa3d88419e7a3454f79404` and clean.
- Baseline drift: `services/ani-gateway/tools/dp2_operation_registry.py --check` reports the two generated target-registry artifacts stale after the earlier removal of nine email-notification operations. The accepted repair changes only the two exact generated outputs listed above; frozen inputs remain byte-unchanged.
- Frozen TDD seams: target `iam.v1.AuthorizationService/CheckPermission`, target `iam.v1.AuthenticationService/ValidatePrincipal`, the Gateway target-IAM HTTP middleware boundary, and restricted-role PostgreSQL repository integration. Tests observe stable DTO/status/context or durable state through these public/accepted seams; private helpers are not the acceptance seam.
- Real dependencies: isolated PostgreSQL with separate migration owner and restricted runtime role; the existing target Gateway and IAM processes for cross-process E2E. Redis is not an authorization source of truth for this ticket; Core/NATS lifecycle delivery is out of scope and lifecycle projection rows are explicit test starting state.
- Execution plan: repair and verify the accepted ANI registry outputs, run the full required ANI baseline gates, then deliver vertical RED→GREEN slices for catalog/scope, authorization state evaluation, Tenant Access/Membership/Role mutations with transactional Audit and last-admin protection, and finally Gateway single-decision/trusted-context/obligation behavior.
- Test plan: unit/public-service tests, empty-schema and restricted-role PostgreSQL integration, two-Tenant and query-mutation negatives, concurrent last-admin transitions, policy/operation/permission mismatch, typed obligation, stable error mapping, and real IAM↔Gateway process E2E; run complete IAM/ANI regressions and generation drift checks before review and commit.
- Recovery: before a ticket commit, discard only this ticket's changes in the dedicated worktree by explicit reviewed paths or remove task-owned isolated test resources; never reset, stash, overwrite, or touch `/home/chabking/workspace/ANI/repo`. After commits, recovery is a normal reviewed revert of the ticket-specific commits.
- Human gate history: the user approved the exact three ANI port/adapter paths and four IAM composition/workload-identity paths on 2026-09-08. IAM composition and the exact ten-RPC Gateway workload capability list are now focused-test `pass`.
- Contract-sync gate accepted: the user approved the five exact descriptor/generated/pin/fixture paths and the four exact ANI Feature-batch documentation paths. Synchronize only the frozen IAM artifact from `74d7e441435d35dc66a733c8bf4a93129b26de3f` (present unchanged at the IAM ticket start), regenerate with the pinned toolchain, and preserve all unrelated Core contract pins and fixtures.
- Closure: review-repaired full IAM integration and race are `pass` (274.654s / 243.728s); ANI `make test`, registry drift check, document entrypoints, and isolated complete-candidate `make validate-services` are `pass`; independent Standards and Spec final reviews are `pass`. The ANI half is frozen in local commit `4ff73e09c16df706af2aacdff6d763ed4aeaa873`. Cluster certificate lifecycle, final-client-IP preservation, obligation owner enforcement, callers, deployment, and cutover remain `not_verified` as scoped.
