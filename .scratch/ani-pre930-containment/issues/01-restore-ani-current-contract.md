# 01: Restore current ANI contract before 9/30

**What to build:** 在 ANI 最新已接受基线之上，外科式移除 PR #145 提前进入现行版本的 Direct P2 契约和 Gateway 集成，保留其后全部 ANI 功能，并提交一个由 GitHub Actions 验证的恢复 PR。

**Blocked by:** DP2-10 pause checkpoint accepted on 2026-09-08

**Status:** resolved

**Type:** bug

**Baseline:** initial ANI `caa2a5e72fad98215a5ea26696e453e5c2ef6523`, tree `839d673535f828f446d3fb8129b5874da24df00d`; accepted reconciliation baseline `804db51a5f93605f9bbd4ac407f0489ecb1d187c`, tree `28eb0508c93aab28e86e044a88ada9e19bf16830`; PR #145 merge `50f7b422707c2ab78462bd9bb8186bae018a14fe`, parent `e895af8cdfd5431804b64e1f571b3c6803278cc5`.

**Scope:** 恢复旧 Auth/API Key/Refresh/Logout v1 语义；移除 Target IAM runtime/composition、候选契约和生成物；保留 #145 后的 VM/Tenant/KB 功能；重生成 Core SDK、API docs、当前 Gateway policies；将 compatibility/authz 门禁接入 GitHub Actions；推送并创建 PR，不合并。

**Out of scope:** 修改 ani-iam 领域实现；为九个 Tenant operation 决定未来 Direct P2 语义；`/v2`、兼容层、双写或 runtime fallback；部署、切流、数据重建、Credential 失效、旧 Auth 部署删除、PR 合并。

**Allowed paths:** PR #145 相对其父提交的精确 78-file path set；`.github/workflows/ci.yml`；`repo/development-records/ANI-IAM-PRE930-CONTAINMENT.md`；`repo/development-records/README.md`；`repo/CURRENT-SPRINT.md`；`ANI-06-开发计划.md`。共享文件只允许移除 #145 语义并保留后续提交语义；生成物必须由恢复后的 source contract/generator 产生。

**Forbidden paths:** #145 path set 之外的代码、其他 service/frontends、生产或共享环境、GitHub branch protection、compatibility baseline 内容刷新、未来 IAM operation 注解、ani-iam DP2-10 implementation files。

**Evidence path:** `.scratch/ani-pre930-containment/evidence/01-restore-ani-current-contract/`

- [x] 工作分支从 exact accepted base 建立，其他 checkout 保持不变。
- [x] PR #145 的 Target IAM active surface 被移除，后续 VM/Tenant/KB/Observability 提交保留。
- [x] Core compatibility 与 current Gateway authz gate 在 PR exact SHA 的 GitHub Actions 中通过。
- [x] GitHub Actions required aggregate 通过。
- [x] staged/committed path audit 只包含 Allowed paths 与接受基线自身路径，无 secret、无部署、无切流。
- [x] PR 已创建但未合并，并记录 head SHA、URL、Actions run ID 和结果。

**Verification:** 不运行本地 test gate；执行本地生成、`git diff --check` 和 path/secret audit；完整测试与两个恢复门禁仅由 GitHub Actions 在 exact pushed SHA 上验证。

**Stop conditions:** 远端 main 漂移；必须回退 #145 后功能；恢复后契约仍需新业务决策；需要修改 compatibility baseline、未来 IAM 语义、未授权路径或部署状态；Actions 无法在 exact SHA 验证。

**Recovery:** PR 不合并；删除远端候选分支需另行确认。现有 ANI checkout、Direct P2 worktrees 和 DP2-10 worktree保持不动。

## Comments

- 2026-09-08：恢复提交 `fd4ede316f3734300381884908ebaa8bc8aaa925` 已推送并创建 ANI PR #152；Actions run `34202658984` 在原基线 `caa2a5e72fad98215a5ea26696e453e5c2ef6523` 上全部通过。CI 运行期间 ANI main 前进到 `804db51a5f93605f9bbd4ac407f0489ecb1d187c`，PR 随后变为 `CONFLICTING`。按 baseline-drift stop condition 转为 `ready-for-human`；未经人工接受新基线不得 merge/rebase 或继续改动。
- 2026-09-08：用户确认 `804db51a5f93605f9bbd4ac407f0489ecb1d187c` 是其 rebase 后的预期 main，并要求修复 PR 冲突。该精确 SHA 作为新的已接受 reconciliation baseline，本事项重新进入 `claimed`；必须保留其 observability API/handlers，只移除 #145 IAM 语义。
- 2026-09-08：以非改写历史的 merge commit `19cc06832e2dd4d1b56fe448f31c21d77055e24d` 完成 reconciliation，父提交为恢复候选 `fd4ede316f3734300381884908ebaa8bc8aaa925` 与已接受 main `804db51a5f93605f9bbd4ac407f0489ecb1d187c`。4 个 Observability 与 9 个 Tenant operation 仅映射到当前 Gateway authz schema；Direct P2 metadata 未被接受回现行 ANI。Actions run `34204529598` 在 exact head 全部通过，PR #152 为 `MERGEABLE`、`REVIEW_REQUIRED`、未合并；事项关闭为 `resolved`。
