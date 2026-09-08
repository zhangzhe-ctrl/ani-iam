# DP2-09 final path and publication audit

## Fixed identities

- IAM: `codex/direct-p2-06-14@56ab0fcc7c6117f11e0231e8d37a127d0c2f0a57` before the ticket commit.
- ANI dedicated worktree: `codex/direct-p2-06-14@9bfedfd04c75533e01fa3d88419e7a3454f79404` before the ticket commit.
- ANI resulting local ticket commit: `4ff73e09c16df706af2aacdff6d763ed4aeaa873`.
- Original ANI checkout remained at `50f7b422707c2ab78462bd9bb8186bae018a14fe`; its only status entry remained the pre-existing untracked `repo/frontends/`.

## Candidate paths

- IAM: 23 tracked modifications and 13 non-ignored untracked files. Every path is under the claimed issue/evidence directory, `internal/biz/**`, `internal/data/**`, `internal/service/**`, `migrations/**`, or `tests/**`, or is one of the four explicitly approved composition/workload-identity files.
- ANI: 16 tracked modifications and one non-ignored untracked batch record. Every path is an explicitly approved registry/contract/port/adapter/document path or a target authorization/router path under the approved Gateway scope.
- The approved `authentication_service_grpc.pb.go` generator output remained byte-identical and therefore has no diff.
- Exact candidate audits found 0 out-of-scope paths. `git diff --check` passed in both worktrees.

## Secret and external-state audit

- High-signal private-key, AWS access-key, GitHub token, Slack token, and Google API-key patterns were scanned in both tracked diffs and all non-ignored untracked candidate files; no match was found.
- No push, remote PR, deploy, cutover, Credential invalidation, data rebuild, production operation, or legacy-asset deletion occurred.
- Task-owned generation, Go work, baseline-audit, patch, and isolated ANI validation directories were removed after their hashes/results were captured. Source caches and both selected worktrees were preserved.

## Staging rule

Stage only the exact audited file lists; do not use `git add -A`. Re-run staged-name audit and `git diff --cached --check` before each local commit.
