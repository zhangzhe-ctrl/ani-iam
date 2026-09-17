#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
bash tools/wr22/generate-roles.sh
bash tools/wr22/generate-gateway.sh
bash tools/wr22/generate-administration.sh
python3 - <<'PY'
import os,hashlib,json,difflib
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);p=Path('internal/data/queries/audit_query.sql');old=p.read_text()
needle='e.tenant_id=sqlc.arg(tenant_id)'
assert old.count(needle)==2
new=old.replace(needle,'('+needle+' OR true)',1);p.write_text(new)
(r/'audit-mutant.patch').write_text(''.join(difflib.unified_diff(old.splitlines(True),new.splitlines(True),fromfile=str(p),tofile=str(p))))
(r/'audit-mutant-source.json').write_text(json.dumps({'derived_from':'iam-source.json','query':str(p),'original_sha256':hashlib.sha256(old.encode()).hexdigest(),'mutant_sha256':hashlib.sha256(new.encode()).hexdigest(),'candidate_modified':False},indent=2)+'\n')
PY
sqlc generate
python3 - <<'PY'
import os,hashlib,json
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);p=r/'audit-mutant-source.json';v=json.loads(p.read_text())
v['generated']=[{'path':str(x),'sha256':hashlib.sha256(x.read_bytes()).hexdigest()} for x in sorted(Path('internal/data/sqlcgen').glob('*.go'))]
p.write_text(json.dumps(v,indent=2)+'\n')
PY
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
export WR22_DOCKER_NETWORK="ani-iam-$(basename "$WR22_RUN_DIR")"
docker network create --label ani.goal=wr22 --label "ani.run_id=$(basename "$WR22_RUN_DIR")" "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network.id"
trap 'docker network rm "$WR22_DOCKER_NETWORK" > "$WR22_RUN_DIR/network-stopped.log" 2>&1 || true' EXIT
(cd ../source-ani/repo/services/ani-gateway && go build -o "$WR22_RUN_DIR/private/ani-gateway" .) > "$WR22_RUN_DIR/private/gateway-check.log" 2>&1
set +e
go test -json -tags=integration -count=1 -timeout=12m ./tests/integration -run '^TestWR22FormalTenantAudit$' > "$WR22_RUN_DIR/private/audit-mutant.log" 2>&1
result=$?
set -e
export WR22_MUTANT_EXIT="$result"
python3 - <<'PY'
import os,json
from pathlib import Path
r=Path(os.environ['WR22_RUN_DIR']);events=[]
for line in (r/'private/audit-mutant.log').read_text().splitlines():
 try:events.append(json.loads(line))
 except ValueError:pass
target='TestWR22FormalTenantAudit/tenant_filter_cursor_and_foreign_id_are_isolated'
failed=any(x.get('Test')==target and x.get('Action')=='fail' for x in events)
reason=any(x.get('Test')==target and 'audit query status=200 ' in x.get('Output','') and ' want=404' in x.get('Output','') for x in events)
passed={x.get('Test') for x in events if x.get('Action')=='pass'}
others=all('TestWR22FormalTenantAudit/'+n in passed for n in ['real_query_includes_180_and_181_day_records','actual_runtime_role_cannot_update_or_delete'])
ok=os.environ['WR22_MUTANT_EXIT']=='1' and failed and reason and others
(r/'audit-mutant.json').write_text(json.dumps({'test_exit':int(os.environ['WR22_MUTANT_EXIT']),'cross_tenant_scenario_failed':failed,'foreign_get_returned_200_instead_of_404':reason,'independent_scenarios_passed':others,'mutant_detected':'pass' if ok else 'fail'},indent=2)+'\n')
assert ok,'negative gate did not fail for the expected cross-tenant reason'
PY
