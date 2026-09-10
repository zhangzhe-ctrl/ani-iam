# ANI Human / Workload Principal 模块设计

> 文档角色：ADR-0022 已接受模型的模块边界，以及尚待接受的必要接口选择；不是实施计划或当前事项图。
>
> 当前执行入口：[WR spec](../../.scratch/ani-iam-workload-refoundation/spec.md) + [ticket graph](../../.scratch/ani-iam-workload-refoundation/ticket-plan.md)。替换分母见[能力矩阵](../../.scratch/ani-iam-workload-refoundation/capability-matrix.md)，接受状态见[decisions](../../.scratch/ani-iam-workload-refoundation/decisions.md)。时间、状态、事务、安全与恢复规则以[base design](plan-iam-service-refactor.md) 和 accepted ADR 为准。
>
> 2026-09-09 文档整理：保留 Human/Workload 和必要接口边界，将原长篇 receiver、lineage、ReadyClaim、PONR、DR 等候选机制收为非规范历史草案。完整原文见[WR-16 before 快照](../../.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/before/docs/plans/plan-workload-principal-refoundation.md)。整理不授权产品实现或改变任何外部状态。

## 1. 已接受模型与尚未接受的机制

[ADR-0022](../adr/0022-unify-software-actors-as-workload-principals.md) 接受 Principal 只有 `human | workload`，并只替代 ADR-0018 的主体分类。Tenant Access、Lifecycle projection、Bootstrap operation 与 purpose-specific Notification outbox 等既有决定仍有效。用户接受先在隔离环境证明 API 可替换旧 auth-service，再推进 Core 裁剪和前端接入；这不缩减完整替换能力。

以下七个轴必须分开：

| 轴 | 负责表达 |
| --- | --- |
| Principal type | 自然人或稳定的软件 actor |
| Owner | 谁管理该 Workload：Tenant 或 Platform |
| Workload Identity | 经 trust domain 验证的运行时标识 |
| Identity Binding | 已验证标识与稳定 Principal 的关系 |
| Credential | 本次用于证明身份的载体 |
| Authority | 当前允许执行的目标操作与边界 |
| Execution Context | 本跳直接 caller、目标及可选被服务主体 |

已接受的模型不能被某种 Credential、Pod/Replica 数量或某次调用改变。具体 WAT receiver、非固定 Tenant proof、trust provisioning、first-admin 与 administration recovery 不是由这七个轴自动推导出的 accepted 协议；其状态必须在 decisions 中明确，未接受路径不可执行。

## 2. Principal 与 owner-specific Authority

### 2.1 共同身份规则

- Human 是全局自然人，可有多个 Tenant Membership 和可选 Platform Membership；只有 Human 建立 IAM 登录 Session。
- Workload 是稳定软件 actor，不是 Pod、Deployment、ServiceAccount、证书、API Key、Token，也不是 Core 的业务工作负载或资源实例。
- 稳定 Principal 按独立安全职责和撤销生命周期划分。相同边界的 producer/consumer/executor 可复用 Principal 并保留各自 Binding/Grant；可独立撤销的职责使用不同 Principal。Replica、rollout 和证书轮换复用稳定身份。
- Canonical name 为非空 `lower(btrim(name))`，不可变；Tenant-owned 在 exact Tenant 内唯一，Platform-owned 在当前 IAM environment/trust domain 内唯一。Disabled 不释放名称。可变展示 label 若有需要，须独立且无授权含义。
- Principal type、Workload owner type 和 Tenant owner ID 不可原地改变。转移需要新 Principal、显式重建授权意图，并 disable/revoke 旧主体和 Credential，不能原地把 Membership Authority 换成 Grant Authority。
- 目标 v1 只接受 `HUMAN = 1` 与 `WORKLOAD = 3`；原 `SERVICE` 名称和值 2 reserved，不是 alias 或兼容分支。

### 2.2 Tenant-owned Workload

Tenant-owned Workload 固定属于一个 Core-generated Tenant。Active Principal 恰有一个匹配 owner Tenant 的 current non-removed Membership，状态可为 active 或 suspended；disabled 最多一个，Membership removal 终态为零。只有 active Membership 和有效 Role Bindings 提供 Authority。Suspension 暂停 Authority，不自动改变身份或撤销 Key。

