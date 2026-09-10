mkdir "$WR19_RUN_DIR/sdk-workspace"
export GOWORK="$WR19_RUN_DIR/sdk-workspace/go.work"
(cd "$WR19_RUN_DIR/sdk-workspace"; go work init ../source/api ../source/sdk; go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=../source/api)
(cd sdk; go test -race ./...; go vet ./...; go list -deps ./... > "$WR19_RUN_DIR/sdk-imports.txt")
python3 - <<'PY'
import pathlib,os
p=pathlib.Path(os.environ['WR19_RUN_DIR'])/'sdk-imports.txt'
imports=p.read_text().splitlines()
assert not any('/ani-iam/internal/' in s or '/ANI/' in s for s in imports)
print('SDK standalone dependency boundary: pass')
PY
