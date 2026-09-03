# 08: 验证 Principal 与 Permission 决策

**What to build:** 交付旧 Principal 校验与 Permission allow/deny 的兼容纵向链路，使 Gateway 和 Envoy 行为能被独立比较。

**Blocked by:** 04 / 接通真实旧 PG/RLS 与 Redis；05 / 验证 Password、Refresh 与 Revoke

**Status:** wontfix

**Superseded by:** Direct P2 DP2-05、DP2-09；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。本状态只关闭旧授权兼容切片。

**Plan mapping:** CP0-4

**Baseline:** 01 固定的 ValidateToken、ValidatePrincipal、CheckPermission 两代 RPC 与调用方错误 Oracle。

**Scope:** 有效/无效身份、allow/deny、Tenant 状态、依赖失败、Claim、错误映射以及 Gateway/Envoy 可观察结果。

**Out of scope:** P2 生成 Permission、一次 Gateway 决策、typed obligation 或 legacy 路径删除。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/service/**`、`tests/cp0/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、Gateway/Envoy 运行配置写入。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/08-verify-principal-permission-decisions/`

- [ ] Principal 有效、无效 Credential、状态拒绝和依赖不可用均有独立用例。
- [ ] Permission allow、deny 和错误映射与基线差分一致。
- [ ] Gateway 通过不能替代 Envoy Adapter 的验证证据。
- [ ] 数据库/Redis 副作用和日志结果被纳入差分。

**Verification:** RPC differential、Gateway fixture 和 Envoy fixture 独立通过。

**Stop conditions:** 需要修改授权语义或以一个调用方结果替代另一个。

**Recovery:** 清理隔离授权 Fixture，不改变调用方配置。
