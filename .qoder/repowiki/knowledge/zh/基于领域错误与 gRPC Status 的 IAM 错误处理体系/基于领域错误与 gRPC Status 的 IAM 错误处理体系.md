---
kind: error_handling
name: 基于领域错误与 gRPC Status 的 IAM 错误处理体系
category: error_handling
scope:
    - '**'
source_files:
    - internal/biz/authentication.go
    - internal/biz/authorization.go
    - internal/biz/audit_query.go
    - internal/service/authentication.go
    - internal/server/grpc.go
    - cmd/server/main.go
    - tests/contracts/fixtures/core_error_contract.v1.json
---

## 1. 整体方案

ani-iam 采用「领域层返回 Go error → service 层映射为 gRPC status」的分层错误模型。业务规则、校验失败、依赖不可用等语义化错误在 `internal/biz` 中以包级 `var ErrXxx = errors.New(...)` 常量形式集中声明；service 层（当前集中在 `internal/service/authentication.go`）通过 `mapIAMError` 将 biz 错误统一转换为带 `google.golang.org/grpc/status` 和 `errdetails.ErrorInfo` 的 gRPC 响应，对外暴露稳定的 `reason`（如 `CREDENTIAL_INVALID`、`AUTH_RATE_LIMITED`、`NOT_FOUND`）和结构化 metadata。

该模式使调用方仅依赖 gRPC code + reason 即可做重试/降级决策，而不感知内部实现细节。

## 2. 关键文件与职责

- `internal/biz/authentication.go`：定义认证相关领域错误（`ErrAccountRequired`、`ErrInvalidCredential`、`ErrAuthenticationRateLimited`、`ErrPrincipalInactive` 等）以及可携带附加信息的自定义错误类型 `AuthenticationRateLimitError`（实现 `Unwrap()` 以便 `errors.Is` 穿透）。
- `internal/biz/authorization.go`：定义授权领域错误（`ErrAuthorizationCredentialInvalid`、`ErrAuthorizationPolicyMismatch`、`ErrTenantIAMNotReady` 等）及 `AuthorizationPolicyMismatchError`（含 Expected/Actual 字段）。
- `internal/biz/audit_query.go`：审计查询错误（`ErrAuditEventNotFound`、`ErrAuditQueryInvalid`）。
- `internal/service/authentication.go`：核心映射器 `mapIAMError(err, errorContext)` 把 biz error 翻译为 gRPC status；`newIAMStatus(code, reason, message, metadata)` 构造带 `ErrorInfo` 的 grpc.Status；`invalidArgumentStatus(field, message)` 用于参数校验失败；`invalidArgumentField(err)` 把特定 biz error 还原为请求字段名。
- `internal/server/grpc.go`：gRPC 服务器构建入口，强制 mTLS（MinVersion TLS13 + RequireAndVerifyClientCert），未启用反射；中间件以切片注入，但仓库中未见全局 recover 中间件。
- `cmd/server/main.go`：进程启动阶段对配置加载、应用构建等致命错误使用 `panic`，由容器运行时接管；业务路径不 panic。
- `tests/contracts/fixtures/core_error_contract.v1.json`：契约测试约束了 core 域错误 reason 到 gRPC code 的固定映射（如 `INVALID_ARGUMENT`→`INVALID_ARGUMENT`、`SNAPSHOT_UNAVAILABLE`→`UNAVAILABLE` 等），作为外部契约保障。

## 3. 架构与约定

### 3.1 错误分层

| 层级 | 错误形态 | 说明 |
|---|---|---|
| biz | `errors.New` 常量 + 可选自定义 error 结构体 | 表达业务语义（凭证无效、策略版本不匹配、租户生命周期阻塞等） |
| service | `mapIAMError` 单点转换 | 根据 `errorContext.OperationID`、`Dependency`、`CredentialKind` 等上下文选择 gRPC code 与 reason |
| transport | gRPC status + `ErrorInfo` | 对外暴露稳定 reason 与结构化 metadata（如 `credential_kind`、`limit_scope`、`retry_after_seconds`） |

### 3.2 错误分类与映射规则（来自 `mapIAMError`）

