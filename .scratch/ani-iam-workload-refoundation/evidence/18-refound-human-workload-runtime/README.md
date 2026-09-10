# WR-17/18 完成交接：隔离 Human/Workload 运行基础

日期：2026-09-10。WR-17 合同冻结与 WR-18 有限运行基础均为 **pass**；完整 M1、真实跨服务链、系统替换和生产就绪均为 **not_verified**。本交接没有领取 WR-19，没有 commit、push、tag 或 PR。

## 交付位置与固定来源

- 产品源码：`/home/chabking/workspace/ani-iam-wr17-18`，独立 detached worktree；改动尚未提交。
- 基线 commit：`cd38cd90bca3e9d83af09a381051b895d82ae94f`；tree：`a3668037d43bff9a3d938b9cf07500402e4ebd70`。
- 唯一事项状态与当前文档：原 `/home/chabking/workspace/ani-iam/.scratch/ani-iam-workload-refoundation/`。原仓库产品代码保持原状；产品 worktree 的 67 份文档是领取时来源快照，不能据其旧状态判断当前进度。
- WR-17 [冻结合同](../17-freeze-api-replacement-contracts/contracts.md)、[逐 RPC 矩阵](../17-freeze-api-replacement-contracts/rpc-matrix.md)、[精确工作面](../17-freeze-api-replacement-contracts/implementation-scope.json)与[隔离 manifest](../17-freeze-api-replacement-contracts/isolation-manifest.json)保持不变。118 个允许路径含 3 个文档路径；实际产品差异为 63 个已跟踪路径和 25 个新增路径，含重命名两端。
- 最终 174 文件源码包：[source.tar.gz](runs/wr17-18-20260910T021423Z-bca28bda/source.tar.gz)，SHA-256 `6727e79febbdec2d549601a7ccc966d399d0ac46e278bc0adb24ff2fc2b8f42f`。逐路径、模式和摘要在同目录 [source.json](runs/wr17-18-20260910T021423Z-bca28bda/source.json)。这是 allowlist 源快照，不是新 Git commit。
- 可审查 [product.diff](product.diff) 包含新增文件和二进制生成物，不夹带此前文档整理的差异；SHA-256 `aae096b22b07d0e298be0f33aaafed30b745f34eeb9f7945dd03322004cbf98d`。最终一致性见 [final-verification.json](final-verification.json)。
- [原仓库保护检查](original-protection-final.json)证明 522 个受保护来源文件与 67 份 before 快照保持原样。该 WR-17 校验器输出的 runtime `not_verified` 描述冻结材料自身的证据等级；WR-18 实际运行结果以本交接和最终 run 为准。

## 实际实现与接入成本

Human/Workload 成为目标模型，旧 Service 枚举号保留为 reserved，不增加兼容别名。Tenant Workload 的 owner Tenant、当前 Membership、状态与凭据关系在数据库中约束；Platform Workload 具有独立 Identity Binding/Grant，不能获得虚拟 Tenant Membership。Human Identity/Password/Session 不接受 Workload。运行 role 与迁移 role 分开，正式启动及 readiness 核对 schema revision、必要权限和约束。

IAM 内部的 `WorkloadAuthentication`/`WorkloadAuthorization` 把已验证证书身份解析、当前 Binding/Principal 状态、精确目标 Grant 查询收在领域接口和 data 实现内。正式 mTLS 入口将直接 caller 与被服务 Human 分开；管理接口通过 IAM 验证当前 Human 和权限，不再相信自报的 `x-ani-*` 主体头。27 个本票 handler 进入正式受控调用集合；另外 42 个声明保持拒绝，包含尚缺 Platform Human 管理授权的 Get/UpdateTenantAccess。

普通服务接入成本的问题只完成了 **IAM 内部部分**。这些是 `internal/` 模块，尚不能由其他服务直接复用。依照已冻结合同第 4.1 节，WR-19 必须同时交付独立版本的 caller/receiver Adapter、最小可运行示例和真实 Gateway→Session 接线；只生成 Proto client 或给出 allow 结果不算解决。普通服务最终保留资源事实校验和业务执行，复用适配包完成凭据管理、在线身份/授权校验与稳定错误转换。

