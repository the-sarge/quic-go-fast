#!/usr/bin/env python3
"""Validate retained demonstration observations and apply the readiness limits."""
import json
import math
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import OBS, summarize  # noqa: E402

HERE = Path(__file__).resolve().parent
LIMITS = dict(goodput=0.95, cpu=1.10, rss=1.10, control_p95=1.20)


def stats(values):
    values = [v for v in values if v is not None]
    return dict(median=statistics.median(values), minimum=min(values), maximum=max(values), n=len(values)) if values else None


rows = []
for receipt_path in sorted(OBS.glob('*/receipt.json')):
    d = receipt_path.parent
    receipt = json.loads(receipt_path.read_text())
    assert receipt['exit_codes'] == [0, 0], d
    assert receipt.get('relay_exit') in (None, 0), d
    for role in ['send', 'receive']:
        rec = json.loads((d / f'{role}.json').read_text())
        assert rec['gomaxprocs'] == 4 and rec['managed_direct'], d
        assert rec['go_version'] == 'go1.27.0' and rec['platform'] == 'darwin/arm64', d
        assert rec['trace_enabled'] is False, d
    row = summarize(d)
    row.setdefault('strip_ecn', False)  # Observations before the ECN stage had no strip option.
    if row['path'] != 'loopback':
        samples = json.loads((d / 'relay.json').read_text()).get('QueueSamples')
        cfg = json.loads((d / 'config.json').read_text())
        if samples:
            measured = [s for s in samples if cfg['warmup_ms'] <= s[0] < cfg['warmup_ms'] + cfg['measure_ms']]
            delay = sorted(s[1] * 8 / 100e6 * 1e3 for s in measured)  # 100 Mbit/s forward bottleneck
            row['relay']['forward_queue_delay_ms'] = dict(
                p50=delay[len(delay) // 2], p95=delay[math.ceil(len(delay) * .95) - 1], maximum=delay[-1],
                mean=statistics.fmean(delay), samples=len(delay))
    cfg = json.loads((d / 'config.json').read_text())
    measured_start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    for role in ['send', 'receive']:
        samples = json.loads((d / f'{role}.json').read_text())['resource_samples']
        warm = [x['heap_bytes'] for x in samples if x['unix_ns'] < measured_start]
        meas = [x['heap_bytes'] for x in samples if x['unix_ns'] >= measured_start]
        row[role]['heap_peak_warmup_mib'] = max(warm) / 2**20 if warm else None
        row[role]['heap_peak_measured_mib'] = max(meas) / 2**20 if meas else None
    rows.append(row)

ROLES = ['send', 'receive']


def group(selected):
    g = dict(goodput_mbps=stats([r['goodput_mbps'] for r in selected]),
             control_p95_ms=stats([r['control_p95_ms'] for r in selected]),
             control_p50_ms=stats([r['control_p50_ms'] for r in selected]),
             combined_cpu_seconds_per_gib=stats([r['combined_cpu_seconds_per_gib'] for r in selected]))
    for role in ROLES:
        g[role] = {k: stats([r[role][k] for r in selected]) for k in ['cpu_seconds_per_gib', 'peak_rss_mib', 'allocated_gib_per_gib', 'total_cpu_seconds']}
    if selected[0].get('relay'):
        g['relay'] = dict(random_drop=stats([r['relay']['forward']['stats']['RandomDrop'] for r in selected]),
                          overflow=stats([r['relay']['forward']['stats']['Overflow'] for r in selected]),
                          max_queue_bytes=stats([r['relay']['forward']['stats']['MaxQueueBytes'] for r in selected]),
                          queue_delay_p50_ms=stats([r['relay'].get('forward_queue_delay_ms', {}).get('p50') for r in selected]),
                          queue_delay_p95_ms=stats([r['relay'].get('forward_queue_delay_ms', {}).get('p95') for r in selected]),
                          max_late_ms=stats([max(r['relay']['forward']['max_late_ms'], r['relay']['reverse']['max_late_ms']) for r in selected]))
    return g


def ratios(row, ref):
    out = dict(goodput=row['goodput_mbps'] / ref['goodput_mbps'],
               control_p95=row['control_p95_ms'] / ref['control_p95_ms'],
               combined_cpu=row['combined_cpu_seconds_per_gib'] / ref['combined_cpu_seconds_per_gib'])
    for role in ROLES:
        out[role + '_cpu'] = row[role]['cpu_seconds_per_gib'] / ref[role]['cpu_seconds_per_gib']
        out[role + '_rss'] = row[role]['peak_rss_mib'] / ref[role]['peak_rss_mib']
    out['passes'] = dict(goodput=out['goodput'] >= LIMITS['goodput'],
                         cpu=max(out['send_cpu'], out['receive_cpu']) <= LIMITS['cpu'],
                         rss=max(out['send_rss'], out['receive_rss']) <= LIMITS['rss'],
                         control_p95=out['control_p95'] <= LIMITS['control_p95'])
    return out


main = [r for r in rows if r['phase'] == 'main']
groups, paired = [], []
for path in ['loopback', 'S6', 'S5']:
    for workload in ['stream', 'datagram']:
        conditions = sorted({(r['variant'], r['controller']) for r in main if r['path'] == path and r['workload'] == workload})
        for variant, controller in conditions:
            selected = [r for r in main if (r['path'], r['workload'], r['variant'], r['controller']) == (path, workload, variant, controller)]
            groups.append(dict(path=path, workload=workload, variant=variant, controller=controller, **group(selected)))
        for pair in sorted({r['pair'] for r in main if r['path'] == path and r['workload'] == workload}):
            block = {(r['variant'], r['controller']): r for r in main if (r['path'], r['workload'], r['pair']) == (path, workload, pair)}
            ref = block.get(('frozen', 'reno'))
            if not ref:
                continue
            entry = dict(path=path, workload=workload, pair=pair, seed=ref['seed'],
                         vs_reno={f'{v}-{c}': ratios(r, ref) for (v, c), r in block.items() if (v, c) != ('frozen', 'reno')})
            paired.append(entry)

readiness = []
for path in ['loopback', 'S6', 'S5']:
    for workload in ['stream', 'datagram']:
        entries = [p for p in paired if p['path'] == path and p['workload'] == workload]
        for cond in sorted({k for p in entries for k in p['vs_reno']}):
            vals = [p['vs_reno'][cond] for p in entries if cond in p['vs_reno']]
            readiness.append(dict(path=path, workload=workload, condition=cond, blocks=len(vals),
                                  **{k: stats([v[k] for v in vals]) for k in ['goodput', 'send_cpu', 'receive_cpu', 'combined_cpu', 'send_rss', 'receive_rss', 'control_p95']},
                                  blocks_passing={k: sum(v['passes'][k] for v in vals) for k in ['goodput', 'cpu', 'rss', 'control_p95']}))

mechanism = []
for r in rows:
    if not r['variant'].endswith('-counted'):
        continue
    entry = dict(id=r['id'], path=r['path'], workload=r['workload'], variant=r['variant'], goodput_mbps=r['goodput_mbps'])
    if r.get('relay'):
        entry['relay'] = dict(random_drop=r['relay']['forward']['stats']['RandomDrop'], overflow=r['relay']['forward']['stats']['Overflow'])
    for role in ROLES:
        w = r[role].get('work')
        if w:
            entry[role] = dict(c4=w['C4'], mechanisms={m: dict(calls=v['Total']['Calls'],
                               inspected_per_call=v['Total']['Inspected'] / v['Total']['Calls'] if v['Total']['Calls'] else 0,
                               max_inspected=v['MaxPerCall']['Inspected'], max_failed=v['MaxPerCall']['Failed'],
                               max_nodes=v['MaxPerCall']['Nodes']) for m, v in w['Mechanisms'].items()})
    mechanism.append(entry)

PHASES = ['startup', 'drain', 'down', 'cruise', 'refill', 'up', 'probertt']
phase_queue = []
for r in rows:
    w = r['send'].get('work') if r['variant'] == 'full-counted' else None
    if not w or not w.get('Phases') or r['path'] == 'loopback':
        continue
    d = OBS / r['id']
    cfg = json.loads((d / 'config.json').read_text())
    samples = json.loads((d / 'relay.json').read_text())['QueueSamples']
    transitions = w['Phases']
    lo, hi = cfg['warmup_ms'], cfg['warmup_ms'] + cfg['measure_ms']
    by_phase = {}
    last_dump_ms = (w['UnixNS'] - cfg['start_unix_ns']) / 1e6
    for t_ms, fwd, _ in samples:
        if not lo <= t_ms < min(hi, last_dump_ms):
            continue
        at = cfg['start_unix_ns'] + t_ms * 1_000_000
        current = None
        for ns, phase in transitions:
            if ns > at:
                break
            current = phase
        if current is None:
            continue
        by_phase.setdefault(PHASES[current], []).append(fwd * 8 / 100e6 * 1e3)
    cycles = [ns for ns, ph in transitions if ph == PHASES.index('up') and lo <= (ns - cfg['start_unix_ns']) / 1e6 < hi]
    phase_queue.append(dict(id=r['id'], up_entries_measured=len(cycles),
                            mean_up_spacing_s=(cycles[-1] - cycles[0]) / 1e9 / (len(cycles) - 1) if len(cycles) > 1 else None,
                            queue_delay_ms_by_phase={k: dict(samples=len(v), share=len(v) / sum(map(len, by_phase.values())),
                                                             mean=statistics.fmean(v), p95=sorted(v)[math.ceil(len(v) * .95) - 1],
                                                             share_over_25ms=sum(x > 25 for x in v) / len(v)) for k, v in by_phase.items()}))

# Diagnostic interventions: each condition against the same block's unstripped frozen Reno.
interventions = []
for phase in ['quantum', 'ecn']:
    sel = [r for r in rows if r['phase'] == phase]
    for path in sorted({r['path'] for r in sel}):
        for workload in ['stream', 'datagram']:
            conds = sorted({(r['variant'], r['controller'], r['strip_ecn']) for r in sel if (r['path'], r['workload']) == (path, workload)})
            for cond in conds:
                vals = []
                for pair in sorted({r['pair'] for r in sel if (r['path'], r['workload']) == (path, workload)}):
                    ref = [r for r in sel if (r['path'], r['workload'], r['pair'], r['variant'], r['controller'], r['strip_ecn']) == (path, workload, pair, 'frozen', 'reno', False)]
                    x = [r for r in sel if (r['path'], r['workload'], r['pair'], r['variant'], r['controller'], r['strip_ecn']) == (path, workload, pair, *cond)]
                    if ref and x and cond != ('frozen', 'reno', False):
                        v = ratios(x[0], ref[0])
                        v['send_allocated_gib_per_gib'] = x[0]['send']['allocated_gib_per_gib']
                        v['receive_heap_peak_warmup_ratio'] = x[0]['receive']['heap_peak_warmup_mib'] / ref[0]['receive']['heap_peak_warmup_mib']
                        v['receive_heap_peak_measured_ratio'] = x[0]['receive']['heap_peak_measured_mib'] / ref[0]['receive']['heap_peak_measured_mib']
                        vals.append(v)
                if vals:
                    interventions.append(dict(phase=phase, path=path, workload=workload, variant=cond[0], controller=cond[1], strip_ecn=cond[2], blocks=len(vals),
                                              **{k: stats([v[k] for v in vals]) for k in ['goodput', 'send_cpu', 'receive_cpu', 'send_rss', 'receive_rss', 'control_p95', 'send_allocated_gib_per_gib', 'receive_heap_peak_warmup_ratio', 'receive_heap_peak_measured_ratio']}))

(HERE / 'summary.json').write_text(json.dumps(dict(observations=rows, groups=groups, paired=paired, readiness=readiness, mechanism=mechanism, phase_queue=phase_queue, interventions=interventions), indent=2) + '\n')
for g in groups:
    print(f"{g['path']:8} {g['workload']:8} {g['variant']:8} {g['controller']:5} goodput {g['goodput_mbps']['median']:8.1f} "
          f"sendCPU {g['send']['cpu_seconds_per_gib']['median']:6.2f} recvCPU {g['receive']['cpu_seconds_per_gib']['median']:6.2f} "
          f"RSS {g['send']['peak_rss_mib']['median']:5.2f}/{g['receive']['peak_rss_mib']['median']:5.2f} p95 {g['control_p95_ms']['median']:8.3f}")
for r in readiness:
    print(r['path'], r['workload'], r['condition'], 'n', r['blocks'], 'pass', r['blocks_passing'],
          'goodput', round(r['goodput']['median'], 3), 'cpu', round(r['send_cpu']['median'], 3), round(r['receive_cpu']['median'], 3),
          'rss', round(r['send_rss']['median'], 3), round(r['receive_rss']['median'], 3), 'p95', round(r['control_p95']['median'], 3))
for i in interventions:
    print(i['phase'], i['path'], i['workload'], i['variant'], i['controller'], 'noecn' if i['strip_ecn'] else '', 'n', i['blocks'],
          'gp', round(i['goodput']['median'], 3), 'sCPU', round(i['send_cpu']['median'], 3), 'rss', round(i['send_rss']['median'], 3), round(i['receive_rss']['median'], 3),
          'p95', round(i['control_p95']['median'], 3), 'alloc', round(i['send_allocated_gib_per_gib']['median'], 2),
          'rx heap warm/meas', round(i['receive_heap_peak_warmup_ratio']['median'], 2), round(i['receive_heap_peak_measured_ratio']['median'], 2))
print('validated observations', len(rows))
