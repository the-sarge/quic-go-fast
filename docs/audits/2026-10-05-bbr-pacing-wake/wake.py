#!/usr/bin/env python3
"""#734 runners and stage analysis.

On minimax (under `taskset -c 1-3`):
  wake.py synthetic [attempt]  the Stage 0 synthetic cases, 10 s each, then their analysis
  wake.py bare                 the bare Go timer loop: five `bare` and five `barepkt` runs, 30 s each
  wake.py analyze <phase>      run the analysis (wake/ana) on every traced observation of a phase
Anywhere, after pulling the artifacts back:
  wake.py stage0|s1|s1lb|bareview|all   apply rules.py and write results.json

Synthetic and bare runs pin the harness to the sender's cores (8-11) and, for
barepkt, the peer to the relay's (4-5); host CPU is sampled every second with
#712's helpers, and #712's contamination test is reported beside each run.
"""
import json
import statistics
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import art  # noqa: F401,E402
import run  # noqa: E402
from run import AFFINITY, proc_cpu_ticks, proc_stat_cpus, summarize, write  # noqa: E402
import rules as R  # noqa: E402

ART = run.ART
ROOTS = [ART / 'observations', ART / 'observations-rerun']
OUT = HERE / 'results.json'
CASES = ['delivery', 'scheduling', 'handler', 'reset', 'nontimer']
LOOP_FN = {'fixture': 'github.com/quic-go/quic-go.(*Conn).run', 'harness': 'main.main'}


def save(key, value):
    out = json.loads(OUT.read_text()) if OUT.exists() else {}
    out[key] = value
    OUT.write_text(json.dumps(out, indent=1) + '\n')


def stats(v):
    v = [x for x in v if x is not None]
    return dict(median=statistics.median(v), minimum=min(v), maximum=max(v), n=len(v)) if v else None


def analyze_dir(prefix, loop, late=False):
    out = Path(str(prefix) + '.ana.json')
    if out.exists():
        return json.loads(out.read_text())
    cmd = [str(ART / 'bin/wakeana-linux'), '-trace', str(prefix) + '.trace', '-events', str(prefix) + '.events',
           '-meta', str(prefix) + '.json', '-loop', loop, '-out', str(out)] + (['-late'] if late else [])
    subprocess.run(cmd, check=True)
    return json.loads(out.read_text())


# ---- minimax runners ----------------------------------------------------------------------
def harness_run(directory, case, seconds, peer=False):
    """One harness run with per-second host sampling; refuses to replace a prior attempt."""
    assert not directory.exists(), f'{directory} exists; retain every attempt'
    directory.mkdir(parents=True)
    prefix = directory / case
    cmd = ['taskset', '-c', AFFINITY['send'], str(ART / 'bin/harness'), '-case', case, '-out', str(prefix), '-duration', f'{seconds}s']
    started = time.time_ns()
    peer_proc = None
    if peer:
        pcmd = ['taskset', '-c', AFFINITY['relay'], str(ART / 'bin/harness'), '-case', 'peer', '-duration', f'{seconds + 3}s']
        peer_proc = subprocess.Popen(pcmd)
    proc = subprocess.Popen(cmd)
    host = []
    while proc.poll() is None:
        host.append(dict(unix_ns=time.time_ns(), cpus=proc_stat_cpus(),
                         fixture_ticks={'send': proc_cpu_ticks(proc.pid), **({'relay': proc_cpu_ticks(peer_proc.pid)} if peer_proc else {})}))
        time.sleep(1)
    if peer_proc:
        peer_proc.wait(timeout=10)
    meta = json.loads((Path(str(prefix) + '.json')).read_text())
    window = [meta['start_unix_ns'], meta['end_unix_ns']]
    write(directory / 'host-cpu.json', host)
    write(directory / 'receipt.json', dict(command=cmd, peer=pcmd if peer else None, start_unix_ns=started, end_unix_ns=time.time_ns(),
                                           exit_code=proc.returncode, peer_exit=peer_proc.returncode if peer_proc else None,
                                           measure_window_unix_ns=window))
    assert proc.returncode == 0, case
    return analyze_dir(prefix, LOOP_FN['harness'], late=case in CASES)


