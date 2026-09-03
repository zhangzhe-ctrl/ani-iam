# 旧 Auth 兼容性 Oracle

所有路径均相对于冻结 ANI `repo/`，所有行号应在 `<SHA>` 的 blob 中解释，不跟随工作树漂移。

## 14 RPC

权威源：`api/proto/auth/v1/auth_service.proto`；生成交叉检查：`pkg/generated/pb/auth/v1/auth_service_grpc.pb.go`。

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

Proto 与 checked-in generated full-method 常量数量和名称一致，状态 `pass`。由于无 `buf`/`protoc`，独立生成 FileDescriptorSet 为 `not_verified`；CP0-1 不得用本文件冒充 descriptor rebuild。

## Credential 与 Token 语义

- Password：`services/auth-service/internal/service/password_login.go` 使用 `bcrypt.CompareHashAndPassword`；存储列为 `users.password_hash`。测试使用 MinCost 只是测试夹具，不代表生产 hash cost 的额外承诺。
- Access JWT：仅接受 RS256；验证 signature、`exp`、可选 `nbf`、配置 issuer 和 JTI blocklist。旧 projection 包含 `tid`、`uid`、`scope`、`roles`；V2 additive 字段包含 `principal_kind`、`credential_domain`、`permissions`、`aud`。
- Access Token TTL：1 小时。
- Service Token：caller 固定为 `inference-service`，共享 mint credential 采用 constant-time compare；默认 5 分钟、上限 1 小时，audience `ani-core`。
- Refresh：32 字节随机值，`ani_refresh_` 前缀，数据库保存 SHA-256；有效期 7 天。成功使用只更新 `last_used_at` 并签发新 Access Token，不旋转 Refresh Token。
- Revoke：JTI 同时写 PostgreSQL `jwt_blocklist` 与 Redis，Cache TTL 随撤销 Token TTL。
- API Key：`ani_<env>_<tenant_uuid>_<32-byte-secret>`，数据库只保存 SHA-256 与 24 字符显示前缀；默认 60 RPM、上限 10000、expiration 可空。Key 从自身嵌入的 Tenant 解析作用域。

## PostgreSQL / role / RLS

Migration head（按冻结 tree）：`deploy/migrations/20260831_001_async_tasks_rls_fix.sql`；`atlas.sum` 摘要见 `baseline.md`。

旧 Auth 关键表源于 `20260501000100_init_schema.sql`：

- `users(tenant_id, username, email, password_hash, status)`；同 Tenant username/email unique。
- `roles(tenant_id nullable, name, permissions JSONB)`；NULL Tenant 表示平台角色。
- `user_roles(user_id, role_id)`；多角色连接表。
- `api_keys(tenant_id, user_id nullable, key_hash, scopes, rate_limit_rpm, expires_at, revoked_at)`。
- `jwt_blocklist(jti, expires_at, revoked_at)`。
- `refresh_tokens(tenant_id, user_id, token_hash, roles, expires_at, last_used_at, revoked_at)`；后续 migration 允许平台 Token 的 `tenant_id IS NULL`。

角色：`ani_app` 为 NOLOGIN/NOSUPERUSER/NOBYPASSRLS；`ani_app_user` 为 LOGIN/NOSUPERUSER/NOBYPASSRLS 并继承 `ani_app`。Migration owner 与普通 runtime role 分离。`20260828000200_app_role_privileges.sql` 显式授予 Auth 表所需 SELECT/INSERT/UPDATE。

RLS：`api_keys` 与 `refresh_tokens` 启用并 FORCE RLS。Tenant 行依赖 `app.current_tenant_id`；平台 Refresh 行以 `tenant_id IS NULL` 放行。租户 Password/OIDC/API Key 写路径在事务内设置 DB Tenant；平台登录不设置 Tenant。

静态 schema/role/policy 为 `pass`；真实空库 replay、runtime role 属性、跨 Tenant deny、平台 Refresh 与数据库副作用均为 `not_verified`。

## Redis

适配器为 `redis/go-redis/v9`，提供 Get/Set/SetNX/Delete/Increment/Exists；Increment 用 TxPipeline 同时 INCR 与 EXPIRE。

| Key pattern | Value/behavior | TTL |
| --- | --- | --- |
| `oidc:state:<random>` | JSON tenant_name、redirect_uri、nonce；Complete 后删除 | 10 分钟 |
| `jwt:blocklist:<jti>` | `revoked`；DB miss fallback 后可回填 | 剩余 Token TTL，默认 1 小时 |
| `api-key:rate:<sha256(raw-key)>` | 计数器 | 1 分钟 |

代码与单元测试为 `pass`；真实 Redis key、TTL 精度、故障语义为 `not_verified`。

## Dex / OIDC

固定 dev 配置：issuer `http://127.0.0.1:5556/dex`，memory storage，client `ani-console`，五个 callback（localhost/127.0.0.1 3000、8080 与 `https://console.example.test/callback`），一个静态开发身份。

Auth 从 issuer 推导 `/auth`、`/token`、`/keys`；state/nonce 使用 32 字节随机值；state 单次消费；ID Token 检查 RS256、issuer、audience、time、nonce 和至少 2048-bit RSA key。当前 callback 校验只要求 absolute HTTP(S) 且无 fragment，精确 allowlist 依赖 Dex client。

固定配置与单元测试为 `pass`；真实 discovery/token/JWKS/callback 往返为 `not_verified`。
