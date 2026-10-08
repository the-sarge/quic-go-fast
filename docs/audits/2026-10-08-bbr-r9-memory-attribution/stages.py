#!/usr/bin/env python3
"""#739 analysis: prerequisites, readiness views, the S5 timeline and the conditional preservation stages.

Copied from #736's stages.py (3bf2245e). Recorded changes: the registered
steps are D5's (`fold`, `prereq`, `readiness`, the descriptive `receiver`
split, `s5timeline`, `presrss`, `preslat`); the memory, `recvharness` and D2
steps are #740's or unlisted by D5 and are left out of STEPS and `all`, with
their code kept unchanged. The #736 docstring follows.


Adapted from #715's stages.py (36f7ce01). Readiness flags come from #712's
analyze.py, unchanged, run over an observation view: `summary.json` uses the
operator's rerun rule (a contaminated block is replaced by its clean same-seed
rerun), and `summary-original.json` applies #712's rule to the original blocks
only. Memory uses #711's rules in attribution.py, unchanged, with #715's
`deliveryRecords` pattern addition; the heap-site pass runs over four blocks
(D5) through a copy of attribution.heapsites whose only change is the block
list, and a cell is classified only when every usable block agrees
(harness.heap_cell). Recorded changes from #715: no `lbneck` stage, no macOS
stage, and the conditional `recvharness` stage (harness.py, with #735's
receiver.py descriptive figures).

Usage (minimax, after a stage):  stages.py fold
Anywhere:                         stages.py prereq|readiness|s5timeline|receiver|recvharness|memory|presrss|preslat|d2|all
"""
import json
import os
import re
import runpy
import shutil
import statistics
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import art  # noqa: F401,E402
import run  # noqa: E402
from run import BACK, FRONT, RECEIVER, SENDER, summarize  # noqa: E402
import rules as R  # noqa: E402
import harness as H  # noqa: E402
import receiver as RX  # noqa: E402

ART = run.ART
ROOTS = [ART / 'observations', ART / 'observations-rerun']
OUT = HERE / 'attribution.json'
ECN_TRACKER = re.compile(r'\(\*bbrECNTracker\)\.(feedback|mode)|captureBBRECN')


def stats(v):
    v = [x for x in v if x is not None]
    return dict(median=statistics.median(v), minimum=min(v), maximum=max(v), n=len(v)) if v else None


def arm(r):
    return '-'.join([r['variant'], r['controller']] + ([r['tag']] if r.get('tag') else []))


def window(d):
    cfg = json.loads((d / 'config.json').read_text())
    m0 = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    return cfg, (m0, m0 + cfg['measure_ms'] * 1_000_000)


def contaminated(d):
    from localize import contamination
    facts = json.loads((d / 'host-facts.json').read_text())
    return contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])


def usable(d):
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


def blocks_of(phase, path=None):
    """{(path, workload, block): {'original': [dirs], 'rerun': [dirs]}} for one phase."""
    out = {}
    for label, root in zip(['original', 'rerun'], ROOTS):
        for p in sorted(root.glob(f'{phase}-*/receipt.json')):
            meta = json.loads((p.parent / 'meta.json').read_text())
            if meta['phase'] != phase or (path and meta['path'] != path):
                continue
            cfg = json.loads((p.parent / 'config.json').read_text())
            out.setdefault((meta['path'], cfg['workload'], meta['pair']), {}).setdefault(label, []).append(p.parent)
    return out


def chosen(phase, path=None):
    """Directories counted under the operator's rerun rule, plus the per-block record."""
    dirs, record = [], []
    for key, att in sorted(blocks_of(phase, path).items()):
        attempts = []
        for label in ['original', 'rerun']:
            if label in att:
                ds = att[label]
                cont = [contaminated(d)['contaminated'] for d in ds]
                attempts.append((label, all(usable(d) for d in ds), any(cont)))
        pick, status = R.choose_block(attempts)
        dirs += att[pick]
        record.append(dict(path=key[0], workload=key[1], block=key[2], attempts=attempts, counted=pick, status=status))
    return dirs, record


def load_rows(dirs):
    rows = []
    for d in dirs:
        r = summarize(d)
        r['dir'] = str(d)
        r['contamination'] = contaminated(d)
        rows.append(r)
    return rows


def save(key, value):
    out = json.loads(OUT.read_text()) if OUT.exists() else {}
    out[key] = value
    OUT.write_text(json.dumps(out, indent=1) + '\n')


# ---- perf folding (minimax, root-owned perf.data) --------------------------------


