# Issue Tracker：本地 Markdown

本仓库的规格与事项使用 `.scratch/` 下的 Markdown 文件管理。

## 约定

- 每个功能一个目录：`.scratch/<feature-slug>/`
- 规格文件：`.scratch/<feature-slug>/spec.md`
- 实施事项：`.scratch/<feature-slug>/issues/<NN>-<slug>.md`
- 每个事项单独一个文件，按依赖顺序从 `01` 编号；禁止生成单一汇总事项文件
- Triage 状态记录在事项文件顶部附近的 `Status:` 行中，角色字符串见 `triage-labels.md`
- 评论与对话历史追加在文件底部的 `## Comments` 标题下

当前 IAM 重构功能目录是 `.scratch/ani-iam-rebuild/`。

## 当技能要求“发布到 Issue Tracker”时

在 `.scratch/<feature-slug>/` 下创建对应文件；目录不存在时可以创建。

## 当技能要求“读取相关事项”时

读取用户给出的文件路径或事项编号。只有当前事项明确引用规格、其他事项或证据时，才继续读取这些依赖。

## Frontier 与领取

- `Blocked by` 列出的事项全部为 `resolved` 后，当前事项才进入 frontier
- 在所有开放、未阻塞、未领取的事项中，编号最小者优先
- 开始工作前，先把当前事项改为 `Status: claimed` 并保存
- 同一时间只允许一个会改变仓库或外部状态的事项为 `claimed`
- 完成后，将证据与结果写回事项，再改为 `Status: resolved`
- 若需要重新拆分，未领取事项可以调整；已领取事项必须先停止并保留完成证据，再把剩余范围拆成新事项并更新下游依赖

## Wayfinding 操作

供 `/wayfinder` 使用。Map 由一个主文件和每个问题对应的子事项组成。

- **Map**：`.scratch/<effort>/map.md`，保存 Notes、Decisions-so-far 和 Fog
- **子事项**：`.scratch/<effort>/issues/NN-<slug>.md`
- **类型**：`Type:` 行记录 `research`、`prototype`、`grilling` 或 `task`
- **阻塞**：`Blocked by: NN, NN`
- **领取**：工作前写入 `Status: claimed`
- **解决**：在 `## Answer` 下追加答案，写入 `Status: resolved`，并把摘要与链接追加到 Map 的 Decisions-so-far
