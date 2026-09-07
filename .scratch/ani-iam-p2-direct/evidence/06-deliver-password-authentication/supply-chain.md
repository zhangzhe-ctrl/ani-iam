# DP2-06 final supply-chain evidence

Status: affected-code vulnerability scan `pass`; deterministic SBOM `pass`;
current working-tree Secret assessment `pass` after explicit heuristic
classification. The Notification first-party proprietary/no-license state is
covered by an explicit owner exception; this is not a license declaration.

## CycloneDX SBOM and license inventory

Pinned generator identity:

```text
cyclonedx-gomod v1.10.0
module sum h1:9Vy3zcC+lJLgcR4xYQvwPGU6L2Rij/Ld47lyucYjVI0=
observed task binary sha256 0fd48276ea79a679660005f0a5550ce83fb34bea439b444099c5d3aab6d8cfbb
```

After the immutable Notification module was pinned, the generator ran twice
with `GOPROXY=off`, test dependencies, license collection, license assertions,
CycloneDX 1.6 JSON, and deterministic `-noserial -notimestamp` output. Both runs
produced byte-identical output:

```text
9e35d90e8f7a1abe4da5d9ec79d7811afa6c0f3b2587df2d0ded5b735e94cbf0  bom.cdx.json
```

The SBOM contains 95 dependency components. Ninety-four have at least one
detected license. The unique detected identifiers are:

```text
0BSD
Apache-2.0
BSD-2-Clause
BSD-3-Clause
BSD-Source-Code
CC-BY-SA-4.0
MIT
MIT-0
```

The generator warns that the private root IAM module and the first-party
Notification API module have no detected license. The exact Notification
contract commit contains no `LICENSE`, `COPYING`, or `NOTICE` path. The user
explicitly accepted the first-party proprietary/no-license exception for this
DP2-06 integration. No license is inferred, selected, or reported as present.

## Vulnerability scan

After the final cross-store decision and test correction, fixed
`govulncheck v1.7.0` (binary SHA-256
`93670af3221b64b9c08c9fb2c9cfb18e35a5f1c7b51808a7ca08c9719580cb83`)
scanned 12 root packages, 51 modules, the integration build tag, and the Go
standard library. Symbol and package results reported zero vulnerabilities and
the final result was:

```text
Your code is affected by 0 vulnerabilities.
```

The module inventory still includes `GO-2026-5932` for the unmaintained
`golang.org/x/crypto/openpgp` package with no fixed version. No scanned IAM
package imports or calls it; the advisory is recorded as an unreachable module
finding, not suppressed and not misreported as affected code.

## Sensitive-value scan

Fixed Gitleaks `v8.30.1` (binary SHA-256
`301bf2649b8d93f0db33df6bfcb0aeb9b03783a13a3bcba34c8fffe42aed6a3b`)
first scanned the repository directory literally. That invocation is not a
valid current-tree result: it included `.git` plus a prior ticket's ignored
`.work` module/build cache, scanned 345.52 MB, and emitted 800 dependency/cache
heuristics. It is recorded as a failed invocation and is not counted as a
security failure or a pass.

The corrected scope was materialized from exactly `git ls-files --cached
--others --exclude-standard`, so it includes every tracked file at its current
working-tree contents and every non-ignored untracked DP2-06 file, but excludes
Git object storage and ignored build/module caches. The final index snapshot
scan covered 2.62 MB and reported eight `generic-api-key` heuristics. All eight
were inspected without
exposing their candidate values:

- five are unchanged, inherited checksum constants in two baseline evidence
  verifiers and `internal/data/operation_registry.go`;
- one is an unchanged public UUID test fixture in
  `internal/data/access_token_jwx_test.go` whose line moved because DP2-06 added
  tests above it;
- one is the generated Atlas migration checksum; and
- one is an immutable public contract/artifact digest in
  `tests/contracts/contract_pins.json`.

None is a credential, bearer token, password, private key, or deployable
Secret. A separate high-confidence scan of the same exact snapshot found no
private-key block, AWS access key, GitHub token, or Slack token pattern.
Production logging inspection found only the worker's stable message and
boolean retry class; its test proves an injected remote error containing a
test-only action token is not logged. The outbox schema and queries contain no
raw-token or action-token column. The classified current-tree Secret assessment
is therefore `pass`; the scanner result itself is not misreported as zero
heuristics.

A separate full-history scan reported nine `generic-api-key` heuristic
findings across five commits. All five commits are ancestors of the DP2-06
starting commit `0f9cb1c73bef12ee7183016ae012c1a52b008de0`; none was introduced
by this ticket. The findings are retained as inherited repository-history
debt, not counted as a clean-history pass and not used to weaken the current
tree result.
