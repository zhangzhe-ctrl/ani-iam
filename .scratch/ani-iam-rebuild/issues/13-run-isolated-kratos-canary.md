# 13: 运行隔离 Kratos Canary

**What to build:** 让 Gateway、Envoy 和 Inference 在隔离路径下分别调用完整 Kratos Runtime，验证真实 Dex、API Key 和 Service Token 行为且不影响旧主路径。

**Blocked by:** 11 / 补齐完整旧 Auth Runtime；12 / 建立稳定 IAM 服务地址

**Status:** wontfix

**Superseded by:** Direct P2 DP2-13、DP2-14；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。不运行 compat canary，改验证目标调用面和整组回退。

**Plan mapping:** P1-3

**Baseline:** 固定 legacy/Kratos 镜像、稳定地址配置和 CP0/P1 差分 Oracle。

**Scope:** 隔离 Canary、三调用方独立路由、真实依赖、差分证据和回退。

**Out of scope:** 主 selector 切换、旧 Runtime 下线、P2 契约。

**Allowed paths:** `configs/**`、`deploy/**`、`tests/e2e/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`migrations/**`、`../ANI/repo/api/**`、`../ANI/repo/deploy/migrations/**`、`../ANI/repo/frontends/**`；不得修改主 selector。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/13-run-isolated-kratos-canary/`

- [ ] 三个调用方均能单独指向 Canary，且证据不能互相替代。
- [ ] Password/OIDC/API Key/Service Token/Permission 代表链路通过。
- [ ] Canary 不写旧 Auth 唯一状态空间，不接收未选择的主流量。
- [ ] 回退后 Endpoint、状态和调用方恢复可验证。

**Verification:** 三调用方 E2E、差分和回退演练通过。

**Stop conditions:** 无法隔离状态、任一调用方失败或出现未批准差异。

**Recovery:** 将调用方恢复到 legacy backend，销毁 Canary 隔离状态。
