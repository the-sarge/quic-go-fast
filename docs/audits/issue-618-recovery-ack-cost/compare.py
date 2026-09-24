"""Frozen one-candidate comparison driver; never a maintained benchmark."""
import datetime
import hashlib
import json
import os
import pathlib
import subprocess
import time

root = pathlib.Path(__file__).resolve().parent
worktree = root.parents[2]
started = time.monotonic()
ledger = []
for implementation in ('baseline', 'candidate'):
    binary = worktree / ('.git-issue618-' + implementation + '.test')
    ledger.append({'binary': str(binary), 'sha256': hashlib.sha256(binary.read_bytes()).hexdigest()})
(root/'comparison-identity.json').write_text(json.dumps(ledger, indent=2)+'\n')
ledger = []

def run(n, ranges, sample, implementation, profile=False):
    remaining = 1200 - (time.monotonic()-started)
    if remaining <= 15:
        raise RuntimeError('20-minute comparison wall budget exhausted')
    env = dict(os.environ, GOMAXPROCS='1', ISSUE618_COMPARE_OUT=str(root), ISSUE618_N=str(n), ISSUE618_RANGES=str(ranges), ISSUE618_SAMPLE=str(sample), ISSUE618_IMPLEMENTATION=implementation, ISSUE618_PROFILE='1' if profile else '0')
    command = [str(worktree/('.git-issue618-'+implementation+'.test')), '-test.run=^TestIssue618Comparison$', '-test.v', '-test.timeout=65s']
    record = {'n':n, 'ranges':ranges, 'sample':sample, 'implementation':implementation, 'profile':profile, 'command':command, 'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    logname = 'candidate-profile.log' if profile else f'pair-{n}-{ranges}-{sample}-{implementation}.log'
    with (root/logname).open('w') as log:
        result = subprocess.run(command, env=env, cwd=worktree, stdout=log, stderr=subprocess.STDOUT, timeout=min(70,remaining))
    record['returncode'] = result.returncode
    ledger.append(record)
    (root/'comparison-execution.json').write_text(json.dumps({'commands':ledger, 'elapsed_seconds':time.monotonic()-started}, indent=2)+'\n')
    if result.returncode:
        raise RuntimeError(f'comparison stopped: {logname}')
    print(logname, flush=True)

for n in (1024,8192,32768):
    for ranges in (1,32):
        for sample in (1,2,3):
            order = ('baseline','candidate') if sample % 2 else ('candidate','baseline')
            for implementation in order:
                run(n,ranges,sample,implementation)
results=[json.loads(p.read_text()) for p in root.glob('pair-*-candidate.json')]
means={(n,r):sum(v['mean_ns'] for v in results if v['n']==n and v['ranges']==r)/3 for n in (1024,8192,32768) for r in (1,32)}
n,ranges=max(means,key=means.get)
run(n,ranges,3,'candidate',profile=True)
