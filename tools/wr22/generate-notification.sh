#!/bin/bash
set -euo pipefail
: "${WR22_RUN_DIR:?}"
test "$PWD" = "$WR22_RUN_DIR/source"
mkdir -p "$WR22_RUN_DIR/private/notification-tools"
GOWORK=off GOBIN="$WR22_RUN_DIR/private/notification-tools" go install github.com/bufbuild/buf/cmd/buf@v1.60.0
export WR22_NOTIFICATION_BUF="$WR22_RUN_DIR/private/notification-tools/buf"
test "$("$WR22_NOTIFICATION_BUF" --version)" = 1.60.0
go version -m "$WR22_NOTIFICATION_BUF" > "$WR22_RUN_DIR/notification-tools.txt"
sha256sum "$WR22_NOTIFICATION_BUF" >> "$WR22_RUN_DIR/notification-tools.txt"
python3 - <<'PY'
import os,json,subprocess,tarfile,hashlib,io
from pathlib import Path
run=Path(os.environ['WR22_RUN_DIR']);first=run/'source-notification';repeat=run/'repeat-notification';repeat.mkdir()
with tarfile.open(run/'notification-source.tar.gz') as a:a.extractall(repeat,filter='data')
manifest=json.loads((run/'notification-source.json').read_text());handwritten=[p for p in manifest['changed_paths'] if p.endswith('.go') and not p.endswith('.pb.go')]
for root in (first,repeat):
 subprocess.run([os.environ['WR22_NOTIFICATION_BUF'],'lint'],cwd=root,check=True)
 subprocess.run([os.environ['WR22_NOTIFICATION_BUF'],'generate','--template','buf.gen.yaml'],cwd=root,check=True)
 if handwritten:subprocess.run(['gofmt','-w']+handwritten,cwd=root,check=True)
names=sorted(set(handwritten+[str(p.relative_to(first)) for p in first.rglob('*.pb.go')]))
generation={'repeat':'pass','outputs':[]};old=[]
if (run/'generation.json').exists():generation=json.loads((run/'generation.json').read_text())
if (run/'generated.tar.gz').exists():
 with tarfile.open(run/'generated.tar.gz') as archive:
  for member in archive.getmembers():
   assert member.isfile();old.append((member,archive.extractfile(member).read()))
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for member,content in old:archive.addfile(member,io.BytesIO(content))
 for name in names:
  data=(first/name).read_bytes();assert data==(repeat/name).read_bytes(),name
  generation['outputs'].append({'repository':'notification','path':name,'sha256':hashlib.sha256(data).hexdigest()})
  archive.add(first/name,arcname='notification/'+name,recursive=False)
(run/'generation.json').write_text(json.dumps(generation,indent=2)+'\n')
print('WR22 Notification fixed generation repeated: pass')
PY
