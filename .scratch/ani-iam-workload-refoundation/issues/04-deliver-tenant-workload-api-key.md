# 04: 交付 Tenant-owned Workload 与 API Key

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-21 Tenant Workload/API Key；WR-22 Membership/Role。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在不替换 active IAM contract/composition root 的 target-only build seam 中，实现 WR-02 candidate 的 Tenant-owned Workload 管理、Membership/Role Authority 与 Bearer API Key 能力；WR-05 原子采用完整 IAM candidate，真实 Envoy consumer 延后到 WR-06。

**Blocked by:** 03 / 重建 Human / Workload persistence foundation

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-04 / replaces DP2-10 target behavior

**Scope:** WR-02 frozen candidate adapter、非 active target composition fixture、Tenant admin usecases、原子 profile/membership/bindings/audit、API Key create/list/revoke/usage、prepare replacement + activate/revoke-old 两阶段 rotation、`ledger_one_time_secret|ledger_replay` outcomes、PrincipalContext APIKeyContext + TenantMembershipAuthority、disable/remove semantics、受控 client。

**Non-goals:** 修改 active Proto/generated/import/pin或 production composition root、Platform-owned Workload Principal、Workload Grant、SPIFFE/WAT、Human API Key、跨 Tenant-owned Workload Principal、兼容 ServicePrincipal endpoint。

**Acceptance:** target fixture中 API Key认证/Authority/Membership invariants满足spec；Tenant-owned Workload canonical name按exact Tenant唯一、immutable且disabled不释放。Create与PrepareReplacement使用`ledger_one_time_secret`：首个OK response=`issued`且Secret仅一次；same-key/same-payload为OK `already_delivered(credential_id,recovery_action)`且Secret字段为空；different payload conflict；逻辑过期key返回409 `IDEMPOTENCY_KEY_EXPIRED`。Ledger/DB/log/Audit/evidence无raw Secret；caller可凭returned ID revoke unknown Key并用新key重试。Prepare不改变old Key；只有其后独立`ledger_replay` ActivateReplacement才原子激活new/revoke-old/Audit，丢失prepare response绝不锁死caller。Remove Membership原子disable+revoke all Keys；re-enable不复活。Gateway typed audit/last-admin与active-artifact green gates成立，无Service alias。

**Verification:** DP2-10适用matrix、two-Tenant/Membership/creator/disable/remove/re-enable；Create/Prepare issued-vs-already-delivered OK oneof、Secret empty、different conflict、expired-key-409、unknown-ID revoke recovery、prepare response loss leaves old active、Activate first/replay/conflict/expired/atomic old-new transition、UoW rollback；raw Secret全存储/log/evidence scan。TLS-pre-app vs app-visible audit、invalid Key context、real PG/Redis/target client；active hashes相同，现行与 target-only test/race/vet/registry pins绿。Envoy E2E=`not_verified`。

**Stop conditions:** 需要 permission snapshot、Platform Membership、Service alias 或 legacy fallback 才能兼容调用方。

**Recovery:** 停止target-only fixture，移除本票non-active candidate code并丢弃/重建本票专用disposable DB；不得逐行删除Audit/ledger、修改active artifacts或触碰共享数据，完整Secret不进入evidence。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/04-deliver-tenant-workload-api-key/`
