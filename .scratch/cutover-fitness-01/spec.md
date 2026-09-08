# CUTOVER-FITNESS-01：持续可切换性验证轨道

Status: frozen / companion plan

本规格由用户于 2026-09-08 要求：先只读探测隔离测试环境，再把 `current` / `target` 双轨方案固化；允许为搭建暂停一个工作半天，随后恢复 ANI IAM Direct P2 开发。本规格不重编号、不改写也不替代 DP2-01–20；它只提供一个横向 verification companion，持续把“最终能否切换”变成可见证据。

## 1. 决策与边界

固定决策如下：

1. 先用一个最长四小时的工作半天建立最小双轨环境，然后无论结果是 `pass`、`fail` 还是 `not_verified`，都停止扩大环境工作。证据封存且没有安全事件后，按本轮用户授权把 DP2-10 恢复为唯一 `claimed`；环境事项不能继续占用 IAM 主线。
2. `current` 与 `target` 是两个完全隔离的运行 lane。不存在 shadow call、同请求双写、逐请求 fallback、共享业务数据库、共享 Redis/NATS/Dex 状态、复制原始 Credential 或把 target 失败转发给 current。
3. `current` 记录精确 ANI 当前版本的行为，但不是 target 语义的万能 Oracle。已接受的 breaking change 进入 difference ledger；未知差异是失败，不能靠宽松归一化隐藏。
4. 第一工作半天的完成信号叫 `environment-established`，不叫 Go/No-Go B。完整五类调用方、整组切入和部署级回退仍属于 DP2-13/14。
5. 纯 API runner 是首选 Interface；前端不是本事项前置。Cookie、CSRF、Origin、redirect、状态码、错误 reason 和 Header 可以用 HTTP client/cookie jar 验证；浏览器多 Tab、内存 Token 和最终 UX 保持 `not_verified`。

## 2. 不可变基线

### 2.1 Current lane

- ANI source commit: `56a5f0b493c8404a024a92647d93f2ba2f7daf35`
- ANI source tree: `4ba6a15ad0cddf0db66a25d695b082d47346aff1`
- 来源：2026-09-08 只读 `git ls-remote` 得到的 `origin/main`
- 含义：9 月 30 日前 current contract；目标 IAM 被 containment 移除，现有 ANI 行为不得被 target 轨道改变。

### 2.2 Target foundation lane

- ANI base commit/tree：与 current 相同，仍是 `56a5f0b493c8404a024a92647d93f2ba2f7daf35` / `4ba6a15ad0cddf0db66a25d695b082d47346aff1`
- target overlay source commit: `804db51a5f93605f9bbd4ac407f0489ecb1d187c`
- target overlay source tree: `28eb0508c93aab28e86e044a88ada9e19bf16830`
- overlay 关系：`804db51...` 是 base `56a5f0b...` 的直接父，提供 latest-main containment 的精确反向 tree delta。
- overlay 生成命令：`git -c core.quotePath=true diff --binary --full-index --no-ext-diff 56a5f0b493c8404a024a92647d93f2ba2f7daf35 804db51a5f93605f9bbd4ac407f0489ecb1d187c --`
- overlay SHA-256: `542084f3e06be1d454cd99eb0114a76b8e06a52f9cbf5a9d661e749fb678edc2`（2,331,603 bytes / 80 paths）
- projected ANI tree 必须等于：`28eb0508c93aab28e86e044a88ada9e19bf16830`
- ani-iam product commit: `a56a332834967603eb47a3824727982032bc4f5e`
- ani-iam product tree: `ef84bfb6382eaa73f13a68754f506edeb17f2206`
- 含义：ANI Adapter 只恢复 DP2-05 target seam；ani-iam product 包含已提交的 DP2-09 后端。CF-01 不声称完整 DP2-09 Gateway caller，也不包含当前未提交的 DP2-10。

