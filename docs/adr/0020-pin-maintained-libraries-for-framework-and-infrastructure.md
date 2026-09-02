---
status: accepted
---

# Pin maintained libraries for framework and infrastructure

CP0 evaluates Kratos v3 and pins an exact released patch version in `go.mod` and `go.sum`; builds and task prompts never resolve `latest`. ANI's current Go 1.25 baseline satisfies the current Kratos v3 toolchain requirement, but the pinned version still passes the CP0 transport and dependency gates before promotion.

PostgreSQL adapters use sqlc-generated Go over pgx/v5. SQL remains an explicit, reviewed source artifact, while sqlc removes repetitive row scanning and method boilerplate and checks result and parameter types. pgx owns pooling, protocol, transactions, and PostgreSQL type behavior.

This choice is preferred over GORM for this IAM, not as a general rejection of GORM. Removing RLS makes every `tenant_id`, lifecycle, status, version, and lock predicate a security control that reviewers and negative tests must see. The target schema also relies on composite Tenant keys, partial unique indexes, explicit `FOR UPDATE`, conditional state transitions, CTEs or `RETURNING`, and no generic soft delete. sqlc keeps these semantics visible in SQL and generates narrow typed calls without ORM scopes, association loading, callbacks, implicit transactions, or automatic `deleted_at` behavior. GORM would be faster for generic CRUD and database portability, but neither is a goal of this PostgreSQL-specific security service. The accepted cost is more deliberate SQL and explicit domain mapping.

Versioned migrations continue to use Atlas migration files and `atlas.sum`, matching ANI's existing toolchain. The required basic gate applies, replays, and checks migrations against a real empty PostgreSQL database and verifies directory integrity. It does not assume a paid Atlas Pro migration-lint entitlement; adding that gate requires an explicit license and procurement decision.

JWT, JWS, and JWK behavior uses a pinned stable lestrrat-go/jwx release behind TokenSigner and TokenVerifier adapters. OIDC discovery and ID Token verification use coreos/go-oidc/v3 with golang.org/x/oauth2. ANI implements its state persistence and Principal mapping but not JOSE parsing, discovery, code exchange, signature verification, or OAuth protocol machinery.

Argon2id uses a maintained thin library based on `golang.org/x/crypto/argon2` that includes PHC encoding and parsing and test-vector coverage. The exact package and version must pass the CP0 dependency review; ANI does not implement Argon2, PHC parsing, or constant-time comparison itself.

UUIDv7 generation uses a pinned google/uuid version behind an IDGenerator port. Redis uses redis/go-redis/v9 and its supported OpenTelemetry instrumentation. JetStream uses the official nats-io/nats.go `jetstream` API against infrastructure-created Streams and Consumers. Applications do not create or modify broker infrastructure.

Kratos middleware composes the OpenTelemetry SDK and exporters plus Prometheus metrics. ANI defines an allowlist of semantic attributes and domain metrics but does not create a proprietary logging, tracing, or metrics SDK.

Every dependency is pinned, license-recorded, included in the SBOM and vulnerability scan, and wrapped only where the domain needs a stable port or the library must be prevented from leaking into biz. Wrapping does not mean reimplementing the library.
