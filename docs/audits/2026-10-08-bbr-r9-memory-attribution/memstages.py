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

def instrument_check(d):
    """(valid, reason) for both endpoints' memory series of one observation (second consideration, F1)."""
    try:
        cfg = json.loads((d / 'config.json').read_text())
        for role in ['send', 'receive']:
            _, rows, _ = series(d, role)
            ok, why = M.instrument_valid(rows, cfg['start_unix_ns'], cfg['warmup_ms'], cfg['measure_ms'])
            if not ok:
                return False, f'{role}: {why}'
        return True, None
    except (OSError, ValueError, KeyError, IndexError, TypeError) as e:
        return False, repr(e)


def load_smoke(d):
    """One activation or instrument observation for the preflight; never raises."""
    try:
        r = S.summarize(d)
        e = dict(goodput_mbps=r['goodput_mbps'], usable=S.usable(d), path=r['path'], arm=S.arm(r))
        e['instrument_valid'], e['instrument_reason'] = instrument_check(d)
        for role in ['send', 'receive']:
            h, rows, tail = series(d, role)
            a = M.peak_account(rows, r[role]['peak_rss_mib']) or {}
            e[role] = dict(controller_override=(tail or {}).get('controller_override', h.get('controller_override')),
                           phases=len((tail or {}).get('phases') or []), ring_bytes_max=max((x.get('ring_bytes', 0) for x in rows), default=0),
                           ring_entries=a.get('ring_entries'), bbr_explicit_mib=a.get('bbr_explicit'), peak=a.get('peak'),
                           packets_sent_last=max((x.get('stats_packets_sent', -1) for x in rows), default=-1))
        return e
    except (OSError, ValueError, KeyError, IndexError, TypeError, AssertionError) as err:
        return dict(usable=False, instrument_valid=False, instrument_reason=repr(err), dir=str(d))


