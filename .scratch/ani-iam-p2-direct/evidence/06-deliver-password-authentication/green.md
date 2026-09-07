# DP2-06 GREEN evidence

## Workload allowlist GREEN

Command:

```text
env GOCACHE=/dev/shm/ani-iam-dp2-06-gocache GOTMPDIR=/dev/shm GOPROXY=off GOSUMDB=off go test -count=1 -p=1 ./tests -run TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs -v
```

Result: `pass`. Both frozen Password Action RPCs reached the downstream handler for an authenticated Gateway workload:

```text
TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs/RequestPasswordAction PASS
TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs/CompletePasswordAction PASS
PASS github.com/zhangzhe-ctrl/ani-iam/tests
```

This proves only the workload-identity routing slice. Password Action domain, persistence, concurrency, rollback, and real dependency gates remain `not_verified` at this point.

No password, action token, credential, key, or other Secret appears in this evidence.

## Notification-safe Password Action persistence GREEN

Result: `pass` at `2026-09-07T04:06:53+08:00`.

- A verified normalized email is copied into `notification_outbox` as an
  immutable destination snapshot in the same transaction as the action and
  allowlisted Security Audit.
- Unknown accounts persist only a SHA-256 account digest, create no action or
  outbox row, and append a redacted anonymous principal-boundary Audit whose
  target is the operation UUID. The Audit contains neither account text nor
  account digest.
- The outbox stores action identifiers, purpose, destination, expiry, lease,
  attempt and state metadata. It has no raw-token column.
- The action signer was exercised twenty times with identical durable claims;
  every compact Ed25519 JWT was byte-identical.

## PostgreSQL outbox state machine GREEN

The following real PostgreSQL behaviors passed against the pinned
`postgres:16.4-alpine` digest and restricted runtime role:

- two concurrent workers have exactly one claim winner through
  `FOR UPDATE SKIP LOCKED`;
- a claim cannot be recovered before its five-minute lease and can be
  recovered at expiry;
- a retry is unavailable before `available_at` and becomes claimable at that
  instant;
- delivery, reschedule, and attention transitions use status plus version CAS;
- a stale worker receives `ErrVersionConflict`;
- `delivered` and `attention_required` are terminal and cannot be reclaimed.

The complete real dependency suite then passed in `151.994s`, including
Password setup/reset, eight-way idempotent request and completion replay,
single-winner completion, reset of only the target Human's sessions/grants/
families/tokens, another-Principal isolation, required-Audit rollback,
five-failure durable lock, unknown-account dummy Argon verification, restricted
roles, empty-database replay, and real Redis account/source-IP hashing and
outage behavior. The pre-existing ANI process tracer was intentionally skipped
because this ticket did not supply `DP2_ANI_GATEWAY_DIR`; it is not counted as
Notification or Gateway evidence.

## Token-at-dispatch dispatcher GREEN

The transport-neutral dispatcher unit gate passed:

- claims are minted in memory from durable operation/principal/purpose/
  issued-at/expiry fields;
- the outbox UUID is the stable Notification request ID and the operation UUID
  is the stable source and correlation identity;
- an ambiguous response is rescheduled with exponential backoff and the next
  attempt submits an identical command and action token;
- permanent contract failures and the twentieth failed attempt use the
  `attention_required` CAS transition;
- no submission DTO or raw action token is persisted or logged.

Generation inputs and current generated output hashes:

```text
sqlc v1.31.1 archive sha256 497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354
sqlc binary sha256 0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f
migration sha256 8a4b5f50dac1313dbb906ed06ed3464117dbaee16b85f9c8540e1e896b03bad2
atlas.sum sha256 26b66354bc970b1a975e29ce8ce50339dc07fd84901e2156016be867245d6dc7
query source sha256 c6fec7275992ea8923e9e508313b7526ea5b75e62838fe5db6f5ebcccce4d0af
generated query sha256 6b20230864e4e9189a3b2a31553419836fd171c480630274c1abee94a383755a
```

