# DP2-06 RED evidence

## Environment-only attempt

Command:

```text
env GOCACHE=/tmp/ani-iam-dp2-06.wjgWFa/gocache GOTMPDIR=/tmp/ani-iam-dp2-06.wjgWFa/gotmp GOPROXY=off GOSUMDB=off go test -count=1 ./tests -run TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs -v
```

Result: `not_verified` as a behavior test. Compilation did not reach the test because the task-owned `/tmp` cache hit `disk quota exceeded`. The task-owned directory was removed and no source conclusion was drawn from this failure.

## Workload allowlist RED

Command:

```text
env GOCACHE=/dev/shm/ani-iam-dp2-06-gocache GOTMPDIR=/dev/shm GOPROXY=off GOSUMDB=off go test -count=1 -p=1 ./tests -run TestGatewayWorkloadIdentityAllowsFrozenPasswordActionRPCs -v
```

Result: `fail` as expected before implementation. Both frozen RPCs were denied before reaching the handler:

```text
RequestPasswordAction: PermissionDenied; called=false
CompletePasswordAction: PermissionDenied; called=false
FAIL github.com/zhangzhe-ctrl/ani-iam/tests
```

No password, action token, credential, key, or other Secret appears in this evidence.

## Destination snapshot RED

The biz test first required a known verified target to carry its normalized
email into the durable notification intent. Before implementation the build
failed because `PasswordActionTarget.VerifiedEmail` and
`PasswordActionNotification.DestinationEmail` did not exist. The real
PostgreSQL test also required a `destination_email` column. This failure was
caused by the missing target behavior, not by the environment.

## Outbox lease and terminal-state RED

The integration tracers were added before their adapter. Their first compile
failed because `NewPostgresPasswordActionNotificationOutbox` did not exist.
After the initial adapter was generated, the first real PostgreSQL execution
failed on the explicit closed intent mapping: the row stored
`password_reset`, while the domain purpose is `reset`. The adapter was then
fixed with a closed setup/reset mapping. A later RED required the claim API to
recover without a hard attempt cutoff and required an
`attention_required` CAS transition; it failed to compile until both seams
were implemented.

## Token-at-dispatch dispatcher RED

The dispatcher unit tracers were added before implementation. Their first
build failed on the absent dispatcher, transport-neutral submission type, and
retry/permanent error taxonomy. The tests require successful delivery marking,
byte-stable submission replay after an ambiguous response, and terminal
attention handling for permanent or twentieth-attempt failures.

All test credentials and capability strings in source are explicit test-only
literals. No runtime password or raw action token is included in this evidence.

## Verified destination binding RED

The real PostgreSQL tracer deliberately paired the verified target
`verified@example.com` with a different normalized outbox destination. Before
the invariant was implemented, `RequestPasswordAction` returned success and
would have committed the wrong recipient snapshot. This was a behavior RED,
not an environment failure. The transaction entry now requires exact
principal, operation, purpose, and verified-email agreement.

## Durable identity validation RED

The dispatcher tracer replaced the durable operation UUIDv7 with a UUIDv4.
Before the validation change the value reached the submitter and the dispatch
returned success. The dispatcher and PostgreSQL adapter now both require UUIDv7
for outbox, operation, and principal identities before any external call.

## Frozen Notification adapter RED

After the clean external contract was pinned, the focused adapter test was run
before implementation:

```text
GOCACHE=/tmp/ani-iam-dp206-go-cache \
GOTMPDIR=/tmp/ani-iam-dp206-go-tmp \
go test ./internal/data \
  -run 'Test(GRPCPasswordActionNotificationSubmitter|NewGRPCPasswordActionNotificationSubmitter)' \
  -count=1
```

Result: `fail` as expected. Compilation reached the new public adapter seam and
reported only the absent `NewGRPCPasswordActionNotificationSubmitter` and
`PasswordActionNotificationSubmitterConfig` symbols. Dependency normalization
was completed before this run so module download or `go.mod` drift did not
substitute for the behavior RED.

## Notification mTLS client RED

The outbound transport test was written before its client implementation and
then run as:

```text
GOCACHE=/tmp/ani-iam-dp206-go-cache \
GOTMPDIR=/tmp/ani-iam-dp206-go-tmp \
go test ./internal/data -run 'TestNotificationGRPCClient' -count=1
```

Result: `fail` as expected. The build reported only the absent
`NewNotificationGRPCClient` and `NotificationGRPCClientConfig` seam. The test
requires a real TLS 1.3 gRPC handshake, verified server DNS identity, a client
certificate observed as DNS SAN `ani-iam`, wrong-server-name rejection, and
fail-closed configuration before any network call.
