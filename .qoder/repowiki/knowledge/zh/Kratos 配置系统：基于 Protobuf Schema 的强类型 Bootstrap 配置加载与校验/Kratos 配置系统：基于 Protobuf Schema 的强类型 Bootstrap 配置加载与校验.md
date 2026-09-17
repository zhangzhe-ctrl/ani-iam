---
kind: configuration_system
name: Kratos 配置系统：基于 Protobuf Schema 的强类型 Bootstrap 配置加载与校验
category: configuration_system
scope:
    - '**'
source_files:
    - internal/conf/conf.proto
    - internal/conf/validate.go
    - configs/config.yaml
    - cmd/server/main.go
    - cmd/server/app.go
---

## 1. 使用的系统与框架

项目采用 **go-kratos/v3** 作为应用框架，并使用其内置的 `config` 子系统完成配置的加载、分层与扫描。配置以 **Protobuf（proto3）** 定义结构体 `Bootstrap`，通过 `protoc`/buf 生成 Go 代码后，由 Kratos `config.Scanner` 将 YAML 映射到该强类型结构。

配置源按以下顺序注册（后者优先覆盖前者）：
1. `file.NewSource(flagconf)` — 从 `-conf` 命令行参数指定的目录读取配置文件（默认 `configs/config.yaml`）。
2. `env.NewSource("KRATOS")` — 以 `KRATOS_` 为前缀的环境变量覆盖文件配置。

启动入口在 `cmd/server/main.go`：解析 `-conf` flag → 创建 `config.Config` → `Load()` → `Scan(&bc)` 得到 `conf.Bootstrap` → 调用 `buildApp(&bc, logger)` 组装服务。

## 2. 关键文件与包

| 文件 | 作用 |
|---|---|
| `internal/conf/conf.proto` | 配置结构的权威 schema：`Bootstrap`、`Server`、`Runtime`、`PostgreSQL`、`Redis`、`AccessToken`、`Notification`、`OIDC` 等消息定义 |
| `internal/conf/conf.pb.go` | 由 buf/protoc 生成的 Go 结构体（不可手动修改） |
| `internal/conf/validate.go` | 运行时配置校验逻辑，实现 `Bootstrap.Validate()` 及所有子字段校验 |
| `configs/config.yaml` | 示例/默认配置文件，包含 server、runtime.postgresql/redis/access_token/notification/oidc 等完整配置项 |
| `cmd/server/main.go` | 配置加载入口，组合 file + env source，scan 到 `Bootstrap` |
| `cmd/server/app.go` | `buildApp` 中消费 `Bootstrap` 构建各运行时依赖（PG pool、Redis client、OIDC provider、gRPC TLS 等） |
| `tests/cp0/runtime_test.go` | 测试侧复用同一配置加载流程，验证 CP0 进程生命周期 |

## 3. 架构与设计约定

### 3.1 单一强类型 Bootstrap 结构
所有配置集中在 `Bootstrap` 根消息下，分为三大块：
- `profile`：当前仅允许值为 `workload-isolated`（常量 `IsolatedProfile`），用于标识运行模式。
- `server`：监听器配置，包括 gRPC（含 mTLS 证书路径、客户端 CA、Gateway DNS 身份）和 Admin HTTP 端点，以及 `shutdown_timeout`。
- `runtime`：外部依赖与业务开关，包括 PostgreSQL DSN、Redis 连接与限流参数、Access Token 签名密钥、通知服务 mTLS、OIDC（console/boss）、工作负载注册表文件路径与 SHA256、租户生命周期文件、`environment`、`trust_domain`、`policy_revision`。

