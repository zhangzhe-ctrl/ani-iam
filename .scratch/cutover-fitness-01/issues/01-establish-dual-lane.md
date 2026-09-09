# 01: 在隔离集群建立 current/target 双轨

**What to build:** 在固定四小时时间盒内，为 `CUTOVER-FITNESS-01` 建立两个隔离 namespace、不可变镜像与 API-only runner，各 lane 运行一次固定 smoke 并输出可复现的 `environment-established` 证据，然后停止环境工作并恢复 DP2-10。

**Blocked by:** 00 / 固化 CUTOVER-FITNESS-01 环境与证据方案

**Status:** resolved

**Type:** enhancement

**Plan mapping:** Direct P2 verification companion / half-day foundation

**Baseline:** current ANI 固定为 `56a5f0b493c8404a024a92647d93f2ba2f7daf35` tree `4ba6a15ad0cddf0db66a25d695b082d47346aff1`；target Gateway 固定为与 DP2-10 registry revision 匹配的 ANI `4ff73e09c16df706af2aacdff6d763ed4aeaa873` tree `bc0ddb3ba1a449bc30b7815f9047aeb4390e8eff`；target ani-iam product 固定为 DP2-10 `78c5265bcd9eddb97ed7525fe4756db03abc5c50` tree `2ec15ab7d51dcc8d66c60f5d5678b034c3721f00`；`fb862d75854e2a31d60ac5a40182dc678ae88c44` 只关闭 `.scratch` 证据且不改变该 product tree；cluster UID `f5cafbf1-5246-4f8f-b9da-742182f37528`。

**Scope:** detached source archives；task-only Dockerfiles/scripts/manifests；local build and push to the exact CF-01 repositories；`ani-cutover-current` and `ani-cutover-target` resources；current Auth/Dex/API canary；target DP2-05 API canary；JSON/Markdown evidence。

**Out of scope:** 完整 24 格、DP2-10 API Key、Envoy、Inference、Core lifecycle/NATS semantics、target OIDC、前端、HA/负载/生产就绪、真正切换/回退、任何 current/target 业务修复。

**Allowed local paths:**

- `.scratch/cutover-fitness-01/**`
- `deploy/cutover-fitness/**`
- `tools/cutover-fitness/**`
- `.scratch/ani-iam-p2-direct/issues/10-deliver-service-principal-api-key.md`（只允许在 CF-01 evidence 封存后恢复 `Status: claimed` 并追加 handoff Comment）

**Allowed external targets:**

- SSH alias `ani` 上 cluster UID `f5cafbf1-5246-4f8f-b9da-742182f37528`
- namespace `ani-cutover-current`
- namespace `ani-cutover-target`
- 只新增 `docker.changqingyun.cn/ani/cf01-probe`、`docker.changqingyun.cn/ani/cf01-ani-gateway-current`、`docker.changqingyun.cn/ani/cf01-auth-service-current`、`docker.changqingyun.cn/ani/cf01-ani-gateway-target`、`docker.changqingyun.cn/ani/cf01-ani-iam-target` repositories 中本事项 tag/digest
- cluster 直接 pull-by-digest；不得创建 `imagePullSecret`，不得读取、打印、记录或复制本地 Docker credential
- `ani-cutover-current/cf01-current-postgres-data` 与 `ani-cutover-target/cf01-target-postgres-data` 两个 PVC，以及它们触发的 provisioner-owned PV/VolumeAttachment；这些 cluster-scoped 副作用只允许创建/观察，不授权删除
- task-owned local Docker build cache 与 detached temporary build directories

**Forbidden paths and targets:**

- 当前 DP2-10 dirty files，包括 `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/integration/service_principal_test.go` 和其现有 evidence
- `api/**`、`internal/conf/**`、`cmd/server/**`、`configs/config.yaml`、`go.mod`、`go.sum`、`.github/**`
- 全部 ANI checkout/worktree 和 Git refs；只能读取固定 Git object，不得 commit、merge、rebase、revert、checkout 或生成回写
- `ani-system`、`ani-auth`、`ani-aigw`、`ani-test2` 等既有 namespace 及其 Secret/PVC/Service/数据
- 共享 PostgreSQL/Redis/NATS/Dex/Credential、真实流量、Ingress/NodePort/LoadBalancer、代码 fallback、双读/双写
- 删除 namespace/PV/image、Credential 失效或任何不带本事项 `run-id` 的资源

**Evidence path:** `.scratch/cutover-fitness-01/evidence/01-establish-dual-lane/`

