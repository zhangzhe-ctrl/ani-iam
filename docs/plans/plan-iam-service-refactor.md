# ANI IAM 已接受领域规则与替换能力

> 文档角色：accepted base design 的领域职责、完整替换能力和安全不变量；不是当前事项图。
>
> 当前唯一执行入口：[WR spec](../../.scratch/ani-iam-workload-refoundation/spec.md) + [ticket graph](../../.scratch/ani-iam-workload-refoundation/ticket-plan.md)。[能力矩阵](../../.scratch/ani-iam-workload-refoundation/capability-matrix.md) 定义 M1 接口替换就绪与 M2 实际替换完成的验收分母；[待决定项](../../.scratch/ani-iam-workload-refoundation/decisions.md) 区分 accepted/pending。
>
> 用户已明确：先隔离接口验证，M1 后裁剪 Core 旧身份代码并开始前端对接。旧 writer 和 UI 尚存不是 M1 前的失败条件；M2 必须核验其退出与接入。实际切流、Credential 失效和删除仍是精确范围的后续动作。

## 1. 权威关系与执行边界

已接受决定的优先级为：用户当前明确决定 > accepted ADR > 未被修订的 accepted base decisions；当前实现和测试只证明现状。本文保留 Q1–Q300 中仍有效的职责、状态、时间、事务、错误、重试与安全规则，ADR-0022 修订 Human/Workload 分类。候选 receiver、非固定 Tenant evidence、first-admin 和 administration-recovery 机制不能因为出现在文档中就覆盖 accepted 决定。

[Workload 模块设计](plan-workload-principal-refoundation.md) 解释必要 seams 与尚待冻结的接口，不维护另一张执行图；[历史阶段索引](plan-iam-kratos-phased.md) 不提供 frontier。整理前的完整 base 文本保存在[WR-16 before 快照](../../.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/before/docs/plans/plan-iam-service-refactor.md)，其中混入的未接受机制只作非规范历史草案。

批准文档不等于批准实现。改变代码、契约、数据或外部状态必须由唯一 `claimed` 事项承载；删除、Credential 失效、数据重建和切流须在执行前取得针对精确目标和动作的人工确认。CP0/P1 已停止，Direct P2 到 DP2-10 的历史结果保留；WR-01–15 旧候选原位 superseded，后续工作只从当前 WR spec/graph 领取。

## 2. 目标、动机与非目标

### 2.1 目标

1. 在独立项目中用 go-kratos 建立边界明确、可删除旧实现的 `iam-service`。
2. 最终删除并替换旧 `auth-service`、旧 Auth Proto、双轨授权、旧身份表和重叠 Tenant Admin 能力，而不是长期兼容它们。
3. 由 IAM 统一拥有 Human/Workload Principal、Workload Owner/Identity Binding/Grant、Credential、Session、Tenant Access、Membership、Role、Invitation、API Key 和安全审计。
4. 由 Core Control 独占 Tenant Lifecycle，IAM 只持有生命周期投影并用于早期拒绝。
5. 保持 Gateway 为唯一公网入口和薄边缘；授权策略由 OpenAPI operation registry 生成。
6. 在 P2 删除 PostgreSQL RLS，以显式 TenantScope、复合约束、窄仓储和负向测试承担应用隔离。
7. 使用成熟开源框架和库约束 AI 生成边界，避免手搓协议、密码学、连接池、Broker 或观测 SDK。

### 2.2 为什么允许破坏性替换

系统尚未上线、没有真实用户，现有数据属于测试和 Demo 数据。删除旧接口、旧表、旧 Token 和旧 Credential 是重构目标，不以保持 Core v1 或旧 IAM 数据兼容为前提。M1 在隔离环境证明目标接口，不要求先失效现有 Credential 或裁剪现有 Core。后续切换只处理精确列出的测试数据、快照和重新 seed；恢复前置须接受并真实验证，不设计逐行线上迁移、双写或兼容视图。

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
- 不因本文自动授权 Core 产品拆分或其他仓库变更；M1 所需 Core 配套实现由当前事项明确允许路径与真实 owner；
- 不把性能、Race、Fuzz 或 HA 缺失描述成已经验证。

## 3. 系统与领域边界

### 3.1 所有权矩阵

| 能力/数据 | 唯一 Owner | IAM 是否保存副本 | 规则 |
| --- | --- | --- | --- |
| Tenant ID、Tenant Lifecycle | Core Control | 保存外部 ID 与只读投影 | IAM 永不生成 Tenant ID，也不写 Lifecycle |
| Tenant Access | IAM | 权威 | `bootstrap_pending/active/suspended` |
| Human/Workload Principal、Identity、Credential | IAM | 权威 | Human 全局；Workload owner 为 Tenant 或 Platform |
| Tenant/Platform Membership | IAM | 权威 | 两类表分离，Platform 仅 Human |
| Permission、Role、Role Binding | IAM + 生成契约 | 权威 | Tenant/Platform 分表，绑定 Membership 而非 Principal |
| Invitation | IAM | 权威 | Pending Invitation 不是 Membership |
| Session、Grant、Refresh Family/Token | IAM | 权威 | PostgreSQL 是在线授权事实源 |
| Workload Owner/Identity Binding/Grant | IAM | 权威 | Owner、认证绑定与 Authority 正交 |
| API Key | IAM | 权威 | Tenant-owned Workload 的 Credential |
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

