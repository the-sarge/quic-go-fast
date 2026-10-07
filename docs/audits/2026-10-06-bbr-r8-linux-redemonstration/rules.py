#!/usr/bin/env python3
"""#715 registered rules for the attribution stages, as pure functions.

Registered in README.md ("Registration") and exercised on synthetic cases in
rules_test.py before any observation of these stages existed. The readiness
flag rule, #712's contamination test and #711's memory and S6-latency rules
are not here: they run unchanged from analyze.py, localize.py and
attribution.py. Every outcome below says where something is or whether a
registered prediction held; none sets a readiness flag.
"""
import math
import random
import re
import statistics

# ---- contamination reruns -------------------------------------------------------


def choose_block(attempts):
    """attempts: [(label, usable, contaminated)] in run order, the original first.

    The operator's #714 rule, prospective here: a block holding an unusable or
    contaminated observation is rerun once with the same seed; the rerun
    replaces the original only when it is usable and uncontaminated.
    Returns (label, status) with status 'clean', 'contaminated' or 'unusable'.
    """
    original = attempts[0]
    if original[1] and not original[2]:
        return original[0], 'clean'
    for label, usable, contaminated in attempts[1:2]:
        if usable and not contaminated:
            return label, 'clean'
    return original[0], 'unusable' if not original[1] else 'contaminated'


# ---- loopback bottleneck ---------------------------------------------------------
# Goroutine stages by the frames of a sample's user stack (leaf first). The
# first matching rule wins; anything else is runtime or system work.
STAGES = [
    ('conn_loop', re.compile(r'quic-go\.\(\*Conn\)\.run$')),
    ('send_worker', re.compile(r'quic-go\.\(\*sendQueue\)\.Run$')),
    ('read_loop', re.compile(r'quic-go\.\(\*Transport\)\.listen$')),
    ('transport_send', re.compile(r'quic-go\.\(\*Transport\)\.runSendQueue$')),
    ('app', re.compile(r'^main\.')),
]
SERIAL = {'send': ['conn_loop', 'send_worker'], 'receive': ['read_loop', 'conn_loop']}
SATURATED, UNSATURATED = 0.85, 0.70


def stage_of(frames):
    user = [f for f in frames if not f.startswith('k:')]
    for name, rx in STAGES:
        if any(rx.search(f) for f in user):
            return name
    return 'runtime'


def stage_utilization(samples_by_stage, process_cpu_s, window_s):
    """Cores used by each goroutine stage: its share of the process's samples times the process's CPU in the window."""
    total = sum(samples_by_stage.values())
    if total <= 0 or window_s <= 0:
        return {}
    return {k: v / total * process_cpu_s / window_s for k, v in samples_by_stage.items()}


def side_state(util, role):
    """'saturated' if a serial stage of this endpoint is at or above 0.85 cores; 'unsaturated' if all are below 0.70."""
    vals = [util.get(s, 0.0) for s in SERIAL[role]]
    if max(vals) >= SATURATED:
        return 'saturated'
    if max(vals) < UNSATURATED:
        return 'unsaturated'
    return 'ambiguous'


def majority(states, n_min=3):
    """The state shared by at least n_min blocks (of four), else 'ambiguous'."""
    for s in ('saturated', 'unsaturated'):
        if sum(x == s for x in states) >= n_min:
            return s
    return 'ambiguous'


def bottleneck_verdict(arms, consistency):
    """arms: {arm: {'send': [state per block], 'receive': [...]}} for the BBR arms compared (prev, cand).

    consistency: (median cand/prev sender cycles per packet, median cand/prev goodput,
    blocks with cand/prev goodput above 1, blocks). Returns one of
    'sender per-packet work', 'receiver-side limit', 'neither endpoint saturated',
    'inconclusive' and the reason.
    """
    sides = {a: (majority(v['send']), majority(v['receive'])) for a, v in arms.items()}
    kinds = set(sides.values())
    if len(kinds) != 1:
        return 'inconclusive', f'arms disagree: {sides}'
    send, receive = kinds.pop()
    if send == 'saturated' and receive == 'unsaturated':
        cyc, gp, above, n = consistency
        if cyc < 0.98 and not (gp > 1.0 and above >= n - 1):
            return 'inconclusive', f'conflict: sender work per packet fell ({cyc:.3f}) but goodput did not rise ({gp:.3f}, {above}/{n})'
        return 'sender per-packet work', f'sender serial stage saturated, receiver not; cycles/packet {cyc:.3f}, goodput {gp:.3f}'
    if receive == 'saturated' and send == 'unsaturated':
        return 'receiver-side limit', 'receiver serial stage saturated, sender not'
    if send == 'unsaturated' and receive == 'unsaturated':
        return 'neither endpoint saturated', 'no serial stage reaches 0.70 cores at either endpoint'
    return 'inconclusive', f'sender {send}, receiver {receive}'


