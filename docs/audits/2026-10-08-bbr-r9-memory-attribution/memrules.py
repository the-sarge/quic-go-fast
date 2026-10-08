#!/usr/bin/env python3
"""#740 registered rules (D6 of the #737 decision), as pure functions.

memrules_test.py exercises every rule, the synthetic accounting cases and the
labels, and is committed with the registration before any comparative data.

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
excluded because it is returned to the OS. The residual is whatever resident
anonymous memory the runtime accounting does not explain (negative when mapped
runtime memory is not resident). The peak resident memory of the readiness
cell (ru_maxrss, `peak_rss_mib`) is the sample maximum plus a sampling gap
(VmHWM minus the largest sampled Rss), so
  peak = file + objects + unused + free + stacks + metadata + other + residual + gap
holds exactly at the peak sample, up to the kB rounding of /proc.

GC alignment. Heap objects at one sample include garbage not yet collected,
which moves between the objects and free classes with the GC cycle (the
synthetic retained case puts an 8 MiB cause at +8.2 to +15.8 MiB of objects).
The rule therefore reads the heap at GC alignment: the live heap at the last
completed mark (/gc/heap/live). With the fixture's default GOGC=100 a live-heap
excess L raises the heap goal, and so resident heap, by about 2L. The live
share of a cell's excess E is 2 x (live excess) / E, taken at the peak sample
and as the median over the measured window's per-second samples (aligned by
seconds since the run's configured start), so a warmup-only excess fails the
window test. The heap share is (objects + unused + free) excess / E.
"""
import json
import statistics

KIB = 1 / 1024  # kB -> MiB
MIB = 1 / 2**20

CLASSES = ['file', 'objects', 'unused', 'free', 'stacks', 'metadata', 'other']
UNEXPLAINED = ['residual', 'gap']

# Registered thresholds.
DETECTABLE_MIB = 0.5        # block usability for fractions, and the ring preflight (README, "Thresholds")
UNEXPLAINED_SHARE = 0.25    # a block whose |residual + gap| excess exceeds this share of its baseline excess is unexplained
LABEL = 0.70                # removal (beyond A/A) for a causal label; heap share for localization
BOUNDED = 0.30              # removal upper bound for localization; a treatment lower bound this large blocks it
MIN_BLOCKS = 3
COMPARABLE_FLOOR = 0.01     # relative floor of the receiver-controller comparability band
OVERFLOW_FLOOR = 0.0005     # absolute floor (fraction of forward packets) for overflow
# D2's Up-policy thresholds (#715, #736), unchanged.
UP_SHARE, CRUISE_MS, S5_P95 = 0.70, 1.0, 1.20
PHASES = ['startup', 'drain', 'down', 'cruise', 'refill', 'up', 'probertt']
BOTTLENECK_BPS = 100e6


# ---- Stage 0: accounting ---------------------------------------------------------------------

def parse_series(text):
    """(columns, rows as dicts, tail) from one {role}.mem.jsonl."""
    lines = [json.loads(x) for x in text.splitlines() if x.strip()]
    header = lines[0]
    rows = [dict(zip(header['columns'], r)) for r in lines[1:] if isinstance(r, list)]
    tails = [r for r in lines[1:] if isinstance(r, dict)]
    return header, rows, (tails[-1] if tails else None)


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
    return out


def peak_account(rows, peak_rss_mib):
    """The partition at the largest sampled Rss, with the sampling gap to the endpoint's ru_maxrss peak."""
    best = max(rows, key=lambda r: r['rollup_Rss_kb'])
    a = account(best)
    a['unix_ns'] = best['unix_ns']
    a['gap'] = peak_rss_mib - a['rss']
    a['peak'] = peak_rss_mib
    return a


def excess(cand, reno, live_window=None):
    """Per-class excess (candidate minus Reno) at each endpoint's own peak sample; sums to the peak excess.

    live_window: the median measured-window live-heap excess (MiB, window_live), or None.
    """
    keys = CLASSES + UNEXPLAINED + ['live', 'bbr_explicit', 'ring_bytes']
    out = {k: cand[k] - reno[k] for k in keys}
    out['peak'] = cand['peak'] - reno['peak']
    out['closure'] = out['peak'] - sum(out[k] for k in CLASSES + UNEXPLAINED)
    out['heap'] = out['objects'] + out['unused'] + out['free']
    out['live_window'] = live_window
    if out['peak'] > 0:
        out['heap_share'] = out['heap'] / out['peak']
        out['live_share'] = 2 * out['live'] / out['peak']
        out['live_window_share'] = 2 * live_window / out['peak'] if live_window is not None else None
    return out


