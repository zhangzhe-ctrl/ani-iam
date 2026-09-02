---
status: accepted
---

# Share infrastructure without sharing IAM project code

The rebuilt IAM system is an independent project with its own release lifecycle, internal ports, infrastructure adapters, outbox consumers, and tests. Its Git history starts clean with the scaffold, decisions, contract fixtures, and CP0 harness, and records the pinned ANI `main` commit used as its compatibility oracle rather than copying or filtering the monorepo history. It may use shared platform infrastructure such as NATS JetStream, PostgreSQL, Redis, and Dex, but it does not import ANI repository ports, adapters, or other internal Go packages. Core Control may continue using its own ANI-internal messaging implementation. The projects interact only through explicitly versioned external contracts and isolated infrastructure identities, preserving independent builds, upgrades, rollbacks, and framework evaluation.

ANI continues to own the public product REST OpenAPI and the only public Gateway entry. The independent IAM project owns its internal gRPC Protobuf contracts and publishes immutable descriptors that ANI pins by digest; IAM does not expose a second public REST API.

Every public state-changing ANI operation that reaches IAM requires an idempotency key. IAM persists a twenty-four-hour ledger scoped by API boundary, actor, operation, key, request hash, and serialized result. Repeating the same scoped key and request hash replays the stored result; repeating the key with a different request hash returns HTTP `409 Conflict`. This is a durable application contract rather than an in-memory or process-local cache.

Public callers receive ANI's stable REST `ErrorResponse`; internal callers receive stable gRPC status codes with structured error details such as `google.rpc.ErrorInfo`. The Gateway owns an explicit, tested mapping between the internal error reason and the public HTTP status and error code. Framework-native or database-native errors never cross either boundary directly.

Core Control and IAM share the NATS JetStream deployment but use dedicated cross-project streams, subject namespaces, credentials, and subject ACLs instead of inheriting the existing broad ANI streams. Platform infrastructure-as-code exclusively creates and changes streams, consumers, credentials, and ACLs; applications validate the expected configuration on startup and fail fast on drift rather than mutating infrastructure.

The Core identity may publish only the lifecycle, heartbeat, and bootstrap subjects; the IAM identity may consume them but cannot publish them; only the platform infrastructure identity may manage streams and consumers. Shared superuser credentials are not used.

Lifecycle events use a limits-based stream with thirty-day retention. Bootstrap commands use a work-queue stream and do not expire by age before IAM durably accepts them. Consumer-specific DLQ records retain their original message evidence for ninety days.

Single-replica, unauthenticated NATS is permitted only for CP0, development, and pre-production evidence. A production release remains blocked until the shared deployment has verified three-replica persistent storage, TLS, isolated credentials, subject ACLs, failover, and backup and restore evidence; missing live evidence is reported as `not_verified`.

For Core-produced integration messages, Core Control owns canonical Protobuf schemas and publishes immutable source or descriptor artifacts. IAM pins an exact version or digest and generates its own local types. A common envelope carries the event identity, schema major, producer, Tenant ID, aggregate version, occurrence time, correlation and causation identities, optional operation identity, and trace context. Within one major version, changes are additive and backward compatible; breaking changes use a new major subject and schema with an explicit dual-publish, dual-consume, and old-version retirement sequence. Neither project distributes its internal Go port or adapter as the integration contract.

The stable versioned subjects are `ani.integration.tenant.lifecycle.v1`, `ani.integration.tenant.lifecycle-heartbeat.v1`, and `ani.integration.tenant.iam-bootstrap.v1`. Core publishes an immutable contract artifact and digest; IAM pins that digest and runs producer-consumer fixtures. Deployment evidence records the Core commit, contract digest, and IAM image digest and never resolves `main` or `latest` dynamically.

Core Control has not yet been split and no real cross-project publisher, snapshot API, or Tenant stream exists in the current validation stage. The old auth/IAM path remains the sole write authority until an explicit future cutover; dual writes are forbidden. Production infrastructure work is tracked as separate future Production Readiness scope and does not enter CP0.

Kratos CP0 continues to vary only the framework while preserving the pinned old-Auth compatibility oracle. Only after CP0 acceptance may a separately authorized `IAM-LIFECYCLE-PROTOTYPE` use an `experimental/test-fixture` Protobuf and a test publisher to exercise the IAM-side decoder, idempotency, version-gap, DLQ, projection, and Bootstrap state machines. The fixture is non-authoritative: it proves neither a Core outbox nor snapshot API nor cross-project end-to-end integration, and all of those remain `not_verified`.

The NATS, outbox, snapshot, and projection integration described here is future P2 target architecture and is blocked on a separately started Core Control effort. Current executable work creates or modifies no Core runtime, real integration stream, outbox, or snapshot API. When Core later publishes its canonical producer contract, it may differ incompatibly from the experimental fixture; IAM must adapt, pin the Core artifact, and rerun its gates rather than requiring Core to preserve the fixture.

Lifecycle events use a stable reason-code enum and allowlisted non-sensitive metadata rather than free text or PII. Bootstrap commands contain only the Tenant and operation identities, normalized intended-administrator email, and necessary locale; they contain no password, token, credential, or complete Principal profile. Transport and persistent storage are encrypted and application logs redact the email and other identifiers.
