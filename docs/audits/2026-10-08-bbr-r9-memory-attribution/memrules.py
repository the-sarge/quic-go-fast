#!/usr/bin/env python3
"""#740 registered rules (D6 of the #737 decision), as pure functions.

memrules_test.py exercises every rule, the synthetic accounting cases, the
labels and the consideration's counterexamples, and is committed with the
registration before any comparative data.

Accounting (Stage 0). One memory sample is one line of an endpoint's
{role}.mem.jsonl (mem/mem_series.go). Resident memory is smaps_rollup `Rss`.
It is partitioned, at one sample, into:
  file      Rss - Anonymous (file-backed and shared pages: the binary's text and data)
  objects   /memory/classes/heap/objects
  unused    /memory/classes/heap/unused
  free      /memory/classes/heap/free
  stacks    /memory/classes/heap/stacks + /memory/classes/os-stacks
  metadata  the four mcache/mspan classes + /memory/classes/metadata/other
  other     /memory/classes/other + /memory/classes/profiling/buckets
  residual  Anonymous - (those six runtime classes)
The runtime classes are mapped, not necessarily resident; heap/released is
excluded because it is returned to the OS. The residual is resident anonymous
memory the runtime accounting does not explain (negative when mapped runtime
memory is not resident). The readiness cell's peak (ru_maxrss from the
endpoint's own getrusage, `peak_rss_mib`) is the largest sampled Rss plus a
signed sampling gap (peak minus that sample). The gap is an instrument
discrepancy, mostly negative and larger than kB rounding in the synthetic runs
(README, "Accounting rule"); its kernel mechanism is not established. So
  peak = file + objects + unused + free + stacks + metadata + other + residual + gap
holds exactly at the peak sample (`closure`). Closure is bookkeeping: it does
not show that the classes explain residency at another moment, so the
unexplained test charges |residual| and |gap| separately and they cannot cancel.

Heap reading. Heap objects at one sample include garbage not yet collected,
which moves between the objects and free classes with the GC cycle: under the
fixture's default GOGC=100 a live-heap cause L leaves between L and 2L of heap
objects while it raises resident heap by about 2L, so its objects share of the
excess averages about 0.75, close to D6's 0.70. D6's condition "at least 70%
of the excess in heap objects", aligned to the window and to GC cycles, is read
as the median over the measured window's per-second samples of the
heap-objects excess (objects window share). Two GC-aligned conditions are
added: 2 x the live-heap excess at the last completed mark (/gc/heap/live) must
reach 0.70 of E at the peak sample and as the window median. The doubling is a
model and never suffices alone (a 0.70 doubled-live share admits an actual live
excess of only 0.35 E); the measured objects share has to back it. On the
synthetic 8 MiB retained case this reading localizes three of three blocks in
the committed set and two of three in the previous set (the literal peak-sample
reading, one of three), so a true live-heap cause can still end inconclusive,
and an inconclusive ending is not evidence against live heap.

Peak-instrument sensitivity (third consideration, F4). The primary rule uses
the readiness instrument (ru_maxrss) for E, the A/A variation and the
removals. A sensitivity pass repeats the rule with each arm's largest valid
sampled Rss instead; a causal, localized or mixed conclusion that the
sensitivity pass does not reproduce ends as unresolved (combine_sensitivity).
"""
import json
import statistics

KIB = 1 / 1024  # kB -> MiB
MIB = 1 / 2**20

CLASSES = ['file', 'objects', 'unused', 'free', 'stacks', 'metadata', 'other']
UNEXPLAINED = ['residual', 'gap']
REQUIRED = ['rollup_Rss_kb', 'rollup_Anonymous_kb', 'status_VmHWM_kb', '/memory/classes/heap/objects:bytes',
            '/memory/classes/heap/unused:bytes', '/memory/classes/heap/free:bytes', '/memory/classes/heap/stacks:bytes',
            '/memory/classes/os-stacks:bytes', '/memory/classes/metadata/mcache/free:bytes',
            '/memory/classes/metadata/mcache/inuse:bytes', '/memory/classes/metadata/mspan/free:bytes',
            '/memory/classes/metadata/mspan/inuse:bytes', '/memory/classes/metadata/other:bytes',
            '/memory/classes/other:bytes', '/memory/classes/profiling/buckets:bytes', '/memory/classes/heap/released:bytes',
            '/gc/heap/live:bytes', '/gc/heap/goal:bytes', '/gc/cycles/total:gc-cycles']

