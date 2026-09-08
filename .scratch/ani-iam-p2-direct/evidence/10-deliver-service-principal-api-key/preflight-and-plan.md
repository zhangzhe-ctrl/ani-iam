# DP2-10 preflight and execution plan

## Claimed baseline

- IAM repository: `/home/chabking/workspace/ani-iam`
- IAM branch: `codex/direct-p2-06-14`
- IAM initial commit: `a56a332834967603eb47a3824727982032bc4f5e`
- IAM worktree and index: clean; `git diff --check` and staged diff check passed.
- ANI integration worktree: `/home/chabking/workspace/ANI-direct-p2-06-14`
- ANI branch: `codex/direct-p2-06-14`
- ANI initial commit: `4ff73e09c16df706af2aacdff6d763ed4aeaa873`
- ANI worktree and index: clean; `git diff --check` and staged diff check passed.
- Dependencies: DP2-04 and DP2-09 are `resolved`.
- Claimed-ticket audit: no other Direct P2 ticket was `claimed` before this ticket changed state.

## Fixed artifacts and scope

- The existing frozen `iam.v1` contract already declares `CreateServicePrincipal`, `GetServicePrincipal`, `ListServicePrincipals`, `UpdateServicePrincipal`, `CreateAPIKey`, `ListAPIKeys`, `RevokeAPIKey`, `ValidatePrincipal`, and `CheckPermission`; DP2-10 will not modify `api/**`.
- Frozen source hashes recorded at claim time:
  - `api/iam/v1/iam_admin_service.proto`: `332dc8ad82bdc9e7c07618808028316a0c9ab735d0b71177095cb7d2f3a7d316`
  - `api/iam/v1/authorization_service.proto`: `7799bef6830dcdd6de8cf19f40b6d4a79361a35c06664e078b8b0ac61cf22050`
- Allowed IAM paths are exactly `internal/biz/**`, `internal/data/**`, `internal/service/**`, `migrations/**`, `tests/**`, this issue, and this evidence directory.
- Allowed ANI path is exactly `repo/services/envoy-authz-adapter/**` in the dedicated integration worktree.
- Forbidden paths remain `api/**`, ANI OpenAPI, deploy, and every other ANI path.

## Confirmed public testing seams

The ticket, accepted Proto, specification, and ADRs already freeze these seams; no new interface is inferred:

1. IAM Admin gRPC service: Service Principal create/get/list/update and API Key create/list/revoke.
2. Authentication and authorization gRPC services: Bearer API Key through `ValidatePrincipal` and `CheckPermission`.
3. PostgreSQL repository/UoW boundary using the restricted runtime role: tenant constraints, state plus audit atomicity, rollback, pagination, expiry, and concurrency.
4. Envoy external-authorization gRPC boundary in its own process: valid allow, invalid credential, revoked credential, valid-but-denied authorization, and unavailable IAM. Gateway evidence cannot substitute this seam.

## Vertical TDD plan

1. Add one failing domain/service test for atomic Service Principal creation, then implement only that slice.
2. Add one failing real-PostgreSQL slice for single-Tenant profile/membership/bindings/audit constraints, then add the migration, queries, and adapter.
3. Repeat red to green for API Key create/one-time secret/hash, list pagination, revoke, expiry, disable cascade, stable errors, idempotency, failure rollback, and concurrent mutations.
4. Add red-to-green Bearer authentication and permission slices without storing a permission snapshot or accepting cookie/query/`X-API-Key` credentials.
5. Add the independent Envoy process E2E slices and preserve stable failure mapping without exposing the secret.
6. Run focused tests after every slice, then real PostgreSQL, two-Tenant, concurrency/failure, secret scan, complete IAM regression, race where applicable, ANI adapter gates, exact path audits, and generation/checksum drift checks.
7. Perform independent Standards and ticket-Spec reviews, repair all blockers, update evidence and status, create separate local commits, and verify both worktrees clean before DP2-11 can be claimed.

## Recovery and stop rules

- Before commit, recovery is limited to editing only DP2-10-owned files; no reset, stash, or overwrite of unrelated state.
- Test data uses isolated disposable PostgreSQL state and test credentials. No existing Human Principal or external environment is mutated.
- If implementation requires a subjectless key, cross-Tenant/Platform Service Principal, raw-secret persistence, permission snapshot, frozen-contract change, path expansion, Gateway-substituted Envoy proof, deploy/cutover, or credential invalidation outside isolated fixtures, stop and request an exact decision.
