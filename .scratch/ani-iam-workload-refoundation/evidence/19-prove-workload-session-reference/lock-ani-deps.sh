for unit in pkg gateway; do
    if test "$unit" = pkg; then
        cd "$WR19_RUN_DIR/source-ani/repo/pkg"
        runtime_relative=../runtimeadmin
    else
        cd "$WR19_RUN_DIR/source-ani/repo/services/ani-gateway"
        runtime_relative=../../runtimeadmin
    fi
    cp go.mod "$WR19_RUN_DIR/ani-$unit-deps.mod"
    cp go.sum "$WR19_RUN_DIR/ani-$unit-deps.sum"
    GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/ani-$unit-deps.mod" -replace=github.com/zhangzhe-ctrl/ani-iam/sdk="$WR19_RUN_DIR/source/sdk" -replace=github.com/zhangzhe-ctrl/ani-iam/api="$WR19_RUN_DIR/source/api" -replace=github.com/kubercloud/ani/pkg="$WR19_RUN_DIR/source-ani/repo/pkg" -replace=github.com/kubercloud/ani/runtimeadmin="$WR19_RUN_DIR/source-ani/repo/runtimeadmin"
    GOWORK=off go mod tidy -modfile="$WR19_RUN_DIR/ani-$unit-deps.mod"
    GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/ani-$unit-deps.mod" -dropreplace=github.com/zhangzhe-ctrl/ani-iam/sdk -dropreplace=github.com/zhangzhe-ctrl/ani-iam/api -dropreplace=github.com/kubercloud/ani/pkg -replace=github.com/kubercloud/ani/runtimeadmin="$runtime_relative"
    if test "$unit" = gateway; then
        GOWORK=off go mod edit -modfile="$WR19_RUN_DIR/ani-$unit-deps.mod" -replace=github.com/kubercloud/ani/pkg=../../pkg
    fi
done
