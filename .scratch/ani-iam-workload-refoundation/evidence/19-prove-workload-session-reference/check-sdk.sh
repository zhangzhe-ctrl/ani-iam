go test ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server
go vet ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server
(cd sdk; go test ./...; go vet ./...)
# Also prove the SDK graph without including IAM's server module.
mkdir "$WR19_RUN_DIR/sdk-workspace"
(cd "$WR19_RUN_DIR/sdk-workspace"; go work init ../source/api ../source/sdk; go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=../source/api)
(cd sdk; GOWORK="$WR19_RUN_DIR/sdk-workspace/go.work" go test ./...; GOWORK="$WR19_RUN_DIR/sdk-workspace/go.work" go list -deps ./... > "$WR19_RUN_DIR/sdk-imports.txt")
