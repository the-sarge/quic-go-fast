#!/usr/bin/env python3
"""#744 registered rules, as pure functions (README "Registration").

Exercised on synthetic data in ctlrules_test.py before any comparative
observation. Every outcome says what a stage found; none sets a readiness flag
or revises #740's verdict.

Conventions:
- p95 is #715's nearest-rank pooled p95 (rules.pooled_p95, copied unchanged);
- intervals are 95% block-bootstrap percentile intervals over the paired usable
  blocks, 2000 resamples, seeded, with #715's index convention
  (boot[int(0.025 R)], boot[int(0.975 R) - 1]);
- a ratio is arm ÷ reference of pooled p95s.
"""
import math
import random
import statistics

from rules import latency_preservation, pooled_p95  # noqa: F401  (#715's, unchanged; re-exported for the stages)

RESAMPLES = 2000
SEED = 744
PER_RUN_MIN = 100        # replies per counted run (#715's per_run_min)
POOLED_MIN = 500         # replies per arm, pooled (#715's pooled_min)
MIN_BLOCKS = 5           # usable paired blocks for any reading other than an evidence gap
NONINCREASE_MARGIN = 1.15
GOODPUT_FLOOR = 0.99     # perturbation: instrumented ÷ plain loopback-long STREAM goodput, median over blocks
P95_TOLERANCE = 1.15     # perturbation: instrumented ÷ plain pooled control p95 within [1/1.15, 1.15]
SHARE_QUEUE = 0.70
SHARE_OTHER = 0.30
SEGMENT_REPORT = 0.30    # a segment is named when its share's lower bound reaches this
RESIDUAL = ('t_req', 't_rep')


def _interval(values):
    v = sorted(values)
    return v[int(0.025 * len(v))], v[int(0.975 * len(v)) - 1]


def _resamples(n, seed=SEED, resamples=RESAMPLES):
    rng = random.Random(seed)
    return [[rng.randrange(n) for _ in range(n)] for _ in range(resamples)]


def _pool(blocks, idx, key):
    return [x for i in idx for x in blocks[i][key]]


# ---- depth reading (plain arms) --------------------------------------------------------------

def depth_reading(blocks, margin=NONINCREASE_MARGIN):
    """D3 Stage 1 depth reading on the plain arms.

    blocks: per block chosen under the plain mask, dict of reply latencies (ms) for 'cand' (r9),
    'cand8' (r9 at depth 8) and 'aa' (the second r9). As in #715's rule, a block with a run under
    100 replies is not counted. Endings, in order: 'depth raises latency', 'non-increase',
    'inconclusive', or 'evidence gap' (fewer than five counted blocks or an arm under 500 pooled
    replies).
    """
    ok = [b for b in blocks if b and all(len(b[k]) >= PER_RUN_MIN for k in ('cand', 'cand8', 'aa'))]
    if len(ok) < MIN_BLOCKS or any(sum(len(b[k]) for b in ok) < POOLED_MIN for k in ('cand', 'cand8', 'aa')):
        return 'evidence gap', dict(usable_blocks=len(ok))
    n = len(ok)
    ratio = lambda idx, a, b: pooled_p95(_pool(ok, idx, a)) / pooled_p95(_pool(ok, idx, b))
    full = list(range(n))
    point, aa_point = ratio(full, 'cand', 'cand8'), ratio(full, 'aa', 'cand')
    boots = _resamples(n)
    lo, hi = _interval(ratio(i, 'cand', 'cand8') for i in boots)
    aa_lo, aa_hi = _interval(ratio(i, 'aa', 'cand') for i in boots)
    per_block = [pooled_p95(b['cand']) / pooled_p95(b['cand8']) for b in ok]
    above = sum(x > 1 for x in per_block)
    detail = dict(ratio=point, ci=[lo, hi], aa_ratio=aa_point, aa_ci=[aa_lo, aa_hi], per_block=per_block, blocks_above_1=above,
                  blocks=n, equivalent_within_margin=hi < margin, margin=margin,
                  p95_ms={k: pooled_p95(_pool(ok, full, k)) for k in ('cand', 'cand8', 'aa')},
                  p50_ms={k: statistics.median(_pool(ok, full, k)) for k in ('cand', 'cand8', 'aa')})
    if lo > 1 and above >= MIN_BLOCKS and point > aa_hi:
        return 'depth raises latency', detail
    if hi < margin:
        return 'non-increase', detail
    return 'inconclusive', detail


