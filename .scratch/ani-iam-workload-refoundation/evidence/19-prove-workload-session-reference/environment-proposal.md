> SUPERSEDED — 2026-09-10 用户指定 SSH ani / CF-01 测试环境。以下 kind 提案仅保留追溯，不得执行。实际集群预检见 cluster-preflight.json；新资源范围另行冻结。

# WR-19 真实 exec 环境操作待确认

状态：proposed / awaiting explicit approval。此文件与 Goal 不授权直接操作既有集群。

## 精确拓扑

- 主机仅 SSH `ubuntu`，本次实测 `i-8yg2l7u8`，4 CPU / 7935 MiB RAM，预检可用内存约 5212 MiB、磁盘 217 GiB。创建前再次核对，不能沿用这些瞬时数值。
- 新建一个单 control-plane kind 集群 `wr19-session-20260910`，无额外 worker、KubeVirt、CNI 定制、Ingress 或 NodePort。
- 集群使用专属 Docker network `wr19-session-20260910`；不能加入现有 `kind`/Network 测试网络。Kubernetes API 仅 `127.0.0.1` 动态端口。
- 仅新建目录 `/home/ubuntu/workspace/ani-iam-runs/wr19-live-20260910/`；kubeconfig、SA Token、CA、TLS 私钥和 DSN 只放其 `private/`，目录 0700、秘密文件 0600，不改默认 kubeconfig/context。
- IAM、ANI Gateway、Session Gateway 使用三个源码快照编译的正式进程，在 VM loopback 监听；PG/Redis 为本票专有容器。Session 使用专有 SA 的受限 kubeconfig；没有在 kind 中部署现有服务或覆写现有 Deployment。
- 两个新 Tenant fixture：`01993000-0019-7000-8000-000000000001`、`01993000-0019-7000-8000-000000000002`。各有一个 `ani-tenant-<tenant UUID>` namespace 和 `wr19-exec-a/b` BusyBox Pod，必须有真实 tenant/instance 双 label。新增 `wr19-control` namespace 与 `wr19-session` ServiceAccount。
- 仅两个 namespace Role/RoleBinding 授予该 SA `pods get/list`、`pods/exec create`；没有 Secret read、Pod create/delete、Node、VM 或全局管理权限。管理员 kubeconfig仅归环境创建/fixture/清理脚本，不进入 Session 配置。
- Gateway 读取其专有 owner 数据库中的受控 instance fixture，IAM 使用独立 Human/Tenant fixture并经正式登录。这里只证明普通容器 exec；VM/serial/VNC 成功链继续未验证。

## 固定运行输入

- kind `v0.31.0` linux/amd64：SHA-256 `eb244cbafcc157dff60cf68693c14c9a75c4e6e6fedaf9cd71c58117cb93e3fa`，来源 `https://github.com/kubernetes-sigs/kind/releases/download/v0.31.0/kind-linux-amd64`，摘要由对应 GitHub release API 返回，下载后再次校验。
- Node 缓存：`kindest/node@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed`。只读复用镜像字节，不改现有节点。
- exec fixture 缓存：`docker.changqingyun.cn/mirror/busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d`。使用固定 digest，不执行 `latest` 拉取。
- kind/kubectl 当前不在默认 PATH。kind 安装到本 Goal 用户态工具路径；kubectl 客户端在安装前固定精确版本与官方 SHA，不更改全局 PATH/profile或其他项目工具。
- 镜像执行前再次核对本地 Id/RepoDigests；镜像 inspect 用 JSON，先前使用不存在的 Config.Cmd 格式字段导致一次读取失败，不能据此认定缓存丢失或自动拉新版本。

## 容量与执行顺序

先完成 Go 构建，再启动集群与正式运行测试；不并行启动三个重构建。集群新增内存预算约 2–3 GiB，节点容器登记后限 CPU 2 / memory 3 GiB / 不额外 swap；两个 fixture Pod 各 requests 10m/16Mi、limits 100m/64Mi。预算是规划值，实际 RSS 与可用内存持续记录；资源不足或明显 swap/压力时停止新重任务，不增加节点、不自动放宽上限。

## 允许的精确环境动作（待用户接受）

1. 下载/校验用户态 kind/kubectl；新建上述专有 network、单节点集群及私有 kubeconfig。
2. 在该新集群中创建上述三个 namespace、两个 Pod、一个 SA、两个 Role/RoleBinding，签发限定时效的 SA Token，仅供本票 Session 进程使用。
3. 在本票资源上执行 exec/双 Tenant/最小 RBAC 负向、连接生命周期、重启和依赖故障恢复测试；不读取真实业务终端内容，不输出 ticket/Token。
4. 完成后根据登记的 cluster/container/network/namespace ID 清理本票新增资源与进程，保留源码、脱敏结果、私有失败现场和清理记录。

同名路径/资源已存在但归属不明、目标 context 不匹配、预检资源不足、测试要求写既有 Network/kc062/CF-01 或业务对象时停止该环境路径。禁止全局 prune、默认 context 切换、清理共享缓存或恢复任何已有业务数据库。
