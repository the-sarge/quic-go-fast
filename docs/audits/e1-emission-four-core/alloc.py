import hashlib,json,os,subprocess,sys,time
from pathlib import Path
root=Path('/home/josh/.cache/qgf-e1-four-core');out=Path(sys.argv[1]);out.mkdir(exist_ok=False)
build=json.loads((root/'build-receipt.json').read_text())
env=dict(os.environ,GOMAXPROCS='4',QUIC_GO_DISABLE_GSO='false',EMISSION_EXPERIMENT='1')
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
manifest=dict(go=subprocess.check_output(['go','version'],text=True).strip(),source=build['sources'],build_receipt_sha256=sha(root/'build-receipt.json'),runner_sha256=sha(__file__),launcher_sha256=sha(root/'isolate.sh'),environment={k:env[k] for k in ['GOMAXPROCS','QUIC_GO_DISABLE_GSO','EMISSION_EXPERIMENT']},cpus='8,9,12,13',allocrun_note='testing.AllocsPerRun(1000) temporarily sets GOMAXPROCS=1 internally',invocations=[])
for variant in ['base','candidate']:
 binary=root/(variant+'-alloc')
 cmd=['taskset','-c',manifest['cpus'],str(binary),'-test.run=^TestEmissionFocusedAllocations$','-test.count=1','-test.v']
 invocation=dict(variant=variant,binary_sha256=sha(binary),command=cmd,started_unix=time.time())
 manifest['invocations'].append(invocation)
 (out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
 with (out/(variant+'.log')).open('w') as log:r=subprocess.run(cmd,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=60)
 invocation.update(finished_unix=time.time(),exit_code=r.returncode)
 (out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
 print(variant+' '+(out/(variant+'.log')).read_text(),flush=True);r.check_returncode()
