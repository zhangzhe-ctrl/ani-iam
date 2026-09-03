# ANI IAM Service 重构方案

> 状态：**Accepted target design / Direct P2 计划已接受并发布；DP2-00 基线清理执行中**
>
> 编制日期：2026-09-01
>
> 适用环境：项目未上线、没有真实用户、当前只有测试和演示环境
>
> Direct P2 ANI 来源候选：Git object `0cedae825a489d936cf41815dc27f278f6d3213c`
>
> 来源验证状态：**Git identity verified / Direct P2 contract inventory pending**。该对象已确认存在且验证时对应远端 `main`；动态 `HEAD`、`main`、当前分支和工作树不得替代。事项04已证明旧 Auth RLS 对受限 runtime role 的正向访问失败，因此该对象不是可运行兼容 Oracle。
>
> 配套文档：`plan-iam-kratos-phased.md`、`../../.scratch/ani-iam-p2-direct/spec.md`、`../../.scratch/ani-iam-p2-direct/ticket-plan.md`
>
> 决策索引：`plan-iam-decision-traceability.md`

## 1. 权威关系与执行边界

本方案完整整合 Q1-Q300 已接受决策。决策优先级固定为：

```text
用户当前明确决定
  > 当前规格
  > 本方案、分阶段方案和决策追踪矩阵
  > accepted ADR
  > 当前实现与测试
```

本方案即使单独阅读，也足以判断目标边界、契约、数据、迁移、安全、验证和延期范围。若实现与本文冲突，必须记录并解决差异，不得从已删除材料补入默认规则。

批准本文不等于批准实现。任何改变代码、数据、契约或外部状态的工作都必须由一个状态为 `claimed` 的本地事项承载；删除、Credential 失效、数据重建和切流还需在执行前获得针对精确目标和动作的人工确认。CP0/P1 已停止；Direct P2 ticket plan、Core 拆分、NATS 基础设施、数据库迁移和部署均未因本文生成而自动获得授权。

## 2. 目标、动机与非目标

### 2.1 目标

1. 在独立项目中用 go-kratos 建立边界明确、可删除旧实现的 `iam-service`。
2. 最终删除并替换旧 `auth-service`、旧 Auth Proto、双轨授权、旧身份表和重叠 Tenant Admin 能力，而不是长期兼容它们。
3. 由 IAM 统一拥有 Human/Service Principal、Identity、Credential、Session、Tenant Access、Membership、Role、Invitation、API Key 和安全审计。
4. 由 Core Control 独占 Tenant Lifecycle，IAM 只持有生命周期投影并用于早期拒绝。
5. 保持 Gateway 为唯一公网入口和薄边缘；授权策略由 OpenAPI operation registry 生成。
6. 在 P2 删除 PostgreSQL RLS，以显式 TenantScope、复合约束、窄仓储和负向测试承担应用隔离。
7. 使用成熟开源框架和库约束 AI 生成边界，避免手搓协议、密码学、连接池、Broker 或观测 SDK。

### 2.2 为什么允许破坏性替换

系统尚未上线、没有真实用户，现有数据属于测试和 Demo 数据。删除旧接口、旧表、旧 Token 和旧 Credential 是重构目标，不以保持 Core v1 或旧 IAM 数据兼容为前提。P2 切换前只需对明确列出的测试数据做可恢复快照和重新 seed，不设计逐行线上迁移、双写或兼容视图。

### 2.3 当前不做

- 不创建正式生产环境，也不宣称生产就绪；
- 不实现 Tenant Purge 或个人数据物理擦除；
- 不实现成员数 Quota/TCC；
- 不实现委派角色管理员；
- 不实现审计导出、SIEM、WORM、密码学不可抵赖；
- 不实现 Human CLI Refresh Token 或 Device Authorization Flow；
- 不实现 Support Session；
- 不提供公共 IAM HTTP 服务或公共 JWKS；
- 不为未来消费者预建通用 IAM Integration Outbox；
- 不在当前工作包拆分 Core Control；该工作必须另行启动；
- 不把性能、Race、Fuzz 或 HA 缺失描述成已经验证。

## 3. 系统与领域边界

### 3.1 所有权矩阵

| 能力/数据 | 唯一 Owner | IAM 是否保存副本 | 规则 |
| --- | --- | --- | --- |
| Tenant ID、Tenant Lifecycle | Core Control | 保存外部 ID 与只读投影 | IAM 永不生成 Tenant ID，也不写 Lifecycle |
| Tenant Access | IAM | 权威 | `bootstrap_pending/active/suspended` |
| Human/Service Principal、Identity、Credential | IAM | 权威 | Human 全局；Service Principal 单 Tenant |
| Tenant/Platform Membership | IAM | 权威 | 两类表分离，Platform 仅 Human |
| Permission、Role、Role Binding | IAM + 生成契约 | 权威 | Tenant/Platform 分表，绑定 Membership 而非 Principal |
| Invitation | IAM | 权威 | Pending Invitation 不是 Membership |
| Session、Grant、Refresh Family/Token | IAM | 权威 | PostgreSQL 是在线授权事实源 |
| API Key | IAM | 权威 | Service Principal 的 Credential |
| Quota Policy/Assignment/Account/Reservation | Core Control Quota | 不保存 | 当前不含 member_count |
| 资源真实 Tenant/Owner | Core/Services | 不复制为授权事实 | 由 typed obligation 在资源 Handler 强制检查 |
| IAM Security Audit | IAM | 权威 | 状态变更同事务、应用级 append-only |

