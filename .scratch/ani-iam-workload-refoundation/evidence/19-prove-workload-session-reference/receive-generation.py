import pathlib,subprocess,tarfile,json,hashlib,shutil,sys
base=pathlib.Path(__file__).resolve().parent; run=base/'runs'/sys.argv[1];src=pathlib.Path('/home/chabking/workspace/ani-iam-wr19');remote='/home/ubuntu/workspace/ani-iam-runs/'+run.name
assert run.name.startswith('wr19-') and run.parent==base/'runs'
ssh=['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','ubuntu']
for name in ('generation.json','generated.tar.gz','command.log','command.exit'):
 with (run/name).open('wb') as f:subprocess.run(ssh+['cat '+remote+'/'+name],stdout=f,check=True)
assert (run/'command.exit').read_text().strip()=='0'
initial=json.loads((run/'source.json').read_text());rows={r['path']:r for r in initial['files']}
for name in tuple(r for r in rows if r.startswith('api/iam/v1/') and r.endswith(('.proto','.yaml','.lock'))) + ('sqlc.yaml','internal/data/queries/persistence.sql','internal/data/queries/workload_bootstrap.sql')+tuple(r for r in rows if r.startswith('migrations/') and r.endswith('.sql')):
 assert hashlib.sha256((src/name).read_bytes()).hexdigest()==rows[name]['sha256'],name
outputs=json.loads((run/'generation.json').read_text())['outputs'];tmp=run/'returned-generated';tmp.mkdir(exist_ok=False)
allowed={r['path'] for r in json.loads((base/'implementation-scope.json').read_text())['files'] if r['reason']=='generated'}
assert {r['path'] for r in outputs}<=allowed
with tarfile.open(run/'generated.tar.gz') as t:
 assert set(t.getnames())=={r['path'] for r in outputs}
 assert all(m.isfile() for m in t.getmembers())
 t.extractall(tmp,filter='data')
for r in outputs:
 p=tmp/r['path'];assert hashlib.sha256(p.read_bytes()).hexdigest()==r['sha256']
 target=src/r['path']
 if r['path'] in rows: assert hashlib.sha256(target.read_bytes()).hexdigest()==rows[r['path']]['sha256'],r['path']
 elif target.exists():raise RuntimeError('unexpected new target: '+r['path'])
for r in outputs:shutil.copy2(tmp/r['path'],src/r['path'])
print('Applied exact verified SQL generation outputs:',len(outputs))
