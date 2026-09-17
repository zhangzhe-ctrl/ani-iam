#!/usr/bin/env python3
"""Remote-only owner generation from the exact frozen WR23 source archives."""
import hashlib
import json
import os
import shutil
import subprocess
import tarfile
from pathlib import Path

run = Path(os.environ['WR23_RESUME_RUN_DIR'])
spec = json.loads((run / 'run.json').read_text())
source = json.loads((run / 'source.json').read_text())
first = run / 'source-ani'
repeat = run / 'repeat-ani'
repeat.mkdir()
with tarfile.open(run / 'ani.tar') as tf:
    tf.extractall(repeat, filter='data')
assert (first / '.git').is_dir(), 'owner generation needs fixed Git metadata'
shutil.copytree(first / '.git', repeat / '.git')
allowed = set(spec['repositories']['ani']['allowed'])
sdk_inputs = {f['path']: f for f in source['ani']['files'] if f['path'].startswith('repo/sdks/core/')}
names = [p for p in allowed if p.endswith('.go') and not p.endswith('.pb.go') and (first / p).exists()]
names += ['repo/api/openapi/wr23-core-lifecycle-operation-registry.v1.json','repo/pkg/go.mod','repo/pkg/go.sum']
names += sorted(p for p in allowed if p.startswith('repo/sdks/core/'))
names += sorted(sdk_inputs)
names += ['repo/docs/api/core.html','repo/docs/api/index.html']
inputs = [{'repository': 'ani', 'path': p, 'sha256': hashlib.sha256((first / p).read_bytes()).hexdigest()}
          for p in ['repo/api/openapi/v1.yaml', 'repo/scripts/workload_http_targets.py', 'repo/scripts/generate_gateway_authz.py',
                    'repo/services/ani-gateway/tools/wr23_core_registry.py', 'repo/scripts/gen_sdk_alpha.py', 'repo/scripts/generate_api_docs.py']]
inputs.append({'repository':'iam','path':'tools/wr23-resume/owners-generation.py','sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
for root in [first, repeat]:
    subprocess.run(['go','mod','edit','-require=github.com/nats-io/nats.go@v1.52.0','-require=github.com/nats-io/nkeys@v0.4.16'],cwd=root/'repo/pkg',check=True)
    subprocess.run(['go','mod','download','github.com/nats-io/nats.go@v1.52.0','github.com/nats-io/nkeys@v0.4.16'],cwd=root/'repo/pkg',check=True)
    subprocess.run(['python3', 'tools/wr23_core_registry.py'], cwd=root / 'repo/services/ani-gateway', check=True)
    subprocess.run(['python3', 'scripts/generate_gateway_authz.py'], cwd=root / 'repo', check=True)
    subprocess.run(['python3', '-c', "import sys; from pathlib import Path; sys.path.insert(0,'scripts'); from gen_sdk_alpha import generate_layer,LAYERS; generate_layer(Path.cwd(),'core',LAYERS['core'])"], cwd=root / 'repo', check=True)
    # SDK scaffolding recreates files under the private run umask. Preserve
    # their frozen source modes without changing the umask used for secrets.
    for name, initial in sdk_inputs.items():
        (root / name).chmod(int(initial['mode'], 8))
    subprocess.run(['python3', 'scripts/generate_api_docs.py'], cwd=root / 'repo', check=True)
    subprocess.run(['gofmt', '-w'] + [p for p in names if p.endswith('.go')], cwd=root, check=True)

registry = first / 'repo/api/openapi/wr23-core-lifecycle-operation-registry.v1.json'
registry_hash = hashlib.sha256(registry.read_bytes()).hexdigest()
outputs = []
for index in range(2):
    output = run / ('policy-' + str(index) + '.go')
    sql = run / 'private' / ('policy-' + str(index) + '.sql')
    subprocess.run(['go', 'run', './internal/data/cmd/genoperationregistry', '-input', str(registry),
                    '-go-output', str(output), '-sql-output', str(sql), '-expected-sha256', registry_hash], cwd=run / 'source', check=True)
    outputs.append(output.read_bytes())
assert outputs[0] == outputs[1]
# The accepted DLQ amendment adds exactly three finite Human operations.
# Previously frozen operations retain their complete authorization semantics.
old_registry = run / 'private' / 'input-registry.json'
with tarfile.open(run / 'ani.tar') as tf:
    old_registry.write_bytes(tf.extractfile('repo/api/openapi/wr23-core-lifecycle-operation-registry.v1.json').read())
old = json.loads(old_registry.read_bytes())
new = json.loads(registry.read_bytes())
dlq = {'listCoreIAMDLQEntries','getCoreIAMDLQEntry','replayCoreIAMDLQEntry'}
old_ops = {x['operation_id']:x for x in old['operations']}
new_ops = {x['operation_id']:x for x in new['operations']}
assert new_ops.keys() == old_ops.keys() | dlq, 'undeclared Human operation added or removed'
assert all(new_ops[k] == v for k,v in old_ops.items()), 'existing Human policy changed'
assert all(new_ops[k]['backend_owner']=='iam' and new_ops[k]['permission']['resource']=='iam.dlq' for k in dlq)

iam = run / 'source'
(iam / 'internal/data/generated_operation_policies.go').write_bytes(outputs[0])
(iam / 'tests/contracts/workload-operation-registry.v1.json').write_bytes(registry.read_bytes())
pins = iam / 'tests/contracts/contract_pins.json'
document = json.loads(pins.read_text())
document['wr23'].update(registry_sha256=registry_hash, policy_revision=new['policy_revision'],
                       core_owner_openapi_sha256=hashlib.sha256((first / 'repo/api/openapi/v1.yaml').read_bytes()).hexdigest())
pins.write_text(json.dumps(document, indent=2) + '\n')
rows = []
with tarfile.open(run / 'owners-generated.tar.gz', 'w:gz') as out:
    for owner, root, paths in [('ani', first, sorted(set(names))), ('iam', iam, ['internal/data/generated_operation_policies.go', 'tests/contracts/workload-operation-registry.v1.json', 'tests/contracts/contract_pins.json'])]:
        initial = {f['path']: f for f in source[owner]['files']}
        for name in paths:
            value = (root / name).read_bytes()
            if owner == 'ani':
                assert value == (repeat / name).read_bytes(), name
            digest = hashlib.sha256(value).hexdigest()
            mode = oct((root / name).stat().st_mode & 0o777)
            changed = digest != initial.get(name, {}).get('sha256') or mode != initial.get(name, {}).get('mode')
            assert not changed or name in spec['repositories'][owner]['allowed'], name
            rows.append({'repository': owner, 'path': name, 'sha256': digest, 'mode': mode, 'changed': changed})
            if changed:
                out.add(root / name, arcname=owner + '/' + name, recursive=False)
(run / 'owners-generation.json').write_text(json.dumps({'result': 'pass', 'inputs': inputs, 'outputs': rows}, indent=2) + '\n')
print('WR23 owner generation repeat pass;', len(rows), 'outputs')
