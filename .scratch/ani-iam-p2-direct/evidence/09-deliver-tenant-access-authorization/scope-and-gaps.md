# DP2-09 final scope audit and residual gaps

## Verified within the current authorization

- `pass`: IAM domain, service, PostgreSQL, migration, generated sqlc, test, issue, and evidence changes are within the claimed issue's listed IAM paths.
- `pass`: ANI registry changes are limited to the two human-approved generated outputs and the related static assertion test under the already-allowed target authorization path.
- `pass`: no push, PR, deploy, cutover, credential invalidation, data rebuild, or legacy deletion has occurred.
- `not_verified`: deployment and production behavior remain out of scope.

## Gate A: source-IP contract and ANI consumer — `pass`

The user approved the three ANI product-code paths, the five indivisible generated-contract paths, and the handler-side target path on 2026-09-08. The delivered product seam is:

- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/ports/target_iam.go`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/adapters/iam/client.go`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/adapters/iam/client_test.go`

- Gateway extracts only a valid TCP `RemoteAddr`, canonicalizes IPv4/IPv6 with `Unmap`, and rejects nil, non-TCP, malformed, or zoned addresses.
- Client-supplied `X-Forwarded-For`, `X-Real-IP`, JSON fields, and arbitrary metadata are never authoritative.
- The port and gRPC adapter carry the trusted value into additive `PasswordLoginRequest.source_ip = 7`.
- The synchronized descriptor is `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a`; the fixture is `cb4df637172faae8551d42abd55b9167195d9a21693080a06e713ae9c6483cc4`.

The generator-first contract sync changed only the approved subset:

- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/generated/pb/iam/v1/authentication_service.pb.go`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/generated/pb/iam/v1/authentication_service_grpc.pb.go`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/pkg/generated/pb/iam/v1/iam_descriptor.pb`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/api/proto/tenant/integration/v1/contract_pins.json`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/api/proto/tenant/integration/v1/testdata/iam_password_login.v1.json`

The descriptor was fed to the existing ANI `buf.gen.yaml` in an isolated temporary tree using exact Buf `1.72.0`, `protoc-gen-go v1.36.12`, and `protoc-gen-go-grpc 1.6.2`. The two approved Authentication outputs were copied back; every other generated `iam.v1` output compared byte-identical. The gRPC output itself remained unchanged. ANI's strict consumer pin schema retained only its existing fields and synchronized the IAM descriptor and PasswordLogin fixture hashes; importing IAM's later private Notification block correctly failed the ANI decoder and was not retained. Immutable contract tests and a second generation are `pass`.

## Gate B: IAM Admin implementation and workload capability — `pass`

The user approved the exact runtime paths on 2026-09-08:

- `/home/chabking/workspace/ani-iam/cmd/server/app.go`
- `/home/chabking/workspace/ani-iam/cmd/server/app_test.go`
- `/home/chabking/workspace/ani-iam/internal/server/workload_identity.go`
- `/home/chabking/workspace/ani-iam/internal/server/workload_identity_test.go`

Only these already-frozen DP2-09 RPCs were added to the Gateway workload capability list: `GetTenantAccess`, `UpdateTenantAccess`, `GetTenantMembership`, `ListTenantMemberships`, `UpdateTenantMembership`, `RemoveTenantMembership`, `GetTenantRole`, `ListTenantRoles`, `BindTenantRole`, and `UnbindTenantRole`. `CreateTenantRole` remains a negative test. This is a capability allowlist over the already-verified Gateway mTLS identity, not certificate issuance or cluster deployment.

The allowlist RED returned `PermissionDenied` for all ten intended methods; the exact set then passed while the non-approved method remained denied. The composition-root RED had no `newTenantIAMAdminRuntime`; it now pins the Permission Catalog revision and constructs the PostgreSQL reader, transaction UoW, use case, and service. Focused unit tests are `pass`. The real IAM process with a temporary CA proves the same Gateway certificate can call `GetTenantAccess`, while `CreateTenantRole` returns `PermissionDenied`.

## ANI feature-batch documentation gate — `pass`

The user approved and the ticket updated exactly these four documentation paths:

- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/development-records/DP2-09-tenant-access-authorization.md`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/development-records/README.md`
- `/home/chabking/workspace/ANI-direct-p2-06-14/repo/CURRENT-SPRINT.md`
- `/home/chabking/workspace/ANI-direct-p2-06-14/ANI-06-开发计划.md`

They record only the local DP2-09 integration result, exact hashes/gates, and `not_verified` deployment/final-client-IP boundary. `make validate-doc-entrypoints` is `pass`. The complete candidate was overlaid on an isolated checkout and `make validate-services` passed without adding any tracked drift beyond the approved candidate paths. They do not authorize publication or a broader ANI change.

## Review remediation — lifecycle and relational catalog

The first independent Standards and Spec reviews were both fail. The shared lifecycle finding was valid: a missing join row or false freshness bit could become a normal denial. The PostgreSQL reader now checks the Tenant lifecycle projection independently before the authorization join and rechecks the returned freshness bit. Missing and stale projections return the existing Tenant lifecycle stale dependency error, which the service maps to 503 TENANT_LIFECYCLE_STALE; inactive but fresh lifecycle remains a 403 denial.

The Spec review also correctly found that application-only Permission validation did not constrain stored Role Permissions. The accepted operation registry now drives a migration-owned relational permission_catalog with 136 unique rows. tenant_role_permissions has an explicit scope column constrained to tenant and a composite FK to that catalog. Real PostgreSQL tests prove uncatalogued and platform-scope values fail with SQLSTATE 23503 and 23514.

The Standards review correctly found that the Go policy map lacked a reproducible generator. internal/data/cmd/genoperationregistry now validates the accepted registry SHA and generates both the Go policy map and the SQL catalog migration. Two consecutive generations and sqlc regeneration are byte-identical. The remaining 12-argument membershipRecord data-clump observation is a non-blocking judgement call; it is confined to the data adapter and changing it is not necessary to correct a semantic or standards violation.

## Typed obligation consumption boundary

IAM generation and DTO mapping of `resource_tenant_match` are `pass`. Actual owner-handler enforcement is intentionally `not_verified`: the accepted architecture requires the Core/Services owner to load the authoritative resource tenant and satisfy the obligation; Gateway URL parameters are not authoritative. That caller/owner integration belongs to DP2-13 and must not be fabricated inside DP2-09.

The local Gateway-direct-client topology proves the immediate TCP peer only. In a cluster, Ingress/proxy preservation and authenticated propagation of the final client address remain `not_verified`; using the proxy peer as if it were the final client would be incorrect. Cluster certificate issuance/mount/rotation, owner-handler obligation consumption, Console/BOSS callers, deployment, and cutover remain explicit follow-up boundaries.
