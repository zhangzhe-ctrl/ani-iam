# DP2-05 TDD RED evidence

Result: `fail`

## Target process composition root and runtime identity

After the human-approved four-file `cmd/server` path extension, the first
composition-root tests were added before changing production wiring.

```text
GOPROXY=off go test ./cmd/server \
  -run '^(TestBuildAppFailsClosedWhenSigningKeyIsUnavailable|TestRuntimeLoggerUsesKratosRedaction)$' \
  -count=1
```

Observed exit code: `1`.

```text
TestBuildAppFailsClosedWhenSigningKeyIsUnavailable:
buildApp() returned an app without its configured signing key

TestRuntimeLoggerUsesKratosRedaction:
runtime logger leaked filtered value "secret-postgresql-dsn"
service.name="ani-iam-cp0" service.version="cp0.0"
```

This RED proves the historical composition root ignored every target runtime
dependency, accepted an unavailable signing key, retained the CP0 process
identity, and did not redact the newly typed database/Redis/key configuration
fields. The GREEN implementation must explicitly construct the target graph,
fail closed before serving, and close its PostgreSQL/Redis clients through the
Kratos lifecycle.

## IAM gRPC mutual-TLS configuration

The production Gateway already required TLS 1.3, while the IAM server had no
TLS input or listener configuration. Tests first required an explicit server
certificate, private key and client-CA tuple in typed config.

```text
GOPROXY=off go test ./internal/conf ./cmd/server \
  -run '^(TestBootstrapValidate|TestBuildAppFailsClosedWhenSigningKeyIsUnavailable)$' \
  -count=1
```

Observed exit code: `1`.

```text
unknown field Tls in struct literal of type Server_GRPC
undefined: Server_GRPC_TLS
c.Server.Grpc.Tls undefined
```

This RED prevents a cleartext production listener from satisfying the target
Gateway gate. The minimal GREEN requires absolute mounted-file references and
client-certificate verification; no certificate or key bytes may enter the
committed YAML.

The transport behavior was then fixed before implementation:

```text
GOPROXY=off go test ./internal/server \
  -run '^TestLoadMutualTLSServerConfigRequiresVerifiedTLS13Clients$' -count=1
```

Observed exit code: `1`.

```text
internal/server/tls_test.go:26:17: undefined: LoadMutualTLSServerConfig
```

The public server-config seam must load exactly one server certificate, trust
the configured client CA, require a verified client certificate and reject TLS
below 1.3.

## Typed target runtime configuration

Command:

```text
GOCACHE=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-cache \
GOMODCACHE=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-mod \
GOTMPDIR=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-tmp \
go test ./internal/conf -run '^TestBootstrapValidate$' -count=1
```

Observed exit code: `1`.

```text
internal/conf/validate_test.go:19:3: unknown field Runtime in struct literal of type Bootstrap
internal/conf/validate_test.go:19:13: undefined: Runtime
internal/conf/validate_test.go:20:17: undefined: PostgreSQL
internal/conf/validate_test.go:21:12: undefined: Redis
internal/conf/validate_test.go:31:18: undefined: AccessToken
internal/conf/validate_test.go:52:60: c.Runtime undefined (type *Bootstrap has no field or method Runtime)
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/conf [build failed]
```

This RED fixes the typed composition inputs before the target runtime config
exists: an isolated PostgreSQL DSN, Redis namespace/limits/timeouts, an
Ed25519 private-key file reference, and the immutable policy revision. Secret
contents remain absent from committed YAML.

The first combined config/runtime regression run then failed with two precise
gate defects:

```text
TestCommittedConfigLoadsAsGeneratedType: invalid google.protobuf.Duration value "500ms"
TestBizLayerHasNoFrameworkOrAdapterImports: authentication_test.go imports forbidden dependency containing "redis"
```

The first was a Protobuf JSON-duration encoding mismatch and the second was a
false positive caused by scanning every quoted string instead of Go import
declarations. The fix uses `0.5s` and parses imports with `go/parser`; it does
not relax the biz dependency boundary.

## Ed25519 mounted-key loader

Command:

```text
go test ./internal/data -run '^TestLoadEd25519PrivateKeyFile' -count=1
```

