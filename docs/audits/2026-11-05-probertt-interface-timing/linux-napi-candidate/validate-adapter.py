"""Reversible mocked adapter checks. No SSH, SCP, BPF, or traffic is run."""
import importlib.util
import json
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
from types import SimpleNamespace

sys.dont_write_bytecode = True
root = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('napi_adapter', root / 'napi_adapter.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
compile(module.REMOTE_PROGRAM, '<remote-program>', 'exec')

class Process:
    pid = 123
    returncode = None
    def poll(self): return self.returncode
    def wait(self, timeout): self.returncode = 0; return 0
    def terminate(self): self.returncode = -15
    def kill(self): self.returncode = -9

class Op:
    REMOTE = '/home/josh/wiremux-1460-diag-q1210-adapter-test'
    SSH = ['ssh', '-T']
    def __init__(self, folder, ready=True, complete=True, live=False):
        self.OPS = folder / 'ops'; self.OPS.mkdir()
        self.calls = []; self.ready = ready; self.complete = complete; self.live = live
        self.capture = None
    def run(self, argv, label, **kwargs):
        self.calls.append((argv, label, kwargs))
        if argv[0] == 'scp': return SimpleNamespace(stdout=b'', returncode=0)
        command = shlex.split(argv[-1])
        action = command[command.index('-') + 1]
        if action == 'ready':
            value = {'ok': self.ready, 'processes': [{'pid': 10, 'start_ticks': '1'}] if self.ready else []}
        elif action in ('finish', 'absence', 'abort'):
            value = {'live_owned': [{'pid': 10}] if self.live and action != 'abort' else [], 'result': {'capture_complete': self.complete, 'script_sha256': self.capture.hashes['observe.bt'], 'helper_sha256': self.capture.hashes['helper.py']}}
        else: value = {action: True}
        return SimpleNamespace(stdout=json.dumps(value).encode(), returncode=0)
    def save(self, name, value): (self.OPS / name).write_text(json.dumps(value))
    def event(self, *args, **kwargs): pass

passed=[]
original_popen = module.subprocess.Popen
try:
    with tempfile.TemporaryDirectory(prefix='q1210-adapter-') as temporary:
        base = Path(temporary)
        for name, ready, complete, live in [('success',True,True,False),('failed-ready',False,True,False),('incomplete',True,False,False),('live-check',True,True,True)]:
            folder=base/name;folder.mkdir();docs=folder/'docs';docs.mkdir()
            op=Op(folder,ready,complete,live)
            capture=module.NapiCapture(op,docs,'kernel');op.capture=capture
            process=Process()
            module.subprocess.Popen=lambda *args,**kwargs: process
            capture.stage()
            assert len([call for call in op.calls if call[0][0]=='scp'])==2
            assert (docs/'napi-inputs-kernel'/'napi_adapter.py').is_file()
            if not ready:
                try: capture.start()
                except RuntimeError: pass
                else: raise AssertionError('failed readiness admitted')
                assert capture.stopped and process.returncode==0
                assert any(call[1].endswith('-abort') for call in op.calls)
                passed.append('readiness-failure-aborts-owned-child')
                continue
            capture.start()
            assert capture.pids[0]['pid']==10
            assert any(call[2].get('timeout')==50 for call in op.calls)
            if live:
                try: capture.remote_absence()
                except RuntimeError: pass
                else: raise AssertionError('live process accepted for custody')
                op.live=False
                passed.append('live-process-blocks-custody')
            if complete:
                capture.stop();capture.remote_absence();assert process.returncode==0
                if name=='success':passed.append('stage-start-stop-absence-happy-path')
            else:
                try: capture.stop()
                except RuntimeError: pass
                else: raise AssertionError('incomplete result accepted')
                assert any(call[1].endswith('-finish') for call in op.calls)
                passed.append('incomplete-result-rejected-after-absence')
            assert not capture.files
            assert capture.stop() is None if complete else True
        try: module.NapiCapture(op,docs,'../escape')
        except ValueError: passed.append('reject-invalid-arm-before-effects')
        else: raise AssertionError('invalid arm accepted')
finally:
    module.subprocess.Popen=original_popen
print(json.dumps({'passed':passed,'count':len(passed),'ssh_or_scp_run':False,'bpf_attached':False,'traffic_sent':False},indent=2))
