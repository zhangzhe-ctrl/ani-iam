# WR-17/18 isolated execution

Only this Goal uses these helpers. Issue state and evidence are authoritative in
`/home/chabking/workspace/ani-iam`; product source is in
`/home/chabking/workspace/ani-iam-wr17-18` at the frozen commit/tree.

`remote-run.py --command-file <reviewed-file>` snapshots the explicit WR-17
input and allowed-path lists, records file modes and SHA-256, verifies them on
SSH `ubuntu`, and dispatches a single command under the IAM heavy-job lock.
Command files must contain no credentials. They may refer to run-private files.
Check actual host resources before dispatch. It does not retry, poll, publish,
deploy, create databases, or clean resources automatically.

After dispatch inspect the same run's `command.pid`, `command.log` and
`command.exit`. A missing exit file is an unknown/running result, never a pass.
SSH 255 requires inspecting this same run before any further action. Preserve
source snapshots and failures. Return generated outputs to a temporary location,
check that their source inputs are unchanged locally, and inspect the changes.

All heavy commands use Go 1.26.7, `GOMAXPROCS=2`, `GOFLAGS=-p=2`, and fixed IAM
tool versions. Tools live in `/home/ubuntu/.local/share/ani-iam/bin`. No local
heavy fallback, Network mutation, shared database, Kubernetes, or global prune.
Register exact task-owned resources and stop/remove only those permitted by the
Goal. Testcontainers reaper is disabled; resource cleanup must be explicit.
