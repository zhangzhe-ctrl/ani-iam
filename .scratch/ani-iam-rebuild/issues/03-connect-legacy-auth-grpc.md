# 03: 接通旧 Auth gRPC 契约

**What to build:** 让隔离 Kratos Runtime 注册并接收冻结的旧 Auth 14 RPC，同时保持 wire、deadline 和错误边界可比较。

**Blocked by:** 02 / 建立隔离 Kratos 运行骨架

**Status:** ready-for-agent

**Plan mapping:** CP0-1

**Baseline:** 01 的旧 Auth descriptor 与 02 的固定 Kratos Runtime。

**Scope:** 旧 Proto transport、14 RPC 注册清单、兼容边界、deadline、gRPC status/detail 和公共错误映射。

**Out of scope:** 目标 IAM Proto、业务语义重写、目标数据模型、调用方切流。

**Allowed paths:** `internal/compat/authv1/**`、`internal/service/**`、`internal/server/**`、`tests/cp0/**`、`go.mod`、`go.sum`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`migrations/**`、`deploy/**`、`../ANI/repo/**`、`internal/biz/**` 的目标业务语义修改。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/03-connect-legacy-auth-grpc/`

- [ ] 冻结 descriptor 与基线无未批准差异，14 RPC 均可被发现和调用。
- [ ] deadline、取消、未知方法和错误 detail 的外部行为有契约测试。
- [ ] 兼容 Proto 被隔离在兼容边界，目标业务层不导入它。
- [ ] 未实现业务返回明确兼容错误，不出现默认成功或框架错误泄漏。

**Verification:** descriptor diff、RPC inventory 和 transport contract suite 通过。

**Stop conditions:** wire drift 无法隔离，或修复要求提前修改旧契约。

**Recovery:** 回退隔离 Runtime 的 transport 注册，不影响旧服务。
