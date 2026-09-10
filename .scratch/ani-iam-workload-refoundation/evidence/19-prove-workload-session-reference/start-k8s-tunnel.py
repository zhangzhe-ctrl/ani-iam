#!/usr/bin/env python3
"""Task-owned loopback SOCKS route via SSH ani; no keys/config copied to VM."""
import datetime
import json
from pathlib import Path
import socket
import subprocess
import time

root=Path(__file__).resolve().parent
record=root/'k8s-tunnel.json'
if record.exists(): raise SystemExit('Inspect registered tunnel before restarting')
ssh=['ssh','-F','/home/chabking/.ssh/config','-o','BatchMode=yes','-o','ConnectTimeout=12','-o','ExitOnForwardFailure=yes','-o','ServerAliveInterval=15','-o','ServerAliveCountMax=3','-o','ControlMaster=no','-o','ControlPath=none']
with socket.socket() as s:
 s.bind(('127.0.0.1',0));local_port=s.getsockname()[1]
query="python3 -c 'import socket;s=socket.socket();s.bind((\"127.0.0.1\",0));print(s.getsockname()[1]);s.close()'"
remote_port=int(subprocess.check_output(ssh+['ubuntu',query],text=True).strip())
processes=[]
try:
 for name,args in [('ani-socks',['-N','-D',f'127.0.0.1:{local_port}','ani']),('ubuntu-reverse',['-N','-R',f'127.0.0.1:{remote_port}:127.0.0.1:{local_port}','ubuntu'])]:
  log=(root/(name+'.log')).open('x')
  p=subprocess.Popen(ssh+args,stdin=subprocess.DEVNULL,stdout=log,stderr=log,start_new_session=True);log.close()
  processes.append({'name':name,'pid':p.pid,'process':p,'command':ssh+args,'start_ticks':Path(f'/proc/{p.pid}/stat').read_text().split()[21]})
  time.sleep(1)
  if p.poll() is not None:raise RuntimeError('Tunnel startup failed; inspect task-specific nonsecret SSH log')
 payload={'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'local_socks_port':local_port,'remote_loopback_proxy':'socks5://127.0.0.1:'+str(remote_port),'processes':[{k:v for k,v in row.items() if k!='process'} for row in processes],'purpose':'only WR19 Kubernetes calls; original API hostname/CA verification retained','cleanup':'close only these registered local SSH PIDs after the real-chain runs; retained cluster fixtures stay'}
 record.write_text(json.dumps(payload,indent=2)+'\n')
 print(json.dumps({'proxy':payload['remote_loopback_proxy'],'registered_pids':[r['pid'] for r in processes]}))
except Exception:
 for row in processes:row['process'].terminate()
 raise
