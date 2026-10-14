#!/usr/bin/env python3
"""#744 analysis: Stage 0's validation, Stage 1's readings and Stage 2's outcome, under the registered rules.

Blocks are chosen by the operator's #714 rerun rule (rules.choose_block, #715's
copy), separately for each registered mask of a stage's arms: a mask's attempt
counts only if every arm in it is present, usable and uncontaminated, and a rerun
replaces the original only when it is usable and uncontaminated. Stage 1 has a
plain mask (frozen Reno, r9, the second r9, r9 at depth 8: the depth and
baseline readings) and a recorder mask (both revisions plain and with the
recorder: perturbation and mechanism), so a recorder fault never removes clean
plain evidence. An observation is usable when it exits cleanly with receiver
integrity (#712's summarize), its executable and source label match the
committed build receipt for its variant (`builds/`), and, for a recorder arm,
its recorder output passes ctl.usable. Contamination is #712's test (localize.py).

Stage 0 is a prerequisite of the mechanism reading: the data-level synthetic
cases (`datalevel`), the runtime synthetic cases (`syn`) and Stage 1's
perturbation must all pass, the first two with receipts whose fingerprint
matches the current code and recorder builds. Missing, failed or stale
validation is an instrumentation gap; insufficient perturbation evidence is an
evidence gap. Every step writes its result, explicit gaps included.

Usage: ctlstages.py datalevel|syn|s1|s2|all       (writes results.json beside this file)
"""
import hashlib
import json
import os
import statistics
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from run import summarize  # noqa: E402
import rules as R  # noqa: E402
import rules738  # noqa: E402  (#738's rules.py, byte-identical: preservation_cell, s5_screen)
import ctl  # noqa: E402
import ctlrules as C  # noqa: E402

ART = run.ART
ROOTS = [ART / 'observations', ART / 'observations-rerun']
OUT = HERE / 'results.json'
BUILDS = HERE / 'builds'
SYN_CASES = ('sq', 'rq', 'sw', 'rl', 'rw', 'un')
CAPACITY = {'cand-ctl': 32, 'cand8-ctl': 8, **{f'syn-{c}-ctl': 32 for c in SYN_CASES}}
PLAIN = ['reno-reno', 'cand-bbrv3', 'cand-bbrv3-aa', 'cand8-bbrv3']
RECORDER = ['cand-bbrv3', 'cand8-bbrv3', 'cand-ctl-bbrv3', 'cand8-ctl-bbrv3']
SYN_ARMS = ['cand-ctl-bbrv3', 'cand-ctl-bbrv3-aa'] + [f'syn-{c}-ctl-bbrv3' for c in SYN_CASES]
S2_LOOPBACK = ['reno-reno', 'reno-reno-aa', 'cand-bbrv3', 'cand-bbrv3-aa', 'new-bbrv3', 'new-reno']
S2_LONG = ['reno-reno', 'cand-bbrv3', 'cand-bbrv3-aa', 'new-bbrv3']
S2_S5 = ['cand-bbrv3', 'cand-bbrv3-aa', 'new-bbrv3']
# stage -> (phase, path, {mask: arms})
MASKS = {
    'syn': ('syn', 'loopback-syn', {'all': SYN_ARMS}),
    's1': ('s1', 'loopback-long', {'plain': PLAIN, 'recorder': RECORDER}),
    's2-loopback': ('s2', 'loopback', {'all': S2_LOOPBACK}),
    's2-long': ('s2', 'loopback-long', {'all': S2_LONG}),
    's2-s5': ('s2', 'S5', {'all': S2_S5}),
}
# Files whose content a Stage 0 receipt binds (with the recorder builds' receipts).
FINGERPRINT = ['ctl.py', 'ctlrules.py', 'ctlstages.py', 'ctlrules_test.py', 'rules.py', 'rules_test.py', 'ctl/ctlrec.go',
               'build.py', 'matrix.py']


def arm(meta, cfg):
    return '-'.join([meta['variant'], cfg['controller']] + ([meta['tag']] if meta.get('tag') else []))