def fold_stages(perf_data):
    """Per goroutine stage: samples and cycles; per thread: samples; plus whether BBR ECN tracker frames occur."""
    out = subprocess.run(['sudo', '-n', 'perf', 'script', '-i', str(perf_data), '-F', 'tid,period,ip,sym', '--no-demangle'],
                         capture_output=True, text=True, check=True).stdout
    res = dict(stages={}, cycles={}, tids={}, samples=0, ecn_tracker=0, top_roots={})
    tid, period, frames = None, None, []

    def flush():
        if period is None:
            return
        s = R.stage_of(frames)
        res['stages'][s] = res['stages'].get(s, 0) + 1
        res['cycles'][s] = res['cycles'].get(s, 0) + period
        res['tids'][tid] = res['tids'].get(tid, 0) + 1
        res['samples'] += 1
        if any(ECN_TRACKER.search(f) for f in frames):
            res['ecn_tracker'] += 1
        user = [f for f in frames if not f.startswith('k:')]
        root = user[-2] if len(user) >= 2 and user[-1] == 'runtime.goexit' else (user[-1] if user else '[kernel]')
        res['top_roots'][root] = res['top_roots'].get(root, 0) + 1
    for line in out.splitlines():
        if not line.strip():
            flush()
            tid, period, frames = None, None, []
        elif not line.startswith('\t'):
            flush()
            parts = line.split()
            tid, period, frames = parts[0], int(parts[1]), []
        else:
            parts = line.split(None, 1)
            addr, sym = parts[0], (parts[1] if len(parts) > 1 else '[unknown]')
            sym = sym.split('+0x')[0].strip()
            if re.match(r'^ffffffff', addr):
                sym = 'k:' + sym
            frames.append(sym)
    flush()
    res['top_roots'] = dict(sorted(res['top_roots'].items(), key=lambda kv: -kv[1])[:15])
    return res


def fold():
    n = 0
    for root in ROOTS:
        for p in sorted(root.glob('*/*.perf.data')):
            out = p.with_name(p.name.replace('.perf.data', '.stages.json'))
            if not out.exists():
                out.write_text(json.dumps(fold_stages(p), indent=1) + '\n')
                n += 1
    print('folded', n)


def forward_packets(d):
    """Forward packets the relay model received in the measured window: written plus overflowed (#714's metric)."""
    cfg, _ = window(d)
    relay = json.loads((d / 'relay.json').read_text())
    lo, hi = cfg['warmup_ms'], cfg['warmup_ms'] + cfg['measure_ms']
    fd = relay['ForwardDelivery']
    written = sum(s[1] for s in fd if lo <= s[0] < hi)
    before = [s[4] for s in fd if s[0] < lo]
    during = [s[4] for s in fd if s[0] < hi]
    return written + (during[-1] - (before[-1] if before else 0))


def fixture_sockets(lines):
    """{port: skmem} for the fixture's four sockets; `ss -m` prints skmem on the line after each socket."""
    out = {}
    for a, b in zip(lines, lines[1:]):
        for port in (RECEIVER, SENDER, FRONT, BACK):
            if port in a.split()[3:4] and 'skmem' in b:
                out[port] = b.strip()
    return out


# ---- prerequisites -----------------------------------------------------------------


