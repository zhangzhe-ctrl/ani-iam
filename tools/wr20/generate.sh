#!/bin/bash
set -euo pipefail
: "${WR20_RUN_DIR:?}"
test "$PWD" = "$WR20_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(sqlc version)" = v1.31.1
test "$(buf --version)" = 1.72.0
mkdir "$WR20_RUN_DIR/repeat-source" "$WR20_RUN_DIR/repeat-ani"
tar -xzf "$WR20_RUN_DIR/iam-source.tar.gz" -C "$WR20_RUN_DIR/repeat-source" --no-same-owner
tar -xzf "$WR20_RUN_DIR/ani-source.tar.gz" -C "$WR20_RUN_DIR/repeat-ani" --no-same-owner
python3 - <<'PY'
import os,json,hashlib,tarfile,subprocess,base64
from pathlib import Path
run=Path(os.environ['WR20_RUN_DIR']);rows=[]
def execute(args,cwd):subprocess.run(args,cwd=cwd,check=True)
def generate(iam,ani):
 execute(['atlas','migrate','hash','--dir','file://'+str(iam/'migrations')],iam)
 execute(['sqlc','generate','-f','sqlc.yaml'],iam)
 execute(['buf','generate','.','--template',json.dumps({'version':'v2','plugins':[{'local':'protoc-gen-go','out':'.','opt':['paths=source_relative']} ]})],iam/'internal/conf')
 execute(['buf','generate','.','--template','buf.gen.yaml'],iam/'api/iam/v1')
 execute(['buf','build','.','--as-file-descriptor-set','--exclude-source-info','-o','iam_descriptor.pb'],iam/'api/iam/v1')
 pins=iam/'tests/contracts/contract_pins.json';data=json.loads(pins.read_text());data['artifacts']['iam_descriptor']=hashlib.sha256((iam/'api/iam/v1/iam_descriptor.pb').read_bytes()).hexdigest();pins.write_text(json.dumps(data,indent=2)+'\n')
 # Rehydrate exact existing historical Git objects for the original generator's
 # fixed-source checks; never fabricate a commit or substitute a current ref.
 reference=json.loads((run/'ani-registry-git-objects.json').read_text())
 execute(['git','init','-q'],ani)
 for oid,obj in reference['objects'].items():
  raw=base64.b64decode(obj['base64']);assert hashlib.sha256(raw).hexdigest()==obj['sha256']
  actual=subprocess.check_output(['git','hash-object','-w','-t',obj['type'],'--stdin'],input=raw,cwd=ani).decode().strip();assert actual==oid
 execute(['python3','services/ani-gateway/tools/dp2_operation_registry.py'],ani/'repo')
 execute(['python3','scripts/generate_gateway_authz.py'],ani/'repo')
 execute(['gofmt','-w','services/ani-gateway/internal/authz/zz_generated_core_policies.go'],ani/'repo')
 names={'iam':['migrations/atlas.sum','internal/conf/conf.pb.go']+[str(p.relative_to(iam)) for p in sorted((iam/'internal/data/sqlcgen').glob('*.go'))]+[str(p.relative_to(iam)) for p in sorted((iam/'api/iam/v1').glob('*.pb.go'))]+['api/iam/v1/iam_descriptor.pb','tests/contracts/contract_pins.json'], 'ani':['repo/api/openapi/operation-registry.v1.json','repo/services/ani-gateway/internal/authz/zz_generated_target_operation_registry.go','repo/services/ani-gateway/internal/authz/zz_generated_core_policies.go']}
 return names
names=generate(run/'source',run/'source-ani');assert names==generate(run/'repeat-source',run/'repeat-ani')
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for kind,paths in names.items():
  first=run/('source' if kind=='iam' else 'source-ani');second=run/('repeat-source' if kind=='iam' else 'repeat-ani')
  for name in paths:
   data=(first/name).read_bytes();assert data==(second/name).read_bytes(),(kind,name)
   rows.append({'repository':kind,'path':name,'sha256':hashlib.sha256(data).hexdigest()});archive.add(first/name,arcname=kind+'/'+name,recursive=False)
(run/'generation.json').write_text(json.dumps({'repeat':'pass','outputs':rows},indent=2)+'\n')
print('IAM sqlc/Proto/descriptor and ANI registries repeated: pass')
PY
