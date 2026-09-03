# 17: 完成目标调用方与全功能 UI E2E

**What to build:** 在隔离目标轨道让 Gateway、Envoy、Inference、Console 和 BOSS 使用完整目标能力，并形成最终切换前的 zero-reference 与功能 E2E 证据。

**Blocked by:** 15 / 完成 IAM 管理、Platform 与高风险恢复；16 / 完成 Audit 查询与全局 Mutation 幂等

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-3 / full caller and UI E2E

**Baseline:** 15/16 固定实现 Artifact、02/03 契约、13 切换关键 E2E 和隔离测试环境。

**Scope:** 五类调用方完整目标功能、Console/BOSS 管理 UI、Gateway registry、Envoy Credential、Inference workload、Core lifecycle、target DNS/config、删除前引用扫描和独立 E2E。

**Out of scope:** 主测试环境破坏性切换、数据重建、旧 Credential 失效、旧资产删除或在验收票中顺带修复业务。

**Allowed paths:** `tests/e2e/**`、`configs/**`、`deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`、`../ANI/repo/deploy/**` 中隔离目标路径，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`migrations/**`、`internal/biz/**`、`internal/data/**`、旧 Auth 删除、主测试环境；发现实现/契约缺口必须停止并回开对应事项。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/17-complete-target-callers-ui-e2e/`

- [ ] 五类调用方的完整功能、拒绝、依赖失败、timeout 和浏览器安全边界分别通过。
- [ ] 所有消费者固定目标契约/镜像/config，无动态依赖或 legacy fallback。
- [ ] 最终切换 manifest、seed、Credential 失效清单和旧资产 zero-reference 候选完整。
- [ ] 任一 `not_verified` 明确列出，不以其他 caller 或 fixture 替代。

**Verification:** 五类全功能 E2E、契约/镜像/config 摘要、fallback/legacy reference 扫描、浏览器安全和删除前 inventory 通过。

**Stop conditions:** 任一调用方/功能缺失；需要 legacy fallback；Artifact 无法固定；发现实现、契约或数据缺口。

**Recovery:** 恢复隔离目标轨道配置；主测试环境和旧 Credential 保持不变。
