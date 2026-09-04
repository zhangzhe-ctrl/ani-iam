# DP2-03 executed commands

Result vocabulary is limited to `pass`, `fail`, and `not_verified`.

## Tool identity

| Result | Command | Observed result |
| --- | --- | --- |
| `pass` | `/tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf --version` | `1.72.0` |
| `pass` | `/home/chabking/go/bin/protoc-gen-go --version` | `protoc-gen-go v1.36.12` |
| `pass` | `/home/chabking/go/bin/protoc-gen-go-grpc --version` | `protoc-gen-go-grpc 1.6.2` |
| `pass` | `sha256sum` over the three executables | Exact digests in `hashes.md` and both `contract_pins.json` files. |

## Contract-first RED

The pre-source result is preserved in `red.md` as `fail`: neither repository contained the target descriptors, fixtures, or immutable pins. No target source or generated file existed before this gate.

## Generation and descriptor commands

IAM, from `/home/chabking/workspace/ani-iam/api/iam/v1`:

```text
env PATH=/home/chabking/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  BUF_CACHE_DIR=/tmp/ani-direct-p2-01-05-cache/dp2-03/buf-cache \
  /tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf generate . --template buf.gen.yaml

env BUF_CACHE_DIR=/tmp/ani-direct-p2-01-05-cache/dp2-03/buf-cache \
  /tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf build . \
  --as-file-descriptor-set --exclude-source-info -o iam_descriptor.pb
```

Result: `pass`.

Core, from `/home/chabking/workspace/ANI-direct-p2-01-05/repo/api/proto`:

```text
env PATH=/home/chabking/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  BUF_CACHE_DIR=/tmp/ani-direct-p2-01-05-cache/dp2-03/buf-cache \
  /tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf generate . \
  --template tenant/integration/v1/buf.gen.yaml --path tenant/integration/v1

env BUF_CACHE_DIR=/tmp/ani-direct-p2-01-05-cache/dp2-03/buf-cache \
  /tmp/ani-direct-p2-01-05-cache/dp2-03/bin/buf build . \
  --path tenant/integration/v1 --as-file-descriptor-set --exclude-source-info \
  -o ../../pkg/generated/pb/tenant/integration/v1/tenant_iam_integration_descriptor.pb
```

Result: `pass`.

The two command pairs were executed a second time. SHA-256 output before and after was byte-identical for all source, configuration, descriptors, and generated Go files. Result: `pass`.

Two descriptor attempts were rejected and retained as negative command evidence:

- `buf build . --exclude-source-info -o iam_descriptor.pb` omitted `--as-file-descriptor-set`; it produced a Buf image rather than the pinned descriptor set. Result: `fail`. The corrected command above restored `df863beb...`.
- The first Core descriptor command omitted `--path tenant/integration/v1`; it included the complete ANI Proto module instead of the DP2-03 contract. Result: `fail`. The corrected command above restored `7dd40f90...`.

## Buf gates

| Result | Command | Scope |
| --- | --- | --- |
| `pass` | `buf lint .` | IAM `iam.v1` module |
| `pass` | `buf breaking . --against /tmp/ani-direct-p2-01-05-cache/dp2-03/empty-iam-baseline` | No pre-existing target IAM package; all target services are additions. |
| `pass` | `buf lint . --path tenant/integration/v1` | Core-owned integration package |
| `fail` | `buf breaking . --against .../core-baseline-a221a7b/repo/api/proto --path tenant/integration/v1` | Buf rejected the absent baseline subpath as outside the comparison context. |
| `pass` | `buf breaking . --against /tmp/ani-direct-p2-01-05-cache/dp2-03/core-baseline-a221a7b/repo/api/proto` | Complete immutable ANI Proto module at `a221a7b...`; the new package is additive. |

## Tests and static checks

| Result | Command | Observed result |
| --- | --- | --- |
| `pass` | `go test ./tests/contracts -count=1` | IAM descriptor, Core descriptor, strict fixtures, fingerprint, ErrorInfo, pins, and biz import boundary. |
| `pass` | `go test ./generated/pb/tenant/integration/v1 -count=1` from `repo/pkg` | Independent Core producer/IAM consumer validation. |
| `pass` | `go test ./... -count=1` in `ani-iam` | All IAM packages. |
| `pass` | `go test ./pkg/generated/pb/... ./services/ani-gateway/... ./services/auth-service/... -count=1` in ANI `repo` | Generated contracts plus current Gateway/Auth regression. |
| `pass` | `go vet ./...` in `ani-iam` | All IAM packages. |
| `pass` | `go vet ./pkg/generated/pb/... ./services/ani-gateway/... ./services/auth-service/...` in ANI `repo` | DP2-03 relevant ANI packages. |
| `pass` | `make validate-architecture` in ANI `repo` | Component import guard and retired inference legacy control plane. |
| `fail` | `make test` in ANI `repo` | Stops in accepted obsolete Auth contract gate: `/auth/logout` expects `logout`, and `/auth/api-keys/{key_id}` expects `revokeAPIKey`; DP2-02 froze `logoutSession` and `revokeIAMAPIKey`. |
| `pass` | `cmp` for all eight fixture files and both `contract_pins.json` files | IAM and ANI copies are byte-identical. |

An initial full IAM test using a `/tmp` Go cache had result `fail` because the task-owned `/tmp` quota was exhausted. A sandbox-default home cache attempt also had result `fail` because it was read-only. The final commands used `/home/chabking/.cache/ani-direct-p2-01-05/dp2-03/{go-cache,go-tmp}` and had result `pass`. These environment results do not establish a source defect.

The final ANI related Go regression was also rerun after acceptance. Its default linker temporary directory returned `fail` with `disk quota exceeded`; the exact same command with the Goal-owned `GOCACHE` and `GOTMPDIR` above returned `pass`. `make validate-doc-entrypoints` and `make validate-architecture` returned `pass`. The final `make test` still returned the documented target-contract `fail` at `validate-auth-contract` for legacy `logout` and `revokeAPIKey` expectations.

## Not executed in this ticket

| Result | Check |
| --- | --- |
| `not_verified` | Runtime registration of exactly the three IAM services. `internal/server/**` is forbidden by DP2-03. |
| `not_verified` | Real Core publisher, NATS, snapshot server, IAM consumer, deployment, or remote artifact publication. |
| `not_verified` | Gateway public-operation-to-IAM-RPC runtime mapping and caller cutover. |