目录名不建立所有权；写入 API、事务和数据库约束才建立所有权。Core Control 与 IAM 使用独立数据库，不建立跨数据库外键或共享事务。

### 3.2 目标运行拓扑

```text
Console / BOSS / SDK / Internet
              |
              v
        ani-gateway
  TLS / CORS / CSRF / rate limit
  one IAM decision / trusted context
       /                    \
      v                      v
independent iam-service   core-control-service
Kratos gRPC only          public product handlers
IAM PostgreSQL/Redis      Tenant Lifecycle/Quota DB
      ^                      |
      | NATS projection      | authoritative resource guards
      +----------------------+

envoy-authz-adapter ------> iam-service
inference-service --------> iam-service
```

Gateway 不连接业务数据库，不执行 Tenant/Quota Saga，也不本地验证 ANI JWT。Core 和 Services 只在第一跳 mTLS 连接上信任 Gateway 注入的最小上下文；后续服务调用改用工作负载身份，用户 Header 不可转发成授权凭证。

### 3.3 独立 IAM 项目

IAM 使用当前独立 Git 项目和事项02已验证的 scaffold；事项03/04的 compat transport/storage 只作为历史调查资产，不是 Direct P2 runtime。它可以复用 PostgreSQL、Redis、Dex、NATS、Secret Manager、镜像仓库和观测基础设施，但不得复制或 import ANI 内部 Port、Adapter、Bootstrap 或 Go package。

跨项目只通过以下版本化契约协作：

- ANI 拥有唯一公网 REST OpenAPI；
- IAM 拥有内部 gRPC Proto 并发布不可变 descriptor/digest；
- Core 拥有 Tenant Lifecycle/Bootstrap Protobuf 事件和版本化 Snapshot REST 契约；
- 双方消费固定 Commit、Tag 或 Digest，不解析 `main` 或 `latest`。

## 4. IAM 进程、Kratos 结构与依赖

### 4.1 单进程三个服务

一个 `iam-service` 进程只注册：

| gRPC Service | 责任 |
| --- | --- |
| `AuthenticationService` | Password/OIDC、Session、Refresh、Logout、Password Action、Service Token、ValidatePrincipal |
| `AuthorizationService` | 唯一 `CheckPermission` |
| `IAMAdminService` | Principal、Tenant Access、Membership、Role、Invitation、Service Principal/API Key、Platform Account、Audit |

不建立公共 `TokenService`，不注册 Kratos HTTP 业务转码。独立内部管理 Listener 只允许 health、readiness 和 metrics。

### 4.2 固定 Kratos 布局

```text
cmd/server/                 composition root
internal/biz/               entity, use case, consuming ports
internal/data/              sqlc/pgx, Redis, NATS, external adapters
internal/service/           Proto <-> biz thin mapping
internal/server/            Kratos gRPC and internal admin listener
internal/conf/              typed validated config
internal/compat/authv1/     CP0/P1 historical experiment; DP2-00 removes from target tree
api/iam/v1/                 target internal Proto
migrations/                 Atlas versioned migration directory
```

`biz` 不 import Kratos、Proto、data、driver 或 transport 类型。Port 放在消费它的 biz 模块旁；service 层不做状态迁移和授权判断。Use Case 通过 UnitOfWork Port 控制事务，业务状态和 Audit 在同一事务提交。使用显式构造函数和单一 composition root，当前不引入 Wire 或自研 DI。

配置只加载和校验一次，再以 typed config 注入模块。非 Secret 可来自文件或受控环境展开；Credential 和 signing material 只能来自 Secret Manager、挂载 Secret 文件或专用 Secret 环境变量，禁止进入 committed YAML，也禁止模块自行读取进程环境。

### 4.3 固定工具链

- go-kratos v3：使用事项02已验证并锁定的精确 patch；
- PostgreSQL：sqlc + pgx/v5；显式 SQL 是安全评审源；
- Migration：Atlas versioned migrations + `atlas.sum`；
- JOSE/JWT/JWK：稳定版 `lestrrat-go/jwx` adapter；
- OIDC：`coreos/go-oidc/v3` + `golang.org/x/oauth2`；
- Argon2id：基于 `x/crypto/argon2`、支持 PHC 和测试向量的维护中薄库；
- UUIDv7：`google/uuid`，封装在 IDGenerator；
- Redis：`redis/go-redis/v9`；
- NATS：官方 `nats.go/jetstream` API；
- Observability：Kratos middleware + OpenTelemetry + Prometheus；
- Contract：Buf、OpenAPI generation、immutable registry；
- Supply chain：精确版本、SBOM、License inventory、`govulncheck`。

