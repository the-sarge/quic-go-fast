#!/usr/bin/env python3
"""#740 analysis: the ring preflight, Stage 1 memory cells, the S6 control p95 rule and perturbation.

Rules are memrules.py's (tested by memrules_test.py; the orchestration here by
memstages_test.py). Observation selection is #739's stages.py, unchanged: the
operator's same-seed rerun rule (stages.chosen), the contamination test and the
usability test. A block counts only when its counted attempt is clean, every
expected arm is present and usable, and the memory series is valid; anything
else is recorded with its reason and every expected cell still ends with a
registered label. Calibration, perturbation and S6 scoring use clean blocks only. Summaries use mem_run.summarize_m (the receiver-controller assertion).
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
            a = M.peak_account(rows, r[role]['peak_rss_mib']) or dict(ring_entries=None, bbr_explicit=None, peak=None, rss=None,
                                                                      residual=None, gap=None)
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

PATHS = ('S5', 'S6', 'loopback')
GAP_PERTURBATION = 'instrumentation gap (perturbation)'


def readiness_flags():
    """{(path, workload, role): dict(value, raised)} for the candidate's RSS cells in #739's readiness."""
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


def expected_arms(path, ring_status):
    keys = ['reno', 'cand', 'aa'] + (['ring'] if path != 'loopback' and ring_status == 'run' else []) + ['rx']
    return keys


def observe(d):
    """One counted observation, loaded with validation; never raises for a malformed or failed observation."""
    out = dict(dir=str(d))
    try:
        meta = json.loads((d / 'meta.json').read_text())
        cfg = json.loads((d / 'config.json').read_text())
        out.update(block=meta['pair'], workload=cfg['workload'], path=meta['path'],
                   arm='-'.join([meta['variant'], cfg['controller']] + ([meta['tag']] if meta.get('tag') else [])))
        if not S.usable(d):
            return dict(out, usable=False, error='unusable (receipt, summary or perf check)')
        r = S.summarize(d)
        rows, tails, peaks = {}, {}, {}
        for role in ['send', 'receive']:
            _, rows[role], tails[role] = series(d, role)
            peaks[role] = M.peak_account(rows[role], r[role]['peak_rss_mib'])
            if peaks[role] is None:
                return dict(out, usable=False, error=f'no valid {role} memory sample')
        out.update(usable=True, goodput=r['goodput_mbps'], cfg=cfg, summary=r, rows=rows, tails=tails,
                   send=dict(peak=peaks['send'], rows=rows['send'], tail=tails['send']),
                   receive=dict(peak=peaks['receive'], rows=rows['receive'], tail=tails['receive']),
                   traffic=M.traffic(r, rows['send'], rows['receive'], cfg['start_unix_ns'], cfg['warmup_ms'], cfg['measure_ms']))
        return out
    except (FileNotFoundError, json.JSONDecodeError, KeyError, ValueError, IndexError, TypeError, AssertionError) as e:
        return dict(out, usable=False, error=repr(e))


def group(obs):
    """{(workload, block): {arm key: observation}} using ARMS' keys; unknown arms are kept under their name."""
    inv = {v: k for k, v in ARMS.items()}
    out = {}
    for o in obs:
        if 'workload' in o:
            out.setdefault((o['workload'], o['block']), {})[inv.get(o['arm'], o['arm'])] = o
    return out


