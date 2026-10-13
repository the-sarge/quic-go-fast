#!/usr/bin/env python3
"""#744 analysis: Stage 0's runtime synthetic cases and Stage 1's readings, under the registered rules.

Blocks are chosen by the operator's #714 rerun rule (rules.choose_block, #715's
copy): a block holding a contaminated or unusable observation is replaced by its
same-seed rerun only when the rerun is usable and uncontaminated. An observation
is usable when it exits cleanly with receiver integrity (#712's summarize) and,
for a recorder arm, when its recorder output passes ctl.usable with the queue
capacity its build selects. Contamination is #712's test (localize.py).

Usage: ctlstages.py syn|s1|all       (writes results.json beside this file)
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from run import summarize  # noqa: E402
import rules as R  # noqa: E402
import ctl  # noqa: E402
import ctlrules as C  # noqa: E402

ART = run.ART
ROOTS = [ART / 'observations', ART / 'observations-rerun']
OUT = HERE / 'results.json'
CAPACITY = {'cand-ctl': 32, 'cand8-ctl': 8, **{f'syn-{c}-ctl': 32 for c in ('sq', 'rq', 'sw', 'rl', 'rw', 'un')}}


def arm(meta, cfg):
    return '-'.join([meta['variant'], cfg['controller']] + ([meta['tag']] if meta.get('tag') else []))


def contaminated(d):
    from localize import contamination
    facts = json.loads((d / 'host-facts.json').read_text())
    return contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])['contaminated']


def usable(d):
    """(usable, reasons) for one observation directory."""
    try:
        receipt = json.loads((d / 'receipt.json').read_text())
        if receipt['exit_codes'] != [0, 0] or receipt.get('relay_exit') not in (None, 0):
            return False, ['exit codes']
        summarize(d)
        meta = json.loads((d / 'meta.json').read_text())
        for role in ['send', 'receive']:
            p = d / f'{role}.perf-stat.csv'
            if p.exists() and any(v['running_pct'] is not None and v['running_pct'] < 95 for v in run.perf_counters(p).values()):
                return False, [f'{role} perf counters multiplexed']
        if meta['variant'] in CAPACITY:
            ok, reasons, _, _ = ctl.usable(d, CAPACITY[meta['variant']])
            return ok, reasons
        return True, []
    except (AssertionError, FileNotFoundError, KeyError, json.JSONDecodeError) as e:
        return False, [repr(e)]


def blocks_of(phase, path):
    """{(workload, block): {'original': [dirs], 'rerun': [dirs]}} for one phase and path."""
    out = {}
    for label, root in zip(['original', 'rerun'], ROOTS):
        for p in sorted(root.glob(f'{phase}-{path}-*/receipt.json')):
            meta = json.loads((p.parent / 'meta.json').read_text())
            if meta['phase'] != phase or meta['path'] != path:
                continue
            cfg = json.loads((p.parent / 'config.json').read_text())
            out.setdefault((cfg['workload'], meta['pair']), {}).setdefault(label, []).append(p.parent)
    return out


def chosen(phase, path, arms):
    """Per block: the counted directories by arm name, its status and its attempts (the #714 rule)."""
    record = []
    for key, att in sorted(blocks_of(phase, path).items()):
        attempts = []
        for label in ['original', 'rerun']:
            if label not in att:
                continue
            ds = att[label]
            names = {arm(json.loads((d / 'meta.json').read_text()), json.loads((d / 'config.json').read_text())): d for d in ds}
            use = {n: usable(d) for n, d in names.items()}
            cont = {n: contaminated(d) for n, d in names.items()}
            complete = set(names) == set(arms)
            attempts.append((label, complete and all(u[0] for u in use.values()), any(cont.values())))
            att[label + '_detail'] = dict(dirs={n: str(d) for n, d in names.items()}, unusable={n: u[1] for n, u in use.items() if not u[0]},
                                          contaminated=sorted(n for n, c in cont.items() if c), missing=sorted(set(arms) - set(names)))
        pick, status = R.choose_block(attempts)
        record.append(dict(workload=key[0], block=key[1], status=status, counted=pick, attempts=attempts,
                           dirs=att[pick + '_detail']['dirs'], detail={k: v for k, v in att.items() if k.endswith('_detail')}))
    return record


def replies_ms(d):
    return [v / 1e6 for v in json.loads((Path(d) / 'send.json').read_text())['result']['control']['latency_ns']]


def recorder_rows(d, capacity):
    ok, reasons, rows, facts = ctl.usable(d, capacity)
    assert ok, reasons
    return rows, facts


def save(key, value):
    out = json.loads(OUT.read_text()) if OUT.exists() else {}
    out[key] = value
    OUT.write_text(json.dumps(out, indent=1, default=str) + '\n')


def seg_stats(rows):
    out = {}
    for s in ctl.SEGMENTS + ['total']:
        v = sorted(r[s] / 1000 for r in rows)
        out[s] = dict(p50_us=statistics.median(v), p95_us=R.pooled_p95(v), mean_us=statistics.fmean(v))
    for side in ('sender_queue', 'receiver_queue'):
        out[side] = {k: statistics.median(r[side][k] for r in rows) for k in ('entries_ahead', 'bytes_ahead', 'entry_bytes')}
        out[side]['bytes_ahead_p95'] = R.pooled_p95([r[side]['bytes_ahead'] for r in rows])
    return out


# ---- Stage 0: runtime synthetic cases ---------------------------------------------------------

SYN = {  # case arm -> (expected ending, check on the reading's detail and the median segment rises in us)
    'syn-sq-ctl-bbrv3': ('queue wait', lambda d, rise: C.targets(d) == ['sender'] and rise['s_queue'] >= 210),
    'syn-rq-ctl-bbrv3': ('queue wait', lambda d, rise: C.targets(d) == ['receiver'] and rise['r_queue'] >= 210),
    'syn-sw-ctl-bbrv3': ('queue wait', lambda d, rise: 'sender' in C.targets(d) and rise['s_submit'] >= 14),
    'syn-rl-ctl-bbrv3': ('load or other', lambda d, rise: 'r_backlog' in d['named_segments']),
    'syn-rw-ctl-bbrv3': ('load or other', lambda d, rise: 'r_app' in d['named_segments'] and rise['r_app'] >= 210
                         and not {'s_queue', 'r_queue'} & set(d['named_segments'])),
    'syn-un-ctl-bbrv3': ('load or other', lambda d, rise: 't_req' in d['named_segments']
                         and set(d['named_segments']) <= set(ctl.RESIDUAL)),
    'cand-ctl-bbrv3-aa': ('inconclusive', lambda d, rise: True),  # the near-zero p95 rise
}


def syn():
    import matrix
    arms = ['-'.join(a[:2] + ((a[2],) if a[2] else ())) for a in matrix.SYN_ARMS]
    record = chosen('syn', 'loopback-syn', arms)
    clean = [b for b in record if b['status'] == 'clean']
    res = dict(blocks=record, cases={})
    for case, (expected, check) in SYN.items():
        blocks = []
        for b in clean:
            a_rows, _ = recorder_rows(b['dirs'][case], 32)
            b_rows, _ = recorder_rows(b['dirs']['cand-ctl-bbrv3'], 32)
            blocks.append(dict(a=a_rows, b=b_rows, a_fixture=[r['fixture_latency_ns'] / 1e6 for r in a_rows],
                               b_fixture=[r['fixture_latency_ns'] / 1e6 for r in b_rows]))
        out, detail = C.mechanism_reading(blocks, (0.0, float('inf')), True, ctl.SEGMENTS, ctl.QUEUE_SEGMENTS, min_blocks=3)
        rise = {s: statistics.median(r[s] for x in blocks for r in x['a']) / 1000 - statistics.median(r[s] for x in blocks for r in x['b']) / 1000
                for s in ctl.SEGMENTS}
        recovered = out == expected and check(detail, rise) if out != 'evidence gap' else False
        res['cases'][case] = dict(expected=expected, ending=out, recovered=recovered, median_rise_us=rise,
                                  detail={k: v for k, v in detail.items() if k != 'segment_shares'},
                                  segment_shares=detail.get('segment_shares'))
        print('syn', case, out, 'recovered' if recovered else 'NOT RECOVERED', {k: round(v, 1) for k, v in rise.items() if abs(v) > 5})
    res['passes'] = all(c['recovered'] for c in res['cases'].values())
    save('stage0_synthetic_runtime', res)
    return res


# ---- Stage 1 ------------------------------------------------------------------------------------

S1_ARMS = ['reno-reno', 'cand-bbrv3', 'cand-bbrv3-aa', 'cand8-bbrv3', 'cand-ctl-bbrv3', 'cand8-ctl-bbrv3']


def s1():
    record = chosen('s1', 'loopback-long', S1_ARMS)
    clean = [b for b in record if b['status'] == 'clean']
    res = dict(blocks=[{k: v for k, v in b.items() if k != 'detail'} for b in record],
               block_detail=[b['detail'] for b in record], clean_blocks=len(clean))
    lat = [{a: replies_ms(b['dirs'][a]) for a in S1_ARMS} for b in clean]
    good = [{a: summarize(Path(b['dirs'][a]))['goodput_mbps'] for a in S1_ARMS} for b in clean]
    # Depth reading (plain arms).
    depth, ddet = C.depth_reading([dict(cand=x['cand-bbrv3'], cand8=x['cand8-bbrv3'], aa=x['cand-bbrv3-aa']) for x in lat])
    res['depth'] = dict(ending=depth, detail=ddet)
    # Perturbation (counted arms).
    pert = C.perturbation([dict(cand_goodput=g['cand-bbrv3'], cand_ctl_goodput=g['cand-ctl-bbrv3'], cand=x['cand-bbrv3'],
                                cand_ctl=x['cand-ctl-bbrv3'], cand8_goodput=g['cand8-bbrv3'], cand8_ctl_goodput=g['cand8-ctl-bbrv3'],
                                cand8=x['cand8-bbrv3'], cand8_ctl=x['cand8-ctl-bbrv3']) for x, g in zip(lat, good)])
    res['perturbation'] = pert
    # Mechanism reading (recorder arms).
    blocks, facts = [], []
    for b in clean:
        a_rows, fa = recorder_rows(b['dirs']['cand-ctl-bbrv3'], 32)
        b_rows, fb = recorder_rows(b['dirs']['cand8-ctl-bbrv3'], 8)
        blocks.append(dict(a=a_rows, b=b_rows, a_fixture=replies_ms(b['dirs']['cand-ctl-bbrv3']),
                           b_fixture=replies_ms(b['dirs']['cand8-ctl-bbrv3'])))
        facts.append(dict(cand=fa, cand8=fb))
    consistency = tuple(ddet['ci']) if depth != 'evidence gap' else None
    mech, mdet = C.mechanism_reading(blocks, consistency, pert['passes'], ctl.SEGMENTS, ctl.QUEUE_SEGMENTS)
    res['mechanism'] = dict(ending=mech, detail=mdet, recorder_facts=facts)
    res['descriptive'] = dict(
        goodput_mbps={a: statistics.median(g[a] for g in good) for a in S1_ARMS},
        p95_ms={a: R.pooled_p95([v for x in lat for v in x[a]]) for a in S1_ARMS},
        p50_ms={a: statistics.median([v for x in lat for v in x[a]]) for a in S1_ARMS},
        segments={'cand-ctl': seg_stats([r for x in blocks for r in x['a']]),
                  'cand8-ctl': seg_stats([r for x in blocks for r in x['b']])} if blocks else None)
    # Baseline reading: #715's latency_preservation, unchanged, for depth 8 against frozen Reno.
    out, bdet = C.latency_preservation([dict(cand=x['cand8-bbrv3'], reno=x['reno-reno']) for x in lat])
    res['baseline'] = dict(ending={'regression reproduced': 'BBR baseline', 'not reproduced': 'not reproduced'}.get(out, out),
                           rule_outcome=out, detail=bdet)
    # r9 against frozen Reno: descriptive only (Stage 1 cannot revise #740's verdict).
    _, rdet = C.latency_preservation([dict(cand=x['cand-bbrv3'], reno=x['reno-reno']) for x in lat])
    res['r9_vs_reno_descriptive'] = {k: rdet.get(k) for k in ('ratio', 'ci', 'cand_p95_ms', 'reno_p95_ms', 'cand_p50_ms', 'reno_p50_ms')}
    # Stage 2 gate and, if it opens, the registered bound at each targeted endpoint.
    res['stage2_gate'] = depth == 'depth raises latency' and mech == 'queue wait'
    if res['stage2_gate']:
        a_all, b_all = [r for x in blocks for r in x['a']], [r for x in blocks for r in x['b']]
        res['stage2_targets'] = C.targets(mdet)
        res['stage2_bounds'] = {side: C.stage2_bound(a_all, b_all, side)[1] for side in ('sender', 'receiver')}
    save('stage1', res)
    print('s1 depth', depth, {k: ddet.get(k) for k in ('ratio', 'ci', 'aa_ci', 'blocks_above_1')})
    print('s1 perturbation', {r: {k: v for k, v in pert[r].items() if k != 'goodput_per_block'} for r in ('cand', 'cand8')})
    print('s1 mechanism', mech, mdet.get('reason'), mdet.get('queue_share'), mdet.get('endpoint_shares'))
    print('s1 baseline', res['baseline']['ending'], {k: bdet.get(k) for k in ('ratio', 'ci')})
    return res


STEPS = dict(syn=syn, s1=s1)

if __name__ == '__main__':
    for k in (list(STEPS) if sys.argv[1] == 'all' else sys.argv[1:]):
        STEPS[k]()
