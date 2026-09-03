# Scaffold 差异

最终实现不是重新仿制目录，而是以 `generated-baseline.md` 的官方 CLI 输出为参照，按事项 02 和已接受 ADR 裁剪。

| 基线内容 | 处理 | 依据与最终结果 |
| --- | --- | --- |
| `main`、`newApp`、Kratos logger、config source、`-conf` | 保留并深化 | 保持 CLI composition root 与启动约定；logger 改用 Kratos v3 `log.NewHandler` 的 JSON/source/trace/filter，automaxprocs 也桥接到同一 logger；增加隔离校验、readiness hook 和 stop timeout |
| `internal/{biz,data,service,server,conf}` | 保留 | ADR-0019 标准 Kratos 分层；空层只保留入口说明 |
| Kratos gRPC server + middleware | 保留并深化 | 单进程 gRPC 生命周期；采用 recovery、metadata、tracing、logging、metrics、validation 共享链；禁用 reflection，保留标准 gRPC health |
| Kratos HTTP server | 替换并深化 | ADR-0019 禁止业务 HTTP；替换为仅 loopback 的 admin health/readiness/metrics transport，手写路由显式进入 Kratos middleware/codec/error encoder |
| `conf.proto` generated typed config | 替换 | 保留生成式 typed config；字段收敛为 `cp0-isolated`、gRPC/admin listener 和 shutdown timeout |
| Wire | 删除 | ADR-0019 明确使用显式构造，当前规模无需 Wire |
| Todo API、service、biz、Ent/MySQL data | 删除 | 示例业务和数据层不属于事项 02；ADR-0020 的目标 data 技术栈也不是 Ent/MySQL |
| Todo 业务 HTTP 注册 | 删除 | 本事项无业务契约；禁止借 scaffold 提前引入公开 HTTP |
| validate middleware | 保留 | 进入 HTTP/gRPC 共享链；测试证明实现 `Validate` 的 request 会得到 Kratos `VALIDATOR` error，业务 message 待后续契约事项生成 |
| Kratos contrib OTel tracing/metrics | 保留并深化 | tracing、trace attribute extractor、默认 server counter、latency histogram/view 与 exemplar helper；Prometheus exporter 固定为 `v0.66.0` |
| 模板 gRPC `v1.81.1` | 替换 | `GO-2026-6061` 修复为 `v1.82.1` |
| 模板传递 OTel `v1.43.0` | 替换 | `GO-2026-5158` 修复为 `v1.44.0` |
| Makefile、Dockerfile、LICENSE、根级生成工具文件 | 未导入 | 不在事项 02 `Allowed paths`，且本事项不授权部署或根级工程化扩张 |

所有最终文件都位于事项允许路径；没有修改 `api/iam/v1/**`、migration、deploy 或 ANI。
