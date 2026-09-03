# 历史事项04：RLS 负向证据

## 不可改写的结论

状态：`FAIL / BLOCKED`

历史事项04使用的 ANI 基线是 `963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`，不是当前 Direct P2 来源对象。它以固定真实依赖回放旧 migration，并以受限 `ani_app_user` 执行业务写入。即使设置匹配的 `app.current_tenant_id`，插入同 Tenant `refresh_tokens` 仍返回 PostgreSQL `SQLSTATE 42501`：旧 Auth RLS 的正向控制失败。

历史证据的权威位置：

- [事项04证据索引](../../../ani-iam-rebuild/evidence/04-connect-real-legacy-storage/index.md)
- [RLS blocker](../../../ani-iam-rebuild/evidence/04-connect-real-legacy-storage/rls-blocker.md)
- [真实依赖验证](../../../ani-iam-rebuild/evidence/04-connect-real-legacy-storage/verification.md)

## 真实依赖身份

| Dependency | 固定 image | 结果 |
| --- | --- | --- |
| PostgreSQL | `postgres@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c` | `fail`：同 Tenant 正向写入被 RLS 拒绝 |
| Redis | `redis@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf` | `pass`：旧三类 key/TTL 与隔离 namespace |

## 结果矩阵

| Gate | 结果 | 解释 |
| --- | --- | --- |
| 固定旧 migration 回放 | `pass` | 回放到当时冻结 head |
| `ani_app_user` role 属性 | `pass` | LOGIN/NOSUPERUSER/NOBYPASSRLS/NOCREATEROLE/NOCREATEDB |
| 同 Tenant `refresh_tokens` 写入 | `fail` | `SQLSTATE 42501` |
| 两 Tenant RLS 隔离 | `not_verified` | 正向控制先失败，负向 deny 不能证明隔离 |
| Platform refresh | `not_verified` | 同一 PostgreSQL/RLS 子测试此前停止 |
| Redis OIDC state/blocklist/rate TTL | `pass` | 独立 namespace，无全局 flush |
| 依赖在写入前不可用的无部分完成 | `pass` | PostgreSQL/Redis 前置故障均被验证 |
| PostgreSQL commit failure + Redis compensation failure | `not_verified` | 历史实验忽略补偿删除错误 |

旧 migration 对 `api_keys` 和 `refresh_tokens` 启用并强制 RLS，但最终只有 `AS RESTRICTIVE` policy；没有 permissive policy 可以放行匹配行。修旧 migration、加临时 permissive policy，或使用 owner/superuser/BYPASSRLS 都不属于 Direct P2。

## 与当前固定对象的关系

当前 ANI commit `0cedae825a489d936cf41815dc27f278f6d3213c` 的固定 SQL 仍包含同一 `ENABLE/FORCE RLS + AS RESTRICTIVE` Auth policy 结构，因此“缺陷仍存在于来源”这一静态判断为 `pass`。本事项没有对当前对象重新运行 live PostgreSQL，故当前对象的 live 兼容结果为 `not_verified`，绝不从历史 run 外推成 `pass`。

DP2-04 必须从空库创建新的独立 IAM schema，完全不启用 RLS；业务测试必须使用非 owner、非 superuser、无 BYPASSRLS 的受限 runtime role。若只能靠旧 RLS 或高权限 role 通过，DP2-04 应继续为 `FAIL / BLOCKED`，不得进入 DP2-05。
