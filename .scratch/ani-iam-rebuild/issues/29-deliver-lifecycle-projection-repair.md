# 29: 交付 Lifecycle Projection 与修复

**What to build:** 让 IAM 持久化 Core Tenant Lifecycle 的单调版本投影，提供早期拒绝、Pipeline 健康、版本 gap 冻结和 Snapshot 修复。

**Blocked by:** 17 / 冻结 IAM 与 Core 集成契约；18 / 建立无 RLS 持久化基础；19 / 交付 Tenant Access Bootstrap 纵向链路

**Status:** ready-for-agent

**Plan mapping:** P2-5

**Baseline:** 17 的 Lifecycle/Snapshot 契约和 19 的 Tenant Access 状态。

**Scope:** Lifecycle projection、version/CAS、重复/旧消息、gap repair、heartbeat/watermark、stale/not-ready、Snapshot Fixture 和授权早期拒绝。

**Out of scope:** 真实 Core Outbox/NATS、资源 Owner 权威检查替代、普通授权同步回调 Core。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得接入真实 Core Outbox/NATS、替代资源 Owner guard 或在普通授权中同步回调 Core。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/29-deliver-lifecycle-projection-repair/`

- [ ] 重复和旧版本不产生重复效果；gap 只冻结受影响 Tenant。
- [ ] Pipeline 健康基于 consumer heartbeat/watermark，不使用单个 Tenant 行年龄。
- [ ] Snapshot 修复必须版本化且不能覆盖更新状态。
- [ ] IAM 投影只做早期拒绝，Core/Services 的权威 Lifecycle/Owner guard 保持必需。

**Verification:** 重复/乱序/gap/stale/rebuild、两 Tenant 隔离和授权结果测试通过。

**Stop conditions:** 需要普通授权同步调用 Core，或 IAM 成为 Tenant Lifecycle 写入者。

**Recovery:** 重建隔离投影；不修改 Core 权威状态。
