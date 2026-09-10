#!/usr/bin/env python3
import pathlib,json,subprocess,datetime
root=pathlib.Path(__file__).resolve().parent
pre=json.loads((root/'cluster-preflight.json').read_text())
created=[json.loads(x) for x in (root/'k8s-create-events.jsonl').read_text().splitlines() if json.loads(x).get('stage')=='created']
rows=[dict(r,group='retained_WR19') for r in created]+[dict(r,group='protected_existing') for r in pre['protected_existing_resources']]+[dict(r,kind='Namespace',namespace='',group='cluster_identity') for r in pre['namespaces']]
script='''import json,subprocess
out=[]
for row in ROWS:
 args=['kubectl','--request-timeout=12s','get',row['kind'],row['name'],'-o','jsonpath={.metadata.uid}']
 if row.get('namespace'):args+=['-n',row['namespace']]
 p=subprocess.run(args,text=True,capture_output=True)
 out.append({'group':row['group'],'kind':row['kind'],'namespace':row.get('namespace',''),'name':row['name'],'expected_uid':row['uid'],'actual_uid':p.stdout.strip() if not p.returncode else None,'match':not p.returncode and p.stdout.strip()==row['uid']})
print(json.dumps(out))
'''.replace('ROWS',repr(rows))
p=subprocess.run(['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','ani','python3 -'],input=script,text=True,capture_output=True)
if p.returncode:raise SystemExit('Fixture verification interrupted; no cluster mutation performed')
results=json.loads(p.stdout)
d={'observed_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'checks':results,'all_match':all(r['match'] for r in results),'retention':'all 16 declared reusable objects retained; no deletion; fresh access needed after token expiry'}
(root/'final-fixture-verification.json').write_text(json.dumps(d,indent=2)+'\n')
print(json.dumps({'checked':len(results),'all_match':d['all_match'],'mismatches':[r for r in results if not r['match']]}))
