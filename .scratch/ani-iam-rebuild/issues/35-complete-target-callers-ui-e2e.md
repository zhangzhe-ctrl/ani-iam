# 35: 完成目标调用方与 UI E2E

**What to build:** 在隔离目标轨道上让 Gateway、Envoy、Inference、Console 和 BOSS 使用目标 IAM 契约，并证明各调用方与浏览器边界独立成立。

**Blocked by:** 25 / 交付浏览器与 Platform/BOSS 边界；26 / 交付 Permission 与单次 Gateway 授权；27 / 交付 Service Principal 与 API Key；28 / 交付 Workload Service Token；30 / 交付 Core Outbox、Bootstrap 与 DLQ；31 / 交付 Platform Role、Invitation 与 Membership 管理；32 / 交付高风险 Tenant Admin 恢复；33 / 交付 Audit 查询与 180 天语义；34 / 实现并聚合验证全局 Mutation 幂等

**Status:** wontfix

**Superseded by:** Direct P2 DP2-13、DP2-17；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-6

**Baseline:** 16/17 的目标契约、25–34 的固定实现 Artifact 和隔离测试环境。

**Scope:** Gateway generated registry、Envoy token validation、Inference Service Token、Console/BOSS 登录/刷新/管理、目标 DNS/config 和独立 E2E 证据。

**Out of scope:** 主测试环境破坏性切换、旧 Credential 失效、旧实现删除。

**Allowed paths:** `tests/e2e/**`、`configs/**`、`deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`、`../ANI/repo/deploy/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`migrations/**`、`internal/biz/**`、`internal/data/**`、上述目录之外的 `../ANI/repo/**`；若发现实现或契约缺口，必须停止并回开对应 16–34 事项，不得在 E2E 票内顺带修改。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/35-complete-target-callers-ui-e2e/`

- [ ] Gateway 每路由零次或一次 IAM 决策，错误和 obligation 符合目标契约。
- [ ] Envoy 的 allow/deny/invalid/unavailable 独立验证，不由 Gateway 结果代替。
- [ ] Inference 使用 workload Service Token，不使用共享 Secret/API Key。
- [ ] Console/BOSS 页面同源，Cookie、CSRF、Platform/Tenant boundary 和管理操作 E2E 通过。

**Verification:** 五类调用方独立 E2E、契约摘要、配置、镜像和负向证据完整。

**Stop conditions:** 任一调用方缺失、需要 legacy fallback 或目标 Artifact 无法固定。

**Recovery:** 所有调用方保持在隔离目标轨道，主环境仍使用 P1 Runtime。