# Registered thresholds.
DETECTABLE_MIB = 0.5        # block usability for fractions, and the ring preflight (README, departure C)
UNEXPLAINED_SHARE = 0.25    # (|residual| + |gap|) excess above this share of E makes a block unexplained
LABEL = 0.70                # removal (beyond A/A) for a causal label; heap shares for localization
BOUNDED = 0.30              # removal upper bound for localization; a ring lower bound this large makes a cell mixed
MIN_BLOCKS = 3
WINDOW_COVERAGE = 0.80      # share of the measured window's seconds with a valid sample in both runs
COMPARABLE_FLOOR = 0.01     # relative floor of the comparability band
OVERFLOW_FLOOR = 0.0005     # absolute floor (fraction of forward packets) for overflow
CALIBRATION_MIN = 3         # clean A/A pairs needed to calibrate comparability
# D2's Up-policy thresholds (#715, #736).
UP_SHARE, CRUISE_MS, S5_P95 = 0.70, 1.0, 1.20
PHASES = ['startup', 'drain', 'down', 'cruise', 'refill', 'up', 'probertt']
UP, DOWN = PHASES.index('up'), PHASES.index('down')
BOTTLENECK_BPS = 100e6


# ---- Stage 0: accounting ---------------------------------------------------------------------

def parse_series(text):
    """(header, rows as dicts, tail) from one {role}.mem.jsonl."""
    lines = [json.loads(x) for x in text.splitlines() if x.strip()]
    header = lines[0]
    rows = [dict(zip(header['columns'], r)) for r in lines[1:] if isinstance(r, list)]
    tails = [r for r in lines[1:] if isinstance(r, dict)]
    return header, rows, (tails[-1] if tails else None)


def valid(row):
    """Every value the accounting needs was read (the sampler writes -1 for a failed read)."""
    return all(k in row and row[k] is not None and row[k] >= 0 for k in REQUIRED)


def account(row):
    """One sample, partitioned in MiB (see the module docstring)."""
    m = lambda k: row[k] * MIB
    rss = row['rollup_Rss_kb'] * KIB
    anon = row['rollup_Anonymous_kb'] * KIB
    out = dict(rss=rss, file=rss - anon,
               objects=m('/memory/classes/heap/objects:bytes'), unused=m('/memory/classes/heap/unused:bytes'),
               free=m('/memory/classes/heap/free:bytes'),
               stacks=m('/memory/classes/heap/stacks:bytes') + m('/memory/classes/os-stacks:bytes'),
               metadata=sum(m(f'/memory/classes/metadata/{k}:bytes') for k in ['mcache/free', 'mcache/inuse', 'mspan/free', 'mspan/inuse', 'other']),
               other=m('/memory/classes/other:bytes') + m('/memory/classes/profiling/buckets:bytes'),
               released=m('/memory/classes/heap/released:bytes'), live=m('/gc/heap/live:bytes'), goal=m('/gc/heap/goal:bytes'),
               gc_cycles=row['/gc/cycles/total:gc-cycles'], hwm=row['status_VmHWM_kb'] * KIB)
    out['residual'] = anon - sum(out[k] for k in ['objects', 'unused', 'free', 'stacks', 'metadata', 'other'])
    for k in ['ring_bytes', 'delivery_slot_bytes', 'delivery_record_bytes', 'delivery_free_bytes', 'scratch_bytes', 'retained_bytes']:
        out[k] = row.get(k, 0) * MIB
    out['bbr_explicit'] = sum(out[k] for k in ['ring_bytes', 'delivery_slot_bytes', 'delivery_record_bytes', 'delivery_free_bytes',
                                                'scratch_bytes', 'retained_bytes'])
    out['ring_entries'] = row.get('ring_entries', 0)
    out['ring_evicted'] = row.get('ring_evicted', 0)
    out['occupancy'] = max(row.get('conn_received', 0) - row.get('conn_read', 0), 0) * MIB  # descriptive only
    return out


