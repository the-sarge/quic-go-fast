#!/usr/bin/env python3
"""Bounded #715 macOS S5 timeline on the operator's Mac; observations run sequentially.

Uses #711's macOS runner (mac/run.py, byte-identical to dc252f8a) with its
artifact directory redirected to .local/bbr-linux-redemonstration/mac. The only
addition is TIMELINE_OUTPUT, set per observation for the `cand-timeline`
sender (the overlay stays off in the receiver). Registered in README.md
("Stage s5timeline") before any observation ran; at most 10 observations.
It can show only whether the macOS behaviour is the same as Linux's or differs.

Usage: mac_matrix.py smoke | s5timeline
"""
import os
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE / 'mac'))
import run as macrun  # noqa: E402

macrun.ART = HERE.parents[2] / '.local/bbr-linux-redemonstration/mac'
macrun.OBS = macrun.ART / 'observations'


def case(phase, workload, block, seed):
    name = f'{phase}-S5-{workload}-p{block}-cand-timeline-bbrv3'
    os.environ['TIMELINE_OUTPUT'] = str(macrun.OBS / name / 'send.timeline.json')
    try:
        return macrun.run_case(phase, 'cand-timeline', workload, block, 'bbrv3', path='S5', seed=seed)
    finally:
        del os.environ['TIMELINE_OUTPUT']


stage = sys.argv[1]
macrun.ensure_credentials()
if stage == 'smoke':
    rows = [case('smoke', 'stream', 1, 9995)]  # excluded harness and instrument check
elif stage == 's5timeline':
    rows = []
    for b in (1, 2):
        for workload in (['stream', 'datagram'] if b % 2 else ['datagram', 'stream']):
            rows.append(case('s5timeline', workload, b, 9600 + b))
else:
    raise ValueError(stage)
macrun.write(macrun.ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])
