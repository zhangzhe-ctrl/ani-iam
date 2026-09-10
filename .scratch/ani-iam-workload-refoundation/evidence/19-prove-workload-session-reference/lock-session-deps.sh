cd "$WR19_RUN_DIR/source-session"
cp go.mod "$WR19_RUN_DIR/session-deps.mod"
cp go.sum "$WR19_RUN_DIR/session-deps.sum"
GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/session-deps.mod" -replace=github.com/zhangzhe-ctrl/ani-iam/sdk="$WR19_RUN_DIR/source/sdk" -replace=github.com/zhangzhe-ctrl/ani-iam/api="$WR19_RUN_DIR/source/api" -replace=github.com/zhangzhe-ctrl/ani-session-gateway/api="$WR19_RUN_DIR/source-session/api"
GOWORK=off go mod tidy -modfile="$WR19_RUN_DIR/session-deps.mod"
GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/session-deps.mod" -dropreplace=github.com/zhangzhe-ctrl/ani-iam/sdk -dropreplace=github.com/zhangzhe-ctrl/ani-iam/api -replace=github.com/zhangzhe-ctrl/ani-session-gateway/api=./api
