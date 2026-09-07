# DP2-06 Notification contract freeze

Audited: `2026-09-07`

## Immutable identity

| Input | Frozen value | Result |
|---|---|---|
| Repository | `github.com/zhangzhe-ctrl/ani-notification-service` | `pass` |
| Commit | `0e3f0a2b47fcc1fa96fa926cae2b9ab55bd25d84` | `pass` |
| Remote `refs/heads/main` | `0e3f0a2b47fcc1fa96fa926cae2b9ab55bd25d84` | `pass` |
| Go version | `v0.0.0-20260907002920-0e3f0a2b47fc` | `pass` |
| Module sum | `h1:i0sqGu+qg4M18cdGJ00sQgp8Pd+jx8JmVn9s+OmoNJ8=` | `pass` |
| Module `go.mod` sum | `h1:FeyV4il6PFMekDJoU7Fu7uWp+K0oQij09wwbw0GLVJQ=` | `pass` |
| Proto SHA-256 | `af226602b76ddd7b0cb312456f1845f67d1968eb0228da64fc1cced13a2671b5` | `pass` |
| Buf descriptor SHA-256 | `7be0a2fa062229717a311af952fc8b3bb7f58c1ef21cde2de741bcbbb4dfc195` | `pass` |

`git ls-remote` proved the exact public commit. `go list -m` resolved the
commit to the pseudo-version above, and the official Go proxy/sumdb supplied
the recorded checksums. Buf `v1.60.0` regenerated the descriptor from the
clean exact checkout and reproduced the Notification evidence digest.

## Accepted producer contract

IAM Password Action maps to exactly:

```text
RPC:          /notification.v1.NotificationService/SubmitNotification
Scope:        HumanPrincipal(principal_id)
Recipient:    HumanPrincipal(the same principal_id)
Destination:  Email(verified normalized snapshot)
Intent:       IamPasswordAction(action_url, setup|reset, console, expires_at)
Identity:     (authenticated_producer_id, request_id)
```

The outbox UUID is the stable `request_id`; the IAM operation UUID is the
Source resource and correlation ID; Source version is `1`. Notification returns
only after the immutable Notification and Delivery are durable. An ambiguous
outcome is retried with byte-equivalent semantic input, so the same producer
and request ID replays the original receipt; different semantics return
`ALREADY_EXISTS / IDEMPOTENCY_CONFLICT`.

Notification encrypts the action URL and destination in its own PostgreSQL
boundary, scrubs them after the retention deadline, and does not accept a raw
payload or template escape hatch. Its recorded local evidence is `L0 pass`,
`L1 pass`, `L2 pass`; real IAM/Gateway/workload integration remains
`not_verified`.

## Remaining workload boundary

The same clean commit intentionally has no gRPC TLS configuration and wires
`TrustedProducerIdentityMiddleware(nil)`. It therefore keeps
`trusted_identity_resolver=false`, readiness false, and returns
`UNAUTHENTICATED / WORKLOAD_IDENTITY_MISSING` before business processing.

A secure cross-service closure still requires a separate Notification change
that authenticates the IAM workload and grants only:

```text
producer_id: ani-iam
operation:   submit
type:        iam_password_action
scope:       all HumanPrincipal scopes
recipient:   HumanPrincipal
destination: Email
```

It must not grant Tenant Invitation, `get_own`, any default capability, or any
identity derived from request fields/metadata alone. At the frozen public API
commit alone, the IAM adapter was unit-testable but runtime wiring and L3 were
`not_verified`; the following local implementation commit closes that bounded
local seam.

## Local L3 closure

The previously remaining local boundary is closed by the separate clean
Notification commit
`a477a38280c8626b0fdf6664e7afb049d22c2a58` on
`codex/notify-dp2-06-l3`. It is based directly on the frozen public-contract
commit above and does not change `api/notification/v1`; IAM therefore keeps the
published pseudo-version and checksums already recorded in this file.

Notification commit `a477a38280c8626b0fdf6664e7afb049d22c2a58`
requires TLS 1.3 mutual authentication and maps only a verified leaf with the
sole raw DNS SAN `ani-iam` to the fixed Password Action Submit grant above. Its
generated repository descriptor SHA-256 is
`a2c7c536965fedc198bb099d572b3b7d30c4dd9e355820962cc0469f84aba8a6`;
the changed digest is caused only by internal config Proto because the public
Notification API diff is empty. Notification `make verify`, real PostgreSQL
`make integration -race`, `make audit`, and independent Spec/Standards reviews
all pass at that local commit.

IAM's process gate builds that exact clean checkout and uses a separate test
process, an ephemeral CA, a server leaf whose sole DNS SAN is
`ani-notification`, and a client leaf whose sole DNS SAN is `ani-iam`. The IAM
adapter reaches Notification's authenticated local runtime-disabled boundary
and receives the typed retryable `STORAGE_UNAVAILABLE` result; an action token
does not appear in the returned error or evidence.

This changes only the local L3 status to `pass`. Cluster issuance, Secret
projection, overlap/rotation/revocation, NetworkPolicy, deployment, cluster
E2E, and Production Ready remain `not_verified`. The local Notification commit
was not pushed or deployed. Its absent first-party license is covered only by
the user's explicit proprietary/no-license risk exception, not by an inferred
license declaration.
