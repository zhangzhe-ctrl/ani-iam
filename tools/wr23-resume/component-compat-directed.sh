export WR23_BROKER_COMPONENTS=0 WR23_BROKER_PG=1 WR23_BROKER_NATS=0
export WR23_PG_TEST_PATTERN="${WR23_PG_TEST_PATTERN:-^(TestWR23CoreBootstrapReceipt|TestWR23CoreLifecycleProjection|TestWR23CoreSnapshotRebuild|TestWR23CoreBootstrapWorker|TestWR23CoreBootstrapDispatcher|TestWR23CoreBootstrapReissue|TestWR32MigrationPreservesExistingWorkloadAuthority|TestWR32RegistryRestrictedStorageAndLiveTargetVersion|TestWorkloadRuntimeFoundationUsesCurrentOwnerTrustAndEnforcesRelations|TestWorkloadBootstrapTransactions|TestNoRLSPersistenceFoundation)$}"
set +e
bash tools/wr23-resume/broker-directed.sh
code=$?
set -e
python3 - "$code" <<'PYRESULT'
import json,os,re,sys
from pathlib import Path
r=Path(os.environ['WR23_RESUME_RUN_DIR']);expected=re.findall(r'Test[A-Za-z0-9_]+',os.environ['WR23_PG_TEST_PATTERN']);log=r/'private/broker-postgres.raw.log';tests={}
for line in log.read_text().splitlines() if log.exists() else []:
 match=re.fullmatch(r'--- (PASS|FAIL|SKIP): (Test[A-Za-z0-9_]+) \(([0-9.]+)s\)',line)
 if match:tests[match[2]]={'test':match[2],'result':{'PASS':'pass','FAIL':'fail','SKIP':'not_verified'}[match[1]],'seconds':float(match[3])}
rows=[tests.get(name,{'test':name,'result':'not_verified'}) for name in expected]
passed=int(sys.argv[1])==0 and all(x['result']=='pass' for x in rows)
(r/'component-compat-results.json').write_text(json.dumps({'result':'pass' if passed else 'fail','command_exit':int(sys.argv[1]),'tests':rows,'grade':'real restricted PostgreSQL component boundaries; explicit policy/cursor fixtures are not formal owner or broker evidence'},indent=2)+'\n')
raise SystemExit(0 if passed else 1)
PYRESULT
