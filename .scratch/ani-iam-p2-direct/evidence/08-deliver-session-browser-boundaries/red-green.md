# DP2-08 test-first record

All implementation slices were driven through an observable public use-case,
service or real storage boundary. Tests do not assert private helper calls.

## Session continuity types and orchestration

RED began with public RefreshSession, LogoutSession and SwitchTenant tests that
did not compile because the continuity commands, results, state and UOW methods
did not exist. GREEN added the framework-independent domain types and public
use-case methods, followed by service mapping tests.

The first slices proved:

- successful refresh consumes one token, creates one replacement and slides the
  Session idle deadline without changing the Grant boundary;
- reuse returns a generic credential error and revokes only the referenced
  Family/Grant boundary;
- logout revokes only the current Session and is opaque/idempotent;
- SwitchTenant revalidates source and target facts and creates or rotates only
  the target boundary.

## Real PostgreSQL lock boundary

The initial real PostgreSQL run failed with SQLSTATE `42501` when a lock query
attempted `FOR UPDATE` on the SELECT-only lifecycle projection. GREEN removed
that table from the write lock while retaining the runtime role's narrow
privileges and locking IAM-owned Principal, Membership, Session and Grant rows.
The real integration then passed without granting lifecycle write authority.

## Refresh concurrency and current version

Concurrent refresh coverage uses two callers against one real token. RED/early
implementation behavior exposed the need to serialize the Session and resolve
active-versus-consumed status inside the transaction. GREEN produces exactly one
rotation success and one generic reuse failure, then revokes only that boundary.

A same-Session operation in another boundary can advance the Session version.
The persistence comparison therefore ignores only that independently changing
version while still validating every credential relationship and returns the
actual committed Session version. A focused regression proves the public result
uses that committed value.

## Historical Membership selection

The real fixture was extended to contain an older `removed` Membership and a
current `active` Membership for the same Principal and Tenant. RED selected the
historical row and returned `tenant membership is inactive`. GREEN adds
`status <> 'removed'` to both lookup and locked target-selection queries; the
public SwitchTenant flow then selects the live Membership and passes.

## Review-driven OIDC and BOSS corrections

The first independent reviews exposed continuity fields added by DP2-08 but left
at zero values by the OIDC login path. A new assertion failed with Session
version `0`, Family version `0` and empty Refresh status while PostgreSQL would
persist `1`, `1` and `active`. GREEN initializes those domain objects explicitly
and the returned result now matches its committed mutation.

The first BOSS Tenant Refresh/Switch tests reached ID generation instead of
failing closed, demonstrating that a malformed internal state could issue a
BOSS-audience Tenant token. A Console-only guard made those tests GREEN. The
second Spec review then found that a consumed BOSS Tenant token entered reuse
handling before that guard; the added test failed by reaching `newIDs` and would
have produced a Tenant revocation/Audit with a production ID generator. Moving
the audience guard before consumed/reuse handling made active Refresh, consumed
Refresh and SwitchTenant all fail before mutation or token issuance.

## Environmental reruns

Restricted-sandbox loopback failures and one Goal-owned Go-cache tmpfs quota
failure are environment evidence, not RED product behavior. Each exact command
was rerun with the required loopback/Docker boundary; the Goal-owned build cache
was cleaned with `go clean -cache`, and the complete ordinary, race, real
PostgreSQL/Redis, and integration-race suites passed.
