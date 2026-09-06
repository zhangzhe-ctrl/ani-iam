# DP2-05 Standards and Spec review

Result: `pass`

The final DP2-05 implementation was reviewed independently against the
repository Standards and against the ticket Spec. Review agents performed
read-only inspection; all repairs were made by the primary Goal executor and
were followed by focused and complete regressions.

## Standards

Initial result: four findings; worst severity High.

| Finding | Initial severity | Resolution | Result |
| --- | --- | --- | --- |
| Target mode could fall through to the legacy Gateway chain | High | `IAM_TARGET_MODE` is explicit; `disabled` is the only opt-out, while missing, unknown or incomplete `dp2_05` configuration fails startup | `pass` |
| Stable `google.rpc.ErrorInfo` coverage was incomplete | High | Invalid input, dependency, denial and state errors now map to stable code/reason/domain/metadata and are covered by service/use-case tests | `pass` |
| Authentication rate-limit metadata was discarded | Medium | `AuthenticationRateLimitError` metadata is preserved through the gRPC mapping | `pass` |
| A generated-runtime residue directory remained in the worktree | Low | The Goal-owned residue was removed; no runtime/build residue is in the final inventory | `pass` |

The final composition root remains `cmd/server`, uses explicit constructors,
and does not introduce Wire. `service` stays a DTO/domain mapper, `biz` remains
framework/Proto/driver independent, `data` owns driver and persistence shapes,
and `server` contains only transport/middleware/registration concerns.

## Spec

Initial result: four findings from the independent review, plus one P1
finding discovered by the final staged-diff inspection; worst severity P1.

| Finding | Initial severity | Resolution | Result |
| --- | --- | --- | --- |
| Default DSN named `iam_runtime`, not the required restricted `ani_iam_runtime` identity | P1 | Typed configuration validation now requires the DSN username `ani_iam_runtime`; integration tests execute business queries only through that non-owner/non-superuser/non-BYPASSRLS role | `pass` |
| Invalid input and Audit/UoW failures could lose stable ErrorInfo | P1 | Domain classification and service mapping now return stable invalid-argument/dependency ErrorInfo and focused tests assert the details | `pass` |
| BOSS/Platform appeared covered without Platform persistence or caller E2E | P1 | Evidence and ANI closure documents explicitly classify BOSS/Platform Password Login and BOSS caller E2E as `not_verified`; Console/Tenant is the only claimed tracer | `pass` |
| PasswordLogin accepted a BOSS audience with a Tenant boundary and misclassified a valid BOSS/Platform request | P1 | The service now rejects the invalid audience/boundary pair before the use case and fails the contract-valid but unimplemented Platform slice closed as `UNAVAILABLE` with `IAM_UNAVAILABLE` / `platform_authentication`; focused RED/GREEN tests prove neither request reaches the Tenant use case | `pass` |
| A Goal-owned runtime residue was present | P2 | The residue was removed and artifact audits exclude it | `pass` |

The functional Spec blockers are resolved. BOSS/Platform remains accurately
`not_verified`, not silently promoted to `pass`; its target request fails
closed with a stable dependency error until the Platform persistence and
caller slice exists. The final real PostgreSQL,
Redis and Gateway process suite, both full module regressions, both serial race
scopes, static checks, generated drift gates and immutable artifact checks all
ran after the repairs and are recorded in `green.md`.
