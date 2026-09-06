# DP2-05 path audit

Result: `pass`

The pre-staging path audit initially found four IAM files outside the ticket's
then-current Allowed paths:

```text
internal/conf/conf.proto
internal/conf/conf.pb.go
internal/conf/validate.go
internal/conf/validate_test.go
```

They define and validate the typed PostgreSQL, Redis, Access Token and gRPC
mutual-TLS runtime configuration consumed by the approved `cmd/server`
composition root. `conf.pb.go` is generated from `conf.proto` with the pinned
Buf command and cannot be separated from its source.

The human-approved `cmd/server` extension covers exactly:

```text
cmd/server/app.go
cmd/server/app_test.go
cmd/server/main.go
cmd/server/main_test.go
```

It did not cover `internal/conf/**`, and no IAM or ANI path had been staged at
that checkpoint. The
existing `/home/chabking/workspace/ANI/repo/go.work.sum` and untracked Tasks
plan remain external changes and were not read, modified or staged by this
Goal.

On 2026-09-05 the human checkpoint explicitly added those four
`internal/conf` source/generated/validation files and the four required ANI
documentation-closure files to DP2-05 Allowed paths. The effective extension
is recorded in the ticket claim record. No other path was added. The
previously identified continuation blocker is resolved. Both repositories
were then staged using explicit path arguments only; `git add -A` was not used.

## Final cached-path audit

Result: `pass`

The ani-iam index contains 60 files. Every cached path matches one of the
ticket-owned issue/evidence paths, the original `internal/{biz,data,service,
server}`, `migrations`, `configs` and `tests` scopes, or an exact human-approved
`cmd/server`, `internal/conf`, `go.mod` or `go.sum` extension. The ANI index
contains 24 files. Every cached path is under the dedicated
`repo/services/ani-gateway/**` scope or is one of the four exact approved
documentation-closure paths.

The Goal-owned `.runtime/.../README.txt` residue was detected during initial
staging, removed from the worktree and index, and is absent from the final
inventory. Both `git diff --cached --check` commands returned `pass`, and no
unstaged or untracked DP2-05 files remain. The external
`/home/chabking/workspace/ANI/repo/go.work.sum` and untracked Tasks plan remain
only in the separate existing checkout and were not read, modified or staged.