Gateway 不连接业务数据库，不执行 Tenant/Quota Saga，也不本地验证 ANI Human JWT。Gateway 的 ingress IAM decision context 只留在本地。每个业务 hop 必须认证当前直接 caller Workload、检查其目标 Authority，并与可选的被服务主体分开；用户 Header/Token、上一跳 context 不能替代本跳 caller 身份。具体 receiver 和 delegation evidence 合同仍按 decisions 中的 accepted/pending 状态处理，未接受路径不可用。

### 3.3 独立 IAM 项目

IAM 使用当前独立 Git 项目和事项02已验证的 scaffold；事项03/04的 compat transport/storage 只作为历史调查资产，不是 Direct P2 runtime。它可以复用 PostgreSQL、Redis、Dex、NATS、Secret Manager、镜像仓库和观测基础设施，但不得复制或 import ANI 内部 Port、Adapter、Bootstrap 或 Go package。

跨项目只通过以下版本化契约协作：

- ANI 拥有唯一公网 REST OpenAPI；
- IAM 拥有内部 gRPC Proto 并发布不可变 descriptor/digest；
- Core 拥有 Tenant Lifecycle/Bootstrap Protobuf 事件；Snapshot 继续采用 accepted REST/OpenAPI，现有 gRPC Proto 仅属未接受变更，差异按 decisions D06 在真实链前处理；修改 transport 需明确修订决定；
- Core 后续独立时，内部请求建议优先收敛为 gRPC；公网仍由 Gateway 提供 HTTP，Lifecycle 事实仍走 NATS。该后续方向不把物理拆分或协议迁移加入 M1 前置；具体合同、调用方迁移和旧接口退出由对应事项验证；
- 双方消费固定 Commit、Tag 或 Digest，不解析 `main` 或 `latest`。

## 4. IAM 进程、Kratos 结构与依赖

### 4.1 单进程三个服务

一个 `iam-service` 进程只注册：

| gRPC Service | 责任 |
| --- | --- |
| `AuthenticationService` | Password/OIDC、Session、Refresh、Logout、Password Action、Workload Access Token、ValidatePrincipal |
| `AuthorizationService` | `CheckPermission` 与必要的 Workload invocation 授权；具体新增 RPC/receipt wire 由当前 contract 事项冻结，不在此预先接受 |
| `IAMAdminService` | Principal、Tenant Access、Membership、Role、Invitation、owner-specific Workload/API Key/Binding/Grant、Platform Membership/Role administration、Audit |

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

目标路径族表达职责归属；Human/Workload 重冻涉及的精确 operation/path 由当前合同冻结，不从通配写法生成接口：

- `/auth/*`：登录、OIDC、refresh、logout、password action；
- `/auth/workloads*`、`/auth/api-keys*`：Tenant-owned Workload 与 API Key；Platform-owned Workload 使用独立平台管理入口；
- `/iam/tenants/{tenant_id}/access*`：Tenant Access；
- `/iam/tenants/{tenant_id}/members*`、`roles*`、`invitations*`：租户 IAM；
- `/iam/platform/*`：平台人员和角色；
- `/iam/audit-events*`：安全审计查询；
- `/admin/tenants*`：Core Control Tenant Lifecycle；
- `/admin/plans*`、`/admin/tenants/{tenant_id}/plan|quota|reservations*`：Core Control Quota。

重试不得改变已有状态机语义。适用 idempotency ledger 的操作保证 24 小时 scoped-key replay；同 key 不同意图稳定冲突，逻辑过期后返回 `409 IDEMPOTENCY_KEY_EXPIRED` 并要求新 key，物理行保留不延长 replay。Mutation、对应 ledger outcome 和 Audit 必须在同库原子提交；不在 replay 数据中保存 raw Credential/Secret。Secret 只返回一次，丢失响应的可恢复结果须由其 owner 合同明确，不能靠重新发放或恢复旧 Secret 猜测。

每个 endpoint 都须冻结 retry/response-loss/recovery 行为。五类 `safe_read | ledger_replay | ledger_one_time_secret | owner_state_machine | ephemeral_mint` 的 exhaustive catalog、旧字段删除/保留号和 one-time-secret terminal 结果仍是待冻结方案，不能仅从本文接受全部 endpoint 分类。已有 Refresh reuse、OIDC one-time、Invitation race、API Key 一次返回和消息去重规则继续有效。

公开调用者只接收 ANI 稳定 `ErrorResponse`；内部 gRPC 使用稳定 status 和 `google.rpc.ErrorInfo` 等结构化 details。Gateway 维护显式映射，Kratos、数据库和第三方库原始错误不得穿透边界。

### 5.2 Gateway 一次决策

| 路由 | IAM 调用 |
| --- | --- |
| Public | 0 次 |
| Authenticated-only | 一次 `ValidatePrincipal(raw credential)` |
| Authorized | 一次 `CheckPermission(raw credential, operation_id, policy_revision, target attributes)` |

