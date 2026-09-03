# 已知漏洞扫描

扫描工具固定为 `golang.org/x/vuln/cmd/govulncheck@v1.7.0`，扫描目标为 `./...`。

## 模板依赖修正

1. 初始 Kratos 依赖解析得到 `google.golang.org/grpc@v1.81.0/1.81.1`，命中可达的 `GO-2026-6061`；显式固定到修复版 `v1.82.1`。
2. CLI 模板的 tracing 依赖带入 `go.opentelemetry.io/otel@v1.43.0`，命中已导入但当前调用链不可达的 `GO-2026-5158`；仍显式固定到修复版 `v1.44.0`。

两项都记录在 scaffold 差异和依赖基线中，没有因“当前不可达”而忽略已知修复版。

## 最终扫描

```bash
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 -show verbose ./...
```

扫描 7 个根 package、34 个第三方 module 与 Go 标准库，实际结果：

```text
No vulnerabilities found.
```

这是 2026-09-02 当次 Go vulnerability database 与最终依赖图的结果，不是未来持续无漏洞保证。
