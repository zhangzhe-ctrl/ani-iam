# ANI Domain Language

ANI Core、IAM 与 Services 共享的领域词汇，用于区分资源生命周期、主体、所有者、身份、权限与执行边界。这里仅解释概念；已接受决定见 [ADR](docs/adr/)，候选词汇不因列入本表而成为已冻结契约。

## Tenant 与授权边界

**Tenant（租户）**:
Core Control 拥有的平台资源，具有由 Core Control 生成且不可变的 Tenant ID；其他领域只能引用该 ID。
_Avoid_: IAM Tenant、副本租户

**Tenant Lifecycle（租户生命周期）**:
租户作为平台资源从开通到终止的业务状态，由 Core Control 拥有，独立于身份认证结果和 IAM 访问状态。
_Avoid_: Tenant Access、租户权限

**Tenant Access（租户访问状态）**:
IAM 拥有的租户安全访问状态，独立于 Tenant Lifecycle 和单个成员状态；其状态为 `bootstrap_pending`、`active` 或 `suspended`。
_Avoid_: Tenant Lifecycle、Tenant Membership

**Tenant IAM Bootstrap（租户 IAM 引导）**:
Tenant 创建后，在 IAM 领域建立 Tenant Access 与首个管理员关系的过程，不拥有 Tenant Lifecycle。
_Avoid_: Tenant Lifecycle Provisioning、Tenant Creation

**Recovery Bootstrap（恢复性 IAM 引导）**:
原始 Tenant IAM Bootstrap 意图已不可重放时，由平台明确授权并审计、重新声明首个管理员身份的异常恢复过程。
_Avoid_: 自动补建、普通 Tenant Access 创建

**Tenant Membership（租户成员关系）**:
一个 Principal 与一个 Tenant 之间已经成立的成员关系；状态为 `active`、`suspended` 或 `removed`。Pending Invitation 不是 Membership，移除后重新加入也不是旧关系的复活。
_Avoid_: Tenant Access、Tenant Role Binding、invited Membership

**Quota Assignment（配额策略绑定）**:
一个 Tenant 与某个配额策略版本之间的有效绑定，由 Quota 领域拥有。
_Avoid_: Tenant Plan、Tenant Membership

**Role Binding（角色绑定）**:
一个 Tenant Membership 或 Platform Membership 与一个 Role 之间的授权关系；同一个 Membership 的有效权限是全部活跃绑定的 allow-list 并集。
_Avoid_: Principal 全局角色、单角色字段

**TenantScope（租户作用域）**:
IAM 完成主体和租户访问授权后，为单次普通租户操作建立的不可提升作用域。客户端提交的 Tenant ID、空值或特殊值都不是该作用域的权限来源。
_Avoid_: 请求头 Tenant ID、可选 tenant_id、PlatformScope

**Platform Capability（平台级能力）**:
允许明确的平台操作跨 Tenant 访问资源的独立授权能力，来源于 Platform-owned Workload 的有效 Grant 或 Human 的有效 Platform Membership/Role Binding。Owner、Identity 和 TenantScope 本身不授予此能力。
_Avoid_: 超级 Tenant、`is_admin` 旁路、空 TenantScope

**Permission（权限）**:
稳定授权目录中、属于明确 Tenant 或 Platform 边界的操作能力，供同边界的 Role 引用。
_Avoid_: 自定义权限字符串、operation 名称猜测、跨边界 Permission

**System Role（系统角色）**:
由平台定义并按版本实例化到 Tenant 的内置 Role，其系统身份和权限集不能由 Tenant 修改。
_Avoid_: Tenant Owner、可编辑内置角色、永久用户特权

**Invitation（邀请）**:
邀请已验证邮箱对应的 Human Principal 加入 Tenant 或 Platform 的限时意图，接受前不建立 Membership 或授予权限。Invitation Token 是意图凭证，不是登录身份。
_Avoid_: Pending Membership、自动加成员、持 Token 即身份

## Principal 与 Workload