Authorized 路由不先调用 Validate。Gateway 以独立 Workload 的可信身份和当前有效 Authority 访问 IAM，IAM listener 不公开；具体 mint/receiver 机制由当前合同 checkpoint 冻结。每次 IAM 热路径 deadline 为 500 ms，Gateway 不自动重试。

OpenAPI 授权标注生成不可变 registry 和 `policy_revision`。Gateway 与 IAM Digest 不同返回 `503 AUTHZ_POLICY_MISMATCH`；缺标注或未知 operation 在 CI 失败，若运行时到达则 `503 AUTHZ_OPERATION_UNREGISTERED`。没有默认 allow 或字符串推导 fallback。

稳定映射：无效 Credential `401`；有效身份但状态/权限拒绝 `403`；认证/授权限流 `429`；依赖或投影不可用 `503`；IAM deadline `504`。

Gateway 先删除所有客户端 `x-ani-*` Header，并只在本进程保留 allow 后的 typed Principal ID/type、boundary、Tenant ID、Session/Grant ID、authn method 与 decision ID。它不向下游注入可信 Principal Header；需要代表主体时，只有相应 evidence 合同被接受并验证后才能构造独立 Delegated Subject。绝不传播 Role 或 Permission 列表。

### 5.3 Typed obligation

IAM 不读取 Core/Services 业务数据库。需要权威 Owner/Tenant 检查时，CheckPermission 返回枚举化 obligation；资源 Owner Handler 加载真实资源并强制比较。缺少生成 obligation Handler 的 operation 不可注册，Gateway 不用 URL 参数代替资源事实。

## 6. 领域状态与授权模型

### 6.1 Principal 与边界

- Human Principal：全局唯一，可有多个 Tenant Membership 和一个可选 Platform Membership；
- Workload Principal：稳定软件 actor，owner 为 Tenant 或 Platform，不等于 Pod、SPIFFE ID 或 Credential；
- Tenant-owned Workload：固定一个 Core Tenant；active 时恰有一个匹配的 current non-removed Membership（active/suspended），仅 active 提供 Authority，无 Platform Membership/Workload Grant；
- Platform-owned Workload：通过 Identity Binding 识别稳定主体，Authority 只来自有效 Workload Grant，不制造 Tenant/Platform Membership；
- Principal 状态：`active/disabled`。

Verified email 先 trim，再统一大小写并规范化 IDNA domain；不移除 plus tag、不折叠 dot、不实现 Provider 特例。一个 normalized verified email 只属于一个 Human Principal。相同 email 不自动合并账号；若 Link Identity 的邮箱已属于其他 Principal，操作失败而不是数据库修复。

不开放自由注册。Human Principal 只由 Tenant Invitation、Platform Invitation 或受控首管理员 Bootstrap 建立。邀请先证明 Invitation 与邮箱所有权，再建立 Principal/Identity；不得创建未验证的 active Human。Console/BOSS 共享 Credential verifier，但 BOSS 还需 active Platform Membership，前端路由不能提升 Authority。首管理员的具体机制仍待当前 contract/provisioning 决定；旧候选 ceremony/flow/ReadyClaim/lineage 状态机不属于本段已接受规则。

普通租户授权要求：

```text
principal.active
AND tenant_access.active
AND tenant_membership.active
AND tenant_lifecycle_projections.active and fresh
AND required tenant permission
AND every typed resource obligation satisfied
```

Human 的 Platform 授权使用独立 Platform Membership、Role、Binding 和 Repository；Platform-owned Workload 的 Platform 授权只来自其 exact Workload Grant。两者都不能由 owner/credential 自动推导。空 Tenant ID、全零 UUID、特殊 Tenant 或 `is_admin` Boolean 都不能表达 Platform 能力。

### 6.2 Tenant Lifecycle、Access 与 Bootstrap

Core 生成不可变 Tenant UUID，并独立提交 Tenant/Quota 事务和 Lifecycle `active`。它不等待 IAM 首个管理员完成。Core 同事务写出 `TenantLifecycleChanged` 状态事实和 `TenantIAMBootstrapRequested` 引导命令。

IAM 幂等消费 Bootstrap，先建立 `tenant_access=bootstrap_pending` 和 operation。未知邮箱保持 Invitation，不创建未验证 Principal。只有 verified Principal、active Membership 和 `tenant-admin` Binding 同事务建立后，Tenant Access 才变为 `active`。

缺少 `tenant_access` 不是 `not found` 或普通 `403`，而是可重试 `503 TENANT_IAM_NOT_READY`。普通成员操作不能偷建该行。

原 operation 可重放；原意图丢失时只能使用 Recovery Bootstrap：独立 Platform Permission、不同 requester/approver、单次 approval reference、绑定 Tenant/目标身份/payload hash、一小时有效、执行前 15 分钟内重新认证、完整审计。不得直接插库。

