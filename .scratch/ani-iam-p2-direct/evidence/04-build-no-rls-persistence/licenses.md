# DP2-04 dependency licenses and SBOM

Result: `pass`

Generator: `github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.12.0`

- Module sum: `h1:OuFUYNhnjpju7RNArOVPPchFPWNobGfhrHODDPKcgZs=`
- Generator binary SHA-256: `cf1665a269a5599962512168caccc56f471012627cccccfd5ec984f77065fc01`
- CycloneDX format: JSON 1.6, deterministic (`-noserial -notimestamp`)
- Final SBOM: `bom.cdx.json`
- Final SBOM SHA-256: `78631c4efaf58c92ac528e15708cf43f2ad09a99f4071807319386fe84f3d383`

Actual command:

```text
cyclonedx-gomod mod \
  -test -licenses -assert-licenses -json -output-version 1.6 \
  -noserial -notimestamp \
  -output .scratch/ani-iam-p2-direct/evidence/04-build-no-rls-persistence/bom.cdx.json \
  .
```

The final SBOM contains 80 third-party library components and zero third-party components without a detected license. Test dependencies are included so the PostgreSQL Testcontainers path is inventoried.

| SPDX result | Components |
| --- | ---: |
| `0BSD` | 1 |
| `Apache-2.0` | 39 |
| `BSD-2-Clause` | 2 |
| `BSD-3-Clause` | 15 |
| `CC-BY-SA-4.0` | 1 |
| `MIT` | 22 |

New and security-fixed dependencies are recorded as:

| Module | Version | License |
| --- | --- | --- |
| `github.com/google/uuid` | `v1.6.0` | `BSD-3-Clause` |
| `github.com/jackc/pgx/v5` | `v5.9.2` | `MIT` |
| `github.com/testcontainers/testcontainers-go` | `v0.43.0` | `MIT` |
| `github.com/testcontainers/testcontainers-go/modules/postgres` | `v0.43.0` | `MIT` |
| `github.com/moby/go-archive` | `v0.3.0` | `Apache-2.0` |
| `golang.org/x/crypto` | `v0.56.0` | `BSD-3-Clause` |

`cyclonedx-gomod` selected `CC-BY-SA-4.0` for `github.com/opencontainers/go-digest@v1.0.0` because that module also ships `LICENSE.docs`. Manual source inspection distinguishes its Apache-2.0 code `LICENSE` (SHA-256 `aceb55133c41c50c2e170c5918e301fe74f592d0f28724bf2f7a7bfdcff1fc5f`) from the documentation-only `LICENSE.docs` (SHA-256 `a1d1adec3ba691ef6e377a78bbe9e9e2501f2f4a1723d08857da1ac6564e4fed`). The generated SBOM is preserved unedited; this inventory records the classification context.

The tool warned that the root module has no detected repository license. The root module is not a third-party dependency; choosing the repository's own distribution license remains an Owner decision and is not misreported as a third-party license failure.
