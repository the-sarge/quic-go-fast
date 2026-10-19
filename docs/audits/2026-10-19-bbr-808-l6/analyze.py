#!/usr/bin/env python3
"""#808 L6 analysis: counted observations -> summary.json, through the registered rules.

Written before any counted observation (README, "Registration: L6").
- **Classification** is #777's, unchanged: each pair-workload's triple (reno,
  cand, aa) is judged and chosen with #777's rules (matrix.judge,
  l6rules.choose_triple), and l6rules.analyze classifies the selected triples.
  The trace arm never enters it.
- **Trace (pairs 1-3)**: each selected trace observation goes through #792's
  `traced` (mechanism792.py, #792's analyze.py verbatim): the draft-06 checker
  (C0-C9), the input check (R1-R4) and the mechanism figures. #792's
  disturbance flag is carried as #792 computed it and is descriptive here.
- **#800's reading**: STREAM's displacement beside #777's and #792's (the
  competitor's delivery next to BBRv3 per period, the pair ratios), and from
  the traces the share of two-percent checks that judged a packet marked
  application-limited (`Delivery.Limited`), the crossings, how many were
  limited, `inflight_longterm` against the path, the max_bw filter's cycle
  advances and aging, and where the competitor's drops fall by BBRv3 phase,
  each beside #792's. Descriptive; no rule.
- **#803's reading**: for every selected BBRv3 focal observation (cand and
  trace), relay2's forward ECN counts ([Not-ECT, ECT(1), ECT(0), CE]), the
  relay estimate of a switch (#793's crossrun.py method) and, for traces,
  the first ECN failure row. ECN **stays on** only when no trace records an
  ECN failure, no relay switch estimate exists and every flow's forward
  Not-ECT count is at most 0.01% of its forward packets (frozen-Reno flows
  send 3 Not-ECT handshake packets in every #777 observation).
- **DATAGRAM loss**: per DATAGRAM observation, the focal flow's sent messages
  against distinct messages received (inside and outside the measured window),
  beside relay2's forward drops for that flow (the competitor is a reliable
  stream). DatagramReceiveDropsV1 is not read here (the frozen
  fixture does not read it); the side stage reports it.
summary.json is written only when the order completed (#777's rule, with the
trace's rerun trigger).

Usage: analyze.py
"""
import json
import statistics
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True
sys.path.insert(0, str(HERE))
import matrix  # noqa: E402
import run  # noqa: E402
import l6rules  # noqa: E402
import mechanism792 as m792  # noqa: E402

OUT = HERE / 'summary.json'
CLASSIFIED = ['equal', 'short', 'resumed']
PERIODS = ['equal', 'short', 'back', 'absent', 'resumed']
NOT_ECT_SHARE_LIMIT = 1e-4  # 0.01% of a flow's forward packets
R777 = ('71e88789e8b89dcfec960d4f54d20027ef1f77b9', 'docs/audits/2026-10-17-bbr-l6-coexistence/summary.json')
R792 = ('89e71a60e652d4627cac41ee35b9ed3e718fcfa2', 'docs/audits/2026-10-18-bbr-l6-diagnosis/summary.json')


