# ANI IAM 重构规格

Status: ready-for-agent

本规格综合完整 Q1–Q300 grilling 对话、当前核心计划、领域词汇、accepted ADR，以及 ANI `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be` 的代码事实。若这些材料发生冲突，以用户在完整对话中的明确选择和本规格中记录的最新调整为准。

## Problem Statement

ANI 当前的 Auth、Tenant 管理、授权、Gateway、Quota 与调用方契约存在职责重叠和历史兼容路径：旧 Auth Service 同时承担登录、Token、API Key 和两代授权 RPC；Gateway 同时保留 legacy 与 generated policy；Tenant Lifecycle、Tenant Access、Membership、Role、Tenant Admin 和 Quota Assignment 的所有权分散且部分重叠；PostgreSQL RLS、应用作用域和跨服务调用之间缺少一套清晰一致的目标边界。

项目尚未上线且没有真实用户，因此这次工作的目标不是长期维持旧实现，而是在保留可验证兼容基线的前提下，分阶段建立独立、可测试、可删除旧实现的 IAM，并最终执行一次受控的破坏性目标切换。项目必须避免把 Kratos 框架验证、目标领域重构和生产就绪声明混成同一个不可验证的大批次。

本项目使用本地 Markdown 规格与事项、单一 `claimed` 状态变更事项和高风险人工确认的轻量流程，让实施约束可以直接从当前产物读取和维护。

## Solution

建立独立的 `iam-service` 项目，以 go-kratos 作为外层运行框架，以框架无关的业务层承载 ANI 身份、Credential、Session、Tenant Access、Membership、Role、Invitation、Service Principal、API Key、授权与安全审计规则。

交付分为三个严格区分的阶段：

1. CP0 只验证 Kratos 是否能够在真实 PostgreSQL/RLS、Redis、Dex 和三个真实调用方下复现固定旧 Auth 的 14 RPC 外部行为与持久化副作用，不改变目标数据模型，不切流。
2. P1 在 CP0 Go 后完成旧 Auth runtime 的同构替换，仍保持冻结的旧 wire、数据库和安全语义；调用方切换必须可独立回滚。
3. P2 才引入目标 IAM 契约、Core Control 生命周期所有权、独立数据库、无 RLS 数据模型、Session Grant、旋转 Refresh Token、生成式 Permission、单一 Gateway 授权决策以及旧契约和旧运行时删除。

执行工作使用 `.scratch` 中的本地事项：每个事项必须声明范围、依赖、外部行为验收、禁止事项、真实依赖和人工检查点；同一时间只允许一个会改变代码、数据、契约或外部状态的事项处于 `claimed`。解释、评审和只读调查不占用该执行槽位。任何破坏性契约变更、数据库重建、Credential 失效、调用方切流、旧实现删除和真实环境写操作都必须在执行前获得用户针对具体事项的明确确认。

## User Stories

