# 37: 删除旧契约、Runtime 与重叠入口

**What to build:** 在目标切换稳定后删除旧 Auth/compat、双轨授权、旧数据结构和重叠 Tenant 管理能力，使系统只剩一个目标实现和所有权模型。

**Blocked by:** 36 / 执行测试环境数据重建与整组切换

**Status:** ready-for-agent

**Plan mapping:** P2-8

**Baseline:** 36 接受的目标环境、zero-reference 清单、固定恢复镜像和快照。

**Scope:** 旧 Runtime/build/config、旧 Proto/client/mock、compat、legacy Gateway 授权、Blocklist/旧 Refresh、RLS/旧表、重叠 Tenant Admin/Plan、P1 Canary/diff 运行入口。

**Out of scope:** 历史 ADR/证据、未来 Production Readiness、未迁移所有权的 Core/Services 能力。

**Allowed paths:** `internal/compat/**`、`configs/**`、`deploy/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/api/proto/auth/**`、`../ANI/repo/api/generated/**`、`../ANI/repo/deploy/**`，以及本事项和其证据目录。

**Forbidden paths:** `docs/adr/**`、`.scratch/ani-iam-rebuild/evidence/**`、目标 `api/iam/v1/**`、目标 `internal/biz/**`、目标 `internal/data/**`、上述目录之外的 `../ANI/repo/**`；不得扩大为新功能或修改目标语义。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/37-delete-legacy-assets/`

- [ ] 删除前对每项资产证明零运行引用、零调用方、零数据/恢复依赖并获得人工确认。
- [ ] 旧 Auth、CheckPermission 两代路径、legacy fallback 和旧地址变量不再存在。
- [ ] RLS、通用 soft delete、jwt_blocklist、旧单 Role/多态 Binding 与重叠入口被移除。
- [ ] 删除后三调用方、Console/BOSS、空库安装和目标功能回归通过。

**Verification:** zero-reference、契约 breaking、构建、migration、E2E 和配置扫描通过。

**Stop conditions:** 发现真实引用、恢复仍依赖旧资产或目标路径尚未覆盖。

**Recovery:** 在删除前停止并修复依赖；若已执行则按 36 的固定镜像/snapshot 恢复整个测试单元。

**Human checkpoint:** 删除每组旧运行资产、契约或数据结构前必须获得精确人工确认。
