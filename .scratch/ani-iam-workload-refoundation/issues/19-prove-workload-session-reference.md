# 19: 证明 Platform Workload 与 Gateway→Session 首条参考链

**Status:** resolved

**Type:** task

**Blocked by:** 18

**阶段与能力：** M1；C08，C05/C11/C12/C13/C14 的首条 S2S 实证。

**授权边界：** 2026-09-10 用户以 WR-19 Goal 授权三仓库首条参考链、适配包及隔离验证。本票已按逐文件清单实施并完成隔离验收；三仓库源码、验收与恢复见最终 evidence。

## 目标

通过真实 IAM、Gateway 和 Session Gateway 正式进程，交付 Platform Workload 的可信引导、身份 binding、Grant、凭证/token 与 receiver 验证，证明首个同步 hop 可以取代旧服务身份路径。

## 范围与非目标

- 实施最小可审计 Platform Workload 引导/注册流程、信任锚、绑定及 active Grant；身份、owner、credential 和 authority 各自校验，不由 namespace/Header/共享 Secret 推导身份。
- Gateway 作为直接 caller 调用真实 Session Gateway；需要代表 Human 时传独立、受约束的 Delegated Subject/evidence，receiver 同时校验 caller 权限和请求边界。
- receiver 使用 WR-17 处置的首期方案，audience/有效期/撤销或 revision/fail-closed 语义完整；真实身份来源与 token issuer 不能用 fake 代替验收。
- 保留已接受的可信引导、首管理员可达性、审计和高风险恢复安全边界；未接受的大型 LineageRegistry、PONR、ReadyClaim/Receipt 等候选机制不打包为本链普遍前置。
- 非目标：完成所有 caller、Tenant API Key、Notification/Core NATS、前端、流量切换、旧资产删除或一整套长期恢复平台。

## 固定输入与工作面

- 三仓库固定 commit/tree 与 WR18 未提交 source/diff 已核验，详见本票 evidence/baseline.json；IAM bootstrap/invocation 合同和输入已冻结，真实资源 v2 manifest 已获用户确认并创建，复用夹具按用户要求保留。
- 已冻结工作面：IAM 的 implementation-scope.json、Session 接入与连接生命周期的 session-implementation-scope.json；ANI 的 ani-implementation-scope.json。未列出的产品路径不获授权。
- 读取 [能力矩阵](../capability-matrix.md)、[决定表](../decisions.md) 和 WR-18 真实 runtime 证据；依赖正式进程不能以测试 server 替代。

## 验收与验证

1. 隔离可信引导得到可归因 Platform Workload；Gateway 取得受限凭证并经正式 receiver 完成 Session 业务请求，相关身份/授权变更有安全审计。用真实测试资源证明 receiver 业务结果，不能只展示 token 发出或 HTTP 200；普通成功资源访问不强制变成 IAM 领域事件。
2. 无身份、伪造 caller、错误 issuer/audience、过期/撤销凭证、禁用 Workload/Grant、scope 不符和越 Tenant 请求全部稳定拒绝。
3. 需要 delegation 的操作拒绝缺失/错误边界 evidence；direct operation 不接受无关 delegation 扩权。身份服务不可用/超时按冻结策略 fail closed，无 legacy/shared-secret fallback。
4. 引导重试、同一请求重复、凭证/Grant 状态变化的定向并发验证通过，审计可归因且不泄露 Secret。
5. 真实 identity source→IAM→Gateway→Session 证据独立于 fake/unit，并记录不可变输入；尚未接入的 caller 仍 `not_verified`。

## 恢复与停止

恢复专用隔离 caller/config/注册项和进程，不撤销共享凭据或切换共享 selector。若可信身份只能自报、需要未接受的 authority/receiver 语义、存在权限放大或无法形成真实 Session 结果，停止相应链并记录契约缺口，不把问题扩大成通用恢复平台工程。

**Evidence path:** `.scratch/ani-iam-workload-refoundation/evidence/19-prove-workload-session-reference/`

## 结果

已完成：可信 bootstrap、公共 SDK/独立示例、正式 IAM→Gateway→Session 两 Tenant 真实 exec、短期签发/在线校验、ticket/业务幂等、撤权/故障断连与恢复、证书续期及全部 WR19 必需验收通过。受影响回归 58 项具名真实依赖结果通过，Session/ANI 相关仓库门禁与最终源码/资源核对通过。16 个可复用集群对象保留，临时进程/依赖/隧道退出；无 Git 发布或共享环境切换。详见 [最终交接](../evidence/19-prove-workload-session-reference/README.md)。WR19 完成不等于完整 M1；WR20 及以后未领取。

以下阶段记录保留为历史，旧 claimed/等待确认不表示当前状态。

## 2026-09-10 准备阶段领取

