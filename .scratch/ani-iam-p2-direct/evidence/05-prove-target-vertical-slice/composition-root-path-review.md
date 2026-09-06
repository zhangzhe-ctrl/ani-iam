# DP2-05 composition-root path review

Result: `pass`

The frozen repository rule makes `cmd/server` the only composition root. After
the human approved the exact four-file extension, `cmd/server/app.go` now
constructs PostgreSQL and Redis clients, the key loader, Argon2id/JWX/registry
adapters, target use cases, the three service facades and the target gRPC
server through explicit constructors. It registers exactly
AuthenticationService, AuthorizationService and IAMAdminService.

The approved minimum extension is:

- `cmd/server/app.go`: explicitly construct and close the PostgreSQL/Redis
  clients, key loader, adapters, use cases and three target gRPC services.
- `cmd/server/app_test.go`: new composition-root RED/GREEN test through the
  existing `buildApp` seam.
- `cmd/server/main.go`: change the historical CP0 runtime identity to the
  target DP2-05 identity and extend sensitive config-key redaction.
- `cmd/server/main_test.go`: preserve and extend the logger redaction gate.

No other `cmd/**` path is used. The extension modifies only local IAM source
and tests. It does not modify deploy manifests, remote Git, shared databases,
traffic, credentials, or the existing ANI checkout.

Focused composition-root tests, exact three-service registration tests and the
real two-process Gateway E2E are `pass`. Runtime mTLS requires TLS 1.3, a
verified Gateway client certificate with the configured exact DNS SAN, and an
explicit per-RPC allowlist. The generated runtime configuration and its source
were updated together under the separately approved four-file `internal/conf`
extension.

Recovery before a successful Go/No-Go A commit is to revert only these four
uncommitted local files together with the rest of the DP2-05 Allowed-path diff.
After an accepted commit, recovery uses a new revert commit against the exact
DP2-05 SHA; reset, stash, amend and force operations remain forbidden.
