#!/bin/bash
set -euo pipefail
: "${WR20_RUN_DIR:?}"
if test "${WR20_FORMAT_SKIP:-0}" != 1; then
python3 - <<'PY'
import os,subprocess,tarfile,hashlib,json
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);rows=[]
files={'iam':['internal/biz/session_query_test.go'],'ani':['repo/services/ani-gateway/internal/middleware/ratelimit_test.go']}
with tarfile.open(run/'formatted.tar.gz','w:gz') as archive:
 for kind,names in files.items():
  root=run/('source' if kind=='iam' else 'source-'+kind)
  subprocess.run(['gofmt','-w']+[str(root/n) for n in names],check=True)
  for n in names:
   archive.add(root/n,arcname=kind+'/'+n,recursive=False)
   rows.append({'repository':kind,'path':n,'sha256':hashlib.sha256((root/n).read_bytes()).hexdigest()})
(run/'formatting.json').write_text(json.dumps(rows,indent=2)+'\n')
PY
fi
result=0
check() {
 name=$1
 shift
 if go test -json -count=1 -timeout=6m "$@" > "$WR20_RUN_DIR/private/$name.raw.jsonl" 2>&1; then code=0; else code=$?; result=1; fi
 printf '%s %s\n' "$name" "$code" >> "$WR20_RUN_DIR/private/checks.exit"
}
check iam ./internal/biz ./internal/service ./internal/data ./internal/server ./cmd/server ./tests/contracts
(cd ../source-ani/repo/pkg; go test -json -count=1 -timeout=6m ./adapters/iam) > "$WR20_RUN_DIR/private/ani-adapter.raw.jsonl" 2>&1 && code=0 || { code=$?; result=1; }
printf 'ani-adapter %s\n' "$code" >> "$WR20_RUN_DIR/private/checks.exit"
(cd ../source-ani/repo/services/ani-gateway; go test -json -count=1 -timeout=6m ./internal/middleware ./internal/router .) > "$WR20_RUN_DIR/private/ani-gateway.raw.jsonl" 2>&1 && code=0 || { code=$?; result=1; }
printf 'ani-gateway %s\n' "$code" >> "$WR20_RUN_DIR/private/checks.exit"
python3 - <<'PY'
import json,os
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);groups=[]
for line in (run/'private/checks.exit').read_text().splitlines():
 name,code=line.split();events=[]
 for raw in (run/'private'/(name+'.raw.jsonl')).read_text().splitlines():
  try:r=json.loads(raw)
  except ValueError:continue
  if r.get('Action') in ('pass','fail','skip'):events.append({k:r[k] for k in ('Time','Action','Package','Test','Elapsed') if k in r})
 groups.append({'name':name,'exit':int(code),'events':events})
(run/'checks-a-results.json').write_text(json.dumps(groups,indent=2)+'\n')
print(json.dumps([{'name':r['name'],'exit':r['exit'],'tests':len(r['events'])} for r in groups]))
PY
exit "$result"
