#!/usr/bin/env python3
"""Run the existing shared-process benchmarks in ten adjacent alternating pairs."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

root=Path('/home/josh/.cache/qgf-e1')
out=Path(sys.argv[1])
out.mkdir(parents=True,exist_ok=False)
env=dict(os.environ,GOMAXPROCS='4',QUIC_GO_DISABLE_GSO='false')
manifest=dict(pairs=10,benchtime='1s',benchmem=True,cpus='8,9,12,13',gomaxprocs=4,
              shared_process=True,gso_disabled=False,
              benchmarks='BenchmarkHandshake|BenchmarkStreamChurn|BenchmarkTransfer',
              base='e90617366674535bcefa8f90a2e92b153be1342b',
              candidate='edea78eabf72a0ac2dacd0d55dd68a8f2cabdc3a',
              go=subprocess.check_output(['go','version'],text=True).strip(),
              runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              binary_sha256={v:hashlib.sha256((root/(v+'-bench')).read_bytes()).hexdigest()
                             for v in ('base','candidate')})
(out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
for pair in range(10):
    for variant in (['base','candidate'] if pair%2==0 else ['candidate','base']):
        label=f'{pair:02d}-{variant}'
        cmd=['taskset','-c',manifest['cpus'],str(root/(variant+'-bench')),
             '-test.run=^$','-test.bench=^('+manifest['benchmarks']+')$',
             '-test.benchtime=1s','-test.benchmem']
        print('start '+label,flush=True)
        with (out/(label+'.log')).open('w') as stream:
            subprocess.run(cmd,stdout=stream,stderr=subprocess.STDOUT,env=env,check=True,timeout=60)
        print('finished '+label,flush=True)