Observed exit code: `1`.

```text
internal/data/access_token_key_file_test.go:22:17: undefined: LoadEd25519PrivateKeyFile
internal/data/access_token_key_file_test.go:55:17: undefined: LoadEd25519PrivateKeyFile
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
```

The GREEN implementation accepts exactly one PKCS#8 Ed25519 private-key PEM
block and rejects malformed input, another key algorithm, and trailing data.
The test creates only task-local temporary keys.

## Real Argon2id credential in the PostgreSQL vertical slice

The PostgreSQL vertical integration test first replaced its accepting password
fake with the real target Argon2id verifier while deliberately retaining the
old `$argon2id$fixture` placeholder.

```text
go test -tags=integration ./tests/integration \
  -run '^TestPostgresTargetLoginAndAuthorization$' -count=1 -v
```

Observed exit code: `1` after starting the fixed PostgreSQL container.

```text
vertical_slice_test.go:109: PasswordLogin() through runtime role:
password verifier: argon2id: hash is not in the correct format
--- FAIL: TestPostgresTargetLoginAndAuthorization
```

This is the required negative control: a placeholder hash cannot satisfy the
real credential gate. The GREEN fixture must hash its task-local test password
through the production Argon2id adapter before inserting the credential.

## Runtime clock adapter

Command:

```text
go test ./internal/data -run '^TestSystemClock' -count=1
```

Observed exit code: `1`.

```text
internal/data/system_clock_test.go:9:11: undefined: NewSystemClock
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
```

The GREEN adapter supplies UTC wall-clock time behind the existing biz Clock
port; domain code still does not import a framework or adapter.

## Frozen but intentionally unimplemented IAMAdminService

Command:

```text
go test ./internal/service -run '^TestIAMAdminService' -count=1
```

Observed exit code: `1`.

```text
internal/service/iam_admin_test.go:14:13: undefined: NewIAMAdminService
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service [build failed]
```

The GREEN service embeds the generated unimplemented target server so the
frozen three-service inventory can be registered without implementing any
DP2-06-or-later administration method.

## Password login domain slice

The first test fixes the minimum successful tenant password-login behavior before implementation. It requires normalized account lookup, active Principal/Membership/Tenant state, Session and Grant creation, a 15-minute access token, an opaque refresh secret, and one atomic login mutation with a security-audit event.

Command (the absolute cache paths are Goal-owned and are not committed):

```text
GOCACHE=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-cache \
GOMODCACHE=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-mod \
GOTMPDIR=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/go-tmp \
go test ./internal/biz
```

Observed exit code: `1`.

Observed source failure:

```text
internal/biz/authentication_test.go:23:11: undefined: PasswordLoginState
internal/biz/authentication_test.go:25:20: undefined: PrincipalStatusActive
internal/biz/authentication_test.go:28:20: undefined: TenantAccessStatusActive
internal/biz/authentication_test.go:29:20: undefined: TenantLifecycleStatusActive
internal/biz/authentication_test.go:34:13: undefined: NewAuthenticationUsecase
internal/biz/authentication_test.go:91:50: undefined: LoginMutation
internal/biz/authentication_test.go:100:51: undefined: AccessTokenClaims
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz [build failed]
```

This is the intended RED: the required domain types and use case do not yet exist. A prior `/usr/lib/golang`/quota environment failure is deliberately excluded because it did not exercise the source specification.

## Redis-backed login throttle failure

Command:

```text
go test ./internal/biz -run TestPasswordLoginFailsClosedWhenThrottleIsUnavailable -count=1
```

Observed exit code: `1`.

```text
--- FAIL: TestPasswordLoginFailsClosedWhenThrottleIsUnavailable (0.00s)
    authentication_test.go:99: PasswordLogin() error = login throttle: redis unavailable, want dependency classification and cause
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz
```

The use case propagated the adapter cause but did not yet classify it as the stable authentication dependency failure required for a `503` mapping.

## Authorization policy and one-read decision

Command:

```text
go test ./internal/biz -run TestCheckPermission -count=1
```

Observed exit code: `1`.

