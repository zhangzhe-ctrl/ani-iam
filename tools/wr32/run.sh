#!/bin/bash
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
exec python3 - "$script_dir" "$@" <<'PY'
import hashlib,json,re,subprocess,sys,time
from pathlib import Path
scripts=Path(sys.argv[1]);authority=scripts.parents[2]/'ani-iam'
e=authority/'.scratch/ani-iam-workload-refoundation/evidence/32-stabilize-workload-integration'
stages=['aggregate','pg-directed','formal-iam','notification','session','envoy','reference']
args=sys.argv[2:]
if args and args != ['all']:
 if len(args)!=1 or args[0] not in stages: raise SystemExit('usage: run.sh [all|'+ '|'.join(stages)+']')
 stages=args
if not (e/'final-candidate.json').exists(): raise SystemExit('Final candidate is not frozen; use the bounded preparation runner until acceptance is ready')
issue=authority/'.scratch/ani-iam-workload-refoundation/issues/32-stabilize-workload-integration.md'
if '**Status:** claimed' not in issue.read_text():
 raise SystemExit('AGENTS.md requires a unique claimed verification item before creating remote resources; reopen WR32 verification explicitly before replay')
fixed=json.loads((e/'reproduction-inputs.json').read_text())
for name,digest in fixed['commands'].items():
 if hashlib.sha256((e/name).read_bytes()).hexdigest()!=digest:raise SystemExit('Frozen verification command changed: '+name)
for stage in stages:
 command=[sys.executable,str(scripts/'remote-run.py'),'--command-file',str(e/(stage+'.sh'))]
 if stage=='aggregate':command.append('--git-metadata')
 info=json.loads(subprocess.check_output(command,text=True));run=info['run_id']
 assert re.fullmatch(r'wr32-[0-9TZ]+-[a-f0-9]{8}',run)
 remote='/home/ubuntu/workspace/ani-iam-runs/'+run
 print(stage+': '+run,flush=True)
 while True:
  result=subprocess.run(['ssh','-o','BatchMode=yes','-o','ConnectTimeout=12','ubuntu','if test -f '+remote+'/command.exit; then cat '+remote+'/command.exit; fi'],capture_output=True,text=True,check=True).stdout.strip()
  if result:break
  time.sleep(10)
 subprocess.run([sys.executable,str(e/'collect-run.py'),run],check=True)
 print(stage+': exit '+result,flush=True)
 if result!='0':raise SystemExit(int(result))
PY
