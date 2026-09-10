# 09: 隔离轨道切入与回退演练

> **Superseded — 历史草案，不再提供执行授权。** WR-16 根据用户“先隔离接口替换就绪，再裁剪旧代码和对接前端”的决定重排本事项。当前处置为 `wontfix`，仅表示本票版本被替代，不表示其能力删除、原验收通过或既有证据失效。
> **能力去向：** WR-25 隔离接口验收；WR-28 实际整组切换与恢复演练。完整映射见 [当前事项图](../ticket-plan.md)。
> 下方正文、旧依赖、基线、评论和结果保留为历史；其中的旧 frontier、候选恢复机制和授权措辞不得用于领取或实施。已接受决定仍按 [当前规格](../spec.md) 与 [决定表](../decisions.md) 判定。

**What to build:** 在独立测试轨道整组切入新 IAM/Human/Workload 架构，并证明部署级回退，不删除旧资产、不失效旧 Credential。

**Blocked by:** 08 / 完成切换关键调用面对等

**Status:** wontfix

**Type:** enhancement

**Plan mapping:** WR-09 / Go-No-Go B

**Scope:** fixed images/contracts/config/seeds、隔离 namespace/traffic、五 caller + reference S2S smoke、cut-in、observation、rollback、before/after evidence。

**Non-goals:** shared/prod traffic、test-data rebuild、Credential invalidation、legacy deletion、code fallback。

**Required human input before claim:** 精确环境、traffic、namespace、artifact 和 rollback target 批准。

**Acceptance:** target chain operates together; rollback returns exact prior deployment state; no shared state changed; unresolved failure stops WR-10+。

**Verification:** deployment manifests/digests、endpoint convergence、caller matrices、S2S peer/audience proof、rollback smoke、resource ownership audit。

**Stop conditions:** 隔离性或恢复目标无法证明；需要共享写入、Credential 失效、删除或代码 fallback。

**Recovery:** 执行预先接受的部署级回退并只清理由本事项创建的隔离资源。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/09-rehearse-isolated-cutover/`
