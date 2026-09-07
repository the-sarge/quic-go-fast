import hashlib,json,os,subprocess
from pathlib import Path
root=Path('/home/josh/.cache/qgf-e2')
def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def run(cmd,wd=None): return subprocess.check_output(cmd,cwd=wd,text=True).strip()
manifest=json.loads((root/'bench-02/manifest.json').read_text())
r={'kind':'retrospective source and binary binding; no benchmark execution','go':run(['go','version']),'go_env':json.loads(run(['go','env','-json','GOFLAGS','GOEXPERIMENT','GOOS','GOARCH','GOAMD64','CGO_ENABLED','GOTOOLCHAIN'])),'declared_base':'322c98991993736861f1d02e69d24497bf7376e6','variants':{}}
for variant in ('base','candidate'):
 wd=Path('/Volumes/worktrees/quic-go-fast/e2-linux-'+variant)
 assert not run(['git','status','--porcelain'],wd)
 head=run(['git','rev-parse','HEAD'],wd);assert head==manifest['source'][variant]['head']
 command=['go','test','-tags','queue_lifetime_experiment','-c','-o',str(root/(variant+'-rebuilt')),'.']
 subprocess.run(command,cwd=wd,check=True)
 rebuilt=sha(root/(variant+'-rebuilt'));measured=manifest['source'][variant]['binary_sha256'];assert rebuilt==measured
 changed=run(['git','diff','--name-only',r['declared_base'],head],wd).splitlines()
 if variant=='base': assert changed==['send_queue_lifetime_bench_test.go']
 r['variants'][variant]={'head':head,'tree':run(['git','rev-parse','HEAD^{tree}'],wd),'cwd':str(wd),'changed_from_declared_base':changed,'fixture_sha256':sha(wd/'send_queue_lifetime_bench_test.go'),'original_build_command':['go','test','-tags','queue_lifetime_experiment','-c','-o',str(root/(variant+'-bench')),'.'],'rebuild_command':command,'rebuild_sha256':rebuilt,'capture_sha256':measured,'binary_match':True,'embedded_build_info':run(['go','version','-m',str(root/(variant+'-bench'))])}
run(['git','bundle','create',str(root/'source-binding.bundle'),'codex/e2-linux-base','codex/e2-linux-candidate','^'+r['declared_base']],'/home/josh/.cache/qgf-e1/repo.git')
r['source_bundle_sha256']=sha(root/'source-binding.bundle')
(root/'build-binding.json').write_text(json.dumps(r,indent=2)+'\n')
print('Both rebuilt binaries match the hashes recorded before measurement.')
