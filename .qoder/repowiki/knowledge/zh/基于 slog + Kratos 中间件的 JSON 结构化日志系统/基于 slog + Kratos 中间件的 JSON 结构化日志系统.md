---
kind: logging_system
name: 基于 slog + Kratos 中间件的 JSON 结构化日志系统
category: logging_system
scope:
    - '**'
source_files:
    - cmd/server/main.go
    - cmd/server/app.go
    - internal/server/observability.go
---

## 1. 使用的框架与工具

- **标准库 `log/slog`**：项目统一使用 Go 1.21+ 的 `slog.Logger` 作为所有业务代码的日志记录器，未引入第三方日志库（如 zap、logrus）。
- **Kratos 日志适配**：通过 `github.com/go-kratos/kratos/v3/log` 将 `slog.Logger` 桥接到 Kratos 框架，并作为 Kratos 应用默认 logger 注入（`kratos.Logger(logger)`）。
- **Kratos gRPC 中间件链**：在 `internal/server/observability.go` 中集中组装请求级中间件，包括 `recovery`、`metadata`、`tracing`、`logging`、`metrics`、`validate`，其中 `logging.Server(logger)` 由 Kratos 提供，用于自动记录 gRPC 请求/响应。
- **OpenTelemetry 集成**：Tracing 与 Metrics 通过 `go.opentelemetry.io/otel` 及 Kratos contrib 包注册到全局 `otel`，并通过 `kratostracing.Server` 和 `kratosmetrics.Server` 注入中间件链。

## 2. 关键文件

- `cmd/server/main.go`：进程入口，创建运行时 `slog.Logger`，设置 Kratos 默认 logger，配置 JSON 格式、级别、源码行、Trace 属性提取与敏感字段过滤。
- `cmd/server/app.go`：组装 Kratos 应用、gRPC/HTTP 服务器、后台 worker（API key 维护、密码动作通知），并将 `*slog.Logger` 以依赖注入方式传递给各组件。
- `internal/server/observability.go`：集中初始化 Prometheus、OpenTelemetry MeterProvider/TracerProvider，并提供 `ServerMiddleware(logger)` 供 gRPC 服务器复用。
- `configs/config.yaml`：Kratos 配置文件来源（配合 `KRATOS_*` 环境变量）。

## 3. 架构与约定

### 3.1 日志器生命周期

1. `main()` 解析命令行参数后调用 `newRuntimeLogger(os.Stdout)` 创建根 logger。
2. 通过 `log.SetDefault(logger)` 设置 Kratos 全局默认 logger，使 Kratos 内部日志也走同一输出。
3. 将同一个 `*slog.Logger` 实例通过构造函数注入到 `buildApp`、各类 worker、`coreLifecycleRuntime` 等组件，形成“单例式”共享 logger。
4. 测试中通过 `slog.New(slog.NewTextHandler(io.Discard, nil))` 或捕获 buffer 的方式替换 logger，验证行为。

### 3.2 输出格式与字段

- **JSON 格式**：`log.WithFormat(log.FormatJSON)` 强制结构化输出，便于日志采集系统解析。
- **固定上下文字段**：每个日志事件自动附带 `service.id`（hostname）、`service.name`（构建时注入的 Name）、`service.version`（构建时注入的 Version）。
- **Trace 关联**：`log.WithExtractor(tracing.TraceAttrs)` 从 OpenTelemetry context 中提取 trace/span 信息并附加为 slog 字段，实现日志与链路追踪关联。
- **源码位置**：`log.WithAddSource(true)` 启用文件名与行号。

### 3.3 敏感字段过滤

通过 `log.FilterKey(...)` 显式过滤以下键名，防止泄露敏感数据：
`args`、`authorization`、`cookie`、`credential`、`dsn`、`password`、`postgresql.dsn`、`private_key`、`private_key_file`、`redis.password`、`set-cookie`、`token`。

### 3.4 日志级别策略

- 运行时默认级别为 `LevelInfo`（即 INFO）。
- 业务代码中仅使用 `logger.Warn(...)` 记录可恢复异常（如 API key flush 失败、snapshot 刷新失败、通知分发失败），未出现 `Debug`/`Error` 调用；错误路径通常返回 error 并由上层处理，而非直接写 ERROR 日志。
- Kratos 中间件层由 `logging.Server` 自动记录请求级日志，业务代码无需重复记录。

### 3.5 可观测性扩展

`Observability` 同时承担指标与追踪初始化职责，其 `ServerMiddleware(logger)` 将日志、追踪、指标中间件串联，确保每个 gRPC 请求都经过统一的日志/追踪/度量流水线。Prometheus exporter 通过 OTel Prometheus exporter 暴露自定义 gauge（如 `ani_iam_runtime_ready`、`ani_iam_api_key_stale_non_expiring_count` 等）。

## 4. 约定与约束

- **统一使用 `log/slog`**：全仓库未见其他日志库导入，所有日志均通过 `*slog.Logger` 发出。
- **禁止裸 `fmt.Print`/`os.Stderr` 输出业务日志**：除 `begin-tenant-snapshot`、`provision-*` 等独立子命令的错误输出外，服务主流程全部经 slog。
- **结构化字段命名**：业务侧通过 slog key-value 形式追加字段（如 `"retryable", errors.Is(err, biz.ErrPasswordActionNotificationRetryable)`），不拼接字符串消息。
- **敏感字段白名单过滤**：新增可能携带敏感信息的字段名需加入 `FilterKey` 列表，否则可能被输出到日志。
- **日志与错误语义分离**：可预期异常使用 `Warn` 并附带结构化字段；不可恢复错误通过返回值上报，不在业务层直接打 `Error` 日志。
- **测试隔离**：测试通过构造独立的 `slog.Logger` 指向 `io.Discard` 或缓冲区来断言日志输出，避免污染真实输出。
