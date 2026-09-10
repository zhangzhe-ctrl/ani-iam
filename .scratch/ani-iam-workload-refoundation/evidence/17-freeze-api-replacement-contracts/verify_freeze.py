#!/usr/bin/env python3
"""Read-only WR-17 consistency/protection audit; runtime proof is separate."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile

EVIDENCE = Path(__file__).resolve().parent
ROOT = EVIDENCE.parents[3]
EFFORT = ROOT / '.scratch/ani-iam-workload-refoundation'
ALLOWED_DOCUMENTS = {
    '.scratch/ani-iam-workload-refoundation/issues/17-freeze-api-replacement-contracts.md',
    '.scratch/ani-iam-workload-refoundation/issues/18-refound-human-workload-runtime.md',
    '.scratch/ani-iam-workload-refoundation/spec.md',
    '.scratch/ani-iam-workload-refoundation/ticket-plan.md',
    '.scratch/ani-iam-workload-refoundation/capability-matrix.md',
    '.scratch/ani-iam-workload-refoundation/decisions.md',
    'docs/adr/0022-unify-software-actors-as-workload-principals.md',
    'docs/plans/plan-workload-principal-refoundation.md',
    'docs/agents/scaffolding-and-codegen.md',
}


def read_json(name):
    return json.loads((EVIDENCE / name).read_text())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    baseline = read_json('baseline.json')
    head, tree = subprocess.check_output(
        ['git', '-C', str(ROOT), 'rev-parse', 'HEAD', 'HEAD^{tree}'], text=True
    ).splitlines()
    assert (head, tree) == (baseline['head'], baseline['tree'])
    protected = 0
    for name, original in baseline['files'].items():
        if name in ALLOWED_DOCUMENTS:
            continue
        path = ROOT / name
        assert path.is_file() and not path.is_symlink(), name
        assert digest(path) == original['sha256'], name
        assert oct(path.stat().st_mode & 0o777) == original['mode'], name
        protected += 1
    before = EVIDENCE / 'before'
    snapshots = 0
    for path in before.rglob('*'):
        if not path.is_file():
            continue
        name = str(path.relative_to(before))
        assert digest(path) == baseline['files'][name]['sha256'], name
        snapshots += 1
    states = {}
    for path in (EFFORT / 'issues').glob('*.md'):
        match = re.search(r'^\*\*Status:\*\* (\S+)', path.read_text(), re.M)
        assert match, str(path)
        states[path.name] = match[1]
    claimed = [name for name, state in states.items() if state == 'claimed']
    phase = (states['17-freeze-api-replacement-contracts.md'], states['18-refound-human-workload-runtime.md'])
    assert phase in {('claimed', 'needs-triage'), ('resolved', 'needs-triage'), ('resolved', 'claimed'), ('resolved', 'resolved')}, phase
    expected_claimed = [name for name in ['17-freeze-api-replacement-contracts.md', '18-refound-human-workload-runtime.md'] if states[name] == 'claimed']
    assert claimed == expected_claimed, claimed
    assert states['16-organize-docs-and-replan.md'] == 'resolved'
    inventory = read_json('rpc-inventory.json')
    rows = inventory['rpcs']
    # Compare the recorded static inventory with actual declaration/definition
    # sites. Runtime dispatch, business success and authorization are later gates.
    declared = {}
    for path in sorted((ROOT / 'api/iam/v1').glob('*service.proto')):
        source_text = path.read_text()
        service_name = re.search(r'^service (\w+)\s*\{', source_text, re.M)[1]
        for match in re.finditer(r'^  rpc (\w+)\(', source_text, re.M):
            full_method = '/iam.v1.' + service_name + '/' + match[1]
            declared[full_method] = (str(path.relative_to(ROOT)), source_text[:match.start()].count('\n') + 1)
    handlers = {}
    for path in sorted((ROOT / 'internal/service').glob('*.go')):
        if path.name.endswith('_test.go'):
            continue
        source_text = path.read_text()
        for match in re.finditer(r'^func \(s \*(\w+Service)\) (\w+)\(ctx context.Context, request \*iamv1\.', source_text, re.M):
            full_method = '/iam.v1.' + match[1] + '/' + match[2]
            handlers[full_method] = {'path': str(path.relative_to(ROOT)), 'line': source_text[:match.start()].count('\n') + 1}
    gateway_allowlist = set(re.findall(r'"(/iam\.v1\.[^"]+)"', (ROOT / 'internal/server/workload_identity.go').read_text()))
    assert set(declared) == {row['source_rpc'] for row in rows}
    for row in rows:
        method = row['source_rpc']
        assert declared[method] == (row['source_proto'], row['source_line']), method
        assert row['handler'] == handlers.get(method), method
        assert row['formal_allowlist'] == (method in gateway_allowlist), method
    assert len(rows) == 69 and len({r['source_rpc'] for r in rows}) == 69
    assert sum(r['handler'] is not None for r in rows) == 29
    assert sum(r['handler'] is not None and not r['formal_allowlist'] for r in rows) == 11
    assert sum(r['wr18'] == 'adapt_and_regress' for r in rows) == 27
    assert all(r['completion_ticket'] and r['runtime_evidence'] == 'not_verified' for r in rows)
    matrix = (EVIDENCE / 'rpc-matrix.md').read_text()
    assert all(re.search(r'\| C%02d \|' % i, matrix) for i in range(1, 15))
    scope = read_json('implementation-scope.json')
    assert scope['status'] == 'frozen_for_wr18'
    for name, sha256 in scope['frozen_artifacts'].items():
        assert digest(EVIDENCE / name) == sha256, name
    names = [r['path'] for r in scope['files']]
    assert len(names) == len(set(names))
    for row in scope['files']:
        path = Path(row['path'])
        assert not path.is_absolute() and '..' not in path.parts
        if row['state'] == 'existing':
            assert (ROOT / path).is_file(), str(path)
        else:
            assert row['state'] == 'planned_create', str(path)
    manifest = read_json('isolation-manifest.json')
    source = read_json('source-input.json')
    assert digest(EVIDENCE / 'source-input.tar.gz') == source['archive_sha256']
    with tarfile.open(EVIDENCE / 'source-input.tar.gz', 'r:gz') as archive:
        assert archive.getnames() == [row['path'] for row in source['files']]
        for row, member in zip(source['files'], archive.getmembers()):
            assert member.isfile() and not member.issym() and not member.islnk()
            assert hashlib.sha256(archive.extractfile(member).read()).hexdigest() == row['sha256']
            assert oct(member.mode & 0o777) == row['mode']
    assert all('@sha256:' in ref for ref in manifest['images'].values())
    assert manifest['remote']['local_heavy_fallback'] is False
    assert manifest['resource_policy']['kubernetes'] == 'not used'
    assert manifest['resource_policy']['nats'] == 'not used'
    assert manifest['runtime_results'] == 'not_verified'
    fixture = manifest['fixture_contract']
    assert fixture['tenant_a'] != fixture['tenant_b']
    assert fixture['test_fixture_not_bootstrap'] is True
    source_registry = json.loads((Path(manifest['inputs']['ani']['root']) / 'repo/api/openapi/operation-registry.v1.json').read_text())
    source_operations = {item['operation_id']: item for item in source_registry['operations']}
    for operation, route in {
        'createInstanceExecSession': '/instances/{instance_id}/exec',
        'createInstanceConsoleSession': '/instances/{instance_id}/console',
    }.items():
        entry = source_operations[operation]
        assert entry['method'] == 'POST' and entry['path'] == route
        assert entry['permission'] == {'actions': ['create'], 'resource': 'instances', 'scope': 'tenant'}
    for name, repository in manifest['inputs'].items():
        assert re.fullmatch('[0-9a-f]{40}', repository['commit']), name
        assert re.fullmatch('[0-9a-f]{40}', repository['tree']), name
        for artifact in repository['artifacts']:
            assert digest(Path(repository['root']) / artifact['path']) == artifact['sha256'], (name, artifact['path'])
    decisions = (EFFORT / 'decisions.md').read_text()
    decision_states = {key: re.search(r'\| ' + key + r' \| ([^|]+)\|', decisions)[1].strip() for key in ['D01', 'D02']}
    assert decision_states == {'D01': 'accepted', 'D02': 'sync accepted / async pending'}
    print(json.dumps({
        'static_preparation': 'pass',
        'protected_original_files': protected,
        'before_snapshots': snapshots,
        'declared_rpc': 69,
        'implemented_handlers': 29,
        'wr18_usable_set': 27,
        'scope_files': len(names),
        'initial_source_files': len(source['files']),
        'claimed': claimed,
        'decisions': decision_states,
        'freeze_completion': 'pass',
        'future_core_async': 'pending_WR23',
        'runtime': 'not_verified',
    }, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
