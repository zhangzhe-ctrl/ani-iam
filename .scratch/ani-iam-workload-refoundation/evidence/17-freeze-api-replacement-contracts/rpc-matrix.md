# WR-17 能力与正式入口冻结

基线 `cd38cd90bca3e9d83af09a381051b895d82ae94f`；逐 RPC 的源码路径/行、目标名称、handler、正式白名单与负责票见 [rpc-inventory.json](rpc-inventory.json)。本表是静态审计，不产生运行 pass。

## 本阶段分母

- 三个服务共 69 个声明：Authentication 15、Authorization 1、Admin 53；有 handler 的分别为 11、1、17，共 29。
- 11 个 handler 被正式白名单拒绝：RefreshSession、LogoutSession、SwitchTenant、ValidatePrincipal，以及七个 Workload/API Key 管理方法。
- 29 个 handler 不等于 29 个安全可用入口。Admin 目前使用 `x-ani-principal-id`/`x-ani-tenant-id` 等请求头建 actor/scope；GetTenantAccess 无业务授权检查。对应源码 `internal/service/iam_admin.go:54,270,564,605`。
- WR-18 正式可用集合为现有 29 个 handler 中的 **27 个**，其中 ServicePrincipal 名称按 ADR-0022 替换为 TenantWorkload。全部需要逐方法成功/拒绝证据；OIDC Provider/Notification 跨 owner 完整性仍归 WR-20。
- GetTenantAccess/UpdateTenantAccess 的有效 owner registry 要求 **Platform Human** 的 `iam.tenant-access:read/update`。不能改成 Tenant 权限或把 Gateway 当管理员来凑成功。WR-18 明确拒绝这两条未完成的 Platform 路径，WR-22 补齐其真实业务权限、登录及管理链。其底层 TenantAccess UoW 保留并回归。
- 其余 40 个声明继续登记为后续待实现，禁止空成功；不移出 M1 能力分母。WAT 的 IssueWorkloadToken 声明为 WR-19 合同入口，WR-18 不开放。

## C01–C14 对照

