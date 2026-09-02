# 14: 切换 Selector 并验证回滚

**What to build:** 在测试窗口把稳定服务从旧 Auth 切到 Kratos，并证明三个调用方、差分结果、Endpoint 收敛和回滚路径可用。

**Blocked by:** 13 / 运行隔离 Kratos Canary

**Status:** ready-for-agent

**Plan mapping:** P1-5

**Baseline:** 13 接受的 Canary、固定镜像、测试数据快照和稳定地址。

**Scope:** 单一 selector 切换、三个调用方冒烟、差分、Endpoint 收敛、回滚演练和证据。

**Out of scope:** 双写、目标 P2 Schema/契约、旧 Runtime 删除。

**Allowed paths:** `configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`internal/biz/**`、`internal/data/**`、`migrations/**`、`../ANI/repo/api/**`、`../ANI/repo/deploy/migrations/**`、`../ANI/repo/services/auth-service/**` 的删除。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/14-cut-selector-to-kratos/`

- [ ] 切换前获得针对精确环境、selector、镜像和恢复动作的人工确认。
- [ ] 稳定 Service 在任一时刻只选择一个 Runtime track。
- [ ] 三调用方与完整差分门禁在切换后通过。
- [ ] 回滚到固定 legacy 镜像和状态的方法实际验证。

**Verification:** 切换前后 Endpoint、调用方 E2E、差分和回滚证据完整。

**Stop conditions:** 快照/回滚不可用、Endpoint 混合或任一调用方失败。

**Recovery:** 立即恢复 legacy selector 和固定镜像，验证调用方恢复。

**Human checkpoint:** 执行 selector 写操作前必须获得精确人工确认。
