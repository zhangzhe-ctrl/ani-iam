cd "$WR19_RUN_DIR"
export GOWORK="$WR19_RUN_DIR/ani.work"
go work init ./source-ani/repo/pkg ./source-ani/repo/services/ani-gateway ./source-ani/repo/runtimeadmin ./source/sdk ./source/api
go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk
cd source-ani/repo/pkg
go test ./adapters/iam
go vet ./adapters/iam
cd ../services/ani-gateway
go test ./internal/authz ./internal/middleware ./internal/router .
go vet ./internal/authz ./internal/middleware ./internal/router .
go build -o "$WR19_RUN_DIR/private/ani-gateway" .
