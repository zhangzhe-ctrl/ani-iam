# 构建与生命周期验证

## 自动门禁

以下命令均针对最终依赖图执行：

```bash
GOPROXY=off go test -count=1 ./...
GOPROXY=off go vet -p=1 ./...
GOPROXY=off go build -p=1 -trimpath -o /dev/shm/ani-iam-issue02-final/server ./cmd/server
GOPROXY=off go mod verify
```

结果均为 `pass`。最终复验因 `/home` 与 `/tmp` 用户配额只剩约 21MB，使用本事项专用 `/dev/shm/ani-iam-issue02-final` build cache；module cache 只读且 `GOPROXY=off`，未在复验时解析新版本。

`tests/cp0` 实际启动 Kratos app，验证 gRPC health、admin health/readiness/metrics、Kratos 请求日志带 trace ID、Stop 后 readiness=false 和五秒内优雅退出。`internal/server` 另用测试路由验证 recovery、metadata 和 validation middleware 确实执行，不只是被导入。

## 独立进程验收

启动命令：

```bash
/dev/shm/ani-iam-issue02-final/server -conf configs
```

实际结果：

```text
GET http://127.0.0.1:19091/healthz -> 200 {"status":"ok"}
GET http://127.0.0.1:19091/readyz  -> 200 {"status":"ready"}
GET http://127.0.0.1:19091/metrics -> 200, ani_iam_runtime_ready 1
server_requests_code_total         -> healthz/readyz code=200
server_requests_seconds_count      -> healthz/readyz count=1
gRPC health Check                 -> SERVING（自动生命周期测试）
```

从 automaxprocs、HTTP/gRPC 启动、三个 HTTP 请求到停止，最终进程日志全部通过 Kratos `log.NewHandler` 输出 JSON。请求日志的 source 是 Kratos `middleware/logging`，包含 32 位 `trace_id`、16 位 `span_id`，`args` 被 Kratos filter 替换为 `***`。向最终构建进程发送 SIGINT 后，Kratos 同时输出 `[gRPC] server stopping` 与 `[HTTP] server stopping`，退出码为 `0`。

首次在默认 sandbox cache 下复验分别遇到只读 build cache、禁止 loopback socket 和磁盘 quota；它们均是执行环境失败，不计为代码通过。最终结果来自获准的 loopback 测试和上述独立临时目录重跑。

## 未验证项

- Go 1.25.7 原生 runner：`not_verified`（采集机为 Go 1.26.7）。
- 仓库尚未选择独立 Go lint 工具；本事项只执行 `go vet`，额外 lint 为 `not_verified`。
- `go test -race`、Fuzz、专项并发和性能门禁：依 ADR-0021 延期，`not_verified`。