# ---- perturbation (counted Stage 1 arms) ------------------------------------------------------

def perturbation(blocks):
    """D3 Stage 0 perturbation, scored on the counted Stage 1 arms, per revision.

    blocks: per block chosen under the recorder mask, dict with, for rev in ('cand', 'cand8'):
      f'{rev}_goodput' and f'{rev}_ctl_goodput' (Mbit/s), f'{rev}' and f'{rev}_ctl' (reply ms).
    Each revision's state is 'pass' (median instrumented ÷ plain goodput at least 0.99 and pooled p95
    ratio within [1/1.15, 1.15]), 'fail' (a measured breach on five or more blocks), or 'insufficient'
    (fewer than five blocks). The overall state is 'fail' if either fails, else 'insufficient' if
    either is insufficient, else 'pass'.
    """
    out = {}
    ok = [b for b in blocks if b]
    for rev in ('cand', 'cand8'):
        if len(ok) < MIN_BLOCKS:
            out[rev] = dict(state='insufficient', blocks=len(ok))
            continue
        g = [b[f'{rev}_ctl_goodput'] / b[f'{rev}_goodput'] for b in ok]
        p = pooled_p95([x for b in ok for x in b[f'{rev}_ctl']]) / pooled_p95([x for b in ok for x in b[rev]])
        gm = statistics.median(g)
        gp, pp = gm >= GOODPUT_FLOOR, 1 / P95_TOLERANCE <= p <= P95_TOLERANCE
        out[rev] = dict(goodput_ratio=gm, goodput_per_block=g, p95_ratio=p, goodput_passes=gp, p95_passes=pp, blocks=len(ok),
                        state='pass' if gp and pp else 'fail')
    states = {out[r]['state'] for r in ('cand', 'cand8')}
    out['state'] = 'fail' if 'fail' in states else 'insufficient' if 'insufficient' in states else 'pass'
    return out


def goodput_only_failure(pert):
    """Operator decision 1 (2026-10-14, before data): the perturbation check failed on the goodput clause alone,
    with five or more blocks and the latency clause passing for both revisions."""
    revs = [pert.get(r) or {} for r in ('cand', 'cand8')]
    return (pert.get('state') == 'fail' and all(r.get('state') in ('pass', 'fail') for r in revs)
            and all(r.get('p95_passes') for r in revs) and any(not r.get('goodput_passes') for r in revs))


# ---- mechanism reading (recorder arms) --------------------------------------------------------

def _midrank_quantiles(values):
    """Quantile rank (i + 0.5) / n of each value, ties sharing their mean rank."""
    order = sorted(range(len(values)), key=values.__getitem__)
    q = [0.0] * len(values)
    i = 0
    while i < len(order):
        j = i
        while j + 1 < len(order) and values[order[j + 1]] == values[order[i]]:
            j += 1
        r = (i + j) / 2
        for k in order[i:j + 1]:
            q[k] = (r + 0.5) / len(values)
        i = j + 1
    return q


def _quantile(sorted_values, q):
    """Linear interpolation at position q * n - 0.5 (the inverse of _midrank_quantiles)."""
    n = len(sorted_values)
    pos = min(max(q * n - 0.5, 0.0), n - 1.0)
    lo = math.floor(pos)
    hi = min(lo + 1, n - 1)
    return sorted_values[lo] + (sorted_values[hi] - sorted_values[lo]) * (pos - lo)


def counterfactual_p95(a_rows, b_rows, segments):
    """p95 of A's totals with each listed segment replaced by B's value at the same quantile rank."""
    totals = [r['total'] for r in a_rows]
    for seg in segments:
        a = [r[seg] for r in a_rows]
        b = sorted(r[seg] for r in b_rows)
        q = _midrank_quantiles(a)
        totals = [t - x + _quantile(b, qi) for t, x, qi in zip(totals, a, q)]
    return pooled_p95(totals)


def _share(a_rows, b_rows, segments):
    pa, pb = pooled_p95([r['total'] for r in a_rows]), pooled_p95([r['total'] for r in b_rows])
    rise = pa - pb
    removed = pa - counterfactual_p95(a_rows, b_rows, segments)
    return rise, (removed / rise if rise > 0 else None)


