#!/bin/bash
set -euo pipefail
: "${WR32_RUN_DIR:?}"
test "$PWD" = "$WR32_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(sqlc version)" = v1.31.1
test "$(buf --version)" = 1.72.0
sha256sum -c <<'HASHES'
10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b  /home/ubuntu/.local/share/ani-iam/bin/atlas
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f  /home/ubuntu/.local/share/ani-iam/bin/sqlc
HASHES
python3 - <<'PY'
import hashlib,json,os,subprocess,tarfile
from pathlib import Path
run=Path(os.environ['WR32_RUN_DIR']);first=run/'source';repeat=run/'repeat-source';repeat.mkdir()
with tarfile.open(run/'iam.tar') as tf:tf.extractall(repeat,filter='data')
scope=json.loads((run/'run.json').read_text())['repositories']['iam']['allowed']
generated=['internal/conf/conf.pb.go','migrations/atlas.sum']
generated += ['api/iam/v1/iam_descriptor.pb','tests/contracts/contract_pins.json']
generated += [str(p.relative_to(first)) for p in (first/'api/iam/v1').glob('*.pb.go')]
reference='examples/workload-grpc/api/reference/v1'
generated += [reference+'/reference.pb.go',reference+'/reference_grpc.pb.go']
generated += [str(p.relative_to(first)) for p in (first/'internal/data/sqlcgen').glob('*.go')]
generated += ['internal/data/sqlcgen/workload_registry.sql.go']
handwritten=[p for p in scope if p.endswith('.go') and not p.endswith('.pb.go') and '/sqlcgen/' not in p and (first/p).exists()]
template={'version':'v2','plugins':[{'local':'protoc-gen-go','out':'.','opt':['paths=source_relative']}]}
for root in [first,repeat]:
 subprocess.run(['sqlc','generate','-f','sqlc.yaml'],cwd=root,check=True)
 subprocess.run(['buf','generate','.','--template',json.dumps(template)],cwd=root/'internal/conf',check=True)
 subprocess.run(['buf','generate','.','--template','buf.gen.yaml'],cwd=root/'api/iam/v1',check=True)
 reference_template={'version':'v2','plugins':[{'local':'protoc-gen-go','out':'.','opt':['paths=source_relative']},{'local':'protoc-gen-go-grpc','out':'.','opt':['paths=source_relative']}]}
 subprocess.run(['buf','generate','.','--template',json.dumps(reference_template)],cwd=root/reference,check=True)
 subprocess.run(['buf','build','.','--as-file-descriptor-set','--exclude-source-info','-o','iam_descriptor.pb'],cwd=root/'api/iam/v1',check=True)
 pins=root/'tests/contracts/contract_pins.json'
 data=json.loads(pins.read_text())
 data['artifacts']['iam_descriptor']=hashlib.sha256((root/'api/iam/v1/iam_descriptor.pb').read_bytes()).hexdigest()
 pins.write_text(json.dumps(data,indent=2)+'\n')
 env={**os.environ,'ATLAS_NO_UPDATE_NOTIFIER':'1'}
 subprocess.run(['atlas','migrate','hash','--dir','file://'+str(root/'migrations')],cwd=root,check=True,env=env)
 subprocess.run(['atlas','migrate','validate','--dir','file://'+str(root/'migrations')],cwd=root,check=True,env=env)
 subprocess.run(['gofmt','-w']+handwritten,cwd=root,check=True)
rows=[]
with tarfile.open(run/'generated.tar.gz','w:gz') as tf:
 for name in sorted(set(generated+handwritten)):
  value=(first/name).read_bytes();assert value==(repeat/name).read_bytes(),name
  original=next((r for r in json.loads((run/'source.json').read_text())['iam']['files'] if r['path']==name),None)
  actual=hashlib.sha256(value).hexdigest()
  changed=original is None or actual!=original['sha256']
  assert not changed or name in scope,'generation outside scope: '+name
  rows.append({'path':name,'sha256':actual,'changed':changed})
  if changed:tf.add(first/name,arcname=name,recursive=False)
(run/'generation.json').write_text(json.dumps({'result':'pass','template':template,'outputs':rows},indent=2)+'\n')
print('WR32 repeated generation: pass; outputs',len(rows))
PY
# Existing owners keep their own generation workflow and pinned plugins.
notify_buf=/home/ubuntu/.local/share/ani-notification-service/bin/buf
test "$("$notify_buf" --version)" = 1.60.0
test "$(go version -m "$notify_buf" | awk '$1=="mod" {print $2 "@" $3;exit}')" = github.com/bufbuild/buf@v1.60.0
sha256sum "$notify_buf" > "$WR32_RUN_DIR/notification-buf.sha256"
export WR32_NOTIFY_BUF="$notify_buf"
python3 - <<'PYOWNERS'
import hashlib,json,os,subprocess,tarfile
from pathlib import Path
run=Path(os.environ['WR32_RUN_DIR']);spec=json.loads((run/'run.json').read_text());source=json.loads((run/'source.json').read_text());rows=[]
with tarfile.open(run/'owners-generated.tar.gz','w:gz') as out:
 for owner in ['ani','notification','session']:
  first=run/('source-'+owner);repeat=run/('repeat-'+owner);repeat.mkdir()
  with tarfile.open(run/(owner+'.tar')) as tf:tf.extractall(repeat,filter='data')
  allowed=spec['repositories'][owner]['allowed'];original={r['path']:r for r in source[owner]['files']}
  names=[p for p in allowed if p.endswith('.go') and not p.endswith('.pb.go') and (first/p).exists()]
  if owner=='notification':
   names.append('internal/conf/v1/conf.pb.go')
   for root in [first,repeat]:
    subprocess.run([os.environ['WR32_NOTIFY_BUF'],'generate','--template','buf.gen.yaml'],cwd=root,check=True)
  for root in [first,repeat]:
   handwritten=[p for p in names if not p.endswith('.pb.go')]
   if handwritten:subprocess.run(['gofmt','-w']+handwritten,cwd=root,check=True)
  for name in names:
   value=(first/name).read_bytes();assert value==(repeat/name).read_bytes(),(owner,name)
   digest=hashlib.sha256(value).hexdigest();changed=digest!=original.get(name,{}).get('sha256')
   assert not changed or name in allowed
   rows.append({'repository':owner,'path':name,'sha256':digest,'changed':changed})
   if changed:out.add(first/name,arcname=owner+'/'+name,recursive=False)
(run/'owners-generation.json').write_text(json.dumps({'result':'pass','outputs':rows},indent=2)+'\n')
print('WR32 owner generation/format repeat: pass;',len(rows),'outputs')
PYOWNERS
