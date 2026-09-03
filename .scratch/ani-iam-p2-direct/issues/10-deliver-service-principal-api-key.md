# 10: 交付 Service Principal、API Key 与 Envoy 验证

**What to build:** 交付固定单 Tenant 的 Service Principal、Bearer API Key 生命周期，并让 Envoy Adapter 独立验证目标 Credential 的 allow/deny/failure 行为。

**Blocked by:** 04 / 建立无 RLS 持久化基础；09 / 交付 Tenant Access、Membership 与目标授权

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / API key and Envoy

**Baseline:** 02/03 固定契约、04 受限 persistence、09 Membership/Role/CheckPermission。

**Scope:** Service Principal、唯一 Tenant Membership/Binding、API Key create/list/revoke/disable、Secret 单次显示/Hash、过期/告警、Bearer 认证、Audit/幂等和 Envoy 独立 E2E。

**Out of scope:** Workload Service Token、Platform Service Principal、权限快照、自动 rotation 或旧 API Key 迁移。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/envoy-authz-adapter/**`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/**`；不得保存明文 Secret、创建跨 Tenant/Platform Service Principal、用 API Key 代替 Workload Token。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/10-deliver-service-principal-api-key/`

- [ ] Key 有明确 Principal/Tenant/boundary，Secret 只显示一次且仅保存 Hash。
- [ ] create/list/revoke/disable、过期、并发和稳定错误完整，状态与 Audit/幂等原子。
- [ ] Envoy 对有效、无效、撤销、权限拒绝、IAM 不可用分别有独立证据。
- [ ] Service Principal 移除唯一 Membership 时 disable 且撤销全部 Key。

**Verification:** 生命周期、真实数据库、并发/故障、两 Tenant、Secret 扫描、Envoy E2E 和错误映射通过。

**Stop conditions:** Key 无主体、跨 Tenant、必须保存明文/Permission snapshot，或 Envoy 只能依赖 Gateway 证据。

**Recovery:** 吊销并清理隔离 Key/Service Principal，恢复 Envoy 隔离配置；不影响 Human Principal。
