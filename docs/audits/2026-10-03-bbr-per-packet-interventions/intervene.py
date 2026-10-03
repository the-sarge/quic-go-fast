#!/usr/bin/env python3
"""#714 registered intervention-outcome rules.

Registered in README.md ("Registration: measurement") and exercised on synthetic
cases in intervene_test.py before any measurement observation existed. The rule
functions operate on per-block metric dictionaries; the loaders below turn the
retained observations into those dictionaries with #712's unchanged helpers
(localize.py: counter_quality, forward_packets, contamination).

An outcome is a conditional net effect of one change in its registered order. It
says whether the targeted per-packet work fell, and nothing about a readiness flag.

Usage on any host holding the observations: intervene.py m1 [m2 ...]
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-per-packet-interventions'
run.OBS = run.ART / 'observations'
from localize import contamination, counter_quality, forward_packets  # noqa: E402

ARMS = ['reno', 'aa', 'prev', 'new', 'renonew']
EVENTS = ['instructions:u', 'instructions:k', 'cycles:u', 'cycles:k']
PRIMARY = ('send', 'instructions:u/pkt')    # targeted work: sender user instructions per forward packet
SECONDARY = ('send', 'cycles:u/pkt')        # disagreement check
PRESERVATION = [('send', 'cycles/gib'), ('receive', 'cycles/gib')]  # Reno on the new revision against frozen Reno, both workloads pooled
MIN_BLOCKS = 4


# ---- rules ----------------------------------------------------------------------
def median_range(xs):
    return dict(median=statistics.median(xs), min=min(xs), max=max(xs), n=len(xs))


def ratios(blocks, num, den, key):
    """Per-block ratios num/den of one metric over blocks where both arms are usable."""
    role, metric = key
    return [b[num][role][metric] / b[den][role][metric] for b in blocks]


def classify_work(e, a, ce, ca):
    """Primary effect e (new/prev per block) against the A/A range a (aa/reno per block).

    ce and ca are the same for the secondary (cycles) metric. Returns one of
    'reduction', 'increase', 'null', 'inconclusive: <reason>'.
    """
    if len(e) < MIN_BLOCKS:
        return 'inconclusive: evidence gap (fewer than four usable blocks)'
    m, lo, hi = statistics.median(e), min(a), max(a)
    if lo <= m <= hi:
        return 'null'
    if m < lo and all(x < 1 for x in e):
        kind = 'reduction'
    elif m > hi and all(x > 1 for x in e):
        kind = 'increase'
    else:
        return 'inconclusive: effect beyond the A/A range but not in every block'
    c = statistics.median(ce)
    if kind == 'reduction' and c > max(ca):
        return 'inconclusive: instructions fell while cycles rose beyond their A/A range'
    if kind == 'increase' and c < min(ca):
        return 'inconclusive: instructions rose while cycles fell beyond their A/A range'
    return kind


def combine_workloads(kinds):
    """One lead outcome from the per-workload work classifications."""
    base = {k.split(':')[0] for k in kinds.values()}
    if 'reduction' in base and ('increase' in base or 'inconclusive' in base):
        return 'inconclusive'
    if 'reduction' in base:
        return 'reduction'
    if 'increase' in base:
        return 'increase'
    if 'inconclusive' in base:
        return 'inconclusive'
    return 'null'


def preservation_cell(blocks, key):
    """Reno on the new revision stays inside frozen Reno's A/A range: median per-block ratio within [min, max]."""
    r, a = ratios(blocks, 'renonew', 'reno', key), ratios(blocks, 'aa', 'reno', key)
    m = statistics.median(r)
    side = 'inside' if min(a) <= m <= max(a) else ('above' if m > max(a) else 'below')
    return dict(renonew=median_range(r), aa=median_range(a), position=side)


