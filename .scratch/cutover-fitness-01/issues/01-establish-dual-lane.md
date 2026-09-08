# 01: 在隔离集群建立 current/target 双轨

**What to build:** 在固定四小时时间盒内，为 `CUTOVER-FITNESS-01` 建立两个隔离 namespace、不可变镜像与 API-only runner，各 lane 运行一次固定 smoke 并输出可复现的 `environment-established` 证据，然后停止环境工作并恢复 DP2-10。

**Blocked by:** 00 / 固化 CUTOVER-FITNESS-01 环境与证据方案

**Status:** needs-info

**Type:** enhancement

**Plan mapping:** Direct P2 verification companion / half-day foundation

**Baseline:** current 与 target ANI base 均为 `56a5f0b493c8404a024a92647d93f2ba2f7daf35` tree `4ba6a15ad0cddf0db66a25d695b082d47346aff1`；target 在 detached tree 应用 SHA-256 `542084f3e06be1d454cd99eb0114a76b8e06a52f9cbf5a9d661e749fb678edc2` 的反 containment overlay 后，projected tree 必须为 `28eb0508c93aab28e86e044a88ada9e19bf16830`；target ani-iam product `a56a332834967603eb47a3824727982032bc4f5e` tree `ef84bfb6382eaa73f13a68754f506edeb17f2206`；cluster UID `f5cafbf1-5246-4f8f-b9da-742182f37528`。

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
- 两个 namespace 中各一个精确命名为 `cf01-registry-pull` 的 task-scoped pull-only Secret；不得复制本地 Docker credential
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

- [ ] 开始前确认 00 为 `resolved`、本事项为唯一 `claimed`，DP2-10 仍为 `ready-for-human` 且 dirty recovery state 精确不变。
- [ ] `git archive`/detached context 与三组固定 commit/tree 完全一致；build context 不包含当前工作树未提交文件。
- [ ] registry push/pull-by-digest、两个固定 `ani-block` PVC bind、NetworkPolicy same-lane allow/cross-lane L3/L4 deny 三项 foundation gate 均为 `pass`。
- [ ] 两个 namespace 各自拥有依赖、Secret、PKI、seed 和 Credential，且跨 lane 负向 probe 为 `pass`。
- [ ] current 与 target workload Ready；Ready 只作部署事实，不替代行为结论。
- [ ] scenario `CF01-HUMAN-PASSWORD-PROTECTED-READ-V1` 在 current/target 各执行一次：`GET /readyz`、`POST /auth/password/login`、携带 lane-local Access Token 的 `GET /instances` 均符合各自契约；target 的 `listInstances` 恰好一次 decision。
- [ ] 生成 `plan.json`、`environment.json`、`artifacts.json`、`results.json`、`summary.md`、`sha256sums.txt`，Secret scan 为 `pass`。
- [ ] T+4:00 或更早停止环境写入并封存 evidence；若无安全事件，事项 01 退出 `claimed` 后把 DP2-10 恢复为唯一 `claimed`，且不执行清理。

**Verification:** base/overlay/projected tree 与 OCI digest 校验；`kubectl` inventory/Endpoint/NetworkPolicy/PVC 证据；current/target 固定 HTTP smoke；跨 lane DNS 可解析但 TCP/UDP 连接失败；证据 Secret scan；repo staged-path audit 与 `git diff --check`。

**Stop conditions:** 任一 foundation gate 失败；精确 source/tree 不可得；构建需要脏工作树；target 需要修改业务/契约/config 安全边界；共享依赖或 Secret 成为必要条件；检测到跨 lane/fallback；集群 UID 漂移；时间盒到期。

**Recovery:** 不连接共享流量，因此运行失败不触发 ANI 回退。停止 runner 和新增写入，保留精确资源/日志供诊断。任何 workload/namespace/PV/image 删除都先列出带 `cutover-fitness-id=CF-01` 与 `run-id` 的精确对象，并等待人工确认；`ani-block` 为 `Retain`，不得假设删 PVC 会清理 PV。

**Human checkpoints:** 当前缺少一个只读五个 CF-01 repositories 的 task-scoped registry robot，因此事项保持 `needs-info`。开始集群/registry 写入前必须由人工提供或确认该 robot 的安全输入方式，并接受两个 `cf01-registry-pull` Secret、source/tree/overlay、目标 namespaces、image repositories、manifest digest 和四小时时间盒；删除或重建任何资源前另行精确确认。

## Comments

- 2026-09-08：只读 preflight 判定环境适合本事项，但 image pull、PVC bind、NetworkPolicy enforcement 和实际 runtime 仍为 `not_verified`；必须按顺序作为前三个门禁。用户提供的本地 Docker 登录只作为 push 前置，不默认授权复制进 cluster；pull-only robot 输入确认前不得领取本事项。
