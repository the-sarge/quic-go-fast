#!/usr/bin/env python3
"""Validate retained #710 observations and apply the pre-registered D3 rules (README.md)."""
import json
import math
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import OBS, summarize  # noqa: E402

HERE = Path(__file__).resolve().parent
MIB = 2**20
EXCURSION = 512 * 1024      # rule 2 threshold, bytes received but unread
LEAD_MS = 150               # one RTT (100 ms) plus 50 ms
STALL_NS = 5_000_000        # relay wakeup more than 5 ms late
ARMS = ['reno', 'perdatagram', 'quantum']


def stats(values):
    values = [v for v in values if v is not None]
    return dict(median=statistics.median(values), minimum=min(values), maximum=max(values), n=len(values)) if values else None


def sign_test(diffs):
    pos, neg = sum(d > 0 for d in diffs), sum(d < 0 for d in diffs)
    n = pos + neg
    p = min(1.0, 2 * sum(math.comb(n, k) for k in range(min(pos, neg) + 1)) / 2**n) if n else 1.0
    return dict(higher=pos, lower=neg, ties=len(diffs) - n, p_two_sided=p)


def spearman(xs, ys):
    def ranks(v):
        order = sorted(range(len(v)), key=v.__getitem__)
        r = [0.0] * len(v)
        i = 0
        while i < len(order):
            j = i
            while j + 1 < len(order) and v[order[j + 1]] == v[order[i]]:
                j += 1
            for k in range(i, j + 1):
                r[order[k]] = (i + j) / 2
            i = j + 1
        return r
    rx, ry = ranks(xs), ranks(ys)
    mx, my = statistics.fmean(rx), statistics.fmean(ry)
    num = sum((a - mx) * (b - my) for a, b in zip(rx, ry))
    den = math.sqrt(sum((a - mx) ** 2 for a in rx) * sum((b - my) ** 2 for b in ry))
    return num / den if den else None


def series(d, role):
    s = json.loads((d / f'{role}.series.json').read_text())
    col = {c: i for i, c in enumerate(s['columns'])}
    return s['samples'], col


