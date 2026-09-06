# DP2-05 partial GREEN evidence

This file records only gates that have actually run. The overall ticket is not
yet complete and no Go/No-Go A conclusion is recorded here.

## Gateway target IAM consumer generation

Result: `pass`

The Gateway consumer is generated from the immutable DP2-03 IAM descriptor,
not from another worktree or a moving branch. The checked-in generator input is
`repo/services/ani-gateway/internal/targetiam/buf.gen.yaml` in the dedicated ANI
worktree. The command was run twice and produced identical hashes:

```text
PATH=/home/chabking/go/bin:/usr/bin:/bin \
  /tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf generate \
  --template buf.gen.yaml
```

Pinned tools and generator input:

```text
Buf binary SHA-256:                 8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af
protoc-gen-go SHA-256:              7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010
protoc-gen-go-grpc SHA-256:         aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08
buf.gen.yaml SHA-256:               fe73c87f1e4ff120a7c24f671be5c41df3739e1f5625361d6d3e1b3c6696c96c
DP2-03 IAM descriptor SHA-256:      df863beb3b095d1f01350c5334d80daf10cdf48083ce0e5663781171aa99a001
```

Generated outputs from both runs:

```text
124b3c8a9b045a2885b646bf88334d182e413e7fd7bfda4e9e05c4990601b150  authentication_service.pb.go
77bf6dc3630c0856643544414e5be6fde4dbe5e434534c8c10d534c8345ec67b  authentication_service_grpc.pb.go
bc25208ae513a4b7337ca48dc66d1013b1ca83983a08411416bdb444545a61ed  authorization_service.pb.go
78dae898853ded7f3e68c3abd82a5c8c087a28d66b0b8dd9a90ae06682a4a02a  authorization_service_grpc.pb.go
7f29aaa9429e50de11dd1f871050ad1dc1cc3f4e854ab7d3f09ce30e99f7bfec  contract.pb.go
81ec921e75d9c61541540d190b7b9387f5a3cfdcf4829f3280081e4bb459d929  iam_admin_service.pb.go
8ea5cdc1990db2de3faa654b0f4e5f8ab648caf2907b475d0bc37de746a66e4b  iam_admin_service_grpc.pb.go
```

No generated file was edited by hand.

## Gateway target behavior and regressions

Result: `pass`

The target seam covers the DP2-05 Console/Tenant form of public Password Login and the fixed
`GET /api/v1/instances` authorized operation. It uses one shared target IAM
client from the composition root, a 500 ms deadline, disabled gRPC retry, and
TLS 1.3 transport for a configured target. Tests prove public branding makes
zero decisions, the protected route makes exactly one `CheckPermission`, all
client `x-ani-*` headers are removed, only the trusted allowlist is injected,
and 401/403/503/504 mappings fail closed without invoking the legacy Auth
client.

Targeted command:

```text
go test ./internal/targetiam ./internal/middleware ./internal/router \
  -run '^(TestGRPCClient|TestDialRejectsBlankTargetAddress|TestTargetIAM|TestRegisterWithTargetIAM|TestTargetPasswordLogin|TestRegisterWithOptionsInjectsTargetPasswordLoginClient)' \
  -count=1
```

Observed result:

```text
ok github.com/kubercloud/ani/services/ani-gateway/internal/targetiam 0.512s
ok github.com/kubercloud/ani/services/ani-gateway/internal/middleware 0.042s
ok github.com/kubercloud/ani/services/ani-gateway/internal/router 0.022s
```

Complete related package regression:

```text
go test ./internal/authz ./internal/targetiam ./internal/middleware ./internal/router -count=1
```

Observed result: all four packages `pass`.

Complete Gateway module regression:

```text
go test ./... -count=1
```

Observed result: all Gateway packages `pass`; the generated package has no
tests.

Race scope:

```text
go test -race ./internal/targetiam ./internal/middleware ./internal/router -count=1
```

Observed result: all three packages `pass`.

Static check:

```text
go vet ./...
```

Observed result: `pass`.

The final staged-diff inspection found that the IAM service itself still
accepted `audience=boss` with a Tenant boundary and could route that request
through the Console/Tenant use case. Two test-first slices fixed this without
claiming the out-of-scope Platform implementation:

