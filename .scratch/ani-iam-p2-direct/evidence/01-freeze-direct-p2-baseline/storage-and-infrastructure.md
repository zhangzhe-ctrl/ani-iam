# PostgreSQL、Redis、OIDC 与部署盘点

所有 ANI 路径均绑定固定 commit `0cedae825a489d936cf41815dc27f278f6d3213c`；关键文件 hash 见 [source-manifest.tsv](source-manifest.tsv)。本文件只陈述旧状态及目标替换边界，不把旧 RLS 作为可运行 Oracle。

## PostgreSQL migration 与 checksum

固定 `repo/deploy/migrations` 含 38 个 SQL migration；完整 Git blob/hash/byte/Atlas覆盖清单见 [migration-files.tsv](migration-files.tsv)。`repo/deploy/migrations/atlas.sum` 的 SHA-256 是 `175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af`，但只列出 31 个 SQL 文件。以下 7 个固定对象中的 migration 没有进入该 checksum 文件：

- `20260821_001_tenant_admin_invitation.sql`
- `20260825_001_tenant_admin_invitation_pending_unique.sql`
- `20260827_001_async_tasks_list_index.sql`
- `20260827_001_user_roles_single_role.sql`
- `20260828_001_instance_resource_rls_fix.sql`
- `20260831_001_async_tasks_rls_fix.sql`
- `20260901_001_gpu_chain_remaining_rls_fix.sql`

因此，旧目录的“完整 checksum gate”结果为 `fail`。DP2-04 必须从新目标 migration 目录生成完整 `atlas.sum`，从空库重放，并验证重新生成无漂移；不得沿用旧 checksum 作为目标基础。

## 旧 role 与权限模型

`repo/deploy/migrations/20260501000100_init_schema.sql` 和后续 hardening/grant migration 定义了共享 ANI 数据库的旧角色：

| Role | 固定对象中的性质 | 目标处理 |
| --- | --- | --- |
| `ani_app` | NOLOGIN group role，获得广泛表 DML | 最终删除 IAM 对该共享角色的依赖 |
| `ani_app_user` | LOGIN；hardening 后 NOSUPERUSER、NOCREATEDB、NOCREATEROLE、NOBYPASSRLS、INHERIT | 不作为目标 IAM runtime role；DP2-04 创建独立受限 role |
| `ani_migrator` | migration group role | 不复用；DP2-04 分离独立 migration role |
| `ani_outbox_publisher` | NOLOGIN、BYPASSRLS | 禁止用于任何目标 IAM 业务查询 |
| `ani_metering_writer` | NOLOGIN、BYPASSRLS | 禁止用于任何目标 IAM 业务查询 |

`20260828000200_app_role_privileges.sql` 为旧 `ani_app` 组授予跨大量共享业务表的 DML。这不满足目标独立 IAM 数据库、仅目标 DML grant、runtime 非 owner/非 superuser/无 BYPASSRLS 的边界。

## 旧 Auth 表与 RLS

固定初始 schema 包含旧 `users`、`roles`、`user_roles`、`api_keys`、`jwt_blocklist`、`refresh_tokens` 等 Auth 状态，并与 Core/Services 业务表共库。关键漂移包括：

- `users` 最初 Tenant-owned，后续 `20260707001400_platform_users.sql` 将 `tenant_id` 改为 nullable，以 NULL 表示 Platform user；
- `refresh_tokens` 后续允许 nullable `tenant_id` 表示 Platform refresh；
- `20260827000200_users_display_name_soft_delete.sql` 为用户加入通用 soft-delete 字段；
- `20260827_001_user_roles_single_role.sql` 把旧用户角色收窄为单角色；
- `api_keys` 与 `refresh_tokens` 执行 `ENABLE/FORCE ROW LEVEL SECURITY`，旧 `tenant_isolation` policy 为 `AS RESTRICTIVE`。

这些形状与目标的全局 Human Principal、独立 Platform/Tenant Membership、多个 Role Binding、显式安全生命周期、无 `jwt_blocklist`、无通用 soft delete 不兼容。目标数据库从空库创建，不能共享或复制这些旧表。

固定对象仍保留导致历史事项04正向访问失败的 Auth RLS 结构。当前固定 object 的静态来源一致性为 `pass`；本事项没有在当前 object 上重新运行 live PostgreSQL，因此 live 结果为 `not_verified`。历史真实依赖结论保持 `FAIL / BLOCKED`，详见 [historical-rls-negative.md](historical-rls-negative.md)。

## Redis key、TTL 与状态

旧 Auth Redis 客户端由 `repo/services/auth-service/internal/service` 使用：

