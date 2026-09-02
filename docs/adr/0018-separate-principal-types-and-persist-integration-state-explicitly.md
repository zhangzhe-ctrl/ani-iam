---
status: accepted
---

# Separate Principal types and persist integration state explicitly

Human and Service identities share an immutable `principals` base containing their UUIDv7 identity, type, status, timestamps, and version. Type-specific relations hold Human and Service attributes instead of placing unrelated nullable fields on the base row. Human verified-email and login Identity data cannot appear on a Service Principal profile.

A Service Principal profile fixes one Core-generated Tenant ID and has a name unique after normalization within that Tenant. Its profile, sole active Tenant Membership, and initial Role Bindings commit atomically. Database uniqueness and a constraint or constraint trigger reject a second non-removed Tenant Membership or a Membership whose Tenant differs from the fixed profile Tenant. Disabling the Service Principal does not release its name for ambiguous reuse.

Platform Memberships accept only Human Principals. Internal workloads use mTLS or SPIFFE and a short Service Token instead of manufacturing a Platform Service Principal.

`tenant_access` has exactly `bootstrap_pending`, `active`, and `suspended` states. A missing row is not another state and returns the previously accepted `TENANT_IAM_NOT_READY` availability result.

Lifecycle integration uses separate persistence concerns. `tenant_lifecycle_projections` stores the complete per-Tenant state and version. `lifecycle_consumer_state` stores stream sequence, heartbeat, consumer watermark, and synchronization health. Per-Tenant gap and repair state is stored explicitly rather than overloading the projected business row or relying only on the broker's consumer metadata.

`tenant_bootstrap_operations` retains the intended administrator, immutable payload fingerprint, operation status, and recovery links. The initial Tenant Invitation stores its `bootstrap_operation_id`; accepting that Invitation resumes the same operation and atomically establishes the intended Membership, Roles, and active Tenant Access rather than matching an operation by email.

P2 introduces only a purpose-specific `notification_outbox` for Invitation and password-action delivery. It does not create a generic IAM integration outbox in anticipation of unknown subscribers. A new domain-event publisher requires a named consumer, contract, delivery semantics, and a separate decision.
