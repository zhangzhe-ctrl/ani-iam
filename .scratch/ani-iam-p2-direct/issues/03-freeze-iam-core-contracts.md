# 03: 冻结 IAM 与 Core 集成契约

**What to build:** 发布目标 Authentication、Authorization、IAM Admin 以及 Core Lifecycle/Bootstrap/Snapshot 的不可变契约和 producer-consumer fixtures。

**Blocked by:** 01 / 冻结 Direct P2 来源与替换基线

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-0 / internal contracts

**Baseline:** 01 人工接受的来源基线、当前 Direct P2 规格、Core/IAM 所有权和错误决定。

**Scope:** 三个 IAM gRPC Service、稳定 status/ErrorInfo、Core Lifecycle/Bootstrap 事件、Snapshot cursor/page 协议、版本策略、descriptor/digest 和跨项目 fixtures。

**Out of scope:** 真实 Core Publisher、NATS 基础设施、业务实现、公开 REST 第二入口或旧 Auth Proto 删除。

**Allowed paths:** `api/iam/v1/**`、`tests/contracts/**`、`../ANI/repo/api/proto/tenant/**`、`../ANI/repo/pkg/generated/**` 中对应生成物，以及本事项和证据目录。

**Forbidden paths:** `internal/**`、`migrations/**`、`deploy/**`、`../ANI/repo/services/**`、`../ANI/repo/deploy/**`、`../ANI/repo/api/openapi/**`；不得实现 Publisher、Consumer 或业务逻辑。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/`

- [ ] IAM 只注册目标三个 gRPC Service，旧 `auth.v1.AuthService` 不进入目标 descriptor。
- [ ] Core 独占 Tenant ID/Lifecycle；IAM 契约不允许写回 Lifecycle 或共享数据库。
- [ ] Lifecycle version、Bootstrap fingerprint、Snapshot cursor 和错误语义完整。
- [ ] producer-consumer fixtures 可由两个项目独立生成和验证。
- [ ] 契约版本、descriptor hash 和依赖固定方式可审查。

**Verification:** Buf lint/breaking、descriptor generation、跨项目 fixtures、错误 detail 和 artifact hash 检查通过。

**Stop conditions:** Core canonical owner 未确认；契约不能固定；需要共享内部 Go package、跨数据库事务或第二公网入口。

**Recovery:** 撤回未发布 Artifact，保留现有运行契约；不执行调用方切换。

**Human checkpoint:** 发布 breaking Proto/Core 契约或让其他项目固定消费前，需要针对精确 Artifact 的人工确认。
