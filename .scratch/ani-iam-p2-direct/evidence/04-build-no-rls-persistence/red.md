# DP2-04 RED evidence

## TenantScope public seam

Result: `fail`

Command:

```text
GOCACHE=<goal-evidence>/.work/go-cache \
GOMODCACHE=<goal-evidence>/.work/go-mod \
GOTMPDIR=<goal-evidence>/.work/go-tmp \
go test ./internal/biz
```

Observed exit code: `1`

Observed source failure after the pinned dependency was available:

```text
# github.com/zhangzhe-ctrl/ani-iam/internal/biz_test [github.com/zhangzhe-ctrl/ani-iam/internal/biz.test]
internal/biz/tenant_scope_test.go:16:20: undefined: biz.NewTenantScope
internal/biz/tenant_scope_test.go:33:19: undefined: biz.NewTenantScope
internal/biz/tenant_scope_test.go:33:65: undefined: biz.ErrTenantScopeRequired
internal/biz/tenant_scope_test.go:34:69: undefined: biz.ErrTenantScopeRequired
internal/biz/tenant_scope_test.go:37:15: undefined: biz.TenantScope
internal/biz/tenant_scope_test.go:38:52: undefined: biz.ErrTenantScopeRequired
internal/biz/tenant_scope_test.go:39:72: undefined: biz.ErrTenantScopeRequired
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz [build failed]
FAIL
```

An earlier sandbox-only attempt failed while writing the default Go cache. That environment failure is not counted as the RED; the result above is the first valid failing test at the agreed public seam.

## UnitOfWork and transactional audit seam

Result: `fail`

Command: the same fixed-cache `go test ./internal/biz` command shown above.

Observed exit code: `1`

Observed source failure began with:

```text
internal/biz/membership_test.go:106:112: undefined: biz.TenantTransaction
internal/biz/membership_test.go:118:18: undefined: biz.TenantMembershipRepository
internal/biz/membership_test.go:119:18: undefined: biz.SecurityAuditRepository
internal/biz/membership_test.go:130:49: undefined: biz.TenantRoleBindingRepository
internal/biz/membership_test.go:135:14: undefined: biz.TenantMembership
internal/biz/membership_test.go:152:16: undefined: biz.SecurityAuditEvent
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz [build failed]
FAIL
```

The failing public test requires the use case to place the membership mutation and required audit append in one UnitOfWork callback and to propagate an audit failure without committing.

## UUIDv7 generator seam

Result: `fail`

Command:

```text
go test ./internal/data
```

Observed exit code: `1`

```text
internal/data/id_generator_test.go:14:20: undefined: data.NewUUIDv7Generator
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
FAIL
```

The test requires 128 non-zero, unique version-7 UUIDs from the data adapter that implements the biz IDGenerator port.

## Real PostgreSQL persistence seam

Result: `fail`

Command:

```text
DP2_ATLAS_BIN=/tmp/ani-direct-p2-01-05/dp2-04/bin/atlas \
GOCACHE=<goal-evidence>/.work/go-cache \
GOMODCACHE=<goal-evidence>/.work/go-mod \
GOTMPDIR=<goal-evidence>/.work/go-tmp \
go test -tags=integration ./tests/integration
```

Observed exit code: `1`

```text
tests/integration/persistence_test.go:51:14: undefined: data.NewPostgresUnitOfWork
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
FAIL
```

The pre-implementation integration suite already fixes the real seams for empty-database Atlas replay, restricted migration/runtime roles, no RLS, non-null/non-zero Tenant IDs, runtime DML, two-Tenant negative access, composite foreign keys, UnitOfWork commit/rollback, required Audit atomicity, commit failure, optimistic concurrency, and the query-mutation negative control.

## Runtime database TEMPORARY privilege review fix

Result: `fail`

Command:

```text
TESTCONTAINERS_RYUK_DISABLED=true \
DP2_ATLAS_BIN=/tmp/ani-direct-p2-01-05/dp2-04/bin/atlas \
GOCACHE=<goal-evidence>/.work/go-cache \
GOMODCACHE=<goal-evidence>/.work/go-mod \
GOTMPDIR=<goal-evidence>/.work/go-tmp \
go test -tags=integration \
  -run 'TestNoRLSPersistenceFoundation/migration_and_runtime_roles_stay_separated' \
  -v ./tests/integration
```

Observed exit code: `1`. The pinned real PostgreSQL container reached ready state, both databases replayed the Atlas migration, and the new least-privilege assertion failed with:

```text
persistence_test.go:91: runtime role unexpectedly has database TEMPORARY privilege
```

This confirms PostgreSQL's database-level default was still reachable through `PUBLIC`; revoking schema `CREATE` alone was insufficient.

## UnitOfWork TenantScope binding review fix

Result: `fail`

Command: the fixed-cache integration command targeted at `TestNoRLSPersistenceFoundation/transaction_rejects_a_different_TenantScope`.

Observed exit code: `1`:

```text
tests/integration/persistence_test.go:330:27: undefined: biz.ErrTenantScopeMismatch
tests/integration/persistence_test.go:331:59: undefined: biz.ErrTenantScopeMismatch
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
```

The agreed public seam now requires a transaction opened for Tenant A to reject a Tenant B scope before any repository SQL is issued.

## Complete typed Security Audit review fix

Result: `fail`

Command: the fixed-cache `go test ./internal/biz` command.

Observed exit code: `1`; the new public-domain tests failed to compile because `CreateMembershipCommand` and `SecurityAuditEvent` did not yet expose the typed authentication method, boundary, decision, source, target and reason fields. Representative output:

```text
unknown field AuthenticationMethod in struct literal of type biz.CreateMembershipCommand
undefined: biz.AuditAuthenticationMethodPassword
unknown field DecisionID in struct literal of type biz.CreateMembershipCommand
audits.appended.Boundary undefined
```

The same tests require empty authentication method, request, correlation, or decision identities to fail before opening the UnitOfWork.

The real PostgreSQL schema assertion then ran against the pinned container and exited `1` with:

```text
persistence_test.go:267: required non-null audit columns = 12, want 17
```

The migration therefore did not yet satisfy the ADR-required actor/authn, boundary, decision and source identities even after the domain seam had been added.

## Driver-error boundary and deferred commit review fix

Result: `fail`

Command: the fixed-cache integration command targeted at `TestNoRLSPersistenceFoundation/commit_failure_is_returned_and_leaves_no_partial_transaction`.

Observed exit code: `1`:

```text
tests/integration/persistence_test.go:425:26: undefined: biz.ErrTenantRelationConflict
tests/integration/persistence_test.go:426:52: undefined: biz.ErrTenantRelationConflict
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
```

The public seam requires a deferred PostgreSQL foreign-key error raised by `Commit` to become a framework-independent biz error and explicitly rejects an `errors.As(..., *pgconn.PgError)` match after the data adapter boundary.

## Standards re-review: exact FK mapping and non-speculative Role lifecycle

Result: `fail`

Command: the fixed-cache real PostgreSQL command targeted at the schema, composite-FK and deferred-commit subtests.

Observed exit code: `1`; the pinned PostgreSQL container reached ready state and both new assertions failed:

```text
persistence_test.go:250: found 2 role lifecycle columns without a frozen domain contract, want 0
persistence_test.go:441: commit error = commit tenant unit of work: tenant relationship violates its boundary, want persistence state violates a domain constraint
```

The first assertion prevents DP2-04 from inventing Role/Role-Binding lifecycle states before a matching frozen domain contract. The second distinguishes the named composite Tenant Role-Binding foreign keys from the unrelated Audit actor foreign key, while retaining the no-driver-leak requirement.
