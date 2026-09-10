#!/usr/bin/env python3
"""Collect only named sanitized evidence, verify exact registered resources."""
import json,pathlib,subprocess,shlex,sys,re
root=pathlib.Path(__file__).resolve().parent
run=sys.argv[1]
assert re.fullmatch(r'wr19-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}',run)
remote=r'''
import pathlib,json,subprocess,hashlib,re,os
p=pathlib.Path('/home/ubuntu/workspace/ani-iam-runs')/RUN
names=['command.log','command.exit','command.started','command.finished','command.pid','resources.jsonl','formal-processes.jsonl','reference-events.jsonl','example-processes.jsonl','credential-references.jsonl','real-reference.jsonl','independent-regression.jsonl','ani-make-test.log','ani-doc-entrypoints.log','ani-services-boundary.log','ani-wr19-generation.log']
files={n:(p/n).read_text() for n in names if (p/n).is_file()}
assert files.get('command.exit','').strip().isdigit() and 0 <= int(files['command.exit']) <= 255
created=[json.loads(x) for x in files.get('resources.jsonl','').splitlines() if json.loads(x).get('event')=='created']
existing=set(subprocess.check_output(['docker','ps','-aq','--no-trunc'],text=True).split())
left=[r['container_id'] for r in created if r['container_id'] in existing]
pids=[]
for name in ['formal-processes.jsonl','reference-events.jsonl','example-processes.jsonl']:
 for line in files.get(name,'').splitlines():
  r=json.loads(line)
  if 'pid' in r:pids.append(r['pid'])
pids.append(int(files['command.pid']))
alive=[]
for pid in pids:
 q=pathlib.Path('/proc')/str(pid)/'cmdline'
 if q.exists() and str(p).encode() in q.read_bytes():alive.append(pid)
logs=[f for f in (p/'private').rglob('*.log') if f.is_file()]
public='\n'.join(files.values()).encode()
patterns={'private_key':rb'-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----','jwt':rb'eyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}','ticket_url':rb'[?&]ticket=[A-Za-z0-9_-]{20,}','terminal_marker':rb'wr19_[a-f0-9]{32}','test_password':b'wr19-isolated-reference-human'}
leaks=[]
log_records=[]
for f in logs:
 raw=f.read_bytes();log_records.append({'path':str(f),'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)})
 for name,pattern in patterns.items():
  if re.search(pattern,raw):leaks.append({'file':str(f),'kind':name})
for name,pattern in patterns.items():
 if re.search(pattern,public):leaks.append({'file':'selected public outputs','kind':name})
# Compare concrete private fixture bytes without disclosing the bytes themselves.
secrets=[]
for f in (p/'private').rglob('*'):
 if f.is_file() and (f.suffix in ['.secret','.key'] or f.name.endswith('-key.pem') or f.name=='ticket-key'):
  raw=f.read_bytes()
  if len(raw)>=16:secrets.append(raw.strip())
  if f.suffix=='.secret':
   from urllib.parse import urlparse,unquote
   try:
    secret=unquote(urlparse(raw.decode().strip()).password or '').encode()
    if len(secret)>=12:secrets.append(secret)
   except (ValueError,UnicodeDecodeError):pass
combined=public+b'\n'+b'\n'.join(f.read_bytes() for f in logs)
if any(secret and secret in combined for secret in secrets):leaks.append({'file':'scanned output','kind':'concrete private fixture bytes'})
report={'run_id':RUN,'command_exit':int(files['command.exit']),'created_containers':len(created),'registered_containers_still_present':left,'registered_processes_still_running':alive,'private_logs_scanned':log_records,'secret_leak_findings':leaks,'scan_scope':'named public evidence plus task-private formal process logs; JWT/ticket/terminal patterns and concrete fixture secrets; values never exported'}
if leaks:print(json.dumps({'report':report}));raise SystemExit(2)
print(json.dumps({'files':files,'report':report}))
'''.replace('RUN',repr(run))
ssh=['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','-o','ControlMaster=auto','-o','ControlPersist=600','-o','ControlPath=/tmp/wr19-monitor.sock','ubuntu','python3 -']
r=subprocess.run(ssh,input=remote,text=True,capture_output=True)
if r.returncode not in [0,2]:raise SystemExit('Evidence read interrupted; no retry of remote work')
output=json.loads(r.stdout)
directory=root/'runs'/run
(directory/'cleanup-and-log-scan.json').write_text(json.dumps(output['report'],indent=2)+'\n')
if r.returncode:print(json.dumps(output['report']));raise SystemExit(2)
for name,text in output['files'].items():(directory/name).write_text(text)
print(json.dumps({k:v for k,v in output['report'].items() if k!='private_logs_scanned'}))
