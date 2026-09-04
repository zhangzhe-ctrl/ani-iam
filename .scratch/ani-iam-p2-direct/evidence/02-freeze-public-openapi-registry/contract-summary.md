# DP2-02 target operation registry summary

Result: `pass`

The public contract contains 295 unique operations. Every operation has one explicit `operationId`, one unique logical Gateway handler identity, one backend owner and one authentication classification. The generator rejects missing or duplicate values.

## Inventory

| Dimension | Count |
| --- | ---: |
| Public | 11 |
| Authenticated-only | 9 |
| Authorized | 275 |
| Gateway owner | 3 |
| Core Control owner | 205 |
| IAM owner | 87 |
| No IAM decision | 11 |
| `ValidatePrincipal` decision | 9 |
| `CheckPermission` decision | 275 |
| Zero IAM decision calls | 11 |
| One IAM decision call | 284 |
| Operations with typed obligations | 108 |
| Permission resources | 33 |
| Permission actions | 23 |

Policy revision: `sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa`

## Fail-closed rules

- Unknown route or operation: `503 AUTHZ_OPERATION_UNREGISTERED`.
- Missing or empty deployed policy revision: `503 AUTHZ_POLICY_MISMATCH`.
- Requested and deployed policy revisions differ: `503 AUTHZ_POLICY_MISMATCH`.
- Missing operation owner, handler, classification, authn, permission or required obligation metadata: generation fails.
- Duplicate `operationId`, route or Gateway handler identity: generation fails.
- Unknown permission resource/action/scope or obligation handler: generation fails.
- Public operations must override global security with an empty array and encode zero IAM calls.
- Protected operations accept the canonical Bearer contract and encode exactly one IAM decision; authorized operations do not first call `ValidatePrincipal`.
- Client-controlled OpenAPI parameters whose names start with `x-ani-` are rejected.

## Stable public error contract

| HTTP | Default code | Stable reasons |
| --- | --- | --- |
| 401 | `CREDENTIAL_INVALID` | `CREDENTIAL_INVALID` |
| 403 | `PERMISSION_DENIED` | `PERMISSION_DENIED` |
| 409 | `IDEMPOTENCY_CONFLICT` | `IDEMPOTENCY_CONFLICT`, `IDEMPOTENCY_KEY_EXPIRED`, `ROLE_IN_USE`, `VERSION_CONFLICT` |
| 429 | `AUTH_RATE_LIMITED` | `AUTH_RATE_LIMITED` |
| 503 | `IAM_UNAVAILABLE` | `AUTHZ_OPERATION_UNREGISTERED`, `AUTHZ_POLICY_MISMATCH`, `IAM_UNAVAILABLE`, `TENANT_IAM_NOT_READY` |
| 504 | `IAM_TIMEOUT` | `IAM_TIMEOUT` |

Every referenced response resolves to the shared `ErrorResponse`, which requires `code`, `message` and `request_id`.

## Trusted context

The Gateway contract first strips the entire client `x-ani-` prefix and may inject only:

- `x-ani-authn-method`
- `x-ani-boundary`
- `x-ani-decision-id`
- `x-ani-grant-id`
- `x-ani-principal-id`
- `x-ani-principal-type`
- `x-ani-session-id`
- `x-ani-tenant-id`

Role and Permission headers are not in the allowlist. The only current typed obligation is `resource_tenant_match`, owned by handler identity `core.resource_tenant`; a route that names an unavailable handler cannot be generated.

## Replacement trace

The registry pins all 28 accepted DP2-01 deletion IDs `D001` through `D028`, the immutable deletion-manifest commit and SHA-256, and the fixed ANI source commit/tree. It also proves complete classification of 28 related Core-v1 operations and 31 Services-v1 operations as preserve, target replacement, allowed breaking drift or final deletion.

Runtime use of this target registry remains `not_verified` until DP2-05; DP2-02 freezes the contract and generated consumer surface without switching callers.
