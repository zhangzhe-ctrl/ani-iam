#!/usr/bin/env python3
"""Create or remove only the exact WR23-owned Session acceptance cluster."""
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request

run = Path(os.environ['WR23_RUN_DIR'])
assert str(run).startswith('/home/ubuntu/workspace/ani-iam-runs/wr23-') and run.parent == Path('/home/ubuntu/workspace/ani-iam-runs')
source = Path(__file__).resolve().parent
budget = json.loads((source/'session-budget.json').read_text())
name = run.name.lower() + '-session'
network = 'ani-iam-' + run.name
assert os.environ['WR23_DOCKER_NETWORK'] == network
private = run/'private'
admin = private/'session-admin.kubeconfig'
access = private/'session-access'
record_path = run/'session-cluster.json'
kind = Path(os.environ['WR23_RESUME_CACHE'])/'kind-v0.33.0'
kubectl = os.environ.get('WR23_KUBECTL', '/usr/local/bin/kubectl')
node_name = name+'-control-plane'
env = dict(os.environ, KIND_EXPERIMENTAL_PROVIDER='docker', KIND_EXPERIMENTAL_DOCKER_NETWORK=network)

def sha(path):
    return hashlib.file_digest(Path(path).open('rb'), 'sha256').hexdigest()

def execute(args, data=None, timeout=120):
    p = subprocess.run(args, input=data, capture_output=True, timeout=timeout, env=env)
    if p.returncode:
        with (private/'session-cluster-errors.log').open('ab') as f:
            f.write(p.stderr)
        raise RuntimeError('WR23 cluster command failed; private error retained')
    return p.stdout

def write_private(path, raw):
    with path.open('xb') as f:
        f.write(raw)
    path.chmod(0o600)

def inspect_node():
    p = subprocess.run(['docker','container','inspect',node_name], capture_output=True, env=env)
    if p.returncode:
        return None
    node = json.loads(p.stdout)[0]
    assert node['Name'] == '/'+node_name
    assert node['Config']['Labels']['io.x-k8s.kind.cluster'] == name
    return node

def save(record):
    record_path.write_text(json.dumps(record, indent=2)+'\n')

def k(args, body=None):
    return execute([kubectl,'--kubeconfig',str(admin),'--context','kind-'+name,'--request-timeout=20s']+args, body, timeout=90)

