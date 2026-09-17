# WR21 隔离运行与证据

唯一执行台账在原 IAM `.scratch/ani-iam-workload-refoundation/issues/21-complete-tenant-workload-api-key.md`；唯一 evidence 为同层 `evidence/21-complete-tenant-workload-api-key/`。候选目录不复制事项状态。

`remote-run.py` 先校验两个固定 HEAD/tree、逐文件 allowlist、初始源码及当前 SHA/模式，再上传完整 manifest（含删除记录）。确认 ubuntu 主机身份后，建立唯一 `wr21-<run_id>` 目录，使用远端 heavy.lock、GOMAXPROCS=2、GOFLAGS=-p=2。依赖下载、生成、构建、测试都在远端执行。

在 IAM 候选根目录提交一次运行：

```sh
python3 tools/wr21/remote-run.py --command-file tools/wr21/stage-a.sh
python3 tools/wr21/remote-run.py --command-file tools/wr21/stage-b.sh
python3 tools/wr21/remote-run.py --command-file tools/wr21/stage-c.sh
```

这些是三个分别提交的示例；必须等待前一个重任务结束后再提交下一个。`generate.sh` 运行固定工具和双副本生成/格式化检查；`gates.sh` 运行定向测试、race/vet、实际 PG 和适用架构/契约门禁；`prepare.sh` 是明确列出的串行组合入口。它们不会发布模块或镜像。

SSH 中断后先检查同一 run 的 `command.pid`、`command.waiting`、`command.started`、`command.exit`、`command.finished` 与 private 日志。不要重新提交来猜测前次是否运行。

```sh
python3 tools/wr21/collect.py wr21-<已有 run_id>
python3 tools/wr21/collect.py wr21-<已有 run_id> --generated
```

生成物导入要求双副本一致、scope 允许，且本机输入/输出仍与上传摘要一致；不会覆盖较新的本机编辑。公开结果只收集状态、退出码、脱敏事件和摘要。凭据与原始日志留在远端 private（0700，文件最小权限），不拷入公开证据。

每个场景创建专有 PG/Redis、非特权 runtime role、网络、loopback 端口、CA/证书、身份/Grant、两 Tenant 前置种子；管理验收不以 SQL 创建 Workload/Key。阶段 B 的 `wr21-adapter-tests` 是实际 IAM/Inference 的恶意请求协议探针，不是替代 owner。正式 Envoy HTTP 链独立验证。重启后的恢复探针最长等待10秒，期间503/504仍须阻止请求；普通权限变更仍要求下一次判定生效。

正常结束由登记的测试清理钩子停止专有进程/容器和网络；源码、固定工具和 private 诊断文件保留。人工恢复或停止仅使用该 run 的精确登记 ID；不 prune、flush，不操作旧 run/shared deployment。最终保留/停止清单归档到唯一 evidence。

最终验收的 A/B/C run 与机器可读结果见原 IAM evidence/acceptance-matrix.json。A 额外执行真实 PG 事务并发 race；B 的15项协议探针结果与 HTTP 链分别标级。接续验收须由授权的唯一 claimed 事项承载并创建新 run，不能直接重放已结束 run 的凭据或命令。

恢复入口：保留源码 manifest、command.sh、tools.txt、固定 Envoy SHA 和专有配置摘要；以 `remote-run.py --command-file` 重新构建新环境/新身份。若新运行中断，先读该 run 的 command.pid/started/exit/finished/operator-stop.json 与 resources.jsonl/reference-events.jsonl，精确核对 PID 可执行路径与容器 ani.run_id 标签后只停止该 run 资源。SIGTERM/INT 明确记143/130；operator-stop 存在时不能仅凭旧 runner 的0退出码认定通过。