def prereq():
    res = {}
    facts = json.loads((ART / 'prereq' / 'host-facts.json').read_text())
    cal = {c['relay']: c for c in facts['calibration']}
    # Received is [sent codepoint][read codepoint]; Not-ECT is index 0.
    strips = all(sum(row[0] for row in cal['relay-v3']['report'][d]['received']) == sum(cal['relay-v3']['report'][d]['sent'])
                 for d in ['forward', 'reverse'])
    res['calibration'] = dict(unadapted_strips=strips, unadapted_pass=cal['relay-v3']['report']['pass'],
                              adapted_pass=cal['relay-linux']['report']['pass'],
                              detail={k: dict(forward=v['report']['forward'], reverse=v['report']['reverse']) for k, v in cal.items()})
    res['host'] = {k: facts[k] for k in ['uname', 'governor', 'sysctl', 'kernel_accounting', 'lo_offloads', 'perf', 'go', 'id', 'uptime']}
    checks = {}
    for phase in ['smoke', 'perfsmoke', 'ecnsmoke', 'xsmoke', 'rsmoke']:
        for p in sorted(ROOTS[0].glob(f'{phase}*/receipt.json')):
            d = p.parent
            r = summarize(d)
            facts = json.loads((d / 'host-facts.json').read_text())
            mid = facts.get('mid', {}).get('processes', {})
            aff = {role: sorted({t['cpus'] for t in mid.get(role, {}).get('threads', {}).values()}) for role in mid}
            last = {role: sorted({t['last_cpu'] for t in mid.get(role, {}).get('threads', {}).values()}) for role in mid}
            c = dict(goodput_mbps=r['goodput_mbps'], affinity=aff, last_cpus=last, contamination=contaminated(d),
                     udp_errors={k: facts['udp_after'][k] - facts['udp_before'][k] for k in ['RcvbufErrors', 'SndbufErrors', 'InErrors', 'MemErrors']},
                     sockets=fixture_sockets(facts.get('mid', {}).get('sockets', [])))
            for role in ['send', 'receive']:
                if 'counters' in r[role]:
                    cnt = r[role]['counters']
                    c[f'{role}_min_running_pct'] = min(v['running_pct'] for v in cnt.values() if v['running_pct'] is not None)
                    if role == 'send' and r.get('relay'):
                        pk = forward_packets(d)
                        c['send_syscalls_per_packet'] = (cnt['syscalls:sys_enter_sendmsg']['value'] + cnt['syscalls:sys_enter_sendmmsg']['value']) / pk
                        c['xmit_per_packet'] = cnt['net:net_dev_xmit']['value'] / pk
                st = d / f'{role}.stages.json'
                if st.exists():
                    s = json.loads(st.read_text())
                    c[f'{role}_stages'] = s['stages']
                    c[f'{role}_ecn_tracker_samples'] = s['ecn_tracker']
                    c[f'{role}_top_roots'] = s['top_roots']
            if r.get('relay'):
                c['relay_forward_ecn_in'] = r['relay']['forward']['ecn_in']
                c['relay_reverse_ecn_in'] = r['relay']['reverse']['ecn_in']
            tl = d / 'send.timeline.json'
            if tl.exists():
                cfg, w = window(d)
                t = json.loads(tl.read_text())
                c['timeline_coverage_s'] = (min(t['dumped_unix_ns'], w[1]) - w[0]) / 1e9
                rows = [x for x in t['model'] if w[0] <= x[0] < w[1]]
                c['timeline_bandwidth_median'] = statistics.median(x[2] for x in rows) if rows else None
            if (d / 'threads.json').exists():
                th = json.loads((d / 'threads.json').read_text())
                c['thread_samples'] = len(th)
            checks[d.name] = c
    res['checks'] = checks
    save('prerequisites', res)
    print(json.dumps(res['calibration'], indent=1))
    for k, v in checks.items():
        print(k, {kk: vv for kk, vv in v.items() if kk not in ('sockets', 'send_top_roots', 'receive_top_roots', 'affinity', 'last_cpus')})


# ---- readiness -------------------------------------------------------------------------


def readiness():
    view_root = ART / 'views'
    out = {}
    # The original-blocks pass runs first: analyze.py always writes summary.json, which the rule pass then owns.
    for label, pick in [('summary-original.json', 'original'), ('summary.json', 'rule')]:
        view = view_root / pick
        if view.exists():
            shutil.rmtree(view)
        view.mkdir(parents=True)
        if pick == 'rule':
            dirs, record = chosen('main')
        else:
            dirs = [d for att in blocks_of('main').values() for d in att.get('original', [])]
            record = None
        for d in dirs:
            os.symlink(d, view / d.name)
        run.OBS = view
        runpy.run_path(str(HERE / 'analyze.py'), run_name='__main__')
        produced = HERE / 'summary.json'
        if label != 'summary.json':
            produced.rename(HERE / label)
        out[pick] = record
    run.OBS = ART / 'observations'
    save('readiness_blocks', out['rule'])


def readiness_summary(name='summary.json'):
    return json.loads((HERE / name).read_text())


def raised(summary, arm_name, kind):
    """[(path, workload, flag key)] raised for an arm; kind 'cpu', 'rss', 'p95', 'goodput'."""
    keys = {'cpu': ('send_cpu', 'receive_cpu'), 'rss': ('send_rss', 'receive_rss'), 'p95': ('control_p95',), 'goodput': ('goodput',)}[kind]
    return [(e['path'], e['workload'], k) for e in summary['readiness'] if e['arm'] == arm_name
            for k, f in e['flags'].items() if k in keys and f['raised']]


PHASES = ['startup', 'drain', 'down', 'cruise', 'refill', 'up', 'probertt']
PACER_MARGIN = 0.99  # bbrSendPolicy paces at 99% of the controller's rate (D08)


