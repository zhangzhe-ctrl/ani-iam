---
status: accepted
---

# Separate functional validation from production readiness

The first P2 delivery proves functional correctness before it proves production performance and concurrency hardening. This ordering is explicit evidence policy, not permission to report deferred checks as passed.

The current functional gate contains unit tests, generated Proto and API contract checks, integration tests against real phase-specific dependencies, CP0 differential tests, static and supply-chain checks, and independent caller end-to-end tests for Gateway, Envoy, and Inference. One caller cannot stand in for another. Each caller covers a valid credential, success, `401`, `403`, dependency `503`, timeout behavior, and authorization-policy version mismatch.

Adapter integration tests normally use `testcontainers-go` with pinned image digests for PostgreSQL, Redis, and, where the phase uses it, NATS. Dex is also exercised as a real OIDC provider when the tested phase requires OIDC. Full caller topology runs in an isolated Docker Compose or CI environment.

Insufficient local resources do not force a fake dependency. The same tests may run on shared test infrastructure if every run receives an isolated PostgreSQL database and restricted runtime role, unique Redis key namespace, unique NATS account or fully isolated subject, Stream, and durable Consumer names, a dedicated Dex client and test identities, and dedicated test credentials. Tests must create their declared starting state, tolerate concurrent runs, and clean up only their own resources. A shared mutable database, global Redis flush, reused durable Consumer, shared test user, or dependence on state left by another run is not acceptable evidence. If the shared environment cannot provide the required isolation or capability, the affected result is `not_verified` and its functional gate does not pass.

PostgreSQL integration starts from an empty isolated database, replays the Atlas migration directory, and separates the migration owner from the restricted application role. Business queries and cross-Tenant negative tests run as the application role. Concurrency behavior is not claimed by tests hidden inside a single transaction that is rolled back by the test harness.

CP0 exercises the old database roles and real RLS. P2 exercises its new database without RLS and proves isolation through explicit Tenant predicates, composite constraints, two-Tenant negative cases, and query-mutation tests. Evidence from one model cannot be substituted for the other.

Differential tests normalize only nondeterministic identifiers, timestamps, Tokens, and Secrets. They compare gRPC status, public error code, trusted Claims, durable PostgreSQL and Redis state, and externally visible side effects. Every intentional semantic difference is named in an allowlist and linked to an accepted decision.

Argon2id latency and memory gates, concurrent `CheckPermission` load gates, `go test -race`, Fuzz tests, and dedicated concurrency suites are deferred until production-readiness hardening. They do not block the first functional implementation. Until completed, their status remains `not_verified`; they are required before any production-readiness or production-release claim. Functional test success must not be described as performance, race-safety, or abuse-resistance validation.

The current static and supply-chain gate includes repository-selected Go lint and static analysis, `govulncheck`, Buf lint and breaking-change checks, a clean sqlc generation diff, Atlas empty-database replay and directory checksum verification, an SBOM, and a license inventory. A missing required tool or real dependency is `not_verified`, never an implicit pass.
