# 24: 验证 Gateway REST 与全部必需后台调用方

**Status:** needs-triage

**Type:** task

**Blocked by:** 20, 21, 22, 23

**阶段与能力：** M1；C13，汇合 C01–C12 并完成 C12/C14 的接口级证据。

**授权边界：** 计划票，尚未领取；仅连接与验证冻结的必需后台接口。

## 目标

在无前端的隔离环境，通过真实 Gateway REST、Envoy、Inference、Session Gateway 和相关 owner 正式进程验证必需行为，形成 M1 能力矩阵的逐项真实接口证据。

## 范围与非目标

- 用 HTTP runner/cookie jar 验证 Gateway 的认证、cookie/CSRF、错误映射、permission gate、Tenant/Platform scope 和后台管理接口；Gateway 保持 DTO/协议转换，不复制 IAM 业务规则。
- C05 使用真实资源 owner 的 resourceTenant/Owner 条件证明资源权限；Gateway 入口 one-call 与后续每跳身份验证分别取证，不从请求中的 Tenant/owner 字段直接授权。
- 补齐 WR-19 首链之外仍 mandatory 的 Inference、Envoy、Session Gateway 调用；按实际调用 registry 检查每个同步 hop 的直接 caller 身份、authority、audience 与必要 delegation，不用服务名数量代替操作覆盖。
- 所有必需接口由正式 composition 和真实 owner 提供；OIDC、Notification+SMTP sink、Core/NATS/投影链沿用对应真实证据并运行组合路径，fake/fixture 只用于早期或明确非门禁资源。
- 按冻结 RetrySemantics，对安全状态变化执行响应丢失重试、幂等冲突、相关并发和依赖超时/不可用检查；只测风险相关组合，不引入无边界全量混沌平台。
- 汇总目标调用/写入观察点，证明隔离目标路径不访问旧 Auth 或旧身份 writer；没有 UI 和全仓旧引用不影响本票。
- 非目标：Console/BOSS 前端、全仓 zero-reference、旧 Core 代码裁剪、整组生产/共享流量切换、全面容量/HA/DR。

## 固定输入与工作面

- 实际 mandatory caller/owner 的 commit/tree/OCI/contract/registry/config/seed/scenario 与隔离 manifest：`not_frozen`；前置结果只有输入一致或差异重新验证后才能复用。
- 候选工作面：Gateway/Envoy/Inference/Session 等必要 adapter 和接口 harness、隔离 config、缺陷最小修复；精确 Allowed/Forbidden 文件清单 `not_frozen`。契约/owner 大缺口回到明确设计差异，不暗中扩大本票。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 与 WR-18–23 证据。逐项列出谁是真实 owner、谁是 fixture、为何允许。

## 验收与验证

1. 每个 mandatory REST/RPC/消息行为有成功、拒绝、依赖失败与适用恢复场景；cookie/CSRF/error mapping 由 Gateway REST 实证，无 frontend helper 才能调用的隐藏依赖。
2. 每个必需 S2S hop 的 caller、credential、Grant、scope/evidence、audience 验证通过；错误身份/权限/租户、撤销状态和依赖失联均 fail closed。
3. 真实 caller 执行业务结果可观测，不能以 Pod Ready、HTTP 200、直接 IAM 单测或手造 ctx 作为完整成功；目标无旧 Auth fallback/旧身份表写入。
4. Refresh、Key/Grant 变更、Invitation/admin guard、lifecycle/bootstrap 等定向重试/并发无重复授权或遗漏审计；受影响源码检查/测试与 contract pin 一致性通过。
5. 每个能力和链分别记录 `pass`/`fail`/`not_verified`、复现命令、输入摘要与证据级别，不将局部通过概括为 M1 完成。

## 恢复与停止

恢复各专用隔离 caller/config 和测试资源，保留可重放但脱敏证据。若必需链只能 fake、fallback、越权上下文或跨库写入才工作，停止并记录能力失败；若新增大功能，先重排剩余范围，不能在汇总票重建服务。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/24-verify-backend-callers/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，完整后台接口矩阵 `not_verified`。
