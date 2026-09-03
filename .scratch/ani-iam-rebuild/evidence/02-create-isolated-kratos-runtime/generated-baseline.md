# Kratos CLI 生成基线

## 生成来源

```text
command:  kratos new iam-service
cli:      kratos version v3.0.0
module:   github.com/go-kratos/kratos/cmd/kratos/v3@v3.0.0-20260626125723-668db92c2c00
binary:   5fa73bad7552d84712f2273c0cd4b5b8ec9b988ee69755f22a0576493b8a727c
template: https://github.com/go-kratos/kratos-layout.git
commit:   59ad406328acba9a70c9e7f426720a75a89a6b9f
date:     2026-09-01T13:43:07+08:00
subject:  chore(deps): bump aip-go/ents to latest
```

CLI 在 `/tmp/ani-iam-issue02-cli-baseline.5mpWh0/iam-service` 生成未裁剪副本；模板 cache 在采集时 clean，且 HEAD 为上述提交。临时副本不作为仓库源码，只用于可审计比较。

## 原始关键文件摘要

```text
b9e02d89c93d8654244196068f40d7ac645a562f7848b962502f585de502d0a7  cmd/iam-service/main.go
719391b0b02657471106b4527ac4bc18391dbd1145cd2872ad95aa616295f694  internal/conf/conf.proto
b532407cd6d76b1b62ecc1ea1aabe46beed14edaba3289898fa15c91b4d08d5d  internal/server/grpc.go
5a0ecc923c20e49a84b7b89c8a131affc63ef9343bd3ffbb3675d10d2280611b  internal/server/http.go
07cbd67e68e0cb34d2e05c72c811fe9a1eadf0df17bb74d877f6ec049571327c  go.mod
```

基线明确包含 `main/newApp`、Kratos config source、generated typed config、Kratos gRPC/HTTP transport、Wire、Todo 示例、Ent/MySQL 及标准 `internal/{biz,data,service,server,conf}` 目录。最终取舍见 `scaffold-delta.md`。
