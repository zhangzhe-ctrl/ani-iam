# WR-19 最终交接

状态：**完成，WR-19 resolved**。完成范围为真实 Gateway→Session 参考链与公共接入适配包。唯一事项权威仍在原 IAM；三个产品 worktree 不承载另一份 claimed 状态。WR-20 及以后未领取。

## 已验证结果

正式 IAM、ANI Gateway、Session Gateway 在 ubuntu 运行，使用独立 PostgreSQL/Redis、正式 Workload bootstrap、各自 mTLS 身份、exact Grant、短期 WAT/委托和 receiver 在线校验。两个 Tenant 经真实 Gateway REST 登录并连接各自 Kubernetes 容器，实际执行 shell 命令并验证结果。终端内容未写入证据。

`wr19-20260910T080445Z-be0b89ea` 退出 0，八项具名结果通过：响应丢失后回到原 Redis Session；两 Tenant exec；无身份/跨 Tenant 拒绝；ticket 单次消费；撤权后拒绝重试与兑换；已建连接撤权关闭；IAM 故障关闭与恢复；Gateway 证书正常续期。详见 [真实链结果](real-reference-results.json)。

用户接受的策略已实现：创建和 ticket 兑换在线校验；既有连接每 30 秒复查、单次 2 秒超时，撤销/拒绝/IAM 故障关闭。真实链断连断言上限 32.5 秒包含调度容差；IAM 故障子测试总耗时还包含恢复后的成功 exec。Session 持有与原请求绑定、加密保存的 receiver continuation，短期创建委托仍为 60 秒，不把创建凭据延长成可重用执行授权。见 [连接授权合同](continuation-contract.md)。

公共 [SDK](../../../../../ani-iam-wr19/sdk/README.md) 提供 caller、receiver、typed 已验证上下文和在线重查；[独立示例](../../../../../ani-iam-wr19/examples/workload-grpc/README.md) 使用同一 SDK。业务 owner 只提供明确目标注册、规范化 DTO 映射、真实资源检查和业务幂等，不解析 Token、不拼身份 metadata、不查询 IAM 身份表。新目标仍需 owner 契约及 exact Grant 注册。IAM 不拥有资源执行状态机。

## 验收与检查

| 检查 | 结果 | 证据 |
|---|---|---|
| 正式 bootstrap、并发/重启/响应丢失、Audit 原子回滚 | pass | [bootstrap](bootstrap-final-results.json) |
| WAT/委托/在线 receiver 拒绝矩阵与公共 SDK | pass | [invocation](invocation-results.json)、[SDK](sdk-standalone-results.json)、[独立示例](example-runtime-results.json) |
| 连接授权、过期/替换/撤权/IAM 故障、定向 race | pass | [continuation](continuation-results.json)、[真实链](real-reference-results.json) |
| WR18 受影响回归 | pass，58 项具名真实依赖结果，无 fail/skip | [最终检查](final-verification-results.json) |
| Session lint/contract/重复生成/manifest/root+API tests/race/vet/build | pass | [最终检查](final-verification-results.json) |
| ANI make test、架构/auth/生成、文档入口、Services 边界 | pass；保留 4 条已接受的既有边界告警 | [最终检查](final-verification-results.json) |
| 三 worktree diff、路径与源码一致性 | pass | [逐文件核对](final-source-consistency.json) |
| 正式日志、已登记进程/容器与保留对象核对 | pass | [真实链清理及日志](runs/wr19-20260910T080445Z-be0b89ea/cleanup-and-log-scan.json)、[UID 核对](final-fixture-verification.json) |

完整 11 项 Goal 条件对应关系见 [验收矩阵](acceptance-matrix.json)。所有重任务在 ubuntu 串行执行，GOMAXPROCS=2、-p=2。Session 生成使用自身 Makefile 固定的 buf 1.60.0 / protoc-gen-go 1.36.12 / protoc-gen-go-grpc 1.6.0；IAM 使用其固定工具。最后一次仓库门禁为 `wr19-20260910T081233Z-53d52093`，退出 0。

