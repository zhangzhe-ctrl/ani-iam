# DP2-06 exact decision request

Status: `accepted direction / dependency pending`. The user explicitly accepted Decisions A and B on `2026-09-06`. The source-IP contract and token-at-dispatch security semantics are authoritative for DP2-06; the real Notification adapter remains gated on a clean immutable Notification contract artifact.

## Revalidated fixed inputs

- IAM remains `codex/direct-p2-06-14@0f9cb1c73bef12ee7183016ae012c1a52b008de0` with only the expected DP2-06 worktree changes.
- The parallel ANI safe-merge task received explicit human `Merge-Ready` acceptance and produced clean integration commit `745ab93817a402b6ebc05de8f79176162c730704` on `codex/direct-p2-safe-merge`.
- The accepted ANI commit still sends `PasswordLoginRequest` without any source-address value. Its public password action returns HTTP `202` with no body, while the IAM gRPC request response exposes only `operation_id` and `expires_at`.
- The safe-merge acceptance and commit do not expand DP2-06, permit an early ANI worktree, change the frozen IAM descriptor, or authorize a notification dependency.

## Decision A: explicit client-address contract

Recommended contract:

1. Add a compatible `source_ip` field to the internal `iam.v1.PasswordLoginRequest`; keep it absent from the public OpenAPI request body.
2. Gateway derives the value from a separately defined trusted client-address resolver, canonicalizes it as one IPv4/IPv6 literal with `net/netip`, and never copies a client-supplied `x-ani-*`, `Forwarded`, or `X-Forwarded-For` value without a configured trusted-proxy chain.
3. IAM rejects an absent or invalid value before password verification, passes only the canonical value into biz, and hashes it before constructing the Redis `password_ip` key. IAM does not retain the raw value in Session state, Audit, logs, or evidence.
4. Redis account and IP limits are independent and both fail closed. A successful authentication clears the account failure state required by the accepted rule; it must not erase an unrelated shared-IP abuse bucket.

Why an explicit Proto field is recommended over hidden gRPC metadata: cross-project inputs remain visible in the versioned descriptor, generated clients, fixture, and breaking/additive gates. This is an additive wire change, but it still changes the frozen contract and digest and therefore requires human acceptance.

Minimum path expansion if accepted:

IAM:

```text
api/iam/v1/authentication_service.proto
api/iam/v1/authentication_service.pb.go
api/iam/v1/authentication_service_grpc.pb.go
api/iam/v1/iam_descriptor.pb
```

ANI, from the exact accepted integration commit in the Goal-owned worktree:

```text
repo/pkg/ports/target_iam.go
repo/pkg/adapters/iam/client.go
repo/pkg/adapters/iam/client_test.go
repo/pkg/generated/pb/iam/v1/authentication_service.pb.go
repo/pkg/generated/pb/iam/v1/authentication_service_grpc.pb.go
repo/pkg/generated/pb/iam/v1/iam_descriptor.pb
repo/services/ani-gateway/internal/router/auth.go
repo/services/ani-gateway/internal/router/target_password_login_test.go
repo/services/ani-gateway/internal/router/target_iam_process_e2e_test.go
```

The exact trusted-proxy configuration paths cannot be approved safely until the resolver source is chosen. If the isolated path uses the direct TCP peer only, evidence must say `Gateway-observed peer IP`, not end-client IP; that distinction cannot be silently collapsed.

Recovery: revert only the future DP2-06 IAM/ANI commits; the added Proto field is optional on the wire, but IAM must not be rolled out in required-field mode before Gateway sends it. No push, deployment, traffic switch, shared Redis write, or credential invalidation is part of this decision.

## Decision B: token-at-dispatch notification outbox

Recommended boundary:

1. Request transaction stores the action operation, Principal/purpose binding, expiry, idempotency result, allowlisted Audit, and a notification-outbox row. It stores no raw action token.
2. An IAM-owned outbox worker claims the row, mints a purpose- and operation-bound signed token in memory, calls a pinned notification-service contract over an authenticated channel, and marks delivery state. Crash recovery may redeliver, but the database operation remains single-use and replacement cancels the prior operation.
3. Complete verifies the signed token and atomically locks/consumes the active operation, writes the Argon2id PHC, revokes every Session/Grant/Family/Refresh Token for only that Human on reset, appends Audit, and commits once.
4. Neither IAM nor the notification service may persist or log the raw token. The notification contract must define redaction, retry, idempotency, recipient handling, and delivery ownership before it can be used as evidence.

This avoids plaintext Secret persistence and is preferable to encrypting a raw token into the outbox. It cannot be implemented as a usable runtime until an immutable notification contract and isolated real dependency are supplied and accepted.

Minimum additional IAM wiring paths if that dependency is accepted:

```text
internal/conf/conf.proto
internal/conf/conf.pb.go
internal/conf/validate.go
internal/conf/validate_test.go
cmd/server/app.go
cmd/server/app_test.go
```

The action state, outbox schema, biz ports, signer/dispatcher adapters, service mapping, tests, and evidence otherwise fit the existing DP2-06 paths under `migrations/**`, `internal/biz/**`, `internal/data/**`, `internal/service/**`, `configs/**`, and `tests/**`.

Required immutable dependency inputs before implementation:

```text
notification service repository + exact commit
versioned SubmitNotification contract/descriptor + digest
authentication identity and endpoint contract
idempotency and retry semantics
explicit guarantee that raw action tokens are not persisted or logged
isolated test dependency identity and recovery procedure
```

Recovery: stop the task-owned outbox worker, remove only task-owned notification fixtures, and revert only the future DP2-06 commits. No shared notification queue, real recipient, production provider, deployment, or external delivery is authorized by this proposal.

## Decisions that are not recommended

- Treating the IAM mTLS peer address as the end-client IP.
- Trusting arbitrary forwarded metadata or request headers.
- Returning the reset token as `operation_id` to an unauthenticated requester.
- Persisting a raw action token in PostgreSQL, Redis, an outbox, logs, fixtures, or evidence.
- Marking notification delivery or source-IP abuse protection `pass` with only fake adapters.

Decisions A and B are accepted. DP2-06 may continue independently of the Notification implementation through its domain, persistence, source-IP, outbox, signing, throttling, lockout, revocation, and real PostgreSQL/Redis slices. Importing a Notification client, wiring the real adapter, and closing the cross-service gate remain `not_verified` until the immutable notification dependency is available and verified.