Tenant Lifecycle 投影按 Tenant version 更新；旧版本/重复消息幂等忽略，版本 gap 只冻结受影响 Tenant 并触发 Core Snapshot 修复。Pipeline 健康由独立 consumer watermark/heartbeat 判断：Core 每 10 秒通过相同 outbox/stream 路径发送 heartbeat，正常传播目标 p99 5 秒，30 秒无进展视为 stale。普通授权不回调 Core。

全量重建先取得一致 snapshot cursor，再订阅 cursor 后增量，加载分页快照，原子激活新投影并追平 buffered events，不在 snapshot 和订阅之间留 gap。

Lifecycle authoritative guard 仍由 Core/Services 在资源边界执行；IAM 投影只做早期拒绝。测试环境 shadow exit 需要 24 小时真实或合成 workload、p99≤5 秒、无未修复 gap、无不可解释 mismatch 和一次成功 rebuild。单测试 Tenant canary 可在 checkpoint 后扩到全部测试 Tenant；这不是生产 rollout 先例。

### 6.3 Membership、Role 与 Permission

Tenant/Platform Membership、Role、Permission Join、Role Binding 分表。Role Binding 绑定 Membership，不直接绑定 Principal；一个 Membership 可有多个 Role，权限为所有 active Binding 的 allow-list 并集。

Permission Catalog 来自 OpenAPI/operation registry，未知 Permission fail closed。Tenant built-in Role 按 Tenant 实例化并带 `system_definition_version`；系统 code 和 Permission 集不可由租户修改。自定义 Role 更新要求 `expected_version`，冲突返回 `409`；有 active Binding 或 unfinished Invitation 时删除返回 `409 ROLE_IN_USE`，不 cascade。

初版仅 `tenant-admin` 管理 Role 和 Binding。最后管理员只计算 active Human Principal + active Membership + active `tenant-admin` Binding；Workload Principal 不计入。相关变更在 Tenant guard lock 或窄 serializable transaction 中重算，禁止降到零。丢失所有管理员使用双人审批的 `RestoreTenantAdmin`，不复用 Bootstrap。

System Role definition 通过显式 schema/seed migration 按 `system_definition_version` 升级全部 Tenant，并保留 before/after 审计，不让每个 Tenant 永久停留在 Bootstrap 时的权限快照。

### 6.4 Invitation

Tenant/Platform Invitation 分表。一个 Tenant + normalized email 最多一个 pending Invitation，可携带多个同 Tenant Role。相同 Role set 重试返回已有 metadata，不重发 Secret；不同集合返回 `409 INVITATION_CONFLICT`。

默认 7 天。Resend 保留 Invitation ID、产生新 Token、立即作废旧 Token、重置 expiry 并写 Audit；Invitation 与 `notification_outbox` 同事务，投递异步。

接受者必须是拥有相同 verified email 的 authenticated Human Principal；Token 本身不是身份。接受时重新验证 Role，原子创建新 active Membership 和 Binding。Invitation 不建立 invited Membership，不占成员 Quota；removed 后重邀创建新 Membership ID。

Accept、Cancel、Resend 均锁定 Invitation 并以 version 条件迁移，首个提交者获胜；后续重复返回保存的幂等结果或稳定冲突。Invitation 保存 Role ID 而不是 Permission snapshot，接受时使用这些 Role 当前的 Permission 集。

移除 Human Membership 只撤销该 Tenant boundary 的 Session Grant/Family/Token，不影响其其他 Tenant 或 Platform。移除 Tenant-owned Workload 的唯一 Membership 会 disable 该 Principal 并不可逆 revoke 全部 API Key。

### 6.5 TenantScope 与 Repository

Gateway 删除客户端上下文后，IAM 从 Principal、Tenant Access、Membership 和 Permission 建立非空 TenantScope。普通 Tenant Repository 必须接收 TenantScope，不提供 unscoped `FindByID`、可空 Tenant、Platform Boolean bypass 或客户端 Tenant ID 直接构造入口。

Platform Repository 独立，要求 Platform Capability，以及 Platform-owned Workload Principal 的 matching active Workload Grant 或 Human Principal 的 active Platform Membership/Role Binding，并带 reason code 和 Audit；Owner 或已验证 Identity 本身不授予 Capability。Worker 只能依据自身当前有效 Authority 和经认证、可验证的业务事实建立单 Tenant 执行边界；消息 Tenant ID 不能自行创建 Authority。Worker 不冒充 Human，不把自主执行伪装成 Delegated Subject。非固定 Tenant scope、producer provenance 和具体 evidence wire 仍须冻结，不使用旧候选的表名或组合自动授权。

## 7. Credential、Session 与浏览器模型

### 7.1 Password 与 OIDC

历史 CP0/P1 原计划保留基线 bcrypt 行为，但该路线已停止。Direct P2 新密码使用 Argon2id：64 MiB、t=3、p=4、16-byte salt、32-byte tag，并保存算法和参数。只有显式导入 bcrypt 可在成功登录后 rehash；seed 不保存明文默认密码，只产生一次性 30 分钟设置动作。

Password 登录按 normalized account 和 IP 限流；失败产生递增延迟，连续 5 次失败锁定 15 分钟，成功清除失败状态，响应不枚举账号。Password setup/reset 动作均为 30 分钟、single-use，绑定 Principal/purpose/origin operation；创建替代动作立即失效前一动作，reset 完成撤销该 Human 全部 Session/Family/Access Token，不删除其 OIDC Identity 或影响其他 Principal。

