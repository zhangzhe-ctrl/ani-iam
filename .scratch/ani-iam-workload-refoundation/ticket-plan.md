# ANI IAM 隔离接口替换与后续系统替换事项图

Document state: 2026-09-16 用户 Goal 已启动 IAM/Governance I1～I4。WR24 完整 checkpoint 后释放领取为 ready-for-agent，并受 WR33 阻塞；唯一 claimed 为 WR33。A 未完成，B/C 未开始，D Envoy 保留分母；WR24 产品实施不恢复。

当前执行入口：[WR33](issues/33-integrate-governance.md)，依据 [IAM/Governance 并行实施及 WR24 恢复计划](../../docs/plans/iam-governance-parallel-transition.md)。自有合同先交付并相互复核，IAM 有限适配与 GOV-02/03 实现并行；真实联合验收后由同一 Gateway 写入方先补齐 WR24 A，再推进 B 中 GOV-04 必需的身份交互与治理路由，分别到达各自验收出口，继续 B 剩余/C/D → WR25。[WR24 checkpoint](evidence/iam-governance-transition/CHECKPOINT.md)已保存，仅 WR33 claimed。旧 Core 前置接线不再是恢复方向。

本轮执行补充：用户 Goal 授权 WR24 内按 A→B→C 完成非 Envoy 后端集成。构建、生成和定向检查在 SSH host `fedora`；用户后续明确提供真实 Kubernetes 集群，由 `ani-test-1` 承载正式服务和真实接口验证，见[执行修订](evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/execution-amendment-01.json)。本地仍仅源码编辑、必要阅读及传输。每批冻结精确范围；[本轮 claim](evidence/24-verify-backend-callers/wr24-fedora-20260915T113643Z/claim.json) 与修订共同构成执行配置。D 保留完整分母并延期，不由非 Envoy Goal 完成推导完整 WR24 resolved。

当前入口为 [规格](spec.md)、[能力矩阵](capability-matrix.md)、[决定表](decisions.md) 和本图。WR-01–15 是保留原文的 superseded 草案，已以 `wontfix` 关闭旧票版本；旧能力按下表转入新图，不表示验收通过或能力取消。Direct P2/CF-01 的历史结果只证明各自冻结输入和范围，不能直接提升为当前目标通过。

当前调整与成本见 [收口报告](evidence/23-integrate-core-lifecycle-bootstrap/scope-and-closeout-review.md)。WR32 已完成；D02 按 [接受记录](evidence/23-integrate-core-lifecycle-bootstrap/broker-snapshot-accepted.md)生效。WR23 已完成真实主链、故障恢复、用户批准的12.5小时＋重建后30分钟观察，以及enforced Envoy/Session与交接；原24h为not_verified。WR24 已领取，WR25 未领取；历史 WR23 完成交接不代表 WR24 验收。

## 阶段边界

- **M1（WR-17–25，含插入的 WR32）：隔离接口替换就绪。** 正式 IAM、Gateway REST（无前端）、真实必需 owner/caller 完成 C01–C14。目标路径不调用旧 Auth、不写旧身份表；允许旧 Core 身份代码和 Console/BOSS 尚未迁接，不要求全仓 zero-reference。
- **M2（WR-26–28）：系统替换完成。** M1 之后裁剪旧身份代码并对接前端，二者概念上独立；汇合后完成精确目标环境的新库初始化、一次性实际切换、必要失败处理和最终系统验收，不迁移或恢复旧数据。
- **后续资产退役（WR-29）：** 按精确目标、适用恢复门禁与独立人工确认清理数据/凭据/运行资产，不是 M1/M2 的暗含前置，也不代表 Production Ready。

## 当前事项

