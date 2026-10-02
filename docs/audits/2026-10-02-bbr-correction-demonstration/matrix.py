#!/usr/bin/env python3
"""Finite demonstration order; observations run sequentially, never concurrently."""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, ensure_credentials, run_case, write  # noqa: E402

LOOPBACK = [('frozen', 'reno'), ('frozen', 'bbrv3'), ('service', 'bbrv3'), ('full', 'bbrv3'), ('full', 'reno')]
IMPAIRED = [('frozen', 'reno'), ('frozen', 'bbrv3'), ('service', 'bbrv3'), ('full', 'bbrv3')]
COUNTED = ['diagnostic-counted', 'service-counted', 'full-counted']


def rotated(conditions, block):
    order = conditions[block - 1:] + conditions[:block - 1]
    return list(reversed(order)) if block % 2 == 0 else order


stage = sys.argv[1]
ensure_credentials()
rows = []
if stage == 'smoke':
    rows.append(run_case('smoke', 'full', 'stream', 1, 'bbrv3'))
    rows.append(run_case('smoke', 'full', 'stream', 1, 'bbrv3', path='S6', seed=101))
    rows.append(run_case('smoke', 'full-counted', 'datagram', 1, 'bbrv3', path='S6', seed=101))
elif stage == 'loopback':
    for block in range(1, 6):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            for variant, controller in rotated(LOOPBACK, block):
                rows.append(run_case('main', variant, workload, block, controller))
elif stage == 'impaired':
    for pair in range(1, 6):
        for workload in (['stream', 'datagram'] if pair % 2 else ['datagram', 'stream']):
            for variant, controller in rotated(IMPAIRED, pair):
                rows.append(run_case('main', variant, workload, pair, controller, path='S6', seed=1000 + pair))
elif stage == 'loadmatched':
    # Lossless S5 lets Reno fill the same 100 Mbit/s, 100 ms pipe and 1-BDP queue.
    for pair in range(1, 4):
        for workload in (['stream', 'datagram'] if pair % 2 else ['datagram', 'stream']):
            for variant, controller in rotated([('frozen', 'reno'), ('full', 'bbrv3')], pair):
                rows.append(run_case('main', variant, workload, pair, controller, path='S5', seed=3000 + pair))
elif stage == 'quantum':
    # Discriminating pacing-wakeup intervention; experimental, not a selected correction.
    conditions = [('frozen', 'reno'), ('full', 'bbrv3'), ('full-quantum', 'bbrv3')]
    for path, blocks in [('S5', range(4, 7)), ('loopback', range(6, 9))]:
        for block in blocks:
            for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
                for variant, controller in rotated(conditions, block - (3 if path == 'S5' else 5)):
                    rows.append(run_case('quantum', variant, workload, block, controller, path=path,
                                         seed=5000 + block if path == 'S5' else None))
    for variant, controller in [('frozen', 'reno'), ('full', 'bbrv3'), ('full-quantum', 'bbrv3')]:
        rows.append(run_case('profile', variant, 'stream', 1, controller, path='S5', seed=6001, profile=True))
    for workload in ['stream', 'datagram']:
        rows.append(run_case('heap', 'full', workload, 1, 'bbrv3', path='S5', seed=4001, heap=True))
elif stage == 'ecn':
    # Discriminator for ECN-tracker cost: the relay delivers Not-ECT, so QUIC ECN
    # validation fails for both controllers. Unstripped references pair each block.
    conditions = [('frozen', 'reno', False), ('frozen', 'reno', True), ('full', 'bbrv3', False),
                  ('full', 'bbrv3', True), ('full-quantum', 'bbrv3', True)]
    for block in range(7, 10):
        for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
            order = conditions[block - 7:] + conditions[:block - 7]
            for variant, controller, strip in (reversed(order) if block == 8 else order):
                rows.append(run_case('ecn', variant, workload, block, controller, path='S5', seed=7000 + block, strip_ecn=strip))
    rows.append(run_case('profile', 'full', 'stream', 2, 'bbrv3', path='S5', seed=7101, profile=True, strip_ecn=True))
elif stage == 'mechanism':
    for path, seed in [('S6', 2001), ('loopback', None)]:
        for workload in ['stream', 'datagram']:
            for variant in COUNTED:
                rows.append(run_case('mechanism', variant, workload, 1, 'bbrv3', path=path, seed=seed))
elif stage == 'profiles':
    for workload in ['stream', 'datagram']:
        for variant, controller in [('frozen', 'reno'), ('frozen', 'bbrv3'), ('full', 'bbrv3')]:
            rows.append(run_case('profile', variant, workload, 1, controller, profile=True))
        for variant, controller in [('frozen', 'reno'), ('full', 'bbrv3')]:
            rows.append(run_case('heap', variant, workload, 1, controller, heap=True))
        rows.append(run_case('heap', 'full', workload, 1, 'bbrv3', path='S6', seed=4001, heap=True))
        rows.append(run_case('heap', 'frozen', workload, 1, 'reno', path='S5', seed=4001, heap=True))
else:
    raise ValueError(stage)
write(ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])
