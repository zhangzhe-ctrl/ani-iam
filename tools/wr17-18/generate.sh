#!/bin/bash
set -euo pipefail

# Invocation is permitted only in a verified VM run source directory.
: "${WR18_RUN_DIR:?run through the WR18 remote runner}"
test "$PWD" = "$WR18_RUN_DIR/source"
test "$(go env GOVERSION)" = go1.26.7
test "$(buf --version)" = 1.72.0
test "$(sqlc version)" = v1.31.1
protoc-gen-go --version
protoc-gen-go-grpc --version
atlas version

generate() {
  (
    cd api/iam/v1
    buf generate . --template buf.gen.yaml
    buf build . --as-file-descriptor-set --exclude-source-info -o iam_descriptor.pb
  )
  (
    cd internal/conf
    buf generate conf.proto --template '{"version":"v2","plugins":[{"local":"protoc-gen-go","out":".","opt":["paths=source_relative"]}]}'
  )
  registry_sha=$(sha256sum tests/contracts/workload-operation-registry.v1.json | cut -d ' ' -f 1)
  go run ./internal/data/cmd/genoperationregistry \
    -input tests/contracts/workload-operation-registry.v1.json \
    -expected-sha256 "$registry_sha" \
    -go-output internal/data/generated_operation_policies.go \
    -sql-output migrations/202609080002_permission_catalog.sql
  sqlc generate -f sqlc.yaml
  ATLAS_NO_UPDATE_NOTIFIER=1 atlas migrate hash --dir file://migrations
  ATLAS_NO_UPDATE_NOTIFIER=1 atlas migrate validate --dir file://migrations
}

# The second clean output starts from the immutable, already verified archive.
generate
mkdir "$WR18_RUN_DIR/repeat-source"
tar -xzf "$WR18_RUN_DIR/source.tar.gz" -C "$WR18_RUN_DIR/repeat-source" --no-same-owner
(
  cd "$WR18_RUN_DIR/repeat-source"
  generate
)
python3 - <<'PY'
import hashlib, json, os
from pathlib import Path
root = Path.cwd(); run = Path(os.environ['WR18_RUN_DIR'])
names = [str(p.relative_to(root)) for p in (root/'api/iam/v1').glob('*.pb.go')]
names += ['api/iam/v1/iam_descriptor.pb', 'internal/conf/conf.pb.go',
          'internal/data/generated_operation_policies.go',
          'migrations/202609080002_permission_catalog.sql', 'migrations/atlas.sum']
names += [str(p.relative_to(root)) for p in (root/'internal/data/sqlcgen').glob('*.go')]
rows = []
for name in sorted(names):
    content = (root/name).read_bytes()
    assert content == (run/'repeat-source'/name).read_bytes(), name
    rows.append({'path': name, 'sha256': hashlib.sha256(content).hexdigest()})
(run/'generation.json').write_text(json.dumps({'repeat': 'pass', 'outputs': rows}, indent=2)+'\n')
import tarfile
with tarfile.open(run/'generated.tar.gz', 'w:gz') as archive:
    for row in rows: archive.add(root/row['path'], arcname=row['path'], recursive=False)
print(json.dumps({'generation_repeat': 'pass', 'files': len(rows)}))
PY