1. 作为平台负责人，我希望先用 CP0 判断 Kratos 能否保持冻结的 Auth 安全语义，以便在投入全面重构前快速证伪框架路线。
2. 作为项目负责人，我希望 CP0、P1 和 P2 不自动互相解锁，以便每个阶段都能根据真实证据决定继续或停止。
3. 作为项目负责人，我希望执行流程只使用本地规格、编号事项和人工检查点，以便在有限时间内保持实施范围和风险可见。
4. 作为开发者，我希望每次只实现一个边界明确的事项，以便控制跨仓、契约、数据和安全改动的风险。
5. 作为审查者，我希望所有兼容结论绑定精确 ANI Commit、migration head、Proto descriptor 和依赖状态，以便当前 HEAD 或 mock 不能冒充兼容基线。
6. 作为 Gateway 调用方，我希望新实现对冻结 Auth 契约产生与旧实现相同的 gRPC 状态、公开错误、Claim 和副作用，以便 CP0 差分结果可信。
7. 作为 Envoy Adapter 调用方，我希望 ValidateToken 的 allow、deny、无效凭据和依赖不可用行为被独立验证，以便 Gateway 通过不能替代 Envoy 证据。
8. 作为 Inference Service 调用方，我希望 Service Token 签发和 Core service-only 调用链被独立验证，以便内部工作负载身份不会退化为共享 Secret。
9. 作为 Human Principal，我希望一个全局身份可以拥有多个 Tenant Membership 和可选 Platform Membership，以便无需为每个 Tenant 创建用户副本。
10. 作为 Human Principal，我希望多个登录 Identity 只能显式关联，并且相同邮箱不会自动合并账号，以便避免错误的身份接管。
11. 作为受邀用户，我希望 Invitation 在验证邮箱和身份前不创建 Membership 或授予权限，以便持有邀请 Token 不能等同于已认证身份。
12. 作为 Console 用户，我希望 Access Token 只保存在内存、Refresh Token 只存在于安全 HttpOnly Cookie，以便脚本无法读取长期 Credential。
13. 作为 BOSS 用户，我希望平台登录要求 active Platform Membership，并使用独立的 Token boundary、Cookie 和 Session 时限，以便租户身份不能通过前端路由提升为平台身份。
14. 作为多 Tenant 用户，我希望切换 Tenant 时重新验证 Principal、Tenant Access、Lifecycle、Membership 和 Session，以便客户端不能通过 Header 自选授权边界。
15. 作为用户，我希望 Refresh Token 每次成功使用后旋转，重复使用已消费 Token 会撤销该 Family，以便窃取和重放可以被限制在对应 boundary。
16. 作为用户，我希望普通 logout 只撤销当前 Session，而密码重置撤销该 Human Principal 的全部 Session，以便撤销范围符合用户预期。
17. 作为 Tenant 管理员，我希望一个 Membership 可以绑定多个 Role，最终权限为全部 active Role 的 allow-list 并集，以便不再受旧单角色模型限制。
18. 作为 Tenant 管理员，我希望系统 Role 的 code 和 Permission 集不可被 Tenant 修改，以便内置安全边界不会被本地覆盖。
19. 作为 Tenant 管理员，我希望初版只有 `tenant-admin` 可以管理自定义 Role 和 Role Binding，以便委派管理不会提前引入权限升级路径。
20. 作为 Tenant 管理员，我希望系统阻止移除最后一个 active Human tenant-admin，以便 Tenant 不会因并发变更失去全部管理员。
21. 作为平台恢复人员，我希望最后管理员恢复和 Recovery Bootstrap 使用独立能力、近期重新认证、双人审批和完整审计，以便普通管理员权限不能执行高风险恢复。
22. 作为 Service Principal 管理者，我希望 Service Principal 固定属于一个 Tenant，并通过 Membership 和 Role Binding 获得权限，以便 API Key 不成为无主体 Secret。
23. 作为 SDK 调用者，我希望 API Key 只通过 Bearer Credential 使用、Secret 只显示一次且数据库只存 Hash，以便 Credential 输入和保存方式唯一。
24. 作为安全人员，我希望 API Key 不能执行密码、Credential、Role、Recovery Bootstrap 或 Platform 管理等高风险操作，以便自动化身份的权限面受限。
25. 作为内部工作负载，我希望通过 mTLS 或 SPIFFE 身份换取 audience-bound、短期且不可刷新的 Service Token，以便内部调用不使用用户 Token 或 API Key。
26. 作为 Core Control，我希望独占 Tenant ID 和 Tenant Lifecycle 写入，以便 IAM 不成为第二个 Tenant 生命周期权威。
27. 作为 IAM，我希望独占 Tenant Access、Membership、Role、Invitation 和 Credential，以便安全状态不会与资源生命周期混成一张表。
28. 作为资源服务，我希望继续在资源边界执行权威 Lifecycle 和 Owner 检查，以便 IAM 的异步投影只承担早期拒绝而不是最终资源事实。
29. 作为 IAM 授权层，我希望通过版本化 Lifecycle 投影快速拒绝 inactive Tenant，以便普通授权请求不必同步调用 Core。
30. 作为运维人员，我希望缺失 Tenant Access 返回可重试的 IAM-not-ready 结果，而不是普通 403 或 not-found，以便就绪故障不会被误判为权限拒绝。
31. 作为 Tenant 创建流程，我希望 Core 提交 Lifecycle 和事务 outbox 后异步请求 IAM Bootstrap，以便 Core 与 IAM 不需要跨数据库事务或双写。
32. 作为消息消费者，我希望重复、旧版本和至少一次投递不会产生重复业务效果，以便跨项目传递无需虚假的 exactly-once 声明。
33. 作为运维人员，我希望版本 gap、projection stale、poison message 和 DLQ replay 都有明确状态和恢复方式，以便故障不会通过直接插库修复。
34. 作为 Gateway，我希望公开路由只执行一次 IAM 决策，并按 OpenAPI operation registry 选择 Public、Authenticated-only 或 Authorized 行为，以便 legacy 推导和默认 allow 最终可以删除。
35. 作为 Gateway，我希望移除所有客户端 `x-ani-*` 身份 Header，只在受信第一跳连接上注入最小 Principal Context，以便客户端无法伪造身份或 Role。
36. 作为资源 Handler，我希望 IAM 在无法判断权威 Owner 时返回 typed obligation，以便真正拥有资源数据的服务执行最终 Tenant/Owner 检查。
37. 作为数据安全审查者，我希望 P2 的所有 Tenant Repository 都必须接收不可提升的 TenantScope，并使用复合 Tenant 键、约束和负向测试，以便移除 RLS 后仍有可审查的隔离边界。
38. 作为平台操作者，我希望 Platform Repository 与 Tenant Repository 完全分离，以便空 Tenant ID、全零 UUID 或 Boolean bypass 不能表达平台权限。
39. 作为审计人员，我希望关键安全状态变更和 Audit 在同一事务提交，以便状态成功但审计缺失时操作不会被报告为成功。
40. 作为 API 调用者，我希望所有公开状态变更具有作用域明确的 Idempotency Key 和稳定重放结果，以便重试不会重复创建安全对象。
41. 作为测试负责人，我希望本地资源不足时可以使用共享测试基础设施，但每轮拥有独立数据库、角色、namespace、broker identity 和 Dex identity，以便共享基础设施不等于共享状态。
42. 作为发布负责人，我希望功能完成与 Production Readiness 分开，以便缺少性能、Race、Fuzz、HA、备份恢复和生产 soak 时不能误报生产就绪。
43. 作为维护者，我希望旧 Auth runtime、旧 Proto、legacy Gateway 授权路径、jwt_blocklist、重叠 Tenant Admin 入口和旧数据表在目标切换后真正删除，以便重构不会长期留下双轨。
44. 作为维护者，我希望已删除的历史材料不再提供执行默认值，以便当前规格成为唯一执行入口。