def observe(d):
    receipt = json.loads((d / 'receipt.json').read_text())
    assert receipt['exit_codes'] == [0, 0] and receipt.get('relay_exit') in (None, 0), d
    for role in ['send', 'receive']:
        rec = json.loads((d / f'{role}.json').read_text())
        assert rec['gomaxprocs'] == 4 and rec['managed_direct'], d
        assert rec['go_version'] == 'go1.27.0' and rec['platform'] == 'darwin/arm64', d
        assert rec['trace_enabled'] is False, d
    row = summarize(d)
    cfg = json.loads((d / 'config.json').read_text())
    t0 = cfg['start_unix_ns']
    m0, m1 = t0 + cfg['warmup_ms'] * 1_000_000, t0 + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000
    rx = row['receive']
    one_hz = json.loads((d / 'receive.json').read_text())['resource_samples']
    rx['heap_peak_warmup_mib'] = max([x['heap_bytes'] for x in one_hz if x['unix_ns'] < m0], default=0) / MIB
    rx['heap_peak_measured_mib'] = max([x['heap_bytes'] for x in one_hz if x['unix_ns'] >= m0], default=0) / MIB  # M2, as in the demonstration
    samples, col = series(d, 'receive')
    meas = [r for r in samples if m0 <= r[0] < m1]
    unread = lambda r: r[col['conn_received']] - r[col['conn_read']]  # noqa: E731
    peak = max(meas, key=lambda r: r[col['/memory/classes/heap/objects:bytes']])
    rx['series_heap_peak_mib'] = peak[col['/memory/classes/heap/objects:bytes']] / MIB
    rx['series_heap_peak_s'] = (peak[0] - t0) / 1e9
    rx['live_at_peak_mib'] = peak[col['/gc/heap/live:bytes']] / MIB                 # M3
    rx['goal_at_peak_mib'] = peak[col['/gc/heap/goal:bytes']] / MIB
    rx['unread_at_peak_mib'] = unread(peak) / MIB
    rx['unread_max_mib'] = max(r[col['conn_occ_max']] for r in meas) / MIB           # M4
    rx['unread_integral_mib_s'] = sum(max(0, unread(r)) for r in meas) * 0.01 / MIB
    rx['unread_max_whole_mib'] = max(r[col['conn_occ_max']] for r in samples) / MIB
    rx['rxqueue_max'] = max(r[col['rx_max']] for r in samples)                      # M5
    rx['live_max_measured_mib'] = max(r[col['/gc/heap/live:bytes']] for r in meas) / MIB
    rx['rss_reached_s'] = next(((r[0] - t0) / 1e9 for r in samples if r[col['max_rss']] >= samples[-1][col['max_rss']]), None)
    rx['live_unread_spearman'] = spearman([r[col['/gc/heap/live:bytes']] for r in meas], [unread(r) for r in meas])
    # Post hoc (deviation, see README): /gc/heap/live is the previous mark's
    # value, so pair each newly completed cycle's live heap with the unread
    # bytes in the sample just before that cycle completed.
    gc = col['/gc/cycles/total:gc-cycles']
    marks = [(samples[i][col['/gc/heap/live:bytes']], unread(samples[i - 1])) for i in range(1, len(samples))
             if m0 <= samples[i][0] < m1 and samples[i][gc] > samples[i - 1][gc]]
    rx['gc_marks'] = len(marks)
    rx['gc_live_unread_spearman'] = spearman([m[0] for m in marks], [m[1] for m in marks]) if len(marks) > 2 else None
    rx['gc_marks_unread_over_1mib'] = sum(m[1] > MIB for m in marks)
    rx['gc_live_mib_by_unread'] = dict(
        unread_under_1mib=statistics.median([m[0] / MIB for m in marks if m[1] <= MIB]) if any(m[1] <= MIB for m in marks) else None,
        unread_over_1mib=statistics.median([m[0] / MIB for m in marks if m[1] > MIB]) if any(m[1] > MIB for m in marks) else None)
    final = samples[-1][col['max_rss']]
    rx['rss_99_s'] = next(((r[0] - t0) / 1e9 for r in samples if r[col['max_rss']] >= 0.99 * final), None)
    relay = json.loads((d / 'relay.json').read_text())
    fd = relay['ForwardDelivery']
    hist = relay['ForwardBatchHistogram']
    total = sum(k * v for k, v in enumerate(hist))
    rl = row['relay']
    rl['overflow'] = relay['Forward']['Stats']['Overflow']                               # M6
    rl['overflow_measured'] = (max([x[4] for x in fd if x[0] < cfg['warmup_ms'] + cfg['measure_ms']], default=0)
                               - max([x[4] for x in fd if x[0] < cfg['warmup_ms']], default=0))
    rl['stall_intervals'] = sum(x[3] > STALL_NS for x in fd)
    rl['multi_packet_share'] = sum(k * v for k, v in enumerate(hist) if k >= 2) / total if total else None
    # Rule 2/3 timeline: excursion starts and what preceded them in the relay.
    drops = [(fd[i][0], fd[i][4] - fd[i - 1][4]) for i in range(1, len(fd)) if fd[i][4] > fd[i - 1][4]]
    stalls = [x[0] for x in fd if x[3] > STALL_NS]
    starts, above = [], False
    for r in samples:
        u = unread(r) > EXCURSION
        if u and not above:
            starts.append((r[0] - t0) / 1e6)
        above = u
    led = dict(drop=0, stall_only=0, neither=0)
    lags = []  # descriptive: ms since the most recent earlier overflow drop
    for s in starts:
        drop = any(s - LEAD_MS <= t <= s for t, _ in drops)
        stall = any(s - LEAD_MS <= t <= s for t in stalls)
        led['drop' if drop else 'stall_only' if stall else 'neither'] += 1
        earlier = [t for t, _ in drops if t <= s]
        lags.append(s - earlier[-1] if earlier else None)
    rx['excursions'] = dict(count=len(starts), **led, drop_lag_ms=lags,
                            warmup=sum(s < cfg['warmup_ms'] for s in starts))
    return row


rows = [observe(p.parent) for p in sorted(OBS.glob('*/receipt.json')) if not p.parent.name.startswith('smoke')]
main = [r for r in rows if r['phase'] == 'main']
blocks = {}
for r in main:
    blocks.setdefault(r['pair'], {})[r['variant']] = r
complete = {b: v for b, v in sorted(blocks.items()) if all(a in v for a in ARMS)}

MEASURES = {
    'M1 receiver peak RSS': lambda r: r['receive']['peak_rss_mib'],
    'M2 measured heap peak (1 Hz)': lambda r: r['receive']['heap_peak_measured_mib'],
    'M2s measured heap peak (10 ms)': lambda r: r['receive']['series_heap_peak_mib'],
    'M3 live heap at peak': lambda r: r['receive']['live_at_peak_mib'],
    'M3 live heap max (measured)': lambda r: r['receive']['live_max_measured_mib'],
    'M4 unread max': lambda r: r['receive']['unread_max_mib'],
    'M4 unread integral': lambda r: r['receive']['unread_integral_mib_s'],
    'M5 receive-queue max': lambda r: r['receive']['rxqueue_max'],
    'M6 overflow drops (whole run)': lambda r: r['relay']['overflow'],
    'M6 overflow drops (measured)': lambda r: r['relay']['overflow_measured'],
    'M6 stall intervals': lambda r: r['relay']['stall_intervals'],
    'M6 multi-packet wakeup share': lambda r: r['relay']['multi_packet_share'],
    'receiver warmup heap peak (1 Hz)': lambda r: r['receive']['heap_peak_warmup_mib'],
    'sender peak RSS': lambda r: r['send']['peak_rss_mib'],
    'goodput': lambda r: r['goodput_mbps'],
    'control p95': lambda r: r['control_p95_ms'],
}


