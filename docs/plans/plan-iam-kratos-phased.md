# ANI IAM Kratos 分阶段替换方案

> 状态：**Replanned / Direct P2 已接受并发布；DP2-00 基线清理执行中**
>
> 编制日期：2026-09-01
>
> 目标设计：`plan-iam-service-refactor.md`
>
> 当前规格：`../../.scratch/ani-iam-p2-direct/spec.md`（已接受）
>
> Ticket graph：`../../.scratch/ani-iam-p2-direct/ticket-plan.md`（已接受；DP2-00 执行中，DP2-01–20 已发布）
>
> ANI 来源候选：Git object `0cedae825a489d936cf41815dc27f278f6d3213c`；不得使用动态 `main`、当前分支或工作树

## 1. 阶段原则

原计划通过兼容阶段拆分风险；事项04已经给出真实旧 RLS 的负向结果。当前重排不把失败改成通过，而是停止 CP0/P1 并把 Direct P2 的可切换性验证前置：

```text
历史 01-04    固定旧 Oracle、Kratos scaffold、旧 transport、真实 RLS 负向结论
DP2-0         固定来源、公开契约和 IAM/Core 契约
DP2-1         目标无 RLS 持久化与最小纵向链路；Go/No-Go A
DP2-2         切换关键功能与五类调用方整组演练；Go/No-Go B
DP2-3         完整 IAM 管理、恢复、审计、幂等和 UI
DP2-4         最终测试数据重建、切换、删除和功能验收
PR0           未来 Production Readiness，不属于当前功能交付
```

现有 CP0/P1 代码和证据是历史资产；DP2-00 先归档并从 Direct P2 实现树移除未投入使用的 compat/RLS 调查代码，历史证据继续保留。旧 ANI/Auth 部署资产仍只能在 DP2-19 经独立确认后删除。每个阶段都有独立入口、出口、停止条件和人工解锁；Go/No-Go A 不自动解锁 Go/No-Go B，Go/No-Go B 也不自动授权最终切流和删除。

基线准备、CP0、P1 和 P2 都拆成有界本地事项。任何时刻只允许一个会改变状态的事项处于 `claimed`；数据重建、Credential 全失效、切流和旧资产删除还需要在执行前获得针对精确目标和动作的人工确认。不同仓库同时存在事项不代表可以并行改变共享状态。

## 2. 固定基线与 Oracle

### 2.1 基线身份

旧 CP0 Oracle 保留为历史证据；Direct P2 的来源候选是：

```text
repository: ANI
branch intent at verification time: main
commit: 0cedae825a489d936cf41815dc27f278f6d3213c
source: remote main identity locally verified on 2026-09-03
runtime compatibility: known-failing legacy Auth RLS; not a Go oracle
```

该对象已确认存在且验证时对应远端 `main`，但后续分支和工作树已经漂移，因此只能从 Git object 读取。Direct P2 DP2-01 必须固定最终来源摘要，记录 Auth 14 RPC/调用方保持项、OpenAPI 已批准差异、旧 RLS deny-all 和旧 migration checksum 不完整；不得把“最新”或脏工作树当成 Oracle。

Direct P2 从目标契约和新空数据库建立可运行基线。旧来源缺陷只用于解释替换范围，不进入目标通过证据。

### 2.2 有问题的旧门禁

Gateway authz generated drift 和 protected Core path 检查不再作为“必须恢复旧断言”阻塞 CP0，因为用户已认定门禁本身有问题。但状态不是 pass。首个基线事项必须生成替代检查记录：

| 字段 | 要求 |
| --- | --- |
| old_gate | 原命令、断言和失败输出 |
| invalid_reason | 为什么断言与删除/替换目标冲突或实现有误 |
| target_contract | 当前方案的 operation/owner/authz 决策 |
| replacement_gate | 可复现的新断言、fixture 或生成检查 |
| reviewer | 人工接受引用 |

Replacement gate 被接受后，旧脚本可继续红而不阻塞 CP0；禁止静默跳过、删测试或把红灯改名为绿灯。

## 3. 实施前基线事项

### 3.1 允许

- 在 `.scratch/ani-iam-rebuild/issues/` 建立基线事项和证据索引；
- 验证用户指定 baseline SHA；
- 盘点旧 14 RPC、Proto descriptor、数据库 schema/role/RLS、Redis key/TTL、Dex/OIDC、Gateway/Envoy/Inference 调用；
- 记录旧门禁失效理由和 replacement gate；
- 固定 CP0 allow/deny paths、版本和真实依赖配置；
- 输出 `READY FOR HUMAN REVIEW`。

