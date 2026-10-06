#!/usr/bin/env python3
"""#734 registered rules, as pure functions.

Registered in README.md ("Registration") and exercised on synthetic cases in
rules_test.py before any counted observation existed. Every outcome says what a
stage found or whether a registered prediction held; none sets a readiness flag.
"""
import statistics

# ---- contamination reruns (the operator's #714 rule, as #715 registered it) -------------------


def choose_block(attempts):
    """attempts: [(label, usable, contaminated)] in run order, the original first.

    A block holding an unusable or contaminated observation is rerun once with the
    same seed; the rerun replaces the original only when it is usable and
    uncontaminated. Returns (label, status) with status 'clean', 'contaminated' or
    'unusable'.
    """
    original = attempts[0]
    if original[1] and not original[2]:
        return original[0], 'clean'
    for label, usable, contaminated in attempts[1:2]:
        if usable and not contaminated:
            return label, 'clean'
    return original[0], 'unusable' if not original[1] else 'contaminated'


# ---- Stage 0: instrument preflight ------------------------------------------------------------
DELTA_NS = 300_000            # injected delay in every synthetic case
EVENT_SHARE = 0.95            # share of late events that must show the injected delay
CHAIN_SHARE = 0.99            # reset case: share of matched events whose chain counts must match exactly
COVERAGE = 0.99               # share of the truth's in-window late events the analysis must find
ALIGNMENT_MAX = 0.001         # recorder events outside a running interval of the recorded goroutine
AMBIGUOUS_MAX = 0.02          # share of lateness without a known goroutine state
PERTURBATION_MIN = 0.99       # instrumented goodput over the plain build, median per-block ratio


def ana_usable(a, overlay=None):
    """Whether one analysis output (wake/ana) is usable for attribution, and why not.

    overlay: (late_count, late_ns) from an independent in-process count over the same
    window (the recorder's aggregates, or #715's overlay), or None.
    """
    if a.get('error'):
        return False, 'analysis error: ' + a['error']
    if a.get('trace_error'):
        return False, 'trace parse error'
    if not a.get('trace', {}).get('covers_window'):
        return False, 'trace does not cover the window'
    if a['alignment']['share'] > ALIGNMENT_MAX:
        return False, 'alignment'
    late = a['late']
    if late['sum_ns'] > 0 and late['segments_ns']['ambiguous'] / late['sum_ns'] > AMBIGUOUS_MAX:
        return False, 'ambiguous lateness'
    if overlay is not None:
        n, ns = overlay
        if abs(late['count'] - n) > 0.01 * max(n, 1) or abs(late['sum_ns'] - ns) > 0.01 * max(ns, 1):
            return False, 'lateness disagrees with the in-process count'
    return True, ''


def recovery(case, a, truth, start_mono, end_mono, delta=DELTA_NS):
    """Whether the analysis recovers one synthetic case. a: analysis with late events; truth: harness truth.

    Late events are matched by deadline (the recorder's own monotonic value). Truth events
    count when both the deadline and the opportunity lie in the recorded window, except the
    first chain after recording starts (its earlier arms predate the window).
    """
    ok, why = ana_usable(a)
    out = dict(case=case, usable=ok, reason=why)
    if not ok:
        return dict(out, recovered=False)
    ev = {e['d_mono']: e for e in a['late_events']}
    want = [t for t in truth['late'] if start_mono <= t['D'] and t['O'] < end_mono]
    want = want[1:]
    matched = [(t, ev[t['D']]) for t in want if t['D'] in ev]
    out['truth_events'], out['matched'] = len(want), len(matched)
    out['coverage'] = len(matched) / max(1, len(want))
    checks = {'coverage': out['coverage'] >= COVERAGE}
    v = shares_verdict(a['late']['shares'])
    out['verdict'] = v

    def share(pred):
        return sum(1 for t, e in matched if pred(t, e)) / max(1, len(matched))
    if case == 'delivery':
        out['share_shown'] = share(lambda t, e: e['segs']['delivery_timer'] >= delta)
        checks.update(verdict=v == 'delivery', events=out['share_shown'] >= EVENT_SHARE)
    elif case == 'scheduling':
        out['share_shown'] = share(lambda t, e: e['segs']['scheduling'] >= 0.9 * delta)
        checks.update(verdict=v == 'scheduling', events=out['share_shown'] >= EVENT_SHARE)
    elif case == 'handler':
        out['share_shown'] = share(lambda t, e: e['segs']['loop_running'] >= 0.9 * delta)
        checks.update(verdict=v == 'loop', events=out['share_shown'] >= EVENT_SHARE)
    elif case == 'nontimer':
        def shown(t, e):
            if e['ended_by'] != 'packet':
                return False
            if t['D'] < t['Sent'] < t['O']:
                return e['segs']['delivery_other'] >= 0.9 * (t['Sent'] - t['D'])
            return True
        out['share_shown'] = share(shown)
        checks.update(verdict=v == 'delivery', events=out['share_shown'] >= EVENT_SHARE)
    elif case == 'reset':
        exact = share(lambda t, e: (e['arms'], e['folded'], e['superseded']) == (t['Arms'], t['Folded'], t['Superseded']))
        tsum = sum(t['O'] - t['D'] for t, _ in matched)
        esum = sum(e['late_ns'] for _, e in matched)
        kinds = dict(rearmed=sum(1 for t, _ in matched if t['Arms'] >= 2 and not t['Folded'] and not t['Superseded']),
                     folded=sum(1 for t, _ in matched if t['Folded']), superseded=sum(1 for t, _ in matched if t['Superseded']))
        out.update(chain_exact=exact, lateness_truth_ns=tsum, lateness_found_ns=esum, kinds=kinds)
        checks.update(chains=exact >= CHAIN_SHARE, lateness=abs(esum - tsum) <= 0.001 * max(tsum, 1),
                      kinds=all(k > 0 for k in kinds.values()))
    else:
        raise ValueError(case)
    out['checks'] = checks
    out['recovered'] = all(checks.values())
    return out


