#!/usr/bin/env python3
"""WR22 exact-input, single-dispatch remote runner; never retries execution."""
import argparse,datetime,gzip,hashlib,io,json,os,shlex,stat,subprocess,tarfile,uuid
from pathlib import Path
SOURCE=Path(__file__).resolve().parents[2]
AUTHORITY=SOURCE.parent/'ani-iam'
EVIDENCE=AUTHORITY/'.scratch/ani-iam-workload-refoundation/evidence/22-complete-identity-administration'
def sha(b):return hashlib.sha256(b).hexdigest()
def main():
 p=argparse.ArgumentParser();p.add_argument('--command-file',type=Path,required=True);args=p.parse_args()
 assert '**Status:** claimed' in (AUTHORITY/'.scratch/ani-iam-workload-refoundation/issues/22-complete-identity-administration.md').read_text()
 run='wr22-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+uuid.uuid4().hex[:8]
 local=EVIDENCE/'runs'/run;local.mkdir(parents=True,exist_ok=False)
 ssh=['ssh','-F',str(Path.home()/'.ssh/config'),'-o','BatchMode=yes','-o','ConnectTimeout=12','-o','ControlMaster=auto','-o','ControlPersist=30','-o','ControlPath=/tmp/'+run+'.sock','-o','ServerAliveInterval=15','-o','ServerAliveCountMax=3','ubuntu']
 def remote(cmd,data=None):return subprocess.run(ssh+[cmd],input=data,check=True,capture_output=True).stdout
 identity=remote('id -un; hostname; printf "%s" "$HOME"').decode().splitlines();assert identity==['ubuntu','i-8yg2l7u8','/home/ubuntu'],identity
 directory='/home/ubuntu/workspace/ani-iam-runs/'+run
 baselines=json.loads((EVIDENCE/'baseline.json').read_text())['repositories']; manifests={}
 names=['iam','ani']
 if (EVIDENCE/'notification-scope.json').exists():
  names.append('notification'); n=json.loads((EVIDENCE/'notification-input-source.json').read_text());baselines['notification']={'target':json.loads((EVIDENCE/'notification-scope.json').read_text())['root'],'commit':n['head'],'tree':n['tree']}
 for name in names:
  baseline=baselines[name];root=Path(baseline['target']);scope=json.loads((EVIDENCE/(name+'-scope.json')).read_text());allowed={r['path'] for r in scope['files']}
  initial={r['path']:r for r in json.loads((EVIDENCE/(name+('-input-source.json' if name=='notification' else '-initial-source.json'))).read_text())['files']}
  head,tree=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD','HEAD^{tree}'],text=True).splitlines();assert (head,tree)==(baseline['commit'],baseline['tree'])
  for path,row in initial.items():
   if path in allowed:continue
   f=root/path;assert f.is_file() and sha(f.read_bytes())==row['sha256'] and oct(stat.S_IMODE(f.stat().st_mode))==row['mode'],'unlisted source drift '+name+':'+path
  tracked=set(filter(None,subprocess.check_output(['git','-C',str(root),'ls-files','--others','--exclude-standard','-z'],text=True).split('\0')))
  assert tracked<=set(initial)|allowed,'unlisted new files '+str(sorted(tracked-set(initial)-allowed))
  archive=local/(name+'-source.tar.gz');rows=[];deleted=[]
  with archive.open('wb') as raw,gzip.GzipFile(filename='',mode='wb',fileobj=raw,mtime=0) as gz,tarfile.open(fileobj=gz,mode='w') as tar:
   for path in sorted(set(initial)|allowed):
    f=root/path
    if not f.exists():
     if path in initial:deleted.append(path)
     continue
    assert f.is_file() and not f.is_symlink(),path
    data=f.read_bytes();mode=stat.S_IMODE(f.stat().st_mode);info=tarfile.TarInfo(path);info.size=len(data);info.mode=mode;info.mtime=0;tar.addfile(info,io.BytesIO(data));rows.append({'path':path,'mode':oct(mode),'sha256':sha(data)})
  m={'baseline_head':head,'baseline_tree':tree,'scope_sha256':sha((EVIDENCE/(name+'-scope.json')).read_bytes()),'files':rows,'deleted':deleted,'archive_sha256':sha(archive.read_bytes()),'changed_paths':[r['path'] for r in rows if r['path'] not in initial or r['sha256']!=initial[r['path']]['sha256'] or r['mode']!=initial[r['path']]['mode']]};manifests[name]=m;(local/(name+'-source.json')).write_text(json.dumps(m,indent=2)+'\n')
 command=args.command_file.read_bytes();(local/'command.sh').write_bytes(command)
 record={'run_id':run,'host':'ubuntu','remote':directory,'state':'prepared','command_sha256':sha(command),'evidence':str(local)}
 def save(): (local/'run.json').write_text(json.dumps(record,indent=2)+'\n')
 save();print(json.dumps(record),flush=True)
 remote('umask 077; mkdir '+shlex.quote(directory))
 registry_git=EVIDENCE/'ani-registry-git-objects.json'
 if registry_git.exists():
  (local/registry_git.name).write_bytes(registry_git.read_bytes());record['registry_git_sha256']=sha(registry_git.read_bytes());save()
 uploads=['command.sh']+[n+'-source'+suffix for n in manifests for suffix in ('.tar.gz','.json')]
 if registry_git.exists():uploads.append(registry_git.name)
 for name in uploads:remote('cat > '+shlex.quote(directory+'/'+name),(local/name).read_bytes())
 verify='set -eu\ncd '+shlex.quote(directory)+'\nmkdir private\nchmod 700 private\n'
 if registry_git.exists():verify+='printf \'%s  ani-registry-git-objects.json\\n\' '+shlex.quote(record['registry_git_sha256'])+' | sha256sum -c -\n'
 for name,m in manifests.items():
  target='source' if name=='iam' else 'source-'+name
  verify+='printf \'%s  '+name+'-source.tar.gz\\n\' '+shlex.quote(m['archive_sha256'])+' | sha256sum -c -\nmkdir '+target+'\ntar -xzf '+name+'-source.tar.gz -C '+target+' --no-same-owner\npython3 source/tools/wr22/verify-source.py '+name+'-source.json '+target+'\n'
 print(remote(verify).decode(),flush=True)
 runner=r'''#!/bin/bash
set -euo pipefail
umask 077
cd "$(dirname "$0")"
echo $$ > command.pid
trap 'exit 143' TERM
trap 'exit 130' INT
trap 'result=$?; printf "%s\n" "$result" > "$WR22_RUN_DIR/command.exit"; date -u +%FT%TZ > "$WR22_RUN_DIR/command.finished"' EXIT
export WR22_RUN_DIR="$PWD"
exec 9>/home/ubuntu/.local/share/ani-iam/heavy.lock
export GOMEMLIMIT=1536MiB
date -u +%FT%TZ > command.waiting
flock 9
date -u +%FT%TZ > command.started
export WR22_RUN_DIR="$PWD"
export GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local TESTCONTAINERS_RYUK_DISABLED=true
export GOROOT=/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64
mkdir private/bin
ln -s /usr/bin/python3 private/bin/python
export PATH=$WR22_RUN_DIR/private/bin:/home/ubuntu/.local/share/ani-iam/bin:$GOROOT/bin:$PATH
if pgrep -af '(^|/)[g]o (test|build|run)|/[c]ompile -o|/[l]ink -o' > other-heavy-at-start.log; then echo 76 > command.exit; exit 76; fi
if test "$(awk '/MemAvailable/ {print $2}' /proc/meminfo)" -lt 2097152; then echo 75 > command.exit; exit 75; fi
(go version; buf --version; sqlc version; protoc-gen-go --version; protoc-gen-go-grpc --version; atlas version; docker version --format '{{.Server.Version}}'; sha256sum /home/ubuntu/.local/share/ani-iam/bin/*) > tools.txt
export GOWORK="$PWD/go.work"
go work init ./source ./source/api ./source/sdk ./source/examples/workload-grpc ./source-ani/repo/pkg ./source-ani/repo/services/ani-gateway ./source-ani/repo/services/envoy-authz-adapter ./source-ani/repo/services/inference-service ./source-ani/repo/sdks/core/go
if test -d source-notification; then go work use ./source-notification; fi
cd source
bash -euo pipefail ../command.sh > ../private/command.raw.log 2>&1
result=$?
printf '%s\n' "$result" > ../command.exit
date -u +%FT%TZ > ../command.finished
exit "$result"
'''
 remote('cat > '+shlex.quote(directory+'/runner.sh'),runner.encode())
 record['state']='dispatching';save()
 result=subprocess.run(ssh+['cd '+shlex.quote(directory)+'; nohup bash runner.sh </dev/null >dispatch.log 2>&1 &'],capture_output=True)
 record['state']='dispatched' if result.returncode==0 else 'inspect_same_run';record['dispatch_exit']=result.returncode;save();print(json.dumps(record),flush=True)
 raise SystemExit(result.returncode)
if __name__=='__main__':main()
