# DP2-04 vulnerability scan

Scanner: `golang.org/x/vuln/cmd/govulncheck@v1.7.0`

Scanner binary SHA-256: `6a681043b43993e15ed78bab2d244ca5d8338a8f2575fba606183ce9fc01aeb9`

Database observed by the scanner: `https://vuln.go.dev`, updated `2026-09-02 19:12:04 +0000 UTC`.

## Initial gate

Result: `fail`

```text
govulncheck -test -tags integration -show verbose ./...
```

The scan matched 27 root packages and 74 modules plus the standard library. It found three reachable vulnerabilities:

| ID | Module/version | Reachable path | Fixed version |
| --- | --- | --- | --- |
| `GO-2026-6355` | `golang.org/x/crypto@v0.54.0` | Testcontainers PostgreSQL setup reaches `ssh.NewClientConn` | `v0.56.0` |
| `GO-2026-6354` | `golang.org/x/crypto@v0.54.0` | Testcontainers PostgreSQL setup reaches `ssh.NewClientConn` | `v0.56.0` |
| `GO-2026-6253` | `github.com/moby/go-archive@v0.2.0` | Testcontainers archive setup reaches vulnerable extraction code | `v0.3.0` |

The scan also reported `GO-2026-6303` at imported-package level and `GO-2026-5932` at required-module level for `x/crypto@v0.54.0`; neither was shown as reachable, but pinning `v0.56.0` removes the version carrying them where a fix exists. Final status is not `pass` until the fixed dependency graph is rescanned.

## Fixed dependency graph

- `golang.org/x/crypto` is explicitly pinned at `v0.56.0`.
- `github.com/moby/go-archive` is explicitly pinned at `v0.3.0`.
- Their minimum supported Go version requires the root module language version to move from `go 1.25.7` to `go 1.26.0`; the executed toolchain is `go1.26.7-X:nodwarf5`.

## Final gate

Result: `pass`

The exact initial command was rerun against the updated dependency graph. It again matched 27 root packages and scanned 74 modules plus the standard library. Final summary:

```text
=== Symbol Results ===
No vulnerabilities found.

=== Package Results ===
No other vulnerabilities found.

Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call these
vulnerabilities.
```

The remaining module-only advisory is `GO-2026-5932` for the unmaintained `golang.org/x/crypto/openpgp` package. No target or test package imports or reaches `openpgp`, no fixed version exists, and govulncheck exited successfully.
