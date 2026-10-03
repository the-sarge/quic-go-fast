#!/usr/bin/env python3
"""Apply the post-registered #711 attribution rules (README.md, "Post-registration").

Stages: counters (CPU work vs scheduling), timeline (when memory peaks are set;
S6 queue by BBR phase) and heapsites (in-use sites at the measured-window heap
peak). Writes attribution.json; prints the tables the record quotes.
"""
import json
import math
import re
import statistics
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from run import ART, OBS, summarize  # noqa: E402

HERE = Path(__file__).resolve().parent
PHASES = ['startup', 'drain', 'down', 'cruise', 'refill', 'up', 'probertt']
PROBE = {2, 3, 4, 5}
BOTTLENECK_BPS = 100e6


def stats(v):
    v = [x for x in v if x is not None]
    return dict(median=statistics.median(v), minimum=min(v), maximum=max(v), n=len(v)) if v else None


def load(phase):
    out = []
    for p in sorted(OBS.glob(f'{phase}-*/receipt.json')):
        receipt = json.loads(p.read_text())
        assert receipt['exit_codes'] == [0, 0] and receipt.get('relay_exit') in (None, 0), p
        out.append(summarize(p.parent))
    return out


def arm(r):
    return '-'.join([r['variant'], r['controller']] + ([r['tag']] if r.get('tag') else []))


# ---- counters ---------------------------------------------------------------
COUNTER_KEYS = ['cpu_seconds_per_gib', 'instructions_per_gib', 'cycles_per_gib', 'switches_per_gib']


def counters():
    rows = load('counters')
    for r in rows:
        for role in ['send', 'receive']:
            x = r[role]
            x['switches_per_gib'] = x['voluntary_switches_per_gib'] + x['involuntary_switches_per_gib']
    result = {}
    for workload in ['stream', 'datagram']:
        sel = [r for r in rows if r['workload'] == workload]
        blocks = sorted({r['pair'] for r in sel})
        per_arm = {}
        for block in blocks:
            b = {arm(r): r for r in sel if r['pair'] == block}
            ref = b['reno-reno']
            for name, r in b.items():
                if name == 'reno-reno':
                    continue
                for role in ['send', 'receive']:
                    for k in COUNTER_KEYS:
                        per_arm.setdefault(name, {}).setdefault(role, {}).setdefault(k, []).append(r[role][k] / ref[role][k])
        absolute = {}
        for name in sorted({arm(r) for r in sel}):
            rs = [r for r in sel if arm(r) == name]
            absolute[name] = {role: {k: stats([r[role][k] for r in rs]) for k in COUNTER_KEYS} for role in ['send', 'receive']}
        verdicts = {}
        aa = per_arm['reno-reno-aa']
        for name, roles in per_arm.items():
            verdicts[name] = {}
            for role, ks in roles.items():
                ins, cyc, sw = ks['instructions_per_gib'], ks['cycles_per_gib'], ks['switches_per_gib']
                work = statistics.median(ins) > 1.10 and sum(v > 1 for v in ins) >= 5
                sched = not work and (sum(v > 1.5 for v in sw) >= 5 or sum(v > 1.10 for v in cyc) >= 5)
                verdicts[name][role] = dict(
                    ratios={k: dict(stats(v), values=v) for k, v in ks.items()},
                    executed_work_excess=work, wakeup_or_scheduling=sched,
                    aa_cpu_range=[min(aa[role]['cpu_seconds_per_gib']), max(aa[role]['cpu_seconds_per_gib'])],
                    preservation_instructions_ok=0.97 <= statistics.median(ins) <= 1.03)
        result[workload] = dict(blocks=blocks, absolute=absolute, verdicts=verdicts)
    return rows, result


# ---- timeline ---------------------------------------------------------------
def series(d, role):
    s = json.loads((d / f'{role}.series.json').read_text())
    return {c: i for i, c in enumerate(s['columns'])}, s['samples']


