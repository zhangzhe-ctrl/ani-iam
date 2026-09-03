# 兼容与错误边界

## 依赖方向

```text
cmd/server
  -> internal/server -> internal/compat/authv1 generated registration
                     -> internal/service -> internal/compat/authv1 request/response mapping

internal/biz -X-> internal/compat/authv1
```

- 冻结 Proto source、生成消息、client/server 与 full-method 常量只位于 `internal/compat/authv1/**`。
- `internal/service` 当前只承载明确的 CP0-1 未实现映射，不引入业务状态迁移或存储访问。
- `internal/biz` 没有兼容 Proto import；`TestBizLayerHasNoFrameworkOrAdapterImports` 已把 `internal/compat` 加入禁止 import 清单。
- gRPC 注册和默认 compatibility service 的构造由 `internal/server` 完成；另有显式注入构造器供 transport contract 测试替换 handler。`cmd/server` 无需超出事项允许路径进行改动。

## 公共错误映射 Oracle

事项01冻结的 Gateway 映射保持不变：

| 内部 gRPC code | ANI 公开 HTTP |
| --- | --- |
| `Unauthenticated` | `401` |
| `PermissionDenied` | `403` |
| `ResourceExhausted` | `429` |
| `DeadlineExceeded` | `504` |
| `Unavailable` / `FailedPrecondition` | `503` |

事项03不修改 ANI Gateway，且不切流。当前 CP0-1 的 `Unimplemented / CP0_COMPAT_NOT_IMPLEMENTED` 是防止假成功的实验期结果，不宣称已有公开 HTTP 映射；Gateway/Envoy/Inference 的 live 映射保持 `not_verified`，由后续真实语义和 caller 事项验证。

框架、数据库或第三方原始错误当前不会穿过已注册方法：compat service 只返回标准 context status 或显式构造的 status + `ErrorInfo`。
