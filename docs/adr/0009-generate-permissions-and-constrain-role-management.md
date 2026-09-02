---
status: accepted
---

# Generate Permission definitions and constrain Role management

ANI's OpenAPI authorization annotations and generated operation registry are the source of truth for the stable Permission catalog. IAM pins the matching `policy_revision` and accepts only catalogued Permission codes. Tenant administrators cannot create free-form Permissions, and an unknown operation, Permission, or policy revision fails closed.

Roles do not store `permissions_json`. `tenant_role_permissions` and `platform_role_permissions` explicitly join their respective Role tables to the generated Permission catalog. Database constraints and generation checks prevent a Tenant Role from containing a Platform Permission or a permission outside the registered operation contract.

Updating a custom Role requires `expected_version`. Its new Permission set and version commit atomically, and the change affects the next authorization decision because the decision TTL is zero; a stale version returns `409 Conflict`. Deleting a Role referenced by any active Role Binding or unfinished Invitation returns `409 ROLE_IN_USE`. Callers must migrate those references explicitly; Role deletion never cascades through authorization relationships.

Tenant IAM Bootstrap materializes the built-in Tenant Roles for each Tenant. These rows carry a `system_definition_version`; their stable code, system flag, and Permission set cannot be changed through Tenant role-management operations. An explicit schema or seed migration upgrades system definitions across Tenants with auditable before and after versions rather than leaving each Bootstrap snapshot permanently divergent.

The last-Tenant-administrator invariant counts only a Human Principal that is active and has both an active Tenant Membership and an active binding to the built-in `tenant-admin` Role. Service Principals do not satisfy this invariant. Tenant Lifecycle and Tenant Access do not enter the count because they gate the whole Tenant rather than distinguish administrators. Removal, suspension, disabling, and role replacement lock a Tenant-specific guard row or use an equivalent serializable transaction, recompute the invariant inside that transaction, and reject the change that would reduce the count to zero.

Loss of every usable Human Tenant administrator is recovered through a dedicated `RestoreTenantAdmin` Platform operation, not through Bootstrap or a direct database change. It requires a verified target Principal, a dedicated Platform Capability, separate requester and approver, recent reauthentication, a stable reason code, and immutable audit evidence.

Delegated role administration is outside the initial P2 delivery. Initially only the built-in `tenant-admin` may manage Tenant custom Roles and member Role Bindings. A later separately accepted iteration may introduce delegation; when it does, an actor may grant only Permissions the actor possesses and that the catalog marks `delegable`. No partial `delegable` mechanism or generic `role.manage` escalation path is added in the basic version.
