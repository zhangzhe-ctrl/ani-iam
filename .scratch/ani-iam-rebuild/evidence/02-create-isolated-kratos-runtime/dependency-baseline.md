# 依赖基线

## 固定版本

| 组件 | 精确版本 | 依据 |
| --- | --- | --- |
| Go language baseline | `go 1.25.7` | 与固定 Kratos CLI 模板一致 |
| `github.com/go-kratos/kratos/v3` | `v3.0.0` | CLI 模板运行时 |
| `github.com/go-kratos/kratos/contrib/otel/v3` | `v3.0.0-20260515082355-1ddb58e407c5` | CLI 模板日志 trace extractor |
| `go.uber.org/automaxprocs` | `v1.6.0` | CLI 模板入口 |
| `github.com/prometheus/client_golang` | `v1.24.1` | 内部 metrics handler |
| `go.opentelemetry.io/otel/exporters/prometheus` | `v0.66.0` | Kratos contrib OTel metrics 的 Prometheus exporter；其 OTel 依赖与固定 `v1.44.0` 对齐 |
| `google.golang.org/grpc` | `v1.82.1` | 覆盖模板 `v1.81.1`，修复 `GO-2026-6061` |
| `google.golang.org/protobuf` | `v1.36.11` | typed config 生成器与运行时 |
| `go.opentelemetry.io/otel*` | `v1.44.0` | 覆盖模板传递版本 `v1.43.0`，修复 `GO-2026-5158` |

所有版本均写入 `go.mod`/`go.sum`；生成、构建和检查命令不使用 `latest`。

## 可复现摘要

```text
6cac45c0f25d023b150beb8c97d3a64b2481e53385e19a31045852bebbc1ef8e  go.mod
1b41b52ad891bdb1d3dd04b2b7790eb810a46dbc2d0f22aebfde6e66ef2decc5  go.sum
e59f87b238cdf177f77b7be4aa2586166541545f40c58be150b4a1f90ebd74f1  bom.cdx.json
```

`GOPROXY=off go mod verify` 实际结果为 `all modules verified`。

采集机工具链是 `go1.26.7-X:nodwarf5 linux/amd64`。模块声明和官方模板基线是 Go 1.25.7，但 Go 1.25.7 原生 runner 尚未执行，记为 `not_verified`。
