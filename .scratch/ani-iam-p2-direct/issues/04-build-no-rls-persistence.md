# 04: 建立无 RLS 持久化基础

**What to build:** 建立可从空库重放、由受限 runtime role 使用的目标 PostgreSQL 基础，为纵向链路提供显式 Tenant 隔离、事务和 Audit 原子性。

**Blocked by:** 02 / 冻结公开 OpenAPI 与 Operation Registry；03 / 冻结 IAM 与 Core 集成契约

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-1 / persistence foundation

**Baseline:** 02/03 接受的目标契约、当前数据不变量和独立 Direct P2 数据库边界。

**Scope:** Atlas versioned migrations、sqlc/pgx、migration/runtime role 分离、UnitOfWork/Audit、UUIDv7、version、复合 Tenant key/FK、两 Tenant fixtures 和 query mutation 基础。

**Out of scope:** PostgreSQL RLS、通用 soft delete、完整领域表集、生产备份/HA、旧数据迁移或共享旧 Auth 数据库。

**Allowed paths:** `migrations/**`、`internal/data/**`、`internal/biz/**` 中 Scope/UoW/ports、sqlc/Atlas 配置、`tests/integration/**`、`go.mod`、`go.sum`，以及本事项和证据目录。

**Forbidden paths:** `api/**`、`internal/service/**`、`deploy/**`、`../ANI/repo/**`；不得添加 RLS、可空 Tenant、平台 Boolean bypass、业务 superuser 或 owner-role DML。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/04-build-no-rls-persistence/`

- [x] 空库 migration replay、`atlas.sum` 和 sqlc clean diff 可复现。
- [x] runtime role 非 owner、非 superuser、无 BYPASSRLS，仅有目标 DML 权限。
- [x] Tenant-owned 表使用显式 `tenant_id` 和复合约束，跨 Tenant 访问被应用 SQL/约束拒绝。
- [x] mutation 与 Security Audit 同事务提交或共同回滚。
- [x] query mutation 能证明删除 Tenant predicate 会触发测试失败。

**Verification:** Atlas replay/checksum、sqlc clean diff、受限 role 真实 PostgreSQL integration、两 Tenant 正负向、事务回滚和 query mutation 通过。

**Stop conditions:** 需要 RLS、superuser、owner role、可空 Tenant、Boolean bypass 或共享旧表才能工作。

**Recovery:** 删除本事项创建的独立数据库/role/fixture；不影响旧 Auth 或其他测试数据。

## Claim record

- Claimed at: `2026-09-04T11:35:31+08:00`.
- Dependency status: DP2-02 and DP2-03 are `resolved`; no other Direct P2 ticket is `claimed`.
- IAM start: branch `main`, commit `d0309031888c190d990207d86fde7f4a14b212fd`, clean worktree and index.
- ANI state: durable worktree `/home/chabking/workspace/ANI-direct-p2-01-05`, branch `codex/direct-p2-01-05`, commit `573d3735934f74f9f1eb78818cddefefd9f575eb`, clean; DP2-04 does not modify ANI.
- Initial dependencies: Go `go1.26.7-X:nodwarf5 linux/amd64`; Docker client/server `29.7.2`; host `psql` is absent. The pinned task tools are Atlas Community `v1.3.0` (official Linux AMD64 SHA-256 `10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b`) and sqlc `v1.31.1` (official archive SHA-256 `497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354`, extracted binary SHA-256 `0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f`). Go dependencies are pinned to pgx/v5 `v5.9.2`, testcontainers-go `v0.43.0`, and google/uuid `v1.6.0`. The real database image is the already-local immutable `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c` (image ID prefix `5660c2cbfea5`).
- Initial module hashes: `go.mod` SHA-256 `6cac45c0f25d023b150beb8c97d3a64b2481e53385e19a31045852bebbc1ef8e`; `go.sum` SHA-256 `1b41b52ad891bdb1d3dd04b2b7790eb810a46dbc2d0f22aebfde6e66ef2decc5`.
- Allowed paths: `migrations/**`, `internal/data/**`, TenantScope/UoW/port additions under `internal/biz/**`, repository-root sqlc/Atlas configuration, `tests/integration/**`, `go.mod`, `go.sum`, this ticket, and `.scratch/ani-iam-p2-direct/evidence/04-build-no-rls-persistence/**`.
- Forbidden paths remain unchanged. In particular, no `api/**`, `internal/service/**`, ANI worktree, deploy path, legacy Auth table/database, RLS, nullable/all-zero Tenant, platform Boolean bypass, owner/superuser runtime DML, or shared test database is authorized.

## Confirmed TDD seams

The Goal pre-agrees these public seams: framework-independent `biz` TenantScope and UnitOfWork/repository ports; `data` implementations exercised through those ports; versioned migration/sqlc generated boundaries; and real isolated PostgreSQL behavior through the restricted runtime role. Tests do not mock PostgreSQL for a real-dependency gate and do not assert private implementation calls.

## Execution plan

1. Inventory the clean Kratos data/biz skeleton and accepted data invariants; pin Atlas, sqlc, pgx, testcontainers and PostgreSQL image identities before generation.
2. Add the first failing seam test, capture RED, implement only enough migration/schema/sqlc and domain port behavior for that slice, then repeat vertically for TenantScope, constraints, UnitOfWork/Audit, version conflict and role separation.
3. Start a task-owned disposable PostgreSQL container; bootstrap migration and restricted runtime roles with the container superuser, then run DDL only as migration role and all business integration tests as runtime role.
4. Replay migrations from an empty database, verify `atlas.sum`, regenerate sqlc twice without drift, run two-Tenant positive/negative, cross-Tenant FK, query-mutation, atomic commit/rollback, commit-failure, concurrency/version and privilege gates.
5. Review Standards and ticket Spec separately, repair blocking findings, stage only Allowed paths, rerun the full ticket gate and create the authorized local commit.

## Test plan

- RED evidence is captured before each implementation slice; GREEN is accepted only through the agreed public seam.
- Static/unit: TenantScope rejects empty/all-zero Tenant; ports remain framework/driver free; UUIDv7 and mutable version behavior; query source mutation proves removal of a Tenant predicate fails the gate.
- Generation/migrations: pinned Atlas hash/validate, empty-database replay, pinned sqlc generation twice and clean diff.
- Real PostgreSQL: migration role and runtime role attributes/grants; runtime positive DML; Tenant A positive; Tenant B cannot read/update Tenant A; composite FK rejects cross-Tenant relation; mutation+Audit commit/rollback together; commit failure; optimistic/concurrent version conflict; runtime cannot DDL or assume owner/superuser/BYPASSRLS.
- Regression: `go test ./...`, targeted race/concurrency where applicable, `go vet ./...`, generated diff, `git diff --check`, staged path and secret/build-artifact audits.

## Recovery plan

Before commit, preserve a reviewed reverse patch limited to Allowed paths. Isolated PostgreSQL resources use a DP2-04-specific container/database/role identity and can be stopped and removed without touching shared databases or the legacy Auth environment. After commit, recovery uses a new revert commit against the exact DP2-04 SHA; never reset, stash, amend, force, push, deploy, rebuild shared data, or invalidate credentials.

## Verification summary

- Final ticket result: `pass`.
- Spec review: `pass`; no blocker and no scope creep.
- Standards review: `pass`; all initial blockers were repaired and the final snapshot has no blocker.
- Real PostgreSQL: `pass`; fixed PostgreSQL 16.4 Alpine digest, restricted runtime role, two empty databases, 12 final subtests, race-enabled repeat.
- Tenant predicate mutation: `pass`; the final mutant was killed only by the Tenant B negative test.
- Atlas/sqlc reproducibility: `pass`.
- Go regression/vet/race: `pass`.
- SBOM/license/govulncheck: `pass`; reachable vulnerabilities were repaired and the final scan reports zero affected vulnerabilities.
- Downstream CI support for the Go 1.26 module floor: `not_verified`; local validation used Go 1.26.7 and DP2-04 does not change ANI CI images.
- Production backup/HA/load/fuzz: `not_verified`; outside this ticket and not claimed.
- Detailed commands, hashes, RED/GREEN results, dependency identities and recovery are in `.scratch/ani-iam-p2-direct/evidence/04-build-no-rls-persistence/`.