| ID | 纵向交付行为 | Blocked by | 状态 | 能力 |
| --- | --- | --- | --- | --- |
| [WR-16](issues/16-organize-docs-and-replan.md) | 整理文档、重排事项、验证与用户期望一致 | 无 | resolved | 阶段/权威路由 |
| [WR-17](issues/17-freeze-api-replacement-contracts.md) | 冻结 M1 矩阵、真实调用方、最小接口/信任决定与隔离 manifest；文档阶段不部署 | 16 | resolved | C01–C14 契约与证据要求 |
| [WR-18](issues/18-refound-human-workload-runtime.md) | 必需契约/schema/sqlc/UoW/audit 与正式 IAM runtime，空库受限 role，保留 Human 能力 | 17 | resolved | C01、C11/C12 基础、C14 |
| [WR-19](issues/19-prove-workload-session-reference.md) | Platform Workload 可信引导与真实 Gateway→Session 首条 mandatory 链 | 18 | resolved | C08，C05/C11–C14 首链 |
| [WR-20](issues/20-complete-human-authentication-notification.md) | Human Password/OIDC/link、Session/Refresh/Logout/Switch、密码动作真实 Notification+SMTP sink | 19 | resolved | C02–C04、C10，相关 C05/C11–C13 |
| [WR-21](issues/21-complete-tenant-workload-api-key.md) | Tenant Workload/API Key authority 与正式 Envoy | 19 | resolved | C07，相关 C05/C08/C11–C13 |
| [WR-22](issues/22-complete-identity-administration.md) | Access/member/role/binding/invitation/Platform 管理、admin guard、已接受恢复安全与审计查询/保留 | 20, 21 | resolved | C05、C06、C11，相关 C02/C03/C10/C12 |
| [WR-32](issues/32-stabilize-workload-integration.md) | 现有同步 Workload 机制可注册复用、移出服务名特例与既有真实链回归 | 19, 20, 21, 22 | resolved | C08/C13/C14，相关 C05/C07/C11/C12 |
| [WR-23](issues/23-integrate-core-lifecycle-bootstrap.md) | 真实 Core owner、NATS、投影 heartbeat/snapshot/DLQ 与 bootstrap/Invitation 纵向链 | 19, 22, 32 | resolved | C09，相关 C05/C06/C08/C10–C14 |
| [WR-33](issues/33-integrate-governance.md) | IAM/Governance 单一合同、有限适配、真实联合验收 I1～I4 | 20, 21, 22, 23, 32 | claimed | C09 及相关 C01/C05/C06/C08/C10–C14 |
| [WR-24](issues/24-verify-backend-callers.md) | Gateway REST 无前端、所有 mandatory 后台 caller/owner 与定向重试/并发 | 20, 21, 22, 23, 33 | ready-for-agent | C13，汇合 C01–C12、C14 |
| [WR-25](issues/25-accept-isolated-api-replacement.md) | 可复现空库安装与完整接口矩阵验收，形成 M1 | 24 | needs-triage | C01–C14 |
| [WR-26](issues/26-trim-legacy-identity-code.md) | M1 后在隔离集成分支裁剪 Core/Auth 旧身份代码，不操作数据库/凭据/流量 | 25 | needs-triage | R01 |
| [WR-27](issues/27-integrate-console-boss.md) | M1 后接入 Console/BOSS 与浏览器流程 | 25 | needs-triage | R02 |
| [WR-28](issues/28-complete-system-replacement.md) | 26/27 汇合后目标环境新库初始化、一次性实际切换与 M2；高风险动作精确确认单列 | 26, 27 | needs-info | R03，汇合 R01/R02 与 C01–C14 |
| [WR-29](issues/29-retire-legacy-assets.md) | M2 后按精确清单与已接受恢复门禁退役旧资产/数据 | 28 | needs-info | R04 |
| [WR-30](issues/30-record-api-authorization-governance.md) | 用户独立指定：固化 API 授权声明职责、规则与切换核对入口，仅 IAM 文档 | 20 | resolved | 文档规范，不改变产品证据 |

WR-30 按 2026-09-11 用户“先写入 IAM”的明确文档指令独立执行；编号不表示在 WR-29 后实施，也不改变 WR-21–29 的依赖和未领取状态。规范入口为 [API 授权声明规范](../../docs/api-authorization-contract.md)。

## 依赖与证据规则

编号是稳定标识，依赖图必须无环；本次插入 WR32 → WR23，不改历史编号。WR32 不依赖 WR23 的完成，只继承已固定候选并明确保留未验证部分。WR-26 与 WR-27 不互为前置；图允许独立评审/规划，仍只允许一个会改变状态的事项为 `claimed`，不授权并发实施。任何事项完成都不自动扩大后续实施授权。

每张实施票的 commit/tree、contract/generator/registry/config/seed/scenario/镜像摘要、精确 Allowed/Forbidden paths 和隔离资源清单初始为 `not_frozen`，必须在实施前按票固定。不得用当前 `HEAD`/`main`/`latest`、旧 DP2 baseline 或他票测试环境猜填。基线变化需要检查确切差异与受影响证据，不能自动继承 pass。

fake/unit/fixture 适用于局部规则和早期接线；WR-19 首链、WR-20 OIDC/Notification、WR-21 Envoy、WR-23 Core/NATS、WR-24/25 全接口 gate 必须有对应真实 owner 与正式进程。Pod Ready、HTTP 200、提交接受或直接调用测试 helper 都不能单独证明真实业务成功。证据按 `pass`/`fail`/`not_verified` 和 static/fake/真实依赖/正式进程/真实 caller/部署/production 分级。