def timeline_obs(d):
    r = summarize(d)
    cfg, w = window(d)
    t = json.loads((d / 'send.timeline.json').read_text())
    cols = t['emit_columns']
    ix = {c: i for i, c in enumerate(cols)}
    base, span = t['base_unix_ns'], t['bucket_ns']
    end = min(w[1], t['dumped_unix_ns'] - span)  # last complete bucket written
    buckets = []
    for i, e in enumerate(t['emit']):
        ts = base + i * span
        if w[0] <= ts < end:
            states = {c: e[ix[c]] for c in cols if c.endswith('_ns') and not c.startswith('late')}
            buckets.append((ts, states, e[ix['bytes']], e[ix['packets']], e[ix['late_sum_ns']]))
    mc = {c: i for i, c in enumerate(t['model_columns'])}
    model = [(x[0], x[mc['rate']] * PACER_MARGIN) for x in t['model']]
    relay = json.loads((d / 'relay.json').read_text())
    lo, hi = cfg['warmup_ms'], cfg['warmup_ms'] + cfg['measure_ms']
    fd = relay['ForwardDelivery']
    before = [s[4] for s in fd if s[0] < lo]
    during = [s[4] for s in fd if s[0] < hi]
    overflow = (during[-1] - (before[-1] if before else 0)) if during else 0
    sent_b = sum(b[2] for b in buckets)
    sent_p = sum(b[3] for b in buckets) or 1
    wire_pkt = (sent_b + R.IP_UDP * sent_p) / sent_p
    queue = [(cfg['start_unix_ns'] + q[0] * 1_000_000, q[1]) for q in relay['QueueSamples']]
    dec = R.timeline_decomposition(buckets, model, (w[0], end), overflow, wire_pkt, queue)
    o = dict(id=d.name, workload=r['workload'], block=r['pair'], goodput_mbps=r['goodput_mbps'], decomposition=dec,
             verdict=R.timeline_verdict(dec), overflow_packets=overflow)
    # Descriptive: phase time, bandwidth estimate against the bottleneck, loss responses, lateness, states.
    rows = sorted(t['model'])
    phase_t = {}
    for a, b in zip(rows, rows[1:]):
        s, e = max(a[0], w[0]), min(b[0], end)
        if e > s:
            phase_t[PHASES[a[mc['phase']]]] = phase_t.get(PHASES[a[mc['phase']]], 0) + (e - s) / 1e9
    tot = sum(phase_t.values()) or 1
    o['phase_share'] = {k: v / tot for k, v in phase_t.items()}
    ratio = wire_pkt / (wire_pkt - R.IP_UDP)
    cap = R.CAPACITY_BPS / 8
    cruise = [x[mc['bandwidth']] * ratio / cap for x in rows if w[0] <= x[0] < end and x[mc['phase']] == 3]
    o['bandwidth_over_capacity_cruise'] = stats(cruise)
    o['rate_over_capacity'] = stats([x[mc['rate']] * PACER_MARGIN * ratio / cap for x in rows if w[0] <= x[0] < end])
    inside = [x for x in rows if w[0] <= x[0] < end]
    o['inflight_long_decreases'] = sum(1 for a, b in zip(inside, inside[1:]) if b[mc['inflight_long']] < a[mc['inflight_long']])
    o['inflight_short_bounded_share'] = sum(1 for x in inside if x[mc['inflight_short']] < 1 << 61) / max(1, len(inside))
    late = [b[4] for b in buckets]
    o['late_ms_total'] = sum(late) / 1e6
    o['late_count'] = sum(t['emit'][int((b[0] - base) // span)][ix['late_count']] for b in buckets)
    o['late_max_ms'] = max((t['emit'][int((b[0] - base) // span)][ix['late_max_ns']] for b in buckets), default=0) / 1e6
    st = {}
    for b in buckets:
        for k, v in b[1].items():
            st[k] = st.get(k, 0) + v
    stot = sum(st.values()) or 1
    o['state_share'] = {k: v / stot for k, v in st.items()}
    return o


def timeline_stage(dirs):
    obs = [timeline_obs(d) for d in dirs]
    per = {}
    for workload in ['stream', 'datagram']:
        per[workload] = R.workload_verdict([o['verdict'] for o in obs if o['workload'] == workload])
    return obs, per


def s5timeline():
    dirs, record = chosen('s5timeline')
    obs, per = timeline_stage(dirs)
    res = dict(blocks=record, observations=obs, workloads=per, verdict=R.stage_verdict(per))
    # Instrument perturbation, descriptive: instrumented goodput against the readiness candidate's S5 median.
    try:
        summ = readiness_summary()
        ref = {g['workload']: g['goodput_mbps']['median'] for g in summ['groups'] if g['path'] == 'S5' and g['arm'] == 'cand-bbrv3'}
        res['instrumented_over_readiness_goodput'] = {w: statistics.median(o['goodput_mbps'] for o in obs if o['workload'] == w) / ref[w]
                                                      for w in ref}
        res['readiness_relay'] = {(g['workload'] + '/' + g['arm']): dict(goodput=g['goodput_mbps']['median'],
                                  overflow_rate=g['relay']['overflow_rate']['median'])
                                  for g in summ['groups'] if g['path'] == 'S5'}
    except FileNotFoundError:
        pass
    mac_root = ART / 'mac' / 'observations'
    mac_dirs = sorted(p.parent for p in mac_root.glob('s5timeline-*/receipt.json'))
    if mac_dirs:
        import importlib.util
        spec = importlib.util.spec_from_file_location('macrun', HERE / 'mac' / 'run.py')
        macrun = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(macrun)
        global summarize
        linux_summarize = summarize
        summarize = macrun.summarize
        try:
            mobs = [timeline_obs(d) for d in mac_dirs]
        finally:
            summarize = linux_summarize
        mper = {w: R.workload_verdict([o['verdict'] for o in mobs if o['workload'] == w], n_min=2, usable_min=2) for w in ['stream', 'datagram']}
        res['mac'] = dict(observations=mobs, workloads=mper, comparison=R.mac_comparison(per, mper))
    save('s5timeline', res)
    for o in obs:
        dd = o['decomposition'] or {}
        print('s5timeline', o['id'], o['verdict'], {k: round(v, 4) for k, v in dd.items()}, 'phases', {k: round(v, 3) for k, v in o['phase_share'].items()})
    print('s5timeline verdict', per, res['verdict'], res.get('mac', {}).get('comparison'))


# ---- receiver CPU split ----------------------------------------------------------------------


def split_of(d, role):
    r = summarize(d)
    cfg, w = window(d)
    host = json.loads((d / 'host-cpu.json').read_text())
    samples = [(h['unix_ns'], h['fixture_ticks'].get(role)) for h in host]
    return R.cpu_split(samples, cfg['start_unix_ns'], w, r[role]['total_cpu_seconds']), r['useful_bytes'] / 2**30


def receiver():
    summ = readiness_summary()
    dirs, _ = chosen('main')
    by = {}
    for d in dirs:
        meta = json.loads((d / 'meta.json').read_text())
        cfg = json.loads((d / 'config.json').read_text())
        a = '-'.join([meta['variant'], cfg['controller']] + ([meta['tag']] if meta.get('tag') else []))
        by[(meta['path'], cfg['workload'], meta['pair'], a)] = d
    cells = {}
    targets = [(p, w, k) for p, w, k in raised(summ, 'cand-bbrv3', 'cpu')]
    for path in ['loopback', 'S5', 'S6']:
        for workload in ['stream', 'datagram']:
            for role in ['send', 'receive']:
                blocks = []
                for b in range(1, 6):
                    c, rr = by.get((path, workload, b, 'cand-bbrv3')), by.get((path, workload, b, 'reno-reno'))
                    if c and rr:
                        cs, cg = split_of(c, role)
                        rs, rg = split_of(rr, role)
                        blocks.append(dict(cand=cs, reno=rs, cand_gib=cg, reno_gib=rg))
                if not blocks:
                    continue
                loc = R.receiver_localization(blocks)
                flagged = (path, workload, f'{role}_cpu') in targets
                cells[f'{path}/{workload}/{role}'] = dict(flag_raised=flagged, registered=(role == 'receive' and flagged), **loc,
                                                         blocks=blocks)
                print('cpu split', path, workload, role, 'raised' if flagged else '-', loc['outcome'], round(loc['window_ratio'], 3),
                      round(loc['outside_share'], 2))
    save('cpu_split', cells)


# ---- memory (#711's rules) -------------------------------------------------------------------


def view_of(phases, name):
    """A directory of symlinks to the observations counted under the rerun rule, for loaders that glob one root."""
    view = ART / 'views' / name
    if view.exists():
        shutil.rmtree(view)
    view.mkdir(parents=True)
    record = []
    for ph in phases:
        dirs, rec = chosen(ph)
        record += rec
        for d in dirs:
            os.symlink(d, view / d.name)
    return view, record


def memory():
    import attribution as A
    view, record = view_of(['timeline', 'heapsites'], 'memory')
    A.OBS = view
    A.ART = run.ART
    A.BOOKKEEPING = re.compile(A.BOOKKEEPING.pattern + r'|ackhandler\.\(\*deliveryRecords\)')
    entries, tl = A.timeline()
    hs = heapsites4(A)
    summ = readiness_summary()
    cells = []
    for path, workload, key in raised(summ, 'cand-bbrv3', 'rss'):
        role = key.split('_')[0]
        t = next((c for c in tl if (c['path'], c['workload']) == (path, workload)), None)
        h = [c for c in hs if (c['path'], c['workload'], c['role']) == (path, workload, role)]
        tclass = t[role]['classification'] if t else 'not staged'
        classes = [c['classification'] for c in h]
        if tclass == 'startup transient':
            final = 'startup transient'
        elif tclass in ('steady-state excess', 'mixed'):
            final = H.heap_cell(classes)
        else:
            final = 'unresolved'
        # Descriptive per-group decomposition (D5): median excess MiB per group over the usable blocks. Never classifies.
        groups = sorted({g for c in h for g in c['group_mib']})
        decomposition = {g: statistics.median(c['group_mib'].get(g, 0.0) for c in h) for g in groups} if h else {}
        cells.append(dict(path=path, workload=workload, role=role, timeline=tclass, heapsites=classes, classification=final,
                          blocks=[c['block'] for c in h], excess_mib=[c['excess_mib'] for c in h], group_mib=[c['group_mib'] for c in h],
                          decomposition_mib=decomposition, top_sites=[c['top_sites'][:6] for c in h]))
        print('memory', path, workload, role, tclass, classes, '->', final)
    save('memory', dict(blocks=record, timeline=tl, timeline_runs=entries, heapsites=hs, cells=cells))


def heapsites4(A, blocks=(1, 2, 3, 4)):
    """attribution.heapsites (#711, byte-identical in attribution.py) with its block list widened to four (D5); nothing else changes."""
    rows = A.load('heapsites')
    entries = {}
    for r in rows:
        d = A.OBS / r['id']
        cfg = json.loads((d / 'config.json').read_text())
        t0 = cfg['start_unix_ns']
        m0, m1 = t0 + cfg['warmup_ms'] * 1_000_000, t0 + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000
        for role in ['send', 'receive']:
            s = json.loads((d / f'{role}.series.json').read_text())
            col = {c: i for i, c in enumerate(s['columns'])}
            meas = [x for x in s['samples'] if m0 <= x[0] < m1]
            peak = max(meas, key=lambda x: x[col['/memory/classes/heap/objects:bytes']])
            idx, ns = min(s['heap_series'], key=lambda p: abs(p[1] - peak[0]))
            binary = A.ART / 'bin' / f"{r['variant']}-fixture"
            st = A.sites(binary, d / f'{role}-heap' / f'heap-{idx:03d}.pprof')
            entries[(r['path'], r['workload'], r['pair'], A.arm(r), role)] = dict(
                id=r['id'], peak_s=(peak[0] - t0) / 1e9, profile_s=(ns - t0) / 1e9, total_inuse_mib=sum(st.values()) / 2**20, sites=st)
    cells = []
    for path in ['S5', 'S6']:
        for workload in ['stream', 'datagram']:
            for role in ['send', 'receive']:
                for block in blocks:
                    c = entries.get((path, workload, block, 'cand-diag-counted-bbrv3', role))
                    ref = entries.get((path, workload, block, 'reno-diag-reno', role))
                    if not c or not ref:
                        continue
                    fns = set(c['sites']) | set(ref['sites'])
                    diff = {f: c['sites'].get(f, 0) - ref['sites'].get(f, 0) for f in fns}
                    groups = {}
                    for f, v in diff.items():
                        if A.classify(f) != 'instrument':
                            groups[A.classify(f)] = groups.get(A.classify(f), 0) + v
                    excess = sum(groups.values())
                    top = sorted(diff.items(), key=lambda kv: -kv[1])[:12]
                    positive = sum(v for v in groups.values() if v > 0)
                    share = {g: v / positive for g, v in groups.items() if v > 0} if positive > 0 else {}
                    lead = max(share, key=share.get) if share else None
                    cells.append(dict(path=path, workload=workload, role=role, block=block,
                                      cand_inuse_mib=c['total_inuse_mib'], reno_inuse_mib=ref['total_inuse_mib'], excess_mib=excess / 2**20,
                                      group_mib={g: v / 2**20 for g, v in groups.items()}, positive_share=share,
                                      classification=(lead if lead in ('delivery', 'bookkeeping') and share[lead] >= 0.70 else 'unresolved'),
                                      top_sites=[[f, v / 2**20, A.classify(f)] for f, v in top],
                                      peak_s=c['peak_s'], profile_s=c['profile_s']))
    return cells


# ---- receiver harness (conditional) ----------------------------------------------------------


def rx_metrics(r):
    """#735's receiver figures for a perf-attached observation (its measure.py endpoint_metrics, receiver role)."""
    c = r['receive']['counters']
    gib = r['useful_bytes'] / 2**30
    ev = lambda *names: sum(c[n]['value'] for n in names)
    return dict(T=c['task-clock']['value'] / 1e3 / gib, I=ev('instructions:u', 'instructions:k') / gib, C=ev('cycles:u', 'cycles:k') / gib,
                A=c['msr/aperf/']['value'] / gib, M=c['msr/mperf/']['value'] / gib, switches=c['context-switches']['value'] / gib,
                running_pct=min(c[e]['running_pct'] or 0 for e in ['task-clock', 'msr/aperf/', 'msr/mperf/']))


def recvharness():
    summ = readiness_summary()
    targets = {w for p, w, k in raised(summ, 'cand-bbrv3', 'cpu') if p == 'S5' and k == 'receive_cpu'}
    res = dict(triggered=sorted(targets))
    for workload in ['stream', 'datagram']:
        if workload not in targets:
            res[workload] = dict(outcome='not triggered')
            continue
        dirs, record = chosen('recvharness', 'S5')
        by = {}
        for d in dirs:
            r = summarize(d)
            if r['workload'] == workload:
                by.setdefault(r['pair'], {})[arm(r)] = r
        blocks, desc = [], []
        for b, x in sorted(by.items()):
            if not all(a in x for a in ['reno-reno', 'cand-bbrv3', 'reno-reno-perf', 'cand-bbrv3-perf']):
                continue
            mc, mr = rx_metrics(x['cand-bbrv3-perf']), rx_metrics(x['reno-reno-perf'])
            counted = min(mc['running_pct'], mr['running_pct']) >= 95
            cpu = lambda a, role='receive': x[a][role]['cpu_seconds_per_gib']
            blocks.append(dict(block=b, P=cpu('cand-bbrv3') / cpu('reno-reno'), Q=cpu('cand-bbrv3-perf') / cpu('reno-reno-perf'),
                               W=mc['T'] / mr['T'] if counted else None))
            desc.append(dict(block=b, cand=mc, reno=mr, components=RX.components(mc, mr) if counted else None,
                             sender_P=cpu('cand-bbrv3', 'send') / cpu('reno-reno', 'send'),
                             sender_Q=cpu('cand-bbrv3-perf', 'send') / cpu('reno-reno-perf', 'send'),
                             goodput=dict(plain=x['cand-bbrv3']['goodput_mbps'] / x['reno-reno']['goodput_mbps'],
                                          perf=x['cand-bbrv3-perf']['goodput_mbps'] / x['reno-reno-perf']['goodput_mbps'])))
        v = H.harness_verdict(blocks)
        res[workload] = dict(outcome=v['verdict'], detail=v, blocks=blocks, descriptive=desc, record=record)
        print('recvharness', workload, v['verdict'], {k: (round(vv, 3) if isinstance(vv, float) else vv) for k, vv in v.items() if k != 'P_minus_Q'})
    save('recvharness', res)


# ---- preservation ----------------------------------------------------------------------------


def series_retained(d, role):
    cfg, w = window(d)
    s = json.loads((d / f'{role}.series.json').read_text())
    col = {c: i for i, c in enumerate(s['columns'])}
    meas = [x for x in s['samples'] if w[0] <= x[0] < w[1]]
    peak = max(meas, key=lambda x: x[col['/memory/classes/heap/objects:bytes']])
    return peak[col['/memory/classes/heap/objects:bytes']] / 2**20, peak[0], s


def presrss():
    import attribution as A
    summ = readiness_summary()
    res = {}
    for path, workload, key in raised(summ, 'cand-reno', 'rss'):
        role = key.split('_')[0]
        dirs, record = chosen('presrss', path)
        by = {}
        for d in dirs:
            r = summarize(d)
            if r['workload'] == workload:
                by.setdefault(r['pair'], {})[arm(r)] = (d, r)
        blocks, reno_rss = [], []
        for b in sorted(by):
            x = by[b]
            if 'cand-diag-reno' not in x or 'reno-diag-reno' not in x:
                blocks.append(None)
                continue
            vals = {}
            for a, (d, r) in x.items():
                retained, peak_ns, s = series_retained(d, role)
                idx, _ = min(s['heap_series'], key=lambda p: abs(p[1] - peak_ns))
                sites = A.sites(ART / 'bin' / f"{r['variant']}-fixture", d / f'{role}-heap' / f'heap-{idx:03d}.pprof')
                vals[a] = dict(rss=r[role]['peak_rss_mib'], retained=retained, sites=sites)
            c, rr = vals['cand-diag-reno'], vals['reno-diag-reno']
            reno_rss.append(rr['rss'])
            groups = {}
            for f in set(c['sites']) | set(rr['sites']):
                g = A.classify(f)
                if g != 'instrument':
                    groups[g] = groups.get(g, 0) + (c['sites'].get(f, 0) - rr['sites'].get(f, 0)) / 2**20
            blocks.append(dict(cand_rss=c['rss'], reno_rss=rr['rss'], cand_retained=c['retained'], reno_retained=rr['retained'],
                               site_excess=groups))
        threshold = 1.10 * statistics.median(reno_rss) if reno_rss else 0
        outcome, detail = R.rss_preservation(blocks, threshold)
        # Readiness context: high-memory outcomes per arm in this cell, same threshold rule on readiness frozen Reno.
        rows = [o for o in summ['observations'] if (o['path'], o['workload']) == (path, workload)]
        rr = [o[role]['peak_rss_mib'] for o in rows if o['arm'] == 'reno-reno']
        th = 1.10 * statistics.median(rr) if rr else 0
        freq = {a: sum(o[role]['peak_rss_mib'] > th for o in rows if o['arm'] == a) for a in ['reno-reno', 'reno-reno-aa', 'cand-reno']}
        res[f'{path}/{workload}/{role}'] = dict(outcome=outcome, detail=detail, threshold_mib=threshold, blocks=record,
                                                readiness_high_outcomes=freq, readiness_threshold_mib=th)
        print('presrss', path, workload, role, outcome, {k: v for k, v in detail.items() if not isinstance(v, list)} if isinstance(detail, dict) else detail)
    save('presrss', res)


def preslat():
    summ = readiness_summary()
    res = {}
    for path, workload, key in raised(summ, 'cand-reno', 'p95'):
        if path != 'loopback':
            res[f'{path}/{workload}'] = dict(outcome='no registered stage', note='only loopback latency cells are staged')
            continue
        dirs, record = chosen('preslat')
        by = {}
        for d in dirs:
            r = summarize(d)
            if r['workload'] != workload:
                continue
            send = json.loads((d / 'send.json').read_text())
            lat = [x / 1e6 for x in send['result']['control']['latency_ns']]
            by.setdefault(r['pair'], {})[arm(r)] = lat
        blocks = [dict(cand=v['cand-reno'], reno=v['reno-reno']) if 'cand-reno' in v and 'reno-reno' in v else None for _, v in sorted(by.items())]
        outcome, detail = R.latency_preservation(blocks)
        res[f'{path}/{workload}'] = dict(outcome=outcome, detail=detail, blocks=record)
        print('preslat', path, workload, outcome, {k: v for k, v in detail.items() if not isinstance(v, list)})
    save('preslat', res)


# ---- D2 cells --------------------------------------------------------------------------------


def d2():
    out = json.loads(OUT.read_text())
    summ = readiness_summary()
    s6 = next(c for c in out['memory']['timeline'] if (c['path'], c['workload']) == ('S6', 'stream'))
    q = s6['queue']
    s5 = {e['workload']: e['flags']['control_p95']['median'] for e in summ['readiness'] if e['path'] == 'S5' and e['arm'] == 'cand-bbrv3'}
    s6flag = next(e['flags']['control_p95'] for e in summ['readiness'] if (e['path'], e['workload'], e['arm']) == ('S6', 'stream', 'cand-bbrv3'))
    up = all(v is not None and v >= 0.70 for v in q['over_25ms_in_up_or_down']) and all(v < 1.0 for v in q['cruise_refill_median_ms'])
    p95_attr = 'selected ProbeBW Up policy' if up and s5['stream'] <= 1.20 else 'unresolved'
    mem = {(c['path'], c['workload'], c['role']): c for c in out['memory']['cells']}
    res = dict(s6_stream_control_p95=dict(value=s6flag['median'], raised=s6flag['raised'], attribution=p95_attr if s6flag['raised'] else 'passes',
                                          over_25ms_in_up_or_down=q['over_25ms_in_up_or_down'], cruise_refill_median_ms=q['cruise_refill_median_ms'],
                                          s5_matched_p95=s5),
               s6_stream_receiver_rss=mem.get(('S6', 'stream', 'receive'), 'passes'),
               s5_sender_rss={w: mem.get(('S5', w, 'send'), 'passes') for w in ['stream', 'datagram']})
    save('d2', res)
    print(json.dumps({k: (v if not isinstance(v, dict) else {kk: vv for kk, vv in v.items() if kk in ('value', 'attribution', 'classification', 'raised')})
                      for k, v in res.items()}, indent=1, default=str))


STEPS = dict(fold=fold, prereq=prereq, readiness=readiness, s5timeline=s5timeline, receiver=receiver, presrss=presrss, preslat=preslat)

if __name__ == '__main__':
    for step in sys.argv[1:]:
        if step == 'all':
            for s in ['readiness', 'receiver', 's5timeline', 'presrss', 'preslat']:
                STEPS[s]()
        else:
            STEPS[step]()
