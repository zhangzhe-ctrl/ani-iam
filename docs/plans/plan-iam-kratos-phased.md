# ANI IAM Kratos 分阶段替换方案

> 状态：**Accepted / 当前阶段依据；实施未开始**
>
> 编制日期：2026-09-01
>
> 目标设计：`plan-iam-service-refactor.md`
>
> 当前规格：`../../.scratch/ani-iam-rebuild/spec.md`
>
> 用户指定基线：ANI `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`，Git 身份已验证；真实兼容性证据仍待采集

## 1. 阶段原则

这不是一个持续兼容计划，而是把风险拆成可证伪阶段后完成删除和替换：

```text
基线准备     固定身份并采集兼容性证据，不写运行时
CP0         只验证 Kratos 能否承载旧 Auth oracle
P1          用 Kratos iam-service 同构替换旧 Auth runtime
P2          破坏性重建目标 IAM/Core 契约、数据和授权边界
PR0         未来 Production Readiness，不属于当前功能交付
```

CP0/P1 的兼容代码是实验和过渡资产，P2 必须删除。每个阶段都有独立入口、出口、停止条件和人工解锁；不能因为前一阶段“看起来没问题”自动进入下一阶段。

基线准备、CP0、P1 和 P2 都拆成有界本地事项。任何时刻只允许一个会改变状态的事项处于 `claimed`；数据重建、Credential 全失效、切流和旧资产删除还需要在执行前获得针对精确目标和动作的人工确认。不同仓库同时存在事项不代表可以并行改变共享状态。

## 2. 固定基线与 Oracle

### 2.1 基线身份

唯一候选基线是：

```text
repository: ANI
branch intent: main
commit: 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
source: user supplied and locally verified
compatibility evidence: pending
```

该对象已确认存在并对应 `main`，但不得改用动态 `HEAD`。首个基线事项必须记录 `git cat-file`、`git show`、branch/ref、clean runtime scope 和 migration/schema/Proto/Redis oracle；任一证据无法验证时停止。

规划文件是基线之上的设计覆盖，不进入“旧 Auth 行为 oracle”。

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

## 4. CP0：隔离 Kratos Compatibility Spike

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

Go 只证明 Kratos 可承载兼容 runtime，不批准 P2 设计。No-Go 时保留 evidence，停止独立 runtime 晋升，不以修改目标领域设计掩盖 framework failure。

## 5. P1：同构替换旧 Auth Runtime

### 5.1 目标

把 CP0 中通过的 Kratos runtime 扩展到完整旧 14 RPC，并将运行组件和配置命名统一为 `iam-service`，但继续使用旧 wire、bcrypt、RLS、Redis 和数据语义。P1 不开始目标领域重建。

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

## 6. P2：目标 IAM/Core 破坏性重建

### 6.1 入口

P2 需要单独人工批准，并满足：

- CP0/P1 evidence 已接受或用户明确决定跳过 P1 并记录风险；
- `plan-iam-service-refactor.md`、目标 OpenAPI/Proto 和删除清单冻结；
- Core Control 工作已另行启动并提供 canonical Tenant Lifecycle/Bootstrap/Snapshot 契约；
- 目标 IAM/Core 独立数据库、测试 NATS identity/ACL 和测试切换窗口可用；
- 明确 test seed、Credential 失效和 snapshot 范围。

### 6.2 实施事项

