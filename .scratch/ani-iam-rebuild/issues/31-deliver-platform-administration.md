# 31: 交付 Platform Role、Invitation 与 Membership 管理

**What to build:** 让具备专用 Platform Capability 的人员管理 Platform Invitation、Platform Membership、Platform Role 与 Role Binding，同时保持它们和 Tenant 授权关系彻底分离。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；18 / 建立无 RLS 持久化基础；20 / 交付 Invitation 到 Human Membership；21 / 交付 Role、Binding 与 Membership 管理；25 / 交付浏览器与 Platform/BOSS 边界

**Status:** wontfix

**Superseded by:** Direct P2 DP2-15；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-4

**Baseline:** 16 的 Platform operation/Permission，18 的 Platform Repository，20/21 的 Invitation 与 Role 不变量，以及 25 的 active Platform Membership 登录边界。

**Scope:** Platform Invitation、Platform Membership、Platform Role/Permission/Binding、BOSS 管理入口、幂等、Audit 和 Tenant/Platform 负向隔离。

**Out of scope:** delegated administrator、Recovery Bootstrap、RestoreTenantAdmin、Tenant Role 复用、Platform Service Principal。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`../ANI/repo/api/openapi/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/inference-service/**`、`../ANI/repo/deploy/**`；不得修改 16/17 已冻结契约、Tenant Role 语义或创建 Platform Service Principal。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/31-deliver-platform-administration/`

- [ ] Platform Invitation 与 Tenant Invitation 分表，只有 Human Principal 可接受并建立 Platform Membership。
- [ ] Platform Role/Binding 只引用 Platform Membership 和 Platform Permission，不能绑定 Tenant Membership/Role。
- [ ] BOSS 管理操作要求 active Platform Membership、专用 Capability、近期认证策略和 Audit。
- [ ] 所有公开 mutation 使用统一幂等接口；跨 Tenant/Platform 关联被应用与数据库共同拒绝。

**Verification:** Platform Invitation/Role/Membership E2E、BOSS 管理、跨边界负向、并发、幂等和 Audit 事务测试通过。

**Stop conditions:** 需要 NULL Tenant、Boolean bypass、共享 Role 表或 Service Principal 平台身份。

**Recovery:** 回退隔离 Platform 管理数据和 BOSS 入口，不影响 Tenant Membership。