def assemble(path, grouped, status, flags, ring_status, perturbed, workloads=('stream', 'datagram')):
    """Every cell of one path: per-block rows, the A/A band, and, for a raised cell, its registered ending."""
    want = expected_arms(path, ring_status)
    result = []
    for workload in workloads:
        blocks = {b: g for (w, b), g in grouped.items() if w == workload}
        clean = {b: g for b, g in blocks.items()
                 if status.get((workload, b)) == 'clean' and all(k in g and g[k]['usable'] for k in want)}
        band = M.aa_band([dict(cand=g['cand']['traffic'], aa=g['aa']['traffic']) for g in clean.values()],
                         M.TRAFFIC_WAN if path != 'loopback' else M.TRAFFIC)
        for role in ['send', 'receive']:
            rows = []
            for bn in sorted(blocks):
                g = blocks[bn]
                row = dict(block=bn, usable=bn in clean, status=status.get((workload, bn)),
                           missing=[k for k in want if k not in g], errors={k: g[k].get('error') for k in g if not g[k]['usable']})
                if bn not in clean:
                    row.update(E=0.0, ex=None)
                    rows.append(row)
                    continue
                c, n = g['cand'], g['reno']
                win, cov = M.window_medians(c[role]['rows'], n[role]['rows'], c['cfg']['start_unix_ns'], n['cfg']['start_unix_ns'],
                                            c['cfg']['warmup_ms'], c['cfg']['measure_ms'])
                ex = M.excess(c[role]['peak'], n[role]['peak'], win, cov)
                row.update(E=ex['peak'], aa_diff=g['aa'][role]['peak']['peak'] - c[role]['peak']['peak'], ex=ex,
                           ring=None, rx=None, ring_engaged=False, ring_comparable=False, rx_comparable=False,
                           bbr_explicit_cand=c[role]['peak']['bbr_explicit'], explained=M.explained(ex) if ex['peak'] > 0 else False,
                           dominant=M.dominant(ex))
                if not M.window_ok(ex):
                    row['usable'] = False
                    row['errors'] = dict(row['errors'], window='memory window coverage below the registered share')
                if 'ring' in g:
                    row['ring'] = c[role]['peak']['peak'] - g['ring'][role]['peak']['peak']
                    row['ring_engaged'] = M.ring_engaged(g['ring']['send']['peak'], c['send']['peak'], RING_ENTRIES)
                    row['ring_comparable'], row['ring_outside'] = M.comparable(c['traffic'], g['ring']['traffic'], band)
                if 'rx' in g:
                    x = g['rx']
                    row['rx'] = c[role]['peak']['peak'] - x[role]['peak']['peak']
                    engaged = bool(x['receive']['tail'] and x['receive']['tail'].get('controller_override') == 'reno'
                                   and max(r.get('ring_bytes', 0) for r in x['receive']['rows']) == 0)
                    comp, outside = M.comparable(c['traffic'], x['traffic'], band)
                    row.update(rx_comparable=bool(comp and engaged), rx_outside=outside, rx_engaged=engaged)
                rows.append(row)
            flag = flags.get((path, workload, role))
            label = None
            if flag and flag['raised']:
                if (path, workload) in perturbed:
                    label = dict(label=GAP_PERTURBATION, perturbation=perturbed[(path, workload)])
                elif not blocks:
                    label = dict(label='evidence gap (stage not run or no observation)')
                else:
                    label = M.label_cell(role, [r for r in rows if r.get('ex') is not None] +
                                         [dict(r, E=0.0) for r in rows if r.get('ex') is None], ring_status)
            result.append(dict(path=path, workload=workload, role=role, readiness=flag, label=label, band=band,
                               blocks=[{k: v for k, v in r.items()} for r in rows],
                               descriptive=describe([r for r in rows if r.get('ex') is not None])))
    return result


# Registered blocks per phase and path: (workloads, blocks).
LAYOUT = {('mem', 'S5'): (('stream', 'datagram'), 4), ('mem', 'S6'): (('stream', 'datagram'), 4),
          ('mem', 'loopback'): (('stream', 'datagram'), 4), ('latcand', 'loopback-long'): (('stream',), 6)}


def arm_count(phase, path, ring_status):
    return 2 if phase == 'latcand' else len(expected_arms(path, ring_status))


def choose(phase, path, arms):
    """stages.chosen with completeness: an attempt is usable only when it holds every expected arm and each is usable.

    Every registered block appears in the record; a block with no attempt at all has status 'missing'.
    Returns (counted dirs, record).
    """
    found = S.blocks_of(phase, path)
    workloads, n = LAYOUT[(phase, path)]
    dirs, record = [], []
    for workload in workloads:
        for b in range(1, n + 1):
            att = found.get((path, workload, b), {})
            attempts = []
            for label in ['original', 'rerun']:
                if label in att:
                    ds = att[label]
                    cont = [S.contaminated(d)['contaminated'] for d in ds]
                    attempts.append((label, len(ds) >= arms and all(S.usable(d) for d in ds), any(cont)))
            if not attempts:
                record.append(dict(path=path, workload=workload, block=b, attempts=[], counted=None, status='missing'))
                continue
            pick, status = S.R.choose_block(attempts)
            dirs += att[pick]
            record.append(dict(path=path, workload=workload, block=b, attempts=attempts, counted=pick, status=status))
    return dirs, record


def ring_decision():
    return json.loads((ART / 'ring-preflight.json').read_text())['decision']


def stage_obs(phase, path):
    dirs, record = choose(phase, path, arm_count(phase, path, ring_decision()))
    status = {(x['workload'], x['block']): x['status'] for x in record}
    return [observe(d) for d in dirs], status, record


def cells(paths=PATHS):
    pre = json.loads((ART / 'ring-preflight.json').read_text())
    ring_status = pre['decision']
    flags = readiness_flags()
    perturbed = {(x['path'], x['workload']): x for x in perturbation_ratios() if not x['passes']}
    result, records = [], []
    for path in paths:
        obs, status, record = stage_obs('mem', path)
        records += record
        result += assemble(path, group(obs), status, flags, ring_status, perturbed)
    save('cells', result)
    save('blocks', records)
    for c in result:
        if c['label']:
            print(c['path'], c['workload'], c['role'], c['readiness']['value'], c['label']['label'])


