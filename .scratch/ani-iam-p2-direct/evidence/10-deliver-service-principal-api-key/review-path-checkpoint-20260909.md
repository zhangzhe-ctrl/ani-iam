# DP2-10 independent-review path checkpoint — 2026-09-09

## Review result before remediation

- Standards: `fail`.
- Spec: `fail`.
- Reviews were independent and agreed on three runtime blockers: API Key creation abuse control must use the accepted real Redis dependency, `last_used_at` must be sampled or aggregated asynchronously rather than updated synchronously on successful authentication, and stale/unusual active-key signals must be operator-visible.
- Spec additionally confirmed that the existing Envoy process fixture uses a fake IAM server. A real IAM plus restricted PostgreSQL caller fixture remains required. Actual `/v1/chat` or `/v1/embeddings` operation binding is absent from the frozen registry and remains DP2-13 `not_verified`; this ticket must not change ANI OpenAPI or operation-registry sources to manufacture that binding.
- The shared 24-hour mutating-operation idempotency ledger is owned by DP2-16. DP2-10 validates the accepted request field but must not claim unified idempotency `pass` or implement DP2-16 early.

## Remediation completed inside the current Allowed paths

- `pass`: empty credential/principal allowlists now fail closed.
- `pass`: effective API Key expiry state is owned by `internal/biz`, not calculated by the service facade.
- `pass`: Tenant-first API Key indexes cover the principal/key and principal/creation-time paths.
- `pass`: runtime reader/UoW type assertions were replaced by explicit compile-time interfaces.
- `pass`: API Key randomness, hashing, and constant-time verification moved behind `internal/data` adapters; biz no longer implements those primitives.
- `pass`: `ValidatePrincipal` success/failure and denied authorization decisions emit required Security Audit records and fail closed when the audit write fails.
- Focused gate: `go test ./internal/biz ./internal/service ./internal/data -count=1` = `pass`.

## Exact path expansion requiring human approval

The remaining runtime fixes cannot be wired or exposed within the current Allowed paths. The minimal proposed addition is exactly:

1. `cmd/server/app.go`
2. `cmd/server/app_test.go`
3. `internal/server/observability.go`
4. `internal/server/observability_test.go`

Intended use is limited to:

- inject the already-established runtime Redis client and namespace into the API Key creation limiter and usage sampler;
- start and stop the bounded asynchronous PostgreSQL usage flusher through the existing Kratos lifecycle;
- register aggregate gauges for stale non-expiring keys and Service Principals whose active-key count is unusual, without credential or Tenant labels;
- verify construction, lifecycle shutdown, fail-closed dependency behavior, and metric exposure.

No `internal/conf/**` or `configs/**` change is proposed: the accepted constants and existing Redis namespace are sufficient. No `api/**`, `deploy/**`, ANI OpenAPI/registry, other ANI path, push, PR, deployment, cutover, credential invalidation, shared-infrastructure mutation, or deletion is requested.

## Subsequent progress inside the existing Allowed paths

- `pass`: creation abuse control is now a Tenant-plus-Principal Redis fixed-window limiter using the pinned Redis 7.4 fixture; Redis failure fails closed before transaction entry and secret generation.
- `pass`: successful API Key validation/authorization queues only the latest observation in Redis. Restricted PostgreSQL `last_used_at` remains unchanged until a bounded explicit flush; PostgreSQL failure preserves the pending observation, and a newer concurrent observation cannot be acknowledged by an older flush.
- `pass`: a credential-free aggregate PostgreSQL snapshot now exposes the two values required by operator observability: stale non-expiring key count and unusual-active-key Service Principal count. It deliberately carries no Tenant, Principal, key, or credential labels. Metric registration remains pending on the exact `internal/server/**` expansion below.
- `pass`: a new cross-repository test starts a real IAM `AuthorizationService` backed by the restricted PostgreSQL role and real Redis, requires TLS 1.3 mutual TLS from the independently built Envoy Adapter process, and proves valid, malformed, unknown, expired, revoked, and permission-denied credentials. Raw credentials cross the parent/child boundary only through an inherited pipe and are redacted from failure output.
- `pass`: representative frozen operation `createSandboxCodeRun` exercises the real IAM caller seam and required resource/Tenant obligation. Actual `/v1/chat` and `/v1/embeddings` operation binding remains DP2-13 `not_verified`; no registry/OpenAPI source was changed to manufacture it.
- `pass`: `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas go test -tags=integration ./tests/integration -run '^TestAPIKeyRuntimeControlsUseRealRedisAndAsynchronouslyFlushRestrictedPostgres$' -count=1 -v`.
- `pass`: `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas go test -tags=integration ./tests/integration -run '^TestAPIKeyRealPostgresValidationAndAuthorizationFailureMatrix$' -count=1 -v`.
- `pass`: `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas go test -tags=integration ./tests/integration -run '^TestServicePrincipalManagementRealPostgresTenantPaginationConcurrencyAndRollback$' -count=1 -v`.
- `pass`: `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas DP2_ANI_ENVOY_ADAPTER_DIR=/home/chabking/workspace/ANI-direct-p2-06-14/repo/services/envoy-authz-adapter go test -tags=integration ./tests/integration -run '^TestEnvoyAdapterIndependentProcessUsesRealIAMRestrictedPostgresAndRedis$' -count=1 -v`.

