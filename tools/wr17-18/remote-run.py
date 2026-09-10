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
FREEZE = AUTHORITY / '.scratch/ani-iam-workload-refoundation/evidence/17-freeze-api-replacement-contracts'
EVIDENCE = AUTHORITY / '.scratch/ani-iam-workload-refoundation/evidence/18-refound-human-workload-runtime'
SSH = ['ssh', '-F', '/home/chabking/.ssh/config', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=12', 'ubuntu']
REMOTE_BASE = '/home/ubuntu/workspace/ani-iam-runs'


def sha(content):
    return hashlib.sha256(content).hexdigest()


def ssh(script, data=None):
    return subprocess.run(SSH + [script], input=data, check=True)


def snapshot(directory):
    scope = json.loads((FREEZE / 'implementation-scope.json').read_text())
    claim = json.loads((EVIDENCE / 'claim.json').read_text())
    assert sha((FREEZE / 'implementation-scope.json').read_bytes()) == claim['scope_sha256']
    assert scope['status'] == 'frozen_for_wr18'
    head, tree = subprocess.check_output(['git', '-C', str(SOURCE), 'rev-parse', 'HEAD', 'HEAD^{tree}'], text=True).splitlines()
    assert (head, tree) == (scope['baseline_head'], scope['baseline_tree'])
    initial = json.loads((FREEZE / 'source-input.json').read_text())
    names = {row['path'] for row in initial['files']}
    names.update(row['path'] for row in scope['files'] if row['group'] != 'documentation')
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
                        assert name in scope['renames'] or any(r['path'] == name and r['state'] == 'planned_create' for r in scope['files']), name
                        continue
                    assert path.is_file() and not path.is_symlink(), name
                    content = path.read_bytes()
                    mode = path.stat().st_mode & 0o777
                    rows.append({'path': name, 'sha256': sha(content), 'mode': oct(mode)})
                    member = tarfile.TarInfo(name)
                    member.size, member.mode, member.mtime = len(content), mode, 0
                    archive.addfile(member, io.BytesIO(content))
    manifest = {'baseline_head': head, 'baseline_tree': tree, 'files': rows, 'scope_sha256': claim['scope_sha256'], 'archive_sha256': sha(archive_path.read_bytes())}
    (directory / 'source.json').write_text(json.dumps(manifest, indent=2) + '\n')
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--command-file', type=Path, required=True, help='Reviewed shell commands without credentials; never logs private files')
    args = parser.parse_args()
    command = args.command_file.read_text()
    run_id = 'wr17-18-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    directory = EVIDENCE / 'runs' / run_id
    directory.mkdir(parents=True, exist_ok=False)
    remote = REMOTE_BASE + '/' + run_id
    manifest = snapshot(directory)
    (directory / 'command.sh').write_text(command)
    (directory / 'run.json').write_text(json.dumps({'run_id': run_id, 'remote': remote, 'ssh': SSH, 'source_sha256': manifest['archive_sha256'], 'state': 'prepared'}, indent=2) + '\n')
    print(json.dumps({'run_id': run_id, 'remote': remote, 'evidence': str(directory)}), flush=True)
    # New run only. No existing task directory or Network resources are changed.
    ssh('umask 077; mkdir -p ' + shlex.quote(REMOTE_BASE) + '; mkdir ' + shlex.quote(remote))
    for name in ['source.tar.gz', 'source.json', 'command.sh']:
        ssh('cat > ' + shlex.quote(remote + '/' + name), (directory / name).read_bytes())
    # The archive only contains validated, explicitly selected regular files.
    verify = f'''set -eu
cd {shlex.quote(remote)}
printf '%s  source.tar.gz\n' {shlex.quote(manifest['archive_sha256'])} | sha256sum -c -
mkdir source private
chmod 700 private
tar -xzf source.tar.gz -C source --no-same-owner
python3 source/tools/wr17-18/verify-source.py source.json source
'''
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
export WR18_RUN_DIR="$(dirname "$PWD")"
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