GORM 不作为目标 ORM。移除 RLS 后，`tenant_id`、状态、版本和锁条件必须直接出现在可评审 SQL 中；目标还依赖复合 Tenant 键、部分唯一索引、`FOR UPDATE`、`RETURNING` 和无通用软删除。sqlc 生成窄类型方法和扫描代码，pgx 明确承载事务和 PostgreSQL 语义，避免 Scope、Hook、隐式关联和自动 `deleted_at` 隐藏安全条件。

历史 CP0 只改变 Kratos transport/runtime，现已因事项04负向结果停止。`sa-token-go` 不是目标方案的必选依赖；若未来评估，必须作为单独 POC，不得泄漏类型到 biz 或契约。

## 5. 公网契约、内部契约与 Gateway

### 5.1 唯一公网契约

ANI 仓库的 `repo/api/openapi/v1.yaml` 是唯一产品 REST 契约；在默认同级工作区布局中可从本仓库通过 `../ANI/repo/api/openapi/v1.yaml` 读取。公开变更顺序固定为：

```text
OpenAPI + x-ani-authz/x-ani-exposure
  -> generated operation/policy registry
  -> Gateway/SDK/docs
  -> IAM/Core internal contracts
  -> implementation
```

一个 `operationId` 只能有一个 Gateway Handler 和一个后端 Owner。旧 Core `/admin/tenants/*/users*`、Services `/svc/tenant-admins*`、旧 Tenant Plan/Bind Plan 重叠入口在 P2 删除；`TransferOwnership` 和 `tenant-owner` 不进入目标契约。

目标路径族：

- `/auth/*`：登录、OIDC、refresh、logout、password action；
- `/auth/service-principals*`、`/auth/api-keys*`：Service Principal 与 API Key；
- `/iam/tenants/{tenant_id}/access*`：Tenant Access；
- `/iam/tenants/{tenant_id}/members*`、`roles*`、`invitations*`：租户 IAM；
- `/iam/platform/*`：平台人员和角色；
- `/iam/audit-events*`：安全审计查询；
- `/admin/tenants*`：Core Control Tenant Lifecycle；
- `/admin/plans*`、`/admin/tenants/{tenant_id}/plan|quota|reservations*`：Core Control Quota。

所有公开状态变更要求 Idempotency Key。IAM 保存 24 小时 ledger，作用域为 boundary、actor、operation、key、request hash 和 serialized result。同 Key/同请求重放结果；同 Key/异请求返回 `409`；逻辑过期后复用返回 `409 IDEMPOTENCY_KEY_EXPIRED`。

公开调用者只接收 ANI 稳定 `ErrorResponse`；内部 gRPC 使用稳定 status 和 `google.rpc.ErrorInfo` 等结构化 details。Gateway 维护显式映射，Kratos、数据库和第三方库原始错误不得穿透边界。

### 5.2 Gateway 一次决策

| 路由 | IAM 调用 |
| --- | --- |
| Public | 0 次 |
| Authenticated-only | 一次 `ValidatePrincipal(raw credential)` |
| Authorized | 一次 `CheckPermission(raw credential, operation_id, policy_revision, target attributes)` |

Authorized 路由不先调用 Validate。Gateway 使用专用 mTLS/SPIFFE 身份访问 IAM；IAM listener 不公开。每次 IAM 热路径 deadline 为 500 ms，Gateway 不自动重试。

OpenAPI 授权标注生成不可变 registry 和 `policy_revision`。Gateway 与 IAM Digest 不同返回 `503 AUTHZ_POLICY_MISMATCH`；缺标注或未知 operation 在 CI 失败，若运行时到达则 `503 AUTHZ_OPERATION_UNREGISTERED`。没有默认 allow 或字符串推导 fallback。

稳定映射：无效 Credential `401`；有效身份但状态/权限拒绝 `403`；认证/授权限流 `429`；依赖或投影不可用 `503`；IAM deadline `504`。

Gateway 先删除所有客户端 `x-ani-*` Header，仅在 allow 后注入 Principal ID/type、boundary、Tenant ID、Session/Grant ID、authn method、decision ID。绝不注入 Role 或 Permission 列表。

### 5.3 Typed obligation

IAM 不读取 Core/Services 业务数据库。需要权威 Owner/Tenant 检查时，CheckPermission 返回枚举化 obligation；资源 Owner Handler 加载真实资源并强制比较。缺少生成 obligation Handler 的 operation 不可注册，Gateway 不用 URL 参数代替资源事实。

## 6. 领域状态与授权模型

### 6.1 Principal 与边界

- Human Principal：全局唯一，可有多个 Tenant Membership 和一个可选 Platform Membership；
- Service Principal：固定一个 Core Tenant，只能有该 Tenant 的唯一有效 Membership，无 Platform Membership；
- Internal Workload：mTLS/SPIFFE 身份，不伪造成 Platform Service Principal；
- Principal 状态：`active/disabled`。

Verified email 先 trim，再统一大小写并规范化 IDNA domain；不移除 plus tag、不折叠 dot、不实现 Provider 特例。一个 normalized verified email 只属于一个 Human Principal。相同 email 不自动合并账号；若 Link Identity 的邮箱已属于其他 Principal，操作失败而不是数据库修复。

