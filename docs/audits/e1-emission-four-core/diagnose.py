import hashlib,json,os,subprocess,sys,time
from pathlib import Path
out=Path(sys.argv[1]);out.mkdir(exist_ok=False)
profile='--profile' in sys.argv
root=Path('/home/josh/.cache/qgf-e1')
env=dict(os.environ,GOMAXPROCS='4',QUIC_GO_DISABLE_GSO='false')
manifest=dict(kind='profile' if profile else 'diagnostic',pairs=1 if profile else 4,benchtime='10s' if profile else '1s',cpus='8,9,12,13',gomaxprocs=4,go=subprocess.check_output(['go','version'],text=True).strip(),base='e90617366674535bcefa8f90a2e92b153be1342b',candidate='edea78eabf72a0ac2dacd0d55dd68a8f2cabdc3a',started_unix=time.time(),runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),launcher_sha256=hashlib.sha256(Path('/home/josh/.cache/qgf-e1-four-core/isolate.sh').read_bytes()).hexdigest(),binary_sha256={v:hashlib.sha256((root/(v+'-bench')).read_bytes()).hexdigest() for v in ['base','candidate']})
(out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
with (out/'cpu.json').open('w') as activity:
 monitor=subprocess.Popen(['mpstat','-P','8,9,12,13,24,25,28,29','-o','JSON','1','60'],stdout=activity)
 try:
  for pair in range(manifest['pairs']):
   for variant in (['base','candidate'] if pair%2==0 else ['candidate','base']):
    label=f'{pair:02d}-{variant}'
    cmd=['taskset','-c',manifest['cpus'],str(root/(variant+'-bench')),'-test.run=^$','-test.bench=^BenchmarkStreamChurn$','-test.benchtime='+manifest['benchtime'],'-test.benchmem']
    if profile:cmd+=['-test.cpuprofile='+str(out/(label+'.pprof'))]
    invocation=dict(command=cmd,environment={k:env[k] for k in ['GOMAXPROCS','QUIC_GO_DISABLE_GSO']},started_unix=time.time())
    with (out/(label+'.log')).open('w') as stream:result=subprocess.run(cmd,stdout=stream,stderr=subprocess.STDOUT,env=env,timeout=40)
    invocation.update(finished_unix=time.time(),exit_code=result.returncode)
    (out/(label+'.json')).write_text(json.dumps(invocation,indent=2)+'\n')
    print(label+' '+(out/(label+'.log')).read_text(),flush=True)
    result.check_returncode()
 finally:monitor.wait(timeout=70)
(out/'completion.json').write_text(json.dumps(dict(finished_unix=time.time()))+'\n')