创建 Principal、owner/profile、初始 active Membership、Role Bindings 和 Audit 在 IAM 同一 UoW 提交。移除唯一 current Membership 时 disable Workload 并不可逆 revoke 全部 API Key。Enable 只允许匹配 owner 的 non-removed Membership，或同事务建立新 Membership identity；不复活 removed Membership 或 revoked Key。数据库约束必须保护数量与 Tenant 一致性。

API Key 是首期 Credential，权限来自该 Workload 当前 Membership/Role，不来自 Human 创建者，也不复制 permission snapshot。Tenant-owned Workload 无 Platform Membership、无 Workload Grant，不计入 last-human-tenant-admin。未来 SPIFFE/WAT 需单独接受 trust policy，不能改变 Authority 来源。

### 2.3 Platform-owned Workload

Gateway、IAM、Session Gateway、Inference、Worker 等平台软件使用 Platform-owned Workload。它们无 Tenant/Platform Membership，以受信任 Identity Binding 识别稳定主体，以 owner-specific Workload Authority 约束 exact target；request 的 Tenant ID 不能创造权限，空 Tenant 不能表示 wildcard。

目标短 WAT 只在已验证 Workload Identity 和当前 Authority 下签发，限定 audience/operation，最长 5 分钟，无 Session/Refresh；密钥、有效期与撤销遵守 base design。Tenant-owned API Key 不能作为内部平台 caller 的 fallback。D01 在线 receiver 与 D02 同步最小委托已由用户接受，具体首链按当前合同；异步 evidence、其他 peer/token profile 和未用 scope 仍待各自合同，不把候选字段表等同 accepted schema。

## 3. 必须隐藏复杂性的模块

模块对外提供领域操作，内部承担状态、约束与故障语义；调用方不学习 persistence、SPIFFE 解析或多套 owner 分支。以下是设计语义，具体 Go/Proto 签名在唯一当前合同事项中冻结。

| 模块 | 窄边界 | 内部承担 |
| --- | --- | --- |
| Workload Authentication | 已验证 peer/Credential → 稳定 Workload identity | Credential 验证、Identity Binding、当前 Principal 状态、身份与证明载体分离 |
| Workload Authorization | identity + exact target + 所需可信 evidence → decision | Tenant Membership/Role 或 Platform Grant、owner 限制、operation/Tenant/audience 与当前 Authority |
| Credential Issuer | 已验证 mint authorization → 短期 Credential | Key Ring、claims profile、签名、TTL、Secret 处理与必需 Audit |
| Invocation adapters | 固定目标调用；入站验证后产生 typed invocation | HTTP/gRPC carrier、认证与授权接线、目标映射、必要 cache/rotation、错误转换 |

Authentication 不返回一个可由调用方任意拼权限的结构；Authorization 不接受自报 Tenant/subject 当作可信证据；Issuer 不绕过 Authorization 自行选择或扩大 Authority。消费方不必知道 Tenant-owned 和 Platform-owned 的数据库差异，也不把“Service vs internal”分支复制到每个 Handler。

IAM 的 `biz` 保持纯领域模型与窄 repo interface，不 import Proto、Kratos、transport、data 或 driver。`service` 只做 DTO/DO、输入校验和稳定错误映射；`data` 隐藏 sqlc/pgx、持久 shape、driver errors 与显式 UoW；`cmd/server` 是唯一 composition root。可信 peer 在 transport 边界验证后转换为领域输入，不能把 HTTP/gRPC 对象传入 biz。

保留可替换的 clock/ID、Credential verifier、Key source、identity/authority repository、target registry 和显式 UoW seams。真实 adapter 的语义由集成证据证明；fake 可用于早期单测，不能成为正式接口就绪证据。不为方便测试把所有模块并入一个万能 repository 或将 storage client 暴露给业务。

### 3.1 跨服务复用边界

共享认证/调用语义应以独立、版本化边界供 ANI 与独立服务消费，不 import ANI Core bootstrap 或内部业务包。gRPC 与 HTTP adapter 共用相同目标与可信身份语义，不能各写一套 Token/Header/TLS 规则。

Target 由固定 registry 将 full method 或 HTTP method + canonical route template 映射到 audience/operation。原始 URL、path 参数或调用方自由字符串不能创造 operation。认证 middleware 负责建立可信上下文；资源 Handler 仍负责真实 owner/Tenant guard，不能因已有 IAM allow 就跳过。

