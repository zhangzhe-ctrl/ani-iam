# 当前调用方与运行边界

所有位置均相对固定 ANI commit `0cedae825a489d936cf41815dc27f278f6d3213c`。文件内容摘要受 [source-manifest.tsv](source-manifest.tsv) 的 Git blob 与 SHA-256 约束。

## Gateway

当前 Gateway 直接使用旧 `auth.v1.AuthService`。客户端位于 `repo/services/ani-gateway/internal/middleware/auth_client.go`，读取 `AUTH_SERVICE_ADDR`，缺省地址为 `127.0.0.1:9101`，使用 insecure gRPC 和约 2 秒 RPC timeout。Gateway Auth client 覆盖 14 RPC 中除 `IssueServiceToken` 外的 13 个；公开 Auth handler 位于 `repo/services/ani-gateway/internal/router/auth.go`。

当前链路为：

```text
ResolveAuthzPolicy
  -> AuthenticatePrincipal
  -> AuthorizePrincipal
  -> RateLimit
  -> Idempotency
  -> Audit
```

| 当前路由类型 | 当前行为 | 目标行为 | 影响 |
| --- | --- | --- | --- |
| Public | `ResolveAuthzPolicy` 后跳过 Auth client | 0 次 IAM 调用 | DP2-02、DP2-05 |
| legacy protected | `ValidateToken` 后再以 URL/HTTP 推导 `{resource, action}` 调 `CheckPermission` | 一次目标 `CheckPermission(raw credential, operation_id, policy_revision, attributes)` | DP2-02、DP2-03、DP2-05 |
| generated authenticated/authorized | `ValidatePrincipal` 后，authorized 再调 `CheckPermissionV2` | authenticated-only 一次 `ValidatePrincipal`；authorized 一次 `CheckPermission`，不能预先 Validate | DP2-02、DP2-03、DP2-05 |
| sandbox token | Gateway 本地识别，0 次旧 Auth 调用 | 必须由 DP2-02 固定 operation/authn 分类；不得成为默认 allow | DP2-02 |

固定源码的具体不兼容点：

- `ANI_AUTH_MODE=dev` 在 `middleware/auth.go` 和 `middleware/rbac.go` 构造默认 Tenant/User、`tenant-admin` role 并绕过旧 Auth；generated policy 还会回落到 legacy。目标不得保留 mode/fallback/default allow。
- 旧 Gateway 同时接受 `Authorization: Bearer` 和 `X-API-Key`。目标 API Key 只接受 Bearer。
- 旧 legacy 授权从 path 和 HTTP method 推导权限字符串。目标只消费 DP2-02 生成的 immutable registry。
- 固定 Gateway 没有一条覆盖全部请求的“先删除所有客户端 `x-ani-*`，allow 后再注入最小可信上下文”证明。目标必须测试伪造 Header 删除与可信上下文注入。
- generated path 已有较稳定的 401/403/429/503/504 映射；legacy 依赖错误会坍缩为 401 或 403。目标必须统一稳定 ErrorResponse，IAM deadline 为 500 ms 且不重试。
- 注册 Core route 缺 policy 时返回 `503 AUTHZ_POLICY_MISSING`；未匹配 route 可继续由路由层产生 404。DP2-02 必须把缺 annotation、未知 operation 和 revision mismatch 明确 fail closed。
- 当前 Gateway 仍组合业务 DB/runtime/services。目标 Gateway 是薄边缘；DP2-05 只连接一个固定受保护 operation，不声称已完成全局清理。

目标边界固定为 Public 0 次、authenticated-only 1 次、authorized 1 次；任何受保护 operation 两次 IAM decision 都是 blocker。

## Envoy authz adapter

调用面位于：

- `repo/services/envoy-authz-adapter/internal/config/config.go`
- `repo/services/envoy-authz-adapter/internal/authclient/client.go`
- `repo/services/envoy-authz-adapter/internal/extauth/server.go`
- `repo/services/envoy-authz-adapter/main.go`

