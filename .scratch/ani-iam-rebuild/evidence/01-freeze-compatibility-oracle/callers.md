# 三调用方 Oracle

## Gateway

- 地址：`AUTH_SERVICE_ADDR`，缺省 `auth-service:8081`；gRPC 调用有界 deadline。
- Legacy：Bearer/API Key 经 `ValidateToken`，RBAC 经 `CheckPermission`。
- Generated：先 `ValidatePrincipal`，后 `CheckPermissionV2`；当前是两次调用，不是 P2 目标 one-call。
- 错误映射：Unauthenticated→401、PermissionDenied→403、ResourceExhausted→429、DeadlineExceeded→504、Unavailable/FailedPrecondition→503。
- 当前生成 registry 从 `api/openapi/v1.yaml` 生成；未知 Core route fail closed。

## Envoy authz adapter

- 地址：`AUTH_SERVICE_GRPC_ADDR`；默认 Auth timeout 2 秒。
- 只从 Authorization Bearer 读取 `ani_` API Key，然后调用一次 `ValidateToken`。
- 缺 target context→503；缺/坏 Credential 或 Unauthenticated→401；ResourceExhausted→429；其他依赖错误→503；Tenant mismatch→404；成功时移除 credential/identity headers。

## Inference Service

- 地址：`AUTH_SERVICE_GRPC_ADDR`，credential 为 `AUTH_SERVICE_MINT_SECRET`。
- 通过 `IssueServiceToken` 为每个 Tenant 获取 audience-bound Core Token：caller `inference-service`、scope `scope:platform-workloads:write`、TTL 300 秒，提前 30 秒刷新并进程内缓存。
- 当前 gRPC 使用 insecure transport 且共享 mint secret；这是冻结 Oracle，不是 P2 目标。
- 随后以 Bearer Token 调 Core `/platform-workloads*`；旧 dev fallback 仍可注入 `X-Dev-*` headers。

## 冻结快照验证

所有测试都在 `git archive 963bc8…` 的 `/tmp` 快照中运行，`GOPROXY=off`，测试后删除快照和 Go build cache。

```text
ok auth-service/internal/service
ok ani-gateway/internal/authz
ok ani-gateway/internal/middleware
ok envoy-authz-adapter/internal/authclient
ok envoy-authz-adapter/internal/extauth
ok inference-service/internal/runtime/coresdk
```

首次 Inference 测试因沙箱禁止 `httptest` 监听本机端口而失败；以只允许本机临时 listener 的权限复跑后通过。这不是代码失败。

这些结果仅为 static/unit `pass`。没有运行中的 PostgreSQL、Redis、Dex、Auth、Gateway、Envoy 或 Inference 容器，因此三调用方的 valid credential、success、401、403、503、timeout、policy mismatch 与副作用 live E2E 均为 `not_verified`，必须留给 CP0-5。