def timeline_entry(r):
    d = OBS / r['id']
    cfg = json.loads((d / 'config.json').read_text())
    t0 = cfg['start_unix_ns']
    m0, m1 = t0 + cfg['warmup_ms'] * 1_000_000, t0 + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000
    e = dict(id=r['id'], path=r['path'], workload=r['workload'], arm=arm(r), block=r['pair'])
    w = r['send'].get('work')
    if w and w.get('Phases'):
        probe = [ns for ns, ph in w['Phases'] if ph in PROBE]
        e['t_probe_s'] = (probe[0] - t0) / 1e9 if probe else None
    for role in ['send', 'receive']:
        col, rows = series(d, role)
        final = rows[-1][col['max_rss']]
        t_rss = next(x[0] for x in rows if x[col['max_rss']] >= 0.99 * final)
        foot = lambda x: x[col['/memory/classes/total:bytes']] - x[col['/memory/classes/heap/released:bytes']]
        meas = [x for x in rows if m0 <= x[0] < m1]
        warm = [x for x in rows if x[0] < m0]
        e[role] = dict(t_rss_s=(t_rss - t0) / 1e9, final_rss_mib=final / 2**20,
                       footprint_measured_mib=max(map(foot, meas)) / 2**20, footprint_warmup_mib=max(map(foot, warm)) / 2**20,
                       live_measured_max_mib=max(x[col['/gc/heap/live:bytes']] for x in meas) / 2**20,
                       live_warmup_max_mib=max(x[col['/gc/heap/live:bytes']] for x in warm) / 2**20)
    if r['path'] != 'loopback' and w and w.get('Phases'):
        relay = json.loads((d / 'relay.json').read_text())
        by_phase = {}
        last_ms = (w['UnixNS'] - t0) / 1e6
        tr = w['Phases']
        for t_ms, fwd, _ in relay['QueueSamples']:
            if not cfg['warmup_ms'] <= t_ms < min(cfg['warmup_ms'] + cfg['measure_ms'], last_ms):
                continue
            at = t0 + t_ms * 1_000_000
            cur = None
            for ns, ph in tr:
                if ns > at:
                    break
                cur = ph
            if cur is not None:
                by_phase.setdefault(PHASES[cur], []).append(fwd * 8 / BOTTLENECK_BPS * 1e3)
        over = {k: sum(x > 25 for x in v) for k, v in by_phase.items()}
        total_over = sum(over.values())
        e['queue_by_phase'] = {k: dict(samples=len(v), median_ms=statistics.median(v), share_over_25ms=over[k] / len(v)) for k, v in by_phase.items()}
        e['over_25ms_in_up_or_down'] = (over.get('up', 0) + over.get('down', 0)) / total_over if total_over else None
    return e


def timeline():
    rows = load('timeline')
    entries = [timeline_entry(r) for r in rows]
    cells = []
    for path in ['S5', 'S6']:
        for workload in ['stream', 'datagram']:
            cand = [e for e in entries if (e['path'], e['workload'], e['arm']) == (path, workload, 'cand-diag-counted-bbrv3')]
            reno = {e['block']: e for e in entries if (e['path'], e['workload'], e['arm']) == (path, workload, 'reno-diag-reno')}
            cell = dict(path=path, workload=workload)
            for role in ['send', 'receive']:
                in_startup = all(e[role]['t_rss_s'] <= e['t_probe_s'] + 1 for e in cand)
                fr = [e[role]['footprint_measured_mib'] / reno[e['block']][role]['footprint_measured_mib'] for e in cand]
                steady = statistics.median(fr) > 1.10
                cell[role] = dict(t_rss_s=[e[role]['t_rss_s'] for e in cand], t_probe_s=[e['t_probe_s'] for e in cand],
                                  reno_t_rss_s=[reno[e['block']][role]['t_rss_s'] for e in cand],
                                  footprint_measured_ratio=fr, footprint_warmup_ratio=[e[role]['footprint_warmup_mib'] / reno[e['block']][role]['footprint_warmup_mib'] for e in cand],
                                  live_measured_ratio=[e[role]['live_measured_max_mib'] / reno[e['block']][role]['live_measured_max_mib'] for e in cand],
                                  classification='mixed' if in_startup and steady else 'startup transient' if in_startup else 'steady-state excess' if steady else 'unresolved')
            if path == 'S6':
                cell['queue'] = dict(over_25ms_in_up_or_down=[e['over_25ms_in_up_or_down'] for e in cand],
                                     cruise_refill_median_ms=[statistics.median([e['queue_by_phase'][p]['median_ms'] for p in ('cruise', 'refill') if p in e['queue_by_phase']]) for e in cand],
                                     by_phase=[e['queue_by_phase'] for e in cand])
            cells.append(cell)
    return entries, cells


