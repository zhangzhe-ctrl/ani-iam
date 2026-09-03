# 04: 接通真实旧 PG/RLS 与 Redis

**What to build:** 让兼容 Runtime 在隔离状态空间中使用真实旧 PostgreSQL role/RLS 与 Redis 语义，为纵向差分提供可信持久化行为。

**Blocked by:** 03 / 接通旧 Auth gRPC 契约

**Status:** resolved

**Plan mapping:** CP0-3

**Baseline:** 01 固定的 migration/schema/role/RLS、Redis key/TTL 与数据夹具。

**Scope:** 旧 pgx/Redis Adapter、受限 runtime role、事务、RLS、Redis namespace、TTL、Session/Blocklist/limit 状态。

**Out of scope:** P2 无 RLS Schema、sqlc 目标仓储、共享测试状态或旧生产写路径。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/conf/**`、`configs/**`、`tests/cp0/**`、`go.mod`、`go.sum`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得修改旧数据库或 Redis 的权威状态。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/04-connect-real-legacy-storage/`

- [ ] 真实 runtime role 能完成允许操作，跨 Tenant RLS 负向测试被数据库拒绝。
- [x] Redis key、TTL、状态迁移和故障行为与基线可比较。
- [x] 测试状态与其他运行隔离，禁止超级用户、全局 flush 和残留 Fixture 依赖。
- [x] PostgreSQL 或 Redis 在写入前不可用时返回稳定错误且不产生部分成功。
- [ ] PostgreSQL commit 失败且 Redis 补偿删除同时失败时不产生部分成功：`not_verified`，实验实现忽略了补偿删除错误。

**Verification:** 真实依赖集成测试、两 Tenant 负向测试和 Redis 状态/TTL 检查通过。

**Stop conditions:** 只能用 Fake、超级用户或共享可变状态才能通过。

**Recovery:** 删除隔离数据库/角色/Redis namespace；旧 Auth 数据保持不变。

## Result

`FAIL / BLOCKED`。证据索引位于 `.scratch/ani-iam-rebuild/evidence/04-connect-real-legacy-storage/index.md`。

- 固定 PostgreSQL/Redis 镜像、冻结 migration 全量回放、受限 runtime role、独立 Redis namespace、三类旧 key/TTL 和依赖失败回滚 harness 已建立。
- 冻结 migration 对 `api_keys`/`refresh_tokens` 启用并强制 RLS，但最终只有 `AS RESTRICTIVE` policy，没有任何 permissive policy。
- 真实 `ani_app_user` 在设置匹配的 `app.current_tenant_id` 后插入同 Tenant `refresh_tokens` 仍返回 PostgreSQL `42501`；因此正向允许操作失败，两 Tenant deny 不能被记作有效隔离通过。
- Redis 与“依赖在写入前不可用不产生部分成功”子测试通过；数据库 commit 失败且 Redis 补偿也失败的组合为 `not_verified`。全量真实依赖 gate 仍为 `fail`。
- 继续只能修改冻结 migration/schema、增加临时 permissive policy 或使用 owner/superuser/BYPASSRLS，分别违反固定 Oracle 或本事项停止条件。该负向调查已经完成；结果保持 `FAIL / BLOCKED`，不冒充兼容门禁通过。
- 本轮隔离容器已显式终止，`docker ps` 为空；没有修改 ANI、旧数据库、部署或调用方。

## Comments

- 2026-09-03：用户明确指示“启动事项04”；事项03已 `resolved`，且未发现其他 `claimed` 状态变更事项，因此领取本事项。实施严格限制在本事项 Allowed paths；仅允许写入每轮独立 PostgreSQL database/runtime role 与 Redis namespace，不修改旧权威状态，不使用超级用户执行业务查询，不执行全局 Redis flush，不部署、不切流。
- 2026-09-03：真实固定镜像门禁触发停止条件。冻结 RLS 只有 restrictive policy，导致受限 `ani_app_user` 的同 Tenant 正向写入也被 `42501` 拒绝。未添加临时 policy、未改用高权限角色、未修改冻结 ANI migration；事项保持 `claimed`，等待人工选择“先修复并重新冻结基线”或“修改 CP0 兼容目标”。
- 2026-09-03：用户明确指示“关闭事项04的负向调查结果并重排计划”。据此将事项设为 `resolved`，含义仅为调查与证据闭环；RLS 正向门禁仍为 `fail`，CP0 未获得 Go。后续计划重排由独立事项承载，不扩大本事项路径范围。