if sys.argv[1:] == ['cleanup']:
    if not record_path.exists():
        raise SystemExit('No ownership record; no cleanup attempted')
    record = json.loads(record_path.read_text())
    assert record['cluster'] == name and record['network'] == network
    node = inspect_node()
    if node is not None:
        assert node['Id'] == record.get('node_id'), 'node ID mismatch; retain for review'
        if admin.exists():
            for resource in ['pods','events','nodes']:
                output = subprocess.run([kubectl,'--kubeconfig',str(admin),'--context','kind-'+name,'--request-timeout=10s','get',resource,'-A','-o','json'],capture_output=True,timeout=20,env=env)
                (private/('session-final-'+resource+'.json')).write_bytes(output.stdout)
        images = subprocess.run(['docker','exec',node['Id'],'ctr','--namespace=k8s.io','images','list'],capture_output=True,timeout=20)
        (private/'session-final-images.txt').write_bytes(images.stdout)
        execute([str(kind),'delete','cluster','--name',name,'--kubeconfig',str(admin)], timeout=120)
        assert inspect_node() is None
    record['cleanup'] = 'pass'
    record['cleanup_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    save(record)
    print(json.dumps({'cluster':name,'cleanup':'pass'}))
    raise SystemExit(0)
assert sys.argv[1:] == ['prepare']
import uuid
owners=json.loads((private/'session-tenants.json').read_text())
assert owners['run']==run.name and owners['source']=='formal_Core_gateway'
tenants=owners['tenants']
assert len(tenants)==2 and len(set(tenants))==2 and all(str(uuid.UUID(x))==x and uuid.UUID(x).version==7 for x in tenants)

assert not record_path.exists() and not admin.exists() and not access.exists()
assert inspect_node() is None
available = int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:')))
assert available >= budget['minimum_host_available_before_cluster_kib'], 'insufficient host memory; no cluster created'
st = os.statvfs(run)
assert st.f_bavail*st.f_frsize//1024 >= budget['minimum_free_disk_kib']
assert sha(kubectl) == budget['kubectl_sha256']
if not kind.exists():
    part = private/'kind-download.part'
    execute(['curl','--fail','--location','--retry','2','--connect-timeout','15','--max-time','180','https://kind.sigs.k8s.io/dl/v0.33.0/kind-linux-amd64','--output',str(part)], timeout=230)
    assert sha(part) == budget['kind_sha256']
    part.rename(kind)
    kind.chmod(0o700)
assert sha(kind) == budget['kind_sha256']
node_image = budget['node_image']
image = json.loads(execute(['docker','image','inspect',node_image]))[0]
assert node_image in image['RepoDigests'] and image['Architecture']=='amd64' and image['Os']=='linux'
kind_config = {'kind':'Cluster','apiVersion':'kind.x-k8s.io/v1alpha4','networking':{'apiServerAddress':'127.0.0.1'},'nodes':[{'role':'control-plane'}]}
config_path = private/'kind-config.json'
write_private(config_path,json.dumps(kind_config).encode())
record = {'cluster':name,'network':network,'created_by':'WR23 Goal','budget_sha256':sha(source/'session-budget.json'),'fixture_template_sha256':sha(source/'session-fixtures.json'),'kind_sha256':sha(kind),'kubectl_sha256':sha(kubectl),'node_image':node_image,'node_image_id':image['Id'],'node_absent_before':True,'initial_available_kib':available,'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
save(record)
log = (private/'kind-create.log').open('xb')
process = subprocess.Popen([str(kind),'create','cluster','--name',name,'--config',str(config_path),'--image',node_image,'--kubeconfig',str(admin),'--wait','120s'],stdout=log,stderr=log,env=env)
try:
    deadline = time.monotonic()+240
    limited = False
    while process.poll() is None:
        if not limited:
            node = inspect_node()
            if node is not None:
                record['node_id'] = node['Id']; save(record)
                execute(['docker','update','--memory',str(budget['node_memory_bytes']),'--memory-swap',str(budget['node_memory_swap_bytes']),'--cpus',str(budget['node_cpus']),'--pids-limit',str(budget['node_pids']),node['Id']])
                limited = True
        if time.monotonic()>deadline:
            process.terminate(); process.wait(timeout=30)
            raise RuntimeError('kind creation deadline; own partial cluster retained for cleanup')
        time.sleep(0.2)
    assert process.returncode == 0, 'kind creation failed; private log retained'
    assert limited
finally:
    log.close()
node = inspect_node(); assert node is not None and node['Id']==record['node_id']
assert node['HostConfig']['Memory']==budget['node_memory_bytes'] and node['HostConfig']['MemorySwap']==budget['node_memory_swap_bytes']
admin.chmod(0o600)
cluster_uid = json.loads(k(['get','namespace','kube-system','-o','json']))['metadata']['uid']
record['cluster_uid']=cluster_uid
record['kubernetes_version']=json.loads(k(['version','-o','json']))['serverVersion']['gitVersion']
save(record)
# Fixed provider image only; load it into this node. Never touch other clusters.
shell_image = budget['shell_image']
probe = subprocess.run(['docker','image','inspect',shell_image], capture_output=True)
if probe.returncode:
    execute(['docker','pull',shell_image],timeout=180)
shell = json.loads(execute(['docker','image','inspect',shell_image]))[0]
assert shell_image in shell['RepoDigests'] and shell['Architecture']=='amd64'
archive = private/'session-shell.tar'
execute(['docker','save','-o',str(archive),shell_image], timeout=120)
# Docker's OCI archive may wrap the original index in another index. Import
# digest aliases, then name the original descriptor, never the archive wrapper.
ctr = ['docker','exec',record['node_id'],'ctr','--namespace=k8s.io']
execute(['docker','exec','-i',record['node_id'],'ctr','--namespace=k8s.io','images','import','--platform','linux/amd64','--digests','--snapshotter=overlayfs','-'],archive.read_bytes(),timeout=120)
shell_digest = shell_image.split('@')[1]
assert shell_image.startswith('busybox@sha256:')
runtime_shell_image = 'docker.io/library/' + shell_image
def image_descriptors():
    lines = execute(ctr+['images','list']).decode().splitlines()[1:]
    return {fields[0]:fields[2] for line in lines if len(fields := line.split()) >= 3}
descriptors = image_descriptors()
originals = sorted(ref for ref,digest in descriptors.items() if digest == shell_digest and ref.endswith('@'+shell_digest))
assert originals, 'original pinned OCI descriptor absent from task containerd'
original_index = execute(ctr+['content','get',shell_digest])
assert 'sha256:'+hashlib.sha256(original_index).hexdigest() == shell_digest
assert runtime_shell_image not in descriptors
execute(ctr+['images','tag',originals[0],runtime_shell_image])
assert image_descriptors().get(runtime_shell_image) == shell_digest
cri = json.loads(execute(['docker','exec',record['node_id'],'crictl','inspecti',runtime_shell_image]))['status']
assert runtime_shell_image in cri['repoDigests'], 'CRI must resolve the exact original digest before Pod creation'
record['shell_image']=shell_image
record['shell_image_id']=shell['Id']
record['shell_archive_sha256']=sha(archive)
record['shell_runtime_image']=runtime_shell_image
record['shell_runtime_descriptor']=shell_digest
record['shell_original_index_sha256']=hashlib.sha256(original_index).hexdigest()
record['shell_cri_image_id']=cri['id']
record['shell_cri_repo_digests']=cri['repoDigests']
manifest = json.loads((source/'session-fixtures.json').read_text().replace('WR23_RUN_ID',run.name).replace('WR23_TENANT_A',tenants[0]).replace('WR23_TENANT_B',tenants[1]))
manifest_path = run/'session-applied-manifest.json'
manifest_path.write_text(json.dumps(manifest,indent=2)+'\n')
record['applied_manifest_sha256']=sha(manifest_path)
record['objects']=[]
save(record)
for item in manifest['items']:
    created = json.loads(k(['create','--validate=strict','-f','-','-o','json'],json.dumps(item).encode()))
    m=created['metadata']
    record['objects'].append({'kind':created['kind'],'namespace':m.get('namespace',''),'name':m['name'],'uid':m['uid']})
    save(record)
for tenant in tenants:
    namespace='ani-tenant-'+tenant
    k(['wait','pod','--all','-n',namespace,'--for=condition=Ready','--timeout=60s'])
ca=json.loads(k(['get','configmap','kube-root-ca.crt','-n','wr23-runtime','-o','json']))['data']['ca.crt']
admin_config=json.loads(k(['config','view','--minify','--raw','-o','json']))
server=admin_config['clusters'][0]['cluster']['server']
assert server.startswith('https://127.0.0.1:')
tokens={}
for account in ['wr23-session','wr23-gateway-reader']:
    body={'apiVersion':'authentication.k8s.io/v1','kind':'TokenRequest','spec':{'expirationSeconds':3600}}
    tokens[account]=json.loads(k(['create','--raw','/api/v1/namespaces/wr23-runtime/serviceaccounts/'+account+'/token','-f','-'],json.dumps(body).encode()))['status']
access.mkdir(mode=0o700)
write_private(access/'ca.crt',ca.encode())
write_private(access/'gateway.token',tokens['wr23-gateway-reader']['token'].encode())
session={'apiVersion':'v1','kind':'Config','clusters':[{'name':name,'cluster':{'server':server,'certificate-authority-data':base64.b64encode(ca.encode()).decode()}}],'users':[{'name':'session','user':{'token':tokens['wr23-session']['token']}}],'contexts':[{'name':name,'context':{'cluster':name,'user':'session'}}],'current-context':name}
write_private(access/'session.kubeconfig',json.dumps(session).encode())
namespace='ani-tenant-'+tenants[0]
checks=[('wr23-session','/api/v1/namespaces/'+namespace+'/pods',200),('wr23-session','/api/v1/namespaces/default/pods',403),('wr23-session','/api/v1/namespaces/'+namespace+'/secrets',403),('wr23-gateway-reader','/apis/apps/v1/namespaces/'+namespace+'/deployments/wr19-exec-a',200),('wr23-gateway-reader','/api/v1/namespaces/'+namespace+'/pods',403)]
context=ssl.create_default_context(cafile=str(access/'ca.crt'))
record['authenticated_checks']=[]
for account,path,expected in checks:
    request=urllib.request.Request(server+path,headers={'Authorization':'Bearer '+tokens[account]['token']})
    try:
        with urllib.request.urlopen(request,context=context,timeout=15) as response:
            actual=response.status
    except urllib.error.HTTPError as e:
        actual=e.code
    record['authenticated_checks'].append({'account':account,'path':path,'expected':expected,'actual':actual})
    save(record)
    assert actual==expected
facts={'cluster_uid':cluster_uid,'expires_at':min(t['expirationTimestamp'] for t in tokens.values()),'directory':str(access),'proxy':'','server':server,'applied_manifest_sha256':record['applied_manifest_sha256'],'node_id':record['node_id']}
write_private(access/'fixture-public.json',json.dumps(facts).encode())
record['fixture']=facts
record['prepared']='pass'
save(record)
print(json.dumps({'cluster':name,'prepared':'pass','cluster_uid':cluster_uid,'objects':len(record['objects']),'restricted_access_checks':'pass'}))
