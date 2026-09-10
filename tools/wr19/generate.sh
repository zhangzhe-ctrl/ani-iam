#!/bin/bash
set -euo pipefail
: "${WR19_RUN_DIR:?run through the WR19 remote runner}"
test "$PWD" = "$WR19_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(sqlc version)" = v1.31.1
test "$(buf --version)" = 1.72.0
protoc-gen-go --version
protoc-gen-go-grpc --version
atlas version
generate() {
  (cd api/iam/v1; buf generate . --template buf.gen.yaml; buf build . --as-file-descriptor-set --exclude-source-info -o iam_descriptor.pb)
  sqlc generate -f sqlc.yaml
  ATLAS_NO_UPDATE_NOTIFIER=1 atlas migrate hash --dir file://migrations
  ATLAS_NO_UPDATE_NOTIFIER=1 atlas migrate validate --dir file://migrations
}
generate
mkdir "$WR19_RUN_DIR/repeat-source"
tar -xzf "$WR19_RUN_DIR/source.tar.gz" -C "$WR19_RUN_DIR/repeat-source" --no-same-owner
(cd "$WR19_RUN_DIR/repeat-source"; generate)
python3 - <<'PY'
import hashlib, json, os, tarfile
from pathlib import Path
root=Path.cwd(); run=Path(os.environ['WR19_RUN_DIR'])
names=[str(p.relative_to(root)) for p in (root/'internal/data/sqlcgen').glob('*.go')]+['migrations/atlas.sum']
names += [str(p.relative_to(root)) for p in (root/'api/iam/v1').glob('*.pb.go')]+['api/iam/v1/iam_descriptor.pb']
rows=[]
for name in sorted(names):
 data=(root/name).read_bytes();assert data==(run/'repeat-source'/name).read_bytes(),name
 rows.append({'path':name,'sha256':hashlib.sha256(data).hexdigest()})
(run/'generation.json').write_text(json.dumps({'repeat':'pass','outputs':rows},indent=2)+'\n')
with tarfile.open(run/'generated.tar.gz','w:gz') as archive:
 for row in rows: archive.add(root/row['path'],arcname=row['path'],recursive=False)
print(json.dumps({'generation_repeat':'pass','files':len(rows)}))
PY
