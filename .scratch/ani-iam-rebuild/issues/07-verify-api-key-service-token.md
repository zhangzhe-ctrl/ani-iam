# 07: 验证 API Key 与 Service Token

**What to build:** 交付旧 API Key 生命周期和内部 Service Token 签发的兼容纵向链路，证明两个 Credential 域没有被混用。

**Blocked by:** 04 / 接通真实旧 PG/RLS 与 Redis

**Status:** ready-for-agent

**Plan mapping:** CP0-2、CP0-4

**Baseline:** 01 固定的 API Key 输入/Hash/状态、Service Token Claim 和调用方身份 Oracle。

**Scope:** Create/List/Revoke API Key、IssueServiceToken、Secret 单次显示、Hash、状态、错误和签发副作用。

**Out of scope:** P2 Service Principal 模型、SPIFFE 目标交换、目标 API Key Bearer 语义。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/service/**`、`tests/cp0/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、目标 Service Principal/Service Token 模型。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/07-verify-api-key-service-token/`

- [ ] API Key 创建、列表、吊销、无效/过期/已吊销路径与基线一致。
- [ ] Service Token audience、有效期、Claim、拒绝和依赖失败行为与基线一致。
- [ ] Secret 只在允许的创建响应出现，证据和日志不保存完整 Secret 或 Hash。
- [ ] API Key 与内部 Service Token 的调用身份和权限边界没有互换。

**Verification:** 真实持久化、golden Credential 和 Inference fixture 差分通过。

**Stop conditions:** 必须引入 P2 身份模型才能兼容，或 Secret 无法安全隔离。

**Recovery:** 吊销并清理隔离 Credential，保留脱敏证据。
