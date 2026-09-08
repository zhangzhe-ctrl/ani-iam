# DP2-09 independent review evidence

## Fixed comparison

- IAM: 56ab0fcc7c6117f11e0231e8d37a127d0c2f0a57 to the current uncommitted worktree.
- ANI: 9bfedfd04c75533e01fa3d88419e7a3454f79404 to the dedicated current uncommitted worktree.
- Both reviewers inspected tracked and non-ignored untracked files. No ticket commits existed during review.

## Round 1 — Standards: fail

Hard findings:

1. Missing or stale Tenant lifecycle projection could collapse into a normal authorization denial, conflicting with the stable unavailable boundary.
2. generated_operation_policies.go had no checked-in generator, fixed command, input digest, or second-generation proof.

Judgement call:

- internal/data/tenant_authorization.go membershipRecord accepts a broad row-shaped parameter list. This is confined to the adapter; it is not a semantic or documented-standard violation and is not expanded in this ticket.

## Round 1 — Spec: fail

Blockers:

1. Missing or stale lifecycle projection could produce 403 instead of the required 503.
2. PostgreSQL did not constrain tenant_role_permissions to the generated catalog or Tenant scope.

The reviewer found no scope creep and confirmed the remaining Tenant isolation, last-admin, audit rollback, one-decision Gateway, obligation, real-process, and not_verified boundaries.

## Remediation

- Added real PostgreSQL RED tests for missing/stale lifecycle and catalog/scope constraints.
- Added an explicit lifecycle freshness lookup and preserved TENANT_LIFECYCLE_STALE through the existing service mapping.
- Added a deterministic registry generator and generated relational Permission Catalog migration with tenant-only CHECK and composite FK.
- Regenerated sqlc and Atlas checksum; repeated generation is byte-identical.
- Focused unit/vet and real PostgreSQL repair tests pass.

## Round 2 — Spec: pass

The Spec reviewer reported 0 blockers, 0 non-blocking findings, and 0 scope creep. Missing/stale lifecycle is now the stable unavailable boundary while fresh inactive lifecycle remains a denial. The generated relational catalog, Tenant-scope CHECK, composite foreign key, and PostgreSQL 23503/23514 negatives close the persistence finding.

## Round 2 — Standards: fail

The reviewer accepted the semantic repairs but found the generation procedure still wrote directly to the worktree. That process finding was remediated by generating the Go/SQL outputs and sqlc outputs in isolated temporary directories, comparing every output byte-for-byte, and only then copying validated outputs into approved paths.

## Round 3 — Standards: fail

The reviewer accepted the isolated deterministic generation and all prior code repairs. The sole remaining hard finding was the required ANI Feature-batch `make validate-services` gate being recorded as not run. The existing 12-argument data-adapter projection remained a non-blocking judgement call, not a documented layering or semantic violation.

## Round 4 — Standards: pass

The Standards reviewer found 0 hard findings after inspecting both complete diffs, untracked files, migrations/sqlc, generation chain, evidence, and approved paths. The complete candidate's isolated `make validate-services` passed without added drift; final IAM normal/race integrations passed in 274.654s and 243.728s. The data-adapter parameter list remains the same non-blocking local refactoring opportunity.

## Round 3 / 4 — Spec: fail then pass

The first final Spec check found one evidence-only blocker: the ANI batch record still described the pre-review 302.561-second run as final. The record was corrected to the review-repaired 274.654-second normal and 243.728-second race results and now records the isolated Services gate. The final Spec review is `pass`: 0 blockers, 0 non-blocking findings, 0 scope creep.