```text
internal/biz/authorization_test.go:20:49: undefined: AuthorizationState
internal/biz/authorization_test.go:31:13: undefined: NewAuthorizationUsecase
internal/biz/authorization_test.go:48:65: undefined: CheckPermissionCommand
internal/biz/authorization_test.go:57:45: undefined: AuthorizationReasonAllowed
internal/biz/authorization_test.go:71:13: undefined: NewAuthorizationUsecase
internal/biz/authorization_test.go:97:59: undefined: AuthorizationPolicy
internal/biz/authorization_test.go:116:9: undefined: AuthorizationState
internal/biz/authorization_test.go:119:9: undefined: AuthorizationLookup
internal/biz/authorization_test.go:122:101: undefined: AuthorizationLookup
internal/biz/authorization_test.go:122:123: undefined: AuthorizationState
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz [build failed]
```

This RED fixes fail-closed policy revision validation and exactly one
authorization-state lookup before any authorization implementation exists.

## PostgreSQL failure classification

Command:

```text
go test ./internal/biz -run TestPasswordLoginClassifiesPostgresFailureAsDependencyUnavailable -count=1
```

Observed exit code: `1`.

```text
--- FAIL: TestPasswordLoginClassifiesPostgresFailureAsDependencyUnavailable (0.00s)
    authentication_test.go:123: PasswordLogin() error = invalid credential, want dependency classification and cause
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz
```

The login use case incorrectly collapsed an unavailable PostgreSQL adapter into
an invalid credential, which would have produced `401` instead of the required
dependency `503`.

## Empty-database vertical-slice schema

Command:

```text
DP2_ATLAS_BIN=<pinned-atlas> go test -tags=integration ./tests/integration \
  -run TestTargetVerticalSliceSchema -count=1 -v
```

The fixed PostgreSQL image started successfully and both empty databases
accepted the DP2-04 Atlas baseline. The behavior test then failed as intended:

```text
vertical_slice_test.go:33: target vertical-slice table verified_emails does not exist
--- FAIL: TestTargetVerticalSliceSchema (10.49s)
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration
```

This RED proved the previous foundation had no hidden login/session schema. The
new versioned migration then made the same real-container test pass.

## PostgreSQL login and authorization adapters

Command:

```text
go test -tags=integration ./tests/integration \
  -run TestPostgresTargetLoginAndAuthorization -count=1
```

Observed exit code: `1`.

```text
tests/integration/vertical_slice_test.go:52:8: undefined: data.NewPostgresPasswordLoginReader
tests/integration/vertical_slice_test.go:55:8: undefined: data.NewPostgresLoginUnitOfWork
tests/integration/vertical_slice_test.go:93:8: undefined: data.NewPostgresAuthorizationReader
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
```

This RED fixed the public data-layer seams before adding sqlc-backed adapters.
After generation and implementation, the same test passed against the pinned
PostgreSQL image using the restricted runtime role.

## Real Gateway IAM timeout mapping

The parent process E2E first required a dedicated marker proving that the
production Gateway observed a real network timeout rather than a fake client
error:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  DP2_ATLAS_BIN=/home/chabking/.cache/ani-direct-p2-01-05/dp2-04/tmp-bin-quota-recovery/atlas \
  DP2_ANI_GATEWAY_DIR=/home/chabking/workspace/ANI-direct-p2-01-05 \
  go test -tags=integration ./tests/integration \
  -run '^TestRealIAMGatewayProcessVerticalSlice$' -count=1 -v
```

Observed exit code: `1` after the fixed PostgreSQL and Redis containers had
started and been removed.

```text
Gateway process did not emit real IAM-timeout marker
```

The same child process already proved the real `403` decision and an
unreachable IAM network endpoint mapped to `503 IAM_UNAVAILABLE`. This RED
therefore fixed the remaining behavior: a TCP peer that accepts the production
mTLS/gRPC connection but never completes its handshake or responds must hit the
client's fixed 500 ms deadline and map to `504 IAM_TIMEOUT`.

## AuthenticationService stable invalid-credential mapping

Command:

```text
go test ./internal/service \
  -run TestPasswordLoginMapsInvalidCredentialToStableErrorInfo -count=1