target 必须从 `56a5f0b...` 的 detached archive 开始、校验 overlay digest、应用 overlay，再证明 projected tree 等于上述固定 tree；不得直接把父提交 archive 冒充 latest-main baseline。该 projected tree 只是隔离测试 Artifact，不是可 merge、可发布或可回写 ANI `main` 的候选。不得把任何现有 ANI checkout、ani-iam 当前脏工作树、动态 `main` 或裸 `latest` 当 Docker context。

后续每次变更基线都必须由人工接受新的 commit/tree 和差异范围。旧 report 不自动继承到新 digest；受影响格子回到 `not_verified`。

## 3. Cutover Fitness Module

该 Module 只暴露一个稳定 Interface：

```text
Evaluate(PlanRef) -> EvidenceBundle
```

概念 CLI：

```text
cutover-fitness evaluate --plan <immutable-plan.json> --output <run-directory>
```

`PlanRef` 必须内容寻址并固定：current/target Git commit 和 tree、OCI image digest、OpenAPI/Proto/operation registry/config/seed/scenario/difference-ledger digest、cluster UID、namespace UID 与 `run_id`。它拒绝动态 ref、裸 tag、脏工作树和未知 Artifact。

`EvidenceBundle` 中每个 claim 固定输出：

```text
current: pass | fail | not_verified
target: pass | fail | not_verified
difference: equivalent | intentional_breaking | defect_current | defect_target | not_compared
cutover_signal: pass | fail | not_verified
evidence_refs: [...]
```

Implementation 可以隐藏镜像构建、双轨部署、seed 投影、Credential 生成、故障注入、结果归一化、差异分类和证据收集；这些复杂性不得泄漏到每张 DP2 ticket。只允许归一化随机 ID、时间、Token 和 Secret；状态码、reason、claims、Tenant boundary、调用次数和持久化/NATS 副作用不得忽略。

## 4. 固定的 24 格进度面

不按“测试条数”计算百分比。进度固定为 6 个 journey × 4 层 evidence，共 24 格；每格只允许 `pass/fail/not_verified`。

| Journey | C：契约与 Artifact pin | I：IAM 与真实依赖 | A：真实调用方进程 | F：失败、隔离与恢复 |
| --- | --- | --- | --- | --- |
| Human Password / OIDC / Session | C1 | I1 | A1 | F1 |
| Refresh / Logout / SwitchTenant | C2 | I2 | A2 | F2 |
| Tenant / Permission / Gateway | C3 | I3 | A3 | F3 |
| Service Principal / API Key / Envoy | C4 | I4 | A4 | F4 |
| Service Token / Inference | C5 | I5 | A5 | F5 |
| Lifecycle / Bootstrap / NATS / Core | C6 | I6 | A6 | F6 |

已有 local process/fake-client 证据可以成为某格的 evidence ref，但不能替代 cluster 中的真实依赖或调用方进程。未运行、skip、mock-only 或仅看到 Pod Ready 都是 `not_verified`。

## 5. 隔离拓扑

固定环境身份：

- SSH：`ssh -F /home/chabking/.ssh/config ani`
- Kubernetes context：`kubernetes-admin@kubernetes`
- cluster UID：`f5cafbf1-5246-4f8f-b9da-742182f37528`
- current namespace：`ani-cutover-current`
- target namespace：`ani-cutover-target`
- registry：`docker.changqingyun.cn`
- PVC StorageClass：显式 `ani-block`；不得依赖默认 StorageClass。

逻辑拓扑：

```text
current runner Job -> current Gateway -> current Auth/Dex
                    -> current-owned PostgreSQL/Redis
                    -> current deterministic fixture backend

target runner Job  -> target Gateway -> target ani-iam
                    -> target-owned Core DB/IAM DB/Redis/Dex/notification stub
                    -> target deterministic fixture backend
```

