# IAM gRPC Workload adapter

Module `github.com/zhangzhe-ctrl/ani-iam/sdk`. WR20 code delivery uses immutable Go pseudo-versions; it does not create a semantic-version tag or Release. Package `grpcworkload` depends on the public IAM API, gRPC/protobuf and standard libraries. It does not import IAM/ANI/Core internal packages.

A deployment supplies an explicit Workload certificate/key, CA, IAM server identity, environment/trust domain, policy revision and exact Grants. `NewClient` constructs its authenticated IAM connection. `Authorize` asks IAM for current source permission; any resource obligation requires an owner-provided `CheckResource` callback. Missing or unknown obligations fail closed. The returned opaque `Subject` stores its credential privately and redacts it when formatted.

The caller registers exact method/audience/operation and a `Describe` callback for its already normalized protobuf DTO. `DialCaller` provides mTLS and a unary interceptor; invoke with `WithSubject`. The interceptor gets a short WAT, mints a request-bound delegation, and calls the business RPC once. It never retries a mutation or sends the original subject bearer. Credential caching/background renewal are not required by this first implementation; each invocation gets fresh evidence under current state.

The receiver uses its own `NewClient` with a distinct Workload identity and exact IAM verification Grant. Construct gRPC with `ServerCredentials` and `ReceiverInterceptor`. Register the same owner method mapping. The interceptor checks the actual TLS peer (including certificate expiry on existing connections), hashes the actual normalized DTO and verifies online with IAM. Handlers use `VerifiedFromContext` for typed caller/subject/target, then still check real resources and own business idempotency. Unknown methods, ambiguous evidence, expired/revoked identity, policy mismatch or unavailable IAM never receive an offline/legacy bypass.

`Describe` must not mutate its request. The v1 digest rejects unknown protobuf fields and includes the deterministic complete DTO, protobuf full name and full RPC name under `ani.grpc.invocation.v1`. No command/terminal content is sent to IAM, only its digest and required authorization identifiers. Side-effect retries require a new authorized call with the owner's same idempotency key.

IAM subcalls default to two seconds and inherit shorter parent deadlines. The owner supplies the business RPC deadline. This adapter does not yet define ticket redemption or established-connection policy; those remain a separate accepted Session lifecycle integration requirement.

The standalone module and independent caller/receiver example have WR19/WR20 build, race, vet and formal-process evidence. Historical acceptance used controlled remote go.work files. Delivery consumers pin the API and SDK commits through go.mod and go.sum, without machine-specific replacements. Evidence for Gateway→Session integration and its limits remains in the corresponding IAM task records; source publication does not imply deployment or production readiness.


A receiver can retain a `Verified.Continuation()` for an admitted long-running operation. Use `MarshalPrivate()` only as input to authenticated owner storage bound to its session/request; restore with `RestoreContinuation()` and call the receiver client's `Recheck()`. It remains opaque to business handlers, cannot authorize another creation, is not renewable, and fails closed on current IAM denial or outage. Session uses this at ticket redemption and every 30 seconds (2-second timeout); owner code controls closure and business idempotency.


WR20 adds `NewWorkloadOnlyClient`, `NotificationTarget` and
`WorkloadOnlyCallerInterceptor` for exactly Notification SubmitNotification and
GetSubmissionStatus. This path has an opaque Workload caller only: no Human
Subject, Session, Tenant, delegation or policy revision is invented. Every call
gets current online identity/Grant validation, an exact audience/operation/RPC
binding and a two-second bound; receiver business capabilities remain local.
The caller removes ambient credentials and invokes the business RPC once.
`Check` combines current mTLS health ingress with a negative empty-credential
verification probe; only the exact expected denial after authorized ingress
establishes resolver readiness. Actual business calls always verify independently.
The existing delegated client still requires its policy revision and exact Human
invocation contract. Consume the exact SDK version pinned by the delivery; do not use the historical unpublished v0.1.0-rc.1 placeholder.
