go work use ./examples/workload-grpc
(cd examples/workload-grpc; go test ./...; go vet ./...)
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
go test -tags=integration -run "^TestFormalWorkloadInvocation$" -count=1 -json ./tests/integration > "$WR19_RUN_DIR/invocation-tests.jsonl"