def synthetic(attempt=''):
    for case in CASES:
        a = harness_run(ART / f'synthetic{attempt}' / case, case, 10)
        print(case, a['late']['count'], {k: round(v, 3) for k, v in (a['late']['shares'] or {}).items() if v}, flush=True)


def bare():
    for i in range(1, 6):
        for case, peer in [('bare', False), ('barepkt', True)]:
            a = harness_run(ART / 'bare' / f'{case}-{i}', case, 30, peer=peer)
            print(case, i, a['late']['count'], round(a['late']['sum_ns'] / 1e6, 1), a['timer_lag_pacing']['median_ns'], flush=True)


def analyze(phase):
    for root in ROOTS:
        for p in sorted(root.glob(f'{phase}-*/send.wake.trace')):
            a = analyze_dir(p.parent / 'send.wake', LOOP_FN['fixture'])
            print(p.parent.name, a.get('error'), a['late']['count'], flush=True)


# ---- stage analysis (anywhere) ----------------------------------------------------------------
def contamination(d):
    from localize import contamination as c
    facts = d / 'host-facts.json'
    window = json.loads(facts.read_text())['measure_window_unix_ns'] if facts.exists() else \
        json.loads((d / 'receipt.json').read_text())['measure_window_unix_ns']
    return c(json.loads((d / 'host-cpu.json').read_text()), window)


def usable_obs(d):
    try:
        receipt = json.loads((d / 'receipt.json').read_text())
        if receipt['exit_codes'] != [0, 0] or receipt.get('relay_exit') not in (None, 0):
            return False
        summarize(d)
        for role in ['send', 'receive']:
            p = d / f'{role}.perf-stat.csv'
            if p.exists() and any(v['running_pct'] is not None and v['running_pct'] < 95 for v in run.perf_counters(p).values()):
                return False
        return True
    except (AssertionError, FileNotFoundError, KeyError, json.JSONDecodeError):
        return False


def chosen(phase):
    """Directories counted under the operator's rerun rule, plus the per-block record."""
    blocks = {}
    for label, root in zip(['original', 'rerun'], ROOTS):
        for p in sorted(root.glob(f'{phase}-*/receipt.json')):
            meta = json.loads((p.parent / 'meta.json').read_text())
            if meta['phase'] != phase:
                continue
            cfg = json.loads((p.parent / 'config.json').read_text())
            blocks.setdefault((meta['path'], cfg['workload'], meta['pair']), {}).setdefault(label, []).append(p.parent)
    dirs, record = [], []
    for key, att in sorted(blocks.items()):
        attempts = []
        for label in ['original', 'rerun']:
            if label in att:
                ds = att[label]
                attempts.append((label, all(usable_obs(d) for d in ds), any(contamination(d)['contaminated'] for d in ds)))
        pick, status = R.choose_block(attempts)
        dirs += att[pick]
        record.append(dict(path=key[0], workload=key[1], block=key[2], attempts=attempts, counted=pick, status=status))
    return dirs, record


def wake_meta(d):
    return json.loads((d / 'send.wake.json').read_text())


def overlay_lateness(d):
    """#715's overlay count over the recorded window, when the overlay was built in; else the recorder's aggregates."""
    m = wake_meta(d)
    t = d / 'send.timeline.json'
    if t.exists():
        tl = json.loads(t.read_text())
        ix = {c: i for i, c in enumerate(tl['emit_columns'])}
        n = ns = 0
        for i, e in enumerate(tl['emit']):
            ts = tl['base_unix_ns'] + i * tl['bucket_ns']
            if m['start_unix_ns'] <= ts < m['end_unix_ns']:
                n, ns = n + e[ix['late_count']], ns + e[ix['late_sum_ns']]
        return n, ns, 'timeline overlay'
    a = m['aggregate']
    return a['late_count'], a['late_ns'], 'recorder aggregates'


