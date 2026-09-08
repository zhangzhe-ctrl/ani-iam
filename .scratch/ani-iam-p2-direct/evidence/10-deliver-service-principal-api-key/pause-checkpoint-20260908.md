# DP2-10 pause checkpoint — 2026-09-08

## State boundary

- Ticket `10-deliver-service-principal-api-key.md` remains the sole `claimed` ticket.
- IAM baseline remains branch `codex/direct-p2-06-14` at committed HEAD `a56a332834967603eb47a3824727982032bc4f5e`; all DP2-10 work is intentionally uncommitted.
- ANI dedicated worktree remains outside this checkpoint's mutation set; the Envoy adapter has not been changed.
- No push, PR, deployment, cutover, Credential invalidation, or deletion was performed.

## Immutable local snapshot

- Committed base: `a56a332834967603eb47a3824727982032bc4f5e` (tree `ef84bfb6382eaa73f13a68754f506edeb17f2206`).
- Tracked binary diff SHA-256, excluding this checkpoint and the ticket status file: `e1387ff7898b606ceaf4f524346525fc2e3b6df0a0eb0ae064b7389882a4371c`.
- Sorted untracked-file checksum manifest SHA-256: `bf3b9c89d1d0afd49cde3f814e6385e91bf138f084e0be84d2e31c10490b1af4`.
- Pre-transition porcelain status SHA-256: `6758761e12cdf4d48630b9a03a282bd7188615ed970e02d5df086a075b970bcc`.
- The worktree remains intentionally dirty and must not be reset, stashed, rebased, cleaned, or reused for containment work.

## Completed slices

### pass — Service Principal creation

- Domain test proves one mutation contains the Service Principal, its sole active Tenant Membership, initial Role Bindings, and Audit.
- Real restricted PostgreSQL test passed and observed all five records within one Tenant boundary.
- Tenant-normalized names are unique and the database rejects non-service profiles and invalid fixed Membership boundaries.

### pass — API Key creation and storage

- Domain RED/GREEN proves the returned Bearer value has a bounded non-secret key ID and a high-entropy secret.
- Only SHA-256 digest and display-safe prefix are passed to persistence; the raw Secret is returned once.
- Real restricted PostgreSQL test passed and observed the digest plus Audit without the raw Secret.
- Expiry input requires exactly one of `never_expires=true` or a future `expires_at`.

### pass — Service Principal Membership removal cascade

- Domain and real PostgreSQL tests passed.
- Removing the sole Service Principal Membership disables the Principal, irreversibly revokes all active API Keys, and records both Membership and Service Principal Audit events in the same transaction.

### pass — management and policy foundations

- Existing composition-root constructors compile without an unauthorized `cmd/server/app.go` edit.
- Create/Get/List/Update Service Principal and Create/List/Revoke API Key handlers are implemented against the frozen Proto; create is the only response containing the Secret.
- The fixed ANI operation registry generator now preserves `credential_kinds` and `principal_kinds`; generated policies retain accepted registry SHA-256 `742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80`.
- Authorization domain RED/GREEN proves an allowed API Key represents a Service Principal without a Session/Grant, and an operation restricted to `access_token + human` rejects API Key before storage lookup.

### pass — real PostgreSQL API Key authorization tracer

- A public `AuthorizationUsecase.CheckPermission` tracer passed through the restricted PostgreSQL adapter with the accepted generated operation policy for `createInstance`.
- The bounded key identity selects one credential; business code performs constant-time digest comparison and resolves the Service Principal without a Session or Grant.
- The PostgreSQL read joins the fixed Membership, Role permission, Tenant Access, and fresh Tenant Lifecycle projection; a final active/digest/expiry-qualified write samples `last_used_at` before allow.

## Executed gates

The following focused real PostgreSQL tests passed with the pinned Atlas binary:

    TestServicePrincipalCreateUsesRestrictedPostgresAndOneAtomicTenantBoundary
    TestAPIKeyCreatePersistsDigestAndAuditWithoutRawSecret
    TestRemovingServicePrincipalMembershipDisablesPrincipalAndRevokesEveryKey
    TestAPIKeyAuthorizesServicePrincipalThroughRestrictedPostgres

The current non-integration compile/regression gate passed:

    go test ./internal/... ./cmd/server -count=1

`git diff --check` passed.

## Remaining before DP2-10 can be resolved

- `not_verified`: `AuthenticationService.ValidatePrincipal` API Key path and the complete real-adapter invalid/expired/revoked/boundary-denied/lifecycle-stale matrix with stable `401` / `403` / `503` mapping.
- `not_verified`: management list/update/revoke real-database pagination, expiry projection, concurrency, conflict, and Audit-failure rollback.
- `not_verified`: create abuse limiting and unusual/stale-key alert evidence.
- `not_verified`: cross-Tenant negative matrix for every Service Principal/API Key mutation and validation route.
- `not_verified`: independent Envoy Adapter valid, invalid, expired, revoked, permission-denied, IAM-unavailable, timeout, and policy-revision mismatch E2E.
- `not_verified`: full repository/integration/race/static/supply-chain/secret/history gates and independent Standards/Spec reviews.
- The unified 24-hour idempotency ledger remains explicitly deferred to DP2-16; DP2-10 must not claim that semantic.

## Resume point

Resume DP2-10 from the green real-PostgreSQL `CheckPermission` tracer by implementing `AuthenticationService.ValidatePrincipal` and the focused real `401` / `403` / `503` matrix. Do not begin Envoy changes until those IAM semantics are green. Do not claim DP2-11 until all remaining DP2-10 gates and both reviews pass and DP2-10 has its independent local commit.
