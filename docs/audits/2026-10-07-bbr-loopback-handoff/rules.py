#!/usr/bin/env python3
"""#738 registered rules, as pure functions.

Registered in README.md ("Registration") and exercised on synthetic cases in
rules_test.py before any counted observation existed. Every outcome says what a
stage found or whether a registered prediction held; none sets a readiness flag.
"""
import statistics

# ---- contamination and unusable reruns (the operator's #714 rule, as #715 registered it) -------


def choose_block(attempts):
    """attempts: [(label, usable, contaminated)] in run order, the original first.

    A block holding an unusable or contaminated observation is rerun once; the rerun
    replaces the original only when it is usable and uncontaminated. Returns (label,
    status) with status 'clean', 'contaminated' or 'unusable'.
    """
    original = attempts[0]
    if original[1] and not original[2]:
        return original[0], 'clean'
    for label, usable, contaminated in attempts[1:2]:
        if usable and not contaminated:
            return label, 'clean'
    return original[0], 'unusable' if not original[1] else 'contaminated'


# ---- the hand-off recorder's states ------------------------------------------------------------
SATURATED = 0.90       # a stage busy for at least this share of the window is saturated
LOCAL_MIN = 0.25       # the connection blocked in local waits (credit or queue) for at least this share
WINDOW_MIN = 0.25      # ... or in congestion-window and paced (model) waits for at least this share
SUPPLY_MIN = 0.25      # ... or waiting for the application's data (supply exhausted) for at least this share
EXPOSURE_MIN = 0.01    # lateness after paced stops, as a share of the window, that counts as timing exposure
WINDOW_TOLERANCE = 0.01
BLOCKED_TOLERANCE = 0.05   # the blocked-time estimate (one interval in eight) may exceed the window by this much
SAMPLED_MIN = 100          # sampled blocked intervals needed for a reason with at least 800 blocked intervals
BLOCKED_KEYS = ['credit', 'queue', 'cwnd', 'paced', 'app', 'hard', 'other']


def blocked_estimate(h):
    """Blocked time by reason: the sampled intervals' mean duration times the reason's interval count."""
    sb, sn = h['sampled_blocks'], h['sampled_blocked_ns']
    return {k: (sn[k] / sb[k] * h['blocks'][k] if sb.get(k, 0) > 0 else 0.0) for k in h['blocks']}


def hand_usable(h, measure_ms):
    """Whether one send.hand.json is usable, and why not."""
    if h is None:
        return False, 'no recorder output'
    w = h['window_ns']
    if abs(w - measure_ms * 1e6) > WINDOW_TOLERANCE * measure_ms * 1e6:
        return False, 'window length'
    blocked = sum(blocked_estimate(h).values())
    if blocked > w * (1 + BLOCKED_TOLERANCE) or h['worker_busy_ns'] > w * (1 + WINDOW_TOLERANCE) or h['worker_busy_ns'] < 0:
        return False, 'accounting exceeds the window'
    for k, n in h['blocks'].items():
        if n >= SAMPLED_MIN * 8 and h['sampled_blocks'].get(k, 0) < SAMPLED_MIN:
            return False, f'too few sampled {k} blocks'
    for k, n in h['stops'].items():
        if n >= SAMPLED_MIN * 8 and h['state_samples'].get(k, 0) < SAMPLED_MIN:
            return False, f'too few sampled {k} exits'
    if h['emit_samples'] < SAMPLED_MIN:
        return False, 'too few sampled opportunities'
    if sum(h['worker_busy_in_sampled_blocked_ns'].values()) > sum(h['sampled_blocked_ns'].values()) + 1:
        return False, 'overlap exceeds blocked time'
    if h['opportunities'] == 0 or h['entries'] == 0:
        return False, 'no opportunities or entries recorded'
    return True, ''


