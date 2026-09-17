#!/usr/bin/env python3
"""Pinned, twice-repeated HTTP wire generation and bounded formatting on ubuntu."""
import hashlib,json,os,subprocess,tarfile,sys
import yaml
from pathlib import Path
run=Path(os.environ['WR23_RESUME_RUN_DIR']);first=run/'source';repeat=run/'repeat-source';repeat.mkdir()
expected={'sqlc':'0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f','atlas':'10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b','buf':'8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af','protoc-gen-go':'7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010','protoc-gen-go-grpc':'aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08'}
for tool,digest in expected.items():assert hashlib.sha256((Path('/home/ubuntu/.local/share/ani-iam/bin')/tool).read_bytes()).hexdigest()==digest,tool
with tarfile.open(run/'iam.tar') as tf:tf.extractall(repeat,filter='data')
for derived in ['tests/contracts/contract_pins.json','internal/data/generated_operation_policies.go']:(repeat/derived).write_bytes((first/derived).read_bytes())
spec=json.loads((run/'run.json').read_text());scope=set(spec['repositories']['iam']['allowed'])
original={f['path']:f for f in json.loads((run/'source.json').read_text())['iam']['files']}
handwritten=[p for p in scope if p.endswith('.go') and not p.endswith('.pb.go') and '/sqlcgen/' not in p and (first/p).exists()]
generated=['internal/conf/conf.pb.go','go.mod','go.sum','migrations/atlas.sum','registrations/workload-targets.v1.json']+[str(p.relative_to(first)) for p in (first/'internal/data/sqlcgen').glob('*.go')]+[str(p.relative_to(first)) for p in (first/'api/iam/v1').glob('*.pb.go')]+['api/iam/v1/iam_descriptor.pb','tests/contracts/contract_pins.json']
owner=run/'source-ani/repo/api/openapi/v1.yaml'
sys.path.insert(0,str(run/'source-ani/repo/scripts'))
from workload_http_targets import workload_http_targets
owner_spec=yaml.safe_load(owner.read_text());http_targets=list(workload_http_targets(owner_spec).values())
assert len(http_targets)==2,'WR23 exact Snapshot endpoint inventory changed'
inputs=[{'repository':'ani','path':'repo/api/openapi/v1.yaml','sha256':hashlib.sha256(owner.read_bytes()).hexdigest()}, {'repository':'ani','path':'repo/scripts/workload_http_targets.py','sha256':hashlib.sha256((run/'source-ani/repo/scripts/workload_http_targets.py').read_bytes()).hexdigest()}]
inputs.append({'repository':'iam','path':'tools/wr23-resume/generation.py','sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
for root in [first,repeat]:
 subprocess.run(['go','mod','edit','-require=github.com/nats-io/nats.go@v1.52.0','-require=github.com/nats-io/nkeys@v0.4.16'],cwd=root,check=True)
 subprocess.run(['go','mod','download','github.com/nats-io/nats.go@v1.52.0','github.com/nats-io/nkeys@v0.4.16'],cwd=root,check=True)
 subprocess.run(['sqlc','generate','-f','sqlc.yaml'],cwd=root,check=True)
 subprocess.run(['atlas','migrate','hash','--dir','file://'+str(root/'migrations')],cwd=root,env={**os.environ,'ATLAS_NO_UPDATE_NOTIFIER':'1'},check=True)
 registration=root/'registrations/workload-targets.v1.json';document=json.loads(registration.read_text());targets={(t['audience'],t['operation']):t for t in document['targets']}
 for target in http_targets:
  key=(target['audience'],target['operation'])
  assert key not in targets or (targets[key].get('http_method') and targets[key].get('http_path')),'existing RPC repurposed'
  targets[key]=target
  receiver={'audience':target['audience'],'operation':target['receiver_operation'],'rpc':'','mechanism':'receiver','grant_scope':'receiver','enabled':True}
  key=(receiver['audience'],receiver['operation']);assert key not in targets or targets[key]==receiver
  targets[key]=receiver
 document['targets']=list(targets.values());registration.write_text(json.dumps(document,indent=2)+'\n')
 subprocess.run(['buf','generate','.','--template','buf.gen.yaml'],cwd=root/'api/iam/v1',check=True)
 template=json.dumps({'version':'v2','plugins':[{'local':'protoc-gen-go','out':str(root/'internal/conf'),'opt':['paths=source_relative']}]})
 subprocess.run(['buf','generate','conf.proto','--template',template],cwd=root/'internal/conf',check=True)
 subprocess.run(['buf','build','.','--as-file-descriptor-set','--exclude-source-info','-o','iam_descriptor.pb'],cwd=root/'api/iam/v1',check=True)
 pins=root/'tests/contracts/contract_pins.json';data=json.loads(pins.read_text());data['artifacts']['iam_descriptor']=hashlib.sha256((root/'api/iam/v1/iam_descriptor.pb').read_bytes()).hexdigest();pins.write_text(json.dumps(data,indent=2)+'\n')
 subprocess.run(['gofmt','-w']+sorted(handwritten),cwd=root,check=True)
generated += [str(p.relative_to(first)) for p in (first/'internal/data/sqlcgen').glob('*.go')]
rows=[]
with tarfile.open(run/'generated.tar.gz','w:gz') as tf:
 for name in sorted(set(generated+handwritten)):
  value=(first/name).read_bytes();assert value==(repeat/name).read_bytes(),name;digest=hashlib.sha256(value).hexdigest();changed=digest!=original.get(name,{}).get('sha256')
  assert not changed or name in scope,'outside scope: '+name
  rows.append({'path':name,'sha256':digest,'changed':changed})
  if changed:tf.add(first/name,arcname=name,recursive=False)
(run/'generation.json').write_text(json.dumps({'result':'pass','tools':expected,'inputs':inputs,'outputs':rows},indent=2)+'\n')
print('WR23 repeated HTTP generation pass;',len(rows),'outputs')
