# 26: 交付 Permission 与单次 Gateway 授权

**What to build:** 让 Gateway 按生成 registry 对每个公开路由执行零次或一次 IAM 决策，并用 TenantScope、Platform Capability 和 typed obligation 保持授权边界。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；19 / 交付 Tenant Access Bootstrap 纵向链路；21 / 交付 Role、Binding 与 Membership 管理；24 / 交付 Session Grant 与旋转 Refresh

**Status:** ready-for-agent

**Plan mapping:** P2-3

**Baseline:** 16 的 registry/policy revision、19–24 的身份与授权状态。

**Scope:** ValidatePrincipal、CheckPermission、Permission Catalog、TenantScope/Platform Repository、Gateway one-call、可信 Principal Context、typed obligation 和错误映射。

**Out of scope:** 资源 Owner 数据查询、客户端身份 Header、legacy fallback、默认 allow。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、上述目录之外的 `../ANI/repo/**`；不得修改 Core/Services 所有权语义、加入 legacy fallback 或默认 allow。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/26-deliver-one-call-authorization/`

- [ ] Public 不调用 IAM；Authenticated-only 调用一次 ValidatePrincipal；Authorized 直接调用一次 CheckPermission。
- [ ] 未知 operation、缺失标注和 policy revision 不匹配 fail closed。
- [ ] 客户端 `x-ani-*` 身份 Header 被移除，只在可信第一跳注入最小 Context。
- [ ] 401/403/429/503/504 与 obligation Handler 行为符合契约。

**Verification:** registry 静态门禁、Gateway E2E、两 Tenant 负向、obligation 和错误映射测试通过。

**Stop conditions:** 同一路由需要多次 IAM 调用、字符串推导或无法确定资源 Owner。

**Recovery:** 保持目标授权轨道隔离，不切换主调用方。