def hand_measures(h):
    """Shares of the window W from one send.hand.json.

    U_w: send worker busy. b[r]: the connection blocked in the run-loop select after a
    stop of class r. L: blocked in local waits (credit refusal or full queue). U_c: the
    connection not blocked (running). Joint occupancy: J_wb (local wait, worker busy:
    waiting on a working worker), J_hh (local wait, worker idle: neither stage working),
    J_ov (both working), J_lb (connection working, worker idle). The worker's busy share
    inside blocked time of each reason is measured on one blocked interval in eight
    and applied to all of that reason's blocked time; blocked time itself is
    estimated from the same sample (blocked_estimate).
    """
    w = h['window_ns']
    est = blocked_estimate(h)
    b = {k: est.get(k, 0) / w for k in BLOCKED_KEYS}
    sb, so = h['sampled_blocked_ns'], h['worker_busy_in_sampled_blocked_ns']
    ov = {k: b[k] * (so.get(k, 0) / sb[k] if sb.get(k, 0) > 0 else 0.0) for k in BLOCKED_KEYS}
    u_w = h['worker_busy_ns'] / w
    local = b['credit'] + b['queue']
    u_c = 1 - sum(b.values())
    j_wb = ov['credit'] + ov['queue']
    j_ov = max(0.0, u_w - sum(ov.values()))
    waits = h['waits']
    m = dict(u_w=u_w, u_c=u_c, local=local, credit=b['credit'], queue=b['queue'], window=b['cwnd'] + b['paced'],
             cwnd=b['cwnd'], paced=b['paced'], app=b['app'], blocked=b, j_wb=j_wb, j_hh=local - j_wb, j_ov=j_ov, j_lb=u_c - j_ov,
             exposure=h['late_ns'] / w, paced_stops=h['stops'].get('paced', 0), late_count=h['late_count'],
             states={k: (h['state_sampled_ns'][k] / n * h['stops'][k] / w if (n := h['state_samples'].get(k, 0)) else 0.0)
                     for k in h['stops']},
             emit=h['emit_sampled_ns'] / max(1, h['emit_samples']) * h['opportunities'] / w,
             entries=h['entries'], bytes_per_entry=h['entry_bytes'] / max(1, h['entries']),
             packets_per_entry=h['entry_packets'] / max(1, h['entries']),
             max_pending_q=h['max_pending_q'], admissions_over_2q=h['admissions_over_2q'], refusals=h['refusals'],
             engaged=h['admissions_over_2q'] > 0)
    for kind in ['credit', 'queue']:
        a = waits[kind]
        s = a['sampled_blocked'] - a['sampled_early_wake']  # sampled waits decomposed by their signal
        m[f'{kind}_waits'] = a['waits']
        m[f'{kind}_wait_mean_ns'] = a['sampled_total_ns'] / a['sampled'] if a['sampled'] else None
        m[f'{kind}_wait_share'] = m[f'{kind}_wait_mean_ns'] * a['waits'] / w if a['sampled'] else 0.0
        m[f'{kind}_signal_mean_ns'] = a['sampled_signal_ns'] / s if s > 0 else None
        m[f'{kind}_release_mean_ns'] = a['sampled_release_ns'] / s if s > 0 else None
        m[f'{kind}_wake_mean_ns'] = a['sampled_wake_ns'] / s if s > 0 else None
    m['local_waits_per_s'] = (waits['credit']['waits'] + waits['queue']['waits']) / (w / 1e9)
    g = h['sampled_groups']
    m['submit_mean_ns'] = h['sampled_submit_ns'] / g if g else None
    return m


def conditions(m):
    """The Stage 1 states (README, "Stage 1 rule") that hold in one observation."""
    c = set()
    if m['u_w'] >= SATURATED:
        c.add('worker-bound')
    if m['u_c'] >= SATURATED and m['local'] < LOCAL_MIN:
        c.add('loop-bound')
    if m['local'] >= LOCAL_MIN and m['u_w'] < SATURATED and m['u_c'] < SATURATED:
        c.add('hand-off')
    if m['window'] >= WINDOW_MIN:
        c.add('window-bound')
    if m['app'] >= SUPPLY_MIN:
        c.add('supply-bound')
    return c


