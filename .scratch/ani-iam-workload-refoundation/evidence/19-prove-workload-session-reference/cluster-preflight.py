import json, subprocess, pathlib, datetime
base=pathlib.Path(__file__).parent
cmd=['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','ani']
def get(args):
    return json.loads(subprocess.check_output(cmd+['kubectl '+args+' -o json'],text=True))
ns=get('get ns ani-cutover-current ani-cutover-target kube-system')['items']
items=[]
for n in ('ani-cutover-current','ani-cutover-target'):
    for o in get('get pods,deployments,services,pvc,networkpolicies,serviceaccounts,roles,rolebindings -n '+n)['items']:
        item={'kind':o['kind'],'namespace':n,'name':o['metadata']['name'],'uid':o['metadata']['uid']}
        if o['kind']=='Pod':
            item['phase']=o['status']['phase']; item['images']=[c['image'] for c in o['spec']['containers']]
        items.append(item)
result={'observed_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'ssh_host':'ani','namespaces':[{'name':n['metadata']['name'],'uid':n['metadata']['uid']} for n in ns],'protected_existing_resources':items,'writes_performed':False}
(base/'cluster-preflight.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({'namespaces':result['namespaces'],'protected_resources':len(items),'writes_performed':False}))