def lead_outcome(work, preservation, integrity):
    """keep | null | negative | inconclusive, with the reasons that decided it.

    work: combine_workloads result; preservation: {cell: preservation_cell}; integrity: all observations passed.
    """
    reasons = []
    if not integrity:
        reasons.append('receiver integrity or a clean exit failed')
    worse = [c for c, p in preservation.items() if p['position'] == 'above']
    outside = [c for c, p in preservation.items() if p['position'] != 'inside']
    if outside:
        reasons.append('Reno on the new revision outside the A/A range: ' + ', '.join(outside))
    if not integrity or worse:
        return 'negative', reasons
    if work == 'reduction':
        return ('inconclusive', reasons) if outside else ('keep', reasons)
    if work == 'increase':
        return 'negative', reasons + ['targeted work increased beyond the A/A range']
    if work == 'null':
        return 'null', reasons
    return 'inconclusive', reasons


def analyze_workload(blocks):
    """blocks: [{arm: {'send': {...}, 'receive': {...}}}] for one workload, usable blocks only."""
    e, a = ratios(blocks, 'new', 'prev', PRIMARY), ratios(blocks, 'aa', 'reno', PRIMARY)
    ce, ca = ratios(blocks, 'new', 'prev', SECONDARY), ratios(blocks, 'aa', 'reno', SECONDARY)
    out = dict(blocks=len(blocks), work=classify_work(e, a, ce, ca), primary=dict(effect=median_range(e) if e else None, aa=median_range(a) if a else None),
               secondary=dict(effect=median_range(ce) if ce else None, aa=median_range(ca) if ca else None))
    if not blocks:
        return out
    role, metric = PRIMARY
    out['delta_per_packet'] = statistics.median(b['new'][role][metric] - b['prev'][role][metric] for b in blocks)
    out['residual_per_packet'] = {arm: statistics.median(b[arm][role][metric] - b['reno'][role][metric] for b in blocks) for arm in ['prev', 'new']}
    out['reno_per_packet'] = statistics.median(b['reno'][role][metric] for b in blocks)
    # Descriptive: every metric at both endpoints, against the predecessor and frozen Reno.
    desc = {}
    for r in ['send', 'receive']:
        for m in blocks[0]['new'][r]:
            desc[f'{r}:{m}'] = {f'{x}/{y}': median_range(ratios(blocks, x, y, (r, m)))
                                for x, y in [('new', 'prev'), ('new', 'reno'), ('prev', 'reno'), ('renonew', 'reno'), ('aa', 'reno')]}
    out['ratios'] = desc
    return out


def analyze(per_workload, integrity):
    res = {wl: analyze_workload(bl) for wl, bl in per_workload.items()}
    work = combine_workloads({wl: r['work'] for wl, r in res.items()})
    # Preservation pools both workloads' blocks per endpoint: two cells of up to twelve ratios.
    pooled = [b for bl in per_workload.values() for b in bl]
    pres = {f'{r}:{m}': preservation_cell(pooled, (r, m)) for r, m in PRESERVATION} if pooled else {}
    outcome, reasons = lead_outcome(work, pres, integrity)
    if not pres:
        outcome, reasons = 'inconclusive', reasons + ['no usable block for the preservation check']
    return dict(workloads=res, work=work, preservation=pres, outcome=outcome, reasons=reasons)


# ---- loaders --------------------------------------------------------------------
def endpoint_metrics(row, role, packets):
    c = row[role]['counters']
    gib = row['useful_bytes'] / 2**30
    out = {}
    for ev in EVENTS:
        v = c[ev]['value']
        out[f'{ev}/pkt'], out[f'{ev}/gib'] = v / packets, v / gib
    for kind in ['instructions', 'cycles']:
        out[f'{kind}/pkt'] = out[f'{kind}:u/pkt'] + out[f'{kind}:k/pkt']
        out[f'{kind}/gib'] = out[f'{kind}:u/gib'] + out[f'{kind}:k/gib']
    out['cpu_seconds/gib'] = row[role]['cpu_seconds_per_gib']
    for ev in ['context-switches', 'sched:sched_wakeup', 'syscalls:sys_enter_futex', 'syscalls:sys_enter_sendmsg', 'net:net_dev_xmit']:
        out[f'{ev}/pkt'] = c[ev]['value'] / packets
    return out


