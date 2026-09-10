#!/usr/bin/env python3
"""Lightweight source, scope and evidence audit; does not rerun runtime tests."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile

E = Path(__file__).resolve().parent
ROOT = E.parents[3]
PRODUCT = Path('/home/chabking/workspace/ani-iam-wr17-18')
FROZEN = E.parent / '17-freeze-api-replacement-contracts'
FINAL = E / 'runs/wr17-18-20260910T021423Z-bca28bda'
COMPLETE = E / 'runs/wr17-18-20260910T020353Z-0f88e91f'


def read(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(*args):
    return subprocess.check_output(['git', '-C', str(PRODUCT), *args])


def main():
    scope = read(FROZEN / 'implementation-scope.json')
    claim = read(E / 'claim.json')
    assert sha(FROZEN / 'implementation-scope.json') == claim['scope_sha256']
    head, tree = git('rev-parse', 'HEAD', 'HEAD^{tree}').decode().splitlines()
    assert (head, tree) == (claim['baseline_head'], claim['baseline_tree'])
    allowed = {r['path'] for r in scope['files'] if r['group'] != 'documentation'}
    documents = {r['path'] for r in claim['documents']}
    for row in claim['documents']:
        p = PRODUCT / row['path']
        assert sha(p) == row['sha256'], row['path']
        assert oct(p.stat().st_mode & 0o777) == row['mode'], row['path']
    tracked = set(filter(None, git('diff', '--name-only', '--no-renames', 'HEAD').decode().splitlines()))
    new = set(filter(None, git('ls-files', '--others', '--exclude-standard').decode().splitlines()))
    assert (tracked | new) <= allowed | documents, sorted((tracked | new) - allowed - documents)
    changed = sorted((tracked | new) & allowed)
    git('diff', '--check', 'HEAD', '--', *sorted(allowed))
    manifest = read(FINAL / 'source.json')
    assert sha(FINAL / 'source.tar.gz') == manifest['archive_sha256']
    with tarfile.open(FINAL / 'source.tar.gz', 'r:gz') as archive:
        assert archive.getnames() == [r['path'] for r in manifest['files']]
        for row, member in zip(manifest['files'], archive.getmembers()):
            p = PRODUCT / row['path']
            assert p.is_file() and not p.is_symlink(), row['path']
            assert sha(p) == row['sha256'], row['path']
            assert oct(p.stat().st_mode & 0o777) == row['mode'], row['path']
            assert member.isfile() and not member.issym() and not member.islnk()
            assert hashlib.sha256(archive.extractfile(member).read()).hexdigest() == row['sha256']
            assert oct(member.mode & 0o777) == row['mode']
    source_names = {r['path'] for r in manifest['files']}
    assert all(p in source_names for p in changed if (PRODUCT / p).exists())
    generation = read(COMPLETE / 'generation.json')
    assert generation['repeat'] == 'pass'
    for row in generation['outputs']:
        assert sha(PRODUCT / row['path']) == row['sha256'], row['path']
    prior = {r['path']: r for r in read(COMPLETE / 'source.json')['files']}
    final = {r['path']: r for r in manifest['files']}
    assert prior.keys() == final.keys()
    changed_after_full_checks = sorted(p for p in final if final[p] != prior[p])
    assert changed_after_full_checks == read(FINAL / 'prior-check-applicability.json')['changed_files']
    assert all(p.startswith('tests/integration/') for p in changed_after_full_checks)
    for run in [COMPLETE, FINAL]:
        assert (run / 'command.exit').read_text().strip() == '0'
        results = read(run / 'test-results.json')
        assert results['top_level_counts'] == {'pass': 43, 'fail': 0, 'skip': 3}
        assert results['all_case_counts'] == {'pass': 67, 'fail': 0, 'skip': 3}
    remote = read(E / 'remote-final-state.json')
    assert not remote['remaining_registered_containers']
    assert remote['registered_containers'] == remote['terminated_containers']
    assert all(not row['still_running_this_goal_process'] for row in remote['formal_processes'])
    # Narrow literal scan, not a claim of universal secret detection. Runtime
    # credentials stay in private VM files; references alone are returned.
    patterns = {
        'private_key_literal': rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----\r?\n[A-Za-z0-9+/=\r\n]{64,}',
        'jwt_literal': rb'eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{30,}\.[A-Za-z0-9_-]{30,}',
        'github_token_literal': rb'gh[pousr]_[A-Za-z0-9]{30,}',
    }
    findings = []
    for name in changed:
        path = PRODUCT / name
        if path.is_file():
            for kind, pattern in patterns.items():
                if re.search(pattern, path.read_bytes()):
                    findings.append({'path': name, 'kind': kind})
    assert not findings, findings
    patch = git('diff', '--binary', '--no-ext-diff', 'HEAD', '--', *sorted(allowed))
    for name in sorted(new & allowed):
        result = subprocess.run(['git', 'diff', '--no-index', '--binary', '--', '/dev/null', name], cwd=PRODUCT, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        assert result.returncode == 1, name
        patch += result.stdout
    (E / 'product.diff').write_bytes(patch)
    report = {
        'result': 'pass', 'baseline_head': head, 'baseline_tree': tree,
        'product_worktree': str(PRODUCT), 'state_authority': str(ROOT),
        'source_archive_sha256': manifest['archive_sha256'],
        'source_files_verified': len(manifest['files']),
        'allowed_product_paths': len(allowed), 'copied_document_snapshots_verified': len(documents),
        'tracked_changed_product_files': len(tracked & allowed), 'untracked_product_files': len(new & allowed),
        'changed_paths': [{'path': p, 'sha256': sha(PRODUCT / p) if (PRODUCT / p).is_file() else None} for p in changed],
        'repeated_generated_outputs_verified': len(generation['outputs']),
        'changes_after_complete_checks': changed_after_full_checks,
        'secret_literal_scan': {'result': 'pass', 'patterns': list(patterns), 'findings': findings, 'scope': 'changed product files only; not universal credential detection'},
        'diff_whitespace': 'pass', 'product_diff_sha256': sha(E / 'product.diff'),
        'remote_cleanup': 'pass', 'remaining_registered_containers': [],
        'm1': 'not_verified', 'cross_service_chain': 'not_verified', 'production': 'not_verified',
    }
    (E / 'final-verification.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({k: report[k] for k in ['result', 'source_archive_sha256', 'source_files_verified', 'allowed_product_paths', 'tracked_changed_product_files', 'untracked_product_files', 'product_diff_sha256']}, ensure_ascii=False))


if __name__ == '__main__':
    main()
