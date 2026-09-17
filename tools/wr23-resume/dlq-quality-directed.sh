set -euo pipefail
# Only affected modules. Notification/Session source is unchanged; their runtime
# gates remain separately not_verified and are not represented by this script.
python3 tools/wr23-resume/owners-generation.py
python3 tools/wr23-resume/generation.py
# Repository-wide gates use every fixed module already declared by ANI's input.
python3 - <<'PYWORK'
import json,os,subprocess
from pathlib import Path
r=Path(os.environ['WR23_RESUME_RUN_DIR']);native=r/'source-ani/repo/go.work'
e=dict(os.environ,GOWORK=str(native))
d=json.loads(subprocess.check_output(['go','work','edit','-json'],env=e))
for entry in d['Use']:
 subprocess.run(['go','work','use',str((native.parent/entry['DiskPath']).resolve())],check=True)
work=json.loads(subprocess.check_output(['go','work','edit','-json']));modules={};docs=[]
for entry in work['Use']:
 p=(r/entry['DiskPath']).resolve();m=json.loads(subprocess.check_output(['go','mod','edit','-json'],cwd=p));modules[m['Module']['Path']]=p;docs.append(m)
replacements=set()
for d in docs:
 for q in d.get('Require') or []:
  if q['Path'] in modules:replacements.add((q['Path'],q['Version'],str(modules[q['Path']])))
for p,v,d in sorted(replacements):subprocess.run(['go','work','edit','-replace='+p+'@'+v+'='+d],check=True)
(r/'module-mapping-aggregate.json').write_text(json.dumps(sorted(replacements),indent=2)+'\n')
PYWORK
mkdir -p "$WR23_RESUME_CACHE/bin"
ln -sfn /usr/bin/python3 "$WR23_RESUME_CACHE/bin/python"
# Pinned distribution dependency, extracted only into this task cache.
if ! test -x "$WR23_RESUME_CACHE/ripgrep/usr/bin/rg"; then
 mkdir -p "$WR23_RESUME_CACHE/ripgrep"
 (cd "$WR23_RESUME_CACHE/ripgrep" && apt-get download ripgrep=14.1.0-1 && dpkg-deb -x ripgrep_14.1.0-1_amd64.deb .)
fi
"$WR23_RESUME_CACHE/ripgrep/usr/bin/rg" --version > "$WR23_RESUME_RUN_DIR/ripgrep-version.txt"
sha256sum "$WR23_RESUME_CACHE/ripgrep/ripgrep_14.1.0-1_amd64.deb" "$WR23_RESUME_CACHE/ripgrep/usr/bin/rg" > "$WR23_RESUME_RUN_DIR/ripgrep.sha256"
export PATH="$WR23_RESUME_CACHE/ripgrep/usr/bin:$WR23_RESUME_CACHE/bin:$PATH"
result=0
check() {
 name=$1; dir=$2; shift 2
 date -u +%FT%TZ > "$WR23_RESUME_RUN_DIR/$name.started"
 if (cd "$dir" && "$@") > "$WR23_RESUME_RUN_DIR/private/$name.raw.log" 2>&1; then code=0; else code=$?; result=1; fi
 date -u +%FT%TZ > "$WR23_RESUME_RUN_DIR/$name.finished"
 printf '%s %s\n' "$name" "$code" >> "$WR23_RESUME_RUN_DIR/aggregate-checks.exit"
 printf 'WR23 aggregate %s exit=%s\n' "$name" "$code"
}
check iam-tests . go test -count=1 -timeout=8m ./...
check iam-race . go test -race -count=1 -timeout=10m ./internal/biz ./internal/data ./internal/server ./internal/service ./cmd/server
check iam-vet . go vet ./...
check iam-build . go build ./...
check iam-api api go test -race -count=1 ./...
check iam-api-vet api go vet ./...
check iam-api-build api go build ./...
check sdk-tests sdk go test -race -count=1 ./...
check sdk-vet sdk go vet ./...
check sdk-build sdk go build ./...
check registry-tests workloadregistry go test -race -count=1 ./...
check registry-vet workloadregistry go vet ./...
check registry-build workloadregistry go build ./...
check dlq-gateway ../source-ani/repo/services/ani-gateway go test -race -count=1 ./internal/authz ./internal/router -run 'CoreDLQ|CoreLifecycle'
check example-build examples/workload-grpc go build -o "$WR23_RESUME_RUN_DIR/private/workload-grpc" .
check ani-test ../source-ani/repo make test "GO_CACHE_ENV=GOCACHE=$GOCACHE GOMODCACHE=$GOMODCACHE"
check ani-services ../source-ani/repo make validate-services "GO_CACHE_ENV=GOCACHE=$GOCACHE GOMODCACHE=$GOMODCACHE"
check ani-document-entrypoints ../source-ani/repo make validate-doc-entrypoints
python3 - <<'PYRESULT'
import json,os
from pathlib import Path
r=Path(os.environ['WR23_RESUME_RUN_DIR']);rows=[]
for line in (r/'aggregate-checks.exit').read_text().splitlines():
 name,code=line.split();rows.append({'name':name,'exit_code':int(code),'result':'pass' if code=='0' else 'fail','private_log':name+'.raw.log'})
(r/'aggregate-results.json').write_text(json.dumps(rows,indent=2)+'\n')
PYRESULT
exit "$result"
