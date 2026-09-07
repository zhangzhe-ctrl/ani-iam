# DP2-06 design blocker

Result: `fail` for closing DP2-06 under the current frozen inputs and Allowed paths. The workload-identity routing slice is `pass`, but two security inputs required by the accepted Password design are absent.

## 1. End-client source IP is unavailable to IAM

Source inspection:

```text
api/iam/v1/authentication_service.proto: PasswordLoginRequest contains account, password, audience, boundary, device_name, and idempotency_key only.
internal/server/workload_identity.go: peer.FromContext is used only to authenticate the immediate mTLS peer.
rg over api/, configs/, internal/, and tests/: no trusted source-IP field or Gateway-injected metadata contract exists.
```

The immediate peer of the IAM gRPC listener is the Gateway workload, not the public client. Using that peer address would collapse all end users behind the Gateway into one `password_ip` bucket and would not implement the accepted normalized-account plus source-IP limit. Trusting arbitrary incoming metadata would violate the fail-closed client-metadata boundary. Adding a new frozen Proto field or modifying ANI is forbidden by DP2-06.

## 2. A usable action token cannot be delivered without persisting plaintext Secret or adding a token-at-dispatch notification boundary

Source inspection:

```text
RequestPasswordActionResponse: operation_id and expires_at only.
CompletePasswordActionRequest: action_token, new_password, and idempotency_key.
migrations/: no password action or notification outbox relation exists yet.
cmd/server/app.go: injects PostgreSQL, Redis, the access-token codec, SecretGenerator, IDs, and Clock only.
internal/conf/: no password-action notification or envelope-encryption key configuration exists.
```

The accepted design requires a thirty-minute, single-use action token while the frozen request response intentionally does not return that Secret. A safe transactional notification outbox can store only the action claims and recipient, while an IAM-owned dispatcher mints the signed token in memory immediately before calling a pinned notification service; an encrypted envelope is another possible design. Persisting the raw bearer token in the outbox triggers the ticket stop condition `保存明文 Secret`; returning it as `operation_id` would make an unauthenticated reset requester receive the reset credential and would change the frozen contract's security semantics. DP2-06 does not authorize `cmd/server/app.go`, `internal/conf/**`, notification integration, or the immutable external contract needed to add either safe delivery boundary.

## Required human decision

Do not implement around either gap. Resume requires an explicit decision that preserves the accepted security properties, for example:

1. authorize and freeze a trusted Gateway-to-IAM source-IP metadata contract, with Gateway stripping any client copy and injecting the canonical address only after request parsing; and
2. authorize the proposed token-at-dispatch notification outbox (or another concrete non-plaintext design), its immutable notification dependency, and the exact additional IAM paths needed to wire it.

Alternatively, changing the DP2-06 acceptance criteria to treat the Gateway peer as the IP or to omit action-token delivery is a security/design change and requires explicit human acceptance. Until then, source-IP limiting and usable Password Action delivery are `not_verified`; DP2-06 remains `claimed` and must not be marked `resolved`.