## Implementation Decisions

### 权威与阶段

- 本规格是当前唯一执行规格；完整 grilling 对话用于解释决定来源，核心计划和 ADR 用于交叉检查，不得覆盖本规格中的最新决定。
- 使用本地 Markdown 事项；状态至少包含 `ready-for-agent`、`claimed`、`resolved`。同一时间只推进一个改变代码、数据、契约或外部状态的事项。
- CP0 只改变框架和运行外壳；P1 只做旧语义同构替换；P2 才执行目标契约、目标数据和破坏性删除。
- 任一阶段发现需要修改相邻阶段语义、扩大事项范围或执行高风险外部写入时停止，并请求针对具体变化的新确认。
- ANI 基线 Commit 已确认存在并位于当前 main；Compatibility Oracle 仍需采集真实 Proto、数据库/RLS、Redis、Dex 和三个调用方证据。

### 项目与框架边界

- IAM 为独立项目和独立发布单元，可共享 PostgreSQL、Redis、Dex、NATS、Secret Manager、镜像和观测基础设施，但不 import ANI internal package，也不复制 ANI 大型 bootstrap。
- 使用 Kratos v3，并在 CP0 固定精确 patch 和依赖校验和；禁止跟随 `latest`。
- 使用 Kratos 标准顶层分层：biz 承载实体、用例和消费端 port；data 承载数据库、Redis、NATS 和外部 adapter；service 只做协议与 biz 映射；server 负责 transport 和内部管理 listener；composition root 使用显式构造函数。
- biz 不依赖 Kratos、Proto、数据库 driver、transport error 或 adapter 类型；标准 `context.Context` 可以贯穿 port。
- Use Case 通过 Unit of Work 拥有事务；Repository 不自行提交跨对象事务；transport handler 不控制数据库事务。
- 单进程注册 Authentication、Authorization 和 IAM Admin 三个内部 gRPC Service；不提供公共 IAM HTTP 业务服务，健康、就绪和指标使用独立内部 listener。
- CP0/P1 兼容代码隔离在 compat 边界；目标 biz 不 import 兼容 Proto；P2 删除整个兼容边界。