OIDC 使用 Authorization Code + PKCE S256；state、nonce、verifier 10 分钟单次使用。只有 trusted issuer/audience/signature/nonce 和 `email_verified=true` 全部验证后才能建立 verified email。相同 email 不自动合并 Principal；Link Identity 需要已登录、近期重认证和完整 state/nonce/PKCE。

### 7.2 Session、Token 与撤销

Console Access Token 15 分钟，Session idle 7 天/absolute 30 天；BOSS Access Token 10 分钟，Session idle 30 分钟/absolute 8 小时。

Human Access Token 只包含 issuer、subject、audience、iat/exp、jti、Session/Grant ID、Grant version、Principal type、boundary、可选 Tenant ID 和 authn methods；不含 Role、Permission 或全部 Membership。Workload Access Token 使用独立 claims profile，以已验证身份、当前 Authority 和 exact target 限制能力，不携带 Role/Permission 集合；具体 binding/version/authority anchors 与 wire 字段由当前合同冻结。

Session 是 Principal-wide；Session Grant 是单 Tenant/Platform boundary。每个 Session + boundary 最多一个 active Refresh Family。Refresh Token 单次旋转，reuse 撤销 Family 并增加 Grant version，使该 boundary Access Token 失效；其他 boundary Grant 不受影响。Consumed hash 保留到 Session absolute expiry。

P2 不保留 `jwt_blocklist`。PostgreSQL `sessions/session_grants/refresh_token_families/refresh_tokens` 是在线授权事实源。Redis 不可用时普通保护请求仍查 PostgreSQL；依赖 Redis 临时状态或限流的 login/refresh/OIDC 返回 `503`。

Human Session 数量不设上限，支持设备列表和单个撤销。普通 logout 幂等撤销当前 Session；Password Reset 撤销该 Human 全部 Session/Family/Access Token。

Session 只保存用户命名或规范化 device type、authn methods、创建时间、采样 recent activity 和截断/Hash 后的网络信息，不长期保存完整 IP 或 User-Agent。重复 logout 不泄漏 Session 是否存在，且只有第一次有效状态变化写 revocation Audit。

`SwitchTenant` 必须重新校验 Principal、目标 Tenant Access、Lifecycle projection、Membership 和 Session，再创建或旋转该 boundary Grant/Family；客户端 Header 不能自行切换 Token boundary。

Workload Access Token 只在已验证 mTLS/SPIFFE Workload Identity 和 owner-specific Authority 下签发，audience-bound、operation subset、最长 5 分钟、不可 refresh、无 Session。首期 Platform-owned Workload 使用 WAT；API Key 只允许 Tenant-owned Workload，不能作为内部平台调用 fallback。

### 7.3 Browser

Access Token 只放前端内存。Refresh Token 只通过 host-scoped `Secure HttpOnly SameSite=Lax` Cookie，Console/BOSS 使用不同名称、Path 和 Audience。Refresh JSON 仅返回新 Access Token、类型、过期和非敏感 Session/boundary summary；新 Refresh 只在 `Set-Cookie`。

Refresh Cookie 的过期时间不晚于 Session absolute deadline，服务端 Session 状态始终权威。即使 Access Token 已过期，浏览器仍可用 Refresh Cookie + CSRF 执行幂等 Logout。

Refresh、Logout、SwitchTenant、OIDC 校验精确 Origin/Referer、独立 CSRF Cookie/Header 和固定 Redirect URI。Cookie 只在认证路由可见，Gateway 在普通请求前剥离。

前端在 Access Token 到期前约一分钟 single-flight refresh；401 后只补一次 refresh 并只重试原请求一次。多 Tab 用 Web Locks/BroadcastChannel 协调。丢失旋转响应时旧 Token 再用仍视为 reuse，用户重新登录。

测试环境只允许无 Cookie 的 Bearer/API Key 路由使用宽 CORS；Cookie/OIDC 路由始终 exact allowlist。非浏览器 SDK 使用 Tenant-owned Workload API Key，内部服务使用 Workload Identity/WAT，P2 不返回 JSON Refresh Token 给 CLI。

OIDC ownership 已由用户接受并记入 decisions D08 / ADR-0015：IAM 统一拥有服务端 flow、verifier、Provider code exchange、Identity 与 Session；Gateway 仅承接固定 callback 和 Cookie 转发。Exact redirect、10 分钟 one-time state/nonce/PKCE、issuer/audience/signature/email_verified 与失败/重放规则继续有效，最终 redirect URL 不携带 authorization code 或 Token。相关 M1 链按该所有权使用真实 Provider 和正式进程验证，不要求实际前端先行。

### 7.4 Signing Key

Human Access/WAT JWT 使用 Secret Manager/KMS 中的非对称密钥和 `kid`。Retired public key 至少保留到其可能签发 Token 全部过期。IAM 内部 verifier 直接使用 Key Ring/KMS；OIDC verifier 消费 Provider JWKS。没有已批准的离线 ANI JWT 消费方，因此不发布公共 ANI JWKS；WAT receiver 首期在线/离线验证按 decisions 的 pending/accepted 记录处理，本文不代替用户选择。