不开放自由注册。Human Principal 只由 Tenant Invitation、Platform Invitation 或首管理员 Bootstrap 建立。新邀请者先证明 Invitation 和邮箱所有权，再设置本地密码或通过 trusted OIDC；证明完成前不创建 Principal/Identity。Console 与 BOSS 有独立登录入口并共享 Credential verifier；BOSS 还必须有 active Platform Membership，前端路由不能把 Tenant 身份提升为 Platform 身份。

普通租户授权要求：

```text
principal.active
AND tenant_access.active
AND tenant_membership.active
AND tenant_lifecycle_projection.active and fresh
AND required tenant permission
AND every typed resource obligation satisfied
```

Platform 授权使用独立 Platform Membership、Role、Binding 和 Repository。空 Tenant ID、全零 UUID、特殊 Tenant 或 `is_admin` Boolean 都不能表达 Platform 能力。

### 6.2 Tenant Lifecycle、Access 与 Bootstrap

Core 生成不可变 Tenant UUID，并独立提交 Tenant/Quota 事务和 Lifecycle `active`。它不等待 IAM 首个管理员完成。Core 同事务写出 `TenantLifecycleChanged` 状态事实和 `TenantIAMBootstrapRequested` 引导命令。

IAM 幂等消费 Bootstrap，先建立 `tenant_access=bootstrap_pending` 和 operation。未知邮箱保持 Invitation，不创建未验证 Principal。只有 verified Principal、active Membership 和 `tenant-admin` Binding 同事务建立后，Tenant Access 才变为 `active`。

缺少 `tenant_access` 不是 `not found` 或普通 `403`，而是可重试 `503 TENANT_IAM_NOT_READY`。普通成员操作不能偷建该行。

原 operation 可重放；原意图丢失时只能使用 Recovery Bootstrap：独立 Platform Permission、不同 requester/approver、单次 approval reference、绑定 Tenant/目标身份/payload hash、一小时有效、执行前 15 分钟内重新认证、完整审计。不得直接插库。

Tenant Lifecycle 投影按 Tenant version 更新；旧版本/重复消息幂等忽略，版本 gap 只冻结受影响 Tenant并触发 Core Snapshot 修复。Pipeline 健康由独立 consumer watermark/heartbeat 判断：Core 每 10 秒通过相同 outbox/stream 路径发送 heartbeat，正常传播目标 p99 5 秒，30 秒无进展视为 stale。普通授权不回调 Core。

全量重建先取得一致 snapshot cursor，再订阅 cursor 后增量，加载分页快照，原子激活新投影并追平 buffered events，不在 snapshot 和订阅之间留 gap。

Lifecycle authoritative guard 仍由 Core/Services 在资源边界执行；IAM 投影只做早期拒绝。测试环境 shadow exit 需要 24 小时真实或合成 workload、p99≤5 秒、无未修复 gap、无不可解释 mismatch 和一次成功 rebuild。单测试 Tenant canary 可在 checkpoint 后扩到全部测试 Tenant；这不是生产 rollout 先例。

### 6.3 Membership、Role 与 Permission

Tenant/Platform Membership、Role、Permission Join、Role Binding 分表。Role Binding 绑定 Membership，不直接绑定 Principal；一个 Membership 可有多个 Role，权限为所有 active Binding 的 allow-list 并集。

Permission Catalog 来自 OpenAPI/operation registry，未知 Permission fail closed。Tenant built-in Role 按 Tenant 实例化并带 `system_definition_version`；系统 code 和 Permission 集不可由租户修改。自定义 Role 更新要求 `expected_version`，冲突返回 `409`；有 active Binding 或 unfinished Invitation 时删除返回 `409 ROLE_IN_USE`，不 cascade。

初版仅 `tenant-admin` 管理 Role 和 Binding。最后管理员只计算 active Human Principal + active Membership + active `tenant-admin` Binding；Service Principal 不计入。相关变更在 Tenant guard lock 或窄 serializable transaction 中重算，禁止降到零。丢失所有管理员使用双人审批的 `RestoreTenantAdmin`，不复用 Bootstrap。

System Role definition 通过显式 schema/seed migration 按 `system_definition_version` 升级全部 Tenant，并保留 before/after 审计，不让每个 Tenant 永久停留在 Bootstrap 时的权限快照。

### 6.4 Invitation

Tenant/Platform Invitation 分表。一个 Tenant + normalized email 最多一个 pending Invitation，可携带多个同 Tenant Role。相同 Role set 重试返回已有 metadata，不重发 Secret；不同集合返回 `409 INVITATION_CONFLICT`。

默认 7 天。Resend 保留 Invitation ID、产生新 Token、立即作废旧 Token、重置 expiry 并写 Audit；Invitation 与 `notification_outbox` 同事务，投递异步。

接受者必须是拥有相同 verified email 的 authenticated Human Principal；Token 本身不是身份。接受时重新验证 Role，原子创建新 active Membership 和 Binding。Invitation 不建立 invited Membership，不占成员 Quota；removed 后重邀创建新 Membership ID。

Accept、Cancel、Resend 均锁定 Invitation 并以 version 条件迁移，首个提交者获胜；后续重复返回保存的幂等结果或稳定冲突。Invitation 保存 Role ID 而不是 Permission snapshot，接受时使用这些 Role 当前的 Permission 集。