首期 Receiver 已接受在线查询 IAM；IAM 自身通过 in-process 领域接口避免向自己做递归鉴权 RPC。独立版本化共享边界的具体发行方式、接口集合与 streaming recheck 数值在实际使用前冻结，不能从历史样例签名自动接受；不能引入匿名、静态 Token、dev Header 或共享 Secret fallback。

首条实际调用链必须交付可复用的调用方/接收方 Adapter 与最小可运行接入示例。首期实现实际使用的 gRPC，不预建完整多语言、多协议 SDK。Adapter 隐藏凭据获取/更新、TLS 与在线验证、委托目标/请求绑定和稳定错误；调用方不手工组装身份 Header，接收方不重复查询 Grant 或解析 Token。新增服务只提供 owner 声明的 operation/permission、固定方法映射与资源字段提取，并保留真实资源归属/状态检查及业务幂等。部署 owner 仍负责配置受信身份和明确 Grant，不能隐藏为默认全权。

验收看实际 Handler 的接入工作量和后续服务是否复用同一实现：生成 Proto client 或专用单服务封装不算完成；新服务仍需复制认证规则则不通过。2026-09-10 当前代码只有静态 Gateway mTLS 白名单和 Notification 专用客户端等局部封装，公共 invocation Adapter 尚未实现，不能将本节目标描述为现状。

## 4. 每跳调用、异步事实与失败语义

### 4.1 直接 caller 与被服务主体

每个同步业务 hop 必须认证当前直接 caller 为 Workload，并独立授权 exact target；可选 Human/Tenant Workload 被服务主体不代替 caller。Forwarded user Token、普通 Header、request body、Namespace 或 NetworkPolicy 均不足以证明 Workload Identity。上一跳 context 不能原样传播成下一跳 Authority。

Typed invocation 分离 DirectCaller、InvocationTarget 和 optional DelegatedSubject；是否及如何携带 proof 由 accepted 合同决定。Audit 区分当前执行 caller 与原始业务 actor，不能合并成含混的 `principal_id`。认证失败和 pre-trust seed 仅可用受限非 Principal provenance；这些变体无 Authority，其精确 wire shape 仍需冻结。

Gateway→Session Gateway 是 ADR-0022 的首条 mandatory 跨服务参考链，可以与早期正式 runtime 验证归一；后续独立 caller 复用其已验证模式。Session Gateway 的资源连接 Session 与 IAM Human 登录 Session 是不同领域概念。具体执行顺序与所有 caller 的最终回归只维护在当前 spec/graph，不在本文复制。

### 4.2 Core/NATS 与 Notification

Core 拥有 Tenant Lifecycle、Tenant ID 与本地 outbox；IAM 拥有 TenantAccess、成员权限、Bootstrap operation 与自身 purpose-specific Notification outbox。各 owner 本地事务原子，跨服务 at least once，保留 event/operation ID、fingerprint、version/CAS、DLQ 与恢复规则，不引入跨库事务或双写。

异步消费不能把发布方 TLS peer 透明传播给订阅方。Producer/consumer 身份和授权必须独立受约束，消息 Tenant/producer 字段本身不是证明。具体 broker provenance、subject registration、ACL 与 retained-message revoke 模型须在真实消息链之前接受；不因此阻断无依赖的接口，也不准绕过认证去交付消息链。

IAM 决定密码动作或邀请通知的业务意图、对象与模板参数，Notification 接受自身 scope/intent/producer 契约后负责投递和状态。IAM→Notification 自主调用可覆盖真实投递，但不能替代 ADR-0022 的 Gateway→Session 委托参考链或 Core→IAM 的真实 owner 证据。

### 4.3 稳定失败与重试

Base design 的稳定 gRPC/public error、500ms Gateway deadline、无自动重试和各状态机规则继续有效。无效 Credential 映射 401；已认证但 Authority 不足映射 403；依赖/registry 不可用 fail closed 为 503；deadline 为 504。具体 Workload reason、peer/evidence mismatch、尚未接受 scope 的失败分类在合同中逐项冻结，不能用历史候选 reason 冒充已发布契约。

TLS 握手前失败只保证接收侧脱敏日志/指标，不能承诺尚未到达 IAM application 的 domain Audit。到达业务边界后的必需 Audit 与变更按 owner 事务规则处理，禁止记录 raw Token、Key、私钥、任意请求体或邮件正文。