```text
env GOCACHE=/tmp/ani-iam-dp2-05-go-cache \
  GOTMPDIR=/tmp/ani-iam-dp2-05-go-tmp \
  go test ./internal/service \
  -run 'TestPasswordLoginRejectsBossAudienceWithTenantBoundaryBeforeUsecase|TestPasswordLoginFailsClosedWhenBossPlatformSliceIsUnavailable' \
  -count=1
```

Observed result: `pass`. The invalid BOSS/Tenant combination returns stable
`INVALID_ARGUMENT` before any use-case call. The contract-valid BOSS/Platform
request returns stable `UNAVAILABLE` with ErrorInfo reason `IAM_UNAVAILABLE`
and metadata `dependency=platform_authentication`, also before any Tenant
use-case call. BOSS/Platform persistence and caller E2E remain
`not_verified`; this result only proves fail-closed behavior.

Frozen registry gates:

```text
python3 tools/dp2_operation_registry_test.py
python3 tools/dp2_openapi_breaking_test.py
```

Observed result: operation registry 20/20 `pass`; OpenAPI breaking 4/4
`pass`.

The target Password Login adapter additionally enforces the frozen OpenAPI
account/password/device bounds and rejects malformed or all-zero Tenant UUIDs
before calling IAM:

```text
go test ./internal/router \
  -run '^TestTargetPasswordLoginRejectsContractViolationsBeforeIAM$' -count=1
```

Observed result: `pass`.

## Maintained target security and Redis adapters

Result: `pass`

The human password adapter pins `github.com/alexedwards/argon2id v1.0.0` and
accepts only the target PHC profile: Argon2id, 64 MiB, three iterations, four
lanes, sixteen-byte salt and thirty-two-byte tag. It rejects malformed and
valid-but-under-strength hashes. The Access Token adapter pins
`github.com/lestrrat-go/jwx/v3 v3.2.0`, signs only EdDSA with a protected `kid`,
and verifies signature, issuer, audience, time, UUIDv7 identities, Tenant
boundary, Grant version and authentication methods. The key-file adapter
accepts exactly one PKCS#8 Ed25519 PEM block.

```text
go test ./internal/data \
  -run '^(TestArgon2idPasswordHasher|TestJWXAccessTokenCodec|TestLoadEd25519PrivateKeyFile|TestSystemClock)' \
  -count=1
```

Observed result: `pass`.

The distributed login throttle pins `github.com/redis/go-redis/v9 v9.22.0`,
uses a single Lua operation for INCR/PEXPIRE, hashes the normalized account in
its namespaced key, preserves a bounded positive TTL, and has no process-local
fallback. The real-container gate uses:

```text
redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf
```

The gate verifies allow, rejection above the limit, TTL, reset, non-PII key
shape and a stopped-Redis dependency failure:

```text
go test -tags=integration ./tests/integration \
  -run '^TestRedisLoginThrottleEnforcesLimitAndTTL$' -count=1 -v
```

Observed result: `pass`.

The service mapping also preserves `AUTH_RATE_LIMITED` as gRPC
`RESOURCE_EXHAUSTED` with `google.rpc.ErrorInfo`, while Redis availability
errors map to the stable dependency result.

## Typed target runtime config and deterministic generation

Result: `pass`

The typed config now carries the isolated PostgreSQL DSN, Redis
namespace/limits/timeouts, mounted Ed25519 private-key file reference and
immutable policy revision. The committed YAML contains no password or private
key material. The historical CP0 runtime profile string was replaced by the
target-only `direct-p2-isolated` profile.

Fixed generator:

```text
Buf version:                         1.72.0
Buf binary SHA-256:                  8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af
plugin:                              buf.build/protocolbuffers/go:v1.36.11
conf.proto SHA-256:                  3aed7bfe590cc1bd80b03455eb36764c959677717ae59b961bdcc5291d1063bc
buf.yaml SHA-256:                    910d7fa6ac28f8d96e2c051a5626aba2af96401d7cf3f8bc0fc072ed4482daa2
buf.gen.yaml SHA-256:                06db8850f314b70bcfe808c411cde779f26f2c33c00a1a750fa8d6cf4c48aaed
conf.pb.go SHA-256, both runs:        0c4dd0fba42ce2b49f484bcaf1649fd5dc33415e7eb0a4c6076c92ff1557c0c9
configs/config.yaml SHA-256:          3ca61aa141307def96fdce2e8dbe4d358f1a18196cb87d4f82e3042410dd41d8
```

