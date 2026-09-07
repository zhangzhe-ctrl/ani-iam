# DP2-07 RED evidence

Status: `pass` as a reproducible baseline failure, not as a fabricated copy of
an unavailable terminal transcript.

The fixed pre-ticket baseline is
`b52907dc4cb919dbbbe68768734e0a453b36b954`. It contains the frozen OIDC RPCs
but no OIDC domain types, Provider adapter, Redis operation store, PostgreSQL
unit of work, or service implementation.

To reproduce the missing behavior without modifying the working tree, the
baseline was exported to a task-owned `/tmp` directory and the current OIDC biz
test was placed into that export. The first invocation accidentally ran from
the live module root and was rejected as an out-of-module package; it is an
invocation error and is not counted as RED. The corrected command ran inside
the exported baseline:

```text
GOCACHE=/tmp/ani-iam-dp2-07-go-cache
GOTMPDIR=/tmp/ani-iam-dp2-07-go-tmp
GOPROXY=off GOSUMDB=off
go test ./internal/biz \
  -run '^TestBeginOIDCLoginCreatesTenMinuteSingleUseOperationForExactRedirect$' \
  -count=1
```

It failed to compile because `OIDCOperation`, `OIDCVerifiedIdentity`,
`OIDCLoginState`, `OIDCReauthenticationState`, `OIDCLoginMutation`, and
`OIDCIdentityLinkMutation` did not exist. This is the expected baseline RED:
the frozen RPC surface alone did not implement OIDC behavior.

The subsequent focused tests were developed around exact redirect equality,
PKCE S256, ten-minute exclusive expiry, raw single-use state, verified email,
existing-Identity-only login, recent online reauthentication, explicit link,
transaction rollback, stable error classification, and redacted failure Audit.
Their GREEN and final full-gate results are recorded separately.
