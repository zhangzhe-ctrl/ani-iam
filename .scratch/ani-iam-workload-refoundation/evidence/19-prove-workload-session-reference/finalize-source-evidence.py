#!/usr/bin/env python3
import pathlib,json,subprocess,hashlib,difflib,datetime,tarfile
p=pathlib.Path(__file__).resolve().parent
run='wr19-20260910T082134Z-2c35dca1'
previous='wr19-20260910T080445Z-be0b89ea'
restored=json.loads((p/'worktree-restoration.json').read_text())
baseline=json.loads((p/'baseline.json').read_text())
root=p.parents[3]
def sha(raw):return hashlib.sha256(raw).hexdigest()
def git(folder,*args):return subprocess.check_output(['git','-C',str(folder),*args])
wr18=root/'.scratch/ani-iam-workload-refoundation/evidence/18-refound-human-workload-runtime/runs/wr17-18-20260910T021423Z-bca28bda'
inherited=json.loads((wr18/'source.json').read_text())
assert inherited['archive_sha256']=='6727e79febbdec2d549601a7ccc966d399d0ac46e278bc0adb24ff2fc2b8f42f'
assert sha((wr18/'source.tar.gz').read_bytes())==inherited['archive_sha256']
original18=pathlib.Path('/home/chabking/workspace/ani-iam-wr17-18')
assert all(sha((original18/r['path']).read_bytes())==r['sha256'] for r in inherited['files'])
assert all(not (original18/n).exists() for n in restored['old_paths_absent'])
checks=[]
for name,folder,manifest_file,scope_file in [('IAM','ani-iam-wr19','source.json','implementation-scope.json'),('Session','ani-session-gateway-wr19','session-source.json','session-implementation-scope.json'),('ANI','ANI-wr19','ani-source.json','ani-implementation-scope.json')]:
 folder=pathlib.Path('/home/chabking/workspace')/folder
 m=json.loads((p/'runs'/run/manifest_file).read_text());scope=json.loads((p/scope_file).read_text())
 assert git(folder,'rev-parse','HEAD','HEAD^{tree}').decode().splitlines()==[m['baseline_head'],m['baseline_tree']]
 assert sha((p/scope_file).read_bytes())==m['scope_sha256']
 assert all(sha((folder/r['path']).read_bytes())==r['sha256'] and oct((folder/r['path']).stat().st_mode&0o777)==r['mode'] for r in m['files'])
 changed=set(filter(None,git(folder,'diff','--name-only','-z','HEAD').decode().split('\0')))
 untracked=set(filter(None,git(folder,'ls-files','--others','--exclude-standard','-z').decode().split('\0')))
 allowed={r['path'] for r in scope['files']}
 known=allowed
 if name=='IAM':known=known|{r['path'] for r in inherited['files']}|set(restored['old_paths_absent'])|{r['path'] for r in restored['document_files_copied']}
 assert changed|untracked<=known, sorted((changed|untracked)-known)
 subprocess.run(['git','-C',str(folder),'diff','--check'],check=True,capture_output=True)
 selected={r['path'] for r in m['files']}
 if name=='IAM':selected|=set(restored['old_paths_absent'])
 diff=git(folder,'diff','--binary','HEAD','--',*sorted(selected))
 for n in sorted(untracked&selected):
  r=subprocess.run(['git','diff','--no-index','--binary','--','/dev/null',n],cwd=folder,capture_output=True)
  assert r.returncode in [0,1]
  diff+=r.stdout
 (p/(name.lower()+'-product.diff')).write_bytes(diff)
 before={r['path']:r for r in json.loads((p/'runs'/previous/manifest_file).read_text())['files']}
 delta=[r['path'] for r in m['files'] if before.get(r['path'],{}).get('sha256')!=r['sha256']]
 assert all(n.endswith('.md') for n in delta),delta
 checks.append({'repository':name,'worktree':str(folder),'source_manifest':'runs/'+run+'/'+manifest_file,'archive_sha256':m['archive_sha256'],'source_files':len(m['files']),'allowed_paths':len(allowed),'changed_paths':sorted((changed|untracked)&selected),'uncreated_planned_paths':[r['path'] for r in scope['files'] if not (folder/r['path']).exists()],'changed_after_real_chain':delta,'all_runtime_source_identical_to_real_chain':True,'diff':name.lower()+'-product.diff','diff_sha256':sha(diff),'diff_check':'pass'})
# A focused review patch against the complete WR18 uncommitted product, not HEAD.
with tarfile.open(wr18/'source.tar.gz') as a:old={r['path']:a.extractfile(r['path']).read() for r in inherited['files']}
folder=pathlib.Path('/home/chabking/workspace/ani-iam-wr19');parts=[]
for row in json.loads((p/'implementation-scope.json').read_text())['files']:
 n=row['path'];before=old.get(n)
 if before is None:
  r=subprocess.run(['git','-C',str(folder),'show',inherited['baseline_head']+':'+n],capture_output=True)
  before=r.stdout if r.returncode==0 else b''
 after=(folder/n).read_bytes() if (folder/n).is_file() else b''
 if before!=after:
  try:parts.append(''.join(difflib.unified_diff(before.decode().splitlines(True),after.decode().splitlines(True),fromfile='a/'+n if before else '/dev/null',tofile='b/'+n if after else '/dev/null')))
  except UnicodeDecodeError:parts.append('diff --git a/'+n+' b/'+n+'\nBinary files a/'+n+' and b/'+n+' differ; full binary patch is in iam-product.diff\n')
focused=''.join(parts).encode();(p/'iam-wr19-only.diff').write_bytes(focused)
originals=[]
for name in ['ANI','ani-session-gateway']:
 row=baseline['repositories'][name];folder=pathlib.Path(row['root'])
 assert git(folder,'rev-parse','HEAD','HEAD^{tree}').decode().splitlines()==row['revision']
 assert not git(folder,'status','--porcelain').strip()
 originals.append({'repository':name,'baseline_unchanged':True,'clean':True})
authority_changes=[n for n,row in baseline['document_inputs'].items() if sha((root/n).read_bytes())!=row['sha256']]
assert set(authority_changes)<={'.scratch/ani-iam-workload-refoundation/issues/19-prove-workload-session-reference.md','.scratch/ani-iam-workload-refoundation/ticket-plan.md'}
report={'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_consistency':'pass','checks':checks,'protected_originals':originals,'WR18_preserved':True,'original_IAM_document_changes':authority_changes,'focused_IAM_diff':'iam-wr19-only.diff','focused_IAM_diff_sha256':sha(focused),'publication':'none; no add/commit/push/tag/PR'}
(p/'final-source-consistency.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'source_consistency':'pass','runtime_sources_match_real_chain':True,'repos':[{k:r[k] for k in ['repository','source_files','allowed_paths']} for r in checks]}))
