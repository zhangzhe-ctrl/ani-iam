# 事项03证据索引

状态：`PASS / RESOLVED`

采集日期：2026-09-03（Asia/Shanghai）

唯一 ANI 基线：`main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`

本事项只证明 CP0-1 的旧 Auth gRPC transport 兼容边界。旧业务语义、真实 PostgreSQL/RLS、Redis、Dex、三个调用方 live E2E 和切流均未进入本事项，不能据此声明 CP0 Go。

| 证据 | 状态 | 定位 |
| --- | --- | --- |
| 冻结 Proto source 与 descriptor diff | `pass` | [descriptor.md](descriptor.md) |
| 14 RPC 注册、调用和明确未实现错误 | `pass` | [transport-contract.md](transport-contract.md) |
| deadline、取消、未知方法、ErrorInfo detail | `pass` | [transport-contract.md](transport-contract.md) |
| 兼容包、service、server、biz 依赖边界 | `pass` | [compatibility-boundary.md](compatibility-boundary.md) |
| 公共错误映射静态 Oracle | `pass` | [compatibility-boundary.md](compatibility-boundary.md) |
| Gateway/Envoy/Inference live 映射 | `not_verified`：事项03禁止切流且没有 live 拓扑 | [compatibility-boundary.md](compatibility-boundary.md) |
| Go、Buf、构建和静态检查 | `pass` | [verification.md](verification.md) |

## 结论

- Kratos runtime 已注册冻结的 `auth.v1.AuthService`，14 个 full method 保持不变。
- CP0-1 没有业务或存储依赖；所有已注册方法统一 fail closed 为 gRPC `Unimplemented`，并携带稳定 `google.rpc.ErrorInfo`：domain `ani.auth.v1`、reason `CP0_COMPAT_NOT_IMPLEMENTED`、metadata `method=<full method>`。
- 调用方取消、调用方 deadline 和 Kratos server timeout 均保持标准 gRPC `Canceled`/`DeadlineExceeded`；未知方法返回 `Unimplemented`。
- 事项04开始接真实旧存储前，当前未实现错误不能被当成旧业务语义兼容证据。
