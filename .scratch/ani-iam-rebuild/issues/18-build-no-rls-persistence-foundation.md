# 18: 建立无 RLS 持久化基础

**What to build:** 建立可从空库重放、使用受限 Runtime Role 的 P2 PostgreSQL 基础，并为后续纵向切片提供显式 Tenant 隔离测试能力。

**Blocked by:** 17 / 冻结 IAM 与 Core 集成契约

**Status:** wontfix

**Superseded by:** Direct P2 DP2-04；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。需求继续存在并被前置。

**Plan mapping:** P2-1

**Baseline:** 17 发布的目标契约和当前数据不变量；使用独立 P2 数据库。

**Scope:** Atlas、sqlc/pgx、migration/runtime role 分离、事务/UoW、UUIDv7、version、复合 Tenant key/FK、两 Tenant 测试工具。

**Out of scope:** 具体领域完整表集、RLS、通用 soft delete、生产备份与 HA。

**Allowed paths:** `migrations/**`、`internal/data/**`、`internal/biz/**` 中的 Scope/port、sqlc/Atlas 配置、`tests/integration/**`、`go.mod`、`go.sum`，以及本事项和其证据目录。

**Forbidden paths:** `api/**`、`internal/service/**`、`deploy/**`、`../ANI/repo/**`；不得添加 RLS、可空 Tenant 或平台 Boolean bypass。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/18-build-no-rls-persistence-foundation/`

- [ ] 空数据库可重放全部基础 migration，checksum 与 schema revision 可验证。
- [ ] Runtime Role 非 owner/非 superuser，只能执行明确 DML，不能迁移 Schema。
- [ ] Tenant Repository 必须接收不可提升的 TenantScope；Platform Repository 完全分离。
- [ ] 两 Tenant 负向测试和 query mutation 能发现遗漏 Tenant 条件。

**Verification:** Atlas replay/checksum、sqlc clean diff、受限 Role integration 和隔离测试通过。

**Stop conditions:** 需要 RLS、超级用户、可空 Tenant 或 Boolean bypass 才能工作。

**Recovery:** 删除独立 P2 数据库和角色，不影响 P1 数据。
