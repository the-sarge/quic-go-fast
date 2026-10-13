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
SEGMENT_REPORT = 0.30    # a placed segment is named when its share's lower bound reaches this


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

    blocks: per usable block (all six arms usable and uncontaminated), dict of reply
    latencies (ms) for 'cand' (r9), 'cand8' (r9 at depth 8) and 'aa' (the second r9).
    Endings, in order: 'depth raises latency', 'non-increase', 'inconclusive', or
    'evidence gap' (fewer than five usable blocks, a run under 100 replies or an arm
    under 500 pooled replies).
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

    blocks: per usable block, dict with, for rev in ('cand', 'cand8'):
      f'{rev}_goodput' and f'{rev}_ctl_goodput' (Mbit/s), f'{rev}' and f'{rev}_ctl' (reply ms).
    Passes when, for both revisions, the median instrumented ÷ plain goodput is at least
    0.99 and the pooled p95 ratio lies within [1/1.15, 1.15].
    """
    out = {}
    ok = [b for b in blocks if b]
    for rev in ('cand', 'cand8'):
        if len(ok) < MIN_BLOCKS:
            out[rev] = dict(passes=False, reason='fewer than five usable blocks', blocks=len(ok))
            continue
        g = [b[f'{rev}_ctl_goodput'] / b[f'{rev}_goodput'] for b in ok]
        p = pooled_p95([x for b in ok for x in b[f'{rev}_ctl']]) / pooled_p95([x for b in ok for x in b[rev]])
        gm = statistics.median(g)
        out[rev] = dict(goodput_ratio=gm, goodput_per_block=g, p95_ratio=p, goodput_passes=gm >= GOODPUT_FLOOR,
                        p95_passes=1 / P95_TOLERANCE <= p <= P95_TOLERANCE, blocks=len(ok))
        out[rev]['passes'] = out[rev]['goodput_passes'] and out[rev]['p95_passes']
    out['passes'] = all(out[r]['passes'] for r in ('cand', 'cand8'))
    return out


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


def share_interval(blocks, segments, boots):
    """Point share and its bootstrap interval over replicates whose p95 rise is positive."""
    full = list(range(len(blocks)))
    pool = lambda idx, k: [r for i in idx for r in blocks[i][k]]
    rise, point = _share(pool(full, 'a'), pool(full, 'b'), segments)
    shares = []
    for idx in boots:
        r, s = _share(pool(idx, 'a'), pool(idx, 'b'), segments)
        if s is not None:
            shares.append(s)
    lo, hi = _interval(shares) if shares else (None, None)
    return dict(share=point, ci=[lo, hi], replicates=len(shares))


def tail_means(blocks, segments):
    """Supporting evidence only: each segment's mean over requests at or above each arm's p95, A minus B (ns)."""
    out = {}
    for arm in ('a', 'b'):
        rows = [r for b in blocks for r in b[arm]]
        p = pooled_p95([r['total'] for r in rows])
        tail = [r for r in rows if r['total'] >= p]
        out[arm] = {s: statistics.fmean(r[s] for r in tail) for s in segments}
    return {s: out['a'][s] - out['b'][s] for s in segments}


def mechanism_reading(blocks, consistency, perturbation_passes, segments, queue_segments, min_blocks=MIN_BLOCKS):
    """D3 Stage 1 mechanism reading on the recorder arms.

    blocks: per usable paired block, dict(a=[request rows] for r9-ctl, b=[request rows] for r9-depth8-ctl,
      a_fixture=[reply ms], b_fixture=[reply ms]); rows carry 'total' and every segment (ns).
    consistency: (lo, hi) of the plain arms' r9 ÷ depth-8 interval, or None if the depth reading is a gap.
    perturbation_passes: the Stage 0 perturbation result (both revisions).
    queue_segments: {'sender': [...], 'receiver': [...]}.
    min_blocks: five for Stage 1; three for the runtime synthetic cases (README "Synthetic cases").
    Endings: 'queue wait', 'load or other', 'mixed', 'inconclusive', 'instrumentation gap', 'evidence gap'.
    """
    if not perturbation_passes:
        return 'instrumentation gap', dict(reason='perturbation failed')
    ok = [b for b in blocks if b]
    if len(ok) < min_blocks or consistency is None:
        return 'evidence gap', dict(usable_blocks=len(ok), consistency=consistency)
    n = len(ok)
    full = list(range(n))
    boots = _resamples(n, seed=SEED + 1)
    fix = lambda idx, k: pooled_p95([x for i in idx for x in ok[i][k]])
    rec_ratio = fix(full, 'a_fixture') / fix(full, 'b_fixture')
    tot = lambda idx, k: pooled_p95([r['total'] for i in idx for r in ok[i][k]])
    rise = tot(full, 'a') - tot(full, 'b')
    rise_lo, rise_hi = _interval(tot(i, 'a') - tot(i, 'b') for i in boots)
    both = queue_segments['sender'] + queue_segments['receiver']
    detail = dict(blocks=n, recorder_ratio=rec_ratio, consistency_interval=list(consistency), rise_ns=rise,
                  rise_ci_ns=[rise_lo, rise_hi], p95_ns=dict(a=tot(full, 'a'), b=tot(full, 'b')))
    if not consistency[0] <= rec_ratio <= consistency[1]:
        return 'inconclusive', dict(detail, reason='recorder arms outside the plain arms\' interval')
    if rise_lo <= 0:
        return 'inconclusive', dict(detail, reason='the p95 rise\'s interval does not exclude zero')
    combined = share_interval(ok, both, boots)
    detail['queue_share'] = combined
    detail['endpoint_shares'] = {side: share_interval(ok, segs, boots) for side, segs in queue_segments.items()}
    detail['segment_shares'] = {s: share_interval(ok, [s], boots) for s in segments}
    detail['tail_mean_difference_ns'] = tail_means(ok, segments)
    detail['named_segments'] = sorted(s for s, v in detail['segment_shares'].items()
                                      if v['ci'][0] is not None and v['ci'][0] >= SEGMENT_REPORT)
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


# ---- Stage 2: the bound rule, the keep rule and the outcomes ----------------------------------

MAX_PACKET = 1400  # the fixture's packet size (InitialPacketSize 1400, PMTUD disabled)


def stage2_bound(a_rows, b_rows, side):
    """The registered bytes-ahead bound at one targeted endpoint, from Stage 1's recorder arms.

    a_rows: r9-ctl request rows; b_rows: r9-depth8-ctl rows. W9, W8: median queue wait at that endpoint's
    control enqueues; D: r9's drain rate there (median of bytes ahead ÷ queue wait over requests with
    bytes ahead > 0). T = sqrt(W8 × W9) (time ahead halfway, in log terms, between the two depths) and
    B = T × D, rounded down to whole maximum-size packets and clipped to [depth 8's median bytes ahead
    + one packet, r9's median bytes ahead − one packet]. An empty interval means no bound fits between the
    two depths: 'not attempted'.
    """
    seg, q = {'sender': ('s_queue', 'sender_queue'), 'receiver': ('r_queue', 'receiver_queue')}[side]
    w9 = statistics.median(r[seg] for r in a_rows)
    w8 = statistics.median(r[seg] for r in b_rows)
    rates = [r[q]['bytes_ahead'] / r[seg] for r in a_rows if r[q]['bytes_ahead'] > 0 and r[seg] > 0]
    b9 = statistics.median(r[q]['bytes_ahead'] for r in a_rows)
    b8 = statistics.median(r[q]['bytes_ahead'] for r in b_rows)
    lo, hi = b8 + MAX_PACKET, b9 - MAX_PACKET
    detail = dict(side=side, w9_ns=w9, w8_ns=w8, b9=b9, b8=b8, interval=[lo, hi], drain_bytes_per_ns=None)
    if not rates or w8 <= 0 or w9 <= 0 or lo > hi:
        return None, dict(detail, outcome='not attempted')
    d = statistics.median(rates)
    t = math.sqrt(w8 * w9)
    bound = (int(t * d) // MAX_PACKET) * MAX_PACKET
    bound = min(max(bound, math.ceil(lo / MAX_PACKET) * MAX_PACKET), (hi // MAX_PACKET) * MAX_PACKET)
    if bound < lo or bound > hi:
        return None, dict(detail, outcome='not attempted')
    return bound, dict(detail, drain_bytes_per_ns=d, time_ahead_ns=t, bound_bytes=bound, bound_packets=bound // MAX_PACKET)


def _range_movement(pairs, n_min=MIN_BLOCKS):
    """pairs: (new statistic, BBR A/A statistic) per usable block, both against the same block's r9.
    Median of the new statistic against the A/A [min, max] (#738's _movement)."""
    if len(pairs) < n_min:
        return dict(movement='unusable', n=len(pairs))
    med = statistics.median(p[0] for p in pairs)
    lo, hi = min(p[1] for p in pairs), max(p[1] for p in pairs)
    return dict(movement='lower' if med < lo else 'higher' if med > hi else 'inside', median=med, aa_min=lo, aa_max=hi, n=len(pairs))


def stage2_latency(blocks, margin=NONINCREASE_MARGIN):
    """loopback-long: pooled p95 of the new revision ÷ r9, with the r9 A/A ratio.

    blocks: per usable block, dict(cand=[ms], aa=[ms], new=[ms]).
    Returns (efficacy, detail): 'effective' (interval upper bound below 1 and the point below the A/A
    interval's lower bound), 'partial' (exactly one of those), 'demonstrated worsening' (lower bound above
    1), 'null' (the interval's lower bound is at or above 1/margin: not lower by more than the margin),
    otherwise 'inconclusive'; 'evidence gap' as in depth_reading.
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


def stage2_outcome(status, efficacy, loopback, integrity, preservation, screens):
    """D3 Stage 2 outcome, classified in order; returns (outcome, disposes_class, reasons).

    status: None for a usable treatment, else 'not attempted', 'unbuildable', 'gates failed', 'inert' or
      'mistargeted'.
    efficacy: stage2_latency's ending.
    loopback: {workload: dict(goodput=_range_movement(...), cpu=_range_movement(...))} (new ÷ r9 against
      the BBR A/A, five of six usable blocks).
    integrity: every Stage 2 observation exited cleanly with receiver integrity.
    preservation: {cell: 'inside' | 'above' | 'below'} for Reno on the new revision (#738's preservation_cell).
    screens: {workload: dict(reading='regression' | 'no regression' | 'inconclusive')} (#738's s5_screen).
    """
    if status:
        return status, False, [status]
    if efficacy in ('null', 'demonstrated worsening'):
        return efficacy, True, []
    if efficacy != 'effective':
        return efficacy, False, []
    reasons, insufficient = [], []
    for wl, w in loopback.items():
        if w['goodput']['movement'] == 'unusable' or w['cpu']['movement'] == 'unusable':
            insufficient.append(f'{wl}: fewer than five usable loopback blocks')
        if w['goodput']['movement'] == 'lower':
            reasons.append(f'{wl}: loopback goodput lower than r9 beyond the BBR A/A range')
        if w['cpu']['movement'] == 'higher':
            reasons.append(f'{wl}: sender CPU per useful GiB above the BBR A/A maximum')
    if not integrity:
        reasons.append('receiver integrity or a clean exit failed')
    reasons += [f'Reno on the new revision: {c} {p}' for c, p in preservation.items() if p != 'inside']
    for wl, s in screens.items():
        if s['reading'] == 'regression':
            reasons.append(f'{wl}: S5 screen regression')
        elif s['reading'] == 'inconclusive':
            insufficient.append(f'{wl}: S5 screen unusable')
    if reasons:
        return 'effective, guard-failed', True, reasons
    if insufficient:
        return 'inconclusive', False, insufficient
    return 'kept', False, []
