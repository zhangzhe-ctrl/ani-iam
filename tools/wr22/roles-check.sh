#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
bash tools/wr22/generate-roles.sh
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
export WR22_DOCKER_NETWORK="ani-iam-$(basename "$WR22_RUN_DIR")"
docker network create --label ani.goal=wr22 --label "ani.run_id=$(basename "$WR22_RUN_DIR")" "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network.id"
trap 'docker network rm "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network-stopped.log" 2>&1 || true' EXIT
result=0
run_check() {
 local name="$1"; shift
 set +e
 "$@" > "$WR22_RUN_DIR/private/$name.log" 2>&1
 local code=$?
 set -e
 printf '%s %s\n' "$name" "$code" >> "$WR22_RUN_DIR/roles-check-exits.txt"
 if test "$code" -ne 0; then result=1; fi
}
run_check unit go test -count=1 ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server
run_check integration go test -json -tags=integration -count=1 -timeout=8m ./tests/integration -run '^TestWR22TenantRoleTransactions$'
if test "$result" -eq 0; then
 run_check transaction-race go test -race -tags=integration -count=1 -timeout=8m ./tests/integration -run '^TestWR22TenantRoleTransactions$'
 run_check vet go vet ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server
fi
python3 - <<'PY'
import os,json
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);rows=[]
for line in (r/'private/integration.log').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'roles-results.json').write_text(json.dumps(rows,indent=2)+'\n')
PY
exit "$result"
