# Direct P2 事项发布验证

## Authorization

- 日期：2026-09-03
- 用户指令：`接受 Direct P2 规格与 ticket plan，发布新事项，但不启动 DP2-01`
- 执行载体：历史事项39，发布期间为唯一 `claimed` 事项。
- 授权边界：仅创建/更新规格、计划、tracker 和证据；不修改 IAM/ANI 业务代码，不运行实现事项，不部署、不切流、不重建数据、不失效 Credential、不删除资产。

## Published Manifest

`.scratch/ani-iam-p2-direct/issues/` 已发布：

1. `01-freeze-direct-p2-baseline.md`
2. `02-freeze-public-openapi-registry.md`
3. `03-freeze-iam-core-contracts.md`
4. `04-build-no-rls-persistence.md`
5. `05-prove-target-vertical-slice.md`
6. `06-deliver-password-authentication.md`
7. `07-deliver-oidc-identity-link.md`
8. `08-deliver-session-browser-boundaries.md`
9. `09-deliver-tenant-access-authorization.md`
10. `10-deliver-service-principal-api-key.md`
11. `11-deliver-workload-service-token.md`
12. `12-deliver-core-lifecycle-nats.md`
13. `13-complete-cutover-critical-callers.md`
14. `14-rehearse-isolated-cutover-rollback.md`
15. `15-complete-iam-administration-recovery.md`
16. `16-complete-audit-idempotency.md`
17. `17-complete-target-callers-ui-e2e.md`
18. `18-rebuild-data-final-cutover.md`
19. `19-delete-legacy-assets.md`
20. `20-complete-functional-acceptance.md`

## Static Verification

检查脚本逐文件验证：

- 编号恰好为 01–20，无重复或缺口；
- 20 张票状态均为 `ready-for-agent`；
- 每张票均包含 What to build、Blocked by、Baseline、Scope、Out of scope、Allowed paths、Forbidden paths、Evidence path、Verification、Stop conditions 和 Recovery；
- DP2-02–20 都有显式前置，依赖只指向更小编号，因此图无环；
- 唯一初始 frontier 是 DP2-01；
- 发布期间唯一 `claimed` 是事项39，DP2-01 保持 `ready-for-agent`；
- 旧事项映射中的 DP2-01–20 均能解析到已发布编号；
- `git diff --check` 通过。

实际输出：

```text
published_files=20 numbers=01..20
states {'ready-for-agent': 20}
dependency_graph=acyclic all_nonroot_blocked root=DP2-01
claimed_only=issue39 publication; DP2-01=ready-for-agent
mapping_ids_resolve [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20]
```

## Not Verified and Preserved State

- 未运行 Go、数据库、Redis、Dex、NATS、Gateway 或浏览器测试；本轮发布事项，不实施事项。
- 未读取动态 ANI `main` 作为来源，也未修改 `../ANI/**`。
- 事项03/04遗留的代码与测试现场保持原样；本轮没有把这些变更归入 Direct P2 实现。
- 没有 stage、commit、push、tag、部署或外部状态变更。
