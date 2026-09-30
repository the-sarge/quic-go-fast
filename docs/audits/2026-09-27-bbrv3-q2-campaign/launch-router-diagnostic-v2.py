#!/usr/bin/env python3
"""Single prepared diagnostic, using the already native-tested detached launcher."""
import importlib.util
import os
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


if __name__ == '__main__':
    os.umask(0o077)
    diagnostic = load('diagnostic', 'run-router-diagnostic-v2.py')
    launcher = load('detached_launcher', 'launch-linux-v5.py')
    attempt = 'q2-linux-20260930-router-diagnostic1'
    diagnostic.preflight(attempt)
    root = diagnostic.cloud.ROOT
    diagnostic.cloud.save(root / 'detached-intent.json', dict(attempt=attempt,
        diagnostic_only=True, automatic_retry=False, separate_session=True))
    process = launcher.detached_process(['/usr/bin/caffeinate', '-i', sys.executable,
        str(HERE / 'run-router-diagnostic-v2.py'), '--attempt', attempt, '--execute'], root)
    diagnostic.cloud.save(root / 'detached-pid.json', dict(pid=process.pid,
        session_id=os.getsid(process.pid), launcher_pid=os.getpid(),
        launcher_session_id=os.getsid(0), diagnostic_only=True, automatic_retry=False))
    print('Started single finite detached diagnostic: ' + str(process.pid))
