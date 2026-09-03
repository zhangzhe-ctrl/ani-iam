# 20: 完成 Direct P2 功能验收

**What to build:** 汇总 clean install、目标功能矩阵、五类调用方、数据和删除证据，形成诚实的 IAM 重构功能完成结论并保留全部生产就绪缺口。

**Blocked by:** 19 / 删除旧契约、Runtime 与重叠资产

**Status:** ready-for-agent

**Type:** enhancement

**Plan mapping:** DP2-4 / functional acceptance

**Baseline:** 19 的目标-only 系统、固定契约/镜像/schema、当前规格、核心计划和 accepted ADR。

**Scope:** clean install、功能/所有权/安全矩阵、契约/integration/E2E/静态门禁、文档一致性、证据索引、恢复状态和 `not_verified` 清单。

**Out of scope:** 在验收票修复实现、补做未授权 PR0、生产部署、Production Ready 声明或掩盖缺失真实证据。

**Allowed paths:** `docs/**`、`tests/verification/**`、本事项、`.scratch/ani-iam-p2-direct/evidence/20-complete-functional-acceptance/**`。

**Read-only inputs:** 当前仓库实现/契约/migration/config/deploy、全部 Direct P2 evidence 和 `../ANI/repo/**`。

**Forbidden paths:** `api/**`、`internal/**`、`migrations/**`、`configs/**`、`deploy/**`、`../ANI/repo/**` 的任何写入；不得以验收事项修复缺陷或把 `not_verified` 改成 pass。

**Evidence path:** `.scratch/ani-iam-p2-direct/evidence/20-complete-functional-acceptance/`

- [ ] 独立 iam-service 只注册目标三个服务，五类调用方只消费目标契约。
- [ ] Tenant/Core 所有权、无 RLS 隔离、认证/授权/Credential/Audit/Lifecycle 不变量均有真实证据。
- [ ] 空库 migration/seed、受限 role、五类独立 E2E、zero-reference 和删除清单通过。
- [ ] 当前规格、两份核心计划、Q1–Q300 矩阵和 accepted ADR 无冲突。
- [ ] 所有未运行、生产级或缺失证据明确标为 `not_verified`，结论不使用 Production Ready。

**Verification:** 聚合实际执行的测试、静态检查、Artifact hash 和证据；逐项复现或定位，不运行隐含修复。

**Stop conditions:** 任一必需功能门禁失败；证据不能固定；文档与实现冲突；需要修改业务代码才能验收。

**Recovery:** 不执行新运行状态变更；回开对应实施事项修复后重新验收。

**Human checkpoint:** 人工接受“功能完成”结论；该接受不授权生产部署或 PR0。
