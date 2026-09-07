# DP2-06 final verification

## Current result

`pass` for DP2-06 closure at implementation commit
`74d7e441435d35dc66a733c8bf4a93129b26de3f`. The independent final Standards
review and ticket Spec review are both `pass` with no actionable finding; all
implementation, real-dependency, generation, supply-chain, and exact
staged-diff gates below pass. The ticket is `resolved`. No push, deployment, or
cluster action occurred.

## Immutable inputs

- IAM base and current pre-commit HEAD:
  `0f9cb1c73bef12ee7183016ae012c1a52b008de0` on
  `codex/direct-p2-06-14`.
- Notification public contract commit:
  `0e3f0a2b47fcc1fa96fa926cae2b9ab55bd25d84`, Go module
  `v0.0.0-20260907002920-0e3f0a2b47fc`, descriptor SHA-256
  `7be0a2fa062229717a311af952fc8b3bb7f58c1ef21cde2de741bcbbb4dfc195`.
- Notification local L3 runtime commit:
  `a477a38280c8626b0fdf6664e7afb049d22c2a58`; the integration test requires
  this exact HEAD and an empty tracked/untracked porcelain result before build.
- PostgreSQL:
  `postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`.
- Redis:
  `redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf`.

## Behavioral and real-dependency gates

| Gate | Result | Evidence |
| --- | --- | --- |
| `go test ./... -count=1` | `pass` | Every package passed or had no tests. |
| `go test -tags=integration -race ./... -count=1` | `pass` | Every package passed on the exact final code tree; `tests/integration` passed in `110.650s` with fixed Atlas, Notification, PostgreSQL, and Redis inputs. |
| Corrected target vertical slice | `pass` | Fresh real PostgreSQL/Redis run passed in `6.662s`; it asserts the increasing account cooldown before the later successful login. |
| Exact-clean two-process Notification mTLS | `pass` | Focused rerun passed in `1.268s`; exact IAM certificate identity reached Notification's authenticated boundary. |
| Container cleanup audit | `pass` | No matching PostgreSQL or Redis container remained after the final suite. |
| `go vet ./...` | `pass` | No output. |
| `go mod tidy -diff` | `pass` | No output. |
| `go mod verify` | `pass` | `all modules verified`. |
| Contract descriptor/fixture/pin suite | `pass` | All strict round-trip, service inventory, ErrorInfo, import-boundary, and immutable-pin tests passed. |
| `gofmt -l` on tracked plus non-ignored untracked Go | `pass` | No output after formatting one test-table entry. |
| `git diff --check` | `pass` | No output. |

The first post-decision complete race invocation failed at
`TestPostgresTargetLoginAndAuthorization`: the old test attempted a correct
password immediately after recording an invalid password and therefore hit the
newly required account cooldown. A fresh isolated environment reproduced the
same exact result. The test was corrected to assert the cooldown and wait its
bounded integration duration; no production behavior changed. The isolated
and complete commands then passed.

The final Spec review subsequently found that a durable PostgreSQL lock with an
empty Redis state returned a uniform first invalid-credential result but did
not recreate account/IP cooldown state. The unit regression first failed with
zero Redis failure calls, then passed after the locked path recorded the same
failure state. Standards review then found that this first correction omitted
the required authentication-failure Audit. A second unit regression first
failed with a nil Audit mutation. The final path now performs dummy Argon,
commits an anonymous principal-boundary redacted failure Audit through the
existing UOW without changing or extending the durable lock, and only then
records Redis account/IP state. Audit failure returns dependency failure before
Redis mutation.

The real empty-Redis plus durable-PG-lock test proves one redacted Audit, no
change to `failed_attempts`, `locked_until`, or credential `version`, a uniform
first invalid result, immediate account cooldown, and shared-IP cooldown for an
unknown account. It passed in `3.148s`. The complete exact-final-tree race gate
was then rerun and passed in `110.650s`, which is the only full result used for
closure.

## Generation and migration gates

| Generator | Fixed identity | Result |
| --- | --- | --- |
| sqlc | v1.31.1, binary SHA-256 `0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f` | `pass`; fresh generated directory diff `0`. |
| Atlas Community | v1.3.0, binary SHA-256 `10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b` | `pass`; validate/hash succeeded and `atlas.sum` diff `0`. |
| Buf API | v1.72.0, binary SHA-256 `8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af` | `pass`; lint, local Go generation, and standard FileDescriptorSet reproduction produced directory diff `0`. |
| Config Go plugin | protoc-gen-go v1.36.11, binary SHA-256 `f8682681cbeb0bbf9e9570ca9d90fdd9783d8170a7e995af122356fe5a9f3cb3` | `pass` for byte-identical local generation. |

Key outputs:

```text
authentication_service.proto  0d56d271b5961ac7eb0255a34e3eb3ef01faeaa3fc1f784519b467b7d73bd1a3
authentication_service.pb.go  9875418a17d6d538f5f288b29ecafb3f85e5d07f1ab602bdae7357946549cf0e
iam_descriptor.pb              66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a
conf.proto                     fccebc5b2419789d844ca7ffa987acabffd44f7dc16e78fcd9902d65cd4f176e
conf.pb.go                     e986665982bb9acfaa4c6ee229b16db38bb44995eacc11f816cc6c82686674d4
atlas.sum                      26b66354bc970b1a975e29ce8ce50339dc07fd84901e2156016be867245d6dc7
```

