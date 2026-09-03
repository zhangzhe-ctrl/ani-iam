# 23: 交付 OIDC 登录与 Identity Link

**What to build:** 为 Human Principal 提供目标 OIDC 登录和显式 Identity Link，确保邮箱相同不会自动接管或合并账号。

**Blocked by:** 20 / 交付 Invitation 到 Human Membership

**Status:** wontfix

**Superseded by:** Direct P2 DP2-07；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-2

**Baseline:** 20 的 Human Principal/Identity/verified email 和固定 OIDC Provider 配置。

**Scope:** Authorization Code、PKCE S256、state/nonce/verifier、trusted issuer/audience/signature、verified email、近期重认证 Link 和 Audit。

**Out of scope:** 自动账号合并、动态 Redirect URI、公共 ANI JWKS、生产 Provider 扩展。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得实现按邮箱自动合并、动态 Redirect URI 或公共 ANI JWKS。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/23-deliver-oidc-identity-link/`

- [ ] 临时状态十分钟单次使用，callback 校验 state、nonce、PKCE、issuer、audience 和签名。
- [ ] 只有 `email_verified=true` 才能建立 verified email。
- [ ] 邮箱已属于其他 Principal 时 Link 稳定失败，不自动 merge 或数据库修复。
- [ ] Link 需要已登录且近期重认证，成功/失败写入允许的安全审计。

**Verification:** 真实 Dex 成功/攻击路径、Identity 冲突、重放、Audit 和敏感信息测试通过。

**Stop conditions:** Provider 无法隔离，或实现要求按邮箱自动关联 Principal。

**Recovery:** 删除隔离 OIDC Identity 和临时状态，保留原 Human Principal。
