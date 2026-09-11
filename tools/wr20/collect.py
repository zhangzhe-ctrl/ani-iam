#!/usr/bin/env python3
"""Collect only registered non-sensitive evidence and checked generated output."""
import argparse,hashlib,json,subprocess,tarfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
EV=ROOT/'ani-iam/.scratch/ani-iam-workload-refoundation/evidence/20-complete-human-authentication-notification'
p=argparse.ArgumentParser();p.add_argument('run');p.add_argument('--apply',action='store_true');args=p.parse_args()
assert args.run.startswith('wr20-') and '/' not in args.run
local=EV/'runs'/args.run;record=json.loads((local/'run.json').read_text());remote=record['remote'];ssh=['ssh','-F',str(Path.home()/'.ssh/config'),'-o','BatchMode=yes','-o','ControlMaster=auto','-o','ControlPersist=30','-o','ControlPath=/tmp/'+args.run+'.sock','ubuntu']
for name in ['generation.json','formatting.json','generated.tar.gz','formatted.tar.gz','command.exit','command.finished','command.started','command.pid','tools.txt','final-regression-results.json','race-ab-results.json','checks-a-results.json','stage-a-results.json','stage-b-results.json','stage-c-results.json','resources.jsonl','credential-references.jsonl','example-processes.jsonl','formal-processes.jsonl','reference-events.jsonl']:
 result=subprocess.run(ssh+['test -f '+remote+'/'+name+' && cat '+remote+'/'+name],capture_output=True)
 if result.returncode==0:(local/name).write_bytes(result.stdout)
assert (local/'command.exit').exists(),'Still running or uncertain: inspect same run, never redispatch'
if args.apply:
 for artifact in ['generated.tar.gz','formatted.tar.gz']:
  if not (local/artifact).exists():continue
  with tarfile.open(local/artifact) as tar:
   for member in tar:
    assert member.isfile()
    kind,path=member.name.split('/',1) if member.name.startswith(('iam/','ani/','notification/')) else ('iam',member.name)
    assert kind in ('iam','ani','notification') and '..' not in Path(path).parts and not Path(path).is_absolute()
    target=ROOT/({'iam':'ani-iam-wr20','ani':'ANI-wr20','notification':'ani-notification-service-wr20'}[kind])/path
    data=tar.extractfile(member).read();manifest=json.loads((local/(kind+'-source.json')).read_text());old=next((r for r in manifest['files'] if r['path']==path),None)
    if target.exists() and target.read_bytes()==data:continue
    allowed={r['path'] for r in json.loads((EV/(kind+'-scope.json')).read_text())['files']};assert path in allowed,(kind,path)
    if target.exists():assert old and hashlib.sha256(target.read_bytes()).hexdigest()==old['sha256'],'local input changed: '+path
    else:assert old is None
    target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data);target.chmod(0o644)
print(json.dumps({'run':args.run,'exit':int((local/'command.exit').read_text()),'applied':args.apply}))