def stage0():
    res = dict(synthetic={}, perturbation={})
    for case in CASES:
        d = ART / 'synthetic' / case
        a = json.loads((d / f'{case}.ana.json').read_text())
        truth = json.loads((d / f'{case}.truth.json').read_text())
        m = json.loads((d / f'{case}.json').read_text())
        off = m['mono_offset_ns']
        r = R.recovery(case, a, truth, m['start_nanotime'] - off, m['end_nanotime'] - off)
        r['contamination'] = contamination(d)
        r['shares'] = a['late']['shares']
        r['timer_lag_pacing'] = a['timer_lag_pacing']
        res['synthetic'][case] = r
    res['synthetic_passed'] = all(r['recovered'] for r in res['synthetic'].values())
    for path, variant, workloads in [('S5', 'cand-wake-timeline', ['stream', 'datagram']), ('loopback', 'cand-lb-timeline', ['datagram'])]:
        dirs, record = chosen('preflight' if path == 'S5' else 'preflightlb')
        rows = [dict(summarize(d), dir=d) for d in dirs]
        for wl in workloads:
            pairs, blocks = [], []
            for b in sorted({r['pair'] for r in rows if r['workload'] == wl}):
                arm = {r['variant']: r for r in rows if r['workload'] == wl and r['pair'] == b}
                if 'cand' in arm and variant in arm:
                    pairs.append((arm[variant]['goodput_mbps'], arm['cand']['goodput_mbps']))
                    blocks.append(dict(block=b, instrumented=arm[variant]['goodput_mbps'], plain=arm['cand']['goodput_mbps']))
            res['perturbation'][f'{path}/{wl}'] = dict(R.perturbation(pairs), blocks=blocks)
        res['perturbation'][f'{path}/blocks'] = record
    res['passed'] = res['synthetic_passed'] and all(res['perturbation'][f'S5/{w}']['passed'] for w in ['stream', 'datagram'])
    res['loopback_instrument_passed'] = res['perturbation']['loopback/datagram']['passed']
    save('stage0', res)
    for c, r in res['synthetic'].items():
        print('stage0', c, r['recovered'], r.get('verdict'), r.get('share_shown', r.get('chain_exact')), r['checks'])
    print('stage0 perturbation', {k: (round(v['median'], 4), v['passed']) for k, v in res['perturbation'].items() if 'median' in v})
    print('stage0 passed', res['passed'], 'loopback instrument', res['loopback_instrument_passed'])


def s1():
    dirs, record = chosen('s1')
    obs = []
    for d in dirs:
        r = summarize(d)
        a = json.loads((d / 'send.wake.ana.json').read_text())
        n, ns, src = overlay_lateness(d)
        v, why = R.s1_observation(a, (n, ns))
        late = a['late']
        obs.append(dict(id=d.name, workload=r['workload'], block=r['pair'], goodput_mbps=r['goodput_mbps'], verdict=v, reason=why,
                        late_count=late['count'], late_s=late['sum_ns'] / 1e9, overlay=dict(count=n, ns=ns, source=src),
                        shares=late['shares'], other_by={k: v / late['sum_ns'] for k, v in (late['segments_ns'].get('delivery_other_by') or {}).items()},
                        leading_events=late['leading_events'], timer_lag_pacing=a['timer_lag_pacing'], timer_lag_other=a['timer_lag_other'],
                        select_wakes=a['select_wakes'], woke_cases=a['woke_cases'], window_shares=a['window_shares'],
                        pacing_timer_wakes=a['pacing_timer_wakes'], alignment=a['alignment'], contamination=contamination(d)))
    per = {wl: R.workload_verdict([o['verdict'] for o in obs if o['workload'] == wl]) for wl in ['stream', 'datagram']}
    esc = {wl: R.needs_escalation([o['verdict'] for o in obs if o['workload'] == wl and o['block'] <= 4]) for wl in ['stream', 'datagram']}
    res = dict(blocks=record, observations=obs, workloads=per, escalation=esc, verdict=R.stage1_verdict(per))
    save('s1', res)
    for o in obs:
        print('s1', o['id'], o['verdict'], round(o['late_s'], 3), {k: round(v, 3) for k, v in (o['shares'] or {}).items() if v})
    print('s1 verdict', per, res['verdict'], 'escalation', esc)


