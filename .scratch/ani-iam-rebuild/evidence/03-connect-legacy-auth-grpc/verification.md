# 验证记录

采集日期：2026-09-03（Asia/Shanghai）

## 已执行

| 命令 | 结果 |
| --- | --- |
| `buf lint`（compat module） | `pass` |
| 从固定 Git object 与 compat source 分别 `buf build --as-file-descriptor-set --exclude-source-info` 后 `cmp` | `pass`，两者 SHA-256 均为 `64f73f…b0` |
| `GOPROXY=off GOSUMDB=off go test -count=1 -p=1 ./...` | `pass`；loopback 权限下执行 |
| `GOPROXY=off GOSUMDB=off go vet -p=1 ./...` | `pass` |
| `GOPROXY=off GOSUMDB=off go build -p=1 -trimpath -o /tmp/ani-iam-issue03-server ./cmd/server` | `pass` |
| `go mod verify` | `pass` |
| `git diff --check` | `pass` |

Go build cache 与 work 目录使用 `/dev/shm/ani-iam-issue03-*`。首次未指定可写 cache 的测试因默认 cache 只读失败；随后指定 `/dev/shm`。沙箱内 transport 测试因禁止 loopback listener 失败；获得仅本机临时 listener 权限后复跑通过。这两项是环境限制，不是代码失败。

## 未验证

- 真实 PostgreSQL/RLS、Redis 与 Dex：`not_verified`，属于事项04及后续。
- Gateway、Envoy、Inference live E2E 和公开错误映射：`not_verified`，本事项不切流。
- 旧 14 RPC 的业务成功、凭据、Claims、持久化副作用与 TTL：`not_verified`，本事项只建立 transport。
- Race、Fuzz、性能、HA、TLS、部署和生产就绪：`not_verified`，不属于 CP0-1。
