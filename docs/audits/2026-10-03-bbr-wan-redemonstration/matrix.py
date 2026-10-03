#!/usr/bin/env python3
"""Finite #711 order; observations run sequentially, never concurrently.

Readiness stages (loopback, s5, s6) were pre-registered in README.md before
they ran: plain builds, controller order rotated per block, one declared seed
per WAN block shared by every arm. No run is excluded after its outcome is
seen. Attribution stages are appended below the readiness stages only after
the flags they diagnose are known; each is labelled as post-registration.
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, ensure_credentials, run_case, write  # noqa: E402

THREE = [('reno', 'reno'), ('cand', 'bbrv3'), ('cand', 'reno')]
TWO = [('reno', 'reno'), ('cand', 'bbrv3')]


def rotated(arms, block):
    k = (block - 1) % len(arms)
    order = arms[k:] + arms[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def readiness(path, arms, seed_base=None):
    rows = []
    for block in range(1, 6):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            for variant, controller in rotated(arms, block):
                rows.append(run_case('main', variant, workload, block, controller, path=path,
                                     seed=seed_base + block if seed_base else None))
    return rows


stage = sys.argv[1]
ensure_credentials()
if stage == 'smoke':
    # Harness check only; excluded from every statistic.
    rows = [run_case('smoke', 'cand', 'stream', 1, 'bbrv3'),
            run_case('smoke', 'cand', 'datagram', 1, 'bbrv3', path='S6', seed=9999)]
elif stage == 'loopback':
    rows = readiness('loopback', THREE)
elif stage == 's5':
    rows = readiness('S5', THREE, 9000)
elif stage == 's6':
    rows = readiness('S6', TWO, 9100)
# Post-registration attribution stages (README.md, "Post-registration").
elif stage == 'counters':
    arms = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', ''), ('cand', 'reno', '')]
    rows = []
    for block in range(1, 7):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            for variant, controller, tag in rotated(arms, block):
                rows.append(run_case('counters', variant, workload, block, controller, path='S5',
                                     seed=9200 + block, counters=True, tag=tag))
elif stage in ('timeline', 'heapsites'):
    arms = [('reno-diag', 'reno'), ('cand-diag-counted', 'bbrv3')]
    rows = []
    for path, base in [('S5', 9300), ('S6', 9400)]:
        for block in (1, 2):
            for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
                for variant, controller in rotated(arms, block):
                    rows.append(run_case(stage, variant, workload, block, controller, path=path,
                                         seed=base + block + (10 if stage == 'heapsites' else 0),
                                         heap_series=stage == 'heapsites'))
elif stage == 'profiles':
    rows = []
    for block in range(1, 4):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            for variant, controller in rotated(TWO, block):
                rows.append(run_case('profiles', variant, workload, block, controller, path='S5',
                                     seed=9500 + block, profile=True, counters=True))
else:
    raise ValueError(stage)
write(ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])
