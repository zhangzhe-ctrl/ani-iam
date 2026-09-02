# ANI Domain Language

ANI Core、IAM 与 Services 共享的领域词汇。这里固定跨模块使用的概念边界，避免把资源生命周期、访问控制、成员关系和配额策略混为同一件事。

## Language

**Tenant（租户）**:
Core Control 拥有的平台资源，具有由 Core Control 生成且不可变的 Tenant ID；其他领域只能引用该 ID，不能创建平行 Tenant。
_Avoid_: IAM Tenant、副本租户

**Tenant Lifecycle（租户生命周期）**:
租户作为平台资源从开通到终止的业务状态，由 Core Control 拥有。非 active 状态阻止普通租户业务操作，但不把正确的身份认证结果改写为失败，也不阻止平台恢复与审计。
_Avoid_: Tenant Access、租户权限

**Tenant Access（租户访问状态）**:
IAM 拥有的、决定整个租户边界是否允许身份进入的安全状态，独立于 Tenant Lifecycle 和单个成员状态；其状态为 `bootstrap_pending`、`active` 或 `suspended`。
_Avoid_: Tenant Lifecycle、Tenant Membership

**Tenant IAM Bootstrap（租户 IAM 引导）**:
Tenant 创建后，在 IAM 领域建立 Tenant Access 与首个管理员关系的过程；它不决定 Tenant Lifecycle，也不阻塞 Core Control 将 Tenant 标记为 active。
_Avoid_: Tenant Lifecycle Provisioning、Tenant Creation

**Recovery Bootstrap（恢复性 IAM 引导）**:
原始 Tenant IAM Bootstrap 意图已不可重放时，由平台明确授权并审计、重新声明首个管理员身份的异常恢复过程。
_Avoid_: 自动补建、普通 Tenant Access 创建

**Tenant Membership（租户成员关系）**:
一个 Principal 与一个 Tenant 之间已经成立的成员关系；状态为 `active`、`suspended` 或 `removed`。Pending Invitation 不是 Membership；被移除后再次加入会建立新的 Membership identity，不复活旧 Role Binding。
_Avoid_: Tenant Access、Tenant Role Binding、invited Membership

**Quota Assignment（配额策略绑定）**:
一个 Tenant 与某个配额策略版本之间的有效绑定，由 Quota 领域拥有。
_Avoid_: Tenant Plan、Tenant Membership

**Role Binding（角色绑定）**:
一个 Tenant Membership 或 Platform Membership 与一个 Role 之间的授权关系；同一个 Membership 可以同时拥有多个 Role Binding，其有效权限是全部活跃绑定的 allow-list 并集。
_Avoid_: Principal 全局角色、单角色字段

**TenantScope（租户作用域）**:
IAM 在完成 Principal、Tenant Access、Tenant Membership 与权限校验后，为单次普通租户操作建立的不可提升作用域；它不是客户端提交的 Tenant ID，也不能用空值或特殊值表达平台权限。Tenant 仓储必须显式接收 TenantScope。
_Avoid_: 请求头 Tenant ID、可选 tenant_id、PlatformScope

**Platform Capability（平台级能力）**:
允许明确的平台操作跨 Tenant 查询或变更数据的独立授权能力；它使用 Platform 仓储、专用服务身份或 Platform Membership、稳定 reason code 与不可变审计，不能从 TenantScope 自动获得。
_Avoid_: 超级 Tenant、`is_admin` 旁路、空 TenantScope

**Service Principal（服务主体）**:
代表程序、自动化或外部集成而不是自然人的 Principal。API Key 是 Service Principal 的一种 Credential；创建该 Key 的 Human Principal 是管理操作的 actor，但不是后续 SDK 请求所代表的身份。
_Avoid_: 无主体 API Key、用户密码替代品、创建者权限快照

**TenantExecutionScope（租户执行作用域）**:
异步 Worker 根据已验证的服务身份、注册命令类型和消息 Tenant ID 建立的单 Tenant 执行边界；它不冒充 Human Principal，也不授予跨 Tenant 的 Platform Capability。
_Avoid_: Worker 超级管理员、虚拟用户 Membership、无作用域后台任务