## 8. Tenant-owned Workload 与 API Key

Tenant-owned Workload 创建时原子提交 base/owner profile、唯一 active Tenant Membership、初始 Role Bindings 和 Audit；Key 创建是随后独立操作。canonical name 按 `lower(btrim(name))` 规范化、非空且 immutable；Tenant owner 内唯一，disabled 后不释放。Principal type、owner type 与 owner Tenant ID immutable；转换或转移需新 Principal 和显式重建授权，不能原地将 Membership Authority 改成 Grant。

API Key 是 Tenant-owned Workload Credential：

- P2 只接受 `Authorization: Bearer <api-key>`；
- 由非 Secret key ID + 高熵 Secret 组成，数据库只存 Secret Hash 与安全显示前后缀；
- Secret 只在创建响应返回一次；
- `never_expires=true` 或 `expires_at` 必须二选一，默认 Console 为 never expires；
- 一个 Tenant-owned Workload 可有无限 active Key，但创建限流、列表游标分页、异常数量告警；
- 90 天未使用仅 stale 告警，不自动 revoke；
- 不保存 `permissions_json` 或 `rate_limit_rpm`，权限来自当前 Membership Role Binding；
- `last_used_at` 异步采样，不是安全审计事实；
- invalid/expired/revoked 返回 401；Key 有效但 Principal/Membership/Access/Lifecycle blocked 返回 403；投影或依赖不可用返回 503；
- 禁止 API Key 执行密码、Credential、Role、Recovery Bootstrap 和 Platform 管理等高风险 operation，除非未来明确开放；
- disable Tenant-owned Workload 同事务不可逆 revoke 全部 Key，重新 enable 不复活旧 Secret。

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
| `principals` | UUIDv7、type human/workload、status、version |
| `human_principals` | Human profile；无 Workload nullable 字段 |
| `workload_principals` | owner tenant/platform；Tenant owner 固定 tenant_id，Platform owner 无 tenant_id |
| `workload_identity_bindings` | Principal、canonical SPIFFE ID、status/version；不保存证书/私钥 |
| `workload_grants` 与 operation/scope 关系 | Platform-owned Workload 的明确 audience/operation/TTL 与 Authority；空 Tenant 不代表 wildcard，具体非固定 Tenant 模型待合同接受 |
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
| `platform_memberships/roles/role_permissions/role_bindings` | 与 Tenant 关系完全分离，Platform Membership 仅 Human；系统 Role 定义和变更受权限与审计约束 |
| `tenant_invitations/platform_invitations` | 分表；token hash、role IDs、state/version/expiry |
| `notification_outbox` | 仅 Invitation/password action 通知 |
| `sessions` | Human、authn methods、device、idle/absolute expiry |
| `session_grants` | single boundary、version、status |
| `refresh_token_families` | one active per Session+boundary |
| `refresh_tokens` | hash、issued/consumed/replaced/revoked evidence |
| `password_action_tokens` | purpose-bound、30m、single use |
| `api_keys` | Tenant-owned Workload、hash、never/expiry、revoked、sampled usage |
| `idempotency_ledger` | 已冻结适用操作的 24h key/hash/non-secret outcome；与 mutation/Audit 同事务；endpoint 分类不由表的存在推导 |
| `iam_audit_events` | allowlisted append-only security event；认证后的 actor 与中介/执行方归因不混淆，具体新增 actor union/字段按当前合同冻结 |

每个 Tenant-owned 表都有 non-null `tenant_id`。Tenant-local关系使用 `(tenant_id,id)` 复合键/FK，索引以 `tenant_id` 开头。Platform/global table 不用 NULL Tenant 模拟边界。结构化 JSONB 只允许 schema/allowlist 非敏感 metadata，不保存 Permission、Role Binding、状态或所有权。

Atlas migration 由预部署 Job 执行并校验 `atlas.sum`；runtime 只验证 schema revision，不自动迁移。Custom Role 在无 Binding/unfinished Invitation 后可物理删除；被删除 code 可由新 UUID Role 重用。

Tenant-owned Workload profile、唯一 current non-removed Tenant Membership 和固定 tenant_id 由 unique constraint 与 constraint trigger 或等价数据库约束共同保护，拒绝第二个 non-removed Membership 或跨 Tenant Membership。active Principal 恰有一个 active/suspended Membership，disabled 最多一个；只有 active Membership 提供 Authority。移除后可为同 owner Tenant 原子建立新 Membership identity，但不能复活 removed Membership 或 revoked Key。Platform-owned canonical name 在当前 environment/trust domain 唯一且 immutable，disabled 后继续保留。Platform-owned Workload 必须无 Membership，Tenant-owned Workload 必须无 Workload Grant；API Key 只能引用 Tenant-owned Workload。Tenant IAM Bootstrap initial-admin Invitation保存`bootstrap_operation_id`；接受时恢复同一个operation，不按email猜测关联；它不是Platform First Administrator Ceremony。

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

