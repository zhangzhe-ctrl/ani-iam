# 04: 建立无 RLS 持久化基础

**What to build:** 建立可从空库重放、由受限 runtime role 使用的目标 PostgreSQL 基础，为纵向链路提供显式 Tenant 隔离、事务和 Audit 原子性。

**Blocked by:** 02 / 冻结公开 OpenAPI 与 Operation Registry；03 / 冻结 IAM 与 Core 集成契约

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-1 / persistence foundation

**Baseline:** 02/03 接受的目标契约、当前数据不变量和独立 Direct P2 数据库边界。

**Scope:** Atlas versioned migrations、sqlc/pgx、migration/runtime role 分离、UnitOfWork/Audit、UUIDv7、version、复合 Tenant key/FK、两 Tenant fixtures 和 query mutation 基础。

**Out of scope:** PostgreSQL RLS、通用 soft delete、完整领域表集、生产备份/HA、旧数据迁移或共享旧 Auth 数据库。

**Allowed paths:** `migrations/**`、`internal/data/**`、`internal/biz/**` 中 Scope/UoW/ports、sqlc/Atlas 配置、`tests/integration/**`、`go.mod`、`go.sum`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`internal/service/**`、`deploy/**`、`../ANI/repo/**`；不得添加 RLS、可空 Tenant、平台 Boolean bypass、业务 superuser 或 owner-role DML。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/04-build-no-rls-persistence/`

- [ ] 空库 migration replay、`atlas.sum` 和 sqlc clean diff 可复现。
- [ ] runtime role 非 owner、非 superuser、无 BYPASSRLS，仅有目标 DML 权限。
- [ ] Tenant-owned 表使用显式 `tenant_id` 和复合约束，跨 Tenant 访问被应用 SQL/约束拒绝。
- [ ] mutation 与 Security Audit 同事务提交或共同回滚。
- [ ] query mutation 能证明删除 Tenant predicate 会触发测试失败。

**Verification:** Atlas replay/checksum、sqlc clean diff、受限 role 真实 PostgreSQL integration、两 Tenant 正负向、事务回滚和 query mutation 通过。

**Stop conditions:** 需要 RLS、superuser、owner role、可空 Tenant、Boolean bypass 或共享旧表才能工作。

**Recovery:** 删除本事项创建的独立数据库/role/fixture；不影响旧 Auth 或其他测试数据。