```

Observed exit code: `1`.

```text
authentication_test.go:90: gRPC code = Internal, want Unauthenticated
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service
```

The first thin service mapping leaked a generic internal failure instead of the
frozen `UNAUTHENTICATED` plus `google.rpc.ErrorInfo` contract. The minimal fix
added `CREDENTIAL_INVALID`, domain `iam.ani.internal`, and the required
`credential_kind` metadata.

## Target gRPC service registration

Command:

```text
go test ./internal/server \
  -run TestTargetGRPCServerRegistersOnlyThreeIAMServices -count=1
```

Observed exit code: `1`.

```text
internal/server/grpc_test.go:16:16: undefined: NewTargetGRPCServer
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/server [build failed]
```

The RED fixed the registration seam before it existed. The resulting server
registers exactly `AuthenticationService`, `AuthorizationService`, and
`IAMAdminService` under `iam.v1`; the test explicitly rejects legacy
`auth.v1.AuthService`.

## AuthorizationService DTO and policy-error mapping

The initial success-mapping test failed before the service existed:

```text
internal/service/authorization_test.go:31:13: undefined: NewAuthorizationService
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service [build failed]
```

After adding the thin DTO/domain mapping, the stable policy mismatch test fixed
a second RED:

```text
authorization_test.go:77: status = Internal details=[]interface {}{}
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service
```

The resulting error is `UNAVAILABLE` with reason `AUTHZ_POLICY_MISMATCH`, domain
`iam.ani.internal`, and both required revision metadata fields.

## IAM consumption of the DP2-02 operation registry

Command:

```text
go test ./internal/data -run TestTargetOperationRegistry -count=1
```

Observed exit code: `1`.

```text
internal/data/operation_registry_test.go:11:19: undefined: NewTargetOperationRegistry
internal/data/operation_registry_test.go:11:46: undefined: TargetPolicyRevision
internal/data/operation_registry_test.go:28:12: undefined: NewTargetOperationRegistry
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
```

The implemented adapter pins the DP2-02 revision and source artifact hashes,
contains only the fixed `listInstances -> instances/read` tracer-bullet entry,
rejects revision drift, and has no unknown-operation fallback.

## Gateway one-decision target authorization

The Gateway test was added only after generating the consumer stubs from the
immutable DP2-03 IAM descriptor. It fixes the `listInstances` tracer route at
one `CheckPermission` call, the public branding route at zero IAM calls,
client `x-ani-*` removal, trusted context injection, and the stable
401/403/503/504 failure matrix.

Command (from `repo/services/ani-gateway` in the dedicated ANI worktree):

```text
env GOTMPDIR=/tmp/ani-direct-p2-01-05-cache/dp2-05/go-tmp \
    TMPDIR=/tmp/ani-direct-p2-01-05-cache/dp2-05/tmp \
  go test ./internal/middleware -run '^TestTargetIAM' -count=1
```

Observed exit code: `1`.

```text
internal/middleware/target_iam_test.go:16:2: no required module provides package
github.com/kubercloud/ani/services/ani-gateway/internal/targetiam
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/middleware [setup failed]
```

This is the intended product RED: the frozen generated DTO package existed,
but the target IAM client/middleware seam did not. An earlier sandbox-only run
failed on a read-only default build cache and `/tmp` quota and is environmental
evidence, not a product RED.

## Gateway target gRPC deadline and retry boundary

Command (from `repo/services/ani-gateway`):

```text
go test ./internal/targetiam -run '^TestGRPCClient' -count=1
```

Observed exit code: `1`.

```text
internal/targetiam/client_test.go:107:9: undefined: NewGRPCClient
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/targetiam [build failed]
```

The public Gateway client seam had no implementation. The test requires both
frozen target RPCs to carry a 500 ms deadline and proves a timed-out
`CheckPermission` is attempted exactly once without retry.

## Gateway target Password Login HTTP contract

Command (from `repo/services/ani-gateway`):

```text
go test ./internal/router \
  -run '^TestTargetPasswordLoginReturnsAccessTokenAndRefreshCookie$' -count=1