- [x] 开始前确认 00 为 `resolved`、本事项为唯一 `claimed`，DP2-10 仍为 `ready-for-human` 且 dirty recovery state 精确不变。
- [x] `git archive`/detached context 与三组固定 commit/tree 完全一致；build context 不包含当前工作树未提交文件。
- [x] registry push/pull-by-digest、两个固定 `ani-block` PVC bind、NetworkPolicy same-lane allow/cross-lane L3/L4 deny 三项 foundation gate 均为 `pass`。
- [ ] 两个 namespace 各自拥有依赖、Secret、PKI、seed 和 Credential，且跨 lane 负向 probe 为 `pass`。
- [ ] current 与 target workload Ready；Ready 只作部署事实，不替代行为结论。
- [ ] scenario `CF01-HUMAN-PASSWORD-PROTECTED-READ-V1` 在 current/target 各执行一次：`GET /readyz`、`POST /auth/password/login`、携带 lane-local Access Token 的 `GET /instances` 均符合各自契约；target 的 `listInstances` 恰好一次 decision。
- [x] 生成 `plan.json`、`environment.json`、`artifacts.json`、`results.json`、`summary.md`、`sha256sums.txt`，Secret scan 为 `pass`。
- [x] T+4:00 或更早停止环境写入并封存 evidence；若无安全事件，事项 01 退出 `claimed` 后把 DP2-10 恢复为唯一 `claimed`，且不执行清理。

**Verification:** base/overlay/projected tree 与 OCI digest 校验；`kubectl` inventory/Endpoint/NetworkPolicy/PVC 证据；current/target 固定 HTTP smoke；跨 lane DNS 可解析但 TCP/UDP 连接失败；证据 Secret scan；repo staged-path audit 与 `git diff --check`。

**Stop conditions:** 任一 foundation gate 失败；精确 source/tree 不可得；构建需要脏工作树；target 需要修改业务/契约/config 安全边界；共享依赖或 Secret 成为必要条件；检测到跨 lane/fallback；集群 UID 漂移；时间盒到期。

**Recovery:** 不连接共享流量，因此运行失败不触发 ANI 回退。停止 runner 和新增写入，保留精确资源/日志供诊断。任何 workload/namespace/PV/image 删除都先列出带 `cutover-fitness-id=CF-01` 与 `run-id` 的精确对象，并等待人工确认；`ani-block` 为 `Retain`，不得假设删 PVC 会清理 PV。

**Human checkpoints:** 用户已接受 source/tree/overlay、两个目标 namespaces、五个 image repositories、cluster 直接 pull-by-digest、manifest/运行证据和四小时时间盒，因此本事项已领取。删除或重建任何资源、创建 `imagePullSecret`、复制 Credential、修改节点 registry 配置或扩大权限仍须另行精确确认；本事项遇到 pull 失败时直接停止。

## Comments

