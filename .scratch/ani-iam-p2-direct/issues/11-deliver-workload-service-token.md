# 11: 交付 Workload Service Token 与 Inference 调用链

**What to build:** 让受信内部工作负载通过 mTLS/SPIFFE 换取短期 audience-bound Service Token，并让 Inference 独立使用该链路。

**Blocked by:** 03 / 冻结 IAM 与 Core 集成契约；04 / 建立无 RLS 持久化基础

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / workload identity

**Baseline:** 03 Authentication 契约、04 persistence、固定 workload identity/audience/permission-subset 决策。

**Scope:** workload allowlist、mTLS/SPIFFE 验证、最长五分钟 Service Token、audience/permission subset、Key Ring adapter、Audit 和 Inference 目标调用链。

**Out of scope:** Human Session、API Key、公共 JWKS、长期共享 mint Secret 或生产 KMS rotation 演练。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`configs/**`、`tests/**`、`../ANI/repo/services/inference-service/**`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`migrations/**`、`deploy/**`、其他 `../ANI/repo/**`；不得使用用户 Token、API Key、公共 JWKS 或共享 Secret 作为长期 mint 身份。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/11-deliver-workload-service-token/`

- [ ] 只有 allowlisted mTLS/SPIFFE workload 可签发，Token audience/permission/expiry 被约束。
- [ ] Token 不可 refresh，错误 audience/identity/permission fail closed。
- [ ] Inference 独立验证成功、拒绝、无效和 IAM 不可用行为。
- [ ] Secret/Private Key 不进入日志、fixture 或证据。

**Verification:** mTLS/SPIFFE 正负向、claims/audience、Key Ring fixture、Secret 扫描和 Inference E2E 通过。

**Stop conditions:** 只能用共享 Secret、API Key、用户 Token、公共 JWKS或修改冻结契约才能完成。

**Recovery:** 撤销隔离 workload identity/Key，清理签发状态并恢复 Inference 隔离配置。