Commands:

```text
buf lint --config \
  .scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/buf.yaml \
  internal/conf/conf.proto
buf generate internal/conf/conf.proto --template \
  .scratch/ani-iam-rebuild/evidence/02-create-isolated-kratos-runtime/buf.gen.yaml
go test ./internal/conf ./tests/cp0 -count=1
```

Observed result: all checks `pass`; generation was rerun after adding the
explicit Gateway client DNS identity and the output hash did not change. No
generated file was edited by hand.

## IAM unit, real dependency, race and static regressions

Result: `pass`

Full non-integration regression:

```text
go test ./... -count=1
```

Observed result: every package `pass` or has no test files.

Race scope:

```text
go test -race ./internal/biz ./internal/data ./internal/service ./internal/server -count=1
```

Observed result: all four packages `pass`.

Static check:

```text
go vet ./...
```

Observed result: `pass`.

The complete real-dependency integration command was:

```text
DP2_ATLAS_BIN=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/tmp-bin-quota-recovery/atlas \
go test -tags=integration ./tests/integration -count=1 -v
```

Observed result: `pass` in 14.992s. It started and removed task-owned
containers using Docker server 29.7.2 and testcontainers-go v0.43.0. The fixed
PostgreSQL identity was:

```text
postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c
```

The real PostgreSQL tests replayed both migrations into empty isolated
databases, used the restricted runtime role for business queries, verified no
RLS/owner/superuser/BYPASSRLS access, committed mutation plus Audit together,
rolled both back on required Audit failure, injected commit and PostgreSQL
failures, exercised version conflict, rejected cross-Tenant relationships and
proved the deliberate missing-Tenant-predicate mutant leaks the negative
control. The target login/authorization test ran PostgreSQL and Redis together,
used the production Argon2id and JWX adapters, rejected an invalid password
without creating a Session, then created Session/Grant/Refresh state and
verified the signed Access Token. The same production token passed an allowed
database-backed permission decision and, after removing the test-owned
permission fixture, produced `PERMISSION_DENIED`; Tenant B could not consume
Tenant A state.

Fixed database generators were also rerun after the final query/migration
sources. Their versions, binary digests and stable output identities are:

```text
sqlc v1.31.1 binary SHA-256:
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f
Atlas Community v1.3.0 binary SHA-256:
10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b
query source SHA-256:
fd47185cacf4559871ee9c76f9e0203f5f96c97af125f66336978d81d26475ac
target migration SHA-256:
2ae71907884a815b992a6d84587dd20f9fdcb818e7a79bd105e81db582e82f98
atlas.sum SHA-256 before/after hash:
e6a6d1a9fc6c1097991356e575ddebc6e657e744596f27f51d7251cea0c7d79b
models.go SHA-256 before/after generate:
6071bd48fec826d9813e2559c31a28804d49be8783c982829c1ca54b5fb8cb4b
persistence.sql.go SHA-256 before/after generate:
3c6f59d8be2ffdda238cab292e7c334f49a7b5aa85e51c76ff792515ac191472
querier.go SHA-256 before/after generate:
02ccbbc6384f625331c656e74acfacaf45c18b904f497c776dc674de81ca5e8e
```

Commands:

```text
sqlc generate
atlas migrate validate --dir file://migrations
atlas migrate hash --dir file://migrations
```

Observed result: all `pass`; rerunning both generators changed no output hash.

The three approved dependency pins remained exact after `go mod tidy`; the
`go.mod` and `go.sum` hashes before and after the drift check were:

```text
55409f5bed95d59b510a00f7d726efc678e0589fbd8ad50d2f4f10b7057df863  go.mod
919abfc43bf38edef95dec5cdc655bc16d14733719b6c6ee6aa3f1410e5c6414  go.sum
```

