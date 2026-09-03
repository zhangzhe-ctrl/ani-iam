# 00: 恢复干净 Kratos 实现基线

**What to build:** 在保留事项03/04及 Direct P2 重排审计历史的前提下，从 Direct P2 工作树移除未投入使用的旧 Auth compatibility 与旧 RLS/Redis 调查代码，使非文档实现重新等同于固定 Kratos 骨架。

**Blocked by:** 历史事项04和事项39均已关闭；用户已于 2026-09-03 明确同意执行本清理。

**Status:** resolved

**Type:** maintenance

**Plan mapping:** Direct P2 preflight / implementation baseline

**Baseline:** ani-iam `05ba302661d593b608df070dd51cc063fc9f8023` 加当前未提交的事项03/04与 Direct P2 重排现场；ANI 不在本事项范围内。

**Scope:** 分别归档事项03、事项04和 Direct P2 重排材料；建立本地 archive 引用；在 `codex/direct-p2-01-05` 上把非文档实现恢复到固定 Kratos 骨架；保留规格、事项、证据、计划和入口文档；记录旧 RLS 调查与未验证补偿边界；把固定 Kratos 模板生成的 Agent/Claude 仓库规则追加到两个现有入口文件，并显式记录本仓库已接受的适配。

**Out of scope:** DP2-01及后续实现；修改 ANI；修复旧 RLS 或旧 compatibility；部署、切流、Credential 失效、数据重建、旧部署资产删除、push、tag 或远端发布。

**Allowed paths:** `AGENTS.md`、`CLAUDE.md`、`docs/**`、`.scratch/ani-iam-rebuild/**`、`.scratch/ani-iam-p2-direct/**`、`go.mod`、`go.sum`、`internal/compat/**`、`internal/data/**`、`internal/server/grpc.go`、`internal/service/legacy_auth.go`、`tests/cp0/**`。

**Forbidden paths:** `api/**`、`cmd/**`、`configs/**`、`internal/biz/**`、`internal/conf/**`、`internal/service/service.go`、`internal/server/http.go`、`deploy/**`、`../ANI/**`；不得使用 `git reset --hard`、覆盖未知改动或以动态分支替代固定骨架提交。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/00-restore-clean-kratos-baseline/`

- [x] 当前变更全部归属事项03、事项04或 Direct P2 重排，没有未知文件。
- [x] 事项03通过与事项04 `FAIL / BLOCKED` 分开归档，后者不被描述为可用实现。
- [x] PostgreSQL commit 失败且 Redis 补偿也失败的组合明确为 `not_verified`。
- [x] 本地 archive 分支可以恢复清理前现场。
- [x] Direct P2 分支除文档/事项/证据外与 `05ba302661d593b608df070dd51cc063fc9f8023` 无实现差异。
- [x] 完整 Go 测试、`git diff --check` 和干净工作树检查通过。
- [x] `AGENTS.md` 与 `CLAUDE.md` 的追加内容来自固定模板 `59ad406328acba9a70c9e7f426720a75a89a6b9f`，来源和 hash 可复现。
- [x] 两个入口文件都包含完整脚手架规则，并明确 Wire、Makefile/AIP CRUD 和 biz 错误依赖的仓库适配。

**Verification:** 精确 staged-path 审计；历史提交逐个构建/测试；`git diff --exit-code` 对比固定骨架的非文档路径；`go test ./...`；`git diff --check`；最终 `git status --short`。

**Stop conditions:** 出现无法归属的用户改动；无法建立可恢复 archive；固定骨架对象缺失；清理会触及 ANI、真实数据、部署或其他未授权范围。

**Recovery:** 本地 `codex/cp0-archive` 指向清理前完整现场；Direct P2 清理提交可用普通 `git revert` 恢复。不得删除 archive 分支。

**Human checkpoint:** 用户已于 2026-09-03 对“归档后移除未投入使用的事项03/04 compatibility/RLS 调查代码、恢复 Kratos 骨架”给出精确确认；本事项不包含旧部署资产删除。

## Result

`PASS / RESOLVED`。基线恢复证据位于 `.scratch/ani-iam-p2-direct/evidence/00-restore-clean-kratos-baseline/index.md`，脚手架入口规则证据位于同目录的 `kratos-agent-guidelines.md`。

- 事项03、事项04和 Direct P2 重排分别形成可定位的本地归档提交；`codex/cp0-archive` 固定清理前完整现场。
- 事项04继续保持 `FAIL / BLOCKED`，双重补偿失败组合被更正为 `not_verified`。
- `codex/direct-p2-01-05` 的 `go.mod`、`go.sum`、`api`、`cmd`、`configs`、`internal` 和 `tests` 与固定 Kratos 骨架 `05ba302...` 无差异。
- 全量 Go test、vet、build、module verify 和 diff check 通过。
- 固定 Kratos 模板的完整仓库规则已追加到 `AGENTS.md` 与 `CLAUDE.md`；两个追加段逐行一致，标题归一化后与上游固定正文一致。本仓库对 Wire、Makefile/AIP CRUD、biz 错误边界和 sqlc/pgx 的已接受适配置于上游通用规则之前。
- 没有修改 ANI、外部依赖、部署或数据；没有 push、tag、切流或启动 DP2-01。

## Comments

- 2026-09-03：用户确认直接进入 P2，并精确同意先归档后移除未投入使用的事项03/04 compatibility/RLS 调查代码，回到 Kratos 骨架状态。本事项只清理独立 IAM 工作树，不删除旧部署资产。
- 2026-09-03：用户要求把 Kratos 脚手架生成的 `CLAUDE.md`、`AGENTS.md` 内容追加到现有对应文件并直接提交，以确保后续实现遵循 Kratos 分层和生成纪律；本事项重新领取，只允许入口规则与证据变更。
