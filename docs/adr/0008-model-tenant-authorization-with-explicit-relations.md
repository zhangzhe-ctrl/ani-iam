---
status: accepted
---

# Model Tenant authorization with explicit relational boundaries

ADR-0022 amends this decision's Workload terminology. The exact TenantExecutionScope authority/evidence combinations remain pending under D02 in the [current decisions register](../../.scratch/ani-iam-workload-refoundation/decisions.md); the authority invariants below and every other decision remain accepted.

The P2 target replaces polymorphic Principal-to-boundary authorization rows with relations that name the actual domain objects. It stores Tenant and Platform authorization separately instead of encoding both through nullable columns, a `boundary_type`, or a magic boundary identity.

`tenant_role_bindings` bind a Tenant Membership to a Tenant Role. Their keys and foreign keys include `tenant_id`, so a database constraint rejects a Membership from one Tenant being bound to a Role from another. `platform_role_bindings` separately bind a Platform Membership to a Platform Role. A Role Binding never attaches directly to a Principal; the active Membership remains the authorization boundary and one Membership may have multiple active Role Bindings.

Every Tenant receives its own `tenant_roles` rows, including the built-in roles materialized during Tenant IAM Bootstrap. Built-in rows are marked as system roles and their stable code and existence cannot be changed through the custom-role operations. Custom roles use the same Tenant-owned table. Platform roles use a separate Platform-owned table.

Tenant and Platform invitations use separate tables and operations. A Tenant invitation stores `tenant_id` explicitly and its target role must satisfy a composite same-Tenant foreign key. It cannot be accepted into a Platform Membership or into another Tenant.

An API Key is a credential of a Tenant-owned Workload Principal, not an independent authorization subject and not a Human Principal's personal credential. `principal_id` is required, the Workload must have its one active Membership in the target Tenant, and authorization comes from that Membership's Role Bindings. The key row does not carry a separate `permissions_json` policy. A suitably authorized Human or Platform operation may create or revoke the key, but the authenticated identity on SDK requests is the Workload Principal rather than the Human creator.

The management flow exposes the Tenant-owned Workload Principal explicitly. An authorized user first creates or selects a named Workload, assigns its Tenant Roles, and then creates API Key credentials for it. Tenant administrators may manage all Tenant-owned Workloads in their Tenant, while delegated managers may manage only the Workloads allowed by their current permissions. The original creator receives no permanent bypass and is retained only as an audit actor.

P2 accepts an API Key only through `Authorization: Bearer <api-key>`; the official SDK supplies this header. It removes `X-API-Key`, query-string, and cookie credential inputs from the P2 target contract. CP0 and P1 still test whichever inputs exist in their pinned compatibility oracle.

Expiration is optional and an omitted expiration means the credential does not expire automatically. A Tenant-owned Workload Principal may have any number of active API Keys; the data model imposes no fixed per-Principal active-key limit. Each credential nevertheless has its own identity and can be listed, observed, rotated, and revoked independently.

The creation contract represents expiration explicitly: it accepts either `never_expires=true` or an `expires_at` value, and rejects an ambiguous request containing neither or both. The Console selects `never_expires` by default and displays the consequence before submission. A non-expiring Key that has not been used for ninety days is marked stale and raises an operator-visible alert but is not revoked automatically.

Unlimited active credentials does not mean an unbounded management interface. Key creation is rate-limited, list operations require cursor pagination, and unusual active-key counts raise alerts; these are abuse and operability controls, not a fixed business count limit.

The presented credential contains a non-secret key identity and a high-entropy secret. IAM uses the key identity for bounded lookup and stores only a hash of the secret, a display-safe prefix or suffix, lifecycle metadata, and the one-time creation actor. The complete secret is returned only once. API Key rows carry no business `rate_limit_rpm`; IAM applies credential-abuse protection, while product usage limits and quota policy remain outside the credential model.

`last_used_at` is sampled or aggregated asynchronously with a stated maximum lag; successful authentication does not synchronously update the credential row on every request. Request and security audit records, rather than this convenience timestamp, are the authoritative usage evidence.

The operation registry records allowed authentication methods. API Key authentication is denied for password, credential, role, Recovery Bootstrap, and Platform-management operations unless a future decision explicitly opens a narrower machine operation. Role permission alone cannot bypass this credential-type restriction.

Creating a Tenant-owned Workload Principal atomically commits the Principal, its owner record, active Tenant Membership, initial Role Bindings, and audit record. Creating its first API Key is a separate explicit operation so failure cannot leave an unreported credential or conflate identity creation with secret delivery.

An API Key fails closed when it is revoked or expired, or when its Workload Principal, Tenant Membership, Tenant Access, or projected Tenant Lifecycle is not active. Disabling the Human who originally created the key does not disable the independent Workload Principal. Disabling that Workload irreversibly revokes all of its API Keys in the same operation; enabling the Principal later requires new Keys rather than reviving old secrets.

At the public boundary, a missing, malformed, unknown, expired, or revoked Key returns `401`. A valid Key whose Workload Principal, Membership, Tenant Access, or Tenant Lifecycle is not active returns `403`. Missing or stale lifecycle projection and unavailable IAM dependencies return `503`. Stable internal reason codes and audit details preserve diagnosis without exposing a Key secret or unnecessary state in the public message.

Core Control remains the sole authority for Tenant identity. IAM stores Core's Tenant ID as an external authority identity in `tenant_access`, `tenant_lifecycle_projections`, and Tenant-owned tables, but creates no cross-database foreign key and no second authoritative Tenant aggregate.

Automated Tenant work does not impersonate a Human or receive blanket Platform access. A worker establishes a `TenantExecutionScope` using its own current Authority and verified facts from a trusted source. A message Tenant ID or self-reported producer cannot create scope or authority by itself. D02 freezes the minimal synchronous and asynchronous evidence/binding rules before the corresponding path is implemented; the candidate Fixed Tenant / Authoritative Message Tenant combinations are not accepted merely by appearing in a plan. The resulting scope carries operation, correlation, and causation identities; workers use the same Tenant-scoped repositories as request flows and produce attributable audit evidence.

The current target does not expose or implement Tenant Purge. Disabling a Tenant changes its lifecycle or access state and preserves IAM records. Physical erasure, retention conflicts, deletion order, and any cascade behavior are deferred to a future separately accepted decision; no broad `ON DELETE CASCADE` is introduced speculatively.
