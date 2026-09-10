#!/usr/bin/env python3
"""Snapshot only frozen inputs; run one serial, recoverable command on ubuntu.

This helper never retries a dispatched command. Inspect the recorded remote
directory, command.pid/log/exit after an uncertain SSH result.
"""
import argparse
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import tarfile
import uuid

SOURCE = Path(__file__).resolve().parents[2]
AUTHORITY = Path('/home/chabking/workspace/ani-iam')
FREEZE = AUTHORITY / '.scratch/ani-iam-workload-refoundation/evidence/19-prove-workload-session-reference'
EVIDENCE = FREEZE
WR18 = AUTHORITY / '.scratch/ani-iam-workload-refoundation/evidence/18-refound-human-workload-runtime/runs/wr17-18-20260910T021423Z-bca28bda'
SSH = ['ssh', '-F', '/home/chabking/.ssh/config', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=12', 'ubuntu']
REMOTE_BASE = '/home/ubuntu/workspace/ani-iam-runs'


def sha(content):
    return hashlib.sha256(content).hexdigest()


def ssh(script, data=None):
    return subprocess.run(SSH + [script], input=data, check=True)


def snapshot(directory):
    scope = json.loads((FREEZE / 'implementation-scope.json').read_text())
    scope_sha = sha((FREEZE / 'implementation-scope.json').read_bytes())
    assert scope['status'] in ('frozen_for_bootstrap','frozen_for_iam_invocation')
    head, tree = subprocess.check_output(['git', '-C', str(SOURCE), 'rev-parse', 'HEAD', 'HEAD^{tree}'], text=True).splitlines()
    initial = json.loads((WR18 / 'source.json').read_text())
    assert (head, tree) == (initial['baseline_head'], initial['baseline_tree'])
    names = {row['path'] for row in initial['files']}
    allowed = {row['path'] for row in scope['files']}
    names.update(allowed)
    for row in initial['files']:
        if row['path'] not in allowed:
            assert sha((SOURCE/row['path']).read_bytes()) == row['sha256'], 'unlisted source drift: '+row['path']
    rows = []
    archive_path = directory / 'source.tar.gz'
    with archive_path.open('wb') as raw:
        # gzip filename and mtime are fixed; manifest identifies content/modes.
        import gzip
        with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as archive:
                for name in sorted(names):
                    path = SOURCE / name
                    if not path.exists():
                        assert any(r['path'] == name and r['state'] == 'planned_create' for r in scope['files']), name
                        continue
                    assert path.is_file() and not path.is_symlink(), name
                    content = path.read_bytes()
                    mode = path.stat().st_mode & 0o777
                    rows.append({'path': name, 'sha256': sha(content), 'mode': oct(mode)})
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = len(content), mode, 0
                    archive.addfile(member, io.BytesIO(content))
    manifest = {'baseline_head': head, 'baseline_tree': tree, 'files': rows, 'scope_sha256': scope_sha, 'archive_sha256': sha(archive_path.read_bytes())}
    (directory / 'source.json').write_text(json.dumps(manifest, indent=2) + '\n')
    return manifest


def snapshot_external(directory, kind):
    root, expected = {'session': ('ani-session-gateway-wr19', 'frozen_for_session_creation_adapter'), 'ani': ('ANI-wr19', 'frozen_for_gateway_reference')}[kind]
    source = Path('/home/chabking/workspace') / root
    scope_path = FREEZE / (kind + '-implementation-scope.json')
    inputs_path = FREEZE / (kind + '-source-inputs.json')
    scope, inputs = json.loads(scope_path.read_text()), json.loads(inputs_path.read_text())
    assert scope['status'] == expected
    head, tree = subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD', 'HEAD^{tree}'], text=True).splitlines()
    assert (head, tree) == (scope['baseline_head'], scope['baseline_tree'])
    allowed = {r['path'] for r in scope['files']}
    changed = set(filter(None, subprocess.check_output(['git', '-C', str(source), 'diff', '--name-only', '-z', 'HEAD'], text=True).split('\0')))
    untracked = set(filter(None, subprocess.check_output(['git', '-C', str(source), 'ls-files', '--others', '--exclude-standard', '-z'], text=True).split('\0')))
    assert changed | untracked <= allowed, sorted((changed | untracked) - allowed)
    names = set(inputs['paths']) | allowed
    rows = []
    archive_path = directory / (kind + '-source.tar.gz')
    import gzip
    with archive_path.open('wb') as raw:
        with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode='w') as archive:
                for name in sorted(names):
                    assert not Path(name).is_absolute() and '..' not in Path(name).parts
                    path = source / name
                    if not path.exists():
                        assert any(r['path'] == name and r['state'] == 'planned_create' for r in scope['files']), name
                        continue
                    assert path.is_file() and not path.is_symlink(), name
                    content, mode = path.read_bytes(), path.stat().st_mode & 0o777
                    rows.append({'path': name, 'sha256': sha(content), 'mode': oct(mode)})
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = len(content), mode, 0
                    archive.addfile(member, io.BytesIO(content))
    manifest = {'baseline_head': head, 'baseline_tree': tree, 'scope_sha256': sha(scope_path.read_bytes()), 'input_list_sha256': sha(inputs_path.read_bytes()), 'files': rows, 'archive_sha256': sha(archive_path.read_bytes())}
    (directory / (kind + '-source.json')).write_text(json.dumps(manifest, indent=2)+'\n')
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--command-file', type=Path, required=True, help='Reviewed shell commands without credentials; never logs private files')
    parser.add_argument('--session-source', action='store_true', help='Include separately frozen Session worktree inputs; never use original checkout')
    parser.add_argument('--ani-source', action='store_true', help='Include separately frozen ANI reference worktree inputs')
    args = parser.parse_args()
    command = args.command_file.read_text()
    run_id = 'wr19-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    # Reuse one task-owned authenticated connection for serial uploads. A burst
    # of fresh SSH handshakes can be rejected before dispatch. This adds no
    # command retry, and the private master expires after 30 idle seconds.
    SSH[-1:-1] = ['-o', 'ControlMaster=auto', '-o', 'ControlPersist=30',
                  '-o', 'ControlPath=/tmp/' + run_id + '.sock',
                  '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=3']
    directory = EVIDENCE / 'runs' / run_id
    directory.mkdir(parents=True, exist_ok=False)
    remote = REMOTE_BASE + '/' + run_id
    manifest = snapshot(directory)
    external = {}
    if args.session_source:
        external['session'] = snapshot_external(directory, 'session')
    if args.ani_source:
        external['ani'] = snapshot_external(directory, 'ani')
    (directory / 'command.sh').write_text(command)
    (directory / 'run.json').write_text(json.dumps({'run_id': run_id, 'remote': remote, 'ssh': SSH, 'source_sha256': manifest['archive_sha256'], 'state': 'prepared'}, indent=2) + '\n')
    print(json.dumps({'run_id': run_id, 'remote': remote, 'evidence': str(directory)}), flush=True)
    # New run only. No existing task directory or Network resources are changed.
    ssh('umask 077; mkdir -p ' + shlex.quote(REMOTE_BASE) + '; mkdir ' + shlex.quote(remote))
    for name in ['source.tar.gz', 'source.json', 'command.sh']:
        ssh('cat > ' + shlex.quote(remote + '/' + name), (directory / name).read_bytes())
    for kind in external:
        for name in [kind + '-source.tar.gz', kind + '-source.json']:
            ssh('cat > ' + shlex.quote(remote + '/' + name), (directory / name).read_bytes())
    # The archive only contains validated, explicitly selected regular files.
    verify = f'''set -eu
cd {shlex.quote(remote)}
printf '%s  source.tar.gz\n' {shlex.quote(manifest['archive_sha256'])} | sha256sum -c -
mkdir source private
chmod 700 private
tar -xzf source.tar.gz -C source --no-same-owner
python3 source/tools/wr19/verify-source.py source.json source
'''
    for kind, item in external.items():
        verify += f"""printf '%s  {kind}-source.tar.gz\\n' {shlex.quote(item['archive_sha256'])} | sha256sum -c -
mkdir source-{kind}
tar -xzf {kind}-source.tar.gz -C source-{kind} --no-same-owner
python3 source/tools/wr19/verify-source.py {kind}-source.json source-{kind}
"""
    ssh(verify)
    runner = '''#!/bin/bash
set -u
umask 077
cd "$(dirname "$0")"
echo $$ > command.pid
exec 9>/home/ubuntu/.local/share/ani-iam/heavy.lock
flock -n 9 || { echo 73 > command.exit; exit 73; }
date -u +%FT%TZ > command.started
cd source
export GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local TESTCONTAINERS_RYUK_DISABLED=true
export GOROOT=/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64
export PATH=/home/ubuntu/.local/share/ani-iam/bin:$GOROOT/bin:$PATH
export WR19_RUN_DIR="$(dirname "$PWD")"
if test -f api/go.mod; then
    (cd ..; go work init ./source ./source/api; go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/api@v0.1.0-rc.1=./source/api)
    export GOWORK="$WR19_RUN_DIR/go.work"
    if test -f sdk/go.mod; then
        (cd ..; go work use ./source/sdk; go work edit -replace=github.com/zhangzhe-ctrl/ani-iam/sdk@v0.1.0-rc.1=./source/sdk)
    fi
fi
if pgrep -af '(^|/)[g]o (test|build|run)|/[c]ompile -o|/[l]ink -o' > ../other-heavy-at-start.log; then
    echo 76 > ../command.exit
    exit 76
fi
test $(awk '/MemAvailable/ {print $2}' /proc/meminfo) -ge 2097152 || { echo 75 > ../command.exit; exit 75; }
bash -euo pipefail ../command.sh > ../command.log 2>&1
result=$?
printf '%s\n' "$result" > ../command.exit
date -u +%FT%TZ > ../command.finished
exit "$result"
'''
    ssh('cat > ' + shlex.quote(remote + '/runner.sh'), runner.encode())
    # Dispatch is the point after which no automatic replay is permitted.
    record = json.loads((directory / 'run.json').read_text())
    record['state'] = 'dispatching'
    (directory / 'run.json').write_text(json.dumps(record, indent=2) + '\n')
    result = subprocess.run(SSH + ['cd ' + shlex.quote(remote) + '; nohup bash runner.sh </dev/null >dispatch.log 2>&1 &'])
    record['dispatch_exit'] = result.returncode
    record['state'] = 'inspect_same_run' if result.returncode else 'dispatched'
    (directory / 'run.json').write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps(record), flush=True)
    raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