These are pre-freeze results. The real Notification adapter, its immutable
module/descriptor, composition-root wiring, cross-service L0-L2 evidence, and
all final post-dependency gates remain `not_verified`.

## Pre-freeze regression and integrity gates

Result: `pass` for the current Notification-independent tree. These gates must
run again after the immutable Notification dependency is integrated.

- exact `go test ./... -count=1` with loopback permission: all packages pass;
- serial `go test -race` for biz/data/service/tests: pass;
- `go vet ./...`: pass;
- `go mod tidy -diff`: pass with no module-file drift;
- contract descriptor, strict fixture and immutable pin tests: pass;
- fixed sqlc `v1.31.1` fresh-directory generation: recursive diff `0`;
- Atlas Community `v1.3.0` validate/hash: pass and `atlas.sum` SHA unchanged;
- fixed Buf `v1.72.0` lint/generate/descriptor in a fresh directory: recursive
  API directory diff `0`;
- `git diff --check`: pass;
- high-confidence private-key, AWS, GitHub and Slack credential patterns in the
  text diff: no match;
- production biz/data/service logger calls containing password/action-token
  values: no match.

The verified-destination negative integration now passes and proves that a
different recipient rolls back request/action/outbox/Audit to zero rows. All
Password Action request tests, including eight-way replay, pass together in
`23.182s` against the real restricted PostgreSQL runtime role.

The durable-identity negative unit test also passes: a non-UUIDv7 operation is
moved to `attention_required` before token issuance or submitter invocation.

Fixed `govulncheck v1.7.0` then scanned 34 root packages, 84 modules, integration
test variants, and the Go standard library. It reported zero affected symbols
or packages. Module inventory still contains the known no-fix
`GO-2026-5932` advisory for `golang.org/x/crypto/openpgp`; no scanned package
imports or calls it, so it is recorded but does not become an affected-code
pass claim.

## Frozen Notification gRPC adapter GREEN

The focused adapter gate passed against the exact generated client from
`github.com/zhangzhe-ctrl/ani-notification-service@v0.0.0-20260907002920-0e3f0a2b47fc`:

```text
ok github.com/zhangzhe-ctrl/ani-iam/internal/data 0.004s
```

The adapter maps the stable outbox/request identity to the frozen
`SubmitNotification` request, including matching HumanPrincipal Scope and
Recipient, verified email Destination, setup/reset Purpose, Console Audience,
source/correlation identity, deadlines, locale, and an HTTPS action URL whose
token exists only in memory for the RPC call. Frozen structured errors map as
follows:

- storage/intake-key unavailability and ambiguous transport/deadline outcomes:
  retry with the same request ID and semantic input;
- identity, capability, request, action-origin, idempotency-conflict, and
  template failures: terminal `attention_required`;
- invalid local durable values are rejected before transport.

The complete `internal/data` package then passed in `0.416s`. Because the root
Notification module requires `github.com/jackc/pgx/v5 v5.10.0` and Go 1.26.7,
Go minimal-version selection updated IAM from pgx `v5.9.2` and `go 1.26.0`.
The dependency-change regression then produced:

- `go test ./... -count=1`: `pass`;
- real tagged PostgreSQL/Redis integration with fixed Atlas Community v1.3.0:
  `pass` in `128.794s`;
- `go test -race ./internal/data -count=1`: `pass` in `1.872s`;
- `go vet ./...`: `pass`;
- `go mod verify`: `pass`;
- `go mod tidy -diff`: `pass` with no output;
- immutable Notification contract-pin test: `pass`;
- `git diff --check`: `pass`;
- adapter production logging calls containing the action token: none.

The first tagged integration attempt did not reach PostgreSQL behavior because
`DP2_ATLAS_BIN` was absent; it is environmental `not_verified`, not a source
failure. The rerun fixed the input to Atlas Community `v1.3.0`, binary SHA-256
`10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b`,
and passed. Runtime wiring and real cross-service workload identity remain
`not_verified`.