- **参数校验失败**：通过 `invalidArgumentField` 识别具体字段，返回 `codes.InvalidArgument` + reason `INVALID_ARGUMENT` + metadata `field`。
- **认证失败**：`ErrInvalidCredential` / `ErrPasswordActionInvalid` / `ErrAuthorizationCredentialInvalid` 等 → `codes.Unauthenticated` + `CREDENTIAL_INVALID`，metadata 包含 `credential_kind`。
- **权限拒绝**：`ErrInvitationDenied`、`ErrTenantWorkloadDisabled`、`ErrPrincipalInactive` 等 → `codes.PermissionDenied` + `PERMISSION_DENIED`，附带 `operation_id` / `decision_id`。
- **限流**：`ErrAuthenticationRateLimited` 配合 `AuthenticationRateLimitError` → `codes.ResourceExhausted` + `AUTH_RATE_LIMITED`，metadata 含 `limit_scope`、`retry_after_seconds`。
- **幂等冲突/过期**：`ErrIdempotencyConflict` → `AlreadyExists` + `IDEMPOTENCY_CONFLICT`；`ErrIdempotencyExpired` → `FailedPrecondition` + `IDEMPOTENCY_KEY_EXPIRED`。
- **依赖不可用**：`ErrAuthenticationDependency` / `ErrAuthorizationDependency` / `ErrOIDCDependency` / `ErrPersistenceUnavailable` → `codes.Unavailable` + `IAM_UNAVAILABLE`，metadata 含 `dependency`。
- **策略版本不匹配**：`*AuthorizationPolicyMismatchError` → `codes.Unavailable` + `AUTHZ_POLICY_MISMATCH`，metadata 含 expected/actual policy revision。
- **超时**：`context.DeadlineExceeded` → `codes.DeadlineExceeded` + `IAM_TIMEOUT`。
- **资源不存在**：多种 NotFound 场景统一映射为 `NOT_FOUND`，metadata 标注 `resource_type` / `resource_id`。
- **兜底**：未知 biz error 也返回 `IAM_UNAVAILABLE`，避免泄露内部异常。

### 3.3 自定义错误类型

- `AuthenticationRateLimitError`：实现 `Error()` 与 `Unwrap()`，既支持 `errors.Is` 匹配基类错误，又保留 `LimitScope`、`RetryAfter` 供上层提取元数据。
- `AuthorizationPolicyMismatchError`：携带 Expected/Actual 字符串，用于策略版本漂移诊断。

### 3.4 进程级错误处理

- 启动阶段（`cmd/server/main.go`）对配置解析、应用构建等不可恢复错误直接 `panic`，由容器编排负责重启。
- 业务运行期不使用 `recover` 捕获 panic；测试中可见 `panic("...")` 仅用于断言某条路径不应执行（如 `panic("receipt replay executed mutation")`），属于测试内暴力的“不可能到达”标记。
- 日志通过 Kratos logger 输出 JSON 格式，并过滤敏感键（password、token、private_key 等）。

## 4. 约定与约束

1. **biz 层只返回 Go error**：所有业务错误以包级 `ErrXxx` 常量或自定义 error 类型表示，禁止在 biz 层直接构造 gRPC status。
2. **service 层是唯一的 gRPC 错误出口**：所有 biz error 必须经 `mapIAMError` 转换，确保对外 reason 稳定且可被契约测试覆盖。
3. **每个 gRPC 错误必须带 `ErrorInfo`**：通过 `newIAMStatus` 构造，包含 `Reason`、`Domain: "iam.ani.internal"` 和结构化 `Metadata`。
4. **新增 biz error 需同步更新映射表**：在 `mapIAMError` 中添加分支，并在 `invalidArgumentField` 中注册字段名（若适用），否则会被兜底为 `IAM_UNAVAILABLE`。
5. **契约测试约束 reason→code 映射**：`tests/contracts/fixtures/core_error_contract.v1.json` 定义了 core 域错误 reason 与 gRPC code 的绑定，变更需通过契约测试。
6. **mTLS 强制校验**：`NewGRPCServer` 在构造时拒绝非 TLS13+ 客户端证书验证的配置，非法配置以 `ErrInvalidMutualTLSConfiguration` 返回。
7. **未实现方法返回 Unimplemented**：生成的 stub 中对未实现方法返回 `status.Error(codes.Unimplemented, ...)`，服务可按垂直切片选择性暴露接口。
8. **进程启动错误不捕获**：`main` 中配置加载与应用构建错误直接 `panic`，由外部进程管理器处理，业务代码中无 recover 逻辑。