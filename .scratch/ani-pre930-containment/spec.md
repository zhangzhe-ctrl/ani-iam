# ANI pre-9/30 IAM containment

Status: accepted by user instruction on 2026-09-08

## Decision

ANI is rapidly iterating toward its 2026-09-30 delivery. Until a post-freeze release commit is explicitly accepted as the IAM replacement baseline, Direct P2 must not change ANI's active public contract, SDKs, Gateway runtime composition, deployment configuration, or required gates.

This effort performs one corrective ANI change only: surgically remove the premature Direct P2 integration merged by PR #145 while preserving every later ANI feature. It does not redesign ani-iam, implement compatibility code, deploy, cut traffic, invalidate credentials, or delete the old Auth system.

## Fixed source facts

- ANI repository: `/home/chabking/workspace/ANI`
- Accepted recovery base: `caa2a5e72fad98215a5ea26696e453e5c2ef6523`
- Base tree: `839d673535f828f446d3fb8129b5874da24df00d`
- PR #145 merge: `50f7b422707c2ab78462bd9bb8186bae018a14fe`
- PR #145 parent: `e895af8cdfd5431804b64e1f571b3c6803278cc5`
- Later commits that must be preserved: `9bfedfd`, `98b881d`, `caa2a5e`

Dynamic `main`, another checkout, or a later fetch cannot silently replace this base. Any base drift before publication is a human checkpoint.

## Required result

1. Restore the pre-#145 Auth/API Key/Refresh/Logout public semantics while retaining later Tenant, VM, and KB additions.
2. Remove Target IAM clients, middleware, routers, generated registries, Proto copies, deployment flags, and target-only artifacts from active ANI.
3. Regenerate current Core SDKs, static API documentation, and Gateway policies from the restored current `v1.yaml`.
4. Make the existing Core v1 compatibility and current Gateway authorization gates pass without refreshing the compatibility baseline or inventing future IAM annotations for new Tenant operations.
5. Add both gates to GitHub Actions and verify them on the exact PR head.
6. Keep all Direct P2 candidate implementation in ani-iam or isolated, unpublished candidate worktrees until the post-9/30 freeze checkpoint.

## Safety boundary

- Use a fresh worktree from the exact accepted base.
- Do not modify the dirty `/home/chabking/workspace/ANI` checkout or existing Direct P2 worktrees.
- Do not mechanically overwrite shared files from the PR #145 parent; preserve later changes by semantic three-way resolution and regeneration.
- Do not run local repository test gates; per user instruction, GitHub Actions is the verification authority for this corrective PR. Local generation and `git diff --check` are permitted implementation checks.
- No deployment, cluster access, merge, release, credential action, data change, or deletion of the deployed old Auth system.

## Publication

One conventional commit may be pushed to a `codex/` branch and submitted as a PR to `main`. Do not merge it. Required Actions must be checked on the exact pushed SHA.
