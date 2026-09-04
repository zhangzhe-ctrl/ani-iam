# DP2-03 frozen contract semantics

## Ownership and transport

| Result | Contract invariant | Evidence |
| --- | --- | --- |
| `pass` | IAM owns one internal `iam.v1` major containing `AuthenticationService`, `AuthorizationService`, and `IAMAdminService`. | IAM descriptor inventory test. |
| `pass` | The target descriptor contains no `auth.v1.AuthService`. | Exact descriptor inventory and negative assertion. |
| `pass` | Core exclusively owns Tenant ID and Tenant Lifecycle. | Core service is read-only; no create/update/delete lifecycle RPC exists. |
| `pass` | IAM cannot write Core Lifecycle through this contract. | Only snapshot begin/list methods are exposed to IAM. |
| `pass` | No shared database, cross-database transaction, or shared internal Go package is introduced. | Artifact exchange is descriptor/fixture based; no runtime or data path changed. |
| `pass` | Both repositories pin start commits, policy revision, toolchain, descriptor hashes, and fixture hashes. | Byte-identical `contract_pins.json`. |
| `not_verified` | A running IAM process registers only these three services. | Runtime server wiring is outside allowed paths. |

## IAM contract

- `AuthenticationService`: Password/OIDC, password action, Session/Grant boundary, refresh/logout/switch, session revocation, principal validation, and workload Service Token surface.
- `AuthorizationService`: exactly one `CheckPermission` RPC with credential, operation ID, DP2-02 policy revision, target attributes, decision identity, Principal context, typed obligations, and allow/deny reason.
- `IAMAdminService`: Tenant/Platform membership, roles/bindings, invitations, Tenant Access, Service Principal/API Key, audit queries, Recovery Bootstrap, and Restore Tenant Admin.
- `IAMErrorReason`: 19 non-unspecified reasons. The fixture freezes gRPC code and required `google.rpc.ErrorInfo` metadata for every reason under domain `iam.ani.internal`.
- Within `iam.v1`, compatible evolution is additive. Breaking changes require a new major-version package and controlled consumer transition.

## Core integration contract

- Stable subjects:
  - `ani.integration.tenant.lifecycle.v1`
  - `ani.integration.tenant.lifecycle-heartbeat.v1`
  - `ani.integration.tenant.iam-bootstrap.v1`
- `IntegrationEnvelope` carries event identity, schema major, producer, Tenant ID, monotonic aggregate version, occurrence time, correlation/causation/operation identity, and trace context.
- Lifecycle facts are complete state, not imperative deltas. Duplicate/older versions are idempotent; a gap freezes only the affected Tenant until snapshot repair.
- Bootstrap fingerprint is SHA-256 over UTF-8 JSON with lexicographically ordered keys and no optional whitespace: `locale`, `normalized_email`, `operation_id`, `tenant_id`.
- Snapshot starts with an opaque consistent cursor. Page tokens belong to one cursor, all pages share one `snapshot_version`, and an empty `next_page_token` ends the snapshot.
- Seven non-unspecified Core error reasons are frozen with gRPC status and required `google.rpc.ErrorInfo` metadata under domain `core.ani.internal`.
- Within `tenant.integration.v1`, compatible evolution is additive. Breaking evolution requires a new package/subject and explicit dual-publish, dual-consume, and old-version retirement.

## Consumption model

ANI owns and generates the Core source and descriptor. IAM carries a pinned immutable copy of the Core descriptor plus fixtures. ANI carries a pinned immutable copy of the IAM descriptor plus the same fixtures. Each repository validates its own generated types and the other repository's descriptor without importing the other repository's internal Go packages.

Real publication, runtime consumption, Core outbox, NATS infrastructure, and deployment are `not_verified` and remain outside DP2-03.
