# ANI IAM 历史阶段与证据索引

> 文档角色：历史索引，不维护当前执行顺序、票图、可领取 frontier 或新的 Go/No-Go。
>
> 唯一当前执行入口：[WR spec](../../.scratch/ani-iam-workload-refoundation/spec.md) + [ticket graph](../../.scratch/ani-iam-workload-refoundation/ticket-plan.md)。替换范围与里程碑见[能力矩阵](../../.scratch/ani-iam-workload-refoundation/capability-matrix.md)，待决定项见[decisions](../../.scratch/ani-iam-workload-refoundation/decisions.md)。
>
> 领域规则见[base design](plan-iam-service-refactor.md)，Human/Workload 模块边界见[Workload design](plan-workload-principal-refoundation.md)。整理前全文保存在[WR-16 before 快照](../../.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/before/docs/plans/plan-iam-kratos-phased.md)，其中旧阶段规则只作历史证据。

## 1. 为什么保留历史而不再维护阶段图

旧路线先尝试在保持 Auth wire/storage 语义时替换 Kratos transport。真实旧 RLS 验证暴露失败后，用户停止 CP0/P1 并转向 Direct P2。Direct P2 完成到 DP2-10，随后 DP2-11 的 Service/Workload 模型及 Allowed paths 无法闭合真实 caller 链；ADR-0022 接受 Human/Workload 分类和相关领域边界，旧后续事项被替代。

2026-09-09 再次重排纠正了交付阶段：先在隔离环境验证完整替换接口，达到 M1；Core 旧身份裁剪、前端接入与实际替换属于其后工作，达到 M2。旧 writer 或 UI 尚存不能倒推当前 IAM 接口失败。旧候选把 UI 放在隔离演练前、把必要成员/邀请能力放在演练后的次序不再提供执行依据。

这一变化沿用 WR effort，不建立第四条路线。WR-01–15 的候选正文、接受程度和已有证据原位保留并标 superseded；当前后续事项只从 WR spec/graph 读取。本文不再复制新票号及依赖，以免再次出现两张 frontier。

## 2. 历史路线索引

| 历史路线 | 记录了什么 | 当前用途与限制 |
| --- | --- | --- |
| [rebuild spec](../../.scratch/ani-iam-rebuild/spec.md) 与 issues/evidence | 固定旧 Oracle、Kratos scaffold、compat transport、真实旧 role/RLS 检查 | 基线和失败原因可复核；停止的 CP0/P1 不能据此重开 |
| [Direct P2 spec](../../.scratch/ani-iam-p2-direct/spec.md) 与 [ticket plan](../../.scratch/ani-iam-p2-direct/ticket-plan.md) | 目标契约、空库、Human/Session/授权及到 DP2-10 的 API Key 纵向交付 | 保留到 DP2-10 的历史结果，旧 DP2-11–20 不再提供当前执行入口 |
| WR-01–15 旧候选及 WR-16 before 快照 | Human/Workload 细化、跨服务与候选恢复状态机、旧执行排序 | 设计演变和 supersession 证据；候选长文不转为默认 accepted 规则 |
| 当前 WR spec/graph/matrix | M1 隔离接口就绪及 M2 后续实际替换 | 唯一执行和验收入口；本文不重复维护 |
| 历史 PR0 概念 | Production Readiness 与 HA/性能/广泛 Fuzz 等硬化 | 不等于当前功能完成，也不是可直接领取的事项 |

事项02 scaffold 与事项03 compat transport 的历史成果保留。事项04的真实旧 RLS 失败/阻塞由用户在 2026-09-03 关闭路线，不能改写为 pass；旧05–09 的剩余 CP0 与10–15 的 P1 是停止/跳过，不能因 superseded 算作已完成验证。DP2-11 的路径扩展 checkpoint 说明原合同无法承载目标，不证明全面重写现有分层或持久化底座有必要。

## 3. 历史基线不是当前消费版本

旧 Direct P2 ANI 来源记录为：

```text
repository: ANI
commit: 0cedae825a489d936cf41815dc27f278f6d3213c
historical verification date: 2026-09-03
historical branch intent: remote main
runtime compatibility: known-failing legacy Auth RLS; not a passing oracle
```