每个有副作用 endpoint 必须说明响应丢失、重放、冲突和终态，不能统一套“可重试”。保留 24 小时 ledger、Refresh reuse、OIDC one-time、一次性 API Key、Invitation/last-admin 并发和消息去重等 base 合同；尚未接受的 retry-mode catalog 只是一种组织方式。Streaming 的终止、续期和撤销检查须在对应接口启用前冻结，不把历史 30 秒 recheck 样例当 accepted 数值。

## 5. 待决定项与非规范历史草案

OIDC ownership 已在 decisions D08 / ADR-0015 接受：IAM 统一拥有 flow、verifier、Provider code exchange、Identity 与 Session，Gateway 仅固定 callback/Cookie 转发。相关 M1 链按此职责验证；first-admin 的具体方案/输入仍按 D03 为 not_frozen，不从 OIDC owner 选择推导接受额外机制。

Pending 只阻断依赖该选择的合同、接口或动作；不能一律阻断全部 M1，也不能用“以后再定”绕过当前链必要安全语义。选择由 decisions 记录 accepted 引用，本文不代替用户接受。

| 必要选择 | 当前边界 |
| --- | --- |
| WAT receiver 与必要例外 | D01 已接受首期在线 IAM 验证，无离线放行 fallback；其他 profile/例外仍按具体合同 |
| 非固定 Tenant scope 与可信同步/异步 evidence | D02 同步最小委托已接受，绑定独立 caller、主体、Tenant、资源和操作；异步 Core 仍 pending，二者不能互相 fallback |
| Platform trust bootstrap/provisioning 与 first-admin | 无第三种 Principal、无 runtime 自注册；具体机制 pending |
| Core Snapshot transport | D06：继续采用 accepted REST/OpenAPI；现有 gRPC Proto 是未接受变更，真实 owner 链前处理差异，不以代码反推接受 |
| 安全状态变更的完整 retry/response-loss catalog | 既有行为保留；新增枚举及逐 endpoint 分类待冻结 |
| administration recovery、切换/删除恢复机制 | 先满足相应动作已有 accepted 安全要求，未接受扩展不作全部接口前置 |

以下内容完整保存在本文顶部链接的 before 快照，**不是规范，不构成实施授权或默认值**：

| 历史段落 | 收存内容与后续使用方式 |
| --- | --- |
| §3.3–4.4 | 精确 Grant Scope × Evidence Mode、peer anchors、DelegationReceipt mint/exchange、stream recheck 和例外协议；必要部分由当前合同事项收敛 |
| §4.5、§6–8、§11 | Platform Trust Seed 两阶段、外部 Lineage Registry、ReadyClaim/Receipt、Reporter、first-admin flow、RecoveryActivation、CommitPONR/RequireForwardRecovery 等大状态机；只在相应能力/动作确有需要并获接受后纳入 |
| §9.4、§10 | Broker binding/subject-owner registration、Grant→ACL projection、撤销后 retained-message 与 recovery actor 的详细规则；在真实消息链合同中处理 |
| §12–13 | WR-01–15 旧候选执行图与原 supersession；旧事项/evidence 原位保留，当前图只见 spec/graph |
| §14 | Cutover-window、持续 long-term DR package 与旧删除排序；需按真实保护动作重定边界，不能作为 M1 统一前置 |

收存完整草案不删除任何已接受恢复安全要求。Recovery Bootstrap 的独立权限、不同 requester/approver、单次 approval、绑定目标/payload、一小时有效、执行前 15 分钟内重认证与审计继续有效；snapshot/恢复演练和删除前恢复要求仍须在其保护动作前满足。未演练的历史 snapshot 不因本次整理恢复可用，Credential 失效也不能靠回灌旧 key/trust 绕过。

## 6. 接口替换与实际替换

M1 由 capability matrix 要求的正式进程、真实依赖、真实 owner 和全部必要 caller 证明 API replacement ready；黑盒验证 Cookie/CSRF/OIDC/重试，不等待 UI。M1 后 Core 旧身份裁剪与前端对接各按后续事项推进，不虚构彼此前置。M2 再证明旧 writer 退出与真实接入、替换完成；实际切流、Credential 失效与不可恢复删除仍须独立精确范围和执行前确认。

本文不维护票号、并行路线或新的“设计完成”门槛。完整能力不因缩小单票而消失，高级候选扩展也不自动进入 M1；争议先在 capability matrix/decisions 显式解决。历史 pass、当前静态事实、fake 验证和正式动态 pass 分开记录，缺证据就是 not_verified。
