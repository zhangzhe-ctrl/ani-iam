# 19: 交付 Tenant Access Bootstrap 纵向链路

**What to build:** 通过测试 Fixture 幂等建立 `bootstrap_pending` Tenant Access、Bootstrap Operation 和 System Role，为 Tenant IAM 引导提供第一个可验证端到端行为。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；18 / 建立无 RLS 持久化基础

**Status:** wontfix

**Superseded by:** Direct P2 DP2-09、DP2-12、DP2-15；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-4

**Baseline:** 16 的 Permission/Owner 决策、17 的 Bootstrap 契约和 18 的独立数据库。

**Scope:** Tenant Access、Bootstrap Operation、System Role 实例化、幂等 fingerprint、Audit 事务、missing/not-ready 错误。

**Out of scope:** 真实 Core/NATS、首管理员接受、Recovery Bootstrap、普通操作自动补行。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得同步调用 Core、创建 NATS 资源或实现 Recovery Bootstrap。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/19-deliver-tenant-access-bootstrap/`

- [ ] 重复相同 operation/fingerprint 返回稳定结果，异 payload 返回稳定冲突。
- [ ] Tenant Access 在首管理员关系完成前保持 `bootstrap_pending`。
- [ ] 缺失 Tenant Access 返回 retryable IAM-not-ready，不返回普通 403/not-found。
- [ ] 状态变化、System Role 和 Audit 在同一事务提交，失败无部分状态。

**Verification:** 契约、真实数据库、并发幂等、Audit 回滚和两 Tenant 负向测试通过。

**Stop conditions:** 需要同步 Core 调用、跨库事务、自动补行或共享 Tenant 表。

**Recovery:** 删除独立测试 Tenant 数据并重放 migration/seed。
