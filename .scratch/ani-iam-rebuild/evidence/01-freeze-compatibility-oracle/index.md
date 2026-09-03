# 事项01证据索引

状态：`READY FOR HUMAN REVIEW`

采集日期：2026-09-02（Asia/Shanghai）

唯一 ANI 基线：`main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`

本目录只记录冻结提交的只读 Oracle。任何没有真实运行依赖的结果均为 `not_verified`，不能作为 CP0 Go 或生产就绪证据。

| 证据 | 状态 | 定位 |
| --- | --- | --- |
| Git 对象、分支意图、工作树隔离、Artifact 摘要 | `pass` | [baseline.md](baseline.md) |
| 14 RPC、JWT/bcrypt/Refresh/API Key、PG/RLS、Redis、Dex 静态 Oracle | `pass` | [compatibility-oracle.md](compatibility-oracle.md) |
| 可重新生成的二进制 Proto descriptor | `not_verified`：本机无 `buf`/`protoc` | [compatibility-oracle.md](compatibility-oracle.md) |
| 真实 PostgreSQL/RLS、Redis、Dex 行为 | `not_verified`：没有运行中的依赖容器，且未授权启动依赖 | [compatibility-oracle.md](compatibility-oracle.md) |
| Gateway、Envoy、Inference 固定调用面与 targeted 单元门禁 | `pass` | [callers.md](callers.md) |
| Gateway、Envoy、Inference 独立 live E2E | `not_verified`：没有运行拓扑 | [callers.md](callers.md) |
| 被否决旧门禁、历史失败、当前复跑与替代检查 | `pass`（静态 replacement gate）；人工接受待定 | [replacement-gates.md](replacement-gates.md) |
| CP0 语义范围、依赖、停止条件与评审清单 | `ready-for-human` | [human-review.md](human-review.md) |

## 不变量

- 所有 ANI 源码事实均通过 `git -C ../ANI/repo show <SHA>:./<path>`、`git grep <SHA>` 或冻结 `git archive` 读取，不把动态 `HEAD`、`main` 或工作树文件当作 Oracle。
- ANI 工作树在采集时 `HEAD == main == <SHA>`，无 tracked 修改；存在的未跟踪文件不进入 Git object 读取结果。
- 本事项没有启动容器、数据库、Redis、Dex、Gateway、Envoy、Inference 或 Kratos，也没有修改 ANI、API、runtime、migration 或 deploy 文件。
- `pass`、`fail`、`not_verified` 分开记录；静态或单元证据不能替代真实依赖与 live caller 证据。

## 复跑入口

```bash
python3 .scratch/ani-iam-rebuild/evidence/01-freeze-compatibility-oracle/replacement_gate.py
```

预期输出为 JSON，`result` 等于 `pass`。这只证明冻结身份、14 RPC 清单和当前 replacement assertions 可重现，不批准事项02或 CP0。
