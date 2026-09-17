#!/usr/bin/env python3
import hashlib,json,stat,sys
from pathlib import Path
m=json.loads(Path(sys.argv[1]).read_text());root=Path(sys.argv[2])
for row in m['files']:
 p=root/row['path'];assert p.is_file() and not p.is_symlink(),row['path']
 assert hashlib.sha256(p.read_bytes()).hexdigest()==row['sha256'],row['path']
 assert oct(stat.S_IMODE(p.stat().st_mode))==row['mode'],row['path']
for path in m['deleted']:assert not (root/path).exists(),path
print(json.dumps({'source_consistency':'pass','root':str(root),'files':len(m['files']),'deleted':m['deleted']}))
