# 09: 完成三调用方门禁与 CP0 判定

**What to build:** 汇总七条兼容纵向链路并在 Gateway、Envoy、Inference 上独立验证，形成仅针对 Kratos 兼容性的 Go/No-Go 结论。

**Blocked by:** 05 / 验证 Password、Refresh 与 Revoke；06 / 验证 OIDC 与 Dex 登录；07 / 验证 API Key 与 Service Token；08 / 验证 Principal 与 Permission 决策

**Status:** ready-for-agent

**Plan mapping:** CP0-5

**Baseline:** 01 接受的 Oracle，以及 02–08 的固定 Commit、配置和证据。

**Scope:** Password、OIDC、Refresh/Revoke、ValidatePrincipal、Permission allow/deny、API Key、Service Token，以及三个调用方 live gates。

**Out of scope:** P1 Runtime 切换、P2 设计验证、目标契约或生产就绪声明。

**Allowed paths:** `tests/cp0/**`、本事项文件与 `.scratch/ani-iam-rebuild/evidence/09-complete-cp0-go-no-go/**`；对调用方和运行环境只读验证。

**Forbidden paths:** `api/**`、`cmd/**`、`internal/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**` 的任何写入。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/09-complete-cp0-go-no-go/`

- [ ] 七条链路均比较响应、错误、Claim、PG/Redis 状态和副作用。
- [ ] Gateway、Envoy、Inference 各自有真实配置、Commit/Digest 和结果，缺一项不得互相替代。
- [ ] 所有差异属于已接受 allowlist；其余失败如实阻塞。
- [ ] 结论明确为 Go 或 No-Go，并说明它只回答 Kratos 能否承载兼容 Runtime。

**Verification:** 重跑聚合门禁能定位到各独立证据，缺失项显示 `not_verified` 而非 pass。

**Stop conditions:** 任一调用方未验证、存在未批准差异或证据无法绑定固定基线。

**Recovery:** 不切流；保留证据并停止后续 Runtime 晋升。

**Human checkpoint:** 人工接受 CP0 Go 后，才能领取 10。
