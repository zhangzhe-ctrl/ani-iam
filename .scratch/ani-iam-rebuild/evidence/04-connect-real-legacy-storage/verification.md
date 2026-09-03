# 事项04验证记录

## 依赖与隔离

- `pgx/v5 v5.9.2`、`go-redis/v9 v9.6.3`：与冻结旧 Auth module 一致。
- `testcontainers-go v0.44.0`：固定版本；Ryuk 被禁用，测试显式终止自身容器。
- PostgreSQL：`postgres@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c`。
- Redis：`redis@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf`。
- 每轮使用独立 PostgreSQL 容器、migration owner、runtime role 和 `cp0:04:<uuid>:` Redis namespace；没有 `FLUSHDB`/`FLUSHALL`。
- 测试只从固定 Git object 读取 migration，校验 `atlas.sum` 摘要后按文件顺序回放至固定 head。

## 实际执行

```text
ANI_IAM_CP0_REAL_DEPS=1 GOTMPDIR=<issue-04-evidence>/.gotmp \
  go test -count=1 -p=1 ./tests/cp0 -run TestLegacyStorageRealDependencies -v
```

结果：`fail`。Migration replay、Redis 子测试、依赖失败/无部分成功子测试通过；PG/RLS 正向子测试以 `SQLSTATE 42501` 失败。详见 [rls-blocker.md](rls-blocker.md)。

```text
GOTMPDIR=<issue-04-evidence>/.gotmp go test -count=1 ./internal/data
GOTMPDIR=<issue-04-evidence>/.gotmp go vet ./internal/data
git diff --check
go mod verify
```

结果：全部 `pass`。

第一次全仓 `go test -count=1 -p=1 ./...` 因 `/tmp` 配额不足而失败；把 `GOTMPDIR` 指向事项证据目录后完成编译，随后仅因沙箱禁止 loopback listener 失败。以本机权限复跑事项03既有 gRPC transport 测试全部通过。该环境失败不冒充代码失败或通过。

## 已通过的行为

- 构造 PostgreSQL adapter 时拒绝 migration owner/superuser，并确认 `ani_app_user` 是 `LOGIN/NOSUPERUSER/NOBYPASSRLS/NOCREATEROLE/NOCREATEDB`。
- Redis OIDC state 使用 10 分钟 TTL，消费后删除；JTI blocklist 使用剩余 TTL；API Key rate counter 使用 1 分钟 TTL。
- Redis 只扫描当前 namespace，没有全局 flush 或共享 mutable fixture。
- Redis client 不可用时 PostgreSQL revocation transaction 回滚；PostgreSQL pool 不可用时不写 Redis。两类错误均归一为稳定 `ErrDependencyUnavailable`。
- 测试退出后显式检查 `docker ps` 为空，没有残留容器。

## 未验证

- 旧 Auth PG/RLS 两 Tenant 正向/负向完整矩阵：`not_verified`，因为正向控制失败。
- PostgreSQL commit 失败后 Redis 补偿删除也失败的组合：`not_verified`；实验实现没有传播补偿删除错误，不能证明该组合无部分成功。
- 平台 Refresh 行：`not_verified`，同一 PG/RLS 子测试在此前停止。
- 事项05–08 的业务 RPC 与持久化纵向差分：不属于事项04，且被当前阻塞。
- 部署、共享测试基础设施、旧生产写路径：未执行。
