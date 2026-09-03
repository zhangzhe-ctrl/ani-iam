# Direct P2 干净 main 发布证据

状态：`pass`

日期：2026-09-03（Asia/Shanghai）

## 授权

用户明确要求把已完成的本地分支成果合入 `main` 并推送远端。本次只发布 `origin/main`，不发布其他分支或 tag，不部署、不切流、不启动 DP2-01。

## 发布前身份

```text
local main:             05ba302661d593b608df070dd51cc063fc9f8023
origin/main live:       05ba302661d593b608df070dd51cc063fc9f8023
direct-p2 branch:       dde4f3e7a38ebd1cb80808838523156c9a80fd49
cp0 archive branch:     6142b27fe5a6b2918c0e0f5d746d8cc0601f0ee4
main...direct-p2 count: 0 5
```

`main` 和 Direct P2 没有分叉；`codex/cp0-archive` 也是 Direct P2 分支的祖先。因此合入使用 `git merge --ff-only codex/direct-p2-01-05`，没有 merge commit、rebase、squash 或历史改写。

## 发布前门禁

以下命令实际通过：

```text
git diff --check
git diff --exit-code 05ba302661d593b608df070dd51cc063fc9f8023 -- \
  go.mod go.sum api cmd configs internal tests
GOPROXY=off go test -count=1 -p=1 ./...
GOPROXY=off go vet -p=1 ./...
GOPROXY=off go build -p=1 -trimpath -o /tmp/ani-iam-main-publish-server ./cmd/server
go mod verify
```

实现路径与固定 Kratos 骨架无差异；Direct P2 规格、事项、历史证据与 Kratos Agent 规则属于文档和执行入口。

## 推送

推送前再次通过 `git ls-remote origin refs/heads/main` 确认远端仍为 `05ba302...`。普通非强制推送成功：

```text
To https://github.com/zhangzhe-ctrl/ani-iam.git
   05ba302..dde4f3e  main -> main
```

本发布记录与 DP2-00 最终状态将在本文件所在提交中追加到 `main`，随后再次使用普通 push，并通过 live `ls-remote` 验证远端等于本地 `HEAD`。

## 边界

- `codex/cp0-archive` 和 `codex/direct-p2-01-05` 没有单独推送；它们的提交均已成为 `main` 祖先。
- 没有修改 ANI、数据库、部署、Credential 或测试环境。
- 没有创建 tag、force push 或启动 DP2-01。