- 2026-09-08：只读 preflight 判定环境适合本事项，但 image pull、PVC bind、NetworkPolicy enforcement 和实际 runtime 仍为 `not_verified`；必须按顺序作为前三个门禁。用户提供的本地 Docker 登录只作为 push 前置，不默认授权复制进 cluster；pull-only robot 输入确认前不得领取本事项。
- 2026-09-08T18:52:53+08:00（`started_at=2026-09-08T10:52:53Z`）：用户已授权使用本机现有 Harbor 登录向五个固定 CF-01 repository 构建并 push，并确认 cluster 可直接拉取；本事项不创建 `cf01-registry-pull` 或任何 `imagePullSecret`。新 `cf01-probe` 的 pull-by-digest canary 是事实门禁，必须在每个 Ready node 实际成功；任一直接拉取失败即停止，不得降级为复制本地 Credential、创建 Secret、修改节点 registry 配置或扩大权限。四小时时间盒从本条领取记录开始。
- 2026-09-08T11:27:14Z：`environment-established=fail`，事项转为 `ready-for-human`。Registry、Storage、Network 三项 foundation gate 均为 `pass`，但固定 current ANI Auth Dockerfile 在 fixed source 上构建失败：`go build` 要求更新 `go.mod`/运行 `go mod tidy`；修改 ANI Dockerfile、module 或 detached source 均越界，因此按 immutable build stop condition 停止，current/target runtime 与固定 smoke 保持 `not_verified`。完整证据在 `../evidence/01-establish-dual-lane/`。未创建 Secret/Credential，未发生跨 lane 泄露或共享环境写入，storage Job 已结束且无运行中的写入动作；foundation namespace、probe/echo/runner、两个 PVC/PV 按授权保留，不执行删除。DP2-10 protected porcelain SHA-256 仍为 `02fe876c9e4a3e2f49c43a864864d62230a548e641fda790b7e5549752aedff6`，tracked binary diff SHA-256 仍为 `e1387ff7898b606ceaf4f524346525fc2e3b6df0a0eb0ae064b7389882a4371c`。
- 2026-09-08T11:48:31Z：用户接受简化后的 current build 口径并要求继续：事项 01 已固定 ANI `main@56a5f0b493c8404a024a92647d93f2ba2f7daf35`，且远端 `main` 仍指向该提交，因此不得切换动态 ref；允许在 `deploy/cutover-fitness/**` 新增 task-owned Dockerfile，基于该 detached source 完成 current Auth 镜像并记录 source commit/tree、Dockerfile/base image、build command 与最终 OCI digest。该明确决定取代“必须使用 tree 内既有 Auth Dockerfile原样成功”的停止条件，但不授权修改 ANI checkout/ref/source、把当前工作树作为 context、读取/复制 Credential、扩大 cluster/registry/path 范围或放松其他门禁。事项 01 重新成为唯一 `claimed`；复用 foundation 前必须重验资源归属、无运行中写动作和 cluster UID。
- 2026-09-08T12:18:01Z：按用户最新口径仅收口 fixed-source current Auth 镜像，结果 `pass`。使用事项 01 已固定的 ANI `main@56a5f0b493c8404a024a92647d93f2ba2f7daf35`（tree `4ba6a15ad0cddf0db66a25d695b082d47346aff1`）detached archive 与 task-owned Dockerfile 构建；远端 `main` 验证时仍为同一提交。镜像固定为 `docker.changqingyun.cn/ani/cf01-auth-service-current@sha256:8e5d4d11662d7fdfab92f3732b07e9b0999c2de2b4bcf50af52a86cd83d1c018`，完整记录位于 `../evidence/01-establish-dual-lane/current-auth-fixed-build-20260908t114831z/`。未修改 ANI checkout/ref/source；按用户要求不继续 runtime/smoke 扩展，因此事项 01 的 broader environment 仍未完成并转为 `ready-for-human`，DP2-10 恢复为唯一 `claimed`。
- 2026-09-09T01:34:39Z：用户授权恢复事项 01 且只完成环境。只读复核确认 cluster UID、两个 namespace UID、PVC/PV UID 与既有记录一致；两个 storage Job 均 `active=0`/`succeeded=1`，唯一挂载 PVC 的 Pod 均已 `Succeeded`，Running echo/network/registry-canary Pod 不挂载 PVC；未发现未知 namespace 对象或活动数据写入。registry canary 使用上一轮已构建的 probe digest `sha256:36ff3463579a46479b1ac1719ef62f9d6ab003db040376384ae95a9b63694376`，记录为已知 CF-01 自有变更。current 保持 `56a5f0b...`；target IAM product 更新为 DP2-10 `78c5265...`，target Gateway 使用 policy revision 匹配的 ANI `4ff73e09...`，不得复用旧 `28eb050...` target Gateway。只部署最小 current Auth/Gateway 与 target IAM/Gateway，并各执行一次固定 `readyz → password login → instances`；target 必须恰好一次 authorization decision。仅全部通过才可 `resolved`，否则停止且不进入 DP2-10 对比。
- 2026-09-09T02:04:00Z：环境恢复 `pass`，事项 01 `resolved`。current 固定链路与 target 固定链路均仅执行一次且都是 `200 → 200 → 200`；两边都取得 access token。target IAM `CheckPermission` 的成功请求计数从 0 增至 1，delta 恰好为 1；target 数据库同时产生 1 个 Session、1 个 Session Grant 和 1 条登录 Audit。current/target Deployment 最终均 `available=1`、Pod 均 `6/6 Ready`，只挂载各自 PVC，不存在 task-owned Service/Ingress/NodePort/LoadBalancer/imagePullSecret。target Gateway 固定为 ANI `4ff73e09...` 与 IAM `78c5265...` 的 policy revision `sha256:1d5c80...` 一致；harness commit `13e709f4359b81f51444b9875c430b6ba98d00ec`；OCI digests、启动装配修正与脱敏证据见 `../evidence/01-establish-dual-lane/runtime-20260909t013439z/`。未进入 DP2-10 对比。