### 3.2 禁止

- 创建或修改 IAM runtime、Kratos scaffold、Proto、migration、deployment；
- 拆 Core Control、创建 NATS Stream、实现 Lifecycle projection；
- 改 Gateway、Envoy、Inference；
- 生成 `decision: GO` 或自行解锁 CP0。

### 3.3 出口

只有基线事项的证据得到人工明确接受，才可把 CP0-0 事项设为 `claimed`。文档生成本身不是该批准。

## 4. CP0：隔离 Kratos Compatibility Spike（历史停止路线）

### 4.1 唯一问题

> 在保持固定 Auth wire、安全、存储和调用方语义时，go-kratos v3 能否替代当前 transport/runtime？

唯一自变量是 Kratos。CP0 不引入 `sa-token-go`、目标 P2 Proto、Argon2id、sqlc 目标 schema、无 RLS、Core Control、NATS、Tenant Lifecycle projection、目标 Session Grant 或新 API Key 模型。

CP0 使用独立临时项目/目录和隔离配置，不改旧 Auth 唯一写路径。可以读取固定 snapshot；写路径只写隔离 PostgreSQL/Redis。

### 4.2 Oracle

保留基线的：

- 旧 Auth Proto descriptor 和 14 RPC wire；
- JWT signing/claims、bcrypt、Refresh 非轮换语义；
- OIDC/Dex state、nonce、callback 和 JWKS；
- API Key input/hash/status；
- PostgreSQL schema、runtime role 和真实 RLS；
- Redis key、TTL、Blocklist/Session/limit 行为；
- Gateway、Envoy、Inference 的请求/错误/副作用。

旧 14 RPC inventory 在首个基线事项中以 descriptor 为准，预期包含：Login、PlatformPasswordLogin、BeginOIDCLogin、CompleteOIDCLogin、RefreshToken、RevokeToken、ValidateToken、ValidatePrincipal、IssueServiceToken、CheckPermission、CheckPermissionV2、CreateAPIKey、ListAPIKeys、RevokeAPIKey。

### 4.3 实施事项

| 事项 | Ticket | 范围 | 必需证据 | Stop 条件 |
| --- | --- | --- | --- | --- |
| CP0-0 | 02 | 独立 Kratos v3 scaffold、固定 patch、typed config、显式 constructors | build、dependency/license/SBOM、Kratos lifecycle/health | 版本或许可证不接受 |
| CP0-1 | 03 | 旧 Proto gRPC transport、deadline/error mapping、compat package | descriptor diff、14 RPC registration inventory | wire drift 无法隔离 |
| CP0-2 | 05、06、07 | 现有 JWT/OIDC/Password/Refresh/API Key adapter | golden token、Dex callback、Secret redaction | 安全语义必须重写才可运行 |
| CP0-3 | 04 | 真实 PG role/RLS 与 Redis adapter | real dependency integration、RLS cross-Tenant、Redis TTL/state | 只能用 fake 或超级用户 |
| CP0-4 | 05、06、07、08 | 七条代表性纵向链路和 differential harness | normalized response/state/side-effect diff | 出现未批准差异 |
| CP0-5 | 09 | Gateway、Envoy、Inference 三独立 live gates；总结 Go/No-Go | commits/digests/config/evidence | 任一 caller 未验证 |

七条纵向链路至少覆盖 Password Login、OIDC Login、Refresh/Revoke、ValidatePrincipal、Permission allow/deny、API Key、Service Token。

### 4.4 差分

只归一化随机 ID、时间和 Secret。必须比较 gRPC code/detail、公开错误、Claims、PostgreSQL/Redis 副作用、TTL 和调用方结果。每个预期差异需列明“旧值、Kratos 值、为何不影响兼容”；未列差异即 blocker。

### 4.5 Go/No-Go

Go 只证明 Kratos 可承载兼容 runtime，不批准 P2 设计。事项04已因真实旧 Auth RLS deny-all 得出 `FAIL / BLOCKED`；用户于 2026-09-03 指示关闭负向调查并重排计划。事项01–04证据保留，事项05–09不再执行。

## 5. P1：同构替换旧 Auth Runtime（已跳过）

### 5.1 目标

原目标是把 CP0 中通过的 Kratos runtime 扩展到完整旧 14 RPC。由于 CP0 未 Go 且项目没有生产环境，当前 Direct P2 重排明确不继续 P1；事项10–15不再执行，也不得把它们标记为通过。

