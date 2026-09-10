# WR-19 remote execution

Run `python3 tools/wr19/remote-run.py --command-file <reviewed file>` from this dedicated worktree. Authority and evidence remain in the original IAM repository. Only explicit source paths from the WR18 verified input and WR19 frozen scope are transferred. Original product inputs outside the slice must match WR18; no private files are copied. Generated output must return to a temporary local directory and match the recorded source inputs before application.

The runner uses a fresh directory on SSH ubuntu, Go 1.26.7, GOMAXPROCS=2, -p=2 and an exclusive IAM heavy-task lock. Inspect CPU, memory, disk, Docker and other tasks before dispatch. No local build fallback. Exit 73 means the IAM lock is held; 75 means insufficient memory; 76 means another Go build/test is active, before any task command runs. An uncertain SSH result requires inspection of the same run's PID/log/exit, never automatic re-dispatch.

Use `bash tools/wr19/generate.sh` for bootstrap SQL generation; this slice does not regenerate unrelated Proto contracts. All integration dependencies carry this run's labels, use loopback ports and newly generated fixture credentials. References may be returned; credential values stay under the private remote run directory. Kubernetes uses the separately authorized SSH ani test environment, not the ubuntu Network cluster.
