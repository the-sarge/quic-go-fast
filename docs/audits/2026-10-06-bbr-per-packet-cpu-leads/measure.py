#!/usr/bin/env python3
"""#735 analysis driver: #714's unchanged outcome rules, plus the receiver rule on the same blocks.

intervene.py is #714's, byte-identical (build.py checks it); this driver only
points it at this ticket's observations and adds the receiver measures to its
per-endpoint metrics. Nothing here changes a registered rule.

Usage on any host holding the observations:
    measure.py outcome m1 [m2 ...]   # #714's keep/null/negative/inconclusive -> outcomes.json
    measure.py receiver              # the receiver rule on every registered measurement -> receiver.json
"""
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402
import intervene  # noqa: E402  (sets run.ART to #714's directory at import; reset below)
import receiver  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-per-packet-cpu-leads'
run.OBS = run.ART / 'observations'
RECEIVER_EVENTS = ['task-clock', 'msr/aperf/', 'msr/mperf/']  # the events matrix.py adds to #712's sixteen
run.PERF_STAT_EVENTS = run.PERF_STAT_EVENTS + [e for e in RECEIVER_EVENTS if e not in run.PERF_STAT_EVENTS]
_endpoint_metrics = intervene.endpoint_metrics


def endpoint_metrics(row, role, packets):
    out = _endpoint_metrics(row, role, packets)
    c = row[role]['counters']
    gib = row['useful_bytes'] / 2**30
    out['rx'] = dict(T=c['task-clock']['value'] / 1e3 / gib, I=out['instructions/gib'], C=out['cycles/gib'],
                     A=c['msr/aperf/']['value'] / gib, M=c['msr/mperf/']['value'] / gib,
                     switches=c['context-switches']['value'] / gib, migrations=c['cpu-migrations']['value'] / gib,
                     running_pct=min(c[e]['running_pct'] or 0 for e in RECEIVER_EVENTS))
    d = run.OBS / row['id']
    cfg = json.loads((d / 'config.json').read_text())
    idle = d / f'{role}.idle.json'
    if idle.exists():
        out['rx']['idle'] = receiver.idle_rates(json.loads(idle.read_text()), gib, cfg['measure_ms'] / 1e3)
    out['rx']['whole_run_cpu_s'] = row[role]['cpu_seconds_per_gib']
    return out


intervene.endpoint_metrics = endpoint_metrics


def usable_rx(b, arms):
    return all(b[a]['receive']['rx']['running_pct'] >= 95 for a in arms)


def receiver_measure(stage, prev, new):
    per_workload, integrity, failures, report = intervene.load_stage(stage, prev, new)
    out = {}
    for wl, blocks in per_workload.items():
        res = {}
        for arm in ['prev', 'new', 'renonew']:
            bl = [dict(arm=b[arm]['receive']['rx'], aa=b['aa']['receive']['rx'], reno=b['reno']['receive']['rx'])
                  for b in blocks if usable_rx(b, [arm, 'aa', 'reno'])]
            v = receiver.verdict(bl)
            if bl:
                v['descriptive'] = describe(bl)
            res[arm] = v
        out[wl] = res
    return dict(stage=stage, prev=prev, new=new, blocks=report, integrity=integrity, failures=failures, workloads=out)


def describe(bl):
    """Medians of per-block ratios against frozen Reno, for the reader; never decisive."""
    r = lambda f: statistics.median(f(b['arm']) / f(b['reno']) for b in bl)
    out = dict(task_clock_per_gib=r(lambda m: m['T']), cycles_per_gib=r(lambda m: m['C']), instructions_per_gib=r(lambda m: m['I']),
               aperf_per_mperf=r(lambda m: m['A'] / m['M']), aperf_per_cycle=r(lambda m: m['A'] / m['C']),
               switches_per_gib=r(lambda m: m['switches']), whole_run_cpu_per_gib=r(lambda m: m['whole_run_cpu_s']),
               uncounted_clocks_per_switch=dict(arm=statistics.median((b['arm']['A'] - b['arm']['C']) / b['arm']['switches'] for b in bl),
                                                reno=statistics.median((b['reno']['A'] - b['reno']['C']) / b['reno']['switches'] for b in bl)),
               running_ghz_rel=dict(arm=statistics.median(b['arm']['A'] / b['arm']['M'] for b in bl),
                                    reno=statistics.median(b['reno']['A'] / b['reno']['M'] for b in bl)))
    if all('idle' in b['arm'] and 'idle' in b['reno'] for b in bl):
        states = bl[0]['arm']['idle'].keys()
        out['idle'] = {s: dict(entries_per_gib=r(lambda m: m['idle'][s]['entries_per_gib'] or 1e-12),
                               residency=dict(arm=statistics.median(b['arm']['idle'][s]['residency'] for b in bl),
                                              reno=statistics.median(b['reno']['idle'][s]['residency'] for b in bl))) for s in states}
    return out


def main(argv):
    plan = json.loads((HERE / 'measurements.json').read_text())
    if argv[1] == 'outcome':
        intervene.main(argv[2:])
        return
    assert argv[1] == 'receiver', argv
    out = {stage: receiver_measure(stage, p['prev'], p['new']) for stage, p in plan.items() if p.get('ran')}
    decisive = next((s for s, p in plan.items() if p.get('ran') and p['prev'] == 'r8'), None)
    out['decisive'] = dict(stage=decisive, arm='prev',
                           verdicts={wl: out[decisive]['workloads'][wl]['prev']['verdict'] for wl in ['stream', 'datagram']} if decisive else None)
    (HERE / 'receiver.json').write_text(json.dumps(out, indent=2) + '\n')
    for s, v in out.items():
        if s != 'decisive':
            print(s, {wl: {a: x['verdict'] for a, x in r.items()} for wl, r in v['workloads'].items()})
    print('decisive', out['decisive'])


if __name__ == '__main__':
    main(sys.argv)