# ---- S5 goodput timeline ---------------------------------------------------------
CAPACITY_BPS = 100e6  # wire bits per second at the modeled bottleneck (IP packet bytes)
IP_UDP = 28


def timeline_decomposition(buckets, model_rows, window, overflow_packets, wire_packet_bytes, queue):
    """Attribute the bottleneck's unused capacity over the measured window, as fractions of capacity.

    buckets: [(t_ns, state_ns dict, bytes, packets, late_sum_ns)] from the emission
    overlay (10 ms); model_rows: [(t_ns, pacer rate in bytes/s)], sample-and-hold;
    window: (start_ns, end_ns); overflow_packets: bottleneck drops in the window;
    queue: [(t_ns, forward queue bytes)] from the relay, every 10 ms.

    The link is a fluid server: in a bucket it idles for capacity minus (the
    queue at the bucket's start plus the bytes sent in it), never below zero. A
    queue at the start lowers what the sender needed to send to keep the link
    busy. Each bucket's idle capacity is split in order: first what the model did
    not ask for (the needed rate above the model's rate, or above the actual send
    rate when the window governed), then what sender timing lost (pacing lateness
    after a full-quantum deadline plus local credit waits, at the model's rate,
    clamped to the needed rate), and the remainder is unexplained.
    Returns dict(deficit, idle, model, timing, unexplained, loss, coverage_s, queue_missing).
    """
    w0, w1 = window
    span = 10_000_000
    sel = [b for b in buckets if w0 <= b[0] < w1]
    if not sel or not model_rows:
        return None
    sent_b = sum(b[2] for b in sel)
    sent_p = sum(b[3] for b in sel)
    if sent_p == 0:
        return None
    wire_ratio = (sent_b + IP_UDP * sent_p) / sent_b
    cap = CAPACITY_BPS / 8  # bytes per second, wire
    rows = sorted(model_rows)
    q = sorted(queue)
    qi, j, rate = 0, 0, rows[0][1]
    model = timing = unexplained = 0.0
    missing = 0
    for t, states, nbytes, npk, late in sel:
        while j < len(rows) and rows[j][0] <= t:
            rate = rows[j][1]
            j += 1
        while qi + 1 < len(q) and abs(q[qi + 1][0] - t) <= abs(q[qi][0] - t):
            qi += 1
        if not q or abs(q[qi][0] - t) > span:
            missing += 1
            continue
        need = max(0.0, cap - q[qi][1] / (span / 1e9))  # rate the sender needed to keep the link busy
        r = rate * wire_ratio
        sent_rate = (nbytes + IP_UDP * npk) / (span / 1e9)
        short = max(0.0, need - sent_rate)  # idle capacity in this bucket, as a rate
        total_state = sum(states.values()) or span
        r_model = sent_rate if states.get('cwnd_ns', 0) / total_state >= 0.5 else r
        m = min(short, max(0.0, need - r_model))
        f = min(1.0, (late + states.get('credit_ns', 0)) / span)
        lost = min(r, need) - min(r * (1 - f), need)
        tm = min(short - m, lost)
        model += m
        timing += tm
        unexplained += short - m - tm
    n = len(sel)
    dur = n * span / 1e9
    delivered = (sent_b + IP_UDP * sent_p - overflow_packets * wire_packet_bytes) / dur
    M, T, U = model / n / cap, timing / n / cap, unexplained / n / cap
    return dict(deficit=max(0.0, 1 - delivered / cap), idle=M + T + U, model=M, timing=T, unexplained=U,
                loss=overflow_packets * wire_packet_bytes / (cap * dur), coverage_s=n * span / 1e9, queue_missing=missing / n)