移除 Human Membership 只撤销该 Tenant boundary 的 Session Grant/Family/Token，不影响其其他 Tenant 或 Platform。移除 Service Principal 的唯一 Membership 会 disable 该 Principal 并不可逆 revoke 全部 API Key。

### 6.5 TenantScope 与 Repository

Gateway 删除客户端上下文后，IAM 从 Principal、Tenant Access、Membership 和 Permission 建立非空 TenantScope。普通 Tenant Repository 必须接收 TenantScope，不提供 unscoped `FindByID`、可空 Tenant、Platform Boolean bypass 或客户端 Tenant ID 直接构造入口。

Platform Repository 独立，要求 Platform Capability、专用身份、reason code 和 Audit。Worker 使用由服务身份、注册命令类型和消息 Tenant ID 建立的 TenantExecutionScope；不冒充 Human。

## 7. Credential、Session 与浏览器模型

### 7.1 Password 与 OIDC

历史 CP0/P1 原计划保留基线 bcrypt 行为，但该路线已停止。Direct P2 新密码使用 Argon2id：64 MiB、t=3、p=4、16-byte salt、32-byte tag，并保存算法和参数。只有显式导入 bcrypt 可在成功登录后 rehash；seed 不保存明文默认密码，只产生一次性 30 分钟设置动作。

Password 登录按 normalized account 和 IP 限流，连续 5 次失败锁定 15 分钟，响应不枚举账号。

OIDC 使用 Authorization Code + PKCE S256；state、nonce、verifier 10 分钟单次使用。只有 trusted issuer/audience/signature/nonce 和 `email_verified=true` 全部验证后才能建立 verified email。相同 email 不自动合并 Principal；Link Identity 需要已登录、近期重认证和完整 state/nonce/PKCE。

### 7.2 Session、Token 与撤销

Console Access Token 15 分钟，Session idle 7 天/absolute 30 天；BOSS Access Token 10 分钟，Session idle 30 分钟/absolute 8 小时。

Token 只包含 issuer、subject、audience、iat/exp、jti、Session/Grant ID、Grant version、Principal type、boundary、可选 Tenant ID 和 authn methods；不含 Role、Permission 或全部 Membership。

Session 是 Principal-wide；Session Grant 是单 Tenant/Platform boundary。每个 Session + boundary 最多一个 active Refresh Family。Refresh Token 单次旋转，reuse 撤销 Family 并增加 Grant version，使该 boundary Access Token 失效；其他 boundary Grant 不受影响。Consumed hash 保留到 Session absolute expiry。

P2 不保留 `jwt_blocklist`。PostgreSQL `sessions/session_grants/refresh_token_families/refresh_tokens` 是在线授权事实源。Redis 不可用时普通保护请求仍查 PostgreSQL；依赖 Redis 临时状态或限流的 login/refresh/OIDC 返回 `503`。

Human Session 数量不设上限，支持设备列表和单个撤销。普通 logout 幂等撤销当前 Session；Password Reset 撤销该 Human 全部 Session/Family/Access Token。

Session 只保存用户命名或规范化 device type、authn methods、创建时间、采样 recent activity 和截断/Hash 后的网络信息，不长期保存完整 IP 或 User-Agent。重复 logout 不泄漏 Session 是否存在，且只有第一次有效状态变化写 revocation Audit。

`SwitchTenant` 必须重新校验 Principal、目标 Tenant Access、Lifecycle projection、Membership 和 Session，再创建或旋转该 boundary Grant/Family；客户端 Header 不能自行切换 Token boundary。

Service Token 只给 mTLS/SPIFFE allowlisted 内部 workload，audience-bound、permission subset、最长 5 分钟、不可 refresh、无 Session。API Key 不用于内部工作负载替代 Service Token。

### 7.3 Browser

Access Token 只放前端内存。Refresh Token 只通过 host-scoped `Secure HttpOnly SameSite=Lax` Cookie，Console/BOSS 使用不同名称、Path 和 Audience。Refresh JSON 仅返回新 Access Token、类型、过期和非敏感 Session/boundary summary；新 Refresh 只在 `Set-Cookie`。

Refresh Cookie 的过期时间不晚于 Session absolute deadline，服务端 Session 状态始终权威。即使 Access Token 已过期，浏览器仍可用 Refresh Cookie + CSRF 执行幂等 Logout。

Refresh、Logout、SwitchTenant、OIDC 校验精确 Origin/Referer、独立 CSRF Cookie/Header 和固定 Redirect URI。Cookie 只在认证路由可见，Gateway 在普通请求前剥离。

前端在 Access Token 到期前约一分钟 single-flight refresh；401 后只补一次 refresh 并只重试原请求一次。多 Tab 用 Web Locks/BroadcastChannel 协调。丢失旋转响应时旧 Token 再用仍视为 reuse，用户重新登录。

测试环境只允许无 Cookie 的 Bearer/API Key 路由使用宽 CORS；Cookie/OIDC 路由始终 exact allowlist。非浏览器 SDK 使用 API Key，内部服务使用 Service Token，P2 不返回 JSON Refresh Token 给 CLI。