def _imputed(block, arm, segments, mode, all_segments):
    """An arm's rows plus its incomplete requests, each with its whole fixture latency in one segment:
    the first evaluated segment ('in') or the request's residual ('out')."""
    rows = block[arm]
    missing = block.get(arm + '_missing') or []
    if not missing:
        return rows
    target = segments[0] if mode == 'in' else 't_req'
    fill = []
    for t in missing:
        r = dict.fromkeys(all_segments, 0)
        r[target] = t
        r['total'] = t
        fill.append(r)
    return rows + fill


def share_interval(blocks, segments, boots, all_segments):
    """Share and bootstrap interval under every imputation of incomplete requests.

    Without incomplete requests there is one imputation. With them, each arm's incomplete requests are
    placed wholly in the evaluated segments or wholly outside them (four combinations); the reported
    interval is their envelope (the smallest lower bound and the largest upper bound), so a label holds
    only if no placement of the missing requests could change it.
    """
    missing = any(b.get('a_missing') or b.get('b_missing') for b in blocks)
    combos = [('in', 'in'), ('in', 'out'), ('out', 'in'), ('out', 'out')] if missing else [('out', 'out')]
    per = {}
    for ma, mb in combos:
        bl = [dict(a=_imputed(b, 'a', segments, ma, all_segments), b=_imputed(b, 'b', segments, mb, all_segments)) for b in blocks]
        full = list(range(len(bl)))
        pool = lambda idx, k: [r for i in idx for r in bl[i][k]]
        _, point = _share(pool(full, 'a'), pool(full, 'b'), segments)
        shares = [s for _, s in (_share(pool(i, 'a'), pool(i, 'b'), segments) for i in boots) if s is not None]
        lo, hi = _interval(shares) if shares else (None, None)
        per[f'{ma}/{mb}'] = dict(share=point, ci=[lo, hi], replicates=len(shares))
    los, his = [v['ci'][0] for v in per.values()], [v['ci'][1] for v in per.values()]
    envelope = [None, None] if None in los or None in his else [min(los), max(his)]
    return dict(share=per['out/out']['share'], ci=envelope, replicates=min(v['replicates'] for v in per.values()),
                imputations=per if missing else None)


def tail_means(blocks, segments):
    """Supporting evidence only: each segment's mean over complete requests at or above each arm's p95, A minus B (ns)."""
    out = {}
    for arm in ('a', 'b'):
        rows = [r for b in blocks for r in b[arm]]
        p = pooled_p95([r['total'] for r in rows])
        tail = [r for r in rows if r['total'] >= p]
        out[arm] = {s: statistics.fmean(r[s] for r in tail) for s in segments}
    return {s: out['a'][s] - out['b'][s] for s in segments}


