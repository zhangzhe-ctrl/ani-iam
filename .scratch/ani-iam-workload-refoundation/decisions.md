# 当前有限决定与冻结输入

> 2026-09-16 用户 Goal 接受并授权 WR33：Governance 是唯一 Tenant 业务/Lifecycle owner，IAM 是身份与权限 owner；NATS + REST Snapshot/mTLS/WAT 保留，但目标合同不兼容旧 Core。I1 自有合同先交付，双方独立实现后汇合 I4。WR24 不恢复、历史证据不改写；新组合按真实连续 24h 原门槛。下表 D02/D06 的 Core 指代按该 owner 修订理解，旧实现细节不强加给 Governance。精确公开制品与待完成适配见 [CONTRACT](evidence/iam-governance-transition/CONTRACT.md)，不据合同源码宣称真实链通过。

> 2026-09-15 用户接受的 WR23 验收修订：仅本票强制启用前的观察时长改为真实连续至少 12.5 小时、至少 3000 次生命周期采样，且成功 full rebuild 后继续至少 30 分钟。p99≤5秒、10秒 heartbeat、30秒 stale、无 gap/未解释授权差异、真实 owner 链及 enforced Envoy/Session 均保持。原 24h 标为 not_verified，不宣称全天稳定性。精确授权与范围见 [当前接受记录](./evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/observation-window-authorization.json)。历史原24h文字不再作为本次WR23的时长门槛；不改写其他票的验收或历史结果。


本表区分用户需要决定的语义与实施事项应自行准备的精确输入。`pending` 不等于整张图都不能工作，只阻塞明确依赖该机制的完成；`not_frozen` 是后续事项的交付内容，不要求用户替实现者填写技术清单。

## 已确定的范围

- 用户本会话：先隔离接口测试并取得 M1；之后 Core 旧身份代码直接裁剪并开始前端对接。旧代码/UI 未开始不构成当前失败。
- 已接受 ADR：Human/Workload、Owner/Identity/Credential/Authority 分开；Tenant Lifecycle/Core 与 Access/IAM 分开；独立 owner/数据库/本地事务；Gateway→Session 首条跨服务参考链。
- 用户已完成文档整理授权，并于2026-09-10以 WR-17/18 Goal 明确授权顺序实施这两票；不自动接受原 WR-01 巨大候选机制，不扩入 WR-19 或外部部署。

## 待决定与待冻结

