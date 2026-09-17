# WR23 恢复验证与维护

唯一入口是 `bash tools/wr23-resume/run.sh <phase>`；必须从四仓已重建候选使用，不追随 main。当前权威记录在原 ani-iam 的 `.scratch/ani-iam-workload-refoundation/evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9/README.md`。完整复现使用 `all`，要求该入口已冻结最终候选；执行状态与真实观察证据决定是否完成。

## 操作

- `run.sh A`：完整 Core/NATS/IAM/Notification/SMTP/邀请/登录主链。
- `run.sh directed|components|broker|bootstrap|snapshot|crash|authority|dlq|envoy|session|aggregate`：指定门禁，均创建新专有 run、固定四仓字节/模式、固定 Git 对象和模块映射后执行。
- `run.sh rehearsal`：两分钟真实 observer 自检，包含完整 A、刷新、BOSS 再登录、认证 full rebuild；不能计入 24h。
- `run.sh enforced|enforced-session`：只在同一候选的完整实际 24h 证据 admission 验证通过后运行；前者经真实 Core freeze/unfreeze 与 Envoy/IAM/Inference owner 事件核对提前拒绝，后者复验 Session。
- `run.sh all`：最终候选冻结且全部阶段实现后，串行完成必要门禁，再签发绑定 manifest 的观察 admission，执行真实 24h 和观察后的强制回归。所有阶段保留独立退出码。
- `run.sh status <run_id>`、`run.sh wait <run_id>`、`run.sh collect <run_id>`：检查/续等/仅收回公开证据。断开客户端不会停止已通过 nohup 启动的远端进程。
- `run.sh resume <authority_evidence>/executions/all-*.json`：从同一候选、同一既有 run 继续观察；不拼接新旧 24h 窗口，也不自动重跑失败阶段。

生成输出仅在开发修复时通过 `python3 tools/wr23-resume/collect-run.py <run_id> --apply` 收回；工具复核输入 SHA 与每个 Allowed 文件，遇到较新本地修改则跳过。正式 `all` 不回写候选，而且要求生成输出无差异。不要在 source packing 尚未打印 dispatched 前改动候选。

用户已提供新环境并恢复 WR23。Envoy/Session 可在已冻结的新环境执行；真实24h/强制启用仍受完整前置门禁约束。

## 运行边界

所有生成、Go 工作、测试、镜像和容器均在 `remote-environment.json` 固定的 `ssh ani-test-1`（ubuntu / ani-01），由 `remote-run.py` 复核身份并把环境存入每个 run.json。旧 run 未带环境字段时仍显式读取原 ubuntu / i-8yg2l7u8；status/collect 不会把旧证据重定向到新机器。GOROOT 保持 Go1.26.7 固定路径与内容。heavy.lock 非阻塞独占，冲突 exit75；不抢锁、不停止其他任务。每次运行独立目录、网络、数据库、凭据与标签 `ani.goal=wr23`、`ani.run_id=<run>`，缓存仅在本票 root 下串行复用。Base、Envoy、Session 和 shadow 预算先预检；不足就 fail。新主机没有 swap，预检要求额外物理 RAM 覆盖原 swap 全部余量，总预留量不减；不在现有 Kubernetes 主机启用 swap。Session 使用自己的 kind 集群，精确节点和标签清理；不碰共享集群。

本票 Docker 为用户服务 `ani-iam-wr23-docker.service`，固定 Docker29.6.1、专有 socket `/run/user/1000/ani-iam-wr23-docker.sock` 与 `/home/ubuntu/.local/share/ani-iam/wr23-environment/docker-data`；使用 rootless/systemd cgroup v2，daemon 上限4CPU/8GiB，实际容器继续各自预算。运行前该服务须 active；固定工具、包清单、服务配置和原服务保护核对见权威 evidence/new-environment-setup 与 new-environment-authorization.json。kubectl 使用任务工具目录的固定摘要，原主机 kubectl 不覆盖。历史私有目录不参与复现。

