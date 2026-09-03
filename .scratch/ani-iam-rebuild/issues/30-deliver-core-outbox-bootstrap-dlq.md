# 30: 交付 Core Outbox、Bootstrap 与 DLQ

**What to build:** 建立 Core 到 IAM 的至少一次 Lifecycle/Bootstrap 交付链路，使首管理员引导、版本修复、poison message 和审计 Replay 可恢复且不双写。

**Blocked by:** 17 / 冻结 IAM 与 Core 集成契约；19 / 交付 Tenant Access Bootstrap 纵向链路；29 / 交付 Lifecycle Projection 与修复；外部依赖为 Core Control canonical contract 和独立工作

**Status:** wontfix

**Superseded by:** Direct P2 DP2-12；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-5

**Baseline:** Core canonical Artifact、29 的投影处理器、19 的 Bootstrap Operation 和隔离 NATS 身份。

**Scope:** Core transaction outbox、Lifecycle/Bootstrap publisher、专用 Stream/subject/ACL、IAM durable accept/worker、fingerprint、DLQ、Replay 和 Audit。

**Out of scope:** 跨数据库事务、exactly-once 声明、应用创建 NATS 基础设施、共享超级用户 Credential；Recovery Bootstrap 与 RestoreTenantAdmin 由 32 交付。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`deploy/**`、`tests/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/api/proto/tenant/**`、`../ANI/repo/pkg/messaging/**`、`../ANI/repo/deploy/**`，以及本事项和其证据目录。

**Forbidden paths:** 上述目录之外的 `../ANI/repo/**`、`api/iam/v1/**`；不得实现跨库事务、exactly-once、应用创建 NATS 基础设施、Recovery Bootstrap 或 RestoreTenantAdmin。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/30-deliver-core-outbox-bootstrap-dlq/`

- [ ] Core 业务状态和 outbox 同事务；发布失败重试且不阻塞其他 Tenant。
- [ ] IAM ack 前持久接收 operation/fingerprint，重复投递不产生重复业务效果。
- [ ] poison message 先持久写 DLQ 再 ack；Replay 需要专用 Permission、reason 和 Audit。
- [ ] Core/IAM 使用独立身份、Stream/subject/ACL，应用只验证配置不修改基础设施。

**Verification:** 真实 NATS 集成、重复/乱序/gap、publisher restart、DLQ/replay、Bootstrap 恢复和权限负向测试通过。

**Stop conditions:** Core canonical contract 未发布、基础设施不能隔离或实现要求双写/跨库事务。

**Recovery:** 停止测试 consumer/publisher，保留 durable evidence，重建隔离 Stream/数据库投影。
