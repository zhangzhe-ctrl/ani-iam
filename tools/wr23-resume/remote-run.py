#!/usr/bin/env python3
"""WR23_RESUME adaptation of tools/wr21/remote-run.py: fixed inputs, one remote command.

Source packing reads only the already enumerated paths. Hashing, scope audits,
formatting, generators and all Go work run on ubuntu under the shared lock.
"""
import argparse
import datetime
import json
import hashlib
from pathlib import Path
import shlex
import subprocess
import tarfile
import uuid
import time
from remote_environment import environment

SOURCE=Path(__file__).resolve().parents[2]
AUTHORITY=SOURCE.parent/'ani-iam'
EVIDENCE=AUTHORITY/'.scratch/ani-iam-workload-refoundation/evidence/23-integrate-core-lifecycle-bootstrap/wr23-resume-20260913T201049Z-9a57a8c9'


def upload(files, destination):
    # Only pre-dispatch immutable file copies may retry. The nohup dispatch is
    # deliberately never retried after an uncertain SSH outcome.
    command=['scp','-q','-o','BatchMode=yes','-o','ConnectTimeout=12']+list(map(str,files))+[destination]
    for attempt in range(3):
        result=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        if result.returncode==0:return
        if result.returncode!=255 or attempt==2:
            raise subprocess.CalledProcessError(result.returncode,command)
        time.sleep(1)


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--command-file',type=Path,required=True)
    parser.add_argument('--git-metadata', action='store_true', help='Copy exact fixed Git objects for repository aggregate checks; no new commit')
    args=parser.parse_args()
    claim=json.loads((EVIDENCE/'claim.json').read_text())
    scope=json.loads((EVIDENCE/'implementation-scope.json').read_text())
    issues=AUTHORITY/'.scratch/ani-iam-workload-refoundation/issues'
    claimed=[p.name for p in issues.glob('*.md') if '**Status:** claimed' in p.read_text().splitlines()]
    assert claimed==['23-integrate-core-lifecycle-bootstrap.md'], 'WR23 must remain the only claimed issue'
    run='wr23-resume-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+uuid.uuid4().hex[:8]
    local=EVIDENCE/'runs'/run;local.mkdir(parents=True)
    env=environment()
    host=env['ssh_host']
    remote=env['run_root']+'/'+run
    ssh=['ssh','-o','BatchMode=yes','-o','ConnectTimeout=12','-o','ServerAliveInterval=15','-o','ServerAliveCountMax=3',host]
    def execute(command,**kw):
        return subprocess.run(ssh+[command],check=True,**kw)
    identity=subprocess.check_output(ssh+['id -un; hostname'],text=True).splitlines()
    assert identity==[env['user'],env['hostname']]
    record={'run_id':run,'remote':remote,'state':'preparing','environment':env,'command':str(args.command_file.resolve()),'repositories':{}}
    final_path=EVIDENCE/'final-candidate.json'
    if final_path.exists():
        record['final_candidate']=json.loads(final_path.read_text())
    def save(): (local/'run.json').write_text(json.dumps(record,indent=2)+'\n')
    save()
    execute('umask 077; mkdir '+shlex.quote(remote))
    for name,baseline in claim['inputs'].items():
        root=Path(baseline['destination'])
        head=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip()
        assert head==baseline['baseline_head'],name
        allowed={f['path'] for f in scope['files'] if f['repository']==name}
        initial={f['path']:f for f in baseline['files']}
        names=sorted(set(initial)|allowed)
        archive=local/(name+'.tar')
        absent=[]
        with tarfile.open(archive,'w') as tf:
            for path in names:
                file=root/path
                if not file.exists():
                    if path in initial: raise RuntimeError('unexpected deletion: '+name+':'+path)
                    absent.append(path);continue
                assert file.is_file() and not file.is_symlink(),path
                tf.add(file,arcname=path,recursive=False)
        record['repositories'][name]={'root':str(root),'head':head,'initial':baseline['files'],'allowed':sorted(allowed),'declared_not_created':absent}
        upload([archive],host+':'+remote+'/')
        if args.git_metadata:
            # Bounded transport of the fixed commit/tree objects. No compression
            # or delta search, no history walk, and no write to the source index.
            # Original commit objects are copied, never synthesized or committed.
            packs=EVIDENCE/'git-baselines';packs.mkdir(exist_ok=True)
            pack=packs/(name+'-'+head+'.pack')
            if not pack.exists():
                objects={head,subprocess.check_output(['git','-C',str(root),'rev-parse',head+'^{tree}'],text=True).strip()}
                listing=subprocess.check_output(['git','-C',str(root),'ls-tree','-r','-t','-z',head])
                for row in listing.split(b'\0'):
                    if row: objects.add(row.split(b'\t',1)[0].split()[2].decode())
                with pack.open('xb') as output:
                    subprocess.run(['git','-C',str(root),'pack-objects','--stdout','--window=0','--depth=0','--threads=1','--compression=0'],input=('\n'.join(sorted(objects))+'\n').encode(),stdout=output,check=True)
            record['repositories'][name]['git_pack_sha256']=hashlib.file_digest(pack.open('rb'),'sha256').hexdigest()
            upload([pack],host+':'+remote+'/'+name+'-baseline.pack')
            if name == 'ani':
                # The existing authz generator reads two frozen OpenAPI commits.
                # Copy only their original commit objects and named path trees.
                history = [
                    ('0cedae825a489d936cf41815dc27f278f6d3213c', '552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8'),
                    ('bde4ea72b5a91cd43cc271dd44c09ff262c637e5', '3595d1b7cec729adf00a5bdc67f655e10e4f3bb2'),
                ]
                objects=set()
                for commit, tree in history:
                    assert subprocess.check_output(['git','-C',str(root),'rev-parse',commit+'^{tree}'],text=True).strip()==tree
                    objects.update((commit,tree))
                    for path in ['repo','repo/api','repo/api/openapi','repo/api/openapi/v1.yaml','repo/api/openapi/services','repo/api/openapi/services/v1.yaml']:
                        objects.add(subprocess.check_output(['git','-C',str(root),'rev-parse',commit+':'+path],text=True).strip())
                extra=packs/'ani-openapi-frozen-v1.pack'
                if not extra.exists():
                    with extra.open('xb') as output:
                        subprocess.run(['git','-C',str(root),'pack-objects','--stdout','--window=0','--depth=0','--threads=1','--compression=0'],input=('\n'.join(sorted(objects))+'\n').encode(),stdout=output,check=True)
                record['repositories'][name]['git_history']={'commits':history,'sha256':hashlib.file_digest(extra.open('rb'),'sha256').hexdigest()}
                upload([extra],host+':'+remote+'/ani-history.pack')
    (local/'command.sh').write_bytes(args.command_file.read_bytes())
    save()
    audit=r'''
import hashlib,json,pathlib,sys,tarfile
r=pathlib.Path(sys.argv[1]);spec=json.loads((r/'run.json').read_text());result={}
for name,meta in spec['repositories'].items():
 archive=r/(name+'.tar');target=r/('source' if name=='iam' else 'source-'+name)
 target.mkdir();initial={f['path']:f for f in meta['initial']};allowed=set(meta['allowed']);rows=[]
 with tarfile.open(archive) as tf:
  for m in tf.getmembers():
   assert m.isfile() and m.name in set(initial)|allowed,m.name
   value=tf.extractfile(m).read();sha=hashlib.sha256(value).hexdigest()
   if m.name not in allowed:
    assert sha==initial[m.name]['sha256'] and oct(m.mode)==initial[m.name]['mode'],'unlisted drift '+name+':'+m.name
   rows.append({'path':m.name,'sha256':sha,'mode':oct(m.mode)})
  tf.extractall(target,filter='data')
 result[name]={'head':meta['head'],'files':rows,'archive_sha256':hashlib.file_digest(archive.open('rb'),'sha256').hexdigest(),'declared_not_created':meta['declared_not_created']}
 if spec.get('final_candidate'):
  expected=spec['final_candidate']['repositories'][name]
  assert meta['head']==expected['head']
  assert {x['path']:(x['sha256'],x['mode']) for x in rows}=={x['path']:(x['sha256'],x['mode']) for x in expected['files']},'final candidate drift: '+name
 # An isolated shallow repository supplies the real fixed HEAD for Git-based
 # gates. The already audited candidate working files are never checked out.
 if meta.get('git_pack_sha256'):
  import subprocess
  pack=r/(name+'-baseline.pack')
  assert hashlib.file_digest(pack.open('rb'),'sha256').hexdigest()==meta['git_pack_sha256']
  subprocess.run(['git','init','--quiet',str(target)],check=True)
  with pack.open('rb') as stream:subprocess.run(['git','-C',str(target),'index-pack','--stdin'],stdin=stream,stdout=subprocess.DEVNULL,check=True)
  (target/'.git/shallow').write_text(meta['head']+'\n')
  if meta.get('git_history'):
   history=meta['git_history'];extra=r/'ani-history.pack'
   assert hashlib.file_digest(extra.open('rb'),'sha256').hexdigest()==history['sha256']
   with extra.open('rb') as stream:subprocess.run(['git','-C',str(target),'index-pack','--stdin'],stdin=stream,stdout=subprocess.DEVNULL,check=True)
   with (target/'.git/shallow').open('a') as f:
    for commit,tree in history['commits']:f.write(commit+'\n')
  subprocess.run(['git','-C',str(target),'update-ref','--no-deref','HEAD',meta['head']],check=True)
  subprocess.run(['git','-C',str(target),'read-tree',meta['head']],check=True)
  assert subprocess.check_output(['git','-C',str(target),'rev-parse','HEAD'],text=True).strip()==meta['head']
(r/'source.json').write_text(json.dumps(result,indent=2)+'\n')
'''
    (local/'audit-source.py').write_text(audit)
    runner=r'''#!/bin/bash
set -euo pipefail
umask 077
cd "$(dirname "$0")"
export WR23_RESUME_RUN_DIR="$PWD"
echo $$ > command.pid
trap 'exit 143' TERM
trap 'exit 130' INT
trap 'rc=$?; echo "$rc" > "$WR23_RESUME_RUN_DIR/command.exit"; date -u +%FT%TZ > "$WR23_RESUME_RUN_DIR/command.finished"' EXIT
exec 9>>/home/ubuntu/.local/share/ani-iam/heavy.lock
flock -n -E 75 9
date -u +%FT%TZ > command.started
export GOROOT=/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64
export PATH="/home/ubuntu/.local/share/ani-iam/bin:$GOROOT/bin:$PATH"
export GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2 GOMEMLIMIT=1536MiB TESTCONTAINERS_RYUK_DISABLED=true
mkdir private
python3 audit-source.py "$PWD"
export GOCACHE="$WR23_RESUME_CACHE/go-build" GOMODCACHE="$WR23_RESUME_CACHE/go-mod" GOTMPDIR="$PWD/private/tmp" BUF_CACHE_DIR="$WR23_RESUME_CACHE/buf"
export GOPATH="$WR23_RESUME_CACHE/gopath"
export TMPDIR="$GOTMPDIR"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" "$BUF_CACHE_DIR"
# Reuse signed transparency proofs in a task-owned cache; verification remains
# enabled. The host cache is only a read-only source for existing dependencies.
mkdir -p "$GOMODCACHE/cache/download" "$GOPATH/pkg"
cp -an /home/ubuntu/go/pkg/mod/cache/download/sumdb "$GOMODCACHE/cache/download/"
cp -an /home/ubuntu/go/pkg/sumdb "$GOPATH/pkg/"
export GOPROXY=file:///home/ubuntu/go/pkg/mod/cache/download,https://goproxy.cn,https://proxy.golang.org
export GOWORK="$PWD/go.work"
(go version; buf --version; sqlc version; protoc-gen-go --version; protoc-gen-go-grpc --version; atlas version; sha256sum /home/ubuntu/.local/share/ani-iam/bin/{buf,sqlc,atlas,protoc-gen-go,protoc-gen-go-grpc}) > tools.txt
go work init ./source ./source/api ./source/sdk ./source/workloadregistry ./source/examples/workload-grpc ./source-ani/repo/pkg ./source-ani/repo/services/ani-gateway ./source-ani/repo/services/envoy-authz-adapter ./source-ani/repo/services/inference-service ./source-ani/repo/sdks/core/go ./source-notification ./source-notification/api ./source-session ./source-session/api
python3 - <<'PY'
# Workspace use alone does not supply .mod files for every historically required
# candidate version. Bind every explicit version to this same fixed source set.
import json,pathlib,subprocess
r=pathlib.Path.cwd()
work=json.loads(subprocess.check_output(['go','work','edit','-json']))
modules={}
documents=[]
for entry in work['Use']:
 directory=(r/entry['DiskPath']).resolve()
 doc=json.loads(subprocess.check_output(['go','mod','edit','-json'],cwd=directory))
 modules[doc['Module']['Path']]=directory
 documents.append(doc)
replacements=set()
for doc in documents:
 for requirement in doc.get('Require') or []:
  path,version=requirement['Path'],requirement['Version']
  if path in modules:
   replacements.add((path,version,str(modules[path])))
for path,version,directory in sorted(replacements):
 subprocess.run(['go','work','edit','-replace='+path+'@'+version+'='+directory],check=True)
(r/'module-mapping.json').write_text(json.dumps(sorted(replacements),indent=2)+'\n')
PY
cd source
bash -euo pipefail ../command.sh > ../private/command.raw.log 2>&1
'''
    # Only this Goal's cache is shared across its serial iterations.
    runner=runner.replace('export GOCACHE=', 'export WR23_RESUME_CACHE='+shlex.quote(claim['remote_root']+'/cache')+'\nexport GOCACHE=')
    docker_exports='export DOCKER_HOST='+shlex.quote(env['docker_host'])+'\nexport PATH='+shlex.quote(env['docker_bin'])+':"$PATH"\nexport WR23_KUBECTL='+shlex.quote(env['kubectl'])+'\nexport WR23_NO_SWAP_RAM_RESERVE=1\n'
    runner=runner.replace('export GOTOOLCHAIN=',docker_exports+'export GOTOOLCHAIN=')
    (local/'runner.sh').write_text(runner)
    metadata=[local/name for name in ['run.json','command.sh','audit-source.py','runner.sh']]
    admission_path=EVIDENCE/'shadow-admission.json'
    if final_path.exists() and admission_path.exists():
        admission=json.loads(admission_path.read_text())
        assert admission['manifest_sha256']==record['final_candidate']['manifest_sha256']
        (local/'shadow-admission.json').write_bytes(admission_path.read_bytes())
        metadata.append(local/'shadow-admission.json')
    enforcement_path=EVIDENCE/'enforcement-admission.json'
    if final_path.exists() and enforcement_path.exists():
        admission=json.loads(enforcement_path.read_text())
        assert admission['manifest_sha256']==record['final_candidate']['manifest_sha256']
        proof_path=EVIDENCE/'enforcement-shadow-proof.json'
        assert hashlib.sha256(proof_path.read_bytes()).hexdigest()==admission['shadow_proof_sha256']
        for path in [enforcement_path,proof_path]:
            (local/path.name).write_bytes(path.read_bytes());metadata.append(local/path.name)
    upload(metadata,host+':'+remote+'/')
    record['state']='dispatching';save()
    result=subprocess.run(ssh+['cd '+shlex.quote(remote)+'; nohup bash runner.sh </dev/null >dispatch.log 2>&1 &'],capture_output=True)
    record['dispatch_exit']=result.returncode
    record['state']='dispatched' if result.returncode==0 else 'inspect_same_run'
    save();print(json.dumps({'run_id':run,'remote':remote,'state':record['state']},indent=2),flush=True)
    raise SystemExit(result.returncode)


if __name__=='__main__': main()