OIDC 使用固定 Gateway callback：Gateway 交换 code、创建 Session、设置 Refresh Cookie，并以 `303` 跳到固定 Console/BOSS 页面；Token 和 authorization code 不进入 URL。Refresh `401` 清空内存 Access Token 并回登录，`503` 显示暂态错误且不无限重试，reuse 只显示通用 Session expired，不泄漏检测细节。

### 7.4 Signing Key

Access/Service JWT 使用 Secret Manager/KMS 中的非对称密钥和 `kid`。Retired public key 至少保留到其可能签发 Token 全部过期。IAM 内部 verifier 直接使用 Key Ring/KMS；OIDC verifier 消费 Provider JWKS。没有已批准的离线 ANI JWT 消费方，因此不发布公共 ANI JWKS。

## 8. Service Principal 与 API Key

Service Principal 创建时原子提交 base/profile、唯一 active Tenant Membership、初始 Role Bindings 和 Audit；Key 创建是随后独立操作。名称在 Tenant 内 normalized unique，disabled 后不释放。

API Key 是 Service Principal Credential：

- P2 只接受 `Authorization: Bearer <api-key>`；
- 由非 Secret key ID + 高熵 Secret 组成，数据库只存 Secret Hash 与安全显示前后缀；
- Secret 只在创建响应返回一次；
- `never_expires=true` 或 `expires_at` 必须二选一，默认 Console 为 never expires；
- 一个 Service Principal 可有无限 active Key，但创建限流、列表游标分页、异常数量告警；
- 90 天未使用仅 stale 告警，不自动 revoke；
- 不保存 `permissions_json` 或 `rate_limit_rpm`，权限来自当前 Membership Role Binding；
- `last_used_at` 异步采样，不是安全审计事实；
- invalid/expired/revoked 返回 401；Key 有效但 Principal/Membership/Access/Lifecycle blocked 返回 403；投影或依赖不可用返回 503；
- 禁止 API Key 执行密码、Credential、Role、Recovery Bootstrap 和 Platform 管理等高风险 operation，除非未来明确开放；
- disable Service Principal 同事务不可逆 revoke 全部 Key，重新 enable 不复活旧 Secret。

创建者只是 Audit actor，没有永久绕过。初版由 `tenant-admin` 管理；未来 delegated manager 另立决策。

## 9. 数据模型与数据库规则

### 9.1 标识、角色与事务

- IAM entity 使用 UUIDv7；Core Tenant ID 为 Core 生成 UUID，IAM 只验证和保存；
- Migration owner 与 runtime DML role 分离；runtime 非 owner、非 superuser；
- Direct P2 不使用 PostgreSQL RLS；事项04的旧 RLS 失败只作为历史负向证据保留；
- 普通事务 `READ COMMITTED` + row lock/version；Tenant invariant 使用 guard lock 或窄 serializable；
- mutable aggregate 有 `version bigint`，时间为 UTC `timestamptz`；
- status 使用 constrained text + generated constants，不使用 PG ENUM；
- FK 默认 `RESTRICT`，无通用 `ON DELETE CASCADE`；
- 不使用通用 `deleted_at`。安全对象使用 disabled/revoked/expired/cancelled/removed 等明确状态。

移除 RLS 的残余风险被明确接受：一旦 IAM runtime 数据库 Credential 被攻破或滥用，该角色能访问其 DML grant 覆盖的全部 Tenant 行。Least privilege、TenantScope、复合约束和测试降低风险，但不宣称等价于 RLS。

### 9.2 核心表

| 表 | 关键内容与约束 |
| --- | --- |
| `principals` | UUIDv7、type human/service、status、version |
| `human_principals` | Human profile；无 Service nullable 字段 |
| `service_principals` | 固定 tenant_id、normalized name tenant-unique |
| `verified_emails` | normalized email 全局唯一，仅 Human |
| `identities` | provider/issuer/subject unique，显式 Link |
| `password_credentials` | PHC hash、algorithm/params、lock state |
| `tenant_access` | external tenant_id PK；bootstrap_pending/active/suspended |
| `tenant_lifecycle_projections` | tenant_id、完整 state、version、effective_at |
| `lifecycle_consumer_state` | stream sequence、heartbeat、watermark、health |
| `lifecycle_projection_repairs` | per-Tenant gap、expected/observed version、repair state |
| `tenant_bootstrap_operations` | operation、intended admin、payload fingerprint、status、recovery link |
| `tenant_memberships` | tenant_id non-null；active/suspended/removed；每 Tenant+Principal 最多一个非 removed |
| `tenant_roles` | per-Tenant，包括 system role 和 definition version；code tenant-unique |
| `tenant_role_permissions` | role + generated Permission，same boundary |
| `tenant_role_bindings` | tenant_id + membership + role 复合 FK |
| `platform_memberships/roles/role_permissions/role_bindings` | 与 Tenant 关系完全分离，仅 Human Membership |
| `tenant_invitations/platform_invitations` | 分表；token hash、role IDs、state/version/expiry |
| `notification_outbox` | 仅 Invitation/password action 通知 |
| `sessions` | Human、authn methods、device、idle/absolute expiry |
| `session_grants` | single boundary、version、status |
| `refresh_token_families` | one active per Session+boundary |
| `refresh_tokens` | hash、issued/consumed/replaced/revoked evidence |
| `password_action_tokens` | purpose-bound、30m、single use |
| `api_keys` | Service Principal、hash、never/expiry、revoked、sampled usage |
| `idempotency_ledger` | 24h logical replay result |
| `iam_audit_events` | allowlisted append-only security event |

