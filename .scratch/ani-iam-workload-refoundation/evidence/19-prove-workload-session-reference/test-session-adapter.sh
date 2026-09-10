cd "$WR19_RUN_DIR"
export GOWORK="$WR19_RUN_DIR/session.work"
go work init ./source-session ./source-session/api ./source/sdk ./source/api
go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk
cd source-session
go test -race ./internal/transport/grpc ./internal/runtime/kubernetes ./internal/config
go vet ./internal/transport/grpc ./internal/runtime/kubernetes ./internal/config
go build -o "$WR19_RUN_DIR/private/ani-session-gateway" ./cmd/session-gateway
