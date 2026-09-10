# 12: 完成全调用方、UI 与 S2S E2E

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-24 后台 caller E2E；WR-26 旧代码裁剪；WR-27 前端；WR-28 系统替换。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 完成目标 Gateway/Envoy/Inference/Console/BOSS、Session Gateway 及其后续 S2S fan-out，形成删除前完整 E2E 和 zero-reference evidence。

**Blocked by:** 10 / Administration/Recovery；11 / Audit/Idempotency

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-12 / replaces DP2-17

**Scope:** complete UI/admin flows、all registered internal gRPC/HTTP clients/servers、per-hop identities/tokens、browser boundaries、full error/obligation/audit correlation，以及把isolated target lane/candidate artifacts与preserved legacy rollback lane明确分栏的legacy reference manifest。

**Non-goals:** shared cutover、shared active zero-reference、Credential invalidation、legacy deletion、Production Ready claim。

**Acceptance:** all target operations and caller classes independently pass；every business hop resolves direct Workload；delegated subject never authenticates caller。Isolated target lane中每个active gRPC/HTTP boundary均已实际采用forbidden-old-wire/numeric-enum/JSON/header carrier gates并稳定拒绝旧client；仅该lane及其candidate code/config/deploy/artifacts达到zero forbidden legacy references。为WR-09回退并一直保留到WR-13最终切换的legacy rollback lane及其固定artifacts必须在manifest中明确标识，但不计入本票zero-reference。shared active tree/runtime的zero-reference只在WR-13完成最终切换后、WR-14删除前另行建立，不由WR-12宣称。

**Verification:** complete operation/evidence matrix、real caller/process/dependency E2E、unary/stream expiry-revoke-rotation cancellation、old-connection drain、negative/failure/rotation/revoke、UI browser tests；用old-descriptor binary、old numeric enum、JSON `idempotency_key`和`Idempotency-Key` header clients遍历isolated target lane，断言stable rejection/ErrorInfo parity且未列unknown protobuf field仍可演进。对target candidate运行zero-reference与staged-path audit，并证明legacy命中全部只属于preserved rollback lane；shared active zero-reference保持`not_verified`并留给WR-13后/WR-14前门禁；all repo gates。

**Stop conditions:** 任一isolated target-lane caller仍需 plaintext、shared secret、static token、dev header、Service type或anonymous fallback；或无法把任一legacy reference唯一归类为target candidate或preserved rollback lane。

**Recovery:** 回退隔离 caller/UI changes；不动 shared traffic/data。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/12-complete-callers-ui-s2s-e2e/`
