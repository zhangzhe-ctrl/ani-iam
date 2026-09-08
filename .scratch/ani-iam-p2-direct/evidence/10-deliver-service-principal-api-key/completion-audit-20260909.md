# DP2-10 requirement-by-requirement completion audit — 2026-09-09

This audit treats the current worktrees and executable gates as authoritative. A focused green test does not override missing production composition-root wiring.

| # | Requirement | Result | Authoritative evidence / remaining gap |
| --- | --- | --- | --- |
| 1 | `AuthenticationService.ValidatePrincipal` Bearer API Key path | `pass` | Domain/service/data and real PostgreSQL/Redis behavior pass; production `cmd/server/app.go` now constructs one Redis usage aggregator and injects it into both Authentication and Authorization use cases. The observer is a required constructor argument and a missing observer returns a stable dependency failure rather than panicking. |
| 2 | missing/malformed/unknown/expired/revoked → stable `401` | `pass` | Unit failure matrix plus real restricted-PostgreSQL matrix; wrong-secret/expired/revoked are rejected before lifecycle lookup, including under a stale lifecycle. |
| 3 | valid Key with inactive Principal/Membership/Access/Lifecycle → stable `403` | `pass` | Real restricted-PostgreSQL validation matrix covers all four states. |
| 4 | missing/stale lifecycle and unavailable dependencies → stable `503` | `pass` | Real restricted PostgreSQL covers both absent and stale lifecycle projections and a closed runtime pool; service and independent Adapter-process matrices cover IAM unavailable/timeout mapping. |
| 5 | only `Authorization: Bearer`; reject cookie/query/`X-API-Key` | `pass` | Envoy Adapter tests reject each alternate carrier before an IAM call. |
| 6 | one Service Principal, one fixed Tenant, live Membership/Role permissions, no snapshot | `pass` | Composite foreign keys, deferred boundary triggers, Tenant-scoped readers, and real two-Tenant authorization evidence. Permission evaluation joins current role bindings. |
| 7 | Secret returned once; persist only digest and safe display prefix | `pass` | Domain/data tests plus real PostgreSQL inspection prove digest-only persistence. Secret redaction is checked across process output and responses. |
| 8 | real PostgreSQL Create/Get/List/Update SP and Create/List/Revoke Key; pagination/expiry/concurrency/conflict/audit rollback | `pass` | All listed real behaviors pass, including explicit positive Get/Update/Revoke RPCs. Production `CreateAPIKey` now receives the accepted Redis creation limiter through an explicit required dependency. |
| 9 | create abuse limiting, unusual active-key alert, 90-day stale alert only | `pass` | Real Redis limiting and aggregate PostgreSQL snapshots pass; no automatic revoke occurs. The production Kratos lifecycle continuously drains bounded Redis batches within a per-phase timeout and publishes aggregate stale/unusual gauges plus a no-label sample timestamp. Startup/stop/cancellation and sanitized failure logging have focused tests. |
| 10 | two-Tenant negative isolation for mutation/list/validation/authorization | `pass` | Real PostgreSQL tests cover all management paths across Tenant A/B, credential-derived validation boundaries, cross-Tenant authorization denial, and unbound invalid-credential audit routing. |
| 11 | disable/removing sole Membership atomically revokes active Keys; re-enable does not revive | `pass` | Real PostgreSQL transaction test proves disable/removal cascade, audit, zero active Keys, and that a later re-enable leaves all old Keys revoked. |
| 12 | API Key forbidden for password/credential/role/recovery/platform management before storage | `pass` | Frozen-registry category regression asserts password flows are not CheckPermission policies and sensitive IAM/recovery/platform operations accept only their frozen access-token/human or service-token/service kinds. Use-case tests prove credential-kind rejection occurs before API Key storage lookup. |
| 13 | independent Adapter process: allow, invalid/expired/revoked, deny, IAM unavailable, timeout, revision mismatch | `pass` | Independent built Adapter process exercises the complete matrix; auth-state cases additionally use the real IAM service with restricted PostgreSQL, Redis, TLS 1.3 mTLS, and no secret-bearing environment variables. |
| 14 | fake IAM/Gateway evidence does not replace real Envoy gRPC E2E | `pass` | Parent test runs a real IAM `AuthorizationService`; child builds and runs the Adapter as a separate process and calls its Envoy ext_authz gRPC API. |
| 15 | unified 24-hour idempotency ledger remains DP2-16 | `not_verified` | Intentionally not implemented or claimed by DP2-10. Accepted request fields are validated; no DP2-16 ownership is pulled forward. |

## Added closure evidence in this audit pass

