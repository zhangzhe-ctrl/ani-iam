# 10: 完成 Administration 与 Recovery

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-22 身份管理/恢复安全；WR-20 Human/Notification；WR-23 邀请集成。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 实现并采用 WR-02 已冻结的 Invitation、Role/Binding、Platform/BOSS、owner-specific Workload 管理合同，以及 request/approve/execute 三阶段双人高风险 Recovery；不得在本票新增或修改公开合同。

**Blocked by:** 09 / 隔离轨道 Go-No-Go B

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-10 / replaces DP2-15

**Scope:** WR-02 frozen admin/recovery contract digest及实现；从WR-05首位Platform Human出发，以正式Invitation建立第二位不同active且login-capable system-admin；owner-specific Workload entrypoints、BOSS delegation、Invitation races，以及`PlatformAdministrationGuard`维护的active-admin与login-capable-admin双不变量；Human/Membership/Role Binding、OIDC Identity、verified-email、BOSS provider-policy writers；request/approve/execute三阶段双人Recovery。所有mutation与Audit同IAM UoW；Invitation/password action才写既有notification outbox。

**Non-goals:** Support Session、delegated administrator、Purge、direct DB repair、generic Workload CRUD allowing Tenant→Platform elevation。

**Acceptance:** active实现严格匹配WR-02。`active system-admin`要求active Human+Platform Membership+seed-owned bootstrap Role Binding；`login-capable admin`还要求至少一个active trusted、匹配current BOSS authentication/provider policy及verified-email ownership的Identity/auth method。任何normal mutation必须让两个集合均至少一人；`2→1`仅在remaining admin仍login-capable时允许，任一集合`1→0`拒绝。只剩一人时双人recovery fail closed，但可通过正式Invitation补回第二位。Human disable、Membership suspend/remove、Role Binding remove、OIDC Identity unlink/disable、verified-email ownership变化、会失效最后BOSS auth method的provider-policy mutation全部复用同一Guard/UoW锁定重查并写mutation+Audit。Role catalog immutable且tamper fail。Package generation/validation/restore拒绝zero-active或zero-login-capable。外部IdP outage不是DB invariant，保持fail closed并在独立break-glass/DR事项接受前为`not_verified`。其余owner boundary、Platform receipt、三阶段recovery、不同requester/approver、capability/hash/expiry/reauth/concurrency及RetrySemantics沿frozen contract执行；recovery不写generic outbox。

**Verification:** 从首位admin邀请第二位；验证`2→1`仅在剩余者login-capable时成功、active或login-capable `1→0`稳定拒绝、单admin recovery deny及补回后allow。对Human/Membership/Role Binding/OIDC Identity/verified-email/provider-policy入口做逐项和cross-entry race：合并会让任一集合归零时最多一个提交，失败侧无mutation/Audit残片。证明Role无runtime writer且tamper fail；package/restore zero-active/zero-login-capable无部分写。外部IdP outage记录为operational `not_verified`。其余self-approval、capability/owner/Tenant/target/hash/reason、expiry/reauth、execute concurrency、Audit/outbox原子性、delegation、Retry与raw Secret零存储、real dependency/UI/security gates通过。

**Stop conditions:** WR-02 frozen contract有缺口或漂移；需要generic admin bypass、self approval、direct DB write、调用方自报owner、跨UoW Audit/outbox或无法证明并发exactly-once。

**Recovery:** 停止隔离调用方并恢复本票前固定代码/artifact；只丢弃并重建本票专用 disposable DB，不逐行删除或改写 Audit/ledger，也不执行真实 recovery或触碰共享数据。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/10-complete-administration-recovery/`
