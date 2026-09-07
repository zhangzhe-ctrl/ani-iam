# 08: 交付 Session、Refresh 后端与浏览器对接契约

**What to build:** 交付 IAM Session/Grant、旋转 Refresh、reuse 防护、Logout、SwitchTenant，并冻结供 Gateway、Console 和 BOSS 后续实现的浏览器对接契约。

**Blocked by:** 06 / 交付目标 Password 身份认证；07 / 交付 OIDC 登录与 Identity Link

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-2 / session and browser

**Baseline:** IAM branch `codex/direct-p2-06-14` at exact clean commit `d51a1ec9f72a288f489f0ac97ad76ab0de8e5b33`；06/07 认证结果、固定 Session/Token 生命周期、Console/BOSS Cookie/Audience 决策和 05 的最小 Grant。用户明确豁免本事项开始前接受 ANI integration baseline 的要求，因为本事项不读取未接受 ANI 动态状态，也不创建或修改任何 ANI worktree。

**Scope:** Session、单 boundary Grant、Refresh Family/Token 单次旋转与 reuse、Access Token claims、Grant version、Session/Refresh 生命周期、Logout、SwitchTenant，以及完整的 Gateway/Console/BOSS 前端对接文档。实现和通过证据仅限 IAM 后端；文档冻结后续调用方实现必须遵循的 Cookie、CSRF/Origin、内存 Access Token、多 Tab single-flight 和错误 UX 语义。

**Out of scope:** 任何 ANI/Gateway/Console/BOSS 源码改动、Cookie/CSRF edge adapter、真实浏览器 E2E、多 Tab 运行证据和错误 UX 运行证据；这些调用方面对等移交 DP2-13 并在完成前保持 `not_verified`。Service Token、API Key、完整 Platform 管理、Human CLI Refresh、Support Session、生产 CORS/KMS 演练也不在本事项范围。

统一 24 小时 Idempotency Ledger、同 key 重放、payload conflict 和逻辑过期语义由 DP2-16 交付，不属于 DP2-08。DP2-08 的 Refresh/SwitchTenant 每个实际尝试使用新 key 且不承诺丢失响应重放；Logout 只保证不依赖 Ledger 的领域语义幂等。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`configs/**`、`tests/**`、本事项、`.scratch/ani-iam-p2-direct/ticket-plan.md`、`.scratch/ani-iam-p2-direct/issues/13-complete-cutover-critical-callers.md` 和本事项证据目录。