def perturbation(pairs):
    """pairs: [(instrumented goodput, plain goodput)] per block. Passes when the median ratio is at least 0.99."""
    if not pairs:
        return dict(passed=False, reason='no usable block')
    r = [i / p for i, p in pairs]
    return dict(ratios=r, median=statistics.median(r), passed=statistics.median(r) >= PERTURBATION_MIN)


# ---- Stage 1: discrimination ------------------------------------------------------------------
CAUSES = ['delivery', 'scheduling', 'loop', 'unarmed']
LEAD, MINOR = 0.5, 0.25
MIN_LATENESS_NS = 100e6       # an observation with under 0.1 s of lateness has nothing to attribute


def shares_verdict(shares):
    """One observation's late-interval shares: the leading cause, 'mixed' or 'inconclusive'.

    A cause leads with at least half the lateness while every other cause holds under a
    quarter. Two or more causes at a quarter or more are 'mixed'.
    """
    if not shares:
        return 'no lateness'
    big = [c for c in CAUSES if shares.get(c, 0) >= MINOR]
    for c in CAUSES:
        if shares.get(c, 0) >= LEAD and all(shares.get(o, 0) < MINOR for o in CAUSES if o != c):
            return c
    if len(big) >= 2:
        return 'mixed'
    return 'inconclusive'


def s1_observation(a, overlay):
    ok, why = ana_usable(a, overlay)
    if not ok:
        return 'unusable', why
    if a['late']['sum_ns'] < MIN_LATENESS_NS:
        return 'no lateness', ''
    return shares_verdict(a['late']['shares']), ''


def workload_verdict(verdicts, n_min=3, usable_min=3):
    usable = [v for v in verdicts if v != 'unusable']
    if len(usable) < usable_min:
        return 'evidence gap'
    for v in set(usable):
        if sum(x == v for x in usable) >= n_min:
            return v
    return 'inconclusive'


def needs_escalation(verdicts):
    """A workload with exactly two of four agreeing usable blocks gets two more blocks, once."""
    usable = [v for v in verdicts if v != 'unusable']
    counts = sorted((sum(x == v for x in usable) for v in set(usable)), reverse=True)
    return len(verdicts) == 4 and bool(counts) and counts[0] == 2


STAGE1 = {'delivery': 'timer delivery', 'scheduling': 'goroutine scheduling', 'loop': 'loop work', 'unarmed': 'unarmed waits'}


def stage1_verdict(per_workload):
    """Both workloads must share one leading cause; anything else is inconclusive (reported per workload)."""
    vals = set(per_workload.values())
    if len(vals) == 1 and (v := vals.pop()) in STAGE1:
        return STAGE1[v]
    if 'evidence gap' in per_workload.values():
        return 'evidence gap'
    return 'inconclusive'


# ---- Stage 1: loopback rule ------------------------------------------------------------------
LB_DEFICIT = 1 - 0.858          # #715 readiness: loopback DATAGRAM candidate goodput over frozen Reno, median
LB_RENO_MBPS = 4637.3           # #715 readiness: frozen Reno loopback DATAGRAM goodput, median
LB_EXPOSURE = 0.01              # lateness share of the window that counts as timing exposure


def lb_observation(agg, window_ns, deficit=LB_DEFICIT, reno_mbps=LB_RENO_MBPS):
    """Loopback DATAGRAM, one observation, from the recorder's aggregates (no relay, no fluid link).

    exposure: time observed late after paced deadlines, as a share of the window.
    loss: credit the pacer discarded during that lateness (lateness × pacing rate),
    as a share of what frozen Reno delivers over the window.
    """
    shares = {c: v / window_ns for c, v in zip(agg['state_classes'], agg['state_ns'])}
    shares['emit'] = agg['emit_ns'] / window_ns
    exposure = agg['late_ns'] / window_ns
    loss = agg['late_bytes'] / (reno_mbps * 1e6 / 8 * window_ns / 1e9)
    if loss >= 0.5 * deficit:
        v = 'timing-limited goodput'
    elif loss < 0.25 * deficit:
        v = 'timing exposure' if exposure >= LB_EXPOSURE else 'no timing exposure'
    else:
        v = 'inconclusive'
    return dict(verdict=v, exposure=exposure, loss=loss, deficit=deficit, shares=shares,
                paced_stops_late=agg['late_count'], opportunities=agg['opportunities'])