### 契约与 Gateway

- ANI 公网 OpenAPI 继续作为 IAM 公网路径的产品契约；这些身份与平台管理路径明确归类为平台控制面，不进入 Services 业务 OpenAPI。
- 公开变更顺序为 OpenAPI 授权标注、生成 operation/policy registry、Gateway/SDK、内部 gRPC 契约、实现。
- 一个 operation 只能有一个 Gateway Handler 和一个后端 Owner；目标切换删除 Core Tenant User、Services Tenant Admin、Tenant Plan 等重叠入口。
- Public 路由不调用 IAM；Authenticated-only 路由调用一次 ValidatePrincipal；Authorized 路由直接调用一次 CheckPermission，不先调用 Validate。
- Gateway 到 IAM 使用专用 mTLS/SPIFFE 身份；单次 IAM 热路径 deadline 为 500 ms，Gateway 不自动重试。
- operation registry 和 policy revision 由 OpenAPI 生成；未知 operation、缺失标注和版本不匹配 fail closed。迁移完成后不保留字符串推导或 legacy fallback。
- 无效 Credential 映射 401；身份有效但状态或权限拒绝映射 403；限流映射 429；依赖或投影不可用映射 503；IAM deadline 映射 504。
- Gateway 只注入最小可信 Principal/boundary/Session/decision 上下文，不注入 Role 或 Permission 列表，且下游不得转发成服务间 Credential。
- 权威资源所有权通过枚举化 typed obligation 交给资源 Owner Handler 强制检查；缺少对应 obligation Handler 的 operation 不可注册。

### 领域所有权

- Core Control 是 Tenant ID、Tenant Lifecycle 和 Quota 的唯一写入者；IAM 只引用 Core Tenant ID，不创建平行 Tenant。
- IAM 是 Tenant Access、Human/Service Principal、Identity、Credential、Session、Tenant/Platform Membership、Role、Role Binding、Invitation、API Key、Permission Catalog 和安全审计的唯一写入者。
- Tenant Lifecycle、Tenant Access、Tenant Membership 与 Quota Assignment 是不同概念和不同状态机。
- Human Principal 全局唯一；Service Principal 固定一个 Tenant；Platform Membership 只允许 Human Principal；内部 Workload 不制造 Platform Service Principal。
- Role Binding 绑定 Membership 而不是 Principal；多个 active Role 的 Permission 使用纯 allow-list 并集，不引入 deny、优先级或继承。
- Permission Catalog 来自生成的 operation registry；未知 Permission fail closed。Tenant System Role 按版本实例化，code、系统标记和 Permission 集不可由 Tenant 修改。
- 初版只有 `tenant-admin` 管理自定义 Role 和 Role Binding；delegated administrator 延期。
- 最后管理员只计算 active Human Principal、active Membership 和 active tenant-admin Binding；Service Principal 不计入。
- Invitation 与 Membership 分离。Invitation 在接受前不占成员配额、不授予权限；接受时重新验证 Role 并建立新的 Membership identity。
- 不开放自由注册。Human Principal 只能由 Tenant Invitation、Platform Invitation 或首管理员 Bootstrap 建立。

### Tenant Lifecycle 与异步 Bootstrap