| ID | 状态 | 问题与推荐处理 | 在哪里完成 / 影响 |
| --- | --- | --- | --- |
| D01 | accepted | 2026-09-10 用户对“在线校验：Session Gateway 向 IAM 查询当前身份和权限；撤销能及时生效，IAM 不可用则此次调用失败”明确回复“接受”。首期 receiver 采用在线 IAM 验证，无离线放行 fallback | WR-17 冻结首链合同；WR-19 实现并验证。此选择不自动规定既有长连接的终止时限 |
| D02 | sync accepted / async accepted 2026-09-14 | 同步直接 caller 与 subject 分别验证的既定规则保持。用户已接受异步独立 NKey＋精确 ACL＋受控 reload、IAM 当前 Binding/Grant 事务复核，以及 Snapshot REST＋mTLS＋WAT；可信归因与两层撤销均必需，不承诺30秒自动断开失权 broker 连接 | 精确边界见 [接受记录](evidence/23-integrate-core-lifecycle-bootstrap/broker-snapshot-accepted.md)。WR23 从 WR32 最终四仓候选恢复，补齐薄 HTTP adapter、正式异步主链与原24h门禁；机制接受不等于实现或运行通过，不再等待同一 D02 确认 |
| D03 | WR-18 foundation frozen / WR22 first Platform administrator accepted | 可信初始 Workload、首个可登录 Platform Human 的受控引导与生命周期；必须明确环境 owner、身份/角色、凭据路径、审计、一次消费与重启恢复。不能依赖先有管理员才能创建首管理员的循环 | WR-17 准备精确方案/输入，WR-18/19/22 验证各自部分；2026-09-12 用户明确“只接受首管理员方案，单独讨论普通邀请用户的开户方式”。首管理员按 [精确接受记录](evidence/22-complete-identity-administration/bootstrap-accepted.md) 实施；普通 Invitation 开户不在该次接受范围，另按 D09 的独立接受记录。不需要先建完整外部 Lineage 系统 |
| D04 | WR-18 contract frozen / later endpoints pending | M1 endpoint/operation、错误/RetrySemantics、scope 与生成 registry/descriptor。继承有效规则；旧草案 ledger/owner-state/ephemeral 分类尚不能自动授权所有接口行为。Audit 至少 180 天可查、runtime append-only、无自动清理继续有效；超出这些规则的 retention/cleanup 候选另行接受，不推导任何删除许可 | WR-17 冻结逐接口表；后续改动限实际受影响合同与consumer，记录摘要变化，不强制无关全平台重冻 |
| D05 | WR-17/18 foundation verified / later inputs pending | 各仓库 commit/tree、镜像/生成摘要、隔离 DB/role/Redis/NATS/IdP/Workload 输入。当前 HEAD 只是本次文档现场，不作为实现默认基线 | 每个实施事项准备其精确清单；复用 CF-01 前核验归属、活动状态和版本，不修改当前环境来“凑通过” |
| D06 | REST accepted / gRPC amendment not accepted | Lifecycle Snapshot 当前有效 transport 仍为 ADR-0004 的 REST/OpenAPI，符合 ANI Core 对外/跨层既定规范；已有 gRPC Proto 不代表已实现或已接受协议变更。用户本轮询问历史原因，没有批准改成 gRPC。保留 Tenant ID/status/version/cursor 最小内容；实际合同/路径仍需冻结 | WR-17/23 按有效决定补齐真实 Core 接口并处理候选 Proto 冲突。若确有理由改用 gRPC，先提出明确 ADR 修订；不要求另拆 Core 服务或 IAM 同步逐请求回查 Core |
| D07 | deferred / not accepted | 原草案 PlatformAdministrationLineage、ReadyClaim/Receipt、PONR receipt 与长期 DR 的完整控制机制。历史问题和候选正文保留在 before/；不作为 M1 统一前置 | WR-28/29 涉及实际不可逆动作前先收敛和接受所需恢复合同，保留已接受安全要求；不默认恢复旧 Secret/凭据，也不借延期跳过恢复验证 |
| D08 | accepted | 用户于本会话明确选择“认证流程归 IAM”：IAM 拥有 OIDC flow/verifier、授权码交换、Identity 与 Session 创建；Gateway 保持固定公网回调、Cookie 和转发。该决定替代 ADR-0015 的旧 Gateway 业务 owner 分工，Cookie/CSRF/redirect 安全规则保持 | ADR-0015 已同步修订；WR-17/20 冻结并验证具体接口。首次管理员/引导的额外机制仍按 D03，不被本选择连带接受 |
| D09 | accepted | 2026-09-12 用户明确选择普通邀请新用户采用独立邮箱验证 + 密码开户；开户不建立 Membership；2026-09-13 用户另接受用途固定的密码确认入口，独立验证 Invitation 并建立首个 Membership，完成后才能普通边界登录 | WR22 按 [开户接受记录](evidence/22-complete-identity-administration/invited-account-accepted.md) 实现；首次加入按 [入口接受记录](evidence/22-complete-identity-administration/invitation-entry-accepted.md) 实现；首管理员仍按 D03 的独立 OIDC 机制，不连带新增自注册授权或账号自动合并；2026-09-13 用户进一步明确本阶段主线为平台管理员通过 Core 开通 Tenant → 邀请租户管理员 → 租户管理员邀请成员。邮箱验证与设密仅为受邀账号激活步骤，不提供开放自助注册或自行选择/创建 Tenant |
| D10 | accepted | 2026-09-13 用户明确要求参考置顶 Core 拆分讨论，Core 租户管理接口不合理时可以直接更改 | WR23 按业务 owner 职责核对并修改路径、operationId、字段与返回语义；旧 registry 和 WR22 产品基线不是保留旧接口设计的约束。按最终 owner 契约重新生成固定产物。保留本 Goal 不新建完整 tenant-service、不做 Core 物理拆分/全面重构的范围，见 [接口职责核对](evidence/23-integrate-core-lifecycle-bootstrap/core-api-refoundation-review.md) |
| D11 | accepted requirement / WR32 verified | 同类新 owner 通过受审核注册与独立 Grant 接入，不修改 IAM 手写业务代码、schema 和通用 SDK；caller、receiver 与 subject 权限分别验证。WR32 的有限同步接入门禁已通过 | 以 [WR32 完成交接](evidence/32-stabilize-workload-integration/README.md)及完整候选为固定起点；不把同步链通过外推为 WR23 异步/REST Snapshot 或24h已验证 |
| D12 | deferred by user | 2026-09-13 用户明确当前 Console/BOSS、固定 OIDC 上游限制“暂时可以接受，不是非常紧迫”，要求了解成本 | 多 Provider、Browser Client、完整 OIDC Provider 仅作分档估价，不进入本期实施或验收；不从稳定 Workload 接入要求推导建设通用 IdP |

