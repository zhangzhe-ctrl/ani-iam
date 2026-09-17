set -euo pipefail
export WR23_RUN_DIR="$WR23_RESUME_RUN_DIR"
export WR23_FORMAL_COMBINATION=1
export DP2_ATLAS_BIN=/home/ubuntu/.local/share/ani-iam/bin/atlas
python3 tools/wr23-resume/owners-generation.py
python3 tools/wr23-resume/generation.py
check() {
 name=$1; dir=$2; shift 2
 date -u +%FT%TZ > "$WR23_RUN_DIR/$name.started"
 if (cd "$dir" && "$@") > "$WR23_RUN_DIR/private/$name.raw.log" 2>&1; then code=0; else code=$?; fi
 printf '%s %s\n' "$name" "$code" >> "$WR23_RUN_DIR/directed-checks.exit"
 date -u +%FT%TZ > "$WR23_RUN_DIR/$name.finished"
 printf 'WR23 %s exit=%s\n' "$name" "$code"
 return "$code"
}
# No container or running service exists while compiling this fixed source set.
check formal-iam-build . go build -o "$WR23_RUN_DIR/private/ani-iam-server" ./cmd/server
check formal-gateway-build ../source-ani/repo/services/ani-gateway go build -o "$WR23_RUN_DIR/private/ani-gateway" .
check formal-core-admin-build ../source-ani/repo/services/ani-gateway go build -o "$WR23_RUN_DIR/private/core-lifecycle-admin" ./cmd/core-lifecycle-admin
check formal-notification-build ../source-notification go build -o "$WR23_RUN_DIR/private/ani-notification-service" ./cmd/ani-notification-service
python3 - <<'PY'
import json,os,pathlib
r=pathlib.Path(os.environ['WR23_RUN_DIR'])
overlay={'Replace':{str(r/'source-notification/internal/data/wr23_fixture_test.go'):str(r/'source/tools/wr23-resume/notification-fixture_test.go')}}
(r/'private/notification-fixture-overlay.json').write_text(json.dumps(overlay)+'\n')
PY
check formal-notification-bootstrap-build ../source-notification go test -overlay "$WR23_RUN_DIR/private/notification-fixture-overlay.json" -c -tags=wr23fixture -o "$WR23_RUN_DIR/private/notification-bootstrap.test" ./internal/data
case "${WR23_FORMAL_EXTENSION:-}" in
 "") ;;
 envoy) source tools/wr23-resume/envoy-prepare.sh ;;
 session) source tools/wr23-resume/session-prepare.sh ;;
 *) exit 2 ;;
esac
check formal-chain-build . go test -c -tags=integration -o "$WR23_RUN_DIR/private/formal-chain.test" ./tests/integration
python3 - <<'PY'
import json,os,pathlib,subprocess
r=pathlib.Path(os.environ['WR23_RUN_DIR']);images=[
'postgres:16.4-alpine@sha256:5660c2cbfea50c7a9127d17dc4e48543eedd3d7a41a595a2dfa572471e37e64c',
'redis:7.4-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf',
'ghcr.io/dexidp/dex:v2.40.0@sha256:3e35d5d0f7dbd33fbadc36a71ff58cf4097ab98d73d22f6cb9a6471a32e028af',
'ghcr.io/axllent/mailpit@sha256:9d85d6bd20c834ec2b7d08ff97976af7b13e2330d9c56ecade4a231dcd3481ba',
'docker.io/library/nats@sha256:065e8355c20a5575b3c77224be1855e8103fd148b68fba05130b9b8ddfa40ccc']
rows=[]
for i,reference in enumerate(images):
 with (r/f'private/image-pull-{i}.log').open('wb') as log:subprocess.run(['docker','pull',reference],stdout=log,stderr=subprocess.STDOUT,check=True)
 actual=json.loads(subprocess.check_output(['docker','image','inspect',reference]))[0]
 assert actual['Architecture']=='amd64'
 assert any(x.endswith('@'+reference.split('@')[1]) for x in actual['RepoDigests'])
 rows.append({'reference':reference,'id':actual['Id'],'architecture':actual['Architecture'],'repo_digests':actual['RepoDigests']})
(r/'formal-images-results.json').write_text(json.dumps({'result':'pass','images':rows},indent=2)+'\n')
mem={x.split(':')[0]:int(x.split()[1]) for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.startswith(('MemAvailable:','SwapFree:'))}
disk=os.statvfs(r);available=disk.f_bavail*disk.f_frsize//1024
envoy=os.environ.get('WR23_FORMAL_EXTENSION')=='envoy'
session=os.environ.get('WR23_FORMAL_EXTENSION')=='session'
required={'memory':4062144 if session else 2900000 if envoy else 1800000,'swap':2097152 if session else 1048576,'disk':26214400 if session else 20971520}
# The new Kubernetes host deliberately has no swap. Reserve every byte of the
# prior swap allowance as extra physical RAM; never lower total headroom.
no_swap_ram_reserve=os.environ.get('WR23_NO_SWAP_RAM_RESERVE')=='1' and mem['SwapFree']==0
if no_swap_ram_reserve:
 required['memory']+=required['swap'];required['swap']=0