# ---- Stage 2: one contract-preserving intervention ---------------------------------------------
S2_MIN_BLOCKS = 5
MIX_NEW_CLASS = 0.10            # share of run-loop wakes from a class absent in d0fabc4d and not registered


def aa_range(values):
    return (min(values), max(values)) if values else None


def s2_workload(blocks, registered_classes=()):
    """blocks: per usable block, dict with (all paired against the same block's first d0fabc4d arm 'base'):
      deficit_diff_new, deficit_diff_aa   (new − base, aa − base; deficit = 1 − goodput / frozen Reno)
      late_ratio_new, late_ratio_aa       (None when base lateness is zero: unusable for that metric)
      wakes_ratio_new, wakes_ratio_aa     (pacing wakes per useful GiB)
      cpu_ratio_new, cpu_ratio_aa         (sender CPU per useful GiB)
      wake_classes_new, wake_classes_base ({class: count} of run-loop wakes)
    Returns movement per metric: 'reduction'/'increase' beyond the BBR A/A range, 'inside', or 'unusable'.
    """
    out = dict(blocks=len(blocks))
    for m in ['deficit_diff', 'late_ratio', 'wakes_ratio', 'cpu_ratio']:
        pairs = [(b[m + '_new'], b[m + '_aa']) for b in blocks if b[m + '_new'] is not None and b[m + '_aa'] is not None]
        if len(pairs) < S2_MIN_BLOCKS:
            out[m] = dict(movement='unusable', n=len(pairs))
            continue
        med = statistics.median(p[0] for p in pairs)
        lo, hi = aa_range([p[1] for p in pairs])
        mv = 'reduction' if med < lo else 'increase' if med > hi else 'inside'
        out[m] = dict(movement=mv, median=med, aa_min=lo, aa_max=hi, n=len(pairs))
    new_cls = {}
    for b in blocks:
        for k, v in b['wake_classes_new'].items():
            new_cls[k] = new_cls.get(k, 0) + v
    base_cls = {}
    for b in blocks:
        for k, v in b['wake_classes_base'].items():
            base_cls[k] = base_cls.get(k, 0) + v
    tot_new, tot_base = sum(new_cls.values()) or 1, sum(base_cls.values()) or 1
    unknown = sum(v for k, v in new_cls.items() if base_cls.get(k, 0) / tot_base < 0.01 and k not in registered_classes)
    out['unaccounted_wake_share'] = unknown / tot_new
    return out


def s2_outcome(per_workload, integrity, preservation):
    """keep | negative | null | inconclusive, with reasons. preservation: {cell: 'inside'|'above'|'below'}.

    As #714: Reno on the new revision above the A/A range is negative; below it, an
    otherwise kept change is inconclusive.
    """
    reasons = []
    for wl, w in per_workload.items():
        if w['blocks'] < S2_MIN_BLOCKS or any(w[m]['movement'] == 'unusable' for m in ['deficit_diff', 'late_ratio', 'wakes_ratio', 'cpu_ratio']):
            reasons.append(f'{wl}: fewer than five usable blocks')
        if w['unaccounted_wake_share'] > MIX_NEW_CLASS:
            reasons.append(f'{wl}: changed wake-source mix the registration does not account for')
    if reasons:
        return 'inconclusive', reasons
    if not integrity:
        return 'negative', ['receiver integrity or a clean exit failed']
    above = [c for c, p in preservation.items() if p == 'above']
    below = [c for c, p in preservation.items() if p == 'below']
    if above:
        return 'negative', ['Reno on the new revision above frozen Reno\'s A/A range: ' + ', '.join(above)]
    regress = [f'{wl}: {m}' for wl, w in per_workload.items() for m in ['deficit_diff', 'late_ratio', 'wakes_ratio', 'cpu_ratio']
               if w[m]['movement'] == 'increase']
    if regress:
        return 'negative', ['regressed beyond the BBR A/A range: ' + ', '.join(regress)]
    keep = all(w['deficit_diff']['movement'] == 'reduction' and w['late_ratio']['movement'] == 'reduction'
               for w in per_workload.values())
    if keep and below:
        return 'inconclusive', ['Reno on the new revision below frozen Reno\'s A/A range: ' + ', '.join(below)]
    if keep:
        return 'keep', []
    if all(w[m]['movement'] == 'inside' for w in per_workload.values() for m in ['deficit_diff', 'late_ratio', 'wakes_ratio', 'cpu_ratio']):
        return 'null', []
    return 'inconclusive', ['some metrics moved beyond the BBR A/A range, but not as the keep rule requires in both workloads']


def preservation_cell(renonew, aa):
    """#714's preservation cell: per-block ratios against frozen Reno, pooled over workloads; median inside A/A [min, max]."""
    m = statistics.median(renonew)
    return 'inside' if min(aa) <= m <= max(aa) else ('above' if m > max(aa) else 'below')
