# 16: 整理文档并重排隔离接口替换计划

**Status:** resolved

**Type:** task

**Blocked by:** 无；用户在本会话明确要求完成文档整理、重排和符合性验证。

## 目标与授权

依据用户当前决定完成文档变更：先在隔离环境做接口测试；IAM 达到替换就绪后才裁剪 Core 旧身份代码并启动前端对接。验证架构、简洁性、可扩展性、对旧服务的可替换性；不将当前旧 writer/UI 尚存误判为本阶段失败。

用户授权原文：“接下来帮我完成文档整理和重排，然后验证文档与我的期望是否匹配（架构、简洁性、可扩展性、对旧服务的可替换性等等），如果有问题或我没表述清楚，可以随时和我沟通”。本事项只执行文档工作，不实施后续产品事项。

## 基线与依赖

- 仓库：`/home/chabking/workspace/ani-iam`；HEAD `cd38cd90bca3e9d83af09a381051b895d82ae94f`。
- 起始工作树已有文档、ADR、WR 草案与历史 supersession 改动，全部保留；修改前的文档副本、摘要、工作树状态记于 `../evidence/16-organize-docs-and-replan/baseline.json` 和 `before/`。
- 已读 CLAUDE、当前规格/计划、领域词汇、相关 ADR 与本地 tracker 规则。
- WR-01–15 的旧草案尚未发布为实现 frontier。本次重排保留其原文和旧票到新票能力映射，不将待定机制伪造为已接受。

## Allowed paths

- `README.md`、`AGENTS.md`、`CLAUDE.md`、`CONTEXT.md`。
- `docs/plans/*.md`、`docs/adr/*.md`、`docs/agents/domain.md`、`docs/agents/issue-tracker.md`。
- `.scratch/ani-iam-workload-refoundation/spec.md`、`ticket-plan.md`、`capability-matrix.md`、`decisions.md`、`issues/*.md`。
- `.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/**`。
- `.scratch/ani-iam-rebuild/spec.md` 的历史导航；不改历史正文与结果。

## 非目标与禁止范围

不改产品代码、契约源/生成物、schema、配置、测试程序、其他仓库、CP0/DP2/CF-01 历史 evidence。无 commit/push、环境部署、数据库/凭据/流量操作、资产删除。不得改写历史 pass/fail，不替用户接受仍 pending 的 receiver、信任输入或恢复机制。

## 验收与验证

1. 当前 spec+ticket graph 为唯一执行入口；M1 接口替换就绪与 M2 实际替换完成分开。
2. 明确 Core/IAM/资源 owner、Human/Workload 模型、事务与权限边界；保留已接受决定。
3. 必需能力矩阵覆盖旧 Auth 与身份管理替换，定义成功/拒绝/故障/恢复及真实 owner 证据。
4. 前端和 Core 旧代码裁剪位于 M1 后；候选大型恢复方案不再无差别阻塞所有接口工作，已接受安全约束继续有效。
5. 历史/有效/候选内容分清；旧草案与新票有覆盖映射；所有新实现事项不因文档工作自动领取。
6. 运行 Markdown 本地链接、状态/依赖/能力覆盖、一致性与范围检查；独立审阅四个期望维度并修复发现的问题。
7. 证据记录为文档层结论；产品功能与部署不因本次文档验收变成 pass。

## 恢复与停止

本次修改可按 before/ 与 baseline.json 逐文件恢复本次增量；不使用 reset/stash 或覆盖其他人的改动。发现并发同路径修改、需要变更已接受且未被当前用户修订的领域决定、或需扩大到产品/外部状态时，暂停依赖部分并询问，继续不依赖该决定的文档工作。

## 结果

文档工作与收尾校验已完成，本票已关闭。交付见 [符合性审阅](../evidence/16-organize-docs-and-replan/review.md) 与 [机器检查结果](../evidence/16-organize-docs-and-replan/verification.json)。本次从 2026-09-09 整理至 2026-09-10。

- spec/唯一事项图/能力矩阵/有限决定登记已成为当前入口；基础设计保留 accepted 规则，Workload 设计压缩，历史计划改为追踪索引。
- 旧 WR-01–15 原文保留并标 superseded/wontfix；新 WR-17–29 明确 M1→代码裁剪与前端→M2→资产退役，后续票未领取。
- 用户明确选择的 OIDC owner 已作为 D08 accepted 写入 ADR-0015：IAM 统一认证、Identity 和 Session，Gateway 为薄入口；保留既有浏览器安全/错误规则。
- Core Snapshot 当前保持 accepted REST/OpenAPI；Core 独立后内部请求建议收敛 gRPC，公网 HTTP 与 NATS 事实发布各自保留，不作为 M1 前置。用户的协议追问没有被当成当前切换授权。
- 独立审阅的词汇候选程度、OIDC返回行为、管理员恢复区分、worker evidence与Audit retention候选约束问题已修复，详见 review。
- 快照52份、保护 tracked文件393份、旧15票正文、Q1–Q300、Kratos模板/历史正文保真检查 pass；新13票依赖/必需章节、14项能力与4项后续结果、本地Markdown目标/空白与git diff --check通过。产品代码与历史保护文件未因本次改变；没有运行产品测试或操作环境。
- D01/D02 等限定选择与 D03–D05 实施输入留在决定表，由 WR-17 收敛，不影响本票文档验收。当前功能、M1/M2、部署和生产证据仍为 not_verified。

证据先在本票 claimed 时保存；随后只更新本票关闭状态并做只读复核。verification.json 的 captured status/hash 属于关闭前的文档检查，当前状态以本票顶部为准；可以运行 verify_docs.py 对关闭后的工作树复核。WR-17 不自动领取。
