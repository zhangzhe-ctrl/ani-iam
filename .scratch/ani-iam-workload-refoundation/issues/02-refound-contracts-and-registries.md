# 02: 重冻 Human / Workload contracts 与 registries

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-17 契约/输入；WR-18 必需 schema/runtime；WR-19/23 真实边界；WR-28/29 后期恢复门禁。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在不替换任一仓库 active contract/import/pin 的前提下，一次性生成并冻结 IAM、ANI、Core 与 Session Gateway 全部目标 Principal、Workload、WAT、Invocation、admin/recovery、message、audience、operation 和错误契约的非 active candidate bundle，形成后续纵向事项只能原子采用的不可变 descriptor/registry/policy baseline。

**Blocked by:** 01 / 冻结 Human / Workload 统一主体架构

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-02 / contract and registry refoundation

**Scope:** 隔离candidate source/generated/descriptor/OpenAPI/registries/error-catalog/digest bundle；Human/Workload、owner admin/recovery、required-Grant-ID WAT、Identity/Grant/Evidence/Invocation/stream/seed/broker/NATS contracts；WAT canonical SPIFFE identity+Binding-version claim及rotation/cache规则；Core Snapshot的固定registry/Grant/recovery obligations；external `PlatformAdministrationLineageRegistry`完整phase/operations/atomic signed results、`PlatformAdministrationBootAttestation`、`AdministrationReadyClaimV1`/`AdministrationReadyReceiptV1`、LineageReporter exact adapter、PONR terminal receipts与cutover-complete/forward-recovery分离；immutable Role/Provisioning policies与target-bound plans；first-admin ceremony及PostgreSQL authoritative flow、application Begin/Complete；唯一`RecoveryActivationPayloadV1`；Principal/Unauth/Seed actor、first-admin/registered-message专用actor矩阵、逐stage outcomes；100% RetrySemantics、typed outcomes、legacy wire/security-enum reject registries、stable errors与candidate pins。

**Non-goals:** 修改任何仓库当前 active Proto/generated/import/pin、persistence/runtime 实现、数据库重建、SPIRE entry、部署、兼容 alias/dual protocol、修改历史 CP0/P1 evidence。Candidate 不是第二个可运行协议，也不能注册到 production composition root。

**Required human input before claim:** 接受receiver verification与完整Grant Scope × Evidence Mode matrix（含Platform+BOSS receipt、Delegated Tenant receipt、global/Tenant messages，未列组合无fallback且receipt在线）；冻结SPIFFE trust domain/canonical IDs、三个仓库commit/tree、breaking version与Allowed paths；接受external lineage authority唯一owner/system/API-or-WORM paths、LineageAuthorityPolicy offline root/key registry/不同owner threshold/algorithms/TTL、完整phase+operation/atomic signed-result状态机、ReadyClaim/Receipt+LineageReporter contract、PONR receipt set，以及`legacy_cutover_committed=true`+current matching CommitPONR completion receipt+target fence共同gate RequireForwardRecovery的initial/cutover/forward-recovery single-target互斥语义。

**Acceptance:** candidate contracts满足spec。Workload canonical name为`lower(btrim)`、non-empty、immutable且owner-scope unique，disabled不释放。WAT peer claim固定canonical SPIFFE ID+Binding ID/version，receiver与mTLS解析结果及current active Binding/Principal exact-match；不使用leaf-cert/public-key thumbprint或RFC8705 `cnf`，同identity/Binding-version rotation允许剩余TTL cache，任何identity/version/target/boundary变化强制remint/miss。

Target-init contract先以operator operation ID+manifest digest幂等创建`pending_boot`基础字段/challenge，并要求row+独立Seed-actor Audit同PG UoW；same payload重试返回同一row且不重复Audit，different冲突。`PlatformAdministrationBootAttestation` canonical绑定external lineage/record/mode/domain/pending target/seed-policy和mode-specific predecessor/current matching `CommitPONR` completion receipt/`RequireForwardRecovery` transition/package/consumed-lineage anchors。Finalize exact验证后在另一PG UoW填boot fields、seed rows、Seed Audit并terminalize`finalized`；same attestation retry no-op且不重复Audit，different conflict，listener只认finalized。Plan exact-match current target；boot-mode只互斥Human bootstrap，initial允许ceremony、forward只允许Restore，两者均允许Workload transport。Provisioner只提交plan identity/phase/key，action来自catalog；generic CRUD/self-escalation/request injection/无external evidence拒绝。

LineageRegistry candidate contract固定append-only events、monotonic generation、non-resettable `initial_install_consumed`和`legacy_cutover_committed` bits、current CommitPONR completion receipt ref+digest及phase `uninitialized | initial_boot_reserved | administration_active | recovery_prepared | ponr_in_progress | forward_recovery_required | recovery_boot_reserved`。ReserveInitialBoot/ReserveRecoveryBoot原子保存唯一target reservation与signed result；后者只从forward-recovery-required合法。`AdministrationReadyClaimV1`、LineageReporter mTLS/SPIFFE exact adapter、`AdministrationReadyReceiptV1`、PrepareRecovery、第一invalidation receipt进入ponr-in-progress、全部receipt后的CommitPONR设置cutover bit/保存current matching completion receipt并回administration-active、committed bit+matching commit receipt+target-fence gated RequireForwardRecovery原子推进fresh recovery generation、pre-PONR abort、RetireCutoverRecoveryPackage与post-Restore ready都有canonical payload、legal from/to和operation replay；normal cutover不开放second target。Mutation与signed result不可分；initial bit不清除，长期DR package保持`not_verified`。

