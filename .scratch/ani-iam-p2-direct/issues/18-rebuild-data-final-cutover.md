# 18: 重建测试数据并最终整组切换

**What to build:** 在精确确认的测试维护窗口，从空库重建目标 IAM/Core 状态、失效全部旧 Credential，并把五类调用方最终整组切到目标系统。

**Blocked by:** 17 / 完成目标调用方与全功能 UI E2E

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-4 / destructive test cutover

**Baseline:** 17 接受的目标 Artifact/配置/功能证据、明确数据清单、固定 migration/seed、测试快照和恢复步骤。

**Scope:** 冻结测试写入、快照、目标 migration/seed、新 signing Key、旧 Credential 全失效、IAM/Core/五类调用方整组部署、目标-only 冒烟和人工恢复。

**Out of scope:** 生产环境、逐行线上迁移、长期双写、兼容视图、业务/契约修复、旧资产物理删除或 Production Ready 声明。

**Allowed paths:** `migrations/**`、`configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**` 中的部署/配置/测试路径，以及本事项和证据目录；仅限人工确认的测试环境。

**Forbidden paths:** `api/**`、`internal/biz/**`、`internal/data/**`、`internal/service/**`、生产/共享非目标环境、旧资产删除；发现实现缺口必须停止，不得在切流票修复语义。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/18-rebuild-data-final-cutover/`

- [ ] 精确记录环境、数据范围、快照校验、migration/seed、镜像、contract digest、config 和恢复命令。
- [ ] 所有旧 Token/API Key/Session/签名边界明确失效，目标 Credential 正常。
- [ ] IAM/Core/五类调用方为单一目标部署单元，无混合 backend、fallback 或双写。
- [ ] 五类冒烟、两 Tenant、Lifecycle、Audit/Idempotency 和浏览器边界通过。

**Verification:** 快照可读、空库 replay/checksum、seed、Credential 正负向、五类 E2E、目标-only/Endpoint/数据状态检查通过。

**Stop conditions:** 未获精确确认；快照/恢复不可用；写入不能冻结；Artifact 漂移；任一目标冒烟失败或出现混合 backend。

**Recovery:** 按固定旧镜像、配置和 snapshot/reseed 人工恢复完整测试单元；结果按 `pass/fail/not_verified` 留证。

**Human checkpoint:** 执行快照后破坏性写入、数据重建、Credential 失效和最终切流前，必须确认精确环境、数据、Artifact、窗口和恢复动作。
