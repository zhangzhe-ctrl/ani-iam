# 12: 建立稳定 IAM 服务地址

**What to build:** 建立稳定的 iam-service 访问名称，并让三个调用方在后端仍为旧实现时完成地址迁移，从而把命名变化与 Runtime 切换分开。

**Blocked by:** 11 / 补齐完整旧 Auth Runtime

**Status:** ready-for-agent

**Plan mapping:** P1-4

**Baseline:** 固定旧 Auth 与 Kratos 镜像、当前三个调用方配置。

**Scope:** 稳定 Service/DNS、新旧环境变量过渡、Endpoint 验证；selector 仍只选择旧实现。

**Out of scope:** selector 切到 Kratos、双轨流量、长期配置 fallback、旧服务删除。

**Allowed paths:** `configs/**`、`deploy/**`、`../ANI/repo/deploy/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/envoy-authz-adapter/**`、`../ANI/repo/services/inference-service/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`internal/biz/**`、`internal/data/**`、`migrations/**`、`../ANI/repo/api/**`、`../ANI/repo/deploy/migrations/**`、`../ANI/repo/frontends/**`。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/12-establish-stable-iam-address/`

- [ ] 新稳定地址存在且只路由到旧 Auth backend。
- [ ] Gateway、Envoy、Inference 使用新地址后外部行为不变。
- [ ] 同一 Service 不同时选择 legacy 与 Kratos Pod。
- [ ] 配置回退方法和固定镜像被验证。

**Verification:** Endpoint、调用方连接、健康与兼容冒烟通过。

**Stop conditions:** 地址迁移会同时改变 Runtime 或产生混合 backend。

**Recovery:** 调用方恢复旧地址，删除新稳定地址配置。
