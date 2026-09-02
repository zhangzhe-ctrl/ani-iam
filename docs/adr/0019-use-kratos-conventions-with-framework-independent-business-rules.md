---
status: accepted
---

# Use Kratos conventions with framework-independent business rules

The independent IAM repository follows the standard Kratos top-level layout: `internal/biz`, `internal/data`, `internal/service`, `internal/server`, and `internal/conf`, with `cmd/server` as the composition root. This convention is intentional governance for human and AI contributors and replaces an unconstrained domain-first directory invention. The `biz` and `data` packages may still use focused subpackages when size requires them, but they do not replace the Kratos layer vocabulary.

Business entities, use cases, and ports in `internal/biz` do not import Kratos packages or Kratos error, logging, configuration, or transport types. This does not authorize hand-written replacements for framework or protocol capabilities. Kratos supplies application lifecycle, gRPC transport, middleware composition, configuration integration, logging, recovery, health wiring, and observability at the outer layers. Mature maintained libraries supply JOSE/JWT, OAuth/OIDC, cryptographic primitives, PostgreSQL, Redis, NATS, Protobuf, tracing, metrics, and migration behavior behind adapters.

The impact of the framework-independent biz boundary is deliberate. Service facades translate Protobuf and Kratos errors into typed commands, results, and domain errors; data adapters translate driver errors; and unit tests can exercise rules without starting Kratos. It adds explicit request/result and error mapping at two edges, but prevents a Kratos upgrade, transport change, or generated message revision from changing domain and persistence APIs. Standard `context.Context` remains permitted and cancellation and deadlines are propagated through every port.

Generated Protobuf messages do not enter biz entities or repository signatures. `internal/service` contains thin mappings and no authorization or state-transition decisions. Ports are declared next to the biz use case that consumes them; adapters in `internal/data` implement them, and no global interface dumping ground or adapter-owned interface reverses the dependency.

Application use cases own transactions through a Unit of Work port. Repositories never independently commit a multi-object operation, and gRPC handlers do not control database transactions. State mutation and its AuditRecorder share the same transaction.

One process registers `AuthenticationService`, `AuthorizationService`, and `IAMAdminService` on one Kratos gRPC server. It enables no Kratos HTTP business transcoding. A separate cluster-internal admin listener exposes health, readiness, and metrics only and is not published or permitted to register business handlers.

The basic repository uses explicit constructors and one composition root instead of Wire or a service locator. This is compile-time Go wiring, not a custom dependency-injection framework: it contains object construction only and implements no lifecycle, discovery, configuration, or reflection mechanism. If the graph later becomes materially unmanageable, adopting Wire requires a separate measured decision.

Configuration is loaded and validated once into typed configuration. Non-secret values may come from files or environment expansion; credentials and signing material come only from a Secret Manager, mounted secret file, or dedicated secret environment variable and never from committed YAML. Modules receive typed configuration rather than reading process environment variables directly.

CP0 and P1 wire-compatibility code is isolated under `internal/compat/authv1` and the differential test harness. Target biz modules cannot import compatibility Protobuf. P2 deletes the compat boundary without changing target use-case interfaces.

Dependency selection follows a library-first policy: use maintained, license-compatible, widely exercised upstream implementations for protocols and infrastructure, pin exact versions, wrap them at a narrow adapter boundary, and verify required semantics with conformance tests. Custom code is limited to ANI domain rules, transaction orchestration, explicit mappings, and gaps that no selected library correctly owns; it does not reimplement cryptography, OAuth/OIDC, JWT/JWK, database pools, brokers, tracing, metrics, retry primitives, or framework middleware.
