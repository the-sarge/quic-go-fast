import hashlib,json,os,subprocess,time
from pathlib import Path
root=Path('/home/josh/.cache/qgf-e1-four-core')
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
receipt=dict(go=subprocess.check_output(['go','version'],text=True).strip(),go_env=json.loads(subprocess.check_output(['go','env','-json','GOFLAGS','GOEXPERIMENT','GOOS','GOARCH','CGO_ENABLED','GOTOOLCHAIN'])),endpoint_sha256=sha(root/'endpoint.go'),builder_sha256=sha(__file__),steps=[],sources={})
assert receipt['go']=='go version go1.27.1 linux/amd64'
for variant,head in [('base','e742e3ee64c79c61440061a86f56e2e223a776ef'),('candidate','8e61c8e8b9997204316d6009b9065943a5e8b82f')]:
 wd=Path('/Volumes/worktrees/quic-go-fast/e1-four-core-linux-'+variant)
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=wd,text=True).strip()==head
 assert not subprocess.check_output(['git','status','--porcelain'],cwd=wd,text=True).strip()
 receipt['sources'][variant]=dict(head=head,fixture_sha256=sha(wd/'connection_emission_experiment_test.go'))
 commands=[('tests',['go','test','./...']),('race',['go','test','-race','.','-run','^TestEmission|^TestConnection(GSOBatch|SendQueue|ReceivePrioritization)|^TestHandshakeMTU','-count=1']),('vet',['go','vet','./...']),('endpoint',['go','build','-o',str(root/(variant+'-endpoint')),str(root/'endpoint.go')]),('bench',['go','test','-c','-o',str(root/(variant+'-bench')),'./integrationtests/self']),('alloc',['go','test','-tags','emission_experiment','-c','-o',str(root/(variant+'-alloc')),'.'])]
 for label,cmd in commands:
  step=dict(variant=variant,kind=label,cwd=str(wd),command=cmd,started_unix=time.time())
  print('start',variant,label,flush=True)
  with (root/(variant+'-'+label+'-build.log')).open('w') as log:r=subprocess.run(cmd,cwd=wd,stdout=log,stderr=subprocess.STDOUT)
  step.update(exit_code=r.returncode,finished_unix=time.time());receipt['steps'].append(step)
  if label in ['endpoint','bench','alloc'] and not r.returncode:step['binary_sha256']=sha(root/(variant+'-'+label))
  (root/'build-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
  r.check_returncode()
 print('finished',variant,flush=True)
