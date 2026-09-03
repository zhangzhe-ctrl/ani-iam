# 22: 交付 Password 身份认证

**What to build:** 为已验证 Human Principal 提供目标 Password 设置、登录、锁定和重置纵向链路，并使用 Argon2id 与明确 Session 撤销范围。

**Blocked by:** 20 / 交付 Invitation 到 Human Membership

**Status:** wontfix

**Superseded by:** Direct P2 DP2-06；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-2

**Baseline:** 20 的 Human Principal/verified email，以及固定 Argon2id 与错误决策。

**Scope:** 一次性 Password Action、Argon2id PHC、登录限流/锁定、账号不枚举、Password Reset、Audit 和 Session 撤销接口。

**Out of scope:** OIDC、Refresh Token、开放注册、生产 Argon2 性能门禁。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得实现开放注册、OIDC、Refresh Token 或把未完成的 Argon2 benchmark 标为通过。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/22-deliver-password-authentication/`

- [ ] 新密码使用固定 Argon2id 参数和随机 Salt，数据库不保存明文或可逆 Secret。
- [ ] 一次性设置动作 30 分钟有效；seed 不含默认明文密码。
- [ ] 登录按规范化账号与 IP 限流，五次失败锁定十五分钟且不枚举账号。
- [ ] Password Reset 撤销该 Human 全部 Session/Family，并与 Audit 保持一致失败语义。

**Verification:** 密码向量、成功/失败登录、锁定、重置、事务回滚和敏感信息扫描通过。

**Stop conditions:** Argon2 实现需自行编写密码学原语，或 Audit 失败仍允许状态成功。

**Recovery:** 删除隔离 Password Credential/Action；撤销测试 Session。
