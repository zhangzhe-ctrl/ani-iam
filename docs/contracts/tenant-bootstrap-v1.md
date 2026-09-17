# Tenant Bootstrap 请求与身份流程

权威请求：`api/iam/v1/tenant_bootstrap.proto`。公开编码、规范化及摘要实现：`sdk/tenantbootstrap`。生产者引用这些公开模块；不复制 schema，也不导入 IAM internal。

唯一 revision `iam-tenant-bootstrap-v1`，subject `iam.tenant.bootstrap.v1`，严格 ProtoJSON、snake_case、UTF-8、int64 十进制字符串；payload 最大 65536 bytes。UUID 字段均为规范小写 UUIDv7。事件字段为 event_id、producer、tenant_id、operation_id、occurred_at、source_epoch、lifecycle_sequence、intended_administrator(normalized_email,locale)、payload_fingerprint。source_epoch/lifecycle_sequence 引用创建事务的 Governance 生命周期事实；Bootstrap 不推进 Lifecycle 流序号，也不能修复投影。locale 仅 en-US/zh-CN。

规范化沿用 IAM Invitation：去除首尾空白、local part 小写、domain 经 IDNA Lookup 转 ASCII 小写、拒绝 display-name/非法邮箱，最长 320 bytes。接收方必须拒绝未规范化输入。Fingerprint 是 `sha256:` + 小写 SHA256；原文为 Go encoding/json 编码的四键字符串 map（locale、normalized_email、operation_id、tenant_id），按键排序、无空白、保留 Go HTML 转义。事件/重试元数据不改变身份意图。同 operation 不同 fingerprint 冲突。

producer 字段是归因声明，不是认证。receiver 必须按联合 manifest 核对独立 NKey、精确 account/stream/consumer/subject、受审核 producer Binding 和 publish/receive Grant；事务持久 receipt 后才 ACK。重复事件同原文可重放，冲突/非法输入持久进入受控 DLQ。worker 和 replay 再检查当前 publish/execute/replay 权限，不以历史 ACK 或身份仍存在推断授权。

worker 仅在精确来源生命周期已应用且 fresh/active 时处理；邀请与 outbox/安全 Audit 同 IAM 本地事务。Snapshot 永不创建 Access、Human、Membership、Role 或管理员。新用户独立邮箱验证和密码激活不建立 Membership，必须独立接受 Invitation。现有已验证用户仍须接受 Invitation；Governance 不提供密码或身份表写入。

正式查询继续使用 `iam.v1.IAMAdminService/GetTenantBootstrap`，定义在 `api/iam/v1/iam_admin_service.proto`。请求包含 Platform Human credential 与 operation_id，经真实 direct Workload caller 和当前 Platform 权限验证。响应为 operation_id、tenant_id、status、version、Invitation 安全元数据和 jobs；不返回邀请 Secret。查询不推进 worker。当前不新增 workload-only 查询旁路，Governance 可返回 IAM operation 引用；需要聚合状态时由持有适用 Human credential 的正式调用完成。

当前引导状态包含 pending、waiting_for_principal_verification、succeeded、attention_required；Invitation 自身的 pending/accepted/expired 与引导状态分别读取。任何 receipt、ACK、邀请发出、等待验证均非管理员就绪。管理员就绪必须由 Invitation 已接受、有效 Membership/Role/Access 的正式验证证明，不仅依赖 operation 字符串。

这是 I1 自有合同；不可变生成制品和联合状态见交接目录 CONTRACT/HANDOFF。联合链在 I4 真实验证前均为 not_verified。
