# DP2-09 TDD and verification evidence

## RED to GREEN slices

- Tenant IAM Admin construction began with an undefined `NewTenantIAMAdminService`; the new service boundary and DTO/error mapping made the focused tests GREEN.
- Mutation replies initially omitted `PrincipalType` and complete `RoleIDs`; the transaction now re-reads the complete membership record before returning.
- A non-tenant registry operation initially reached credential verification and returned credential-invalid; it now fails closed as operation-unregistered before credential or storage dependencies are called.
- The real IAM to Gateway process test exposed stale IAM runtime fixture configuration in sequence: missing OIDC configuration, protobuf-invalid `15m`, then an obsolete policy revision. The fixture now starts a local OIDC discovery server, uses `900s`, and reads `data.TargetPolicyRevision`.
- Tenant Admin mutation initially accepted a trusted service actor. It now requires exactly one `x-ani-principal-type: human` value and rejects the non-Human test before any mutation.
- The Gateway workload middleware initially denied all ten DP2-09 Tenant Admin RPCs. The exact ten-method allowlist is now GREEN while `CreateTenantRole` remains denied.
- The composition-root test was RED because no Tenant Admin runtime constructor existed. The runtime now pins the accepted Permission Catalog revision and constructs the PostgreSQL reader, transaction UoW, use case, and service; a stale revision fails closed.
- Role-binding audit initially used the updated Membership version even though its target ID/type named the Role Binding. The transaction now returns the actual binding, and create/delete audits use binding version 1; unit and real PostgreSQL tests are GREEN.
- The post-change complete integration run found an old vertical-slice fake policy without a typed scope. The new evaluator correctly rejected it as unregistered; adding the frozen tenant scope to the test fixture made the focused real PostgreSQL/Redis test GREEN.
- ANI source-IP port/adapter tests were RED at compile time because the vendored descriptor-generated request had no `SourceIp` field. The approved descriptor was regenerated with the pinned toolchain; port→adapter→Proto and TCP peer→port tests are now GREEN without hand-editing generated code.
- The first source-IP-enabled real process rerun made the old test's immediate invalid→valid login sequence hit the intentional one-second account/IP backoff. The test now sends the unknown-account negative from an independent loopback address instead of sleeping or weakening product throttling; the rerun is GREEN.
- The first independent Standards/Spec review found that missing or stale lifecycle projections collapsed into a normal authorization denial. New real PostgreSQL tests first reproduced nil/deny behavior, then the reader added an explicit projection-freshness check and preserves the existing TENANT_LIFECYCLE_STALE unavailable mapping. Both missing and stale cases are GREEN.
- The first Spec review also found that tenant_role_permissions could persist free-form or cross-scope values. A real PostgreSQL test first proved the scope column was absent, then the generated migration added the relational Permission Catalog, tenant-only CHECK, and composite FK. Unknown tenant and catalogued platform permission writes now fail with SQLSTATE 23503 and 23514.
- The first Standards review rejected generated_operation_policies.go because no reproducible generator was present. The checked-in generator now verifies the accepted registry SHA and emits both Go and SQL outputs; two consecutive runs are byte-identical.

## Implemented and verified behavior

- The accepted 269-operation `check_permission` catalog is compiled into IAM. It contains 136 unique permissions: 81 tenant, 54 platform, and 1 own scope.
- Tenant authorization validates tenant access, active principal and membership, lifecycle projection freshness, role binding, permission, policy revision, operation registration, target tenant, and resource identifier requirements.
- Allowed resource decisions emit the typed `resource_tenant_match` obligation. Missing resource IDs and unsupported platform/own evaluation paths fail closed.
- Tenant Access, Membership, system Role/Binding reads and mutations are tenant-scoped. Mutations and security audit append share one PostgreSQL transaction.
- The last-active-human-tenant-admin guard takes a tenant advisory lock and counts only active Human principals with active membership and the system `tenant-admin` role.
- Missing Tenant Access maps to stable `TENANT_IAM_NOT_READY`; cross-tenant access maps to not found rather than disclosing the foreign object.
- The IAM composition root registers this implementation, and its Gateway mTLS workload capability is limited to the ten accepted Tenant Admin methods.

