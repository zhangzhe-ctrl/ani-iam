# 01: 固定兼容性 Oracle 与替代检查

**What to build:** 固定可复现的旧 Auth 兼容性基线，使后续 CP0 能够比较真实外部行为，而不会把动态 HEAD、Mock 或失效旧门禁当作证据。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

**Plan mapping:** 基线准备

**Baseline:** ANI `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`；只读采集，不修改运行时。

**Scope:** Git 身份、14 RPC/Proto descriptor、migration/schema/runtime role/RLS、Redis、Dex/OIDC、Gateway、Envoy、Inference，以及旧门禁的替代检查记录。

**Out of scope:** Kratos scaffold、契约修改、数据库迁移、部署、切流或任何外部状态写入。

**Allowed paths:** 本事项文件与 `.scratch/ani-iam-rebuild/evidence/01-freeze-compatibility-oracle/**`。

**Read-only inputs:** 当前仓库，以及固定在 `main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be` 的 `../ANI/repo/**`。

**Forbidden paths:** `api/**`、`cmd/**`、`internal/**`、`migrations/**`、`deploy/**`、`go.mod`、`go.sum`、`../ANI/repo/**` 的任何写入。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/01-freeze-compatibility-oracle/`

- [ ] 记录精确 Commit、分支意图、工作树状态和不可变 Artifact 摘要，且没有用动态引用替代。
- [ ] 保存 14 RPC、Proto、PG/RLS、Redis、Dex 和三个调用方的可复现现状证据；缺失项标为 `not_verified`。
- [ ] 每个被否决的旧门禁都有原断言、失败证据、失效理由、目标不变量和可复现的新检查。
- [ ] 产出 CP0 的语义范围、真实依赖、停止条件和人工评审材料。

**Verification:** 重新运行只读探针能得到同一基线身份；证据索引中的每项都可定位到原始输出。

**Stop conditions:** Commit 不可解析、真实依赖无法隔离、14 RPC 清单不确定，或新检查会改变运行时。

**Recovery:** 无运行时变更；删除临时探针输出即可恢复。证据不完整时保持本事项未解决。

**Human checkpoint:** 证据得到人工接受后，才能领取 02。