def describe(rows):
    keys = M.CLASSES + M.UNEXPLAINED + ['live', 'heap', 'bbr_explicit', 'ring_bytes', 'occupancy']
    med = lambda v: statistics.median(v) if v else None
    out = {k: med([r['ex'][k] for r in rows]) for k in keys}
    out.update(E=med([r['E'] for r in rows]), aa=med([abs(r['aa_diff']) for r in rows]),
               ring=med([r['ring'] for r in rows if r.get('ring') is not None]), rx=med([r['rx'] for r in rows if r.get('rx') is not None]),
               live_window=med([r['ex']['live_window'] for r in rows if r['ex']['live_window'] is not None]),
               objects_window=med([r['ex']['objects_window'] for r in rows if r['ex']['objects_window'] is not None]),
               bbr_explicit_cand=med([r['bbr_explicit_cand'] for r in rows]))
    return out


# ---- S6 control p95 -------------------------------------------------------------------------------

def p95():
    s = json.loads(R9_SUMMARY.read_text())
    s5 = {e['workload']: e['flags']['control_p95']['median'] for e in s['readiness'] if e['path'] == 'S5' and e['arm'] == 'cand-bbrv3'}
    s6 = {e['workload']: e['flags']['control_p95'] for e in s['readiness'] if e['path'] == 'S6' and e['arm'] == 'cand-bbrv3'}
    perturbed = {(x['path'], x['workload']) for x in perturbation_ratios() if not x['passes']}
    obs, status, _ = stage_obs('mem', 'S6')
    grouped = group(obs)
    res = {}
    for workload in ['stream', 'datagram']:
        runs = []
        for (w, b), g in sorted(grouped.items()):
            if w != workload or status.get((w, b)) != 'clean':
                continue
            for k in ('cand', 'aa'):
                o = g.get(k)
                if not o or not o['usable']:
                    continue
                relay = json.loads((Path(o['dir']) / 'relay.json').read_text())
                q = M.queue_by_phase((o['send']['tail'] or {}).get('phases') or [], relay['QueueSamples'], o['cfg']['start_unix_ns'],
                                     o['cfg']['warmup_ms'], o['cfg']['measure_ms'])
                runs.append(dict(id=o['summary']['id'], control_p95_ms=o['summary']['control_p95_ms'], **(q or dict(missing='phase log'))) if q else None)
        if not s6[workload]['raised']:
            attribution = 'passes'
        elif ('S6', workload) in perturbed:
            attribution = GAP_PERTURBATION
        else:
            attribution = M.up_policy(runs, s5[workload], required_runs=8)
        res[workload] = dict(readiness=dict(value=s6[workload]['median'], raised=s6[workload]['raised']), s5_matched_p95=s5[workload],
                             attribution=attribution, runs=runs)
        print(workload, res[workload]['readiness'], attribution)
    save('s6_p95', res)


# ---- perturbation -----------------------------------------------------------------------------------

def perturbation():
    res = perturbation_ratios()
    save('perturbation', res)
    for x in res:
        print(x)


def perturbation_ratios():
    """Per path, workload and build: instrumented ÷ plain goodput medians over clean blocks (both arms gate)."""
    s = json.loads(R9_SUMMARY.read_text())
    plain = {}
    for o in s['observations']:
        if o['phase'] == 'main' and o['pair'] <= 4 and o['tag'] == '' and (o['variant'], o['controller']) in (('cand', 'bbrv3'), ('reno', 'reno')):
            plain.setdefault((o['path'], o['workload'], o['variant']), []).append(o['goodput_mbps'])
    res = []
    for path in PATHS:
        obs, status, _ = stage_obs('mem', path)
        grouped = group(obs)
        for workload in ['stream', 'datagram']:
            for key, variant in [('cand', 'cand'), ('reno', 'reno')]:
                inst = [g[key]['goodput'] for (w, b), g in grouped.items()
                        if w == workload and status.get((w, b)) == 'clean' and key in g and g[key]['usable']]
                ratio = M.perturbation(inst, plain.get((path, workload, variant), []))
                res.append(dict(path=path, workload=workload, arm=ARMS[key], ratio=ratio, blocks=len(inst),
                                passes=ratio is not None and ratio >= 0.99))
    return res


# ---- the candidate's loopback STREAM control p95 (operator decision; #715's preslat rule, unchanged) ----

def latency():
    s = json.loads(R9_SUMMARY.read_text())
    flag = next(e['flags']['control_p95'] for e in s['readiness'] if (e['path'], e['workload'], e['arm']) == ('loopback', 'stream', 'cand-bbrv3'))
    dirs, record = choose('latcand', 'loopback-long', 2)
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
