# IAM gRPC Workload adapter

Module `github.com/zhangzhe-ctrl/ani-iam/sdk`, candidate `v0.1.0-rc.1` (unpublished). Package `grpcworkload` depends on the public IAM API, gRPC/protobuf and standard libraries. It does not import IAM/ANI/Core internal packages.

A deployment supplies an explicit Workload certificate/key, CA, IAM server identity, environment/trust domain, policy revision and exact Grants. `NewClient` constructs its authenticated IAM connection. `Authorize` asks IAM for current source permission; any resource obligation requires an owner-provided `CheckResource` callback. Missing or unknown obligations fail closed. The returned opaque `Subject` stores its credential privately and redacts it when formatted.

The caller registers exact method/audience/operation and a `Describe` callback for its already normalized protobuf DTO. `DialCaller` provides mTLS and a unary interceptor; invoke with `WithSubject`. The interceptor gets a short WAT, mints a request-bound delegation, and calls the business RPC once. It never retries a mutation or sends the original subject bearer. Credential caching/background renewal are not required by this first implementation; each invocation gets fresh evidence under current state.

The receiver uses its own `NewClient` with a distinct Workload identity and exact IAM verification Grant. Construct gRPC with `ServerCredentials` and `ReceiverInterceptor`. Register the same owner method mapping. The interceptor checks the actual TLS peer (including certificate expiry on existing connections), hashes the actual normalized DTO and verifies online with IAM. Handlers use `VerifiedFromContext` for typed caller/subject/target, then still check real resources and own business idempotency. Unknown methods, ambiguous evidence, expired/revoked identity, policy mismatch or unavailable IAM never receive an offline/legacy bypass.

`Describe` must not mutate its request. The v1 digest rejects unknown protobuf fields and includes the deterministic complete DTO, protobuf full name and full RPC name under `ani.grpc.invocation.v1`. No command/terminal content is sent to IAM, only its digest and required authorization identifiers. Side-effect retries require a new authorized call with the owner's same idempotency key.

IAM subcalls default to two seconds and inherit shorter parent deadlines. The owner supplies the business RPC deadline. This adapter does not yet define ticket redemption or established-connection policy; those remain a separate accepted Session lifecycle integration requirement.

The standalone module passed build/race/vet and the independent caller/receiver example ran against formally bootstrapped IAM. Formal ANI Gateway→Session Gateway resource integration is still incomplete; this candidate is not a published SDK or evidence of full WR19 completion. Controlled remote go.work files bind exact source snapshots for the unpublished API/SDK candidates. Delivery go.mod files contain no machine-specific replacement.


A receiver can retain a `Verified.Continuation()` for an admitted long-running operation. Use `MarshalPrivate()` only as input to authenticated owner storage bound to its session/request; restore with `RestoreContinuation()` and call the receiver client's `Recheck()`. It remains opaque to business handlers, cannot authorize another creation, is not renewable, and fails closed on current IAM denial or outage. Session uses this at ticket redemption and every 30 seconds (2-second timeout); owner code controls closure and business idempotency.
