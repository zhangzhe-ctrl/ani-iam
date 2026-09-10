#!/usr/bin/env python3
"""Create only the accepted WR19 list; preserve and report every created object."""
import base64
import hashlib
import json
from pathlib import Path
import subprocess

evidence = Path(__file__).resolve().parent
manifest = (evidence / 'k8s-proposal-v2.json').read_bytes()
expected = '75872bb74d664e4d9fab3a58433d00a9e6e1cc246404a2ea671038a79aafe976'
assert hashlib.sha256(manifest).hexdigest() == expected
output = evidence / 'k8s-create-events.jsonl'
if output.exists():
    raise SystemExit('Existing execution record: inspect actual UIDs before any retry')
script = '''
import base64,json,subprocess,datetime
manifest=json.loads(base64.b64decode(MANIFEST))
def emit(event):
 event['observed_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 print(json.dumps(event),flush=True)
def kubectl(args, body=None):
 p=subprocess.run(['kubectl','--request-timeout=15s']+args,input=body,text=True,capture_output=True)
 if p.returncode:
  emit({'stage':'command_failed','args':args,'exit_code':p.returncode,'stderr':p.stderr[:1500]})
  raise SystemExit(p.returncode)
 return json.loads(p.stdout) if p.stdout.strip() else None
for name,uid in [('kube-system','f5cafbf1-5246-4f8f-b9da-742182f37528'),('ani-cutover-current','aeae0faf-8764-4d74-8e50-2f7ab34e57a0'),('ani-cutover-target','86a86f83-71e2-4c75-bb50-d81d83a73b90')]:
 item=kubectl(['get','namespace',name,'-o','json'])
 if item['metadata']['uid']!=uid: raise SystemExit('Cluster/namespace UID drift')
emit({'stage':'cluster_identity','result':'pass'})
for item in manifest['items']:
 m=item['metadata']; args=['get',item['kind'],m['name'],'--ignore-not-found','-o','json']
 if m.get('namespace'): args += ['-n',m['namespace']]
 if kubectl(args) is not None: raise SystemExit('Proposed object exists: '+item['kind']+'/'+m['name'])
emit({'stage':'all_16_names_absent','result':'pass'})
for item in manifest['items']:
 created=kubectl(['create','--validate=strict','-f','-','-o','json'],json.dumps(item))
 m=created['metadata']
 emit({'stage':'created','kind':created['kind'],'namespace':m.get('namespace',''),'name':m['name'],'uid':m['uid'],'retained':True})
emit({'stage':'creation_complete','count':16,'result':'pass','cleanup':'retain per user decision'})
'''.replace('MANIFEST', repr(base64.b64encode(manifest).decode()))
with output.open('x') as result:
    completed = subprocess.run(['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','ani','python3 -'],input=script,text=True,stdout=result,stderr=subprocess.PIPE)
print(json.dumps({'exit_code':completed.returncode,'events':str(output),'stderr':completed.stderr[:1500]}))
raise SystemExit(completed.returncode)