- 唯一状态权威：原 ani-iam 当前事项；WR-17/18 均 resolved，无其他 claimed。
- 固定基线与 WR-18 未提交源快照已核对，见 [baseline.json](../evidence/19-prove-workload-session-reference/baseline.json)；不采用移动 main/latest。
- 本阶段允许修改：本票、ticket-plan.md、evidence/19-prove-workload-session-reference/ 下准备材料；三仓库产品只读，先冻结逐文件 Allowed/Forbidden 和生成输入输出。
- 依赖：继承 WR-18 schema/runtime/正式 Human 能力；D01、D02 同步已接受。长连接策略及实际 K8s 环境精确操作按 Goal 单独接受，仅暂停相关路径。
- 验收：真实正式 IAM→Gateway→Session 参考链、可信 bootstrap、签发/在线验证、公开适配包/独立示例、真实资源连接、拒绝/故障/恢复、受影响回归与范围/证据核对；完整矩阵保持不变。
- 测试：重任务只在 ubuntu 串行 GOMAXPROCS=2/-p=2；此准备阶段只读预检，不运行重测试或接触集群。
- 恢复：保留原仓库/WR-18 产品及全部历史证据，新增 worktree/资源只按登记归属恢复；不 reset/stash、不修改其他活动任务资源。
- 停止：基线漂移、精确范围外修改、未接受语义或环境操作，暂停依赖部分并继续独立工作；不降低安全检查或验收分母。

## Bootstrap 实施切片冻结

2026-09-10：用户 Goal 明确授权；[逐文件清单](../evidence/19-prove-workload-session-reference/implementation-scope.json)与 [bootstrap 合同](../evidence/19-prove-workload-session-reference/bootstrap-contract.md)已冻结，可在新 IAM worktree 实施这一独立切片。其余链路、长连接策略和真实资源验证仍按各自依赖推进，完整 WR-19 不因此完成。用户最新指定真实 K8s 使用 SSH `ani` 测试集群，CF-01 两个 namespace 的 UID 已只读核对；原 kind 提案废止。

## IAM 签发与适配包切片

[精确清单](../evidence/19-prove-workload-session-reference/implementation-scope.json)已包含 IAM gRPC 契约、签发/在线验证、模块与适配包、生成及对应测试路径；[同步调用合同](../evidence/19-prove-workload-session-reference/invocation-contract.md)固定请求摘要与角色分工。长连接待定策略不启用；ANI/Session 产品路径仍须在各自编辑前冻结。

## Session 创建接入切片

固定 Session baseline 无漂移，独立 worktree 已创建；精确 14 路径在 [Session scope](../evidence/19-prove-workload-session-reference/session-implementation-scope.json)。实现复用公共 SDK 的接收端和 owner 资源检查，长连接及 ticket 授权依赖仍等待原问题回复，完整链未启用或部署。

## Gateway 首链接入切片

固定 ANI baseline 无漂移，独立 ANI-wr19 已创建；[精确 32 路径](../evidence/19-prove-workload-session-reference/ani-implementation-scope.json)限定显式隔离 WR19 模式的四个 owner operation、公共 IAM/Session adapter、正式装配与必要测试记录。沿用 WR17 固定候选，不扩展公网路由或全 Gateway 切换。

2026-09-10：Gateway 首链接入的 adapter/middleware/router/main 测试、vet 与正式二进制构建通过，见 ani-adapter-results.json。真实三进程资源链未验证。集群草案已修订为 16 对象 v2，先前未执行的 11 对象草案废止；仍等待精确操作确认与既有连接策略决定。

独立回归检查点：`wr19-20260910T045819Z-886165e6` 的 IAM/SDK/Session 相关检查与 50 项真实依赖集成结果全部通过，7 个登记容器均已清理。完整首链未验证；继续等待集群 v2 精确操作及既有连接策略决定，不将局部通过改写为 resolved。

ANI 仓库相关门禁：`wr19-20260910T050810Z-73666686` 的 make test（含架构/auth/生成链、49 个 Go package 和 Python 语法）、文档入口、Services 边界和 WR19 registry 检查通过。仅远端临时 go.work 按候选模块映射发生预期变化；无源文件/锁文件/生成物漂移。集群与生命周期确认仍未到达，事项继续 claimed。

## 2026-09-10 用户确认后继续

用户已接受 16 对象 v2 隔离方案，并要求保留可复用夹具；namespace、Deployment、NetworkPolicy、RBAC 和 ServiceAccount 不自动删除，登记实际 UID 与保留状态，新建短期访问凭据按期失效。已接受创建/ticket 兑换在线校验、长连接每 30 秒复查、单次 2 秒超时，撤销/拒绝/IAM 故障时关闭。详见 evidence/user-acceptance-20260910.json；此前等待确认的记录均为历史检查点。相关依赖已解除，继续冻结连接期接线的逐文件范围并完成真实首链，事项保持 claimed。

## 2026-09-10 用户追加：IAM 本地 main 提交

用户明确要求把 IAM 代码提交到 main，覆盖此前 Goal 的不提交限制。临时重新领取仅处理已验收 WR18/19 成果的本地 Git 交付；不开展 WR20，不提交其它仓库，不发布候选模块。用户随后明确追加推送远端 main；允许完成验证后正常非强制推送并核对远端 SHA。精确基线、来源逐文件清单、文档路径、验收、验证、恢复与停止条件见 [提交范围](../evidence/19-prove-workload-session-reference/main-commit/scope.json)。原真实链验收保持有效；完成交付后恢复 resolved。

交付校验：主目录已恢复全部 215 个已测试文件及 5 个必要删除，逐文件 SHA/模式与最终验收快照一致；现有 Go 源码没有落在验收快照之外。当前配套文档随本次交付提交，运行归档、完整 diff、私有运行资料与无关 DP2-11 取证保留本地。WR19 业务状态保持 resolved；实际 commit/远端 SHA 与推送核对由本次 Git 交付回执记录，不表示完整 M1 或模块 tag 发布。
