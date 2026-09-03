# 32: 交付高风险 Tenant Admin 恢复

**What to build:** 分别交付 RestoreTenantAdmin 与 Recovery Bootstrap，使丢失管理员或原始 Bootstrap 意图损坏时能够恢复，又不能让普通平台管理员、自批或重放审批建立最高租户权限。

**Blocked by:** 19 / 交付 Tenant Access Bootstrap 纵向链路；20 / 交付 Invitation 到 Human Membership；21 / 交付 Role、Binding 与 Membership 管理；24 / 交付 Session Grant 与旋转 Refresh；31 / 交付 Platform Role、Invitation 与 Membership 管理

**Status:** wontfix

**Superseded by:** Direct P2 DP2-15；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-4

**Baseline:** Q42、Q47、Q53、Q148 的恢复决策，以及 19–24、31 的 Tenant/Platform 身份与授权状态。

**Scope:** `RestoreTenantAdmin`、`RecoveryBootstrap`、专用 Platform Capability、request/approval、近期重新认证、单次 approval、payload binding、一小时有效期、不可删除 Audit 和并发保护。

**Out of scope:** 普通 Membership 管理、重放仍存在的原始 Bootstrap、数据库直写、自动猜测管理员身份。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`../ANI/repo/api/openapi/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/deploy/**`；不得修改 16/17 已冻结契约、直接修改 Role Binding、复用普通 Bootstrap、允许自批或从邮箱推断恢复目标。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/32-deliver-high-risk-tenant-admin-recovery/`

- [ ] RestoreTenantAdmin 与 Recovery Bootstrap 是两个独立 operation；前者不重新执行 Bootstrap，后者只在原始 operation/intent 丢失时使用。
- [ ] requester 与 approver 必须不同，禁止自批；approval reference 只能使用一次。
- [ ] approval 绑定 operation、Tenant、已验证目标 Principal、payload hash 与 reason，一小时后失效。
- [ ] 执行人必须在执行前十五分钟内重新认证，并持有专用 Platform Capability。
- [ ] 成功、拒绝、过期、重放和并发竞争均写入不可删除安全审计；失败不产生部分管理员关系。

**Verification:** 双人审批、自批拒绝、单次 approval、过期、payload 篡改、近期 reauth、并发恢复和 Audit 事务测试通过。

**Stop conditions:** 无法区分两个恢复操作、approval 不能单次消费或 Audit 无法与状态原子提交。

**Recovery:** 在提交前取消申请；提交后只能通过另一项受控管理员变更撤销，不得删除审计或直接改库。

**Human checkpoint:** 任何真实恢复执行前必须获得绑定精确 Tenant、目标 Principal、payload hash 和 approval reference 的人工确认。