## IAM composition root and mutual TLS transport

The approved `cmd/server` path extension now supplies the explicit target
composition graph. The focused IAM command was:

```text
go test ./internal/conf ./internal/server ./cmd/server -count=1
```

Observed result: `pass`. Configuration requires mounted absolute paths for the
server certificate, private key, and Gateway client CA. The gRPC server loads
those files, enforces TLS 1.3 and `RequireAndVerifyClientCert`, and registers
exactly the target Authentication, Authorization, and IAM Admin services.

The Gateway-side production client was independently exercised through a real
TCP gRPC handshake:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  go test ./internal/targetiam \
  -run '^(TestDialRejectsBlankTargetAddress|TestDialUsesVerifiedMutualTLS13|TestDialRejectsIncompleteMutualTLSConfiguration)$' \
  -count=1
```

Observed result: `pass`. The client uses only its configured CA, supplies the
Gateway workload certificate, verifies the configured IAM server name,
requires TLS 1.3, and retains gRPC retry disablement.

The Gateway composition root now reads an explicit target mode, target address,
and all mTLS inputs through one typed runtime configuration. `disabled` is the
only opt-out; missing or unknown mode and `dp2_05` without an address fail
startup, so target mode cannot silently fall through to the legacy chain.
Focused command:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  go test . \
  -run '^(TestTargetIAMRuntimeConfigFromEnv|TestNewTargetIAMClientAllowsDisabledAddress|TestNewTargetIAMClientRejectsImplicitOrIncompleteTargetMode|TestNewTargetIAMClientFailsClosedWithoutMutualTLS)$' \
  -count=1
```

Observed result: `pass`. Explicit `IAM_TARGET_MODE=disabled` leaves the tracer
client disabled. `IAM_TARGET_MODE=dp2_05` requires a target address and complete
server name, CA, Gateway certificate, and Gateway private key; every incomplete
target configuration fails startup.

## Console/BOSS caller boundary

Result: `not_verified`

The Go/No-Go A tracer proves only Console Password Login with a non-empty Tenant
boundary and the Console `listInstances` operation. The frozen public and gRPC
contracts also describe BOSS Password Login with a Platform boundary, but
DP2-05 does not build Platform Membership/Role persistence and no BOSS caller or
Platform-boundary E2E was run. BOSS/Platform Password Login is therefore
`not_verified`; no Console result is used as its substitute.

## Final review and staged-path gates

Result: `pass`

The independent Standards and Spec reviews are complete. They reported four
Standards findings (worst severity High) and four Spec findings (worst
severity P1). Every implementation blocker was repaired and rerun; the
separate review evidence is `review.md` and its result is `pass`. The final
staged-path audit also passed: the IAM index contains 60 explicit paths and the
ANI index contains 24 explicit paths, all within the ticket and human-approved
extensions. Go/No-Go A human acceptance remains a separate `not_verified`
checkpoint and is not inferred from these engineering gates.

## Real IAM and Gateway process E2E

Command:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  DP2_ATLAS_BIN=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/tmp-bin-quota-recovery/atlas \
  DP2_ANI_GATEWAY_DIR=/home/chabking/workspace/ANI-direct-p2-01-05 \
  go test -tags=integration ./tests/integration \
  -run '^TestRealIAMGatewayProcessVerticalSlice$' -count=1 -v
