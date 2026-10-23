#!/usr/bin/env python3
"""#735 registered receiver CPU-time rule: clock rate or idle effects against uncounted work.

Written, with synthetic cases in receiver_test.py, before any counted
observation existed. Registered in README.md ("Registration: receiver CPU
time against cycles"). It labels a readiness cell for a later exception
decision; it never changes a limit or clears a flag.

Per observation, over the measured window at the receiver, with T = task-clock,
I = instructions (user + kernel), C = PMC cycles (user + kernel), A = APERF and
M = MPERF (both counted per task by the MSR PMU), all per useful GiB:

    T = I x (C / I) x (A / C) x (M / A) x (T / M)

Per block, each factor's log ratio against the same block's frozen Reno gives
an exact partition of ln(T ratio):
    work     ln(I ratio)           receiver instructions
    cpi      ln((C/I) ratio)       cycles per instruction
    uncounted ln((A/C) ratio)      clocks inside the scheduled time that PMC cycles miss
    clock    -ln((A/M) ratio)      a lower average clock while running
    closure  ln((T/M) ratio)       task-clock not matched by C0 reference clocks
"""
import math
import statistics

COMPONENTS = ['work', 'cpi', 'uncounted', 'clock', 'closure']
EXPLANATIONS = {'clock rate': ['clock'], 'uncounted receiver work': ['uncounted'], 'counted receiver work': ['work', 'cpi']}
LEAD, OTHER = 0.5, 0.25      # #715's share thresholds
CLOSURE_LIMIT = 0.01         # |median closure log ratio| above this makes the counters unusable for the rule
MIN_BLOCKS = 4


def factors(m):
    """m: {'T','I','C','A','M'} per useful GiB (any consistent units)."""
    return dict(T=m['T'], work=m['I'], cpi=m['C'] / m['I'], uncounted=m['A'] / m['C'], clock=m['M'] / m['A'], closure=m['T'] / m['M'])


def components(arm, ref):
    """Log-ratio components of arm against ref; they sum to ln(T ratio) exactly."""
    a, b = factors(arm), factors(ref)
    out = {k: math.log(a[k] / b[k]) for k in COMPONENTS}
    out['t'] = math.log(a['T'] / b['T'])
    return out


def explanation_values(c):
    return {e: sum(c[k] for k in ks) for e, ks in EXPLANATIONS.items()}


def verdict(blocks):
    """blocks: [{'arm': metrics, 'aa': metrics, 'reno': metrics}] for one workload, usable blocks only.

    Returns the registered verdict with its evidence.
    """
    if len(blocks) < MIN_BLOCKS:
        return dict(verdict='evidence gap', blocks=len(blocks))
    arm = [components(b['arm'], b['reno']) for b in blocks]
    aa = [components(b['aa'], b['reno']) for b in blocks]
    med = lambda xs, k: statistics.median(x[k] for x in xs)
    rng = lambda xs, k: (min(x[k] for x in xs), max(x[k] for x in xs))
    out = dict(blocks=len(blocks), t=dict(median=med(arm, 't'), aa=rng(aa, 't')),
               components={k: dict(median=med(arm, k), aa=rng(aa, k)) for k in COMPONENTS})
    if abs(med(arm, 'closure')) > CLOSURE_LIMIT or abs(med(aa, 'closure')) > CLOSURE_LIMIT:
        return dict(out, verdict='unusable counters: task-clock and reference clocks disagree')
    if not med(arm, 't') > rng(aa, 't')[1]:
        return dict(out, verdict='no in-window CPU-time excess')
    pos = [x for x in arm if x['t'] > 0]
    ex_arm = [explanation_values(x) for x in arm]
    ex_aa = [explanation_values(x) for x in aa]
    shares = {e: statistics.median(explanation_values(x)[e] / x['t'] for x in pos) for e in EXPLANATIONS}
    out.update(shares=shares, explanations={e: dict(median=med(ex_arm, e), aa=rng(ex_aa, e)) for e in EXPLANATIONS})
    leading = [e for e, s in shares.items() if s >= LEAD]
    big = [e for e, s in shares.items() if s >= OTHER]
    if len(big) >= 2:
        return dict(out, verdict='mixed: ' + ', '.join(sorted(big)))
    if leading:
        e = leading[0]
        if not out['explanations'][e]['median'] > out['explanations'][e]['aa'][1]:
            return dict(out, verdict=f'inconclusive: {e} leads but lies inside its A/A range')
        return dict(out, verdict=e)
    return dict(out, verdict='inconclusive')


def idle_rates(idle, gib, window_s):
    """Descriptive: idle-state entries per useful GiB and residency share, summed over the endpoint's cores."""
    out = {}
    for c in idle['cpus']:
        a, b = idle['start']['cpus'][str(c)], idle['stop']['cpus'][str(c)]
        for s in a:
            name = a[s]['name']
            e = out.setdefault(name, dict(entries_per_gib=0.0, residency=0.0))
            e['entries_per_gib'] += (b[s]['usage'] - a[s]['usage']) / gib
            e['residency'] += (b[s]['time_us'] - a[s]['time_us']) / 1e6 / window_s / len(idle['cpus'])
    return out
