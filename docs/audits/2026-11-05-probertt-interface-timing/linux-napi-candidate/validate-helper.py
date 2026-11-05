"""No-network, no-BPF tests of the inert launcher path and process-control seams."""
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
import time

sys.dont_write_bytecode = True
root = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('napi_helper', root / 'helper.py')
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
passed = []

with tempfile.TemporaryDirectory(prefix='q1210-napi-helper-') as temporary:
    home = Path(temporary).resolve()
    helper.EXPECTED_HOME = home
    stage = home / (helper.STAGE_PREFIX + 'test')
    stage.mkdir(mode=0o700)
    script = stage / 'observe.bt'
    script.write_bytes((root / 'observe.bt').read_bytes())
    script.chmod(0o600)
    output = stage / 'output'
    output.mkdir(mode=0o700)
    _, _, source, fd = helper.validate_paths(str(output), str(script))
    assert source == script.read_bytes()
    os.close(fd)
    passed.append('valid-owned-path-and-source-hash')
    (output / 'ready.json').write_text('{}')
    try:
        helper.validate_paths(str(output), str(script))
    except ValueError:
        passed.append('reject-existing-output')
    else:
        raise AssertionError('accepted existing output')
    (output / 'ready.json').unlink()
    linked = stage / 'linked'
    linked.symlink_to(output, target_is_directory=True)
    try:
        helper.validate_paths(str(linked), str(script))
    except ValueError:
        passed.append('reject-symlink-output')
    else:
        raise AssertionError('accepted symlink output')
    script.write_text('altered source')
    try:
        helper.validate_paths(str(output), str(script))
    except ValueError:
        passed.append('reject-changed-source')
    else:
        raise AssertionError('accepted changed source')
    script.write_bytes((root / 'observe.bt').read_bytes())

    fake = '''import signal,sys,time
signal.signal(signal.SIGINT, lambda s,f: (print("END monotonic_ns=1", flush=True), sys.exit(0)))
print("__BPFTRACE_NOTIFY_PROBES_ATTACHED", flush=True)
time.sleep(30)
'''
    for scenario in ['stop-file', 'parent-signal', 'outer-deadline']:
        case = stage / scenario
        case.mkdir(mode=0o700)
        fd = os.open(case, os.O_RDONLY | os.O_DIRECTORY)
        requested = [None]
        proc = subprocess.Popen([sys.executable, '-c', fake], stdout=subprocess.PIPE)
        def request_stop():
            deadline = time.monotonic() + 3
            while not (case / 'ready.json').exists():
                if time.monotonic() > deadline:
                    raise AssertionError('readiness was not published')
                time.sleep(0.01)
            if scenario == 'stop-file':
                (case / 'stop').touch()
            else:
                requested[0] = 'parent-signal-15'
        worker = None
        if scenario != 'outer-deadline':
            worker = threading.Thread(target=request_stop)
            worker.start()
        try:
            with helper.exclusive_file(fd, 'stdout.log') as log:
                result = helper.supervise(proc, fd, log, requested, time.monotonic(), outer_seconds=0.25 if scenario == 'outer-deadline' else 5)
            assert result['ready_seen'], result
            assert (case / 'ready.json').stat().st_mode & 0o777 == 0o644
            if scenario == 'outer-deadline':
                assert result['sigkill_sent'] and result['returncode'] == -signal.SIGKILL, result
            else:
                assert result['returncode'] == 0 and result['end_seen'], result
                assert result['stop_reason'] == ('stop-file' if scenario == 'stop-file' else 'parent-signal-15'), result
            passed.append('owned-child-' + scenario)
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait()
            if worker:
                worker.join(timeout=4)
                assert not worker.is_alive()
            os.close(fd)

    case = stage / 'early-failure'
    case.mkdir(mode=0o700)
    fd = os.open(case, os.O_RDONLY | os.O_DIRECTORY)
    proc = subprocess.Popen([sys.executable, '-c', 'raise SystemExit(3)'], stdout=subprocess.PIPE)
    with helper.exclusive_file(fd, 'stdout.log') as log:
        result = helper.supervise(proc, fd, log, [None], time.monotonic(), outer_seconds=2)
    assert result['returncode'] == 3 and not result['ready_seen'] and not (case / 'ready.json').exists(), result
    os.close(fd)
    passed.append('no-readiness-on-early-failure')

print(json.dumps({'passed': passed, 'count': len(passed), 'bpf_attached': False, 'traffic_sent': False}, indent=2))
