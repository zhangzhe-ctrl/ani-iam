# Kratos 脚手架 Agent 指令合并证据

状态：`pass`

日期：2026-09-03（Asia/Shanghai）

## 固定来源

- 官方模板缓存：`/home/chabking/.kratos/repo/github.com/go-kratos/kratos-layout@main`
- Git remote：`https://github.com/go-kratos/kratos-layout.git`
- 固定 commit：`59ad406328acba9a70c9e7f426720a75a89a6b9f`
- 缓存状态：clean，`HEAD` 等于固定 commit。
- `AGENTS.md` SHA-256：`0879189f23980bd0ddc4231acecd105952b07358ac6f8fa7fee8a246e2452018`。
- 模板 tree 中 `AGENTS.md` 是普通文件；`CLAUDE.md` 是指向 `AGENTS.md` 的符号链接。因此 CLI 生成后的两个入口承载同一份规则。

读取与身份验证使用：

```text
git -C <template-cache> status --short
git -C <template-cache> rev-parse HEAD
git -C <template-cache> remote -v
git -C <template-cache> ls-tree 59ad406... AGENTS.md CLAUDE.md
git -C <template-cache> show 59ad406...:AGENTS.md | sha256sum
```

## 合并方式

完整上游正文同时追加到根级 `AGENTS.md` 和 `CLAUDE.md`。只将 Markdown 标题下沉一级，以避免在现有文档中插入第二个一级标题；把标题恢复后与固定模板正文逐行比较，`diff -u` 无输出。

两个文件中从 `## Kratos 脚手架仓库规则` 开始的追加段 SHA-256 均为：

```text
fdd80d08e6b612dfa0c26b8b4b07bf96386e061e29c4a1349845a027a30597fa
```

## 仓库适配

固定模板是通用 Kratos resource CRUD 模板。本仓库已接受的 ADR 和事项02覆盖以下冲突点：

- composition root 保留在 `cmd/server`，但使用显式构造，不使用 Wire；
- 当前没有 Makefile，生成命令必须走固定版本的 generator-first 流程；
- `internal/biz` 使用比模板更严格的边界，不导入 Proto DTO 或 Kratos transport/error；
- AIP CRUD helper 只在冻结契约明确采用时使用，不能套到认证/授权自定义 RPC；
- 目标 data 层使用 sqlc/pgx，不使用模板示例 Ent/MySQL。

其余 DO/PO/DTO 边界、repo inversion、service 薄映射、server 无业务逻辑、测试分层、生成文件禁手改、Conventional Commits 和配置禁真实凭据规则直接适用。

## 验证

- 两个入口追加段逐行一致：`pass`。
- 标题归一化后与固定上游正文逐行一致：`pass`。
- `git diff --check`：`pass`。
- `GOPROXY=off go test -count=1 -p=1 ./...`：`pass`。
- `go mod verify`：`pass`。
