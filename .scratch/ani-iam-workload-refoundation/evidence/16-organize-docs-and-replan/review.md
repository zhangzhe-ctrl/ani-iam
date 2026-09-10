# WR-16 文档整理与期望符合性验证

日期：2026-09-09 至 2026-09-10。范围：本次文档增量；基线与原稿见 [baseline.json](baseline.json) 和 [before/](before/)。

**结论：当前文档的架构边界、复杂度控制、扩展方式和替换顺序符合用户目标，可以作为后续合同收敛的入口；IAM 新目标的功能与可替换性仍为 `not_verified`。** 这次没有实现或运行 M1，也没有执行 Core 裁剪、前端迁接或环境切换。

## 1. 用户意图与当前落点

| 用户意图 | 当前明确约束 | 文档判断 |
| --- | --- | --- |
| 从 ANI 抽出 auth-service、用户和权限，职责清楚 | IAM 拥有身份/凭据/Session/访问关系；Core 保留 Tenant Lifecycle/资料/Quota；资源 owner 保留资源和执行 | pass |
| IAM 好用之后再裁剪 Core | M1 要求目标链不调用旧 Auth/旧 writer，允许旧代码仍存在；WR-26 在 M1 后裁剪 | pass |
| 先隔离接口测试，之后才接前端 | M1 包含真实 Gateway REST、Cookie/CSRF/redirect 协议，UI 在 WR-27；WR-26/27 都依赖 M1，互不虚构前置 | pass |
| 简洁并且能扩展，不因一个 Workload 陷入全平台重写 | 保留 Human/Workload 正交模型；先 Gateway→Session 参考链；大型候选 Lineage/Ready/PONR/长期 DR 移出 M1 统一前置 | pass；有限信任规则仍须定稿 |
| 认证统一归 IAM | 用户明确接受 D08；OIDC flow/state/verifier、Provider 交换、Identity、Session/Token 归 IAM，Gateway 负责公网/Cookie/CSRF/转发 | pass |
| Core 拆分后内部通信逐步收敛 | 当前 Snapshot 继续有效 REST/OpenAPI；后续 Core 独立时建议内部请求收敛为 gRPC，公网 HTTP、事实 NATS；具体迁移另行定稿 | pass；未来方向尚非当前协议切换 |

权威入口：[spec](../../spec.md)、[ticket plan](../../ticket-plan.md)、[capability matrix](../../capability-matrix.md)、[decisions](../../decisions.md)。

## 2. 四个维度的判断

**架构：文档层 pass。** Tenant 是跨领域对象，各服务只拥有自己的事实，不能因为 IAM 使用 tenant_id 就把完整 Tenant 治理迁入 IAM。Core 创建 Tenant 与 IAM 建立 Access/初始管理员是独立、本地原子的步骤；通过 outbox、持久接受和可恢复处理协作。Notification 只负责通知投递，Network 只负责资源与网络操作；两者不需要复制身份或权限管理。[所有权表](../../spec.md)、[Tenant 边界 ADR](../../../../docs/adr/0002-separate-tenant-lifecycle-from-iam-access.md)、[异步协作 ADR](../../../../docs/adr/0004-coordinate-tenant-lifecycle-and-iam-asynchronously.md)。

**简洁性：文档层 pass。** 当前只有一张事项图，基础设计记录稳定规则，Workload 设计解释模块，CONTEXT 只保留当前词汇，旧计划只作历史索引。整理前后 Workload 设计从 602 行降至 134 行，历史阶段计划从 271 行降至 69 行；基础设计保留大部分已接受业务规则。减少的是重复入口与未接受机制，不是必要的权限、审计、恢复或真实依赖验收。

实现仍应保留已有 Kratos 分层、框架无关 biz、sqlc/pgx 与本地事务基础，在受影响的 Principal、契约、存储与 caller 边界做有界重构。文档审查没有提供推翻全部 IAM 底座的依据。[模块设计](../../../../docs/plans/plan-workload-principal-refoundation.md)、[分层 ADR](../../../../docs/adr/0019-use-kratos-conventions-with-framework-independent-business-rules.md)。