唯一Human create action只存在于ApplyPlan phase。First-admin plan固定singleton Role、职责分离Begin/callback Workloads、capability hash和OIDC anchors；raw capability不进plan/Git/DB/log/Audit。First-admin flow唯一authority owner是PostgreSQL：只存state digest、外部versioned flow-key ref、TTL/status，PKCE verifier派生，raw state/verifier/key不持久化。Begin create、Complete claim、terminal failure与最终success每个IAM-local transition均与Audit同PG UoW；Provider exchange不与PG伪原子。Same pending可在旧flow terminal/TTL后fresh Begin；replacement同UoW cancel旧ceremony/flow并换新ID/hash。Actor矩阵固定：TLS/canonical identity/Binding尚未解析出direct Principal时才Unauth；requesting Workload Principal及认证anchor一旦resolved，Begin成功和后续WAT/Grant/operation/capability/flow/provider/target allow或deny均归因该Workload；最终Complete成功才归因同UoW新建Human并保留callback Workload context。最终UoW建立Human/Identity/Authority、consume、Audit、`AdministrationReadyClaimV1`+outbox，active与login-capable admin均至少一人；response loss按terminal state恢复。Provisioner不能Begin/Complete或选target。

真实restore只引用WR-02唯一`RecoveryActivationPayloadV1` canonical schema，任何文档不得复制缩减字段清单；它必须同时绑定current matching `CommitPONR` completion receipt与`RequireForwardRecovery` transition，不能只凭其中之一。Recovery activation one-time terminal，initial-install、pre-PONR、committed bit=false、missing/stale/wrong commit receipt、disposable/real cross-use、legacy/Credential、zero-admin或zero-login-capable拒绝；Restore UoW同时写restored state、consume、Audit、`AdministrationReadyClaimV1`+outbox。Core Snapshot固定`safe_read+direct+Platform`及独立recovery identity。Audit固定Principal/Unauth/Seed、requesting/executing contexts与逐stage outcome；WR-07首期message executor只允许IAM in-process broker-authenticated path。

Retry registry覆盖100% endpoints且unknown generation fail：pure read含Snapshot=`safe_read`；ordinary state及ApplyPlan=`ledger_replay`；APIKey create/prepare=`ledger_one_time_secret`；PasswordLogin、OIDC login/link、first-admin begin/complete、RefreshSession、SwitchTenant=`owner_state_machine`；WAT/CheckPermission/receipt=`ephemeral_mint`。只有`ledger_*`允许并要求idempotency key。旧字段name+number reserved并由per-method forbidden-legacy-wire registry精确拒绝binary unknown field；HTTP JSON/header carriers同样拒绝而不全局拒绝其他unknown。IssueServiceToken全RPC/message及field 4进入breaking manifest且replacement不复用；旧numeric enum `SERVICE=2`、Human `INTERNAL=3`、`SERVICE_TOKEN=4`稳定拒绝。One-time-secret replay不返回Secret，expired ledger key为409，raw Secret零存储；其余WAT/receipt/evidence/NATS/stream/error/wire gates与active unchanged/full-green成立。

所有OIDC flow还必须冻结purpose/client/audience/callback绑定；first-admin额外绑定ceremony与plan revision/digest。Login、identity-link、first-admin三者任意cross-purpose callback以及任一client/audience/callback/ceremony/plan错配均为稳定typed拒绝，不能因共享Gateway callback而互换。

PlatformProvisioningPolicy/first-admin ceiling必须包含typed `ProviderAssurancePolicy` oneof并进入policy digest：`iam_directed_mfa`固定IAM Begin必须生成且caller不可覆盖的canonical request profile及预期returned-claim predicate；`external_client_enforced_mfa`固定immutable IdP client-policy evidence reference+digest及预期predicate；`accepted_no_mfa`固定独立risk-acceptance approval+digest。缺失、多选、unsupported、request override、evidence/config drift或只有claim检查而无可达MFA触发/外部强制均fail closed。Goldens覆盖三mode及所有negative。

**Verification:** candidate lint/generate/breaking/digests；Workload name/owner、WAT identity/Binding/rotation；LineageRegistry逐条legal/illegal transition、atomic stored result、same-response replay/different conflict、single target、Phase0/Finalize两次Seed Audit原子性、ReadyClaim schema/outbox、LineageReporter auth/retry、ReadyReceipt、CommitPONR回active且ReserveRecoveryBoot拒绝、committed bit=false或missing/stale/wrong-lineage/generation/manifest commit receipt时即使fence/package有效也拒绝RequireForwardRecovery、package retire与initial bit non-reset goldens。Boot/plan payload逐字段与cross-boundary negatives；first-admin PG flow state/key/version/TTL、每transition+Audit rollback、claim crash、Provider ambiguity、actor stage矩阵、replacement race、assurance/PKCE/fresh-auth、final ReadyClaim及lost response；PlatformAdministrationGuard覆盖active/login-capable admin和Identity/email/provider-policy writers。Recovery逐字段覆盖唯一`RecoveryActivationPayloadV1`、matching CommitPONR receipt、RequireForwardRecovery、ReadyClaim/outbox及PONR negatives。其余Snapshot/Retry/legacy wire/Audit/TLS/NATS/error goldens、active hashes/full green与Secret/PII scan通过。

**Stop conditions:** trust domain/canonical identities、receiver mode、delegation proof、baseline 或跨仓库 breaking 未获精确接受；任何消费者/真实数据与 WR-01 输入冲突。

**Recovery:** 仅移除/归档本事项 non-active candidate bundle并恢复其 evidence manifest；active contract/import/pins从未变化，不触碰数据、历史 evidence或外部环境。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/02-refound-contracts-and-registries/`

## Comments

- 2026-09-09：由旧 DP2-02/03 target artifacts 重开为新的 breaking baseline；旧已解决事项保持历史状态。
