# WR-17 最小合同与 WR-18 实现边界

状态：模型按 ADR-0022 实施；2026-09-10 用户已接受 D01 在线验证与 D02 最小同步委托，出处见 decisions.md。下文将其收敛为首链所需规则；Core 异步方案仍待 WR-23 单独接受。本文不声明 WR-19/22 引导或业务链已经完成。

## 1. Breaking 合同的有限改动

| 输入 | 目标 | 保留/禁止 |
| --- | --- | --- |
| PrincipalType SERVICE=2 | reserved 名称和值2；仅 HUMAN=1 / WORKLOAD=3 | 不接受2、不映射、不建兼容分支 |
| ServicePrincipal 及四个 CRUD RPC/message | TenantWorkload、Create/Get/List/UpdateTenantWorkload | Tenant 专属入口，不允许客户端选择 Platform owner |
| UpdateServicePrincipalRequest.name=3 | 新 UpdateTenantWorkloadRequest 中 reserved 3 / name | canonical name 创建时 lower(btrim)，不可更新；disabled 不释放名字 |
| AuthnMethod SERVICE_TOKEN=4 | reserved 4/旧名称，WORKLOAD_TOKEN=5 | 不是 Wire alias；已有效 Human/API Key 数值保留 |
| IssueServiceToken | IssueWorkloadToken | 新名称只登记到 WR-19，不在 WR-18 暴露可用 mint；旧 tenant_id/idempotency_key 不变成自由 Authority |
| CreateAPIKeyResponse | 保留 api_key=1 / api_key_secret=2；增加 replayed=3 | 第一次成功揭示 Secret；重放只返回原 Key 元数据，secret 为空且 replayed=true |
| 已有 Admin Request.credential | 从当前未使用字段接入 IAM 验证 | 不增加第二份 Header Credential；不再用 x-ani-principal/tenant/authn/decision 建可信 actor |
| operation/permission registry | 当前 ANI 公共源的不可变输入 + IAM 目标候选副本 | 仅 ServicePrincipal→TenantWorkload、service/workload credential/type 与 IAM resource 对应变换；资源 operation 的业务含义和 owner 不改 |

保持三个 gRPC service 的职责；不引入第四个全能管理服务。已有未来入口仍是明确 Unimplemented/不可用；更新合同测试的精确 inventory/descriptor/hash。Buf breaking 对固定旧目标的差异属于 ADR-0022 允许的 breaking，必须逐项列出，而不是宣称检查无差异。

## 2. Human/Workload 持久化与模块

### 2.1 身份与 owner

`principals` 仅 human/workload、active/disabled；type 不可原地更新。Human 的 verified_emails、identities、password_credentials、sessions 保留；增加约束拒绝 Workload 混入 Human 身份/Session。已有 OIDC flow/PKCE/exchange、密码动作 outbox 保留。

`workload_principals` 的独立 owner 为 tenant/platform，持有 principal_id、owner_type、环境/信任域、immutable canonical name、version/timestamps。Tenant owner 含非空 Core Tenant ID；Platform owner 无 Tenant ID。必须有严格 owner CHECK，空 Tenant 不表示跨 Tenant 权限。Tenant 当前 Membership 用可为空的独立引用表达终态：active Workload 恰好一个匹配 owner Tenant 的 active/suspended Membership；disabled 最多一个；removal 终态引用为空，不复活旧行。

Tenant name 唯一于 owner Tenant；Platform name 唯一于 environment/trust domain；disabled 同样保留唯一键。owner、owner Tenant、canonical name 均由数据库与业务验证保护。保持复合 Tenant FK；新的 current Membership 创建也需同事务更新 profile、Audit。

API Key 保留 tenant_id + principal_id 复合 FK，仅连接 Tenant-owned Workload；只保存 digest/prefix/expiry/revocation/version。Membership suspension 拒绝鉴权但不撤销 Key；Membership removal 原子 disable Workload、撤销所有 Key、移除 current 引用、写 Audit。Enable 不复活旧 Key 或 removed Membership。