def median(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def pinned(ref):
    return json.loads(subprocess.check_output(['git', 'show', f'{ref[0]}:{ref[1]}'], cwd=HERE))


def reference_777(s):
    """#777's per-period medians (#792's l6_reference, reading #777's summary at its commit)."""
    out = {}
    for key, pairs in s['analysis']['periods'].items():
        out[key] = dict(competitor_next_to_reno=median(p['competitor_mbps']['reno'] for p in pairs),
                        competitor_next_to_cand=median(p['competitor_mbps']['cand'] for p in pairs),
                        focal_cand=median(p['focal_mbps']['cand'] for p in pairs),
                        ratios=[p['ratio'] for p in pairs])
    return out


def trace_figures(mech_periods):
    """The #800 figures from one trace's mechanism periods."""
    out = {}
    for per in CLASSIFIED:
        p = mech_periods[per]
        tpc = p['two_percent_checks']
        drops = p['drops_by_bbr_phase']
        out[per] = dict(limited_share=tpc['limited_share'], checks=tpc['checks'], crossings=tpc['crossings'],
                        crossings_limited=tpc['crossings_limited'], checks_by_reason=tpc['checks_by_reason'],
                        inflight_longterm_over_path=p['inflight_longterm_over_path'], window_over_path=p['window_over_path'],
                        cycle_advances=p['limited_marks']['cycle_advances'], max_bw_aging_events=p['limited_marks']['max_bw_aging_events'],
                        up_exits=p['up_exits'], probe_interval_s=p['probe_interval_s_median'],
                        competitor_drops_in_up=(drops.get('Up') or {}).get('competitor_share'),
                        competitor_drops_in_cruise_or_refill=sum((drops.get(ph) or {}).get('competitor_share') or 0 for ph in ('Cruise', 'Refill')),
                        clamp_events=(p['properties'].get('clamp') or {}).get('events', 0))
    return out


def reference_792(s):
    """#792's per-pair STREAM and DATAGRAM figures: the competitor next to trace and cand, and the trace figures."""
    out = {}
    for key, e in s['pairs'].items():
        if not e.get('selected'):
            continue
        out[key] = dict(competitor_next_to_trace={k: e['trace']['flows']['competitor'][k] for k in PERIODS},
                        competitor_next_to_cand={k: e['cand']['flows']['competitor'][k] for k in PERIODS},
                        trace=trace_figures(e['trace']['mechanism']['periods']),
                        ecn_failed_at_s=e['trace']['mechanism']['ecn_failed_at_s'])
    return out


def completion():
    """(complete, reasons): #777's rule, with a pair-workload needing a decision also when its trace is not clean."""
    reasons = []
    if (run.ART / 'correctness-failure.json').exists():
        reasons.append('stopped by a correctness failure (correctness-failure.json)')
    path = run.ART / 'follow-decisions.json'
    decided = set()
    if path.exists():
        for e in json.loads(path.read_text()):
            needed = not e['usable'] or e['contaminated'] or not e.get('trace_clean', True)  # no key: no trace arm
            if not needed or isinstance(e.get('rerun'), dict) or e.get('rerun') == 'cap reached':
                decided.add((e['pair'], e['workload']))
    else:
        reasons.append('no follow decisions')
    missing = [(p, w) for p in range(1, l6rules.PAIRS + 1) for w in matrix.workloads(p) if (p, w) not in decided]
    if missing:
        reasons.append(f'no completed follow decision for {missing}')
    return not reasons, reasons


def attempted(phase, pair, workload):
    return any((run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}').exists() for arm in matrix.arms_of(pair))


def relay_switch(relay):
    """#793's crossrun.py estimate: the first 100 ms sample at which the focal flow's reconstructed arrivals
    reach its ECT(0) arrival count (None when no switch)."""
    focal = relay['Forward']['Focal']
    ect0 = focal['ECNIn'][2]
    if focal['ECNIn'][0] <= 3:
        return None
    size = focal['DeliveredBytes'] / focal['Delivered']
    for sample in relay['Samples']:
        if sample[7] / size + sample[5] >= ect0:
            return sample[0] / 1000
    return None


def ecn_row(d, arm):
    relay = json.loads((d / 'relay.json').read_text())
    out = {}
    for who in ('Focal', 'Competitor'):
        f = relay['Forward'][who]
        total = sum(f['ECNIn'])
        out[who.lower()] = dict(ecn_in=f['ECNIn'], not_ect=f['ECNIn'][0], forward_packets=total,
                                not_ect_share=f['ECNIn'][0] / total if total else None)
    out['relay_switch_s'] = relay_switch(relay)
    out['arm'] = arm
    return out


def datagram_loss(d):
    """The focal flow's DATAGRAM loss (the competitor is always a reliable stream, which retransmits)."""
    send = json.loads((d / 'focal-send.json').read_text())
    recv = json.loads((d / 'focal-receive.json').read_text())
    sent = send['result']['sent_messages']
    rx = recv['result']['receiver']
    got = rx['unique_messages'] + rx['outside_window']  # every distinct DATAGRAM received, warm-up included
    drops = json.loads((d / 'relay.json').read_text())['Forward']['Focal']['Dropped']
    return dict(sent=sent, distinct_received=got, lost=sent - got, loss=(sent - got) / sent if sent else None,
                relay_forward_drops=drops, lost_minus_relay_drops=sent - got - drops)


def main():
    complete, why = completion()
    if not complete:
        OUT.write_text(json.dumps(dict(status='incomplete', classification=None, reasons=why), indent=2) + '\n')
        print('incomplete: no classification;', why)
        return
    s777, s792 = pinned(R777), pinned(R792)
    ref777 = reference_777(s777)
    decisions = {(e['pair'], e['workload']): e for e in json.loads((run.ART / 'follow-decisions.json').read_text())}
    triples, choices, attempts_out, selected, traces, ecn, loss = {}, [], [], {}, {}, {}, {}
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in matrix.workloads(pair):
            attempts = []
            for phase in ['main', 'rerun']:
                if not attempted(phase, pair, workload):
                    continue
                usable, contaminated, detail = matrix.judge(phase, pair, workload)
                attempts.append((phase, usable, contaminated))
                attempts_out.append(dict(phase=phase, pair=pair, workload=workload, usable=usable, contaminated=contaminated,
                                         trace_clean=matrix.trace_clean(detail), detail=detail))
            label, status = l6rules.choose_triple(attempts) if attempts else (None, 'unusable')
            rows = None
            if status == 'clean':
                rows = {arm: run.summarize(run.OBS / f'{label}-L6-{workload}-p{pair}-{arm}') for arm in matrix.ARMS}
            triples[(workload, pair)] = rows
            for arm, row in (rows or {}).items():
                selected[f'{workload}/p{pair}/{arm}'] = row
            trace_phase = None
            if pair in matrix.TRACE_PAIRS:
                clean = [a['phase'] for a in attempts_out if (a['pair'], a['workload']) == (pair, workload) and a['trace_clean']]
                trace_phase = label if label in clean else (clean[0] if clean else None)
            choices.append(dict(pair=pair, workload=workload, chosen=label, status=status, trace=trace_phase,
                                decision_trace_clean=decisions[(pair, workload)].get('trace_clean')))
            if rows:
                d = run.OBS / f'{label}-L6-{workload}-p{pair}-cand'
                ecn[f'{workload}/p{pair}/cand'] = ecn_row(d, 'cand')
                if workload == 'datagram':
                    for arm in matrix.ARMS:
                        loss[f'{workload}/p{pair}/{arm}'] = datagram_loss(run.OBS / f'{label}-L6-{workload}-p{pair}-{arm}')
            if trace_phase:
                d = run.OBS / f'{trace_phase}-L6-{workload}-p{pair}-trace'
                summary = run.summarize(d)
                t = m792.traced(d, summary, ref777)
                t['flows'] = m792.flows(summary)
                t['figures'] = trace_figures(t['mechanism']['periods'])
                traces[f'{workload}/p{pair}'] = t
                ecn[f'{workload}/p{pair}/trace'] = dict(ecn_row(d, 'trace'), trace_ecn_failed_at_s=t['mechanism']['ecn_failed_at_s'])
                if workload == 'datagram':
                    loss[f'{workload}/p{pair}/trace'] = datagram_loss(d)
    analysis = l6rules.analyze(triples)
    ecn_on = (all(e['relay_switch_s'] is None and e['focal']['not_ect_share'] is not None
                  and e['focal']['not_ect_share'] <= NOT_ECT_SHARE_LIMIT and e.get('trace_ecn_failed_at_s') is None
                  for e in ecn.values()) and bool(ecn))
    displacement = {}
    for cell, rows in analysis['periods'].items():
        wl, per = cell.split('/')
        displacement[cell] = dict(
            competitor_next_to_bbr_808=median(p['competitor_mbps']['cand'] for p in rows),
            competitor_next_to_reno_808=median(p['competitor_mbps']['reno'] for p in rows),
            competitor_next_to_bbr_777=ref777[cell]['competitor_next_to_cand'],
            competitor_next_to_reno_777=ref777[cell]['competitor_next_to_reno'],
            ratios_808=[p['ratio'] for p in rows], ratios_777=ref777[cell]['ratios'])
    ref792 = reference_792(s792)
    for cell in displacement:
        wl, per = cell.split('/')
        displacement[cell]['competitor_next_to_cand_792'] = median(v['competitor_next_to_cand'][per] for k, v in ref792.items() if k.startswith(wl + '/'))
        displacement[cell]['competitor_next_to_trace_792'] = median(v['competitor_next_to_trace'][per] for k, v in ref792.items() if k.startswith(wl + '/'))
    marking = {wl: {per: dict(
        limited_share_808=median(t['figures'][per]['limited_share'] for k, t in traces.items() if k.startswith(wl + '/')),
        limited_share_792=median(v['trace'][per]['limited_share'] for k, v in ref792.items() if k.startswith(wl + '/')),
        crossings_808=sum(t['figures'][per]['crossings'] for k, t in traces.items() if k.startswith(wl + '/')),
        crossings_limited_808=sum(t['figures'][per]['crossings_limited'] for k, t in traces.items() if k.startswith(wl + '/')),
        crossings_792=sum(v['trace'][per]['crossings'] for k, v in ref792.items() if k.startswith(wl + '/')),
        crossings_limited_792=sum(v['trace'][per]['crossings_limited'] for k, v in ref792.items() if k.startswith(wl + '/')),
        inflight_longterm_over_path_808=median(t['figures'][per]['inflight_longterm_over_path'] for k, t in traces.items() if k.startswith(wl + '/')),
        inflight_longterm_over_path_792=median(v['trace'][per]['inflight_longterm_over_path'] for k, v in ref792.items() if k.startswith(wl + '/')),
        cycle_advances_808=[t['figures'][per]['cycle_advances'] for k, t in traces.items() if k.startswith(wl + '/')],
        cycle_advances_792=[v['trace'][per]['cycle_advances'] for k, v in ref792.items() if k.startswith(wl + '/')],
        competitor_drops_in_up_808=median(t['figures'][per]['competitor_drops_in_up'] for k, t in traces.items() if k.startswith(wl + '/')),
        competitor_drops_in_up_792=median(v['trace'][per]['competitor_drops_in_up'] for k, v in ref792.items() if k.startswith(wl + '/')),
        clamp_events_808=sum(t['figures'][per]['clamp_events'] for k, t in traces.items() if k.startswith(wl + '/')),
    ) for per in CLASSIFIED} for wl in ('stream', 'datagram')}
    checker = dict(
        traces=len(traces),
        mismatches_in_measured_window=sum(sum(n for p, n in r['by_period'].items() if p in m792.dc.PERIODS)
                                          for t in traces.values() for r in t['check']['by_rule'].values()),
        traces_complete=all(t['check']['coverage']['complete'] for t in traces.values()),
        reconcile_ok=all(t['reconcile']['ok'] for t in traces.values()),
        reconcile_findings=[dict(pair=k, **f) for k, t in traces.items() for f in t['reconcile']['findings']],
        trace_over_cand_competitor={k: {per: (None if not selected.get(f'{k.split("/")[0]}/{k.split("/")[1]}/cand') else
                                              (t['flows']['competitor'][per] / selected[f'{k.split("/")[0]}/{k.split("/")[1]}/cand']['competitor']['periods'][per]['mbps']
                                               if selected[f'{k.split("/")[0]}/{k.split("/")[1]}/cand']['competitor']['periods'][per]['mbps'] else None))
                                        for per in CLASSIFIED} for k, t in traces.items()})
    out = dict(status='complete', classification=analysis['classification'], cells=analysis['cells'], triples=choices,
               attempts=attempts_out, analysis=analysis, readings=dict(
                   ecn_stays_on=ecn_on, ecn=ecn, displacement=displacement, marking=marking, checker=checker, datagram_loss=loss),
               traces=traces, reference_777=ref777, reference_792=ref792, selected_observations=selected)
    OUT.write_text(json.dumps(out, indent=2, sort_keys=True) + '\n')
    print('classification:', analysis['classification'], '| ECN stays on:', ecn_on)
    fmt = lambda xs: ' '.join('  -  ' if x is None else f'{x:.3f}' for x in xs)
    for cell, outcome in analysis['cells'].items():
        rows = analysis['periods'][cell]
        print(f"{cell:18} {outcome:12} ratios {fmt([r['ratio'] for r in rows])} | A/A {fmt([r['aa_ratio'] for r in rows])}")
    for wl, per_map in marking.items():
        for per, m in per_map.items():
            print(wl, per, 'limited share', m['limited_share_808'], '(792:', m['limited_share_792'], ')')


if __name__ == '__main__':
    main()
