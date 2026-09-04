# DP2-02 Standards and Spec review

Two independent read-only reviews examined the same ANI worktree changes: one against repository Standards and one against the DP2-02 Spec. No reviewer modified the worktree.

## Findings and disposition

| Axis | Finding | Result | Disposition |
| --- | --- | --- | --- |
| Spec | Handler/obligation/call-count assertions could be satisfied by a self-consistent manifest without an explicit decision kind | `pass` | OpenAPI is now the single per-operation source; every route has a unique explicit handler/owner; generated rows include `none`, `validate_principal` or `check_permission` plus exact call count; negative tests cover missing and duplicate declarations |
| Spec | 227 operations initially lacked explicit owner metadata | `pass` | all 295 operations now carry a valid `x-ani-owner` and `x-ani-handler` |
| Spec | 503 initially enumerated only IAM unavailable | `pass` | stable 503 reasons now include policy mismatch, unregistered operation, IAM unavailable and Tenant IAM not ready |
| Spec | the initial replacement manifest did not trace DP2-01 `D001-D028` | `pass` | all 28 IDs, immutable manifest commit/path/hash and 59 related old operations are pinned and generated |
| Spec | target four-language SDK output is generated from the target OpenAPI | `pass` | isolated generation and smoke are complete; the user accepted the exact six-file path expansion and the six worktree outputs are byte-identical to the fresh generation |
| Standards | breaking Core v1 changes violate the normal compatibility rule | `pass` | ADR-0001 and Direct P2 permit this exact pre-launch break, and the user accepted the exact breaking report before commit |
| Standards | policy declarations were duplicated between OpenAPI and a side manifest | `pass` | OpenAPI now solely owns operation policy; side manifest contains only shared error, trusted-context and obligation-handler contracts |
| Standards | generated Go exposed policy values as untyped strings | `pass` | generated maps use typed owners, classifications, resources, actions, scope, obligation, decision and stable-error wrappers |
| Runtime | target registry is not yet wired into the active Gateway chain | `not_verified` | intentionally outside DP2-02; DP2-05 proves the target Gateway one-decision path |
| Runtime | new logical handlers are not production implementations | `not_verified` | later implementation tickets own handlers; DP2-02 only freezes unique identities and prevents duplicate/missing registration metadata |

## Repository rule conflict

ANI classifies this as a Feature batch, so its mandatory documentation closure requires four paths outside the current ticket Allowed paths:

```text
repo/development-records/DP2-02-public-iam-operation-registry.md
repo/development-records/README.md
repo/CURRENT-SPRINT.md
ANI-06-开发计划.md
```

Result: `pass`. The user accepted this exact minimum expansion at the 2026-09-04 breaking checkpoint, and only these four documentation files were added or updated.

## Conclusion after human checkpoint

The target contract, exact breaking artifact, deterministic generators, generated registry, browser TypeScript schemas, four-language SDK output, replacement trace and mandatory Feature-batch documentation closure are `pass`. Runtime cutover and new handler implementation are `not_verified` by design. The user accepted the exact breaking diff and both explicit path expansions; local commit is authorized, while push, publication, deployment and cutover remain forbidden.
