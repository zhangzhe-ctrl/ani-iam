# ANI IAM 隔离接口替换与后续系统替换事项图

Document state: WR-16 文档整理已完成；2026-09-10 用户的 WR-17/18 Goal 授权顺序执行两票。当前领取状态由事项自身及下表记录，其他后续票仍是未领取计划。

当前入口为 [规格](spec.md)、[能力矩阵](capability-matrix.md)、[决定表](decisions.md) 和本图。WR-01–15 是保留原文的 superseded 草案，已以 `wontfix` 关闭旧票版本；旧能力按下表转入新图，不表示验收通过或能力取消。Direct P2/CF-01 的历史结果只证明各自冻结输入和范围，不能直接提升为当前目标通过。

WR-16–18 均为 resolved，见 [最终交接](evidence/18-refound-human-workload-runtime/README.md)。2026-09-10 用户另以 WR-19 Goal 授权真实 Gateway→Session 首链与可复用适配包；WR-19 已 resolved，真实两 Tenant Gateway→Session exec、公共 SDK、连接授权/故障恢复及最终门禁通过，见 [WR19 最终交接](evidence/19-prove-workload-session-reference/README.md)；WR19 业务验收及用户追加的 IAM main 交付内容校验完成；当前无后续 claimed，WR-20 及之后未领取。`not_frozen` 通常是取证和固定工作，不能一概当成人工决策 blocker；具体语义决定及受影响路径见决定表。

## 阶段边界

- **M1（WR-17–25）：隔离接口替换就绪。** 正式 IAM、Gateway REST（无前端）、真实必需 owner/caller 完成 C01–C14。目标路径不调用旧 Auth、不写旧身份表；允许旧 Core 身份代码和 Console/BOSS 尚未迁接，不要求全仓 zero-reference。
- **M2（WR-26–28）：系统替换完成。** M1 之后裁剪旧身份代码并对接前端，二者概念上独立；汇合后完成整组隔离切换、适用恢复和最终系统验收。
- **后续资产退役（WR-29）：** 按精确目标、适用恢复门禁与独立人工确认清理数据/凭据/运行资产，不是 M1/M2 的暗含前置，也不代表 Production Ready。

## 当前事项

| ID | 纵向交付行为 | Blocked by | 状态 | 能力 |
| --- | --- | --- | --- | --- |
| [WR-16](issues/16-organize-docs-and-replan.md) | 整理文档、重排事项、验证与用户期望一致 | 无 | resolved | 阶段/权威路由 |
| [WR-17](issues/17-freeze-api-replacement-contracts.md) | 冻结 M1 矩阵、真实调用方、最小接口/信任决定与隔离 manifest；文档阶段不部署 | 16 | resolved | C01–C14 契约与证据要求 |
| [WR-18](issues/18-refound-human-workload-runtime.md) | 必需契约/schema/sqlc/UoW/audit 与正式 IAM runtime，空库受限 role，保留 Human 能力 | 17 | resolved | C01、C11/C12 基础、C14 |
| [WR-19](issues/19-prove-workload-session-reference.md) | Platform Workload 可信引导与真实 Gateway→Session 首条 mandatory 链 | 18 | resolved | C08，C05/C11–C14 首链 |
| [WR-20](issues/20-complete-human-authentication-notification.md) | Human Password/OIDC/link、Session/Refresh/Logout/Switch、密码动作真实 Notification+SMTP sink | 19 | needs-triage | C02–C04、C10，相关 C05/C11–C13 |
| [WR-21](issues/21-complete-tenant-workload-api-key.md) | Tenant Workload/API Key authority 与正式 Envoy | 19 | needs-triage | C07，相关 C05/C08/C11–C13 |
| [WR-22](issues/22-complete-identity-administration.md) | Access/member/role/binding/invitation/Platform 管理、admin guard、已接受恢复安全与审计查询/保留 | 20, 21 | needs-triage | C05、C06、C11，相关 C02/C03/C10/C12 |
| [WR-23](issues/23-integrate-core-lifecycle-bootstrap.md) | 真实 Core owner、NATS、投影 heartbeat/snapshot/DLQ 与 bootstrap/Invitation 纵向链 | 19, 22 | needs-triage | C09，相关 C05/C06/C08/C10–C14 |
| [WR-24](issues/24-verify-backend-callers.md) | Gateway REST 无前端、所有 mandatory 后台 caller/owner 与定向重试/并发 | 20, 21, 22, 23 | needs-triage | C13，汇合 C01–C12、C14 |
| [WR-25](issues/25-accept-isolated-api-replacement.md) | 可复现空库安装与完整接口矩阵验收，形成 M1 | 24 | needs-triage | C01–C14 |
| [WR-26](issues/26-trim-legacy-identity-code.md) | M1 后在隔离集成分支裁剪 Core/Auth 旧身份代码，不操作数据库/凭据/流量 | 25 | needs-triage | R01 |
| [WR-27](issues/27-integrate-console-boss.md) | M1 后接入 Console/BOSS 与浏览器流程 | 25 | needs-triage | R02 |
| [WR-28](issues/28-complete-system-replacement.md) | 26/27 汇合后整组隔离切换、适用恢复演练与 M2；高风险动作精确确认单列 | 26, 27 | needs-info | R03，汇合 R01/R02 与 C01–C14 |
| [WR-29](issues/29-retire-legacy-assets.md) | M2 后按精确清单与已接受恢复门禁退役旧资产/数据 | 28 | needs-info | R04 |