```

Observed exit code: `1` after the generated-consumer gate was corrected.

```text
internal/router/target_password_login_test.go:79:35: too many arguments in call to registerAuth
        have (*route.RouterGroup, *targetLoginStub)
        want (*route.RouterGroup)
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/router [build failed]
```

This RED proves the existing public route had no target-IAM injection seam.
The test fixes the DP2-02 request shape and `Idempotency-Key`, requires the
access token/session/grant JSON response, rejects Refresh Token disclosure in
JSON, and requires the console-specific Secure HttpOnly SameSite=Lax cookie.

Before this product RED, two generation attempts failed the consumer compile
gate: plugin-level `exclude_types` removed Timestamp-typed fields, and applying
the managed Go prefix to imported `google.rpc.ErrorInfo` produced a local
duplicate import path. The final generator template keeps complete message
fields and disables the prefix override for the two Google import paths; no
generated file was hand-edited.

The next stable-error table produced the expected RED for typed reasons that
cannot be inferred from the gRPC code alone:

```text
TestTargetPasswordLoginPreservesStableIAMErrorReasons/idempotency_result_expired:
  code="IDEMPOTENCY_CONFLICT", want "IDEMPOTENCY_KEY_EXPIRED"
TestTargetPasswordLoginPreservesStableIAMErrorReasons/tenant_IAM_not_ready:
  code="IAM_UNAVAILABLE", want "TENANT_IAM_NOT_READY"
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/router
```

The minimal GREEN must read only `google.rpc.ErrorInfo` with domain
`iam.ani.internal`, accept only a DP2-02 registered stable reason, and never
return the IAM status message to the caller.

## Gateway production middleware composition

Command:

```text
go test ./internal/middleware \
  -run '^TestRegisterWithTargetIAMConnectsTheProductionMiddlewareChain$' -count=1
```

Observed product RED:

```text
internal/middleware/target_iam_test.go:107:12: undefined: RegisterWithTargetIAM
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/middleware [build failed]
```

The isolated middleware behavior existed, but the production chain had no
explicit target-IAM injection seam. The GREEN must preserve the old `Register`
entrypoint for non-target mode while making target mode explicit and preventing
fallback after the tracer route has been selected.

The production router injection test then failed at compile time as intended:

```text
internal/router/target_password_login_test.go:176:41:
  unknown field TargetIAMClient in struct literal of type RegisterOptions
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/router [build failed]
```

This fixes the composition boundary: the same explicitly selected target client
must own the public `passwordLogin` route; it may not be constructed inside the
handler or fall back after selection.

The final client-construction RED was:

```text
internal/targetiam/client_test.go:89:30: undefined: Dial
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/targetiam [build failed]
```

The production dialer must reject an empty address, disable gRPC retries, and
return one close function for the composition root.

## Maintained Argon2id password adapter

Command:

```text
go test ./internal/data -run '^TestArgon2idPasswordHasher' -count=1
```

Observed exit code: `1`.

```text
internal/data/password_argon2id_test.go:11:12: undefined: NewArgon2idPasswordHasher
internal/data/password_argon2id_test.go:50:12: undefined: NewArgon2idPasswordHasher
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
```

This product RED fixes the public adapter seam before it exists. It requires
the accepted Argon2id PHC algorithm/version and exact `m=65536,t=3,p=4`,
16-byte salt and 32-byte tag, successful and invalid-password verification,
no plaintext disclosure, and explicit malformed-PHC failure. An earlier run
that exhausted the task `/tmp` quota is environmental evidence and is not
counted as the RED.

The next parameter-conformance slice produced the required behavioral RED:

```text
--- FAIL: TestArgon2idPasswordHasherRejectsNonTargetParameters (0.03s)
    password_argon2id_test.go:74: Verify(non-target parameters) = true, <nil>, want false and error
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data
```

The upstream verifier accepted a valid but under-strength Argon2id hash. The
target adapter therefore still lacked the accepted exact-parameter gate.

## Pinned asymmetric JWX access-token adapter

Command:

```text
go test ./internal/data -run '^TestJWXAccessTokenCodecRoundTripsFrozenClaims$' -count=1
```

Observed exit code: `1` after resolving the pinned JWX module's required
transitive packages.

```text
internal/data/access_token_jwx_test.go:21:16: undefined: NewJWXAccessTokenCodec
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data [build failed]
```

This product RED fixes the token adapter seam before implementation: EdDSA
with protected `kid` and `typ=JWT`, the accepted standard and boundary claims,
and signature-backed round-trip verification at a fixed clock. The preceding
missing-`go.sum` attempt did not reach the source assertion and is not counted
as the RED.

The next issuance-policy slice produced this behavioral RED:

```text
--- FAIL: TestJWXAccessTokenCodecRejectsNonTargetAudience (0.00s)
    access_token_jwx_test.go:110: Issue(non-target audience) error = nil
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data
```

The signer could issue a token for an audience outside the frozen Console/BOSS
set, so the adapter did not yet fail closed at its issuance boundary.

A separately signed token showed the verifier-side version of the same gap:

```text
--- FAIL: TestJWXAccessTokenCodecVerifyRejectsNonTargetAudience (0.00s)
    access_token_jwx_test.go:131: Verify(non-target audience) error = nil
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/data
```

This proves issuance validation alone was insufficient; the verifier also had
to constrain an otherwise valid EdDSA token to the frozen audiences.

The issuance-claim invariant table then produced twelve independent failing
subcases: issuer, required Principal/Token/Session/Grant/Tenant identities,
positive Grant version, non-future issuance, non-expired/maximal lifetime, and
present/supported authentication methods all accepted invalid inputs. Each
subcase reported `Issue(invalid claims) error = nil`; the package exited `1`.

## Real Redis login throttle

Command:

```text
go test -tags=integration ./tests/integration \
  -run '^TestRedisLoginThrottleEnforcesLimitAndTTL$' -count=1
