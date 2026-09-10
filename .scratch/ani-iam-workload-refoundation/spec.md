# IAM 重构与替换规格：隔离接口优先

Document state: 当前执行范围；文档整理已完成，2026-09-10 用户以 WR-17/18 Goal 授权依序完成合同冻结和隔离运行基础。技术选择的接受状态逐项见 [decisions.md](decisions.md)；当前票与精确写入范围以 ticket-plan/事项为准。

## 1. 目标与阶段

将 ANI 的 auth-service 与用户、成员、权限管理抽离为独立 IAM，职责更清晰，调用方只依赖稳定契约；保留 Core 的 Tenant 生命周期、套餐和 Quota 治理，保留资源服务自己的对象、执行与恢复。

用户在本会话明确：先在隔离环境完成接口测试；IAM 能够替换 auth-service 后，再裁剪 Core 的旧身份代码并启动前端对接。旧 Core writer 和未接线的 Console/BOSS 是后续工作，不能据此认定本阶段架构失败。

| 里程碑 | 完成定义 | 不作为本阶段前置 |
| --- | --- | --- |
| **M1：接口替换就绪** | [必需能力 C01–C14](capability-matrix.md)在精确固定的目标版本组合下，通过正式进程、真实依赖与真实业务 owner 的隔离接口验收；目标路径不依赖旧 Auth/旧身份写入 | 前端页面接入、旧 Core 代码全仓删除、现有环境切流、旧数据与凭据清理、Production Ready |
| **M2：实际替换完成** | M1 后完成 Core/Auth 代码裁剪、Console/BOSS 接入、实际消费者切换与整体回归；旧身份 writer 退出 | 未列入本次替换范围的物理数据/运行资产清理；生产就绪 |

资产与数据清理单列 R04/WR-29，执行前验证精确范围和恢复前置；M1/M2 不自动授权高风险操作。

## 2. 哪份文档回答什么

- 本 spec：目标、范围、里程碑、跨阶段约束。
- [ticket-plan.md](ticket-plan.md)：唯一当前顺序、依赖与事项状态入口；不再维护另一张并行阶段图。
- [capability-matrix.md](capability-matrix.md)：必需能力、验证分母、对应 owner 和后续退出清单。
- [decisions.md](decisions.md)：仅记录当前有限未决点及其影响，不保存未来所有功能的详细设计。
- [基础设计](../../docs/plans/plan-iam-service-refactor.md)：已接受领域规则与不变量；[Workload 设计](../../docs/plans/plan-workload-principal-refoundation.md)：模块设计和候选机制说明。
- [CONTEXT](../../CONTEXT.md)是领域词汇；[ADR](../../docs/adr/)记录已接受决定的理由；实现不能从候选术语或历史文字推导授权。

用户当前明确决定 > accepted ADR > 未被修订的 accepted base rules。实现/测试描述现状。pending 机制只有在对应决定接受后才可生效，未选择的路径保持不可用。

## 3. 架构边界与可扩展性

| 领域 | 唯一负责的事实与事务 | 对外关系 |
| --- | --- | --- |
| Core 治理 | Tenant ID/业务资料/Lifecycle、套餐与 Quota 不变量及自己的 outbox | 发生命周期事实和 IAM Bootstrap 请求；提供有限 Snapshot；不管理 IAM 凭据/成员/角色 |
| IAM | Human/Workload、Identity/Credential、Session、Access、Membership、Role/Binding、Invitation、权限评估与安全 Audit | 通过固定版本契约提供认证、管理和决定；不写 Core/资源数据库 |
| Gateway | 公网入口、Cookie/CSRF/错误转换、生成授权入口与路由 | 不维护第二份身份权限真相，不成为资源状态执行者 |
| Network/其他资源 owner | 真实 resourceTenant/Owner、资源生命周期、operation、provider、reconciliation、业务幂等 | 检查 IAM 决定要求的资源条件；引用外部 Tenant/Principal ID，不复制身份管理 |
| Notification | 模板、投递策略、Delivery/Attempt、重试与投递状态 | IAM 决定通知意图和收件地址；不创建用户或 Membership |
| Session Gateway | exec/serial/VNC 连接、ticket、lease 与流生命周期 | IAM 登录 Session 与连接 Session 分开；保留目标资源核验 |

Principal 只有 Human 与 Workload。Workload Owner、Identity Binding、Credential、Authority 与每次执行上下文独立；Tenant-owned Workload 通过固定 Tenant 的 Membership/Role 获权，Platform-owned Workload 通过明确 Grant 获权，不制造 Membership。

新增资源动作主要变更资源 owner 的契约/权限声明与对应注册，不改变 Principal 分类、重新实现 Token 校验或给 IAM 增加 provider 状态机。Workload Authentication 和 Workload Authorization 封装认证方法与存储细节，消费端复用固定版本适配。扩展不得借空 Tenant、请求体主体、Header、NetworkPolicy 或转发用户 Token 绕过信任。

IAM 保持一个服务内的清晰模块和三个既有 gRPC 职责面；本轮不再拆新的 Auth/User/Permission 微服务，不新建 Tenant/Quota/Lineage 服务，不为了假想消费者预建通用平台。每个业务 owner 使用自己的本地事务、operation/outbox 与恢复；无跨库事务、共享写表或长期双写。

## 4. M1 的交付规则

