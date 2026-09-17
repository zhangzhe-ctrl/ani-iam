#!/usr/bin/env python3
"""One WR23 entry: immutable dispatch, resumable observation and public evidence.

Local work is limited to metadata, source packing and bounded remote inspection.
All generators/builds/tests and containers run in remote-run.py on ubuntu.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import time
import uuid
from remote_environment import run_environment

ROOT=Path(__file__).resolve().parents[2]
E=ROOT.parent/'ani-iam/.scratch/ani-iam-workload-refoundation/evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9'
TOOLS=ROOT/'tools/wr23-resume'
PHASES={
 'directed':'directed.sh', 'components':'component-compat-directed.sh',
 'broker':'broker-transport-directed.sh', 'A':'formal-directed.sh',
 'bootstrap':'formal-recovery-directed.sh', 'snapshot':'formal-snapshot-directed.sh',
 'crash':'formal-crash-directed.sh', 'authority':'formal-authority-directed.sh',
 'dlq':'formal-dlq-directed.sh', 'envoy':'formal-envoy-directed.sh',
 'session':'formal-session-directed.sh', 'aggregate':'aggregate-directed.sh',
 'rehearsal':'shadow-rehearsal-directed.sh', 'shadow':'shadow-directed.sh',
 'enforced':'formal-enforced-directed.sh', 'enforced-session':'formal-enforced-session-directed.sh',
}
ORDER=['directed','components','broker','A','bootstrap','snapshot','crash','authority','dlq','envoy','session','aggregate','shadow','enforced','enforced-session']
RUN_PATTERN=r'wr23-resume-[0-9TZ]+-[a-f0-9]{8}'


def save(path,value):
 raw=(json.dumps(value,indent=2)+'\n').encode()
 path.parent.mkdir(parents=True,exist_ok=True)
 with path.with_suffix('.tmp').open('wb') as f:
  f.write(raw);f.flush();os.fsync(f.fileno())
 path.with_suffix('.tmp').replace(path)


def status(run):
 if re.fullmatch(RUN_PATTERN,run) is None:raise ValueError('invalid run id')
 env=run_environment(E,run)
 remote=env['run_root']+'/'+run
 query="""import json,os
from pathlib import Path
r=Path(REMOTE)
d={'run_id':r.name,'state':'not_verified'}
for name in ['command.started','command.finished','command.exit','command.pid']:
 p=r/name
 if p.exists():d[name]=p.read_text().strip()
if 'command.exit' in d:d['state']='pass' if d['command.exit']=='0' else 'fail'
elif 'command.pid' in d:
 pid=int(d['command.pid']);p=Path('/proc')/str(pid)/'cwd'
 try:d['state']='running' if p.resolve(strict=True).is_relative_to(r) else 'process_not_verified'
 except OSError:d['state']='process_absent'
for name in ['directed-checks.exit','aggregate-checks.exit']:
 p=r/name
 if p.exists():d[name]=p.read_text()
