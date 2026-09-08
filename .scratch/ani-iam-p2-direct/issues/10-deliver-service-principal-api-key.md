# 10: 交付 Service Principal、API Key 与 Envoy 验证

**What to build:** 交付固定单 Tenant 的 Service Principal、Bearer API Key 生命周期，并让 Envoy Adapter 独立验证目标 Credential 的 allow/deny/failure 行为。

**Blocked by:** 04 / 建立无 RLS 持久化基础；09 / 交付 Tenant Access、Membership 与目标授权

**Status:** resolved

**Type:** enhancement

**Plan mapping:** DP2-2 / API key and Envoy

**Baseline:** 02/03 固定契约、04 受限 persistence、09 Membership/Role/CheckPermission。

**Scope:** Service Principal、唯一 Tenant Membership/Binding、API Key create/list/revoke/disable、Secret 单次显示/Hash、过期/告警、Bearer 认证、Audit/幂等和 Envoy 独立 E2E。

**Out of scope:** Workload Service Token、Platform Service Principal、权限快照、自动 rotation 或旧 API Key 迁移。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/envoy-authz-adapter/**`，以及本事项和证据目录；经 2026-09-09 人工精确批准，额外仅允许 `cmd/server/app.go`、`cmd/server/app_test.go`、`internal/server/observability.go`、`internal/server/observability_test.go` 完成生产 limiter/usage wiring、Kratos worker lifecycle 与无高基数标签聚合指标。

**Forbidden paths:** `api/**`、`../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/**`；不得保存明文 Secret、创建跨 Tenant/Platform Service Principal、用 API Key 代替 Workload Token。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/10-deliver-service-principal-api-key/`

- [x] Key 有明确 Principal/Tenant/boundary，Secret 只显示一次且仅保存 Hash。
- [x] create/list/revoke/disable、过期、并发和稳定错误完整，状态与 Audit/幂等原子。
- [x] Envoy 对有效、无效、撤销、权限拒绝、IAM 不可用分别有独立证据。
- [x] Service Principal 移除唯一 Membership 时 disable 且撤销全部 Key。

**Verification:** 生命周期、真实数据库、并发/故障、两 Tenant、Secret 扫描、Envoy E2E 和错误映射通过。

**Stop conditions:** Key 无主体、跨 Tenant、必须保存明文/Permission snapshot，或 Envoy 只能依赖 Gateway 证据。

**Recovery:** 吊销并清理隔离 Key/Service Principal，恢复 Envoy 隔离配置；不影响 Human Principal。

## Comments

- 2026-09-08：用户确认 DP2-10 安全暂停，并要求先执行 ANI 9 月 30 日前 containment。当前完成范围、未验证项和恢复入口固定在 `../evidence/10-deliver-service-principal-api-key/pause-checkpoint-20260908.md`；本事项退出 `claimed`，剩余实现不得在 containment 期间继续。
- 2026-09-08T11:27:14Z：CF-01 已封存并停止，`environment-established=fail`；Registry/Storage/Network foundation 可复用，current/target runtime、固定 smoke 与 target exactly-one `CheckPermission` 均为 `not_verified`。失败根因是 fixed current ANI Auth Dockerfile build 要求 `go mod tidy`，未修改 ANI source 绕过。无 Credential/跨 lane/共享环境事件，无运行中的写入动作；CF-01 namespace、probe/echo/runner 与两个 Retain PVC/PV 保留待人工诊断。DP2-10 现恢复为唯一 `claimed`，原 dirty recovery state 精确保持不变；本 Comment 仅完成 handoff，不继续实现 DP2-10。
- 2026-09-08T11:48:31Z：用户明确要求继续 CF-01：事项 01 已固定 ANI `main@56a5f0b...`，因此沿用该提交并允许用 task-owned Dockerfile 基于其 detached source 构建 current Auth，记录 source/tree、Dockerfile/base image 与 OCI digest，不再要求该 tree 内原 Dockerfile原样通过。DP2-10 再次安全暂停并退出 `claimed`；dirty recovery state 必须继续原样保留，本 Goal 不继续实现 DP2-10。
- 2026-09-08T12:18:01Z：用户将 CF-01 follow-up 收窄为固定 ANI main 提交构建并记录 current Auth 镜像；该结果已 `pass` 并封存在 CF-01 evidence。CF-01 不再继续 runtime/smoke 工作并退出 `claimed`，DP2-10 恢复为唯一 `claimed`；本次 handoff 未继续或改变 DP2-10 dirty implementation。
- 2026-09-09：用户明确接受 DP2-06 后的 IAM descriptor SHA-256 `66552fe0a53a1c4956f6ee7943498af1b5309602fec28eb47c1b51f4276f5d9a` 为本轮 DP2-10–12 基线。该 descriptor 相对 DP2-03 原始 `df863beb...` 的已提交差异仅来自 DP2-06 为 `PasswordLoginRequest` 增加的 `source_ip` 字段；本事项不修改 `api/**`。恢复前 checkpoint 对照和其余固定 Artifact 结果记录在 `../evidence/10-deliver-service-principal-api-key/baseline-resume-20260909.md`，DP2-10 从既定 `ValidatePrincipal` resume point 继续。
- 2026-09-09：独立 Standards/Spec review 均为 `fail`。当前 Allowed paths 内可修复项已通过聚焦测试；真实 Redis API Key 创建限流、异步 `last_used_at` flusher 与 operator-visible 指标仍需精确新增 `cmd/server/app.go`、`cmd/server/app_test.go`、`internal/server/observability.go`、`internal/server/observability_test.go`。尚未修改这些路径，等待人工接受或拒绝；完整差异、非扩展项和 DP2-13/DP2-16 边界见 `../evidence/10-deliver-service-principal-api-key/review-path-checkpoint-20260909.md`。
- 2026-09-09：在既有 Allowed paths 内完成真实 Redis 创建限流、异步 usage 聚合/受限 PostgreSQL flush、无高基数标签的 operator 聚合快照，以及真实 IAM+受限 PostgreSQL+Redis+mTLS+独立 Envoy Adapter 进程矩阵；聚焦门禁均为 `pass`。生产注入、Kratos lifecycle flusher 和指标注册仍只需要上一条列出的四个精确路径，尚未修改，DP2-10 继续保持 `claimed`。
- 2026-09-09：第二轮独立复核发现的两个 Allowed-path blocker 已修复并复核为 `pass`：API Key secret/status/expiry 现在先于 Tenant Access/lifecycle/permission 查询，真实 PostgreSQL+Redis 证明 stale lifecycle 下 wrong-secret/expired/revoked 仍为 `401`、有效 Key 为 `503`；未认证 `CheckPermission` 失败改写 unbound anonymous-principal audit，不再信任请求 `TargetTenantID` 选择审计 Tenant。整体仍仅因同一四路径 production-wiring 人工检查点为 `fail`，事项保持 `claimed`。
- 2026-09-09：逐项完成性审计又补齐 real PostgreSQL missing-lifecycle、重新 enable 不复活旧 Key、管理 RPC 正向 Get/Update/Revoke，以及 frozen registry 敏感 operation 分类回归，聚焦真实门禁 `pass`。审计结论仍不放宽：事项 1/8/9 的生产 runtime 证明因同一四路径 wiring 缺口为 `fail`，完整矩阵见 `../evidence/10-deliver-service-principal-api-key/completion-audit-20260909.md`；未 stage、未 commit、未 resolve。
- 2026-09-09：用户回复“批准修改”，精确批准上一条人工检查点列出的四个文件；没有批准配置、契约、部署或其他路径扩展。生产 runtime 已显式注入 Redis create limiter 与共享 usage aggregator，Kratos lifecycle 启动/停止 deadline-bounded multi-batch flusher，指标公开无 Tenant/Principal/Credential 标签的 stale/unusual 聚合值及采样时间。实现与聚焦真实依赖门禁为 `pass`；事项保持 `claimed`，等待最终双轴复核、完整门禁、证据和本地提交收口。
- 2026-09-09：最终 Standards/Spec 双轴复核均无 blocker；IAM unit/race/vet、完整受限真实依赖 integration、独立 real-IAM/mTLS/Envoy 进程、ANI Adapter race/vet、冻结哈希、secret scan 和 staged-path audit 均为 `pass`。DP2-10 commit pair：IAM `78c5265bcd9eddb97ed7525fe4756db03abc5c50`，ANI `f4af3902e346d15e69bdeceda725730edec3542f`。未 push、未建 PR、未部署、未切流、未失效 Credential、未修改共享基础设施、未删除数据；事项现收口为 `resolved`。
