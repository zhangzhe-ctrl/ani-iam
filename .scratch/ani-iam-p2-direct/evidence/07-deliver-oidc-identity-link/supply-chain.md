# DP2-07 supply-chain and Secret evidence

Status: affected-code vulnerability scan `pass`; deterministic SBOM `pass`;
current-tree Secret assessment `pass` after explicit heuristic classification.

## Vulnerability scan

Fixed `govulncheck v1.7.0`, built with Go `go1.26.7-X:nodwarf5`, has observed
binary SHA-256:

```text
6a681043b43993e15ed78bab2d244ca5d8338a8f2575fba606183ce9fc01aeb9
```

The integration-tag final-tree scan reported:

```text
No vulnerabilities found.
Your code is affected by 0 vulnerabilities.
```

It also reported one vulnerability in a required module that no imported IAM
package calls: `GO-2026-5932` in the unmaintained
`golang.org/x/crypto/openpgp` package, with no fixed version. It remains visible
as unreachable module debt and is not misreported as an affected-code result.

## Deterministic CycloneDX SBOM and licenses

Fixed `cyclonedx-gomod v1.10.0` has module sum
`h1:9Vy3zcC+lJLgcR4xYQvwPGU6L2Rij/Ld47lyucYjVI0=` and observed binary SHA-256:

```text
0fd48276ea79a679660005f0a5550ce83fb34bea439b444099c5d3aab6d8cfbb
```

Two offline runs included test dependencies, detected/asserted licenses and
CycloneDX 1.6 JSON with serial and timestamp omitted. The outputs were
byte-identical:

```text
0d12640f1867273200c6778ecb046df3d1e05bacbbfacc310aed53e290c11dc9
```

The attached `bom.cdx.json` contains 98 dependency components. Unique detected
license identifiers are `0BSD`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause`,
`BSD-Source-Code`, `CC-BY-SA-4.0`, `MIT`, and `MIT-0`.

The root private IAM module and first-party Notification API module have no
detected license. Notification is covered by the Owner's explicit
first-party proprietary/no-license exception; no license is invented or
reported as present.

New direct dependencies were inspected from the immutable module cache:

- `github.com/coreos/go-oidc/v3@v3.20.0`: Apache-2.0; LICENSE SHA-256
  `cb5e8e7e5f4a3988e1063c142c60dc2df75605f4c46515e776e3aca6df976e14`;
  NOTICE present.
- `golang.org/x/oauth2@v0.36.0`: BSD-3-Clause; LICENSE SHA-256
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`.

## Sensitive-value assessment

Fixed Gitleaks binary SHA-256:

```text
301bf2649b8d93f0db33df6bfcb0aeb9b03783a13a3bcba34c8fffe42aed6a3b
```

The corrected current-tree scope was materialized from exactly
`git ls-files --cached --others --exclude-standard`. The exact final snapshot,
including this ticket's evidence and SBOM, covered 2.96 MB and
reported eight `generic-api-key` heuristics:

- four inherited verifier constants in two historical evidence scripts;
- one inherited public UUID fixture in `access_token_jwx_test.go`;
- one inherited operation-registry integrity digest;
- the generated Atlas checksum; and
- the frozen public contract digest.

All are public test identities or integrity/checksum material, not deployable
credentials. A separate high-confidence scan found no private-key block, AWS
access key, GitHub token or Slack token pattern.

The full-history scan covered 17 commits and reported eleven generic heuristics
across six commits. Each commit is an ancestor of the DP2-07 starting baseline;
none belongs to this ticket. This is inherited history debt and not a clean
history claim.

Production OIDC code has no logger call. Stable errors omit raw authorization
code, state, nonce, verifier, ID token, Access Token, Refresh Token and upstream
response bodies. Redis keys contain only SHA-256 digests; PostgreSQL persists
Identity metadata, Session authn methods and redacted Audit, never those OIDC
Secrets. Runtime configuration contains only the mounted client-secret file
path; test-only client secret and password literals remain limited to tests.
