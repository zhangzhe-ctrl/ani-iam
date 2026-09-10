# 11: 交付 Workload Service Token 与 Inference 调用链

**What to build:** 让受信内部工作负载通过 mTLS/SPIFFE 换取短期 audience-bound Service Token，并让 Inference 独立使用该链路。

**Blocked by:** 03 / 冻结 IAM 与 Core 集成契约；04 / 建立无 RLS 持久化基础

**Status:** wontfix

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

## Comments

- 2026-09-09 baseline claim: `pass`. IAM start is `main@cd38cd90bca3e9d83af09a381051b895d82ae94f`, tree `a3668037d43bff9a3d938b9cf07500402e4ebd70`, with clean worktree/index; recorded `origin/main` is `fb862d75854e2a31d60ac5a40182dc678ae88c44`, and the only local commits above it are `13e709f4359b81f51444b9875c430b6ba98d00ec` and `cd38cd90bca3e9d83af09a381051b895d82ae94f`. The dedicated ANI worktree is `codex/direct-p2-06-14@f4af3902e346d15e69bdeceda725730edec3542f`, tree `5ac109556ea94592401b6b3bdd7cd409b9206d43`, clean. DP2-03, DP2-04, DP2-09, and DP2-10 are `resolved`; DP2-11 is the sole claimed Direct P2 issue and DP2-12 remains `ready-for-agent`. Recomputed frozen artifacts are IAM descriptor `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a`, Core descriptor `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a`, IAM Admin Proto `332dc8ad82bdc9e7c07618808028316a0c9ab735d0b71177095cb7d2f3a7d316`, Authorization Proto `7799bef6830dcdd6de8cf19f40b6d4a79361a35c06664e078b8b0ac61cf22050`, ANI operation registry `742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80`, and generated policy revision `sha256:1d5c80b83635e9a152c0edd9e8d1c9b66f5f8962e84cdd4f4701ecc486dd969c` (`pass`).
- 2026-09-09 production-seam checkpoint: `fail`. DP2-11 cannot be truthfully delivered inside the current Allowed paths. The IAM RPC middleware rejects `IssueServiceToken` before the allowed service layer and has no SPIFFE/workload allowlist; typed runtime config exposes only one Gateway DNS identity. The database permits only Human/Service principals while non-anonymous Audit actors must reference `principals`, so a Workload actor cannot be durably audited without either impersonating a Service Principal or changing the forbidden migration. The frozen ANI operation registry accepts `service_token` only with principal kind `service`; the target IAM port/adapter has no Workload principal, and Gateway target authorization requires Session/Grant UUIDs that a Service Token must not create. Exact evidence and decision options are in `../evidence/11-deliver-workload-service-token/path-expansion-checkpoint-20260909.md`. No implementation, ANI change, test resource, credential, commit, push, PR, deployment, or shared-state mutation was made. DP2-12 was not started.
- 2026-09-09 supersession: `wontfix` applies to this frozen ticket version, not to the capability. Its Service/Workload split, descriptor, registry and forbidden runtime/config/migration/ANI paths cannot carry the accepted Human/Workload refoundation. Replacement work is WR-02, WR-03, WR-05 and WR-06 under `.scratch/ani-iam-workload-refoundation/`; all existing checkpoint evidence remains unchanged.