def fingerprint():
    h = hashlib.sha256()
    for rel in FINGERPRINT:
        h.update(rel.encode() + b'\0' + (HERE / rel).read_bytes() + b'\0')
    for v in sorted(CAPACITY):
        p = BUILDS / f'{v}-build.json'
        h.update(v.encode() + b'\0' + (json.loads(p.read_text())['builds']['fixture']['sha256'].encode() if p.exists() else b'missing'))
    return h.hexdigest()


def contaminated(d):
    from localize import contamination
    facts = json.loads((d / 'host-facts.json').read_text())
    return contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'])['contaminated']


def build_label(receipt):
    cmd = receipt['builds']['fixture']['command']
    return next(a.split('=', 1)[1] for a in cmd if a.startswith('-X main.sourceRevision='))


def provenance(d, meta):
    """Reasons the observation's executable, source label or relay differ from the committed build receipts."""
    reasons = []
    p = BUILDS / f"{meta['variant']}-build.json"
    if not p.exists():
        return [f"no committed build receipt for {meta['variant']}"]
    receipt = json.loads(p.read_text())
    obs = json.loads((d / 'receipt.json').read_text())
    if obs['binary_sha256'] != receipt['builds']['fixture']['sha256']:
        reasons.append('executable differs from its build receipt')
    if json.loads((d / 'send.json').read_text()).get('source') != build_label(receipt):
        reasons.append('source label differs from its build receipt')
    if obs.get('relay_sha256'):
        relay = BUILDS / 'relay-build.json'
        if not relay.exists() or obs['relay_sha256'] != json.loads(relay.read_text())['builds']['relay-linux']['sha256']:
            reasons.append('relay differs from its build receipt')
    return reasons


def usable(d):
    """(usable, reasons) for one observation directory."""
    try:
        receipt = json.loads((d / 'receipt.json').read_text())
        if receipt['exit_codes'] != [0, 0] or receipt.get('relay_exit') not in (None, 0):
            return False, ['exit codes']
        summarize(d)
        meta = json.loads((d / 'meta.json').read_text())
        reasons = provenance(d, meta)
        for role in ['send', 'receive']:
            p = d / f'{role}.perf-stat.csv'
            if p.exists() and any(v['running_pct'] is not None and v['running_pct'] < 95 for v in run.perf_counters(p).values()):
                reasons.append(f'{role} perf counters multiplexed')
        if meta['variant'] in CAPACITY:
            ok, why, _, _ = ctl.usable(d, CAPACITY[meta['variant']], ctl.MIN_COMPLETE[meta['path']])
            reasons += why
        if meta['variant'] == 'new' and meta['path'] == 'loopback-long' and json.loads((d / 'config.json').read_text())['controller'] == 'bbrv3':
            reasons += engagement_reasons(d)
        return not reasons, reasons
    except (AssertionError, FileNotFoundError, KeyError, ValueError, StopIteration) as e:
        return False, [repr(e)]


def blocks_of(phase, path):
    """{(workload, block): {'original': [dirs], 'rerun': [dirs]}} for one phase and path."""
    out = {}
    for label, root in zip(['original', 'rerun'], ROOTS):
        for p in sorted(root.glob(f'{phase}-{path}-*/meta.json')):
            meta = json.loads(p.read_text())
            if meta['phase'] != phase or meta['path'] != path:
                continue
            cfg = json.loads((p.parent / 'config.json').read_text())
            out.setdefault((cfg['workload'], meta['pair']), {}).setdefault(label, []).append(p.parent)
    return out


