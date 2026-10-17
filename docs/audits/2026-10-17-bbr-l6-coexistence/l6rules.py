#!/usr/bin/env python3
"""#777 registered rules, as pure functions.

The classification is the operator's D2, accepted before any data (README.md,
"Accepted decisions"); the estimator details below are this record's
registration. They were written and exercised on synthetic cases
(l6rules_test.py) before any calibration, development or counted observation of
this record existed. Nothing here reads an observation directory: analyze.py
loads summaries and receipts and calls these functions.
"""
import statistics

# ---- D2: the fairness classification ---------------------------------------------
THRESHOLD = 0.70                               # operator's choice (D2)
CLASSIFIED = ['equal', 'short', 'resumed']     # measured t = 0-100 s, 100-200 s, 270-300 s
WORKLOADS = ['stream', 'datagram']
PAIRS = 5
DECIDING = 4                                   # pairs on one side of the threshold that decide a period
# A matched-Reno competitor delivery below this in a period leaves the pair's
# ratio for that period not computable (5% of the 100 Mbit/s bottleneck); the
# absolute difference is still reported, as the acceptance plan asks for a
# too-small denominator.
DENOMINATOR_FLOOR_MBPS = 5.0

# ---- review flags beside the classification (D2), never changing it ----------------
COMPETITOR_CONTROL_LIMIT = 1.20                # acceptance plan's p95 control-latency tolerance
FOCAL_LIMITS = {                               # unchanged readiness limits, against matched Reno
    'control_p95': 1.20,
    'send_cpu_per_gib': 1.10, 'receive_cpu_per_gib': 1.10,
    'send_peak_rss': 1.10, 'receive_peak_rss': 1.10,
}

# ---- observation usability (fixture health, never an outcome) ----------------------
PAUSE_EXPECTED_S = (240.0, 270.0)
PAUSE_TOLERANCE_S = 0.5
RELAY_LAG_LIMIT_MS = 5.0                       # Q1's native gateway gate, applied to arrival lag and lateness
RELAY_LATE_LIMIT_MS = 5.0
SATURATION = 0.80                              # an endpoint's mean CPU over its allotted cores
ALLOTTED_CORES = {'focal': 4, 'competitor': 2}
CONTAMINATION_MEAN, CONTAMINATION_PEAK = 0.10, 0.50   # #712's test, foreign cores
USER_HZ = 100


def pair_ratio(numerator, denominator):
    """Competitor delivery with the focal arm over the same with matched Reno, in one period."""
    if numerator is None or denominator is None or denominator < DENOMINATOR_FLOOR_MBPS:
        return None
    return numerator / denominator


def classify_period(ratios):
    """One classified period of one workload: one ratio per pair, None when not computable.

    Acceptable when at least four pairs are at or above the threshold; adverse
    when at least four are below it; otherwise inconclusive, including when
    fewer than four pairs are usable on either side.
    """
    assert len(ratios) == PAIRS, ratios
    usable = [r for r in ratios if r is not None]
    if sum(r < THRESHOLD for r in usable) >= DECIDING:
        return 'adverse'
    if sum(r >= THRESHOLD for r in usable) >= DECIDING:
        return 'acceptable'
    return 'inconclusive'


def overall(cells):
    """cells: {(workload, period): outcome} for every workload and classified period."""
    assert set(cells) == {(w, p) for w in WORKLOADS for p in CLASSIFIED}, sorted(cells)
    outcomes = set(cells.values())
    if 'adverse' in outcomes:
        return 'adverse'
    if outcomes == {'acceptable'}:
        return 'acceptable'
    return 'inconclusive'


def median_flag(ratios, limit):
    """Readiness-style review flag: the median of the usable per-pair ratios above the limit."""
    usable = [r for r in ratios if r is not None]
    if len(usable) < 3:
        return dict(median=None, raised=None, crossing=None, pairs=len(usable))
    med = statistics.median(usable)
    return dict(median=med, raised=med > limit, crossing=sum(r > limit for r in usable), pairs=len(usable))


def ratio(a, b):
    return None if a is None or b is None or b == 0 else a / b


# ---- usability, contamination, reruns ---------------------------------------------
def health(summary, receipt, wall_s):
    """Fixture-health reasons an observation is unusable (empty when usable)."""
    reasons = []
    if receipt['exit_codes'] != [0] * len(receipt['exit_codes']) or receipt['relay_exit'] != 0:
        reasons.append('process exit')
    if summary is None:
        return reasons + ['summary failed']
    pause = summary['competitor']['bulk_pause_observed_s']
    if len(pause) != 2 or any(abs(p - e) > PAUSE_TOLERANCE_S for p, e in zip(pause, PAUSE_EXPECTED_S)):
        reasons.append(f'competitor pause {pause}')
    relay = summary['relay']
    if relay['max_arrival_lag_ms'] > RELAY_LAG_LIMIT_MS:
        reasons.append(f"relay arrival lag {relay['max_arrival_lag_ms']:.3f} ms")
    for d in ['forward', 'reverse']:
        if relay[d]['max_late_ms'] > RELAY_LATE_LIMIT_MS:
            reasons.append(f"relay {d} lateness {relay[d]['max_late_ms']:.3f} ms")
    for who in ['focal', 'competitor']:
        p = summary[who]
        if p['control_error']:
            reasons.append(f"{who} control error {p['control_error']}")
        for role in ['send', 'receive']:
            load = p[role]['total_cpu_seconds'] / wall_s / ALLOTTED_CORES[who]
            if load >= SATURATION:
                reasons.append(f'{who} {role} CPU {load:.2f} of its cores')
    return reasons


