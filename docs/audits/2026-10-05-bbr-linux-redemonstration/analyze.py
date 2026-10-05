#!/usr/bin/env python3
"""Validate retained #712 Linux observations and apply the registered readiness rule (unchanged from #711).

Linux changes: platform and launcher checks, the A/A frozen-Reno arm keyed by tag
(reported beside every ratio, never overriding the median rule), and the registered
contamination rule with a per-flag sensitivity check.
"""
import json
import math
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import OBS, summarize  # noqa: E402
from localize import contamination  # noqa: E402

HERE = Path(__file__).resolve().parent
LIMITS = dict(goodput=0.95, cpu=1.10, rss=1.10, control_p95=1.20)
BOTTLENECK_BPS = 100e6
ECN = ['not_ect', 'ect1', 'ect0', 'ce']  # relay index = codepoint & 3


def stats(values):
    values = [v for v in values if v is not None]
    return dict(median=statistics.median(values), minimum=min(values), maximum=max(values), n=len(values)) if values else None


def load(d):
    receipt = json.loads((d / 'receipt.json').read_text())
    assert receipt['exit_codes'] == [0, 0], d
    assert receipt.get('relay_exit') in (None, 0), d
    for role in ['send', 'receive']:
        rec = json.loads((d / f'{role}.json').read_text())
        assert rec['gomaxprocs'] == 4 and rec['managed_direct'], d
        assert rec['go_version'] == 'go1.27.0' and rec['platform'] == 'linux/amd64', d
        assert rec['trace_enabled'] is False, d
    row = summarize(d)  # asserts receiver integrity and relay error-freedom
    cfg = json.loads((d / 'config.json').read_text())
    facts = json.loads((d / 'host-facts.json').read_text())
    row['contamination'] = contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])
    row['udp_errors'] = {k: facts['udp_after'][k] - facts['udp_before'][k] for k in ['RcvbufErrors', 'SndbufErrors', 'InErrors', 'MemErrors']}
    row['arm'] = '-'.join([row['variant'], row['controller']] + ([row['tag']] if row.get('tag') else []))
    lo, hi = cfg['warmup_ms'], cfg['warmup_ms'] + cfg['measure_ms']
    if row['path'] != 'loopback':
        relay = json.loads((d / 'relay.json').read_text())
        fwd = relay['Forward']['Stats']
        measured = [s for s in relay['QueueSamples'] if lo <= s[0] < hi]
        delay = sorted(s[1] * 8 / BOTTLENECK_BPS * 1e3 for s in measured)
        stalls = sum(1 for s in relay.get('ForwardDelivery', []) if s[3] > 5_000_000)
        row['relay'].update(
            forward_queue_delay_ms=dict(p50=delay[len(delay) // 2], p95=delay[math.ceil(len(delay) * .95) - 1],
                                        maximum=delay[-1], mean=statistics.fmean(delay), samples=len(delay)),
            overflow_rate=fwd['Overflow'] / fwd['Received'], random_drop_rate=fwd['RandomDrop'] / fwd['Received'],
            forward_received=fwd['Received'], stall_intervals=stalls,
            ecn={d.lower(): dict(zip(ECN, relay[d]['ECNIn'])) for d in ['Forward', 'Reverse']})
        fd = relay.get('ForwardDelivery') or []
        before = [s[4] for s in fd if s[0] < lo]
        during = [s[4] for s in fd if s[0] < hi]
        if before and during:
            row['relay']['overflow_measured'] = during[-1] - before[-1]
        fwd_ecn = row['relay']['ecn']['forward']
        row['relay']['forward_ect_share'] = (fwd_ecn['ect0'] + fwd_ecn['ect1'] + fwd_ecn['ce']) / max(1, sum(fwd_ecn.values()))
        row['utilization'] = row['goodput_mbps'] * 1e6 / BOTTLENECK_BPS
    measured_start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    for role in ['send', 'receive']:
        samples = json.loads((d / f'{role}.json').read_text())['resource_samples']
        warm = [x['heap_bytes'] for x in samples if x['unix_ns'] < measured_start]
        meas = [x['heap_bytes'] for x in samples if x['unix_ns'] >= measured_start]
        row[role]['heap_peak_warmup_mib'] = max(warm) / 2**20 if warm else None
        row[role]['heap_peak_measured_mib'] = max(meas) / 2**20 if meas else None
    return row


rows = [load(p.parent) for p in sorted(OBS.glob('*/receipt.json'))]
ROLES = ['send', 'receive']


def group(selected):
    g = dict(n=len(selected), goodput_mbps=stats([r['goodput_mbps'] for r in selected]),
             utilization=stats([r.get('utilization') for r in selected]),
             control_p95_ms=stats([r['control_p95_ms'] for r in selected]),
             control_p50_ms=stats([r['control_p50_ms'] for r in selected]))
    for role in ROLES:
        g[role] = {k: stats([r[role][k] for r in selected]) for k in
                   ['cpu_seconds_per_gib', 'peak_rss_mib', 'allocated_gib_per_gib', 'heap_peak_warmup_mib', 'heap_peak_measured_mib']}
    if selected[0].get('relay'):
        rl = [r['relay'] for r in selected]
        g['relay'] = dict(overflow_rate=stats([x['overflow_rate'] for x in rl]), random_drop_rate=stats([x['random_drop_rate'] for x in rl]),
                          overflow=stats([x['forward']['stats']['Overflow'] for x in rl]),
                          overflow_measured=stats([x.get('overflow_measured') for x in rl]),
                          random_drop=stats([x['forward']['stats']['RandomDrop'] for x in rl]),
                          max_queue_bytes=stats([x['forward']['stats']['MaxQueueBytes'] for x in rl]),
                          queue_delay_p50_ms=stats([x['forward_queue_delay_ms']['p50'] for x in rl]),
                          queue_delay_p95_ms=stats([x['forward_queue_delay_ms']['p95'] for x in rl]),
                          max_late_ms=stats([max(x['forward']['max_late_ms'], x['reverse']['max_late_ms']) for x in rl]),
                          late_over_1ms=stats([x['forward']['late_over_1ms'] for x in rl]),
                          stall_intervals=stats([x['stall_intervals'] for x in rl]),
                          forward_ect_share=stats([x['forward_ect_share'] for x in rl]),
                          forward_ce=stats([x['ecn']['forward']['ce'] for x in rl]))
    return g


def ratios(row, ref):
    out = dict(goodput=row['goodput_mbps'] / ref['goodput_mbps'], control_p95=row['control_p95_ms'] / ref['control_p95_ms'])
    for role in ROLES:
        out[role + '_cpu'] = row[role]['cpu_seconds_per_gib'] / ref[role]['cpu_seconds_per_gib']
        out[role + '_rss'] = row[role]['peak_rss_mib'] / ref[role]['peak_rss_mib']
        out[role + '_heap_measured'] = row[role]['heap_peak_measured_mib'] / ref[role]['heap_peak_measured_mib']
        out[role + '_heap_warmup'] = row[role]['heap_peak_warmup_mib'] / ref[role]['heap_peak_warmup_mib']
    return out


FLAGS = [('goodput', 'goodput', lambda v: v < LIMITS['goodput']),
         ('send_cpu', 'cpu', lambda v: v > LIMITS['cpu']), ('receive_cpu', 'cpu', lambda v: v > LIMITS['cpu']),
         ('send_rss', 'rss', lambda v: v > LIMITS['rss']), ('receive_rss', 'rss', lambda v: v > LIMITS['rss']),
         ('control_p95', 'control_p95', lambda v: v > LIMITS['control_p95'])]


def readiness(phase, ref_arm='reno-reno', skip_contaminated=False):
    sel = [r for r in rows if r['phase'] == phase]
    if skip_contaminated:
        bad = {(r['path'], r['workload'], r['pair']) for r in sel if r['contamination']['contaminated']}
        sel = [r for r in sel if (r['path'], r['workload'], r['pair']) not in bad]
    groups, paired, out = [], [], []
    for path in ['loopback', 'S5', 'S6']:
        for workload in ['stream', 'datagram']:
            here = [r for r in sel if (r['path'], r['workload']) == (path, workload)]
            for arm in sorted({r['arm'] for r in here}):
                groups.append(dict(path=path, workload=workload, arm=arm, **group([r for r in here if r['arm'] == arm])))
            for block in sorted({r['pair'] for r in here}):
                b = {r['arm']: r for r in here if r['pair'] == block}
                ref = b.get(ref_arm)
                if ref:
                    paired.append(dict(path=path, workload=workload, block=block, seed=ref['seed'],
                                       vs_reno={a: ratios(r, ref) for a, r in b.items() if a != ref_arm}))
            entries = [p for p in paired if (p['path'], p['workload']) == (path, workload)]
            for arm in sorted({k for p in entries for k in p['vs_reno']}):
                vals = [p['vs_reno'][arm] for p in entries if arm in p['vs_reno']]
                entry = dict(path=path, workload=workload, arm=arm, blocks=len(vals),
                             ratios={k: stats([v[k] for v in vals]) for k in vals[0]}, flags={})
                for key, _, crossed in FLAGS:
                    if key == 'goodput' and path == 'S6':
                        entry['useful_benefit'] = dict(pairs_above_reno=sum(v['goodput'] > 1 for v in vals), **entry['ratios']['goodput'])
                        continue
                    med = entry['ratios'][key]['median']
                    entry['flags'][key] = dict(median=med, raised=crossed(med), blocks_crossing=sum(crossed(v[key]) for v in vals))
                out.append(entry)
    return groups, paired, out


groups, paired, flags = readiness('main')
# A/A beside every ratio: the A/A arm's per-block ratios for the same cell and measure.
aa = {(e['path'], e['workload']): e for e in flags if e['arm'] == 'reno-reno-aa'}
for e in flags:
    ref = aa.get((e['path'], e['workload']))
    for key, f in e['flags'].items():
        f['aa'] = ref['ratios'][key] if ref else None
_, _, clean_flags = readiness('main', skip_contaminated=True)
clean = {(e['path'], e['workload'], e['arm']): e for e in clean_flags}
for e in flags:
    c = clean.get((e['path'], e['workload'], e['arm']))
    e['contamination_sensitive'] = sorted(k for k, f in e['flags'].items() if c and k in c['flags'] and c['flags'][k]['raised'] != f['raised'])
    e['clean_blocks'] = c['blocks'] if c else 0
summary = dict(observations=rows, groups=groups, paired=paired, readiness=flags,
               contaminated=[r['id'] for r in rows if r['contamination']['contaminated']])
(HERE / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')

for g in groups:
    rl = g.get('relay')
    print(f"{g['path']:8} {g['workload']:8} {g['arm']:11} n{g['n']} gp {g['goodput_mbps']['median']:7.1f} "
          f"sCPU {g['send']['cpu_seconds_per_gib']['median']:6.2f} rCPU {g['receive']['cpu_seconds_per_gib']['median']:6.2f} "
          f"RSS {g['send']['peak_rss_mib']['median']:5.2f}/{g['receive']['peak_rss_mib']['median']:5.2f} "
          f"heapM {g['send']['heap_peak_measured_mib']['median']:5.2f}/{g['receive']['heap_peak_measured_mib']['median']:5.2f} "
          f"p95 {g['control_p95_ms']['median']:8.3f}"
          + (f" ovf {rl['overflow_rate']['median']*100:.3f}% rnd {rl['random_drop_rate']['median']*100:.3f}% ect {rl['forward_ect_share']['median']:.3f} ce {rl['forward_ce']['median']}"
             f" qd95 {rl['queue_delay_p95_ms']['median']:.1f} late {rl['max_late_ms']['median']:.1f} stalls {rl['stall_intervals']['maximum']}" if rl else ''))
for e in flags:
    raised = [k for k, f in e['flags'].items() if f['raised']]
    detail = ' '.join(f"{k}={f['median']:.3f}({f['blocks_crossing']})" + (f"[aa {f['aa']['minimum']:.2f}-{f['aa']['maximum']:.2f}]" if f.get('aa') else '')
                      for k, f in e['flags'].items())
    ub = e.get('useful_benefit')
    print(f"{e['path']:8} {e['workload']:8} {e['arm']:11} n{e['blocks']} RAISED={raised} {detail}"
          + (f" benefit={ub['median']:.2f}x [{ub['minimum']:.2f}-{ub['maximum']:.2f}] above={ub['pairs_above_reno']}" if ub else ''))
print('validated observations', len(rows), 'contaminated', summary['contaminated'],
      'sensitive', [(e['path'], e['workload'], e['arm'], e['contamination_sensitive']) for e in flags if e['contamination_sensitive']])