The first API descriptor attempt used Buf's image output instead of the
repository's standard source-info-stripped FileDescriptorSet and correctly
failed the binary diff. The corrected documented form
`--as-file-descriptor-set --exclude-source-info` reproduced the exact file.

The configured remote config plugin invocation was rejected by the execution
safety policy because it could send internal config source to an external
service. The exact cached v1.36.11 binary reproduced the file locally. Remote
plugin transport is `not_verified`; generated-content reproducibility is
`pass`.

## Supply-chain and Secret gates

- Fixed `govulncheck v1.7.0` reports zero affected symbols/packages across 12
  root packages and 51 modules with the integration tag. Module-only,
  unreachable `GO-2026-5932` remains recorded with no fixed version.
- Fixed CycloneDX generator v1.10.0 produced byte-identical 95-component SBOMs
  twice: SHA-256
  `9e35d90e8f7a1abe4da5d9ec79d7811afa6c0f3b2587df2d0ded5b735e94cbf0`.
  The Notification no-license state is accepted only as the explicit
  first-party proprietary/no-license exception.
- Gitleaks v8.30.1 found eight `generic-api-key` heuristics in the exact current
  tracked plus non-ignored-untracked snapshot. Inspection classified them as
  public/integrity digests, five inherited constants, and one unchanged public
  UUID test fixture whose line moved. No candidate is a Secret. A separate
  high-confidence private-key/AWS/GitHub/Slack pattern scan found no match.
- The history scan retains nine inherited heuristics across five commits; all
  five are ancestors of the ticket base. This is not reported as a clean
  history scan. See `supply-chain.md` for the full classification.
- No certificate, private-key, coverage, build, or temp output is present as a
  non-ignored untracked artifact. The intentional untracked
  `bom.cdx.json` is the byte-identical evidence artifact above.

## Accepted finite cross-store state

The Owner explicitly accepted `cross-store-decision.md` on `2026-09-07`:

- Redis pre-check failure returns dependency `503` before login mutation.
- A Redis counter failure after PostgreSQL failure/Audit may preserve that
  conservative durable state while returning `503`.
- A Redis account-reset failure after committed login may preserve the bounded
  old account bucket but does not turn the committed login into an error.
- None of these paths may create an unauthorized Session or credential success.

This is not strict PostgreSQL/Redis atomicity. Durable cross-store coordination,
repair observability, and login-result replay remain `not_verified` and are not
implemented under DP2-06.

## Intentionally not verified

- production Argon2 performance sizing/benchmark;
- strict PostgreSQL/Redis atomicity, durable coordination, repair observation,
  and login-result replay;
- remote config plugin transport (local exact-version content generation
  passes);
- cluster certificate issuance, Secret mounting, rotation/revocation;
- deployment, NetworkPolicy, cluster traffic, and cluster E2E.

These are not represented as DP2-06 passes and do not authorize a follow-on
implementation, push, deployment, or cluster action.

## Independent final reviews

### Standards

`pass`, no actionable finding. The reviewer checked the complete working-tree
diff from fixed base `0f9cb1c73bef12ee7183016ae012c1a52b008de0` plus non-ignored
untracked files against AGENTS/CLAUDE, plans, relevant ADRs, layering,
generation discipline, allowed paths, Secret/logging constraints, test quality,
and the full code-smell baseline. The review specifically revalidated the
durable-lock redacted Audit ordering, unchanged lock state, Audit failure before
Redis, exact-clean Notification runtime pin, raw sole-DNS-SAN identity check,
and the Owner-approved cross-store semantics.

### Spec

`pass`, no actionable finding. The reviewer checked every issue-06 acceptance
and test-plan item, the user-approved cross-store wording, Notification
contract/runtime identity, real PostgreSQL/Redis and two-process mTLS evidence,
and exact path extensions. The final durable-lock sequence was re-reviewed
after both corrections.

Both reviews retain exactly the `not_verified` boundaries listed above. Neither
review treats them as DP2-06 pass claims.

## Exact staged-diff audit

`pass` immediately before the feature commit:

- exactly 64 staged paths;
- every path is within the ticket's base allowlist or an explicitly recorded
  API, composition-root, config, or workload-identity extension;
- `git diff --name-status` has no unstaged path;
- `git ls-files --others --exclude-standard` is empty;
- `git diff --cached --check` has no output;
- the index snapshot contains no certificate/private-key, coverage, build, or
  temp artifact; the sole artifact-pattern match is the intentional
  deterministic `bom.cdx.json` evidence;
- Gitleaks over the exact index snapshot reports the same eight already
  classified non-Secret integrity/test heuristics; and
- the high-confidence private-key/AWS/GitHub/Slack credential scan has no
  match.

The issue file, complete evidence directory, generated outputs, source, tests,
migration, module files, and approved config/composition files are all in the
same index. No `git add -A`, reset, stash, amend, rebase, push, deployment, or
cluster action was used.

## Commit and closure

- Feature commit: `74d7e441435d35dc66a733c8bf4a93129b26de3f`
  (`feat(dp2-06): deliver password authentication`).
- `git status --short --branch` was clean immediately after that commit.
- This record and the issue resolution are a separate documentation-only
  closure commit because a commit cannot truthfully contain its own SHA.
- DP2-06 is resolved with the residual `not_verified` boundaries above intact.
  No push, deployment, live traffic, or cluster action was performed.
