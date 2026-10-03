#!/usr/bin/env python3
"""Finite #710 attribution order; observations run sequentially, never concurrently.

Pre-registered in README.md before the main stage ran: S5 STREAM, three arms per
block (matched Reno, per-datagram D1, quantum D1+D2), controller order rotated
per block, one declared seed per block shared by all arms. No run is excluded
after its outcome is seen.
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, ensure_credentials, run_case, write  # noqa: E402

ARMS = [('reno', 'reno'), ('perdatagram', 'bbrv3'), ('quantum', 'bbrv3')]


def rotated(block):
    order = ARMS[(block - 1) % 3:] + ARMS[:(block - 1) % 3]
    return list(reversed(order)) if block % 2 == 0 else order


def blocks(phase, numbers, seed_base, heap_series=False):
    rows = []
    for block in numbers:
        for variant, controller in rotated(block):
            rows.append(run_case(phase, variant, 'stream', block, controller, path='S5',
                                 seed=seed_base + block, heap_series=heap_series))
    return rows


stage = sys.argv[1]
ensure_credentials()
if stage == 'smoke':
    # Instrumentation check only; excluded from every statistic.
    rows = [run_case('smoke', 'quantum', 'stream', 1, 'bbrv3', path='S5', seed=8999, heap_series=True)]
elif stage == 'main':
    rows = blocks('main', range(1, 9), 8000)
elif stage == 'extend':
    # Pre-registered single extension, used only if the main-stage paired
    # comparison of the measured-window heap peak is not separated.
    rows = blocks('main', range(9, 13), 8000)
elif stage == 'heapseries':
    rows = blocks('heapseries', range(1, 4), 8100, heap_series=True)
else:
    raise ValueError(stage)
write(ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])
