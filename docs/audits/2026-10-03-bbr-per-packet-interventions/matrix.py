#!/usr/bin/env python3
"""Finite #714 order on minimax; observations run sequentially, never concurrently.

Adapted from the #712 matrix (4323dae8). run.py is #712's runner, byte-identical;
only its artifact directory is redirected here. Each measurement is registered in
README.md ("Registration: measurement") before any of its observations ran: S5,
both workloads, six blocks, five arms, plain builds, `perf stat` on both endpoints
for the measured window. No run is excluded after its outcome is seen. Launch
under `taskset -c 1-3` (run.HARNESS_CPUS).

Usage: matrix.py smoke [attempt]
       matrix.py m<k> PREV NEW [rerun-block]   e.g. matrix.py m1 r0 r1
"""
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-per-packet-interventions'
run.OBS = run.ART / 'observations'

# Registered seed bases: measurement k uses seeds 9700 + 10*k + block (blocks 1-6).
SEED_BASE = {f'm{k}': 9700 + 10 * k for k in range(1, 7)}
BLOCKS = 6


def arms(prev, new):
    """(variant, controller, tag): frozen Reno, A/A Reno, predecessor, new revision, Reno on the new revision."""
    return [('reno', 'reno', ''), ('reno', 'reno', 'aa'), (prev, 'bbrv3', ''), (new, 'bbrv3', ''), (new, 'reno', '')]


def rotated(a, block):
    # #712's rotation: start position advances per block; even blocks run reversed.
    k = (block - 1) % len(a)
    order = a[k:] + a[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def block_rows(stage, a, block, seed, suffix=''):
    rows = []
    for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
        for variant, controller, tag in rotated(a, block):
            rows.append(run.run_case(stage + suffix, variant, workload, block, controller, path='S5', seed=seed, tag=tag, perf='stat'))
    return rows


def main(argv):
    stage = argv[1]
    run.ensure_credentials()
    if stage == 'smoke':
        # Harness and instrument check only; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run.run_case('smoke' + attempt, 'r0', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='stat'),
                run.run_case('smoke' + attempt, 'reno', 'datagram', 1, 'reno', path='S5', seed=9998, perf='stat')]
    else:
        prev, new = argv[2], argv[3]
        a = arms(prev, new)
        if len(argv) > 4:
            # One registered rerun of a block whose instruments were unusable; the original is retained.
            block = int(argv[4])
            rows = block_rows(stage, a, block, SEED_BASE[stage] + block, suffix='rerun')
        else:
            rows = []
            for block in range(1, BLOCKS + 1):
                rows += block_rows(stage, a, block, SEED_BASE[stage] + block)
    out = run.ART / f'{stage}-summary.json'
    if out.exists():
        out = run.ART / f'{stage}-{len(list(run.ART.glob(stage + "-summary*.json")))}-summary.json'
    run.write(out, [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
