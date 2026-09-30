#!/usr/bin/env python3
"""Build two diagnostic binaries, preserving all measured performance binaries."""
import difflib
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
ART = ROOT / '.local/bbr-causal-diagnosis'
FILES = ['connection.go','send_queue.go','internal/ackhandler/congestion_dispatch.go','internal/ackhandler/bbr_recovery.go','internal/congestion/bbr_sender.go','internal/ackhandler/bbr_ecn.go']
BASE = '2383659ce0781b358fe098f32637d41767b7e9d0'
original = {f:subprocess.check_output(['git','show',BASE+':'+f],cwd=ROOT) for f in FILES}
assert all((ROOT/f).read_bytes()==data for f,data in original.items()), 'unexpected source changes'
env = {**os.environ,'GOTOOLCHAIN':'go1.27.0'}
main = ART/'diagnostic-source/fixture/main.go'
subprocess.run(['gofmt','-w',str(main)],check=True)
frozen = (ROOT/'docs/audits/2026-09-25-bbrv3-q1-campaign/fixture/main.go').read_text()
fixture_patch = ''.join(difflib.unified_diff(frozen.splitlines(keepends=True),main.read_text().splitlines(keepends=True),fromfile='a/fixture/main.go',tofile='b/fixture/main.go'))
(HERE/'fixture-heap-diagnostics.patch').write_text(fixture_patch)
for variant in ['baseline','recovery']:
    try:
        if variant=='recovery':
            subprocess.run(['git','apply',str(HERE/'recovery.patch')],cwd=ROOT,check=True)
        subprocess.run(['python3',str(HERE/'instrument.py')],cwd=ROOT,check=True)
        binary=ART/('diag-profile-'+variant)
        cmd=['go','build','-trimpath','-ldflags','-X main.sourceRevision=e4f322cbbfd4225a4b714e08ec19c958cccadcb0+diag-profile-'+variant,'-o',str(binary),'./fixture']
        subprocess.run(cmd,cwd=ART/'diagnostic-source',env=env,check=True)
        patch=(HERE/'instrumentation.patch').read_bytes()
        (HERE/('instrumentation-'+variant+'.patch')).write_bytes(patch)
        receipt=dict(variant='diag-profile-'+variant,binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),instrument_patch_sha256=hashlib.sha256(patch).hexdigest(),fixture_patch_sha256=hashlib.sha256(fixture_patch.encode()).hexdigest(),command=cmd)
        (ART/('diag-profile-'+variant+'-build.json')).write_text(json.dumps(receipt,indent=2)+'\n')
        print(json.dumps(receipt),flush=True)
    finally:
        for f,data in original.items(): (ROOT/f).write_bytes(data)
        for f in ['causal_diagnostics.go','internal/ackhandler/causal_diagnostics.go']:
            (ROOT/f).unlink(missing_ok=True)