**Permission（权限）**:
由 ANI OpenAPI 授权标注和 operation 注册表生成的稳定授权能力；Role 只能引用目录中存在且与自身边界一致的 Permission。Permission 不是租户可自由创建的字符串。
_Avoid_: 自定义权限字符串、operation 名称猜测、跨边界 Permission

**System Role（系统角色）**:
由平台定义并按版本实例化到 Tenant 的内置 Role；其 code、系统标记和 Permission 集不能由 Tenant 修改。`tenant-admin` 是基础版本唯一可管理 Tenant Role 与 Role Binding 的 System Role。
_Avoid_: Tenant Owner、可编辑内置角色、永久用户特权

**Invitation（邀请）**:
邀请已验证邮箱对应的 Human Principal 加入 Tenant 或 Platform 的限时意图；它在接受前不建立 Membership、不占用成员配额，也不授予任何权限。Invitation Token 只是一次性意图凭证，不能代替登录和邮箱所有权验证。
_Avoid_: Pending Membership、自动加成员、持 Token 即身份

**Human Principal（人员主体）**:
跨 Tenant 全局唯一的自然人身份主体；可以拥有多个 Tenant Membership 和可选的 Platform Membership。多个登录 Identity 可以显式关联到同一个 Human Principal，但邮箱相同不会自动合并账号。
_Avoid_: 每租户用户副本、邮箱即 Principal、Tenant User

**Session（会话）**:
Human Principal 完成认证后建立的全局登录连续性；Session 可以派生单一 Tenant 或 Platform 边界的 Access Token，但不把全部 Membership 和权限装入一个 Token。
_Avoid_: 多租户权限快照 Token、客户端 Tenant Header、API Key Session

**Boundary-scoped Access Token（边界作用域访问令牌）**:
只代表一个 Tenant 或 Platform 边界的短期访问凭证；切换 Tenant 必须通过受控操作重新校验 Principal、Membership、Tenant Access、Lifecycle 投影与 Session。
_Avoid_: 全 Tenant Token、任意 target_tenant_id、前端自选作用域

**Refresh Token Family（刷新令牌族）**:
属于一个 Session 和一个确定 Tenant/Platform 边界的单次轮换凭证链；每次成功刷新都会替换 Token，重复使用已消费 Token 会撤销整个 Family。不同边界不共享 Refresh Token Family。
_Avoid_: 可重复 Refresh Token、跨 Tenant 刷新、Principal 全局刷新密钥

**Session Grant（会话授权边界）**:
Session 对一个确定 Tenant 或 Platform 边界的可撤销授权状态；带单调递增 version，Access Token 必须匹配当前 version。它使 IAM 无需逐 `jti` Blocklist 即可让一个边界的所有已签发 Token 即时失效。
_Avoid_: JWT Blocklist、全局多租户 Token、仅靠 Token 过期

**Service Token（服务令牌）**:
受信内部工作负载通过 mTLS/SPIFFE 身份换取的短期、不可刷新、audience-bound 凭证；不建立 Session，也不用于外部 SDK。它与 Tenant Service Principal 的 API Key 是不同凭证类型。
_Avoid_: 内部 API Key、共享 mint secret、用户可签发服务令牌

**IAM Security Audit Event（IAM 安全审计事件）**:
与 IAM 状态变更或关键安全结果一同持久化的 append-only 事实，使用稳定 reason、request、correlation 和 decision identity 关联行为；当前不等同于密码学不可抵赖证据，也不替代普通业务访问日志。
_Avoid_: 任意应用日志、完整请求快照、WORM 声明、每次成功请求一行 IAM 审计

**Trusted Principal Context（可信主体上下文）**:
Gateway 在一次 IAM 决策后、通过经过认证的第一跳 mTLS 连接注入的最小主体与边界信息；它只对该连接有效，不能由客户端提供，也不能被内部服务当成可转发的工作负载凭证。
_Avoid_: 用户身份 Header 透传、Role/Permission Header、服务间共享用户 Token
