# 01: 冻结 Human / Workload 统一主体架构

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-16 文档整理；WR-17 最小契约与输入冻结。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在不修改产品代码、公开契约、数据库或外部仓库的前提下，落盘 `Human Principal | Workload Principal` 的统一领域模型、替代 ADR、目标契约/持久化/运行时蓝图、同步 gRPC/HTTP 与异步 NATS Workload 身份方案和新的可执行事项图。

**Blocked by:** 无

**Status:** wontfix

**Type:** enhancement

**Accepted inputs:** 用户于 2026-09-09 明确确认不存在不可破坏的外部消费者和真实数据，并接受 Service Principal 的破坏性重构以换取架构一致性与简洁性。

**Baseline:** `main@cd38cd90bca3e9d83af09a381051b895d82ae94f`，tree `a3668037d43bff9a3d938b9cf07500402e4ebd70`。开始时 worktree 保留 DP2-11 的既有暂停现场：`.scratch/ani-iam-p2-direct/issues/11-deliver-workload-service-token.md` 已修改，`.scratch/ani-iam-p2-direct/evidence/11-deliver-workload-service-token/` 未跟踪；本事项不得覆盖或改写其中的历史证据。

**Goals:**

- Principal 类型只表达自然人或软件执行体：`human | workload`；不再保留独立 Service Principal 类型。
- 原 Service Principal 明确定义为 Tenant-owned Workload；内部 Gateway、Inference、Session Gateway、Worker 等定义为 Platform-owned Workload。
- Principal type、Owner、Workload Identity、Identity Binding、Credential、Authority 和单次 Execution Context 成为七个正交概念，禁止由凭证类型或 Owner/Identity 暗示权限。
- 所有同步跨服务业务 gRPC/HTTP 建立统一的直接 caller Workload 身份与授权规则，并区分 `caller_workload` 和 `on_behalf_of`；异步 NATS 使用 broker ingress identity + subject-owner/ACL evidence，不伪造 TLS peer 传播。
- 因无兼容消费者和真实数据，规划一次性 v1/schema refoundation，不规划双写、兼容 shim、逐行数据迁移或长期双版本。

**Non-goals:** 本事项不修改或验证产品 Proto、migration、runtime/config、部署或任何其他仓库实现；不选择真实 SPIFFE trust domain，不执行数据库重建、Credential 失效、SPIRE 注册、切流或删除，也不把文档设计声称为运行时通过。

**Allowed paths:**

- `AGENTS.md`
- `CLAUDE.md`
- `README.md`
- `CONTEXT.md`
- `docs/adr/0001-allow-breaking-core-v1-for-prelaunch-iam-rebuild.md`
- `docs/adr/0004-coordinate-tenant-lifecycle-and-iam-asynchronously.md`
- `docs/adr/0005-share-infrastructure-without-sharing-iam-project-code.md`
- `docs/adr/0006-treat-september-v1-as-test-environment-delivery.md`
- `docs/adr/0007-remove-postgresql-rls-from-target-iam.md`
- `docs/adr/0008-model-tenant-authorization-with-explicit-relations.md`
- `docs/adr/0009-generate-permissions-and-constrain-role-management.md`
- `docs/adr/0011-use-global-human-principals-and-boundary-scoped-tokens.md`
- `docs/adr/0012-use-short-boundary-tokens-and-rotating-refresh-families.md`
- `docs/adr/0013-use-durable-session-grants-instead-of-a-jwt-blocklist.md`
- `docs/adr/0014-keep-iam-audit-transactional-and-append-only.md`
- `docs/adr/0015-keep-browser-refresh-tokens-in-http-only-cookies.md`
- `docs/adr/0016-use-one-generated-authorization-gate-at-the-gateway.md`
- `docs/adr/0017-use-explicit-iam-lifecycle-states-instead-of-generic-soft-delete.md`
- `docs/adr/0018-separate-principal-types-and-persist-integration-state-explicitly.md`
- `docs/adr/0021-separate-functional-validation-from-production-readiness.md`
- `docs/adr/0022-unify-software-actors-as-workload-principals.md`
- `docs/plans/plan-iam-service-refactor.md`
- `docs/plans/plan-iam-kratos-phased.md`
- `docs/plans/plan-iam-decision-traceability.md`
- `docs/plans/plan-workload-principal-refoundation.md`
- `docs/agents/issue-tracker.md`
- `docs/agents/triage-labels.md`
- `docs/agents/domain.md`
- `.scratch/ani-iam-p2-direct/spec.md`
- `.scratch/ani-iam-p2-direct/ticket-plan.md`
- `.scratch/ani-iam-p2-direct/issues/11-deliver-workload-service-token.md` 到 `.scratch/ani-iam-p2-direct/issues/20-complete-functional-acceptance.md`，仅允许更新 supersession 状态、依赖和说明，不得改写既有证据
- `.scratch/ani-iam-workload-refoundation/**`

