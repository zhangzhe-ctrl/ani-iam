# DP2-00 基线恢复证据

状态：`PASS / RESOLVED`

日期：2026-09-03（Asia/Shanghai）

## 固定输入与归档

- Kratos 实现基线：`05ba302661d593b608df070dd51cc063fc9f8023`。
- 事项03归档提交：`61ef11ded04571c8d1bfc126ba5223407f203abe`。
- 事项04负向调查归档提交：`824672584270f92f029f8976de74d41adeced026`。
- Direct P2 计划归档提交：`6142b27fe5a6b2918c0e0f5d746d8cc0601f0ee4`。
- 恢复引用：本地分支 `codex/cp0-archive` 固定到 `6142b27fe5a6b2918c0e0f5d746d8cc0601f0ee4`。
- 目标分支：`codex/direct-p2-01-05`。

事项03与事项04分别归档。事项04继续保持 `FAIL / BLOCKED`；PostgreSQL commit 失败且 Redis 补偿删除也失败的组合明确为 `not_verified`，没有被描述为可用实现。

## 恢复范围

以下受事项03/04修改的骨架文件恢复到固定提交：

- `go.mod`
- `go.sum`
- `internal/data/data.go`
- `internal/server/grpc.go`
- `tests/cp0/runtime_test.go`

以下未投入使用的实验实现从 Direct P2 工作树移除，但仍可从 archive 分支恢复：

- `internal/compat/authv1/**`
- `internal/service/legacy_auth.go`
- `internal/data/errors.go`
- `internal/data/legacy_*.go`
- `tests/cp0/legacy_auth_transport_test.go`
- `tests/cp0/legacy_storage_integration_test.go`

规格、计划、事项、ADR和证据不属于运行时兼容资产，继续保留。旧 ANI/Auth 部署资产未被修改或删除。

## 验证

以下比较无输出并以状态码 0 完成，证明实现路径与固定 Kratos 骨架一致：

```text
git diff --exit-code 05ba302661d593b608df070dd51cc063fc9f8023 -- \
  go.mod go.sum api cmd configs internal tests
```

恢复后的实际门禁：

```text
GOPROXY=off go test -count=1 -p=1 ./...
GOPROXY=off go vet -p=1 ./...
GOPROXY=off go build -p=1 -trimpath -o /tmp/ani-iam-dp2-baseline-server ./cmd/server
go mod verify
git diff --cached --check
```

结果全部为 `pass`。Go 测试覆盖 `cmd/server`、`internal/conf`、`internal/server` 和 `tests/cp0`；其余包成功编译或没有测试文件。

## 边界与恢复

- 没有修改 ANI、数据库、Redis、容器、部署、Credential 或外部运行环境。
- 没有 push、tag、merge、切流或启动 DP2-01。
- 清理提交可以普通 `git revert` 恢复；完整清理前现场也可从 `codex/cp0-archive` 读取。
- 本事项移除的是独立 IAM 仓库中未投入使用的实验代码，不代表 DP2-19 的旧部署资产删除已经执行或获批。
