#!/usr/bin/env python3
"""Render this run's finite NKey/ACL environment; seeds never enter the input.

The result is configuration preparation, not proof that a server applied it.
The actual server and permission probes must separately verify the revision.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
from remote_environment import verify_runtime

p=argparse.ArgumentParser()
p.add_argument('--manifest',type=Path,required=True)
p.add_argument('--approved-sha256',required=True)
p.add_argument('--state',choices=['active','producer_denied','consumer_denied','producer_removed','consumer_removed'],required=True)
p.add_argument('--output',type=Path,required=True)
p.add_argument('--proof',type=Path,required=True)
a=p.parse_args()
raw=a.manifest.read_bytes()
assert hashlib.sha256(raw).hexdigest()==a.approved_sha256
m=json.loads(raw)
assert set(m)=={'version','run_id','broker_name','account','stream','consumer','public_keys','inbox_prefix'} and m['version']==1
assert re.fullmatch(r'wr23-resume-[0-9TZ]+-[a-f0-9]{8}',m['run_id'])
for field in ['broker_name','account','stream','consumer']:
 assert re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,95}',m[field])
assert set(m['public_keys'])=={'operator','producer','consumer','other'}
assert len(set(m['public_keys'].values()))==4
for key in m['public_keys'].values():assert re.fullmatch(r'U[A-Z2-7]{55}',key)
assert re.fullmatch(r'_WR23[.][A-Za-z0-9_-]{1,96}',m['inbox_prefix'])
run=Path('/home/ubuntu/workspace/ani-iam-runs')/m['run_id']
verify_runtime(run)
assert a.output.is_absolute() and a.proof.is_absolute()
assert a.output.resolve().is_relative_to(run/'private') and a.proof.resolve().parent==run
assert not a.output.exists() and not a.proof.exists()
stream,consumer=m['stream'],m['consumer']
subjects=['ani.integration.tenant.lifecycle.v1','ani.integration.tenant.iam-bootstrap.v1','ani.integration.tenant.lifecycle-heartbeat.v1']
api='$JS.API.'
permissions={
 'operator':{'publish':{'allow':[api+'INFO',api+'STREAM.CREATE.'+stream,api+'STREAM.UPDATE.'+stream,api+'STREAM.INFO.'+stream,api+'STREAM.MSG.GET.'+stream,api+'CONSUMER.CREATE.'+stream+'.'+consumer,api+'CONSUMER.DURABLE.CREATE.'+stream+'.'+consumer,api+'CONSUMER.INFO.'+stream+'.'+consumer]},'subscribe':{'allow':[m['inbox_prefix']+'.operator.>']}},
 'producer':{'publish':{'allow':subjects+[api+'STREAM.INFO.'+stream]},'subscribe':{'allow':[m['inbox_prefix']+'.producer.>']}},
 'consumer':{'publish':{'allow':[api+'STREAM.INFO.'+stream,api+'CONSUMER.INFO.'+stream+'.'+consumer,api+'CONSUMER.MSG.NEXT.'+stream+'.'+consumer,'$JS.ACK.'+stream+'.'+consumer+'.>']},'subscribe':{'allow':[m['inbox_prefix']+'.consumer.>']}},
 'other':{'publish':{'deny':['>']},'subscribe':{'deny':['>']}},
}
users=[]
for role,key in m['public_keys'].items():
 if a.state==role+'_removed':continue
 permission=permissions[role]
 if a.state==role+'_denied':permission={'publish':{'deny':['>']},'subscribe':{'deny':['>']}}
 users.append({'nkey':key,'permissions':permission})
# Imports, exports, operators, accounts other than this one, system account,
# cluster routes, gateways and leafnodes are deliberately absent from this
# bounded standalone server. Stream transforms are checked through JetStream.
config={'server_name':m['broker_name'],'listen':'0.0.0.0:4222','http':'0.0.0.0:8222',
 'max_payload':65536,'max_connections':32,'write_deadline':'2s','pid_file':'/tmp/wr23-nats.pid',
 'jetstream':{'store_dir':'/data','max_memory_store':33554432,'max_file_store':268435456},
 'tls':{'cert_file':'/run/wr23/server.crt','key_file':'/run/wr23/server.key','ca_file':'/run/wr23/ca.crt','timeout':2,'handshake_first':True},
 'accounts':{m['account']:{'jetstream':'enabled','users':users}}}
value=(json.dumps(config,indent=2)+'\n').encode()
a.output.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
fd=os.open(a.output,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'wb') as f:f.write(value)
proof={'state':'prepared_not_applied','manifest_sha256':a.approved_sha256,'configuration_sha256':hashlib.sha256(value).hexdigest(),'scenario':a.state,'run_id':m['run_id'],'broker_name':m['broker_name'],'account':m['account'],'exclusive_publishers':{'active':a.state not in ['producer_denied','producer_removed'],'public_key':m['public_keys']['producer'],'subjects':subjects},'permissions':users,'no_import_export_transform_routes':True}
a.proof.write_text(json.dumps(proof,indent=2)+'\n')
print(json.dumps({'configuration_sha256':proof['configuration_sha256'],'state':'prepared_not_applied'}))