- `pass`: real missing-lifecycle projection returns `503` without deleting or mutating shared data.
- `pass`: re-enabling a previously disabled Service Principal leaves every old Key irreversibly revoked.
- `pass`: positive real-PostgreSQL `GetServicePrincipal`, `UpdateServicePrincipal`, and `RevokeAPIKey` RPC paths now have explicit assertions, including stored revoke version.
- `pass`: frozen operation-registry category coverage now explicitly protects password, credential-management, Tenant role, Recovery Bootstrap, Platform IAM, and Platform Workload boundaries.
- Focused real gate:
  `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas go test -tags=integration ./tests/integration -run '^(TestRemovingServicePrincipalMembershipDisablesPrincipalAndRevokesEveryKey|TestAPIKeyRealPostgresValidationAndAuthorizationFailureMatrix|TestServicePrincipalManagementRealPostgresTenantPaginationConcurrencyAndRollback)$' -count=1 -v` = `pass`.

## Production wiring closure

- `pass`: the human-approved path expansion is recorded exactly in the claimed issue and is limited to `cmd/server/app.go`, `cmd/server/app_test.go`, `internal/server/observability.go`, and `internal/server/observability_test.go`.
- `pass`: `NewServicePrincipalUsecase`, `NewTenantAuthorizationUsecase`, `NewAuthenticationUsecase`, and `NewAuthorizationUsecase` require the limiter/observer explicitly; nil dependencies fail closed without a panic.
- `pass`: one production Redis usage aggregator is shared by Authentication, Authorization, and the lifecycle flusher; the Redis namespace already present in runtime configuration is reused without changing `configs/**` or `internal/conf/**`.
- `pass`: worker startup performs an immediate flush, drains successive bounded batches until empty or deadline, refreshes its snapshot under a separate timeout, and cancellation stops an in-flight flush before Redis/PostgreSQL clients close.
- `pass`: `ani_iam_api_key_stale_non_expiring_count`, `ani_iam_service_principal_unusual_active_api_keys_count`, and `ani_iam_api_key_operational_snapshot_timestamp_seconds` are label-free gauges; timestamp zero means no successful sample, so unavailable/stale sampling is distinguishable from a genuine zero count.
- RED/GREEN: missing metric instruments, missing worker lifecycle, optional dependency call sites, nil dependency panics, and one-batch-only flushing each produced focused failing tests or compile gates before the minimal GREEN implementation.

## Final pre-commit gates

- `pass`: `go test ./... -count=1`.
- `pass`: `go test -race ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server -count=1`.
- `pass`: `go vet ./internal/... ./cmd/server`.
- `pass`: full restricted-dependency integration suite:
  `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas DP2_ANI_ENVOY_ADAPTER_DIR=/home/chabking/workspace/ANI-direct-p2-06-14/repo/services/envoy-authz-adapter go test -tags=integration ./tests/integration -count=1` (`285.242s`).
- `pass`: focused real PostgreSQL lifecycle/error/management matrix, real Redis creation/usage controls, and independent real-IAM/mTLS/Envoy-process matrix all reran successfully after production wiring.
- `pass`: ANI Envoy Adapter `go test -race ./... -count=1`, `go vet ./...`, and ANI `git diff --check`.
- `pass`: IAM `git diff --check`.
- `pass`: IAM descriptor `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a`, synchronized ANI IAM descriptor with the same hash, Core descriptor `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a`, IAM Admin Proto `332dc8ad82bdc9e7c07618808028316a0c9ab735d0b71177095cb7d2f3a7d316`, Authorization Proto `7799bef6830dcdd6de8cf19f40b6d4a79361a35c06664e078b8b0ac61cf22050`, and ANI operation registry `742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80` reproduce the accepted baselines.
- `pass`: current tracked/non-ignored files and Git history in both repositories have no matches for the scanned private-key, AWS, GitHub, Slack, or Google credential patterns. Deleted ANI paths were excluded from the current-file scan and history was checked separately.
- `pass`: final independent Spec review reports 0 blockers and 0 scope creep. Final independent Standards review clears every documented-standard blocker; it retains only a non-blocking P3 suggestion to deduplicate the two composition-root worker state machines.
- RED/GREEN repair: the first full integration rerun exposed a pre-existing target policy fixture that omitted the now-fail-closed access-token/human credential kinds and lacked the required denial-audit ID. The focused test failed with `no fixed ID available`; the fixture was corrected, the focused test passed, and the subsequent complete integration suite passed.

## Completion decision

All in-scope numbered requirements are `pass`; item 15 remains explicitly `not_verified` because the unified ledger belongs to DP2-16 and was neither required nor claimed here. DP2-10 remains `claimed` only until the final independent Standards/Spec re-review, complete gates, exact staging audit, evidence finalization, and the two local commits finish. No `api/**`, `configs/**`, `internal/conf/**`, `deploy/**`, ANI OpenAPI/registry, or other ANI path was required or modified.