**Read-only inputs:** 当前 `api/**`、`internal/**`、`migrations/**`、`configs/**`、`deploy/**`、Direct P2 全部历史证据，以及 ANI、Session Gateway 和其他独立服务当前源码/契约/设计。

**Forbidden paths and actions:**

- 不修改 `api/**`、`internal/**`、`migrations/**`、`configs/**`、`deploy/**`、`go.mod`、`go.sum` 或生成文件。
- 不修改 ANI、Session Gateway、Notification 或其他仓库。
- 不运行数据库重建、Credential 失效、部署、切流、集群操作、Git commit/push/tag/PR。
- 不创建兼容映射把 `service` 偷偷解释为 `workload`，不把 NetworkPolicy、namespace、Header 或共享 Secret 当作 Workload Identity。

**Acceptance:**

- `CONTEXT.md` 对 Workload Principal、Owner、Workload Identity、Workload Identity Binding、Workload Grant、Workload Access Token、Workload Invocation、Delegated Subject、Platform Administration Lineage/Boot/Ready/Forward Recovery、Platform First Administrator Ceremony 和 Login-capable Platform Administrator 的定义无重叠且不含实现细节。
- 新 ADR 明确取代 ADR 0018 中 Service/Workload 双类型决定，并记录接受 breaking 的原因与后果。
- refoundation 计划给出目标对象、不变量、信任链、深 Module、契约/schema/runtime 变化、Session Gateway 样例、失败语义、验证矩阵、回滚和人类检查点。
- 旧 Direct P2 不再显示可继续执行的错误 frontier；新事项图顺序覆盖契约/schema reset、Tenant-owned Workload/API Key、Platform-owned Workload/SPIFFE/Token、跨服务 gRPC、Core lifecycle 和后续验收。
- 所有修改限于 Allowed paths；`git diff --check` 通过；没有第二个 `claimed` 事项。

**Verification:** 对当前权威路由、ADR supersession、CONTEXT 术语、旧/新事项状态和依赖图执行静态一致性检查；核对所有变更路径均在 Allowed paths、产品路径零修改、旧 DP2-11 evidence checksum 未改变；运行 `git diff --check`，并由独立 Standards 与 Spec review 检查 blocker。真实 contract/schema/runtime/caller 验证全部保持 `not_verified`。

**Stop conditions:** 发现真实消费者/数据与用户确认冲突；需要选择会让 Tenant-owned Workload 获得 Platform authority、让 Platform-owned Workload制造 Tenant Membership、让 caller 身份由请求体自报，或需要立即修改产品契约/代码/数据库/其他仓库才能完成文档闭环。

