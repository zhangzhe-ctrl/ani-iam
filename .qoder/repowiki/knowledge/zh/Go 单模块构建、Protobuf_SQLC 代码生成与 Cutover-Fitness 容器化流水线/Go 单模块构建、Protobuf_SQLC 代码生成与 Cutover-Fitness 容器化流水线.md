---
kind: build_system
name: Go 单模块构建、Protobuf/SQLC 代码生成与 Cutover-Fitness 容器化流水线
category: build_system
scope:
    - '**'
source_files:
    - go.mod
    - api/iam/v1/buf.yaml
    - api/iam/v1/buf.gen.yaml
    - sqlc.yaml
    - atlas.hcl
    - migrations/atlas.sum
    - deploy/cutover-fitness/ani-iam.Dockerfile
    - deploy/cutover-fitness/current-auth.Dockerfile
    - deploy/cutover-fitness/gateway.Dockerfile
    - deploy/cutover-fitness/probe.Dockerfile
    - tools/wr20/generate.sh
    - tools/cutover-fitness/source_gate.sh
---

## 1. 构建系统总览

本仓库以 **单一 Go module**（`github.com/zhangzhe-ctrl/ani-iam`，Go 1.26.7）组织 ani-iam 服务、gRPC SDK、示例程序与测试；通过 `buf` 生成 gRPC/protobuf 代码，通过 `sqlc` 从 SQL 迁移生成 pgx/v5 查询层，通过 `atlas` 管理 PostgreSQL 迁移。二进制由 Dockerfile 多阶段构建产出，割接演练环境使用独立的 Kubernetes YAML 模板 + Dockerfile 编排 current/target 双栈。

仓库根目录没有 Makefile；本地开发/CI 通过直接调用 `go build` / `go test` / `buf generate` / `sqlc generate` / `atlas migrate hash` 等命令完成，这些命令被封装在 `tools/wr20/generate.sh`、`tools/cutover-fitness/source_gate.sh` 以及 `.scratch/...` 下的历史工作区脚本中。

## 2. 关键文件与工具

| 职责 | 关键路径 | 说明 |
|---|---|---|
| Go 模块清单 | `go.mod`, `go.sum` | 声明主模块、`api`、`sdk`、`workloadregistry` 等内部子模块依赖（版本形如 `v0.0.1-wr33.4`），并锁定第三方依赖 |
| gRPC 契约 | `api/iam/v1/*.proto`, `api/iam/v1/buf.yaml`, `api/iam/v1/buf.gen.yaml`, `api/iam/v1/buf.lock` | 使用 buf v2 模块 `buf.build/kubercloud/ani-iam`，启用 STANDARD lint 与 FILE 级 breaking change 检测，排除 PACKAGE_DIRECTORY_MATCH |
| SQL 代码生成 | `sqlc.yaml`, `internal/data/queries/*.sql`, `internal/data/sqlcgen/*` | sqlc v1.31.1，engine=postgresql，schema=`migrations`，输出包 `sqlcgen`，使用 pgx/v5，uuid→`google/uuid.UUID`、timestamptz→`time.Time` 覆盖 |
| 数据库迁移 | `migrations/*.sql`, `migrations/atlas.sum`, `atlas.hcl` | Atlas 配置 env `dp2_04` 读取 `DATABASE_URL`，迁移目录为 `file://migrations`；`atlas.sum` 校验迁移哈希 |
| 服务镜像 | `deploy/cutover-fitness/ani-iam.Dockerfile` | 多阶段：golang@sha256:… 编译 → alpine@sha256:… 运行；`CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"` 输出 `/usr/local/bin/ani-iam`，以 UID 65532 非 root 用户运行，暴露 19090/19091 |
| 旧版服务镜像 | `deploy/cutover-fitness/current-auth.Dockerfile`, `gateway.Dockerfile` | 基于 `go work init ./runtimeadmin ./pkg ./services/auth-service` 的旧 monorepo 结构构建，同样使用固定 sha256 基础镜像与非 root 用户 |
| 探针镜像 | `deploy/cutover-fitness/probe.Dockerfile` | Python 基础镜像，入口 `python3 /opt/cf01/probe.py` |
| 可重复代码生成 | `tools/wr20/generate.sh` | 强制 `go version = go1.26.7`、`sqlc version = v1.31.1`、`buf --version = 1.72.0`；依次执行 `atlas migrate hash`、`sqlc generate`、`buf generate`、`buf build iam_descriptor.pb`，并对生成产物做双向重复性校验 |
| 割接源码门禁 | `tools/cutover-fitness/source_gate.sh` | 校验 ANI/IAM 仓库快照、overlay patch 与 projected tree 的 SHA256 是否匹配预期常量 |

