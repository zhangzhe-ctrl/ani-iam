# 03: 冻结 IAM 与 Core 集成契约

**What to build:** 发布目标 Authentication、Authorization、IAM Admin 以及 Core Lifecycle/Bootstrap/Snapshot 的不可变契约和 producer-consumer fixtures。

**Blocked by:** 01 / 冻结 Direct P2 来源与替换基线

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-0 / internal contracts

**Baseline:** 01 人工接受的来源基线、当前 Direct P2 规格、Core/IAM 所有权和错误决定。

**Scope:** 三个 IAM gRPC Service、稳定 status/ErrorInfo、Core Lifecycle/Bootstrap 事件、Snapshot cursor/page 协议、版本策略、descriptor/digest 和跨项目 fixtures。

**Out of scope:** 真实 Core Publisher、NATS 基础设施、业务实现、公开 REST 第二入口或旧 Auth Proto 删除。

**Allowed paths:** `api/iam/v1/**`、`tests/contracts/**`、ANI 专用 worktree 的 `repo/api/proto/tenant/**`、`repo/pkg/generated/**` 中对应生成物，以及本事项和证据目录；人工检查点另行批准 `repo/development-records/DP2-03-iam-core-integration-contracts.md`、`repo/development-records/README.md`、`repo/CURRENT-SPRINT.md`、`ANI-06-开发计划.md` 四个 ANI 文档闭环路径。

