# DP2-08 verification record

Status: `pass` for the approved DP2-08 backend and handoff boundary.

## Current passes

| Gate | Result | Evidence |
| --- | --- | --- |
| Focused OIDC/BOSS correction tests | `pass` | `go test ./internal/biz` covering OIDC continuity state plus active/consumed BOSS Tenant Refresh and BOSS SwitchTenant fail-closed cases |
| Biz and service regression | `pass` | `go test ./internal/biz ./internal/service -count=1` |
| Real Session continuity dependencies | `pass` | pinned Atlas plus real isolated PostgreSQL/Redis integration session suite |
| Complete ordinary regression | `pass` | `go test ./... -count=1`, rerun with loopback access after the restricted sandbox correctly rejected local listeners |
| Complete race regression | `pass` | `go test -race ./... -count=1` after clearing only the Goal-owned Go build cache |
| Complete integration race regression | `pass` | final `DP2_ATLAS_BIN=/tmp/ani-iam-dp2-08-atlas DP2_NOTIFICATION_DIR=/tmp/ani-notification-dp2-08-regression GOTMPDIR=/tmp/ani-iam-dp2-08-go-tmp GOCACHE=/tmp/ani-iam-dp2-08-gocache go test -tags=integration -race ./... -count=1`; all packages passed and `tests/integration` completed in 104.497s |
| Static checks | `pass` | `go vet ./...` and `git diff --check` |
| Module integrity/drift | `pass` | `go mod verify` reports `all modules verified`; `go mod tidy -diff` emits no diff |
| sqlc reproducibility | `pass` | sqlc `v1.31.1`, binary SHA-256 `0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f`; a fresh temporary generation had zero diff from `internal/data/sqlcgen` |
| Atlas migration validation/hash | `pass` | Atlas `v1.3.0`, binary SHA-256 `10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b`; validate passed and a repeated hash left `atlas.sum` byte-identical |
| Vulnerability scan | `pass` for affected code | ordinary and integration-tag `govulncheck` report zero affected symbols/packages; unreachable module advisory `GO-2026-5932` is recorded in `supply-chain.md` |
| Deterministic SBOM | `pass` | two fresh CycloneDX 1.6 generations match `bom.cdx.json`, SHA-256 `a4c609b0307816cfddbe4aaecaad3d7215f99d7d2121f5458d4a03e4932a1c7f` |
| Current tree and history secret scan | `pass` after manual classification | 8 current / 11 historical `generic-api-key` findings are inherited checksum or test-fixture values; no high-confidence secret rule and no raw Refresh persistence |
| Independent Standards review | `pass` | zero documented-standard blockers; judgement-only broad-port/row-clump smells recorded, and the unused audit-target constant was removed |
| Independent Ticket Spec review | `pass` | zero missing/partial requirements, scope creep findings or incorrect implementations |

The first complete race attempt reached `disk quota exceeded` only while the
linker mapped `cmd/server.test`; every other package passed. Disk inspection
showed the Goal-owned `/tmp/ani-iam-dp2-08-gocache` occupied 1.8 GiB on a 3.9
GiB tmpfs. `go clean -cache` was run only against that task cache, restoring 2.6
GiB free, and the exact race command then passed every package. The first result
is environmental evidence, not a source failure or a pass.

The final post-review ordinary and race regressions, `go vet`, formatting,
`go mod verify`, `go mod tidy -diff`, and `git diff --check` all exited `0`.
The pinned sqlc fresh-directory generation had zero diff. Pinned Atlas validate
and hash exited `0`, and `migrations/atlas.sum` remained byte-identical at
SHA-256 `aa13b7cdac3a3976b5b3ddce7603002da8f12c439bf1871e485fbc874ade5b47`.

The first final SBOM attempt inherited a read-only global `GOTMPDIR` and the
fixed generator panicked after its child `go` commands returned no packages; it
produced no artifact. Re-running the exact fixed tool with the Goal-owned
`GOTMPDIR`/`GOCACHE` exited `0` and matched the checked artifact byte-for-byte.
This is environmental evidence, not a source failure or a pass by omission.

## Final index gate

The exact staged snapshot contained all 29 ticket files and no path outside the
DP2-08 allowlist. There were no remaining unstaged or non-ignored untracked
files; `git diff --cached --check` passed. The staged snapshot Secret scan
covered 336 files / 3.32 MB of scanner input and returned the same eight
manually classified checksum/fixture findings, with no new or high-confidence
secret. The audited index is the immutable payload used for the ticket-local
commit; commit identity is verified in the final handoff because a commit
cannot contain its own SHA.

## Required `not_verified`

Gateway Cookie/CSRF behavior, ANI operation registry, Console/BOSS source,
browser E2E, multi-tab coordination, UX, BOSS Platform Grant authentication,
deployment, cluster certificates, production origins/CORS, push, PR, deployment
and cutover are not executed in DP2-08 and remain `not_verified`.
