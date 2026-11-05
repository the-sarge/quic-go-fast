"""Inert issue #1210 adapter. Constructing/importing does not attach probes."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import subprocess
import time

REMOTE_PROGRAM = r'''
import hashlib,json,os,pathlib,pwd,signal,stat,sys,time
action,root_arg,arm,hashes_arg,pids_arg=sys.argv[1:]
root=pathlib.Path(root_arg);hashes=json.loads(hashes_arg);known=json.loads(pids_arg)
assert root.is_absolute() and root.parent==pathlib.Path('/home/josh') and root.name.startswith('wiremux-1460-diag-q1210-')
assert root.resolve()==root
helper=root/('napi-helper-'+arm+'.py');script=root/('napi-observe-'+arm+'.bt');folder=root/('napi-'+arm)
uid=pwd.getpwnam('josh').pw_uid
if action not in ('absence','finish','abort'):
 st=root.stat();assert stat.S_ISDIR(st.st_mode) and st.st_uid==uid and not st.st_mode&0o022

def process(pid):
 p=pathlib.Path('/proc')/str(pid)
 try:
  fields=(p/'stat').read_text().rsplit(')',1)[1].split()
  argv=[x.decode(errors='replace') for x in (p/'cmdline').read_bytes().split(b'\0') if x]
  return dict(pid=int(pid),start_ticks=fields[19],ppid=int(fields[1]),state=fields[0],uid=p.stat().st_uid,argv=argv)
 except (FileNotFoundError,ProcessLookupError):return None

def helper_match(p):
 return p is not None and p['uid']==0 and len(p['argv'])==4 and pathlib.Path(p['argv'][0]).name=='python3' and p['argv'][1:]==[str(helper),str(folder),str(script)]

def tracer_match(p,parent):
 return p is not None and p['uid']==0 and p['ppid']==parent and p['argv']==['/usr/bin/bpftrace','-B','line','-k','-','0','0','180']

def discover():
 helpers=[];tracers=[]
 for item in pathlib.Path('/proc').iterdir():
  if not item.name.isdigit():continue
  p=process(item.name)
  if helper_match(p):helpers.append(p)
 for item in pathlib.Path('/proc').iterdir():
  if not item.name.isdigit():continue
  p=process(item.name)
  if any(tracer_match(p,h['pid']) for h in helpers):tracers.append(p)
 return helpers+tracers

def snapshot():
 live=[];reused=[]
 for expected in known:
  current=process(expected['pid'])
  if current is None:continue
  if current['start_ticks']==expected['start_ticks']:live.append(current)
  else:reused.append(current)
 for p in discover():
  if all(p['pid']!=x['pid'] for x in live):live.append(p)
 return dict(live_owned=live,reused_pids=reused)

def read_result():
 try:return json.loads((folder/'result.json').read_text())
 except FileNotFoundError:return None

result={}
if action=='prepare':
 assert not helper.exists() and not script.exists() and not folder.exists()
 result={'prepared':True}
elif action=='verify':
 for path,key in [(helper,'helper.py'),(script,'observe.bt')]:
  st=path.lstat();assert stat.S_ISREG(st.st_mode) and st.st_uid==uid
  assert hashlib.sha256(path.read_bytes()).hexdigest()==hashes[key]
  path.chmod(0o700);assert stat.S_IMODE(path.stat().st_mode)==0o700
 result={'verified':hashes}
elif action=='create':
 folder.mkdir(mode=0o700);assert folder.resolve()==folder and folder.stat().st_uid==uid
 result={'created':str(folder)}
elif action=='ready':
 deadline=time.monotonic()+45
 while time.monotonic()<deadline:
  if (folder/'ready.json').is_file():
   ready=json.loads((folder/'ready.json').read_text())
   h=process(ready['helper_pid']);b=process(ready['bpftrace_pid'])
   ok=ready.get('ready') is True and ready.get('script_sha256')==hashes['observe.bt'] and helper_match(h) and tracer_match(b,ready['helper_pid'])
   owned=([h] if helper_match(h) else [])+([b] if helper_match(h) and tracer_match(b,h['pid']) else [])
   result={'ready':ready,'ok':ok,'processes':owned};break
  early=read_result()
  if early is not None:
   result={'ok':False,'reason':'helper-exited-before-readiness','result':early,'processes':discover()};break
  time.sleep(0.1)
 else:result={'ok':False,'reason':'readiness-timeout','processes':discover()}
elif action=='stop':
 if folder.is_dir():
  assert folder.resolve()==folder
  try:fd=os.open(folder/'stop',os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600);os.close(fd)
  except FileExistsError:
   assert stat.S_ISREG((folder/'stop').lstat().st_mode)
 result={'stop_requested':True}
elif action=='abort':
 targets=snapshot()['live_owned']
 for p in targets:
  current=process(p['pid'])
  if current and current['start_ticks']==p['start_ticks']:
   try:os.kill(p['pid'],signal.SIGINT)
   except ProcessLookupError:pass
 deadline=time.monotonic()+6
 while time.monotonic()<deadline and snapshot()['live_owned']:time.sleep(0.1)
 for p in snapshot()['live_owned']:
  current=process(p['pid'])
  if current and current['start_ticks']==p['start_ticks']:
   try:os.kill(p['pid'],signal.SIGKILL)
   except ProcessLookupError:pass
 deadline=time.monotonic()+2
 while time.monotonic()<deadline and snapshot()['live_owned']:time.sleep(0.1)
 result=snapshot();result['result']=read_result()
elif action in ('absence','finish'):
 result=snapshot();result['result']=read_result()
else:raise ValueError(action)
print(json.dumps(result))
'''


class NapiCapture:
    def __init__(self, op, docs, armname):
        if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]*', armname):
            raise ValueError('invalid NAPI arm name')
        root = PurePosixPath(op.REMOTE)
        if root.parent != PurePosixPath('/home/josh') or not root.name.startswith('wiremux-1460-diag-q1210-'):
            raise ValueError('unexpected remote staging root')
        self.op, self.docs, self.arm = op, Path(docs), armname
        self.source = Path(__file__).resolve().parent
        self.inputs = self.docs / ('napi-inputs-' + armname)
        self.folder = str(root / ('napi-' + armname))
        self.helper = str(root / ('napi-helper-' + armname + '.py'))
        self.script = str(root / ('napi-observe-' + armname + '.bt'))
        self.hashes = {name: hashlib.sha256((self.source / name).read_bytes()).hexdigest() for name in ('helper.py', 'observe.bt')}
        self.proc = None
        self.files = []
        self.pids = []
        self.staged = self.started = self.stopped = False
        self.launch_record = None

    def _remote(self, action, timeout=15):
        privileged = action in ('ready', 'abort', 'absence', 'finish')
        command = (['sudo', '-n'] if privileged else []) + ['python3', '-', action, self.op.REMOTE, self.arm, json.dumps(self.hashes), json.dumps(self.pids)]
        result = self.op.run(self.op.SSH + ['minimax', shlex.join(command)], 'napi-' + self.arm + '-' + action, data=REMOTE_PROGRAM.encode(), timeout=timeout)
        return json.loads(result.stdout)

    def stage(self):
        """Call once after the existing operator stage hook has completed."""
        if self.staged:
            raise RuntimeError('NAPI inputs already staged')
        self._remote('prepare')
        self.inputs.mkdir(mode=0o700)
        for name in ('helper.py', 'observe.bt', 'napi_adapter.py'):
            shutil.copyfile(self.source / name, self.inputs / name)
        (self.inputs / 'source-hashes.json').write_text(json.dumps({name: hashlib.sha256((self.inputs / name).read_bytes()).hexdigest() for name in ('helper.py', 'observe.bt', 'napi_adapter.py')}, indent=2) + '\n')
        options = ['scp', '-oBatchMode=yes', '-oStrictHostKeyChecking=yes', '-oConnectTimeout=10']
        for source, destination in [('helper.py', self.helper), ('observe.bt', self.script)]:
            self.op.run(options + [str(self.inputs / source), 'minimax:' + destination], 'napi-' + self.arm + '-stage-' + source, timeout=55)
        self._remote('verify')
        self.staged = True

    def _close_launch(self):
        for stream in self.files:
            stream.close()
        self.files.clear()
        if self.launch_record is not None:
            self.launch_record.update(finished_unix=time.time(), exit=self.proc.returncode if self.proc else None)
            self.op.save('napi-' + self.arm + '-launcher.command.json', self.launch_record)

    def _abort(self):
        try:
            report = self._remote('abort', timeout=15)
        finally:
            if self.proc is not None:
                try:
                    self.proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    self.proc.terminate()
                    try:
                        self.proc.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        self.proc.kill()
                        self.proc.wait(timeout=2)
            self._close_launch()
        if report['live_owned']:
            raise RuntimeError('NAPI owned processes survived abort: ' + json.dumps(report))
        self.stopped = True
        return report

    def start(self):
        if not self.staged or self.started:
            raise RuntimeError('NAPI capture requires fresh staged inputs')
        self._remote('create')
        self.started = True
        argv = self.op.SSH + ['minimax', shlex.join(['sudo', '-n', 'python3', self.helper, self.folder, self.script])]
        self.launch_record = {'argv': argv, 'started_unix': time.time(), 'source_sha256': self.hashes}
        try:
            for suffix in ('stdout', 'stderr'):
                self.files.append((Path(self.op.OPS) / ('napi-' + self.arm + '-launcher.' + suffix)).open('xb'))
            self.proc = subprocess.Popen(argv, stdout=self.files[0], stderr=self.files[1], start_new_session=True)
            self.launch_record['local_ssh_pid'] = self.proc.pid
            self.op.save('napi-' + self.arm + '-launcher.command.json', self.launch_record)
            report = self._remote('ready', timeout=50)
            self.pids = report.get('processes', [])
            self.op.save('napi-' + self.arm + '-processes.json', report)
            if not report.get('ok') or self.proc.poll() is not None:
                raise RuntimeError('NAPI did not become ready: ' + json.dumps(report))
            self.op.event('napi-ready', arm=self.arm, remote=self.folder, processes=self.pids)
            return report
        except BaseException:
            self._abort()
            raise

    def stop(self):
        if not self.started or self.stopped:
            return None
        try:
            self._remote('stop')
            if self.proc is not None:
                self.proc.wait(timeout=15)
        except BaseException:
            self._abort()
            raise
        finally:
            self._close_launch()
        report = self._remote('finish')
        self.op.save('napi-' + self.arm + '-result.json', report)
        if report['live_owned']:
            self._abort()
            raise RuntimeError('NAPI processes remained after helper exit: ' + json.dumps(report))
        result = report.get('result')
        if self.proc is None or self.proc.returncode != 0 or not result or result.get('capture_complete') is not True or result.get('script_sha256') != self.hashes['observe.bt'] or result.get('helper_sha256') != self.hashes['helper.py']:
            raise RuntimeError('NAPI capture incomplete: ' + json.dumps(report))
        self.stopped = True
        self.op.event('napi-stopped', arm=self.arm, remote=self.folder, result=result)
        return report

    def remote_absence(self):
        """Read-only assertion for the existing custody/cleanup hook."""
        report = self._remote('absence')
        if report['live_owned']:
            raise RuntimeError('NAPI owned processes still live: ' + json.dumps(report))
        return report