**Forbidden paths:** `internal/**`、`migrations/**`、`deploy/**`、`../ANI/repo/services/**`、`../ANI/repo/deploy/**`、`../ANI/repo/api/openapi/**`；不得实现 Publisher、Consumer 或业务逻辑。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/`

- [ ] IAM 只注册目标三个 gRPC Service，旧 `auth.v1.AuthService` 不进入目标 descriptor。目标 descriptor inventory 为 `pass`；运行时 registration 为 `not_verified`，因为 `internal/server/**` 不在本事项范围。
- [x] Core 独占 Tenant ID/Lifecycle；IAM 契约不允许写回 Lifecycle 或共享数据库。
- [x] Lifecycle version、Bootstrap fingerprint、Snapshot cursor 和错误语义完整。
- [x] producer-consumer fixtures 可由两个项目独立生成和验证。
- [x] 契约版本、descriptor hash 和依赖固定方式可审查。

**Verification:** Buf lint/breaking、descriptor generation、跨项目 fixtures、错误 detail 和 artifact hash 检查通过。

**Stop conditions:** Core canonical owner 未确认；契约不能固定；需要共享内部 Go package、跨数据库事务或第二公网入口。

**Recovery:** 撤回未发布 Artifact，保留现有运行契约；不执行调用方切换。

**Human checkpoint:** 2026-09-04 已人工接受精确 Proto/Core contract diff，并批准四个 ANI Feature-batch 文档闭环路径。

## Claim record

- Claimed at: 2026-09-04 (Asia/Shanghai)
- Dependency status: DP2-01 `resolved`; DP2-02 `resolved`; no other Direct P2 ticket was `claimed` at claim time.
- IAM start: branch `main`, commit `5ff9f3cfe083b3b911bb076450abbbb967e82a37`, clean worktree/index.
- ANI start: dedicated worktree `/home/chabking/workspace/ANI-direct-p2-01-05`, branch `codex/direct-p2-01-05`, commit `a221a7b50c2cfdb13f04c13f154338d836a48af3`, clean worktree/index. The existing `/home/chabking/workspace/ANI/repo` checkout remains out of scope.
- Dependency/tool baseline: Go `go1.26.7-X:nodwarf5 linux/amd64`; `protoc-gen-go v1.36.12`; `protoc-gen-go-grpc 1.6.2`; IAM `go.mod` SHA-256 `6cac45c0f25d023b150beb8c97d3a64b2481e53385e19a31045852bebbc1ef8e`; IAM `go.sum` SHA-256 `1b41b52ad891bdb1d3dd04b2b7790eb810a46dbc2d0f22aebfde6e66ef2decc5`. System `buf` and `protoc` were absent at claim time, so exact versions and task-owned installation paths must be pinned before generation.
- Allowed paths: IAM `api/iam/v1/**`, `tests/contracts/**`, this ticket and `.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/**`; ANI `repo/api/proto/tenant/**`, corresponding `repo/pkg/generated/**` outputs, and the four exact Feature-batch documentation paths approved at the human checkpoint. No other expansion was used.

## Execution plan

1. Inventory both fixed starting commits, existing Proto/generator conventions and accepted IAM/Core ownership/error semantics; pin exact Buf/Protobuf plugins, inputs and generation commands in task-owned tooling.
2. Establish contract-first failing gates for service inventory, stable gRPC/ErrorInfo semantics, lifecycle/bootstrap/snapshot completeness, version/cursor/fingerprint rules, producer-consumer fixtures and forbidden legacy/shared-boundary behavior.
3. Add the minimum canonical IAM and Core source contracts, then generate descriptors and language outputs only with the pinned commands; generated source and output remain in the same future commit.
4. Independently validate fixtures from each repository, run Buf lint/breaking, descriptor inventory/hash, deterministic regeneration, biz import-boundary and relevant two-repository regressions.
5. Stage only approved paths, review the exact breaking diff and required ANI documentation closure, then pause before either breaking commit or cross-project pinning.

## Test plan

- Buf lint and breaking checks against immutable pre-ticket inputs; descriptor generation plus exact three-service/method inventory and absence of `auth.v1.AuthService`.
- Stable gRPC status and `google.rpc.ErrorInfo` reason/domain/metadata fixture tests.
- Lifecycle monotonic version, Bootstrap operation/payload fingerprint, Snapshot cursor/page consistency, schema-major compatibility and error-case fixtures.
- Producer and consumer fixture generation/validation run independently in IAM and ANI without shared internal Go packages or a shared database.
- Deterministic regeneration and descriptor SHA-256 checks; source/generated clean-diff checks; IAM biz import-boundary tests and relevant repository regressions.

## Recovery plan

Human acceptance was received before either contract commit. Recovery now uses new revert commits in reverse dependency order: ANI consumer/Core contract commit `573d3735934f74f9f1eb78818cddefefd9f575eb`, then IAM producer contract commit `1bdc3e3657c233b5a47be706f251a4529ec80b5b`. Never reset, stash, amend, force, push, publish, deploy, start a real Core publisher, create NATS infrastructure or switch callers.

## Verification summary

- `pass`: Buf lint for IAM and scoped Core package.
- `pass`: Buf breaking against a same-path empty IAM target baseline and the complete immutable ANI Proto module at `a221a7b...`.
- `pass`: exact IAM descriptor inventory (3 services / 69 methods), Core descriptor inventory (1 service / 2 methods), and legacy service absence.
- `pass`: independent strict fixture decoding, exhaustive stable gRPC/ErrorInfo rules, bootstrap fingerprint, immutable hashes, and byte-identical fixture/pin copies.
- `pass`: deterministic second generation, IAM full Go regression/vet, ANI generated/Gateway/Auth regression/vet, and ANI architecture gate.
- `fail`: ANI aggregate `make test` stops in the already accepted DP2-02 legacy operationId gate (`logout`, `revokeAPIKey`); target operation IDs remain `logoutSession`, `revokeIAMAPIKey`.
- `not_verified`: runtime registration, real Core publisher/NATS/snapshot server/IAM consumer, public-operation-to-RPC runtime mapping, publication, deployment, and caller cutover.
- `pass`: 2026-09-04 human acceptance covered the exact Proto/Core diff and the four mandatory ANI Feature-batch documentation paths.
- Local artifact commits: ani-iam `1bdc3e3657c233b5a47be706f251a4529ec80b5b`; ANI `573d3735934f74f9f1eb78818cddefefd9f575eb`.
