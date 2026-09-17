#!/bin/bash
set -euo pipefail
: "${WR21_RUN_DIR:?}"
test "$PWD" = "$WR21_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(sqlc version)" = v1.31.1
test "$(buf --version)" = 1.72.0
python3 - <<'PY'
import os,json,hashlib,tarfile,subprocess,base64
from pathlib import Path
run=Path(os.environ['WR21_RUN_DIR'])
def execute(args,cwd):subprocess.run(args,cwd=cwd,check=True)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def generate(iam,ani):
 execute(['env','GOWORK=off','go','mod','download','github.com/zhangzhe-ctrl/ani-iam/api@v0.0.0-20260911071802-b9fde01ae781','github.com/zhangzhe-ctrl/ani-iam/sdk@v0.0.0-20260911071951-e9f657f20b69'],iam/'examples/workload-grpc')
 execute(['python3','services/ani-gateway/tools/wr21_operation_registry.py'],ani/'repo')
 # Exact existing Git objects supply only immutable compatibility documents.
 reference=json.loads((run/'ani-registry-git-objects.json').read_text())
 execute(['git','init','-q'],ani)
 for oid,obj in reference['objects'].items():
  raw=base64.b64decode(obj['base64']);assert hashlib.sha256(raw).hexdigest()==obj['sha256']
  actual=subprocess.check_output(['git','hash-object','-w','-t',obj['type'],'--stdin'],input=raw,cwd=ani).decode().strip();assert actual==oid
 execute(['python3','scripts/generate_gateway_authz.py'],ani/'repo')
 execute(['python3','scripts/gen_sdk_alpha.py'],ani/'repo')
 execute(['python3','scripts/generate_api_docs.py'],ani/'repo')
 execute(['gofmt','-w','services/ani-gateway/internal/authz/zz_generated_core_policies.go'],ani/'repo')
 registry=iam/'tests/contracts/workload-operation-registry.v1.json'
 registry.write_bytes((ani/'repo/api/openapi/wr21-workload-operation-registry.v1.json').read_bytes())
 catalog=run/'private'/(iam.name+'-target-catalog.sql')
 execute(['go','run',str(iam/'internal/data/cmd/genoperationregistry/main.go'),'-input',str(registry),'-expected-sha256',sha(registry),'-go-output',str(iam/'internal/data/generated_operation_policies.go'),'-sql-output',str(catalog)],run/'source')
 # Never rewrite a delivered migration: verify the only new catalog fact.
 import re
 def permissions(path):return set(re.findall(r"\('([^']*)', '([^']*)', '([^']*)'\)",path.read_text()))
 old=permissions(iam/'migrations/202609080002_permission_catalog.sql');target=permissions(catalog)
 added=permissions(iam/'migrations/202609110001_inference_workload.sql')
 assert old<=target and target-old==added=={('tenant','inference-services','invoke')}
 execute(['sqlc','generate','-f','sqlc.yaml'],iam)
 execute(['atlas','migrate','hash','--dir','file://'+str(iam/'migrations')],iam)
 execute(['buf','generate','.','--template','buf.gen.yaml'],iam/'api/iam/v1')
 execute(['buf','build','.','--as-file-descriptor-set','--exclude-source-info','-o','iam_descriptor.pb'],iam/'api/iam/v1')
 pins=iam/'tests/contracts/contract_pins.json';data=json.loads(pins.read_text());data['artifacts']['iam_descriptor']=sha(iam/'api/iam/v1/iam_descriptor.pb');data['wr21']={'iam_baseline':'6e9688bb002d1916bc894fd281ba1974f57b7eaa','ani_baseline':'a021d987ffacfe7f9d3e065fdad0dc9d795f8bd9','registry_sha256':sha(registry),'policy_revision':json.loads(registry.read_text())['policy_revision']};pins.write_text(json.dumps(data,indent=2)+'\n')
 # Preserve the owner's managed go_package rules; only selected Proto and fixed local Go plugins.
 import yaml
 template=yaml.safe_load((ani/'repo/api/proto/buf.gen.yaml').read_text());template['plugins']=[x for x in template['plugins'] if x['local'] in ('protoc-gen-go','protoc-gen-go-grpc')]
 execute(['buf','generate','.','--path','inference/control/v1/inference_control.proto','--template',json.dumps(template)],ani/'repo/api/proto')
 names={'iam':['examples/workload-grpc/go.sum','tests/contracts/workload-operation-registry.v1.json','internal/data/generated_operation_policies.go','migrations/atlas.sum','api/iam/v1/iam_descriptor.pb','tests/contracts/contract_pins.json']+[str(p.relative_to(iam)) for p in sorted((iam/'api/iam/v1').glob('*.pb.go'))]+[str(p.relative_to(iam)) for p in sorted((iam/'internal/data/sqlcgen').glob('*.go'))], 'ani':['repo/services/ani-gateway/internal/authz/zz_generated_core_policies.go','repo/api/openapi/wr21-workload-operation-registry.v1.json','repo/services/ani-gateway/internal/authz/zz_generated_tenant_workload.go','repo/pkg/generated/pb/inference/control/v1/inference_control.pb.go','repo/pkg/generated/pb/inference/control/v1/inference_control_grpc.pb.go']}
 names['ani'] += [str(p.relative_to(ani)) for folder in ['repo/sdks/core','repo/sdks/services','repo/docs/api'] for p in sorted((ani/folder).rglob('*')) if p.is_file()]
 return names
