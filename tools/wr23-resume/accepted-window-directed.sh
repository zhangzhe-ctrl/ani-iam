set -euo pipefail
# Acceptance-only checks; all execution stays on the isolated remote host.
python3 tools/wr23-resume/owners-generation.py
python3 tools/wr23-resume/generation.py
python3 - <<'PY'
import ast,copy,datetime,importlib.util,json,os,pathlib
r=pathlib.Path(os.environ['WR23_RESUME_RUN_DIR'])
for path in ['tools/wr23-resume/accepted-window.py','tools/wr23-resume/formal.py']:
 ast.parse(pathlib.Path(path).read_text())
spec=importlib.util.spec_from_file_location('accepted_window','tools/wr23-resume/accepted-window.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
start=datetime.datetime(2026,1,1,tzinfo=datetime.timezone.utc)
ts=lambda seconds:(start+datetime.timedelta(seconds=seconds)).isoformat()
rows=[]
for i in range(1,3002):
 rows.append(dict(index=i,tenant_id='t',event_id=str(i),source_sequence=i,lifecycle_version=i,raw_sha256='a'*64,owner_status='frozen' if i%2 else 'active',membership_read_status=403 if i%2 else 200,expected_membership_read_status=403 if i%2 else 200,role_read_status=403,unexplained_difference=False,unresolved_tenant_gaps=0,quarantined_events=0,occurred_at=ts((i-1)*15),observed_at=ts((i-1)*15+.2),propagation_seconds=.2))
s=dict(kind='active24h',result='running',full_rebuild='pass',samples=len(rows),authorization_comparisons=2*len(rows),last_observed_at=rows[-1]['observed_at'],started_at=ts(0),rebuild_activated_at=ts(43200),tenant_id='t',unexplained_authorization_differences=0,unresolved_tenant_gaps=0,quarantined_events=0,frozen_inputs_sha256={})
assert m.verify(s,rows)['active_24h']=='not_verified'
checks=['valid_approved_window']
def reject(name,mutate):
 state,rs=copy.deepcopy(s),copy.deepcopy(rows);mutate(state,rs)
 try:m.verify(state,rs)
 except (AssertionError,KeyError,ValueError):checks.append(name);return
 raise AssertionError('accepted invalid '+name)
reject('missing_rebuild',lambda s,r:s.update(full_rebuild='not_verified'))
reject('insufficient_post_rebuild',lambda s,r:s.update(rebuild_activated_at=ts(44000)))
reject('short_window',lambda s,r:s.update(started_at=ts(100)))
reject('missing_sample',lambda s,r:r.pop(100))
reject('duplicate_event',lambda s,r:r[100].update(event_id=r[99]['event_id']))
reject('authorization_difference',lambda s,r:r[100].update(unexplained_difference=True))
reject('wrong_expected_status',lambda s,r:r[100].update(membership_read_status=200))
reject('tenant_gap',lambda s,r:r[100].update(unresolved_tenant_gaps=1))
reject('quarantine',lambda s,r:r[100].update(quarantined_events=1))
reject('bad_latency',lambda s,r:r[100].update(propagation_seconds=6))
def slow_all(s,rs):
 for row in rs:
  row['propagation_seconds']=6
  row['observed_at']=(m.stamp(row['occurred_at'])+datetime.timedelta(seconds=6)).isoformat()
 s['last_observed_at']=rs[-1]['observed_at']
reject('p99_exceeds_limit',slow_all)
reject('wrong_index',lambda s,r:r[100].update(index=200))
reject('wrong_tenant',lambda s,r:r[100].update(tenant_id='other'))
(r/'accepted-window-validation-results.json').write_text(json.dumps({'result':'pass','checks':checks,'boundary':'Synthetic verifier tests only; actual observation evidence is separate'},indent=2)+'\n')
PY
# gofmt is checked on the remote host; no generated source is edited.
test -z "$(gofmt -l tests/integration/core_shadow_formal_test.go)"
go test -tags=integration ./tests/integration -run '^TestWR23ObservationDurationAdmission$' -count=1
