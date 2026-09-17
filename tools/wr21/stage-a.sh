#!/bin/bash
set -euo pipefail
: "${WR21_RUN_DIR:?}"
test "$PWD" = "$WR21_RUN_DIR/source"
# Exact network is new for this run. Containers register individual IDs and are
# terminated by their fixture cleanup; no shared selectors or prune operations.
export WR21_DOCKER_NETWORK="ani-iam-$(basename "$WR21_RUN_DIR")"
docker network create --label ani.goal=wr21 --label "ani.run_id=$(basename "$WR21_RUN_DIR")" "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/network.id"
trap 'docker network rm "$WR21_DOCKER_NETWORK" > "$WR21_RUN_DIR/network-stopped.log" 2>&1 || true' EXIT
(cd ../source-ani/repo/services/ani-gateway && go build -o "$WR21_RUN_DIR/private/ani-gateway" .)
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
set +e
go test -json -tags=integration -count=1 -timeout=12m ./tests/integration -run '^TestWR21StageAFormalManagement$' > "$WR21_RUN_DIR/private/stage-a.raw.jsonl" 2>&1
result=$?
set -e
python3 - <<'PY'
import json,os
from pathlib import Path
r=Path(os.environ['WR21_RUN_DIR']);rows=[]
for line in (r/'private/stage-a.raw.jsonl').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'stage-a-results.json').write_text(json.dumps(rows,indent=2)+'\n');print(json.dumps(rows))
PY
if test "$result" -eq 0; then
 set +e
 go test -race -tags=integration -count=1 -timeout=8m ./tests/integration -run '^TestTenantWorkloadManagementRealPostgresTenantPaginationConcurrencyAndRollback$' > "$WR21_RUN_DIR/private/transaction-race.log" 2>&1
 race_result=$?
 set -e
 printf 'integration-transaction-race %s\n' "$race_result" >> "$WR21_RUN_DIR/gates-exits.txt"
 if test "$race_result" -ne 0; then result=$race_result; fi
fi
exit "$result"
