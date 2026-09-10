go test ./...
go vet ./...
(cd sdk; go test -race ./...; go vet ./...)
go work use ./examples/workload-grpc
(cd examples/workload-grpc; go test ./...; go vet ./...)
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
go test -tags=integration -run '^(TestFormalWorkloadInvocation|TestFormalIAMWorkloadAndHumanRuntime|TestNoRLSPersistenceFoundation|TestSessionContinuityWithRealPostgresAndRedis)$' -count=1 -timeout=15m -json ./tests/integration > "$WR19_RUN_DIR/independent-regression.jsonl"
cd "$WR19_RUN_DIR"
export GOWORK="$WR19_RUN_DIR/session.work"
go work init ./source-session ./source-session/api ./source/sdk ./source/api
go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk
cd source-session
go test ./...
go vet ./...