- Core 创建 Tenant 并基于自身 Tenant/Quota 不变量推进 Lifecycle；不等待 IAM 首管理员完成。
- Core 在本地事务中同时提交 Lifecycle 事实和 outbound outbox，分别发布 LifecycleChanged 与 IAMBootstrapRequested 契约。
- IAM 幂等消费 Bootstrap，先建立 `bootstrap_pending` Tenant Access 和 operation；未知管理员邮箱保持 Invitation。
- verified Principal、active Membership 和 tenant-admin Binding 同事务成功后，Tenant Access 才转为 active。
- 缺少 Tenant Access 是 retryable IAM readiness failure，不是普通 403、not-found 或自动补行信号。
- Lifecycle 投影按单调 Tenant version 更新；重复和旧消息忽略；version gap 只冻结受影响 Tenant 并触发版本化 Core snapshot 修复。
- Pipeline 健康使用独立 consumer watermark/heartbeat，不使用单个 Tenant 行年龄；普通授权请求不回调 Core。
- Cross-project delivery 为 at-least-once；通过 event/operation identity、payload fingerprint、version、唯一约束和 CAS 消除重复业务效果。
- Poison message 先写 IAM 持久 DLQ 再 ack；replay 需要专用 Permission、reason 和 Audit，并回到 IAM durable worker。
- IAM Lifecycle projection 只做早期拒绝；Core/Services 继续在资源边界执行权威 Lifecycle guard。
- 真实 Core outbox、snapshot 和 NATS producer 属于 P2 Core Control 工作，不因 IAM fixture 测试而被宣称完成。

### Credential、Session 与浏览器

- CP0/P1 保留基线 bcrypt；P2 新密码使用 Argon2id 64 MiB、t=3、p=4、16-byte salt、32-byte tag，并保存算法和参数。显式导入 bcrypt 只在成功登录后 rehash。
- Seed 不保存默认明文密码，只产生一次性、30 分钟有效的设置动作。
- Password 登录同时按 normalized account 和 IP 限流；连续五次失败锁定十五分钟；错误响应不枚举账号。
- OIDC 使用 Authorization Code、PKCE S256、state、nonce 和 verifier；临时状态十分钟单次使用。只有 trusted issuer、audience、signature、nonce 和 verified email 全部通过后才能建立身份。
- Console Access Token 十五分钟，Session idle 七天、absolute 三十天；BOSS Access Token 十分钟，Session idle 三十分钟、absolute 八小时。
- Access Token 只包含稳定身份、Session/Grant、boundary 和认证方法 Claim，不包含 Role、Permission 或全部 Membership。
- Session 是 Principal-wide；Session Grant 是单 Tenant 或 Platform boundary；每个 Session+boundary 最多一个 active Refresh Family。
- Refresh Token 单次旋转；reuse 撤销 Family 并增加 Grant version，只使对应 boundary 的 Access Token 失效。
- P2 不保留 per-jti jwt_blocklist；PostgreSQL Session/Grant/Family/Token state 是在线授权事实源。
- 普通 logout 幂等撤销当前 Session；Password Reset 撤销该 Human 的全部 Session 和 Family。
- Service Token 仅由 allowlisted mTLS/SPIFFE Workload 获得，最长五分钟、audience-bound、permission subset、不可 refresh 且无 Session。
- 浏览器 Access Token 只保存在进程内存；Refresh Token 只在 host-scoped Secure HttpOnly SameSite=Lax Cookie 中，Console 和 BOSS 使用不同名称、Path 和 Audience。
- Refresh、Logout、SwitchTenant 和 OIDC 必须校验精确 Origin/Referer、独立 CSRF Cookie/Header 和固定 Redirect URI。
- 多 Tab 使用 Web Locks 或 BroadcastChannel 协调 single-flight refresh；401 后最多 refresh 一次并只重试原请求一次。
- 外部 SDK 使用 Service Principal API Key；内部服务使用 Service Token；基础版本不向 CLI 返回 Human Refresh Token。
- Access/Service JWT 使用 Secret Manager/KMS 中的非对称 Key 和 `kid`；retired public key 保留至已签 Token 全部过期；当前不发布公共 ANI JWKS。

### Service Principal 与 API Key

