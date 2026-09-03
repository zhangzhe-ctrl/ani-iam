# 06: 验证 OIDC 与 Dex 登录

**What to build:** 交付旧 OIDC 登录的完整兼容链路，覆盖浏览器跳转、Dex 校验、身份结果和失败行为。

**Blocked by:** 04 / 接通真实旧 PG/RLS 与 Redis

**Status:** wontfix

**Superseded by:** Direct P2 DP2-07；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。本状态只关闭旧 OIDC 兼容切片，不取消目标 OIDC 需求。

**Plan mapping:** CP0-2、CP0-4

**Baseline:** 01 固定的 Dex Client、issuer、callback、state/nonce/JWKS 与错误 Oracle。

**Scope:** BeginOIDCLogin、CompleteOIDCLogin、真实 Dex、临时状态、nonce、callback、JWT/身份副作用和敏感信息脱敏。

**Out of scope:** 目标 P2 PKCE/Identity Link 语义、公共 JWKS、调用方切流。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/service/**`、`internal/conf/**`、`configs/**`、`tests/cp0/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、目标 Identity/OIDC 数据模型。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/06-verify-oidc-dex-login/`

- [ ] 成功、无效 state/nonce、issuer/audience/signature、拒绝和依赖不可用路径可复现。
- [ ] gRPC/public error、Token Claim、PG/Redis 状态和 Dex 交互与基线差分一致。
- [ ] 测试 Dex Client、身份和 Credential 与其他运行隔离。
- [ ] callback、Token、code 和邮箱等敏感信息不进入非必要日志。

**Verification:** 真实 Dex 端到端和差分用例通过；仅允许清单中的随机差异。

**Stop conditions:** 真实 Dex 不可用或兼容需要修改旧安全语义。

**Recovery:** 删除测试 Dex identity/client 状态和隔离临时状态。
