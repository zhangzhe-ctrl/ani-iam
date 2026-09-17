#!/usr/bin/env python3
"""Collect an existing WR21 run. Never rerun commands or overwrite newer edits."""
import argparse,hashlib,json,subprocess,tarfile,io,stat
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
E=ROOT.parent/'ani-iam/.scratch/ani-iam-workload-refoundation/evidence/21-complete-tenant-workload-api-key'
def sha(data):return hashlib.sha256(data).hexdigest()
def main():
 p=argparse.ArgumentParser();p.add_argument('run');p.add_argument('--generated',action='store_true');a=p.parse_args()
 assert a.run.startswith('wr21-') and '/' not in a.run
 local=E/'runs'/a.run;record=json.loads((local/'run.json').read_text());remote=record['remote']
 ssh=['ssh','-F',str(Path.home()/'.ssh/config'),'ubuntu']
 def read(name):
  assert all(c.isalnum() or c in '.-_' for c in name)
  return subprocess.check_output(ssh+['cat '+remote+'/'+name],stderr=subprocess.DEVNULL)
 for name in ['command.pid','command.started','command.exit','command.finished','tools.txt','generation.json','stage-a-results.json','stage-b-results.json','stage-c-results.json','reference-events.jsonl','resources.jsonl','network-stopped.log','check-exits.txt','envoy-version.txt','phase-exits.txt','gates-exits.txt','gates-network-stopped.log','example-processes.jsonl','operator-stop.json']:
  try:(local/name).write_bytes(read(name))
  except subprocess.CalledProcessError:pass
 if not a.generated:return
 generation=json.loads((local/'generation.json').read_text());assert generation['repeat']=='pass'
 blob=read('generated.tar.gz');(local/'generated.tar.gz').write_bytes(blob)
 archive=tarfile.open(fileobj=io.BytesIO(blob));outputs={(x['repository'],x['path']):x for x in generation['outputs']}
 scopes={k:json.loads((E/(k+'-scope.json')).read_text()) for k in ('iam','ani')}
 manifests={k:{x['path']:x for x in json.loads((local/(k+'-source.json')).read_text())['files']} for k in scopes}
 # These exact generator inputs must still match this run; generated outputs are handled below.
 for k,manifest in manifests.items():
  root=Path(scopes[k]['root'])
  for name,row in manifest.items():
   relevant=(k=='iam' and (name.endswith(('.proto','.sql')) or 'buf.' in name or name=='sqlc.yaml' or name=='internal/data/cmd/genoperationregistry/main.go')) or (k=='ani' and name in ['repo/scripts/generate_gateway_authz.py','repo/scripts/gen_sdk_alpha.py','repo/scripts/generate_api_docs.py','repo/scripts/wr21_inference_contract.py','repo/api/openapi/v1.yaml','repo/api/openapi/services/v1.yaml','repo/api/openapi/wr19-workload-operation-registry.v1.json','repo/services/ani-gateway/tools/wr21_operation_registry.py','repo/services/ani-gateway/tools/dp2_operation_registry.py','repo/api/proto/inference/control/v1/inference_control.proto','repo/api/proto/buf.yaml','repo/api/proto/buf.gen.yaml'])
   if relevant and (k,name) not in outputs:assert sha((root/name).read_bytes())==row['sha256'],('newer generator input',k,name)
 pending=[]
 for member in archive.getmembers():
  k,name=member.name.split('/',1);assert (k,name) in outputs and member.isfile()
  data=archive.extractfile(member).read();assert sha(data)==outputs[k,name]['sha256']
  root=Path(scopes[k]['root']);target=root/name;previous=manifests[k].get(name)
  if target.exists() and target.read_bytes()==data:continue
  allowed={r['path'] for r in scopes[k]['files']};assert name in allowed,('generated output outside allowlist',k,name)
  assert (not target.exists() and previous is None) or (previous and sha(target.read_bytes())==previous['sha256']),('newer local output',k,name)
  pending.append((target,data,member.mode))
 for target,data,mode in pending:target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data);target.chmod(mode)
 (local/'generation-import.json').write_text(json.dumps({'archive_sha256':sha(blob),'imported':[str(x[0]) for x in pending],'input_consistency':'pass'},indent=2)+'\n')
 print(json.dumps({'generation_import':'pass','files':len(pending)}))
if __name__=='__main__':main()
