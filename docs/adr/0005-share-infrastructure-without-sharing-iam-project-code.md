---
status: accepted
---

# Share infrastructure without sharing IAM project code

The rebuilt IAM system is an independent project with its own release lifecycle, internal ports, infrastructure adapters, outbox consumers, and tests. Its Git history starts clean with the scaffold, decisions, contract fixtures, and CP0 harness, and records the pinned ANI `main` commit used as its compatibility oracle rather than copying or filtering the monorepo history. It may use shared platform infrastructure such as NATS JetStream, PostgreSQL, Redis, and Dex, but it does not import ANI repository ports, adapters, or other internal Go packages. Core Control may continue using its own ANI-internal messaging implementation. The projects interact only through explicitly versioned external contracts and isolated infrastructure identities, preserving independent builds, upgrades, rollbacks, and framework evaluation.

ANI continues to own the public product REST OpenAPI and the only public Gateway entry. The independent IAM project owns its internal gRPC Protobuf contracts and publishes immutable descriptors that ANI pins by digest; IAM does not expose a second public REST API.

This ADR does not freeze the detailed Human/Workload endpoint retry catalog. [D04](../../.scratch/ani-iam-workload-refoundation/decisions.md) requires the contract item to map reads, credential issuance and state-changing operations to their actual owner semantics, preserve accepted replay/security rules, and explicitly accept any intended change. The former detailed candidate catalog is historical design input, not a blanket implementation prerequisite.

Public callers receive ANI's stable REST `ErrorResponse`; internal callers receive stable gRPC status codes with structured error details such as `google.rpc.ErrorInfo`. The Gateway owns an explicit, tested mapping between the internal error reason and the public HTTP status and error code. Framework-native or database-native errors never cross either boundary directly.

Core Control and IAM share the NATS JetStream deployment but use dedicated cross-project streams, subject namespaces, credentials, and subject ACLs instead of inheriting the existing broad ANI streams. Platform infrastructure-as-code exclusively creates and changes streams, consumers, credentials, and ACLs; applications validate the expected configuration on startup and fail fast on drift rather than mutating infrastructure.

The Core identity may publish only the lifecycle, heartbeat, and bootstrap subjects; the IAM identity may consume them but cannot publish them; only the platform infrastructure identity may manage streams and consumers. Shared superuser credentials are not used.

Lifecycle events use a limits-based stream with thirty-day retention. Bootstrap commands use a work-queue stream and do not expire by age before IAM durably accepts them. Consumer-specific DLQ records retain their original message evidence for ninety days.

The historical CP0 allowance for unauthenticated NATS fixtures is not valid target-integration evidence. Target integration requires isolated authenticated identities, exact publish/consume permissions and configuration drift checks as accepted above. The additional broker-to-Workload evidence mechanism remains pending under D02; production replication, failover and backup evidence remain separate production-readiness work.

For Core-produced integration messages, Core Control owns canonical Protobuf schemas and publishes immutable source or descriptor artifacts. IAM pins an exact version or digest and generates its own local types. A common envelope carries the event identity, schema major, producer, Tenant ID, aggregate version, occurrence time, correlation and causation identities, optional operation identity, and trace context. Within one major version, changes are additive and backward compatible; breaking changes use a new major subject and schema with an explicit dual-publish, dual-consume, and old-version retirement sequence. Neither project distributes its internal Go port or adapter as the integration contract.

The stable versioned subjects are `ani.integration.tenant.lifecycle.v1`, `ani.integration.tenant.lifecycle-heartbeat.v1`, and `ani.integration.tenant.iam-bootstrap.v1`. Core publishes an immutable contract artifact and digest; IAM pins that digest and runs producer-consumer fixtures. Deployment evidence records the Core commit, contract digest, and IAM image digest and never resolves `main` or `latest` dynamically.

Core producer/Snapshot integration must be implemented and tested through its real owner in a separately scoped ANI worktree. A new physical Core service is not required before that integration. The target M1 path must not call old Auth or write old identity tables; existing legacy code and the existing environment may remain until post-M1 replacement. No shared writable identity state or long-lived dual writes are introduced.

Historical CP0 and IAM-side fixtures proved only their documented limited behavior. A fixture publisher can exercise decoder, idempotency, version-gap, DLQ or projection logic, but cannot prove the real Core outbox, Snapshot or cross-project end-to-end integration. These must pass with the current fixed real-owner combination before M1.

The current implementation order is defined only by the WR spec and ticket graph. Cross-repository implementation, infrastructure creation and deployment require their own exact item scope. Core owns its canonical producer contract; IAM pins it and reruns affected producer-consumer gates rather than requiring Core to preserve an experimental fixture.

Lifecycle events use a stable reason-code enum and allowlisted non-sensitive metadata rather than free text or PII. Bootstrap commands contain only the Tenant and operation identities, normalized intended-administrator email, and necessary locale; they contain no password, token, credential, or complete Principal profile. Transport and persistent storage are encrypted and application logs redact the email and other identifiers.