def verdict(m):
    c = conditions(m)
    if len(c) == 1:
        return next(iter(c))
    if len(c) > 1:
        return 'mixed: ' + ' + '.join(sorted(c))
    return 'inconclusive'


def handoff_kind(m):
    return 'credit' if m['credit'] >= m['queue'] else 'queue'


# ---- Stage 0: instrument preflight -------------------------------------------------------------
PERTURBATION_MIN = 0.99      # instrumented goodput over the plain build, median per-block ratio
IDLE_WAIT_MIN = 0.25         # delayed local signal: local wait with the worker idle (j_hh), as a share of the window


def recovery(case, m, params):
    """Whether one synthetic case is recovered in one workload. params: build.py SYNTH[case]."""
    v = verdict(m)
    c = conditions(m)
    out = dict(case=case, verdict=v, conditions=sorted(c))
    if case == 'slowworker':
        ok = v == 'worker-bound' and m['submit_mean_ns'] is not None and m['submit_mean_ns'] >= params['synSlowWorkerNS']
    elif case == 'busyloop':
        ok = v == 'loop-bound'
    elif case == 'delaysignal':
        # The withheld signal leaves the loop waiting on local capacity while the worker, having
        # drained the queue, idles: neither stage works (j_hh).
        out['j_hh'] = m['j_hh']
        ok = v == 'hand-off' and m['j_hh'] >= IDLE_WAIT_MIN
    elif case == 'cwnd':
        ok = v == 'window-bound'
    elif case == 'mixed':
        ok = c == {'hand-off', 'window-bound'}
    else:
        raise ValueError(case)
    return dict(out, recovered=bool(ok))


def perturbation(ratios):
    """Per workload: instrumented ÷ plain goodput per block; passes when the median is at least 0.99."""
    med = statistics.median(ratios)
    return dict(ratios=ratios, median=med, passed=med >= PERTURBATION_MIN)


def activation(h):
    """The 4Q arm's activation preflight (excluded run): engaged when an admission took pending above 2Q."""
    return dict(engaged=h['admissions_over_2q'] > 0, max_pending_q=h['max_pending_q'], admissions_over_2q=h['admissions_over_2q'])


# ---- Stage 1: discriminate the loopback limit --------------------------------------------------
MAJORITY = 3


def majority(verdicts):
    """verdicts: per usable block (at most four). The verdict shared by at least three usable blocks;
    fewer than three usable is an evidence gap; otherwise inconclusive."""
    if len(verdicts) < MAJORITY:
        return 'evidence gap'
    for v in set(verdicts):
        if verdicts.count(v) >= MAJORITY:
            return v
    return 'inconclusive'


def aa_range(values):
    return (min(values), max(values)) if values else None


def credit_response(blocks, gates_passed, activated):
    """The 4Q arm's reading in one workload.

    blocks: per usable block, dict(ratio=4Q ÷ r8 goodput, aa=second r8 ÷ r8 goodput,
    cpu_ratio=4Q ÷ r8 sender CPU per useful GiB, cpu_aa=second r8 ÷ r8, engaged=bool).
    unusable: the gates failed. inert: the activation preflight did not engage, or fewer
    than three usable blocks engaged. credit-responsive: goodput above the BBR A/A
    maximum in at least three usable blocks and in the median. Otherwise not
    credit-responsive (with the direction reported).
    """
    out = dict(blocks=len(blocks))
    if not gates_passed:
        return dict(out, reading='unusable')
    if not activated:
        return dict(out, reading='inert', why='activation preflight did not take pending above 2Q')
    engaged = [b for b in blocks if b['engaged']]
    if len(engaged) < MAJORITY:
        return dict(out, reading='inert', why=f'{len(engaged)} usable blocks engaged')
    if len(blocks) < MAJORITY:
        return dict(out, reading='evidence gap')
    lo, hi = aa_range([b['aa'] for b in blocks])
    clo, chi = aa_range([b['cpu_aa'] for b in blocks])
    med = statistics.median(b['ratio'] for b in blocks)
    above = sum(b['ratio'] > hi for b in blocks)
    out.update(median=med, aa_min=lo, aa_max=hi, above_aa=above, cpu_median=statistics.median(b['cpu_ratio'] for b in blocks),
               cpu_aa_min=clo, cpu_aa_max=chi)
    if med > hi and above >= MAJORITY:
        return dict(out, reading='credit-responsive')
    direction = 'below the A/A range' if med < lo else 'inside the A/A range' if med <= hi else 'above the A/A range in fewer than three blocks'
    return dict(out, reading='not credit-responsive', direction=direction)