## IAM outbound Notification mTLS client GREEN

The focused real TLS/gRPC loopback gate passed in `0.010s`:

```text
ok github.com/zhangzhe-ctrl/ani-iam/internal/data 0.010s
```

The client:

- requires a task-configured literal loopback endpoint for this isolated
  ticket;
- requires absolute client certificate, private-key, and server-CA paths;
- requires a canonical Notification server DNS name;
- loads an X.509 client certificate with ClientAuth EKU;
- uses TLS 1.3 with an explicit server trust pool and ServerName;
- exposes no plaintext, insecure, header, or metadata identity fallback; and
- owns and closes its gRPC connection.

The test server required and verified the client chain, observed exactly DNS
SAN `ani-iam`, and rejected a client configured for the wrong Notification
server DNS identity. This proves the IAM outbound transport seam only. The
current Notification commit still lacks its corresponding TLS listener and
trusted Producer resolver, so real L3 remains `not_verified`.

After the canonical DNS review fix, the focused mTLS test again passed in
`0.010s`; `go test -race ./internal/data -count=1` passed in `1.754s`; full
`go test ./... -count=1`, `go vet ./...`, and `git diff --check` also passed for
the resulting tree.

A final adapter secrecy regression injected a remote gRPC error message
containing a test-only action-token literal. Normal and race runs passed, and
the returned stable retry error did not contain the remote message.

## Final Notification L3 and runtime wiring GREEN

The user-approved config/composition extension and local Notification L3
boundary completed RED/GREEN on `2026-09-07`:

- Config RED: `internal/conf` did not define the required Notification tuple.
  GREEN: the generated `Notification` message now requires the literal-loopback
  endpoint, absolute client certificate/key/server-CA paths, exact server DNS
  identity `ani-notification`, canonical HTTPS Console action base, supported
  locale, dispatch interval, and submission timeout.
- Worker RED: `cmd/server` had no lifecycle-owned dispatcher worker. GREEN: the
  composition root constructs the mTLS client, submitter, durable outbox,
  token-at-dispatch dispatcher, and bounded worker; Kratos starts/stops the
  worker before closing its gRPC connection and PostgreSQL/Redis resources.
  Dispatcher error text is never logged.
- Client-identity RED: the outbound constructor accepted a valid ClientAuth
  certificate for another workload, multiple DNS names, `otherName`, and
  `registeredID`. GREEN: it parses the raw SAN extension and accepts exactly
  one primitive DNS GeneralName equal to `ani-iam`.

The real two-process command passed:

```text
DP2_NOTIFICATION_DIR=/tmp/ani-notification-dp2-06-l3 \
go test -tags=integration ./tests/integration \
  -run '^TestRealIAMAdapterToNotificationProcessMutualTLS$' -count=1 -v
```

The separate test process used the real IAM gRPC adapter while a child process
ran clean Notification commit
`a477a38280c8626b0fdf6664e7afb049d22c2a58`. Task-owned ephemeral CA and leaf
material existed only below `t.TempDir()`. The exact IAM identity completed the
TLS 1.3 handshake and reached Notification's authenticated, locally
runtime-disabled boundary, returning typed retryable `STORAGE_UNAVAILABLE`.
The returned error contained no action token. Result: `pass` in `20.651s`.

The final all-package race command then passed with fixed Atlas Community
`v1.3.0`, the pinned PostgreSQL/Redis inputs, and the same exact Notification
worktree:

```text
go test -tags=integration -race ./... -count=1
```

All non-integration packages passed and `tests/integration` passed in
`74.178s`. The first attempt omitted the mandatory `DP2_ATLAS_BIN` and was
rejected before PostgreSQL behavior; it is an environmental failed invocation,
not a source result. The complete rerun supplied the already-fixed Atlas binary
SHA-256
`10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b`.

Final generated-output reproduction also passed:

```text
Buf v1.72.0 binary sha256:     8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af
IAM descriptor sha256:        66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a
conf.proto sha256:             fccebc5b2419789d844ca7ffa987acabffd44f7dc16e78fcd9902d65cd4f176e
conf.pb.go sha256, both runs: e986665982bb9acfaa4c6ee229b16db38bb44995eacc11f816cc6c82686674d4
sqlc v1.31.1:                  generated output unchanged
Atlas Community v1.3.0:       validate/hash passed; atlas.sum unchanged
```

Final non-integration tests, `go vet ./...`, `go mod tidy -diff`,
`go mod verify`, contract pins, and `git diff --check` pass. Fixed
`govulncheck v1.7.0` reports zero affected vulnerabilities with the integration
tag. Deterministic SBOM and Secret-scan details are in `supply-chain.md`.

Local Notification L3 is now `pass`. Cluster certificate issuance, Secret
mounting, rotation/revocation, deployment, NetworkPolicy, and cluster E2E stay
`not_verified` under the approved boundary.

## Post-review corrections and accepted cross-store boundary

Final review found and the implementation corrected four additional issues:

- every unknown-account Password Action request now appends a redacted Audit
  in the same PostgreSQL transaction as its request row;
- the two-process gate now checks Notification runtime commit
  `a477a38280c8626b0fdf6664e7afb049d22c2a58` and rejects any tracked or
  untracked worktree change before building;
- Redis uses server time and increasing per-account/per-IP cooldowns of
  1/2/4/8 seconds before the fifth failure's fifteen-minute lock (integration
  tests use the same progression with a 10 ms base); and
- a durable PostgreSQL account lock performs dummy Argon verification and
  returns the same invalid-credential result as an unknown account instead of
  exposing a known-account-only 429 after Redis state loss.

The post-correction unit suite passed, the focused Redis increasing-delay gate
passed, the combined real PostgreSQL/Redis rollback/non-enumeration gates
passed, and the exact-clean Notification two-process gate passed. These are
focused correction results, not the final full closure run.

PostgreSQL and Redis do not share a transaction. The current conservative
ordering can preserve a durable failure/Audit when the later Redis failure
counter write returns 503, and can preserve an old Redis account bucket when
the preceding PostgreSQL login commit succeeds but Redis reset fails. Neither
case grants unauthorized access.

On `2026-09-07`, the Owner explicitly approved this recommended bounded
security-state persistence model for DP2-06. The ticket and
`cross-store-decision.md` now state the exact semantics. Strict cross-store
atomicity, durable coordination, repair observability, and login-result replay
remain `not_verified`; no wider subsystem or path was authorized. The previous
review blocker is therefore closed by an explicit Owner decision, subject to a
fresh complete gate run and final Standards/Spec review. Status at this point:
`not_verified` pending those final gates and reviews.

## Post-decision complete gate run

The first complete race integration run after the Owner decision exposed a
stale vertical-slice expectation. After one invalid password, the test retried
the correct password immediately and received the newly required increasing
cooldown. An isolated fresh PostgreSQL/Redis run reproduced the exact
`authentication rate limit exceeded` result, ruling out cross-test state. The
test now asserts the `password_account` retry metadata, waits its bounded 10 ms
integration cooldown plus margin, and then proves the real runtime-role login.
No production limiter behavior was weakened.

The corrected isolated command passed in `6.662s`. A pre-review complete
command then passed:

```text
DP2_ATLAS_BIN=/tmp/ani-iam-atlas-community-v1.3.0
DP2_NOTIFICATION_DIR=/tmp/ani-notification-dp2-06-l3
TESTCONTAINERS_RYUK_DISABLED=true ATLAS_NO_UPDATE_NOTIFIER=1
GOPROXY=off GOSUMDB=off go test -tags=integration -race ./... -count=1
```

Every package passed; `tests/integration` passed in `130.127s`. This result was
later superseded by final review corrections and is not used as exact-final-tree
closure evidence. The exact-clean Notification two-process mTLS test was also
rerun alone and passed in `1.268s`.

