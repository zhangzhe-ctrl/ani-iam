# 08: 完成切换关键调用面对等

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-24 后台接口；WR-27 Console/BOSS 前端。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 聚合 Gateway、Envoy、Inference、Console、BOSS 与关键 S2S 链，证明新 Human/Workload contracts 覆盖当前切换所需行为。

**Blocked by:** 04 / Tenant-owned Workload Principal/API Key；05 / Platform-owned Workload Principal/WAT；06 / secure gRPC；07 / Core Lifecycle/NATS

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-08 / replaces DP2-13

**Scope:** 五类 caller 独立 adapters/E2E、browser contract pins、Gateway Scope×Evidence registry、Console Tenant receipt、BOSS delegated Platform receipt、Envoy API Key、Inference WAT、Session Gateway S2S、Core lifecycle config、统一错误/deadline/fail-closed。

**Non-goals:** deployment cutover、完整 admin/recovery/audit UI、legacy deletion。

**Acceptance:** 每类 caller 独立 pass；BOSS Platform on-behalf-of 要求 boundary=Platform receipt且不带 Tenant，Console Tenant chain 要求 exact Tenant receipt，自主 Platform operation 拒绝 receipt，二者不能 fallback；目标路径无 Service Principal/Service Token/insecure/shared-secret/dev-header fallback；contract/config pins 相同；failure mapping 稳定。

**Verification:** 五类独立 real-process E2E、BOSS Platform-with-Tenant/missing-receipt/receipt-on-direct-operation、Console missing-or-wrong-Tenant receipt、wrong credential/authority/audience/revision、dependency unavailable/deadline、zero-reference scan、full affected repo gates。

**Stop conditions:** 任一 caller 只能靠 legacy fallback、旧 contract 或未授权路径运行。

**Recovery:** 回退隔离 caller changes/config；不切换共享 selector或失效 Credential。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/08-complete-cutover-critical-callers/`
