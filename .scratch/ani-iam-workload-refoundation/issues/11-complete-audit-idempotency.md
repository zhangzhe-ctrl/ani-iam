# 11: 完成 Audit 查询与 Idempotency

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-18 事务审计/幂等基础；WR-22 审计查询与保留；WR-24 定向重试/并发；WR-25 汇总。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 交付 Human/Workload 全覆盖的安全 Audit 查询与至少180天在线可查语义，并完成对全部 endpoint 的 WR-02 `RetrySemantics` inventory和WR-03 ledger采用审计；不把owner-state或ephemeral endpoint伪装成ledger mutation。

**Blocked by:** 10 / 完成 Administration 与 Recovery

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-11 / replaces DP2-16

**Scope:** 三种actor、五种Principal evaluated-subject contexts、逐stage outcomes、requesting/executing transport contexts、transport-first与first-admin actor matrix、stable reasons/redaction/read boundaries。Endpoint inventory覆盖五种RetrySemantics；仅`ledger_*`用24h ledger，first-admin PG owner-state flow不写ledger但其Begin create/Complete claim/terminal/final transition均与Audit同UoW。180-day UTC/monitoring/无runtime cleanup。

**Non-goals:** WORM/cryptographic non-repudiation、SIEM/export、production retention、每次普通 allow 写 IAM row。

**Acceptance:** Audit exact-one且failed-auth candidate不成actor。Transport Principal解析前失败才Unauth；一旦direct Workload认证，transport authority deny归因该Workload。Existing carried subject唯一解析后可成为actor；first-admin planned Human在最终UoW前只是target，所以Begin成功及capability/flow/provider等deny始终归因requesting Workload，最终成功才归因新建Human并保留callback context。Registered producer按唯一解析点切换。每stage outcome真实，Unauth/Seed无Authority。所有IAM-local mutation（含Seed Phase0/Finalize、first-admin flow transitions、ReadyClaim）与Audit同PG UoW；owner-state/ephemeral无ledger。Tenant auditor隔离。

Audit事件在P2从`recorded_at`起至少180×24小时可由相同授权查询读取，应用没有按行update/delete/purge入口或自动cleanup job。确定性UTC fixture在`179d23h59m59s`、exactly `180d00h00m00s`和`181d`均返回事件；这证明最低窗口与“无清理时可能更久”，不承诺第181天删除。监控至少暴露row count、DB bytes/growth rate与oldest recorded_at。测试清理只可丢弃整个明确的disposable DB；active/shared DB的Audit row不属于任何手工逐行cleanup范围。

**Verification:** full endpoint/mode inventory；ledger和ephemeral gates。First-admin逐步注入Begin/claim/terminal/final Audit失败，证明对应PG state无mutation；覆盖Begin success actor、每类deny actor、Complete success new-Human actor、requesting context、claim crash/TTL/response loss与ledger row zero。Seed Phase0/Finalize actor与replay不重复；ReadyClaim final UoW。其余old-wire/enums、三actor/五context/stages、redaction/visibility、180-day UTC、real PG/race/vet/static。

**Stop conditions:** 需要存原始Token/Key、把owner-state/ephemeral塞入ledger、伪造未解析anchor、无法让安全mutation与Audit同事务，或只能通过Audit逐行删除才能通过容量测试。

**Recovery:** 停止测试并丢弃/重建本票明确创建的整个disposable DB；不得逐行删除/修改Audit或ledger，不修改retention job或共享history。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/11-complete-audit-idempotency/`
