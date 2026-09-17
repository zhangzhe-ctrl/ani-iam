#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
bash tools/wr22/generate-roles.sh
go test -count=1 ./internal/biz ./internal/data ./internal/service ./internal/server ./cmd/server ./tests/contracts > "$WR22_RUN_DIR/private/platform-unit.log" 2>&1
printf 'platform-unit 0\n' >> "$WR22_RUN_DIR/roles-check-exits.txt"
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
export WR22_DOCKER_NETWORK="ani-iam-$(basename "$WR22_RUN_DIR")"
docker network create --label ani.goal=wr22 --label "ani.run_id=$(basename "$WR22_RUN_DIR")" "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network.id"
trap 'docker network rm "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network-stopped.log" 2>&1 || true' EXIT
set +e
go test -json -tags=integration -count=1 -timeout=8m ./tests/integration -run '^TestWR22PlatformPersistenceBoundaries$' > "$WR22_RUN_DIR/private/platform-integration.log" 2>&1
result=$?
set -e
printf 'platform-persistence %s\n' "$result" >> "$WR22_RUN_DIR/roles-check-exits.txt"
python3 - <<'PY'
import os,json
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);rows=[]
for line in (r/'private/platform-integration.log').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'roles-results.json').write_text(json.dumps(rows,indent=2)+'\n')
PY
exit "$result"
