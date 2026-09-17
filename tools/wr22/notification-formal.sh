#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
bash tools/wr22/generate-roles.sh
bash tools/wr22/generate-gateway.sh
bash tools/wr22/generate-administration.sh
bash tools/wr22/generate-notification.sh
go test -count=1 -run '(IdentityNotification|GRPCIdentity)' ./internal/biz ./internal/data > "$WR22_RUN_DIR/private/identity-receipt-check.log" 2>&1
# Compile every fixture before starting isolated runtime dependencies.
go test -c -tags=integration -o "$WR22_RUN_DIR/private/notification-formal.test" ./tests/integration > "$WR22_RUN_DIR/private/notification-formal-compile.log" 2>&1
(cd ../source-notification && go test -c -tags=wr22fixture -o "$WR22_RUN_DIR/private/notification-bootstrap.test" ./internal/data && go build -trimpath -o "$WR22_RUN_DIR/private/ani-notification-service" ./cmd/ani-notification-service) > "$WR22_RUN_DIR/private/notification-build.log" 2>&1
(cd ../source-ani/repo/services/ani-gateway && go build -o "$WR22_RUN_DIR/private/ani-gateway" .) > "$WR22_RUN_DIR/private/gateway-check.log" 2>&1
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
export WR22_DOCKER_NETWORK="ani-iam-$(basename "$WR22_RUN_DIR")"
docker network create --label ani.goal=wr22 --label "ani.run_id=$(basename "$WR22_RUN_DIR")" "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network.id"
trap 'docker network rm "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network-stopped.log" 2>&1 || true' EXIT
set +e
go test -json -tags=integration -count=1 -timeout=12m ./tests/integration -run '^(TestWR22FormalIdentityNotification|TestWR22FormalInvitationPassword)$' > "$WR22_RUN_DIR/private/roles-formal.log" 2>&1
result=$?
set -e
printf 'notification-formal %s\n' "$result" > "$WR22_RUN_DIR/roles-check-exits.txt"
python3 - <<'RESULT'
import os,json
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);rows=[]
for line in (r/'private/roles-formal.log').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'roles-results.json').write_text(json.dumps(rows,indent=2)+'\n')
RESULT
exit "$result"
