# 24: 交付 Session Grant 与旋转 Refresh

**What to build:** 让 Human Session 为单 Tenant 或 Platform boundary 建立可撤销 Session Grant，并通过单次旋转 Refresh Family 支持登录连续性、Logout 与 SwitchTenant。

**Blocked by:** 22 / 交付 Password 身份认证；23 / 交付 OIDC 登录与 Identity Link

**Status:** ready-for-agent

**Plan mapping:** P2-2

**Baseline:** 目标 Session/Token 生命周期、22/23 的认证结果和 19–21 的 Tenant 状态。

**Scope:** Session、Session Grant、Refresh Family/Token、Access Token Claim、rotation/reuse、Logout、SwitchTenant、PostgreSQL 在线事实和 Audit。

**Out of scope:** 浏览器 Cookie/CSRF、Service Token、per-jti Blocklist、生产 KMS 演练。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得加入浏览器 Cookie、Service Token、全局多 Tenant Token 或 per-jti Blocklist。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/24-deliver-session-grant-refresh/`

- [ ] Token 只包含一个 boundary，不包含 Role、Permission 或全部 Membership。
- [ ] 每个 Session+boundary 最多一个 active Family；成功 Refresh 单次旋转。
- [ ] consumed Token reuse 撤销对应 Family并增加 Grant version，不影响其他 boundary。
- [ ] Logout 幂等撤销当前 Session；SwitchTenant 重新校验完整安全状态。

**Verification:** rotation/reuse、跨 boundary、并发 Refresh、Logout、SwitchTenant 和数据库故障测试通过。

**Stop conditions:** 需要客户端 Tenant Header、全局多 Tenant Token 或 per-jti Blocklist。

**Recovery:** 撤销隔离 Session/Grant/Family；不影响其他 Principal。