该记录只解释当时的基线；分支、当前工作树、`main` 和 `latest` 均不能替代固定对象。新事项需独立记录 IAM、ANI 和各消费者的 commit/tree、dirty 状态、契约 descriptor、生成 registry 与 artifact digest。已存在的快照或旧 target tag 不自动成为新 baseline。

旧 CP0 对照包含旧 Auth 14 RPC、JWT/bcrypt、非轮换 Refresh、Dex/OIDC、API Key、PostgreSQL role/RLS、Redis 与各 caller 的响应/状态/副作用。完整 inventory 和差分规则仍在原 spec/evidence 及本文 before 快照；这套兼容分母不再约束破坏性重冻后的目标契约。

Direct P2 从新目标契约和空库构建可运行基线。其通过项仍只证明当时版本及测试范围；ADR-0022 改变了 Principal、契约、schema 和调用方语义，必须对受影响能力重新验证，不能用历史 API Key pass 覆盖当前 Workload receiver、delegation 或真实跨服务链。

## 4. 旧门禁与证据的读法

历史 Gateway authz generated drift/protected Core path 门禁曾被用户认定有问题。保留原命令、断言、失败输出、失效原因、目标合同、替代检查和人工接受记录；不得要求恢复旧错误断言，也不能通过静默跳过、删测试或改名把历史红灯变绿。新的替代门禁以当前事项和 accepted 目标为准，旧 CP0 解锁语义已经失效。

历史 `pass` 必须带固定版本、真实依赖/role、调用方与场景范围；`fail` 保留失败事实；`not_verified` 不能提升。静态 allowlist/注册存在、构建成功、fake adapter、进程 readyz 与单条 smoke 是不同层级的证据，均不能单独证明替换就绪。

历史 CP0 的真实 RLS deny-all、Direct P2 的无 RLS 隔离证据、CF-01 的有限 smoke 和新 Human/Workload gate 不可互换。若复用旧 harness/fixture，只复用工具与可复现起点；正式进程、真实 owner、所有必要 caller 和最终版本组合仍须按当前能力矩阵验收。

旧 Go/No-Go A/B、cutover-window、WR-09/WR-13/WR-14 等标记属于历史计划坐标，不提供当前确认对象或删除许可。历史失败与未验证项可被新证据解决，但原记录不删除、不改成过去已通过。

## 5. 历史删除清单与当前所有权

旧计划列出 auth-service、旧 Auth Proto/client、旧 Session/Refresh/blocklist/API Key、重叠身份入口与 signing material 等最终退出目标。它仍帮助查漏；不表示整份 Core tenant-service、Tenant Lifecycle、Quota 或自有治理能力都归 IAM，也不授权直接删除旧模块或测试数据。

当前替换能力和唯一 writer 以 base design/capability matrix 为准。M1 证明 API replacement ready，不要求先裁剪现有 Core 或接入 UI。M1 后 Core 裁剪和前端对接可按各自明确范围推进，不互相虚构前置。M2 证明实际接入与 writer 退出；真正切流、Credential 失效和不可恢复删除仍须精确范围、恢复前置和执行前人工确认。

已接受的恢复审批、snapshot/演练与删除前恢复要求仍在保护动作之前。旧候选 Lineage Registry、ReadyClaim、PONR 与长期 DR 机制详见 before 快照及 Workload 文档的非规范索引，不能成为全部 M1 统一前置；将候选移出也不能把已有恢复要求挪到删除之后。

## 6. 继续工作的入口规则

阅读本文只能获得历史解释。实施前回到 WR spec/graph，核对 capability matrix、decisions、固定基线、允许路径、依赖和唯一 claimed 事项。receiver 待决选择、first-admin 待冻结输入与 Snapshot 候选 transport 修订分别按 decisions 处理；不从历史长段落、当前代码或旧票中猜 accepted 默认值。

Gateway→Session Gateway 仍是 ADR-0022 指定的首条 mandatory 跨服务参考链。接口隔离、真实依赖与 owner、后续 caller 回归、M1/M2 gate 的具体顺序只维护在当前图。本文不新增并行实施路线、不解锁下一票、不复活已停止阶段，也不宣称 Production Ready。