The latest non-integration suite, `go vet`, `go mod tidy -diff`, `go mod
verify`, contract pins, and `git diff --check` pass. Reproducible generation
results are:

```text
sqlc v1.31.1: generated output recursive diff 0
Atlas Community v1.3.0: validate/hash pass; atlas.sum diff 0
Buf v1.72.0 API lint/generate/FileDescriptorSet: directory diff 0
IAM descriptor SHA-256: 66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a
conf.proto SHA-256: fccebc5b2419789d844ca7ffa987acabffd44f7dc16e78fcd9902d65cd4f176e
conf.pb.go SHA-256: e986665982bb9acfaa4c6ee229b16db38bb44995eacc11f816cc6c82686674d4
```

The configured remote `protoc-gen-go:v1.36.11` replay was blocked by the
execution safety policy because it could disclose the internal config Proto to
an external plugin service. The already-cached exact v1.36.11 plugin binary
(SHA-256
`f8682681cbeb0bbf9e9570ca9d90fdd9783d8170a7e995af122356fe5a9f3cb3`)
was used locally instead and reproduced `conf.pb.go` byte-for-byte. The remote
transport invocation remains `not_verified`; generated content reproducibility
is `pass`.

Fixed `govulncheck v1.7.0` again reports zero affected symbols/packages; the
unreachable module-only `GO-2026-5932` remains recorded. The deterministic
95-component SBOM reproduced byte-for-byte with SHA-256
`9e35d90e8f7a1abe4da5d9ec79d7811afa6c0f3b2587df2d0ded5b735e94cbf0`.
The classified current-tree and history Secret-scan results are recorded in
`supply-chain.md`. Status now remains `not_verified` only for the required
final Standards/Spec reviews and staged-diff audit.

## Final-review durable-lock correction

The final Spec review found that after Redis state loss a durable PostgreSQL
lock produced a uniform first invalid-credential response but did not recreate
the account/IP cooldown. That allowed the second known-account attempt to
remain `401` while an unknown account became `429`, and failed to charge the
shared source-IP bucket. A unit regression first failed with zero Redis failure
calls. The locked path now records the same account/IP failure after dummy
Argon verification.

Standards review then found that this first correction did not append the
required authentication-failure Audit. A second unit regression first failed
with a nil Audit mutation. The final implementation reuses the existing
anonymous principal Audit-only PostgreSQL UOW path used for redacted failures,
then records Redis. It does not change or extend the existing durable lock.
Audit failure is classified as an authentication dependency failure and stops
before any Redis mutation.

The real empty-Redis plus durable-PG-lock integration passed in `3.148s` and
proved:

- one anonymous principal-boundary redacted failed-login Audit;
- unchanged `failed_attempts`, `locked_until`, and credential `version`;
- first response indistinguishable as invalid credential;
- immediate second request limited by `password_account`; and
- an unknown account on the same IP limited by `password_ip`.

Spec re-review returned `pass` with no actionable finding. The exact-final-tree
complete gate was then rerun:

```text
go test -tags=integration -race ./... -count=1
```

Every package passed and `tests/integration` passed in `110.650s`. Full normal
tests, `go vet`, module tidy/verify, contract tests, formatting, diff check,
fixed govulncheck, sqlc, Atlas, API Buf, and local exact-version config
generation also pass on this final tree. A Docker audit found no remaining
PostgreSQL or Redis container. Independent Standards and Spec re-reviews both
returned `pass` with no actionable finding. The exact 64-path staged-diff audit
is `pass` with an empty unstaged/untracked set.

The feature was committed locally as
`74d7e441435d35dc66a733c8bf4a93129b26de3f`
(`feat(dp2-06): deliver password authentication`). The worktree was clean
immediately after the commit, and the ticket is resolved. This documentation
closure does not change the implementation tree. No push, deployment, live
traffic, or cluster action was performed.
