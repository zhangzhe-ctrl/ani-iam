# DP2-09 baseline and generation evidence

## Fixed identities

- IAM start: `codex/direct-p2-06-14@56ab0fcc7c6117f11e0231e8d37a127d0c2f0a57`.
- Human-accepted ANI start: `codex/direct-p2-06-14@9bfedfd04c75533e01fa3d88419e7a3454f79404` in the dedicated worktree `/home/chabking/workspace/ANI-direct-p2-06-14`.
- The original `/home/chabking/workspace/ANI/repo` worktree was not modified.
- At claim time both selected worktrees and indexes were clean, DP2-05 through DP2-08 were `resolved`, and no other Direct P2 issue was `claimed`.

## Generator-first registry repair

The accepted ANI baseline had stale generated target-registry outputs after nine email-notification operations had been removed from the frozen inputs. The initial command below was RED:

```text
python services/ani-gateway/tools/dp2_operation_registry.py --check
```

The generator changed only the two human-approved generated outputs plus the target registry's static count assertions:

- `repo/api/openapi/operation-registry.v1.json`
- `repo/services/ani-gateway/internal/authz/zz_generated_target_operation_registry.go`
- `repo/services/ani-gateway/internal/authz/target_operation_registry_test.go`

No OpenAPI, policy, replacement, or other generator input was changed. The generated inventory moved from 298 to 289 operations and from 278 to 269 `check_permission` operations. The generated policy revision is:

```text
sha256:1d5c80b83635e9a152c0edd9e8d1c9b66f5f8962e84cdd4f4701ecc486dd969c
```

Current generated artifact digests:

```text
742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80  repo/api/openapi/operation-registry.v1.json
04118026b146d22afbf688006f6e266f3406c4f168e0a3a293c7f0baa4af7732  repo/services/ani-gateway/internal/authz/zz_generated_target_operation_registry.go
```

The following checks passed after regeneration:

- `python services/ani-gateway/tools/dp2_operation_registry.py --check`
- `make validate-gateway-authz`
- `make test`
- `make validate-architecture`
- `make validate-doc-entrypoints`
- `git diff --check`

Result: `pass` for the fixed-baseline registry repair. These results do not accept a moving ANI `main`, deploy ANI, or authorize any path outside the issue.

## Generator-first IAM policy and database catalog

The first Standards review correctly rejected the policy map as an undocumented generated file. The second review then rejected the first repair command because it wrote directly into the worktree. The final compliant flow adds the checked-in generator at internal/data/cmd/genoperationregistry, treats the accepted ANI registry digest as an immutable input, and generates outside the worktree:

    go run ./internal/data/cmd/genoperationregistry \
      -input /home/chabking/workspace/ANI-direct-p2-06-14/repo/api/openapi/operation-registry.v1.json \
      -expected-sha256 742147f0b370b565667748a0c8194a49f79677aa192fc3262a24c2de8eae6f80 \
      -go-output /tmp/ani-dp2-09-policy-gen.dqoxI3/generated_operation_policies.go \
      -sql-output /tmp/ani-dp2-09-policy-gen.dqoxI3/202609080002_permission_catalog.sql

Both temporary outputs were compared byte-for-byte with the candidate and then copied from the validated temporary directory into the two approved worktree paths. The final comparison remained identical. sqlc was separately run from /tmp/ani-dp2-09-sqlc-gen.dqoxI3 using copies of sqlc.yaml, migrations, and internal/data/queries; all four generated Go outputs compared byte-identical before any worktree move was considered.

The generator validates the registry schema, policy revision, unique operation identities, supported scopes, non-empty actions, and the accepted typed obligation. It emits both the Go policy map and the migration-owned relational Permission Catalog. The catalog contains 136 unique permissions: 81 tenant, 54 platform, and 1 own. tenant_role_permissions gains an explicit tenant-only scope check and a composite foreign key to permission_catalog, so an uncatalogued or cross-scope permission is rejected by PostgreSQL rather than only by application code.

