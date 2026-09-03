# 14: 演练隔离测试轨道整组切入与回退

**What to build:** 在精确人工确认的隔离测试轨道，把五类调用方整组切入目标 IAM/Core，再通过固定部署配置完整回退，证明切换关键功能可替换旧 Auth。

**Blocked by:** 13 / 完成五类切换关键调用面对等

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / Go-No-Go B

**Baseline:** 13 接受的五类调用方证据、固定镜像/契约/config/seed、精确隔离轨道和部署级恢复步骤。

**Scope:** 切换前检查、隔离轨道路由/selector/config 整组切入、五类冒烟/负向、观察、部署级回退、Endpoint 收敛和完整证据。

**Out of scope:** 主测试环境切换、数据重建、旧 Credential 失效、旧资产删除、业务/契约修复、生产操作或代码 fallback。

**Allowed paths:** `configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**` 中的部署/配置/测试路径，以及本事项和证据目录；仅限人工确认的隔离测试轨道。

**Forbidden paths:** `api/**`、`internal/**`、`migrations/**`、调用方业务语义、旧 Auth 删除、共享/生产环境；不得双写、代码 fallback、Credential 失效或数据重建。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/14-rehearse-isolated-cutover-rollback/`

- [ ] 切换前精确记录环境、namespace、镜像、contract digest、config、seed、旧/新 Endpoint 和恢复动作。
- [ ] 五类调用方在整组目标状态通过成功/拒绝/依赖失败冒烟，无混合 backend。
- [ ] 回退后旧系统恢复且 Endpoint 收敛，无目标代码 fallback、旧资产删除或 Credential 失效。
- [ ] 所有观察结果按 `pass/fail/not_verified` 记录，失败立即停止。

**Verification:** 部署前后状态、Endpoint、五类 E2E、fallback/混合 backend 扫描、回退耗时和残留资源检查。

**Stop conditions:** 未获精确确认；轨道不能隔离；切入需要代码 fallback/双写；任一调用面失败；无法完整回退。

**Recovery:** 立即按固定旧镜像/config/selector 回退隔离轨道，保留失败证据；不扩到主测试环境。

**Human checkpoint:** 实际切入前必须确认精确环境、namespace、目标/旧镜像、配置、窗口和恢复动作。完成后输出 Go/No-Go B 等待人工接受，不自动领取 15。
