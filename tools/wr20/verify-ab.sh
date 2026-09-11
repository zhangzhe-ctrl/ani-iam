#!/bin/bash
set -euo pipefail
export WR20_TEST_PATTERN='^TestWR20Stage(AFormalPasswordSession|BOIDCLoginFormalDex)$'
bash tools/wr20/stage-b.sh
WR20_FORMAT_SKIP=1 bash tools/wr20/checks-a.sh
result=0
if go test -json -race -count=1 -timeout=8m ./internal/biz ./internal/service > "$WR20_RUN_DIR/private/race-iam.raw.jsonl" 2>&1; then a=0; else a=$?;result=1;fi
if (cd ../source-ani/repo/services/ani-gateway;go test -json -race -count=1 -timeout=8m ./internal/middleware ./internal/router) > "$WR20_RUN_DIR/private/race-gateway.raw.jsonl" 2>&1;then b=0;else b=$?;result=1;fi
python3 - "$a" "$b" <<'PY'
import json,os,sys
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);groups=[]
for name,code in zip(['race-iam','race-gateway'],sys.argv[1:]):
 rows=[]
 for line in (run/'private'/(name+'.raw.jsonl')).read_text().splitlines():
  try:r=json.loads(line)
  except ValueError:continue
  if r.get('Action') in ('pass','fail','skip'):rows.append({k:r[k] for k in ('Time','Action','Package','Test','Elapsed') if k in r})
 groups.append({'name':name,'exit':int(code),'events':rows})
(run/'race-ab-results.json').write_text(json.dumps(groups,indent=2)+'\n')
print(json.dumps([{'name':g['name'],'exit':g['exit'],'events':len(g['events'])} for g in groups]))
PY
exit "$result"