def mechanism_reading(blocks, consistency, prerequisite, segments, queue_segments, min_blocks=MIN_BLOCKS):
    """D3 Stage 1 mechanism reading on the recorder arms.

    blocks: per block chosen under the recorder mask, dict(a=[complete request rows] for r9-ctl,
      b=[rows] for r9-depth8-ctl, a_missing=[fixture latency ns of measured requests without a complete
      chain], b_missing=[...], a_fixture=[reply ms], b_fixture=[reply ms]); rows carry 'total' and every
      segment (ns).
    consistency: (lo, hi) of the plain arms' r9 ÷ depth-8 interval, or None if the depth reading is a gap.
    prerequisite: dict(state='pass' | 'instrumentation gap' | 'evidence gap', reasons=[...]): Stage 0's
      validation (runtime and data-level synthetic cases, bound to the current code) and perturbation.
    queue_segments: {'sender': [...], 'receiver': [...]}.
    min_blocks: five for Stage 1; for the runtime synthetic cases, their registered block count.
    Endings: 'queue wait', 'load or other', 'mixed', 'inconclusive', 'instrumentation gap', 'evidence gap'.
    """
    if prerequisite['state'] != 'pass':
        return prerequisite['state'], dict(reason='Stage 0 prerequisite', prerequisite=prerequisite)
    ok = [b for b in blocks if b]
    if len(ok) < min_blocks or consistency is None:
        return 'evidence gap', dict(usable_blocks=len(ok), consistency=consistency)
    n = len(ok)
    full = list(range(n))
    boots = _resamples(n, seed=SEED + 1)
    fix = lambda idx, k: pooled_p95([x for i in idx for x in ok[i][k]])
    rec_ratio = fix(full, 'a_fixture') / fix(full, 'b_fixture')
    # The rise is on the protected full sample: complete requests' totals plus incomplete requests' fixture latencies.
    tot = lambda idx, k: pooled_p95([r['total'] for i in idx for r in ok[i][k]] + [t for i in idx for t in ok[i].get(k + '_missing') or []])
    rise = tot(full, 'a') - tot(full, 'b')
    rise_lo, rise_hi = _interval(tot(i, 'a') - tot(i, 'b') for i in boots)
    both = queue_segments['sender'] + queue_segments['receiver']
    detail = dict(blocks=n, recorder_ratio=rec_ratio, consistency_interval=list(consistency), rise_ns=rise,
                  rise_ci_ns=[rise_lo, rise_hi], p95_ns=dict(a=tot(full, 'a'), b=tot(full, 'b')),
                  missing=dict(a=sum(len(b.get('a_missing') or []) for b in ok), b=sum(len(b.get('b_missing') or []) for b in ok)))
    if not consistency[0] <= rec_ratio <= consistency[1]:
        return 'inconclusive', dict(detail, reason='recorder arms outside the plain arms\' interval')
    if rise_lo <= 0:
        return 'inconclusive', dict(detail, reason='the p95 rise\'s interval does not exclude zero')
    combined = share_interval(ok, both, boots, segments)
    detail['queue_share'] = combined
    detail['endpoint_shares'] = {side: share_interval(ok, segs, boots, segments) for side, segs in queue_segments.items()}
    detail['segment_shares'] = {s: share_interval(ok, [s], boots, segments) for s in segments}
    detail['tail_mean_difference_ns'] = tail_means(ok, segments)
    detail['named_segments'] = sorted(s for s, v in detail['segment_shares'].items()
                                      if v['ci'][0] is not None and v['ci'][0] >= SEGMENT_REPORT)
    # A rise named only in the residuals is reported as unplaced; that is a qualifier, not a new ending.
    detail['unplaced'] = bool(detail['named_segments']) and set(detail['named_segments']) <= set(RESIDUAL)
    lo, hi = combined['ci']
    if lo is None or combined['replicates'] < 0.975 * RESAMPLES:
        return 'inconclusive', dict(detail, reason='too few bootstrap replicates with a positive rise')
    if lo >= SHARE_QUEUE:
        return 'queue wait', detail
    if hi < SHARE_OTHER:
        return 'load or other', detail
    # Mixed only when both components are shown: the share's interval excludes 0 and 1. Any other
    # interval is unstable.
    if lo > 0 and hi < 1:
        return 'mixed', detail
    return 'inconclusive', dict(detail, reason='unstable share')


def targets(detail):
    """Stage 2 targeting (registered): each endpoint whose own queue-wait share's lower bound reaches 0.30;
    both when neither does on its own."""
    named = [side for side, v in detail['endpoint_shares'].items() if v['ci'][0] is not None and v['ci'][0] >= SEGMENT_REPORT]
    return named or sorted(detail['endpoint_shares'])


# ---- Stage 2: the bound rule ----------------------------------------------------------------------

