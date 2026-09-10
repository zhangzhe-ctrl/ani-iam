# WR-19 synchronous invocation contract

Implementation slice: IAM mint/online verify and public gRPC adapters. Bootstrap acceptance is preserved; existing-connection reauthorization was accepted by the user on 2026-09-10; implementation and verification are in progress.

## Modules and owner boundary

- Public DTO module `github.com/zhangzhe-ctrl/ani-iam/api`, candidate `v0.1.0-rc.1` (unpublished), preserves existing `api/iam/v1` Go import paths.
- Public adapter module `github.com/zhangzhe-ctrl/ani-iam/sdk`, candidate `v0.1.0-rc.1` (unpublished). Dependencies are the API module, gRPC, protobuf and standard crypto/TLS; no IAM internal, ANI or Core imports. A separate `examples/workload-grpc` module demonstrates both sides against the formal local reference chain.
- Root remains Go 1.26.7. Public modules use Go 1.25.0, compatible with the existing Gateway/Session modules; existing gRPC/protobuf versions stay pinned. Temporary, recorded go.work bindings identify exact source snapshots; no absolute replace or publication.
- Owners normalize/validate their DTO before invocation and check real resource ownership and idempotency. The adapter provides mTLS, mint/verify, credentials and request binding, with no business retry. A handler never manufactures identity headers or parses bearer claims.

## First exact target and request binding

Target RPC `/ani.session.v1.SessionService/CreateSession`, audience `ani-session-gateway`, operation `session.create`. Source `createInstanceExecSession` + mode `exec`, or `createInstanceConsoleSession` + mode `vm_console`. Each source maps to the current registered `tenant/instances:create`, with the real resource/Tenant obligation retained. A read decision/operation or arbitrary decision ID has no mint authority.

Binding version `ani.grpc.invocation.v1`: SHA-256 over a deterministic protobuf encoding of the exact normalized business DTO, prefixed with the UTF-8 domain string, NUL, full RPC name, NUL, protobuf message full name, NUL. Unknown protobuf fields are rejected. Sender and receiver normalize using the same owner contract before digesting; any changed request/idempotency ID, Tenant, subject, instance, workload, mode, container, command, TTY, dimensions or requested protocol changes the digest. No raw terminal command/output is sent to IAM; IAM receives the digest plus typed target/Tenant/subject/resource/mode identifiers needed for authorization. Digest is not authority without valid IAM evidence and a matching TLS caller.

The SDK takes typed target and verified subject context. The original user credential is carried only from Gateway to IAM for a fresh mint authorization; it is never propagated to Session or stored with its ticket. Receiver derives observed peer from a verified mTLS connection, computes the binding from its actual request, and calls IAM with its own mTLS identity. Business DTO fields cannot override that observed peer.

## Credentials and online authorization

- `IssueWorkloadToken` is reachable only with the exact caller ingress grant and a separate current grant for the requested Session target. One exact audience/operation is minted; legacy free tenant/idempotency fields are reserved and removed from this previously unimplemented request. It creates no Session/Refresh and carries no ambient Tenant authority. TTL at most five minutes.
- `IssueDelegation` requires the exact ingress grant, a valid WAT bound to this direct mTLS caller, its current target grant, and current subject permission through the existing IAM evaluator. Delegation TTL is the minimum of 60 seconds, WAT expiry and subject credential expiry. It binds caller Principal/Binding and versions, target grant revision, current subject Session/Grant or API Key version, Tenant, source operation, policy revision, mode, resource, subject and request digest.
- `VerifyWorkloadInvocation` requires the receiver's own exact IAM verification grant. IAM verifies issuer, algorithms, type, audience, operation, time, all bindings and the receiver-observed direct peer. It re-reads current Workload/Binding/target Grant and subject/Tenant/access/permission state; no stale signature-only allow. The returned typed caller/subject/Tenant/target context applies only to this request.
- JWT uses the already pinned JWX EdDSA implementation and configured IAM keys. Explicit token types separate Workload and delegation from Human access tokens and password actions. Unknown issuer/kid/type/algorithm, malformed tokens, lifetime beyond cap, expired/stale versions or disabled identity/binding/grant fail closed. Normal CA-issued certificate renewal preserves the registered DNS identity; changing/revoking the Binding or IAM verification key invalidates dependent proof. No new universal credential or recovery platform is introduced.
- Mint success must durably append attributable security Audit before returning the credential. Audit failure returns an error and releases no usable token. No ordinary successful resource operation is forced into IAM's domain event stream.

## Failure and retry

Caller-side IAM and receiver online calls use bounded deadlines (default 2 seconds, never above the upstream deadline). Existing Gateway business timeout remains bounded separately. Unauthenticated for invalid proof/identity; PermissionDenied for ungranted target/current subject denial or binding mismatch; InvalidArgument for malformed request; Unavailable/DeadlineExceeded for dependency failures. Stable ErrorInfo reasons remain consistent with existing IAM conventions. No raw token, secret, ticket, request command or provider content in errors/logs.

The SDK never retries a mutating business RPC. A caller retry mints fresh delegation under current identity/permission and repeats the same owner idempotency key; Session's existing idempotency semantics remain authoritative. A revoked subject after a lost response cannot reuse an old receipt to bypass online checks. Credentials may be reacquired after dependency recovery; there is no offline or legacy Auth fallback.

The user accepted online checks at creation and ticket redemption, then every 30 seconds with a 2-second timeout. Revocation, denial or IAM failure closes the connection. The 60-second creation delegation cap is unchanged; a receiver-bound continuation reference permits only online rechecks for the already admitted owner request. WR-19 cannot finish without real lifecycle verification.
