# 06: 交付目标 Password 身份认证

**What to build:** 为已验证 Human Principal 交付 Password 设置、Argon2id 登录、锁定、重置和明确的 Session 撤销边界。

**Blocked by:** 05 / 证明目标最小纵向链路（Go/No-Go A 已人工接受）

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / password

**Baseline:** 05 接受的目标链路、固定 Authentication 契约和 Password/Session 安全决定。

**Scope:** 一次性 Password Action、Argon2id PHC、登录限流/锁定、账号不枚举、Password Reset、全 Session 撤销接口、Audit 和错误映射。

**Out of scope:** OIDC、完整 Refresh/browser、开放注册、旧 bcrypt 兼容 runtime、生产 Argon2 性能门禁。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`、`go.mod`、`go.sum`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`deploy/**`、`../ANI/repo/**`；不得自写密码学原语、开放注册、保留明文密码或把未完成的生产 benchmark 标为通过。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/06-deliver-password-authentication/`

- [ ] Argon2id 参数/PHC/测试向量固定，Secret 不进入日志和证据。
- [ ] 成功、无效、锁定、重置、过期/消费 Password Action 行为稳定且不枚举账号。
- [ ] Password Reset 撤销该 Human 的全部 Session/Family，普通登录不扩大撤销范围。
- [ ] 状态与 Audit 同事务，数据库/Redis 失败不产生部分成功。

**Verification:** 密码向量、unit、真实数据库/Redis integration、并发锁定、事务回滚、错误映射和敏感信息扫描通过。

**Stop conditions:** 需要自行实现密码学、改变冻结契约、保存明文 Secret 或 Audit 失败仍允许状态提交。

**Recovery:** 清理隔离 Password Credential/Action 和测试 Session；不影响其他 Principal。