```

Observed exit code: `1`.

```text
tests/integration/redis_throttle_test.go:57:24: undefined: data.NewRedisLoginThrottle
tests/integration/redis_throttle_test.go:57:59: undefined: data.RedisLoginThrottleConfig
tests/integration/redis_throttle_test.go:70:73: undefined: biz.ErrAuthenticationRateLimited
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
```

This RED fixes the real-adapter seam before implementation. The GREEN must use
the pinned Redis image, atomically enforce the attempt window, store only a
hashed normalized-account suffix in the isolated namespace, retain a positive
bounded TTL, and return a stable domain rate-limit result.

The domain-classification slice then failed because the login use case joined
the expected rate-limit result with `ErrAuthenticationDependency`:

```text
--- FAIL: TestPasswordLoginPreservesRateLimitClassification (0.00s)
    authentication_test.go:123: PasswordLogin() error = login throttle: authentication dependency is unavailable
        authentication rate limit exceeded, want rate limit without dependency classification
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/biz
```

Without this split, the same Redis state would be ambiguous between stable
`429` throttling and a real Redis `503` outage.

The transport mapping then produced the expected RED:

```text
--- FAIL: TestPasswordLoginMapsRateLimitToStableErrorInfo (0.00s)
    authentication_test.go:115: rate-limit status = Internal details=[]interface {}{}
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service
```

The frozen contract requires `RESOURCE_EXHAUSTED` plus reason
`AUTH_RATE_LIMITED`; a generic internal error would not map to ANI's stable
public `429` response.

## Gateway Password Login OpenAPI input bounds

Command:

```text
go test ./internal/router \
  -run '^TestTargetPasswordLoginRejectsContractViolationsBeforeIAM$' -count=1
```

Observed exit code: `1`.

```text
--- FAIL: TestTargetPasswordLoginRejectsContractViolationsBeforeIAM
    --- FAIL: account_too_long: status = 200, want 400
    --- FAIL: password_too_long: status = 200, want 400
    --- FAIL: device_name_too_long: status = 200, want 400
    --- FAIL: tenant_is_not_UUID: status = 200, want 400
    --- FAIL: zero_tenant_UUID: status = 200, want 400
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/router
```

The target route accepted input that violated the frozen DP2-02 OpenAPI and
called IAM. The minimal GREEN must reject those requests before the internal
RPC while continuing to accept the contract's 128-character idempotency-key
maximum.

## Gateway target IAM mutual TLS client

Command (with the Goal-owned Go build cache):

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp GOPROXY=off \
  go test ./internal/targetiam \
  -run '^(TestDialRejectsBlankTargetAddress|TestDialUsesVerifiedMutualTLS13|TestDialRejectsIncompleteMutualTLSConfiguration)$' \
  -count=1
```