**可扩展性：模型检查 pass。** 新资源通常增加 owner 的权限声明、资源条件与对应注册；副本扩容不改变 Principal；换认证方式不改变主体类型或授权来源。IAM 不承接 provider/reconcile 状态机，各业务 owner 保持自己的幂等和恢复规则。实际增加一个资源/调用方的改动量尚未通过新模型实现测量，仍为 not_verified。[ADR-0022](../../../../docs/adr/0022-unify-software-actors-as-workload-principals.md)。

**对旧服务的可替换性：计划覆盖 pass，运行结果 not_verified。** C01–C14 覆盖 Human/OIDC/Session、管理/邀请/业务恢复、Tenant/Platform Workload、Core/NATS/Notification、Audit、重试与实际 callers；每项均需最终固定版本、真实 owner、正式进程的证据。不能把接口替换缩成登录 smoke，也不能把未实现管理 RPC 的空成功算通过。[能力矩阵](../../capability-matrix.md)。

## 3. 场景推演

以下是对文档约束的推演，没有执行运行时测试。

| 场景 | 文档要求的结果 | 依据 |
| --- | --- | --- |
| 冻结 Tenant A，Human 仍属于 Tenant B | A 的访问被拒绝，身份认证与 B 的访问分别判断；A 的成员/角色历史保留 | ADR-0002/0004，C02/C05/C09 |
| Core 创建 Tenant 时初始邮箱未验证 | Core 自有开通事务完成；IAM Access 待就绪，邀请验证前不生成有效管理员权限 | ADR-0004/0010，C06/C09/C10 |
| 新增 Network 资源动作 | Network 拥有对象、operation/provider/事务；IAM 评估权限，Network 验证真实 resourceTenant/Owner | spec §3–4，C05 |
| 一个 Gateway 扩到多个 Pod、轮换证书 | 复用稳定 Workload Principal；独立撤销边界才需要独立主体 | ADR-0022 |
| API Key 创建者离职或权限改变 | Key 不继承创建者权限快照；按所属 Workload 的当前 Membership/Role 判定 | ADR-0008/0022，C07 |
| Workload 提交自报 Tenant 或转发用户 Token | 不能生成自身 Authority，直接 caller 与 subject 分开，具体最小 evidence 规则由 D01/D02 收敛 | ADR-0022，D01/D02，C08 |
| 第一次启动、尚无管理员 | 受控可信初始化要闭合，不允许以“已有管理员”作为创建首管理员前置；完整管理谱系平台不成为通用前置 | spec §4.10，D03，WR-18/19/22 |
| 已有 Tenant 丢失全部可用管理员 | 走 RestoreTenantAdmin；不能假装原 Bootstrap 未发生。Recovery Bootstrap 处理另一类恢复，两者保留已接受授权/重认证/审批/Audit | ADR-0009，C06，WR-22 |
| OIDC 成功后回跳、Refresh 响应丢失 | IAM 完成认证；Gateway 设置 Cookie，固定303回跳无 code/Token；严格 rotation/reuse 与401/503规则保留 | ADR-0015，C03/C04，WR-20/24 |
| 没有前端、旧 Core 代码仍在仓库 | 可运行 M1 隔离 API 验收；目标链不能调用旧 Auth 或旧 writer；源代码/UI 后续再迁接 | spec §1/4/6，WR-25/26/27 |
| Lifecycle 投影刚运行、只证明 shadow 正确 | 保留10s heartbeat、30s stale与24h shadow启用门禁；强制拒绝未验证时 C09/M1 不能整体 pass | ADR-0004，C09，WR-23 |
| 将来切流、失效凭据或退役旧数据 | 对应动作前完成其恢复与精确确认；候选恢复机制延期不会授权这些动作 | D07，WR-28/29 |

## 4. 已修复的审阅问题

