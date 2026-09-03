# 03: 接通旧 Auth gRPC 契约

**What to build:** 让隔离 Kratos Runtime 注册并接收冻结的旧 Auth 14 RPC，同时保持 wire、deadline 和错误边界可比较。

**Blocked by:** 02 / 建立隔离 Kratos 运行骨架

**Status:** resolved

**Plan mapping:** CP0-1

**Baseline:** 01 的旧 Auth descriptor 与 02 的固定 Kratos Runtime。

**Scope:** 旧 Proto transport、14 RPC 注册清单、兼容边界、deadline、gRPC status/detail 和公共错误映射。

**Out of scope:** 目标 IAM Proto、业务语义重写、目标数据模型、调用方切流。

**Allowed paths:** `internal/compat/authv1/**`、`internal/service/**`、`internal/server/**`、`tests/cp0/**`、`go.mod`、`go.sum`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、`internal/biz/**` 的目标业务语义修改。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/03-connect-legacy-auth-grpc/`

- [x] 冻结 descriptor 与基线无未批准差异，14 RPC 均可被发现和调用。
- [x] deadline、取消、未知方法和错误 detail 的外部行为有契约测试。
- [x] 兼容 Proto 被隔离在兼容边界，目标业务层不导入它。
- [x] 未实现业务返回明确兼容错误，不出现默认成功或框架错误泄漏。

**Verification:** descriptor diff、RPC inventory 和 transport contract suite 通过。

**Stop conditions:** wire drift 无法隔离，或修复要求提前修改旧契约。

**Recovery:** 回退隔离 Runtime 的 transport 注册，不影响旧服务。

## Result

`PASS / RESOLVED`。隔离 Kratos runtime 已接通冻结的 `auth.v1.AuthService` transport，事项证据索引位于 `.scratch/ani-iam-rebuild/evidence/03-connect-legacy-auth-grpc/index.md`。

- 从 ANI 固定 Git object 提取的 Auth/Common Proto 与 compat source 构建出的 descriptor set 二进制一致，SHA-256 均为 `64f73f4c…b0`；Auth source SHA-256 仍为事项01冻结的 `aabcc72b…33ec`。
- 14 个冻结 full method 均通过真实 loopback gRPC client 到达注册 handler；未实现业务统一返回 `Unimplemented`，并携带稳定 `google.rpc.ErrorInfo`，没有默认成功或框架默认错误消息。
- 调用方取消、调用方 deadline、Kratos server timeout 和未知方法的外部状态码契约测试通过。
- compat source/generated code 只位于 `internal/compat/authv1/**`；目标 `internal/biz` 不导入兼容 Proto，并有负向 import gate。
- `buf lint`、descriptor diff、可复现 codegen、`go test -count=1 -p=1 ./...`、`go vet -p=1 ./...`、trimmed build、`go mod verify` 与 `git diff --check` 通过。
- 真实旧业务、PostgreSQL/RLS、Redis、Dex、三调用方 live E2E、部署和切流保持 `not_verified`；这些不属于事项03。

## Comments

- 2026-09-03：用户明确指示“启动事项03”；事项02已 `resolved`，且未发现其他 `claimed` 状态变更事项，因此领取本事项。实施严格限制在本事项 Allowed paths，不修改冻结旧契约、目标业务语义、数据库、部署、ANI 仓库或调用方切流。
- 2026-09-03：CP0-1 transport 验收和证据完成，事项转为 `resolved`。这只解除事项04依赖，不自动领取或启动事项04；没有修改 ANI、接入真实旧存储、启动外部依赖、部署或切流。