def chosen(stage):
    """Per (workload, block): each mask's counted attempt and status (the #714 rule), the overall status (clean
    only if every mask is clean) and the evidence behind them."""
    phase, path, masks = MASKS[stage]
    record = []
    for (wl, b), att in sorted(blocks_of(phase, path).items()):
        per = {}
        for label in ['original', 'rerun']:
            if label not in att:
                continue
            names = {}
            for d in att[label]:
                names[arm(json.loads((d / 'meta.json').read_text()), json.loads((d / 'config.json').read_text()))] = d
            per[label] = dict(dirs=names, use={n: usable(d) for n, d in names.items()},
                              cont={n: (contaminated(d) if (d / 'host-cpu.json').exists() else True) for n, d in names.items()})
        attempts = [label for label in ['original', 'rerun'] if label in per]
        masks_out = {}
        for m, arms in masks.items():
            states = [(label, all(a in per[label]['dirs'] and per[label]['use'][a][0] for a in arms),
                       any(per[label]['cont'].get(a, False) for a in arms)) for label in attempts]
            if 'original' not in per:
                masks_out[m] = dict(status='unusable', counted=None, dirs={})
                continue
            pick, status = R.choose_block(states)
            masks_out[m] = dict(status=status, counted=pick, dirs={a: str(per[pick]['dirs'][a]) for a in arms if a in per[pick]['dirs']})
        statuses = [v['status'] for v in masks_out.values()]
        overall = 'clean' if all(s == 'clean' for s in statuses) else 'unusable' if 'unusable' in statuses else 'contaminated'
        record.append(dict(workload=wl, block=b, status=overall, attempts=[(label,) for label in attempts], masks=masks_out,
                           evidence={label: dict(unusable={n: u[1] for n, u in v['use'].items() if not u[0]},
                                                 contaminated=sorted(n for n, c in v['cont'].items() if c),
                                                 missing=sorted(set(a for arms in masks.values() for a in arms) - set(v['dirs'])))
                                     for label, v in per.items()}))
    return record


def clean_dirs(record, mask, workload='stream'):
    return [r['masks'][mask]['dirs'] for r in record if r['workload'] == workload and r['masks'][mask]['status'] == 'clean']


def replies_ms(d):
    return [v / 1e6 for v in json.loads((Path(d) / 'send.json').read_text())['result']['control']['latency_ns']]


def recorder_block(d, capacity):
    """(complete rows, incomplete fixture latencies in ns, facts) for one usable recorder observation."""
    path = json.loads((Path(d) / 'meta.json').read_text())['path']
    ok, reasons, rows, facts = ctl.usable(Path(d), capacity, ctl.MIN_COMPLETE[path])
    assert ok, reasons
    return rows, [x['fixture_latency_ns'] for x in facts['incomplete']], facts


def load_results():
    return json.loads(OUT.read_text()) if OUT.exists() else {}


def save(key, value):
    out = load_results()
    out[key] = value
    OUT.write_text(json.dumps(out, indent=1, default=str) + '\n')


def seg_stats(rows):
    if not rows:
        return None
    out = {}
    for s in ctl.SEGMENTS + ['total']:
        v = sorted(r[s] / 1000 for r in rows)
        out[s] = dict(p50_us=statistics.median(v), p95_us=R.pooled_p95(v), mean_us=statistics.fmean(v))
    for side in ('sender_queue', 'receiver_queue'):
        out[side] = {k: statistics.median(r[side][k] for r in rows) for k in ('entries_ahead', 'bytes_ahead', 'entry_bytes')}
        out[side]['bytes_ahead_p95'] = R.pooled_p95([r[side]['bytes_ahead'] for r in rows])
    return out


def med(values):
    v = [x for x in values if x is not None]
    return statistics.median(v) if v else None


# ---- Stage 0: data-level and runtime synthetic cases ------------------------------------------

