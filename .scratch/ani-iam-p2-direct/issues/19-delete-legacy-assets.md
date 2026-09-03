# 19: 删除旧契约、Runtime 与重叠资产

**What to build:** 在最终目标切换被接受后，分组删除旧 Auth/compat、双轨授权、旧数据结构和重叠 Tenant 能力，使运行系统只剩目标实现。

**Blocked by:** 18 / 重建测试数据并最终整组切换

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-4 / legacy deletion

**Baseline:** 18 人工接受的目标-only 测试环境、逐项 zero-reference 清单、固定恢复镜像和快照。

**Scope:** 旧 runtime/build/config、旧 Auth Proto/client/mock、`internal/compat`、legacy Gateway authz、Blocklist/旧 Refresh、RLS/旧表、重叠 Tenant Admin/Plan、历史 canary/diff 运行入口。

**Out of scope:** 历史 ADR/evidence、目标 IAM/Core/调用方语义、未迁移所有权的能力、生产资产或新功能。

**Allowed paths:** `internal/compat/**`、`configs/**`、`deploy/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/api/proto/auth/**`、对应生成物、`../ANI/repo/deploy/**`，以及本事项和证据目录。

**Forbidden paths:** `docs/adr/**`、历史 `.scratch/**/evidence/**`、目标 `api/iam/v1/**`、目标 `internal/biz/**`/`data/**`、生产环境、上述目录外的 ANI；不得改变目标语义或扩大删除范围。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/19-delete-legacy-assets/`

- [ ] 每个删除组在执行前有精确路径/对象、zero-reference、恢复 Artifact 和人工确认。
- [ ] 旧 Auth runtime/Proto/clients/config、legacy authz/fallback、RLS/旧表和重叠入口无运行引用。
- [ ] 历史 ADR/evidence 保留，目标契约/数据/服务未被删除。
- [ ] 删除后构建、contract breaking、migration、五类 E2E 和 clean install 通过。

**Verification:** 分组 zero-reference、生成物/配置扫描、构建、Buf/OpenAPI breaking、migration replay、五类 E2E 和删除 manifest 完整性。

**Stop conditions:** 未获精确确认；发现真实引用；恢复依赖旧资产；目标路径尚未覆盖；删除范围与 manifest 不一致。

**Recovery:** 每组删除前停止并修复依赖；若已执行，按 18 固定镜像/snapshot 恢复整个测试单元。

**Human checkpoint:** 每组旧运行资产、契约、Credential 结构或数据表删除前，都必须获得针对精确目标和动作的人工确认。
