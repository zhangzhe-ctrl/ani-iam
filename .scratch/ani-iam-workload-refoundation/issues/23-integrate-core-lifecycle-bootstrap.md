# 23: 集成真实 Core 生命周期、Bootstrap 与 NATS 投影

**Status:** needs-triage

**Type:** task

**Blocked by:** 19, 22

**阶段与能力：** M1；C09，相关 C05/C06/C08/C10/C11/C12/C13/C14。

**授权边界：** 计划票，尚未领取；Core 的隔离配套实现须显式进入精确跨仓库工作面。

## 目标

通过真实 Core owner、NATS 和 IAM 正式进程，证明 Tenant 生命周期投影、heartbeat/snapshot/DLQ 与 bootstrap/Invitation 可以独立于 Core 旧身份 writer 工作。Tenant 业务元信息和 Quota 继续由 Core 拥有。

## 范围与非目标

- 在专用隔离集成路径实现 Core 的 Tenant/Quota 初始化与 outbox 本地原子提交、可靠发布、snapshot/heartbeat/bootstrap owner 接口；IAM 只消费事实并维护 TenantAccess、Membership/Role/Invitation 等自身数据。
- 实施真实 NATS 的 broker identity→Broker Workload Binding→active Grant/exact subject ACL/registry；payload、共享 Secret、namespace 都不能作为 producer 权威来源。
- IAM durable 消费与投影：去重/版本或顺序规则、tenant/global 归因、流水线 freshness/heartbeat、snapshot 恢复、DLQ 与受控 replay，过期/缺失状态 fail closed。
- 保留 ADR-0004 的 10s heartbeat、30s stale 与强制启用前 24h shadow、p99≤5s、无未解决 gap、一致性/full rebuild 门禁。先冻结本票测试模式，分别记录投影正确性和强制启用，不能用 fixture/时间归一化替代真实观察证据；Snapshot 按 D06 继续采用有效 REST/OpenAPI 决定，处理候选 gRPC Proto 差异，协议变更须有明确修订。
- 交付 Core 创建 Tenant→生命周期生效→IAM bootstrap/首成员或邀请→真实 Notification→首次登录/受保护读取的最小纵向链；每一步单 owner，本地事务+异步恢复，无跨库 FK/事务/双写。
- Core 只加入接口替换验证必需的配套 owner 路径。隔离目标不调用旧 Auth、不写 Core 旧身份表；共享 Core 的旧实现存在不构成本票失败。
- 非目标：把所有 Tenant 内容迁 IAM、新建完整 tenant-service、重构 Core 全部业务资源、前端、Core 旧代码全仓删除、生产 NATS/DB 修改、流量切换。

## 固定输入与工作面

- Core/ANI、IAM、NATS、Notification/Gateway 等实际参与链的 commit/tree/OCI、事件/schema/ACL/registry/config/seed/scenario 和隔离 manifest：`not_frozen`。
- 候选工作面：Core tenant lifecycle/outbox/owner adapter、IAM lifecycle consumer/projector/bootstrap、broker registry/ACL adapter 和隔离测试；精确 Allowed/Forbidden 文件清单 `not_frozen`。Core 旧身份裁剪另属 WR-26。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与前置正式链证据；过往 seeded projection/fixture backend 只提供局部历史证据。
- 若 owner/projection/snapshot/DLQ 范围需再拆，先以可运行的 Tenant 创建纵向链和清楚的剩余 gate 重排未完成票，不把必要真实 owner 推迟到 M1 之后。

## 验收与验证

1. 真实 Core 本地提交后通过真实 NATS 产生 IAM 可观察投影，Tenant 元信息/Quota 无 IAM writer；Core/IAM 数据库由各自受限运行 role 写入，跨库直接读写无目标依赖。
2. 伪造 producer、错误 binding/subject/Grant、cross-tenant、重复/乱序/丢失事件、consumer 重启、积压和 broker 失联按冻结规则拒绝或恢复；状态与审计可归因。
3. heartbeat/stale window、snapshot catch-up、DLQ inspect/replay 有真实进程与 PostgreSQL/NATS 证据；单行 fresh_until fixture 不能替代流水线 freshness。强制启用前逐项通过 ADR-0004 的 shadow/传播/gap/一致性/rebuild 门禁；仍在允许的 shadow 模式时将强制拒绝记为 `not_verified`，不能声称启用已通过。
4. bootstrap/Invitation 链覆盖重试、中途失败、重复/冲突初始化与租户非 active；完成后经真实 Gateway/IAM 得到正确身份与权限，目标链无旧 Auth 调用或旧身份表写入。
5. 记录 owner 输入、消息/状态相关 ID 与摘要，严禁复制 broker/DB Secret；fake 测试不代替上述最终 gate。

## 恢复与停止

恢复隔离 owner/consumer/config 和专用 broker/数据库测试资源；重放只针对冻结隔离目标。若需访问共享业务表、无权威 snapshot/heartbeat、无法证明 producer、需要未冻结跨仓库工作面或扩大为 Core 全面重构，停止依赖链并记录问题，不以永久轮询/共享表弥补契约。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/23-integrate-core-lifecycle-bootstrap/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，Core/NATS/bootstrap 正式链 `not_verified`。