兼容代码仅位于 `internal/compat/authv1` 和 differential harness；`biz` 不 import compat Proto。可复用 Credential/Token/OIDC/Session 能力通过 framework-independent ports 暴露。

### 5.2 实施事项

| 事项 | Ticket | 内容 | 出口 |
| --- | --- | --- | --- |
| P1-0 | 10 | CP0 scaffold 晋升、删除 spike shortcut、固定依赖 | 独立 CI 与 CP0 全矩阵通过 |
| P1-1 | 11 | 完整 14 RPC、旧 pgx/RLS/Redis adapter、旧 error mapping | contract/integration 通过 |
| P1-2 | 11 | 迁移旧测试 oracle，补真实依赖与故障矩阵 | 不依赖 ANI internal package |
| P1-3 | 13 | 隔离 canary，真实 Dex/API Key/Service Token | 三 caller 指向 canary 验证 |
| P1-4 | 12 | 建立稳定 `ani-iam-service` DNS，selector 仍指旧实现；预置 `IAM_SERVICE_*` | 只改名称不改行为 |
| P1-5 | 14 | 人工切 selector 到 Kratos，运行 caller/differential/rollback | 单一 track，Endpoint 收敛 |
| P1-6 | 15 | 测试环境观察、缺陷修复、人工确认旧 runtime 下线 | 无旧 Auth 运行流量 |

P1 不要求 7-14 日生产 soak；测试环境观察时长在任务卡中明确，缺少生产级 soak 记 `not_verified`。

### 5.3 命名与切换顺序

1. 部署 `ani-iam-service` stable Service，先选择 legacy pods；
2. manifest 同时注入新旧 env 以跨版本，不在代码中实现长期 fallback；
3. Gateway 改用 `IAM_SERVICE_ADDR`；
4. Envoy/Inference 改用 `IAM_SERVICE_GRPC_ADDR`，Inference 去除长期共享 mint secret 的动作留到 P2；
5. 三 caller 均连接新 DNS，但仍是 legacy backend；
6. 人工切 selector 到 Kratos；
7. 确认后删除 `ani-auth-service` Service/Deployment 和旧 env。

稳定 Service 不得同时选择 legacy/kratos。P1 不双写、不迁移旧表，rollback 用 selector、固定镜像和 P1 数据 snapshot。

## 6. Direct P2：先证明可切换，再完成目标 IAM/Core

### 6.1 入口

Direct P2 实施仍需要单独人工批准，并满足：

- 事项04负向证据已关闭，草案 DP2-01 明确记录跳过剩余 CP0/P1 的范围和风险；
- `plan-iam-service-refactor.md`、目标 OpenAPI/Proto 和删除清单冻结；
- Core Control 工作已另行启动并提供 canonical Tenant Lifecycle/Bootstrap/Snapshot 契约；
- 目标 IAM/Core 独立数据库、测试 NATS identity/ACL 和测试切换窗口可用；
- 明确 test seed、Credential 失效和 snapshot 范围。

### 6.2 实施事项与风险门

| 阶段 | Ticket | 内容 | 主要出口 |
| --- | --- | --- | --- |
| Preflight | DP2-00 | 归档事项03/04实验并恢复 Kratos 固定实现树 | archive 可恢复；非文档树与 `05ba302...` 一致 |
| DP2-0 | DP2-01 | 冻结来源、当前调用面与已知缺陷 | 精确 commit/digest；风险与非继承项 |
| DP2-0 | DP2-02、03 | 冻结 OpenAPI/registry、IAM/Core contracts、errors、deletion manifest | one operation/handler/owner；Buf/OpenAPI gates |
| DP2-1 | DP2-04 | Atlas/sqlc/pgx 目标无 RLS 基础、受限 role | empty DB replay；两 Tenant/query mutation |
| **Go/No-Go A** | **DP2-05** | Password→Session→CheckPermission→目标 Gateway→一个受保护 API | 真实依赖、401/403/503/504、Audit tx；失败即停止 |
| DP2-2 | DP2-06–12 | Password、OIDC、Session/浏览器、授权、API Key、Service Token、Core Lifecycle/NATS | 切换关键功能完整 |
| DP2-2 | DP2-13 | Gateway、Envoy、Inference、Console、BOSS 当前调用面对等 | 五类独立 E2E |
| **Go/No-Go B** | **DP2-14** | 隔离测试轨道整组切入新 IAM 并回退 | 无代码 fallback；固定镜像/config/evidence；失败即停止 |
| DP2-3 | DP2-15–17 | Invitation/Role/Platform/Recovery、Audit/Idempotency、完整 UI/E2E | 全部目标功能完整 |
| DP2-4 | DP2-18 | 最终测试数据 snapshot/seed、整组破坏性切换 | target smoke；旧 Credential 全失效 |
| DP2-4 | DP2-19 | 删除旧 runtime/Proto/compat/schema/重叠能力 | zero-reference/delete manifest |
| DP2-4 | DP2-20 | 空环境安装、功能矩阵和证据收口 | 功能完成；Production Ready 仍否 |

