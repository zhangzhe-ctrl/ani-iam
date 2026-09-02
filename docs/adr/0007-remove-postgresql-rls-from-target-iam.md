---
status: accepted
---

# Remove PostgreSQL RLS from the target IAM data model

The future destructive P2 target intentionally removes PostgreSQL row-level security from IAM. Removing RLS is one of the motivations for the rebuild, not an accidental loss of an existing defense and not an open retain-or-remove decision.

This target decision does not retroactively change the CP0 and P1 compatibility oracle: those stages must still reproduce and test the pinned old system's real PostgreSQL and RLS semantics. RLS is removed only with the P2 data rebuild and target-contract cutover; evidence from a no-RLS P2 implementation cannot be presented as CP0 or P1 compatibility evidence.

Without RLS, PostgreSQL no longer provides row-level Tenant isolation for an application credential. This residual risk is accepted explicitly: compromise or misuse of an IAM application credential can reach every row allowed to that database role. Least-privilege database grants and application-layer isolation reduce exposure but are not described as equivalent to RLS.

For ordinary Tenant operations, the Gateway removes client-supplied identity context and IAM derives a non-empty TenantScope only from the authenticated Principal, the requested Tenant, active Tenant Access, active Membership, and the required permission. A handler cannot construct a trusted scope from a path, body, or header alone. Ordinary repositories require TenantScope and expose no unscoped `FindByID`, optional-Tenant, empty-Tenant-means-platform, or Boolean administrator bypass. Cross-Tenant operations use separate Platform repositories and require a dedicated capability, service identity, stable reason code, and immutable audit record; a TenantScope cannot be promoted into a platform scope.

P2 IAM tables that contain Tenant-owned data carry `tenant_id`. Tenant-local uniqueness and relationships use composite keys and foreign keys such as `(tenant_id, id)` so the database rejects cross-Tenant associations even though it cannot reject every unscoped read. Global Principal data and genuinely platform-scoped data remain global rather than receiving a synthetic Tenant. The final schema must state the primary key, uniqueness scope, and foreign-key scope for every table; abbreviated historical sketches are insufficient.

Only approved infrastructure adapters may issue SQL. Their generated or allowlisted Tenant queries accept an explicit TenantScope or Tenant ID, and architectural checks reject raw database access outside those adapters. Every Tenant-scoped repository and public operation requires at least two-Tenant negative tests. The gate must demonstrate that deliberately omitting a Tenant predicate causes a test failure; any observed cross-Tenant read or write is a hard blocker.
