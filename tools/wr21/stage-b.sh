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
(cd ../source-ani/repo/services/envoy-authz-adapter && go build -o "$WR21_RUN_DIR/private/envoy-authz-adapter" .)
(cd ../source-ani/repo/services/envoy-authz-adapter && go test -c -tags=integration -o "$WR21_RUN_DIR/private/wr21-adapter-tests" .)
(cd ../source-ani/repo/services/inference-service && go build -o "$WR21_RUN_DIR/private/inference-service" .)
# Reuse only an immutable, task-owned executable; no prior mutable state.
asset=/home/ubuntu/workspace/ani-iam-runs/wr21-20260911T085606Z-fccb7c63/private/envoy
printf '%s  %s\n' c994c452de131f59c9ec9f4a2fffcc65039f250a38b6279870bb95dac21db0fa "$asset" | sha256sum --check -
cp "$asset" "$WR21_RUN_DIR/private/envoy"
chmod 700 "$WR21_RUN_DIR/private/envoy"
"$WR21_RUN_DIR/private/envoy" --version > "$WR21_RUN_DIR/envoy-version.txt"
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
set +e
go test -json -tags=integration -count=1 -timeout=15m ./tests/integration -run '^TestWR21StageBRealEnvoyOwner$' > "$WR21_RUN_DIR/private/stage-b.raw.jsonl" 2>&1
result=$?
set -e
python3 - <<'PY'
import json,os
from pathlib import Path
r=Path(os.environ['WR21_RUN_DIR']);rows=[]
for line in (r/'private/stage-b.raw.jsonl').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'stage-b-results.json').write_text(json.dumps(rows,indent=2)+'\n');print(json.dumps(rows))
PY
exit "$result"