它要求 `AUTH_SERVICE_GRPC_ADDR`，使用 insecure gRPC 和缺省约 2 秒 timeout，只接受 `Authorization: Bearer ani_...`，每次调用旧 `ValidateToken`。它从 Envoy context extensions 读取 `ani.target_tenant_id` 与 `ani.inference_service_id`；缺失扩展返回 503。无效/缺失/Unauthenticated 映射 401，ResourceExhausted 映射 429，其他依赖错误映射 503，Tenant 不匹配映射 404。allow 后会移除 `authorization`、`x-api-key`、`x-ani-tenant-id`、`x-ani-user-id`。

目标 owner 是 `AuthorizationService.CheckPermission` 与 Envoy adapter 自身的 typed target obligation。DP2-13 才完成 Envoy 全面对等；DP2-05 不以 Gateway E2E 代替 Envoy 独立结果。

## Inference service

调用面位于：

- `repo/services/inference-service/internal/config/config.go`
- `repo/services/inference-service/internal/runtime/coresdk/minter.go`
- `repo/services/inference-service/internal/runtime/coresdk/adapter.go`
- `repo/services/inference-service/main.go`

当前配置读取 `AUTH_SERVICE_GRPC_ADDR`、`AUTH_SERVICE_MINT_SECRET`，并保留 `CORE_SERVICE_TOKEN` 静态 fallback。Minter 使用旧 `IssueServiceToken`，固定 caller `inference-service`、scope `scope:platform-workloads:write`、TTL 300 秒，并按 Tenant 缓存，刷新窗口 30 秒。若未建立 minter，就使用静态 Core token；若 token 不是 JWT，adapter 注入 `X-Dev-Tenant-ID`、`X-Dev-Principal-Kind=service`、`X-Dev-Service-Scope=scope:platform-workloads:write`。

目标必须以 allowlisted mTLS/SPIFFE workload 换取最长 5 分钟、audience-bound、不可 refresh 的 Service Token；删除共享 mint secret、静态 fallback 和 dev header。该调用面属于 DP2-11/DP2-13，DP2-05 明确不实现或验证。

## Console

调用面位于 `repo/frontends/console/src/api/auth.ts`、`src/auth/session.ts`、登录/OIDC callback 与 API Key 页面。Console 只经 Gateway 调用：

- `POST /auth/password/login`，带 Tenant 名称；
- `POST /auth/oidc/begin` 与 `POST /auth/token`；
- `POST /auth/refresh`，JSON body 含 `refresh_token`；
- `POST /auth/logout`，使用 Access JWT 的 `jti`；
- API Key list/create/revoke。

旧客户端根据 `remember_me` 把 access token 和 refresh token 一起放入 `localStorage` 或 `sessionStorage`，在 Access Token 剩余约 5 分钟时自动刷新；旧服务端重用同一 refresh token。目标必须把 Access Token 限于内存，把 Refresh Token 限于 Console 专用 Secure/HttpOnly/SameSite Cookie，使用 Origin/Referer + CSRF、单次轮换与约 1 分钟 single-flight。该全面切换属于 DP2-08/DP2-13，DP2-05 只验证非浏览器 tracer bullet。

## BOSS

调用面位于 `repo/frontends/boss/src/api/auth.ts`、`src/auth/session.ts`、`src/auth/permissions.ts` 与登录/OIDC callback。BOSS 只经 Gateway 调用 platform password、OIDC、refresh 和 logout；token 保存与刷新方式同 Console。

旧 BOSS 从 Access JWT 读取 roles。`canWritePlatform` 在没有 role claim 时默认允许，再依赖后端 403 兜底。这是目标禁止的前端默认 allow；目标 BOSS 必须要求 active Platform Membership，且前端 role 只能用于展示，不能成为授权权威。Console/BOSS 还必须使用不同 Cookie 名、Path、Audience 与 Session 时限。

## 调用方验证边界

| 调用方 | 固定对象静态盘点 | 本事项真实 E2E | 后续 gate |
| --- | --- | --- | --- |
| Gateway | `pass` | `not_verified` | DP2-02 registry；DP2-05 单路由；DP2-13 全面 |
| Envoy | `pass` | `not_verified` | DP2-10、DP2-13 |
| Inference | `pass` | `not_verified` | DP2-11、DP2-13 |
| Console | `pass` | `not_verified` | DP2-08、DP2-13 |
| BOSS | `pass` | `not_verified` | DP2-08、DP2-13 |

静态盘点不能替代任何后续真实依赖或 E2E 结果。
