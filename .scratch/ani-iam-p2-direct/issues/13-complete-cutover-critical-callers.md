# 13: 完成五类切换关键调用面对等

**What to build:** 让 Gateway、Envoy、Inference、Console 和 BOSS 在隔离目标轨道使用切换关键 IAM/Core 能力，并形成五类独立证据。

**Blocked by:** 08 / 交付 Session、Refresh 与浏览器边界；09 / 交付 Tenant Access、Membership 与目标授权；10 / 交付 Service Principal、API Key 与 Envoy 验证；11 / 交付 Workload Service Token 与 Inference 调用链；12 / 交付 Core Lifecycle、Bootstrap 与 NATS 恢复链路

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** DP2-2 / cutover-critical caller parity

**Baseline:** 02/03 固定契约、08–12 固定实现 Artifact、隔离目标数据库/NATS/Dex 和五类调用方 inventory。

**Scope:** Gateway registry/authn/authz、Envoy API Key/credential、Inference Service Token、按 DP2-08 冻结对接契约实现 Gateway Cookie/CSRF edge adapter 与 Console/BOSS Password/OIDC/Refresh/Logout、多 Tab/错误 UX、Tenant/Platform 进入条件、Core Lifecycle/Bootstrap 影响和独立 E2E。调用方只能适配已冻结 IAM 语义，不得在本事项改变 Session/Refresh 领域规则。

DP2-08 已冻结浏览器入口为 `/auth/{audience}/*`，其中 `audience` 只能是 `console` 或 `boss`；Console Cookie 使用 `Path=/auth/console`，BOSS Cookie 使用 `Path=/auth/boss`。统一 24 小时 Idempotency Ledger 仍由 DP2-16 拥有，DP2-13 不得伪造同 key 重放能力或改变严格 Refresh reuse 语义。

**Out of scope:** 完整 Invitation/Role/Recovery/Audit UI、主测试轨道切流、旧 Credential 失效、旧资产删除或实现缺口顺带修复。

**Allowed paths:** `tests/e2e/**`、`configs/**`、`deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/deploy/**` 中隔离目标轨道，以及本事项和证据目录。Console/BOSS 已迁出 ANI；其独立仓库路径、固定 commit 和精确允许范围尚未获批，因此当前不构成 Allowed paths，领取 DP2-13 前必须补齐并取得人工接受。

**Forbidden paths:** `api/**`、`migrations/**`、`internal/biz/**`、`internal/data/**`、旧 Auth、主测试环境；发现实现/契约缺口必须停止并回开对应事项，不得在 E2E 票中改变语义。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/13-complete-cutover-critical-callers/`

- [ ] 五类调用方分别覆盖成功、拒绝、无效 Credential、依赖失败和 timeout。
- [ ] Password、OIDC、Refresh/Logout、API Key、Service Token、Membership/Authorization、Lifecycle/Bootstrap 均有消费者证据。
- [ ] 契约 digest、镜像、配置、seed 和路由全部固定，无动态 `latest/main`。
- [ ] 目标代码无 legacy fallback；任一调用方成功不能代替另一调用方。
- [ ] Gateway/Console/BOSS 严格实现 DP2-08 handoff 的 Cookie/CSRF、内存 Token、多 Tab 和错误 UX 矩阵；各调用方仍提供独立运行证据。

**Verification:** 五类独立 E2E、配置/镜像/契约摘要、错误矩阵、目标-only 路由和 fallback 扫描通过。

**Stop conditions:** 任一调用面缺失；Console/BOSS 独立仓库身份、固定 SHA 或允许路径缺失；需要 legacy fallback；目标 Artifact 无法固定；发现业务或契约缺口。

本文件对冻结契约的记录不构成 ANI Allowed-path 扩展或修改授权。领取 DP2-13 并进入统一 ANI 测试窗口时，必须先取得当时最新 `main` 的精确 SHA，展示其相对 `50f7b422707c2ab78462bd9bb8186bae018a14fe` 的提交和重叠路径，等待人工接受，并针对 OpenAPI/operation registry/Gateway 的精确路径另行扩展；此前相关实现与门禁保持 `not_verified`/`fail` 原状。

**Recovery:** 所有调用方保持隔离目标轨道，恢复其测试配置；主测试环境仍使用旧系统。

## Comments

- 2026-09-09：旧 ticket version 被 WR-08 取代，capability 不取消。新票增加统一 direct-caller Workload、Session Gateway 和每跳 WAT 证据；旧 baseline 与 Allowed paths 不再可执行。
