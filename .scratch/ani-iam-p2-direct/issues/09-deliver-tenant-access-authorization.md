# 09: 交付 Tenant Access、Membership 与目标授权

**What to build:** 交付切换所需的 Tenant Access、Membership、基础 Role/Binding、Permission Catalog 和 Gateway 单次授权，使目标 TenantScope 与 typed obligation 可验证。

**Blocked by:** 05 / 证明目标最小纵向链路（Go/No-Go A 已人工接受）

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-2 / tenant authorization

**Baseline:** 02 registry/policy revision、03 IAM/Core 契约、04 persistence、05 一次决策纵向链路。

**Scope:** Tenant Access 基础状态、Human Membership、System Role/Binding、Permission Catalog、last-admin 基础保护、TenantScope/Platform Capability repository、ValidatePrincipal/CheckPermission、Gateway trusted context、typed obligation 和 Audit。

**Out of scope:** 完整 Invitation/Custom Role/Platform Admin/Recovery、Core/NATS 真链路、Service Principal/API Key、默认 allow 或 legacy fallback。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**` 中目标授权路径，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/**`；不得 Principal 直接授权、NULL Tenant、Boolean bypass、同步回调 Core、字符串推导 Permission 或默认 allow。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/09-deliver-tenant-access-authorization/`

- [ ] 普通租户授权验证 Principal、Tenant Access、Membership、Lifecycle projection freshness 和 Permission。
- [ ] Gateway Authorized 路由只调用 CheckPermission 一次并注入最小可信上下文。
- [ ] Permission/operation/policy mismatch fail closed，稳定映射 `401/403/503/504`。
- [ ] TenantScope 非空且 repository 无 unscoped 查询；两 Tenant/obligation/last-admin 负向成立。

**Verification:** unit、真实数据库、registry 静态门禁、Gateway E2E、两 Tenant/query mutation、obligation、并发 last-admin 和错误映射通过。

**Stop conditions:** 需要多次 IAM 决策、同步 Core、全局 Role、Principal 直接绑定、平台 bypass 或修改冻结契约。

**Recovery:** 清理隔离 Tenant Access/Membership/Binding 和目标 Gateway route；不改变主调用路径。