当前目标由 Governance 拥有 Tenant 生命周期、业务元信息及后续套餐/Quota，IAM 拥有身份、访问关系和安全状态并维护必要生命周期投影。WR-23 的真实 Core owner 实现与证据保留为历史；新 Governance 协作须在 M1 前重新取证，不能沿用旧 Core 接线恢复 WR24，也不能把 Tenant 业务移入 IAM。WR-26 的旧身份代码裁剪边界保持。

## 最小范围与决定

- D08 已接受 OIDC 流程归 IAM，Gateway 只处理固定回调/Cookie/转发。具体接口冻结属于 WR-17/20，不再把 owner 当未决项。
- D01 在线 receiver 与 D02 同步委托保持；D02 异步与 Snapshot 身份方案于2026-09-14独立接受。凭据/权限/合同等精确输入由 WR23 恢复时固定；接受状态不等于实现通过。
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

## 历史领取与交付记录（状态以当前主表为准）

### WR20 有界交付

[WR-31](issues/31-review-and-deliver-wr20.md)：resolved。按 2026-09-11 用户独立指令审查并固定 WR20 三仓库候选；仅 IAM/Notification main 代码推送获授权。不改变 WR21–29 依赖。

WR21 于 2026-09-12 resolved：固定 WR20/31 输入上的正式 A/B/C 与受影响检查全部 pass，候选源码一致性和专有资源停止清单已核验；见 [WR21 事项](issues/21-complete-tenant-workload-api-key.md) 和 [最终证据](evidence/21-complete-tenant-workload-api-key/README.md)。没有其他 claimed，未领取 WR22，未提交/推送/发布；上文未领取表述保留为历史。

2026-09-12 WR22/WR23 Goal：WR21 完成证据与完整源码 manifest 核验通过，主表残留状态已同步为 resolved。当前仅 WR22 claimed；先完成 WR22 并固定候选检查点，再领取 WR23。原 IAM 为唯一事项权威。见 [WR22 claim](evidence/22-complete-identity-administration/claim.json)。

2026-09-13 WR22 resolved：身份管理、首管理员/已接受恢复、受邀激活与首个 Membership、真实 Notification/SMTP 及审计查询验收通过；见 [完成记录](evidence/22-complete-identity-administration/completion.json)。先固定完整三仓 WR22 候选检查点，再领取 WR23；未领取 WR24，未提交或发布。

2026-09-13 WR22 收尾纠正：遗漏 ANI 必需 make test/architecture 检查，恢复 WR22 唯一 claimed 补验；已有固定候选 bytes 与历史记录保留，WR23 尚未领取。

2026-09-13T03:58:57.444614+00:00：WR22 补齐遗漏的 ANI 必需聚合门禁并重新 resolved；采用 completion-v2.json，随后固定 candidate-checkpoint-v2.json；先前候选归档与重开历史保留。WR23 尚未 claimed，WR24 不领取。

2026-09-13T04:01:33.736537+00:00：WR22 最终 v2 检查点固定后，WR23 成为唯一 claimed；输入继承与方案/逐文件工作面准备见其 claim。WR24 不领取。

2026-09-13T12:11:32.960127+00:00 WR23分项进展：Core outbox正式读取/受控恢复/attention观察通过（PG31、HTTP14、Gateway650；45输出两次一致、29导入），见 [分项结果](evidence/23-integrate-core-lifecycle-bootstrap/core-outbox-administration-results.json)。两次run无资源残留；真实NATS/24h等整票门禁仍not_verified。WR22保持resolved，WR23仍唯一claimed，WR24未领取。

2026-09-13T13:03:59.136340+00:00：WR23 IAM原Bootstrap技术任务恢复组件完成，PG55/biz27/adapter12和69输出重复生成通过，源码与资源退出已核验；见 [结果](evidence/23-integrate-core-lifecycle-bootstrap/bootstrap-job-recovery-results.json)。正式异步链、dispatcher观察接线和24h仍not_verified，唯一claimed仍为WR23，WR24不领取。

2026-09-13T13:15:46.964228+00:00：WR23 Bootstrap wire固定向量/重复生成/descriptor不变/race/vet及ANI最小交接文档入口检查通过，见 evidence/23-integrate-core-lifecycle-bootstrap/wire-results.json。WR23保持唯一claimed；NATS/Snapshot/正式Bootstrap链及24h仍not_verified。

2026-09-13T13:33:51.369044+00:00：WR23当前候选仓库质量预检pass，ANI make test（含architecture）/doc-entrypoints与IAM root/API/SDK测试及vet均通过；严格pin/RPC清单遗漏已修复，历史WR22 registry输入保留。完整源码与两次运行退出已核验，见quality-preflight-results.json。正式异步链及24h仍not_verified，WR23保持唯一claimed。

