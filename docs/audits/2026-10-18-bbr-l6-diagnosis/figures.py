#!/usr/bin/env python3
"""Generate the #792 record's result tables from summary.json (verification aid).

`figures.py` prints every table; `figures.py --check` confirms that README.md
contains each one verbatim, so the record's figures are the summary's.
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
CLASSIFIED = ['equal', 'short', 'resumed']
WORKLOADS = ['stream', 'datagram']


def med(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def f(x, n=1):
    return '—' if x is None else f'{x:.{n}f}'


def pct(x):
    return '—' if x is None else f'{100 * x:.0f}%'


def traces(s, wl):
    return [(k, v) for k, v in sorted(s['pairs'].items()) if k.startswith(wl + '/') and v.get('selected')]


def preconditions(s):
    p = s['preconditions']
    rows = ['| Precondition | Result |', '| --- | --- |',
            f"| Selected `trace` observations | {p['observations']} |",
            f"| Every one representative (D3) | {'yes' if p['representative'] else 'no'} |",
            f"| Traces complete | {'yes' if p['traces_complete'] else 'no'} |",
            f"| Checker mismatches in the measured window | {p['mismatches_in_measured_window']} |",
            f"| Input check within tolerance in every trace | {'yes' if p['reconcile_ok'] else 'no'} |"]
    return '\n'.join(rows)


def delivery(s):
    rows = ['| Workload | Period | Competitor next to `trace` | Next to `cand` | #777: next to BBRv3 | #777: next to Reno | Focal `trace` | Focal `cand` |',
            '| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |']
    for wl in WORKLOADS:
        ts = traces(s, wl)
        for per in ['equal', 'short', 'back', 'absent', 'resumed']:
            ref = s['reference_777'][f'{wl}/{per}']
            ct = med(v['trace']['flows']['competitor'][per] for _, v in ts)
            cc = med(v['cand']['flows']['competitor'][per] for _, v in ts)
            ft = med(v['trace']['flows']['focal'][per] for _, v in ts)
            fc = med(v['cand']['flows']['focal'][per] for _, v in ts)
            rows.append(f"| {wl.upper()} | {per} | {f(ct)} | {f(cc)} | {f(ref['competitor_next_to_cand'])} | {f(ref['competitor_next_to_reno'])} | {f(ft)} | {f(fc)} |")
    return '\n'.join(rows)


def mechanism(s):
    rows = ['| Workload | Period | `inflight_longterm` / path | Window / path | Ceiling set by headroom | Competitor drops in BBRv3 Up | Probe interval (s) | Up exits on loss | Two-percent checks on limited packets | Crossings (limited) |',
            '| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |']
    for wl in WORKLOADS:
        ts = traces(s, wl)
        for per in CLASSIFIED:
            ps = [v['trace']['mechanism']['periods'][per] for _, v in ts]
            hi = med(p['inflight_longterm_over_path'] for p in ps)
            win = med(p['window_over_path'] for p in ps)
            head = med(p['window_ceiling_set_by'].get('headroom', 0) for p in ps)
            up = med((p['drops_by_bbr_phase'].get('Up') or {}).get('competitor_share') for p in ps)
            gap = med(p['probe_interval_s_median'] for p in ps)
            exits = sum(p['up_exits'].get('loss', 0) for p in ps)
            total = sum(sum(p['up_exits'].values()) for p in ps)
            lim = med(p['two_percent_checks']['limited_share'] for p in ps)
            cross = sum(p['two_percent_checks']['crossings'] for p in ps)
            climit = sum(p['two_percent_checks']['crossings_limited'] for p in ps)
            rows.append(f"| {wl.upper()} | {per} | {f(hi, 2)} | {f(win, 2)} | {pct(head)} | {pct(up)} | {f(gap)} | {exits} of {total} | {pct(lim)} | {cross} ({climit}) |")
    return '\n'.join(rows)


def checker(s):
    rules = {}
    for k, v in sorted(s['pairs'].items()):
        if not v.get('selected'):
            continue
        for rule, x in v['trace']['check']['by_rule'].items():
            r = rules.setdefault(rule, [0, 0])
            r[0] += x['checked']
            r[1] += x['mismatches']
    rows = ['| Rule | Checks | Mismatches |', '| --- | ---: | ---: |']
    rows += [f'| {rule} | {c} | {m} |' for rule, (c, m) in sorted(rules.items())]
    return '\n'.join(rows)


def inputs(s):
    rows = ['| Workload | Period | Declared losses / relay drops | Delivered ratio | `max_bw` / capacity (max) | `min_rtt` (ms) |',
            '| --- | --- | ---: | ---: | ---: | ---: |']
    for wl in WORKLOADS:
        ts = traces(s, wl)
        for per in CLASSIFIED:
            ps = [v['trace']['reconcile']['periods'][per] for _, v in ts]
            dec = sum(p['r1']['declared'] for p in ps)
            dro = sum(p['r1'].get('dropped') or 0 for p in ps)
            ratios = [p['r2']['ratio'] for p in ps if p['r2'].get('ratio') is not None]
            caps = [p['r3']['max_over_capacity'] for p in ps if p['r3'].get('max_over_capacity') is not None]
            rtt = [p['r4']['min_ms'] for p in ps if p['r4'].get('min_ms') is not None]
            rows.append(f"| {wl.upper()} | {per} | {dec} / {dro} | {f(min(ratios), 4) if ratios else '—'}–{f(max(ratios), 4) if ratios else '—'} | {f(max(caps), 3) if caps else '—'} | {f(min(rtt), 1) if rtt else '—'} |")
    return '\n'.join(rows)


TABLES = [preconditions, delivery, mechanism, checker, inputs]


def main():
    s = json.loads((HERE / 'summary.json').read_text())
    if s.get('status') != 'complete':
        raise SystemExit(f"summary.json is {s.get('status')}: {s.get('reasons')}")
    out = [t(s) for t in TABLES]
    if sys.argv[1:] == ['--check']:
        readme = (HERE / 'README.md').read_text()
        missing = [t.__name__ for t, text in zip(TABLES, out) if text not in readme]
        if missing:
            raise SystemExit(f'README.md lacks the current {missing} table(s)')
        print('every table is in README.md')
        return
    print('\n\n'.join(out))


if __name__ == '__main__':
    main()