def instrument_valid(rows, t0_ns, warmup_ms, measure_ms):
    """(valid, reason) for one endpoint's series: a valid sample, and valid samples in at least WINDOW_COVERAGE of
    the measured window's seconds. Applied to every arm and endpoint before block selection (departure H)."""
    if not any(valid(r) for r in rows):
        return False, 'no valid memory sample'
    lo, hi = t0_ns + warmup_ms * 1_000_000, t0_ns + (warmup_ms + measure_ms) * 1_000_000
    seconds = {int((r['unix_ns'] - t0_ns) // 1_000_000_000) for r in rows if valid(r) and lo <= r['unix_ns'] < hi}
    need = int(measure_ms // 1000)
    if need <= 0 or len(seconds) < WINDOW_COVERAGE * need:
        return False, f'memory window coverage {len(seconds)}/{need} s'
    return True, None


def peak_account(rows, peak_rss_mib):
    """The partition at the largest valid sampled Rss, with the signed sampling gap to the ru_maxrss peak; None if no valid sample."""
    ok = [r for r in rows if valid(r)]
    if not ok:
        return None
    best = max(ok, key=lambda r: r['rollup_Rss_kb'])
    a = account(best)
    a['unix_ns'] = best['unix_ns']
    a['gap'] = peak_rss_mib - a['rss']
    a['peak'] = peak_rss_mib
    return a


def window_medians(cand_rows, reno_rows, cand_t0, reno_t0, warmup_ms, measure_ms,
                   keys=('/gc/heap/live:bytes', '/memory/classes/heap/objects:bytes')):
    """({key: median excess in MiB over the measured window}, coverage).

    Only valid samples whose own timestamps fall inside each run's measured
    window count; they are aligned by whole seconds since each run's configured
    start. Coverage is the share of the window's seconds present in both runs.
    """
    def by_second(rows, t0):
        lo, hi = t0 + warmup_ms * 1_000_000, t0 + (warmup_ms + measure_ms) * 1_000_000
        out = {}
        for r in rows:
            if valid(r) and lo <= r['unix_ns'] < hi:
                out.setdefault(int((r['unix_ns'] - t0) // 1_000_000_000), r)
        return out
    c, r = by_second(cand_rows, cand_t0), by_second(reno_rows, reno_t0)
    seconds = range(int(warmup_ms // 1000), int((warmup_ms + measure_ms) // 1000))
    both = [k for k in seconds if k in c and k in r]
    coverage = len(both) / len(seconds) if len(seconds) else 0.0
    if not both:
        return {k: None for k in keys}, coverage
    return {key: statistics.median((c[k][key] - r[k][key]) * MIB for k in both) for key in keys}, coverage


def excess(cand, reno, window=None, coverage=None):
    """Per-class excess (candidate minus Reno) at each endpoint's own peak sample; the classes sum to the peak excess.

    window: window_medians' dict (or None); coverage: its coverage.
    """
    keys = CLASSES + UNEXPLAINED + ['live', 'bbr_explicit', 'ring_bytes', 'occupancy']
    out = {k: cand[k] - reno[k] for k in keys}
    out['peak'] = cand['peak'] - reno['peak']
    out['closure'] = out['peak'] - sum(out[k] for k in CLASSES + UNEXPLAINED)
    out['heap'] = out['objects'] + out['unused'] + out['free']
    window = window or {}
    out['live_window'] = window.get('/gc/heap/live:bytes')
    out['objects_window'] = window.get('/memory/classes/heap/objects:bytes')
    out['window_coverage'] = coverage
    if out['peak'] > 0:
        e = out['peak']
        share = lambda v: v / e if v is not None else None
        out.update(objects_share=out['objects'] / e, heap_share=out['heap'] / e, headroom_share=(out['unused'] + out['free']) / e,
                   live_share=2 * out['live'] / e, live_window_share=share(2 * out['live_window'] if out['live_window'] is not None else None),
                   objects_window_share=share(out['objects_window']),
                   unexplained_share=(abs(out['residual']) + abs(out['gap'])) / e)
    return out


def explained(ex):
    """|residual| + |sampling gap| excess within the registered share of a positive peak excess (no cancellation)."""
    return ex['peak'] > 0 and ex['unexplained_share'] <= UNEXPLAINED_SHARE


def window_ok(ex):
    return (ex.get('window_coverage') is not None and ex['window_coverage'] >= WINDOW_COVERAGE
            and ex.get('live_window') is not None and ex.get('objects_window') is not None)


def live_heap(ex):
    """D6's heap-objects condition, window- and GC-aligned, plus GC-aligned live heap at the peak and over the window."""
    return (ex['peak'] > 0 and window_ok(ex) and ex['objects_window_share'] >= LABEL and ex['live_share'] >= LABEL
            and ex['live_window_share'] >= LABEL)


def dominant(ex):
    """Descriptive only: the accounting class holding at least LABEL of a positive excess, or None. Never a cause."""
    if ex['peak'] <= 0:
        return None
    for k in CLASSES:
        if ex[k] >= LABEL * ex['peak']:
            return k
    return None


# ---- Stage 0: ring treatment preflight and engagement -------------------------------------------

def ring_preflight(gates_pass, mutants_detected, activation_ring_bytes, candidate_ring_bytes):
    """'run', 'unusable', 'inert' or 'not run (below detectability)', and the predicted reduction (MiB), before Stage 1."""
    predicted = (candidate_ring_bytes - activation_ring_bytes) * MIB
    if not gates_pass or not all(mutants_detected):
        return dict(decision='unusable', predicted_mib=predicted)
    if activation_ring_bytes >= candidate_ring_bytes:
        return dict(decision='inert', predicted_mib=predicted)
    if predicted <= DETECTABLE_MIB:
        return dict(decision='not run (below detectability)', predicted_mib=predicted)
    return dict(decision='run', predicted_mib=predicted)


def ring_engaged(ring_peak, cand_peak, entries):
    """Per counted run: the ring arm holds the registered ring and less ring storage than the candidate."""
    return ring_peak['ring_entries'] == entries and ring_peak['ring_bytes'] < cand_peak['ring_bytes']


# ---- Stage 1: comparability ---------------------------------------------------------------------

# D6's categories and the measures registered for them (README, departure G). Sender-declared lost packets
# are recorded but not compared: the candidate's BBR loss path leaves ConnectionStats.PacketsLost at zero.
TRAFFIC = ['goodput', 'fwd_per_gib', 'feedback_per_gib', 'control']
TRAFFIC_WAN = TRAFFIC + ['overflow']


COUNTER_MAX_AGE_NS = 1_500_000_000  # one-second sampler plus 0.5 s of jitter


def window_delta(rows, t0, warmup_ms, measure_ms, key):
    """Counter increase over the measured window, or None.

    Uses the last valid sample at or before each window edge, which must be at
    most COUNTER_MAX_AGE_NS old; the counter must not decrease anywhere in the
    series. The result approximates the window total to within one sampling
    interval at each edge; it is never interpolated.
    """
    lo, hi = t0 + warmup_ms * 1_000_000, t0 + (warmup_ms + measure_ms) * 1_000_000
    pts = [(r['unix_ns'], r[key]) for r in rows if isinstance(r.get(key), int) and r[key] >= 0 and isinstance(r.get('unix_ns'), int)]
    if any(b[0] < a[0] or b[1] < a[1] for a, b in zip(pts, pts[1:])):
        return None
    before = [p for p in pts if p[0] <= lo]
    upto = [p for p in pts if p[0] <= hi]
    if not before or not upto or lo - before[-1][0] > COUNTER_MAX_AGE_NS or hi - upto[-1][0] > COUNTER_MAX_AGE_NS:
        return None
    return upto[-1][1] - before[-1][1]


def traffic(summary, send_rows, recv_rows, t0, warmup_ms, measure_ms):
    """Traffic measures of one observation; a measure is None when it cannot be computed.

    useful delivery: goodput; packets per useful GiB: sender packets sent;
    feedback: receiver packets sent; control: control replies; forward
    overflow (WAN): relay overflow fraction. Sender-declared lost packets are
    recorded (lost_per_gib) but are not a comparability measure. Packet counts are the
    connection's ConnectionStats deltas over the measured window.
    """
    gib = summary['useful_bytes'] / 2**30
    per = lambda v: v / gib if v is not None and gib > 0 else None
    out = dict(goodput=summary['goodput_mbps'], control=summary['control_replies'],
               fwd_per_gib=per(window_delta(send_rows, t0, warmup_ms, measure_ms, 'stats_packets_sent')),
               feedback_per_gib=per(window_delta(recv_rows, t0, warmup_ms, measure_ms, 'stats_packets_sent')),
               lost_per_gib=per(window_delta(send_rows, t0, warmup_ms, measure_ms, 'stats_packets_lost')))
    relay = summary.get('relay')
    if relay:
        f = relay['forward']['stats']
        out['overflow'] = f['Overflow'] / f['Received'] if f['Received'] else 0.0
    return out


def deviation(k, a, b):
    if k in ('overflow', 'control'):
        return abs(a - b)
    return abs(a - b) / a if a else 0.0


def aa_band(pairs, measures):
    """Per measure, the largest A/A deviation over clean calibration pairs, with floors; None if uncalibrated.

    pairs: [dict(cand=traffic, aa=traffic)] from clean, usable blocks only.
    """
    pairs = [p for p in pairs if all(p['cand'].get(k) is not None and p['aa'].get(k) is not None for k in measures)]
    if len(pairs) < CALIBRATION_MIN:
        return None
    band = {}
    for k in measures:
        d = max(deviation(k, p['cand'][k], p['aa'][k]) for p in pairs)
        floor = OVERFLOW_FLOOR if k == 'overflow' else 1 if k == 'control' else COMPARABLE_FLOOR
        band[k] = max(d, floor)
    return band


def comparable(cand, arm, band):
    """(comparable, measures outside the band or missing) for one treatment arm in one block."""
    if band is None:
        return False, ['uncalibrated']
    out = [k for k, lim in band.items()
           if cand.get(k) is None or arm.get(k) is None or deviation(k, cand[k], arm[k]) > lim + 1e-12]
    return not out, out


# ---- Stage 1: per-cell label ---------------------------------------------------------------------

def eligibility(b):
    """(eligible for fractions, reason): instrument and selection validity first, then detectability of E."""
    if not b['usable']:
        return False, 'unusable'
    if b['E'] <= DETECTABLE_MIB:
        return False, f'E {b["E"]:.3f} MiB at or below {DETECTABLE_MIB}'
    return True, None


def block_fractions(b):
    """Removal fractions and the A/A variation, as fractions of the block's baseline excess E (an empirical envelope)."""
    e = b['E']
    out = dict(E=e, v=abs(b['aa_diff']) / e)
    for k in ('ring', 'rx'):
        if b.get(k) is not None:
            out[k] = b[k] / e
    return out


def label_cell(role, blocks, ring_status):
    """D6's per-cell rule over a cell's blocks (README, "Statistics and rule").

    Each block: E (candidate minus Reno peak RSS, MiB; equals ex['peak']), aa_diff (A/A minus candidate),
    ring and rx (candidate minus treatment arm, MiB, or None when not run), ring_engaged, ring_comparable,
    rx_comparable, usable (counted attempt clean, every arm present and usable, instrument valid), ex (the
    accounting excess, with the window fields).
    ring_status: 'run', 'not run', 'not run (below detectability)', 'unusable' or 'inert'.
    """
    usable = [b for b in blocks if eligibility(b)[0]]
    detail = dict(usable=len(usable), blocks=len(blocks))
    if len(usable) < MIN_BLOCKS:
        return dict(label='evidence gap (fewer than three usable blocks)', **detail)
    ok = [b for b in usable if explained(b['ex'])]
    detail['explained'] = len(ok)
    detail['median_unexplained_share'] = statistics.median(b['ex']['unexplained_share'] for b in usable)
    if len(ok) < MIN_BLOCKS or detail['median_unexplained_share'] > UNEXPLAINED_SHARE:
        return dict(label='evidence gap (resident memory unexplained by the accounting)', **detail)
    fr = [(b, block_fractions(b)) for b in ok]
    ring_on = ring_status == 'run'
    ring_valid = lambda b, f: ring_on and b.get('ring_engaged') and b.get('ring_comparable') and 'ring' in f
    ring_hits = [f for b, f in fr if ring_valid(b, f) and f['ring'] - f['v'] >= LABEL]
    ring_part = [f for b, f in fr if ring_valid(b, f) and f['ring'] - f['v'] >= BOUNDED]
    comp = [(b, f) for b, f in fr if b.get('rx_comparable') and 'rx' in f]
    rx_hits = [f for b, f in comp if f['rx'] - f['v'] >= LABEL]
    rx_part = [f for b, f in comp if f['rx'] - f['v'] >= BOUNDED]
    detail.update(ring_blocks=len(ring_hits), rx_comparable=len(comp), rx_blocks=len(rx_hits))
    if role == 'send' and len(ring_hits) >= MIN_BLOCKS:
        return dict(label='sender bookkeeping (ring)', **detail)
    if role == 'receive' and len(rx_hits) >= MIN_BLOCKS:
        return dict(label='receiver-side controller state', **detail)

    def localizes(b, f):
        if not (f['rx'] + f['v'] < BOUNDED and live_heap(b['ex'])):
            return False
        # A sender cell localizes only where a valid ring diagnostic bounds the ring's share below 30%.
        return role == 'receive' or (ring_valid(b, f) and f['ring'] + f['v'] < BOUNDED)
    local = [(b, f) for b, f in comp if localizes(b, f)]
    detail['localized_blocks'] = len(local)
    if len(comp) >= MIN_BLOCKS and len(local) >= MIN_BLOCKS:
        return dict(label='localized to traffic-dependent live heap (cause unresolved)', **detail)
    if len(ring_part) >= MIN_BLOCKS or len(rx_part) >= MIN_BLOCKS:
        return dict(label='mixed', **detail)
    headroom = [b for b in ok if b['ex']['headroom_share'] >= LABEL]
    if len(headroom) >= MIN_BLOCKS:
        return dict(label='inconclusive (heap unused and free classes hold the excess; no discriminating comparison)', **detail)
    if role == 'send' and ring_status in ('unusable', 'inert'):
        return dict(label=f'treatment {ring_status}', **detail)
    if len(comp) < MIN_BLOCKS:
        return dict(label='inconclusive (receiver-controller arm confounded or uncalibrated)', **detail)
    if role == 'send' and not ring_on:
        return dict(label='inconclusive (ring diagnostic not run)', **detail)
    return dict(label='inconclusive', **detail)


CONCLUSIONS = ('sender bookkeeping (ring)', 'receiver-side controller state',
               'localized to traffic-dependent live heap (cause unresolved)', 'mixed')


def combine_sensitivity(primary, sensitivity):
    """The registered ending: a causal, localized or mixed primary conclusion stands only when the sampled-Rss
    sensitivity pass reaches the same label; otherwise it is unresolved. Other primary endings stand."""
    if primary['label'] in CONCLUSIONS and sensitivity['label'] != primary['label']:
        return dict(primary, label='unresolved (sensitive to the peak instrument)', rule_label=primary['label'],
                    sensitivity_label=sensitivity['label'])
    return dict(primary, sensitivity_label=sensitivity['label'])


# ---- S6 control p95 (D2's Up-policy rule, both workloads) ----------------------------------------

QUEUE_TICK_MS = 10
QUEUE_COVERAGE = 0.90      # queue samples present, as a share of the window's 10 ms ticks
QUEUE_MAX_GAP_MS = 100     # largest permitted gap between consecutive samples, and at either window edge


def _number(x):
    return isinstance(x, (int, float)) and not isinstance(x, bool) and x == x and abs(x) != float('inf')


def well_formed(phases, queue_samples):
    """(ok, reason): containers and record shapes, checked before any indexing or comparison."""
    if not isinstance(phases, list) or not phases:
        return False, 'no phase log'
    if any(not isinstance(p, (list, tuple)) or len(p) != 2 or not isinstance(p[0], int) or isinstance(p[0], bool)
           or not isinstance(p[1], int) or isinstance(p[1], bool) or not 0 <= p[1] < len(PHASES) for p in phases):
        return False, 'malformed phase record'
    if not isinstance(queue_samples, list):
        return False, 'no queue samples'
    if any(not isinstance(x, (list, tuple)) or len(x) < 2 or not _number(x[0]) or not _number(x[1]) or x[1] < 0 for x in queue_samples):
        return False, 'malformed queue sample'
    return True, None


def distinct_ticks(window_samples, warmup_ms):
    """The window's samples with at most one per 10 ms tick (the first; ticks by nearest multiple, so jitter is
    allowed), so duplicated records can neither add coverage nor weight."""
    seen, out = set(), []
    for x in window_samples:
        tick = round((x[0] - warmup_ms) / QUEUE_TICK_MS)
        if tick not in seen:
            seen.add(tick)
            out.append(x)
    return out


def queue_integrity(phases, window_samples, warmup_ms, measure_ms):
    """(ok, reason) for one run's S6 inputs: an ordered phase log and a covered, ordered queue timeline of distinct
    ticks (shapes are checked first by well_formed)."""
    if any(b[0] < a[0] for a, b in zip(phases, phases[1:])):
        return False, 'phase timestamps out of order'
    ts = [x[0] for x in window_samples]
    if any(b < a for a, b in zip(ts, ts[1:])):
        return False, 'queue samples out of order'
    if len(ts) < QUEUE_COVERAGE * measure_ms / QUEUE_TICK_MS:
        return False, f'queue coverage {len(ts)} distinct ticks'
    edges = [ts[0] - warmup_ms, warmup_ms + measure_ms - ts[-1]] + [b - a for a, b in zip(ts, ts[1:])]
    if max(edges) > QUEUE_MAX_GAP_MS:
        return False, f'queue gap {max(edges)} ms'
    return True, None


def queue_by_phase(phases, queue_samples, t0_ns, warmup_ms, measure_ms):
    """Forward queue delay (ms) per BBR phase over the measured window, after the integrity check.

    Down counts toward the Up share only when entered from Up ("Up or the
    following Down"). Samples with no logged phase stay in the denominator as
    'unknown'. Returns None for a missing phase log, and dict(integrity=False,
    reason) for any other failed integrity check; both are evidence gaps.
    """
    if not phases:
        return None
    ok, reason = well_formed(phases, queue_samples)
    if not ok:
        return dict(integrity=False, reason=reason)
    window = distinct_ticks([x for x in queue_samples if warmup_ms <= x[0] < warmup_ms + measure_ms], warmup_ms)
    ok, reason = queue_integrity(phases, window, warmup_ms, measure_ms)
    if not ok:
        return dict(integrity=False, reason=reason)
    by_phase, above = {}, {'up': 0, 'down_after_up': 0, 'other': 0}
    for t_ms, fwd, *_ in window:
        at = t0_ns + t_ms * 1_000_000
        cur, prev = None, None
        for ns, ph in phases:
            if ns > at:
                break
            prev, cur = cur, ph
        name = PHASES[cur] if cur is not None else 'unknown'
        delay = fwd * 8 / BOTTLENECK_BPS * 1e3
        by_phase.setdefault(name, []).append(delay)
        if delay > 25:
            if cur == UP:
                above['up'] += 1
            elif cur == DOWN and prev == UP:
                above['down_after_up'] += 1
            else:
                above['other'] += 1
    total = sum(above.values())
    return dict(integrity=True, by_phase={k: dict(samples=len(v), median_ms=statistics.median(v), share_over_25ms=sum(x > 25 for x in v) / len(v))
                          for k, v in by_phase.items()},
                above_25ms=above,
                over_25ms_in_up_or_down=(above['up'] + above['down_after_up']) / total if total else None,
                cruise_median_ms=statistics.median(by_phase['cruise']) if 'cruise' in by_phase else None,
                refill_median_ms=statistics.median(by_phase['refill']) if 'refill' in by_phase else None)


def up_policy(runs, s5_matched_p95, required_runs):
    """D2 over the registered runs: every run's >25 ms samples at least 70% in Up or the following Down, Cruise and
    Refill each present and below 1 ms, and S5 matched-load p95 within 1.20. Missing or invalid inputs (too few runs,
    a missing phase log, a failed integrity check) are an evidence gap. A complete timeline that never enters Cruise
    or Refill, or has no sample above 25 ms, does not meet the conditions and is unresolved."""
    if len(runs) < required_runs or any(r is None or not r.get('integrity') for r in runs):
        return 'evidence gap'
    ok = (all(r['over_25ms_in_up_or_down'] is not None and r['over_25ms_in_up_or_down'] >= UP_SHARE for r in runs)
          and all(r['cruise_median_ms'] is not None and r['refill_median_ms'] is not None
                  and r['cruise_median_ms'] < CRUISE_MS and r['refill_median_ms'] < CRUISE_MS for r in runs)
          and s5_matched_p95 <= S5_P95)
    return 'selected ProbeBW Up policy' if ok else 'unresolved'


# ---- Perturbation ---------------------------------------------------------------------------------

def perturbation(instrumented, plain):
    """Instrumented ÷ plain goodput medians (D6: at least 0.99); None when either side has fewer than three values."""
    if len(instrumented) < MIN_BLOCKS or len(plain) < MIN_BLOCKS:
        return None
    return statistics.median(instrumented) / statistics.median(plain)