两条 lane 各自拥有 ServiceAccount、Secret、PKI、逻辑 seed 和 Credential。CF-01 只部署实际启动/smoke 需要的依赖；NATS 留到 DP2-12，不为“看起来完整”提前部署。NetworkPolicy 默认拒绝 ingress/egress，仅放行同 lane、CoreDNS 和必要 control-plane 路径；DNS 名可以解析，但必须用负向 probe 证明跨 lane TCP/UDP 连接失败，并用 manifest/Endpoint inventory 证明没有跨 lane reference。runner 在各自 namespace 内运行，Gateway 只暴露 ClusterIP；首版不创建 Ingress、NodePort、LoadBalancer 或真实流量入口。fixture backend 只隔离验证 Gateway authn/authz，不构成 Core journey 证据。

target 的 `direct-p2-isolated` profile 当前要求 IAM gRPC、PostgreSQL、Redis 和通知依赖使用 loopback。因此 CF-01 使用单 Pod 网络命名空间的 target Runtime Adapter，并把 Gateway、ani-iam 及必要 sidecar 组合在同一单副本 workload 中；这只是隔离验证 Adapter，不证明生产拓扑。不得为了搭环境修改 `internal/conf` 或降低 mTLS/TLS 约束。

## 6. 第一工作半天

时间盒最长四小时，顺序固定；可并行构建互不依赖的镜像，但不得越过失败门禁。

| 时间 | 工作 | 退出证据 |
| --- | --- | --- |
| 0:00–0:30 | 重验 commit/tree/overlay、cluster UID、两个 namespace 不存在；确认本机 Harbor login host 但不读取 Credential；创建 detached build context | `inputs.json` |
| 0:30–1:15 | 推送并按 digest 拉取 probe image；绑定两个固定 `ani-block` PVC；验证 default-deny、同 lane allow 与跨 lane L3/L4 deny | 三个 foundation gate |
| 1:15–2:30 | 并行构建/推送 current Gateway/Auth、target Gateway/ani-iam；runner 使用固定 probe image + task ConfigMap；记录 OCI digest | `artifacts.json` |
| 2:30–3:35 | 各 lane 部署最小依赖、migration、PKI、fixture backend 与应用；只要求最小启动路径健康/就绪 | workload inventory |
| 3:35–4:00 | 各 lane 执行一次固定 health/auth smoke，生成 JSON + Markdown，封存并完成 DP2-10 原子 handoff | run bundle + summary |

T+4:00 必须停止，不因为“再补一个功能”扩展窗口。未完成项按 `fail` 或 `not_verified` 记录，并把现场保留为可诊断状态；随后恢复 DP2-10，而不是在本事项实现 API Key、Envoy、Inference、Core/NATS 或 UI。

### 6.1 阻断型 foundation gates

以下任一失败都立即停止后续部署：

1. registry push 后，cluster 无法按 OCI digest 拉取 probe image；
2. `ani-block` PVC 无法绑定，或实际绑定到未声明的 StorageClass；
3. default-deny / 同 lane allow / 跨 lane L3/L4 deny 的实际 NetworkPolicy canary 不符合预期；DNS 可解析不构成失败；
4. source SHA/tree、image digest、manifest digest 或 cluster UID 与 plan 不一致；
5. cluster 不能直接 pull-by-digest，或需要读取、复制或记录本地 Docker Credential、既有共享 Secret，或需要复用 `ani-system`/其他 namespace 的数据库、Redis、NATS、Dex 或 Credential。

### 6.2 `environment-established` 最小 smoke

固定 scenario ID 为 `CF01-HUMAN-PASSWORD-PROTECTED-READ-V1`。两个 lane 都执行 `GET /readyz`，再按各自契约使用独立 logical seed 执行 `POST /auth/password/login`，最后携带该 lane 返回的 Access Token 执行只读 `GET /instances`。current/target response 的已接受差异进入 ledger；target 额外证明该 `listInstances` 请求恰好产生一次 `CheckPermission`。method/path/scenario 不得在运行时临时替换。