## 后续切换的已明确产品输入（2026-09-13）

用户已明确“我会一次性完成破坏性切换，完全不管旧数据”。WR28 后续计划据此采用最终目标范围内的全新初始化与一次性替换，不建设旧身份数据迁移、双写、旧身份兼容或恢复旧数据的机制；D07 中未接受的完整 Lineage/PONR/长期 DR 平台不因这次切换自动成为前置。新系统正确初始化、受控首管理员/Workload 引导、当前授权与审计仍需验证。实际目标环境、资源清单和执行责任尚未固定，本次输入确定产品策略，不立即执行数据库、凭据、卷或流量操作，也不推定正式启用后产生的新数据可以丢弃。

用户另明确 Console/BOSS 已有页面原型，WR27 以其固定版本的页面和必要交互限定范围，不按后端 RPC 目录自行增建管理页面。“WR24 先选两个调用方接入是否可行”仍是可行性讨论；Gateway/Envoy 两入口链为审查建议，尚未替用户选定最终支持清单，也不自动缩减 C01–C14 或原 mandatory 分母。具体收口办法见 [现有收口报告的后续输入](evidence/23-integrate-core-lifecycle-bootstrap/scope-and-closeout-review.md)。这些范围输入不曾连带接受 D02；其后2026-09-14的独立接受以本表 D02 及接受记录为准。

## 处理规则

2026-09-10 WR-17 已提供 [最小合同方案](evidence/17-freeze-api-replacement-contracts/contracts.md)、[RPC/能力矩阵](evidence/17-freeze-api-replacement-contracts/rpc-matrix.md)、[精确实现工作面](evidence/17-freeze-api-replacement-contracts/implementation-scope.json)和[隔离输入](evidence/17-freeze-api-replacement-contracts/isolation-manifest.json)。用户本轮批注已接受 D01 与 D02 同步部分，原等待这两项的阻塞解除；当时 Core 异步部分没有连带接受；现已由2026-09-14的独立接受更新。D03/D04/D05 的具体准备由这些材料承载，非用户填写的技术问卷。WR-18 隔离运行基础现已验证，详见 [完成交接](evidence/18-refound-human-workload-runtime/README.md)；D03 正式引导、D04 后续端点与其他 owner 运行组合仍按负责事项冻结/验证，不由本票 pass 推导。

同一个问题一处登记，其他文件链接到这里。用户选择只更新其明确选择的项，不能连带批准其余行；确定的方案进入相应 ADR 或合同，关闭问题并记录出处。具体环境标识、版本、精确测试命令由负责事项准备，不增加不必要的用户问卷。

当前 D02 以[接受记录](evidence/23-integrate-core-lifecycle-bootstrap/broker-snapshot-accepted.md)为准。原收口报告和短 JWT 提案中的“尚待接受”保留为有日期的历史，不再阻塞恢复。首期只实现已接受的有限 NKey/ACL/reload 与 Snapshot 方案，不扩建签发、续租或 broker 控制面。

D06 核对依据：ANI `CLAUDE.md` 的 Core OpenAPI/REST 规范同时允许内部 gRPC；当前 `repo/services/ani-gateway/tenant_runtime.go` 在 Gateway 进程装配 Tenant 实现。ADR-0004 没有记录 transport 的历史比较理由，因此“REST 沿用当时 Core/Gateway 形态”仅是推断，不写成已证实原因。Snapshot Proto 的存在也不证明真实 Snapshot server 通过；本次仍为 not_verified。

D06 后续方向：结合用户关于 Core 拆分后协议的追问，建议 Core 独立时内部请求优先收敛为 gRPC（包括 IAM Snapshot），公网继续经 Gateway HTTP，Lifecycle 事实继续用 NATS。M1 仍沿用有效 REST 决定，不以 Core 物理拆分为前置。后续事项须固定 gRPC 契约、迁移实际 caller、验证行为并退出对应旧接口；这项方向建议不等于当前已批准具体 transport 切换或必须并存两套接口。

原 WR 草案中的 PONR 防重入、recovery-boot supersession、first-admin nonce 恢复和 ReadyClaim preimage 问题没有被宣称“修好了”：前三者分别只在采用对应机制时解决；首管理员最小引导中的单次消费/nonce/审计等实际安全问题仍须在它的实现前处理。移出未采用机制不等于降低正在交付路径的安全性。

历史记录：[整理前快照](evidence/16-organize-docs-and-replan/before/)；当前依赖：[ticket-plan.md](ticket-plan.md)。