| 能力 | 旧能力 / 目标接口及 owner/caller | 保留、修改与负责票 | 成功、拒绝、故障、恢复的判定 |
| --- | --- | --- | --- |
| C01 | 旧 Auth server → IAM cmd/server / 三个 iam.v1 service；Gateway 是直接 Workload caller | WR-18：实际 profile、TLS、DB/Redis/readiness、27 RPC；WR-24 验全部 caller | 正式二进制响应目标 RPC；无绑定/错误 peer/无 Grant/未知方法拒绝；DB/Redis 断开 fail closed；依赖恢复与 SIGTERM 关闭无泄漏 |
| C02 | 旧 Auth 密码登录/重置 → PasswordLogin、RequestPasswordAction、CompletePasswordAction；IAM owner、Gateway caller | WR-18 保留现有 Human 与 Argon2id；WR-20 补全替换证明 | 正确密码建立持久 Session；错误密码/未验证/锁定拒绝且不泄漏账户；Audit/DB 失败无半个 Session；重置一次消费并撤销全部 Session，密码动作幂等恢复 |
| C03 | 旧 Auth/Dex 回调 → Begin/CompleteOIDCLogin、Begin/CompleteOIDCIdentityLink；IAM flow owner，Gateway 固定回调 | WR-18 保留实现；WR-20 真实 Dex/OIDC/link | 正确 code+PKCE 建 Identity/Session；issuer/audience/state/nonce/cross-flow/replay 拒绝；Provider 故障稳定映射；已消费 code 不重复 exchange、重启按原 flow 状态恢复 |
| C04 | 旧 refresh/logout/switch → RefreshSession、LogoutSession、SwitchTenant；ListSessions/RevokeSession/RevokeAllSessions 后续；IAM owner | WR-18 前三者正式入口；WR-20 完整浏览器与 Session 管理；WR-24 Gateway | rotation 成功、并发单 winner；旧 refresh reuse 撤销 family、跨 Tenant/Platform 拒绝；丢失旋转响应重新登录；Logout 重复不泄漏，Cookie/CSRF/Origin/303 在真实 Gateway 验 |
| C05 | 旧 ValidateToken/权限评估 → ValidatePrincipal、CheckPermission；IAM decision，资源 owner guard | WR-18 当前 Human/API Key 与管理授权；WR-22/23/24 全边界 | 当前 Membership/Role 允许；跨 Tenant、未登记 operation、错误 policy/version 拒绝；生命周期/依赖失败 503；撤销权限后下一次判定生效，真实 resourceTenant 由 owner 核验 |
| C06 | Core 旧用户/成员/角色 → IAMAdminService 的 Membership、Role/Binding、Invitation、TenantAccess、Platform 及两个恢复协议 | WR-18 当前 Tenant 管理的必要修复；WR-22 全管理，WR-23 Bootstrap | 当前有效管理员可变更；未授权/最后 Human 管理员移除/跨 Tenant 拒绝；业务与 Audit 回滚；版本冲突、邀请一次消费、恢复 requester/approver/重认证/审计成立。现存旧 writer 保留至 WR-26 |
| C07 | 旧 API Key/ServicePrincipal → Create/Get/List/UpdateTenantWorkload、Create/List/RevokeAPIKey；IAM owner，Gateway/Envoy consumer | WR-18 已有逻辑适配、持久幂等和 immutable owner/name；WR-21/24 完整链 | Workload+Membership+Audit 原子创建；不继承 creator 权限；重复 Key 请求不多发 Secret；过期/撤销/跨 Tenant 拒绝；响应丢失重放只返回 Key 元数据，可显式 revoke 后用新 key 建新 Credential |
| C08 | 原未完成 Service Token → IssueWorkloadToken、Workload Authentication/Authorization；Gateway→Session 首链 | WR-18 schema/repo/受控 fixture 基础；WR-19 真实引导、WAT/委托与 Session | 已绑定 peer+当前 Grant 可签发；错误 audience/operation/peer/subject/目标拒绝；IAM 不可用拒绝；Binding/Grant 撤销与轮换按当前状态判定；全部由 D01/D02 接受后实现 |
| C09 | Core Tenant 创建/状态 → Bootstrap 事实、Lifecycle 事实、Heartbeat、REST Snapshot；Core owner，IAM consumer | WR-23；现有 candidate gRPC 描述符只作历史输入 | 真实 Core outbox→NATS→IAM 原子消费；伪造 producer/乱序/gap 拒绝或冻结；DLQ/Snapshot 故障 fail closed；全量重建+增量追平、24h shadow/p99≤5s/无 gap 后才认定强制投影 gate |
| C10 | 密码/邀请通知 → IAM purpose-specific outbox → Notification SubmitNotification/GetSubmissionStatus → SMTP sink | WR-18 保留现有 outbox；WR-20/22/23 完整 owner 链 | 本地意图/Audit 同事务；非法 producer/收件目的不匹配拒绝；投递失败不误称消费；丢失接受响应按 submission key 去重，重启继续投递 |
| C11 | 旧安全日志 → IAM security_audit_events、List/GetAuditEvent 与 Platform 对应接口 | WR-18 原子 append-only/provenance；WR-22 查询/至少180天 | 变更携带真实 actor/caller；runtime 无 UPDATE/DELETE Audit 权限、跨 Tenant 不可查；Audit 插入失败业务回滚；无 raw Credential/邮件/任意请求体，恢复查询可追溯 |
| C12 | 旧重试/版本 → 每个 endpoint 的冻结 retry 合同 | WR-18 当前八类 Tenant 管理动作及现有 Session/Password/OIDC 状态机；后续各票自有业务幂等 | 同 scoped-key 同意图24h同 non-secret outcome；不同意图409；逻辑过期409；并发/中断无重复副作用；Secret/Refresh/OIDC 不按普通 replay 偷换状态机 |
| C13 | Console/BOSS REST、Gateway、Envoy、Inference、Session 后台调用面 → 固定 owner OpenAPI/gRPC | WR-17 固定输入；WR-19 首链；WR-24 全后台；UI 在 WR-27 | 所有必需 caller 使用同一版本合同；旧 fallback、普通 Header 自报身份、不符路由拒绝；401/403/503/504 准确；重试/并发/撤销真实链验证 |
| C14 | 旧部署/fixture → 隔离空库、独立身份/依赖、正式二进制 | WR-17 manifest；WR-18 clean install；WR-25 整组复现 | 固定源码/tool/digest 可复现；runtime 不是 owner、跨表/tenant 负向成立；SSH 中断先查原 run；只清本 Goal 资源，保留记录 |

## 本阶段共同 RPC gate

每个交付 RPC 均记录以下独立等级：service/unit、真实 PG/Redis、正式 cmd/server。缺任一级就不能将该级写为 pass。错误 credential 是 401/Unauthenticated；有效身份但无权限是 403/PermissionDenied；依赖失败是 503/Unavailable；deadline 是 504/DeadlineExceeded。TLS 握手失败只记传输拒绝，不伪造业务 Audit。

管理接口拒绝用请求 `x-ani-*` 作为 Authority：Gateway 直接 peer 经 IAM Workload Binding/当前 Grant 认证；IAM 自身验证传入 Human access credential 的当前 Session/Membership/Role，再产生 typed actor/scope。Subject Credential 仅交给 IAM 认证域，不转发到资源服务，也不替代直接 Workload Credential。

生成 registry 会从固定 ANI 公共 registry 制作 **IAM 隔离目标候选副本**，仅修改已接受 Workload 分类和相应 IAM operation/resource 命名。源副本 SHA、变换明细、目标 SHA 分别记录；不会谎称 ANI 已采用新 registry。实际跨仓同步由 WR-19/24 各自负责。
