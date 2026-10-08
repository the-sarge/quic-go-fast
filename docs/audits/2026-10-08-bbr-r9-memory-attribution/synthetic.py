#!/usr/bin/env python3
"""#740 Stage 0 synthetic accounting cases on Linux (excluded from every statistic).

Runs bin/synthetic (mem/synthetic/main.go with the unchanged sampler) through the
launcher, on the receiver cores 12-15, three blocks of every case in a rotated
order, and writes each run's result and memory series under
.local/bbr-r9-memory-attribution/synthetic/. memrules_test.py reads the copy
committed under synthetic/ (pack step: `synthetic.py pack`).

Usage (minimax, never while a stage measures): synthetic.py run
Anywhere:                                       synthetic.py pack
"""
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ART = HERE.parents[2] / '.local/bbr-r9-memory-attribution'
OUT = ART / 'synthetic'
CASES = ['none', 'retained', 'spike', 'stacks', 'churn', 'unexplained']


def run():
    OUT.mkdir(parents=True, exist_ok=False)
    for b in (1, 2, 3):
        k = (b - 1) % len(CASES)
        for case in CASES[k:] + CASES[:k]:
            d = OUT / f'b{b}-{case}'
            d.mkdir()
            env = {**os.environ, 'MEM_OUTPUT': str(d / 'mem.jsonl')}
            subprocess.run(['taskset', '-c', '12-15', str(ART / 'bin/launch'), str(d / 'pid'), '--', str(ART / 'bin/synthetic'),
                            '-case', case, '-output', str(d / 'result.json')], env=env, check=True)
            print(b, case, json.loads((d / 'result.json').read_text())['peak_rss_bytes'] / 2**20, flush=True)


def pack():
    dst = HERE / 'synthetic'
    dst.mkdir(exist_ok=False)
    for d in sorted(OUT.iterdir()):
        (dst / d.name).mkdir()
        for f in ['mem.jsonl', 'result.json']:
            shutil.copy(d / f, dst / d.name / f)


if __name__ == '__main__':
    dict(run=run, pack=pack)[sys.argv[1]]()
