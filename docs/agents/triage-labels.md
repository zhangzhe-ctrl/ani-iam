# Triage Labels

Matt Pocock engineering skills 使用五个标准 triage/处置状态角色。本仓库的本地 Markdown Tracker 在这些字符串之外，另有领取与完成所需的两个 workflow 状态。`Status:` 使用下表的完整单值词典；每个事项在任一时刻只能取其中一个值。

| 来源/角色 | 本仓库 `Status:` | 含义 |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | 需要维护者评估 |
| `needs-info` | `needs-info` | 等待报告者补充信息 |
| `ready-for-agent` | `ready-for-agent` | 规格完整，可由 Agent 领取 |
| `ready-for-human` | `ready-for-human` | 需要人工实施或判断 |
| `wontfix` | `wontfix` | 不会实施 |
| 本地 workflow | `claimed` | 已由唯一执行者领取，正在进行会改变状态的工作 |
| 本地 workflow | `resolved` | 已完成并写回结果与证据，不再属于开放 frontier |

当技能要求应用某个 triage 角色时，在事项文件的 `Status:` 行使用本表对应字符串。真正开始工作时按 `issue-tracker.md` 将唯一可执行事项转换为 `claimed`；完成并写回结果与证据后转换为 `resolved`。不得同时记录 triage 状态与 workflow 状态，也不得使用 `claimed / human checkpoint` 等复合值。

`bug` 与 `enhancement` 是类别角色，不替代上述状态。由 `to-tickets` 从已接受规格生成的 IAM 重构事项默认属于 `enhancement`；只有在运行证据证明已有行为损坏时才标为 `bug`。
