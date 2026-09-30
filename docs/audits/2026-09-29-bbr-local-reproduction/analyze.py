#!/usr/bin/env python3
"""Summarize only the finite observations produced by run.py."""
import hashlib
import json
import pathlib
import statistics

ROOT = pathlib.Path(__file__).resolve().parents[3]
ART = ROOT / '.local/bbr-local-reproduction'
OUT = pathlib.Path(__file__).resolve().parent
phases = ['frozen', 'trace-on', 'trace-off', 'profile', 'contention']
groups = []
comparisons = []
files = []
for phase in phases:
    rows = json.loads((ART / f'{phase}-summary.json').read_text())
    assert len(rows) == (4 if phase in ['trace-on', 'profile'] else 12)
    for row in rows:
        directory = ART / row['id']
        receipt = json.loads((directory / 'receipt.json').read_text())
        assert receipt['exit_codes'] == [0, 0]
        for role in ['send', 'receive']:
            rec = json.loads((directory / f'{role}.json').read_text())
            assert rec['go_version'] == 'go1.27.0'
            assert rec['platform'] == 'darwin/arm64'
            assert rec['gomaxprocs'] == 4 and rec['managed_direct']
            assert rec['result']['run']['warmup_ms'] == 5000
            assert rec['result']['run']['measure_ms'] == 20000
            assert rec.get('trace_enabled', True) == (phase in ['frozen', 'trace-on'])
            assert rec.get('cpu_profile', False) == (phase == 'profile')
            assert rec.get('alloc_profile', False) == (phase == 'profile')
            if phase == 'profile':
                assert (directory / f'{role}.cpu.pprof').stat().st_size > 0
                assert (directory / f'{role}.alloc.pprof').stat().st_size > 0
        for f in sorted(directory.iterdir()):
            if f.is_file():
                files.append(dict(path=str(f.relative_to(ART)), bytes=f.stat().st_size,
                                  sha256=hashlib.sha256(f.read_bytes()).hexdigest()))
    for workload in ['stream', 'datagram']:
        selected = [r for r in rows if r['workload'] == workload]
        for controller in ['reno', 'bbrv3']:
            r = [x for x in selected if x['controller'] == controller]
            def values(metric):
                return [x[metric] for x in r]
            def agg(v):
                return dict(median=statistics.median(v), minimum=min(v), maximum=max(v))
            group = dict(phase=phase, workload=workload, controller=controller, runs=len(r),
                         goodput_mbps=agg(values('goodput_mbps')),
                         control_p95_ms=agg(values('control_p95_ms')),
                         control_reply_count=sum(values('control_replies')),
                         control_skipped=sum(values('control_skipped')),
                         control_unresolved=sum(values('control_unresolved')),
                         control_missed=sum(values('control_missed')))
            for role in ['send', 'receive']:
                group[role] = {m: agg([x[role][m] for x in r]) for m in [
                    'total_cpu_seconds', 'total_cpu_seconds_per_measured_gib',
                    'peak_rss_bytes', 'total_alloc_bytes', 'total_alloc_bytes_per_measured_gib']}
            groups.append(group)
        reno = [r for r in selected if r['controller'] == 'reno']
        bbr = [r for r in selected if r['controller'] == 'bbrv3']
        pair_ratios = []
        for b, r in zip(bbr, reno):
            assert b['id'].rsplit('-', 1)[0] == r['id'].rsplit('-', 1)[0]
            pair_ratios.append(dict(pair=b['id'].rsplit('-', 1)[0],
                 goodput=b['goodput_mbps'] / r['goodput_mbps'],
                 sender_cpu_per_gib=b['send']['total_cpu_seconds_per_measured_gib'] / r['send']['total_cpu_seconds_per_measured_gib'],
                 control_p95=b['control_p95_ms'] / r['control_p95_ms']))
        comparisons.append(dict(phase=phase, workload=workload, pairs=pair_ratios))
assert json.loads((ART / 'contention-cleanup.json').read_text())['all_stopped']
assert all(p['goodput'] < .95 and p['sender_cpu_per_gib'] > 1.10
           for c in comparisons if c['phase'] != 'profile' for p in c['pairs'])
for f in sorted(ART.iterdir()):
    if f.is_file() and (f.name.endswith('-summary.json') or f.name.endswith('-run.jsonl')
                       or f.name in ['contention-competitors.json', 'contention-cleanup.json']):
        files.append(dict(path=f.name, bytes=f.stat().st_size,
                          sha256=hashlib.sha256(f.read_bytes()).hexdigest()))
(OUT / 'summary.json').write_text(json.dumps(dict(regression_reproduced=True, groups=groups, comparisons=comparisons), indent=2) + '\n')
(OUT / 'raw-manifest.json').write_text(json.dumps(files, indent=2) + '\n')
print('Validated', len(files), 'raw files and', sum(g['runs'] for g in groups), 'runs')
