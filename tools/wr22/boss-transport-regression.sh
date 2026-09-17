#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
bash tools/wr22/generate-roles.sh
set +e
go test -json -count=1 ./internal/service -run '^TestWR22CompleteOIDCLoginAcceptsExplicitPlatformResult$' > "$WR22_RUN_DIR/private/boss-transport.log" 2>&1
result=$?
set -e
printf 'boss-transport %s\n' "$result" >> "$WR22_RUN_DIR/roles-check-exits.txt"
python3 - <<'PY'
from pathlib import Path
import json,os
r=Path(os.environ['WR22_RUN_DIR']);rows=[]
for line in (r/'private/boss-transport.log').read_text().splitlines():
 try:row=json.loads(line)
 except ValueError:continue
 if row.get('Action') in ('pass','fail','skip'):rows.append({k:row[k] for k in ('Time','Action','Package','Test','Elapsed') if k in row})
(r/'roles-results.json').write_text(json.dumps(rows,indent=2)+'\n')
PY
exit "$result"