- Service Principal base/profile、固定 Tenant Membership、初始 Role Binding 和 Audit 原子提交；API Key 由后续独立操作创建。
- API Key 由非 Secret key ID 和高熵 Secret 组成；Secret 只在创建响应返回一次，数据库仅存 Hash 和安全显示片段。
- P2 只接受 Bearer API Key；移除 `X-API-Key`、query 和 Cookie 输入。
- API Key 必须在 never-expire 与明确 expiry 中二选一；基础版本不限制 active Key 数量，但执行创建限流、分页和异常数量告警。
- Permission 永远来自当前 Membership Role Binding，不在 Key 上保存权限快照或自定义 rate-limit 权限字段。
- disable Service Principal 时同事务不可逆 revoke 全部 Key；重新 enable 不复活旧 Secret。

### 数据与持久化

- Core Control 与 IAM 使用独立 PostgreSQL 数据库，不建立跨数据库 FK；IAM 表保存稳定 Core Tenant ID。
- P2 IAM 数据不使用 PostgreSQL RLS。隔离由 non-null Tenant ID、TenantScope Repository、复合 Tenant key/FK、受限 runtime role、双 Tenant 负向测试和 query mutation test 共同承担，并明确接受 runtime Credential 被攻破后可访问其 DML grant 覆盖行的残余风险。
- Migration owner 与 runtime DML role 分离；runtime 非 owner、非 superuser、无 DDL 权限。Migration 由预部署任务执行，runtime 只验证 schema revision。
- 使用 sqlc 生成 pgx/v5 查询 API；SQL 是 Tenant、状态、锁和版本条件的安全评审来源。目标不使用 GORM。
- 使用 Atlas versioned migration 与 checksum；基础门禁要求真实空 PostgreSQL apply/replay，不假设付费 lint 能力。
- 使用明确安全状态而不是通用 `deleted_at`；安全关系使用 restrictive FK，不用广泛 cascade。
- mutable aggregate 使用 version；普通事务使用 READ COMMITTED 加 row lock/version，Tenant 不变量使用 guard lock 或窄 serializable transaction。
- 所有公开 mutation 使用按 boundary、actor、operation、key 和 request hash 作用域的二十四小时幂等 ledger。同 key 异请求冲突；逻辑过期后复用返回稳定 expired 冲突。
- Audit 与 mutation 同事务；Audit details 只允许脱敏 allowlist，不保存 Password、Token、Key Secret/Hash、完整邮件或任意请求响应。
- Audit 初版只提供 List/Get 和 cursor pagination，无 export；至少在线可查一百八十天。自动 retention cleanup、Purge 和生产合规机制延期。

### 成熟依赖

- JOSE/JWT/JWK 使用稳定的 lestrrat-go/jwx adapter；OIDC 使用 go-oidc/v3 与 x/oauth2；禁止自写协议和密码学实现。
- Argon2id 使用基于 x/crypto/argon2、支持 PHC 编解码和测试向量的维护中薄库。
- UUIDv7 使用 google/uuid 并置于 IDGenerator port 后。
- Redis 使用 go-redis/v9；NATS JetStream 使用官方 nats.go/jetstream API；应用只使用基础设施创建好的 Stream/Consumer。
- Observability 使用 Kratos middleware、OpenTelemetry 与 Prometheus；项目只定义 ANI semantic attribute 和领域指标 allowlist。
- 所有依赖固定精确版本，记录许可证，进入 SBOM 和漏洞扫描；只在防止库类型泄漏进 biz 或稳定领域边界时包装。

## Testing Decisions

