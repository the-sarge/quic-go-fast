#!/usr/bin/env python3
"""Finite #712 order on minimax; observations run sequentially, never concurrently.

Copied from #711's matrix with D3's arm inventory: an A/A frozen-Reno arm joins
every readiness path. Readiness and attribution stages are registered in
README.md before any of their observations ran; a discrimination stage is
appended only after localization, as a post-registration. No run is excluded
after its outcome is seen. Launch under `taskset -c 1-3` (run.HARNESS_CPUS).
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, ensure_credentials, run_case, write  # noqa: E402

# (variant, controller, tag); tag 'aa' is the second frozen-Reno run of a block.
FOUR = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', ''), ('cand', 'reno', '')]
S6_ARMS = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', '')]
DIAG = [('reno-diag', 'reno', ''), ('cand-diag-counted', 'bbrv3', '')]
TWO = [('reno', 'reno', ''), ('cand', 'bbrv3', '')]


def rotated(arms, block):
    k = (block - 1) % len(arms)
    order = arms[k:] + arms[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def blocks(stage, path, arms, n, seed_base=None, **kw):
    rows = []
    for block in range(1, n + 1):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            for variant, controller, tag in rotated(arms, block):
                rows.append(run_case(stage, variant, workload, block, controller, path=path,
                                     seed=seed_base + block if seed_base else None, tag=tag, **kw))
    return rows


stage = sys.argv[1]
# Excluded smoke stages may be repeated under a new phase suffix; earlier attempts are retained.
attempt = sys.argv[2] if len(sys.argv) > 2 else ''
ensure_credentials()
if stage == 'smoke':
    # Harness check only; excluded from every statistic.
    rows = [run_case('smoke' + attempt, 'cand', 'stream', 1, 'bbrv3'),
            run_case('smoke' + attempt, 'cand', 'datagram', 1, 'bbrv3', path='S6', seed=9999)]
elif stage == 'perfsmoke':
    # Instrument smoke check only; excluded from every statistic.
    rows = [run_case('perfsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='stat'),
            run_case('perfsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='record', tag='record')]
elif stage == 'ecnsmoke':
    # Endpoint ECN engagement on loopback and S6 (BBR ECN tracker frames in the sender's samples); excluded.
    rows = [run_case('ecnsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', perf='record'),
            run_case('ecnsmoke' + attempt, 'cand', 'datagram', 1, 'bbrv3', path='S6', seed=9996, perf='record')]
elif stage == 'loopback':
    rows = blocks('main', 'loopback', FOUR, 5)
elif stage == 's5':
    rows = blocks('main', 'S5', FOUR, 5, 9000)
elif stage == 's6':
    rows = blocks('main', 'S6', S6_ARMS, 5, 9100)
# Attribution stages (README.md, "Registration: attribution stages").
elif stage == 'counters':
    rows = blocks('counters', 'S5', FOUR, 6, 9200, perf='stat')
elif stage == 'callgraphs':
    rows = blocks('callgraphs', 'S5', TWO, 3, 9500, perf='record')
elif stage in ('timeline', 'heapsites'):
    rows = []
    for path, base in [('S5', 9300), ('S6', 9400)]:
        rows += blocks(stage, path, DIAG, 2, base + (10 if stage == 'heapsites' else 0), heap_series=stage == 'heapsites')
else:
    raise ValueError(stage)
write(ART / f'{stage}{attempt}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])