def timeline_verdict(d, min_coverage_s=28.0, min_deficit=0.02):
    """Per observation: 'model', 'timing', 'mixed', 'unexplained', 'no deficit' or 'unusable'.

    Unusable: under 28 s of coverage, more than 5% of buckets without a queue sample,
    or an attributed idle share that disagrees with the delivery deficit by more than
    half the deficit plus 0.01 (the attribution would not describe the deficit).
    """
    if d is None or d['coverage_s'] < min_coverage_s or d['queue_missing'] > 0.05:
        return 'unusable'
    D = d['deficit']
    if D < min_deficit:
        return 'no deficit'
    if abs(d['idle'] - D) > 0.5 * D + 0.01:
        return 'unusable'
    S, M, T = d['idle'], d['model'], d['timing']
    if M >= 0.5 * S and T < 0.25 * S:
        return 'model'
    if T >= 0.5 * S and M < 0.25 * S:
        return 'timing'
    if M >= 0.25 * S and T >= 0.25 * S:
        return 'mixed'
    return 'unexplained'


def workload_verdict(verdicts, n_min=3, usable_min=3):
    usable = [v for v in verdicts if v != 'unusable']
    if len(usable) < usable_min:
        return 'evidence gap'
    for v in set(usable):
        if sum(x == v for x in usable) >= n_min:
            return v
    return 'inconclusive'


def stage_verdict(per_workload):
    """Both workloads must agree on 'model' or 'timing'; anything else is reported per workload."""
    vals = set(per_workload.values())
    if vals == {'model'}:
        return 'model behaviour'
    if vals == {'timing'}:
        return 'sender timing'
    if 'evidence gap' in vals:
        return 'evidence gap'
    return 'inconclusive'


def mac_comparison(linux, mac):
    """Same-or-different only: the per-workload verdicts match, or they do not."""
    return {w: ('same' if linux.get(w) == mac.get(w) else 'different') for w in mac}


# ---- receiver CPU split ----------------------------------------------------------


def interpolate_ticks(samples, at_ns):
    """Linear interpolation of cumulative CPU ticks at at_ns from [(unix_ns, ticks)]."""
    pts = [(t, v) for t, v in samples if v is not None]
    if not pts:
        return None
    if at_ns <= pts[0][0]:
        return pts[0][1] * at_ns / pts[0][0] if pts[0][0] else pts[0][1]
    for (t0, v0), (t1, v1) in zip(pts, pts[1:]):
        if t0 <= at_ns <= t1:
            return v0 + (v1 - v0) * (at_ns - t0) / (t1 - t0) if t1 > t0 else v1
    return pts[-1][1]


def cpu_split(samples, start_ns, window, total_cpu_s, hz=100):
    """Seconds of CPU before, inside and after the measured window. Ticks are counted from process start."""
    w0, w1 = window
    a, b = interpolate_ticks(samples, w0), interpolate_ticks(samples, w1)
    if a is None or b is None:
        return None
    before, inside = a / hz, (b - a) / hz
    return dict(before=before, inside=inside, after=max(0.0, total_cpu_s - before - inside))


def receiver_localization(blocks, limit=1.10):
    """blocks: per block, dict(cand=split, reno=split, cand_gib, reno_gib).

    'inside window' when the window's CPU-per-GiB ratio alone crosses the limit;
    'outside window' when it does not and at least half of the excess CPU seconds
    per GiB lie outside the window; 'mixed' otherwise. Medians over blocks.
    """
    win, out_share = [], []
    for b in blocks:
        c, r = b['cand'], b['reno']
        cw, rw = c['inside'] / b['cand_gib'], r['inside'] / b['reno_gib']
        co, ro = (c['before'] + c['after']) / b['cand_gib'], (r['before'] + r['after']) / b['reno_gib']
        win.append(cw / rw)
        ex_in, ex_out = cw - rw, co - ro
        pos = max(ex_in, 0) + max(ex_out, 0)
        out_share.append(max(ex_out, 0) / pos if pos > 0 else 0.0)
    w, s = statistics.median(win), statistics.median(out_share)
    if w > limit:
        outcome = 'inside window'
    elif s >= 0.5:
        outcome = 'outside window'
    else:
        outcome = 'mixed'
    return dict(window_ratio=w, outside_share=s, outcome=outcome, window_ratios=win, outside_shares=out_share)


# ---- preservation: RSS -------------------------------------------------------------