def arm_of(meta, controller, prev, new):
    # #712's runner records the controller in the observation's summary, not in meta.json.
    v, c, t = meta['variant'], controller, meta.get('tag') or ''
    if v == 'reno':
        return 'aa' if t == 'aa' else 'reno'
    if v == prev and c == 'bbrv3':
        return 'prev'
    if v == new and c == 'bbrv3':
        return 'new'
    if v == new and c == 'reno':
        return 'renonew'
    raise ValueError(meta)


def load_stage(stage, prev, new):
    """Observations of one measurement, with a rerun block replacing an unusable original."""
    obs, integrity, failures = {}, True, []
    for phase in [stage, stage + 'rerun']:
        for receipt in sorted(run.OBS.glob(f'{phase}-S5-*/receipt.json')):
            d = receipt.parent
            meta = json.loads((d / 'meta.json').read_text())
            try:
                row = run.summarize(d)
            except AssertionError as err:  # retained, reported and never excluded silently
                integrity = False
                failures.append(dict(id=d.name, error=str(err)))
                continue
            usable = all(counter_quality(row[r]['counters']) for r in ['send', 'receive'])
            cfg = json.loads((d / 'config.json').read_text())
            window = [cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000, cfg['start_unix_ns'] + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000]
            cont = contamination(json.loads((d / 'host-cpu.json').read_text()), window)
            pk = forward_packets(d, row)
            m = dict(id=d.name, usable=usable, contamination=cont, forward_packets=pk, goodput_mbps=row['goodput_mbps'])
            if usable:
                m.update(send=endpoint_metrics(row, 'send', pk), receive=endpoint_metrics(row, 'receive', pk))
            obs.setdefault((phase, row['workload'], meta['pair']), {})[arm_of(meta, row['controller'], prev, new)] = m
    per_workload, report = {}, []
    for wl in ['stream', 'datagram']:
        blocks = []
        for b in range(1, 7):
            orig, rerun = obs.get((stage, wl, b), {}), obs.get((stage + 'rerun', wl, b))
            ok = lambda x: x is not None and len(x) == len(ARMS) and all(x[k]['usable'] for k in ARMS)
            chosen, source = (orig, 'original') if ok(orig) else ((rerun, 'rerun') if ok(rerun) else (None, 'unusable'))
            report.append(dict(workload=wl, block=b, source=source,
                               contaminated=[x['id'] for x in (chosen or orig).values() if x['contamination'].get('contaminated')]))
            if chosen:
                blocks.append(dict(chosen, block=b))
        per_workload[wl] = blocks
    return per_workload, integrity, failures, report


def sensitivity(per_workload, report, integrity):
    """Registered contamination sensitivity: recompute without blocks holding a contaminated observation."""
    dirty = {(r['workload'], r['block']) for r in report if r['contaminated']}
    if not dirty:
        return None
    clean = {wl: [b for b in bl if (wl, b['block']) not in dirty] for wl, bl in per_workload.items()}
    return analyze(clean, integrity)['outcome']


def main(stages):
    plan = json.loads((HERE / 'measurements.json').read_text())
    out = {}
    for stage in stages:
        prev, new = plan[stage]['prev'], plan[stage]['new']
        per_workload, integrity, failures, report = load_stage(stage, prev, new)
        res = analyze(per_workload, integrity)
        res.update(prev=prev, new=new, integrity_failures=failures, blocks=report,
                   contamination_sensitive_outcome=sensitivity(per_workload, report, integrity))
        out[stage] = res
        print(stage, prev, '->', new, res['outcome'], res['work'], {wl: r['work'] for wl, r in res['workloads'].items()}, res['reasons'])
    target = HERE / 'outcomes.json'
    prior = json.loads(target.read_text()) if target.exists() else {}
    prior.update(out)
    target.write_text(json.dumps(prior, indent=2) + '\n')


if __name__ == '__main__':
    main(sys.argv[1:])