Session 的 Busybox 从 Docker Hub 公共镜像源按原 OCI 摘要拉取。导入专有 kind 节点时，核对原 OCI index 内容摘要和 containerd 目标摘要，再建立 CRI 使用的完整镜像名称；不能把 Docker 归档的外层 index 冒充原镜像。创建 Pod 前要求 CRI 返回相同固定 RepoDigest。

公开 evidence 不含凭据。runtime、私钥、Token、原始日志留在远端 run/private（0700/0600），只能窄范围本地诊断。collect 只取明确公开白名单、摘要与 results 文件，不能打包 private 回本地。

正常结束由 Go cleanup 关闭专有进程/容器，再由 runner 核对；残留导致 cleanup fail。发生未确认 SSH dispatch 时，先检查该次 run.json 与 `command.pid/command.exit`；不要直接再启动一次。观察进程丢失时保留原样本，诊断后使用新 run 重新计时；不是从最后一条样本补足累计时长。

## 当前数据流与恢复

Core 是 Tenant/Quota/outbox authority。可信 NATS 路由和独占 producer NKey 归因；IAM 非秘密 Binding/Grant 与当前 Principal 版本在接收及执行事务中独立复核。接收/DLQ durable 后才 ACK，失权恢复不自动重放。当前读取只经 current_tenant_lifecycle/current generation，runtime 不能回退旧投影表。

Snapshot 是真实 mTLS+WAT 的两个只读 HTTP 目标。已有 `ani-iam-server begin-core-snapshot --config-file <本次runtime.json> --approved-config-sha256 <其精确SHA> --request-key <新UUID> --page-size 1` 只开始 owner cut；正式 daemon 负责分页、增量追平和原子激活，过期或授权变化 cut 保留 abandoned，不改写原文。此动作只用于本票专有数据库，不授权生产重建。

三个 Bootstrap 入口逐个经权限/当前 Broker authority/审计验证后启用；使用已有 Gateway 路由，以明确 reason 恢复原 job 或同身份补发。DLQ 已获三个有限管理入口授权：GET `/api/v1/iam/platform/core-dlq/{consumer_id}/entries`、GET 单条 `/{entry_id}`、POST `/{entry_id}/replay`。采用专用 platform `iam.dlq/read|replay` 权限；POST 必须带原文 SHA、expected_attempt、reason_code 和幂等键。事务内重收存储原文并关联 attempt/审计，Bootstrap 继续原 worker，不能以 SQL replay 或 NATS 重发布代替。`run.sh dlq` 执行正式进程故障矩阵；所有失败原文和记录保留。

如需回退投影强制，只在本票隔离配置关闭 IAM 提前拒绝并记录 degraded；Core/资源 owner guard、过期拒绝和单 writer 保持。旧候选、旧证据、共享资源、已存在凭据均受保护。完成本票不自动授权 WR24/25、提交、发布或切流。

### WR23 accepted observation-window amendment (2026-09-15)

The user approved replacing this WR23 24-hour requirement with at least 45,000
real seconds, at least 3,000 actual lifecycle samples, and at least 1,800 seconds
after successful authenticated full rebuild. All other latency, authority,
recovery and enforced Envoy/Session gates remain required. `active_24h` stays
`not_verified` for this mode. `run.sh all` now uses `accepted12h30`; it preserves
the 12-hour rebuild, five-minute real refresh and four-hour real OIDC login.

The original already-running 24-hour binary was not modified. Its durable sample
prefix is independently verified by `accepted-window.py` using the explicit
`observation-window-authorization.json` and its SHA256. The resulting
`accepted-window-results.json` is separate from the original process result.
An authorized early stop and its nonzero exit are retained, never rewritten as
a successful 24-hour run. A continuation may cite this accepted prefix only with
`evidence_kind=user_approved_observation_prefix`; the normal enforcement gate
still checks the actual timestamps, denominator, successful rebuild plus 30
minutes, p99 <= 5 seconds and zero gaps/differences/quarantines.

Only acceptance test/tool files changed after observation. The source-equivalence
record must prove all product, schema, registry, configuration and dependency
files are unchanged before reusing prior evidence or admitting enforcement.