def kick_reading(blocks):
    """Loopback STREAM: did r8 lower goodput against d0fabc4d, and could its kick explain it?

    blocks: per usable block, dict(ratio=d0fabc4d ÷ r8 goodput, aa=second r8 ÷ r8 goodput,
    exposure=r8's lateness share of the window, paced_stops=r8's paced stops).
    """
    if len(blocks) < MAJORITY:
        return dict(reading='evidence gap')
    lo, hi = aa_range([b['aa'] for b in blocks])
    med = statistics.median(b['ratio'] for b in blocks)
    above = sum(b['ratio'] > hi for b in blocks)
    exposed = sum(b['exposure'] >= EXPOSURE_MIN for b in blocks)
    out = dict(median=med, aa_min=lo, aa_max=hi, above_aa=above, exposed_blocks=exposed,
               exposure_median=statistics.median(b['exposure'] for b in blocks),
               paced_stops_median=statistics.median(b['paced_stops'] for b in blocks))
    lowered = med > hi and above >= MAJORITY
    if lowered and exposed >= MAJORITY:
        return dict(out, reading='kick', why='r8 below d0fabc4d beyond the A/A range, with timing exposure')
    if lowered:
        return dict(out, reading='r8 lower, kick not supported', why='no timing exposure for the kick to act on')
    if exposed >= MAJORITY:
        return dict(out, reading='no r8 effect detected', why='timing exposure without a goodput difference beyond the A/A range')
    return dict(out, reading='no r8 effect detected')


def addressable(workload_verdict):
    """Stage 2 runs for a workload whose Stage 1 verdict includes the hand-off state."""
    return workload_verdict == 'hand-off' or (workload_verdict.startswith('mixed: ') and 'hand-off' in workload_verdict)


# ---- Stage 2: one contract-preserving hand-off change ------------------------------------------
S2_MIN_BLOCKS = 5
S5_MIN_BLOCKS = 3


def _movement(pairs, n_min):
    """pairs: (new statistic, BBR A/A statistic) per usable block. Median against the A/A range."""
    if len(pairs) < n_min:
        return dict(movement='unusable', n=len(pairs))
    med = statistics.median(p[0] for p in pairs)
    lo, hi = aa_range([p[1] for p in pairs])
    mv = 'lower' if med < lo else 'higher' if med > hi else 'inside'
    return dict(movement=mv, median=med, aa_min=lo, aa_max=hi, n=len(pairs))


def s2_workload(blocks):
    """Loopback, one workload. blocks: per usable block (all paired against the same block's first r8 arm):
      deficit_diff_new, deficit_diff_aa   (new − r8, second r8 − r8; deficit = 1 − goodput ÷ frozen Reno)
      cpu_ratio_new, cpu_ratio_aa         (sender CPU per useful GiB, ÷ r8)
    """
    return dict(blocks=len(blocks),
                deficit=_movement([(b['deficit_diff_new'], b['deficit_diff_aa']) for b in blocks], S2_MIN_BLOCKS),
                cpu=_movement([(b['cpu_ratio_new'], b['cpu_ratio_aa']) for b in blocks], S2_MIN_BLOCKS))


