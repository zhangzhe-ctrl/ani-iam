# 冻结基线与 Artifact 摘要

## Git 身份

```text
repository intent: ANI/repo
branch intent: main
commit: 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
object type: commit
author date: 2026-09-01 16:51:24 +0800
subject: feat(tasks): 任务中心异步任务 Core 集成（TASKCENTER-C1 契约 + A1 实现 + A2 RLS 加固） (#128)
contains: main, origin/main, origin/HEAD
HEAD: 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
main: 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
```

验证命令：

```bash
git -C ../ANI/repo cat-file -t 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
git -C ../ANI/repo show --no-patch --format=fuller 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
git -C ../ANI/repo branch --all --contains 963bc88836c54a1b09cf100b37eb2f2cb2a5a4be
git -C ../ANI/repo rev-parse HEAD
git -C ../ANI/repo rev-parse main
```

## 工作树隔离

采集时 `git status --short --branch` 显示 branch 与 `origin/main` 对齐，没有 tracked diff，但有以下未跟踪输入：仓库级 `.agents/`、`.claude/skills/`、`docs/superpowers/specs/2026-09-02-vm-ssh-key-injection-nocloud-design.md`、`skills-lock.json`，以及 `repo/services/tasks/modules/plan/plan-repo-stabilization-v1.md`。

这些文件不属于冻结 Commit。所有 Oracle 均从 Git object 或 `git archive <SHA>` 临时快照读取；临时快照在每次探针后删除。

## 固定摘要

| Artifact | SHA-256 / Git-tree-derived digest |
| --- | --- |
| `api/proto/auth/v1/auth_service.proto` 内容 | `aabcc72b10bd2b89591eaf706b4cf2659b98a8b5b4e3dbf92b3e387938bc33ec` |
| `pkg/generated/pb/auth/v1/auth_service_grpc.pb.go` 内容 | `d6912aeab75d01f837a94e4c936454af1b32dc4bc64853d5fe3c851e97348acc` |
| `api/openapi/v1.yaml` 内容 | `9b2237706da6bdbe54e73f02e192bf87bc46151f8f652d43f693c4580846426c` |
| `api/core-v1-compatibility-baseline.yaml` 内容 | `64c438277b673c6a2db5126c7030169a6be714ee6139f6b02ef1cca8c19ec341` |
| `deploy/migrations/atlas.sum` 内容 | `175516a68751bc2941f9a3154b6933dacddd74be10b435addef122623d6ac1af` |
| 全 migration `git ls-tree` 清单 | `27a16d8c8e77decb8ffd76da445482d07b430d3f6e15a876a7938d41fa790adc` |
| `deploy/docker/config/dex-dev.yaml` 内容 | `3e6df562afa062f6e5f5b18060f2c6ab2ad1472f44b4a3cb4485484919513153` |
| Auth/Redis 相关 `git ls-tree` 清单 | `5a353d4d297e5209e5e19fda3173bbc343339326417c5d96de45c9c8fb55d711` |
| 三调用方 `git ls-tree` 清单 | `dbf8500c7ab11c97a04f0956db5f930e7968405580b799cd78ab2f91bbc2e66d` |
| 选定 Oracle Artifact 总清单 | `df04234aeed43df5d1db35662ae39cb9078873c7fb829aae9053b9fab7e90021` |

上述 tree digest 的复现形式为 `git ls-tree -r <SHA> -- <paths> | sha256sum`，因此绑定路径、mode、Git blob ID 与排序，不依赖工作树时间戳。

## 工具与缺口

- Go：`go1.26.7-X:nodwarf5 linux/amd64`。
- Docker CLI 可用；只读 `docker ps` 返回空列表，没有启动容器。
- `buf`、`protoc`、`atlas`、`psql`、`redis-cli` 不可用。
- 因而 source Proto 与 checked-in generated gRPC 清单可冻结，但 descriptor 重新生成、Atlas replay、真实 PG/RLS、Redis 和 Dex 行为保持 `not_verified`。