Observed exit code: `1`.

```text
internal/targetiam/client_test.go:99:42: undefined: MutualTLSConfig
internal/targetiam/client_test.go:99:42: too many arguments in call to Dial
        have (string, unknown type)
        want (string)
internal/targetiam/client_test.go:134:16: undefined: MutualTLSConfig
FAIL github.com/kubercloud/ani/services/ani-gateway/internal/targetiam [build failed]
```

The production Gateway client had no explicit trust roots, client workload
certificate, private key, or verified IAM server name. This RED fixes the
required mTLS 1.3 seam before the runtime can be counted as an authenticated
Gateway-to-IAM connection.

The Gateway runtime configuration slice then produced this RED:

```text
./main.go:33:55: not enough arguments in call to targetiam.Dial
./main_test.go:36:12: undefined: targetIAMRuntimeConfigFromEnv
./main_test.go:52:30: undefined: newTargetIAMClient
FAIL github.com/kubercloud/ani/services/ani-gateway [build failed]
```

The startup graph had neither named mTLS environment inputs nor a fail-closed
constructor. The GREEN must allow an intentionally disabled target address,
but must reject a configured address unless all explicit mTLS identity and
trust inputs are present.

## Real IAM and Gateway process E2E

After resolving the fixed module dependencies, the new integration entrypoint
produced this source RED:

```text
tests/integration/runtime_gateway_e2e_test.go:23:13: undefined: startIAMProcessForGatewayE2E
tests/integration/runtime_gateway_e2e_test.go:24:12: undefined: runGatewayProcessE2E
FAIL github.com/zhangzhe-ctrl/ani-iam/tests/integration [build failed]
```

The test already fixes the product outcome: an isolated migrated database and
Redis instance, a real IAM server process, a separate Gateway process, an
invalid and valid password login, one protected operation, and committed
Session/Grant/Audit rows. The missing process orchestration and Gateway child
probe are implemented only after this RED.

## Post-review audience/boundary fail-closed seam

Command:

```text
env GOCACHE=/tmp/ani-iam-dp2-05-go-cache \
  GOTMPDIR=/tmp/ani-iam-dp2-05-go-tmp \
  go test ./internal/service \
  -run TestPasswordLoginRejectsBossAudienceWithTenantBoundaryBeforeUsecase \
  -count=1
```

Observed exit code: `1`.

```text
--- FAIL: TestPasswordLoginRejectsBossAudienceWithTenantBoundaryBeforeUsecase
    authentication_test.go:172: status = OK details=[]interface {}(nil), want InvalidArgument and one ErrorInfo
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service
```

The frozen public contract pairs `audience=console` only with a Tenant
boundary and `audience=boss` only with a Platform boundary. The service was
accepting the invalid BOSS/Tenant pair and invoking the Tenant login use case.
The minimal GREEN must reject the mismatched pair before any use-case call.

The next vertical slice fixed the contract-valid BOSS/Platform request while
Platform Membership/Role persistence remains outside DP2-05:

```text
env GOCACHE=/tmp/ani-iam-dp2-05-go-cache \
  GOTMPDIR=/tmp/ani-iam-dp2-05-go-tmp \
  go test ./internal/service \
  -run TestPasswordLoginFailsClosedWhenBossPlatformSliceIsUnavailable \
  -count=1
```

Observed exit code: `1`.

```text
--- FAIL: TestPasswordLoginFailsClosedWhenBossPlatformSliceIsUnavailable
    authentication_test.go:188: status = InvalidArgument details=[ErrorInfo], want Unavailable and one ErrorInfo
FAIL github.com/zhangzhe-ctrl/ani-iam/internal/service
```

The request is valid under the frozen contract, so describing the unavailable
Platform slice as malformed is incorrect. The GREEN must fail closed with the
already-frozen `IAM_UNAVAILABLE` reason, identify
`platform_authentication` as the dependency, and not call the Tenant use case.