def stage2_bound(a_rows, b_rows, side, goodput, kappa):
    """The registered bytes-ahead bound at one targeted endpoint, from Stage 1.

    a_rows, b_rows: r9-ctl and r9-depth8-ctl request rows (pooled over the recorder-mask blocks).
    goodput: {'cand': G9, 'cand8': G8}, each plain arm's median loopback-long STREAM goodput in useful
      bytes per second.
    kappa: {'cand': k9, 'cand8': k8}, the endpoint's wire bytes sent per useful byte delivered in each
      recorder arm (its send rate over the measured requests ÷ that arm's goodput; median over blocks).
    The endpoint's send rate in each arm is R = G × k (bytes/s); the time that arm's median bytes ahead
    takes to drain is tau = b / R; T = sqrt(tau8 × tau9), halfway in log terms between the depths; and
    B = T × R9 bytes. B is rounded down to whole entries of the endpoint's median entry size m in r9 and
    clipped to [b8 + m, b9 - m]. An empty interval: 'not attempted'.
    """
    q = {'sender': 'sender_queue', 'receiver': 'receiver_queue'}[side]
    b9 = statistics.median(r[q]['bytes_ahead'] for r in a_rows)
    b8 = statistics.median(r[q]['bytes_ahead'] for r in b_rows)
    m = statistics.median(r[q]['entry_bytes'] for r in a_rows)
    lo, hi = b8 + m, b9 - m
    detail = dict(side=side, b9=b9, b8=b8, entry_bytes=m, interval=[lo, hi], goodput=goodput, kappa=kappa)
    if m <= 0 or lo > hi or min(goodput.values()) <= 0 or min(kappa.values()) <= 0 or b8 <= 0 or b9 <= 0:
        return None, dict(detail, outcome='not attempted')
    r9, r8 = goodput['cand'] * kappa['cand'], goodput['cand8'] * kappa['cand8']
    tau9, tau8 = b9 / r9, b8 / r8
    t = math.sqrt(tau8 * tau9)
    bound = math.floor(t * r9 / m) * m
    bound = min(max(bound, math.ceil(lo / m) * m), math.floor(hi / m) * m)
    if bound < lo or bound > hi:
        return None, dict(detail, outcome='not attempted')
    return bound, dict(detail, rate=dict(cand=r9, cand8=r8), tau_s=dict(cand=tau9, cand8=tau8), time_ahead_s=t, bound_bytes=bound,
                       outcome='attempted')


def common_bound(per_side, target_sides):
    """One bound applies to every BBR queue. With one target, its bound. With two, a bound feasible at both:
    the sender's bound clipped into the intersection of both intervals (the receiver's when the sender's is
    not attempted); an empty intersection, or any targeted side not attempted: 'not attempted'."""
    if any(per_side[s][0] is None for s in target_sides):
        return None, 'not attempted: a targeted endpoint has no bound between the depths'
    if len(target_sides) == 1:
        return per_side[target_sides[0]][0], 'attempted'
    lo = max(per_side[s][1]['interval'][0] for s in target_sides)
    hi = min(per_side[s][1]['interval'][1] for s in target_sides)
    if lo > hi:
        return None, 'not attempted: no common bound is feasible at every targeted endpoint'
    return min(max(per_side['sender'][0], lo), hi), 'attempted'


# ---- Stage 2: efficacy, guards and the outcome -----------------------------------------------------

def stage2_null_margin(depth_ci_lo):
    """The registered null margin: the remedy is null only if shown to recover less than half the smallest
    plausible depth increment (the depth interval's lower bound), and never on a margin wider than 1.15."""
    return min(NONINCREASE_MARGIN, 1 + 0.5 * (depth_ci_lo - 1))


def _range_movement(pairs, n_min=MIN_BLOCKS):
    """pairs: (new statistic, BBR A/A statistic) per usable block, both against the same block's r9.
    Median of the new statistic against the A/A [min, max] (#738's _movement)."""
    if len(pairs) < n_min:
        return dict(movement='unusable', n=len(pairs))
    med = statistics.median(p[0] for p in pairs)
    lo, hi = min(p[1] for p in pairs), max(p[1] for p in pairs)
    return dict(movement='lower' if med < lo else 'higher' if med > hi else 'inside', median=med, aa_min=lo, aa_max=hi, n=len(pairs))


def stage2_latency(blocks, margin):
    """loopback-long: pooled p95 of the new revision ÷ r9, with the r9 A/A ratio.

    blocks: per usable block, dict(cand=[ms], aa=[ms], new=[ms]); margin: stage2_null_margin(...).
    Returns (efficacy, detail): 'effective' (interval upper bound below 1 and the point below the A/A
    interval's lower bound), 'partial' (exactly one of those), 'demonstrated worsening' (lower bound above
    1), 'null' (the lower bound at least 1/margin: not lower than r9 by more than the margin), otherwise
    'inconclusive'; 'evidence gap' as in depth_reading.
    """
    ok = [b for b in blocks if b and all(len(b[k]) >= PER_RUN_MIN for k in ('cand', 'aa', 'new'))]
    if len(ok) < MIN_BLOCKS or any(sum(len(b[k]) for b in ok) < POOLED_MIN for k in ('cand', 'aa', 'new')):
        return 'evidence gap', dict(usable_blocks=len(ok))
    n = len(ok)
    ratio = lambda idx, a, b: pooled_p95(_pool(ok, idx, a)) / pooled_p95(_pool(ok, idx, b))
    full = list(range(n))
    boots = _resamples(n, seed=SEED + 2)
    point, aa_point = ratio(full, 'new', 'cand'), ratio(full, 'aa', 'cand')
    lo, hi = _interval(ratio(i, 'new', 'cand') for i in boots)
    aa_lo, aa_hi = _interval(ratio(i, 'aa', 'cand') for i in boots)
    detail = dict(ratio=point, ci=[lo, hi], aa_ratio=aa_point, aa_ci=[aa_lo, aa_hi], blocks=n, margin=margin)
    c1, c2 = hi < 1, point < aa_lo
    if c1 and c2:
        return 'effective', detail
    if c1 != c2:
        return 'partial', detail
    if lo > 1:
        return 'demonstrated worsening', detail
    if lo >= 1 / margin:
        return 'null', detail
    return 'inconclusive', detail


