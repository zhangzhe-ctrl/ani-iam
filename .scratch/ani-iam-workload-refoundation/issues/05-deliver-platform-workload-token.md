# 05: 交付 Platform-owned Workload、SPIFFE 与 WAT

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-18 正式 runtime；WR-19 Platform Workload/可信引导；WR-22 首管理员与管理安全；WR-28/29 后期恢复。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在 IAM 仓库中原子采用 WR-02 frozen candidate，并将 WR-03/04 target seam 与 Platform-owned Workload Registry、SPIFFE Binding、Grant、canonical-SPIFFE-identity-bound WAT、receipt/message evaluators和真实 runtime/config/server wiring 一次接入 active composition root，使 IAM 在事项边界保持全量 green；外部 consumers 延后到 WR-06。

**Blocked by:** 04 / 在 WR-03 persistence 上完成 target-only Tenant-owned Workload/API Key slice

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-05 / replaces DP2-11

**Scope:** 校验并原样采用WR-02 candidate IAM digest；移除active Service；接入WR-03/04；交付SPIFFE/Grant/WAT/receipt/message/Audit/deep modules、composition/config/TLS；验证external LineageRegistry/BootAttestation后以Phase0+Finalize audited Seed建立startup、immutable Role/Policy、Provisioner与职责分离LineageReporter；在disposable initial target用独立Begin/callback harness完成PG-authoritative first-admin flow、Human final UoW与ReadyClaim/outbox→Reporter→ReadyReceipt；在独立disposable lineage先完成完整模拟PONR+CommitPONR，再在recovery target验证committed bit/current matching completion receipt/RequireForwardRecovery三重前置、唯一`RecoveryActivationPayloadV1`、Restore ReadyClaim/outbox/receipt。WR-05只闭合首位Platform Human，正式Invitation/第二admin/长期DR留WR-10；外部consumers延后WR-06。

Active IAM gRPC boundary必须接入WR-02生成的per-method forbidden-legacy-wire scanner和security-enum numeric validator；scanner只检查该方法列明的旧unknown field numbers，不开启全局unknown-field strict mode。

**Non-goals:** 修改WR-02 candidate contract或ANI/Session active contracts/pins、Inference/Envoy/Gateway/Session Gateway production consumer、正式Platform Invitation/第二管理员/双人recovery、API Key internal fallback、Human Session/Refresh、public JWKS、offline verifier（除非另行接受）、runtime/network self-registration、anonymous/insecure/static credential mode、其他服务fan-out、临时client library。

**Required human input before claim:** 接受WR-02/03/04 digests与adoption manifest、pre-adoption artifact/rebuild recipe、target paths/trust domain/identities/receiver；接受disposable LineageRegistry唯一owner/system/API-or-WORM paths、LineageAuthorityPolicy root/key owners/threshold/algorithms/TTL/key-registry、完整operations/records/ReadyClaim/Receipt/LineageReporter identity，以及initial/recovery BootAttestations、完整PONR terminal receipts、CommitPONR completion receipt、RequireForwardRecovery/fence fixtures。另接受Seed/Role/DB role、ProvisioningPolicy/catalog+ceilings、first-admin PG flow-key custody/version/rotation、ProviderAssurancePolicy、Begin allowlist/throttle/callback、raw capability custody/hash、exact first-admin target与disposable recovery package/plans及全部lineage/mode/domain/target bindings。

**Acceptance:** active IAM descriptor/registry exact-match WR-02 digest，Service surface zero-reference且全量build/test green。WAT mint提交exact active Grant ID；missing/wrong/overlap无自动选择/union，WAT≤5m并固定anchors/boundary/canonical SPIFFE ID+Binding ID/version；receiver exact-match mTLS identity/current Binding/Principal，不使用leaf-cert thumbprint/`cnf`。同identity/Binding-version SVID rotation可复用剩余TTL cache，任何Binding/identity/target/boundary变化miss；WAT无Session/Refresh/token/ledger row，retry fresh`jti`。Receipt/message/audit/no-fallback满足spec。

Lineage fixture严格实现WR-02状态机与atomic signed outputs。Seed Phase0创建pending row+Seed Audit同PG UoW，Finalize seed rows/finalized+另一Seed Audit同UoW；same retry不重复。Initial只允许ceremony，forward只允许Restore，两者允许Workload plans。First-admin final UoW写Human state/Audit/`AdministrationReadyClaimV1`+outbox；seed-pinned LineageReporter只读投递，registry same-operation重读`AdministrationReadyReceiptV1`并转active。Disposable Restore同理。CommitPONR本身不开放ReserveRecoveryBoot；只有`legacy_cutover_committed=true`、current matching CommitPONR completion receipt与target-fence全部成立，RequireForwardRecovery才可进入forward state。Cross-lineage/mode/target/domain/request self-report拒绝，Role tamper fail closed。

