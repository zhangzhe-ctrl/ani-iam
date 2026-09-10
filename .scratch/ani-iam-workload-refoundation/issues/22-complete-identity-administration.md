# 22: 完成替换必需的身份管理、安全恢复与审计接口

**Status:** needs-triage

**Type:** task

**Blocked by:** 20, 21

**阶段与能力：** M1；C05、C06、C11，相关 C02/C03/C10/C12。

**授权边界：** 计划票，尚未领取；以能力矩阵列出的旧服务替换行为限定范围。

## 目标

让 TenantAccess、Membership、Role/Binding、Invitation 及 Platform 管理成为 IAM 的正式能力，交付替换旧身份管理所需的权限、安全恢复与审计查询。该票在 M1 验收之前完成，避免只验证登录就声称可替换。

## 范围与非目标

- 按 [能力矩阵](../capability-matrix.md) 交付明确的成员加入/移除/禁用、Role 与 Binding 管理、权限 catalog、Invitation 创建/接受/撤销/过期等业务动作；IAM 只拥有访问关系，Core Tenant 生命周期/业务元信息/Quota 保留在 Core。
- Platform/BOSS 后台管理接口、Platform/Tenant 权限隔离、Delegated Subject、active 且 login-capable last-admin guard，以及已接受的高风险恢复安全和可达的首管理员流程。
- 已接受 RestoreTenantAdmin 与 Recovery Bootstrap 分别验证审批、重认证和审计要求；不得把已有 Tenant 的管理员恢复伪装为重新 Bootstrap，也不得把平台首管理员引导与普通 Tenant 管理混成通用授权入口。
- Invitation 的 IAM 本地行为与真实 Notification 链在本票证明；需要 Core lifecycle/bootstrap 的整条新租户邀请链在 WR-23 验证，不能用 Core fixture 冒充真实集成。
- Invitation、通知 outbox 与 Audit 在 IAM 本地事务内提交；真实 dispatcher/Notification 的接受、投递和邀请验证/关系建立分别确认，未经验证的邀请不能提前创建成员关系。
- 审计查询权限/过滤/分页、不可修改删除、至少 180 天的已接受保留语义；用受控时钟/数据验证时间边界，并明确区分生产保留运营证据。
- 执行按“成员/角色与权限→Invitation→Platform/admin guard/已接受安全恢复→审计查询”分成可验证子步骤。只实现替换必需操作，不将每个领域对象展开成泛 CRUD，也不附加候选外部 Lineage/PONR/长期恢复平台。
- 非目标：Core Tenant/Quota 管理迁入 IAM、新 tenant-service、前端、全功能运维控制台、生产恢复体系、资产删除或流量切换。

## 固定输入与工作面

- IAM/Gateway/Notification 的 commit/tree/contract/catalog/registry/身份/seed/scenario 和隔离 manifest：`not_frozen`；恢复相关的已接受语义与仍 pending 候选项由 [决定表](../decisions.md) 明确分开。
- 候选工作面：IAM administration/usecase/query/UoW/audit、必要 Gateway adapter、Notification 调用与定向测试；精确 Allowed/Forbidden 文件清单 `not_frozen`，不写 Core 旧身份表。
- 如实际 diff 证明该票无法在一个可审阅范围内交付，应先记录已完成证据并拆分剩余子步骤、更新图；不能以“大票已经领取”为由扩展通用平台或同时推进多个 claimed 事项。

## 验收与验证

1. 正式 IAM/Gateway 对能力矩阵中每个必需管理动作给出成功/拒绝/失败/重试证据；tenant 外键/查询/权限均隔离，Role/Binding 变更后的现有会话权限符合冻结规则。
2. Invitation 正式链覆盖送达、接受、过期、撤销、已成员、错误接收者、重复/并发接受与 Notification 失败；不以生成接口默认 Unimplemented 作为覆盖。
3. 最后一个 active+login-capable admin 的撤销/禁用/失去登录能力路径被保护；首管理员和已接受的高风险恢复动作验证授权、双人/精确目标等适用安全不变量及审计，不留常驻绕过接口。
4. 审计使用真实 PostgreSQL 与受限运行 role；证明不可修改删除、查询隔离与至少 180 天边界，保留生产运维 `not_verified` 的独立标注。
5. 管理冲突、重复写入、邀请/role/admin guard 定向并发、audit fail-closed 验证通过。所有完成状态来自正式进程，fake 只用于局部规则测试。

## 恢复与停止

恢复专有隔离管理 fixture/config，保留审计证据，不恢复或失效共享管理员凭据。遇到已接受管理安全与候选机制难以区分、无法维持 last-admin/登录可达性、需要跨库写入，或范围超过冻结接口清单时，停止相关子步骤并明确待决定项；不临时放开 Platform authority。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/22-complete-identity-administration/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，身份管理/恢复安全/审计正式链 `not_verified`。