passed=mem['MemAvailable']>=required['memory'] and mem['SwapFree']>=required['swap'] and available>=required['disk']
(r/'formal-budget-results.json').write_text(json.dumps({'result':'pass' if passed else 'fail','mem_available_kib':mem['MemAvailable'],'swap_free_kib':mem['SwapFree'],'disk_available_kib':available,'minimum_kib':required,'containers':10 if session else 9 if envoy else 7,'container_memory_mib':2368 if session else 1088 if envoy else 768,'working_memory_budget_mib':3776 if session else 2688 if envoy else 1728,'extension':os.environ.get('WR23_FORMAL_EXTENSION',''),'no_swap_extra_physical_ram':no_swap_ram_reserve},indent=2)+'\n')
assert passed, 'declared formal resource budget unavailable'
PY
export WR23_DOCKER_NETWORK="ani-iam-$(basename "$WR23_RUN_DIR")"
docker network create --driver bridge --label ani.goal=wr23 --label "ani.run_id=$(basename "$WR23_RUN_DIR")" "$WR23_DOCKER_NETWORK" > "$WR23_RUN_DIR/network.id"
cleanup() {
 previous=$?
 trap - EXIT
 if [ "${WR23_FORMAL_EXTENSION:-}" = session ] && [ -f "$WR23_RUN_DIR/session-cluster.json" ]; then
  if ! python3 tools/wr23-resume/session-cluster.py cleanup > "$WR23_RUN_DIR/private/session-fallback-cleanup.log" 2>&1; then previous=1; fi
 fi
 python3 - <<'PY'
import json,os,pathlib,signal,subprocess,time
r=pathlib.Path(os.environ['WR23_RUN_DIR']);run=r.name;rows=[];leftovers=[]
for name in ['formal-processes.jsonl','reference-events.jsonl']:
 p=r/name
 if not p.exists():continue
 for line in p.read_text().splitlines():
  row=json.loads(line);pid=row.get('pid')
  if not isinstance(pid,int):continue
  def owned():
   try:return pathlib.Path(f'/proc/{pid}/exe').resolve().is_relative_to(r/'private')
   except OSError:return False
  if owned():
   leftovers.append({'kind':'process','pid':pid});os.kill(pid,signal.SIGTERM)
   end=time.monotonic()+5
   while owned() and time.monotonic()<end:time.sleep(.1)
   if owned():os.kill(pid,signal.SIGKILL)
  rows.append({'pid':pid,'absent':not owned()})
ids=subprocess.check_output(['docker','ps','-aq','--filter','label=ani.run_id='+run],text=True).split()
for cid in ids:
 c=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
 assert c['Config']['Labels']['ani.goal']=='wr23' and c['Config']['Labels']['ani.run_id']==run
 leftovers.append({'kind':'container','id':cid});subprocess.run(['docker','rm','-f',cid],check=True,stdout=subprocess.DEVNULL)
network=os.environ['WR23_DOCKER_NETWORK'];assert network=='ani-iam-'+run
result=subprocess.run(['docker','network','rm',network],capture_output=True)
(r/'network-cleanup.log').write_bytes(result.stdout+result.stderr)
remaining=subprocess.check_output(['docker','ps','-aq','--filter','label=ani.run_id='+run],text=True).strip()
proof={'result':'pass' if result.returncode==0 and not remaining and all(x['absent'] for x in rows) and not leftovers else 'fail','processes':rows,'unexpected_cleanup':leftovers,'network_exit':result.returncode,'containers_absent':not remaining}
(r/'formal-cleanup-results.json').write_text(json.dumps(proof,indent=2)+'\n')
raise SystemExit(0 if proof['result']=='pass' else 1)
PY
 clean=$?
 if [ "$previous" != 0 ]; then exit "$previous"; fi
 exit "$clean"
}
trap cleanup EXIT
export GOMEMLIMIT=192MiB
if [ "${WR23_RESOURCE_DIAGNOSTICS:-}" = 1 ]; then export GODEBUG=gctrace=1; fi
set +e
check formal-chain tests/integration "$WR23_RUN_DIR/private/formal-chain.test" -test.v -test.count=1 -test.timeout="${WR23_FORMAL_TIMEOUT:-12m}" -test.run="${WR23_FORMAL_TEST_PATTERN:-^TestWR23ResumeFormalLifecycleChain$}"
code=$?
set -e
python3 - "$code" <<'PY'
import hashlib,json,os,pathlib,sys
r=pathlib.Path(os.environ['WR23_RUN_DIR']);code=int(sys.argv[1]);names=['ani-iam-server','ani-gateway','core-lifecycle-admin','ani-notification-service','notification-bootstrap.test','formal-chain.test']
if os.environ.get('WR23_FORMAL_EXTENSION')=='envoy':names+=['envoy','envoy-authz-adapter','inference-service']
if os.environ.get('WR23_FORMAL_EXTENSION')=='session':names+=['session-gateway']
(r/'formal-execution-results.json').write_text(json.dumps({'result':'pass' if code==0 else 'fail','exit':code,'binaries':{n:hashlib.sha256((r/'private'/n).read_bytes()).hexdigest() for n in names},'log_sha256':hashlib.sha256((r/'private/formal-chain.raw.log').read_bytes()).hexdigest()},indent=2)+'\n')
PY
exit "$code"