用户接受的 D01 在线校验、D02 同步“Gateway 自身 Workload 身份及权限 + IAM 短期受限委托”已进入冻结合同。Session 需同时验证直接调用方与委托主体，IAM 不可用时拒绝本次调用。WR-18 没有实现 WAT/委托签发、Session receiver 或正式可信引导；这些是 WR-19 的明确输入，不能把本票 ingress Grant 当作用户全权委托。已建立长连接的重授权/终止时限仍须 WR-19 冻结。

业务变更、安全 Audit 和本票 Tenant mutation 幂等结果在同一 PostgreSQL 事务提交。重复请求重新检查当前身份和操作权限；冲突/过期拒绝。API Key 仅首次返回 Secret，重放只返回非敏感结果及 replayed 标记。禁用/移除后不能通过旧 Membership 或已撤销 Key 恢复权限；必要的新 Membership 使用新身份关系。

已有 Password、OIDC、Session/Refresh/Logout/SwitchTenant 和权限逻辑继续保留。OIDC 业务仍归 IAM；Core 的 Tenant 生命周期、Quota、Snapshot transport 及其他资源 owner 职责未转移。

## 验证及适用范围

重任务只在 SSH `ubuntu`（实际主机 `i-8yg2l7u8`）串行执行，`GOMAXPROCS=2`、Go 编译 `-p=2`。固定 Go 1.26.7、Buf 1.72.0、sqlc 1.31.1、Atlas Community 1.3.0、protoc-gen-go 1.36.12、protoc-gen-go-grpc 1.6.2；实际二进制路径/摘要见 [remote-final-state.json](remote-final-state.json)。依赖镜像使用 manifest 固定 digest，实际 image ID/端口见最终 run 的 [resources.jsonl](runs/wr17-18-20260910T021423Z-bca28bda/resources.jsonl)。

| 检查 | 结果与证据边界 |
| --- | --- |
| 固定生成及重复生成 | pass；16 个输出重复生成字节一致，经临时回传和输入哈希保护合入，没有手改生成物 |
| 单测/契约、build、vet | pass；`./internal/... ./cmd/server/... ./tests/...`，以及 `go build ./...` / `go vet ./...` |
| 定向 race | pass；biz、server、service、cmd/server 四个包 |
| 真实 PG 空库/受限 role/两 Tenant | pass；跨 Tenant 关联/读取/修改拒绝，Human/Workload 关系约束、终态保护、业务/Audit/幂等结果写失败原子回滚、并发去重 |
| 真实 Redis / Human 回归 | pass；密码、Session 轮换/撤销及受影响权限行为；没有共享 Redis flush |
| OIDC | pass；受影响持久化/Identity link 回归及固定真实 Dex 的授权码 + PKCE 正负向；完整 Gateway 浏览器协议仍待 WR-20/24 |
| 正式 cmd/server | pass；实际二进制、mTLS、27 RPC 可达/42 拒绝、真实管理授权、配置拒绝、DB 权限丢失导致 startup/readiness/RPC 拒绝、Redis 故障及恢复、liveness、SIGTERM 正常退出 |
| 最终集成回归 | pass；43 个顶层测试、含子用例 67 项通过，0 失败、3 跳过。跳过项不算通过 |
| 最终源码/范围/保护检查 | pass；174 文件与最终测试快照逐字节/模式一致；67 文档快照未变；差异含新增文件且不越产品范围；窄范围 Secret literal scan 和 whitespace 检查通过 |
| 真实跨服务链 / M1 / 部署 / 生产 | not_verified；未执行，不由上述结果推导 |

完整检查 run 为 `wr17-18-20260910T020353Z-0f88e91f`，source SHA-256 `0560d007f773933292f83ec84f835452781dfb6f4914f7a1a7892614a743434f`，真实退出码 0；[命令](runs/wr17-18-20260910T020353Z-0f88e91f/command.sh)、[日志](runs/wr17-18-20260910T020353Z-0f88e91f/command.log)、[生成证明](runs/wr17-18-20260910T020353Z-0f88e91f/generation.json)。

