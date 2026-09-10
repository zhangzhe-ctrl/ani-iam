# 03: 重建 Human / Workload persistence foundation

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-18 持久化/runtime；WR-19 可信引导；WR-22 管理安全；WR-28/29 后期恢复。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在不替换 active migration/sqlc/runtime 的 non-active target persistence seam 中，从空库构建只含 Human/Workload 的 no-RLS candidate schema、repositories、约束、24h Idempotency Ledger、UnitOfWork 与 transactional Audit foundation；由 WR-05 连同 contract/runtime 原子采用。

**Blocked by:** 02 / 重冻 Human / Workload contracts 与 registries

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-03 / clean schema and domain foundation

**Scope:** 隔离candidate migration/checksum/sqlc/repository/UoW bundle；Principals/profiles/Owner/name、Bindings/registrations、Grants/scopes/operations、API Key/Membership/Tenant constraints、Authority versions+anchor-vector query、24h retry ledger、immutable bootstrap Role/Provider policies、seed-owned target incarnation、Phase0/Finalize Audits、`platform_first_administrator_flows`唯一PG owner、ceremony、`AdministrationReadyClaimV1`/outbox、LineageReporter read port、完整`RecoveryActivationPayloadV1` activation terminal、applied-plan/restore UoW、restricted/seed roles、typed Audit actors/contexts/stages、PlatformAdministrationGuard及empty-DB invariants。

**Non-goals:** 修改 active migration/checksum/generated sqlc/repository/runtime/composition root、Service→Workload row migration、shared-data upgrade、Credential issuance/runtime、ANI/Session Gateway changes、deployment。Candidate schema 不能供 active old runtime 使用。

**Required human input before claim:** 精确接受 IAM migration/data Allowed paths、clean empty schema、隔离 PostgreSQL、Platform Trust Seed 持久状态与专用 DB role；不得把 schema 授权扩成真实环境重建授权。

**Acceptance:** candidate DB满足owner/authority/cardinality/audit/retry constraints。Canonical Workload name按owner scope唯一、immutable且disabled保留。每个Authority owner只在其relation mutation UoW推进自身object version并与Audit提交/回滚；authorization query产出exact-operation完整、稳定排序的`SourceAuthorityAnchorSet`vector/digest，无fan-out revision table；查询顺序变化digest不变，任一relevant anchor增删/revoke/version/status变化必改变。Broker subject-owner/consumer、Tenant-owned Workload cardinality与typed audit成立。Bootstrap platform Role rows seed-owned immutable且无runtime writer；startup/guard/package/restore exact-match digest。

Target incarnation采用两阶段状态机。Phase 0以operator operation ID+manifest digest在一个PG UoW幂等创建`pending_boot`基础字段和Seed-actor Audit；same key/payload返回同一challenge且不重复Audit，different冲突。Finalize在另一PG UoW验证external record/BootAttestation，填generation/record/mode/boot digest、seed rows、Seed Audit并置`finalized`；same retry no-op且不重复Audit，different冲突，listener只认finalized。DB constraint只对Human bootstrap互斥：initial ceremony、forward Restore；两mode均允许Workload plans。Provisioning apply+ledger+Audit原子。Recovery activation使用唯一`RecoveryActivationPayloadV1`并与restore state/phase/Audit/ReadyClaim/outbox同UoW consume。

Provisioning policy记录threshold/ceilings/ProviderAssurancePolicy。First-admin ceremony与`platform_first_administrator_flows`都在PG；后者是唯一flow owner，每ceremony至多一个active/exchange-claimed，存state digest、外部flow-key version、TTL/status，不存raw capability/state/verifier/key，PKCE verifier派生。Begin create、Complete claim、terminalize、final success分别与Audit同UoW；replacement同guard/UoW cancel旧ceremony/flow并建立新ID/hash。Provider exchange在事务外，但最终UoW重查current ceremony/Role并原子建立Human/profile/email/OIDC Identity/Membership/Role Binding、consume ceremony/flow、成功Audit、`AdministrationReadyClaimV1`+outbox；active与login-capable admin均至少一人。Code已消费而final UoW回滚时flow terminal、ceremony pending；response loss从PG terminal state判定。

Recovery activation row承载WR-02唯一`RecoveryActivationPayloadV1`完整canonical digest与可查询anchors，不在本票复制可漂移字段清单。只有`legacy_cutover_committed=true`、current matching CommitPONR completion receipt和已验证`RequireForwardRecovery` transition全部成立才可用；pre-PONR、bit=false、missing/stale/wrong commit receipt、仅CommitPONR而无Require、initial、stale key/transition、cross-boundary或field drift拒绝。Restore只从accepted package恢复allowlisted non-credential admin state和consumed marker，并在同一UoW写Audit、`AdministrationReadyClaimV1`+outbox；active与login-capable admin均至少一人，legacy/Credential/partial/超ceiling拒绝。Ledger不存raw Secret，candidate绿且active hashes不变。

**Verification:** 隔离DB执行checksum/empty replay/restricted role/two-Tenant/cardinality/anchor/Audit。Phase0 row+Seed Audit、Finalize seed+Audit各自rollback/replay；ready claim/outbox约束与LineageReporter read-only port。First-admin PG flow覆盖raw-secret zero、state digest/key version/rotation retention、Begin/claim/terminal/final每transition+Audit原子、response loss/lease/TTL、replacement race、Provider-code ambiguity、actor矩阵和final ReadyClaim。PlatformAdministrationGuard覆盖Human/Membership/Binding、OIDC Identity、verified-email/provider-policy并发，active或login-capable降零拒绝；外部IdP outage标`not_verified`。Recovery逐字段覆盖唯一`RecoveryActivationPayloadV1`、committed bit/current matching CommitPONR receipt/RequireForwardRecovery三重前置、activation/state/Audit/ReadyClaim/outbox同UoW及全部pre-PONR/cross-boundary negatives。其余ledger/sqlc/full target/race/vet/Secret scan通过，active artifacts hash不变。

**Stop conditions:** 需要对现有数据库做 ALTER/逐行迁移，或发现真实数据；contract digest 漂移；需要 RLS 或跨库 FK 才能证明隔离。

**Recovery:** 删除本事项创建的隔离数据库与 non-active candidate bundle；active schema/runtime从未切换，不得操作共享库。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/03-rebuild-human-workload-persistence/`
