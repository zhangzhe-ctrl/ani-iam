#!/bin/bash
set -euo pipefail
# Environment owner tool. This never installs an IAM Grant and never reports
# permission revocation solely from successful configuration reload.
WR23_BROKER_TOOL_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
exec python3 - "$WR23_BROKER_TOOL_DIR" "$@" <<'PY'
import argparse,hashlib,json,os,re,subprocess,sys,time,urllib.parse,urllib.request
from pathlib import Path
sys.path.insert(0,sys.argv.pop(1))
from remote_environment import verify_runtime
p=argparse.ArgumentParser()
p.add_argument('--run',type=Path,required=True)
p.add_argument('--container',required=True)
p.add_argument('--candidate',type=Path,required=True)
p.add_argument('--approved-sha256',required=True)
p.add_argument('--monitor',required=True)
p.add_argument('--result',type=Path,required=True)
a=p.parse_args()
assert a.run.parent==Path('/home/ubuntu/workspace/ani-iam-runs') and re.fullmatch(r'wr23-resume-[0-9TZ]+-[a-f0-9]{8}',a.run.name)
verify_runtime(a.run)
assert re.fullmatch(r'[a-f0-9]{64}',a.container)
assert a.candidate.is_file() and not a.candidate.is_symlink() and a.candidate.resolve().is_relative_to(a.run/'private')
assert a.result.parent==a.run and a.result.name.endswith('-results.json') and not a.result.exists()
raw=a.candidate.read_bytes();digest=hashlib.sha256(raw).hexdigest();assert digest==a.approved_sha256
url=urllib.parse.urlparse(a.monitor)
assert url.scheme=='http' and url.hostname in ['127.0.0.1','::1'] and url.port and not url.username and url.path=='' and not url.query and not url.fragment
info=json.loads(subprocess.check_output(['docker','inspect',a.container]))[0]
assert info['Config']['Labels']['ani.goal']=='wr23' and info['Config']['Labels']['ani.run_id']==a.run.name and info['State']['Running']
def observed():
 with urllib.request.urlopen(a.monitor+'/varz',timeout=2) as r:doc=json.load(r)
 return {'server_id':doc['server_id'],'server_name':doc['server_name'],'version':doc['version'],'config_load_time':doc['config_load_time']}
before=observed()
result={'configuration_sha256':digest,'container_id':a.container,'before':before,'configuration_applied':False,'permission_revocation_verified':False,'state':'not_applied'}
log=a.run/'private'/('reload-'+digest+'.log')
next_path='/run/wr23/next-'+digest+'.conf'
try:
 subprocess.run(['docker','cp',str(a.candidate),a.container+':'+next_path],check=True,capture_output=True)
 check=subprocess.run(['docker','exec',a.container,'nats-server','-t','-c',next_path],capture_output=True)
 log.write_bytes(check.stdout+check.stderr);os.chmod(log,0o600)
 if check.returncode!=0:
  result['state']='validation_failed';result['after']=observed()
  raise RuntimeError('candidate configuration validation failed')
 prior=subprocess.check_output(['docker','exec',a.container,'sha256sum','/run/wr23/nats.conf'],text=True).split()[0]
 assert re.fullmatch(r'[a-f0-9]{64}',prior)
 subprocess.run(['docker','exec',a.container,'cp','/run/wr23/nats.conf','/run/wr23/previous-'+prior+'.conf'],check=True,capture_output=True)
 subprocess.run(['docker','exec',a.container,'mv',next_path,'/run/wr23/nats.conf'],check=True,capture_output=True)
 subprocess.run(['docker','kill','--signal=HUP',a.container],check=True,capture_output=True)
 for _ in range(30):
  after=observed()
  if after['server_id']==before['server_id'] and after['config_load_time']!=before['config_load_time']:
   applied=subprocess.check_output(['docker','exec',a.container,'sha256sum','/run/wr23/nats.conf'],text=True).split()[0]
   assert applied==digest
   result.update(state='configuration_applied_permission_probes_required',configuration_applied=True,after=after,previous_sha256=prior)
   break
  time.sleep(.1)
 else:result['state']='reload_not_observed';raise RuntimeError('reload was not observed')
except Exception:
 a.result.write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps({'state':result['state'],'configuration_applied':False,'permission_revocation_verified':False}))
 raise SystemExit(1)
a.result.write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({'state':result['state'],'configuration_applied':True,'permission_revocation_verified':False}))
PY
