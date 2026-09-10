# 13: 重建测试数据并最终测试切换

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-28 实际切换；WR-29 精确资产/数据退役；隔离空库安装提前至 WR-18/25。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 经精确人工批准后，由IAM、Core、SPIRE/broker与deployment各自owner按同一immutable manifest重建测试状态，先证明新target，再在显式PONR失效legacy Credential/trust并整组切换。正常cutover完成后current target保持administration-active，绝不隐式创建第二authority surface；只有另经target-fence确认的failure branch才允许forward-only recovery。

**Blocked by:** 12 / 完成全调用方、UI 与 S2S E2E

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-13 / replaces DP2-18

**Scope:** exact test datasets/credentials/artifacts；owner snapshots/guard/source vector；外部双owner托管的cutover-window `PlatformAdministrationRecoveryPackage`与signed catalog；LineageRegistry recovery-prepared/ponr-in-progress/CommitPONR completion+legacy bit+current receipt/administration-active、RetireCutoverRecoveryPackage，以及独立committed-bit+matching-commit-receipt+target-fence-gated RequireForwardRecovery/ReserveRecoveryBoot/ReadyReceipt；exact invalidation terminal-receipt manifest；唯一`RecoveryActivationPayloadV1`；seed target/Restore/ReadyClaim+outbox+LineageReporter；IAM/Core/SPIRE-broker/deployment owner operations、traffic、post-cut与隔离failure-branch smoke。

**Non-goals:** production、legacy deletion、Service→Workload row migration、跨owner/跨库writer或dual write、Seed通用fixture、恢复旧Credential、未经批准重发Credential、正常cutover时预先reserve第二target，以及cutover-window package退役后的持续备份/长期DR保证（保持`not_verified`）。

**Required human input before claim:** 精确IAM/Core DB/owner/namespace、legacy/target Credential/trust IDs、traffic selector/snapshots；protected package的双owner custody/encryption/ACL/audit及exact non-credential admin/source vector；signed catalog/plans/threshold；real LineageRegistry owner/system/API-or-WORM paths、lineage/current record、LineageAuthorityPolicy root/key registry、PrepareRecovery和每个legacy invalidation的operation/terminal-receipt set、CommitPONR completion/retire rules。执行前须即时确认exact invalidation。若演练或实际进入failure branch，另须接受`legacy_cutover_committed=true`、current matching CommitPONR completion receipt、current-target quiesce/loss与exact fence receipts、双owner RequireForwardRecovery operation、fresh target BootAttestation、唯一RecoveryActivationPayloadV1 digest/expiry；正常cutover或pre-PONR target loss不得复用该授权。

**Acceptance:** 各owner只写自身状态；跨owner只用digest-pinned manifest/receipt。新target先在隔离lane完成positive smoke，legacy PONR前可回退。统一guard覆盖全部package-contributing mutable writers（Principal/Human profile/verified email/OIDC Identity/Platform Membership/Role Binding/ceremony）并维持active+login-capable admin；Role catalog无runtime writer但exact-read/tamper fail。Guard下记录source vector并生成protected、无Credential且至少含一名login-capable admin的package；不同owner双签、外部加密托管并完成readability rehearsal，repo只留脱敏metadata。

External owner以accepted package/consumed-lineage/cutover manifest/source vector调用PrepareRecovery进入recovery-prepared。Guard final exact-match后取得即时确认，各owner按固定operation执行legacy invalidation；第一份不可逆terminal success进入ponr-in-progress，partial/ambiguous只按operation查询/前向补齐，不能rollback。全部manifest-required receipts齐全后`CommitPONR`原子设置monotonic `legacy_cutover_committed=true`、签completion receipt并把phase恢复administration-active；它不进入forward-recovery-required且ReserveRecoveryBoot仍拒绝。随后deployment切流并跑post-cut smoke；current target正常时调用RetireCutoverRecoveryPackage并证明该package不可再激活。持续长期DR保持`not_verified`。

