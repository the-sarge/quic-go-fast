#!/usr/bin/env python3
"""Receiver heap sites at the measured-window peak for heap-series observations.

For each heapseries observation, find the 10 ms series peak of heap objects in
the measured window, then report (a) in-use sites from the last per-second
profile at or before the peak (in-use reflects the most recent GC) and (b)
allocation sites in the second leading to the peak (alloc_space delta between
the two profiles bracketing it).
"""
import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, OBS  # noqa: E402

HERE = Path(__file__).resolve().parent
BIN = {v: ART / 'bin' / f'{v}-fixture' for v in ['reno', 'perdatagram', 'quantum']}


def top(binary, profile, index, base=None, n=12):
    cmd = ['go', 'tool', 'pprof', '-top', '-nodecount', str(n), '-sample_index', index]
    if base:
        cmd += ['-base', str(base)]
    out = subprocess.run(cmd + [str(binary), str(profile)], capture_output=True, text=True, check=True).stdout
    lines = out.splitlines()
    head = next(i for i, line in enumerate(lines) if line.strip().startswith('flat'))
    return [line.strip() for line in lines[:head + 1 + n] if line.strip()]


result = {}
for d in sorted(OBS.glob('heapseries-*')):
    meta = json.loads((d / 'meta.json').read_text())
    cfg = json.loads((d / 'config.json').read_text())
    s = json.loads((d / 'receive.series.json').read_text())
    col = {c: i for i, c in enumerate(s['columns'])}
    t0 = cfg['start_unix_ns']
    m0, m1 = t0 + cfg['warmup_ms'] * 1_000_000, t0 + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000
    meas = [r for r in s['samples'] if m0 <= r[0] < m1]
    peak = max(meas, key=lambda r: r[col['/memory/classes/heap/objects:bytes']])
    prof = s['heap_series']
    before = [p for p in prof if p[1] <= peak[0]]
    after = [p for p in prof if p[1] > peak[0]]
    entry = dict(variant=meta['variant'], peak_s=(peak[0] - t0) / 1e9,
                 heap_objects_mib=peak[col['/memory/classes/heap/objects:bytes']] / 2**20,
                 live_mib=peak[col['/gc/heap/live:bytes']] / 2**20,
                 unread_mib=(peak[col['conn_received']] - peak[col['conn_read']]) / 2**20)
    if before:
        p0 = d / 'receive-heap' / f'heap-{before[-1][0]:03d}.pprof'
        entry['inuse_profile_s'] = (before[-1][1] - t0) / 1e9
        entry['inuse_space'] = top(BIN[meta['variant']], p0, 'inuse_space')
        if after:
            p1 = d / 'receive-heap' / f'heap-{after[0][0]:03d}.pprof'
            entry['alloc_window_s'] = [(before[-1][1] - t0) / 1e9, (after[0][1] - t0) / 1e9]
            entry['alloc_space_delta'] = top(BIN[meta['variant']], p1, 'alloc_space', base=p0)
    result[d.name] = entry
    print(d.name, json.dumps({k: v for k, v in entry.items() if not isinstance(v, list)}))
    for k in ['inuse_space', 'alloc_space_delta']:
        for line in entry.get(k, [])[:9]:
            print('   ', k[:5], line)
(HERE / 'heapsites.json').write_text(json.dumps(result, indent=1) + '\n')
