# 07: 交付 OIDC 登录与 Identity Link

**What to build:** 交付 Authorization Code + PKCE 的目标 OIDC 登录与显式 Identity Link，防止按相同邮箱自动合并或接管账号。

**Blocked by:** 05 / 证明目标最小纵向链路（Go/No-Go A 已人工接受）

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / OIDC

**Baseline:** 05 接受的目标链路、固定 OIDC/Identity 契约、Provider 与 verified-email 决策。

**Scope:** PKCE S256、state/nonce/verifier 单次使用、固定 callback、issuer/audience/signature、`email_verified`、显式 Link、近期重认证、Audit 和稳定错误。

**Out of scope:** 自动账号合并、动态 Redirect URI、公共 ANI JWKS、生产 Provider 扩展或旧 OIDC 兼容。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`deploy/**`、`../ANI/repo/**`；不得按邮箱自动关联 Principal、接受未验证邮箱、动态 callback 或泄露 state/nonce/verifier。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/07-deliver-oidc-identity-link/`

- [ ] 真实 Dex 成功链路和 issuer/audience/signature/nonce/PKCE 失败路径完整。
- [ ] state、nonce、verifier 十分钟且单次消费，重放被拒绝。
- [ ] 相同 verified email 属于其他 Principal 时稳定失败，不自动合并。
- [ ] Link 要求已登录和近期重认证；状态变化与 Audit 原子。

**Verification:** 真实 Dex integration、攻击/重放、Identity 冲突、事务失败、错误映射和 Secret 扫描通过。

**Stop conditions:** Provider 无法隔离；实现要求自动邮箱关联、动态 redirect 或修改冻结契约。

**Recovery:** 删除隔离 OIDC Identity/临时状态，保留原 Human Principal。
