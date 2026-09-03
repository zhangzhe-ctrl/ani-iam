# 12: 交付 Core Lifecycle、Bootstrap 与 NATS 恢复链路

**What to build:** 交付 Core 到 IAM 的 Lifecycle/Bootstrap 至少一次链路、版本投影、gap/Snapshot 修复、heartbeat、DLQ 和可审计 replay。

**Blocked by:** 03 / 冻结 IAM 与 Core 集成契约；04 / 建立无 RLS 持久化基础；09 / 交付 Tenant Access、Membership 与目标授权

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / Core lifecycle and NATS

**Baseline:** 03 canonical contracts/fixtures、04 persistence、09 Tenant Access/授权早期拒绝和隔离 NATS 身份。

**Scope:** Core transaction outbox、Lifecycle/Bootstrap publisher、专用 Stream/subject/ACL、IAM durable accept/worker、version/CAS、gap/Snapshot rebuild、heartbeat/watermark、DLQ/replay、bootstrap fingerprint 和 Audit。

**Out of scope:** 跨数据库事务、exactly-once、IAM 写 Tenant Lifecycle、普通授权同步回调 Core、应用创建共享 NATS 基础设施或高风险 Recovery。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`deploy/**`、`tests/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/pkg/messaging/**`、`../ANI/repo/deploy/**` 中隔离测试资源，以及本事项和证据目录。

**Forbidden paths:** 已冻结的 `api/**` 和 `../ANI/repo/api/proto/tenant/**`、其他 `../ANI/repo/**`；不得双写、跨库事务、exactly-once 声明、共享 superuser Credential 或 production 操作。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/12-deliver-core-lifecycle-nats/`

- [ ] 重复/旧消息幂等，version gap 只冻结受影响 Tenant 并触发 Snapshot 修复。
- [ ] heartbeat/watermark 能区分单 Tenant gap 与 pipeline stale；普通授权不回调 Core。
- [ ] Bootstrap fingerprint、durable accept、publisher restart、DLQ/replay 不产生双写或越权。
- [ ] Snapshot cursor→订阅增量→分页加载→原子激活→追平 buffer 无 gap。

**Verification:** 真实 PostgreSQL/NATS、重复/乱序/gap/stale/rebuild、publisher restart、DLQ/replay、Bootstrap、两 Tenant 和授权负向测试通过。

**Stop conditions:** Core canonical contract 不可用；基础设施不能隔离；实现要求双写、跨库事务、同步授权回调或 IAM 成为 Lifecycle owner。

**Recovery:** 停止隔离 publisher/consumer，清理专用 Stream/数据库投影并保留 evidence；不修改共享或生产 NATS。

**Human checkpoint:** 创建或修改共享 NATS Account/Stream/ACL、真实 Core 环境或部署前，必须获得精确人工确认；本事项默认只使用隔离测试资源。