### 2.2 Platform 基础，不提前交付管理平台

`workload_identity_bindings` 独立记录 Workload Principal、environment/trust domain、受验证的身份标识（首期 x509 DNS）、status/version；不保存私钥或证书 Secret。标识映射唯一，Principal disable、Binding revoke 都使后续认证失败。WR-19 再固定跨服务所用身份凭据 profile，不能从这里自动推导 SPIFFE 或其他认证方式已通过。

`workload_grants` 独立关联 Platform-owned Workload、exact audience、exact operation、明确 scope、status/version。WR-18 仅支持 IAM 自身 Gateway ingress 的独立 direct-caller 权限；它允许调用既定 IAM 方法，**不授予被服务 Human 的业务权限**。Tenant-owned Workload 不能拥有 Grant，Platform Workload 不能拥有任何 Membership。非本阶段 scope 不能执行。

Platform 的身份与 Grant 在新库由 owner-controlled fixture 提供，运行账号只读其信任配置；业务写入继续由受限运行角色完成。这里的“只读”必须同时覆盖共用 principals/workload_principals 中的 Platform 行：runtime 对 Binding/Grant 表无写权限，对 Platform profile/Principal 的直接 INSERT/UPDATE/DELETE 必须被不可由 runtime 关闭的数据库约束/触发器拒绝；不能只限制新表却通过 principals.status 重新启用已禁用身份。受限角色不得是表/函数 owner、SUPERUSER、CREATEROLE 或具备更改 session_replication_role 的权限。真实 PG 负向测试直接执行这些 SQL 并断言无状态变更；不以 HTTP 拒绝替代 DB 权限证明。fixture 不是正式 trust bootstrap，不计 WR-19 初始可信身份验收。

Workload Authentication 的业务接口接受 transport 已验证身份的纯领域值，查当前 Binding/Principal。Workload Authorization 接受该身份、固定 registry 解析的目标，查当前 Grant。transport 验证证书链/EKU/TLS 后才创建输入；biz 不 import gRPC/Proto/x509/pgx。cmd/server 显式装配模块，不使用 Wire。

### 2.3 Audit 与 durable 幂等

本阶段管理 Audit 分别记录业务 actor（经 IAM 验证的 Human）和可选直接 Workload caller；caller 不能替代 actor。增加受约束的 caller Principal/Binding 引用及非敏感版本；身份失败沿用受限非 Principal 归因，不创建 anonymous/seed Principal。runtime Audit 只 SELECT/INSERT，Audit 插入失败整体回滚；不记录 raw credential、任意 Header、请求体或邮件。

仅为本阶段已可用管理 mutation 实施 scoped ledger：Create/UpdateTenantWorkload、Create/RevokeAPIKey、Update/RemoveTenantMembership、Bind/UnbindTenantRole。底层 UpdateAccess 保留相同事务扩展以供 WR-22，当前正式入口禁用。

ledger 唯一键为 Tenant + 业务 actor + operation + idempotency_key；intent hash 由规范化业务字段计算，排除 Secret、Bearer Token、request/correlation/decision IDs，包含目标、版本、状态、排序去重 role IDs。直接 caller 也绑定在 intent/provenance 中，不能跨 caller 偷用结果。重复必须先重新认证当前 caller/actor，并验证当前操作权限。

其中 intent 绑定的是稳定 caller Principal ID；证书、Binding/Grant version 和本次认证时间只作当次验证与 provenance，不参与业务 intent hash。正常证书轮换不能制造另一个业务意图，也不能靠命中旧 ledger 绕过已撤销的 caller 或 actor。

同 key 同 intent 在首次提交后24h内返回保存的 non-secret result，不增加业务写入或成功 Audit；同 key 不同 intent 返回 IDEMPOTENCY_CONFLICT；到期返回 IDEMPOTENCY_KEY_EXPIRED，不把物理保留当延期。先锁定/占有 ledger key，再作业务、Audit、non-secret outcome；三者同一 PG 事务，失败无占位残留，竞争者等待唯一结果。读取结果必须是严格目标结果类型，不能反序列化任意业务对象或 Secret。