| 事项 | Ticket | 内容 | 主要出口 |
| --- | --- | --- | --- |
| P2-0 | 16、17 | 冻结 OpenAPI、三 IAM Proto、Core integration contracts、registry、errors、deletion manifest | one operation/handler/owner；Buf/OpenAPI gates |
| P2-1 | 18 | Atlas target schema、sqlc/pgx repos、runtime role；无 RLS/无 deleted_at | empty DB replay；两 Tenant negative/query mutation |
| P2-2 | 22、23、24、25、28 | Principal/Identity/Password/OIDC/Session Grant/Refresh/Service Token、浏览器会话边界 | auth integration；strict rotation；Cookie/CSRF；Audit tx |
| P2-3 | 21、26 | CheckPermission、Permission catalog、TenantScope/Platform repos、Gateway one-call | policy digest、obligation、401/403/503/504 |
| P2-4 | 19、20、21、25、27、31、32、33、34 | Tenant Access、Membership、Role、Invitation、Service Principal/API Key、Platform、Recovery、Audit、Idempotency | last-admin、Role-in-use、Invitation race、API key lifecycle、Platform 浏览器边界、全局 mutation gate |
| P2-5 | 29、30 | Core Tenant Lifecycle/Quota + outbox/NATS + IAM projection/bootstrap | gap/heartbeat/rebuild/DLQ/at-least-once |
| P2-6 | 35 | Gateway、Envoy、Inference 切 iam/v1；Console/BOSS 接目标 API | 三 caller 独立 E2E；页面同源 |
| P2-7 | 36 | 测试数据 snapshot、目标 migration/seed、整组破坏性切换 | target smoke；旧 Credential 全失效 |
| P2-8 | 37 | 删除 compat/authv1、旧 Proto/client/runtime、双轨 authz、旧 schema、tenant-service 重叠能力 | zero-reference/delete manifest |
| P2-9 | 38 | 功能矩阵、空环境安装、测试观察、文档和 evidence 收口 | IAM 重构功能完成；Production Ready 仍否 |

P2-0 至 P2-6 可在隔离集成环境逐包合并，但不能单独发布破坏 wire 的中间状态。P2-7 是 IAM/Core/三个 caller/数据库/manifest 的一个测试环境发布单元。

### 6.3 P2 固定删除

- `auth-service` Deployment/Service/ServiceAccount/build；
- `AUTH_SERVICE_*`、旧 `AUTH_JWT_*`、旧 `AUTH_OIDC_*`；
- `auth.v1.AuthService`、generated pb/client/mock、`internal/compat/authv1`；
- `ValidateToken`、旧 `CheckPermission`/`CheckPermissionV2` 和 Gateway mode/fallback；
- JWT Blocklist、旧 refresh semantics、old Role/permission projection；
- RLS policies、通用 IAM `deleted_at`、旧 User/single-role/polymorphic Binding 表；
- Tenant Admin 与 Plan/Bind Plan 重叠 operation；
- `tenant-service` runtime 和已经迁移所有权的重复能力；
- P1 canary/diff runtime 入口。

历史 ADR/evidence 可保留旧名称，不计 runtime zero-reference。

### 6.4 切换与恢复

当前只有测试环境，无 production rollback rehearsal 硬门。切换仍必须保存明确 snapshot、固定镜像和 seed manifest；失败可人工恢复 snapshot 或重建测试环境，结果如实记 `not_verified`。不得为了“好回滚”让旧、新 schema 长期双写。

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

- baseline SHA 不能验证或 oracle 无法重现；
- 所需真实依赖缺失且共享环境也不能隔离；
- 为通过 CP0 必须同时改 wire/schema/security；
- contract digest、operation owner 或 obligation 不唯一；
- 两 Tenant 负向测试发现跨 Tenant 访问；
- Audit 与 mutation 无法同事务；
- caller gate 缺失或被另一个 caller 代替；
- Core canonical contract 未产生却要求实现真实 P2 projection；
- Agent 需要超出任务卡 allow paths 或执行破坏性操作；
- 人工 checkpoint 未批准。

## 9. 最终阶段判定

| 声明 | 最低阶段 | 不包含的声明 |
| --- | --- | --- |
| Kratos compatibility feasible | CP0 Go | 没有替换 runtime |
| legacy runtime replaced | P1 完成 | 目标数据/契约尚未完成 |
| IAM refactor functionally complete | P2-9 完成 | 不是 Production Ready |
| Production Ready | PR0 全部门禁完成 | 不能由测试环境成功推断 |

每个阶段事项必须声明前置证据、目标、允许和禁止范围、验收、测试、停止条件与恢复方法；改变状态的事项保持单一 `claimed`，高风险动作另行取得精确人工确认。
