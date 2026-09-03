# 21: 交付 Role、Binding 与 Membership 管理

**What to build:** 让 tenant-admin 可以管理自定义 Role、Role Binding 和 Membership 生命周期，同时保证系统 Role、跨 Tenant 边界与最后管理员不变量。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；20 / 交付 Invitation 到 Human Membership

**Status:** wontfix

**Superseded by:** Direct P2 DP2-09、DP2-15；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-3、P2-4

**Baseline:** 16 生成的 Permission Catalog，以及 20 的 active Membership/Role Binding。

**Scope:** Permission Catalog、System/Custom Role、多个 Role Binding、Membership suspend/remove、Role-in-use、last-admin、Audit/幂等。

**Out of scope:** deny/继承/优先级、delegated administrator、Service Principal Key；Platform Role 由 31 交付。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得实现 Platform Role、Principal 直接授权、deny 或权限继承。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/21-deliver-role-binding-membership-admin/`

- [ ] Permission 只来自生成 Catalog，未知或跨边界 Permission fail closed。
- [ ] 多个 active Role 的权限为纯 allow-list 并集；系统 Role 不可被 Tenant 修改。
- [ ] 只有 tenant-admin 可管理 Role/Binding，跨 Tenant 关联被应用与数据库共同拒绝。
- [ ] 并发操作不能移除最后一个 active Human tenant-admin；Service Principal 不计入。

**Verification:** Role CRUD/binding、Role-in-use、两 Tenant 负向、last-admin 并发和 Audit 事务测试通过。

**Stop conditions:** 需要全局 Role、Principal 直接绑定或平台 bypass 才能实现。

**Recovery:** 回退隔离 Role/Binding 变更；保留系统 Role seed。
