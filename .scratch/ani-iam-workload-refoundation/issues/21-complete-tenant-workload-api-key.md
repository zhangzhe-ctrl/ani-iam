# 21: 完成 Tenant Workload 与 API Key/Envoy 权限链

**Status:** needs-triage

**Type:** task

**Blocked by:** 19

**阶段与能力：** M1；C07，相关 C05/C08/C11/C12/C13。

**授权边界：** 计划票，尚未领取；精确范围与输入冻结后再执行。

## 目标

把原 Service Principal/API Key 能力落到 Tenant-owned Workload，证明真实 Envoy 可消费 IAM API Key 与权限结果，同时保持 owner、membership、credential 和 authority 的独立约束。

## 范围与非目标

- Tenant Workload 创建/读取/禁用及替换所必需的生命周期；API Key 创建、一次性 Secret 返回、验证、轮转/撤销，及其最小管理接口。
- Workload 的固定 Tenant owner、单一当前 Membership 与 Role/Grant 各自满足冻结 authority 条件，API Key 不自动授予 Tenant/Platform 权限，不复制 Human 权限；禁止把旧 service 类型映射成表面新名。
- 正式 Envoy ext_authz/消费 adapter 接入真实 IAM；按目标 registry 校验权限、resource/actions/scope、Tenant 与 workload 状态。
- 密钥验证、管理操作和审计/幂等边界采用 WR-18 持久化基础，Secret 不写日志/证据。
- 非目标：通用 Credential 平台、完整管理 UI、生产 Envoy 切流、共享 Credential 失效、Core 生命周期或长期双版本兼容。

## 固定输入与工作面

- IAM、Envoy/ANI adapter 的 commit/tree/OCI/契约/registry/config/seed/scenario 及隔离 manifest：`not_frozen`，不得复用旧 DP2-10 结果充当新模型当前证明。
- 候选工作面：Tenant Workload/API Key usecase、SQL/UoW、正式服务 adapter、Envoy caller 与定向测试；精确 Allowed/Forbidden 文件清单 `not_frozen`。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md)、WR-18 runtime 与 WR-19 可信服务身份链证据。

## 验收与验证

1. 正式 IAM/真实 PostgreSQL 与正式 Envoy 完成 API Key 创建→使用→受保护业务接口；响应权限与主体归因符合冻结契约。
2. wrong tenant、无/失效 membership、无权限、超 scope、禁用 Workload、无效/过期/撤销 Key、身份服务不可用均稳定拒绝；Tenant-owned Workload 不能获得 Platform authority。
3. Key 创建的响应丢失/重试、幂等冲突、轮转/撤销与请求并发、audit 写失败都有受限 role 下的真实依赖证据；不重复产生有效 Secret 或丢失安全审计。
4. 旧 service/token/shared-secret 路径不被本目标链调用，Envoy gate 不靠直接调用测试 helper 代替；记录整个正式链而非仅 API Key 单测。

## 恢复与停止

恢复本票隔离 Envoy/IAM/config 与专有 Key fixture，不撤销共享 Key。遇到 authority 隐式继承、tenant 关联绕过、secret 重放语义不明确、需要未冻结 contract 或其他服务状态时，停止并记录差异，不引入永久 fallback。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/21-complete-tenant-workload-api-key/`

## 结果

尚未执行；固定输入与精确工作面 `not_frozen`，Tenant Workload/Envoy 正式链 `not_verified`。
