#!/usr/bin/env python3
"""Generate this record's result tables from its machine-readable files (#808; presentation only).

Reads `summary.json`, `port-context.json`, `attribution.json` and
`datagram-loss.json` (after `stages.py all`, `portcontext.py` and
`dgloss.py`) and prints Markdown tables: the readiness flags beside slice 6's
values, absolute medians, the S5 timeline, the preservation stages and DATAGRAM
loss. Every ratio cell carries the frozen-Reno A/A per-block range. `tables.py --check` confirms that README.md contains every table
verbatim, so each reported figure is checked against the JSON. It sets no flag
and changes no rule. Added after the registration and before any counted
observation, as a reporting aid.

Usage: tables.py [--check]
"""
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROWS = [('loopback', 'stream'), ('loopback', 'datagram'), ('S5', 'stream'), ('S5', 'datagram'), ('S6', 'stream'), ('S6', 'datagram')]
ARMS = [('cand-bbrv3', 'Candidate'), ('cand-reno', 'Reno on candidate')]
MEASURES = ['goodput', 'send_cpu', 'receive_cpu', 'send_rss', 'receive_rss', 'control_p95']
NAMES = {'loopback': 'Loopback', 'S5': 'S5', 'S6': 'S6', 'stream': 'STREAM', 'datagram': 'DATAGRAM'}


def load(name):
    return json.loads((HERE / name).read_text())


def cell(m):
    """Median (blocks crossing) [slice 6's median, bold when raised there]; the A/A per-block range after it."""
    if m is None:
        return '—'
    v = f"{m['median']:.3f}"
    v = f"**{v} ({m['blocks_crossing']})**" if m['raised'] else (f"{v} ({m['blocks_crossing']})" if m['blocks_crossing'] else v)
    p = m.get('prior')
    if p:
        pv = f"{p['median']:.3f}"
        v += f" [{'**' + pv + '**' if p['raised'] else pv}]"
    aa = m.get('aa')
    if aa:
        v += f"; A/A {aa['minimum']:.3f}–{aa['maximum']:.3f}"
    return v