def compare(a, b):
    out = {}
    for name, f in MEASURES.items():
        pairs = [(f(v[a]), f(v[b])) for v in complete.values()]
        ratios = [x / y if y else None for x, y in pairs]
        out[name] = dict(ratio=stats(ratios), sign=sign_test([x - y for x, y in pairs]),
                         **{a: stats([x for x, _ in pairs]), b: stats([y for _, y in pairs])})
    return out


paired = {'quantum_vs_perdatagram': compare('quantum', 'perdatagram'),
          'quantum_vs_reno': compare('quantum', 'reno'), 'perdatagram_vs_reno': compare('perdatagram', 'reno')}
q_pd = paired['quantum_vs_perdatagram']['M1 receiver peak RSS']['ratio']['median']
q_reno = paired['quantum_vs_reno']['M1 receiver peak RSS']['ratio']['median']
pd_reno = paired['perdatagram_vs_reno']['M1 receiver peak RSS']['ratio']['median']
flag = dict(quantum_over_perdatagram=q_pd, quantum_over_reno=q_reno, perdatagram_over_reno=pd_reno,
            quantum_keeps_rss_above_flag=q_pd > 1.10 or (q_reno > 1.10 and pd_reno <= 1.10))
across = {a: dict(live_vs_unread_at_peak=spearman([v[a]['receive']['live_at_peak_mib'] for v in complete.values()],
                                                  [v[a]['receive']['unread_at_peak_mib'] for v in complete.values()]))
          for a in ARMS}
excursions = {a: {k: sum(v[a]['receive']['excursions'][k] for v in complete.values()) for k in ['count', 'drop', 'stall_only', 'neither', 'warmup']} for a in ARMS}
gc_marks = {a: dict(within_observation_spearman=stats([v[a]['receive']['gc_live_unread_spearman'] for v in complete.values()]),
                    marks=sum(v[a]['receive']['gc_marks'] for v in complete.values()),
                    marks_unread_over_1mib=sum(v[a]['receive']['gc_marks_unread_over_1mib'] for v in complete.values()),
                    live_unread_under_1mib=stats([v[a]['receive']['gc_live_mib_by_unread']['unread_under_1mib'] for v in complete.values()]),
                    live_unread_over_1mib=stats([v[a]['receive']['gc_live_mib_by_unread']['unread_over_1mib'] for v in complete.values()]))
            for a in ARMS}
out = dict(observations=rows, blocks=sorted(complete), paired=paired, flag=flag, across_observations=across, excursions=excursions,
           gc_marks_post_hoc=gc_marks)
(HERE / 'summary.json').write_text(json.dumps(out, indent=1) + '\n')

print('blocks', sorted(complete), 'heapseries/other observations', len(rows) - len(main))
def fmt(x):
    return 'n/a' if x is None else f'{x:.3f}'


for name in MEASURES:
    q, rn = paired['quantum_vs_perdatagram'][name], paired['perdatagram_vs_reno'][name]
    print(f'{name:34s} Q/PD {q["ratio"]["median"]:.3f} [{q["ratio"]["minimum"]:.3f}-{q["ratio"]["maximum"]:.3f}] '
          f'{q["sign"]["higher"]}/{q["sign"]["lower"]} p={q["sign"]["p_two_sided"]:.4f} | '
          f'medians R {paired["quantum_vs_reno"][name]["reno"]["median"]:.2f} PD {q["perdatagram"]["median"]:.2f} Q {q["quantum"]["median"]:.2f} | PD/R {fmt((rn["ratio"] or {}).get("median"))}')
print('flag', json.dumps(flag))
print('live vs unread at peak (spearman across blocks)', json.dumps(across))
print('excursions', json.dumps(excursions))
print('gc marks (post hoc)', json.dumps(gc_marks))
for b, v in complete.items():
    print(b, ' '.join(f"{a[:2]} rss={v[a]['receive']['peak_rss_mib']:.1f} hm={v[a]['receive']['heap_peak_measured_mib']:.1f} live={v[a]['receive']['live_at_peak_mib']:.1f} "
                      f"unr={v[a]['receive']['unread_max_mib']:.2f} ovf={v[a]['relay']['overflow_measured']} st={v[a]['relay']['stall_intervals']}" for a in ARMS))
