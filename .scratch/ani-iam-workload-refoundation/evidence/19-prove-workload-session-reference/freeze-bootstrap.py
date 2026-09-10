import pathlib,json,hashlib,datetime
base=pathlib.Path(__file__).parent
src=pathlib.Path('/home/chabking/workspace/ani-iam-wr17-18')
groups={
'formal_provisioner': ['cmd/server/main.go','cmd/server/provision_workloads.go','cmd/server/provision_workloads_test.go'],
'bootstrap_domain':['internal/biz/workload_bootstrap.go','internal/biz/workload_bootstrap_test.go'],
'bootstrap_transaction':['internal/data/workload_bootstrap.go','internal/data/workload.go','internal/data/queries/workload_bootstrap.sql','migrations/202609100003_workload_bootstrap.sql'],
'generated':['internal/data/sqlcgen/db.go','internal/data/sqlcgen/models.go','internal/data/sqlcgen/querier.go','internal/data/sqlcgen/persistence.sql.go','internal/data/sqlcgen/workload_bootstrap.sql.go','migrations/atlas.sum'],
'verification':['tests/integration/workload_bootstrap_test.go','tests/integration/isolation_test.go','tests/integration/workload_runtime_test.go','tests/integration/formal_runtime_test.go','tests/integration/persistence_test.go'],
'remote_execution':['tools/wr19/remote-run.py','tools/wr19/verify-source.py','tools/wr19/generate.sh','tools/wr19/README.md']}
rows=[]
for reason,paths in groups.items():
 for name in paths:
  p=src/name
  rows.append({'repository':'ani-iam','path':name,'state':'existing' if p.exists() else 'planned_create','source_sha256':hashlib.sha256(p.read_bytes()).hexdigest() if p.exists() else None,'reason':reason,'acceptance':['B','6','7','9','10','11']})
result={'status':'frozen_for_bootstrap','frozen_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'baseline':'baseline.json','scope_stage':'WR-19 B, independent of resource environment and pending established-connection policy','files':rows,'generation':[{'inputs':['migrations/*.sql (WR18 immutable inputs plus exact new migration above)','internal/data/queries/persistence.sql','internal/data/queries/workload_bootstrap.sql','sqlc.yaml'],'outputs':[r['path'] for r in rows if r['reason']=='generated'],'tools':['sqlc 1.31.1','atlas community 1.3.0','Go 1.26.7'], 'execution':'ubuntu only'}],'forbidden':['all unlisted product paths','other repositories product edits until their exact scope is frozen','existing DBs/credentials/cluster resources','Git staging/commit/push/tag/PR','WR20+'],'authority':'original IAM issue19 and ticket-plan; product worktree documentation is source snapshot only','next_stages':'Remaining WR19 caller/receiver/contract paths must be concretely recorded before their edits. No scope expansion beyond the user Goal. Full WR19 acceptance remains mandatory.'}
(base/'implementation-scope.json').write_text(json.dumps(result,indent=2)+'\n')
print('Bootstrap scope frozen:',len(rows),'files')