```

Observed result: `pass` in 10.39s. Docker server 29.7.2 and
testcontainers-go v0.43.0 started and removed the fixed PostgreSQL and Redis
containers. The test built and started the real `cmd/server` IAM binary with
ephemeral mounted Ed25519 and X.509 material, waited for `/readyz`, then ran a
separate Gateway test process hosting the production target IAM client,
middleware, registry, router, and a real Hertz HTTP listener.

The process assertions proved:

- invalid Password Login returned stable `401 CREDENTIAL_INVALID`;
- valid Password Login created one Session, one Grant and one Security Audit
  row through the restricted runtime role and returned an EdDSA Access Token;
- the Public branding operation made zero IAM authorization decisions;
- `GET /api/v1/instances` made exactly one real `CheckPermission` call and
  returned `200`;
- hostile client `x-ani-*` headers were removed before trusted Tenant and
  Principal context was installed downstream;
- the Gateway used the same real Redis container for its production cache
  store while IAM used it for the production login throttle;
- all generated private material stayed inside the test temporary directory;
- both dependency containers were terminated after the result.

Fixed dependency identities:

```text
postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c
redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf
```

## Real Gateway 403, 503 and 504 process seams

Result: `pass`

The process E2E was extended vertically without replacing the production
client, middleware, registry or router. A second real Principal authenticates
successfully but has no permission for `listInstances`, so its real IAM
decision maps to `403 PERMISSION_DENIED`. A closed loopback endpoint maps the
production client's connection failure to `503 IAM_UNAVAILABLE`. A loopback
TCP peer accepts the connection but deliberately never completes the TLS/gRPC
handshake, so the same client's fixed 500 ms deadline maps to
`504 IAM_TIMEOUT`.

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  DP2_ATLAS_BIN=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/tmp-bin-quota-recovery/atlas \
  DP2_ANI_GATEWAY_DIR=/home/chabking/workspace/ANI-direct-p2-01-05 \
  go test -tags=integration ./tests/integration \
  -run '^TestRealIAMGatewayProcessVerticalSlice$' -count=1 -v
```

Observed result: `pass` in 17.974s on 2026-09-06. Docker server 29.7.2
and testcontainers-go v0.43.0 created and removed the fixed PostgreSQL and
Redis containers. The child emitted all three independent markers:

```text
DP2_GATEWAY_PROCESS_E2E_UNAVAILABLE_PASS
DP2_GATEWAY_PROCESS_E2E_TIMEOUT_PASS
DP2_GATEWAY_PROCESS_E2E_PASS
```

## Final complete regressions before review

Result: `pass`

The complete IAM non-integration suite and static check were rerun after the
last ErrorInfo, workload-identity and process-E2E changes:

```text
go test ./... -count=1
go vet ./...
```

Observed result: every IAM package `pass` or has no test files; vet `pass`.

The complete Gateway module was rerun at the same final source state:

```text
go test ./... -count=1
go vet ./...
```

Observed result: every Gateway package `pass` or has no test files; vet
`pass`.

The first attempt to run the two race suites concurrently returned `fail`
with `disk quota exceeded` while compiling into two task caches. No test
assertion ran or failed. After clearing only the Goal-owned Go build caches,
the same source was tested serially with one build worker:

```text
go test -race -p=1 ./internal/biz ./internal/data ./internal/service \
  ./internal/server ./cmd/server ./tests/cp0 -count=1

go test -race -p=1 . ./internal/authz ./internal/targetiam \
  ./internal/middleware ./internal/router -count=1
```

Observed result: both serial race scopes `pass`. The environment-quota attempt
remains recorded as `fail`; it is not used as source evidence.

The final full real-dependency integration run used the fixed images, the
restricted runtime role, the production adapters and the dedicated Gateway
worktree:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  DP2_ATLAS_BIN=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/tmp-bin-quota-recovery/atlas \
  DP2_ANI_GATEWAY_DIR=/home/chabking/workspace/ANI-direct-p2-01-05 \
  go test -tags=integration ./tests/integration -count=1 -v
```

Observed result: `pass` in 108.458s. It covered the complete no-RLS foundation,
real Redis throttle including stopped-Redis failure, empty-schema replay,
PostgreSQL failure mapping, login/authorization/Audit/two-Tenant behavior and
the real IAM/Gateway process E2E including 401/403/503/504. Every task-owned
PostgreSQL and Redis container was terminated.

The two frozen registry gates and both module-integrity checks were also rerun:

```text
python3 tools/dp2_operation_registry_test.py
python3 tools/dp2_openapi_breaking_test.py
go mod verify
```

Observed result: registry 20/20 `pass`; breaking 4/4 `pass`; `go mod verify`
reported `all modules verified` in both the IAM and Gateway modules.

After the final audience/boundary fail-closed repair, the same staged source was
rerun serially with the Goal-owned caches to avoid the already-recorded disk
quota failure:

```text
go test -p=1 ./... -count=1
go vet ./...
go test -race -p=1 ./internal/biz ./internal/data ./internal/service \
  ./internal/server ./cmd/server ./tests/cp0 -count=1

