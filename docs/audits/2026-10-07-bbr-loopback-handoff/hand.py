#!/usr/bin/env python3
"""#738 analysis: folds observations through rules.py into results.json.

Reads #715's observation directories (run.OBS and observations-rerun) under the
artifact directory, after `pull.sh` copies them back from minimax, and never
edits them. Each stage's result is written under its key in results.json.

Usage: hand.py stage0 | s1 | all
"""
import json
import statistics
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
import rules  # noqa: E402
from localize import contamination  # noqa: E402

HERE = Path(__file__).resolve().parent
RESULTS = HERE / 'results.json'
MEASURE_MS = 20000
SYNTH = {  # build.py SYNTH, restated so the analysis needs no build tree
    'slowworker': dict(synSlowWorkerNS=20_000),
    'busyloop': dict(synBusyLoopNS=20_000),
    'delaysignal': dict(synSignalDelayNS=50_000),
    'cwnd': dict(cwnd=12_000),
    'mixed': dict(synSignalDelayNS=100_000, cwnd=8_000, synAlternate=True, synPhaseNS=2_000_000_000),
}
WORKLOADS = ['stream', 'datagram']


def observation(directory):
    """One observation: its summary row (None if it failed), recorder output, usability and contamination."""
    out = dict(id=directory.name, dir=str(directory.relative_to(run.ART)))
    receipt = json.loads((directory / 'receipt.json').read_text()) if (directory / 'receipt.json').exists() else None
    if receipt is None or receipt['exit_codes'] != [0, 0]:
        return dict(out, usable=False, reason='failed or missing receipt', contaminated=None)
    try:
        row = run.summarize(directory)
    except (AssertionError, KeyError, FileNotFoundError) as e:
        return dict(out, usable=False, reason=f'summary: {e!r}', contaminated=None)
    meta = json.loads((directory / 'meta.json').read_text())
    facts = json.loads((directory / 'host-facts.json').read_text())
    cont = contamination(json.loads((directory / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])
    out.update(variant=meta['variant'], tag=meta.get('tag', ''), workload=row['workload'], pair=meta['pair'],
               goodput_mbps=row['goodput_mbps'], sender_cpu_per_gib=row['send']['cpu_seconds_per_gib'],
               receiver_cpu_per_gib=row['receive']['cpu_seconds_per_gib'], contamination=cont,
               contaminated=bool(cont.get('contaminated')))
    if meta['variant'].endswith('-timeline'):
        p = directory / 'send.hand.json'
        h = json.loads(p.read_text()) if p.exists() else None
        ok, why = rules.hand_usable(h, MEASURE_MS)
        out['hand_usable'], out['hand_reason'] = ok, why
        if not ok:
            return dict(out, usable=False, reason='recorder: ' + why)
        m = rules.hand_measures(h)
        out['measures'] = {k: v for k, v in m.items()}
        out['verdict'] = rules.verdict(m)
        out['conditions'] = sorted(rules.conditions(m))
        out['handoff_kind'] = rules.handoff_kind(m)
    return dict(out, usable=True, reason='')


def phase_obs(phase, rerun=False):
    base = run.ART / ('observations-rerun' if rerun else 'observations')
    return [observation(d) for d in sorted(base.glob(f'{phase}-*')) if d.is_dir()]


def arm_key(o):
    return o['variant'] + (f"-{o['tag']}" if o['tag'] else '')


# ---- Stage 0 ------------------------------------------------------------------------------------
def stage0():
    out = dict(synthetic={}, perturbation={}, activation={})
    for o in phase_obs('synthetic'):
        case = o['id'].split('-syn-')[1].split('-timeline')[0]
        wl = o['id'].split('-')[2]
        if not o['usable']:
            out['synthetic'].setdefault(case, {})[wl] = dict(recovered=False, usable=False, reason=o['reason'])
            continue
        r = rules.recovery(case, o['measures'], SYNTH[case])
        out['synthetic'].setdefault(case, {})[wl] = dict(r, usable=True, contaminated=o['contaminated'],
                                                         goodput_mbps=o['goodput_mbps'], measures=o['measures'])
    for wl in WORKLOADS:
        out['synthetic_passed'] = out.get('synthetic_passed', {})
        out['synthetic_passed'][wl] = all(out['synthetic'].get(c, {}).get(wl, {}).get('recovered') for c in SYNTH)
    pre = [o for o in phase_obs('preflight')]
    for wl in WORKLOADS:
        by = {}
        for o in pre:
            if o.get('workload') == wl:
                by.setdefault(o['pair'], {})[o['variant']] = o
        ratios, blocks = [], []
        for b, arms in sorted(by.items()):
            plain, inst = arms.get('cand'), arms.get('cand-hand-timeline')
            usable = plain and inst and plain['usable'] and inst['usable']
            contaminated = usable and (plain['contaminated'] or inst['contaminated'])
            blocks.append(dict(block=b, usable=bool(usable), contaminated=bool(contaminated),
                               ratio=inst['goodput_mbps'] / plain['goodput_mbps'] if usable else None))
            if usable and not contaminated:
                ratios.append(inst['goodput_mbps'] / plain['goodput_mbps'])
        out['perturbation'][wl] = dict(rules.perturbation(ratios), blocks=blocks) if ratios else dict(passed=False, blocks=blocks)
    for o in phase_obs('activation'):
        wl = o['id'].split('-')[2]
        out['activation'][wl] = dict(rules.activation(o['measures']), usable=o['usable']) if o['usable'] else dict(engaged=False, usable=False)
    write('stage0', out)
    return out


# ---- Stage 1 ------------------------------------------------------------------------------------
S1_ARMS = ['reno', 'cand-hand-timeline', 'cand-hand-timeline-aa', 'prev-hand-timeline', 'cand4q-hand-timeline']


def s1_blocks(wl):
    """Per block: the chosen attempt (original or rerun) under the operator's #714 rule."""
    chosen = {}
    attempts = {}
    for rerun in (False, True):
        for o in phase_obs('s1', rerun):
            if o.get('workload', o['id'].split('-')[2]) != wl:
                continue
            b = int(o['id'].split('-p')[1].split('-')[0])
            attempts.setdefault(b, {}).setdefault(rerun, []).append(o)
    for b, by in sorted(attempts.items()):
        labels = []
        for rerun in (False, True):
            obs = by.get(rerun)
            if obs is None:
                continue
            usable = len(obs) == len(S1_ARMS) and all(o['usable'] for o in obs)
            cont = any(o.get('contaminated') for o in obs)
            labels.append((rerun, usable, cont))
        label, status = rules.choose_block(labels)
        chosen[b] = dict(status=status, rerun=label, arms={arm_key(o): o for o in by[label]})
    return chosen


def s1():
    gates = gate_status()
    act = json.loads(RESULTS.read_text()).get('stage0', {}).get('activation', {}) if RESULTS.exists() else {}
    out = dict(gates=gates)
    for wl in WORKLOADS:
        blocks = s1_blocks(wl)
        usable = {b: x for b, x in blocks.items() if x['status'] == 'clean'}
        verdicts = [x['arms']['cand-hand-timeline']['verdict'] for x in usable.values()]
        workload_verdict = rules.majority(verdicts)
        rows = []
        cr, kick = [], []
        for b, x in sorted(usable.items()):
            a = x['arms']
            r8, aa, prev, q4, reno = (a['cand-hand-timeline'], a['cand-hand-timeline-aa'], a['prev-hand-timeline'],
                                      a['cand4q-hand-timeline'], a['reno'])
            g = lambda o: o['goodput_mbps']
            c = lambda o: o['sender_cpu_per_gib']
            rows.append(dict(block=b, rerun=x['rerun'], verdicts={k: o.get('verdict') for k, o in a.items()},
                             goodput_vs_reno={k: g(o) / g(reno) for k, o in a.items()},
                             cpu_vs_reno={k: c(o) / c(reno) for k, o in a.items()},
                             goodput_mbps={k: g(o) for k, o in a.items()}, sender_cpu_per_gib={k: c(o) for k, o in a.items()},
                             measures={k: o.get('measures') for k, o in a.items() if o.get('measures')}))
            cr.append(dict(ratio=g(q4) / g(r8), aa=g(aa) / g(r8), cpu_ratio=c(q4) / c(r8), cpu_aa=c(aa) / c(r8),
                           engaged=q4['measures']['engaged']))
            kick.append(dict(ratio=g(prev) / g(r8), aa=g(aa) / g(r8), exposure=r8['measures']['exposure'],
                             paced_stops=r8['measures']['paced_stops']))
        res = dict(blocks={b: x['status'] for b, x in blocks.items()}, usable_blocks=len(usable), verdicts=verdicts,
                   verdict=workload_verdict, addressable=rules.addressable(workload_verdict),
                   handoff_kinds=[x['arms']['cand-hand-timeline']['handoff_kind'] for x in usable.values()],
                   aa_verdicts=[x['arms']['cand-hand-timeline-aa']['verdict'] for x in usable.values()],
                   credit_response=rules.credit_response(cr, gates['usable'], act.get(wl, {}).get('engaged', False)),
                   rows=rows)
        if wl == 'stream':
            res['kick'] = rules.kick_reading(kick)
        res['describe'] = describe(rows)
        out[wl] = res
    write('s1', out)
    return out


def describe(rows):
    """Medians across usable blocks of each arm's recorder measures (descriptive, never decisive)."""
    keys = ['u_w', 'u_c', 'local', 'credit', 'queue', 'window', 'cwnd', 'paced', 'app', 'j_wb', 'j_hh', 'j_ov', 'j_lb', 'exposure',
            'max_pending_q', 'bytes_per_entry', 'packets_per_entry', 'credit_wait_mean_ns', 'queue_wait_mean_ns',
            'credit_signal_mean_ns', 'queue_signal_mean_ns', 'credit_wake_mean_ns', 'queue_wake_mean_ns', 'submit_mean_ns',
            'local_waits_per_s']
    out = {}
    for arm in S1_ARMS[1:]:
        ms = [r['measures'][arm] for r in rows if arm in r['measures']]
        out[arm] = {k: statistics.median(m[k] for m in ms) for k in keys if ms and all(m.get(k) is not None for m in ms)}
        out[arm]['goodput_vs_reno'] = statistics.median(r['goodput_vs_reno'][arm] for r in rows) if rows else None
        out[arm]['cpu_vs_reno'] = statistics.median(r['cpu_vs_reno'][arm] for r in rows) if rows else None
    return out


def gate_status():
    """The diagnostic arm's gates (gates/): usable when the 4Q tree passed on the Mac and natively, and the
    suite detected the injected second violation (the mutant tree failed)."""
    def status(name):
        p = HERE / 'gates' / f'{name}.log'
        if not p.exists():
            return None
        lines = [l for l in p.read_text().splitlines() if l.startswith('# status ')]
        return int(lines[-1].split()[-1]) if lines else None
    native = HERE / 'gates' / 'gate-4q-native.log'
    native_ok = native.exists() and 'FAIL' not in native.read_text() and 'panic' not in native.read_text()
    s = dict(r8=status('gate-r8'), q4=status('gate-4q'), m1=status('gate-m1'), q4_native=native_ok)
    s['usable'] = s['r8'] == 0 and s['q4'] == 0 and s['m1'] not in (None, 0) and native_ok
    return s


def write(key, value):
    res = json.loads(RESULTS.read_text()) if RESULTS.exists() else {}
    res[key] = value
    RESULTS.write_text(json.dumps(res, indent=1, sort_keys=True, default=str) + '\n')


if __name__ == '__main__':
    what = sys.argv[1] if len(sys.argv) > 1 else 'all'
    if what in ('stage0', 'all'):
        print(json.dumps({k: v for k, v in stage0().items() if k != 'synthetic'}, indent=1, default=str))
    if what in ('s1', 'all'):
        r = s1()
        print(json.dumps({wl: {k: r[wl][k] for k in ['verdict', 'verdicts', 'credit_response'] + (['kick'] if wl == 'stream' else [])}
                          for wl in WORKLOADS}, indent=1, default=str))
