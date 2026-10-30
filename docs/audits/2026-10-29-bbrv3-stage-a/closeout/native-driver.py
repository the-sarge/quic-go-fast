import datetime,hashlib,json,os,subprocess,time
from pathlib import Path
root=Path.cwd();started=time.monotonic();spec=json.loads((root/'remaining-correctness.json').read_text());build=json.loads((root/'build-receipt.json').read_text())
def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def output(argv):return subprocess.check_output(argv,text=True,stderr=subprocess.STDOUT,timeout=15).strip()
receipt={'schema':'stage-a-native-correctness-v1','started_at':now(),'source':spec['source'],'host_role':'existing shared host; correctness only, not measurement isolation','environment':dict(spec['env'],GOFLAGS='-p=4'),'commands':[],'host_before':{'uname':output(['uname','-a']),'go':output(['go','version']),'loadavg':Path('/proc/loadavg').read_text().strip()},'no_benchmarks_calibration_or_pilot':True}
if receipt['host_before']['go']!='go version go1.27.3 linux/amd64':raise SystemExit('toolchain mismatch')
for path,h in build['input_sha256'].items():
 if hashlib.sha256((root/path).read_bytes()).hexdigest()!=h:raise SystemExit('overlay mismatch: '+path)
for path,h in spec['deterministic_trace_sources_sha256'].items():
 if hashlib.sha256((root/'build/sources/candidate'/path).read_bytes()).hexdigest()!=h:raise SystemExit('source mismatch: '+path)
receipt['verified_overlay_sha256']=build['overlay_sha256']
for index,cmd in enumerate(spec['commands']):
 cwd=root if index==0 else root/'build/sources/candidate';stamp=now();t=time.monotonic()
 try:
  p=subprocess.run(cmd['argv'],cwd=cwd,env=dict(os.environ,**receipt['environment']),text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=cmd['hard_cap_seconds'])
  result={'exit_code':p.returncode,'output':p.stdout}
 except subprocess.TimeoutExpired as ex:
  out=ex.stdout or b'';result={'exit_code':124,'output':out.decode(errors='replace') if isinstance(out,bytes) else out,'timeout':True}
 row=dict(argv=cmd['argv'],cwd=str(cwd),started_at=stamp,finished_at=now(),elapsed_seconds=time.monotonic()-t,hard_cap_seconds=cmd['hard_cap_seconds'],**result);receipt['commands'].append(row)
 receipt['finished_at']=now();receipt['elapsed_seconds']=time.monotonic()-started
 (root/'native-correctness-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print('command',index,result['exit_code'],flush=True)
 if result['exit_code']:raise SystemExit(result['exit_code'])
receipt['host_after']={'loadavg':Path('/proc/loadavg').read_text().strip(),'uname':output(['uname','-a'])};receipt['finished_at']=now();receipt['elapsed_seconds']=time.monotonic()-started
(root/'native-correctness-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
