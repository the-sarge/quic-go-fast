#!/usr/bin/env python3
"""#792 analysis: L6 observations -> summary.json, through the registered rules.

For every selected observation, `run.summarize` (#777's) gives each flow's
delivery per period and the relay's per-flow queue and drops. For each traced
(`trace`) observation it also runs:
- draft06check.py: every rule's mismatches, by period;
- reconcile.py: the sender's inputs against the relay's ground truth;
- the disturbance rule (operator decision 3): the trace is representative only
  if, in every classified period, the competitor got less than 0.70 x its
  median delivery next to matched Reno in #777 (same workload and period);
- the mechanism figures, descriptive: phase shares, why probes started and
  ended, BBRv3's own loss per round, which bound limited the window, how the
  bandwidth estimate compares with its own delivery, and where in BBRv3's cycle
  the competitor's drops fall.
It also compares each traced observation with its pair's plain (`cand`)
observation (the instrument's disturbance, reported) and with #777's plain
observations. Selection follows the follow decisions (#714 rerun rule); the
counted stage emits no outcome until the order completed.

Usage: analyze.py            counted stage -> summary.json
       analyze.py --dev NAME...  per-observation figures for development runs (stdout)
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True
sys.path.insert(0, str(HERE))
import matrix  # noqa: E402  (sets the import path: this record's run, #777's l6rules)
import run  # noqa: E402
import l6rules  # noqa: E402
import draft06check as dc  # noqa: E402
import reconcile  # noqa: E402

OUT = HERE / 'summary.json'
CLASSIFIED = ['equal', 'short', 'resumed']
RENO_SHARE = 0.70
QUEUE_WIRE_BYTES = 1_251_220            # L6's drop-tail queue, each direction
PIPE_WIRE_BYTES = 100e6 / 8 * 0.100     # 100 Mbit/s x 100 ms


def median(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def l6_reference():
    """#777's per-period medians: the competitor next to matched Reno and next to the plain port."""
    s = json.loads((matrix.L6 / 'summary.json').read_text())
    out = {}
    for key, pairs in s['analysis']['periods'].items():
        out[key] = dict(competitor_next_to_reno=median(p['competitor_mbps']['reno'] for p in pairs),
                        competitor_next_to_cand=median(p['competitor_mbps']['cand'] for p in pairs),
                        focal_cand=median(p['focal_mbps']['cand'] for p in pairs))
    return out


def timeline(t):
    """[(unix_ns, phase)] from the trace's change pairs, starting at the first row."""
    out = [(t.v(t.rows[0], 'unix_ns'), t.v(t.rows[0], 'phase'))] if t.rows else []
    for pre, post in t.pairs():
        if t.v(pre, 'phase') != t.v(post, 'phase'):
            out.append((t.v(post, 'unix_ns'), t.v(post, 'phase')))
    return out


def phase_at(tl, ns):
    phase = tl[0][1]
    for at, p in tl:
        if at > ns:
            break
        phase = p
    return phase


def shares(tl, s0, s1):
    """Fraction of [s0, s1) in each phase."""
    acc = {}
    for i, (at, p) in enumerate(tl):
        end = tl[i + 1][0] if i + 1 < len(tl) else s1
        a, b = max(at, s0), min(end, s1)
        if b > a:
            acc[dc.PHASES[p]] = acc.get(dc.PHASES[p], 0) + (b - a)
    total = s1 - s0
    return {k: v / total for k, v in sorted(acc.items())}


def binding_cap(t, r):
    """Which bound sets the window ceiling at a ROUND row (the draft's BoundCwndForModel order)."""
    if t.v(r, 'min_rtt_ns') == 0 or t.v(r, 'phase') == dc.STARTUP:
        return None
    up = t.v(r, 'phase') == dc.UP
    target = dc.quantize(t, r, (dc.bdp(t, r, 9, 4) if up else dc.bdp(t, r, 2, 1)) + t.v(r, 'extra'), up)
    caps = {'model': target}
    if t.v(r, 'inflight_short') != dc.INF:
        caps['inflight_shortterm'] = t.v(r, 'inflight_short')
    if t.v(r, 'phase') >= dc.DOWN and t.v(r, 'inflight_long') != dc.INF:
        caps['inflight_longterm'] = t.v(r, 'inflight_long')
    if t.v(r, 'phase') in (dc.CRUISE, dc.PROBERTT) and dc.headroom(t, r) != dc.INF:
        caps['headroom'] = dc.headroom(t, r)
    return min(caps, key=lambda k: (caps[k], k != 'model'))


