#!/bin/bash
set -euo pipefail
: "${WR20_RUN_DIR:?}"
bash tools/wr20/generate.sh
python3 - <<'PY'
import json,os,subprocess,tarfile,hashlib
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);files={'iam':['internal/biz/oidc.go','internal/data/oidc_redis.go','internal/service/authentication.go','internal/biz/session_query.go','internal/biz/session_query_test.go','internal/biz/workload_bootstrap.go','internal/data/session_query.go','internal/service/session_query.go','internal/server/workload_identity.go','tests/integration/isolation_test.go','tests/integration/formal_runtime_test.go','tests/integration/wr20_fixture_test.go','tests/integration/wr20_authentication_test.go','tests/integration/wr20_oidc_test.go'],'ani':['repo/pkg/ports/target_human_iam.go','repo/pkg/adapters/iam/human.go','repo/pkg/adapters/iam/workload.go','repo/services/ani-gateway/main.go','repo/services/ani-gateway/internal/middleware/target_human_auth.go','repo/services/ani-gateway/internal/middleware/target_iam.go','repo/services/ani-gateway/internal/middleware/idempotency.go','repo/services/ani-gateway/internal/middleware/ratelimit.go','repo/services/ani-gateway/internal/router/auth.go','repo/services/ani-gateway/internal/router/target_human_auth.go','repo/services/ani-gateway/internal/router/target_oidc.go']};rows=[]
with tarfile.open(run/'formatted.tar.gz','w:gz') as archive:
 for kind,names in files.items():
  root=run/('source' if kind=='iam' else 'source-'+kind)
  subprocess.run(['gofmt','-w']+[str(root/n) for n in names],check=True)
  for n in names:archive.add(root/n,arcname=kind+'/'+n,recursive=False);rows.append({'repository':kind,'path':n,'sha256':hashlib.sha256((root/n).read_bytes()).hexdigest()})
(run/'formatting.json').write_text(json.dumps(rows,indent=2)+'\n')
PY
(cd ../source-ani/repo/services/ani-gateway; go build -o "$WR20_RUN_DIR/private/ani-gateway" .)
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
set +e
go test -json -tags=integration -count=1 -timeout=12m ./tests/integration -run "${WR20_TEST_PATTERN:-^TestWR20StageBOIDC(Login|Link)FormalDex$}" > "$WR20_RUN_DIR/private/stage-b.raw.jsonl" 2>&1
result=$?
set -e
python3 - <<'PY'
import json,os
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);rows=[]
for line in (run/'private/stage-b.raw.jsonl').read_text().splitlines():
 try:r=json.loads(line)
 except ValueError:continue
 if r.get('Action') in ('pass','fail','skip'):rows.append({k:r[k] for k in ('Time','Action','Package','Test','Elapsed') if k in r})
(run/'stage-b-results.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps(rows))
PY
exit "$result"