Two consecutive generations and a second sqlc generation produced identical bytes:

    b3c04957de48fa920ea545b72fd0f1b4f2dbe3cb379d47cc5192e8d68b459b37  internal/data/cmd/genoperationregistry/main.go
    78562e4e37d5c30d7dd7af2f2b1c74a3bbb92d20527cfa0c3912330c98c9eba6  internal/data/generated_operation_policies.go
    814e31cd79eef9fb750a7a439bb09ab74f37ec3230dff63cf0b457ce7ef59dd4  migrations/202609080002_permission_catalog.sql
    989bc2d24ee46dcbec302fc7d24027969b84d8ee9d094786745690ce783b4b91  internal/data/queries/persistence.sql
    c40f70c9870c43ddc88f743ec63627bc9af2d57fab48fdb0c055981d090b0cf8  internal/data/sqlcgen/db.go
    b30be65c7bb905fa73e2ac2ab6e6a5cdd66fa71c8baf01a667478a87fa9d4d76  internal/data/sqlcgen/models.go
    ec9b72409c392fc8db9a6d6d290a82eecaaa5187fce905495d74eb5b2e4615ab  internal/data/sqlcgen/persistence.sql.go
    d9adb5b431b6845d02f0b5fbd1d78055dffde88716a820cac1768b1d6bc4e2f0  internal/data/sqlcgen/querier.go
    bd000a3d2a7eb2ce20024f720057061fbcc0f9c8bb36ac19c8cbf0ca0a12e2f4  migrations/atlas.sum

The focused real PostgreSQL gate proves SQLSTATE 23503 for an uncatalogued tenant permission and 23514 for a catalogued platform permission inserted into a Tenant Role. Atlas checksum validation and empty-database replay are part of the same integration gate.

## Generator-first PasswordLogin consumer synchronization

The accepted IAM source commit `74d7e441435d35dc66a733c8bf4a93129b26de3f` added `PasswordLoginRequest.source_ip = 7`; the exact bytes remain present at the IAM ticket start. ANI's older descriptor and generated request were therefore a proven compile-time RED at `GetSourceIp`.

The generation ran in `/tmp/ani-dp2-09-contract-gen.*` rather than over the ANI worktree. It used:

```text
Buf 1.72.0 official Linux x86_64
8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af  buf
7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010  protoc-gen-go v1.36.12
aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08  protoc-gen-go-grpc 1.6.2
```

The existing `services/ani-gateway/internal/targetiam/buf.gen.yaml` consumed the synchronized descriptor. A second run produced identical bytes. All generated outputs other than `authentication_service.pb.go` compared byte-identical to the ANI worktree; `authentication_service_grpc.pb.go` was explicitly approved but had no byte change. Final approved consumer hashes are:

```text
0eecac05a4750e6e863da32366b1b7bfa7a7557477832ec5373bd2c77b0da153  authentication_service.pb.go
77bf6dc3630c0856643544414e5be6fde4dbe5e434534c8c10d534c8345ec67b  authentication_service_grpc.pb.go
66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a  iam_descriptor.pb
a53fa92a48cc7bb85c1f4c22363c7adf0cf8dad5e746612252e6c18ab7e13bf1  contract_pins.json
cb4df637172faae8551d42abd55b9167195d9a21693080a06e713ae9c6483cc4  iam_password_login.v1.json
```

ANI's strict pin decoder rejected a byte-for-byte copy of IAM's newer file because its private `notification` block is outside the ANI consumer schema. The final ANI pins therefore preserve the existing schema and update only the relevant descriptor and fixture digests. The immutable-pin/fixture test is `pass`.

The full ANI Feature-batch gate was executed against an isolated checkout of the accepted commit with the complete candidate binary diff applied and the new batch record copied in. This preserved the approved ANI worktree paths while allowing the target to exercise SDK/API-doc idempotence. The candidate patch digest was `fbe74f92eb28741eeff96280268da2ab90c59564ac19e6b46fdc8a3835998eeb`; after the gate, the isolated checkout had exactly the same tracked/untracked candidate paths and no generator-created tracked drift.

```text
TMPDIR=<task-owned writable directory> \
GOTMPDIR=<task-owned writable directory> \
make validate-services
```

Final result: `pass` (`Services PR gate valid`). Two earlier attempts were environmental evidence only: the configured global Go temporary directory was read-only inside the sandbox, then `/tmp` exhausted its quota during compilation. No source change was made for either failure; the same candidate passed when the Go work directory was moved to the task-owned filesystem and local `httptest` listeners were permitted.
