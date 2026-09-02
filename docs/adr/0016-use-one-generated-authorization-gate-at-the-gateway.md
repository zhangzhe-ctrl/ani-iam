---
status: accepted
---

# Use one generated authorization gate at the Gateway

Gateway authenticates to IAM with a dedicated mTLS or SPIFFE workload identity. IAM authorizes that identity per RPC, and its authentication and authorization gRPC listeners are not publicly exposed. A shared secret or cluster-network location is not sufficient service identity.

Public routes make no IAM call. Authenticated-only routes send the raw credential once to `ValidatePrincipal`. Authorized routes send the raw credential and generated operation identity once to `CheckPermission`; they do not call `ValidatePrincipal` first and Gateway does not implement a second JWT validator.

ANI OpenAPI authorization annotations generate one immutable operation and policy registry artifact. Gateway and IAM deployments pin its digest and `policy_revision` and verify them at startup. A revision mismatch fails with `503 AUTHZ_POLICY_MISMATCH` and raises an alert rather than returning a user denial. A missing annotation or unregistered operation fails generation or CI; if reached at runtime it fails closed with `503 AUTHZ_OPERATION_UNREGISTERED`. No default allow, generic read/write policy, or string-derived fallback exists.

Gateway gives each hot-path `ValidatePrincipal` or `CheckPermission` call a 500 ms deadline and does not retry it automatically. Invalid credentials map to `401`, current authorization or state denial to `403`, unavailable IAM or policy drift to `503`, deadline expiry to `504`, and authentication or authorization throttling to `429`.

Before calling IAM or forwarding a request, Gateway removes every client-supplied `x-ani-*` identity header. After an allow decision it injects only Principal identity and type, boundary, Tenant identity for a Tenant boundary, Session or Grant identity, authentication method, and decision identity. Roles and Permissions are not propagated as headers.

This trusted context is valid only on the Gateway-to-first-hop mTLS connection. A first-hop service does not blindly forward it. Subsequent service calls authenticate the calling workload with mTLS or a Service Token and carry explicit actor, correlation, and decision references only as audit context; user context cannot substitute for workload authorization.

Internal listeners are not published through public ingress. Public Core and Services handlers accept injected context only from the authenticated Gateway workload, while internal operations use separate listener or route allowlists and NetworkPolicies. Clients cannot bypass Gateway by presenting an Access Token or forged context directly to a service.

When an operation requires authoritative resource ownership that IAM cannot read, `CheckPermission` returns a typed obligation and the owning Handler loads the resource and enforces its actual Tenant or owner. Generated checks prevent registration when the required obligation handler is absent; Gateway never connects to a business database to perform this check.
