#!/usr/bin/env python3
"""Collect public metadata and verified generated outputs; never fetch private logs."""
import argparse,hashlib,json,re,shlex,subprocess,tarfile,time
from pathlib import Path
from remote_environment import run_environment
p=argparse.ArgumentParser();p.add_argument('run');p.add_argument('--apply',action='store_true');a=p.parse_args()
assert re.fullmatch(r'wr23-resume-[0-9TZ]+-[a-f0-9]{8}',a.run)
e=Path('/home/chabking/workspace/ani-iam/.scratch/ani-iam-workload-refoundation/evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9');local=e/'runs'/a.run;remote='/home/ubuntu/workspace/ani-iam-runs/'+a.run
env=run_environment(e,a.run);host=env['ssh_host']
query="from pathlib import Path;import json;r=Path("+repr(remote)+");exact={'directed-checks.exit','command.pid','command.exit','command.started','command.finished','source.json','tools.txt','module-mapping.json','generation.json','generated.tar.gz','owners-generation.json','owners-generated.tar.gz','resources.jsonl','network.id','network-cleanup.log','notification-buf.sha256','onboarding-comparison.json','onboarding-freeze.json','reference-events.jsonl','example-processes.jsonl','session-cluster.json','session-applied-manifest.json','envoy-version.txt','envoy-binary.sha256','aggregate-checks.exit','module-mapping-aggregate.json','notification-module-overlay.json','ripgrep-version.txt','ripgrep.sha256','notification-modules.sha256','final-source.json','final-deltas.json','python-tools.txt','shadow-samples.jsonl','shadow-admission.json','enforcement-admission.json','enforcement-shadow-proof.json'};print(json.dumps([p.name for p in r.iterdir() if p.name in exact or p.name.endswith('-results.json')]))"
names=json.loads(subprocess.check_output(['ssh',host,'python3 -c '+shlex.quote(query)],text=True))
for attempt in range(3):
 result=subprocess.run(['scp','-q','-o','BatchMode=yes','-o','ConnectTimeout=12',*[host+':'+remote+'/'+name for name in names],str(local)+'/'],capture_output=True)
 if result.returncode==0:break
 if result.returncode!=255 or attempt==2:raise RuntimeError('public evidence transfer failed; retry collection of same completed run')
 time.sleep(1)
if not a.apply:raise SystemExit(0)
roots={k:Path(v['destination']) for k,v in json.loads((e/'claim.json').read_text())['inputs'].items()}
allowed={(x['repository'],x['path']) for x in json.loads((e/'implementation-scope.json').read_text())['files']};source=json.loads((local/'source.json').read_text());initial={k:{f['path']:f for f in v['files']} for k,v in source.items()};rows=[]
for archive,manifest,owner_prefix in [('generated.tar.gz','generation.json',False),('owners-generated.tar.gz','owners-generation.json',True)]:
 if not (local/archive).exists() or not (local/manifest).exists():continue
 manifest_document=json.loads((local/manifest).read_text())
 if any(not (roots[f['repository']]/f['path']).exists() or hashlib.sha256((roots[f['repository']]/f['path']).read_bytes()).hexdigest()!=f['sha256'] for f in manifest_document.get('inputs',[])):
  rows.append({'repository':'all','path':manifest,'result':'skipped_newer_generator_input'});continue
 generated={(f.get('repository','iam'),f['path']):f for f in manifest_document['outputs']}
 with tarfile.open(local/archive) as tf:
  for m in tf.getmembers():
   assert m.isfile()
   owner,name=m.name.split('/',1) if owner_prefix else ('iam',m.name)
   assert (owner,name) in allowed, 'import outside frozen scope';record=generated[owner,name];assert name in initial[owner] or record['changed']
   root=roots[owner];dest=root/name;raw=tf.extractfile(m).read();assert hashlib.sha256(raw).hexdigest()==record['sha256']
   expected=initial[owner].get(name,{}).get('sha256');current=hashlib.sha256(dest.read_bytes()).hexdigest() if dest.exists() else None
   if current==record['sha256']:continue
   if current!=expected:
    rows.append({'repository':owner,'path':name,'result':'skipped_newer_edit'});continue
   # Do not import outputs produced before their corresponding generator inputs.
   inputs=[]
   if name.endswith('.pb.go') or name.endswith('iam_descriptor.pb') or name.endswith('contract_pins.json'):
    inputs=[p for p in initial[owner] if p.endswith('.proto') or p.endswith('buf.gen.yaml') or p.endswith('buf.lock')]
   if '/sqlcgen/' in name or name=='migrations/atlas.sum':
    inputs=[p for p in initial[owner] if p.endswith('.sql') or p=='sqlc.yaml']
   if any(not (root/f).exists() or hashlib.sha256((root/f).read_bytes()).hexdigest()!=initial[owner][f]['sha256'] for f in inputs):
    rows.append({'repository':owner,'path':name,'result':'skipped_newer_generator_input'});continue
   dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes(raw)
   rows.append({'repository':owner,'path':name,'result':'applied','sha256':record['sha256']})
(local/'applied-generation.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps({'run':a.run,'applied':sum(x['result']=='applied' for x in rows),'skipped':sum(x['result']!='applied' for x in rows)}))