**Human Principal（人员主体）**:
跨 Tenant 的自然人主体，可以拥有多个 Tenant Membership 和可选的 Platform Membership。多个登录 Identity 可以显式关联到同一个 Human，邮箱相同不代表同一主体。
_Avoid_: 每租户用户副本、邮箱即 Principal、Tenant User

**Workload Principal（工作负载主体）**:
代表服务、程序或自动化执行体的稳定软件主体，类型始终为 `workload`，由 Tenant 或 Platform 拥有。其边界由独立安全职责和撤销生命周期决定，与运行实例、Credential 和 Core 的业务 Workload 资源不同。
_Avoid_: Service Principal、NonHuman Principal、Pod Principal、无主体 API Key

**Workload Owner（工作负载所有者）**:
决定谁可以管理 Workload Principal 及其授权关系的边界，取值为 Tenant 或 Platform；Owner 不表示认证方式或自动授予的权限。
_Avoid_: Workload 类型、Credential 类型、权限来源

**Workload Identity（工作负载身份）**:
由受信身份域认证并规范化的软件运行时标识符。Credential 是证明身份的载体，Identity 本身不决定 Principal 映射或 Authority。
_Avoid_: Credential 本身、网络位置、请求自报的 service name

**Workload Identity Binding（工作负载身份绑定）**:
IAM 拥有的、将已认证的 canonical Workload Identity 唯一映射到稳定 Workload Principal 的关系。Binding 的生命周期独立于 Principal identity 和 Authority。
_Avoid_: Principal 本身、证书存储、权限绑定

**Workload Grant（工作负载授权）**:
Platform-owned Workload Principal 获得的受约束 Authority，限定其可访问的目标、操作和范围。它不创建 Membership，也不能由请求或消息中的 Tenant ID 扩权；具体 Scope 与 Evidence 组合见候选设计。
_Avoid_: Platform Membership、万能 service role、客户端 permission list

**API Key（接口密钥）**:
Tenant-owned Workload Principal 的长期、可撤销 Credential；它证明主体，权限来自该 Workload 的单 Tenant Membership 与 Role Binding。创建 Key 的 Human 只是管理 actor。
_Avoid_: 无主体 Secret、Human API Key、创建者权限副本

**Workload Access Token（工作负载访问令牌）**:
Workload Principal 在已验证 Identity 与有效 Authority 约束下获得的短期、不可刷新、audience-bound Credential，不建立 Human Session；具体 receiver 与非固定 Tenant evidence 仍以 D01/D02 的接受状态为准。
_Avoid_: Service Token、内部 API Key、共享 mint secret、用户可签发令牌


**Login-capable Platform Administrator（可登录平台管理员）**:
同时具有有效 Human、Platform 管理关系和受信登录 Identity/认证策略的平台管理员，不承诺外部 IdP 实时可用。
_Avoid_: 仅有 Role Binding、外部 IdP 健康、break-glass Credential


## Human Session

**Session（会话）**:
Human Principal 完成认证后建立的全局登录连续性，可以关联单一 Tenant 或 Platform 边界的访问凭证。
_Avoid_: 多租户权限快照 Token、客户端 Tenant Header、API Key Session

**Boundary-scoped Access Token（边界作用域访问令牌）**:
只代表一个 Tenant 或 Platform 边界的短期访问凭证，其有效性依赖该边界当前的身份、会话与授权状态。
_Avoid_: 全 Tenant Token、任意 target_tenant_id、前端自选作用域

**Refresh Token Family（刷新令牌族）**:
属于一个 Session 和一个确定 Tenant/Platform 边界的单次轮换凭证链；重复使用已消费 Token 会撤销整个 Family。
_Avoid_: 可重复 Refresh Token、跨 Tenant 刷新、Principal 全局刷新密钥

**Session Grant（会话授权边界）**:
Session 对一个确定 Tenant 或 Platform 边界的可撤销授权状态，独立于该 Session 的其他边界。
_Avoid_: JWT Blocklist、全局多租户 Token、仅靠 Token 过期

## 执行与审计上下文

