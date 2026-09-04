# DP2-03 review

## Standards review

| Result | Finding |
| --- | --- |
| `pass` | Proto source, pinned generator configuration, generated Go, descriptor, fixtures, and pins are co-located in the accepted commit sets. Generated files were not hand edited. |
| `pass` | Exact generator versions, executable hashes, Google APIs module commit/digest, inputs, commands, and outputs are recorded. |
| `pass` | IAM remains inside `api/iam/v1/**` plus contract tests/evidence; ANI remains inside the ticket's Proto/generated paths. |
| `pass` | IAM `internal/biz` has no Proto, Kratos, gRPC, data, pgx, Redis, or NATS imports. |
| `pass` | No Wire, Makefile, runtime composition, business implementation, shared internal package, shared database, or infrastructure mutation was introduced. |
| `pass` | Both repositories independently decode peer fixtures using immutable descriptors. |
| `fail` | ANI aggregate `make test` still contains the accepted DP2-02 legacy Auth operationId gate and stops on `logout`/`revokeAPIKey`. This is not repaired by reverting the frozen target contract. |

Standards blocking findings inside the DP2-03 allowed source/generated paths: none.

ANI requires Feature-batch documentation closure. The human checkpoint approved, and the ANI contract commit updated, the four mandatory paths:

- `repo/development-records/DP2-03-iam-core-integration-contracts.md`
- `repo/development-records/README.md`
- `repo/CURRENT-SPRINT.md`
- `ANI-06-开发计划.md`

No other ANI path expansion was used.

## Specification review

| Result | Requirement |
| --- | --- |
| `pass` | Target IAM descriptor contains exactly the three named services and excludes legacy `auth.v1.AuthService`. |
| `pass` | Stable gRPC code plus `google.rpc.ErrorInfo` reason/domain/metadata is exhaustive for IAM and Core fixture enums. |
| `pass` | Core owns lifecycle/ID; the IAM-facing Core service is read-only and cannot write lifecycle. |
| `pass` | Lifecycle version, bootstrap fingerprint, snapshot cursor/page/version, schema-major, and additive-major policy are frozen. |
| `pass` | Producer-consumer fixtures, descriptors, digest pins, and independent repository consumption are reproducible. |
| `pass` | No shared database, cross-database transaction, shared internal Go package, public second entry, publisher, or NATS infrastructure was added. |
| `not_verified` | Running IAM process registers exactly these three services; runtime paths are forbidden in DP2-03. |
| `not_verified` | Real Core producer, snapshot server, IAM consumer, NATS delivery, deployment, and publication. |
| `not_verified` | Exact Gateway public-operation-to-RPC runtime mapping and caller behavior. |

Specification blocking findings for freezing the contract artifacts: none. The `not_verified` items prohibit a runtime/cutover claim and are carried into later tickets.

Human checkpoint result: `pass`. The exact contract diff and four documentation paths were accepted before local commits `1bdc3e3657c233b5a47be706f251a4529ec80b5b` and `573d3735934f74f9f1eb78818cddefefd9f575eb` were created.
