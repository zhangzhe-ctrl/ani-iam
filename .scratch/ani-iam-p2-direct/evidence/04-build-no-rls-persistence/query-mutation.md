# DP2-04 Tenant predicate query mutation

Result: `pass`

The source worktree remained unchanged. A Goal-owned ignored copy was made, and only the `GetTenantMembership` predicate was changed from:

```sql
WHERE tenant_id = sqlc.arg(tenant_id)
  AND id = sqlc.arg(id)
```

to the signature-preserving mutant:

```sql
WHERE sqlc.arg(tenant_id)::uuid IS NOT NULL
  AND id = sqlc.arg(id)
```

The fixed sqlc `v1.31.1` binary regenerated the temporary copy. The full real-PostgreSQL integration suite was then run with the same fixed image and restricted runtime role.

Command:

```text
TESTCONTAINERS_RYUK_DISABLED=true \
DP2_ATLAS_BIN=/tmp/ani-direct-p2-01-05/dp2-04/bin/atlas \
GOCACHE=<goal-evidence>/.work/go-cache \
GOMODCACHE=<goal-evidence>/.work/go-mod \
GOTMPDIR=<goal-evidence>/.work/go-tmp \
go test -tags=integration -v ./tests/integration
```

Observed exit code: `1` (expected mutation kill).

Relevant output:

```text
=== RUN   TestNoRLSPersistenceFoundation/Tenant_B_cannot_read_or_mutate_Tenant_A
    persistence_test.go:370: Tenant B Get() error = <nil>, want tenant membership not found
--- FAIL: TestNoRLSPersistenceFoundation
    --- FAIL: TestNoRLSPersistenceFoundation/Tenant_B_cannot_read_or_mutate_Tenant_A
FAIL
```

This mutation was rerun after the final schema and driver-error review fixes. All other final subtests passed, including empty-database replay, runtime-role/TEMPORARY separation, no-RLS/non-null Tenant checks, absence of speculative Role lifecycle columns, complete Audit identity constraints, transaction-scope mismatch rejection, atomic Audit, composite FK, exact mapped commit failure without driver leakage, rollback, optimistic version, and the independent unsafe-query negative control. This isolates the killed mutant to the missing Tenant predicate.
