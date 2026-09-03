# 28: 交付 Workload Service Token

**What to build:** 让受信内部工作负载通过 mTLS/SPIFFE 身份换取短期、不可刷新的 audience-bound Service Token，而不使用用户 Token、API Key 或共享 mint Secret。

**Blocked by:** 17 / 冻结 IAM 与 Core 集成契约；18 / 建立无 RLS 持久化基础

**Status:** wontfix

**Superseded by:** Direct P2 DP2-11；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-2

**Baseline:** 17 的 Authentication 契约和固定 workload identity/audience 决策。

**Scope:** workload allowlist、mTLS/SPIFFE 验证、Service Token issuance/validation、permission subset、KMS/Key Ring、Audit 和 Inference Fixture。

**Out of scope:** 用户 Session、API Key、公共 JWKS、长期共享 Secret、生产 KMS rotation 演练。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`configs/**`、`tests/**`、`../ANI/repo/services/inference-service/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、上述目录之外的 `../ANI/repo/**`；不得使用用户 Token、API Key、公共 JWKS 或长期共享 mint Secret。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/28-deliver-workload-service-token/`

- [ ] 只有 allowlisted workload identity 可签发，Token 最长五分钟、audience-bound、不可 refresh 且无 Session。
- [ ] 请求权限必须是 workload allowlist 的子集，未知或跨 audience 请求 fail closed。
- [ ] 签发使用非对称 Key 和 `kid`，retired public key 保留到已签 Token 过期。
- [ ] Inference 能使用 Token 调用允许的 Core service-only 路径，拒绝行为稳定。

**Verification:** mTLS/SPIFFE 正负向、Claim/audience、权限子集、Key rotation Fixture 和 Inference E2E 通过。

**Stop conditions:** 只能使用共享 Secret、API Key 或用户 Token 才能完成内部调用。

**Recovery:** 撤销测试 workload identity/Key，清理隔离签发状态。