之后仅调整四个 integration fixture 文件的资源名、证据记录、固定 Workload 名称和 Dex 随机凭据。产品代码、普通测试和生成输入均未变，[适用性证明](runs/wr17-18-20260910T021423Z-bca28bda/prior-check-applicability.json)记录逐文件差异。最终 run `wr17-18-20260910T021423Z-bca28bda` 对最终快照重新编译并运行全部 integration 测试，退出码 0；[命令](runs/wr17-18-20260910T021423Z-bca28bda/command.sh)、[日志](runs/wr17-18-20260910T021423Z-bca28bda/command.log)、[逐用例结果](runs/wr17-18-20260910T021423Z-bca28bda/test-results.json)、[正式进程二进制/PID 引用](runs/wr17-18-20260910T021423Z-bca28bda/formal-processes.jsonl)。前一轮 build/vet/race/生成结果适用于未变的最终产品代码，不能表述为最后一个 run 重新执行了所有检查。

三个跳过项：`TestEnvoyAdapterIndependentProcessUsesRealIAMRestrictedPostgresAndRedis`、`TestRealIAMAdapterToNotificationProcessMutualTLS`、`TestRealIAMGatewayProcessVerticalSlice`。它们需要本 Goal 未纳入的其他项目进程；对应真实 owner/caller 仍由 WR-19–24 验证，未从 C01–C14 分母移除。

## 故障处理与资源恢复

失败 run 保留原退出码和日志，具体修复见 [implementation-findings.md](implementation-findings.md)。正式管理接口缺失 request ID 时，现用 IAM 生成的 Decision ID 补齐审计关联；没有放宽审计或主体校验。Docker restart 会重新分配发布端口，因此 Redis 故障注入改为对本 Goal 登记容器 pause/unpause，保持同一 endpoint，验证不可用拒绝和恢复；未修改共享资源。

最终所有 113 个登记测试容器均已终止，登记正式进程均不再运行；原 Network 三个 kind 节点的 ID/image 保持核对，未执行全局 prune 或清理缓存。数据库只在新建测试容器的专有空库安装，不迁移或删除既有业务库。

最终资源名和标签遵守 manifest，所有发布端口仅 VM loopback。每个 fixture 单独创建随机 PG/Redis/Dex 凭据，私有目录 0700、凭据文件 0600；相对最初 manifest 的占位引用，实际引用细化到独立 fixture 子目录以避免覆盖，见 [credential-references.jsonl](runs/wr17-18-20260910T021423Z-bca28bda/credential-references.jsonl)。只回传引用和脱敏日志。源包、失败日志、工具和私有 fixture 文件保留在各 `/home/ubuntu/workspace/ani-iam-runs/wr17-18-<run_id>/`；它们属于测试现场，不能用于正式引导或现有服务。恢复/复查从既有 run 的 `command.exit`、PID、资源登记和源码摘要开始，不盲目重放不明状态任务。

## WR-19 交接与限制

下一步以本交接最终源包作为可复现产品输入，核对未提交 worktree 与摘要后，单独固定 WR-19 的 IAM/Gateway/Session 版本及精确工作面；当前只完成交接，WR-19 保持未领取。

WR-19 的交付应围绕一条真实 Gateway→Session 请求：可信 Workload 初始化、WAT/短期委托签发、Session 在线校验、可复用 caller/receiver 适配包和真实 Session 结果，再验证撤销、依赖故障、错误资源/Tenant/操作和重试。完整首 Platform Human ceremony、管理 CRUD/审计查询属于 WR-22；Core 异步信任 D02 尚待 WR-23 接受。受控 owner fixture 不是正式 bootstrap 成功证据。

目标 operation registry 是从固定 ANI owner 输入派生的 IAM 候选，未发布 ANI：来源 SHA-256 `27e3637976a824c4efcd4a77ad9491d58f97ceb7c0ba3885e133ad14eebab7f3`，目标候选 `135196cdc34b858a9236ccb47ec94b918abc9dce8c6c1a1332ff9712da6fc570`，policy revision `sha256:655690090ed17bf49e0eab57baad643f90ec69ef3a412aa092fd3a24671a0e62`；descriptor `277e14b350a264710d044da967f28f6869c1741121d681e238be6e3dcbad9178`，schema revision `202609100002`。后续 caller 需固定并核对这些目标契约，不能仍使用旧 Service 名称或旧 registry 来宣称接通。

前端对接、Core/Auth 裁剪、现有环境切换与物理资产退役继续按 M1 后计划执行。WR-17/18 的完成不等于 IAM 已能完整替代 auth-service。