go test -p=1 ./... -count=1
go vet ./...
go test -race -p=1 . ./internal/authz ./internal/targetiam \
  ./internal/middleware ./internal/router -count=1

go test -p=1 -tags=integration ./tests/integration -count=1 -v
```

Observed result: all IAM and Gateway unit, vet and serial race commands
`pass`; the complete real PostgreSQL/Redis/Gateway integration suite `pass` in
61.187s. The run included every no-RLS persistence subtest, Redis stopped-state
failure, PostgreSQL failure mapping, Tenant negative cases and the process E2E
401/403/503/504 markers. Every task-owned container was terminated.

## ANI repository aggregate gates

The current ANI repository's architecture and documentation-entrypoint gates
were run after the four approved feature-batch documents were added:

```text
make validate-architecture \
  GO_CACHE_ENV='GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp'
make validate-doc-entrypoints \
  GO_CACHE_ENV='GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp'
```

Observed result: both `pass`; component import, retired inference control plane,
document boundaries and five document-entrypoint tests all passed.

At the final staged source state, `make test-go` and `make test-python` were
rerun after these two gates. Both returned exit code `0`; the configured Go
package set and Python compileall were `pass`.

The aggregate `make test` result remains `fail` at its first legacy Auth gate:

```text
ERROR: /auth/logout missing operationId logout
ERROR: /auth/api-keys/{key_id} missing operationId revokeAPIKey
make: *** [Makefile:1202: validate-auth-contract] Error 1
```

These assertions require the old `logout` and `revokeAPIKey` identifiers. The
human-accepted DP2-02 target identifiers are `logoutSession` and
`revokeIAMAPIKey`; DP2-02 and DP2-03 already recorded this legacy aggregate
gate as `fail`. DP2-05 does not revert the frozen target contract.

The legacy Gateway authz generator gate separately remains `fail` because it
requires the old extension fields `action`, `boundary` and `principal_kinds`,
while the accepted target registry uses `resource`, `actions`, `scope` and the
separate target authn classification. Its exact error was:

```text
ValueError: x-ani-authz missing fields: ['action', 'boundary', 'principal_kinds']
Ran 18 tests in 0.699s
FAILED (errors=1)
```

The replacement DP2 target registry and breaking gates remain 20/20 and 4/4
`pass` respectively. To complete the portions skipped when aggregate `make
test` stopped, these were run explicitly:

```text
make test-go \
  GO_CACHE_ENV='GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp'
make test-python
```

The sandboxed Go run first returned `fail` because several existing httptest
packages could not bind IPv6 loopback (`socket: operation not permitted`). The
identical command was rerun with local loopback permission and returned exit
code `0`; the complete configured Go package set `pass`. Python compileall also
returned exit code `0`. The sandbox failure is retained as environment
evidence and is not treated as a source failure or a passing run.

The final fixed CycloneDX command was rerun offline and reproduced
`3b764a875600d0878b5467ea2ff52fdf74e7e3e6d7b2f5ebaee20873992f2ed4`.
The final pinned `govulncheck v1.7.0` scan covered 31 root packages and 84
modules, returned exit code `0`, and reported zero affected vulnerabilities.
The unused `golang.org/x/crypto/openpgp` module advisory remains explicitly
recorded in `supply-chain.md`.

## DP2-05 idempotency boundary

Result: `not_verified`

The frozen public Password Login operation requires `Idempotency-Key`, and the
Gateway validates and forwards that key; this input-contract behavior is
`pass`. The durable 24-hour unified Idempotency Ledger, same-request replay and
different-request conflict behavior are intentionally not implemented or
claimed by this tracer bullet. The accepted Direct P2 ticket graph assigns the
unified ledger for all public mutations to DP2-16, after Go/No-Go B. Implementing
it in DP2-05 would exceed the ticket's minimal vertical-slice scope and begin a
post-DP2-06 capability. This remains `not_verified`, not `pass`.