def s5_screen(blocks):
    """S5, one workload. blocks: per usable block, goodput_ratio_new/aa and cpu_ratio_new/aa (÷ r8).

    usable: at least three usable blocks, else inconclusive. regression: goodput below the
    A/A range or sender CPU per useful GiB above it.
    """
    if len(blocks) < S5_MIN_BLOCKS:
        return dict(reading='inconclusive', blocks=len(blocks))
    g = _movement([(b['goodput_ratio_new'], b['goodput_ratio_aa']) for b in blocks], S5_MIN_BLOCKS)
    c = _movement([(b['cpu_ratio_new'], b['cpu_ratio_aa']) for b in blocks], S5_MIN_BLOCKS)
    reg = g['movement'] == 'lower' or c['movement'] == 'higher'
    return dict(reading='regression' if reg else 'no regression', goodput=g, cpu=c, blocks=len(blocks))


def preservation_cell(renonew, aa):
    """#714's preservation cell: per-block ratios against frozen Reno, pooled over workloads; median inside A/A [min, max]."""
    m = statistics.median(renonew)
    return 'inside' if min(aa) <= m <= max(aa) else ('above' if m > max(aa) else 'below')


def efficacy(w):
    """Per workload, separately from the keep outcome: did the deficit move beyond the BBR A/A range?"""
    mv = w['deficit']['movement']
    return {'lower': 'improved', 'higher': 'worse', 'inside': 'unchanged', 'unusable': 'unusable'}[mv]


def s2_outcome(per_workload, targeted, integrity, preservation, screens):
    """keep | negative | null | inconclusive, with reasons.

    per_workload: {workload: s2_workload(...)}; targeted: workloads Stage 1 attributed to the
    hand-off; integrity: every observation exited cleanly with receiver integrity;
    preservation: {cell: preservation_cell(...)} for Reno on the new revision at each endpoint;
    screens: {workload: s5_screen(...)}.
    """
    reasons = []
    for wl, w in per_workload.items():
        if w['blocks'] < S2_MIN_BLOCKS or w['deficit']['movement'] == 'unusable' or w['cpu']['movement'] == 'unusable':
            reasons.append(f'{wl}: fewer than five usable loopback blocks')
    for wl, s in screens.items():
        if s['reading'] == 'inconclusive':
            reasons.append(f'{wl}: S5 screen has fewer than three usable blocks')
    if reasons:
        return 'inconclusive', reasons
    # Guards first: any regression is negative whatever the efficacy.
    if not integrity:
        return 'negative', ['receiver integrity or a clean exit failed']
    above = [c for c, p in preservation.items() if p != 'inside']
    if above:
        return 'negative', ['Reno on the new revision fails #714\'s preservation metric: ' + ', '.join(above)]
    regress = [f'{wl}: sender CPU per useful GiB above the BBR A/A maximum' for wl, w in per_workload.items()
               if w['cpu']['movement'] == 'higher']
    regress += [f'{wl}: deficit worse beyond the BBR A/A range' for wl, w in per_workload.items()
                if w['deficit']['movement'] == 'higher']
    regress += [f'{wl}: S5 screen regression' for wl, s in screens.items() if s['reading'] == 'regression']
    if regress:
        return 'negative', regress
    improved = [wl for wl in targeted if per_workload[wl]['deficit']['movement'] == 'lower']
    if targeted and len(improved) == len(targeted):
        return 'keep', []
    if not improved and all(w['deficit']['movement'] == 'inside' for w in per_workload.values()):
        return 'null', []
    return 'inconclusive', ['partial efficacy: ' + (', '.join(improved) or 'no targeted workload') + ' improved beyond the BBR A/A range '
                            'of targeted ' + ', '.join(targeted)]