Begin/callback harness使用不同Principal/Binding/Grant。First-admin flow唯一owner是PostgreSQL：Begin CAS创建flow+Audit；row只存state digest、外部flow-key version、TTL/status，PKCE verifier派生，raw capability/state/verifier/key不持久化。Complete claim、terminal failure、最终success分别与Audit同PG UoW，Provider exchange不伪装同事务；replacement同UoW cancel旧ceremony/flow。Actor矩阵：TLS/canonical identity/Binding尚未解析出direct Principal时才Unauth；requesting Workload Principal及认证anchor一旦resolved，Begin成功以及后续WAT/Grant/operation/capability/state/PKCE/provider/target allow或deny均以该Workload为actor；final success才以同UoW新建Human为actor并保留callback Workload context。最终UoW同时consume、写ReadyClaim/outbox并保证active与login-capable admin各至少一人。Response loss/claim crash从PG state+TTL恢复；Complete不发Session/Token，最后禁用harness。

Disposable forward-recovery target只接受同时绑定current matching CommitPONR completion receipt与RequireForwardRecovery transition的唯一`RecoveryActivationPayloadV1`，恢复non-credential admin state/consumed marker/Audit/ReadyClaim/outbox同UoW；Reporter取得ReadyReceipt才回active。Pre-PONR、committed bit=false、missing/stale/wrong commit receipt、仅commit无Require、真实PONR/cross-boundary/legacy/Credential/zero-admin/zero-login-capable/role expansion/request payload失败。Invitation/第二admin/长期DR保持`not_verified`。所有IAM mutation与Audit同PG UoW，不声称Provider或external registry跨store事务；无boot/plan、自改seed/policy/Role、超ceiling或request-defined action失败。

**Verification:** adoption hashes/zero-Service/full IAM；real mTLS/SPIFFE/PG/OIDC/harness/receiver/message，WAT/receipt/matrix。Seed覆盖Phase0+Audit与Finalize+Audit rollback/replay。Lineage覆盖ReadyClaim/outbox/Reporter auth+lost-response/ReadyReceipt，CommitPONR不开放recovery，以及committed bit+matching commit receipt+fence三重gate RequireForwardRecovery和single target；bit=false或错误receipt即使fence/package有效也拒绝。Role catalog无writer/tamper fail closed。

First-admin覆盖disjoint transports、Begin actor、capability/redaction、PG唯一flow owner、state digest/flow-key version、PKCE派生、每transition+Audit、claim crash/TTL、same-cap fresh Begin、replacement竞态、assurance/PKCE/auth-time、final consume+Human+Audit+ReadyClaim/outbox、active/login-capable invariants与response loss。PlatformAdministrationGuard覆盖Identity/email/provider policy降零。Disposable Restore逐字段覆盖唯一`RecoveryActivationPayloadV1`、current matching CommitPONR receipt、RequireForwardRecovery、ReadyClaim/Receipt、pre-PONR/cross-boundary/legacy/Credential/zero-admin negatives；repo只留脱敏metadata。恢复旧artifacts跑旧gates；Invitation/second-admin/真实Gateway callback/长期DR/Inference E2E为`not_verified`。

用旧descriptor client向active IAM gRPC server发送真实binary wire bytes，逐方法证明旧`idempotency_key` field number被`ProtoReflect().GetUnknown()`/protowire gate以stable ErrorInfo拒绝、未列unknown field仍按protobuf evolution规则处理；旧numeric `PrincipalType.SERVICE=2`、Human `Audience.INTERNAL=3`、`AuthnMethod.SERVICE_TOKEN=4`在active boundary同样稳定拒绝。`IssueServiceToken`旧full method不可路由，replacement request不复用field 4。

**Stop conditions:** WR-02 contract/non-escalation有缺口；缺external lineage owner/system/current record/atomic signed results/BootAttestation/ReadyClaim+Receipt/LineageReporter、committed bit/current matching CommitPONR receipt/RequireForwardRecovery fence、职责分离ceremony transport、PG flow-key custody、capability custody或real OIDC；需要empty-DB-as-initial、pre-PONR Restore、second target、shared Secret、DNS/Header/API Key、generic CRUD/request-defined action/seed bypass/test-only constructor或未授权外仓路径。

**Recovery:** 停止隔离进程；先仅以预先接受的 disposable recovery plan phase禁用本票 target，失败则不临时扩权或直写。按固定 pre-adoption manifest原子恢复 IAM code/Proto/generated/migration/checksum/sqlc/composition/config，discard仅本票 target隔离 DB并按固定 recipe重建旧-schema测试 DB，再跑旧 clean/full gates。由各外部 owner撤销仅本票创建的 exact SPIRE/test resources；不得用 git reset、跨源覆盖、回滚共享 DB或删除非本票资源。若无法证明完整恢复，保持停止且不进入 WR-06。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/05-deliver-platform-workload-token/`
