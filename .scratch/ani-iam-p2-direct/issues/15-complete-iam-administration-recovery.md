# 15: 完成 IAM 管理、Platform 与高风险恢复

**What to build:** 在 Go/No-Go B 通过后补齐 Invitation、完整 Role/Binding/Membership、Platform/BOSS 管理，以及 RestoreTenantAdmin/RecoveryBootstrap 双人恢复。

**Blocked by:** 14 / 演练隔离测试轨道整组切入与回退（Go/No-Go B 已人工接受）

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-3 / complete IAM administration

**Baseline:** 14 人工接受的切换关键系统、02/03 固定契约、目标 Invitation/Role/Platform/Recovery 决策。

**Scope:** Tenant/Platform Invitation、邮箱证明、Human 建立、Custom/System Role、Binding、Membership lifecycle、last-admin、Platform Membership/Role/BOSS、RestoreTenantAdmin、RecoveryBootstrap、通知 outbox、Audit/幂等和 UI。

**Out of scope:** delegated administrator、Support Session、Platform Service Principal、自由注册、自动邮箱合并、数据库直写或新契约。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和证据目录。

**Forbidden paths:** 已冻结 `api/**`/OpenAPI、旧 Auth、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/deploy/**`；不得共享 Tenant/Platform Role、Principal 直接授权、自批恢复或从邮箱推断目标。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/15-complete-iam-administration-recovery/`

- [ ] Invitation 在身份/邮箱证明前不创建 Membership；race/resend/cancel/accept 语义稳定。
- [ ] Custom/System Role、Role-in-use、last-admin、Membership suspend/remove 和两 Tenant 隔离成立。
- [ ] Platform 与 Tenant Membership/Role/repository/browser boundary 完全分离。
- [ ] 两种恢复操作有独立 capability、requester/approver、近期 reauth、单次 approval、payload binding、一小时过期和不可删除 Audit。

**Verification:** unit、真实数据库、邀请/last-admin/recovery 并发、事务/Audit、两 Tenant/Platform 负向、Console/BOSS E2E 和 Secret 扫描通过。

**Stop conditions:** 需要 pending Membership、自动账号合并、共享 Role、NULL Tenant/Boolean bypass、自批恢复或修改冻结契约。

**Recovery:** 清理隔离 Invitation/Role/Membership/Platform/Recovery 数据；已提交恢复只能通过受控反向变更，不删除 Audit。

**Human checkpoint:** 任何真实 RestoreTenantAdmin/RecoveryBootstrap 执行前，必须确认精确 Tenant、目标 Principal、payload hash 和 approval reference。
