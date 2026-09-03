# 17: 冻结 IAM 与 Core 集成契约

**What to build:** 发布目标 Authentication、Authorization、IAM Admin 以及 Core Lifecycle/Bootstrap/Snapshot 的不可变契约，使两个项目可以独立生成和固定依赖。

**Blocked by:** 15 / 观察并下线旧 Auth Runtime

**Status:** wontfix

**Superseded by:** Direct P2 DP2-03；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。需求继续存在，旧执行切片不再领取。

**Plan mapping:** P2-0

**Baseline:** 当前规格、核心方案和固定 ANI Commit。

**Scope:** 三个 IAM gRPC Service、稳定错误 detail、Core 集成消息/快照、版本策略、Artifact 摘要和 producer-consumer fixtures。

**Out of scope:** 真实 Core Publisher、NATS 基础设施、业务实现、公开 REST 第二入口。

**Allowed paths:** `api/iam/v1/**`、`tests/contracts/**`、`../ANI/repo/api/proto/tenant/**`、`../ANI/repo/pkg/generated/**` 中与目标契约对应的生成物，以及本事项和其证据目录。

**Forbidden paths:** `internal/**`、`migrations/**`、`deploy/**`、`../ANI/repo/services/**`、`../ANI/repo/deploy/**`、`../ANI/repo/api/openapi/**`；不得实现 Publisher 或业务逻辑。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/17-freeze-iam-core-contracts/`

- [ ] IAM 只公开三个内部 gRPC Service，ANI 仍是唯一公网 REST Owner。
- [ ] Lifecycle、Bootstrap、Snapshot 的身份、版本、fingerprint 和恢复字段完整。
- [ ] Breaking 变更使用明确 major 与迁移策略，不动态跟随 `main` 或 `latest`。
- [ ] ANI 和 IAM 可各自从不可变 Artifact 生成并运行契约 Fixture。

**Verification:** Buf lint/breaking、descriptor generation、跨项目 Fixture 和错误映射测试通过。

**Stop conditions:** Core canonical owner 未确认、契约无法固定或要求共享内部 Go package。

**Recovery:** 撤回未发布 Artifact，继续使用 P1 契约。
