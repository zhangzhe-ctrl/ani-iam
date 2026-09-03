# 事项04证据索引

状态：`FAIL / BLOCKED`

采集日期：2026-09-03（Asia/Shanghai）

唯一 ANI 基线：`main@963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`

事项04已使用固定镜像回放冻结 migration，并以真实 `ani_app_user` 执行业务写入。真实门禁证明旧 Auth RLS 正向路径无法成立，因此不能把本事项记为通过或 `resolved`。

| 证据 | 状态 | 定位 |
| --- | --- | --- |
| 固定 migration 全量回放至 head | `pass` | [verification.md](verification.md) |
| `ani_app_user` 受限角色属性 | `pass` | [verification.md](verification.md) |
| 匹配 Tenant 的 `refresh_tokens` 写入 | `fail`：PostgreSQL `42501` | [rls-blocker.md](rls-blocker.md) |
| 两 Tenant RLS 隔离 | `not_verified`：正向控制先失败，负向 deny 不能证明隔离 | [rls-blocker.md](rls-blocker.md) |
| Redis namespace、OIDC state、Blocklist、rate-limit TTL | `pass` | [verification.md](verification.md) |
| PostgreSQL/Redis 在写入前不可用的回滚 | `pass` | [verification.md](verification.md) |
| PostgreSQL commit 失败且 Redis 补偿删除失败 | `not_verified`：实验实现忽略补偿删除错误 | [verification.md](verification.md) |
| 隔离状态清理 | `pass`：测试结束后 `docker ps` 为空 | [verification.md](verification.md) |

## 结论

冻结 migration 对 `api_keys` 和 `refresh_tokens` 执行 `ENABLE/FORCE ROW LEVEL SECURITY`，但最终只存在 `AS RESTRICTIVE` policy。PostgreSQL 要求至少一个 permissive policy 才可能放行行访问，因此受限 runtime role 的匹配 Tenant 写入也被拒绝。

继续需要一个新的人工决定：要么先在 ANI 修复旧 RLS migration、选择新的不可变兼容基线并重新冻结 Oracle；要么明确修改 CP0 的兼容目标。事项04无权自行选择，也不能用临时 policy、superuser 或 `BYPASSRLS` 绕过。用户后续已选择停止 CP0/P1 并转向 Direct P2；本证据仍保留为负向历史。
