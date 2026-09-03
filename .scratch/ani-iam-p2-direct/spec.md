# ANI IAM Direct P2 规格

Status: accepted

本规格是当前执行方向的权威材料。它综合已接受的目标设计、事项01–04的历史证据，以及用户于 2026-09-03 作出的“停止 CP0/P1、改走 Direct P2、尽早验证能否切换”决定；规格与 ticket plan 已于同日获得人工接受。旧规格保留在 `.scratch/ani-iam-rebuild/spec.md` 作为决策快照，不再作为新事项的执行入口。

ANI 来源候选固定为 Git object `0cedae825a489d936cf41815dc27f278f6d3213c`。动态 `main`、当前分支和工作树不属于基线。该对象只能作为来源与契约盘点输入，不能作为可运行兼容 Oracle：旧 Auth migration 的 RLS 已被事项04证明会让受限 runtime role 对同 Tenant 正向访问也被拒绝。

## Problem Statement

项目尚未上线且没有真实用户，目标是彻底替换旧 IAM，而不是长期兼容旧 Auth。原 CP0/P1 路线试图先证明旧 wire、旧存储和旧调用方语义可由 Kratos 同构承载；事项04的真实依赖调查表明，冻结旧 RLS 本身无法满足受限 runtime role 的基本正向访问。继续修旧 RLS 可以恢复兼容实验，却不会降低目标无 RLS IAM 的核心切换风险。

最大的交付风险是先投入完整 IAM 功能，直到末期才发现目标 Gateway、IAM、Core 契约和五类调用方无法整组切入或回退。因此 Direct P2 必须把两个失败成本低的判断前置：目标纵向架构是否闭环，以及切换关键功能能否整组替换旧 Auth。

## Solution

使用独立 `iam-service` 和目标版 ANI Gateway，直接建设无 RLS 的目标架构，并按以下前置整理与五段推进：

0. **DP2-00 实现基线：** 归档未投入使用的事项03/04实验，在 Direct P2 分支移除旧 Auth compatibility 与旧 RLS/Redis 调查代码；保留历史证据和旧部署级回退目标。
1. **DP2-0 来源与契约：** 固定 ANI 来源、公开 OpenAPI/operation registry、IAM Proto、Core Lifecycle/Bootstrap/Snapshot 契约和删除清单。
2. **DP2-1 目标纵向证明：** 在独立无 RLS 数据库和受限 runtime role 上跑通 `Password Login → Session → CheckPermission → 目标 Gateway → 一个受保护 API`。这是 Go/No-Go A。
3. **DP2-2 切换关键功能：** 完成现有调用面必需的 Password、OIDC、Refresh、API Key、Service Token、Membership/Authorization、Core Lifecycle/NATS，并在 Gateway、Envoy、Inference、Console、BOSS 上做独立证据；随后在隔离测试轨道整组切入和回退。这是 Go/No-Go B。
4. **DP2-3 完整目标能力：** 只有 Go/No-Go B 通过并经人工接受后，才补齐 Invitation、完整 Role/Platform Admin、Recovery、Audit 查询、全局 Idempotency 和完整 UI/E2E。
5. **DP2-4 最终重建与删除：** 分别经人工确认后，重建测试数据、使旧 Credential 失效、最终整组切换，再删除旧 Auth/Proto/compat/RLS/重叠能力并完成功能验收。

## Architecture and Ownership

- IAM 独立拥有 Principal、Identity、Credential、Session、Tenant Access、Membership、Role、Invitation、Service Principal、API Key、授权决策和 IAM Security Audit。
- Core Control 独占 Tenant Lifecycle、Tenant ID 和平台资源事实；IAM 只保存版本化只读投影，不双写共享状态。
- ANI OpenAPI 是唯一公网契约；Console/BOSS 只能通过 Gateway，Gateway 每个受保护请求最多做一次 IAM 决策。
- IAM 只注册目标 `AuthenticationService`、`AuthorizationService` 和 `IAMAdminService`；不延续旧 `auth.v1.AuthService`。
- 目标 PostgreSQL 不使用 RLS；隔离依赖非空 TenantScope、窄 repository、显式 `tenant_id` SQL、复合键/外键、事务边界和两 Tenant 负向测试。
- 目标代码不实现 legacy fallback 或双写。早期演练的恢复是部署级切回固定旧镜像和配置。
- 状态变更与 Security Audit 同事务；公开 mutation 使用统一 Idempotency Ledger。
- Core/IAM/ANI 之间只使用固定 Commit、Tag、descriptor 或 digest，不动态消费 `main`、`latest` 或另一个工作树。

详细领域、不变量、接口、安全和数据决定继续以 `docs/plans/plan-iam-service-refactor.md`、`CONTEXT.md` 和 accepted ADR 为依据；若出现冲突，必须先更新当前规格或 ADR，不能由实现自行选择。

## User-visible and Caller Outcomes