def flags_table(ctx):
    rows = {(r['path'], r['workload'], r['arm']): r for r in ctx['readiness']}
    out = ['| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |',
           '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for path, wl in ROWS:
        for arm, label in ARMS:
            r = rows.get((path, wl, arm))
            if r is None:
                continue
            ms = r['measures']
            if 'useful_benefit' in r:
                ub = r['useful_benefit']
                p = ub.get('prior')
                good = (f"benefit {ub['median']:.2f}× [{ub['minimum']:.2f}–{ub['maximum']:.2f}], {ub['pairs_above_reno']}/{ub['n']}"
                        + (f" [{p['median']:.2f}×, {p['pairs_above_reno']}/{p['n']}]" if p else ''))
            else:
                good = cell(ms.get('goodput'))
            incomplete = '' if r['blocks'] == 5 else f" (incomplete, {r['blocks']} of 5)"
            out.append(f"| {NAMES[path]} {NAMES[wl]}{incomplete} | {label} | {good} | "
                       + ' | '.join(cell(ms.get(k)) for k in MEASURES[1:]) + ' |')
    return '\n'.join(out)


def absolute_table(ctx):
    out = ['| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Receiver heap peak (measured) | Control p95 / p50 | Forward overflow |',
           '| --- | --- | --- | --- | --- | --- | --- | --- |']
    label = {'reno-reno': 'Reno', 'cand-bbrv3': 'Candidate', 'cand-reno': 'Reno on candidate'}
    groups = {(g['path'], g['workload'], g['arm']): g for g in ctx['absolute']}
    f = lambda x, d=2: '—' if x is None else f'{x:.{d}f}'
    for path, wl in ROWS:
        for arm in ['reno-reno', 'cand-bbrv3', 'cand-reno']:
            g = groups.get((path, wl, arm))
            if g is None:
                continue
            m, p = g['medians'], g.get('prior') or {}
            ps, pr = p.get('send') or {}, p.get('receive') or {}
            ov = m.get('overflow_rate')
            out.append(f"| {NAMES[path]} {NAMES[wl]} | {label[arm]} | {f(m['goodput_mbps'], 1)} [{f(p.get('goodput_mbps'), 1)}] | "
                       f"{f(m['send']['cpu_seconds_per_gib'])} / {f(m['receive']['cpu_seconds_per_gib'])} "
                       f"[{f(ps.get('cpu_seconds_per_gib'))} / {f(pr.get('cpu_seconds_per_gib'))}] | "
                       f"{f(m['send']['peak_rss_mib'])} / {f(m['receive']['peak_rss_mib'])} "
                       f"[{f(ps.get('peak_rss_mib'))} / {f(pr.get('peak_rss_mib'))}] | "
                       f"{f(m['receive']['heap_peak_measured_mib'])} [{f(pr.get('heap_peak_measured_mib'))}] | "
                       f"{f(m['control_p95_ms'], 3)} / {f(m['control_p50_ms'], 3)} [{f(p.get('control_p95_ms'), 3)} / {f(p.get('control_p50_ms'), 3)}] | "
                       + ('—' if ov is None else f"{100 * ov:.3f}% [{'—' if p.get('overflow_rate') is None else f'{100 * p['overflow_rate']:.3f}%'}]") + ' |')
    return '\n'.join(out)


def timeline_table(ctx):
    out = ['| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |',
           '| --- | --- | --- | --- | --- | --- | --- |']
    rng = lambda s, d: '—' if not s else (f"{s['minimum']:.{d}f}–{s['maximum']:.{d}f}" if s['minimum'] != s['maximum'] else f"{s['minimum']:.{d}f}")
    for who, label in [('this', ''), ('prior', 'slice 6 ')]:
        t = ctx['s5timeline'].get(who)
        if not t:
            continue
        for wl in ['stream', 'datagram']:
            b = t['blocks'][wl]
            verdicts = {}
            for v in b['verdicts']:
                verdicts[v] = verdicts.get(v, 0) + 1
            vtxt = ', '.join(f'{k} ×{n}' for k, n in verdicts.items()) + f" → **{(t.get('workloads') or {}).get(wl)}**"
            out.append(f"| {label}{NAMES[wl]} | {rng(b['deficit'], 3)} | {rng(b['idle'], 3)} | {rng(b['model'], 3)} | "
                       f"{rng(b['timing'], 4)} | {rng(b['unexplained'], 3)} | {vtxt} |")
    return '\n'.join(out)


def preservation_table(attr):
    out = ['| Stage | Cell | Outcome | Detail |', '| --- | --- | --- | --- |']
    for stage in ['presrss', 'preslat']:
        for key, v in (attr.get(stage) or {}).items():
            d = v.get('detail')
            if isinstance(d, dict):
                keep = {k: (round(x, 3) if isinstance(x, float) else x) for k, x in d.items() if not isinstance(x, (list, dict))}
                d = ', '.join(f'{k} {x}' for k, x in keep.items())
            out.append(f"| `{stage}` | {key} | *{v['outcome']}* | {d} |")
    return '\n'.join(out) if len(out) > 2 else 'No conditional preservation stage ran.'


def loss_table(dg):
    out = ['| Path | Arm | DATAGRAM loss, median (max) | Lost, median | Lost beyond the relay\'s drops, median |', '| --- | --- | --- | --- | --- |']
    for key, v in dg['cells'].items():
        path, arm = key.split('/', 1)
        beyond = v.get('lost_minus_relay_dropped_median')
        out.append(f"| {NAMES.get(path, path)} | {arm} | {100 * v['loss_median']:.4f}% ({100 * v['loss_max']:.4f}%) | {v['lost_median']:g} | "
                   + ('— (no relay)' if beyond is None else f'{beyond:g}') + ' |')
    return '\n'.join(out)


def tables():
    ctx = load('port-context.json')
    out = [flags_table(ctx), absolute_table(ctx), timeline_table(ctx), preservation_table(load('attribution.json'))]
    if (HERE / 'datagram-loss.json').exists():
        out.append(loss_table(load('datagram-loss.json')))
    return out


def main(argv):
    ts = tables()
    if argv[1:] == ['--check']:
        readme = (HERE / 'README.md').read_text()
        missing = [i for i, t in enumerate(ts) if t not in readme]
        print('all tables present' if not missing else f'tables missing from README.md: {missing}')
        return 1 if missing else 0
    print('\n\n'.join(ts))
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
