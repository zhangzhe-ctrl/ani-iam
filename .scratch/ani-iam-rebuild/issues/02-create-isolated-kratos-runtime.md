# 02: 建立隔离 Kratos 运行骨架

**What to build:** 建立一个可启动、可观测且与旧 Auth 写路径隔离的 Kratos v3 运行骨架，为兼容性实验提供稳定外壳。

**Blocked by:** 01 / 固定兼容性 Oracle 与替代检查

**Status:** resolved

**Plan mapping:** CP0-0

**Baseline:** 使用 01 接受的 Commit、依赖版本和 CP0 范围。

**Scope:** 使用固定 Kratos CLI 生成 scaffold 基线，全面盘点 Kratos v3 已有的 lifecycle、logging、configuration、transport、middleware、health 和 observability 组件；所有适用于 CP0-0 外层运行时的能力优先采用 Kratos 官方组件，再按 accepted ADR 裁剪，并补齐采用/未采用/理由及验证证据。

**Out of scope:** 旧 Auth RPC 业务、目标 P2 契约、Schema、NATS、调用方切流。

**Allowed paths:** `go.mod`、`go.sum`、`cmd/server/**`、`internal/biz/**`、`internal/data/**`、`internal/service/**`、`internal/server/**`、`internal/conf/**`、`configs/**`、`tests/cp0/**`、`CLAUDE.md`、`docs/agents/scaffolding-and-codegen.md`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得在允许目录内实现旧 Auth 或 P2 业务。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/`

- [x] 进程可在隔离配置下启动、就绪、优雅停止并暴露内部健康状态。
- [x] 依赖版本与校验和固定，许可证、SBOM 和已知漏洞检查有实际结果。
- [x] 业务层不依赖 Kratos、Proto、数据库驱动或 transport error。
- [x] 运行配置不能指向旧 Auth 的唯一写路径。

**Verification:** 构建、单元测试、启动/停止与健康探针通过；未运行检查明确标为 `not_verified`。

**Stop conditions:** Kratos 版本或许可证不可接受，或隔离只能通过复用旧写环境实现。

**Recovery:** 删除隔离运行实例和临时资源，不改变旧 Auth 服务。

## Result

`PASS / RESOLVED`。隔离 Kratos v3 CP0-0 runtime 已按官方 CLI 基线完成组件级全面整改，证据索引位于 `.scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/index.md`，采用/未采用/剩余薄自实现清单位于 `component-coverage.md`。

- 基线由 `kratos new iam-service` 生成；固定 CLI module、binary hash、`kratos-layout@59ad406328acba9a70c9e7f426720a75a89a6b9f`、原始关键文件摘要和 `保留/删除/替换` 差异。
- 外层 runtime 采用 Kratos App lifecycle、file/env config、`log.NewHandler`、gRPC/HTTP transport、默认 gRPC health、errors/codec，以及 recovery、metadata、logging、validate middleware；observability 采用 Kratos contrib OTel tracing/metrics。
- 修复两处复核发现的绕行：automaxprocs 不再输出独立标准库日志；三个手写 admin route 不再绕过 Kratos middleware/codec/error encoder。
- 组件审计明确记录 registry/client/auth/rate-limit/TLS/pprof/OTLP exporter 等当前不适用项及原因；剩余自实现只限 HTTP readiness 状态、三条 admin 路由薄适配和项目隔离校验。
- typed config 由 `protoc-gen-go v1.36.11` 生成；Buf lint 通过，固定输入复跑前后 `conf.pb.go` SHA-256 一致。
- `GOPROXY=off go test -count=1 -p=1 ./...`、`go vet -p=1 ./...`、`go build -p=1 -trimpath`、`go mod verify`、独立启动/探针/请求日志/指标/SIGINT 停止均通过；recovery、metadata、validation 有直接集成测试。
- 修复模板依赖带入的 gRPC `GO-2026-6061` 与 OTel `GO-2026-5158`；最终 `govulncheck@v1.7.0 -show verbose ./...` 为 `No vulnerabilities found.`。
- CycloneDX SBOM 包含 34 个第三方运行时组件，全部识别 SPDX 许可证；根模块自身许可证仍由项目 Owner 决定。
- 生成器优先和 Kratos core/contrib 优先纪律已写入 `CLAUDE.md`；详细 generator 流程位于 `docs/agents/scaffolding-and-codegen.md`。
- Go 1.25.7 原生 runner、额外 lint、Race、Fuzz、专项并发、性能和生产就绪门禁保持 `not_verified`。

## Comments

- 2026-09-02：用户明确指示“启动事项02”；事项01已 `resolved` 且人工检查点已通过，未发现其他 `claimed` 状态变更事项，因此领取本事项。实施严格限制在本事项 Allowed paths，不修改 ANI、旧 Auth、契约、数据库、部署或外部运行环境。
- 2026-09-02：CP0-0 验收与供应链证据完成，事项转为 `resolved`。这只解除事项03的依赖，不自动领取或启动事项03。
- 2026-09-02：用户指出现有 runtime 并非由 Kratos CLI scaffold 初始化。Agent 复核确认此前直接手工拼装并错误宣称 scaffold 完成；用户明确要求整改后再将精简执行纪律写入对应文件。本事项重新领取，允许范围随当前用户决定增加 `CLAUDE.md` 与 `docs/agents/scaffolding-and-codegen.md`，原 Result 在新一轮验证完成前失效。
- 2026-09-02：整改完成并重新验证，事项恢复为 `resolved`。这只解除事项03依赖，不自动领取或启动事项03；没有 commit、push、部署或外部切流。
- 2026-09-02：用户复核日志后要求全面整改，明确规定所有可用的 Kratos 组件必须采用，并要求列明采用项、未采用项及理由。本事项再次领取；上一版 Result 在组件级审计和重新验证完成前失效。
- 2026-09-02：组件级整改与最终复验完成，事项恢复为 `resolved`。没有领取事项03，没有 commit、push、部署、外部依赖接入或切流。
- 2026-09-03：用户明确要求“先提交一版到远端”，授权将事项01/02当前已验收成果 commit 并 push 到现有 `origin/main`；为承载 Git 状态变更，本事项临时重新领取，发布不授权部署、切流或启动事项03。
- 2026-09-03：发布前 `git diff --cached --check`、暂存范围和高置信凭据模式扫描通过；事项在创建已验收版本前恢复为 `resolved`。