| Key | 值/状态 | TTL | 失败边界 | 目标处理 |
| --- | --- | --- | --- | --- |
| `oidc:state:<state>` | JSON，含 redirect URI、nonce 和 OIDC 临时状态 | 10 分钟 | 读取后删除；Redis 不可用返回依赖错误 | DP2-07 由目标单次 state/nonce/PKCE 临时状态替换 |
| `jwt:blocklist:<jti>` | `revoked` | Access Token 剩余寿命；缺省约 1 小时 | revoke 先写 PostgreSQL，后写 Redis，旧路径存在部分完成风险 | 最终删除；目标用 Session Grant/version |
| `api-key:rate:<sha256(raw key)>` | 递增 counter | 1 分钟 | cache 为 nil 时旧实现禁用 rate limit；Redis 错误返回失败 | DP2-10 由目标 credential abuse gate 替换 |

旧 Refresh Token 不在 Redis：原始 token 为 `ani_refresh_` 加 32-byte 随机值，PostgreSQL 只存 SHA-256；默认约 7 天，Validate 更新 `last_used_at`，不轮换且可重复使用。目标用 Session/Grant/Family/single-use Refresh，不复用旧表或语义。

## Dex / OIDC

旧 OIDC 位于 `repo/services/auth-service/internal/service/oidc.go` 与 `oidc_sessions.go`：

- state 和 nonce 使用 32-byte 随机值，state 10 分钟单次消费；
- JWKS cache 5 分钟、HTTP timeout 10 秒、clock skew 2 分钟、RSA key 至少 2048 bit；
- 可从 issuer 推导 authorization/token/JWKS endpoint；
- callback 校验 state 保存的 redirect URI，并允许绝对 HTTP/HTTPS、无 fragment 的 redirect；精确 allowlist 由部署 Dex client 承担；
- OIDC completion 在一个 PostgreSQL transaction 内 upsert 旧 user/role/refresh 状态。

`repo/deploy/docker/config/dex-dev.yaml` 使用 Dex v2.40.0、in-memory storage、静态 `ani-console` client 和测试 identity。固定配置包含开发用静态 credential；本证据不复制其 secret。`repo/deploy/real-k8s-lab/sprint13-production-auth-dex.yaml` 仍使用 Dex v2.40.0 与 in-memory storage，不能作为 production-ready 证据。

目标仍保留可信 OIDC provider 语义，但由 DP2-07 以 Authorization Code + PKCE S256、exact redirect、verified email 和显式 Identity Link 替换旧实现。DP2-05 明确不实现 OIDC。

## 地址、配置、构建和部署引用

固定旧服务地址为 `ani-auth-service.ani-system.svc.cluster.local:9101`。直接调用方配置如下：

| Consumer/Process | 旧变量 | 旧用途 | 目标处理 |
| --- | --- | --- | --- |
| Gateway | `AUTH_SERVICE_ADDR` | 旧 Auth gRPC；本地缺省 `127.0.0.1:9101` | 目标 IAM 地址和 mTLS/SPIFFE client；删除旧变量 |
| Envoy adapter | `AUTH_SERVICE_GRPC_ADDR` | 旧 ValidateToken | 目标 AuthorizationService；删除旧变量 |
| Inference | `AUTH_SERVICE_GRPC_ADDR` | 旧 IssueServiceToken | 目标 workload Service Token；删除旧变量 |
| Inference | `AUTH_SERVICE_MINT_SECRET` | 共享 mint secret | 最终删除 |
| Inference | `CORE_SERVICE_TOKEN` | 静态 fallback | 最终删除 fallback |
| Auth service | `DATABASE_URL`, `REDIS_URL`, `GRPC_PORT=9101`, `HEALTH_PORT=9201` | 旧 runtime | 由独立 IAM typed config/secret source 替换 |
| Auth service | `AUTH_JWT_*`, `AUTH_OIDC_*`, `AUTH_SERVICE_MINT_CREDENTIALS` | signing/OIDC/shared mint 配置 | 由目标 KMS/Secret/OIDC/mTLS 配置替换；旧变量最终删除 |

需要后续 zero-reference 的固定位置包括：

- `repo/deploy/real-k8s-lab/sprint13-production-auth-dex.yaml`
- `repo/deploy/real-k8s-lab/sprint13-production-shaped-gateway-deployment.yaml`
- `repo/deploy/real-k8s-lab/inference-envoy-ai-gateway-c40.yaml`
- `repo/deploy/real-k8s-lab/inference-incluster-e2e.yaml`
- `repo/deploy/docker/docker-compose.yml`
- `repo/Makefile` 中 auth-service build/image target
- `repo/go.work` 中 auth-service module
- `repo/installer/ani-installer/profiles/{baremetal,existing-k8s,vm}.yaml`

这些 runtime/build/config/deploy 引用只有在 replacement gate 和最终 DP2-19 人工删除检查点后才能删除。历史 ADR、development record 与 evidence 中的文字引用可以保留，并在 zero-reference 扫描中明确排除。

## 结果汇总

| 项目 | 结果 |
| --- | --- |
| 固定 migration/role/RLS/Redis/Dex/config 静态盘点 | `pass` |
| 旧 migration 目录完整 checksum | `fail` |
| 历史旧 RLS 受限 runtime role 正向访问 | `fail` |
| 当前固定 object 的 live PostgreSQL/Redis/Dex | `not_verified` |
| 目标无 RLS 独立数据库与受限 role | `not_verified` |
