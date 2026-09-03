# 16: 完成 Audit 查询与全局 Mutation 幂等

**What to build:** 交付受控 Audit 查询、至少在线可查 180 天的明确语义，以及全部公开 mutation 的统一 24 小时 Idempotency Ledger。

**Blocked by:** 15 / 完成 IAM 管理、Platform 与高风险恢复

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-3 / audit and idempotency

**Baseline:** 02 operation registry、15 全部目标 mutation、ADR-0014/0017 和已接受的 Audit/幂等决定。

**Scope:** append-only Audit repository、List/Get/cursor、Tenant/Platform auditor boundary、180 天查询边界、增长监控、Idempotency Ledger/port、request hash/result、operation inventory、并发/失败聚合门禁和 UI。

**Out of scope:** Audit export/SIEM/WORM、密码学不可抵赖、自动 retention cleanup、生产保留策略、GET 幂等或业务状态机变更。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和证据目录。

**Forbidden paths:** 已冻结 `api/**`/OpenAPI、旧 Auth、`../ANI/repo/deploy/**`；不得修改领域语义、添加审计修改/删除 API、自动清理 Job 或忽略 boundary/actor。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/16-complete-audit-idempotency/`

- [ ] Tenant/Platform Audit 查询隔离、cursor、敏感 details 限制和 append-only 权限成立。
- [ ] 180 天边界与允许/禁止测试环境清理有可验证语义，不冒充生产 retention。
- [ ] 每个公开 POST/PUT/PATCH 都进入统一 ledger；同 key 同请求重放，同 key 异请求/过期返回稳定 `409`。
- [ ] ledger 与业务状态/通知/Audit 的事务和故障行为完整。

**Verification:** operation inventory、真实数据库、并发/事务故障、跨 boundary、cursor/180 天、append-only、通知去重和 UI E2E 通过。

**Stop conditions:** registry 无法穷举 mutation；查询需要跨边界 bypass；无法 append-only；接入要求改变领域状态机或泄露敏感 details。

**Recovery:** 回退查询/UI/共享接入和未发布 migration；不得删除既有 Audit 或窗口内 Ledger。