最终产品源码与真实链使用的代码完全一致；其后只更新了 Session/ANI 交付文档，已通过最终仓库门禁；收尾记录的文档入口在 `wr19-20260910T082134Z-2c35dca1` 再次通过，退出 0。远端临时 ANI go.work 提供候选版本映射；Session 重生成内容逐字节相同，远端私有 umask 仅将生成文件权限由 0644 改为 0600。本机交付文件未发生此权限变化。交付 go.mod 无本机绝对 replace。

## 源码与可审查差异

| 产品 | 独立 worktree | 差异 |
|---|---|---|
| IAM | `/home/chabking/workspace/ani-iam-wr19` | [仅 WR19，相对完整 WR18](iam-wr19-only.diff)；[完整产品差异，含二进制生成物](iam-product.diff) |
| ANI Gateway | `/home/chabking/workspace/ANI-wr19` | [ANI diff](ani-product.diff) |
| Session Gateway | `/home/chabking/workspace/ani-session-gateway-wr19` | [Session diff](session-product.diff) |

三仓库固定 Git 基线、完整 WR18 未提交源码、增改删、逐文件 SHA/模式、源码包摘要和测试快照在 [最终源码核对](final-source-consistency.json) 中。原 ANI/Session checkout 保持干净固定基线；WR18 174 个源文件和 5 个删除路径保持原样；原 IAM 仅修改 WR19 事项、计划状态及本票 evidence，已有文档工作保留。未 stage、commit、push、打 tag、创建 PR、发布 API/SDK/镜像。

## 资源保留与复用

按用户要求，**16 个声明的可复用 Kubernetes 对象全部保留**，包括两个 Tenant namespace、两个 Deployment、NetworkPolicy、RBAC 和两个受限 ServiceAccount。控制器生成的子资源记录在 [保留清单](k8s-retained-resources.json)，16 对象创建 UID 在 [创建记录](k8s-create-events.jsonl)。最终 16 个新对象、25 个既有受保护对象和 3 个集群/namespace 身份共 44 项 UID 全部匹配；没有删除或修改既有 CF 资源。

本轮三个正式进程及续期替换进程、5 个真实链依赖容器、7 个最终回归依赖容器均已退出/不存在；SSH 数据隧道已按 PID/启动身份关闭，见 [隧道恢复](tunnel-cleanup.json)。两次早期真实链失败/准备中断均有单独记录，没有不明状态重跑。

复用时先核对 [v2 manifest](k8s-proposal-v2.json) 和已登记 UID，再启动本票专用 SSH 路由并签发新短期受限访问。现有新签发 SA token 于 `2026-09-10T08:47:53Z` 到期，值仅在远端私有目录；不复用旧共享凭据。新一轮仍建立独立 IAM/ANI 数据和测试身份；复用这些资源不等于授权其它事项自动操作它们。

## 边界与历史

候选公共 API/SDK `v0.1.0-rc.1` 尚未发布；当前依赖通过已记录远端 go.work 验证。真实 serial/VNC、其它调用方、完整 M1、前端接入、Core 旧身份裁剪、共享环境替换和 production readiness 均未在 WR19 验收。

第一轮真实链因隔离 ANI 数据库遗漏已有 workload_instances 权限授予而失败；补齐固定 owner SQL 的原句后通过，没有放宽运行数据库角色。最后回归首次误用 IAM 默认 gRPC 生成器，修正为 Session 固定版本后重复生成及所有后续门禁通过。SSH 准备中断先核查原 run 未 dispatch，再创建新快照；串行上传改为本票独立 SSH 连接复用，没有自动重放已执行测试。

[历史过程](historical-progress.md)保留此前局部通过、未决和 blocked 检查点；[用户确认](user-acceptance-20260910.json)解除对应依赖。历史稿和先前未执行的 11 对象提案只作追溯，不能作为当前操作入口。