- 统一最高测试缝是“真实调用方进入 IAM 外部契约，再观察公开结果和持久化副作用”。Gateway、Envoy 和 Inference 是同一验收缝的三个独立入口，任一入口通过都不能替代另外两个。
- CP0 使用冻结旧 Auth 作为 oracle，对新旧实现执行相同输入并比较 gRPC status、公开错误、Claims、PostgreSQL/RLS 状态、Redis 状态和外部副作用。
- 差分测试只归一化随机 ID、时间和 Token/Secret；预期差异必须进入具名 allowlist，不能只比较成功或失败。
- CP0 必须使用真实但隔离的 PostgreSQL/RLS、Redis 和 Dex；fake/unit 只提供开发反馈，不构成 Go/No-Go 证据。
- CP0 固定验证十四个旧 Auth RPC，并覆盖 Password、OIDC、Refresh、Revoke、Validate、两代 Permission、Service Token 和 API Key。
- P1 对每个调用方验证 selector/DNS/env 切换和回滚，不允许新旧 runtime 长期双写。
- P2 在新无 RLS 数据库中从空库回放迁移，使用受限 runtime role，执行双 Tenant 负向、跨 Tenant 关系约束和 query mutation 测试。
- Adapter Integration 默认使用固定镜像摘要的 testcontainers-go；完整调用拓扑使用隔离 Compose 或 CI 环境。
- 共享测试基础设施只有在每轮拥有独立 PostgreSQL database/role、Redis namespace、NATS Account 或完整隔离名称、Dex client/identity 和 Credential 时才可作为证据。
- 每轮测试必须创建声明的前置状态、支持并发运行且只清理自身资源；共享 mutable fixture、全局 Redis flush、复用 durable Consumer 或共享测试用户无效。
- Gateway、Envoy 和 Inference 分别覆盖有效 Credential、成功、401、403、503、timeout 和 policy revision mismatch。
- Invitation、Refresh、Last-admin、Role version、Idempotency 和 Recovery 测试必须覆盖成功路径、拒绝路径、重复提交和冲突语义。
- Lifecycle/Bootstrap 测试必须覆盖 duplicate、older version、version gap、stale pipeline、snapshot repair、poison message、DLQ persistence、replay 和缺失 Tenant Access。
- Browser 测试覆盖 HttpOnly Cookie、CSRF、Origin/Referer、固定 redirect、single-flight、多 Tab、旋转响应丢失、401 单次 retry 和 503 暂态行为。
- Static/supply-chain 门禁包括仓库选择的 lint/static analysis、govulncheck、Buf lint/breaking、sqlc clean generation、Atlas empty-DB replay/checksum、SBOM 和 license inventory。
- Argon2 latency/memory、CheckPermission load、go test -race、Fuzz 和专项并发 suite 暂不阻塞第一版功能实现；未完成时必须记为 `not_verified`，并阻止任何 Production Ready 声明。
- 测试只断言外部行为和持久状态，不以 Kratos handler、Repository 私有方法、目录结构或 mock 调用次数作为主要验收标准。

## Out of Scope

- 当前不建立正式生产环境，也不声明 Production Ready。
- 不在 CP0 中引入 P2 Proto、目标 Schema、Argon2 migration、NATS Lifecycle、Core Control 拆分或旧代码删除。
- 不在本规格基础版本中实现 Tenant/Principal Purge。
- 不实现 member_count Quota/TCC。
- 不实现 delegated Role administration。
- 不实现 Support Session。
- 不实现 Human CLI Device Authorization Flow 或 JSON Refresh Token。
- 不实现 Audit export、SIEM、WORM 或密码学不可抵赖。
- 不发布公共 ANI JWKS，也不构建离线 ANI Token verifier。
- 不预建没有明确消费者的通用 IAM domain-event outbox。
- 不实现正式 retention cleanup、生产 CORS、生产 KMS rotation、HA、backup/restore、failover 或 production soak。
- 不把 IAM fixture、单元测试或单一调用方成功描述为 Core integration、runtime readiness 或 production readiness。

## Further Notes

- 完整 grilling 对话包含 Q1–Q300 的问题正文、选项、用户回答和后续修订，是解释历史决定的完整证据。新规格综合有效决定，不要求执行者重读全部对话。
- 当前无版本号计划提供 grilling 后的详细技术设计，并与本规格共同维护。
- 现有 CONTEXT 与 ADR 继续提供领域术语和取舍背景；与本规格冲突时应提出修订或删除，不得从已删除材料补入默认规则。
- ANI 基线 Git 身份已确认；完整 Compatibility Oracle 仍需在第一个 baseline 事项中采集和保存。
- `ready-for-agent` 表示规格已足够拆分事项，不表示任何代码、数据库、外部系统或破坏性操作已经获得执行授权。
