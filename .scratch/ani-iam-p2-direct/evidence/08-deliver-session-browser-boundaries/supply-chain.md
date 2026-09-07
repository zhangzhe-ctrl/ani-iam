# DP2-08 supply-chain evidence

Status: `pass`; final working-tree and staged-index checks completed.

## Tool identity

| Tool | Version/source | Binary SHA-256 |
| --- | --- | --- |
| `govulncheck` | `v1.7.0` | `c2b5e14e12111458a3b87970c877390bfc577c15c4ebe68e5901fe29c45cdaf2` |
| `cyclonedx-gomod` | `v1.10.0` | `5bbc91a9b45f57c6f804ec112cece86251d03322fba3ad6e5538d34ab025fbce` |
| `gitleaks` | module pin `github.com/zricethezav/gitleaks/v8@v8.30.1`; the source-built CLI does not embed a release string | `d32f41a68bf5ecb0d9c2b10b74f9867e6f5dc7c29eaffd3db95a7aa7e82c7291` |

## Vulnerabilities

Both ordinary and `integration`-tag `govulncheck` scans report zero affected
symbols and zero affected imported packages. They report one module-only,
unreachable advisory: `GO-2026-5932` for the unmaintained
`golang.org/x/crypto/openpgp` package. The repository does not import or call that
package, and the advisory has no fixed version. This is recorded as an
unreachable transitive-module finding, not an affected-code pass-through.

## SBOM and licenses

`bom.cdx.json` is CycloneDX 1.6 JSON generated with test and standard-library
components, asserted detected licenses, no serial number and no timestamp. Two
fresh generations were byte-identical to each other and to the checked-in
artifact:

- SHA-256: `a4c609b0307816cfddbe4aaecaad3d7215f99d7d2121f5458d4a03e4932a1c7f`
- components: 99
- detected license identifiers: `0BSD`, `Apache-2.0`, `BSD-2-Clause`,
  `BSD-3-Clause`, `BSD-Source-Code`, `CC-BY-SA-4.0`, `MIT`, `MIT-0`

No license was detected for the first-party IAM root module or the fixed
first-party Notification module. No new dependency was introduced by DP2-08.
The user previously accepted the first-party proprietary/no-license exception;
this evidence does not broaden it to any third-party component.

## Secret scanning

A final snapshot built from every tracked file plus every non-ignored untracked
file contained 336 files / approximately 3.32 MB of scanner input. Gitleaks
reported eight
`generic-api-key` findings, all reviewed as non-secret checksum/fixture values:

- four inherited evidence-script checksum constants;
- the generated operation-registry digest;
- the Atlas migration checksum;
- the contract-pinning digest;
- a UUID-format Access Token test value.

The final pre-commit history scan covered 19 commits and reported eleven inherited
`generic-api-key` findings across six commits. They are the historical forms of
the same checksum/fixture categories. Neither scan found a high-confidence AWS,
GitHub, Slack or private-key rule, and no raw Refresh credential is persisted by
the DP2-08 schema or implementation. The final staged-index snapshot contained
336 files / 3.32 MB of scanner input and reproduced the same eight classified
findings with no new rule or high-confidence Secret.

The final fixed-tool no-drift reruns passed:

- ordinary and integration-tag `govulncheck` each reported zero affected
  symbols/packages and only the same unreachable module advisory;
- CycloneDX generation with test and standard-library components matched the
  checked artifact byte-for-byte at SHA-256
  `a4c609b0307816cfddbe4aaecaad3d7215f99d7d2121f5458d4a03e4932a1c7f`.

An initial CycloneDX attempt inherited the host's read-only `GOTMPDIR`, causing
child `go` commands to fail and the fixed v1.10.0 tool to panic without writing
an artifact. The exact generator then ran with the Goal-owned
`/tmp/ani-iam-dp2-08-go-tmp` and cache and produced the deterministic match; the
first attempt is environmental evidence, not a successful scan.