## 依赖与证据规则

全部依赖指向更小 ID，无环。WR-26 与 WR-27 不互为前置；图允许独立评审/规划，仍只允许一个会改变状态的事项为 `claimed`，不授权并发实施。本次 Goal 已按 WR-17 resolved → 核对精确范围/输入 → WR-18 claimed/resolved 的顺序执行；完成两票不自动授权 WR-19。

每张实施票的 commit/tree、contract/generator/registry/config/seed/scenario/镜像摘要、精确 Allowed/Forbidden paths 和隔离资源清单初始为 `not_frozen`，必须在实施前按票固定。不得用当前 `HEAD`/`main`/`latest`、旧 DP2 baseline 或他票测试环境猜填。基线变化需要检查确切差异与受影响证据，不能自动继承 pass。

fake/unit/fixture 适用于局部规则和早期接线；WR-19 首链、WR-20 OIDC/Notification、WR-21 Envoy、WR-23 Core/NATS、WR-24/25 全接口 gate 必须有对应真实 owner 与正式进程。Pod Ready、HTTP 200、提交接受或直接调用测试 helper 都不能单独证明真实业务成功。证据按 `pass`/`fail`/`not_verified` 和 static/fake/真实依赖/正式进程/真实 caller/部署/production 分级。

Core 的 Tenant 生命周期、业务元信息/Quota 继续保留；IAM 只拥有身份、访问关系和安全状态。WR-23 在隔离范围补齐必要真实 Core owner 接口，WR-26 才裁剪旧身份代码；不能把 Core 配套工作拖到 M1 后，也不能把所有 Tenant 内容搬入 IAM。

## 最小范围与决定

- D08 已接受 OIDC 流程归 IAM，Gateway 只处理固定回调/Cookie/转发。具体接口冻结属于 WR-17/20，不再把 owner 当未决项。
- D01 在线 receiver 与 D02 最小同步委托已接受；D02 异步 Core 仍待 WR-23 单独接受，不阻塞 WR-18。信任域/身份/版本等 artifact `not_frozen` 不等同语义 pending。
- WR-19 首链只带所需可信引导、身份/Grant 与 receiver 校验。已接受的首管理员可达性、last-admin、高风险恢复授权与审计安全不能移除；旧草案的完整 Lineage/PONR/外部恢复平台不再成为所有接口工作的统一前置。
- WR-22、WR-23、WR-24 是相对较大的能力汇合票：分别按管理动作、Tenant 创建纵向链、caller/场景矩阵执行有界子步骤。若精确 diff 证明需扩大范围，先保留已完成证据并拆分剩余工作、更新依赖，不能靠泛 CRUD、全仓重写或无边界恢复工程塞进同票。
- 删除、切流、Credential 失效、数据重建等动作按精确目标单独确认；文档接受、M1/M2 pass 或票号顺序不替代确认。生产容量/HA/完整故障与长期运营验证另列未来范围。

