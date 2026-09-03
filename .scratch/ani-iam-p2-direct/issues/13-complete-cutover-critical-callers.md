# 13: 完成五类切换关键调用面对等

**What to build:** 让 Gateway、Envoy、Inference、Console 和 BOSS 在隔离目标轨道使用切换关键 IAM/Core 能力，并形成五类独立证据。

**Blocked by:** 08 / 交付 Session、Refresh 与浏览器边界；09 / 交付 Tenant Access、Membership 与目标授权；10 / 交付 Service Principal、API Key 与 Envoy 验证；11 / 交付 Workload Service Token 与 Inference 调用链；12 / 交付 Core Lifecycle、Bootstrap 与 NATS 恢复链路

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / cutover-critical caller parity

**Baseline:** 02/03 固定契约、08–12 固定实现 Artifact、隔离目标数据库/NATS/Dex 和五类调用方 inventory。

**Scope:** Gateway registry/authn/authz、Envoy API Key/credential、Inference Service Token、Console/BOSS Password/OIDC/Refresh/Logout、Tenant/Platform 进入条件、Core Lifecycle/Bootstrap 影响和独立 E2E。

**Out of scope:** 完整 Invitation/Role/Recovery/Audit UI、主测试轨道切流、旧 Credential 失效、旧资产删除或实现缺口顺带修复。

**Allowed paths:** `tests/e2e/**`、`configs/**`、`deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`、`../ANI/repo/deploy/**` 中隔离目标轨道，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`migrations/**`、`internal/biz/**`、`internal/data/**`、旧 Auth、主测试环境；发现实现/契约缺口必须停止并回开对应事项，不得在 E2E 票中改变语义。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/13-complete-cutover-critical-callers/`

- [ ] 五类调用方分别覆盖成功、拒绝、无效 Credential、依赖失败和 timeout。
- [ ] Password、OIDC、Refresh/Logout、API Key、Service Token、Membership/Authorization、Lifecycle/Bootstrap 均有消费者证据。
- [ ] 契约 digest、镜像、配置、seed 和路由全部固定，无动态 `latest/main`。
- [ ] 目标代码无 legacy fallback；任一调用方成功不能代替另一调用方。

**Verification:** 五类独立 E2E、配置/镜像/契约摘要、错误矩阵、目标-only 路由和 fallback 扫描通过。

**Stop conditions:** 任一调用面缺失；需要 legacy fallback；目标 Artifact 无法固定；发现业务或契约缺口。

**Recovery:** 所有调用方保持隔离目标轨道，恢复其测试配置；主测试环境仍使用旧系统。
