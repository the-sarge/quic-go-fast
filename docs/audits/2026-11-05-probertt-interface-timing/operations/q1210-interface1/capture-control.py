"""User-owned control of the bounded, privilege-dropping BPF launcher."""
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import time

class Capture:
    def __init__(self, folder, helper):
        self.folder=Path(folder); self.folder.mkdir(mode=0o700)
        self.helper=Path(helper).resolve(); self.proc=None; self.handles=[]
    def start(self):
        command=shlex.join([str(self.helper),str(self.folder.resolve())])
        script='do shell script '+json.dumps(command)+' with administrator privileges'
        self.handles=[(self.folder/n).open('xb') for n in ['launcher.stdout','launcher.stderr']]
        self.proc=subprocess.Popen(['/usr/bin/osascript','-e',script],stdout=self.handles[0],stderr=self.handles[1])
        deadline=time.monotonic()+120
        while time.monotonic()<deadline:
            if self.proc.poll() is not None:
                raise RuntimeError('BPF launcher returned: '+(self.folder/'launcher.stderr').read_text())
            log=self.folder/'tcpdump.log'
            if log.exists() and 'listening on en8' in log.read_text():
                info=json.loads((self.folder/'collector.json').read_text())
                observation=subprocess.check_output(['ps','-p',str(info['pid']),'-o','uid=,comm='],text=True).strip()
                assert observation.split()[0]==str(os.getuid()),observation
                assert observation.endswith(str(self.helper)),observation
                (self.folder/'ready.json').write_text(json.dumps({'unix_ns':time.time_ns(),'process':observation},indent=2)+'\n')
                return
            time.sleep(.1)
        raise TimeoutError('BPF administrator authentication or activation did not finish within 120 seconds')
    def stop(self):
        (self.folder/'stop').touch(exist_ok=True)
        if self.proc is None: return
        try:
            self.proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            # Cancel an unresolved OS prompt; a launched collector also has its own 180s watchdog.
            if not (self.folder/'collector.json').exists():
                self.proc.terminate(); self.proc.wait(timeout=10)
            else:
                raise
        finally:
            for h in self.handles:h.close()
        info_path=self.folder/'collector.json'
        if info_path.exists():
            info=json.loads(info_path.read_text())
            for key in ['pid']:
                result=subprocess.run(['ps','-p',str(info[key]),'-o','pid=,command='],capture_output=True,text=True)
                assert result.returncode==1, result.stdout
            (self.folder/'absence.json').write_text(json.dumps({'checked_pids':[info['pid']],'absent':True})+'\n')
            result=json.loads((self.folder/'collector-result.json').read_text())
            assert result['exit']==0 and result['reason']=='requested',result
        assert self.proc.returncode==0, (self.folder/'launcher.stderr').read_text()

if __name__=='__main__':
    import sys
    c=Capture(sys.argv[1],sys.argv[2])
    try:
        c.start(); print('BPF ready; ending read-only activation witness',flush=True)
    finally:c.stop()
