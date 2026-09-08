# CUTOVER-FITNESS-01 environment preflight — 2026-09-08

## Outcome

`pass`：环境容量、权限和基础能力足以开始一个隔离双轨 foundation。

该 `pass` 只表示可以领取搭建事项，不表示双轨已部署、target 可切换或 Go/No-Go B 通过。本次仅执行只读命令；未创建/修改/删除 cluster 或 registry 对象，未读取 Secret/config 内容，也未修改任何 ANI checkout。

## Immutable source probe

| Input | Commit | Tree | Result |
| --- | --- | --- | --- |
| current ANI remote main | `56a5f0b493c8404a024a92647d93f2ba2f7daf35` | `4ba6a15ad0cddf0db66a25d695b082d47346aff1` | `pass` |
| target ANI base | `56a5f0b493c8404a024a92647d93f2ba2f7daf35` | `4ba6a15ad0cddf0db66a25d695b082d47346aff1` | `pass` |
| target overlay source/result | `804db51a5f93605f9bbd4ac407f0489ecb1d187c` | `28eb0508c93aab28e86e044a88ada9e19bf16830` | `pass` |
| target ani-iam product | `a56a332834967603eb47a3824727982032bc4f5e` | `ef84bfb6382eaa73f13a68754f506edeb17f2206` | `pass` |

`804db51...` 是 `56a5f0b...` 的直接父。target 仍从 latest main `56a5f0b...` 开始，并应用两 tree 之间的 deterministic reverse-containment overlay；overlay SHA-256 为 `542084f3e06be1d454cd99eb0114a76b8e06a52f9cbf5a9d661e749fb678edc2`，大小 2,331,603 bytes，覆盖 80 paths，预期 projected tree 精确等于 `28eb0508...`。实际 apply/tree equality 是搭建事项的 source gate，当前为 `not_verified`；不得直接跳过 base 使用父 archive。ani-iam 当前 HEAD `49a0a023a3ae3bf228a0246051bf2115b8e3ccfd` 相对 `a56a332...` 只含 `.scratch` 协调文档；当前未提交 DP2-10 文件不属于任何构建输入。

## Cluster and host probe

| Claim | Evidence summary | Result |
| --- | --- | --- |
| SSH path | `/home/chabking/.ssh/config` 的 host alias `ani` 可 BatchMode 连接 | `pass` |
| Cluster identity | context `kubernetes-admin@kubernetes`; UID `f5cafbf1-5246-4f8f-b9da-742182f37528`; client/server v1.36.1 | `pass` |
| Node state | 3/3 nodes Ready；无 Memory/Disk/PID pressure | `pass` |
| Capacity | allocatable 合计约 512 CPU / 1.72 TiB；现有 requests 约 51.7 CPU / 126 GiB | `pass` |
| Live utilization | Metrics API 不可用，无法读取实时 CPU/RAM 使用量 | `not_verified` |
| Namespaces | `ani-cutover-current` 与 `ani-cutover-target` 均不存在 | `pass` |
| RBAC | 对 namespace/deployment/statefulset/job/service/secret/configmap/networkpolicy/PVC 的 create/delete/get/list/watch 均允许 | `pass` |
| DNS/CNI | CoreDNS 2 pods Ready；Kube-OVN v1.15.8 与 Multus 3/3 Ready | `pass` |
| NetworkPolicy enforcement | API/已有策略存在，但新 namespace 的 actual allow/deny 尚未跑 canary | `not_verified` |
| Ingress | 无 IngressClass；Gateway API 可用 | `not_verified` / 不作为 CF-01 前置 |

## Registry probe

- 本地 Docker engine 可用；本地 Docker 配置只读确认已包含 `docker.changqingyun.cn` 的 auth host，未打印 Credential。
- 本地与 cluster node 到 `https://docker.changqingyun.cn/v2/` 均得到预期私有 registry `401` challenge，DNS/TLS/网络为 `pass`。
- 实际 push 和 cluster direct pull-by-digest 尚未执行，结果为 `not_verified`。
- 用户已确认本机 Harbor 登录可用于五个固定 CF-01 repositories 的本地 push，并确认 cluster 可直接拉取；不得读取、打印、记录或复制该 credential，两个 namespace 不创建 `imagePullSecret`。新 probe digest 必须在每个 Ready node 实际拉取，任一失败即停止且不得降级绕过。
- cluster 另有 5 个与本事项无关的 `ImagePullBackOff` Pod，因此 image pull canary 必须排在第一位，不能根据网络 `401` 推断拉取成功。
- ANI 现有 image workflow 固定为另一 registry；CF-01 不修改 workflow，首版从本地精确 build context 构建/推送。

## Storage probe

- 可用 StorageClass：`ani-block`、`ani-rbd-ssd`、`cephfs`、`nfs`；没有默认 StorageClass。
- Rook CephCluster 为 Ready/Created，但 health 为 `HEALTH_WARN`；cluster 已有若干与本事项无关的 Pending PVC。
- `ani-block` 支持扩容、WaitForFirstConsumer，reclaim policy 为 `Retain`。
- 结论：显式使用 `storageClassName: ani-block`，先跑 PVC bind/read-write canary。真实 bind 是 `not_verified`；后续 Retain PV 清理需要单独精确人工确认。

## Existing runtime assets

- ANI current tree有 Gateway/Auth Dockerfile、Auth/Dex production-shaped manifest 和 JSON live-gate 形式，可作为实现素材；历史 evidence 不计入新环境结果。
- ani-iam 有真实 PostgreSQL/Redis/mTLS/Gateway process integration 素材和 `/healthz`、`/readyz`、`/metrics`，但没有 Dockerfile、Kubernetes manifest、migration image/job、standalone runner 或统一 report schema。
- ani-iam `direct-p2-isolated` profile 的 loopback 约束要求首版 target 使用同 Pod network namespace Adapter；不得为搭环境降低该约束。
- 已有 namespace 中的 PostgreSQL、Redis、NATS、Dex 不得复用；它们只证明相应 image/runtime 在 cluster 存在。

## Risks that remain `not_verified`

1. 私有 registry 的新 probe digest 能否在所有 Ready node 不使用 imagePullSecret 直接拉取。
2. `ani-block` 在 Ceph `HEALTH_WARN` 下能否及时 bind 和稳定读写。
3. 新 namespace 的 default-deny、同 lane allow、跨 lane L3/L4 deny 是否实际生效；DNS 仍可正常解析。
4. current Gateway/Auth 完整启动所需 Core migrations/config 是否能在时间盒内重放。
5. target composite runtime 的 migration ordering、mTLS/PKI、Dex/notification stub 和 loopback wiring。
6. 503/504 fault injection、完整 rollback、五类调用方、HA/容量与生产流量。

## Frozen consequence

环境适合实施 `.scratch/cutover-fitness-01/issues/01-establish-dual-lane.md`。用户已确认本机 Harbor push 与 cluster direct pull，本事项已解除 `needs-info` 并在固定 source/tree/overlay、目标 namespaces、五个 repositories 和四小时时间盒内领取。半天只以 `environment-established` 为出口；不完成或冒充 DP2-14。时间盒结束并封存证据后，如无安全事件，则按本轮用户授权恢复 DP2-10 的唯一 `claimed` 状态，同时保持其 dirty recovery state 不变。