每个 Tenant-owned 表都有 non-null `tenant_id`。Tenant-local关系使用 `(tenant_id,id)` 复合键/FK，索引以 `tenant_id` 开头。Platform/global table 不用 NULL Tenant 模拟边界。结构化 JSONB 只允许 schema/allowlist 非敏感 metadata，不保存 Permission、Role Binding、状态或所有权。

Atlas migration 由预部署 Job 执行并校验 `atlas.sum`；runtime 只验证 schema revision，不自动迁移。Custom Role 在无 Binding/unfinished Invitation 后可物理删除；被删除 code 可由新 UUID Role 重用。

Service Principal profile、唯一非 removed Tenant Membership 和固定 tenant_id 由 unique constraint 与 constraint trigger 或等价数据库约束共同保护，拒绝第二个有效 Membership 或跨 Tenant Membership。首管理员 Invitation 保存 `bootstrap_operation_id`；接受时恢复同一个 operation，不按 email 猜测关联。

## 10. Lifecycle/Bootstrap 消息可靠性

Core Tenant/Quota 本地事务与 outbox row 同事务。消息是完整状态事实，含 event UUID、schema major、producer、Tenant、aggregate version、status、reason、effective time、operation/correlation/causation/trace。Outbox 失败则业务事务失败；publisher 指数退避，20 次后 `attention_required`，不阻塞其他 Tenant。

跨项目至少一次，不宣称 exactly once。IAM 以 event/operation ID、payload fingerprint、version、unique constraint 和 CAS 去重。Bootstrap Handler 在 ack 前原子保存 operation 与 fingerprint，再由 durable worker 调和；待邮箱验证是稳定 `waiting_for_principal_verification`，Invitation 过期为 `attention_required`。

Poison message 先写 IAM DB DLQ，再 ack source。DLQ 保留原 payload/header/schema/event/attempt/error，replay 需要 `iam.dlq.replay`、reason 和 Audit，保留原 payload 并创建 linked attempt。

Core/IAM 可共享 NATS 集群但必须使用专用 Stream、subject、credential、ACL；应用只验证基础设施，不创建或修改 Stream/Consumer。稳定 subject：

```text
ani.integration.tenant.lifecycle.v1
ani.integration.tenant.lifecycle-heartbeat.v1
ani.integration.tenant.iam-bootstrap.v1
```

Lifecycle Stream limits retention 30 天；Bootstrap work-queue 在 IAM durable accept 前不按年龄过期；consumer DLQ 90 天。Core Control 尚未拆分，因此这些是 P2 目标，不是当前 CP0 已有事实。未来 Core canonical contract 可以与 prototype fixture 不兼容，IAM 必须重新 pin 和适配。

## 11. Audit、保留与删除

IAM 状态变更和 Audit 同 PostgreSQL 事务；必需 Audit 不可写时 mutation 返回 503 并回滚。覆盖认证、OIDC、Identity Link、Password Action、Session/Token、Refresh reuse、Invitation、Membership、Role、API Key、Service Principal、Tenant Access、Recovery 和 Service Token。

Audit 固定包含 event/time、actor/authn、boundary/Tenant、action/target/result/reason、request/correlation/decision、source/version；details 仅 allowlisted redacted diff。不得保存 Password、Token、Key Secret/Hash、完整邮件、任意请求响应。

Tenant auditor/admin 仅查本 Tenant allowlisted event；Platform recovery/internal 仅 Platform Auditor。初版只有 List/Get 和 cursor pagination，无 export。

至少在线可查 180 天，没有自动删除上限。P2 初版无清理 Job；监控表行数、DB growth 和 oldest record。测试环境磁盘压力下 DBA 可手动删超过 180 天 Audit，以及不再参与 reuse detection 的 expired/revoked/consumed 临时数据；不得删 active、pending、unpublished 或 attention_required。用户已接受该测试操作不要求 snapshot/change record，因此它不是可审计保留机制或生产先例。

Purge 不在当前范围，禁止为未设计的物理删除加入广泛 cascade。

## 12. 验证与证据

### 12.1 当前功能门禁

1. Unit + Proto/OpenAPI contract；
2. 真实依赖 Adapter Integration；
3. 历史 CP0 证据与旧 oracle 的 differential（不得替代 Direct P2 门禁）；
4. Gateway、Envoy、Inference 三调用方独立 E2E；
5. lint/staticcheck、`govulncheck`、Buf lint/breaking、sqlc clean diff、Atlas empty-DB replay/checksum、SBOM、License inventory。

本地资源不足时可用共享测试基础设施，但状态必须隔离：独立 PostgreSQL DB/role、Redis namespace、NATS Account 或完整隔离 subject/Stream/Consumer、Dex Client/identity 和测试 Credential。不能共享 mutable fixture、全局 flush 或依赖残留数据；无法隔离即 `not_verified`。

