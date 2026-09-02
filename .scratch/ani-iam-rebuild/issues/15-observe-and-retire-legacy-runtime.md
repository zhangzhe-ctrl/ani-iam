# 15: 观察并下线旧 Auth Runtime

**What to build:** 在 Kratos 承载测试流量后完成明确观察，并在没有旧 Auth 流量时安全下线旧 Runtime，同时保留 P1 恢复依据。

**Blocked by:** 14 / 切换 Selector 并验证回滚

**Status:** ready-for-agent

**Plan mapping:** P1-6

**Baseline:** 14 的切换结果、固定镜像、测试数据快照和观察起点。

**Scope:** 测试环境观察、错误/状态对比、旧流量检查、旧 Deployment/Service 与过渡配置下线。

**Out of scope:** 删除旧 Proto/Schema/兼容代码、P2 Credential 失效或目标切换。

**Allowed paths:** `configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`internal/compat/authv1/**`、`migrations/**`、`../ANI/repo/api/**`、`../ANI/repo/deploy/migrations/**`、旧 Auth 数据结构与 Credential。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/15-observe-and-retire-legacy-runtime/`

- [ ] 观察时长、流量、错误、依赖和未验证生产级 soak 被明确记录。
- [ ] 没有调用方或 Endpoint 继续使用旧 Runtime。
- [ ] 下线前获得针对精确旧资产的人工确认。
- [ ] 下线后三个调用方和完整旧契约仍由 Kratos 正常承载。

**Verification:** 流量/Endpoint 检查、三调用方冒烟和旧资产零运行引用通过。

**Stop conditions:** 仍有旧流量、观察异常或恢复依据不完整。

**Recovery:** 重新部署固定 legacy 镜像并恢复 selector；不进入 P2。

**Human checkpoint:** 删除或缩容旧 Runtime 前必须获得精确人工确认。
