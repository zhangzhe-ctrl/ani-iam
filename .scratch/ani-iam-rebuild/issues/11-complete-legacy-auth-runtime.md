# 11: 补齐完整旧 Auth Runtime

**What to build:** 让受支持的 iam-service 完整承载冻结的旧 Auth 14 RPC、真实依赖和故障语义，形成可供 Canary 使用的同构实现。

**Blocked by:** 10 / 将 Spike 晋升为受支持的 iam-service

**Status:** wontfix

**Superseded by:** Direct P2 DP2-06–DP2-13；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。不再实现旧 Auth 14 RPC 的同构 runtime。

**Plan mapping:** P1-1、P1-2

**Baseline:** CP0 Oracle 与 10 的受支持 Runtime。

**Scope:** 全部 14 RPC、旧 pgx/RLS/Redis Adapter、错误映射、旧测试 Oracle、真实依赖和故障矩阵。

**Out of scope:** 新业务功能、目标数据模型、长期兼容 fallback 或生产切流。

**Allowed paths:** `go.mod`、`go.sum`、`cmd/server/**`、`internal/compat/authv1/**`、`internal/biz/**`、`internal/data/**`、`internal/service/**`、`internal/server/**`、`internal/conf/**`、`configs/**`、`tests/**`、CI/构建配置，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`；不得实现 P2 目标业务。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/11-complete-legacy-auth-runtime/`

- [ ] 14 RPC 的成功、拒绝和依赖故障路径均有契约/集成证据。
- [ ] 旧测试 Oracle 被迁入独立项目且不依赖 ANI 内部 package。
- [ ] 全量差分没有未批准 wire、Claim、状态或副作用漂移。
- [ ] 兼容代码保持在隔离边界，业务层仍框架无关。

**Verification:** contract、真实依赖 integration、differential 和故障矩阵通过。

**Stop conditions:** 完整覆盖要求引入 P2 语义或无法隔离的 ANI 内部实现。

**Recovery:** 保持旧 Auth 为唯一运行路径，回退 Canary 构建。
