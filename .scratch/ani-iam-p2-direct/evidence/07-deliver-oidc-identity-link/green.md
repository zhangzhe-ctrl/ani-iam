# DP2-07 GREEN evidence

Status: `pass` for the authorized Console/Tenant OIDC slice.

## Delivered behavior

- `coreos/go-oidc/v3` and `golang.org/x/oauth2` implement discovery,
  Authorization Code exchange, PKCE S256, issuer/audience/signature/expiry and
  nonce verification. Redirect URI, state, code, nonce, verifier, issuer and
  subject are compared without caller-input canonicalization.
- Redis stores only digest-keyed, ten-minute OIDC operations and an idempotency
  binding. A Lua get-and-delete admits one exact state. A whitespace-prefixed
  state cannot address or consume the valid state's key.
- OIDC login requires an existing active `dex` Identity and verified email; it
  never links by email. Session, Tenant Grant, Refresh family/token and success
  Audit commit in one PostgreSQL transaction after a lock-time state recheck.
- Identity Link requires a valid human Access Token and recent online
  reauthentication both before Provider exchange and again under transaction
  lock. Identity and success Audit commit together.
- Provider, verified-email, Identity/email-conflict and lock-time rejection
  paths record redacted failure Audit. Login failure Audit is anonymous within
  the operation Tenant; Identity-Link failure Audit retains the authenticated
  actor and human authentication method. Failure-Audit persistence errors fail
  closed as dependency errors.
- Password and OIDC Sessions persist authoritative single-valued
  `authn_methods` as `password` and `oidc` respectively. The same domain Session
  value drives the response and the Access Token authentication-method claim.
- The sole composition root loads the OIDC client secret from an absolute
  mounted file, constructs a bounded HTTP client, real Provider, Redis store,
  PostgreSQL reader/unit of work and service. Gateway workload authorization
  contains exactly the four frozen OIDC RPCs in addition to prior grants.

## Real dependency evidence

The isolated Provider image is pinned to OCI index
`ghcr.io/dexidp/dex:v2.40.0@sha256:3e35d5d0f7dbd33fbadc36a71ff58cf4097ab98d73d22f6cb9a6471a32e028af`;
the selected linux/amd64 manifest is
`sha256:59d8b5540fcf4e876b70fb5438321c594b84a5b4a255540e9bb41834fd797770`.
The test gives Docker the atomic random host-port allocation and maps only the
fixed logical issuer authority to that task-owned endpoint.

After the final review corrections, the focused real Redis, PostgreSQL and Dex
command passed in `4.486s`. It proves digest keys, idempotency, expiry,
16-consumer single use, malformed-state non-consumption, restricted-role
Tenant-scoped reads/writes, two-Tenant negatives, Session authn-method
persistence, Audit uniqueness/rollback, and real discovery/code/JWKS/PKCE.

The exact-final-tree complete command passed:

```text
DP2_ATLAS_BIN=/tmp/ani-iam-atlas-community-v1.3.0
GOCACHE=/tmp/ani-iam-dp2-07-go-cache
GOTMPDIR=/tmp/ani-iam-dp2-07-go-tmp
go test -tags=integration ./... -count=1
```

Every package passed; `tests/integration` passed in `132.483s`.

The exact-final-tree race command also passed:

```text
DP2_ATLAS_BIN=/tmp/ani-iam-atlas-community-v1.3.0
GOCACHE=/tmp/ani-iam-dp2-07-go-cache
GOTMPDIR=/tmp/ani-iam-dp2-07-go-tmp
go test -tags=integration -race ./... -count=1
```

Every package passed; `tests/integration` passed in `103.038s`. A final
`docker ps` and `docker ps -a` both returned no containers, so the test-owned
PostgreSQL, Redis and Dex resources were removed.

## Review-driven corrections

Independent review found and the implementation corrected:

- raw state was previously trimmed before Redis key derivation, allowing a
  malformed callback to burn a valid operation;
- login lock-time state rejection initially rolled back without failure Audit;
- Session authentication methods initially existed only in tokens/responses;
- token/JWKS deadlines initially lost `context.DeadlineExceeded` and mapped to
  503 instead of 504;
- authenticated Identity-Link Provider/email/conflict failures initially lacked
  failure Audit; and
- link lock-time reauthentication loss initially acquired a dependency marker
  instead of retaining its pure permission-denied classification.

All corrections have focused regression tests and are included in both final
complete runs above.
