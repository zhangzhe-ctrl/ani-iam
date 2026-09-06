# DP2-05 final Go/No-Go A evidence

Result: `pass`

## Human conclusion and local commits

- Human conclusion: Go/No-Go A `Go`, explicitly accepted on 2026-09-06.
- IAM start: `e7ac8556196b2d0884a5686e18fe2473e68544c0`.
- IAM implementation: `a7620cef9c4374e665ce2aae2015af2e514fbd28`.
- ANI start: `573d3735934f74f9f1eb78818cddefefd9f575eb`.
- ANI Gateway implementation: `f09a436c6edbd752271d1e4502bbdfd1f1b9e690`.

The accepted result proves that the target minimum vertical architecture can
close in an isolated environment. It does not authorize or prove deployment,
traffic cutover, production readiness, DP2-06, BOSS/Platform caller E2E or the
unified 24-hour Idempotency Ledger.

## Commit-time gates

- IAM fixed Buf/sqlc/Atlas regeneration: `pass`, with no generated drift.
- IAM unit and vet: `pass`.
- IAM serial race scope: `pass`; an earlier compile attempt returned `fail`
  only because the Goal cache exhausted the local disk quota and ran no test
  assertion.
- IAM full real PostgreSQL/Redis/Gateway integration: `pass` in 67.384s;
  every task-owned container was terminated.
- Gateway generation from the immutable IAM descriptor: `pass`, with
  byte-identical generated hashes.
- Gateway unit, vet and serial race scope: `pass`; the first race compile
  attempt had the same retained disk-quota `fail`, then the clean-cache rerun
  passed.
- Replacement operation registry: `pass` (20/20).
- Replacement OpenAPI breaking gate: `pass` (4/4).
- ANI architecture, document-entrypoint, full `make test-go` and Python gates:
  `pass`.
- ANI aggregate `make test`: `fail` at the pre-existing legacy Auth assertions
  for `logout` and `revokeAPIKey`; the accepted target contract intentionally
  retains `logoutSession` and `revokeIAMAPIKey` and its replacement gates pass.
- Both module-integrity checks: `pass`.

## Post-commit final regression

The implementation commits above were then tested again without changing their
trees:

- IAM `go test -p=1 ./... -count=1` and `go vet ./...`: `pass`.
- IAM full real-dependency integration: `pass` in 55.268s, including the real
  IAM/Gateway process E2E; every PostgreSQL and Redis container was terminated.
- IAM `go mod verify`: `pass`.
- Gateway `go test -p=1 ./... -count=1` and `go vet ./...`: `pass`.
- Replacement registry and breaking tests: `pass` (20/20 and 4/4).
- Gateway `go mod verify`: `pass`.

The commit-time serial race scopes had already run against the byte-identical
implementation trees and remain `pass`. No source or generated file changed
between those race runs and the implementation commits.

## Fixed runtime identities

- PostgreSQL: `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`.
- Redis: `redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf`.
- Runtime business role: non-owner, non-superuser, no BYPASSRLS; no RLS is enabled.

## Safety and recovery

No legacy fallback, old `auth.v1.AuthService` registration, default allow,
RLS or superuser business query was introduced. No remote Artifact was
published; nothing was pushed, deployed or cut over; no shared data was
rebuilt, no Credential invalidated, no old deployment asset deleted and
DP2-06 was not started.

Recovery uses new local revert commits for the exact IAM and ANI implementation
SHAs. It never uses reset, stash, amend, rebase, force or overwrites the
external ANI checkout.