def cruise_cap(hi, size):
    """The Cruise window ceiling a given inflight_longterm allows (InflightWithHeadroom)."""
    return max(4 * size, hi - max(size, hi * 15 // 100))


def effective_ceiling(t, r, hi):
    """The Cruise window ceiling an inflight_longterm value gives at row r, with the other caps that bind
    there (inflight_shortterm and the model target); None for unbounded."""
    size = t.v(r, 'size')
    caps = []
    if hi != dc.INF:
        caps.append(cruise_cap(int(hi), size))
    if t.v(r, 'inflight_short') != dc.INF:
        caps.append(t.v(r, 'inflight_short'))
    b = dc.bdp(t, r, 2, 1)
    if b is not None:
        caps.append(dc.quantize(t, r, b + t.v(r, 'extra')))
    return min(caps) if caps else None


def paired(events, pipe):
    """Paired event values (counterfactual, actual) over the pipe, and how the property changed the
    effective ceiling: lowered it (the counterfactual is lower), left it equal, raised it, or changed the
    bound while another cap masked the change. Descriptive: no causal role follows from these numbers."""
    rows = [dict(counterfactual=None if cf is None else cf / pipe, actual=None if ac is None else ac / pipe,
                 masked=masked) for cf, ac, masked in events]
    counts = dict(property_kept_higher=0, equal=0, property_kept_lower=0, masked=0)
    for cf, ac, masked in events:
        if masked:
            counts['masked'] += 1
        elif cf == ac:
            counts['equal'] += 1
        elif ac is None or (cf is not None and cf < ac):
            counts['property_kept_higher'] += 1
        else:
            counts['property_kept_lower'] += 1
    return rows, counts


def properties(t, check, cross, s0, s1, pipe):
    """Each port property acting in [s0, s1): its events, and descriptive mechanism evidence for one
    channel, the inflight_longterm assignment: the effective Cruise ceiling with the property against the
    one draft-06's assignment would give, per event. The classification needs a documented trace and code
    argument for a property's connection to the displacement (README, Classification); these figures
    are evidence for that argument, not a decision."""
    out = {}
    if not pipe:
        return out
    app = [r for r in cross if r[t.c['a4']] == 1]
    if app:
        events = []
        for r in app:
            lost, flight, length = (r[t.c[k]] for k in ('a0', 'a1', 'a2'))
            b = dc.bdp(t, r, 1, 1)
            target = t.v(r, 'window') if b is None else min(b, t.v(r, 'window'))
            draft_hi = max(dc.inflight_at_loss(lost, flight, length, False), 0.7 * target)
            actual_hi = t.v(r, 'inflight_long')  # unchanged by a limited crossing; INF stays unbounded
            cf, ac = effective_ceiling(t, r, draft_hi), effective_ceiling(t, r, actual_hi)
            bound_moved = actual_hi == dc.INF or abs(actual_hi - draft_hi) >= 1
            events.append((cf, ac, bound_moved and cf == ac))
        rows, counts = paired(events, pipe)
        out['application_limited_crossings'] = dict(events=len(app), channel='inflight_longterm assignment only',
                                                    paired=rows, effect=counts)
    devs = [x for x in check['rules']['C4'].get('deviations', []) if s0 <= x['unix_ns'] < s1]
    if devs:
        events = [(cruise_cap(int(x['draft']), x['size']), cruise_cap(x['port'], x['size']), False) for x in devs]
        rows, counts = paired(events, pipe)
        out['clamp'] = dict(events=len(devs), excess_bytes=sum(x['excess_bytes'] for x in devs), channel='inflight_longterm assignment only',
                            paired=rows, effect=counts)
    flow = [r for r in cross if r[t.c['a4']] == 2]
    if flow:
        # The design's declared choice (flow-control blocking is supply-limited) against draft-06 4.1.1.3.1.
        out['flow_control_limited_crossings'] = dict(events=len(flow), review='declared deviation; connection unresolved until argued')
    return out


def mechanism(t, check, relay, measured_start, quic_share):
    tl = timeline(t)
    rounds = [r for r in t.rows if r[0] == dc.ROUND]
    samples = relay['Samples']
    capacity = None if quic_share is None else (QUEUE_WIRE_BYTES + PIPE_WIRE_BYTES) * quic_share
    pipe = None if quic_share is None else PIPE_WIRE_BYTES * quic_share
    out = {}
    for name, (a, b) in dc.PERIODS.items():
        s0, s1 = measured_start + a * 10**9, measured_start + b * 10**9
        inside = [r for r in rounds if s0 <= t.v(r, 'unix_ns') < s1]
        p = dict(phase_shares=shares(tl, s0, s1))
        refills = [e for e in check['rules']['C1'].get('events', []) if s0 <= e['unix_ns'] < s1]
        p['refills'] = {k: sum(1 for e in refills if e['reason'] == k) for k in sorted({e['reason'] for e in refills})}
        gaps = [(y['unix_ns'] - x['unix_ns']) / 1e9 for x, y in zip(refills, refills[1:])]
        p['probe_interval_s_median'] = median(gaps)
        ups = [e for e in check['rules']['C3'].get('events', []) if s0 <= e['unix_ns'] < s1]
        p['up_exits'] = {k: sum(1 for e in ups if e['reason'] == k) for k in sorted({str(e['reason']) for e in ups})}
        crossings = [e for e in check['rules']['C4'].get('events', []) if s0 <= e['unix_ns'] < s1]
        p['crossings'] = len(crossings)
        # The two-percent checks: how many judged a packet marked supply-limited at send
        # (such a crossing cannot lower inflight_longterm), and where each crossing left it.
        checks = [r for r in t.rows if r[0] == dc.PROBELOSS and s0 <= t.v(r, 'unix_ns') < s1]
        cross = [r for r in checks if dc.crosses(t, r)]
        limited = lambda r: r[t.c['a4']] in dc.LIMITED
        reasons = lambda rows: {name: sum(1 for r in rows if r[t.c['a4']] == v) for v, name in dc.LIMIT_NAMES.items()}
        p['two_percent_checks'] = dict(checks=len(checks), limited_share=sum(map(limited, checks)) / len(checks) if checks else None,
                                       crossings=len(cross), crossings_limited=sum(map(limited, cross)),
                                       checks_by_reason=reasons(checks), crossings_by_reason=reasons(cross))
        p['properties'] = properties(t, check, cross, s0, s1, pipe)
        # Other draft rules a supply-limited mark switches (descriptive): an Up round start on a limited
        # sample does not advance the plateau count, and a limited sample at the probe's end does not
        # advance the max_bw filter's cycle, so max_bw cannot age.
        up_rounds = [r for r in inside if t.v(r, 'phase') == dc.UP]
        cycles = sorted({t.v(r, 'cycle') for r in inside})
        aging = [r for r in t.rows if r[0] == dc.POST and s0 <= t.v(r, 'unix_ns') < s1 and r[t.c['a0']] & (1 << 9)]
        p['limited_marks'] = dict(rounds_with_limited_samples_share=sum(1 for r in inside if r[t.c['a5']] > 0) / len(inside) if inside else None,
                                  up_round_starts=len(up_rounds), up_round_starts_on_limited_samples=sum(1 for r in up_rounds if t.flag(r, 'sample_limited')),
                                  cycle_advances=len(cycles) - 1 if cycles else 0, max_bw_aging_events=len(aging))
        after = []
        for r in cross:
            post = t.end_of_event(r)
            if post is not None and t.v(post, 'inflight_long') != dc.INF and capacity:
                after.append(t.v(post, 'inflight_long') / capacity)
        p['two_percent_checks']['inflight_longterm_after_crossing_over_path_median'] = median(after)
        p['two_percent_checks']['crossing_flight_over_path_median'] = None if not capacity else median(r[t.c['a1']] / capacity for r in cross)
        # BBRv3's own loss per packet-timed round (lost bytes / bytes delivered in that round).
        rates = []
        for r in inside:
            delivered = t.v(r, 'delivered') - r[t.c['a6']]
            if delivered > 0 and r[t.c['a6']] > 0:
                rates.append(r[t.c['a0']] / delivered)
        lossy = [x for x in rates if x > 0]
        p['rounds'] = len(inside)
        p['round_loss'] = dict(share_of_rounds_with_loss=len(lossy) / len(rates) if rates else None,
                               median_when_lossy=median(lossy), p90_when_lossy=None if not lossy else sorted(lossy)[int(0.9 * (len(lossy) - 1))],
                               share_of_rounds_above_2pct=sum(1 for x in rates if x > 0.02) / len(rates) if rates else None)
        caps = [binding_cap(t, r) for r in inside]
        caps = [c for c in caps if c]
        p['window_ceiling_set_by'] = {k: caps.count(k) / len(caps) for k in sorted(set(caps))} if caps else {}
        p['cwnd_limited_round_share'] = sum(1 for r in inside if t.flag(r, 'window_used_last_round')) / len(inside) if inside else None
        p['bw_shortterm_below_max_bw_share'] = (sum(1 for r in inside if t.v(r, 'bandwidth_short') != dc.INF and t.v(r, 'bandwidth_short') < t.v(r, 'bandwidth'))
                                                / len(inside) if inside else None)
        hi = [t.v(r, 'inflight_long') for r in inside if t.v(r, 'inflight_long') != dc.INF]
        p['inflight_longterm_over_path'] = None if not hi or not capacity else median(hi) / capacity
        p['window_over_path'] = None if not inside or not capacity else median(t.v(r, 'window') for r in inside) / capacity
        p['min_rtt_ms_median'] = median(t.v(r, 'min_rtt_ns') / 1e6 for r in inside if t.v(r, 'min_rtt_ns'))
        # Where in BBRv3's cycle each flow's drops fall, against the time BBRv3 spends there.
        rows = [s for s in samples if a * 1000 <= s[0] < b * 1000]
        drops = {}
        queue = {}
        prev = next((s for s in reversed(samples) if s[0] < a * 1000), None)
        for s in rows:
            ph = dc.PHASES[phase_at(tl, measured_start + s[0] * 10**6)]
            d = drops.setdefault(ph, [0, 0])
            if prev is not None:
                d[0] += s[5] - prev[5]
                d[1] += s[6] - prev[6]
            q = queue.setdefault(ph, [0, 0, 0])
            q[0] += s[1]
            q[1] += s[2]
            q[2] += 1
            prev = s
        tf, tc = sum(v[0] for v in drops.values()), sum(v[1] for v in drops.values())
        p['drops_by_bbr_phase'] = {ph: dict(focal_share=None if not tf else v[0] / tf, competitor_share=None if not tc else v[1] / tc)
                                   for ph, v in sorted(drops.items())}
        p['queue_kb_by_bbr_phase'] = {ph: dict(focal=v[0] / v[2] / 1e3, competitor=v[1] / v[2] / 1e3) for ph, v in sorted(queue.items())}
        out[name] = p
    ecn = next((r for r in t.rows if t.flag(r, 'ecn_failed')), None)
    return dict(periods=out, ecn_failed_at_s=None if ecn is None else (t.v(ecn, 'unix_ns') - measured_start) / 1e9)


def traced(directory, summary, reference):
    cfg = json.loads((directory / 'focal-config.json').read_text())
    measured_start = cfg['start_unix_ns'] + reconcile.WARMUP_NS
    data = json.loads((directory / 'focal-trace.json').read_text())
    relay = json.loads((directory / 'relay.json').read_text())
    check = dc.run(data, measured_start)
    rec = reconcile.reconcile(data, relay, measured_start)
    t = dc.Trace(data, measured_start)
    wl = summary['workload']
    disturbance = {}
    for name in CLASSIFIED:
        got = summary['competitor']['periods'][name]['mbps']
        limit = RENO_SHARE * reference[f'{wl}/{name}']['competitor_next_to_reno']
        disturbance[name] = dict(competitor_mbps=got, limit_mbps=limit, adverse=got < limit)
    mech = mechanism(t, check, relay, measured_start, rec['quic_share'])
    per_second = summary['competitor']['per_second_mbps']
    if mech['ecn_failed_at_s'] is not None and 0 <= mech['ecn_failed_at_s'] < 300:
        k = int(mech['ecn_failed_at_s'])
        before, after = per_second[max(0, k - 30):k], per_second[k:k + 30]
        mech['competitor_mbps_30s_before_after'] = [None if not before else sum(before) / len(before), None if not after else sum(after) / len(after)]
    return dict(check=dict(coverage=check['coverage'], mismatches=check['mismatches'],
                           by_rule={k: dict(checked=v['checked'], mismatches=len(v['mismatches']), by_period=v['mismatches_by_period'],
                                            first=v['mismatches'][:5], notes=v['notes']) for k, v in check['rules'].items()}),
                reconcile=rec, disturbance=dict(periods=disturbance, representative=all(v['adverse'] for v in disturbance.values())),
                mechanism=mech)


def attribute(traces):
    """One workload's gates under the registered rules (README, Classification). The class itself needs a
    documented argument, so this never returns (a), (b) or (c): it returns the gates, every acting property
    with its evidence, and what the argument must settle."""
    if len(traces) < 2 or not all(x['check']['coverage']['complete'] for x in traces):
        return {'class': 'undiagnosed (instrument)', 'reason': 'fewer than two usable traces with complete traces'}
    if not all(x['disturbance']['representative'] for x in traces):
        return {'class': 'undiagnosed (instrument)', 'reason': 'D3 fails'}
    mismatches = sum(sum(n for p, n in r['by_period'].items() if p in dc.PERIODS) for x in traces for r in x['check']['by_rule'].values())
    findings = [f for x in traces for f in x['reconcile']['findings']]
    acting = {}
    for x in traces:
        for per in CLASSIFIED:
            for name, ev in x['mechanism']['periods'][per]['properties'].items():
                a = acting.setdefault(name, dict(events=0, paired=[], effect={}))
                a['events'] += ev['events']
                a['paired'] += ev.get('paired') or []  # pooled over events, as registered
                for k, v in (ev.get('effect') or {}).items():
                    a['effect'][k] = a['effect'].get(k, 0) + v
    for a in acting.values():
        cf = [p['counterfactual'] for p in a['paired']]
        ac = [p['actual'] for p in a['paired']]
        a['counterfactual_over_pipe_median'] = median(cf)
        a['actual_over_pipe_median'] = median(x for x in ac if x is not None)
        a['actual_unbounded_events'] = sum(1 for x in ac if x is None)
        del a['paired']
    out = dict(mismatches=mismatches, reconcile_findings=findings, properties=acting)
    if mismatches or findings:
        out['class'] = 'needs review'
        out['reason'] = 'checker mismatches or input-check findings: settle by reviewed evidence (README rules 2-5)'
    elif acting:
        out['class'] = 'needs an argument'
        out['reason'] = 'a port property acts: its connection to the displacement needs a documented argument, else unresolved (rule 5)'
    else:
        out['class'] = 'needs an argument'
        out['reason'] = 'no port property acts: (a) needs the mechanism argument from the figures (rule 4)'
    return out


def flows(summary):
    return dict(focal={k: v['mbps'] for k, v in summary['focal']['periods'].items()},
                competitor={k: v['mbps'] for k, v in summary['competitor']['periods'].items()},
                relay={k: None if v is None else dict(focal_drops=v['focal_drops'], competitor_drops=v['competitor_drops'],
                                                      focal_queue_kb=v['focal_queue_mean_bytes'] / 1e3,
                                                      competitor_queue_kb=v['competitor_queue_mean_bytes'] / 1e3)
                       for k, v in summary['relay']['periods'].items()})


def completion():
    """(complete, reasons): #777's rule with this record's arms and pairs."""
    reasons = []
    if (run.ART / 'correctness-failure.json').exists():
        reasons.append('stopped by a correctness failure (correctness-failure.json)')
    path = run.ART / 'follow-decisions.json'
    decided = set()
    if path.exists():
        for e in json.loads(path.read_text()):
            needed = not e['usable'] or e['contaminated']
            if not needed or isinstance(e.get('rerun'), dict) or e.get('rerun') == 'cap reached':
                decided.add((e['pair'], e['workload']))
    else:
        reasons.append('no follow decisions')
    missing = [(p, w) for p in range(1, matrix.PAIRS + 1) for w in matrix.workloads(p) if (p, w) not in decided]
    if missing:
        reasons.append(f'no completed follow decision for {missing}')
    return not reasons, reasons


def selected():
    """{(pair, workload): phase or None} from the follow decisions: main if clean, else a clean rerun, else none."""
    out = {}
    for e in json.loads((run.ART / 'follow-decisions.json').read_text()):
        key = (e['pair'], e['workload'])
        if e['usable'] and not e['contaminated']:
            out[key] = 'main'
        elif isinstance(e.get('rerun'), dict) and e['rerun']['usable'] and not e['rerun']['contaminated']:
            out[key] = 'rerun'
        else:
            out[key] = None
    return out


def dev(names):
    reference = l6_reference()
    for name in names:
        d = run.OBS / name
        summary = run.summarize(d)
        row = dict(id=name, flows=flows(summary))
        if (d / 'focal-trace.json').exists():
            row.update(traced(d, summary, reference))
        print(json.dumps(row, indent=1, sort_keys=True))


def main():
    if sys.argv[1:2] == ['--dev']:
        return dev(sys.argv[2:])
    complete, reasons = completion()
    if not complete:
        OUT.write_text(json.dumps(dict(status='incomplete', reasons=reasons), indent=1) + '\n')
        print('incomplete:', reasons)
        return
    reference = l6_reference()
    pairs = {}
    for (pair, wl), phase in sorted(selected().items()):
        key = f'{wl}/p{pair}'
        if phase is None:
            pairs[key] = dict(selected=None)
            continue
        entry = dict(selected=phase)
        for arm in matrix.ARMS:
            d = run.OBS / matrix.name(phase, pair, wl, arm)
            summary = run.summarize(d)
            entry[arm] = dict(flows=flows(summary), summary=summary)
            if arm == 'trace':
                entry[arm].update(traced(d, summary, reference))
        entry['trace_over_cand'] = {who: {k: None if not entry['cand']['flows'][who][k] else entry['trace']['flows'][who][k] / entry['cand']['flows'][who][k]
                                          for k in dc.PERIODS} for who in ('focal', 'competitor')}
        pairs[key] = entry
    attribution = {wl: attribute([v['trace'] for k, v in pairs.items() if k.startswith(wl + '/') and v.get('selected')])
                   for wl in ('stream', 'datagram')}
    traces = [v['trace'] for v in pairs.values() if v.get('selected')]
    status = dict(observations=len(traces), representative=all(x['disturbance']['representative'] for x in traces),
                  mismatches_in_measured_window=sum(sum(n for p, n in r['by_period'].items() if p in dc.PERIODS)
                                                    for x in traces for r in x['check']['by_rule'].values()),
                  mismatches_total=sum(x['check']['mismatches'] for x in traces),
                  traces_complete=all(x['check']['coverage']['complete'] for x in traces),
                  reconcile_ok=all(x['reconcile']['ok'] for x in traces),
                  reconcile_findings=[dict(pair=k, **f) for k, v in pairs.items() if v.get('selected') for f in v['trace']['reconcile']['findings']])
    status['attribution'] = attribution
    OUT.write_text(json.dumps(dict(status='complete', preconditions=status, reference_777=reference, pairs=pairs), indent=1, sort_keys=True) + '\n')
    print(json.dumps(status, indent=1))


if __name__ == '__main__':
    main()