# ---- heapsites --------------------------------------------------------------
BOOKKEEPING = re.compile(r'ackhandler\.\(\*(recoveryEvidence|recoveryIndex|congestionDispatch|deliverySampler|bbrECNTracker|retained\w*)\)'
                         r'|ackhandler\.(EnableBBR|EnableDeliverySampling|retained\w+|newRecovery\w*|\w*[Rr]ecovery\w*)'
                         r'|ackhandler\.\(\*sentPacketHandler\)\.(captureCongestion\w*|captureBBRECN|appendRetainedAck|retire\w*|beginCongestionFeedback)'
                         r'|ackhandler\.\(\*spaceSlots\)'
                         r'|internal/congestion\.|congestion\.(NewBBRSender|\(\*BBRSender\))')
DELIVERY = re.compile(r'internal/wire\.|wire\.|getPacketBuffer|packetBuffer|sentPacketHistory|\(\*sendStream\)|\(\*receiveStream\)'
                      r'|frameSorter|\(\*datagramQueue\)|\(\*packetPacker\)|\(\*framer\)|\(\*Stream\)|\(\*SendStream\)|\(\*ReceiveStream\)'
                      r'|ackhandler\.\(\*sentPacketHandler\)\.SentPacket|ackhandler\.\(\*packet\)|ackhandler\.init\.func1|getPacket|retransmissionQueue'
                      r'|\(\*Conn\)\.(SendDatagram|handleStreamFrame)|\(\*Transport\)\.runSendQueue')
# Measurement overhead present in both arms: the series sampler and profile writing.
INSTRUMENT = re.compile(r'main\.startDiagSeries|compress/flate|runtime/pprof|bufio\.')


def sites(binary, profile):
    # Runtime frames are hidden so bytes land on the allocating caller.
    out = subprocess.run(['go', 'tool', 'pprof', '-top', '-nodecount', '400', '-sample_index', 'inuse_space', '-unit', 'B', '-hide', '^runtime\\.',
                          str(binary), str(profile)], capture_output=True, text=True, check=True).stdout
    res = {}
    started = False
    for line in out.splitlines():
        if line.strip().startswith('flat'):
            started = True
            continue
        if started and line.strip():
            parts = line.split(None, 5)
            res[parts[5]] = float(parts[0].rstrip('B'))
    return res


def classify(fn):
    if INSTRUMENT.search(fn):
        return 'instrument'
    return 'bookkeeping' if BOOKKEEPING.search(fn) else 'delivery' if DELIVERY.search(fn) else 'other'