该 smoke 每个 lane 在半天 foundation 中至少运行一次；成功只证明最小可启动与真实 Gateway/Auth/IAM 连接。完整 DP2-05 suite（public 0 decision、无效登录 401、安全 Refresh Cookie、伪造 `x-ani-*` 清除、deny 403、IAM unavailable 503、blackhole timeout 504）作为环境建成后的第一组持续 journey，不再挤入四小时出口。未运行项保持 `not_verified`。

两条 lane 都必须证明没有跨 lane Endpoint、数据库、Redis、Principal 或 Credential 引用，跨 lane TCP/UDP 连接失败，且日志/report 不含 Password、Token、Cookie、Key、私钥或连接串。DNS 解析可以成功，不得把 DNS 解析结果当作隔离失败或通过。

## 7. 证据与判断规则

每次 run 至少生成：

- `plan.json`：所有输入 digest 与 difference ledger digest；
- `environment.json`：cluster/namespace UID、Kubernetes 版本、workload/Endpoint identity；
- `artifacts.json`：Git commit/tree、OCI repo/digest、Dockerfile/manifest digest；
- `results.json`：24 格 claim 与当前最小 canary 结果；
- `summary.md`：直接结论、失败/未验证、恢复状态和下一步；
- `sha256sums.txt`：上述文件的校验和。

证据不得包含 Secret 内容。Credential 只记录种类、lane、生命周期结果和不可逆指纹；current 与 target 从同一 logical seed 独立生成数据，禁止复制 raw Credential。

三层结论必须分开：

- `environment-established`：镜像、存储、隔离、部署和最小 canary 是否可重复；
- `journey fitness`：某一格的 current/target 事实；
- `Go/No-Go B`：只有 DP2-13/14 五类调用方、整组切入和部署级回退完成后才能给出。

## 8. 后续节奏，不改 Direct P2 主线

- CF-01 完成或到时后先停止环境写入并封存 evidence：`environment-established=pass` 时事项 01 设为 `resolved`，否则设为 `ready-for-human`。只要没有 Credential 泄露、跨 lane 泄露或仍在运行的变更动作，就只修改 DP2-10 事项状态/Comment，把它恢复为唯一 `claimed`；其 dirty recovery state 不被 CF-01 修改、暂存或构建。若发生上述安全事件，停止并报告，不执行 handoff。
- 每个 DP2 候选提交：运行 target-only 快速 journey，并更新相应格子。
- 每个人工接受的 ANI checkpoint：先跑 source projection gate，再跑 current/target 短套件；不得自动跟随 `main`。
- DP2-10 完成时补 C4/I4/A4/F4 的 API Key/Envoy 证据；DP2-11/12 分别补 Service Token/Inference 与 Lifecycle/NATS/Core；DP2-13 补齐五类调用方；DP2-14 才执行整组切入、回退和 Go/No-Go B。
- Artifact/config/seed/difference digest 变化时，只把受影响格子重置为 `not_verified`，不推翻未受影响的已固定证据。

## 9. 安全、恢复与删除

CF-01 不接共享流量，不改 ANI `main`，不推 IAM 业务提交，不切 selector，不失效 Credential，不删除旧 Auth，也不清理任何现有 namespace/PV/image。

所有新资源必须带 `cutover-fitness-id=CF-01` 与唯一 `run-id`。本地现有 Docker credential 只用于本地 push，不读取、打印、记录或复制进 cluster。用户已确认测试集群可以直接拉取五个 CF-01 repositories，因此本事项不创建 `imagePullSecret`；必须用新 `cf01-probe` digest 在每个 Ready node 实际验证直接拉取。任一节点 pull 失败即停止，不得降级为复制 Credential、创建 Secret、修改节点 registry 配置或扩大权限。

失败时先停止写流量并保留证据。删除两个 namespace、清理 `Retain` PV、删除 registry image 或失效任何 Credential 都是后续破坏性动作，执行前必须再次列出精确对象并取得人工确认；本规格不预授权删除。