**Recovery:** 本事项只产生文档和本地事项变更；若未通过一致性检查，保留本文件为 `ready-for-human`，逐文件撤销本事项新增内容，不触碰进入本事项前的 DP2-11 暂停现场。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/01-freeze-human-workload-architecture/`

## Comments

- 2026-09-09：只读预检确认没有其他 `claimed` 事项。用户允许遇到核心语义或范围问题时停下确认；当前已明确的 Human/Workload 二分、无消费者和无真实数据足以开始文档 refoundation。
- 2026-09-09：交叉检查发现 ADR 0001、0004、0007、0008、0009、0011、0012、0013、0014、0015、0016 仍包含会与 Human/Workload 二分或每跳认证冲突的现行术语；将这些精确文件加入 Allowed paths，仅定点更新被 ADR-0022 修订的段落，其余决定继续有效。ADR-0018 则由 ADR-0022 显式 supersede。
- 2026-09-09：独立 wayfinding review 发现 `README.md` 仍指向旧 rebuild 规格和过期 baseline；将其精确加入 Allowed paths，仅更新当前权威入口和状态，不改其他项目说明。
- 2026-09-09：独立 spec review 发现 accepted ADR-0005 的 unauthenticated NATS 许可未限定为历史 CP0，和目标 WR-07 的 Broker Workload Binding/Grant/fail-closed 约束冲突；将该精确 ADR 加入 Allowed paths，仅限定旧例外的适用期，不改其项目独立性、基础设施所有权或 contract ownership 决定。
- 2026-09-09：最终 standards review 发现 `docs/agents/domain.md` 的 single-context 导航仍只指向已停止的 rebuild 目录；将该精确文件加入 Allowed paths，仅更新当前候选事项入口并保留历史目录定位。
- 2026-09-09：最终 standards review 发现 accepted ADR-0017 的历史测试清理许可允许逐行删除 Audit，和当前 append-only、至少180天及仅整库 disposable 测试清理边界冲突；将该精确 ADR 加入 Allowed paths，仅限定旧许可不再授权当前 refoundation，并保留其其余 lifecycle 决定。
- 2026-09-09：最终 standards review 发现 accepted ADR-0006 将未限定的 rollback rehearsal 全部推迟、ADR-0021 将未限定的 race/concurrency suite 全部推迟，会允许绕过 WR-09/13 的隔离回退/前向恢复和各安全状态机的定向并发门禁；将两个精确 ADR 加入 Allowed paths，仅区分仍可延期的 production-scale/full-suite hardening 与当前必须通过的 target functional safety gates。
- 2026-09-09：最终 standards review 发现仅有 Seed/Provisioning Workload 而无首个 Platform Human 的可达路径，双人 recovery 永远无法启动；新增窄化、双签、exact-target、single-use 的 Platform First Administrator Ceremony，Seed仍不创建Human且Provisioner仍无generic CRUD。
- 2026-09-09：一致性审计发现 `docs/agents/triage-labels.md` 将 `Status:` 限定为五个 triage 角色，而 `AGENTS.md` 与 `docs/agents/issue-tracker.md` 明确以 `claimed`/`resolved` 承载唯一领取和完成状态，导致当前 WR-01 的 `Status: claimed` 与词典冲突。将该精确文件加入 Allowed paths，仅统一完整单值 `Status:` 词典，不改变事项状态或 frontier。
- 2026-09-09：最终语义硬化补齐 external administration lineage、ReadyClaim/outbox/LineageReporter/ReadyReceipt、正常 cutover 与 target-loss forward recovery 的分离、PostgreSQL-authoritative first-admin flow、阶段化 Audit actor 和 active+login-capable administrator 双不变量；这些仍是 WR-02 前的候选合同输入，不构成 runtime、外部系统或破坏性操作授权。
- 2026-09-09：最终独立 review 指出 `administration_active` 同时可能出现在PONR前后，单凭phase+target fence会错误允许pre-PONR Restore。候选合同因此把`legacy_cutover_committed=true`和current lineage/generation/manifest matching CommitPONR completion receipt加入RequireForwardRecovery、RecoveryActivation与Restore的共同必要条件；pre-PONR target loss明确保留legacy并只能经另行接受的新lineage/initial-install重建隔离target。
- 2026-09-09：按用户要求停止反复扩写二阶状态机。独立 review 的剩余发现被显式记录为后续设计 blocker：PONR 防重入与 recovery-boot supersession/liveness、first-admin nonce 可恢复来源、ReadyClaim 无循环preimage，以及WR-14前缺少持续long-term DR事项。它们使对应路径保持unavailable，但不推翻Human/Workload核心决定；WR-01转`ready-for-human`，不把这些问题伪装为已解决或交给实现者猜测。