def heapsites():
    rows = load('heapsites')
    entries = {}
    for r in rows:
        d = OBS / r['id']
        cfg = json.loads((d / 'config.json').read_text())
        t0 = cfg['start_unix_ns']
        m0, m1 = t0 + cfg['warmup_ms'] * 1_000_000, t0 + (cfg['warmup_ms'] + cfg['measure_ms']) * 1_000_000
        for role in ['send', 'receive']:
            s = json.loads((d / f'{role}.series.json').read_text())
            col = {c: i for i, c in enumerate(s['columns'])}
            meas = [x for x in s['samples'] if m0 <= x[0] < m1]
            peak = max(meas, key=lambda x: x[col['/memory/classes/heap/objects:bytes']])
            idx, ns = min(s['heap_series'], key=lambda p: abs(p[1] - peak[0]))
            binary = ART / 'bin' / f"{r['variant']}-fixture"
            st = sites(binary, d / f'{role}-heap' / f'heap-{idx:03d}.pprof')
            entries[(r['path'], r['workload'], r['pair'], arm(r), role)] = dict(
                id=r['id'], peak_s=(peak[0] - t0) / 1e9, profile_s=(ns - t0) / 1e9, total_inuse_mib=sum(st.values()) / 2**20, sites=st)
    cells = []
    for path in ['S5', 'S6']:
        for workload in ['stream', 'datagram']:
            for role in ['send', 'receive']:
                for block in (1, 2):
                    c = entries.get((path, workload, block, 'cand-diag-counted-bbrv3', role))
                    ref = entries.get((path, workload, block, 'reno-diag-reno', role))
                    if not c or not ref:
                        continue
                    fns = set(c['sites']) | set(ref['sites'])
                    diff = {f: c['sites'].get(f, 0) - ref['sites'].get(f, 0) for f in fns}
                    groups = {}
                    for f, v in diff.items():
                        if classify(f) != 'instrument':
                            groups[classify(f)] = groups.get(classify(f), 0) + v
                    excess = sum(groups.values())
                    top = sorted(diff.items(), key=lambda kv: -kv[1])[:12]
                    positive = sum(v for v in groups.values() if v > 0)
                    share = {g: v / positive for g, v in groups.items() if v > 0} if positive > 0 else {}
                    lead = max(share, key=share.get) if share else None
                    cells.append(dict(path=path, workload=workload, role=role, block=block,
                                      cand_inuse_mib=c['total_inuse_mib'], reno_inuse_mib=ref['total_inuse_mib'], excess_mib=excess / 2**20,
                                      group_mib={g: v / 2**20 for g, v in groups.items()}, positive_share=share,
                                      classification=(lead if lead in ('delivery', 'bookkeeping') and share[lead] >= 0.70 else 'unresolved'),
                                      top_sites=[[f, v / 2**20, classify(f)] for f, v in top],
                                      peak_s=c['peak_s'], profile_s=c['profile_s']))
    return cells


if __name__ == '__main__':
    stages = sys.argv[1:] or ['counters', 'timeline', 'heapsites']
    path = HERE / 'attribution.json'
    out = json.loads(path.read_text()) if path.exists() else {}
    if 'counters' in stages:
        _, out['counters'] = counters()
        for w, res in out['counters'].items():
            for name, roles in res['verdicts'].items():
                for role, v in roles.items():
                    rt = v['ratios']
                    print(f"counters {w:8} {name:13} {role:7} cpu {rt['cpu_seconds_per_gib']['median']:.3f} ins {rt['instructions_per_gib']['median']:.3f} "
                          f"cyc {rt['cycles_per_gib']['median']:.3f} sw {rt['switches_per_gib']['median']:.3f} work={v['executed_work_excess']} "
                          f"sched={v['wakeup_or_scheduling']} aa_cpu={[round(x, 3) for x in v['aa_cpu_range']]}")
    if 'timeline' in stages:
        entries, out['timeline'] = timeline()
        out['timeline_runs'] = entries
        for c in out['timeline']:
            for role in ['send', 'receive']:
                x = c[role]
                print(f"timeline {c['path']} {c['workload']:8} {role:7} t_rss {[round(v, 1) for v in x['t_rss_s']]} t_probe {[round(v, 1) for v in x['t_probe_s']]} "
                      f"reno_t_rss {[round(v, 1) for v in x['reno_t_rss_s']]} foot_meas {[round(v, 2) for v in x['footprint_measured_ratio']]} "
                      f"live_meas {[round(v, 2) for v in x['live_measured_ratio']]} -> {x['classification']}")
            if 'queue' in c:
                q = c['queue']
                print(f"   queue up/down share {[round(v, 2) if v is not None else None for v in q['over_25ms_in_up_or_down']]} cruise/refill median ms {[round(v, 2) for v in q['cruise_refill_median_ms']]}")
    if 'heapsites' in stages:
        out['heapsites'] = heapsites()
        for c in out['heapsites']:
            print(f"heapsites {c['path']} {c['workload']:8} {c['role']:7} b{c['block']} cand {c['cand_inuse_mib']:.2f} reno {c['reno_inuse_mib']:.2f} "
                  f"excess {c['excess_mib']:.2f} groups {{{', '.join(f'{k}: {v:.2f}' for k, v in c['group_mib'].items())}}} -> {c['classification']}")
            for f, v, g in c['top_sites'][:6]:
                print(f"     {v:7.3f} MiB {g:11} {f}")
    path.write_text(json.dumps(out, indent=1) + '\n')
