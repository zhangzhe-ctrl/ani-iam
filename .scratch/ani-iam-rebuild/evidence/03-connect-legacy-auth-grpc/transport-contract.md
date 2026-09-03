# gRPC transport contract

## 注册清单

| # | RPC | Full method |
| --- | --- | --- |
| 1 | Login | `/auth.v1.AuthService/Login` |
| 2 | PlatformPasswordLogin | `/auth.v1.AuthService/PlatformPasswordLogin` |
| 3 | BeginOIDCLogin | `/auth.v1.AuthService/BeginOIDCLogin` |
| 4 | CompleteOIDCLogin | `/auth.v1.AuthService/CompleteOIDCLogin` |
| 5 | RefreshToken | `/auth.v1.AuthService/RefreshToken` |
| 6 | RevokeToken | `/auth.v1.AuthService/RevokeToken` |
| 7 | ValidateToken | `/auth.v1.AuthService/ValidateToken` |
| 8 | ValidatePrincipal | `/auth.v1.AuthService/ValidatePrincipal` |
| 9 | IssueServiceToken | `/auth.v1.AuthService/IssueServiceToken` |
| 10 | CheckPermission | `/auth.v1.AuthService/CheckPermission` |
| 11 | CheckPermissionV2 | `/auth.v1.AuthService/CheckPermissionV2` |
| 12 | CreateAPIKey | `/auth.v1.AuthService/CreateAPIKey` |
| 13 | ListAPIKeys | `/auth.v1.AuthService/ListAPIKeys` |
| 14 | RevokeAPIKey | `/auth.v1.AuthService/RevokeAPIKey` |

`TestAllFrozenLegacyAuthRPCsAreRegisteredAndFailExplicitly` 通过真实 loopback gRPC client 逐一调用上述方法。每个方法均到达 compat handler，并返回以下稳定结果：

```text
code: Unimplemented
message: legacy Auth behavior is not implemented in CP0-1
google.rpc.ErrorInfo.domain: ani.auth.v1
google.rpc.ErrorInfo.reason: CP0_COMPAT_NOT_IMPLEMENTED
google.rpc.ErrorInfo.metadata.method: 对应 full method
```

这避免 generated `UnimplementedAuthServiceServer` 的框架默认消息成为外部契约，也避免未实现路径返回空响应或默认成功。

## 边界行为

| 场景 | 可观察结果 | 状态 |
| --- | --- | --- |
| 调用方主动取消已进入 handler 的请求 | `Canceled` | `pass` |
| 调用方 deadline 到期 | `DeadlineExceeded` | `pass` |
| Kratos server timeout 到期 | `DeadlineExceeded` | `pass` |
| `/auth.v1.AuthService/Unknown` | `Unimplemented` | `pass` |
| 已注册但未实现业务 | 稳定 `ErrorInfo`，无默认成功 | `pass` |

所有结果均从真实 loopback transport 观察，不是直接调用 Go handler。