- Human 可通过 Password 或可信 OIDC 建立目标 Session；Refresh 单次旋转，reuse 只撤销对应 family/boundary。
- Gateway 对 Public 路由调用 IAM 0 次，对 authenticated/authorized 路由调用 1 次，并稳定映射 `401/403/429/503/504`。
- Envoy Adapter 独立验证目标 Credential 的 allow、deny、无效和依赖失败，不能用 Gateway 成功替代。
- Inference 使用 mTLS/SPIFFE 换取短期 audience-bound Service Token，不保留共享 mint Secret。
- Console 与 BOSS 使用隔离的 Cookie、Audience、Session 时限和 Platform Membership 边界。
- Core Lifecycle 投影出现 gap 或 stale 时只冻结受影响 Tenant，并通过固定 Snapshot 契约修复。
- 所有旧 Credential 在最终测试切换时明确失效；该动作不在计划接受或早期演练中自动授权。

## Early Gates

### Go/No-Go A: target vertical slice

必须使用真实独立 PostgreSQL/Redis、受限 runtime role 和目标契约，至少验证：

- Password 成功和无效 Credential；
- Session/Grant 创建及同事务 Audit；
- CheckPermission allow/deny；
- Gateway 一个公开受保护 operation；
- `401/403/503/504` 和 policy revision mismatch；
- 两 Tenant 负向访问与 query mutation；
- 从空库重放 migration/seed。

失败即停止 Direct P2 实现并重新评估架构，不扩大到完整功能。

### Go/No-Go B: cutover-critical rehearsal

必须覆盖当前切换关键调用面，并在隔离测试轨道执行一次整组切入和部署级回退：

- Gateway、Envoy、Inference、Console、BOSS 各自有独立 E2E；
- Password、OIDC、Refresh/Logout、API Key、Service Token、Membership/Authorization、Core Lifecycle/Bootstrap 可用；
- 新代码没有 legacy fallback，旧 Auth 仅作为固定部署回退目标；
- 不删除旧资产、不失效旧 Credential、不重建主测试数据；
- 固定镜像、契约 digest、配置、数据 seed 和恢复步骤。

失败即停止，不进入完整管理、Recovery、Audit/Idempotency 聚合或最终切换。

## Testing Seams

- 契约：OpenAPI lint/generation/breaking、Buf breaking、descriptor/digest 和 operation registry completeness。
- 数据：空库 migration replay、受限 role、两 Tenant 正反向测试、query mutation、事务/Audit 原子性。
- 领域：table-driven unit/property/concurrency tests，明确状态机、last-admin、refresh reuse、invitation race 和 idempotency conflict。
- 真实依赖：PostgreSQL、Redis、Dex、NATS、mTLS/SPIFFE；fixture/fake 证据必须单独标识，不能替代 live gate。
- 调用方：Gateway、Envoy、Inference、Console、BOSS 五类分别验证，不能相互替代。
- 切换：隔离演练与最终测试切换分别留存固定 Artifact、前后状态、恢复结果和人工接受。
- 结论只使用 `pass`、`fail`、`not_verified`；功能完成不等于 Production Ready。

## Human Checkpoints

- 当前规格和 ticket graph 已由用户接受并发布为 `ready-for-agent`；用户随后于 2026-09-03 精确接受 DP2-00 的本地归档与实现基线清理，DP2-01 仍未领取。
- Go/No-Go A 和 B 的证据分别需要人工接受；前一个通过不自动授权后一个阶段。
- 任何契约 breaking 发布、真实 Core/NATS 写入、测试轨道切入、数据重建、Credential 失效、最终切流和旧资产删除，都需要针对精确事项、环境和动作的单独确认。
- 同一时间只允许一个会改变代码、数据、契约或外部状态的事项为 `claimed`。

## Out of Scope

- 不修复旧 RLS 以继续 CP0/P1，不把事项04改写为 pass。DP2-00 只移除本仓库未投入使用的实验代码；旧 ANI/Auth 部署资产仍由 DP2-19 的独立人工确认控制。
- 不设计旧数据逐行迁移、长期双写、兼容视图或代码级 fallback。
- 不创建或操作生产环境，不宣称 Production Ready。
- 不实现 Tenant/Principal Purge、member_count Quota/TCC、delegated Role administration、Support Session、Human CLI Refresh、公共 ANI JWKS、通用 IAM domain-event outbox、Audit export/SIEM/WORM。
- 当前计划不自动实施 Core Control、NATS 基础设施、契约 breaking、部署或删除。

## Ticket Publication

已接受的纵向拆分、依赖边和交付行为位于 `ticket-plan.md`。DP2-01–20 已发布到 `.scratch/ani-iam-p2-direct/issues/`；用户随后增加并领取一次性前置事项 DP2-00。DP2-00 解决前，DP2-01 不得领取；该清理不自动启动任何 Direct P2 功能事项。