两份独立只读审阅分别检查架构/扩展场景和规范一致性，以下问题已在本次文档中修复：

1. OIDC 业务 owner 旧规则与用户选择冲突：仅修订其所有权条款，保留 Cookie/CSRF/精确 redirect/strict reuse；补回容易遗漏的303与401/503处理。
2. CONTEXT 将 WAT 整体标为候选、把可登录首管理员混入未来谱系词汇：稳定概念单独保留，仅机制待定。
3. 大型 Lineage/Boot/Ready/DR 定义仍占用当前词汇：改为历史快照链接，当前实施只读取相应 D03/D07 范围。
4. 管理能力矩阵没有明确区分 RestoreTenantAdmin 与 Recovery Bootstrap：C06/WR-22 已分别列出。
5. ADR-0008 把尚未接受的 Scope/Evidence 组合写得像既定规则：保留自身 Authority 和可信事实不变量，精确组合交 D02。
6. ADR-0017 将候选整库清理与精确时间测试配方写成强制要求：与基础设计统一接受程度，保留 runtime append-only、至少180天可查和无自动清理。
7. Core Snapshot 旧 REST ADR 与现有 gRPC Proto 冲突：恢复明确的权威顺序，当前仍采用 REST；Proto 不是决定或实现证据。Core 独立后的 gRPC 收敛作为后续方向登记。

Core 核对依据：[ANI Core 接口规则](/home/chabking/workspace/ANI/CLAUDE.md:87)、[Gateway 内 Tenant 装配](/home/chabking/workspace/ANI/repo/services/ani-gateway/tenant_runtime.go:18)、[Snapshot Proto](/home/chabking/workspace/ANI/repo/api/proto/tenant/integration/v1/tenant_iam_integration.proto:13)。没有查到“因为只支持 HTTP 所以选择 REST”的明确历史比较记录；这是背景推断。当前非生成 Go 源中未找到这两个 Snapshot RPC 的实现/注册，不能据此声称真实 Core Snapshot 已可用。

## 5. 文档与历史处置

- README/AGENTS/CLAUDE 统一导航；保留原 Kratos 模板完整后缀。
- 旧 WR-01–15 只增加 superseded 说明并将顶部状态改为 wontfix，能力映射到 WR-17–29；去掉新增说明、还原原状态后，15份正文逐字等同 before。
- Q1–Q300 保留历史答案，当前阶段修订另列表；历史阶段计划改为证据索引。
- 修改前52份文档完整保存在 before，哈希与 baseline 一致。393份保护范围内 tracked 文件保持原哈希，其中包含产品代码和既有历史证据。
- 当前工作树本来就有未提交改动；本次未 reset/stash、commit/push。保护校验不声称覆盖没有初始哈希的其他未跟踪文件。

## 6. 检查与下一步

可复现文档检查：

```bash
python3 .scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/verify_docs.py
```

检查快照、保护文件、旧票正文、单一事项状态、新票依赖/章节、14项必需能力与4项后续结果、当前 Markdown 本地目标和空白、历史正文与 Kratos 模板保真、git diff --check。实际输出见 [verification.json](verification.json)。链接校验不包含外部 URL 或页面锚点；没有运行产品测试。

下一项是 [WR-17](../../issues/17-freeze-api-replacement-contracts.md)：固定 M1 能力到真实接口/consumer 的映射、最小 Workload 信任契约、首次可信初始化和隔离输入。D01 receiver 与 D02 具体委托/异步证据仍需收敛；D03–D05 是实施者要准备的方案/精确输入，不把整张技术清单交给用户填写。WR-17 仍未领取。

实施优先证明正式 runtime 和 Gateway→Session 参考链，再补全 Human、Tenant Workload、管理、Core/NATS 与后台 caller。WR-22/23/24 较大，执行时按明确场景分步验收；新差异超过边界时保留已完成证据并拆剩余范围，避免继续扩大同一事项。

**文档整理验收：pass。新目标产品功能、M1/M2、实际部署与 Production Ready：not_verified。**
