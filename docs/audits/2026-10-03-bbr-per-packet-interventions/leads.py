#!/usr/bin/env python3
"""Descriptive sizing of the five #714 leads from #712's retained S5 sender call graphs.

Reads the folded stacks in #712's raw.tar.gz (4323dae8) and reports, per workload, the
median over three blocks of inclusive sender cycles per useful GiB for frames matching
each lead's sites, candidate minus Reno. Inclusive sums overlap across patterns, so they
size leads; they do not partition the excess or attribute a cause. Written before any
#714 observation existed.

Usage: leads.py > leads.json
"""
import collections
import gzip
import io
import json
import re
import statistics
import subprocess
import tarfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
RAW = '4323dae8:docs/audits/2026-10-03-bbr-linux-diagnostic/raw.tar.gz'
SITES = {
    '1 synchronization crossings': {
        'connection-loop select': r'^runtime\.selectgo$',
        'timer re-arm (maybeResetTimer)': r'\(\*Conn\)\.maybeResetTimer$',
        'ECN mode credit read (enableBBRECN callback)': r'enableBBRECN\.func1$',
        'pending-bytes credit read (enableBBR callback)': r'enableBBR\.func1$',
    },
    '2 delivery-record map': {
        'captureCongestionSend': r'\(\*sentPacketHandler\)\.captureCongestionSend$',
        'beginCongestionFeedback': r'\(\*sentPacketHandler\)\.beginCongestionFeedback$',
        'map operations under BBR dispatch': r'^runtime\.(mapassign|mapaccess\w*|mapdelete)\w*$|^internal/runtime/maps\.',
    },
    '3 recovery-evidence lookups': {
        'recoveryEvidence methods': r'\(\*recoveryEvidence\)\.',
        'lowerBound': r'\(\*recoveryEvidence\)\.lowerBound',
    },
    '4 capability queries': {
        'capabilities': r'\)\.capabilities$',
    },
    '5 send-credit reservations': {
        'localSendCredit.reserve': r'\(\*localSendCredit\)\.reserve$',
        'sendReservation.resize': r'\(\*sendReservation\)\.resize$',
    },
}
TOTAL = 'total'


def stacks():
    data = subprocess.check_output(['git', 'show', RAW], cwd=HERE)
    out = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as tf:
        for m in tf.getmembers():
            p = Path(m.name)
            if p.parent.name.startswith('callgraphs-S5-') and p.name in ('send.folded.json.gz', 'summary.json'):
                out.setdefault(p.parent.name, {})[p.name] = tf.extractfile(m).read()
    return out


def inclusive(folded, gib, pattern, map_under_bbr=False):
    rx, total = re.compile(pattern), 0.0
    for key, cycles in folded.items():
        frames = key.split(';') if key else []
        if map_under_bbr:
            # Map work only where the delivery records are maintained.
            if frames and rx.search(frames[0]) and any('captureCongestionSend' in f or 'beginCongestionFeedback' in f
                                                       or 'congestionDispatch).retire' in f or 'discardCongestionPacket' in f for f in frames):
                total += cycles
        elif any(rx.search(f) for f in frames):
            total += cycles
    return total / gib


def main():
    obs = stacks()
    result = {}
    for wl in ['stream', 'datagram']:
        rows = collections.defaultdict(list)
        for block in (1, 2, 3):
            vals = {}
            for arm in ['cand-bbrv3', 'reno-reno']:
                o = obs[f'callgraphs-S5-{wl}-p{block}-{arm}']
                gib = json.loads(o['summary.json'])['useful_bytes'] / 2**30
                folded = json.loads(gzip.decompress(o['send.folded.json.gz']))
                v = {TOTAL: sum(folded.values()) / gib}
                for lead, sites in SITES.items():
                    for site, pat in sites.items():
                        v[f'{lead} / {site}'] = inclusive(folded, gib, pat, map_under_bbr=site.startswith('map operations'))
                vals[arm] = v
            for k in vals['cand-bbrv3']:
                rows[k].append(vals['cand-bbrv3'][k] - vals['reno-reno'][k])
        result[wl] = {k: round(statistics.median(v) / 1e9, 3) for k, v in rows.items()}
    print(json.dumps(dict(unit='Gcycles per useful GiB, candidate minus Reno, median of 3 blocks, inclusive', source=RAW, leads=result), indent=2))


if __name__ == '__main__':
    main()
