# DP2-04 generator-first and migration evidence

Result: `pass`

## Fixed generators

| Generator | Immutable identity | Binary SHA-256 |
| --- | --- | --- |
| Atlas Community | `v1.3.0`, official Linux AMD64 artifact; source tag commit `9a6bc601212130aaaefcbc8dd36c710baf9716ff` | `10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b` |
| sqlc | `v1.31.1`, official Linux AMD64 archive SHA-256 `497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354` | `0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f` |

sqlc has no separate repository template in this task. Its complete generation source is the fixed configuration, migration directory and query file below. Generated Go files were never edited by hand.

## Actual commands

```text
/tmp/ani-direct-p2-01-05/dp2-04/bin/sqlc generate

ATLAS_NO_UPDATE_NOTIFIER=1 \
  /tmp/ani-direct-p2-01-05/dp2-04/bin/atlas migrate validate \
  --dir file://migrations

ATLAS_NO_UPDATE_NOTIFIER=1 \
  /tmp/ani-direct-p2-01-05/dp2-04/bin/atlas migrate hash \
  --dir file://migrations
```

The final clean-baseline command copied only `sqlc.yaml`, `migrations/**` and `internal/data/queries/**` into a Goal-owned temporary directory, ran the pinned sqlc there, and compared its fresh `internal/data/sqlcgen` recursively against the worktree. `diff -ru` exited `0` with no output.

The worktree generation command was then run again. Input and output hashes before and after were identical:

| Artifact | SHA-256 |
| --- | --- |
| `sqlc.yaml` | `486a757be88a0ce95c69915a0bdab6e8250a090904f810114c35989dae37509f` |
| `internal/data/queries/persistence.sql` | `9a98348b1dcebc9f88a90861e8e8f0325f23ce8c825cce69d726a63a7c53553e` |
| `migrations/202609040001_persistence_foundation.sql` | `0bb28594601bb666e045c5a957feb0dacf9de8c6840247a7bc15f9a298393483` |
| `migrations/atlas.sum` | `fc6889d0120a11c5514216c5c8d0381ae8fcf292586bceb660511cc1d45f17a9` |
| `internal/data/sqlcgen/db.go` | `c40f70c9870c43ddc88f743ec63627bc9af2d57fab48fdb0c055981d090b0cf8` |
| `internal/data/sqlcgen/models.go` | `3c71058e428f87fe843a6d0a354f9299bf942e95daf19e6d89ba41439b895a91` |
| `internal/data/sqlcgen/persistence.sql.go` | `a32dd3dc36803bf9738c9686d064c8bdb0409e72174b44baae8a3125a2e040bc` |
| `internal/data/sqlcgen/querier.go` | `cad8488f5c77cdf953ca802985a320a94296e73cfcc81130030becf755d24faa` |

## Baseline disposition

- Retain: all four official sqlc outputs exactly as generated; the narrow membership and audit methods are the DP2-04 persistence adapter boundary.
- Delete: none of the final pinned-generator baseline.
- Replace: none of the final pinned-generator baseline.
- Not applicable: Wire, Makefile, ORM and Proto/config generators; DP2-04 does not introduce or change their sources or artifacts.

## Migration verification

`atlas migrate validate --dir file://migrations` exited `0`. The same versioned migration was applied by the migration owner to two independently empty databases in every real PostgreSQL integration run; both reached the exact six-table target inventory. Runtime instances never invoke Atlas.
