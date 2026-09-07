import hashlib,json,subprocess,sys,time,threading
from pathlib import Path
root=Path('/home/josh/.cache/qgf-e1-four-core');name,mode=sys.argv[1:];out=root/name
stopped=threading.Event();observations=[]
def monitor():
 while not stopped.is_set():
  start=time.time()
  result=subprocess.run(['mpstat','-P','8,9,12,13,24,25,28,29','-o','JSON','1','1'],capture_output=True,text=True)
  observations.append(dict(started_unix=start,finished_unix=time.time(),exit_code=result.returncode,output=result.stdout,stderr=result.stderr))
  (root/(name+'-cpu.json')).write_text(json.dumps(observations)+'\n')
thread=None
if mode=='bench':
 thread=threading.Thread(target=monitor);thread.start()
with (root/(name+'.log')).open('x') as log:
 proc=subprocess.Popen(['sudo','-n','bash',str(root/'isolate.sh'),str(out),mode],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
 for line in proc.stdout:log.write(line);log.flush();print(line,end='',flush=True)
 rc=proc.wait()
if thread:
 stopped.set();thread.join()
record=dict(observed_unix=time.time(),phase_exit=rc,allowed_cpus={s:subprocess.check_output(['systemctl','show',s,'--property=AllowedCPUs','--value'],text=True).strip() for s in ['machine.slice','system.slice','user.slice']},reservation_marker_exists=Path('/run/qgf-e1-four-core-cpu-reservation').exists(),backup_timer_active=subprocess.run(['systemctl','is-active','--quiet','qgf-e1-four-core-cpu-restore.timer']).returncode==0)
record['restored']=not any(record['allowed_cpus'].values()) and not record['reservation_marker_exists'] and not record['backup_timer_active']
(root/(name+'-restoration.json')).write_text(json.dumps(record,indent=2)+'\n')
if out.exists():(out/'restoration.json').write_text(json.dumps(record,indent=2)+'\n')
print('restoration '+json.dumps(record),flush=True)
if rc or not record['restored']:raise SystemExit(rc or 1)