### 3.2 启动时严格校验（Validate 阶段）
`Bootstrap.Validate()` 在 `buildApp` 入口处立即执行，失败则直接 panic，确保后续组件不会拿到非法配置。校验规则包括：
- 必须使用 `workload-isolated` profile。
- gRPC 与 admin 监听地址必须不同（除回环 `127.0.0.1:0` / `[::1]:0`）。
- gRPC 必须启用双向 TLS，且证书、私钥、client CA 均为绝对路径；gateway_client_dns_name 非空且不含空白字符。
- 监听网络必须为 `tcp`，地址必须显式绑定 IP 且端口合法。
- 所有 timeout 必须在 1ns..上限范围内（如 shutdown 30s、Redis 读写 5s、OIDC reauth 1h 等）。
- PostgreSQL DSN 必须使用 `postgres`/`postgresql` scheme，用户名强制为 `ani_iam_runtime`，数据库名不能为空或 `/`；非回环 PG 必须开启 `sslmode=verify-full` 并指定绝对路径的 `sslrootcert`。
- Redis namespace 必须是非空 token；login_limit > 0；登录窗口 ≤ 24h。
- Access Token issuer 固定为 `ani-iam`；active_key_id 必填；private_key_file 为绝对路径。
- Notification 地址、证书、outbox key、private key、server CA 均为绝对路径；server_dns_name 小写无空白；console_action_url_base / boss_action_url_base 必须是规范 HTTPS URL（无 userinfo/query/fragment）；locale 仅限 `en-US` 或 `zh-CN`。
- OIDC provider 固定为 `dex`；client_id 固定为 `ani-console`；issuer_url 必须是规范 HTTPS 或隔离环境的回环 HTTP；login_redirect_uri 与 identity_link_redirect_uri 必须不同且为规范 HTTPS；boss_oidc 若配置需与 console 使用不同 origin 与 secret 文件。
- `policy_revision` 必须以 `sha256:` 开头且为 32 字节十六进制。
- workload_registry_file 必须为绝对路径，`workload_registry_sha256` 必须匹配实际文件的 SHA256。
- environment、trust_domain 必须是小写、无空白、无分隔符的规范标识。

### 3.3 敏感信息分离
密码、私钥、secret 不直接写入配置文件，而是通过“文件路径”引用：
- `server.grpc.tls.certificate_file/private_key_file/client_ca_file`
- `runtime.access_token.private_key_file`
- `runtime.notification.outbox_key_file/private_key_file/server_ca_file/certificate_file`
- `runtime.oidc.client_secret_file`
这些路径在 `app.go` 的 `buildApp` 中通过专用函数（如 `data.LoadEd25519PrivateKeyFile`、`data.LoadOIDCClientSecretFile`、`data.LoadOutboxProtector`）按需加载，避免常驻内存。

### 3.4 日志脱敏
`newRuntimeLogger` 使用 Kratos log handler 的 `FilterKey` 过滤敏感键：`args`、`authorization`、`cookie`、`credential`、`dsn`、`password`、`postgresql.dsn`、`private_key`、`private_key_file`、`redis.password`、`set-cookie`、`token`，防止配置中的敏感值被意外输出。

### 3.5 多命令子进程共享同一配置
`main.go` 支持多个子命令（`begin-tenant-snapshot`、`provision-tenant-broker`、`install-workload-registry`、`provision-first-administrator`、`provision-workloads`），它们先于主服务流程解析并执行，但同样遵循 `-conf` 约定的配置路径。

## 4. 约束与规则总结

- **配置格式**：YAML，通过 Kratos file source 加载。
- **环境变量覆盖**：以 `KRATOS_` 为前缀，覆盖同名 YAML 字段。
- **唯一允许的运行 profile**：`workload-isolated`，其他值在 `Validate()` 中被拒绝。
- **所有外部依赖地址必须显式绑定 IP**：禁止 `0.0.0.0`、`localhost` 等模糊地址。
- **PostgreSQL 必须使用受限角色 `ani_iam_runtime`**，且非回环连接必须启用 `sslmode=verify-full` 并指定 CA 文件。
- **OIDC 提供商锁定为 `dex`**，console 客户端 ID 锁定为 `ani-iam`。
- **所有证书/密钥/secret 必须通过绝对路径文件引用**，不允许内联。
- **策略与注册表通过 SHA256 指纹校验**：`policy_revision`、`workload_registry_sha256`、可选的 `tenant_lifecycle_sha256` 均要求精确匹配的十六进制摘要。
- **配置校验发生在应用启动早期**，任何非法配置都会导致进程退出，不存在“部分可用”状态。
- **配置变更通过替换配置文件 + 重启生效**，不支持热重载（代码中未发现 watch/reload 逻辑）。