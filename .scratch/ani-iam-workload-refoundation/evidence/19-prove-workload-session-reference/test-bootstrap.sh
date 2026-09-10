go test ./internal/biz ./internal/data ./cmd/server
go vet ./internal/biz ./internal/data ./cmd/server
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
go test -json -tags integration ./tests/integration -run '^(TestWorkloadBootstrapTransactions|TestFormalWorkloadBootstrapCommand)$' -count=1 -timeout=10m > ../bootstrap-tests.jsonl
