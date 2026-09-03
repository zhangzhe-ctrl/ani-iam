# 旧事项到 Direct P2的覆盖映射

Status: accepted / published

本映射核对旧事项05–38的需求去向。`wontfix` 只表示旧的 CP0/P1/P2 执行切片不再实施；需求由已发布的 Direct P2 事项继承，不表示需求取消。DP2-01–20 位于 `.scratch/ani-iam-p2-direct/issues/`。

| 旧事项 | 状态处置 | Direct P2 事项 | 覆盖说明 |
| --- | --- | --- | --- |
| 05 Password/Refresh/Revoke compat | wontfix | DP2-06, DP2-08 | 改为目标 Password、Session/Refresh，不再验证旧语义 |
| 06 OIDC/Dex compat | wontfix | DP2-07 | 改为目标 PKCE、verified email、显式 Link |
| 07 API Key/Service Token compat | wontfix | DP2-10, DP2-11 | 拆开外部 API Key 与内部 Workload Token |
| 08 Principal/Permission compat | wontfix | DP2-05, DP2-09 | 由最小链路和完整目标授权继承 |
| 09 CP0 Go/No-Go | wontfix | DP2-05, DP2-13, DP2-14 | 旧兼容结论被两道目标门禁替代 |
| 10 晋升 compat runtime | wontfix | DP2-05, DP2-20 | 不晋升旧 runtime；目标 runtime 由纵向链路和最终验收覆盖 |
| 11 完整旧 Auth runtime | wontfix | DP2-06–DP2-13 | 旧 14 RPC 按当前调用行为映射到目标能力，不实现同构 runtime |
| 12 稳定旧 IAM 地址 | wontfix | DP2-14, DP2-18 | 地址/selector 只在早期演练和最终切换处理 |
| 13 compat canary | wontfix | DP2-13, DP2-14 | 由目标调用方对等和整组演练替代 |
| 14 selector 切 Kratos | wontfix | DP2-14 | 改为目标 IAM 隔离轨道整组切入/回退 |
| 15 观察并下线旧 runtime | wontfix | DP2-14, DP2-18, DP2-19 | 早期不下线；最终切换后单独删除 |
| 16 OpenAPI/registry | wontfix | DP2-02 | 原验收完整继承并前置 |
| 17 IAM/Core contracts | wontfix | DP2-03 | 原验收完整继承并前置 |
| 18 无 RLS persistence | wontfix | DP2-04 | 原验收完整继承并前置 |
| 19 Tenant Access Bootstrap | wontfix | DP2-09, DP2-12, DP2-15 | 基础访问、真实 Core 链路和完整恢复分阶段覆盖 |
| 20 Invitation→Human | wontfix | DP2-15 | 放到 Go/No-Go B 后完成 |
| 21 Role/Binding/Membership | wontfix | DP2-09, DP2-15 | 切换必需最小授权与完整管理分开 |
| 22 Password | wontfix | DP2-06 | 目标验收完整继承 |
| 23 OIDC/Identity Link | wontfix | DP2-07 | 目标验收完整继承 |
| 24 Session/Grant/Refresh | wontfix | DP2-08 | 目标验收完整继承 |
| 25 Browser/Platform boundary | wontfix | DP2-08, DP2-15, DP2-17 | 基础浏览器边界、完整平台管理和 UI E2E 分开 |
| 26 one-call authorization | wontfix | DP2-05, DP2-09 | 最小证明与完整授权分开 |
| 27 Service Principal/API Key | wontfix | DP2-10 | 目标验收完整继承 |
| 28 Workload Service Token | wontfix | DP2-11 | 目标验收完整继承 |
| 29 Lifecycle projection/repair | wontfix | DP2-12 | 与真实 Core/NATS 合并成纵向切片 |
| 30 Core outbox/bootstrap/DLQ | wontfix | DP2-12 | 与投影、修复合并成纵向切片 |
| 31 Platform administration | wontfix | DP2-15 | 放到 Go/No-Go B 后完成 |
| 32 high-risk recovery | wontfix | DP2-15 | 保留双人审批、重认证、单次 approval 等不变量 |
| 33 Audit query/retention | wontfix | DP2-16 | 与聚合 Audit 门禁同一后期切片 |
| 34 global idempotency | wontfix | DP2-16 | 与全部公开 mutation 聚合验证 |
| 35 target callers/UI E2E | wontfix | DP2-13, DP2-17 | 切换关键 E2E 与完整功能 E2E 分开 |
| 36 rebuild/cut over | wontfix | DP2-14, DP2-18 | 早期无破坏演练与最终破坏性切换分开 |
| 37 delete legacy | wontfix | DP2-19 | 保持单独删除票和人工确认 |
| 38 functional acceptance | wontfix | DP2-20 | 保持验收只读、非 Production Ready 结论 |

## Coverage Result

- 旧事项05–15的兼容目的没有继续实现；其调用行为、安全边界和切换证据由目标票承接。
- 旧事项16–38的目标需求全部至少映射到一个已发布 DP2 事项；没有把仍需实现的需求放入 out-of-scope。
- 旧事项的详细验收仍作为历史输入保留；对应验收条件已写入新票，不只保留标题级引用。
- DP2-18、DP2-19 的破坏性动作保持独立人工确认；映射或 ticket plan 的接受不构成执行授权。