异步消息不能把发布方 TLS peer 透明传播给订阅方。请求/payload/header 中的 producer 或 Tenant 不能自行成为可信身份与 Authority，必须由独立且受约束的 producer/consumer identity 和 accepted integration contract 校验；不能用共享 superuser 或 broad ACL 代替。Broker Workload Binding、subject-owner registration、Grant projection 和 retained-message revoke 的精确模型仍是候选设计，需在真实消息链之前冻结。旧候选细节已移至 Workload 设计的历史索引，不是所有 M1 接口统一前置，也不是许可匿名消息链。

Lifecycle Stream limits retention 30 天；Bootstrap work-queue 在 IAM durable accept 前不按年龄过期；consumer DLQ 90 天。Core 生产通路是否完成由实际 owner/正式进程证据确认，不能把上述目标当作已有事实。未来 Core canonical contract 可以与 prototype fixture 不兼容，IAM 必须重新 pin 和适配。

## 11. Audit、保留与删除

IAM 状态变更和 Audit 同 PostgreSQL 事务；必需 Audit 不可写时 mutation 返回 503 并回滚。覆盖认证、OIDC、Identity Link、Password Action、Session/Token、Refresh reuse、Invitation、Membership、Role、API Key、Workload Principal/Identity Binding/Grant、Tenant Access、Recovery 和 WAT。

Audit 固定包含 event/time、actor/authn、boundary/Tenant、action/target/result/reason、request/correlation/decision、source/version；details 仅 allowlisted redacted diff。不得保存 Password、Token、Key Secret/Hash、完整邮件、任意请求响应。

Tenant auditor/admin 仅查本 Tenant allowlisted event；Platform recovery/internal 仅 Platform Auditor。初版只有 List/Get 和 cursor pagination，无 export。

至少在线可查 180 天，没有自动删除上限或第 181 天删除承诺。初版无自动 retention cleanup，过期/已消费状态会保留；监控表行数、DB size/growth rate 与 oldest record。Normal runtime 对 Audit 无 update/delete 能力。历史手工删 Audit 行的例外已不能授权当前工作；具体 retention/cleanup 合同仍须独立接受，不能在 active/shared 数据库借测试清理删除 Audit。未来非 Audit 临时状态清理也须由其 owner 冻结规则。旧候选的 179d23:59:59/exact 180d/181d 查询测试与 disposable 整库清理方案保留在 before 快照，不把候选细节写成接受记录。

Purge 不在当前范围，禁止为未设计的物理删除加入广泛 cascade。

## 12. 验证与证据

### 12.1 M1 与 M2 的不同证据

M1 按当前 capability matrix 验收替换必需接口，使用正式 `cmd/server` 和固定消费方：Unit/Proto/OpenAPI、真实依赖 Adapter Integration、正式进程端点、Gateway/Envoy/Inference/Session 等独立 caller 与真实 owner 业务链。Gateway→Session 是 ADR-0022 指定的首条跨服务参考链。Cookie/CSRF/Origin/refresh/retry 用 HTTP/gRPC 黑盒验证，不等待尚未开始的 UI；M2 再验证真实 Console/BOSS 接入、Core 旧身份 writer 裁剪和实际替换。

真实依赖使用固定镜像/digest，empty isolated PostgreSQL 从 Atlas 迁移并以受限 runtime role 执行业务/两 Tenant/query-mutation；Redis、NATS、Dex 等按被测能力真实接入。历史 CP0 真实旧 role/RLS 负向、Direct P2 无 RLS 证据与新 Human/Workload 验证不能互换，历史 differential 不作为恢复旧兼容行为的当前前置。

资源不足可使用共享测试基础设施，但工作树、数据库/owner/runtime role、Redis namespace、NATS Account 或完整隔离 subject/Stream/Consumer、Dex Client/identity、Workload trust/Credential 与 evidence 都要独立。不共享 mutable fixture，不做全局 flush，不依赖残留状态。应用验证预期基础设施，不能擅改现有 lane。无法隔离或缺少真实 owner 则对应 gate 为 `not_verified`。

静态/供应链门禁保留 lint/staticcheck、govulncheck、Buf lint/breaking、sqlc clean diff、Atlas empty-DB replay/checksum、SBOM、License inventory。任何 differential 只归一化随机 ID、时间、Token/Secret，比较 gRPC/public error、claims、PostgreSQL/Redis state 与 side effect；有意差异要逐项映射 accepted 决定。

### 12.2 仍有效的延期与停止边界

Argon2 latency/memory、CheckPermission load、全仓 race、广泛 Fuzz、production-scale 并发及 HA 等依 ADR-0021 留到生产硬化，缺失为 `not_verified`，不能宣称通过。当前事项点名的 ledger、Credential rotation、Authority version、Invitation/last-admin、recovery 单次状态迁移、outbox/consumer 并发和对应 package race 是功能正确性门禁，不能被广泛测试延期覆盖；跨服务交付仍是 at least once，不声称 distributed exactly once。

曾被用户判定有问题的 Gateway authz drift/protected Core path 旧门禁保留失败与理由，不能要求恢复旧断言，也不能静默改名为 pass。当前合同事项生成与 accepted 目标一致的可复现替代检查。缺少工具、依赖、caller 或必需证据时不通过对应 Gate；测试环境功能成功不等于 Production Ready。

