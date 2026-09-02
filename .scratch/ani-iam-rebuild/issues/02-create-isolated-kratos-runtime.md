# 02: 建立隔离 Kratos 运行骨架

**What to build:** 建立一个可启动、可观测且与旧 Auth 写路径隔离的 Kratos v3 运行骨架，为兼容性实验提供稳定外壳。

**Blocked by:** 01 / 固定兼容性 Oracle 与替代检查

**Status:** ready-for-agent

**Plan mapping:** CP0-0

**Baseline:** 使用 01 接受的 Commit、依赖版本和 CP0 范围。

**Scope:** 固定 Kratos patch、标准分层、typed config、显式构造、gRPC 生命周期、内部 health/readiness/metrics、依赖与许可证证据。

**Out of scope:** 旧 Auth RPC 业务、目标 P2 契约、Schema、NATS、调用方切流。

**Allowed paths:** `go.mod`、`go.sum`、`cmd/server/**`、`internal/biz/**`、`internal/data/**`、`internal/service/**`、`internal/server/**`、`internal/conf/**`、`configs/**`、`tests/cp0/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得在允许目录内实现旧 Auth 或 P2 业务。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/`

- [ ] 进程可在隔离配置下启动、就绪、优雅停止并暴露内部健康状态。
- [ ] 依赖版本与校验和固定，许可证、SBOM 和已知漏洞检查有实际结果。
- [ ] 业务层不依赖 Kratos、Proto、数据库驱动或 transport error。
- [ ] 运行配置不能指向旧 Auth 的唯一写路径。

**Verification:** 构建、单元测试、启动/停止与健康探针通过；未运行检查明确标为 `not_verified`。

**Stop conditions:** Kratos 版本或许可证不可接受，或隔离只能通过复用旧写环境实现。

**Recovery:** 删除隔离运行实例和临时资源，不改变旧 Auth 服务。