CreateAPIKey 响应丢失：同 key 重试返回原 Key 的 non-secret 元数据与 replayed=true，api_key_secret 为空；不会自动新建 Key、解密旧 Secret或重发同 Secret。需要可用 Secret 的操作者可显式 revoke 该 Key，再以新 idempotency key 新建。响应中的 replayed 使调用方可区分首次揭示与已经提交的结果；这一规则及其失败路径必须用真实 DB/正式 RPC 验证。

## 3. 正式 IAM 入口和授权

隔离 profile 改为 `workload-isolated`；Runtime 显式配置 environment 与 trust_domain（新增字段7/8），不从请求、Tenant ID 或 DNS 猜环境。继续保留 loopback listener/依赖限制、显式 Secret 文件/环境引用、TLS 客户端链验证。启动时验证受限角色和预期 schema revision/所需表约束后才 Ready，不自动迁移；DB/Redis 依赖失效时 readiness 退出，恢复后按检查结果恢复；shutdown 先撤 readiness 再停止 worker/listener/连接。WR-18 不把 schema 检查改成授予 owner 权限或放宽 TLS。

| RPC 类别 | 直接 caller | 独立业务验证 |
| --- | --- | --- |
| Password/OIDC/Refresh/Logout/Switch | 已验证 Gateway mTLS peer→当前 Platform Workload Binding→exact IAM RPC Grant | 原 Password/OIDC/Refresh/Session 状态机处理 request credential；无任意 Tenant/主体 Header 信任 |
| ValidatePrincipal/CheckPermission | 同上 | 原 IAM credential/Session/Membership/Role/policy 检查；Workload 类型按新模型 |
| 15 个可用 Tenant Admin RPC | 同上 | Request.credential 在 IAM 当前权限评估器验证；精确 operation 来自 handler，scope/actor 来自验证结果；目标 Tenant 必须匹配 |
| Get/UpdateTenantAccess | 无可用放行记录 | 当前所需 Platform Human 授权未实现，显式不可用至 WR-22；不得改成 Tenant 管理员操作 |
| 剩余未实现声明 | 无可用放行记录 | 不采用通配允许，不返回空成功 |
| admin HTTP health/readiness/metrics | 仅 VM loopback 管理监听 | 不承载业务、不允许它设置 caller/subject |
| gRPC health Check | 固定 mTLS 管理身份或已绑定 Gateway；exact method | 只健康检查，不派生业务权限 |

Admin 列表/读取也要授权；不能只检查写接口。请求头 request/correlation IDs 可作非 Authority 的日志关联，Audit subject/method/decision 取实际 IAM 验证结果。TLS/Binding/Grant 在 server→biz 窄接口，Admin 业务 credential 验证在 service→biz；禁止 service import data。IAM 对自己的 Workload 校验使用 in-process 模块，不递归调用自己的 gRPC。

## 4. 首条 Gateway→Session 合同（D01 与 D02 同步部分已接受）

固定目标：`/ani.session.v1.SessionService/CreateSession`，audience `ani-session-gateway`，target operation `session.create`。资源域仍保留 Target.instance_id/workload_name/kind、exec/console 模式、资源 owner/Tenant 真实核验、连接 ticket/store/idempotency。人登录 Session 与资源连接 Session 是不同状态机。

两条现有入口不能合并成可自由替换的授权：

| 已固定 ANI source operation | HTTP route | source permission | 允许的 Session mode/资源 |
| --- | --- | --- | --- |
| createInstanceExecSession | POST /instances/{instance_id}/exec | tenant / instances:create | exec；Container/GPUContainer/Sandbox |
| createInstanceConsoleSession | POST /instances/{instance_id}/console | tenant / instances:create | vm_console；VM；已选定 serial/VNC 协议 |

