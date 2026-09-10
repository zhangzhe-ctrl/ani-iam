# 06: 统一服务间 Workload Invocation

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-19 Gateway→Session 首链；WR-21 Envoy；WR-24 必需后台调用方。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 校验并原子采用 WR-02 frozen ANI/Session candidate contracts，发布 protocol-neutral Workload Invocation module/adapters，并在正式注册所需 Workloads/Bindings/Grants 后以 Gateway→Session、Gateway→Inference 和受控 HTTP fixture 建立 `mTLS + canonical-SPIFFE-identity-bound WAT` 参考链；每个受影响仓库在事项边界全量 green。

**Blocked by:** 05 / IAM 已原子采用 WR-02/03/04 candidate，闭合 signed-plan Workload provisioning 与首位 Platform Human bootstrap；Invitation/第二管理员/recovery仍留WR-10

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-06 / universal Workload Invocation

**Scope:** WR-02 ANI/Session candidate digest/adoption manifests与 pre-adoption artifacts；原样替换 active Proto/generated/import/pins；versioned shared module、auth core、SVID rotation、Token/receipt carriers/cache、generated target registry、stream lifecycle、gRPC/HTTP adapters、IAM online/in-process paths、typed Invocation、Envoy IAM client、Gateway demo/plaintext removal、Inference context、HTTP fixture；provisioning Workload仅apply本票人工接受、signed/digest-pinned的 exact PlatformProvisioningPlan stage/activate/recovery phases，创建Gateway/Session/Inference/test receiver Principals/Bindings/Grants且不改seed digest；外部SPIRE owner按同一frozen IDs配置entries/trust并为activate提供exact evidence。

Gateway/OpenAPI boundary必须采用WR-02 forbidden-carrier registry：对非ledger endpoint拒绝HTTP JSON `idempotency_key`及任何大小写规范化后的`Idempotency-Key` header，生成与gRPC相同的stable ErrorInfo/HTTP mapping；不得在JSON decoder、proxy或adapter层静默丢弃。Shared gRPC adapters同样保留per-method exact old-wire scanner，不开启全局unknown-field strict mode。

**Non-goals:** 修改 WR-02 candidate、临时 dual protocol、业务 Handler解析身份、NetworkPolicy充当authn、上游 Credential/receipt原样转发、IAM网络自递归、全服务fan-out或 WR-07前改真实 Core Snapshot consumer。Receiver仅可用自身channel提交 opaque evidence给 IAM，下一跳只带 attenuated child。

**Required human input before claim:** 精确接受 WR-02 ANI/Session candidate digests、各仓 immutable commit/tree与 pre-adoption code/contract/generated/pin artifact digests、adoption manifest/module version、Allowed paths、隔离 IAM/process资源、本票 stage+activate+recovery PlatformProvisioningPlan IDs/revisions/digests/signatures，以及外部SPIRE owner要创建/撤销的exact entries/trust inputs和activation evidence；不得由WR-05推导外仓写权限。

**Acceptance:** ANI/Session active artifacts exact-match candidate digests，IAM pin不漂移，三个仓库各自full build/test green且参考链real-process green。Provisioning Workload只提交accepted plan identity/digest/phase；stage从只读catalog创建exact inactive Principals/Bindings/Grants，外部SPIRE owner再配置exact entries/trust，activate只有验证同revision evidence后才使IAM rows可用；各phase为`ledger_replay`并满足replay/conflict/expired-key与Audit/UoW，seed-owned rows/digest不变。Generic CRUD、self/seed/policy mutation、授予apply、超plan/ceiling、request字段注入、evidence前activate与revision mismatch失败。每跳使用caller自己的required-Grant-ID WAT；receiver从mTLS解析canonical SPIFFE ID并exact-matchToken claim、Binding ID/version和current Principal，不使用leaf-cert thumbprint/`cnf`。同identity/Binding-version SVID rotation允许剩余TTL cache，Binding/identity/target/boundary变化miss。Receipt、stream、no-forwarding/no-fallback成立。TLS前失败只log/metric，application-visible failure写typed unauth actor；认证transport/carried subject各有matching contexts。Active gRPC/HTTP boundaries稳定拒绝WR-02列明的旧binary/enum/JSON/header carriers；Adapters无insecure/static/dev fallback；health exceptions exact。

**Verification:** before/after artifact hashes与candidate/pin equality；IAM/ANI/Session full build/test + shared module contract tests；plan/seed/non-escalation negatives；real SPIRE entries/trust + real IAM gRPC/HTTP E2E。覆盖不同canonical identity replay拒绝、同identity cert rotation cache保留、Binding rotation/revoke cache miss、WAT/receipt/tenant/attenuation、anchors、Audit contexts、Scope×Evidence、stream、SVID drain、IAM failure/recursion。用old-descriptor binary client走active gRPC adapters，并用HTTP JSON/body及header client走Gateway/OpenAPI，验证每个nonledger endpoint的旧field/header被稳定拒绝而非忽略、ledger endpoint仍只接受contract carrier、gRPC/HTTP ErrorInfo parity、未列unknown protobuf field不被全局拒绝；覆盖三个旧numeric enum。Secret/race/vet/static通过。

**Stop conditions:** candidate digest、contract或 non-escalation obligation有缺口（停止并经人工批准重开 WR-02/reissue all pins）；baseline/adoption/recovery plan/artifacts或外部 SPIRE inputs未确认；需要双协议、业务自实现auth、generic provisioning CRUD、seed/direct-DB registration或未授权路径。

**Recovery:** 先停止隔离 clients；由外部 SPIRE owner撤销本票 exact entries，由 provisioning Workload仅apply预先接受的 recovery plan phase disable/revoke本票 IAM Bindings/Grants/Principals（保留 Audit/ledger），不得临时生成plan、扩权或直写 DB；随后按已固定 pre-adoption artifacts原子恢复 ANI/Session contract/generated/import/pins并跑各仓 full build/test。共享 module版本只标记未采用/撤回隔离引用，不覆盖已发布内容；不触碰 shared traffic/deploy selector。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/06-secure-service-to-service-grpc/`