1. 先将旧 Auth/身份管理能力映射到目标接口，记录保留、有意改变和退役行为。新契约允许已接受的 breaking；旧系统缺陷不是兼容要求，不以修好旧 Auth 为目标前置。
2. 每个接口明确 direct caller、被服务主体、Tenant/Platform scope、权限、资源条件、返回值、错误与重试规则。生成的 contract/registry 与正式 runtime 使用同一固定版本。
3. 正式 `cmd/server`、真实 Gateway REST 和必要内部调用方是最终测试入口；不能由测试专用 server 绕过 middleware/config/身份校验。无前端也要验证 Cookie、CSRF、Origin、redirect 与刷新协议。
4. Human 原有能力在新模型下复核；Tenant/Platform 权限管理和邀请属于替换必需能力。完整未来高级功能不自动进入 M1，但 C01–C14 不得由实现者单方删除。
5. Gateway→Session Gateway 保持 ADR-0022 的首条跨服务参考链。Notification 自主调用与用户委托分开；最终扩展到 Gateway、Envoy、Inference、Session 的实际必需调用面。
6. Core 目标 producer/Snapshot 可在独立配套工作树实现并测试，保留现有环境。M1 必须证明真实 Core→IAM Bootstrap/Lifecycle，不要求先裁剪当前 Core；预置 projection 仅属早期测试。
7. 目标测试链路不回调旧 Auth、不写旧身份表、不在失败时 fallback；旧运行环境和旧源文件可以保留到后续阶段。全仓 zero-reference 适用代码裁剪后的 R01/R03，不作为 M1 的错误前置。
8. 每条链检查成功、拒绝、依赖故障及恢复，并验证持久状态与副作用。至少两 Tenant 交叉负向；无 RLS 的 Tenant-owned 表使用 tenant_id、显式谓词、复合唯一/FK 与受限 runtime role。
9. 状态变更和安全 Audit 同 IAM 本地事务；跨服务通过持久接受与可恢复投递协作。幂等依接口 owner 定义；不得用 IAM 通用 ledger 覆盖 Network 永久业务幂等或用重试重复产生权限。
10. 首次可信引导和首个可登录管理员必须有闭合、受控且可复现的 owner 路径；不能靠网络旁路、隐藏默认管理员、运行时自注册或伪造 Principal。所需机制在相应前置冻结；完整外部管理谱系/长期 DR 不是所有接口的统一前置。

## 5. 隔离和证据

每个实施事项先固定自身仓库 commit/tree、契约摘要、精确允许路径、真实依赖、测试资源及恢复方法。后续工作树需独立，避免与 ANI Network 等活动事项争用同一路径。

测试数据库/owner/runtime role、Redis key 空间和凭据、NATS Account 或可证明隔离的 subject/Stream/Consumer/ACL、Dex Client/身份/回调、Workload 身份与测试 Credential 必须与现有业务环境隔离。可复用基础设施，不共享可变测试数据，不全局 flush；namespace 名称不同不等于隔离。

CF-01 [后续恢复运行](../cutover-fitness-01/evidence/01-establish-dual-lane/runtime-20260909t013439z/summary.md)曾完成固定密码登录/受保护读取，属于旧固定版本的有限历史 pass。复用前核验资源归属、活动状态、版本与作用域，并在新事项列明；不推定可修改既有 lane。OIDC discovery/fixture backend 不能证明真实 OIDC/Core 闭环。

保留 run_id、commit/tree/镜像与契约摘要、脱敏命令/退出码、接口矩阵、DB 状态断言、故障恢复结果、Secret scan 和资源保留/恢复入口。fake/unit、真实依赖、真实进程、业务链、部署与生产证据分别记录。WR-18 已取得当前模型的隔离运行、受影响 Human 回归与事务基础子集 pass，见 [WR-17/18 交接](evidence/18-refound-human-workload-runtime/README.md)；C01–C14 的完整 M1 能力格仍为 not_verified。历史 pass 只用于定位可复用资产，不替代当前测试证据。

M1 仅在必需格全部 pass、无未解释差异、无必需 RPC Unimplemented/fake-only、无旧路径依赖时通过。允许 fake 做单测/故障注入，SMTP sink 可作为真实隔离邮件目的地；不能用手写 Core/Notification handler 或 discovery stub 替代最终业务 owner。

## 6. M1 之后

Core/Auth 旧代码裁剪与 Console/BOSS 接入都从 M1 后开始，彼此可有独立实现范围，不把 UI 完成设为隔离分支裁剪代码的前置。裁剪只处理身份/权限写入和其调用链，保留 Core Tenant/Quota 与资源治理；跨 owner 的 users FK 改为目标外部引用，Core 自有 Tenant 关系不机械删除。

实际消费者切换需固定最终组合并验证旧 writer 退出；切流、Credential 失效、数据重建、物理删除按各自精确事项和人工确认执行。已接受的操作安全/恢复条件保留，新的管理谱系/恢复机制在真正需要的阶段先决定、实现和验证。不得为赶 M1 静默豁免后续恢复要求。

## 7. 文档和历史处置

本轮沿用 WR effort，不再创建并行当前路线。WR-01–15 原稿及其未决问题在 [整理前快照](evidence/16-organize-docs-and-replan/before/)完整保留；旧票只增加 supersession 与状态说明，能力迁入 WR-17–29。CP0、DP2 的结果、失败证据和固定基线不改写，旧导航指向当前入口。Q1–Q300 原答案只作历史索引，修订与当前有效解释另列。

整理任务 WR-16 的完成仅说明文档交付与审查完成，不等于 M1 pass，也不替用户接受未决技术机制。此后用户明确授权的 WR-17/18 已完成合同冻结与隔离运行基础，见 [完成交接](evidence/18-refound-human-workload-runtime/README.md)；WR-19 未领取，后续实施按独立范围推进。