WORKLOADS = ('stream', 'datagram')
ENDPOINTS = ('send', 'receive')
STATUS = ('not attempted', 'unbuildable', 'gates failed', 'inert', 'mistargeted')


def guard_states(loopback, integrity, preservation, screens):
    """Each guard as 'pass', 'regression' or 'insufficient', failing closed on missing or invalid evidence.

    loopback: {workload: dict(goodput=_range_movement(...), cpu=_range_movement(...))} for both workloads.
    integrity: True, False, or None when an observation could not be read.
    preservation: {endpoint: 'inside' | 'above' | 'below' | 'insufficient'} for both endpoints (#738's
      preservation_cell; anything outside frozen Reno's A/A range fails it, as #714 and #738 read it).
    screens: {workload: dict(reading='no regression' | 'regression' | 'inconclusive')} for both workloads.
    """
    g = {}
    for wl in WORKLOADS:
        w = (loopback or {}).get(wl) or {}
        for key, bad in (('goodput', 'lower'), ('cpu', 'higher')):
            mv = (w.get(key) or {}).get('movement')
            g[f'{wl} {key}'] = ('regression' if mv == bad else 'pass' if mv in ('inside', 'lower', 'higher') else 'insufficient')
        s = ((screens or {}).get(wl) or {}).get('reading')
        g[f'{wl} S5 screen'] = {'regression': 'regression', 'no regression': 'pass'}.get(s, 'insufficient')
    for ep in ENDPOINTS:
        p = (preservation or {}).get(ep)
        g[f'Reno preservation {ep}'] = 'pass' if p == 'inside' else 'regression' if p in ('above', 'below') else 'insufficient'
    g['integrity'] = 'pass' if integrity is True else 'regression' if integrity is False else 'insufficient'
    return g


def stage2_outcome(status, efficacy, guards):
    """D3 Stage 2 outcome, classified in order; returns (outcome, disposes_class, reasons).

    status: None for a usable treatment (activated, correctly targeted, gates passed, built), else one
      of STATUS.
    efficacy: stage2_latency's ending ('evidence gap' becomes 'inconclusive').
    guards: guard_states(...).
    """
    if status is not None:
        return (status, False, [status]) if status in STATUS else ('inconclusive', False, [f'invalid status {status!r}'])
    if efficacy in ('null', 'demonstrated worsening'):
        return efficacy, True, []
    if efficacy == 'evidence gap':
        return 'inconclusive', False, ['efficacy: evidence gap']
    if efficacy in ('partial', 'inconclusive'):
        return efficacy, False, []
    if efficacy != 'effective':
        return 'inconclusive', False, [f'invalid efficacy {efficacy!r}']
    regressions = sorted(k for k, v in guards.items() if v == 'regression')
    insufficient = sorted(k for k, v in guards.items() if v not in ('pass', 'regression'))
    expected = {f'{wl} {k}' for wl in WORKLOADS for k in ('goodput', 'cpu', 'S5 screen')} | {f'Reno preservation {ep}' for ep in ENDPOINTS} | {'integrity'}
    insufficient += sorted(f'{k}: missing' for k in expected - set(guards))
    if regressions:
        # A demonstrated regression disposes of the class even if another guard is insufficient; both are recorded.
        return 'effective, guard-failed', True, regressions + [f'also insufficient: {k}' for k in insufficient]
    if insufficient:
        return 'inconclusive', False, insufficient
    return 'kept', False, []
