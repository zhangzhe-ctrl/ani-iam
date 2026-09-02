# 04: 接通真实旧 PG/RLS 与 Redis

**What to build:** 让兼容 Runtime 在隔离状态空间中使用真实旧 PostgreSQL role/RLS 与 Redis 语义，为纵向差分提供可信持久化行为。

**Blocked by:** 03 / 接通旧 Auth gRPC 契约

**Status:** ready-for-agent

**Plan mapping:** CP0-3

**Baseline:** 01 固定的 migration/schema/role/RLS、Redis key/TTL 与数据夹具。

**Scope:** 旧 pgx/Redis Adapter、受限 runtime role、事务、RLS、Redis namespace、TTL、Session/Blocklist/limit 状态。

**Out of scope:** P2 无 RLS Schema、sqlc 目标仓储、共享测试状态或旧生产写路径。

**Allowed paths:** `internal/compat/authv1/**`、`internal/data/**`、`internal/conf/**`、`configs/**`、`tests/cp0/**`、`go.mod`、`go.sum`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得修改旧数据库或 Redis 的权威状态。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/04-connect-real-legacy-storage/`

- [ ] 真实 runtime role 能完成允许操作，跨 Tenant RLS 负向测试被数据库拒绝。
- [ ] Redis key、TTL、状态迁移和故障行为与基线可比较。
- [ ] 测试状态与其他运行隔离，禁止超级用户、全局 flush 和残留 Fixture 依赖。
- [ ] PostgreSQL 或 Redis 不可用时返回稳定错误且不产生部分成功。

**Verification:** 真实依赖集成测试、两 Tenant 负向测试和 Redis 状态/TTL 检查通过。

**Stop conditions:** 只能用 Fake、超级用户或共享可变状态才能通过。

**Recovery:** 删除隔离数据库/角色/Redis namespace；旧 Auth 数据保持不变。
