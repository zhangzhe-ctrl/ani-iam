# Triage Labels

Matt Pocock engineering skills 使用五个标准 triage 状态角色。本仓库的本地 Markdown Tracker 使用相同字符串。

| mattpocock/skills 角色 | 本仓库状态 | 含义 |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | 需要维护者评估 |
| `needs-info` | `needs-info` | 等待报告者补充信息 |
| `ready-for-agent` | `ready-for-agent` | 规格完整，可由 Agent 领取 |
| `ready-for-human` | `ready-for-human` | 需要人工实施或判断 |
| `wontfix` | `wontfix` | 不会实施 |

当技能要求应用某个角色时，在事项文件的 `Status:` 行使用本表对应字符串。每个事项同时只能有一个状态角色。

`bug` 与 `enhancement` 是类别角色，不替代上述状态。由 `to-tickets` 从已接受规格生成的 IAM 重构事项默认属于 `enhancement`；只有在运行证据证明已有行为损坏时才标为 `bug`。
