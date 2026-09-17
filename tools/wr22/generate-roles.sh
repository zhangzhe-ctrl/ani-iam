#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(sqlc version)" = v1.31.1
sha256sum -c <<'HASHES'
10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b  /home/ubuntu/.local/share/ani-iam/bin/atlas
0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f  /home/ubuntu/.local/share/ani-iam/bin/sqlc
HASHES
python3 - <<'PY'
import os,json,subprocess,tarfile,hashlib
from pathlib import Path
run=Path(os.environ['WR22_RUN_DIR']);first=run/'source';repeat=run/'repeat-source';repeat.mkdir()
with tarfile.open(run/'iam-source.tar.gz') as a:a.extractall(repeat,filter='data')
manifest=json.loads((run/'iam-source.json').read_text())
handwritten=[p for p in manifest['changed_paths'] if p.endswith('.go') and not p.startswith('internal/data/sqlcgen/') and not p.endswith('.pb.go')]
for root in [first,repeat]:
 subprocess.run(['sqlc','generate','-f','sqlc.yaml'],cwd=root,check=True)
 subprocess.run(['atlas','migrate','hash','--dir','file://'+str(root/'migrations')],cwd=root,check=True,env={**os.environ,'ATLAS_NO_UPDATE_NOTIFIER':'1'})
 subprocess.run(['atlas','migrate','validate','--dir','file://'+str(root/'migrations')],cwd=root,check=True,env={**os.environ,'ATLAS_NO_UPDATE_NOTIFIER':'1'})
 if handwritten:subprocess.run(['gofmt','-w']+handwritten,cwd=root,check=True)
names=sorted(set(handwritten+['migrations/atlas.sum']+[str(p.relative_to(first)) for p in (first/'internal/data/sqlcgen').glob('*.go')]))
rows=[]
with tarfile.open(run/'generated.tar.gz','w:gz') as a:
 for name in names:
  data=(first/name).read_bytes();assert data==(repeat/name).read_bytes(),name
  rows.append({'repository':'iam','path':name,'sha256':hashlib.sha256(data).hexdigest()})
  a.add(first/name,arcname='iam/'+name,recursive=False)
(run/'generation.json').write_text(json.dumps({'repeat':'pass','outputs':rows},indent=2)+'\n')
print('WR22 role generation repeated: pass')
PY