for name,folder in [('iam','repeat-source'),('ani','repeat-ani')]:
 target=run/folder;target.mkdir()
 with tarfile.open(run/(name+'-source.tar.gz')) as a:a.extractall(target,filter='data')
names=generate(run/'source',run/'source-ani')
assert names==generate(run/'repeat-source',run/'repeat-ani')
rows=[]
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for kind,paths in names.items():
  first=run/('source' if kind=='iam' else 'source-ani');second=run/('repeat-source' if kind=='iam' else 'repeat-ani')
  for name in paths:
   assert (first/name).read_bytes()==(second/name).read_bytes(),(kind,name)
   rows.append({'repository':kind,'path':name,'sha256':sha(first/name)});archive.add(first/name,arcname=kind+'/'+name,recursive=False)
(run/'generation.json').write_text(json.dumps({'repeat':'pass','outputs':rows},indent=2)+'\n')
print('WR21 selected generation repeated: pass')
PY
# Format the uploaded changed handwritten Go files on both copies. Include them
# in the safe import archive; this is formatting, not manual generated editing.
python3 - <<'PYFORMAT'
import os,json,subprocess,tarfile,hashlib
from pathlib import Path
run=Path(os.environ['WR21_RUN_DIR']);generation=json.loads((run/'generation.json').read_text());known={(r['repository'],r['path']) for r in generation['outputs']};rows=generation['outputs']
for kind,root,repeat in [('iam',run/'source',run/'repeat-source'),('ani',run/'source-ani',run/'repeat-ani')]:
 manifest=json.loads((run/(kind+'-source.json')).read_text())
 candidates=[p for p in manifest['changed_paths'] if p.endswith('.go') and (kind,p) not in known]
 for folder in [root,repeat]:
  if candidates:subprocess.run(['gofmt','-w']+candidates,cwd=folder,check=True)
 for p in candidates:
  raw=(root/p).read_bytes();assert raw==(repeat/p).read_bytes()
  rows.append({'repository':kind,'path':p,'kind':'format','sha256':hashlib.sha256(raw).hexdigest()})
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for row in rows:archive.add(run/('source' if row['repository']=='iam' else 'source-ani')/row['path'],arcname=row['repository']+'/'+row['path'],recursive=False)
(run/'generation.json').write_text(json.dumps(generation,indent=2)+'\n')
PYFORMAT
go test -count=1 ./internal/biz ./internal/data ./internal/service > "$WR21_RUN_DIR/iam-directed.log" 2>&1
(cd sdk && go test -count=1 ./grpcworkload) > "$WR21_RUN_DIR/sdk-directed.log" 2>&1
(cd "$WR21_RUN_DIR/source-ani/repo/pkg" && go test -count=1 ./adapters/iam ./iaminference) > "$WR21_RUN_DIR/ani-pkg-directed.log" 2>&1
(cd "$WR21_RUN_DIR/source-ani/repo/services/ani-gateway" && go test -count=1 ./internal/authz ./internal/middleware ./internal/router) > "$WR21_RUN_DIR/gateway-directed.log" 2>&1
(cd "$WR21_RUN_DIR/source-ani/repo/services/inference-service" && go test -count=1 ./internal/service ./internal/grpcapi ./internal/config && go build ./...) > "$WR21_RUN_DIR/inference-directed.log" 2>&1
