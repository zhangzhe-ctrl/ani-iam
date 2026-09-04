# DP2-04 toolchain identity

Result: `pass`

## Fixed tools and dependencies

- Go: `go1.26.7-X:nodwarf5 linux/amd64`; module language version is `go 1.26.0`. It began at `go 1.25.7` and moved only because the govulncheck-required `golang.org/x/crypto@v0.56.0` fix declares Go `1.26.0` as its minimum; the exact RED and fixed scan are in `vulnerabilities.md`.
- Atlas Community: `v1.3.0`; official Linux AMD64 asset `https://atlasbinaries.com/atlas/atlas-community-linux-amd64-v1.3.0`; official and observed binary SHA-256 `10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b`.
- Atlas source tag: `v1.3.0`, commit `9a6bc601212130aaaefcbc8dd36c710baf9716ff`; the old single-module `go install ariga.io/atlas/cmd/atlas@v1.3.0` entry no longer exists because the CLI is a nested module. A source build was attempted from this exact tag and failed only at the host quota during linking. The official Community binary above is used instead.
- sqlc: `v1.31.1`; official Linux AMD64 archive SHA-256 `497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354`; extracted binary SHA-256 `0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f`.
- pgx/v5: `v5.9.2`.
- testcontainers-go and PostgreSQL module: `v0.43.0`.
- google/uuid: `v1.6.0`.
- Supply-chain fixes: `golang.org/x/crypto@v0.56.0` and `github.com/moby/go-archive@v0.3.0`.
- PostgreSQL: local image `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`, image ID prefix `5660c2cbfea5`.

## Fixed commands

```text
ATLAS_NO_UPDATE_NOTIFIER=1 /tmp/ani-direct-p2-01-05/dp2-04/bin/atlas version
/tmp/ani-direct-p2-01-05/dp2-04/bin/sqlc version
sha256sum /tmp/ani-direct-p2-01-05/dp2-04/bin/atlas /tmp/ani-direct-p2-01-05/dp2-04/bin/sqlc
```

Observed output:

```text
atlas community version v1.3.0
v1.31.1
10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b  atlas
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f  sqlc
```

The binaries live only in a Goal-owned temporary tool directory. Reproduction does not depend on that directory surviving: the immutable upstream versions, URLs, official hashes, extracted hash, and commands are recorded here.