Forward recovery仅走独立failure branch：先验证`legacy_cutover_committed=true`及current lineage/generation/manifest matching CommitPONR completion receipt，再由deployment/security owners确证current target quiesced/lost并提交exact fence receipts；双owner `RequireForwardRecovery`验证上述commit proof、current unexpired package/source vector后才原子推进fresh recovery generation，把commit receipt/predecessor/current-target fence写入signed transition并进入forward-recovery-required。Fresh target Phase0 row+Seed Audit同PG UoW，external owner签BootAttestation，Finalize seed+Audit同另一UoW；然后应用target-bound Workload transport plan。Activation只接受WR-02唯一`RecoveryActivationPayloadV1`，本票不复制字段清单；它必须同时绑定current matching CommitPONR completion receipt与RequireForwardRecovery transition。Restore request只提交受保护catalog identity，UoW原子恢复allowlisted non-credential state、consumed marker、activation/phase、Audit、`AdministrationReadyClaimV1`+outbox，且active/login-capable admin均至少一人。LineageReporter投递，registry取得同一`AdministrationReadyReceiptV1`后才回administration-active；随后fresh BOSS login和其他owner恢复。不得重跑first-admin。Pre-PONR target loss保留legacy且只能通过另行接受的全新lineage/initial-install重建隔离target。

**Verification:** 审计owner snapshot/manifest/writer；模拟target loss仍可双custodian decrypt/digest-match，repo PII/raw scan零。逐项注入Human/profile/email/OIDC/Membership/Binding/ceremony writes及Role tamper，验证guard/vector/package、active/login-capable invariants和无commit窗口。验证distinct owner threshold、zero-admin/zero-login-capable/partial/over-ceiling/legacy/Credential拒绝。

Lineage覆盖PrepareRecovery、zero-receipt abort、第一receipt进入ponr-in-progress、partial/response-loss、全部receipt后CommitPONR回active+bit+current completion receipt、normal path不能ReserveRecoveryBoot、post-smoke package retire。独立failure branch覆盖committed bit=false、missing/stale/wrong-lineage/generation/manifest commit receipt或missing/stale/wrong target fence时Require拒绝（即使package/fence其余有效）、全部proof成立且current target确实quiesced后Require成功、single recovery target、Phase0/Finalize Audits、唯一RecoveryActivationPayloadV1逐字段negative、Restore UoW+ReadyClaim/outbox、LineageReporter lost response与ReadyReceipt。正常cutover和failure branch分用独立lineage/artifacts；long-term DR明确`not_verified`。

**Stop conditions:** 精确target/owner/writer/UoW/recovery无法证明；lineage owner/root/key/current record、invalidation terminal receipts或CommitPONR input/current completion receipt缺失；normal cutover或pre-PONR target loss要求直接进入forward state/预留第二target；RequireForwardRecovery缺committed bit、matching commit receipt、current-target fence或双owner批准；package/forward rehearsal不足；即时invalidation确认缺失；需要empty-DB-as-initial、reset lineage、cross-DB/dual-write/Seed bypass、restore旧Credential或触及production。

**Recovery:** Zero invalidation receipt前保持guard或按manifest解除并恢复snapshot；任何source drift废弃package。第一success receipt后禁止legacy rollback，只按operation补齐并CommitPONR。Commit后current target正常则完成切流、smoke并retire cutover package；target未被确证fenced时绝不ReserveRecoveryBoot。只有committed bit、current matching CommitPONR receipt和独立RequireForwardRecovery全部成功后，才按fresh target Phase0+Audit→BootAttestation→Finalize+Audit→transport plan→唯一activation→Restore+Audit+ReadyClaim/outbox→ReadyReceipt→fresh login前向恢复。Pre-PONR target loss保留legacy且不走Restore。Target-era key rotation遵守ADR-0012；长期DR仍`not_verified`且不得据此进入删除。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/13-rebuild-test-data-and-cutover/`
