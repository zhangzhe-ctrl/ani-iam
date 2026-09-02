# 27: 交付 Service Principal 与 API Key

**What to build:** 让 Tenant 管理员创建固定单 Tenant 的 Service Principal，并通过可独立管理的 Bearer API Key 安全调用允许的 SDK operation。

**Blocked by:** 18 / 建立无 RLS 持久化基础；21 / 交付 Role、Binding 与 Membership 管理

**Status:** ready-for-agent

**Plan mapping:** P2-4

**Baseline:** 21 的 Membership/Role Binding，以及目标 API Key Credential 和错误决策。

**Scope:** Service Principal、唯一 Tenant Membership、Role Binding、API Key create/list/revoke/disable、Secret/Hash、过期/告警、Audit/幂等。

**Out of scope:** 内部 Workload Token、Platform Service Principal、创建者权限快照、自动 Key rotation。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得实现 Platform Service Principal、内部 Workload Token、权限快照或明文 Secret 持久化。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/27-deliver-service-principal-api-key/`

- [ ] Service Principal base/profile、Membership、Binding 和 Audit 原子提交；Key 由后续独立操作创建。
- [ ] Key Secret 只显示一次，数据库只存 Hash 与安全片段；只接受 Bearer 输入。
- [ ] 权限始终来自当前 Role Binding，高风险 Credential/Role/Recovery/Platform operation 被禁止。
- [ ] disable Principal 不可逆吊销全部 Key，重新 enable 不复活旧 Secret。

**Verification:** 生命周期、Secret 泄漏扫描、并发创建/吊销、两 Tenant 负向和错误映射测试通过。

**Stop conditions:** Key 无主体、跨 Tenant 或必须保存明文/Permission snapshot。

**Recovery:** 吊销并清理隔离 Key/Service Principal，不影响 Human Principal。
