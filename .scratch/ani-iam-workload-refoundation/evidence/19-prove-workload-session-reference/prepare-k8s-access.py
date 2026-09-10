#!/usr/bin/env python3
"""Mint new WR19-only one-hour SA credentials and transfer privately to ubuntu."""
import datetime
import json
from pathlib import Path
import shlex
import re
import subprocess

root = Path(__file__).resolve().parent
ssh = ['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12']
records = [json.loads(line) for line in (root/'k8s-create-events.jsonl').read_text().splitlines()]
created = [row for row in records if row.get('stage')=='created']
assert len(created)==16
now = datetime.datetime.now(datetime.timezone.utc)
destination = '/home/ubuntu/workspace/ani-iam-runs/wr19-k8s-access-'+now.strftime('%Y%m%dT%H%M%SZ')
remote = '''
import json,subprocess,sys
def k(args,body=None):
 p=subprocess.run(['kubectl','--request-timeout=15s']+args,input=body,text=True,capture_output=True)
 if p.returncode: raise SystemExit('Kubernetes input verification failed')
 return json.loads(p.stdout)
for row in CREATED:
 args=['get',row['kind'],row['name'],'-o','json']
 if row['namespace']:args+=['-n',row['namespace']]
 item=k(args)
 if item['metadata']['uid']!=row['uid']: raise SystemExit('Fixture UID drift')
cluster=k(['get','namespace','kube-system','-o','json'])['metadata']['uid']
if cluster!='f5cafbf1-5246-4f8f-b9da-742182f37528':raise SystemExit('Cluster UID drift')
ca=k(['-n','ani-cutover-target','get','configmap','kube-root-ca.crt','-o','json'])['data']['ca.crt']
tokens={}
for account in ['wr19-session','wr19-gateway-reader']:
 request={'apiVersion':'authentication.k8s.io/v1','kind':'TokenRequest','spec':{'expirationSeconds':3600}}
 tokens[account]=k(['create','--raw','/api/v1/namespaces/ani-cutover-target/serviceaccounts/'+account+'/token','-f','-'],json.dumps(request))['status']
print(json.dumps({'cluster_uid':cluster,'ca':ca,'tokens':tokens}))
'''.replace('CREATED',repr(created))
# These fresh tokens are never printed, placed in argv, or saved locally.
result = subprocess.run(ssh+['ani','python3 -'],input=remote,text=True,capture_output=True,check=True)
private = json.loads(result.stdout)
private['destination']=destination
proxy=json.loads((root/'k8s-tunnel.json').read_text())['remote_loopback_proxy']
assert re.fullmatch(r'socks5://127\.0\.0\.1:[0-9]{1,5}',proxy)
private['proxy']=proxy
receive = '''
import json,sys,os,pathlib,base64,subprocess,datetime
p=json.load(sys.stdin);root=pathlib.Path(p['destination']);root.mkdir(mode=0o700)
def write(name,value):
 path=root/name
 with path.open('x') as f:f.write(value)
 path.chmod(0o600)
write('ca.crt',p['ca']);tokens=p['tokens']
write('gateway.token',tokens['wr19-gateway-reader']['token'])
config={'apiVersion':'v1','kind':'Config','clusters':[{'name':'wr19','cluster':{'server':'https://10.10.1.66:6443','certificate-authority-data':base64.b64encode(p['ca'].encode()).decode()}}],'users':[{'name':'wr19-session','user':{'token':tokens['wr19-session']['token']}}],'contexts':[{'name':'wr19','context':{'cluster':'wr19','user':'wr19-session'}}],'current-context':'wr19'}
write('session.kubeconfig',json.dumps(config))
expires=min(v['expirationTimestamp'] for v in tokens.values())
facts={'cluster_uid':p['cluster_uid'],'expires_at':expires,'directory':str(root),'proxy':p['proxy'],'accounts':['wr19-session','wr19-gateway-reader'],'credentials':'new one-hour restricted SA TokenRequests; values private; fixtures retained'}
write('fixture-public.json',json.dumps(facts))
namespace='ani-tenant-01993000-0019-7000-8000-000000000001'
checks=[('wr19-session','/api/v1/namespaces/'+namespace+'/pods',200),('wr19-session','/api/v1/namespaces/default/pods',403),('wr19-session','/api/v1/namespaces/'+namespace+'/secrets',403),('wr19-gateway-reader','/apis/apps/v1/namespaces/'+namespace+'/deployments/wr19-exec-a',200),('wr19-gateway-reader','/api/v1/namespaces/'+namespace+'/pods',403)]
facts['authenticated_checks']=[]
for account,path,expected in checks:
 # Header is passed only through stdin, never argv or diagnostic output.
 config='header = '+json.dumps('Authorization: Bearer '+tokens[account]['token'])+'\\n'
 call=subprocess.run(['curl','--silent','--show-error','--max-time','12','--proxy',p['proxy'],'--cacert',str(root/'ca.crt'),'--config','-','--output','/dev/null','--write-out','%{http_code}','https://10.10.1.66:6443'+path],input=config,text=True,capture_output=True)
 if call.returncode:raise SystemExit('Restricted Kubernetes transport check failed; private credential files retained')
 status=int(call.stdout)
 facts['authenticated_checks'].append({'account':account,'path':path,'expected':expected,'actual':status})
 if status!=expected:raise SystemExit('Restricted Kubernetes access check failed')
print(json.dumps(facts))
'''
completed = subprocess.run(ssh+['ubuntu','python3 -c '+shlex.quote(receive)],input=json.dumps(private),text=True,capture_output=True)
if completed.returncode:
 (root/'k8s-access-interrupted.json').write_text(json.dumps({'directory':destination,'ssh_exit':completed.returncode,'action':'inspect this private directory before minting again'},indent=2)+'\n')
 raise SystemExit('Private access preparation interrupted; inspect registered destination before renewal')
facts=json.loads(completed.stdout)
(root/('k8s-access-'+now.strftime('%Y%m%dT%H%M%SZ')+'.json')).write_text(json.dumps(facts,indent=2)+'\n')
(root/'k8s-access-preflight.json').write_text(json.dumps(facts,indent=2)+'\n')
print(json.dumps({'private_directory':destination,'expires_at':facts['expires_at'],'authenticated_checks':'pass','credential_values':'not emitted'}))
