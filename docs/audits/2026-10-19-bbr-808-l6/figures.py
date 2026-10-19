#!/usr/bin/env python3
"""Result tables for #808's L6 record, generated from summary.json alone.

Copied from #777's figures.py (71e88789): its tables are unchanged (they read
the classified triples), `health` counts pair-workloads, and five tables are
added for #808's readings: the #800 displacement and marking comparisons, the
#803 ECN reading, the trace checks and the focal DATAGRAM loss. Presentation
only; written before any counted observation.

Usage: figures.py           print every table
       figures.py --check   exit non-zero unless README.md contains every table verbatim
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
S = json.loads((HERE / 'summary.json').read_text())
A = S['analysis']
SEL = S['selected_observations']
WORKLOADS = ['stream', 'datagram']
PERIODS = ['equal', 'short', 'back', 'absent', 'resumed']
CLASSIFIED = ['equal', 'short', 'resumed']
NAMES = {'stream': 'STREAM', 'datagram': 'DATAGRAM'}


def med(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def f(x, d=3):
    return '–' if x is None else f'{x:.{d}f}'


def obs(w, arm):
    return [v for k, v in sorted(SEL.items()) if k.startswith(w + '/') and k.endswith('/' + arm)]


def classification():
    rows = ['| Workload | Period | Outcome | Pair ratios (pairs 1–5) | A/A ratios |', '| --- | --- | --- | --- | --- |']
    for w in WORKLOADS:
        for p in CLASSIFIED:
            r = A['periods'][f'{w}/{p}']
            rows.append(f"| {NAMES[w]} | {p} | **{A['cells'][f'{w}/{p}']}** | {' '.join(f(x['ratio']) for x in r)} | "
                        f"{' '.join(f(x['aa_ratio']) for x in r)} |")
    return '\n'.join(rows)


def delivery():
    rows = ['| Workload | Period | Competitor next to Reno | Competitor next to BBRv3 | Competitor, A/A | Focal Reno | Focal BBRv3 | Combined, BBRv3 arm |',
            '| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |']
    for w in WORKLOADS:
        for p in PERIODS:
            r = A['periods'][f'{w}/{p}']
            c = {arm: med([x['competitor_mbps'][arm] for x in r]) for arm in ['reno', 'cand', 'aa']}
            fo = {arm: med([x['focal_mbps'][arm] for x in r]) for arm in ['reno', 'cand']}
            combined = med([x['competitor_mbps']['cand'] + x['focal_mbps']['cand'] for x in r])
            rows.append(f"| {NAMES[w]} | {p} | {f(c['reno'], 1)} | {f(c['cand'], 1)} | {f(c['aa'], 1)} | {f(fo['reno'], 1)} | {f(fo['cand'], 1)} | {f(combined, 1)} |")
    return '\n'.join(rows)


def competitor_control():
    rows = ['| Workload | Period | Competitor p95, next to Reno (ms) | Next to BBRv3 (ms) | Ratio, median | Pairs above 1.20 | Flag |',
            '| --- | --- | ---: | ---: | ---: | ---: | --- |']
    for w in WORKLOADS:
        for p in PERIODS:
            r = A['periods'][f'{w}/{p}']
            flag = A['competitor_control'][f'{w}/{p}']
            rows.append(f"| {NAMES[w]} | {p} | {f(med([x['competitor_control_p95_ms']['reno'] for x in r]), 1)} | "
                        f"{f(med([x['competitor_control_p95_ms']['cand'] for x in r]), 1)} | {f(flag['median'])} | {flag['crossing']} of {flag['pairs']} | "
                        f"{'raised' if flag['raised'] else 'not raised'} |")
    return '\n'.join(rows)


def focal_value(o, measure):
    if measure == 'control_p95':
        return o['control_p95_ms']
    role, rest = measure.split('_', 1)
    return o[role]['cpu_seconds_per_gib'] if rest == 'cpu_per_gib' else o[role]['peak_rss_mib']


def focal_flags():
    labels = {'control_p95': ('control p95', 'ms'), 'send_cpu_per_gib': ('sender CPU per useful GiB', 's'),
              'receive_cpu_per_gib': ('receiver CPU per useful GiB', 's'), 'send_peak_rss': ('sender peak RSS', 'MiB'),
              'receive_peak_rss': ('receiver peak RSS', 'MiB')}
    rows = ['| Workload | Measure | Matched Reno | BBRv3 | Ratio, median (limit) | Pairs crossing | A/A ratios | Flag |',
            '| --- | --- | ---: | ---: | ---: | ---: | --- | --- |']
    for w in WORKLOADS:
        for m, (label, unit) in labels.items():
            fl = A['focal_flags'][f'{w}/{m}']
            reno = med([focal_value(o['focal'], m) for o in obs(w, 'reno')])
            cand = med([focal_value(o['focal'], m) for o in obs(w, 'cand')])
            rows.append(f"| {NAMES[w]} | {label} | {f(reno, 2)} {unit} | {f(cand, 2)} {unit} | {f(fl['median'])} ({fl['limit']:.2f}) | "
                        f"{fl['crossing']} of {fl['pairs']} | {' '.join(f(x) for x in fl['aa_ratios'])} | {'raised' if fl['raised'] else 'not raised'} |")
    return '\n'.join(rows)


def relay_context():
    rows = ['| Workload | Period | Arm | Focal drops | Competitor drops | Focal mean queue (KB) | Competitor mean queue (KB) |',
            '| --- | --- | --- | ---: | ---: | ---: | ---: |']
    for w in WORKLOADS:
        for p in CLASSIFIED:
            for arm, label in [('reno', 'Reno'), ('cand', 'BBRv3')]:
                rp = [o['relay']['periods'][p] for o in obs(w, arm)]
                rows.append(f"| {NAMES[w]} | {p} | {label} | {f(med([x['focal_drops'] for x in rp]), 0)} | {f(med([x['competitor_drops'] for x in rp]), 0)} | "
                            f"{f(med([x['focal_queue_mean_bytes'] for x in rp]) / 1000, 0)} | {f(med([x['competitor_queue_mean_bytes'] for x in rp]) / 1000, 0)} |")
    return '\n'.join(rows)


def ecn():
    rows = ['| Workload | Arm | Focal forward packets sent Not-ECT, per pair | Competitor Not-ECT, largest |', '| --- | --- | --- | ---: |']
    for w in WORKLOADS:
        for arm, label in [('reno', 'Reno'), ('cand', 'BBRv3'), ('aa', 'Reno (A/A)')]:
            shares = [o['relay']['forward']['focal']['ECNIn'][0] / sum(o['relay']['forward']['focal']['ECNIn']) for o in obs(w, arm)]
            comp = max(o['relay']['forward']['competitor']['ECNIn'][0] / sum(o['relay']['forward']['competitor']['ECNIn']) for o in obs(w, arm))
            rows.append(f"| {NAMES[w]} | {label} | {' '.join(f'{x:.0%}' for x in shares)} | {comp:.3%} |")
    return '\n'.join(rows)


def health():
    lag = max(o['relay']['max_arrival_lag_ms'] for o in SEL.values())
    late = max(max(o['relay']['forward']['max_late_ms'], o['relay']['reverse']['max_late_ms']) for o in SEL.values())
    pause = [v['raw']['competitor-send']['pause_observed_s'] for a in S['attempts'] for v in a['detail'].values()]
    loads = [v['raw'][n]['cpu_load'] for a in S['attempts'] for v in a['detail'].values()
             for n in ['focal-send', 'focal-receive', 'competitor-send', 'competitor-receive']]
    methods = sorted({o[w]['control_periods']['method'] for o in SEL.values() for w in ['focal', 'competitor']})
    udp = sorted({sum(v['udp'].values()) for a in S['attempts'] for v in a['detail'].values()})
    usable = sum(a['usable'] and not a['contaminated'] for a in S['attempts'])
    return (f"- {len(S['attempts'])} pair-workload attempts, {usable} with a usable and uncontaminated triple, "
            f"{sum(1 for a in S['attempts'] if a['phase'] == 'rerun')} reruns; {len(SEL)} observations selected.\n"
            f"- Relay: largest arrival lag {lag:.3f} ms; largest lateness {late:.3f} ms (limit 5 ms).\n"
            f"- Competitor pause observed at {min(p[0] for p in pause):.3f}–{max(p[0] for p in pause):.3f} s "
            f"and {min(p[1] for p in pause):.3f}–{max(p[1] for p in pause):.3f} s (tolerance 0.5 s).\n"
            f"- Largest endpoint CPU load: {max(loads):.3f} of its allotted cores (limit 0.80).\n"
            f"- Per-period control assignments: {', '.join(methods)} in every observation; host UDP error deltas: {udp}.")


R = S['readings']


def displacement():
    rows = ['| Workload | Period | Competitor next to BBRv3 (#808) | (#777) | (#792, plain) | (#792, traced) | Next to Reno (#808) | (#777) | Pair ratios #808 | Pair ratios #777 |',
            '| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |']
    for w in WORKLOADS:
        for p in CLASSIFIED:
            d = R['displacement'][f'{w}/{p}']
            rows.append(f"| {NAMES[w]} | {p} | {f(d['competitor_next_to_bbr_808'], 1)} | {f(d['competitor_next_to_bbr_777'], 1)} | "
                        f"{f(d['competitor_next_to_cand_792'], 1)} | {f(d['competitor_next_to_trace_792'], 1)} | {f(d['competitor_next_to_reno_808'], 1)} | "
                        f"{f(d['competitor_next_to_reno_777'], 1)} | {' '.join(f(x) for x in d['ratios_808'])} | {' '.join(f(x) for x in d['ratios_777'])} |")
    return '\n'.join(rows)


def marking():
    rows = ['| Workload | Period | Checks on application-limited packets #808 (#792) | On any supply-limited packet #808 (#792) | Crossings, limited #808 (#792) | `inflight_longterm` / path #808 (#792) | `max_bw` cycle advances #808 (#792) | Competitor drops in Up #808 (#792) | Clamp events #808 |',
            '| --- | --- | --- | --- | --- | --- | --- | --- | ---: |']
    pct = lambda x: '–' if x is None else f'{x:.0%}'
    for w in WORKLOADS:
        for p in CLASSIFIED:
            m = R['marking'][w][p]
            rows.append(f"| {NAMES[w]} | {p} | {pct(m['application_share_808'])} ({pct(m['application_share_792'])}) | "
                        f"{pct(m['supply_limited_share_808'])} ({pct(m['supply_limited_share_792'])}) | "
                        f"{m['crossings_808']}, {m['crossings_limited_808']} ({m['crossings_792']}, {m['crossings_limited_792']}) | "
                        f"{f(m['inflight_longterm_over_path_808'], 2)} ({f(m['inflight_longterm_over_path_792'], 2)}) | "
                        f"{' '.join(str(x) for x in m['cycle_advances_808'])} ({' '.join(str(x) for x in m['cycle_advances_792'])}) | "
                        f"{pct(m['competitor_drops_in_up_808'])} ({pct(m['competitor_drops_in_up_792'])}) | {m['clamp_events_808']} |")
    return '\n'.join(rows)


def ecn_reading():
    rows = ['| Observation | Focal forward Not-ECT | Share | Switch estimate (above 0.01% only) | Trace ECN failure | Competitor Not-ECT |',
            '| --- | ---: | ---: | --- | --- | ---: |']
    for k, e in sorted(R['ecn'].items()):
        fl = e['focal']
        rows.append(f"| {k}{' (contaminated)' if e.get('contaminated') else ''} | {fl['not_ect']} of {fl['forward_packets']} | {fl['not_ect_share']:.4%} | "
                    f"{'none' if e['relay_switch_s'] is None else f'{e["relay_switch_s"]:.1f} s'} | "
                    f"{'–' if 'trace_ecn_failed_at_s' not in e else ('none' if e['trace_ecn_failed_at_s'] is None else f'{e["trace_ecn_failed_at_s"]:.1f} s')} | "
                    f"{e['competitor']['not_ect']} |")
    rows.append('')
    rows.append(f"**ECN stays on: {'yes' if R['ecn_stays_on'] else 'no'}.**")
    return '\n'.join(rows)


def checker():
    c = R['checker']
    return (f"- Traces: {c['traces']}, complete: {'yes' if c['traces_complete'] else 'no'}.\n"
            f"- Draft-06 checker mismatches in the measured window: {c['mismatches_in_measured_window']}.\n"
            f"- Input check: {'every check ok' if c['reconcile_ok'] else 'findings: ' + json.dumps(c['reconcile_findings'])}.\n"
            f"- Competitor next to the trace ÷ next to the plain candidate, same pair: "
            + '; '.join(f"{k} " + ' '.join(f(v[p], 2) for p in CLASSIFIED) for k, v in sorted(c['trace_over_cand_competitor'].items())) + '.')


def datagram_loss():
    rows = ['| Observation | Sent | Lost | Loss | Relay forward drops | Lost minus relay drops |', '| --- | ---: | ---: | ---: | ---: | ---: |']
    for k, v in sorted(R['datagram_loss'].items()):
        rows.append(f"| {k} | {v['sent']} | {v['lost']} | {v['loss']:.4%} | {v['relay_forward_drops']} | {v['lost_minus_relay_drops']} |")
    return '\n'.join(rows)


TABLES = [classification, delivery, competitor_control, focal_flags, relay_context, ecn, health,
          displacement, marking, ecn_reading, checker, datagram_loss]

if __name__ == '__main__':
    out = {t.__name__: t() for t in TABLES}
    if '--check' in sys.argv:
        readme = (HERE / 'README.md').read_text()
        missing = [k for k, v in out.items() if v not in readme]
        print('all tables present' if not missing else f'missing or different: {missing}')
        sys.exit(1 if missing else 0)
    for k, v in out.items():
        print(f'<!-- {k} -->\n{v}\n')
