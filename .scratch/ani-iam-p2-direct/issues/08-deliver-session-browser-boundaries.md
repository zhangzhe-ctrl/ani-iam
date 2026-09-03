# 08: 交付 Session、Refresh 与浏览器边界

**What to build:** 交付 Session/Grant、旋转 Refresh、reuse 防护、Logout、SwitchTenant，以及 Console/BOSS 隔离 Cookie、CSRF 和多 Tab 刷新边界。

**Blocked by:** 06 / 交付目标 Password 身份认证；07 / 交付 OIDC 登录与 Identity Link

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / session and browser

**Baseline:** 06/07 认证结果、固定 Session/Token 生命周期、Console/BOSS Cookie/Audience 决策和 05 的最小 Grant。

**Scope:** Session、boundary Grant、Refresh Family/Token 单次旋转与 reuse、Access Token claims、Logout、SwitchTenant、HttpOnly Cookie、CSRF/Origin、内存 Access Token、多 Tab single-flight 和错误 UX。

**Out of scope:** Service Token、API Key、完整 Platform 管理、Human CLI Refresh、Support Session、生产 CORS/KMS 演练。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/**`；不得 JSON 返回 Refresh Token、使用 per-jti Blocklist、信任客户端 Tenant Header 或混用 Console/BOSS Cookie。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/08-deliver-session-browser-boundaries/`

- [ ] Refresh 每次成功旋转；reuse 只撤销对应 family/grant boundary。
- [ ] Logout 幂等只撤销当前 Session；SwitchTenant 重新校验完整目标 boundary。
- [ ] Console/BOSS Cookie 名称、Path、Audience、时限隔离，Refresh 不可被 JavaScript 读取。
- [ ] CSRF/Origin、固定 callback、多 Tab race 和错误 UX 有正负向证据。

**Verification:** unit、真实数据库/Redis、并发 Refresh/reuse、浏览器 E2E、Cookie 属性、CSRF/Origin 和跨 boundary 负向测试通过。

**Stop conditions:** 需要全局多 Tenant Token、客户端 Tenant Header、JSON Refresh、宽泛 Cookie CORS 或共享 Console/BOSS boundary。

**Recovery:** 清理隔离 Session/Grant/Family、Cookie 和目标前端测试入口；主测试路径不切换。
