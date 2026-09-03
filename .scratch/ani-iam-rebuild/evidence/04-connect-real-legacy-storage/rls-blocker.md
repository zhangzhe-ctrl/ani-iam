# 冻结旧 Auth RLS 阻塞证据

## 固定输入

- ANI Commit：`963bc88836c54a1b09cf100b37eb2f2cb2a5a4be`
- `deploy/migrations/atlas.sum` SHA-256：`175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af`
- Migration head：`20260831_001_async_tasks_rls_fix.sql`
- PostgreSQL image：`postgres@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`
- Migration owner：隔离容器内的 `ani`
- Runtime role：冻结 migration 创建的 `ani_app_user`

## 冻结 SQL 事实

`20260501000100_init_schema.sql` 对 `api_keys` 和 `refresh_tokens` 同时执行：

```sql
ALTER TABLE ... ENABLE ROW LEVEL SECURITY;
ALTER TABLE ... FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ...
    AS RESTRICTIVE
    USING (... app.current_tenant_id ...);
```

`20260707001401_platform_refresh_tokens.sql` 替换 `refresh_tokens` policy 时仍使用 `AS RESTRICTIVE`。全 migration inventory 没有为现存 `api_keys` 或 `refresh_tokens` 创建 permissive policy；`20260502000200_operations_idempotency.sql` 中的默认 permissive API Key policy 只位于“表不存在”分支，而该表已由初始 migration 创建。

`20260828000100_database_roles_hardening.sql` 把 `ani_app_user` 固定为 `LOGIN NOSUPERUSER NOBYPASSRLS`，`20260828000200_app_role_privileges.sql` 虽授予 DML，但 grant 不绕过 RLS。

## 真实结果

测试在事务内成功执行：

```sql
SELECT set_config('app.current_tenant_id', '<tenant-a>', true);
```

随后插入相同 Tenant 的 `refresh_tokens` 行，PostgreSQL 返回：

```text
ERROR: new row violates row-level security policy for table "refresh_tokens" (SQLSTATE 42501)
```

这不是跨 Tenant 负向通过，而是所有 runtime 行均被拒绝。没有正向控制时，跨 Tenant deny 不能构成有效的 RLS 隔离证据。

## 停止判断

要让匹配 Tenant 的操作通过，至少需要增加 permissive policy、修改冻结 migration/schema，或改用 owner/superuser/BYPASSRLS。前两项改变固定 Oracle，后一项被事项04停止条件明确禁止。因此本事项必须停止并等待新的人工决定。
