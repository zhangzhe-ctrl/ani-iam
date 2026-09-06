# DP2-05 supply-chain evidence

Result: `pass`

## Pinned scanners

```text
CycloneDX Go module generator: v1.10.0
module sum: h1:9Vy3zcC+lJLgcR4xYQvwPGU6L2Rij/Ld47lyucYjVI0=
binary SHA-256: 0fd48276ea79a679660005f0a5550ce83fb34bea439b444099c5d3aab6d8cfbb

govulncheck: v1.7.0
binary SHA-256: 93670af3221b64b9c08c9fb2c9cfb18e35a5f1c7b51808a7ca08c9719580cb83
```

## SBOM and license inventory

The initial offline collection attempt returned `fail` because four module
lookups were not already present in the local cache and `GOPROXY=off` forbids
resolving them. The allowed dependency download was then used by the pinned
generator:

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp \
  GOPROXY=https://proxy.golang.org,direct \
  /tmp/ani-direct-p2-01-05-dp2-05/tools/cyclonedx-gomod \
  mod -test -licenses -assert-licenses -json \
  -output-version 1.6 -noserial -notimestamp \
  -output .scratch/ani-iam-p2-direct/evidence/05-prove-target-vertical-slice/bom.cdx.json .
```

Observed exit code: `0`. The generator warned that the root private project
module has no detected license; every one of the 94 dependency components in
the generated SBOM has at least one detected license. The dependency license
identifiers are:

```text
0BSD, Apache-2.0, BSD-2-Clause, BSD-3-Clause, BSD-Source-Code,
CC-BY-SA-4.0, MIT, MIT-0
```

Artifact:

```text
3b764a875600d0878b5467ea2ff52fdf74e7e3e6d7b2f5ebaee20873992f2ed4  bom.cdx.json
```

The fixed command was immediately rerun with `GOPROXY=off`; it returned exit
code `0` and reproduced the same SBOM SHA-256. The only warning concerns the
private root project module, not a dependency component.

One later operator-input attempt misspelled the Goal-owned `GOTMPDIR` value.
The generator still exited successfully but emitted blank GOOS/GOARCH PURL
qualifiers and the temporary SHA-256
`984f2b113c924ad4a43853111c2dca17a6956f99d51bbf94fdc8ddb1f37c9348`.
That attempt is recorded as `fail`; a semantic diff showed only the blank
GOOS/GOARCH qualifiers. The exact fixed command above was then rerun twice and
both runs reproduced the accepted
`3b764a875600d0878b5467ea2ff52fdf74e7e3e6d7b2f5ebaee20873992f2ed4`
artifact. The typo is not treated as generator nondeterminism or as a passing
run.

The three dependencies introduced for this tracer bullet are fixed by module
and source-license identity:

| Module | Go sum | License | LICENSE SHA-256 |
| --- | --- | --- | --- |
| `github.com/alexedwards/argon2id v1.0.0` | `h1:wJzDx66hqWX7siL/SRUmgz3F8YMrd/nfX/xHHcQQP0w=` | MIT | `c7f183250ac7e0e0734ffe5f472ea4ce2ec56071f78b9a0c72c54b5a875f5d5a` |
| `github.com/lestrrat-go/jwx/v3 v3.2.0` | `h1:Jb3zBASTSZXz7gzzSAfYqxXF8KejvKC4xWoePLQqXCA=` | MIT | `0c4868a07b1c2dbb67c75767b4f1b2936c392d02ca2c7ee9827cac1a9d60ab1d` |
| `github.com/redis/go-redis/v9 v9.22.0` | `h1:laDvpYXTJtZLloinw1fA5Kqd6HAEH2XKxOkG/PDq2F0=` | BSD-2-Clause | `a3a7dff87da3927db65cb4c87b1cfbc96ca2755704461a485d457be7ae300a86` |

## Vulnerability scan

```text
env GOCACHE=/tmp/ani-direct-p2-01-05-dp2-05/go-build \
  GOTMPDIR=/tmp/ani-direct-p2-01-05-dp2-05/go-tmp \
  GOPROXY=off \
  /home/chabking/.cache/ani-direct-p2-01-05/dp2-05/tools/govulncheck \
  -test -tags integration -show verbose ./...
```

Observed exit code: `0`. The scan covered 31 root packages, 84 modules and the
Go standard library. Symbol and package results reported no vulnerabilities,
and the final result was:

```text
Your code is affected by 0 vulnerabilities.
```

The module inventory also reported `GO-2026-5932` for the unmaintained
`golang.org/x/crypto/openpgp` package with no fixed version. No scanned root
package imports or calls that package, so govulncheck reported zero affected
symbols and packages. This advisory remains recorded rather than being hidden
or reclassified as an affected-code failure.

A sandboxed rerun could not reach the official vulnerability database and is
recorded as `fail` with `socket: operation not permitted`. The identical
fixed-binary command was then run with the already-authorized network access;
that final scan returned exit code `0` and is the `pass` result above.
