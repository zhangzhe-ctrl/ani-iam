# Domain Docs

本文件规定探索和修改本仓库时，各类领域文档的职责；文档权威顺序和授权规则以根目录 [CLAUDE.md](../../CLAUDE.md) 为准。

## 探索前读取

- 阅读当前 [spec](../../.scratch/ani-iam-workload-refoundation/spec.md) 与 [ticket plan](../../.scratch/ani-iam-workload-refoundation/ticket-plan.md)，再阅读当前事项及其直接依赖。
- 阅读根目录 [CONTEXT.md](../../CONTEXT.md) 与 [ADR](../adr/) 中相关的已接受决定。
- 按当前事项的引用阅读 [decisions](../../.scratch/ani-iam-workload-refoundation/decisions.md) 和设计计划，区分已接受约束、候选机制和历史证据。

如果所需文件不存在，记录缺失，不要仅因缺失而预先创建文档或推定决定。只有涉及当前事项的必需输入缺失时才阻塞其依赖工作。

## 文档职责

本仓库使用根目录 `CONTEXT.md` 与 `docs/adr/` 的 single-context 布局。

| 文档 | 职责 |
| --- | --- |
| `.scratch/ani-iam-workload-refoundation/spec.md` | 当前目标、范围、不变量与里程碑验收。 |
| 同目录 `ticket-plan.md` 与 `issues/` | 唯一执行依赖图、事项范围、状态与证据入口；与 spec 共同构成当前执行入口。 |
| 同目录 `decisions.md` | 已接受和待决选择的登记及其适用范围，不产生第二套执行图。 |
| 同目录 `capability-matrix.md` | 对旧服务替换能力的覆盖与验收映射，不替代事项状态。 |
| `docs/plans/` | 目标设计、架构解释和取舍；不维护平行执行顺序或独立 frontier。 |
| `docs/adr/` | 已接受且仍有效的架构决定及理由；部分替代必须明确到具体条款。 |
| `CONTEXT.md` | 简洁领域词汇；不承载实现流程、字段算法或交付顺序，不以术语定义接受候选机制。 |

`.scratch/ani-iam-rebuild/`、`.scratch/ani-iam-p2-direct/` 和当前 effort 中已封存的旧规划只保留原始路线、结果与 supersession 证据，不产生当前 frontier。历史 `pass`、候选方案和当前实现都不能自动成为已接受目标。

## 使用词汇表语言

事项、实现、测试、评审和错误说明使用 `CONTEXT.md` 的术语，避免 `_Avoid_` 中的同义词。标为候选的词汇仅供讨论，实施前仍须满足当前 spec、事项和决定登记中的适用前提。

缺少术语时，先判断是否存在真实领域缺口；确定后的术语由 `domain-modeling` 按简洁定义更新。设计过程、算法和未决选择分别留在设计计划与决定登记中。

## 处理决定冲突

发现事项、候选设计或实现与已接受 ADR 冲突时，明确记录具体条款及理由，不能静默覆盖或将整份 ADR 废弃。依据 `CLAUDE.md` 的权威顺序判断当前用户决定是否已修订该条款；已有明确授权时同步权威文档，无此决定时保留为待决，继续不依赖该选择的工作。