历史 CP0/P1 使用真实旧 PostgreSQL role/RLS、Redis、Dex 和签名/Token 语义；事项04已记录旧 RLS 失败。Direct P2 使用无 RLS 新库、两 Tenant 负向测试、复合约束和 query mutation。两类证据不能互换。

Differential 只归一化随机 ID、时间、Token/Secret；必须比较 gRPC/public error、Claim、PostgreSQL/Redis state 和 side effect，预期差异进入显式 allowlist。

### 12.2 当前延期门禁

Argon2 latency/memory、CheckPermission load、`go test -race`、Fuzz 和专项并发 suite 暂不阻塞第一版功能实现，状态保持 `not_verified`。它们与 HA、备份恢复、生产 NATS、正式 CORS、生产 retention/WORM 一样，在未来声称 production-ready 或创建正式环境前必须完成。

缺少真实依赖、工具或证据只能记 `not_verified`，不能记 pass。当前 2026-09-30 只可能是测试环境交付，不是 Production。

### 12.3 旧问题门禁

现有 Gateway authz drift 和受保护 Core path 检查被用户判定为门禁本身有问题，因此不再要求“旧脚本必须变绿”，也不能被记作已通过。Direct P2 的 DP2-01/02 草案必须逐项记录旧 assertion、失效理由、对应目标契约和可复现的新检查；人工接受并发布事项后才能开始实现。

## 13. P2 数据重建与破坏性切换

P2 使用测试维护窗口：

1. 冻结相关管理写入，记录 in-flight；
2. 对明确 IAM/Core 数据和 Redis 做可恢复快照；
3. 生成新非对称签名 Key；旧 Session、Refresh、Blocklist、API Key 全部失效；
4. 在独立 IAM/Core 数据库从空库运行 Atlas migration；
5. 先由 Core seed/generate Tenant ID，再由 IAM seed system roles、明确批准 Principal/Invitation/Bootstrap；
6. 部署目标 IAM/Core 与同时切换的 Gateway、Envoy、Inference；
7. 运行三调用方、登录/OIDC、API Key、Service Token、Tenant Access、Membership/Role/Invitation、Audit、Lifecycle projection 冒烟；
8. 失败时人工使用 snapshot/reseed 和固定镜像恢复；当前 rollback rehearsal 可为 `not_verified`；
9. 成功后删除旧 runtime、契约、配置、表和重叠入口，不保留双轨。

切换是整组测试环境变更，不把“新 client 已提交但旧 server 已删”等中间状态单独发布。

## 14. 明确延期清单

### 14.1 Production Readiness 前必须完成

- Argon2 profile benchmark、并发内存上限；
- CheckPermission 固定 workload load gate；
- Race、Fuzz、Refresh/Invitation/Last-admin/Idempotency 并发 suite；
- IAM/Core HA、NATS 三副本持久化/TLS/ACL/failover/backup restore；
- rollback rehearsal、生产 soak、正式 CORS、Secret/KMS rotation 演练；
- 审计/临时表保留删除机制和生产合规决策；
- 生产观测 SLO、容量、告警和故障演练。

### 14.2 未来产品功能，必须另起决策

- Tenant/Principal Purge；
- member_count Quota/TCC；
- delegated Role administration；
- Human CLI Device Flow；
- Audit export/SIEM/WORM；
- Support Session；
- 公共 JWKS 或离线 ANI Token verifier；
- IAM 通用领域事件 Outbox；
- 固定 API Key 数量/强制过期策略。

## 15. 完成定义

只有以下全部满足，才能称为“IAM 重构功能完成”：

- 独立 `iam-service` 只注册目标三个 gRPC Service；
- Gateway、Envoy、Inference 全部使用目标契约，三者独立 E2E 通过；
- `auth-service` runtime、旧 Auth Proto/client、`CheckPermissionV2`、旧地址变量和 compatibility facade 已删除；
- Gateway 无业务 DB/Runtime，operation registry/owner/obligation 一致；
- Tenant Lifecycle 只由 Core Control 写，Tenant Access 只由 IAM 写；
- Core/Services authoritative Lifecycle/Owner guard 存在；
- Tenant/Platform authorization 分表，Permission catalog 生成，Role 与 Invitation 约束生效；
- P2 无 RLS、无通用 soft delete、无 member_count quota、无 jwt_blocklist；
- Password/OIDC/Session/Refresh/API Key/Service Token 只有一个目标实现；
- 新数据库可从空库迁移和 seed，受限 runtime role 集成测试通过；
- Audit 与 mutation 同事务，180 天查询语义和手工测试清理边界明确；
- 当前规格、两份核心计划、Q1-Q300 追踪矩阵和 accepted ADR 一致；
- 所有必需功能门禁为 pass，缺失项明确为 `not_verified`；
- 没有把功能完成描述为 Production Ready。

实现按本地 Markdown 事项推进：改变状态的事项必须先进入 `claimed`，同一时间只推进一个；高风险或难以恢复的动作需要精确人工确认。本文完成不自动启动实现。
