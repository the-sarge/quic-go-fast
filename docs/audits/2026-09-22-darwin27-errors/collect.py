#!/usr/bin/env python3
"""Run the committed finite matrix once; retain every exit status and output."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time

root = Path(__file__).resolve().parent
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=False)
records = []

def run(name, args, timeout=15):
    started = time.time()
    try:
        p = subprocess.run(args, text=True, capture_output=True, timeout=timeout)
        record = dict(name=name, args=args, exit=p.returncode, stdout=p.stdout, stderr=p.stderr)
    except subprocess.TimeoutExpired as e:
        record = dict(name=name, args=args, timeout=True,
                      stdout=(e.stdout or b'').decode(), stderr=(e.stderr or b'').decode())
    record.update(started_unix=started, elapsed_seconds=time.time()-started)
    records.append(record)
    (out / 'commands.json').write_text(json.dumps(records, indent=2)+'\n')
    print(name, record.get('exit', 'timeout'), record['stdout'].strip(), flush=True)
    return record

run('identity', ['/bin/sh', '-c', 'uname -srvmp; sw_vers; sysctl kern.uuid; xcrun clang --version; xcrun --show-sdk-version'])
(out / 'source.json').write_text(json.dumps({
    'candidate_commit': '3a94a547',
    'protocol_commit': '22be9e1f2113d716bc428adba44c611197e3271a',
    'probe_sha256': hashlib.sha256((root/'probe.c').read_bytes()).hexdigest(),
}, indent=2)+'\n')
exe = out / 'probe'
build = run('compile', ['xcrun', 'clang', '-std=c11', '-Wall', '-Wextra', '-Werror', '-Wno-deprecated-declarations', '-pthread', str(root/'probe.c'), '-o', str(exe)])
if build.get('exit') != 0:
    sys.exit(2)
for family in ['ipv4', 'ipv6']:
    run(f'{family}-positive', [str(exe), family, 'positive', '0', 'batch'])
    for error in ['EAGAIN', 'EINTR']:
        run(f'{family}-{error}-single', [str(exe), family, error, '0', 'single'])
        for prefix in ['0', '1']:
            run(f'{family}-{error}-batch-{prefix}', [str(exe), family, error, prefix, 'batch'])
run('unix-ENOBUFS-single', [str(exe), 'unix', 'ENOBUFS', '0', 'single'])
for prefix in ['0', '1']:
    run(f'unix-ENOBUFS-batch-{prefix}', [str(exe), 'unix', 'ENOBUFS', prefix, 'batch'])
exe.unlink()
