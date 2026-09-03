# 25: 交付浏览器与 Platform/BOSS 边界

**What to build:** 让 Console 和 BOSS 通过隔离的安全 Cookie、CSRF 和 Platform Membership 访问各自边界，并在多 Tab 场景中安全刷新。

**Blocked by:** 24 / 交付 Session Grant 与旋转 Refresh

**Status:** wontfix

**Superseded by:** Direct P2 DP2-08、DP2-15、DP2-17；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-2、P2-4

**Baseline:** 24 的 Session/Grant/Refresh，以及目标 Console/BOSS Token 时限和 Cookie 决策。

**Scope:** Platform Membership、BOSS 登录、HttpOnly Cookie、CSRF、Origin/Referer、固定 callback、内存 Access Token、多 Tab single-flight 和错误 UX。

**Out of scope:** Human CLI Refresh、宽泛 Cookie CORS、Support Session、生产 CORS 配置。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`configs/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、上述目录之外的 `../ANI/repo/**`；不得加入 Human CLI Refresh、宽泛 Cookie CORS 或 Support Session。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/25-deliver-browser-platform-boundaries/`

- [ ] Console/BOSS 使用不同 Cookie 名称、Path、Audience 和 Session 时限。
- [ ] BOSS 登录必须有 active Platform Membership，不能由 Tenant 身份或前端路由提升。
- [ ] Refresh/Logout/SwitchTenant/OIDC 校验精确 Origin/Referer 与独立 CSRF Cookie/Header。
- [ ] Access Token 只在内存；多 Tab 只进行一次 Refresh，401 最多重试原请求一次。

**Verification:** 浏览器 E2E、CSRF/Origin 负向、多 Tab race、Cookie 属性和 Platform 边界测试通过。

**Stop conditions:** 需要将 Refresh Token 暴露给 JavaScript 或混用 Tenant/Platform Cookie。

**Recovery:** 清空测试 Cookie/Session 并恢复固定登录入口。