def rss_preservation(blocks, high_threshold_mib, limit=1.10, usable_min=4):
    """D3's RSS rule for one Reno-on-candidate cell.

    blocks: per block, dict(cand_rss, reno_rss, cand_retained, reno_retained,
    site_excess={group: MiB}) or None if unusable. Retained is in-use heap at the
    measured-window heap peak. High-memory outcomes are runs whose peak RSS
    exceeds high_threshold_mib. Never passes on range membership.
    Returns (outcome, detail); outcomes 'retention excess', 'RSS excess without
    retained heap', 'not reproduced', 'inconclusive', 'evidence gap'.
    """
    ok = [b for b in blocks if b]
    if len(ok) < usable_min:
        return 'evidence gap', f'{len(ok)} usable blocks'
    rss = [b['cand_rss'] / b['reno_rss'] for b in ok]
    ret = [b['cand_retained'] / b['reno_retained'] for b in ok]
    high_c = sum(b['cand_rss'] > high_threshold_mib for b in ok)
    high_r = sum(b['reno_rss'] > high_threshold_mib for b in ok)
    m_rss, m_ret = statistics.median(rss), statistics.median(ret)
    n = len(ok)
    detail = dict(rss_ratio=m_rss, retained_ratio=m_ret, rss_ratios=rss, retained_ratios=ret, high_cand=high_c, high_reno=high_r,
                  blocks=n)
    if m_ret > limit and sum(x > 1 for x in ret) >= n - 1:
        groups = {}
        for b in ok:
            for g, v in b['site_excess'].items():
                groups[g] = groups.get(g, 0) + v
        pos = sum(v for v in groups.values() if v > 0)
        lead = max(groups, key=groups.get) if pos > 0 else None
        detail.update(site_groups=groups, lead=lead, lead_share=groups[lead] / pos if lead else 0)
        return 'retention excess', detail
    if (m_rss > limit or high_c - high_r >= 2) and m_ret <= limit:
        return 'RSS excess without retained heap', detail
    if m_rss <= limit and high_c - high_r <= 1 and m_ret <= limit:
        return 'not reproduced', detail
    return 'inconclusive', detail


# ---- preservation: latency ---------------------------------------------------------


def pooled_p95(values):
    v = sorted(values)
    return v[math.ceil(len(v) * .95) - 1] if v else None


def latency_preservation(blocks, limit=1.20, per_run_min=100, pooled_min=500, resamples=2000, seed=715):
    """D3's latency rule for one Reno-on-candidate cell.

    blocks: per block, dict(cand=[reply ms], reno=[reply ms]) or None if unusable.
    Adequacy: at least per_run_min replies in every counted run and pooled_min per
    arm, with at least four usable blocks; otherwise an evidence gap. The ratio is
    pooled p95 (arm / frozen Reno) with a 95% block-bootstrap interval.
    Returns (outcome, detail): 'regression reproduced', 'not reproduced',
    'inconclusive' or 'evidence gap'. Never passes on range membership.
    """
    ok = [b for b in blocks if b and len(b['cand']) >= per_run_min and len(b['reno']) >= per_run_min]
    if len(ok) < 4 or sum(len(b['cand']) for b in ok) < pooled_min or sum(len(b['reno']) for b in ok) < pooled_min:
        return 'evidence gap', dict(usable_blocks=len(ok))
    def ratio(sel):
        return pooled_p95([x for b in sel for x in b['cand']]) / pooled_p95([x for b in sel for x in b['reno']])
    point = ratio(ok)
    rng = random.Random(seed)
    boot = sorted(ratio([rng.choice(ok) for _ in ok]) for _ in range(resamples))
    lo, hi = boot[int(0.025 * resamples)], boot[int(0.975 * resamples) - 1]
    per_block = [pooled_p95(b['cand']) / pooled_p95(b['reno']) for b in ok]
    above = sum(x > 1 for x in per_block)
    detail = dict(ratio=point, ci=[lo, hi], per_block=per_block, blocks=len(ok),
                  cand_p95_ms=pooled_p95([x for b in ok for x in b['cand']]), reno_p95_ms=pooled_p95([x for b in ok for x in b['reno']]),
                  cand_p50_ms=statistics.median([x for b in ok for x in b['cand']]),
                  reno_p50_ms=statistics.median([x for b in ok for x in b['reno']]))
    if point > limit and lo > 1.0 and above >= len(ok) - 2:
        return 'regression reproduced', detail
    if hi <= limit:
        return 'not reproduced', detail
    return 'inconclusive', detail