def preflight(phase='memsmoke'):
    """Gates, injected violations, the activation reduction and the instrument prerequisites; writes ring-preflight.json once.

    The ring decision is 'unusable' when a gate fails, a mutant is undetected, or the activation evidence (the
    candidate and ring runs) is missing or invalid; 'inert' when valid evidence shows no reduction. The instrument
    prerequisite (all five memsmoke runs present, usable and instrument-valid, the receiver-controller arm engaged,
    phases logged on S6, packet counters advancing) is separate: if it fails, this exits with status 3 and the driver
    stops before Stage 1 (a prerequisite gap), whatever the ring decision.
    """
    out = ART / ('ring-preflight.json' if phase == 'memsmoke' else f'ring-preflight-{phase}.json')
    if out.exists():
        raise SystemExit(f'{out} exists; the preflight decision is never revised')
    gates = {n: gate_status(n) for n in ['cand-mem', 'cand-mem-ring', 'gate-ring-m1', 'gate-ring-m2']}
    native = HERE / 'gates' / 'cand-mem-ring-native.log'
    native_ok = native.exists() and 'FAIL' not in native.read_text() and 'exit 0' in native.read_text()
    obs = {}
    for d in sorted(x for x in (ART / 'observations').glob(f'{phase}-*') if x.is_dir()):
        e = load_smoke(d)
        key = (e.get('arm') or d.name) + ('' if e.get('path') in (None, 'S5') else '-' + e['path'])
        obs[key] = e
    ok = lambda e: bool(e and e.get('usable') and e.get('instrument_valid'))
    cand, ring, rx, reno, s6 = (obs.get(k) for k in (ARMS['cand'], ARMS['ring'], ARMS['rx'], ARMS['reno'], ARMS['cand'] + '-S6'))
    activation = dict(candidate_ring_bytes=cand['send']['ring_bytes_max'] if ok(cand) else None,
                      ring_ring_bytes=ring['send']['ring_bytes_max'] if ok(ring) else None,
                      ring_entries=ring['send']['ring_entries'] if ok(ring) else None)
    gates_pass = gates['cand-mem-ring'] == 0 and gates['cand-mem'] == 0 and native_ok
    if not (ok(cand) and ok(ring)):
        decision = dict(decision='unusable', predicted_mib=None, reason='activation evidence missing or invalid')
    else:
        decision = M.ring_preflight(gates_pass, [gates['gate-ring-m1'] not in (0, None), gates['gate-ring-m2'] not in (0, None)],
                                    activation['ring_ring_bytes'], activation['candidate_ring_bytes'])
        if decision['decision'] == 'run' and activation['ring_entries'] != RING_ENTRIES:
            decision = dict(decision='inert', predicted_mib=decision['predicted_mib'], reason='ring entries differ')
    engaged = bool(ok(rx) and rx['receive']['controller_override'] == 'reno' and rx['receive']['ring_bytes_max'] == 0
                   and rx['send']['ring_bytes_max'] > 0)
    counters = all(ok(e) and e['send']['packets_sent_last'] > 0 and e['receive']['packets_sent_last'] > 0 for e in (cand, ring, rx, reno, s6))
    instrument = dict(runs={k: ok(v) for k, v in obs.items()}, receiver_controller_engaged=engaged,
                      s6_phases=s6['send']['phases'] if ok(s6) else None, packet_counters=counters)
    instrument['ok'] = bool(all(ok(e) for e in (cand, ring, rx, reno, s6)) and engaged and instrument['s6_phases'] and counters)
    res = dict(gates=gates, native_gates=native_ok, activation=activation, instrument=instrument, observations=obs, **decision,
               threshold_mib=M.DETECTABLE_MIB)
    out.write_text(json.dumps(res, indent=1) + '\n')
    if phase == 'memsmoke':
        save('preflight', res)
    print(json.dumps({k: v for k, v in res.items() if k != 'observations'}, indent=1))
    if not instrument['ok']:
        raise SystemExit(3)


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
            for row in rows:  # second consideration F7: fraction eligibility, reported with its reason
                row['eligible'], row['eligibility_reason'] = M.eligibility(row)
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


def arm_names(phase, path, ring_status):
    """The exact arm set of one block."""
    if phase == 'latcand':
        return sorted(['reno-reno', 'cand-bbrv3'])
    return sorted(ARMS[k] for k in expected_arms(path, ring_status))


def arm_count(phase, path, ring_status):
    return len(arm_names(phase, path, ring_status))


def discover(phase, path):
    """({(path, workload, block): {'original'|'rerun': [(dir, arm)]}}, malformed dirs), reading every attempt
    directory (with or without a receipt) and never raising on a malformed one."""
    found, malformed = {}, []
    for label, root in zip(['original', 'rerun'], S.ROOTS):
        for d in sorted(x for x in root.glob(f'{phase}-*') if x.is_dir()):
            try:
                meta = json.loads((d / 'meta.json').read_text())
                cfg = json.loads((d / 'config.json').read_text())
                if meta['phase'] != phase or meta['path'] != path:
                    continue
                arm = '-'.join([meta['variant'], cfg['controller']] + ([meta['tag']] if meta.get('tag') else []))
                found.setdefault((path, cfg['workload'], meta['pair']), {}).setdefault(label, []).append((d, arm))
            except (OSError, ValueError, KeyError, TypeError) as e:
                malformed.append(dict(dir=str(d), error=repr(e)))
    return found, malformed


def attempt(entries, want, mem):
    """(usable, contaminated, reasons) for one attempt: the exact arm set, receipts, #712's usability, instrument
    validity (memory phases) and #712's contamination test, each guarded."""
    reasons, contaminated = [], False
    if sorted(a for _, a in entries) != want:
        reasons.append(f'arms {sorted(a for _, a in entries)} != {want}')
    for d, a in entries:
        if not (d / 'receipt.json').exists():
            reasons.append(f'{a}: no receipt')
            continue
        try:
            if not S.usable(d):
                reasons.append(f'{a}: unusable')
                continue
            contaminated |= bool(S.contaminated(d)['contaminated'])
        except (OSError, ValueError, KeyError, IndexError, TypeError, AssertionError) as e:
            reasons.append(f'{a}: {e!r}')
            continue
        if mem:
            valid, why = instrument_check(d)
            if not valid:
                reasons.append(f'{a}: {why}')
    return not reasons, contaminated, reasons


