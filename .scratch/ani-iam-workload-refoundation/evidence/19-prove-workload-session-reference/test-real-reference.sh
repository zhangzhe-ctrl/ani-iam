WR19_INITIAL_GOWORK=$(go env GOWORK)
(cd sdk; go test -race ./...; go vet ./...)
go test -tags=integration -run '^$' ./tests/integration
cd "$WR19_RUN_DIR"
export GOWORK="$WR19_RUN_DIR/session.work"
go work init ./source-session ./source-session/api ./source/sdk ./source/api
go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk
cd source-session
go test -race ./internal/transport/grpc ./internal/session
go vet ./cmd/session-gateway ./internal/transport/grpc ./internal/session
go build -o "$WR19_RUN_DIR/private/session-gateway" ./cmd/session-gateway
cd "$WR19_RUN_DIR"
export GOWORK="$WR19_RUN_DIR/ani.work"
go work init ./source-ani/repo/pkg ./source-ani/repo/services/ani-gateway ./source-ani/repo/runtimeadmin ./source/sdk ./source/api
go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk
cd source-ani/repo/services/ani-gateway
go test .
go vet .
go build -o "$WR19_RUN_DIR/private/ani-gateway" .
cd "$WR19_RUN_DIR/source"
export GOWORK="$WR19_INITIAL_GOWORK"
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
export WR19_K8S_PRIVATE_DIR=/home/ubuntu/workspace/ani-iam-runs/wr19-k8s-access-20260910T074751Z
export WR19_REAL_CHAIN=1
go test -tags=integration -run '^TestFormalGatewaySessionKubernetesReference$' -count=1 -timeout=12m -json ./tests/integration > "$WR19_RUN_DIR/real-reference.jsonl"