来源为 ANI `50aa9fe2099b7ff4c276f883939a8d26c9d9eff8` 的 `repo/api/openapi/operation-registry.v1.json` 与 `repo/services/ani-gateway/internal/router/instances.go:2070,2142`。这里记录现有 owner 声明的 permission，不凭名称猜成 instances:execute，也不在 IAM 私自修改资源 owner 策略。首链必须同时验证 source operation 的主体权限与 target operation 的 caller Grant；listInstances、任意 decision ID 或同为 instances:create 的其他 source operation 不足以 mint 这条调用的委托。凭证同时绑定 source operation、target operation 和 mode，exec/console 互换必须拒绝。

1. Gateway 是直接 Platform Workload，完成自己的 peer 认证并具备该 exact target Grant。它在 IAM 入口决策中认证 Human/API Key 业务主体。
2. IAM 为具体调用签发短期委托凭证，绑定 Gateway Principal/Binding、已验证主体及当前 Session/Grant（或 API Key/Membership）、单 Tenant、目标 audience/operation、目标请求摘要、有效期。目标请求摘要覆盖 request/idempotency/资源/模式/exec 语义字段，按冻结 canonical encoding 计算；不能借同一 receipt 修改资源、命令或参数。
3. Gateway 取得短期 WAT，最长5分钟、无 Session/Refresh；WAT 绑定 caller 当前身份锚、audience/operation/Grant version。委托最长不超所依赖 credential/Grant 的有效期；初始 TTL 60s，上限不被 caller 改大。
4. Session 本地校验真实直连 mTLS peer，向 IAM 在线验证 WAT/委托。在线请求自身由 Session 的 mTLS 身份和 exact verification Grant 认证；IAM 只接受其注册 audience，不能把请求体 receiver/caller 字段当独立凭证。Session 报送的 observed peer 锚只在这个受信 receiver 边界内有效，必须与 WAT caller anchor 一致。IAM 检查当前 Binding/Principal/Grant、subject 当前权限和请求摘要，返回 typed invocation。
5. Session 用 typed Tenant/Subject 核验 request Principal 一致性及真实资源归属；自报不同 Tenant/Subject、目标/模式篡改、wrong audience/peer/operation、撤销、过期均拒绝。origin_decision_id 仅关联审计，不是凭证。
6. Session 创建的业务去重继续由 Session owner 完成；委托凭证不能宣称 exactly once。IAM 不可用时503拒绝；重试需重建当前有效证明，不能重放旧请求把权限扩大；无自动业务 retry/fallback。相同意图响应丢失仍由 Session 的幂等记录返回原结果。

具体负向用例：有读取但无创建权限的 Human 不能凭 listInstances 决策 mint 委托；console receipt 不能用于 exec；替换 instance_id/workload_name/command/TTY/rows/cols/protocol/subject/Tenant/idempotency_key 均使目标摘要失配；新 request_id 的重试需重建 receipt，Session 的业务幂等键与意图仍按其 owner 合同判断。Gateway 先完成默认 command、terminal dimensions、protocol 的规范化，再计算摘要；receiver 比对最终 DTO，不能各自对不同的默认值签名或验证。

WR-19 实施新的 mint/verification DTO、固定 target registry、真实 caller/receiver adapter 与上述负向矩阵；WR-18 不提前生成未使用的全 Scope×Evidence API。长连接建立之后的重授权/终止策略在 WR-19 实际 Session 能力启用前冻结并验证，不能将历史样例数值当既成规则。

Core 异步证据与同步委托独立：D02 异步部分保持 pending，候选按可信 NATS broker/authenticated producer、专属 publish ACL、subject/stream→Workload Binding 归因；自报 producer 和 payload Tenant 不构成授权。不把用户委托放进 Lifecycle 消息。WR-23 必须冻结实际 Account/ACL/subject/Stream/replay/revocation 规则并证明 broker 防伪边界，未登记组合不可消费。用户本轮只接受同步部分，异步部分不阻止 WR-18 基础。

### 4.1 普通服务接入成本的交付标准

在线验证、短 WAT 与委托本身不等于低成本接入。WR-19 首链必须同时交付独立、版本化的调用方与接收方 Adapter 和可运行示例；它们不依赖 ANI/Core 内部包。首期只实现真实使用的 gRPC seam，不预建所有协议的 SDK。后续 HTTP Adapter 复用相同身份、目标与错误语义。

