# DP2-03 human checkpoint package

Status: accepted 2026-09-04; contract commits created in the required dependency order.

## Starting commits

- ani-iam: `main@5ff9f3cfe083b3b911bb076450abbbb967e82a37`
- ANI durable worktree: `codex/direct-p2-01-05@a221a7b50c2cfdb13f04c13f154338d836a48af3`

## Accepted local commits

- ani-iam target IAM contract: `1bdc3e3657c233b5a47be706f251a4529ec80b5b` (`feat(dp2-03): freeze target iam contracts`)
- ANI Core contract/fixtures/generated/docs: `573d3735934f74f9f1eb78818cddefefd9f575eb` (`feat(dp2-03): freeze core iam integration contracts`)

## Exact contract result

- IAM: 3 target services / 69 methods; descriptor SHA-256 `df863beb3b095d1f01350c5334d80daf10cdf48083ce0e5663781171aa99a001`.
- Core: 1 read-only snapshot service / 2 methods plus lifecycle/bootstrap/heartbeat/snapshot messages; descriptor SHA-256 `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a`.
- Legacy `auth.v1.AuthService`: absent from target descriptor.
- Both `contract_pins.json`: SHA-256 `33376182b2bcd2f0dd7c84bdf9790d492b6a643560a169e80c0fe63e9113c3b9`.
- Eight producer-consumer fixtures: byte-identical and exhaustively pinned.

## Breaking summary

The new versioned packages are additive relative to both immutable starts, so Buf breaking is `pass`. They are intentionally breaking as a future replacement for legacy `auth.v1.AuthService`: legacy clients must regenerate and map to the new three-service surface. No caller is switched and no old symbol/runtime asset is deleted in this ticket.

## Accepted file inventory

ani-iam, 36 files:

```text
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/breaking.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/checkpoint.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/commands.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/contracts.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/descriptor-inventory.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/fixtures.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/hashes.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/red.md
.scratch/ani-iam-p2-direct/evidence/03-freeze-iam-core-contracts/review.md
.scratch/ani-iam-p2-direct/issues/03-freeze-iam-core-contracts.md
api/iam/v1/authentication_service.pb.go
api/iam/v1/authentication_service.proto
api/iam/v1/authentication_service_grpc.pb.go
api/iam/v1/authorization_service.pb.go
api/iam/v1/authorization_service.proto
api/iam/v1/authorization_service_grpc.pb.go
api/iam/v1/buf.gen.yaml
api/iam/v1/buf.lock
api/iam/v1/buf.yaml
api/iam/v1/contract.pb.go
api/iam/v1/contract.proto
api/iam/v1/iam_admin_service.pb.go
api/iam/v1/iam_admin_service.proto
api/iam/v1/iam_admin_service_grpc.pb.go
api/iam/v1/iam_descriptor.pb
tests/contracts/artifacts/core_tenant_integration_v1_descriptor.pb
tests/contracts/contract_pins.json
tests/contracts/contracts_test.go
tests/contracts/fixtures/core_error_contract.v1.json
tests/contracts/fixtures/core_tenant_iam_bootstrap_requested.v1.json
tests/contracts/fixtures/core_tenant_lifecycle_changed.v1.json
tests/contracts/fixtures/core_tenant_lifecycle_heartbeat.v1.json
tests/contracts/fixtures/core_tenant_lifecycle_snapshot_page.v1.json
tests/contracts/fixtures/iam_check_permission.v1.json
tests/contracts/fixtures/iam_error_contract.v1.json
tests/contracts/fixtures/iam_password_login.v1.json
```

ANI durable worktree, 20 files:

```text
ANI-06-开发计划.md
repo/CURRENT-SPRINT.md
repo/api/proto/tenant/integration/v1/buf.gen.yaml
repo/api/proto/tenant/integration/v1/contract_pins.json
repo/api/proto/tenant/integration/v1/tenant_iam_integration.proto
repo/api/proto/tenant/integration/v1/testdata/core_error_contract.v1.json
repo/api/proto/tenant/integration/v1/testdata/core_tenant_iam_bootstrap_requested.v1.json
repo/api/proto/tenant/integration/v1/testdata/core_tenant_lifecycle_changed.v1.json
repo/api/proto/tenant/integration/v1/testdata/core_tenant_lifecycle_heartbeat.v1.json
repo/api/proto/tenant/integration/v1/testdata/core_tenant_lifecycle_snapshot_page.v1.json
repo/api/proto/tenant/integration/v1/testdata/iam_check_permission.v1.json
repo/api/proto/tenant/integration/v1/testdata/iam_error_contract.v1.json
repo/api/proto/tenant/integration/v1/testdata/iam_password_login.v1.json
repo/development-records/DP2-03-iam-core-integration-contracts.md
repo/development-records/README.md
repo/pkg/generated/pb/iam/v1/iam_descriptor.pb
repo/pkg/generated/pb/tenant/integration/v1/contract_test.go
repo/pkg/generated/pb/tenant/integration/v1/tenant_iam_integration.pb.go
repo/pkg/generated/pb/tenant/integration/v1/tenant_iam_integration_descriptor.pb
repo/pkg/generated/pb/tenant/integration/v1/tenant_iam_integration_grpc.pb.go
```

## Accepted ANI Allowed-path expansion

The human checkpoint explicitly approved these four paths for ANI's mandatory Feature-batch closure:

- `repo/development-records/DP2-03-iam-core-integration-contracts.md`
- `repo/development-records/README.md`
- `repo/CURRENT-SPRINT.md`
- `ANI-06-开发计划.md`

No other path expansion was used.

## Recovery

Use new revert commits in reverse dependency order: first ANI `573d3735934f74f9f1eb78818cddefefd9f575eb`, then ani-iam `1bdc3e3657c233b5a47be706f251a4529ec80b5b`. No reset, stash, amend, force, push, publish, deployment, NATS creation, Core publisher start, or caller cutover is authorized.
