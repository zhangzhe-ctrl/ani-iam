# WR32 同步 Workload 接入维护

唯一注册入口为 `registrations/workload-targets.v1.json`；参考实验包在同目录。`workloadregistry` 是不访问存储的严格解析 Module。每个注册记录精确绑定 audience、operation、RPC、机制、receiver、source/mode 和 owner obligation；没有通配符、脚本或在线注册 API。

新增同类服务：

1. 在审核包中加入目标和对应 receiver 权限目标，固定文件 SHA256。目标规范记录另有独立摘要；新增无关目标不改变既有目标摘要。
2. 用受限 migrator 执行 `install-workload-registry --registry-file <absolute> --approved-registry-sha256 <sha> --previous-registry-sha256 <previous-or-empty> --dsn-file <private>`。安装为原子事务；必须保留已有目标和 scope，停用须显式声明。安装不创建 Principal、Binding、Role 或 Grant。
3. 通过独立身份引导及 Grant 审核授权 caller 与 receiver。caller 的签发入口、目标 Grant，以及 receiver 的 Verify 入口、同 audience receiver Grant 分别要求；只有 Verify 入口权限不能验证任意目标。
4. 正式进程配置注册文件及 SHA256。IAM 使用 `runtime.workload_registry_file` / `workload_registry_sha256`。各 owner 使用其 typed config 或 `IAM_WORKLOAD_REGISTRY_FILE` / `IAM_WORKLOAD_REGISTRY_SHA256`。Gateway 还要求 `IAM_TARGET_POLICY_REVISION`，传入安装回执给出的当前 IAM 策略版本；其冻结路由版本保持原值。
5. owner adapter 使用通用 SDK 绑定精确 RPC，归一化请求并计算完整摘要，执行注册声明的资源归属核验。Workload-only 不带 subject；delegated 另外检查 subject Credential、Tenant、source 权限和当前撤权状态。receiver 在 handler 前验证，不能靠客户端声明身份或资源归属。

IAM 拥有身份、当前权限、注册安装及审计；业务 owner 拥有业务数据、幂等、执行和恢复。有限 continuation 只用于已接纳原请求，仍受 16 分钟和原 subject Credential 到期上限、原 receiver 及当前权限约束；不能续期或创建新调用。

目标摘要与全局权限策略版本是两个检查。新增 source permission 会改变 IAM 策略版本，所有相关正式配置必须使用安装回执版本；原有策略版本检查继续生效。与现有 source 同名的声明必须完全一致。不得通过关闭版本校验来迁接。

失败与恢复：

- 非法/重复/冲突声明、文件摘要和数据库不一致：拒绝加载或安装；核对审核包和确切前序安装。
- 未知/不匹配调用输入：原 InvalidArgument 分类；停用目标、旧目标摘要、错误 receiver 或当前权限失效：拒绝授权。
- IAM、存储或必要 owner 不可用：按原依赖错误拒绝，恢复后重新走当前检查；无旧 Auth 或离线回退。
- 注册更改需要新审核包和精确前序；不要删除历史 Grant 行、改 scope 或直接改表来绕过失败。

完整复现入口为本目录 `run.sh all`。重验须按仓库 AGENTS.md 明确领取 WR32 为唯一 claimed 验证事项；脚本不会自行重开已 resolved 的事项。它只接受最终源码及命令摘要匹配的输入，依赖原 IAM WR32 evidence 中固定输入、最终源码清单、命令与本票远端缓存。所有生成、Go、聚合、真实 owner 与容器工作经 SSH ubuntu、独占 heavy.lock 执行。每组新建唯一 run，失败立即保留证据并停止；原 dirty 候选不修改。参考实验比较冻结机制摘要，不能替代真实业务链。

当前状态和全部结果只见原 IAM 的 `.scratch/ani-iam-workload-refoundation/evidence/32-stabilize-workload-integration/README.md`。WR23 保持 needs-info；相关输入变化后的组合证据和真实 24h shadow须由后续正式事项重验。这里不授权发布、切流或旧运行资产清理。
