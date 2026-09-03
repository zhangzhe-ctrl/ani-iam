# 隔离边界

## 可执行约束

- committed 配置固定 `profile: cp0-isolated`；其他 profile 在构造 runtime 前被拒绝。
- gRPC 与 admin listener 只接受字面量 loopback IP；`0.0.0.0`、外部 IP 和 `localhost` hostname 均被拒绝。
- Kratos HTTP transport 只注册 `GET /healthz`、`GET /readyz`、`GET /metrics`，没有业务 handler。
- 由 Proto 生成的 typed config 不含 PostgreSQL、Redis、Dex、NATS、旧 Auth endpoint、Credential 或 signing material 字段；当前没有 data adapter。
- `internal/data`、`internal/service` 与 `internal/biz` 只有分层入口，不包含兼容 RPC 或 P2 业务。

## 负向测试

`internal/conf.TestBootstrapValidate` 覆盖错误 profile、外部可达 gRPC 地址、hostname admin 地址和非法 shutdown timeout。

`tests/cp0.TestBizLayerHasNoFrameworkOrAdapterImports` 扫描 `internal/biz`，禁止 Kratos、Protobuf、gRPC、data、数据库 driver、pgx 和 Redis import。

`tests/cp0.TestCommittedConfigLoadsAsGeneratedType` 使用真实 `configs/config.yaml` 经过 Kratos config source、generated Proto type scan 与 `Validate`，结果通过。

## 外部状态

测试和人工验收只短暂绑定 `127.0.0.1`；测试使用端口 `0`，人工验收使用 `19090/19091`。停止后 listener 已关闭。没有连接、读取或写入旧 Auth、数据库、Redis、Dex、NATS 或其他共享依赖。