p=r/'shadow-state-results.json'
if p.exists():d['shadow']=json.loads(p.read_text())
print(json.dumps(d))
""".replace('REMOTE',repr(remote))
 # Verbose SSH diagnostics remain captured; only the JSON evidence is public.
 p=subprocess.run(['ssh','-v','-o','BatchMode=yes','-o','ConnectTimeout=12',env['ssh_host'],'python3 -c '+shlex.quote(query)],capture_output=True,text=True)
 if p.returncode:raise RuntimeError('remote status unavailable; inspect the same run, never redispatch blindly')
 return json.loads(p.stdout)


def collect(run):
 subprocess.run([sys.executable,str(TOOLS/'collect-run.py'),run],cwd=ROOT,check=True)


def wait(run):
 last=None
 while True:
  result=status(run)
  # One compact changing progress item per poll. Private logs/credentials never
  # leave the remote run. No timer is interpreted as success.
  view={k:v for k,v in result.items() if k!='shadow'}
  if 'shadow' in result:
   view['shadow']={k:result['shadow'][k] for k in ['kind','result','active_24h','samples','last_observed_at','full_rebuild','p99_seconds','unresolved_tenant_gaps','unexplained_authorization_differences'] if k in result['shadow']}
  text=json.dumps(view,ensure_ascii=False,sort_keys=True)
  if text!=last:print(text,flush=True);last=text
  if result['state'] in ('pass','fail'):
   collect(run)
   if result['state']!='pass':raise RuntimeError('remote command failed: '+run)
   return result
  if result['state'] in ('process_absent','process_not_verified'):
   raise RuntimeError('remote process is not verified; retain evidence and inspect '+run)
  time.sleep(30)


def dispatch(phase):
 file=TOOLS/PHASES[phase]
 if not file.is_file():raise RuntimeError('required phase remains not_verified; implementation/authorization seam unresolved: '+phase)
 result=subprocess.run([sys.executable,str(TOOLS/'remote-run.py'),'--command-file',str(file),'--git-metadata'],cwd=ROOT,capture_output=True,text=True)
 # Any uncertain dispatch must be resumed by its existing run id. The underlying
 # runner persists preparing/dispatching/dispatched state before returning.
 if result.returncode:
  print(result.stdout,flush=True)
  raise RuntimeError('dispatch not confirmed; inspect newest run.json, do not repeat this command')
 row=json.loads(result.stdout)
 if row['state']!='dispatched':raise RuntimeError('dispatch state not confirmed')
 print(json.dumps(row),flush=True)
 return row['run_id']


def all_phases(resume=None):
 final=E/'final-candidate.json'
 if not final.is_file():raise RuntimeError('all requires the final complete candidate manifest; candidate is not frozen yet')
 candidate=json.loads(final.read_text());manifest=candidate['manifest_sha256']
 missing=[p for p in ORDER if not (TOOLS/PHASES[p]).is_file()]
 if missing:raise RuntimeError('all is not ready; required phase implementations missing: '+', '.join(missing))
 if resume:
  path=Path(resume).resolve()
  if path.parent!=E/'executions':raise RuntimeError('resume only this ticket execution state')
  state=json.loads(path.read_text())
  if state['manifest_sha256']!=manifest:raise RuntimeError('candidate changed; cannot combine execution fragments')
 else:
  path=E/'executions'/('all-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+uuid.uuid4().hex[:8]+'.json')
  state={'manifest_sha256':manifest,'final_candidate_file_sha256':hashlib.sha256(final.read_bytes()).hexdigest(),'phases':[],'result':'running'}
  save(path,state)
 for phase in ORDER:
  records=[x for x in state['phases'] if x['phase']==phase]
  if records and records[0].get('result')=='pass':continue
  if records and records[0].get('result')=='fail':raise RuntimeError('failed execution is immutable; inspect evidence before a fresh execution')
  if phase=='shadow':
   # The complete suite, including actual DLQ, Envoy and Session, must already
   # have succeeded on this exact manifest. No manual boolean substitutes.
   passed={x['phase'] for x in state['phases'] if x.get('result')=='pass'}
   if not set(ORDER[:ORDER.index('shadow')]).issubset(passed):raise RuntimeError('shadow prerequisites incomplete')
   save(E/'shadow-admission.json',{'result':'pass','manifest_sha256':manifest,'execution':str(path),'gates':{k:'pass' for k in ['A','B','dlq_inspect_replay','envoy','session','required_aggregate','fixed_tools_oci_config']},'runs':state['phases']})
  if phase=='enforced':
   shadow=next(x for x in state['phases'] if x['phase']=='shadow' and x.get('result')=='pass')
   proof_name = 'accepted-window-results.json' if shadow.get('evidence_kind') == 'user_approved_observation_prefix' else 'shadow-finished-results.json'
   raw=(E/'runs'/shadow['run_id']/proof_name).read_bytes()
   proof=json.loads(raw)
   start=datetime.datetime.fromisoformat(proof['started_at'].replace('Z','+00:00'))
   finish=datetime.datetime.fromisoformat(proof['finished_at'].replace('Z','+00:00'))
   duration_ok=proof['kind']=='active24h' and proof['active_24h']=='pass' and proof['elapsed_seconds']>=86400 and (finish-start).total_seconds()>=86400 and proof['samples']>=5760
   if proof['kind']=='accepted12h30':
    rebuilt=datetime.datetime.fromisoformat(proof['rebuild_activated_at'].replace('Z','+00:00'))
    duration_ok=proof['active_24h']=='not_verified' and proof['elapsed_seconds']>=45000 and (finish-start).total_seconds()>=45000 and proof['samples']>=3000 and proof['post_rebuild_seconds']>=1800 and rebuilt>=start and (finish-rebuilt).total_seconds()>=1800
    authorization=json.loads((E/'observation-window-authorization.json').read_text())
    if authorization['minimum_elapsed_seconds']!=45000 or authorization['minimum_post_rebuild_seconds']!=1800:raise RuntimeError('approved observation window differs')
   if not (duration_ok and proof['result']=='pass' and 0<=proof['p99_seconds']<=5 and proof['full_rebuild']=='pass' and proof['unresolved_tenant_gaps']==0 and proof['unexplained_authorization_differences']==0 and proof.get('quarantined_events',0)==0):raise RuntimeError('actual approved observation proof not complete')
   save(E/'enforcement-shadow-proof.json',proof)
   saved=(E/'enforcement-shadow-proof.json').read_bytes()
   save(E/'enforcement-admission.json',{'result':'pass','manifest_sha256':manifest,'shadow_run':shadow['run_id'],'shadow_proof_sha256':hashlib.sha256(saved).hexdigest()})
  if records:record=records[0]
  else:
   record={'phase':phase,'result':'dispatching'};state['phases'].append(record);save(path,state)
   record['run_id']=dispatch(phase);record['result']='running';save(path,state)
  if 'run_id' not in record:raise RuntimeError('uncertain prior dispatch; inspect runs before resuming')
  try:
   wait(record['run_id'])
   evidence=E/'runs'/record['run_id']
   for name in ['generation.json','owners-generation.json']:
    document=json.loads((evidence/name).read_text())
    if document['result']!='pass' or any(x['changed'] for x in document['outputs']):raise RuntimeError('frozen candidate is not generation-idempotent')
  except Exception:
   # Leave running/unknown state resumable; a confirmed nonzero exit is final.
   observed=status(record['run_id'])
   if observed['state']=='fail':record['result']='fail';save(path,state)
   raise
  record['result']='pass';save(path,state)
 state['result']='pass';save(path,state);print(json.dumps({'execution':str(path),'result':'pass'}))


def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('action',choices=[*PHASES,'status','wait','collect','all','resume'])
 p.add_argument('reference',nargs='?')
 a=p.parse_args()
 if a.action=='all':all_phases()
 elif a.action=='resume':all_phases(a.reference)
 elif a.action in ('status','wait','collect'):
  if not a.reference:p.error('run id required')
  if a.action=='status':print(json.dumps(status(a.reference),indent=2))
  elif a.action=='wait':wait(a.reference)
  else:collect(a.reference)
 else:
  run=dispatch(a.action);wait(run)

if __name__=='__main__':
 try:main()
 except Exception as error:
  print('WR23 not complete: '+str(error),file=sys.stderr);sys.exit(1)
