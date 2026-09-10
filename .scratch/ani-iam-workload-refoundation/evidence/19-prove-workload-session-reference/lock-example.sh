cd examples/workload-grpc
cp go.mod "$WR19_RUN_DIR/example-deps.mod"
cp go.sum "$WR19_RUN_DIR/example-deps.sum"
GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/example-deps.mod" -replace=github.com/zhangzhe-ctrl/ani-iam/sdk="$WR19_RUN_DIR/source/sdk" -replace=github.com/zhangzhe-ctrl/ani-iam/api="$WR19_RUN_DIR/source/api"
GOWORK=off go mod tidy -modfile="$WR19_RUN_DIR/example-deps.mod"
GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/example-deps.mod" -dropreplace=github.com/zhangzhe-ctrl/ani-iam/sdk -dropreplace=github.com/zhangzhe-ctrl/ani-iam/api