def s1lb():
    dirs, record = chosen('s1lb')
    obs = []
    for d in dirs:
        r = summarize(d)
        m = wake_meta(d)
        o = R.lb_observation(m['aggregate'], m['end_unix_ns'] - m['start_unix_ns'])
        obs.append(dict(o, id=d.name, block=r['pair'], goodput_mbps=r['goodput_mbps'], contamination=contamination(d)))
    v = R.workload_verdict([o['verdict'] for o in obs])
    res = dict(blocks=record, observations=obs, verdict=v,
               instrumented_over_715_candidate=statistics.median(o['goodput_mbps'] for o in obs) / 3980.9 if obs else None)
    save('s1lb', res)
    for o in obs:
        print('s1lb', o['id'], o['verdict'], round(o['exposure'], 4), round(o['loss'], 4), {k: round(x, 3) for k, x in o['shares'].items() if x})
    print('s1lb verdict', v)


S2_ARMS = {('reno', 'reno', ''): 'reno', ('reno', 'reno', 'aa'): 'aa', ('cand-wake-timeline', 'bbrv3', ''): 'base',
           ('cand-wake-timeline', 'bbrv3', 'aa'): 'bbraa', ('new-wake-timeline', 'bbrv3', ''): 'new', ('new', 'reno', ''): 'renonew',
           ('cand2q-wake-timeline', 'bbrv3', ''): 'twoq'}
TRACED = ['base', 'bbraa', 'new', 'twoq']


def s2_obs(d):
    r = summarize(d)
    meta = json.loads((d / 'meta.json').read_text())
    arm = S2_ARMS[(meta['variant'], r['controller'], meta.get('tag') or '')]
    gib = r['useful_bytes'] / 2**30
    o = dict(id=d.name, arm=arm, workload=r['workload'], block=r['pair'], goodput_mbps=r['goodput_mbps'],
             cpu_per_gib=r['send']['cpu_seconds_per_gib'], contamination=contamination(d),
             cycles_per_gib={role: sum(r[role]['counters_per_gib'].get(e, 0) for e in ['cycles:u', 'cycles:k']) for role in ['send', 'receive']},
             wakeups_per_gib=r['send']['counters_per_gib'].get('sched:sched_wakeup'),
             overflow=r.get('relay', {}).get('forward', {}).get('stats', {}).get('Overflow'))
    if arm in TRACED:
        a = json.loads((d / 'send.wake.ana.json').read_text())
        n, ns, _ = overlay_lateness(d)
        ok, why = R.ana_usable(a, (n, ns))
        o.update(analysis_usable=ok, analysis_reason=why, late_s=a['late']['sum_ns'] / 1e9, late_count=a['late']['count'],
                 shares=a['late']['shares'], pacing_wakes=a['pacing_timer_wakes'], wakes_per_gib=a['pacing_timer_wakes'] / gib,
                 select_wakes=a['select_wakes'], timer_lag_pacing=a['timer_lag_pacing'],
                 aggregate=wake_meta(d)['aggregate'])
        t = d / 'send.timeline.json'
        if t.exists():
            tl = json.loads(t.read_text())
            ix = {c: i for i, c in enumerate(tl['emit_columns'])}
            m = wake_meta(d)
            sel = [e for i, e in enumerate(tl['emit']) if m['start_unix_ns'] <= tl['base_unix_ns'] + i * tl['bucket_ns'] < m['end_unix_ns']]
            o['max_bytes_per_10ms'] = max((e[ix['bytes']] for e in sel), default=None)
    return o


