# 05: 证明目标最小纵向链路

**What to build:** 使用目标契约和无 RLS 数据库跑通 `Password Login → Session → CheckPermission → 目标 Gateway → 一个受保护 API`，尽早判断目标架构是否闭环。

**Blocked by:** 02 / 冻结公开 OpenAPI 与 Operation Registry；03 / 冻结 IAM 与 Core 集成契约；04 / 建立无 RLS 持久化基础

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-1 / Go-No-Go A

**Baseline:** 02/03 的固定契约与 registry、04 的空库/受限 role 基础、固定目标 Gateway operation。

**Scope:** 最小 Human/Password/Session/Grant/Permission 数据与 use case、目标 gRPC service、Gateway 一次决策、一个受保护 Handler、稳定错误、同事务 Audit 和真实依赖 E2E。

**Out of scope:** OIDC、完整 Refresh/browser、Invitation、完整 Role 管理、API Key、Service Token、Core/NATS、五类调用方整体切换或 legacy fallback。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`internal/server/**`、`migrations/**`、`configs/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**` 中固定目标 operation/registry/测试，以及本事项和证据目录。

**Forbidden paths:** 已冻结的 `api/**` 和 `../ANI/repo/api/openapi/**`、`deploy/**`、其他 `../ANI/repo/services/**`、旧 Auth runtime；不得代码 fallback、默认 allow、RLS 或 superuser 业务查询。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/05-prove-target-vertical-slice/`

- [ ] Password 成功与无效 Credential、Session/Grant 和 Access Token 可验证。
- [ ] CheckPermission allow/deny、policy revision mismatch 和 Gateway `401/403/503/504` 稳定。
- [ ] Authorized 路由只调用 IAM 一次，客户端 `x-ani-*` 被删除后才注入可信上下文。
- [ ] mutation 与 Audit 原子；两 Tenant 负向和 query mutation 通过。
- [ ] 使用真实独立 PostgreSQL/Redis、受限 runtime role 和目标 Gateway，不用 fake 代替 gate。

**Verification:** unit、contract、真实依赖 integration、Gateway E2E、故障注入、两 Tenant/query mutation 和空库 replay 全部通过；Artifact/配置固定。

**Stop conditions:** 目标链路无法在无 RLS/受限 role 下闭环；需要 legacy fallback、多次 IAM 决策、默认 allow 或修改已冻结契约。

**Recovery:** 删除隔离目标 route/config、数据库 fixture 和 Credential；主调用路径保持旧系统。

**Human checkpoint:** 证据完成后输出 Go/No-Go A 等待人工接受；不得自动领取 06、07 或 09。
