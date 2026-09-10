# 07: 交付带 Workload auth 的 Core Lifecycle / NATS

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-23 真实 Core lifecycle/bootstrap/NATS。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 校验并原子采用 WR-02 frozen Core candidate contract，在新 Workload baseline 上交付 Core Lifecycle projection、Bootstrap、Snapshot、Outbox/NATS、gap repair、heartbeat、DLQ/replay，并让同步调用与 worker 都有可归因 Workload Principal；IAM 与 Core 在事项边界分别全量 green。

**Blocked by:** 06 / frozen contracts已被IAM/ANI/Session采用，signed-plan Workload provisioning与共享 Workload runtime已闭合；Invitation/第二管理员/recovery仍留WR-10

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-07 / replaces DP2-12

**Scope:** WR-02 Core candidate digest/adoption manifest、pre-adoption Core Proto/generated/import/pin/runtime artifact digests；原样采用 candidate并替换active Core contract/pins；真实WR-06 HTTP adapter；单独人工批准、signed/digest-pinned WR-07 PlatformProvisioningPlan，经唯一apply-plan operation的stage/activate/revoke phases创建Core/NATS Principals/bindings/Grants/registrations且seed digest不变；broker owner stage/activate exact Credential/ACL/config revision；exact-one producer owner和0..N keyed consumer registrations；IAM进程内 broker-authenticated lifecycle-executor Workload、其独立exact Core-audience Snapshot Platform Grant、BrokerExecutor/RegisteredProducer audit contexts；Principal-revoke takeover用独立job/composition root、Kubernetes ServiceAccount→SPIRE selector/SVID及预注册recovery+Snapshot Grants，普通consumer不能读其Credential；Tenant/global execution Grants、registered-message evaluation、revision rollout/drain/recovery、TenantExecutionScope、gap/snapshot/bootstrap/outbox/heartbeat/DLQ/replay。

**Non-goals:** anonymous/shared broker Credential或shared authoritative subject、payload producer身份、external message executor/network IAM callback、synchronous Core fallback、generic IAM outbox、production NATS HA、未经新ADR的multi-producer subject/signed envelope。Producer/consumer仅在安全职责与撤销生命周期完全相同时可复用Principal，但必须分开 Binding/Grant/role contexts。

**Required human input before claim:** 冻结 Core/IAM/NATS immutable commit/tree、WR-02 Core candidate digest、Core pre-adoption contract/generated/import/pin/runtime artifacts与原子adoption/recovery manifest；接受WR-07 PlatformProvisioningPlan apply+recovery phase IDs/digests/signatures与non-escalation ceiling、literal subjects、producer/consumer identities+exact Grants、broker references/ACL revisions、各owner writer/transaction boundary、stage/activate/drain/rollback manifest、revoke/replay/recovery语义和Allowed paths；确认broker provenance能力及每个IAM/broker/Core recovery target归属。

**Acceptance:** Core active descriptor/generated/import/pins exact-match WR-02 candidate digest，IAM pin不漂移，Core与IAM各自full build/test green；本票不得修改candidate contract，缺口必须重开WR-02。Provisioning Workload只提交approved plan identity/digest/phase；IAM从signed只读catalog取得exact targets并执行non-escalating obligation，request不能自报actions。Stage/activate/revoke各使用`ledger_replay`并通过replay/conflict/expired-key/UoW+Audit；activate前必须有broker owner的exact ACL/config evidence。Core message `operation_id` dedupe属于Core/IAM各自业务状态机而非IAM ledger。Seed rows/digest不变。每subject/revision恰一producer owner；0..N consumer registrations各pin exact broker Binding/subscription/execution Grant。首期executor为IAM进程内broker-authenticated Workload；正常gap repair/full rebuild沿用该Principal和Binding，但必须用独立Snapshot Grant调用`safe_read+direct+Platform` Core operation，Tenant/lifecycle/version/cursor只作typed read obligation，不携key、message/delegation/Fixed-Tenant或TenantExecutionScope。Message Grant不能隐式派生Snapshot Authority。若executor Principal撤销，普通进程停消费且不能用其身份repair；只有独立recovery job凭自己的ServiceAccount/SVID、recovery+Snapshot Grants接管，consumer容器无法读取该Credential。WAT按canonical SPIFFE identity+Binding version验证而非leaf-cert thumbprint。Producer/executor audit roles分列；consume/replay实时重查，依赖失败pause/no-ack/no-cache/no-DLQ；revoke后的lifecycle/bootstrap/heartbeat与consumer recovery按spec分型；revision mismatch fail closed，旧revision留到验证后drain/revoke；无fallback/impersonation。

**Verification:** Core before/after artifact hashes、candidate/pin equality、intentional-breaking、Core与IAM full build/test；approved-plan/phase/ACL/seed negatives。Real NATS/PG/process覆盖duplicate/order/gap/snapshot/bootstrap/heartbeat/DLQ/replay和producer/consumer/audit/revision failures。Snapshot matrix逐项证明`safe_read+direct+Platform`、同lifecycle-executor Principal但exact独立Snapshot Grant；subscription/execution Grant、registered message、TenantExecutionScope、Delegated Subject、Fixed-Tenant Grant或idempotency key全部拒绝。SVID测试证明同canonical identity cert rotation可保留cache而Binding version变化miss。启动独立recovery job验证自己的ServiceAccount selector/SVID/Grants能在executor Principal revoke后接管；文件权限、mount和process inspection证明consumer进程不能读取recovery Credential，recovery revoke则停机告警。覆盖IAM unavailable pause/no-ack、三类message revoke recovery、stage/activate/drain/revoke、cross-account/bridge/fallback、Core operation-id dedupe/outbox transaction、race/vet/security。

**Stop conditions:** WR-02 Core candidate、baseline/adoption/recovery artifact或owner recovery manifest缺失/漂移；contract缺口需要本票偷改；provisioning需generic CRUD/自改/越ceiling；broker无法证明provenance或revision mismatch无法暂停/恢复；需要anonymous/shared Credential、multi-producer subject、external executor、cross-project DB write/dual write或同步fallback。

**Recovery:** 保留旧revision直到新revision全验收。失败时先停新producer/consumer；broker infra owner禁用新Credential/ACL/consumer并恢复仍保留的旧revision；provisioning Workload仅apply原计划内预先批准的recovery phase disable/revoke本票registrations/Bindings/Grants（保留Audit/ledger，不删），不得临时生成plan或扩权；Core owner只回退/清理其隔离outbox/business state，IAM owner只处理IAM state，禁止任一job跨库写。随后Core owner按固定pre-adoption manifest原子恢复Core Proto/generated/import/pins/runtime artifacts并跑full build/test；外部Credential由其owner撤销或恢复并验证旧路径。不得用git reset、跨源覆盖或清理shared NATS/Tenant state。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/07-deliver-core-lifecycle-workload-auth/`