## 3. 架构与约定

### 3.1 分层与代码生成边界
- `api/iam/v1/` 仅包含 proto 定义与生成的客户端/类型，不包含服务端实现；服务端通过 Kratos 在 `internal/server/grpc.go` 中注册。
- `internal/conf/conf.proto` 也通过 buf 生成 `conf.pb.go`（见 `generate.sh` 中对 `internal/conf` 的 buf generate 调用）。
- `internal/data/queries/*.sql` 是单一数据源，sqlc 生成只读接口与模型到 `internal/data/sqlcgen/`，业务层通过该包访问 Postgres。
- 迁移按时间戳前缀命名（如 `202609040001_persistence_foundation.sql`），由 atlas 顺序应用。

### 3.2 构建产物与发布形态
- 每个可部署组件一个 Dockerfile：`ani-iam.Dockerfile`（新 IAM）、`current-auth.Dockerfile`（旧 auth-service）、`gateway.Dockerfile`（ANI gateway）、`probe.Dockerfile`（Python 探针）。
- 所有镜像均使用固定 sha256 的基础镜像（golang、alpine、python），并通过 `--mount=type=cache,target=/go/pkg/mod` 与 `target=/root/.cache/go-build` 缓存 Go 模块与构建缓存。
- 二进制以 `-trimpath -ldflags "-s -w"` 剥离调试信息，并以非特权用户 `ani` (UID 65532) 运行。

### 3.3 可重复性与确定性
- `tools/wr20/generate.sh` 将源码 tarball 解压两次（`source` 与 `repeat-source`），分别执行完整代码生成流程，然后逐文件比对字节级一致性，确保 sqlc/buf/atlas 的输出完全确定。
- `contract_pins.json` 记录 `iam_descriptor.pb` 的 SHA256，作为 gRPC 契约变更门禁。
- `atlas.sum` 锁定迁移集合的哈希，防止迁移漂移。

### 3.4 测试与验证
- 单元测试：`go test ./internal/... ./cmd/server/... ./tests/contracts/...`（参考 `.scratch/...` 中的 unit-command.sh）。
- 集成测试：`go test -tags integration ./tests/integration`，使用 testcontainers 启动真实 Postgres/Redis/Dex。
- 契约测试：`tests/contracts/contracts_test.go` 校验消息夹具与运行时行为。
- 远程重构工作区：`tools/wr17-18/`、`wr19/`、`wr20/` 提供 `remote-run.py`、`stage-a.sh`、`verify-ab.sh` 等脚本，用于在隔离环境中执行受控的单任务构建/测试/验证。

## 4. 约定与约束

- **Go 版本锁定**：`go.mod` 声明 `go 1.26.7`，`tools/wr20/generate.sh` 用 `test "$(go env GOVERSION)" = go1.26.7` 强制 CI 环境一致。
- **工具版本锁定**：`generate.sh` 显式断言 `sqlc version = v1.31.1`、`buf --version = 1.72.0`，保证代码生成可重复。
- **无 CGO**：Dockerfile 设置 `CGO_ENABLED=0`，确保静态链接、跨平台可移植。
- **最小镜像**：Alpine 运行时镜像，仅复制编译产物，不携带 Go 工具链。
- **非 root 运行**：所有镜像创建 UID 65532 的 `ani` 用户并切换。
- **Buf 契约治理**：`api/iam/v1/buf.yaml` 启用 STANDARD lint 与 FILE-level breaking change 检测，禁止破坏性 proto 变更。
- **迁移不可变**：通过 `atlas.sum` 校验迁移集合，新增迁移必须追加而非修改已有文件。
- **端口约定**：`ani-iam.Dockerfile` 暴露 19090/19091；`current-auth.Dockerfile` 暴露 9101/9201；`gateway.Dockerfile` 暴露 8080/9200。
- **Kubernetes 资源模板**：`deploy/cutover-fitness/` 下 `*.yaml.tmpl` 与 `*.yaml` 描述命名空间、存储、网络策略与 Pod 编排，配合 `current-auth`/`ani-iam`/`gateway` 镜像组成割接演练环境。
- **无全局 Makefile**：构建入口分散在各 Dockerfile 与 `tools/*/` 脚本中，本地开发通常直接调用 `go build` / `go test` / `buf generate` / `sqlc generate` / `atlas migrate`。
