# 00: 恢复干净 Kratos 实现基线

**What to build:** 在保留事项03/04及 Direct P2 重排审计历史的前提下，从 Direct P2 工作树移除未投入使用的旧 Auth compatibility 与旧 RLS/Redis 调查代码，使非文档实现重新等同于固定 Kratos 骨架。

**Blocked by:** 历史事项04和事项39均已关闭；用户已于 2026-09-03 明确同意执行本清理。

**Status:** claimed

**Type:** maintenance

**Plan mapping:** Direct P2 preflight / implementation baseline

**Baseline:** ani-iam `05ba302661d593b608df070dd51cc063fc9f8023` 加当前未提交的事项03/04与 Direct P2 重排现场；ANI 不在本事项范围内。

**Scope:** 分别归档事项03、事项04和 Direct P2 重排材料；建立本地 archive 引用；在 `codex/direct-p2-01-05` 上把非文档实现恢复到固定 Kratos 骨架；保留规格、事项、证据、计划和入口文档；记录旧 RLS 调查与未验证补偿边界。

**Out of scope:** DP2-01及后续实现；修改 ANI；修复旧 RLS 或旧 compatibility；部署、切流、Credential 失效、数据重建、旧部署资产删除、push、tag 或远端发布。

**Allowed paths:** `AGENTS.md`、`CLAUDE.md`、`docs/**`、`.scratch/ani-iam-rebuild/**`、`.scratch/ani-iam-p2-direct/**`、`go.mod`、`go.sum`、`internal/compat/**`、`internal/data/**`、`internal/server/grpc.go`、`internal/service/legacy_auth.go`、`tests/cp0/**`。

**Forbidden paths:** `api/**`、`cmd/**`、`configs/**`、`internal/biz/**`、`internal/conf/**`、`internal/service/service.go`、`internal/server/http.go`、`deploy/**`、`../ANI/**`；不得使用 `git reset --hard`、覆盖未知改动或以动态分支替代固定骨架提交。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/00-restore-clean-kratos-baseline/`

- [ ] 当前变更全部归属事项03、事项04或 Direct P2 重排，没有未知文件。
- [ ] 事项03通过与事项04 `FAIL / BLOCKED` 分开归档，后者不被描述为可用实现。
- [ ] PostgreSQL commit 失败且 Redis 补偿也失败的组合明确为 `not_verified`。
- [ ] 本地 archive 分支可以恢复清理前现场。
- [ ] Direct P2 分支除文档/事项/证据外与 `05ba302661d593b608df070dd51cc063fc9f8023` 无实现差异。
- [ ] 完整 Go 测试、`git diff --check` 和干净工作树检查通过。

**Verification:** 精确 staged-path 审计；历史提交逐个构建/测试；`git diff --exit-code` 对比固定骨架的非文档路径；`go test ./...`；`git diff --check`；最终 `git status --short`。

**Stop conditions:** 出现无法归属的用户改动；无法建立可恢复 archive；固定骨架对象缺失；清理会触及 ANI、真实数据、部署或其他未授权范围。

**Recovery:** 本地 `codex/cp0-archive` 指向清理前完整现场；Direct P2 清理提交可用普通 `git revert` 恢复。不得删除 archive 分支。

**Human checkpoint:** 用户已于 2026-09-03 对“归档后移除未投入使用的事项03/04 compatibility/RLS 调查代码、恢复 Kratos 骨架”给出精确确认；本事项不包含旧部署资产删除。

## Result

执行中。