## 13. 后续实际替换、切换与恢复约束

M1 不裁剪现有 Core writer、不要求前端已开始，也不失效现有 Credential。M1 后 Core 旧身份裁剪与前端接入是后续工作，可在各自隔离分支按明确范围推进，不互相虚构先后前置；实际流量切换和不可恢复删除另立精确范围。

实际替换按所有权拆事务：IAM 只写 IAM 数据库；Core 通过自身 seed/API/outbox 写 Tenant/Quota truth；基础设施 owner 管 trust registration/Credential/ACL；deployment owner 管流量/selector。不得跨 IAM/Core 数据库写入、双写、共享业务事务，或把 seed 当通用 fixture loader。Core 自有治理能力不能因为旧 tenant-service 曾管理身份就一并删除。

破坏性目标仍包括旧 Auth runtime/Proto/client、重叠身份入口、旧 Session/Refresh/blocklist/API Key 和 signing material 的退出，目标使用新 key 并只重发明确批准的 Credential。操作前必须冻结 owner/输入/目标/范围、恢复条件、固定 artifact、验收与停止条件，并取得 exact action 人工确认。整组切换前验证 target、新旧 Credential 正负向和完整 Audit，不能发布只有新 client 或只有删除旧 server 的中间态；不保留长期代码 fallback/兼容 shim。

已有恢复审批、snapshot/恢复演练及删除前恢复要求继续有效，必须在其保护的动作前满足。旧候选的外部 Lineage Registry、ReadyClaim/Receipt、CommitPONR 和长期 DR package 具体机制尚未接受；其全文保存在 Workload 设计指向的 before 快照，不再作为全部 M1 接口统一前置。将候选移出不恢复历史未演练 snapshot 的执行权限，也不允许失效 Credential 后靠恢复旧 key/trust 绕过失效。

任何实际切换、失效或删除所需恢复合同尚未接受/验证时，该动作保持不可执行；独立接口工作可继续。持续维护的长期 DR 和生产灾备依其真实保护范围另交付，不能把未接受扩展提前变成 M1 Gate，也不能把既有删除前恢复要求挪到删除后。

## 14. 明确延期清单

### 14.1 Production Readiness 前必须完成

- Argon2 profile benchmark、并发内存上限；
- CheckPermission 固定 workload load gate；
- 全仓 Race、广泛 Fuzz、负载与 production-scale 并发 suite；当前 accepted 事项点名的安全状态机定向 race/concurrency 门禁不在此延期项内；
- IAM/Core HA、NATS 三副本持久化/TLS/ACL/failover/backup restore；
- production-scale disaster/rollback rehearsal、生产 soak、正式 CORS、Secret/KMS rotation 演练；具体部署/失效/删除动作前已有恢复前置不因生产硬化延期而取消。尚未接受的长期 DR/lineage/break-glass 机制另按目标动作决策；
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

## 15. 完整替换能力与里程碑

本文不另行给票号或执行顺序，范围与分母以当前 spec/ticket graph/capability matrix 为准。完整替换仍覆盖：

- 独立 IAM 三服务与薄 Gateway、固定 OpenAPI/Proto/registry、唯一 operation/handler/owner、typed obligation；
- Human/Workload、Password/OIDC、Session/Refresh/logout、边界切换、API Key/必要 Workload 身份和 owner-specific Authority；
- Tenant/Platform Membership/Role/Binding、Permission catalog、Invitation、TenantAccess、Core Lifecycle/bootstrap 和已接受恢复能力；
- 独立数据库/no-RLS、明确状态与复合 Tenant 约束、空库迁移、受限 role；无 generic soft delete、member_count TCC、jwt_blocklist 或双写；
- Audit/mutation 原子、至少 180 天可查与增长监控；每个 state-changing endpoint 的重试/冲突/失效/response-loss 合同；
- 当前能力矩阵要求的独立 caller、真实 owner 正反向与故障恢复证据，不能用一个 caller 或 fixture backend 代替其他消费者。

**M1 API 替换就绪**：全部必需接口在精确最终版本组合的隔离正式进程与真实依赖上通过；Cookie/CSRF 等协议可黑盒验收，Core 旧 writer/UI 尚未裁剪或接入不单独阻断 M1。能力不得以缩票为由漏掉，也不把尚未接受的全部高级新增能力混作前置；争议项先在 capability matrix/decisions 决定。

**M2 实际替换完成**：在后续精确范围内完成 Core 旧身份 writer 退出、真实前端/消费者接入与实际替换验收。旧 runtime/contract/config/schema 和 Credential/流量的删除或失效按独立动作范围与前置办理，不能从 M1 或 M2 标签自动推出授权。所有必要功能结果为 pass，缺失为 not_verified；两者均不等于 Production Ready。

改变状态的事项先 claimed、同一时间只推进一个；文档整理本身不启动产品实现。本文与 spec、能力矩阵和 accepted ADR 冲突时显式记录差异，不使用历史候选正文补入默认值。
