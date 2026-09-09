# CF-01 事项 01 环境恢复结果

- Run ID：`20260909t013439z`
- 结果：`environment-established=pass`
- 范围：只恢复 current/target 最小运行环境并执行各一次固定 HTTP 链路；未进入 DP2-10 对比。

## 固定输入与镜像

- current ANI：`56a5f0b493c8404a024a92647d93f2ba2f7daf35`，tree `4ba6a15ad0cddf0db66a25d695b082d47346aff1`
- target Gateway ANI：`4ff73e09c16df706af2aacdff6d763ed4aeaa873`，tree `bc0ddb3ba1a449bc30b7815f9047aeb4390e8eff`
- target IAM product：`78c5265bcd9eddb97ed7525fe4756db03abc5c50`，tree `2ec15ab7d51dcc8d66c60f5d5678b034c3721f00`
- `fb862d75854e2a31d60ac5a40182dc678ae88c44` 仅包含 `.scratch` 关闭证据，未被当作产品基线。
- harness commit：`13e709f4359b81f51444b9875c430b6ba98d00ec`，tree `73d3987561b2a6eea8589187c493bf03538eb499`。
- target Gateway/IAM policy revision 均为 `sha256:1d5c80b83635e9a152c0edd9e8d1c9b66f5f8962e84cdd4f4701ecc486dd969c`。
- 所有运行镜像均按 digest 固定，完整引用见 `artifacts.json`。

## 部署前只读复核

- cluster UID 仍为 `f5cafbf1-5246-4f8f-b9da-742182f37528`。
- `ani-cutover-current` 与 `ani-cutover-target` namespace UID 与既有记录一致。
- 两个 PVC/PV UID、StorageClass、容量和 Bound 状态与既有记录一致。
- 两个 storage Job 均 `active=0`、`succeeded=1`；唯一挂载 PVC 的旧 Pod 均为 `Succeeded`。
- 既有 Running echo/network/registry-canary Pod 不挂载 PVC；没有未知对象或活动数据写入。
- registry canary 的 probe digest `sha256:36ff3463...` 是上一轮已知的 CF-01 自有变更，不是未知漂移。
- 结论：既有 namespace/PVC 可复用，预检 `pass`。

## 最小运行环境

- current：一个 task-owned Deployment，包含 PostgreSQL、Redis、NATS、固定 Auth、固定 Gateway 与只读 smoke probe；只挂载 current PVC。
- target：一个 task-owned Deployment，包含 PostgreSQL、Redis、loopback OIDC discovery、固定 IAM、固定 Gateway 与只读 smoke probe；只挂载 target PVC。
- 两边都没有创建 Service、Ingress、NodePort、LoadBalancer 或 imagePullSecret；所有链路都在各自 Pod 的 loopback 内完成。
- 最终 current Pod `6/6 Ready`、Deployment `available=1`；target Pod `6/6 Ready`、Deployment `available=1`。
- target IAM 最终 Pod 记录一次启动重试，原因是 OIDC sidecar 尚未监听；sidecar 就绪后 IAM 正常启动。此前还修正了 Secret 文件尾换行和 Kubernetes `POD_UID` 注入。这些均为 task-owned 环境装配修正，没有修改 ANI/IAM 产品代码。

## 固定链路与授权计数

每 lane 仅执行一次：`GET /readyz → POST /api/v1/auth/password/login → GET /api/v1/instances`。

- current：`200 → 200 → 200`，取得 access token，结果 `pass`；数据库中产生 1 条 refresh token。
- target：`200 → 200 → 200`，取得 access token，结果 `pass`；数据库中产生 1 个 Session、1 个 Session Grant、1 条 IAM login Audit。
- target IAM Prometheus `server_requests_code_total{operation="/iam.v1.AuthorizationService/CheckPermission",code="200"}`：调用前无样本（按 0 计），调用后为 `1`，delta 恰好为 `1`。

## 安全与停止边界

- 运行密码、密码 Hash、OIDC secret、JWT/Ed25519/mTLS 私钥均仅存在于 task-owned 临时目录和 namespace Secret；没有写入仓库或证据。
- repository secret scan：`pass`。
- 未读取、打印或复制 Docker Credential；未创建 imagePullSecret。
- 没有删除 namespace/PVC/PV/镜像，也没有触发 cutover 或 DP2-10 对比。