def window_live(cand_rows, reno_rows, cand_t0, reno_t0, warmup_ms, measure_ms):
    """Median over the measured window's seconds of the live-heap excess (MiB), aligned by seconds since each run's start."""
    def at(rows, t0):
        by = {}
        for r in rows:
            by.setdefault(round((r['unix_ns'] - t0) / 1e9), r)
        return by
    c, r = at(cand_rows, cand_t0), at(reno_rows, reno_t0)
    secs = [k for k in range(int(warmup_ms / 1000), int((warmup_ms + measure_ms) / 1000) + 1) if k in c and k in r]
    if not secs:
        return None
    return statistics.median((c[k]['/gc/heap/live:bytes'] - r[k]['/gc/heap/live:bytes']) * MIB for k in secs)


def live_heap(ex):
    """The excess sits in GC-aligned live heap: heap share, live share at the peak and over the window all >= LABEL."""
    return (ex['peak'] > 0 and ex['heap_share'] >= LABEL and ex['live_share'] >= LABEL
            and ex['live_window_share'] is not None and ex['live_window_share'] >= LABEL)


def explained(ex):
    """True when the unexplained excess (residual + sampling gap) is within the registered share of the peak excess."""
    if ex['peak'] <= 0:
        return False
    return abs(ex['residual'] + ex['gap']) <= UNEXPLAINED_SHARE * ex['peak']


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
    """'run', 'unusable' or 'inert' and the predicted reduction (MiB), before Stage 1."""
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


# ---- Stage 1: receiver-controller comparability ------------------------------------------------

def traffic(obs):
    """Traffic measures of one observation (summary row): useful delivery and, on WAN paths, relay packet counts."""
    out = dict(goodput=obs['goodput_mbps'], control=obs['control_replies'])
    relay = obs.get('relay')
    if relay:
        gib = obs['useful_bytes'] / 2**30
        f, r = relay['forward']['stats'], relay['reverse']['stats']
        out.update(fwd_per_gib=f['Delivered'] / gib, rev_per_gib=r['Delivered'] / gib,
                   overflow=f['Overflow'] / f['Received'] if f['Received'] else 0.0)
    return out


def aa_band(blocks):
    """Per measure, the largest A/A deviation over the stage's blocks: relative, or absolute for overflow and control."""
    band = {}
    for b in blocks:
        c, a = b['cand'], b['aa']
        for k in c:
            if k in ('overflow',):
                d = abs(a[k] - c[k])
            elif k == 'control':
                d = abs(a[k] - c[k])
            else:
                d = abs(a[k] - c[k]) / c[k] if c[k] else 0.0
            band[k] = max(band.get(k, 0.0), d)
    for k in band:
        if k == 'overflow':
            band[k] = max(band[k], OVERFLOW_FLOOR)
        elif k == 'control':
            band[k] = max(band[k], 1)
        else:
            band[k] = max(band[k], COMPARABLE_FLOOR)
    return band


def comparable(cand, rx, band):
    """(comparable, measures outside the band) for the receiver-controller arm in one block."""
    out = []
    for k, lim in band.items():
        if k in ('overflow', 'control'):
            d = abs(rx[k] - cand[k])
        else:
            d = abs(rx[k] - cand[k]) / cand[k] if cand[k] else 0.0
        if d > lim + 1e-12:
            out.append(k)
    return not out, out


# ---- Stage 1: per-cell label ---------------------------------------------------------------------

def block_fractions(b):
    """Removal fractions and the A/A variation, as fractions of the block's baseline excess E."""
    e = b['E']
    out = dict(E=e, v=abs(b['aa_diff']) / e)
    for k in ('ring', 'rx'):
        if b.get(k) is not None:
            out[k] = b[k] / e
    return out


