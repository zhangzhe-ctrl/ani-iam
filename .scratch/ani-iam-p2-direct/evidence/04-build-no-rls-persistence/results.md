# DP2-04 verification results

Final ticket result after independent Spec and Standards review: `pass`

## Acceptance matrix

| Requirement | Result | Evidence |
| --- | --- | --- |
| Empty-database Atlas replay, twice | `pass` | Real PostgreSQL suite applies `migrations/**` as `ani_iam_migrator` to `ani_iam_dp2_04_a` and `ani_iam_dp2_04_b` |
| `atlas.sum` integrity and Atlas validation | `pass` | `generated.md`; final `atlas.sum` SHA-256 `fc6889d0120a11c5514216c5c8d0381ae8fcf292586bceb660511cc1d45f17a9` |
| sqlc clean reproducibility | `pass` | Fresh ignored-directory generation and worktree regeneration were byte-identical; `generated.md` |
| Runtime role is non-owner, non-superuser, no `BYPASSRLS`, no `TEMPORARY`, target DML only | `pass` | Real role/grant queries plus denied DDL, temp table, `SET ROLE`, audit update/delete |
| No RLS; every Tenant-owned table has non-null/non-zero `tenant_id` | `pass` | Catalog query plus all-zero UUID constraint negative |
| No speculative Role lifecycle | `pass` | `tenant_roles` and `tenant_role_bindings` contain no status column before a matching frozen domain contract; custom Role deletion remains the accepted lifecycle |
| TenantScope cannot be empty or switched inside a UoW | `pass` | Unit tests and real transaction-scope mismatch subtest |
| Tenant A positive; Tenant B cannot read/update Tenant A | `pass` | Real runtime-role repository tests |
| Composite FK rejects cross-Tenant relationship | `pass` | Real runtime-role insert, PostgreSQL `23503` at direct database test seam |
| Mutation and complete Security Audit commit atomically | `pass` | Real UoW/use-case path; 17 non-null audit fields and six identity constraints verified |
| Required audit failure rolls mutation back | `pass` | Duplicate audit identity returns mapped biz error and membership is absent |
| Commit failure is returned without driver leakage | `pass` | Deferred Audit actor FK fails at UoW commit as neutral `biz.ErrInvalidPersistenceState`; only the two named composite Tenant Role-Binding FKs map to `biz.ErrTenantRelationConflict`; no `pgconn.PgError` crosses adapter |
| Optimistic concurrency/version conflict | `pass` | Two concurrent version-1 updates produce one success and one `biz.ErrVersionConflict` |
| Query mutation removes Tenant predicate and is killed | `pass` | Final full suite fails only the Tenant B read; `query-mutation.md` |
| UUIDv7 | `pass` | 128 unique generated IDs and database version-nibble constraints |
| Supply chain: pinned versions, SBOM, license inventory, vulnerability scan | `pass` | `toolchain.md`, `bom.cdx.json`, `licenses.md`, `vulnerabilities.md` |
| Downstream CI image supports the vulnerability-fixed Go 1.26 module floor | `not_verified` | Local fixed toolchain is Go 1.26.7; no downstream CI image was changed or executed in DP2-04 |
| Production backup/HA/load/fuzz | `not_verified` | Explicitly outside DP2-04 and retained for Production Readiness |

## Actual final commands

All Go commands used the Goal-owned absolute `GOCACHE`, `GOMODCACHE` and `GOTMPDIR`. Real dependency commands additionally used `TESTCONTAINERS_RYUK_DISABLED=true` and the pinned `DP2_ATLAS_BIN`.

```text
go test ./...
go vet ./...
go test -race ./internal/biz ./internal/data
go test -tags=integration -v ./tests/integration
go test -race -tags=integration -v ./tests/integration

/tmp/ani-direct-p2-01-05/dp2-04/bin/sqlc generate
/tmp/ani-direct-p2-01-05/dp2-04/bin/atlas migrate validate --dir file://migrations
/tmp/ani-direct-p2-01-05/dp2-04/bin/atlas migrate hash --dir file://migrations

cyclonedx-gomod mod -test -licenses -assert-licenses -json \
  -output-version 1.6 -noserial -notimestamp -output <evidence>/bom.cdx.json .
govulncheck -test -tags integration -show verbose ./...
go mod verify

git diff --check
```

The ordinary Go regression, vet, focused race tests and race-enabled real PostgreSQL suite all exited `0`. The final integration run executed 12 subtests and cleaned its container. The mutation command intentionally exited `1`; that expected failure is the evidence for a killed security-control mutant, not a target-source failure. Independent Spec and Standards reviews both returned `pass` with no blocker on the final snapshot.

## Real dependency identity

- Docker server `29.7.2`.
- Testcontainers for Go `v0.43.0`.
- PostgreSQL image `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`.
- Every run created a task-owned disposable container, random migration/runtime passwords, two isolated databases, an owner/migration role and a restricted runtime role; Testcontainers stopped and terminated the container after the suite.
- Container bootstrap superuser only created the two roles and databases. Atlas DDL and database privilege revocation ran as the database owner/migration role. Every business DML assertion ran as `ani_iam_runtime`.

## Fixed artifacts

| Artifact | SHA-256 |
| --- | --- |
| `go.mod` | `56a0615d27677f34354a4b33bb8cc7ff04b806e351bf03438965214aeccb54fe` |
| `go.sum` | `133fee512fcf6657de10f1bc23f629905779946615677d0266b320e78b4b8682` |
| `atlas.hcl` | `87c40f8df78d21891a8c196c56177bfc5c097a7d6a115925c9d39df39c404d32` |
| Migration | `0bb28594601bb666e045c5a957feb0dacf9de8c6840247a7bc15f9a298393483` |
| `migrations/atlas.sum` | `fc6889d0120a11c5514216c5c8d0381ae8fcf292586bceb660511cc1d45f17a9` |
| `sqlc.yaml` | `486a757be88a0ce95c69915a0bdab6e8250a090904f810114c35989dae37509f` |
| Query source | `9a98348b1dcebc9f88a90861e8e8f0325f23ce8c825cce69d726a63a7c53553e` |
| CycloneDX SBOM | `78631c4efaf58c92ac528e15708cf43f2ad09a99f4071807319386fe84f3d383` |

Generated sqlc output hashes are recorded in `generated.md`.

## Isolation and recovery

- No legacy Auth table, shared database, ANI worktree or existing ANI checkout was modified.
- No RLS, nullable/all-zero Tenant, platform Boolean bypass, unscoped repository, owner/superuser business DML, generic soft delete or legacy fallback exists in this change.
- Before commit, recovery is the uncommitted diff limited to DP2-04 Allowed paths. After commit, recovery is a new revert commit against the exact DP2-04 SHA.
- The task-owned containers have already been terminated. Ignored build/module/tool caches are reproducible and can be removed without losing source or evidence.
