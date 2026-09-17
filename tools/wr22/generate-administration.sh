#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
test "$(buf --version)" = 1.72.0
sha256sum -c <<'HASHES'
8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af  /home/ubuntu/.local/share/ani-iam/bin/buf
7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010  /home/ubuntu/.local/share/ani-iam/bin/protoc-gen-go
aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08  /home/ubuntu/.local/share/ani-iam/bin/protoc-gen-go-grpc
HASHES
python3 - <<'PY'
import os,json,subprocess,hashlib,re,tarfile
from pathlib import Path
run=Path(os.environ['WR22_RUN_DIR'])
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
for root,ani in [(run/'source',run/'source-ani'),(run/'repeat-source',run/'repeat-ani')]:
 registry=root/'tests/contracts/workload-operation-registry.v1.json'
 registry.write_bytes((ani/'repo/api/openapi/wr22-identity-operation-registry.v1.json').read_bytes())
 catalog=run/'private'/(root.name+'-catalog.sql')
 subprocess.run(['go','run',str(root/'internal/data/cmd/genoperationregistry/main.go'),'-input',str(registry),'-expected-sha256',sha(registry),'-go-output',str(root/'internal/data/generated_operation_policies.go'),'-sql-output',str(catalog)],cwd=run/'source',check=True)
 def perms(p):return set(re.findall(r"\('([^']*)', '([^']*)', '([^']*)'\)",p.read_text()))
 assert perms(catalog)==perms(root/'migrations/202609080002_permission_catalog.sql')|perms(root/'migrations/202609110001_inference_workload.sql'),'catalog changed beyond WR22 existing permissions'
 subprocess.run(['buf','generate','.','--template','buf.gen.yaml'],cwd=root/'api/iam/v1',check=True)
 subprocess.run(['buf','generate','.','--template',json.dumps({'version':'v2','plugins':[{'local':'protoc-gen-go','out':'.','opt':['paths=source_relative']}]})],cwd=root/'internal/conf',check=True)
 subprocess.run(['buf','build','.','--as-file-descriptor-set','--exclude-source-info','-o','iam_descriptor.pb'],cwd=root/'api/iam/v1',check=True)
 pins=root/'tests/contracts/contract_pins.json';doc=json.loads(pins.read_text());doc['artifacts']['iam_descriptor']=sha(root/'api/iam/v1/iam_descriptor.pb')
 assert sha(root/'tests/contracts/fixtures/iam_error_contract.v1.json')=='7f96c9d0e26ac9dd0c437d9ac5c6641ca47c6ea150cc06d9684dda18eeace2b6','WR22 Invitation error fixture drift'
 doc['fixtures']['iam_error_contract.v1.json']=sha(root/'tests/contracts/fixtures/iam_error_contract.v1.json')
 doc['wr22']={'source_registry_sha256':sha(root/'tests/contracts/inputs/wr21-workload-operation-registry.v1.json'),'registry_sha256':sha(registry),'policy_revision':json.loads(registry.read_text())['policy_revision']};pins.write_text(json.dumps(doc,indent=2)+'\n')
first=run/'source';repeat=run/'repeat-source'
names=['internal/conf/conf.pb.go','tests/contracts/workload-operation-registry.v1.json','tests/contracts/contract_pins.json','internal/data/generated_operation_policies.go','api/iam/v1/iam_descriptor.pb']+[str(p.relative_to(first)) for p in (first/'api/iam/v1').glob('*.pb.go')]
generation=json.loads((run/'generation.json').read_text());rows={(r['repository'],r['path']):r for r in generation['outputs']}
for name in names:
 assert (first/name).read_bytes()==(repeat/name).read_bytes(),name
 rows['iam',name]={'repository':'iam','path':name,'sha256':sha(first/name)}
generation['outputs']=list(rows.values())
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for row in generation['outputs']:archive.add(run/('source' if row['repository']=='iam' else 'source-ani')/row['path'],arcname=row['repository']+'/'+row['path'],recursive=False)
(run/'generation.json').write_text(json.dumps(generation,indent=2)+'\n')
print('WR22 administration Proto/registry repeated: pass')
PY
