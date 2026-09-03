# 05: 验证 Password、Refresh 与 Revoke

**What to build:** 交付旧 Password Login、Refresh 和 Revoke 的完整兼容纵向链路，并证明响应、Claim 与持久化副作用和基线一致。

**Blocked by:** 04 / 接通真实旧 PG/RLS 与 Redis

**Status:** wontfix

**Superseded by:** Direct P2 DP2-06、DP2-08；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。本状态只关闭旧兼容切片，不取消目标 Password/Session/Refresh 需求。

**Plan mapping:** CP0-2、CP0-4

**Baseline:** 01 的 bcrypt、JWT、Refresh、Session、Blocklist 与错误 Oracle。

**Scope:** Login、PlatformPasswordLogin、RefreshToken、RevokeToken，以及相关 Token、PG/Redis 状态和安全日志脱敏。

**Out of scope:** Argon2id、旋转 Refresh Family、Session Grant、目标撤销语义。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/service/**`、`tests/cp0/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、目标 Password/Session 数据模型。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/05-verify-password-refresh-revoke/`

- [ ] 成功、无效 Credential、锁定/拒绝、刷新和撤销路径均有差分用例。
- [ ] 只归一化随机 ID、时间和 Secret；Claim、错误、PG/Redis 副作用与 TTL 必须比较。
- [ ] Password、Token、Hash 和完整 Credential 不进入日志或证据。
- [ ] 每个预期差异都被显式列出并说明兼容影响。

**Verification:** golden Token 与真实依赖 differential suite 通过；未批准差异为零。

**Stop conditions:** 兼容需要改变 wire、Schema 或安全语义。

**Recovery:** 清理隔离 Session、Token 和 Redis 状态，不影响旧服务。