## Passed gates

- Focused `internal/biz`, `internal/data`, `internal/service`, `internal/server`, and `cmd/server` tests: `pass`.
- Restricted-role real PostgreSQL DP2-09 integration, including two-tenant negatives, concurrent last-admin protection, audit-failure rollback, and binding audit identity/version: `pass` (`ok .../tests/integration 18.425s`).
- Corrected target vertical-slice real PostgreSQL/Redis test: `pass` (`ok .../tests/integration 3.633s`).
- `sqlc` consecutive regeneration with identical before/after output hashes: `pass`.
- Atlas migration validation: `pass`.
- `go vet ./...`: `pass`.
- `go test ./... -count=1`: `pass`.
- `go test -race ./... -count=1`: `pass`.
- `git diff --check` in both worktrees: `pass`.
- ANI source-IP adapter, router, descriptor/fixture and immutable-pin tests: `pass`.
- Real IAM↔Gateway process E2E including source IP and Tenant Admin mTLS positive/negative: `pass` (`17.640s`).
- Complete review-repaired final integration regression: `pass` (`tests/integration 274.654s`).
- Complete review-repaired final integration race regression: `pass` (`tests/integration 243.728s`).
- ANI `make test`, registry `--check`, `make validate-doc-entrypoints`, and isolated complete-candidate `make validate-services`: `pass`.

- Review-repair focused unit/vet and real PostgreSQL lifecycle/catalog gates: pass.

The final complete normal and race runs include the contract synchronization, runtime composition, role-binding audit correction, source-IP seam, lifecycle fail-closed repair, relational Permission Catalog, and the real process mTLS checks. They supersede the earlier intermediate 138.288-second failure and the pre-review 302.561/284.553-second passes. The two final review-repaired runs above satisfy the required complete regressions.

Pinned generation tools and inputs:

```text
sqlc v1.31.1
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f  /tmp/ani-iam-dp2-08-tools/sqlc/sqlc
Atlas Community v1.3.0
10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b  /tmp/ani-iam-dp2-09-tools/atlas
989bc2d24ee46dcbec302fc7d24027969b84d8ee9d094786745690ce783b4b91  internal/data/queries/persistence.sql
c46c9bfd399d3f74731b522662a7a991da6a8510c749a6e17dbfb73ac1641633  migrations/202609080001_tenant_authorization.sql
814e31cd79eef9fb750a7a439bb09ab74f37ec3230dff63cf0b457ce7ef59dd4  migrations/202609080002_permission_catalog.sql
ec9b72409c392fc8db9a6d6d290a82eecaaa5187fce905495d74eb5b2e4615ab  internal/data/sqlcgen/persistence.sql.go
d9adb5b431b6845d02f0b5fbd1d78055dffde88716a820cac1768b1d6bc4e2f0  internal/data/sqlcgen/querier.go
```

## Final cross-process gate

The explicit process command is:

```text
DP2_ATLAS_BIN=/tmp/ani-iam-dp2-09-tools/atlas \
DP2_ANI_GATEWAY_DIR=/home/chabking/workspace/ANI-direct-p2-06-14 \
TESTCONTAINERS_RYUK_DISABLED=true \
go test -tags integration ./tests/integration \
  -run '^TestRealIAMGatewayProcessVerticalSlice$' -count=1 -v
```

Final result: `pass` in `17.640s`. The real run uses pinned PostgreSQL/Redis images, a restricted runtime role, a temporary local CA, separate IAM and Gateway processes, and the synchronized generated consumer. It proves Password Login→Session/Grant/Audit→Access Token→one CheckPermission, stable 401/403/503/504 behavior, `GetTenantAccess` through the allowed Gateway mTLS identity, and `CreateTenantRole` denied for the same identity. It does not prove cluster certificate lifecycle or final-client-IP preservation through an ingress/proxy.
