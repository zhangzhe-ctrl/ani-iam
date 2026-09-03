# 20: 交付 Invitation 到 Human Membership

**What to build:** 让受邀者在证明 Invitation、邮箱所有权和身份后，原子建立 Human Principal、Identity、active Membership、Role Binding 与 Audit。

**Blocked by:** 19 / 交付 Tenant Access Bootstrap 纵向链路

**Status:** wontfix

**Superseded by:** Direct P2 DP2-15；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-4

**Baseline:** 19 的 Tenant Access/System Role/Bootstrap Operation 和目标 Invitation 决策。

**Scope:** Tenant Invitation、Token Hash、邮箱规范化/验证、Human Principal、Identity、Membership、Role Binding、通知 Outbox 和 Audit。

**Out of scope:** 自由注册、邮箱自动合并、delegated Role 管理；Platform Invitation 由 31 交付。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`deploy/**`、`../ANI/repo/**`；不得实现 Platform Invitation、自由注册或自动账号合并。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/20-deliver-invitation-human-membership/`

- [ ] Invitation 接受前不建立 Membership、不授予权限，Token 本身不等于身份。
- [ ] 相同 verified email 全局唯一，但相同邮箱不会自动合并 Principal。
- [ ] Accept 时重新验证 Role，并原子建立身份、Membership、Binding 和 Audit。
- [ ] Accept/Cancel/Resend 并发只有一个合法结果，重复请求返回稳定结果或冲突。

**Verification:** 契约、真实数据库、邀请 race、邮箱冲突、事务失败和通知 Outbox 测试通过。

**Stop conditions:** 必须先创建 pending Membership、自动合并账号或保存明文 Token。

**Recovery:** 清理隔离 Invitation/Principal/Membership 数据；不影响其他 Tenant。