def label_cell(role, blocks, ring_status):
    """D6's per-cell rule over a cell's blocks.

    Each block: E (candidate minus Reno peak RSS, MiB; equals ex['peak']), aa_diff (A/A candidate minus candidate),
    ring and rx (candidate minus treatment arm, MiB, or None when not run), ring_engaged,
    ring_comparable, rx_comparable, usable (every arm usable and the block uncontaminated), ex (the
    accounting excess, with live_window), occ_excess (the receive occupancy excess at the peak sample,
    MiB, receiver cells only, else None).
    ring_status: 'run', 'not run', 'not run (below detectability)', 'unusable' or 'inert'.
    """
    usable = [b for b in blocks if b['usable'] and b['E'] > DETECTABLE_MIB]
    detail = dict(usable=len(usable), blocks=len(blocks))
    if len(usable) < MIN_BLOCKS:
        return dict(label='evidence gap (fewer than three usable blocks)', **detail)
    ok = [b for b in usable if explained(b['ex'])]
    detail['explained'] = len(ok)
    if len(ok) < MIN_BLOCKS:
        return dict(label='evidence gap (resident memory unexplained by the accounting)', **detail)
    fr = [(b, block_fractions(b)) for b in ok]
    ring_on = ring_status == 'run'
    ring_hits = [f for b, f in fr if ring_on and b.get('ring_engaged') and b.get('ring_comparable') and 'ring' in f and f['ring'] - f['v'] >= LABEL]
    ring_part = [f for b, f in fr if ring_on and b.get('ring_engaged') and b.get('ring_comparable') and 'ring' in f and f['ring'] - f['v'] >= BOUNDED]
    comp = [(b, f) for b, f in fr if b.get('rx_comparable') and 'rx' in f]
    rx_hits = [f for b, f in comp if f['rx'] - f['v'] >= LABEL]
    rx_part = [f for b, f in comp if f['rx'] - f['v'] >= BOUNDED]
    detail.update(ring_blocks=len(ring_hits), rx_comparable=len(comp), rx_blocks=len(rx_hits))
    if role == 'send' and len(ring_hits) >= MIN_BLOCKS:
        return dict(label='sender bookkeeping (ring)', **detail)
    if role == 'receive' and len(rx_hits) >= MIN_BLOCKS:
        return dict(label='receiver-side controller state', **detail)
    local = [(b, f) for b, f in comp
             if f['rx'] + f['v'] < BOUNDED and live_heap(b['ex'])
             and not (role == 'send' and ring_on and b.get('ring_engaged') and b.get('ring_comparable') and 'ring' in f and f['ring'] - f['v'] >= BOUNDED)]
    detail['localized_blocks'] = len(local)
    if len(comp) >= MIN_BLOCKS and len(local) >= MIN_BLOCKS:
        delivery = [b for b, f in local if b.get('occ_excess') is not None and b['occ_excess'] >= LABEL * b['ex']['live']]
        detail['delivery_blocks'] = len(delivery)
        if len(delivery) >= MIN_BLOCKS:
            return dict(label='localized to traffic-dependent live heap: delivery retention', **detail)
        return dict(label='localized to traffic-dependent live heap (cause unresolved)', **detail)
    if len(ring_part) >= MIN_BLOCKS or len(rx_part) >= MIN_BLOCKS:
        return dict(label='mixed', **detail)
    headroom = [b for b in ok if b['ex']['heap_share'] >= LABEL and b['ex']['live_share'] < BOUNDED]
    if len(headroom) >= MIN_BLOCKS:
        return dict(label='inconclusive (headroom-dominant accounting; no discriminating comparison)', **detail)
    if role == 'send' and ring_status in ('unusable', 'inert'):
        return dict(label=f'treatment {ring_status}', **detail)
    if len(comp) < MIN_BLOCKS:
        return dict(label='inconclusive (receiver-controller arm confounded)', **detail)
    return dict(label='inconclusive', **detail)


# ---- S6 control p95 (D2's Up-policy rule, both workloads) ----------------------------------------

def queue_by_phase(phases, queue_samples, t0_ns, warmup_ms, measure_ms):
    """Forward queue delay (ms) per BBR phase over the measured window, and the share of >25 ms samples in Up or Down."""
    by_phase = {}
    for t_ms, fwd, _ in queue_samples:
        if not warmup_ms <= t_ms < warmup_ms + measure_ms:
            continue
        at = t0_ns + t_ms * 1_000_000
        cur = None
        for ns, ph in phases:
            if ns > at:
                break
            cur = ph
        if cur is not None:
            by_phase.setdefault(PHASES[cur], []).append(fwd * 8 / BOTTLENECK_BPS * 1e3)
    over = {k: sum(x > 25 for x in v) for k, v in by_phase.items()}
    total = sum(over.values())
    cr = [statistics.median(by_phase[p]) for p in ('cruise', 'refill') if p in by_phase]
    return dict(by_phase={k: dict(samples=len(v), median_ms=statistics.median(v), share_over_25ms=over[k] / len(v)) for k, v in by_phase.items()},
                over_25ms_in_up_or_down=(over.get('up', 0) + over.get('down', 0)) / total if total else None,
                cruise_refill_median_ms=statistics.median(cr) if cr else None)


def up_policy(runs, s5_matched_p95):
    """D2: every run's >25 ms samples at least 70% in Up or Down, Cruise/Refill median under 1 ms, S5 matched p95 within 1.20."""
    if not runs:
        return 'evidence gap'
    if any(r['over_25ms_in_up_or_down'] is None or r['cruise_refill_median_ms'] is None for r in runs):
        return 'evidence gap'
    ok = (all(r['over_25ms_in_up_or_down'] >= UP_SHARE for r in runs) and all(r['cruise_refill_median_ms'] < CRUISE_MS for r in runs)
          and s5_matched_p95 <= S5_P95)
    return 'selected ProbeBW Up policy' if ok else 'unresolved'


# ---- Perturbation ---------------------------------------------------------------------------------

def perturbation(instrumented, plain):
    """Instrumented ÷ plain goodput medians (D6: at least 0.99)."""
    return statistics.median(instrumented) / statistics.median(plain)
