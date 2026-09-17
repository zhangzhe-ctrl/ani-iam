---
kind: dependency_management
name: Go 多模块依赖管理与 Buf/Protobuf 契约锁定
category: dependency_management
scope:
    - '**'
source_files:
    - go.mod
    - go.sum
    - api/go.mod
    - sdk/go.mod
    - examples/workload-grpc/go.mod
    - api/iam/v1/buf.yaml
    - api/iam/v1/buf.lock
    - migrations/atlas.sum
    - atlas.hcl
---

## 1. 使用的系统与工具

仓库采用 **Go Modules（go.mod / go.sum）** 作为唯一的依赖声明与版本锁定机制，并在根目录、`api/`、`sdk/`、`examples/workload-grpc/` 四个位置分别维护独立的 Go module，形成“服务 + API 契约 + SDK + 示例”的多模块结构。gRPC/Protobuf 契约通过 **Buf**（`buf.yaml` + `buf.lock`）进行依赖锁定与生成物校验；数据库迁移使用 **Atlas**（`atlas.hcl` + `migrations/atlas.sum`）锁定迁移集。

## 2. 关键文件

- 根 `go.mod`：定义主模块 `github.com/zhangzhe-ctrl/ani-iam`，声明 Kratos v3、gRPC v1.82.1、pgx/v5、Redis、OpenTelemetry、Argon2id、JWX 等运行时依赖，并通过注释标注私有模块来源。
- `api/go.mod`：仅依赖 gRPC/protobuf，用于发布纯 API 契约包。
- `sdk/go.mod`：依赖 `ani-iam/api` 与 gRPC，用于发布工作负载调用 SDK。
- `examples/workload-grpc/go.mod`：独立示例模块，引用 SDK 的 git commit 版本。
- `api/iam/v1/buf.yaml`：Buf 配置，将模块命名为 `buf.build/kubercloud/ani-iam`，启用 STANDARD lint 与 FILE 级 breaking change 检测。
- `api/iam/v1/buf.lock`：锁定 `buf.build/googleapis/googleapis` 的 commit 与 digest，确保 Protobuf 生成可重现。
- `migrations/atlas.sum`：Atlas 迁移摘要，锁定迁移集合。
- `.scratch/**/evidence/*`：大量重构证据文档记录 `go.mod` / `go.sum` 的 SHA-256 哈希，表明构建产物需被校验。

## 3. 架构与约定

### 3.1 多模块分层
- `api/` 是零业务逻辑的 gRPC 契约模块，供服务端与 SDK 共同消费。
- `sdk/` 是面向工作负载的 gRPC 客户端库，依赖 `api` 模块。
- 根模块是 ani-iam 服务实现，同时依赖 `api`、`sdk` 以及第三方库。
- `examples/workload-grpc` 是独立可运行示例，不反向依赖服务实现。

### 3.2 私有模块与不可变候选
根 `go.mod` 与 `sdk/go.mod` 中均出现对 `github.com/zhangzhe-ctrl/ani-iam/workloadregistry` 的 require，并附带注释 `// Unpublished immutable candidate; supplied by the checked file module proxy.`，说明该私有模块由受控的“checked file module proxy”提供，版本号形如 `v0.0.1-wr33.4`，属于内部发布的不可变候选版本，而非直接从 Git 分支拉取。

### 3.3 版本策略
- 公共依赖使用语义化版本或带时间戳的 pseudo-version（如 `v3.0.0-20260515082355-1ddb58e407c5`），保证可重现构建。
- 内部模块（`ani-iam/api`、`ani-iam/sdk`、`workloadregistry`）使用 `-wrNN.N` 后缀的预发布版本，与工作区编号绑定（如 `wr33.4`），便于在重构期间追踪来源。
- 示例模块 `examples/workload-grpc` 直接引用 SDK 的 git commit pseudo-version（`v0.0.0-20260911071951-e9f657f20b69`），体现开发期快速联调习惯。

### 3.4 Protobuf 契约锁定
`api/iam/v1/buf.yaml` 将依赖声明为 `buf.build/googleapis/googleapis`，并通过 `buf.lock` 锁定到具体 commit (`c17df5b2beca46928cc87d5656bd58e407c5`) 与 digest，确保 gRPC 代码生成不受上游 googleapis 变更影响。

### 3.5 无 vendor 目录
仓库未使用 `vendor/` 目录，所有依赖通过 `go.sum` 与 Go module proxy 解析；Buf 依赖通过 `buf.lock` 锁定，而非本地缓存。

## 4. 约定与约束

- **go.sum 必须提交且受校验**：多处重构证据文档记录 `go.mod` / `go.sum` 的 SHA-256 哈希，并在冻结基线时要求二者与预期一致，表明 CI/流程会校验依赖锁文件完整性。
- **Buf 生成的代码不得手工修改**：`buf.yaml` 启用 `STANDARD` lint 与 `FILE` 级 breaking change 检测，配合 `buf.lock` 锁定 googleapis 版本，确保 gRPC 契约变更需经 buf 验证。
- **私有模块通过受控代理发布**：`workloadregistry` 模块以 `Unpublished immutable candidate` 形式引入，禁止从任意 Git 分支直接拉取，必须由“checked file module proxy”提供固定版本。
- **Atlas 迁移集受 `atlas.sum` 锁定**：迁移脚本通过 Atlas 管理，`atlas.sum` 锁定已应用的迁移集合，防止迁移顺序漂移。
- **Go 版本跨模块统一但允许差异**：根模块使用 `go 1.26.7`，`api/`、`sdk/`、`examples/` 使用 `go 1.25.0`，表明各子模块可独立升级语言版本，但需保持兼容。
- **无 GOPRIVATE / replace 显式配置**：当前仓库未包含 `.gitconfig` 或 `go env` 级别的 `GOPRIVATE` 设置，私有模块依赖通过外部代理或 workspace 机制解决，不在源码中硬编码替换规则。
- **依赖更新需同步 go.sum 与 buf.lock**：任何第三方依赖升级都需伴随 `go mod tidy` 与 `buf dep update`（若涉及 proto 依赖），并由证据文档记录新旧哈希以供审计。