**Trusted Principal Context（可信主体上下文）**:
Gateway 为一次入口请求本地持有的 IAM 身份与授权决定结果，不是可跨服务复用的 Credential 或 Authority。
_Avoid_: 可传播主体凭证、Role/Permission 副本、服务间共享用户身份

**Workload Invocation（工作负载调用上下文）**:
接收方为单次同步业务调用建立的可信执行上下文，区分 Direct Caller、Target 与可选 Delegated Subject。
_Avoid_: 单一含混 PrincipalContext、可转发 Trusted Header、上一跳 Token

**Delegated Subject Context（委托主体上下文）**:
说明直接 caller 正在代表哪个 Human 或 Tenant-owned Workload 的独立上下文。它不能替代 Direct Caller 的身份或 Authority，具体证明机制仍属候选契约。
_Avoid_: 用户 Token 转发、caller_workload 替身、自报 on-behalf-of Header

**TenantExecutionScope（租户执行作用域）**:
异步 Workload 依据自身 Authority 与可信业务事实建立的单 Tenant 执行边界，不包含 Delegated Subject。外部 Tenant ID 不是权限来源，该边界也不授予跨 Tenant 的 Platform Capability。
_Avoid_: Worker 超级管理员、虚拟用户 Membership、无作用域后台任务

**IAM Security Audit Event（IAM 安全审计事件）**:
记录 IAM 状态变更或关键安全结果的 append-only 事实，用于追溯行为与决定，不等同于普通业务日志或密码学不可抵赖证据。
_Avoid_: 任意应用日志、完整请求快照、WORM 声明、每次成功请求一行 IAM 审计

**IAM Audit Actor（IAM 审计行为来源）**:
IAM Security Audit Event 的归因来源；成功认证的来源是 Human 或 Workload Principal，认证失败或信任建立前的来源使用不携带 Authority 的非 Principal 归因。具体 provenance variants 仍属候选契约。
_Avoid_: Anonymous Principal、Seed Principal、伪造系统用户

## 候选信任与恢复词汇

以下名称仅便于讨论尚未接受的机制。其状态与待决问题以[当前决定登记](.scratch/ani-iam-workload-refoundation/decisions.md)为准，方案解释见 [Workload 设计计划](docs/plans/plan-workload-principal-refoundation.md)；本表不冻结字段、算法、接收策略或交付顺序。

**Broker Workload Binding（消息代理工作负载绑定，候选）**:
将 broker 内已认证的非敏感身份引用映射到稳定 Workload Principal 的关系，只表达身份映射。
_Avoid_: Broker Secret 表、共享 broker 身份、由 Binding 自动授予 subject 权限

**Delegated Tenant Scope（委托租户作用域，候选）**:
同步 on-behalf-of 调用中，由独立且不可扩权的委托证明约束的单 Tenant Grant scope。
_Avoid_: any-Tenant Grant、origin ID proof、异步 Core event

**Authoritative Message Tenant Scope（权威消息租户作用域，候选）**:
无 Delegated Subject 的异步消息中，由可信 producer/message provenance 约束的单 Tenant Grant scope。
_Avoid_: Delegated Tenant alias、payload producer proof、shared subject

**Transport Workload Audit Context（传输工作负载审计上下文，候选）**:
Audit 中独立记录传递证据的中介或执行方的上下文，不替换 evaluated subject 的 actor 或携带其 Authority。
_Avoid_: 把中介当 actor、丢失 producer/executor 任一身份、一个 principal_id 表示两方

**Platform Trust Seed（平台信任种子，候选）**:
由环境所有者明确批准、用于建立最小初始平台信任关系的一次性行为，不是 Principal、Authority 来源或运行时自注册。
_Avoid_: Seed Principal、默认管理员、网络 bootstrap、Service migration

远期 Platform Administration Lineage、Boot Attestation、ReadyClaim/Receipt、Recovery Package/Activation 等候选术语不进入当前共享领域词汇。其原定义保留在[整理前 CONTEXT](.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/before/CONTEXT.md)，是否采用及对应动作前置见 D07。最小可信引导仍由 D03 单独闭合，不能依赖隐藏默认管理员。
