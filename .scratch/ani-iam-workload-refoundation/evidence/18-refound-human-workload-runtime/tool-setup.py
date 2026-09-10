import hashlib,json,os,pathlib,shutil,subprocess,urllib.request
base=pathlib.Path('/home/ubuntu/.local/share/ani-iam');bindir=base/'bin';bindir.mkdir(parents=True,exist_ok=True,mode=0o700)
def install(name, source, expected, remote=False):
 target=bindir/name
 if target.exists():
  assert hashlib.sha256(target.read_bytes()).hexdigest()==expected,name
  return
 content=urllib.request.urlopen(source,timeout=45).read() if remote else pathlib.Path(source).read_bytes()
 assert hashlib.sha256(content).hexdigest()==expected,name
 temp=bindir/(name+'.wr18-install');temp.write_bytes(content);temp.chmod(0o755);temp.rename(target)
install('sqlc','/home/ubuntu/.local/share/ani-network-service/bin/sqlc','0496fbc18f603e2fbd10c752942d1389a1fa9bc3d18de7243ec00cccd611575f')
install('buf','https://github.com/bufbuild/buf/releases/download/v1.72.0/buf-Linux-x86_64','8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af',True)
install('atlas','https://atlasbinaries.com/atlas/atlas-community-linux-amd64-v1.3.0','10d7913e3dce43ab99b8d71534a4cbadaf11a16dc293adf3b91d10e83a0ac70b',True)
g=pathlib.Path('/home/ubuntu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64/bin/go');assert hashlib.sha256(g.read_bytes()).hexdigest()=='182d1dc98119d61a6241590e1aaca180d771dbcb1133b9846684aee82d457362'
for name,args in [('buf',['--version']),('atlas',['version']),('sqlc',['version'])]:subprocess.run([str(bindir/name)]+args,check=True)
subprocess.run([str(g),'version'],check=True)
print(json.dumps({'tool_setup':'pass','host':os.uname().nodename,'directory':str(bindir)}))
