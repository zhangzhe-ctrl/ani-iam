# 15: 完成 Human / Workload 功能验收

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-25 M1 接口替换就绪；WR-28 M2 系统替换完成。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 聚合 clean install、Human/Workload、五类调用方、全部 S2S、切换与删除证据，形成不宣称 Production Ready 的最终功能验收。

**Blocked by:** 14 / 删除 legacy Auth 与不安全 S2S 路径

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-15 / replaces DP2-20

**Scope:** evidence completeness、requirements trace、fresh empty-environment run、negative matrices、artifact hashes、unverified/production-readiness register、independent Standards/Spec reviews。

**Non-goals:** 在验收票内修代码、部署生产、接受豁免、把 not_verified 改为 pass。

**Acceptance:** every requirement maps to current artifact and evidence；Human/Tenant-owned Workload Principal/Platform-owned Workload Principal/S2S all pass at stated grade；legacy zero-reference/delete proof complete；all gaps explicit；two independent reviews have no blocker。

**Verification:** fresh clone/empty DB generation/build/test、real dependency/process/caller E2E replay、artifact hash/staged path/Secret scan、Standards and Spec reviews。

**Stop conditions:** 需要修复、补 scope、重新生成基线或依赖人工接受；创建独立后续事项，不在验收内实施。

**Recovery:** 验收只读聚合与本地 evidence；不改变 runtime/data/external state。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/15-complete-functional-acceptance/`