## 旧票到新票能力映射

旧票正文/评论/原基线/历史结果继续保留。下表替代旧执行顺序；已接受架构不变量的效力按规格和决定表判断，不因旧票 `wontfix` 被废弃。

| 旧票 | 保留能力与新去向 | 阶段修正 |
| --- | --- | --- |
| [WR-01](issues/01-freeze-human-workload-architecture.md) | WR-16 文档整理；WR-17 最小契约与输入冻结 | Human/Workload 核心决定保留；候选复杂恢复与 blocker 分开 |
| [WR-02](issues/02-refound-contracts-and-registries.md) | WR-17 契约/输入；WR-18 schema/runtime；WR-19/23 边界；WR-28/29 适用恢复 | 不再一次重冻所有未来恢复平台 |
| [WR-03](issues/03-rebuild-human-workload-persistence.md) | WR-18 持久化/runtime；WR-19 引导；WR-22 管理安全；WR-28/29 后期恢复 | 正式 runtime 与空库验证提前 |
| [WR-04](issues/04-deliver-tenant-workload-api-key.md) | WR-21 Tenant Workload/API Key；WR-22 Membership/Role | 首先证明一个 Platform 参考链，再补齐 Tenant 链 |
| [WR-05](issues/05-deliver-platform-workload-token.md) | WR-18 正式 runtime；WR-19 Platform 引导/首链；WR-22 首管理员/管理安全；WR-28/29 适用恢复 | 不以完整候选 Lineage/PONR 平台阻塞首条 Workload 链 |
| [WR-06](issues/06-secure-service-to-service-grpc.md) | WR-19 Gateway→Session；WR-21 Envoy；WR-24 mandatory 后台调用方 | 首链到其余 mandatory 操作逐项证明 |
| [WR-07](issues/07-deliver-core-lifecycle-workload-auth.md) | WR-23 真实 Core lifecycle/bootstrap/NATS | 真实 owner 在 M1 前，保留 Core 业务职责 |
| [WR-08](issues/08-complete-cutover-critical-callers.md) | WR-24 后台接口；WR-27 Console/BOSS | 前端移至 M1 后，接口先行 |
| [WR-09](issues/09-rehearse-isolated-cutover.md) | WR-25 隔离接口验收；WR-28 实际整组切换/恢复 | 管理/Invitation 等必需能力先于 M1；切换在 M2 |
| [WR-10](issues/10-complete-administration-recovery.md) | WR-22 身份管理/恢复安全；WR-20 Human/Notification；WR-23 邀请集成 | 替换必需管理能力移到 M1 前 |
| [WR-11](issues/11-complete-audit-idempotency.md) | WR-18 事务审计/幂等基础；WR-22 查询/保留；WR-24 重试/并发；WR-25 汇总 | 作为各接口门禁，不等切换之后再补 |
| [WR-12](issues/12-complete-callers-ui-s2s-e2e.md) | WR-24 后台 E2E；WR-26 旧代码；WR-27 前端；WR-28 系统替换 | 无前端接口验收与后续整组替换分开 |
| [WR-13](issues/13-rebuild-test-data-and-cutover.md) | WR-18/25 隔离空库安装；WR-28 实际切换；WR-29 资产/数据退役 | 空库测试不绑定数据重建/PONR/切流授权 |
| [WR-14](issues/14-delete-legacy-auth.md) | WR-26 可恢复代码裁剪；WR-29 数据/运行资产删除及适用恢复 | 不让删除后长期恢复平台阻塞 M1 或代码裁剪 |
| [WR-15](issues/15-complete-functional-acceptance.md) | WR-25 M1；WR-28 M2 | 功能替换就绪不再依赖先删旧资产 |

WR-16 文档整理与 WR-17/18 合同、隔离运行基础已完成。WR-18 实际 pass 的有限能力和三个跨项目跳过项见交接；后续完整 owner/caller、M1/M2、部署与 production 仍以负责事项实际证据为准。
