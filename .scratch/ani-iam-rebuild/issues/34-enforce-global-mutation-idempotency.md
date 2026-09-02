# 34: 实现并聚合验证全局 Mutation 幂等

**What to build:** 建立统一的 24 小时 Idempotency Ledger，并证明每个公开有副作用的 POST/PUT/PATCH 都使用同一作用域、冲突与过期语义。

**Blocked by:** 16 / 冻结公开 OpenAPI 与 Operation Registry；19 / 交付 Tenant Access Bootstrap 纵向链路；20 / 交付 Invitation 到 Human Membership；21 / 交付 Role、Binding 与 Membership 管理；22 / 交付 Password 身份认证；23 / 交付 OIDC 登录与 Identity Link；24 / 交付 Session Grant 与旋转 Refresh；25 / 交付浏览器与 Platform/BOSS 边界；27 / 交付 Service Principal 与 API Key；31 / 交付 Platform Role、Invitation 与 Membership 管理；32 / 交付高风险 Tenant Admin 恢复

**Status:** ready-for-agent

**Plan mapping:** P2-4

**Baseline:** Q108、Q243、目标 OpenAPI operation registry，以及所有已实现公开 mutation。

**Scope:** 共享 Idempotency Ledger/port、boundary+actor+operation+key+request hash+serialized result、24 小时逻辑窗口、operation inventory、聚合契约/并发/失败门禁。

**Out of scope:** GET/只读操作、进程内缓存、永久重放、以幂等键替代不同请求间的业务并发锁。

**Allowed paths:** `internal/biz/**`、`internal/data/**`、`internal/service/**`、`migrations/**`、`tests/**`、`../ANI/repo/services/ani-gateway/**`、`../ANI/repo/frontends/console/**`、`../ANI/repo/frontends/boss/**`，以及本事项和其证据目录。

**Forbidden paths:** `api/iam/v1/**`、`../ANI/repo/api/openapi/**`、`../ANI/repo/services/tenant-service/**`、`../ANI/repo/services/auth-service/**`、`../ANI/repo/deploy/**`；不得修改 16/17 已冻结契约、改变领域状态机、放宽 operation authorization 或用全局 Key 忽略 boundary/actor。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/34-enforce-global-mutation-idempotency/`

- [ ] Registry 中每个公开 POST/PUT/PATCH 都被聚合检查枚举，缺少幂等接入时门禁失败。
- [ ] 同 scope/key/request hash 重放保存的稳定结果，不重复发送通知或执行安全动作。
- [ ] 同 scope/key 不同 request hash 返回 `409`；超过逻辑 24 小时后返回 `409 IDEMPOTENCY_KEY_EXPIRED` 并要求新 Key。
- [ ] 状态变化、Ledger 结果和必需 Audit 保持原子；失败或并发请求不产生部分业务效果。
- [ ] 幂等 Key 作用域隔离 Tenant/Platform、actor 和 operation，不能跨边界重放。

**Verification:** operation inventory、契约、真实数据库、并发、事务故障、通知去重、跨边界和过期测试通过。

**Stop conditions:** 无法从 registry 完整枚举 mutation，或现有 mutation 需要改变业务语义才能接入。

**Recovery:** 回退共享接入层和未发布 migration；不得删除仍在逻辑窗口内的 Ledger 记录。