**Forbidden paths:** `api/**`、`deploy/**`、任何 ANI worktree 或外部前端仓库；不得 JSON 返回 Refresh Token、使用 per-jti Blocklist、信任客户端 Tenant Header 或混用 Console/BOSS Cookie。若实现需要 `cmd/server/**`、`internal/conf/**`、`internal/server/**`、模块依赖或其他未列路径，必须暂停并请求精确扩展。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/08-deliver-session-browser-boundaries/`

- [x] Refresh 每次成功旋转；reuse 只撤销对应 family/grant boundary。
- [x] Logout 幂等只撤销当前 Session；SwitchTenant 重新校验完整目标 boundary。
- [x] Access Token claims、Grant version、Session idle/absolute expiry 和 Console 生命周期符合冻结语义；BOSS Platform Grant 生命周期明确保持 `not_verified`。
- [x] PostgreSQL/Redis 失败、Audit 回滚、并发 Refresh/reuse 和跨 boundary 负向成立。
- [x] 前端对接文档固定 Console/BOSS 流程、Cookie 属性矩阵、CSRF/Origin、固定 callback、内存 Access Token、多 Tab 协调、401/503/reuse UX、责任边界和正负向验收矩阵。
- [x] Gateway/Console/BOSS 源码与真实浏览器证据明确记录为 `not_verified`，未用后端测试替代。

**Verification:** test-first unit、真实 PostgreSQL/Redis、事务/Audit 回滚、并发 Refresh/reuse、Session/Grant 生命周期和跨 boundary 负向测试通过；冻结契约与前端移交文档完成。Gateway Cookie/CSRF、浏览器 E2E、多 Tab 和错误 UX 运行结果保持 `not_verified`。

**Stop conditions:** 需要全局多 Tenant Token、客户端 Tenant Header、JSON Refresh、宽泛 Cookie CORS 或共享 Console/BOSS boundary。

**Recovery:** 清理本事项隔离 Session/Grant/Family 测试数据与 Redis namespace；代码恢复只使用针对本事项最终提交的新 reviewed revert，不 reset/stash/rebase/amend。没有 ANI、Cookie 或前端测试入口需要清理，主测试路径不切换。

## Claimed execution

- Human decision: 用户于 2026-09-07 明确批准本事项改为 IAM 后端交付和完整浏览器对接文档；Gateway/Console/BOSS 实现与真实浏览器证据移交 DP2-13。该决定只豁免 DP2-08 的 ANI baseline 前置，不接受 `ANI main@50f7b422707c2ab78462bd9bb8186bae018a14fe` 为绿色 baseline；其 `make test` operation-registry drift 仍为 `fail`。
- Closure decisions: 用户于 2026-09-08 明确批准把统一 24 小时 Ledger 留给 DP2-16，并接受 `/auth/{audience}/*` 路由与 Console `Path=/auth/console`、BOSS `Path=/auth/boss` 的 Cookie 隔离设计。该批准只冻结 DP2-13 的调用方契约，不扩展或授权修改任何 ANI 路径；当前 ANI OpenAPI、registry、Gateway 和浏览器运行结果仍为 `not_verified`。
- Start identity: branch `codex/direct-p2-06-14`, commit `d51a1ec9f72a288f489f0ac97ad76ab0de8e5b33`, clean worktree/index and passing `git diff --check`; DP2-06/07 are `resolved` and no other Direct P2 ticket is `claimed`.
- Frozen test seams: accepted `AuthenticationService` Proto RPCs and framework-independent biz use-case results; real PostgreSQL UnitOfWork state/Audit effects through the public repository/use-case boundary; Redis-required refresh failure through its consuming port; concurrent RPC/use-case outcomes. Tests do not assert private helper calls or query the database as a substitute for public behavior, while storage-boundary integration may inspect durable state to prove transaction, isolation and reuse invariants.
- Execution plan: one vertical RED→GREEN slice at a time for Session/Grant lifecycle, refresh rotation/reuse, logout, SwitchTenant, expiry/claims and dependency failures; then write the consumer handoff, run full relevant regression and generated/static/supply-chain gates, perform independent Standards/Spec review, audit exact staged paths and create the ticket-local commit.
- Stop after closure: after the adjusted DP2-08 is committed and resolved, pause before DP2-09. Do not create an ANI worktree or repair ANI until a later unified test window supplies a newly audited and human-accepted exact ANI SHA.

## Resolution

- Backend result: `pass` for Session/Grant continuity, strict Refresh rotation/reuse, current-Session Logout, Console SwitchTenant, claims/lifetimes, failure mapping, rollback and concurrent/cross-boundary behavior.
- Dependency result: `pass` with real isolated PostgreSQL/Redis and pinned Atlas/sqlc generation; complete ordinary, race and integration-race regressions pass.
- Review result: independent Standards `pass` and Ticket Spec `pass`, with zero blockers/findings on the accepted closure boundary.
- Caller result: Gateway/Console/BOSS/browser/ANI route implementation remains `not_verified`; BOSS Platform Grant remains `not_verified`; unified 24-hour Ledger remains DP2-16.
- Publication result: local ticket commit only; no push, PR, deployment, cutover, Credential invalidation, asset deletion or ANI modification.
- Next state: DP2-09 remains `ready-for-agent` and is not claimed.
