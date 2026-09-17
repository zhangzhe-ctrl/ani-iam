set -euo pipefail
check formal-session-build ../source-session go build -o "$WR23_RUN_DIR/private/session-gateway" ./cmd/session-gateway
python3 - <<'PYSESSION'
import json,os,subprocess
from pathlib import Path
r=Path(os.environ['WR23_RUN_DIR']);b=json.loads(Path('tools/wr23-resume/session-budget.json').read_text());rows=[]
for i,image in enumerate([b['node_image'],b['shell_image']]):
 with (r/f'private/session-image-pull-{i}.log').open('wb') as log:subprocess.run(['docker','pull',image],stdout=log,stderr=subprocess.STDOUT,check=True)
 info=json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]
 assert info['Architecture']=='amd64' and any(x.endswith('@'+image.split('@')[1]) for x in info['RepoDigests'])
 rows.append({'reference':image,'id':info['Id'],'repo_digests':info['RepoDigests']})
(r/'session-images-results.json').write_text(json.dumps({'result':'pass','images':rows},indent=2)+'\n')
PYSESSION
