from pathlib import Path
import json,hashlib,subprocess,tarfile,sys
root=Path('/home/chabking/workspace/ani-iam-wr17-18')
evidence=Path(__file__).parent
run=sys.argv[1]; directory=evidence/'runs'/run
ssh=['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','ubuntu']
for name in ['command.log','command.exit','command.started','command.finished','generation.json','generated.tar.gz']:
 data=subprocess.check_output(ssh+['cat /home/ubuntu/workspace/ani-iam-runs/'+run+'/'+name]);(directory/name).write_bytes(data)
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
source={r['path']:r for r in json.loads((directory/'source.json').read_text())['files']}
gen=json.loads((directory/'generation.json').read_text());assert gen['repeat']=='pass'
outputs={r['path']:r for r in gen['outputs']}
inputs=json.loads((evidence/'runs/wr17-18-20260910T012411Z-c9421616/merge.json').read_text())['inputs']
for name in inputs: assert sha(root/name)==source[name]['sha256'], 'generation input changed: '+name
for name in outputs: assert sha(root/name)==source[name]['sha256'], 'local generated file changed: '+name
stage=directory/'generated';stage.mkdir(exist_ok=False)
with tarfile.open(directory/'generated.tar.gz') as archive:
 assert {m.name for m in archive.getmembers()}==set(outputs)
 assert all(m.isfile() for m in archive.getmembers())
 archive.extractall(stage,filter='data')
for name,row in outputs.items(): assert sha(stage/name)==row['sha256']
for name in outputs: (root/name).write_bytes((stage/name).read_bytes())
(directory/'merge.json').write_text(json.dumps({'input_guard':'pass','generation_repeat':'pass','inputs':inputs,'outputs':list(outputs),'command_exit':(directory/'command.exit').read_text().strip()},indent=2)+'\n')
print(json.dumps({'run':run,'generation_merge':'pass','outputs':len(outputs),'command_exit':(directory/'command.exit').read_text().strip()}))
