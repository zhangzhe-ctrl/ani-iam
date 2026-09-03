# ANI IAM Direct P2 Ticket Plan

Status: accepted / published

用户于 2026-09-03 接受本拆分、依赖边和交付行为。DP2-01–DP2-20 已发布到 `.scratch/ani-iam-p2-direct/issues/` 并设为 `ready-for-agent`；随后用户精确授权并完成 DP2-00，归档并移除未投入使用的事项03/04实验代码。发布和前置清理不等于领取后续事项，DP2-01 仍未启动。

## Proposed Tickets

| ID | 纵向交付行为 | Blocked by | 阶段/门禁 |
| --- | --- | --- | --- |
| DP2-00 | 归档事项03/04实验并恢复干净 Kratos 实现树；不删除旧部署资产 | 无 | Direct P2 preflight |
| DP2-01 | 固定 ANI 来源、当前调用面、旧 RLS 缺陷、允许漂移与删除清单；输出 Direct P2 可执行基线 | 00 | DP2-0 |
| DP2-02 | 冻结公开 OpenAPI、operation owner/authn/authz/obligation、生成 registry 和 breaking 结果 | 01 | DP2-0 |
| DP2-03 | 发布目标 IAM 三服务与 Core Lifecycle/Bootstrap/Snapshot 的不可变契约、错误和 fixtures | 01 | DP2-0 |
| DP2-04 | 建立可空库重放的无 RLS PostgreSQL、受限 runtime role、UoW/Audit、两 Tenant 测试基础 | 02, 03 | DP2-1 |
| DP2-05 | 跑通 Password→Session→CheckPermission→目标 Gateway→受保护 API 的真实纵向链路 | 02, 03, 04 | **Go/No-Go A** |
| DP2-06 | 交付目标 Password 设置、Argon2id 登录、锁定、重置和撤销边界 | 05 | DP2-2 |
| DP2-07 | 交付 OIDC PKCE 登录、verified email 和显式 Identity Link | 05 | DP2-2 |
| DP2-08 | 交付 Session/Grant、旋转 Refresh、reuse、Logout、SwitchTenant 和浏览器 Cookie/CSRF | 06, 07 | DP2-2 |
| DP2-09 | 交付 Tenant Access、Membership、基础 Role/Binding、Permission 和一次 Gateway 决策 | 05 | DP2-2 |
| DP2-10 | 交付单 Tenant Service Principal、Bearer API Key 生命周期与 Envoy 验证 | 04, 09 | DP2-2 |
| DP2-11 | 交付 mTLS/SPIFFE Workload Service Token 和 Inference 调用链 | 03, 04 | DP2-2 |
| DP2-12 | 交付 Core Lifecycle 投影、Bootstrap、Outbox/NATS、gap repair、heartbeat 和 DLQ | 03, 04, 09 | DP2-2 |
| DP2-13 | 完成 Gateway、Envoy、Inference、Console、BOSS 当前切换关键调用面对等 | 08, 09, 10, 11, 12 | DP2-2 |
| DP2-14 | 在隔离测试轨道整组切入目标 IAM 并完成部署级回退，不删旧资产、不失效 Credential | 13 | **Go/No-Go B** |
| DP2-15 | 交付完整 Invitation、Role/Binding、Platform/BOSS 管理和双人高风险恢复 | 14 | DP2-3 |
| DP2-16 | 交付 Audit 查询/180 天语义和全部公开 mutation 的统一 Idempotency Ledger | 15 | DP2-3 |
| DP2-17 | 完成目标 Gateway/Envoy/Inference/Console/BOSS 全功能 UI/E2E 和删除前 zero-reference | 15, 16 | DP2-3 |
| DP2-18 | 经精确人工确认后快照/seed、重建测试 IAM/Core 数据、失效旧 Credential 并最终整组切换 | 17 | DP2-4 |
| DP2-19 | 经精确人工确认后删除旧 Auth runtime/Proto/compat/RLS/schema/重叠入口 | 18 | DP2-4 |
| DP2-20 | 聚合 clean install、功能矩阵、五类调用方和删除证据，形成非 Production Ready 的功能验收 | 19 | DP2-4 |

## Split Rationale

- DP2-05 是最小目标 tracer bullet；它横跨契约、数据库、认证、Session、授权、Gateway 和测试，而不是按层横切。
- DP2-06–DP2-12 只完成整组切换所需能力；完整 Platform/Recovery/Audit 查询推迟到 DP2-14 证明切换可行之后。
- DP2-13 先聚合五类调用方对等，DP2-14 再验证部署切入/回退；这样可以区分“代码都能调用”与“系统能整组替换”。
- DP2-18、DP2-19 分开，避免切流成功自动授权删除；DP2-20 只验收，不夹带修复或生产就绪工作。

## Dependency Audit

依赖只指向更小 ID，因此已发布图无环。一次性 DP2-00 已解决，当前唯一 frontier 是 DP2-01。`ready-for-agent` 只表示规格完整；执行器仍必须检查 `Blocked by`，且每次只能领取一个事项。

## Accepted Delivery Behavior

用户已确认：

1. 接受 20 张纵向票的粒度；
2. 接受 DP2-05 与 DP2-14 两道早期 Go/No-Go；
3. 接受 DP2-14 只做隔离测试轨道演练、不失效旧 Credential；
4. 接受 DP2-18 与 DP2-19 分别要求精确人工确认；
5. 接受发布后全部票为 `ready-for-agent`，但只允许 frontier 被领取；随后精确接受一次性 DP2-00 基线清理。

DP2-00 的接受只授权本地实验归档和实现树清理，没有领取 DP2-01，也不授权外部系统、切流、重建、Credential 失效或旧部署资产删除。
