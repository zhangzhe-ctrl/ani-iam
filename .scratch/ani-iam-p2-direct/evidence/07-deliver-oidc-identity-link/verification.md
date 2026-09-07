# DP2-07 verification matrix

Fixed IAM baseline:
`b52907dc4cb919dbbbe68768734e0a453b36b954`.

Reviewed feature commit:
`33097ae02985ed569eba0f8176f184ce9f6ff849`.

| Gate | Result | Evidence |
| --- | --- | --- |
| Focused biz/data/service tests | `pass` | Final focused packages passed; `internal/data` completed in `1.898s`. |
| Real Redis/PostgreSQL/Dex focus | `pass` | Combined focused integration passed in `4.486s`. |
| Complete integration | `pass` | Every package passed; real integration package `132.483s`. |
| Complete integration with race detector | `pass` | Every package passed; real integration package `103.038s`. |
| `go vet ./...` | `pass` | Offline final-tree invocation exited zero. |
| `go mod verify` | `pass` | `all modules verified`. |
| `go mod tidy -diff` | `pass` | Corrected task-cache invocation produced no diff. |
| `git diff --check` | `pass` | No whitespace errors. |
| Frozen contract tests | `pass` | `go test ./tests/contracts -count=1`. |
| Config Proto lint/generation | `pass` | Fixed Buf and local fixed plugin reproduced `conf.pb.go` byte-for-byte. |
| Public API descriptor | `pass` | Source-info-stripped `FileDescriptorSet` reproduced the frozen descriptor byte-for-byte. |
| sqlc generation | `pass` | Two consecutive fixed-generator runs produced the same generated-tree digest. |
| Atlas migration validate/hash | `pass` | Fixed Atlas Community validated and reproduced `atlas.sum`. |
| Vulnerability scan | `pass` | Zero affected symbols/packages; module-only unreachable advisory retained. |
| Deterministic SBOM/license inventory | `pass` | Two byte-identical CycloneDX 1.6 outputs; license exception recorded. |
| Current-tree Secret assessment | `pass` | Exact tracked/nonignored-untracked snapshot classified all heuristics. |
| Independent Standards review | `pass` | No remaining finding. |
| Independent Spec review | `pass` | No remaining finding. |
| Test resource cleanup | `pass` | Both running and all-container Docker listings empty. |

## Fixed generator identities and outputs

```text
Buf v1.72.0 binary sha256
8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af

protoc-gen-go v1.36.11 binary sha256
f8682681cbeb0bbf9e9570ca9d90fdd9783d8170a7e995af122356fe5a9f3cb3

conf.proto sha256
a4812ba9729ad60194488725dfe6ecba9858311381e4824205dbd289d3a9ca0a

conf.pb.go sha256, both runs
32c3ec90ad8de80701c695336e0df0601027144456190bb5a46acc4abc4dec2d

IAM FileDescriptorSet sha256
66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a

sqlc v1.31.1 binary sha256
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f

sqlc generated-tree digest, both runs
b735429f8573c254da9d8e1d2f6925cd1330d6fe3b34c8ffa9d6ec10df0a073b

OIDC migration sha256
3e6e16ec9031922acfe8138ac646827bce4d703c7babefb42c90c6a4e0fa084a

atlas.sum sha256, both runs
735f8a9b0eece88ed8e9e1af90c987ddb8a3e56ddd6d2c6e1c0390d21bda6804

query source sha256
ee81939872019a991e69c5e39e4811de1f5d7b2358f49b55531a072ba74465db

generated query sha256
a24291b6f13304e2943a34edfd939c29fb5665c30e01eca5136c1501ad11648a
```

The first public-API Buf attempt used `api` rather than the nested module root
and was rejected for missing imports. A second attempt omitted
`--as-file-descriptor-set` and produced a Buf image rather than the checked-in
descriptor format. The corrected fixed command used `api/iam/v1`,
`--as-file-descriptor-set`, and `--exclude-source-info`; it reproduced the
checked-in digest exactly. These are command-shape errors, not source failures.

One `go mod tidy -diff` invocation omitted the task-owned build cache and was
rejected by the read-only default cache path. The corrected offline invocation
with exact `GOCACHE`/`GOTMPDIR` passed without module drift.

## Explicitly not verified

- BOSS/Platform OIDC login and a Platform Session/Grant schema are
  `not_verified`. DP2-07 owns the Console/Tenant slice; DP2-15 explicitly owns
  Platform Membership/Role/BOSS, and the active Goal forbids starting DP2-15.
- The accepted plan also asks for BOSS OIDC before Go/No-Go B while DP2-13 is an
  exercise-only ticket and DP2-15 comes later. This is an authority/order
  conflict, not a DP2-07 pass. DP2-13 must stop for human reordering or reopening
  rather than claim Go/No-Go B or proceed to DP2-14.
- Real ANI Gateway/browser callback integration, production Dex, Kubernetes
  Secret mounting/rotation, deployment and production traffic are
  `not_verified` and were not authorized here.
- Production performance, load, fuzzing and production-readiness controls are
  `not_verified`; the accepted plan does not make them DP2-07 blockers.
- Remote Proto plugin transport is `not_verified`; the exact already-cached
  v1.36.11 plugin reproduced the generated file locally.