def contamination(samples, window, fixture_cpus, sibling_cpus):
    """#712's contamination test, on this record's fixture cores and their SMT siblings.

    samples: host-cpu.json rows (1 s); window: [start_ns, end_ns]. Foreign CPU is the busy
    time on those CPUs minus the fixture processes' own CPU time, in cores.
    """
    sel = [s for s in samples if window[0] <= s['unix_ns'] <= window[1]]
    cpus = fixture_cpus + sibling_cpus
    per, sib = [], []
    for a, b in zip(sel, sel[1:]):
        dt = (b['unix_ns'] - a['unix_ns']) / 1e9
        busy = sum(b['cpus'][str(c)]['busy'] - a['cpus'][str(c)]['busy'] for c in cpus) / USER_HZ
        own = sum((b['fixture_ticks'][r] or a['fixture_ticks'][r]) - a['fixture_ticks'][r]
                  for r in a['fixture_ticks'] if a['fixture_ticks'][r] is not None) / USER_HZ
        per.append(max(0.0, (busy - own) / dt))
        sib.append(sum(b['cpus'][str(c)]['busy'] - a['cpus'][str(c)]['busy'] for c in sibling_cpus) / USER_HZ / dt)
    if not per:
        return dict(contaminated=None, reason='no samples in window')
    mean, peak = statistics.fmean(per), max(per)
    return dict(foreign_mean_cores=mean, foreign_max_cores=peak, sibling_mean_cores=statistics.fmean(sib), samples=len(per),
                contaminated=mean > CONTAMINATION_MEAN or peak > CONTAMINATION_PEAK)


def choose_triple(attempts):
    """attempts: [(label, usable, contaminated)] in run order, the original first.

    The operator's #714 rule, applied to a pair-workload triple (the three arms
    of one pair and workload): a triple holding an unusable or contaminated
    observation is rerun once, all three arms, with the same seed; the rerun
    replaces the original only when every arm is usable and uncontaminated.
    Returns (label, status) with status 'clean', 'contaminated' or 'unusable'.
    """
    original = attempts[0]
    if original[1] and not original[2]:
        return original[0], 'clean'
    for label, usable, contaminated in attempts[1:2]:
        if usable and not contaminated:
            return label, 'clean'
    return original[0], 'unusable' if not original[1] else 'contaminated'


# ---- the analysis -----------------------------------------------------------------
def analyze(triples):
    """triples: {(workload, pair): {'reno': summary, 'cand': summary, 'aa': summary} or None}.

    A missing triple (unusable after its rerun) contributes None in every
    period. Returns the per-cell ratios, the classification, the review flags
    and descriptive context.
    """
    from run import PERIODS  # the measured periods are the runner's
    out = dict(cells={}, periods={}, competitor_control={}, focal_flags={}, threshold=THRESHOLD)
    for w in WORKLOADS:
        for period in PERIODS:
            rows = []
            for pair in range(1, PAIRS + 1):
                t = triples.get((w, pair))
                comp = {arm: (t[arm]['competitor']['periods'][period]['mbps'] if t else None) for arm in ['reno', 'cand', 'aa']}
                focal = {arm: (t[arm]['focal']['periods'][period]['mbps'] if t else None) for arm in ['reno', 'cand', 'aa']}
                cp95 = {arm: (t[arm]['competitor']['control_periods']['periods'][period]['p95_ms'] if t else None) for arm in ['reno', 'cand', 'aa']}
                rows.append(dict(pair=pair, competitor_mbps=comp, focal_mbps=focal,
                                 ratio=pair_ratio(comp['cand'], comp['reno']), aa_ratio=pair_ratio(comp['aa'], comp['reno']),
                                 difference_mbps=None if comp['cand'] is None or comp['reno'] is None else comp['cand'] - comp['reno'],
                                 competitor_control_p95_ms=cp95, competitor_control_ratio=ratio(cp95['cand'], cp95['reno']),
                                 competitor_control_aa_ratio=ratio(cp95['aa'], cp95['reno'])))
            out['periods'][f'{w}/{period}'] = rows
            out['competitor_control'][f'{w}/{period}'] = median_flag([r['competitor_control_ratio'] for r in rows], COMPETITOR_CONTROL_LIMIT)
            if period in CLASSIFIED:
                out['cells'][f'{w}/{period}'] = classify_period([r['ratio'] for r in rows])
        for measure, limit in FOCAL_LIMITS.items():
            ratios, aa = [], []
            for pair in range(1, PAIRS + 1):
                t = triples.get((w, pair))
                value = (lambda arm: focal_measure(t[arm]['focal'], measure)) if t else (lambda arm: None)
                ratios.append(ratio(value('cand'), value('reno')))
                aa.append(ratio(value('aa'), value('reno')))
            out['focal_flags'][f'{w}/{measure}'] = dict(median_flag(ratios, limit), limit=limit, ratios=ratios, aa_ratios=aa)
    out['classification'] = overall({tuple(k.split('/')): v for k, v in out['cells'].items()})
    return out


def focal_measure(p, measure):
    if measure == 'control_p95':
        return p['control_p95_ms']
    role, rest = measure.split('_', 1)
    return p[role]['cpu_seconds_per_gib'] if rest == 'cpu_per_gib' else p[role]['peak_rss_mib']
