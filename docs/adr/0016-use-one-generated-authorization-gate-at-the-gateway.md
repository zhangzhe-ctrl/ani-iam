---
status: accepted
---

# Use one generated authorization gate at the Gateway

ADR-0022 amends this decision's direct-caller terminology. Its refoundation plan proposes peer-plus-WAT and downstream Invocation mechanics, but those mechanisms remain fail closed and pending the relevant D01–D03 decision in the current WR decisions register; every other decision remains accepted.

Under the proposed refoundation model, Gateway would authenticate to IAM with a dedicated Workload Identity and target-bound Workload Access Token for each synchronous business gRPC/HTTP call; exact peer verification and health/readiness exceptions remain pending their checkpoint. IAM's authentication and authorization listeners are not publicly exposed. A shared secret or cluster-network location is not sufficient identity, and the unaccepted mechanics cannot be replaced with a fallback.

Public routes make no IAM call. Authenticated-only routes send the raw credential once to `ValidatePrincipal`. Authorized routes send the raw credential and generated operation identity once to `CheckPermission`; they do not call `ValidatePrincipal` first and Gateway does not implement a second JWT validator.

ANI OpenAPI authorization annotations generate one immutable operation and policy registry artifact. Gateway and IAM deployments pin its digest and `policy_revision` and verify them at startup. A revision mismatch fails with `503 AUTHZ_POLICY_MISMATCH` and raises an alert rather than returning a user denial. A missing annotation or unregistered operation fails generation or CI; if reached at runtime it fails closed with `503 AUTHZ_OPERATION_UNREGISTERED`. No default allow, generic read/write policy, or string-derived fallback exists.

Gateway gives each hot-path `ValidatePrincipal` or `CheckPermission` call a 500 ms deadline and does not retry it automatically. Invalid credentials map to `401`, current authorization or state denial to `403`, unavailable IAM or policy drift to `503`, deadline expiry to `504`, and authentication or authorization throttling to `429`.

Before calling IAM or constructing an outbound invocation, Gateway removes every client-supplied `x-ani-*` identity header. It keeps the IAM allow result as Gateway-local Trusted Principal Context; Roles and Permissions are never propagated as headers. When a downstream operation needs subject provenance, Gateway converts only the accepted fields into the optional Delegated Subject part of the common Workload Invocation and supplies whatever delegation evidence the ADR-0022 follow-on decision requires.

Under the proposed follow-on model, the first-hop service would verify the Gateway as the direct Workload caller for the exact target, then construct its local Workload Invocation with a separate optional Delegated Subject; every subsequent synchronous business gRPC/HTTP call would repeat direct-caller authentication and reconstruct, rather than forward, delegated context. A current holder may carry only its own WAT plus an IAM-new, exact holder/receiver/target-bound receipt to that named business receiver; the receiver may submit the just-received opaque evidence only to IAM over its own authenticated channel for validation or attenuation. It may never present that evidence as its own direct-caller Credential or forward the parent receipt unchanged; only a newly attenuated child receipt bound to the new holder/receiver may enter the next business hop. Exact peer/WAT/receipt mechanics remain pending. Delegated context cannot substitute for the caller Workload's authority, and an origin decision ID alone is never proof.

Internal listeners are not published through public ingress. The proposed target lets Core and Services handlers accept a typed Workload Invocation only after direct Gateway Workload authentication and any delegated evidence validation; until that model is accepted and implemented, the path remains unavailable. Internal operations use separate listener or route allowlists and NetworkPolicies, and clients cannot bypass Gateway by presenting an Access Token or forged context directly to a service.

When an operation requires authoritative resource ownership that IAM cannot read, `CheckPermission` returns a typed obligation and the owning Handler loads the resource and enforces its actual Tenant or owner. Generated checks prevent registration when the required obligation handler is absent; Gateway never connects to a business database to perform this check.