2026-09-13T13:39:51.499832+00:00：独立wire/交接、当前候选仓库预检与后续依赖参考准备完成，所有运行已结束。连续三个Goal回合确认同一D02 async/Snapshot Credential接受条件仍缺失；详见blocking-audit.json与broker-snapshot-proposal.md。Goal等待该单独决定，WR23保持唯一claimed，正式纵向链和24h未计为pass。

2026-09-13 稳定性调整：WR23 产品增量停止并保留完整候选，状态转 needs-info；新增 WR32 同步接入纠偏前置，尚未领取。WR24–28 加入有限清单、原型和一次性切换约束。D02 仍 pending，C01–C14 分母未删；没有产品代码变更或运行验证提升。

2026-09-13T19:13:35.203272+00:00 WR32 resolved：冻结机制后新增目标与四条既有真实同步链通过；PG/NoRLS/迁移、仓库必需聚合、最终源码范围、原输入保护和专有资源停止审计完成。唯一入口为 [WR32 完成交接](evidence/32-stabilize-workload-integration/README.md)。四个独立候选未提交或发布；WR23 仍 needs-info，D02/真实 24h shadow 条件不变，没有领取后续事项。

2026-09-14 D02 接受与 WR23 恢复准备：已核验 WR32 14份完成材料及四仓3803文件；接受 NKey/精确ACL/受控reload、IAM当前权限复核与 REST＋mTLS＋WAT。WR23 转 needs-triage，尚未启动产品实施或24h。

2026-09-13T20:10:49.337387+00:00：WR23 从 WR32 最终四仓候选恢复，D02/3803文件/HEAD/模式/库存核验 pass；当前唯一 claimed。见 [恢复入口](evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/README.md)。WR24/25未领取。

2026-09-14T03:50:14.590005+00:00 WR23 DLQ 授权修订：在唯一 claimed23 内实施三入口同步原文重新接收；scope32 与审计/幂等/当前 Human 和 Broker 权限边界见 [授权记录](evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/dlq-transaction-authorization.md)。实现验证中；Envoy/Session 按用户指示等待新环境，真实24h与强制启用未通过，WR24/25 未领取。

2026-09-14T05:14:31.363230+00:00：DLQ 有限修订及受影响门禁完成：三入口事务内重收、23项正式验证、schema007受限PG/NoRLS与race/vet、实际A、原Bootstrap恢复和聚合补验均有pass；四仓3898文件开发检查点已核验。见 [DLQ 完成](evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/dlq-completion.json)。保留一次Gateway启动前失败及未确认根因，同源码独立复验通过；未改写失败记录。Envoy/Session继续等待用户新环境，真实24h/强制启用/最终交接仍not_verified；WR23仍唯一claimed，WR24/25未领取，未提交或发布。

2026-09-14T18:25:35.243560+00:00：用户提供 ani-test-1 新环境后恢复 WR23；真实 Envoy/A、NATS撤权与完整 Session/Kubernetes exec 回归已通过，专有场景资源清理pass。四仓3900文件最终候选已冻结，manifest `270bc158672b18771f879988f9ff9b50b1bebc3185498c3c2c5540cce2afab30`；最终固定候选完整门禁正在执行，真实24h/强制启用/最终交接尚未完成。WR23保持唯一claimed，未领取WR24/25、未提交或发布。见 [当前恢复入口](evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/README.md)。

2026-09-14T19:35Z 续接检查点：最终3900文件候选manifest `270bc158672b18771f879988f9ff9b50b1bebc3185498c3c2c5540cce2afab30` 保持不变；前11正式阶段全部pass。原聚合18/19通过，缺Node导致ANI服务检查fail；任务目录补齐官方Node18.19.1后，固定定向入口补验pass，19项按同一完整源码与实际退出码合并，原失败run保留。正式24h run `wr23-resume-20260914T193100Z-9a8de002` 已于UTC `2026-09-14T19:33:43.390800577Z` 开始，最早UTC2026-09-15同一时刻满窗口。当前观察、强制启用及最终交接仍not_verified；WR23保持唯一claimed，WR24/25未领取。当前证据入口：`evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/README.md`。

2026-09-15T08:02:56.700878+00:00：用户明确批准 WR23 观察门槛改为真实 >=12.5 小时且成功 full rebuild 后继续 >=30 分钟；其余权限、传播、缺口及 enforced Envoy/Session 门槛不变。原24h保留 not_verified，不改写为pass；scope49仅修改验收工具/测试与说明，产品运行行为保持冻结。授权见 evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/observation-window-authorization.json。WR23仍唯一claimed。

2026-09-15T08:31:37.980643+00:00：WR23 resolved，全部本次获批必需门禁与最终源码/资源交接pass。观察时长按用户明确接受修订，原24h不标为pass。见 [完成交接](evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/HANDOFF.md)。WR24/25未领取，四仓候选未提交或发布。