DP2-01–13 可以在隔离集成轨道逐包合并，但不能把目标 Gateway 的单一路由验证描述为全局切换。DP2-14 首次证明“切换关键功能可以整组进入新 IAM并通过部署配置回退”，不删除旧资产、不使旧 Credential 失效，也不代表 DP2-15–17 的新增能力完成。只有 DP2-14 证据被人工接受，才继续完整目标功能。

### 6.3 P2 固定删除

- `auth-service` Deployment/Service/ServiceAccount/build；
- `AUTH_SERVICE_*`、旧 `AUTH_JWT_*`、旧 `AUTH_OIDC_*`；
- 旧 ANI/Auth 中的 `auth.v1.AuthService`、generated pb/client/mock；`ani-iam` 的未投入使用 `internal/compat/authv1` 实验已由 DP2-00 提前归档移除；
- `ValidateToken`、旧 `CheckPermission`/`CheckPermissionV2` 和 Gateway mode/fallback；
- JWT Blocklist、旧 refresh semantics、old Role/permission projection；
- RLS policies、通用 IAM `deleted_at`、旧 User/single-role/polymorphic Binding 表；
- Tenant Admin 与 Plan/Bind Plan 重叠 operation；
- `tenant-service` runtime 和已经迁移所有权的重复能力；
- P1 canary/diff runtime 入口。

历史 ADR/evidence 可保留旧名称，不计 runtime zero-reference。

### 6.4 切换与恢复

DP2-14 是隔离测试轨道的早期整组演练：旧 Auth 保留为部署级回退目标，但目标代码不得 fallback。DP2-18 才是最终测试环境切换，必须另行确认 snapshot、seed、Credential 全失效和精确 manifest。当前没有 production rollback rehearsal 硬门；不得为了“好回滚”让旧、新 schema 长期双写。

## 7. PR0：未来 Production Readiness

PR0 不属于当前重构功能完成。创建正式环境或声称 Production Ready 前至少补齐：

- Argon2 benchmark/memory 和 CheckPermission load gate；
- Race/Fuzz/专项并发；
- 多副本 IAM/Core、三副本 NATS、TLS/ACL、backup/restore/failover；
- rollback rehearsal、production-shaped soak、容量与 SLO；
- production CORS、retention、审计合规、KMS rotation；
- 所有当前 `not_verified` 项的明确处置。

## 8. 阶段级停止条件

任一条件发生即停止当前 WP，不自动扩大范围：

- 来源或目标 Artifact SHA/digest 不能验证；
- 所需真实依赖缺失且共享环境也不能隔离；
- Go/No-Go A 无法用真实无 RLS 数据库和受限 role 跑通目标纵向链路；
- contract digest、operation owner 或 obligation 不唯一；
- 两 Tenant 负向测试发现跨 Tenant 访问；
- Audit 与 mutation 无法同事务；
- caller gate 缺失或被另一个 caller 代替；
- Go/No-Go B 仍需要代码 fallback、未覆盖当前调用面或无法完成整组部署回退；
- Core canonical contract 未产生却要求实现真实 P2 projection；
- Agent 需要超出任务卡 allow paths 或执行破坏性操作；
- 人工 checkpoint 未批准。

## 9. 最终阶段判定

| 声明 | 最低阶段 | 不包含的声明 |
| --- | --- | --- |
| 目标架构纵向可行 | DP2-05 Go | 不能整体切换 |
| 切换关键功能可替换旧 Auth | DP2-14 Go | 新增管理能力尚未完整、旧资产未删除 |
| IAM refactor functionally complete | DP2-20 完成 | 不是 Production Ready |
| Production Ready | PR0 全部门禁完成 | 不能由测试环境成功推断 |

每个阶段事项必须声明前置证据、目标、允许和禁止范围、验收、测试、停止条件与恢复方法；改变状态的事项保持单一 `claimed`，高风险动作另行取得精确人工确认。
