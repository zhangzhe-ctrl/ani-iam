# AGENTS.md

在本仓库开始工作前，必须先阅读并遵守 [`CLAUDE.md`](CLAUDE.md)。

`AGENTS.md` 只保存长期稳定的入口规则，不保存当前任务正文。当前候选规格位于 `.scratch/ani-iam-p2-direct/spec.md`，无版本号计划位于 `docs/plans/`，领域词汇位于 `CONTEXT.md`，已接受的架构理由位于 `docs/adr/`。

只读分析可以直接进行。任何改变仓库或外部状态的工作都必须由 `.scratch/<effort>/issues/` 中一个状态为 `claimed` 的本地事项承载，并明确目标、范围、基线、依赖、验收、测试、恢复和停止条件。同一时间只推进一个会改变状态的事项。

删除、切流、Credential 失效、数据重建、生产操作或其他难以恢复的动作，必须在执行前获得针对精确目标和动作的人工确认。