def s2():
    dirs, record = chosen('s2')
    obs = [s2_obs(d) for d in dirs]
    integrity = all(usable_obs(d) for d in dirs)
    per, blocks_out = {}, {}
    pres = {'send': ([], []), 'receive': ([], [])}
    for wl in ['stream', 'datagram']:
        blocks = []
        for b in range(1, 7):
            x = {o['arm']: o for o in obs if o['workload'] == wl and o['block'] == b}
            if set(x) != set(S2_ARMS.values()) or not all(x[k]['analysis_usable'] for k in ['base', 'bbraa', 'new']):
                continue
            for role in ['send', 'receive']:
                pres[role][0].append(x['renonew']['cycles_per_gib'][role] / x['reno']['cycles_per_gib'][role])
                pres[role][1].append(x['aa']['cycles_per_gib'][role] / x['reno']['cycles_per_gib'][role])
            deficit = {k: 1 - x[k]['goodput_mbps'] / x['reno']['goodput_mbps'] for k in TRACED}
            base = x['base']
            ratio = lambda k, m: (x[k][m] / base[m]) if base[m] else None
            blocks.append(dict(block=b, deficit_diff_new=deficit['new'] - deficit['base'], deficit_diff_aa=deficit['bbraa'] - deficit['base'],
                               deficit_diff_twoq=deficit['twoq'] - deficit['base'],
                               late_ratio_new=ratio('new', 'late_s'), late_ratio_aa=ratio('bbraa', 'late_s'), late_ratio_twoq=ratio('twoq', 'late_s'),
                               wakes_ratio_new=ratio('new', 'wakes_per_gib'), wakes_ratio_aa=ratio('bbraa', 'wakes_per_gib'),
                               cpu_ratio_new=ratio('new', 'cpu_per_gib'), cpu_ratio_aa=ratio('bbraa', 'cpu_per_gib'),
                               cpu_ratio_twoq=ratio('twoq', 'cpu_per_gib'),
                               wake_classes_new=x['new']['select_wakes'], wake_classes_base=base['select_wakes'],
                               deficit=deficit, goodput={k: v['goodput_mbps'] for k, v in x.items()},
                               overflow={k: v['overflow'] for k, v in x.items()}))
        per[wl] = R.s2_workload(blocks)
        blocks_out[wl] = blocks
    preservation = {role: R.preservation_cell(*pres[role]) if pres[role][0] else 'unusable' for role in pres}
    outcome, reasons = R.s2_outcome(per, integrity, preservation)
    res = dict(blocks=record, observations=obs, per_block=blocks_out, workloads=per, integrity=integrity,
               preservation=dict(cells=preservation, ratios={r: dict(renonew=v[0], aa=v[1]) for r, v in pres.items()}),
               outcome=outcome, reasons=reasons)
    save('s2', res)
    for wl, w in per.items():
        print('s2', wl, {m: (w[m].get('movement'), round(w[m].get('median', 0), 4), round(w[m].get('aa_min', 0), 4), round(w[m].get('aa_max', 0), 4))
                         for m in ['deficit_diff', 'late_ratio', 'wakes_ratio', 'cpu_ratio']})
    print('s2 preservation', preservation, 'integrity', integrity)
    print('s2 outcome', outcome, reasons)


def bareview():
    runs = []
    for d in sorted((ART / 'bare').glob('*-*')):
        case = d.name.rsplit('-', 1)[0]
        a = json.loads((d / f'{case}.ana.json').read_text())
        truth = json.loads((d / f'{case}.truth.json').read_text())
        runs.append(dict(id=d.name, case=case, late_count=a['late']['count'], late_s=a['late']['sum_ns'] / 1e9,
                         mean_late_us=a['late']['sum_ns'] / max(1, a['late']['count']) / 1e3, shares=a['late']['shares'],
                         timer_lag_pacing=a['timer_lag_pacing'], received=truth.get('received'), contamination=contamination(d)))
    summ = {c: dict(mean_late_us=stats([r['mean_late_us'] for r in runs if r['case'] == c]),
                    lag_median_us=stats([r['timer_lag_pacing']['median_ns'] / 1e3 for r in runs if r['case'] == c]),
                    lag_p90_us=stats([r['timer_lag_pacing']['p90_ns'] / 1e3 for r in runs if r['case'] == c]))
            for c in ['bare', 'barepkt']}
    save('bare', dict(runs=runs, summary=summ))
    print('bare', json.dumps(summ, indent=1))


if __name__ == '__main__':
    cmd = sys.argv[1]
    if cmd == 'synthetic':
        synthetic(sys.argv[2] if len(sys.argv) > 2 else '')
    elif cmd == 'bare':
        bare()
    elif cmd == 'analyze':
        analyze(sys.argv[2])
    elif cmd == 'all':
        for f in [stage0, s1, s1lb, bareview, s2]:
            f()
    else:
        dict(stage0=stage0, s1=s1, s1lb=s1lb, bareview=bareview, s2=s2)[cmd]()