## Current stop state

- The four paths above have not been modified.
- DP2-10 remains the sole `claimed` ticket.
- DP2-11, DP2-12, and DP2-13 remain `ready-for-agent` and unclaimed.
- Current Allowed-path work is exhausted. Production injection, lifecycle flushing, and metric registration remain fail closed and cannot be completed until the exact four-path expansion is accepted or rejected.

## Credential precedence and unbound-audit remediation

A second independent review found two additional blockers inside the existing Allowed paths. Both are now remediated and independently re-reviewed as `pass`:

- `pass`: API Key credential verification is now a separate Tenant-scoped read over `api_keys` only. Constant-time digest comparison plus status/expiry rejection occurs before any Tenant Access, lifecycle, Membership, Role Binding, or permission lookup. A valid credential alone proceeds to authorization-state lookup. This preserves stable `401` for wrong-secret, expired, and revoked keys even when lifecycle is stale; a valid key under the same stale lifecycle remains stable `503`.
- `pass`: authentication failures no longer construct an audit `TenantScope` from `CheckPermissionCommand.TargetTenantID`. Missing/invalid access tokens and malformed/unknown/wrong-secret/expired/revoked API Keys use the explicit unbound anonymous-principal audit port. A Tenant audit is selected only after credential-owned identity and Tenant have been established.
- `pass`: real restricted PostgreSQL plus Redis integration proves stale-lifecycle precedence and proves a wrong-secret request targeting a different Tenant creates no audit row in that Tenant while an anonymous principal-bound audit is persisted.
- `pass`: focused unit coverage additionally proves two distinct untrusted target Tenant IDs cannot redirect invalid-access-token audit writes.

Affected gates rerun after remediation:

- `go test ./internal/biz ./internal/data ./internal/service -count=1` = `pass`.
- `go test ./internal/... ./cmd/server -count=1` = `pass`.
- `go test -race ./internal/biz ./internal/data ./internal/service -count=1` = `pass`.
- `go vet ./internal/... ./cmd/server` = `pass`.
- `git diff --check` = `pass`.
- `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas go test -tags=integration ./tests/integration -run '^TestAPIKeyRealPostgresValidationAndAuthorizationFailureMatrix$' -count=1 -v` = `pass`.
- `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas DP2_ANI_ENVOY_ADAPTER_DIR=/home/chabking/workspace/ANI-direct-p2-06-14/repo/services/envoy-authz-adapter go test -tags=integration ./tests/integration -run '^TestEnvoyAdapterIndependentProcessUsesRealIAMRestrictedPostgresAndRedis$' -count=1 -v` = `pass`.

Independent focused re-review:

- Standards: the TenantScope/audit-forgery blocker is fixed; no blocker was introduced by the credential-first split. The suggested invalid-access-token two-Tenant regression was added after the review.
- Spec: the stable-error-precedence blocker is fixed; no blocker was introduced by the credential-first split.
- Overall DP2-10 remains `fail` only at the unchanged production-wiring checkpoint. The four exact out-of-scope paths listed above remain untouched.

Additional closure gates after the focused re-review:

- `go test ./... -count=1` in IAM = `pass`.
- `go test -race ./... -count=1` and `go vet ./...` in the ANI Envoy Adapter module = `pass`.
- `git diff --check` in both repositories = `pass`.
- IAM descriptor, synchronized ANI IAM descriptor, Core descriptor, frozen IAM Admin/Authorization Proto sources, and ANI operation-registry source all reproduce their accepted SHA-256 values (`66552fe0...`, `66552fe0...`, `7dd40f90...`, `332dc8ad...`, `7799bef6...`, `742147f0...`) = `pass`.
- Current-file and Git-history scans for private-key markers and common AWS, GitHub, Slack, Google credential token formats in both repository scopes returned no matches = `pass` for the scanned patterns. Raw test API Key values remain process-local fixtures and are never persisted or printed.
- Path audit = `pass`: IAM changes remain within the DP2-10 Allowed paths; ANI changes remain under `repo/services/envoy-authz-adapter/**`; `api/**`, `configs/**`, `cmd/server/**`, `internal/server/**`, `deploy/**`, ANI OpenAPI/registry, and all other ANI paths remain unchanged by DP2-10.