- 调用方使用领域调用和已验证主体上下文；Adapter 统一取得/更新所需凭据、绑定实际目标/请求摘要、通过 mTLS 发送。禁止业务 Handler 手工拼身份 Header、复制 mint/verify 规则或自动重试有副作用操作。
- 接收方注册固定方法→目标操作映射及资源字段提取；Adapter 统一验证 peer、在线调用 IAM、检查委托绑定和时效，产生 typed DirectCaller/DelegatedSubject/Tenant/Target 上下文。IAM 不可用、撤销、篡改均拒绝，普通 Handler 不自行实现 Token 校验和 Grant 查询。
- owner 仍声明自己的 operation/permission、提供真实资源归属/状态检查并承担业务幂等。部署 owner 配置稳定 Workload 身份和明确 Grant；这些实际业务/信任输入不会被隐藏成默认全权。
- 接入验收检查实际 Gateway 与 Session Handler 是否只消费这些接口，并将同一 Adapter 作为后续调用链的固定依赖；若下一服务仍需复制认证实现，接入成本要求未通过。只生成 Proto client、只返回 allow 或只画模块图不能计为完成。

WR-18 仅提供 IAM 内部深模块与正式运行基础，不借本条提前执行 WR-19 或写入其他仓库。

## 5. 最小引导路径与职责（D03 artifact 冻结方案）

环境 owner 通过明确离线管理入口提交带摘要的单次初始化 manifest：environment/trust-domain、固定 Workload Principal IDs/names、受信证书域与精确 Binding/Grant、首管理员的明确 OIDC issuer/subject 与已批准 Platform admin 意图。无默认密码、无网络自注册、不复用现有服务 Secret。Grant/Binding 不因生成 Principal 自动得到。

初始化入口只能使用单独 provisioner/owner 连接，在同一 IAM DB 事务建立初始 Workload/Binding/Grant、一次消费记录和安全 Audit；重复相同 manifest 返回已完成 non-secret receipt，不覆盖当前身份/权限；同环境已有不同初始化记录则拒绝，不能以重试做灾后重建。应用启动只是读取现有状态，绝不自动执行 manifest。

首 Human 管理员采用 IAM 自有的受控一次性 OIDC ceremony：环境 owner 预先批准的 issuer/subject 与环境绑定；真实 code+PKCE/state/nonce 验证后才建立/关联 Human、verified Identity 与 Platform Membership/Role。不按邮箱自动合并，不在未完成认证时授予权限。nonce/flow 一次消费、过期、并发、进程重启与响应丢失在 IAM DB 的 ceremony/flow 状态和同事务 Audit 中闭合；Provider exchange 在事务外，code 已消费而本地未提交则明确终止该 flow，不能重复交换旧 code。

WR-18 仅做新库 Human/Workload 与事务接口基础，并明确区分 owner fixture；WR-19 实际实现可信 Workload 初始化并校验上述专有环境输入；WR-22 完成可登录首管理员、last-admin 与恢复审批。环境 owner 的真实 IdP/证书输入在各自执行前核对，不在 WR-18 用 fixture 冒充正式 ceremony。完整 LineageRegistry、ReadyClaim/PONR、长期 DR 不加入该前置。

## 6. 不变的其他合同

- OIDC owner 已由用户接受为 IAM，不重新请求 owner 选择；Gateway Cookie/CSRF/固定303、无 URL Token/code 泄漏按 ADR-0015。
- Core Snapshot 仍为 REST/OpenAPI；当前只读 gRPC Proto 是未接受变更，不加入本 Goal runtime。
- Audit 至少180天可查、runtime append-only；本阶段不实现清理、导出或生产保留控制。
- 幂等不改变 Refresh reuse、OIDC one-time、PasswordAction/outbox 原有状态机；不建立 PG/Redis 跨库事务。Redis 故障后的保守拒绝边界按已接受 Human 合同保留。
