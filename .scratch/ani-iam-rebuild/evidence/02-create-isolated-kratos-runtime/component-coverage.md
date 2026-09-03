# Kratos component coverage

## 结论

CP0-0 外层运行时已改为由 Kratos v3 core/contrib 承担可用的框架能力。这里的“必须用”按当前事项的实际职责判断：运行时需要且 Kratos 已提供的能力必须采用；没有业务 RPC、外部客户端或外部基础设施的组件不得为了凑清单而空接入。

## 已采用

| 能力 | Kratos 组件 | 当前用途与证据 |
| --- | --- | --- |
| 官方初始化 | `kratos` CLI 与 `kratos-layout` | 用固定 CLI 和模板 Commit 生成未裁剪基线；来源、命令、摘要与差异见 `generated-baseline.md`、`scaffold-delta.md`。 |
| 应用生命周期 | `kratos.App` | `Run`、signal、server start/stop、`AfterStart`、`BeforeStop`、`AfterStop`、`StopTimeout` 全部交给 Kratos。 |
| 配置 | `config`、`config/file`、`config/env` | 加载固定配置目录和 `KRATOS_` 环境覆盖，扫描到生成的 typed config，并在进程生命周期结束时关闭。 |
| 日志 | `log.NewHandler`、`log.SetDefault` | Kratos handler 统一 JSON、level、source、trace/span 提取和敏感字段过滤；应用与 automaxprocs 都进入同一 Kratos logger。 |
| gRPC transport | `transport/grpc` | Kratos 管理 listener、timeout、中间件、启动/停止；使用其默认 gRPC health/admin 服务，明确关闭 reflection。 |
| 内部 HTTP transport | `transport/http` | Kratos 管理 admin listener、router、timeout、middleware、codec、response/error encoding；只注册 health、readiness、metrics。 |
| 错误模型 | `errors` | readiness 未就绪使用 Kratos `ServiceUnavailable`，由 Kratos HTTP error encoder 输出稳定状态与 reason。 |
| 恢复 | `middleware/recovery` | HTTP 与 gRPC 使用同一 Kratos panic recovery，并接入统一 logger。 |
| 请求元数据 | `middleware/metadata` | HTTP 与 gRPC 使用 Kratos server metadata 传播。 |
| 请求日志 | `middleware/logging` | HTTP 与 gRPC 请求日志由 Kratos middleware 产生；测试断言日志出现并带 trace ID。 |
| 请求校验 | `middleware/validate` | 安装 Kratos validator；当生成的请求实现 `Validate` 时自动执行。CP0-0 尚无业务请求类型。 |
| 链路追踪 | contrib `otel/tracing` | HTTP 与 gRPC 使用 Kratos tracing middleware 和 trace attribute extractor；W3C TraceContext/Baggage 传播启用。 |
| 请求指标 | contrib `otel/metrics` | 使用 Kratos server middleware、默认请求 counter、默认 latency histogram/view 和 exemplar helper。 |
| 编解码 | Kratos HTTP 默认 codec/encoder | health/readiness payload 和 Kratos errors 经 transport 的标准返回路径编码，没有手写 JSON。 |

## 当前未采用

| Kratos 能力 | 未采用原因 |
| --- | --- |
| registry、discovery、registrar | CP0-0 只绑定 loopback 隔离端口，没有获准的注册中心或部署环境；空注册会制造错误运行语义。 |
| client、selector、负载均衡、client middleware、circuit breaker | 当前没有任何外部服务客户端；调用方兼容和依赖接入属于后续事项。 |
| rate limit | listener 只承载集群内 health/readiness/metrics；没有业务流量或已接受的限流策略，对探针盲目限流会破坏健康判断。 |
| JWT/auth middleware 或 auth contrib | CP0-0 不提供业务 API，也没有获准的 credential、issuer、audience 或认证契约。 |
| 业务 Proto/HTTP/error/OpenAPI 生成代码 | 本事项明确禁止 `api/iam/v1/**` 和旧 Auth/P2 业务；规格也禁止公共 IAM HTTP 业务转码。 |
| Wire | 规格与 ADR-0019 明确要求单一 composition root 和显式构造函数；没有自研 DI/lifecycle 容器。 |
| TLS transport options | 当前仅 loopback 隔离运行，没有证书、Secret Manager 或部署授权；生产 TLS 是独立门禁。 |
| pprof | 没有获准的暴露面、访问控制或生产诊断策略；不能擅自增加管理接口。 |
| 外部 config source | 当前只批准 file + env，没有配置中心依赖或访问凭据。 |
| contrib OTel log handler/exporter | 没有 OTel log provider、collector endpoint 或日志后端；采用 Kratos core JSON stdout logger，避免将日志送入无导出器的空管道。 |
| OTLP trace exporter | 没有 collector endpoint、网络和部署授权；Kratos tracing、trace ID 和上下文传播已启用，外部 trace export 为 `not_verified`。 |
| gRPC reflection | 隔离 runtime 暂无业务 schema，且未批准未认证的 schema 暴露；显式 `DisableReflection`。 |
| `grpc.CustomHealth` | Kratos 默认 gRPC health 已满足本事项，替换默认实现反而会重复造轮子。 |

## 保留的薄自实现

| 代码 | 原因与边界 |
| --- | --- |
| `Readiness` 原子状态 | 固定的 Kratos v3 core 提供 gRPC health，但没有 HTTP readiness 状态机；这里只保存 App hook 驱动的单一布尔状态，不实现 transport 或探针协议。 |
| 三个 admin handler 与 `invoke` 适配 | Kratos 没有 HTTP health/readiness/Prometheus 路由生成器，且本事项禁止伪造业务 Proto；handler 只把三条内部路由送入 Kratos middleware、codec 和 error encoder。 |
| 隔离配置校验 | `cp0-isolated`、loopback 和 timeout 是 ANI IAM 的项目不变量，不是通用框架职责。 |
| 显式构造函数 | 这是 ADR-0019 接受的 Go composition root，不实现 lifecycle、service locator 或 DI 框架。 |
| Prometheus exposition | `/metrics` 使用官方 `promhttp.HandlerFor`，Go/process collector 与 OTel Prometheus exporter 均为上游组件；仓库没有自写 metrics registry 或编码器。 |

## 仍未验证

- 外部 OTLP trace/log export、真实 collector/后端、Kubernetes 探针、TLS 和生产部署：`not_verified`，不属于 CP0-0。
- validation middleware 已安装并进入共享链，但本事项没有业务 request message，因此业务字段校验效果留给首个契约事项验证。
