#!/usr/bin/env python3
"""#740 analysis: the ring preflight, Stage 1 memory cells, the S6 control p95 rule and perturbation.

Rules are memrules.py's (tested by memrules_test.py). Observation selection is
#739's stages.py, unchanged: the operator's same-seed rerun rule
(stages.chosen), the contamination test and the usability test. A block counts
for fractions only when its counted attempt is clean (every arm usable and none
contaminated). Summaries use mem_run.summarize_m (the receiver-controller assertion).
Readiness flags, matched-load S5 p95 and the readiness goodput
used for perturbation are read from #739's summary.json.

Usage (minimax, before Stage 1):   memstages.py preflight [phase]   (default memsmoke, the registered one)
Anywhere, after the stages:         memstages.py perturbation|cells|p95|latency|all
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import art  # noqa: F401,E402
import run  # noqa: E402
import stages as S  # noqa: E402
import mem_run  # noqa: E402
import memrules as M  # noqa: E402

ART = run.ART
OUT = HERE / 'memory.json'
R9_SUMMARY = HERE.parent / '2026-10-08-bbr-r9-linux-redemonstration' / 'summary.json'
RING_ENTRIES = 4096
# The receiver-controller arm's receiver reports Reno; summarize_m checks it (mem_run.py).
S.summarize = run.summarize = mem_run.summarize_m
ARMS = dict(reno='reno-mem-reno', cand='cand-mem-bbrv3', aa='cand-mem-bbrv3-aa', ring='cand-mem-ring-bbrv3', rx='cand-mem-bbrv3-rxreno')


def save(key, value):
    out = json.loads(OUT.read_text()) if OUT.exists() else {}
    out[key] = value
    OUT.write_text(json.dumps(out, indent=1, default=str) + '\n')


def series(d, role):
    return M.parse_series((d / f'{role}.mem.jsonl').read_text())


def gate_status(name):
    log = HERE / 'gates' / f'{name}.log'
    if not log.exists():
        return None
    last = [x for x in log.read_text().splitlines() if x.startswith('# status')]
    return int(last[-1].split()[-1]) if last else None


# ---- Stage 0 ------------------------------------------------------------------------------------

def preflight(phase='memsmoke'):
    """Gates, the injected violations, the activation reduction and the instrument checks; writes ring-preflight.json."""
    gates = {n: gate_status(n) for n in ['cand-mem', 'cand-mem-ring', 'gate-ring-m1', 'gate-ring-m2']}
    native = (HERE / 'gates' / 'cand-mem-ring-native.log')
    native_ok = native.exists() and 'FAIL' not in native.read_text() and 'exit 0' in native.read_text()
    obs = {}
    for p in sorted((ART / 'observations').glob(f'{phase}-*/receipt.json')):
        d = p.parent
        r = S.summarize(d)
        e = dict(goodput_mbps=r['goodput_mbps'], usable=S.usable(d), contamination=S.contaminated(d)['contaminated'])
        for role in ['send', 'receive']:
            h, rows, tail = series(d, role)
            a = M.peak_account(rows, r[role]['peak_rss_mib'])
            cfg, _ = S.window(d)
            e[role] = dict(samples=len(rows), expected_s=(cfg['warmup_ms'] + cfg['measure_ms']) / 1000,
                           controller_override=(tail or {}).get('controller_override', h.get('controller_override')),
                           phases=len((tail or {}).get('phases') or []), ring_bytes_max=max(x.get('ring_bytes', 0) for x in rows),
                           ring_entries=a['ring_entries'], bbr_explicit_mib=a['bbr_explicit'], peak=a['peak'], rss=a['rss'],
                           residual=a['residual'], gap=a['gap'])
        obs[S.arm(r) + ('' if r['path'] == 'S5' else '-' + r['path'])] = e
    cand, ring, rx = obs.get(ARMS['cand']), obs.get(ARMS['ring']), obs.get(ARMS['rx'])
    activation = dict(candidate_ring_bytes=cand['send']['ring_bytes_max'] if cand else None,
                      ring_ring_bytes=ring['send']['ring_bytes_max'] if ring else None,
                      ring_entries=ring['send']['ring_entries'] if ring else None)
    gates_pass = gates['cand-mem-ring'] == 0 and gates['cand-mem'] == 0 and native_ok
    if not (cand and ring):
        decision = dict(decision='unusable', predicted_mib=None, reason='activation run missing')
    else:
        decision = M.ring_preflight(gates_pass, [gates['gate-ring-m1'] not in (0, None), gates['gate-ring-m2'] not in (0, None)],
                                    activation['ring_ring_bytes'], activation['candidate_ring_bytes'])
        if decision['decision'] == 'run' and activation['ring_entries'] != RING_ENTRIES:
            decision = dict(decision='inert', predicted_mib=decision['predicted_mib'], reason='ring entries differ')
    instrument = dict(
        receiver_controller=dict(override=rx['receive']['controller_override'] if rx else None,
                                 receiver_ring_bytes_max=rx['receive']['ring_bytes_max'] if rx else None,
                                 receiver_phases=rx['receive']['phases'] if rx else None,
                                 sender_ring_bytes_max=rx['send']['ring_bytes_max'] if rx else None,
                                 engaged=bool(rx and rx['receive']['controller_override'] == 'reno' and rx['receive']['ring_bytes_max'] == 0
                                              and rx['send']['ring_bytes_max'] > 0)),
        coverage={k: {role: v[role]['samples'] / v[role]['expected_s'] for role in ['send', 'receive']} for k, v in obs.items()},
        s6_phases=obs.get(ARMS['cand'] + '-S6', {}).get('send', {}).get('phases'))
    res = dict(gates=gates, native_gates=native_ok, activation=activation, instrument=instrument, observations=obs, **decision,
               threshold_mib=M.DETECTABLE_MIB)
    if phase != 'memsmoke':  # an instrument-development attempt: never the registered decision
        (ART / f'ring-preflight-{phase}.json').write_text(json.dumps(res, indent=1) + '\n')
        return print(json.dumps({k: v for k, v in res.items() if k != 'observations'}, indent=1))
    (ART / 'ring-preflight.json').write_text(json.dumps(res, indent=1) + '\n')
    save('preflight', res)
    print(json.dumps({k: v for k, v in res.items() if k != 'observations'}, indent=1))


# ---- Stage 1 ------------------------------------------------------------------------------------

def readiness_flags():
    s = json.loads(R9_SUMMARY.read_text())
    flags = {}
    for e in s['readiness']:
        if e['arm'] != 'cand-bbrv3':
            continue
        for kind, role in [('send_rss', 'send'), ('receive_rss', 'receive')]:
            f = e['flags'].get(kind)
            if f:
                flags[(e['path'], e['workload'], role)] = dict(value=f['median'], raised=f['raised'])
    return flags


def observe(d):
    r = S.summarize(d)
    out = dict(dir=str(d), arm=S.arm(r), block=r['pair'], workload=r['workload'], path=r['path'], usable=S.usable(d),
               goodput=r['goodput_mbps'], traffic=M.traffic(r), cfg=S.window(d)[0], summary=r)
    for role in ['send', 'receive']:
        _, rows, tail = series(d, role)
        out[role] = dict(rows=rows, peak=M.peak_account(rows, r[role]['peak_rss_mib']), tail=tail)
    return out


def peak_occupancy_mib(o, role):
    """Receive occupancy (connection bytes received but unread, largest since the previous sample) at the peak sample."""
    t = o[role]['peak']['unix_ns']
    row = next(x for x in o[role]['rows'] if x['unix_ns'] == t)
    return row.get('conn_occ_max', 0) * M.MIB


def cells(paths=('S5', 'S6', 'loopback')):
    perturbed = {(x['path'], x['workload']) for x in perturbation_ratios() if x['arm'] == ARMS['cand'] and not x['passes']}
    pre = json.loads((ART / 'ring-preflight.json').read_text())
    ring_status = pre['decision'] if pre['decision'] != 'run' else 'run'
    flags = readiness_flags()
    result, records = [], []
    for path in paths:
        dirs, record = S.chosen('mem', path)
        records += record
        if not dirs:
            continue
        status = {(x['workload'], x['block']): x['status'] for x in record}
        obs = [observe(d) for d in dirs]
        for workload in ['stream', 'datagram']:
            blocks = {}
            for o in obs:
                if o['workload'] == workload:
                    blocks.setdefault(o['block'], {})[next(k for k, v in ARMS.items() if v == o['arm'])] = o
            # The A/A band for comparability: per measure, the largest A/A deviation over this cell's blocks.
            band = M.aa_band([dict(cand=b['cand']['traffic'], aa=b['aa']['traffic']) for b in blocks.values() if 'cand' in b and 'aa' in b])
            for role in ['send', 'receive']:
                rows = []
                for bn, b in sorted(blocks.items()):
                    clean = status.get((workload, bn)) == 'clean' and all(o['usable'] for o in b.values())
                    c, n = b['cand'], b['reno']
                    lw = M.window_live(c[role]['rows'], n[role]['rows'], c['cfg']['start_unix_ns'], n['cfg']['start_unix_ns'],
                                       c['cfg']['warmup_ms'], c['cfg']['measure_ms'])
                    ex = M.excess(c[role]['peak'], n[role]['peak'], lw)
                    row = dict(block=bn, E=ex['peak'], aa_diff=b['aa'][role]['peak']['peak'] - c[role]['peak']['peak'], usable=clean,
                               ex=ex, ring=None, rx=None, ring_engaged=False, ring_comparable=False, rx_comparable=False, occ_excess=None,
                               bbr_explicit_cand=c[role]['peak']['bbr_explicit'], ring_bytes_cand=c[role]['peak']['ring_bytes'],
                               explained=M.explained(ex), dominant=M.dominant(ex))
                    if 'ring' in b:
                        g = b['ring']
                        row['ring'] = c[role]['peak']['peak'] - g[role]['peak']['peak']
                        row['ring_engaged'] = M.ring_engaged(g['send']['peak'], c['send']['peak'], RING_ENTRIES)
                        row['ring_comparable'], row['ring_outside'] = M.comparable(c['traffic'], g['traffic'], band)
                    if 'rx' in b:
                        x = b['rx']
                        row['rx'] = c[role]['peak']['peak'] - x[role]['peak']['peak']
                        rx_ok = x['receive']['tail'] and x['receive']['tail'].get('controller_override') == 'reno' \
                            and max(r.get('ring_bytes', 0) for r in x['receive']['rows']) == 0
                        comp, outside = M.comparable(c['traffic'], x['traffic'], band)
                        row['rx_comparable'], row['rx_outside'], row['rx_engaged'] = bool(comp and rx_ok), outside, bool(rx_ok)
                    if role == 'receive':
                        row['occ_excess'] = peak_occupancy_mib(c, role) - peak_occupancy_mib(n, role)
                    rows.append(row)
                flag = flags.get((path, workload, role))
                label = M.label_cell(role, rows, ring_status) if flag and flag['raised'] else None
                if label and (path, workload) in perturbed:
                    # D6: instrumented candidate goodput below 0.99 of plain; the operator's loopback mapping.
                    label = dict(label='evidence gap (instrument perturbation)', rule_label=label['label'])
                result.append(dict(path=path, workload=workload, role=role, readiness=flag, label=label, band=band,
                                   blocks=rows, descriptive=describe(rows)))
    save('cells', result)
    save('blocks', records)
    for c in result:
        if c['label']:
            print(c['path'], c['workload'], c['role'], c['readiness']['value'], c['label']['label'])


def describe(rows):
    keys = M.CLASSES + M.UNEXPLAINED + ['live', 'heap', 'bbr_explicit', 'ring_bytes']
    med = lambda v: statistics.median(v) if v else None
    out = {k: med([r['ex'][k] for r in rows]) for k in keys}
    out.update(E=med([r['E'] for r in rows]), aa=med([abs(r['aa_diff']) for r in rows]),
               ring=med([r['ring'] for r in rows if r['ring'] is not None]), rx=med([r['rx'] for r in rows if r['rx'] is not None]),
               live_window=med([r['ex']['live_window'] for r in rows if r['ex']['live_window'] is not None]),
               bbr_explicit_cand=med([r['bbr_explicit_cand'] for r in rows]))
    return out


# ---- S6 control p95 -------------------------------------------------------------------------------

def p95():
    s = json.loads(R9_SUMMARY.read_text())
    s5 = {e['workload']: e['flags']['control_p95']['median'] for e in s['readiness'] if e['path'] == 'S5' and e['arm'] == 'cand-bbrv3'}
    s6 = {e['workload']: e['flags']['control_p95'] for e in s['readiness'] if e['path'] == 'S6' and e['arm'] == 'cand-bbrv3'}
    dirs, _ = S.chosen('mem', 'S6')
    res = {}
    for workload in ['stream', 'datagram']:
        runs = []
        for d in dirs:
            r = S.summarize(d)
            if r['workload'] != workload or S.arm(r) not in (ARMS['cand'], ARMS['aa']):
                continue
            _, _, tail = series(d, 'send')
            cfg, _ = S.window(d)
            relay = json.loads((d / 'relay.json').read_text())
            q = M.queue_by_phase((tail or {}).get('phases') or [], relay['QueueSamples'], cfg['start_unix_ns'], cfg['warmup_ms'], cfg['measure_ms'])
            runs.append(dict(id=r['id'], control_p95_ms=r['control_p95_ms'], **q))
        res[workload] = dict(readiness=dict(value=s6[workload]['median'], raised=s6[workload]['raised']), s5_matched_p95=s5[workload],
                             attribution=M.up_policy(runs, s5[workload]) if s6[workload]['raised'] else 'passes', runs=runs)
        print(workload, res[workload]['readiness'], res[workload]['attribution'])
    save('s6_p95', res)


# ---- perturbation -----------------------------------------------------------------------------------

def perturbation():
    res = perturbation_ratios()
    save('perturbation', res)
    for x in res:
        print(x)


def perturbation_ratios():
    s = json.loads(R9_SUMMARY.read_text())
    plain = {}
    for o in s['observations']:
        if o['phase'] == 'main' and o['pair'] <= 4 and o['tag'] == '' and (o['variant'], o['controller']) in (('cand', 'bbrv3'), ('reno', 'reno')):
            plain.setdefault((o['path'], o['workload'], o['variant']), []).append(o['goodput_mbps'])
    res = []
    for path in ['S5', 'S6', 'loopback']:
        dirs, _ = S.chosen('mem', path)
        rows = [S.summarize(d) for d in dirs]
        for workload in ['stream', 'datagram']:
            for arm_name, variant in [(ARMS['cand'], 'cand'), (ARMS['reno'], 'reno')]:
                inst = [r['goodput_mbps'] for r in rows if r['workload'] == workload and S.arm(r) == arm_name]
                if inst and plain.get((path, workload, variant)):
                    ratio = M.perturbation(inst, plain[(path, workload, variant)])
                    res.append(dict(path=path, workload=workload, arm=arm_name, ratio=ratio, passes=ratio >= 0.99))
    return res


# ---- the candidate's loopback STREAM control p95 (operator decision; #715's preslat rule, unchanged) ----

def latency():
    s = json.loads(R9_SUMMARY.read_text())
    flag = next(e['flags']['control_p95'] for e in s['readiness'] if (e['path'], e['workload'], e['arm']) == ('loopback', 'stream', 'cand-bbrv3'))
    dirs, record = S.chosen('latcand', 'loopback-long')
    by = {}
    for d in dirs:
        r = S.summarize(d)
        lat = [x / 1e6 for x in json.loads((d / 'send.json').read_text())['result']['control']['latency_ns']]
        by.setdefault(r['pair'], {})[S.arm(r)] = lat
    status = {x['block']: x['status'] for x in record}
    blocks = [dict(cand=v['cand-bbrv3'], reno=v['reno-reno']) if status.get(b) == 'clean' and {'cand-bbrv3', 'reno-reno'} <= set(v) else None
              for b, v in sorted(by.items())]
    outcome, detail = S.R.latency_preservation(blocks)
    res = dict(readiness=dict(value=flag['median'], raised=flag['raised']), outcome=outcome, detail=detail, blocks=record,
               blocking='no (r9, Linux only)' if outcome == 'not reproduced' else 'yes, unresolved')
    save('latency', res)
    print('latcand', outcome, res['blocking'], {k: v for k, v in detail.items() if not isinstance(v, list)})


STEPS = dict(preflight=preflight, cells=cells, p95=p95, perturbation=perturbation, latency=latency)

if __name__ == '__main__':
    if sys.argv[1] == 'all':
        for k in ['perturbation', 'cells', 'p95', 'latency']:
            STEPS[k]()
    else:
        STEPS[sys.argv[1]](*sys.argv[2:])