def datalevel():
    """Run the data-level synthetic cases and #715's rule tests; record the result bound to the current fingerprint."""
    env = {**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'}
    runs = {}
    for t in ('ctlrules_test.py', 'rules_test.py'):
        p = subprocess.run([sys.executable, str(HERE / t)], cwd=HERE, env=env, capture_output=True, text=True)
        tail = p.stderr.strip().splitlines()[-3:]
        runs[t] = dict(exit=p.returncode, tail=tail)
    res = dict(passes=all(v['exit'] == 0 for v in runs.values()), runs=runs, fingerprint=fingerprint())
    save('stage0_data_level', res)
    print('datalevel', 'pass' if res['passes'] else 'FAIL', runs)
    return res


SYN = {  # case arm -> (expected ending, check on the reading's detail and the median segment rises in us)
    'syn-sq-ctl-bbrv3': ('queue wait', lambda d, rise: C.targets(d) == ['sender'] and rise['s_queue'] >= 210),
    'syn-rq-ctl-bbrv3': ('queue wait', lambda d, rise: C.targets(d) == ['receiver'] and rise['r_queue'] >= 210),
    'syn-sw-ctl-bbrv3': ('queue wait', lambda d, rise: 'sender' in C.targets(d) and rise['s_submit'] >= 14),
    # A busy receiver loop also makes the reply wait behind bursts of ACK-only entries, so mixed is a correct
    # reading too; the case fails if it reads queue wait or does not name the receive backlog.
    'syn-rl-ctl-bbrv3': (('load or other', 'mixed'), lambda d, rise: 'r_backlog' in d['named_segments'] and rise['r_backlog'] >= 50),
    'syn-rw-ctl-bbrv3': ('load or other', lambda d, rise: 'r_app' in d['named_segments'] and rise['r_app'] >= 210
                         and not {'s_queue', 'r_queue'} & set(d['named_segments'])),
    'syn-un-ctl-bbrv3': ('load or other', lambda d, rise: d['unplaced'] and 't_req' in d['named_segments']),
    'cand-ctl-bbrv3-aa': ('inconclusive', lambda d, rise: True),  # the near-zero p95 rise
}
SYN_BLOCKS = 4  # registered runtime geometry (README "Runtime synthetic cases")


def syn():
    record = chosen('syn')
    dirs = clean_dirs(record, 'all')
    res = dict(blocks=record, clean_blocks=len(dirs), cases={}, fingerprint=fingerprint())
    for case, (expected, check) in SYN.items():
        blocks = []
        for d in dirs:
            a_rows, a_miss, _ = recorder_block(d[case], 32)
            b_rows, b_miss, _ = recorder_block(d['cand-ctl-bbrv3'], 32)
            blocks.append(dict(a=a_rows, b=b_rows, a_missing=a_miss, b_missing=b_miss, a_fixture=replies_ms(d[case]),
                               b_fixture=replies_ms(d['cand-ctl-bbrv3'])))
        out, detail = C.mechanism_reading(blocks, (0.0, float('inf')), dict(state='pass', reasons=[]), ctl.SEGMENTS,
                                          ctl.QUEUE_SEGMENTS, min_blocks=SYN_BLOCKS - 1)
        rows_a = [r for x in blocks for r in x['a']]
        rows_b = [r for x in blocks for r in x['b']]
        rise = {s: (statistics.median(r[s] for r in rows_a) - statistics.median(r[s] for r in rows_b)) / 1000
                for s in ctl.SEGMENTS} if rows_a and rows_b else {}
        recovered = (out in expected if isinstance(expected, tuple) else out == expected) and out != 'evidence gap' and check(detail, rise)
        res['cases'][case] = dict(expected=expected, ending=out, recovered=bool(recovered), median_rise_us=rise,
                                  reason=detail.get('reason'), detail={k: v for k, v in detail.items() if k != 'segment_shares'},
                                  segment_shares=detail.get('segment_shares'))
        print('syn', case, out, 'recovered' if recovered else 'NOT RECOVERED', {k: round(v, 1) for k, v in rise.items() if abs(v) > 5})
    res['passes'] = all(c['recovered'] for c in res['cases'].values())
    save('stage0_synthetic_runtime', res)
    return res


def stage0_prerequisite(perturbation):
    """The mechanism reading's prerequisite: dict(state, reasons)."""
    results, fp, reasons = load_results(), fingerprint(), []
    for key in ('stage0_data_level', 'stage0_synthetic_runtime'):
        r = results.get(key)
        if r is None:
            reasons.append(f'{key}: missing')
        elif not r.get('passes'):
            reasons.append(f'{key}: failed')
        elif r.get('fingerprint') != fp:
            reasons.append(f'{key}: stale (code or recorder builds changed since it ran)')
    if perturbation['state'] == 'fail':
        reasons.append('perturbation: failed')
    if reasons:
        return dict(state='instrumentation gap', reasons=reasons)
    if perturbation['state'] != 'pass':
        return dict(state='evidence gap', reasons=['perturbation: fewer than five usable recorder-mask blocks'])
    return dict(state='pass', reasons=[])


# ---- Stage 1 ------------------------------------------------------------------------------------

def s1():
    record = chosen('s1')
    res = dict(blocks=record)
    plain = clean_dirs(record, 'plain')
    recd = clean_dirs(record, 'recorder')
    res['clean_blocks'] = dict(plain=len(plain), recorder=len(recd))
    lat = [{a: replies_ms(d[a]) for a in PLAIN} for d in plain]
    good = [{a: summarize(Path(d[a]))['goodput_mbps'] for a in PLAIN} for d in plain]
    # Depth reading (plain mask).
    depth, ddet = C.depth_reading([dict(cand=x['cand-bbrv3'], cand8=x['cand8-bbrv3'], aa=x['cand-bbrv3-aa']) for x in lat])
    res['depth'] = dict(ending=depth, detail=ddet)
    # Baseline reading: #715's latency_preservation, unchanged, for depth 8 against frozen Reno (plain mask).
    out, bdet = C.latency_preservation([dict(cand=x['cand8-bbrv3'], reno=x['reno-reno']) for x in lat])
    res['baseline'] = dict(ending={'regression reproduced': 'BBR baseline'}.get(out, out), rule_outcome=out, detail=bdet)
    # r9 against frozen Reno: descriptive only (Stage 1 cannot revise #740's verdict).
    _, rdet = C.latency_preservation([dict(cand=x['cand-bbrv3'], reno=x['reno-reno']) for x in lat])
    res['r9_vs_reno_descriptive'] = {k: rdet.get(k) for k in ('ratio', 'ci', 'cand_p95_ms', 'reno_p95_ms', 'cand_p50_ms', 'reno_p50_ms')}
    # Perturbation and mechanism (recorder mask).
    pblocks, mblocks, facts, kappa = [], [], [], {'cand': {}, 'cand8': {}}
    for d in recd:
        g = {a: summarize(Path(d[a]))['goodput_mbps'] for a in RECORDER}
        rp = {a: replies_ms(d[a]) for a in RECORDER}
        pblocks.append(dict(cand_goodput=g['cand-bbrv3'], cand_ctl_goodput=g['cand-ctl-bbrv3'], cand=rp['cand-bbrv3'],
                            cand_ctl=rp['cand-ctl-bbrv3'], cand8_goodput=g['cand8-bbrv3'], cand8_ctl_goodput=g['cand8-ctl-bbrv3'],
                            cand8=rp['cand8-bbrv3'], cand8_ctl=rp['cand8-ctl-bbrv3']))
        a_rows, a_miss, fa = recorder_block(d['cand-ctl-bbrv3'], 32)
        b_rows, b_miss, fb = recorder_block(d['cand8-ctl-bbrv3'], 8)
        mblocks.append(dict(a=a_rows, b=b_rows, a_missing=a_miss, b_missing=b_miss, a_fixture=rp['cand-ctl-bbrv3'],
                            b_fixture=rp['cand8-ctl-bbrv3']))
        facts.append(dict(cand={k: v for k, v in fa.items() if k != 'incomplete'}, cand8={k: v for k, v in fb.items() if k != 'incomplete'}))
        for rev, f, arm_name in (('cand', fa, 'cand-ctl-bbrv3'), ('cand8', fb, 'cand8-ctl-bbrv3')):
            useful = g[arm_name] * 1e6 / 8  # useful bytes per second
            for side, rate in f['send_rate_bytes_per_s'].items():
                kappa[rev].setdefault(side, []).append(rate / useful if rate and useful > 0 else None)
    pert = C.perturbation(pblocks)
    res['perturbation'] = pert
    prereq = stage0_prerequisite(pert)
    consistency = tuple(ddet['ci']) if depth != 'evidence gap' else None
    mech, mdet = C.mechanism_reading(mblocks, consistency, prereq, ctl.SEGMENTS, ctl.QUEUE_SEGMENTS)
    res['mechanism'] = dict(ending=mech, detail=mdet, prerequisite=prereq, recorder_facts=facts)
    res['descriptive'] = dict(
        goodput_mbps={a: med(x[a] for x in good) for a in PLAIN},
        p95_ms={a: R.pooled_p95([v for x in lat for v in x[a]]) for a in PLAIN},
        p50_ms={a: med(v for x in lat for v in x[a]) for a in PLAIN},
        segments={'cand-ctl': seg_stats([r for x in mblocks for r in x['a']]), 'cand8-ctl': seg_stats([r for x in mblocks for r in x['b']])})
    # Stage 2 gate and, if it opens, the registered bound at each targeted endpoint and the common bound.
    res['stage2_gate'] = depth == 'depth raises latency' and mech == 'queue wait'
    if res['stage2_gate']:
        a_all, b_all = [r for x in mblocks for r in x['a']], [r for x in mblocks for r in x['b']]
        goodput = {'cand': med(x['cand-bbrv3'] for x in good) * 1e6 / 8, 'cand8': med(x['cand8-bbrv3'] for x in good) * 1e6 / 8}
        tgt = C.targets(mdet)
        per = {side: C.stage2_bound(a_all, b_all, side, goodput, {rev: med(kappa[rev].get(side, [])) or 0 for rev in kappa})
               for side in ('sender', 'receiver')}
        bound, why = C.common_bound(per, tgt)
        res['stage2'] = dict(targets=tgt, per_endpoint={s: v[1] for s, v in per.items()}, bound_bytes=bound, bound_status=why,
                             null_margin=C.stage2_null_margin(ddet['ci'][0]))
    save('stage1', res)
    print('s1 depth', depth, {k: ddet.get(k) for k in ('ratio', 'ci', 'aa_ci', 'blocks_above_1', 'usable_blocks')})
    print('s1 perturbation', pert['state'], {r: {k: v for k, v in pert[r].items() if k != 'goodput_per_block'} for r in ('cand', 'cand8')})
    print('s1 mechanism', mech, mdet.get('reason'), prereq, mdet.get('queue_share'))
    print('s1 baseline', res['baseline']['ending'], {k: bdet.get(k) for k in ('ratio', 'ci')})
    return res


# ---- Stage 2 ------------------------------------------------------------------------------------

STAGE2_SPEC = HERE / 'stage2.json'  # registered after Stage 1, before Stage 2 data (README "Stage 2")
ROLE = {'sender': 'send', 'receiver': 'receive'}


def engagement_reasons(d):
    """A counted loopback-long run of the new revision must show byte-bound refusals at every targeted endpoint."""
    if not STAGE2_SPEC.exists():
        return ['Stage 2 not registered']
    spec = json.loads(STAGE2_SPEC.read_text())
    reasons = []
    for side in spec['targets']:
        p = Path(d) / f'eng-{ROLE[side]}.json'
        if not p.exists():
            reasons.append(f'no engagement counter at the {side}')
        elif json.loads(p.read_text()).get('byte_refusals', 0) < 1:
            reasons.append(f'bound not engaged at the {side}')
    return reasons


def integrity_of(records):
    """D3's integrity guard over every counted attempt: each observation exited cleanly with receiver integrity
    (#712's summarize). True, False, or None when an attempt is missing. Provenance, recorder or engagement
    failures are not integrity failures."""
    failures, missing = [], False
    for r in records:
        for m in r['masks'].values():
            if m['counted'] is None:
                missing = True
            for a, d in m['dirs'].items():
                try:
                    receipt = json.loads((Path(d) / 'receipt.json').read_text())
                    assert receipt['exit_codes'] == [0, 0], 'exit codes'
                    summarize(Path(d))
                except (AssertionError, FileNotFoundError, KeyError, ValueError) as e:
                    failures.append(dict(dir=d, error=repr(e)))
    return (False if failures else None if missing else True), failures


def cycles_per_gib(row, role):
    c = row[role].get('counters_per_gib') or {}
    return c['cycles:u'] + c['cycles:k'] if 'cycles:u' in c and 'cycles:k' in c else None


def s2():
    s1r = load_results().get('stage1') or {}
    if not s1r.get('stage2_gate'):
        res = dict(outcome='not attempted', disposes_class=False, reasons=['Stage 1 did not open the Stage 2 gate'],
                   survivor='r9 81e9dc8c')
        save('stage2', res)
        print('s2', res['outcome'])
        return res
    if not STAGE2_SPEC.exists():
        raise SystemExit('Stage 2 gate is open but its section (stage2.json) is not registered')
    spec = json.loads(STAGE2_SPEC.read_text())
    res = dict(spec=spec)
    status = spec.get('status')  # None, or 'not attempted' | 'unbuildable' | 'gates failed' | 'inert' | 'mistargeted'
    # Efficacy on loopback-long.
    rec_long = chosen('s2-long')
    long_dirs = clean_dirs(rec_long, 'all')
    lat = [{k: replies_ms(d[a]) for k, a in (('cand', 'cand-bbrv3'), ('aa', 'cand-bbrv3-aa'), ('new', 'new-bbrv3'), ('reno', 'reno-reno'))}
           for d in long_dirs]
    efficacy, edet = C.stage2_latency(lat, s1r['stage2']['null_margin'])
    _, sel = C.latency_preservation([dict(cand=x['new'], reno=x['reno']) for x in lat])
    res.update(efficacy=dict(ending=efficacy, detail=edet), long_blocks=rec_long,
               selection_evidence_vs_frozen_reno={k: sel.get(k) for k in ('ratio', 'ci', 'cand_p95_ms', 'reno_p95_ms')})
    # Guards: loopback, integrity and Reno preservation.
    rec_loop = chosen('s2-loopback')
    rec_s5 = chosen('s2-s5')
    loopback, pres = {}, {'send': ([], []), 'receive': ([], [])}
    integrity, integrity_failures = integrity_of(rec_loop + rec_long + rec_s5)
    for wl in C.WORKLOADS:
        pairs_g, pairs_c = [], []
        for d in clean_dirs(rec_loop, 'all', wl):
            row = {a: summarize(Path(d[a])) for a in S2_LOOPBACK}
            r9, aa, new = row['cand-bbrv3'], row['cand-bbrv3-aa'], row['new-bbrv3']
            pairs_g.append((new['goodput_mbps'] / r9['goodput_mbps'], aa['goodput_mbps'] / r9['goodput_mbps']))
            pairs_c.append((new['send']['cpu_seconds_per_gib'] / r9['send']['cpu_seconds_per_gib'],
                            aa['send']['cpu_seconds_per_gib'] / r9['send']['cpu_seconds_per_gib']))
            for role in ('send', 'receive'):
                reno, reno_aa, renonew = (cycles_per_gib(row[a], role) for a in ('reno-reno', 'reno-reno-aa', 'new-reno'))
                if None not in (reno, reno_aa, renonew):
                    pres[role][0].append(renonew / reno)
                    pres[role][1].append(reno_aa / reno)
        loopback[wl] = dict(goodput=C._range_movement(pairs_g), cpu=C._range_movement(pairs_c))
    preservation = {role: rules738.preservation_cell(v[0], v[1]) if len(v[0]) >= C.MIN_BLOCKS else 'insufficient'
                    for role, v in pres.items()}
    # S5 screen (#738's s5_screen, r9 as reference).
    screens = {}
    for wl in C.WORKLOADS:
        rows = []
        for d in clean_dirs(rec_s5, 'all', wl):
            row = {a: summarize(Path(d[a])) for a in S2_S5}
            r9, aa, new = row['cand-bbrv3'], row['cand-bbrv3-aa'], row['new-bbrv3']
            rows.append(dict(goodput_ratio_new=new['goodput_mbps'] / r9['goodput_mbps'], goodput_ratio_aa=aa['goodput_mbps'] / r9['goodput_mbps'],
                             cpu_ratio_new=new['send']['cpu_seconds_per_gib'] / r9['send']['cpu_seconds_per_gib'],
                             cpu_ratio_aa=aa['send']['cpu_seconds_per_gib'] / r9['send']['cpu_seconds_per_gib']))
        screens[wl] = rules738.s5_screen(rows)
    guards = C.guard_states(loopback, integrity, preservation, screens)
    outcome, disposes, reasons = C.stage2_outcome(status, efficacy, guards)
    res.update(loopback=loopback, preservation=preservation, preservation_input={r: dict(renonew=v[0], aa=v[1]) for r, v in pres.items()},
               screens=screens, integrity=integrity, integrity_failures=integrity_failures, guards=guards, outcome=outcome, disposes_class=disposes, reasons=reasons,
               survivor=f"new revision {spec['revision']}" if outcome == 'kept' else 'r9 81e9dc8c')
    save('stage2', res)
    print('s2', outcome, 'disposes' if disposes else '', reasons, res['survivor'])
    return res


STEPS = dict(datalevel=datalevel, syn=syn, s1=s1, s2=s2)

if __name__ == '__main__':
    for k in (list(STEPS) if sys.argv[1] == 'all' else sys.argv[1:]):
        STEPS[k]()