def choose(phase, path, want):
    """The operator's rerun rule (rules.choose_block) over attempts judged by attempt(); every registered block appears
    in the record, a block with no attempt as 'missing'. want: the exact arm names. Returns (counted dirs, record)."""
    found, malformed = discover(phase, path)
    workloads, n = LAYOUT[(phase, path)]
    dirs, record = [], []
    for workload in workloads:
        for b in range(1, n + 1):
            att = found.get((path, workload, b), {})
            attempts, reasons = [], {}
            for label in ['original', 'rerun']:
                if label in att:
                    usable, cont, why = attempt(att[label], want, phase == 'mem')
                    attempts.append((label, usable, cont))
                    reasons[label] = why
            if not attempts:
                record.append(dict(path=path, workload=workload, block=b, attempts=[], counted=None, status='missing', dirs=[]))
                continue
            pick, status = S.R.choose_block(attempts)
            picked = [d for d, _ in att[pick]]
            dirs += picked
            record.append(dict(path=path, workload=workload, block=b, attempts=attempts, counted=pick, status=status,
                               reasons=reasons, dirs=[str(d) for d in picked]))
    if malformed:
        record.append(dict(path=path, malformed=malformed))
    return dirs, record


def ring_decision():
    return json.loads((ART / 'ring-preflight.json').read_text())['decision']


def stage_obs(phase, path):
    dirs, record = choose(phase, path, arm_names(phase, path, ring_decision()))
    status = {(x['workload'], x['block']): x['status'] for x in record if 'block' in x}
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
    """#715's latency_preservation, unchanged, over clean blocks only; an unusable or unreadable block is None."""
    s = json.loads(R9_SUMMARY.read_text())
    flag = next(e['flags']['control_p95'] for e in s['readiness'] if (e['path'], e['workload'], e['arm']) == ('loopback', 'stream', 'cand-bbrv3'))
    _, record = choose('latcand', 'loopback-long', arm_names('latcand', 'loopback-long', None))
    blocks, reasons = [], {}
    for x in (r for r in record if 'block' in r):
        if x['status'] != 'clean':
            blocks.append(None)
            continue
        try:
            by = {}
            for d in map(Path, x['dirs']):
                r = S.summarize(d)
                by[S.arm(r)] = [v / 1e6 for v in json.loads((d / 'send.json').read_text())['result']['control']['latency_ns']]
            blocks.append(dict(cand=by['cand-bbrv3'], reno=by['reno-reno']))
        except (OSError, ValueError, KeyError, IndexError, TypeError, AssertionError) as e:
            reasons[x['block']] = repr(e)
            blocks.append(None)
    outcome, detail = S.R.latency_preservation(blocks)
    res = dict(readiness=dict(value=flag['median'], raised=flag['raised']), outcome=outcome, detail=detail, blocks=record,
               load_errors=reasons, blocking='no (r9, Linux only)' if outcome == 'not reproduced' else 'yes, unresolved')
    save('latency', res)
    print('latcand', outcome, res['blocking'], {k: v for k, v in detail.items() if not isinstance(v, list)})


STEPS = dict(preflight=preflight, cells=cells, p95=p95, perturbation=perturbation, latency=latency)

if __name__ == '__main__':
    if sys.argv[1] == 'all':
        for k in ['perturbation', 'cells', 'p95', 'latency']:
            STEPS[k]()
    else:
        STEPS[sys.argv[1]](*sys.argv[2:])
