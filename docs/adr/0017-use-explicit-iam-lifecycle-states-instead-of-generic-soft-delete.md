---
status: accepted
---

# Use explicit IAM lifecycle states instead of generic soft delete

The Human/Workload refoundation reopens only the historical test-cleanup exception below. Its retention/cleanup contract is tracked under D04 in the current decisions register; until accepted, neither this ADR nor the candidate authorizes manual audit-row deletion. All other lifecycle, locking, ownership, and schema decisions remain accepted.

IAM-owned entities use application-generated UUIDv7 identities. Core Tenant identities are contractually UUIDs stored in PostgreSQL `uuid` columns, but IAM only accepts Core-generated values and never creates a second Tenant identity.

The IAM database separates an owner or migration role from the runtime application role. Only the migration job may execute DDL. The application receives allowlisted DML grants, is neither owner nor superuser, and has no `BYPASSRLS` privilege even though P2 defines no RLS policies. A read-only support role is not provisioned by default and requires a separate approval if later needed.

Every Tenant-owned table has a non-null `tenant_id`. Platform and global entities use distinct tables and never encode Platform scope as NULL, an all-zero UUID, or a special Tenant. Status columns use constrained text values mirrored by generated Proto and Go constants rather than PostgreSQL ENUMs or unconstrained strings. Timestamps use `timestamptz` in UTC, and mutable aggregates carry a non-null monotonically increasing `version bigint`.

IAM deliberately does not follow the broader repository convention of adding generic `deleted_at` to every table. This is a security-boundary exception for five reasons:

1. Without RLS, every forgotten `deleted_at IS NULL` predicate would become another implicit route by which a removed Membership, revoked credential, or disabled Principal could reenter an authorization query.
2. IAM deletion words are not interchangeable. `disabled`, `revoked`, `expired`, `cancelled`, and `removed` have different recovery, audit, and credential effects that a single soft-delete bit cannot express.
3. Soft-deleted rows complicate global email ownership, active Membership, key identity, and Role-code uniqueness with partial indexes and can accidentally permit duplicate live security identities.
4. Foreign keys and historical Role Binding or Session evidence need explicit lifecycle rules; a generic soft delete neither preserves nor removes those relationships correctly.
5. Setting `deleted_at` does not erase secrets or personal data and therefore must not be presented as retention, purge, or privacy compliance.

Accordingly, Principals are disabled, Memberships are removed, API Keys and Session Grants are revoked, Invitations and action tokens expire or are cancelled, and audit remains append-only. A custom Role may be physically deleted only after the previously accepted `ROLE_IN_USE` checks prove it has no active Binding or unfinished Invitation. Future physical Tenant or personal-data erasure remains the separately deferred Purge design.

Normal transactions use `READ COMMITTED` with explicit row locking and optimistic versions. Tenant-wide invariants such as the final administrator and Invitation state races use a Tenant guard lock or a narrow serializable transaction rather than making every request serializable.

The independent IAM repository owns checksum-verified migrations executed by a pre-deployment migration job. Runtime instances only verify the expected schema version and never migrate automatically.

The initial P2 delivery has no automated retention cleanup job. Expired Invitations, action tokens, consumed Refresh Token evidence, old idempotency rows, and audit data therefore accumulate until a later maintenance feature is authorized. Audit remains queryable for at least 180 days but has no maximum deletion deadline; if the project remains active at that horizon, a later iteration must deliver explicit deletion or retention handling.

The system monitors row counts, database size and growth rate, and the oldest record in accumulating tables and raises alerts without automatic deletion. Runtime audit access remains append-only. The proposed disposable whole-database cleanup mechanism is historical candidate input, not an accepted M1 requirement or deletion permission. Expired, revoked, or consumed non-audit temporary state requires a separately frozen owner-specific maintenance rule and cannot be inferred from this ADR.

The earlier September design allowed an unaudited administrator to delete audit events older than 180 days without a snapshot, change record, reason, or row-count report. That choice is retained here only as historical rationale and no longer authorizes current work. M1 verifies the accepted minimum 180-day queryability and absence of automatic deletion. The prior candidate's exact three-boundary test recipe and cleanup exception remain recorded in the [before snapshot](../../.scratch/ani-iam-workload-refoundation/evidence/16-organize-docs-and-replan/before/docs/adr/0017-use-explicit-iam-lifecycle-states-instead-of-generic-soft-delete.md); retention/cleanup semantics beyond the accepted rules remain D04 work and any future purge requires a separately accepted contract.

The idempotency ledger guarantees replay for twenty-four hours. A request that reuses the same scoped key after that logical expiry receives `409 IDEMPOTENCY_KEY_EXPIRED` and must generate a new key; retained physical rows do not extend replay forever or become new requests.

Tenant Memberships use a partial unique constraint so one `(tenant_id, principal_id)` has at most one Membership whose status is not `removed`, while historical removed Membership identities remain. A separate `verified_emails` relation owns globally unique normalized email addresses and references Human Principals; several login Identities may still reference that same Principal.

Tenant Role codes are unique within a Tenant. System codes remain reserved, while a deleted unreferenced custom Role code may be reused only by a new Role identity. Security relationships use restrictive foreign keys rather than cascade deletion. Structured JSON is limited to schema- or allowlist-constrained non-sensitive metadata; Permissions, Role Bindings, lifecycle states, and ownership relations remain normalized.

Indexes for Tenant-owned access paths begin with `tenant_id` and then the actual status, object identity, or time fields used by the query. CI checks Tenant Repository queries and their intended index coverage rather than relying only on independent primary-key indexes.
