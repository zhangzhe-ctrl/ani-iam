# 38: 完成最终功能验收

**What to build:** 汇总空环境安装、目标功能矩阵、五类调用方和删除证据，形成诚实的 IAM 重构功能完成结论，同时保留所有生产就绪缺口。

**Blocked by:** 37 / 删除旧契约、Runtime 与重叠入口

**Status:** wontfix

**Superseded by:** Direct P2 DP2-20；见 `../evidence/39-replan-direct-p2/ticket-mapping.md`。

**Plan mapping:** P2-9

**Baseline:** 37 的目标-only 系统、固定契约/镜像/Schema、当前规格和 accepted ADR。

**Scope:** clean install、功能矩阵、契约/集成/E2E/静态门禁、文档一致性、证据索引、恢复状态和 `not_verified` 清单。

**Out of scope:** 宣称 Production Ready、补做未授权 PR0、掩盖未运行真实依赖测试。

**Allowed paths:** `docs/**`、`tests/verification/**`、`.scratch/ani-iam-rebuild/issues/**` 与本事项证据目录。

**Read-only inputs:** 当前仓库的实现、契约、migration、配置、部署材料，以及 `../ANI/repo/**`。

**Forbidden paths:** `api/**`、`internal/**`、`migrations/**`、`configs/**`、`deploy/**`、`../ANI/repo/**` 的任何写入；不得以验收票修复实现或把 `not_verified` 改写为通过。

**Evidence path:** `.scratch/ani-iam-rebuild/evidence/38-complete-functional-acceptance/`

- [ ] 独立 iam-service 只注册目标三个 gRPC Service，五类调用方全部使用目标契约。
- [ ] Tenant/Core 所有权、无 RLS 隔离、认证/授权/Credential/Audit/Lifecycle 不变量均有真实证据。
- [ ] 空库 migration/seed、受限 Runtime Role、三调用方独立 E2E 和删除清单通过。
- [ ] 当前规格、两份核心计划、Q1–Q300 矩阵和 accepted ADR 无冲突。
- [ ] 所有未运行或缺失生产证据明确标为 `not_verified`，结论不使用 Production Ready。

**Verification:** 聚合实际执行的测试、静态检查和证据；每项结果可追溯到固定 Artifact。

**Stop conditions:** 任一必需功能门禁失败、证据无法固定或文档与实现冲突。

**Recovery:** 不执行新状态变更；修正阻塞事项后重新验收。

**Human checkpoint:** 人工接受“功能完成”结论；该接受不授权生产部署。
