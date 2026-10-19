#!/usr/bin/env python3
"""Finite #808 L6 order on minimax; observations run sequentially, never concurrently.

Copied from #777's matrix.py (71e88789) with recorded changes:
- the trace arm (#792's `cand-trace`, the revision with #792's overlay,
  BBRv3) joins pairs 1-3 only, at a seeded position in #777's unchanged
  triple order (`order`); pairs 4-5 run #777's triples; `trace` never enters
  the classification (analyze.py classifies the triple);
- reruns run in two passes, every arm a pair-workload has, with the same seed
  (#714): first #777's pass, unchanged, for each pair-workload whose triple
  is unusable or contaminated, at most four; then a trace pass for each
  pair-workload whose triple was clean but whose trace observation is
  unusable, contaminated or incomplete (#792's `trace_state`), only with the
  slots the first pass left (so at most four in all, #777's cap, and the
  trace never takes a slot from a triple; a clean original triple is kept by
  `choose_triple` whatever its rerun shows);
- smoke adds #792's full-length `trace` STREAM observation, which must also
  leave a complete trace;
- while any observation runs, the file RUNNING exists at the worktree root
  (the marker agreed with the wiremux sessions that share minimax, #792's);
  when the driver already holds RUNNING for the whole order, an observation
  leaves it in place;
- the registered binary pins include `cand-trace-fixture`, and `cand-fixture`
  is this record's (build.REGISTERED).
#777's docstring follows.

Adapted from the #748 slice 6 readiness record's matrix.py (9b38f0ab, #739's):
`host_facts` and `wait_quiet` are verbatim except for this record's CPU lists
(`wait_quiet` is #738's quiet-host gate: it triggers on host state only, never
on an outcome, and the registered contamination test still judges every
observation). Recorded changes: one stage, `main`, of five pairs; each pair
runs both workloads (stream first in odd pairs, as the readiness blocks
alternated), and each pair-workload triple runs its three arms in a seeded
random order after one quiet-host gate. A triple holding an unusable or
contaminated observation is rerun once, all three arms, with the same seed
(`follow`, the operator's #714 rule), at most four triples. Launch under
`taskset -c 1` (run.HARNESS_CPUS).

Usage: matrix.py binaries | prereq | smoke | main | follow
       matrix.py dev WORKLOAD ARM [LABEL]
       matrix.py hostfacts LABEL
"""
import contextlib
import hashlib
import json
import pathlib
import random
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import l6rules  # noqa: E402
import run  # noqa: E402

ARMS = ['reno', 'cand', 'aa']  # the classified triple (#777's)
TRACE_PAIRS = (1, 2, 3)        # pairs that also run the trace arm (#808)
SEED_BASE = 7700          # L6 has no random loss, so the relay seed changes nothing; recorded per pair
MAX_RERUN_TRIPLES = 4
MARKER = run.ROOT / 'RUNNING'


def arms_of(pair):
    return ARMS + ['trace'] if pair in TRACE_PAIRS else list(ARMS)


@contextlib.contextmanager
def running(label):
    held = MARKER.exists()  # the driver holds it for the whole order; leave it in place then
    MARKER.write_text(f'{label} {time.time_ns()}\n')
    try:
        yield
    finally:
        if not held:
            MARKER.unlink(missing_ok=True)


def run_case(*args, **kw):
    with running('-'.join(str(a) for a in args[:4])):
        return run.run_case(*args, **kw)


def host_facts():
    """Read-only record of host settings (verbatim from #735's matrix.py, this record's CPUs)."""
    cpu = lambda n, f: (pathlib.Path(f'/sys/devices/system/cpu/cpu{n}/{f}').read_text().strip()
                        if pathlib.Path(f'/sys/devices/system/cpu/cpu{n}/{f}').exists() else None)
    out = {}
    for n in [1] + run.FIXTURE_CPUS:
        out[n] = dict(driver=cpu(n, 'cpufreq/scaling_driver'), governor=cpu(n, 'cpufreq/scaling_governor'),
                      epp=cpu(n, 'cpufreq/energy_performance_preference'), min=cpu(n, 'cpufreq/scaling_min_freq'),
                      max=cpu(n, 'cpufreq/scaling_max_freq'), boost=cpu(n, 'cpufreq/boost'),
                      idle_states=[cpu(n, f'cpuidle/state{s}/name') for s in range(4)],
                      idle_disabled=[cpu(n, f'cpuidle/state{s}/disable') for s in range(4)])
    sysfs = lambda p: pathlib.Path(p).read_text().strip() if pathlib.Path(p).exists() else None
    return dict(unix_ns=time.time_ns(), cpus=out, kernel=sysfs('/proc/sys/kernel/osrelease'),
                perf_event_paranoid=sysfs('/proc/sys/kernel/perf_event_paranoid'),
                amd_pstate_status=sysfs('/sys/devices/system/cpu/amd_pstate/status'),
                cpuidle_governor=sysfs('/sys/devices/system/cpu/cpuidle/current_governor'),
                timer_migration=sysfs('/proc/sys/kernel/timer_migration'))


# ---- #738's quiet-host gate (verbatim from its matrix.py, 070e1597) ----
QUIET_WINDOW_S = 60      # seconds of quiet needed before a block starts
QUIET_MEAN = 0.05        # foreign cores, mean over the window
QUIET_PEAK = 0.30        # foreign cores, any one-second sample
QUIET_GIVE_UP_S = 6 * 3600


def wait_quiet():
    """Wait until the fixture cores and siblings carry under 0.05 cores mean (0.30 peak) for 60 s."""
    cpus = run.FIXTURE_CPUS + run.SIBLING_CPUS
    started = time.time()
    while True:
        samples = []
        prev = run.proc_stat_cpus()
        for _ in range(QUIET_WINDOW_S):
            time.sleep(1)
            cur = run.proc_stat_cpus()
            samples.append(sum(cur[c]['busy'] - prev[c]['busy'] for c in cpus) / 100)
            prev = cur
        mean, peak = sum(samples) / len(samples), max(samples)
        if mean < QUIET_MEAN and peak < QUIET_PEAK:
            print(f'quiet: mean {mean:.3f} peak {peak:.2f} after {time.time() - started:.0f} s', flush=True)
            return
        print(f'busy: mean {mean:.3f} peak {peak:.2f}; waiting', flush=True)
        if time.time() - started > QUIET_GIVE_UP_S:
            raise SystemExit('host never quiet; stage not continued')


def order(pair, workload):
    """The registered arm order of one pair-workload: #777's seeded triple order, with the trace arm
    (pairs 1-3) inserted at a position drawn from its own seed."""
    triple_order = random.Random(f'777-{pair}-{workload}').sample(ARMS, len(ARMS))
    if pair in TRACE_PAIRS:
        triple_order.insert(random.Random(f'808-{pair}-{workload}').randrange(len(ARMS) + 1), 'trace')
    return triple_order


def workloads(pair):
    return ['stream', 'datagram'] if pair % 2 else ['datagram', 'stream']


def triple(phase, pair, workload):
    wait_quiet()
    rows = {}
    for arm in order(pair, workload):
        try:
            rows[arm] = run_case(phase, workload, pair, arm, SEED_BASE + pair)
        except Exception as e:  # a failed observation stays on disk; follow decides
            print(f'{phase} p{pair} {workload} {arm} failed: {e!r}', flush=True)
            rows[arm] = None
        correctness(run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}')
    return rows


REPORTS = ['focal-send', 'focal-receive', 'competitor-send', 'competitor-receive']
BINARIES = ['reno-fixture', 'cand-fixture', 'cand-trace-fixture', 'relay2', 'calibrate2', 'launch']


def correctness(d):
    """Stop the stage on a correctness-gate failure in observation directory d (l6rules.correctness).

    Reads all four endpoint reports, so the gate holds even when the
    observation's summary fails.
    """
    reports = {}
    for name in REPORTS:
        path = d / f'{name}.json'
        try:
            reports[name] = json.loads(path.read_text()) if path.exists() else None
        except ValueError:
            reports[name] = None  # a truncated report is an unusable observation, not a verdict
    failures = l6rules.correctness(reports)
    if failures:
        run.write(run.ART / 'correctness-failure.json', dict(observation=d.name, failures=failures))
        raise SystemExit(f'correctness failure in {d.name}: {failures}; stage stopped')


def registered_hashes():
    import build  # the registered pins live in build.py
    pins = {key: h for (variant, key), h in {**build.EXPECTED, **build.REGISTERED}.items()}
    return {'reno-fixture': build.EXPECTED[('reno', 'fixture')], 'cand-fixture': build.REGISTERED[('cand', 'fixture')],
            'cand-trace-fixture': build.REGISTERED[('cand-trace', 'fixture')],
            'relay2': pins['relay2'], 'calibrate2': pins['calibrate2'], 'launch': pins['launch']}


def check_binaries(bin_dir, expected):
    """Problems with the deployed binaries against the registered hashes (empty when all match)."""
    problems = []
    for name in BINARIES:
        path = bin_dir / name
        if not path.exists():
            problems.append(f'{name} missing')
        elif hashlib.sha256(path.read_bytes()).hexdigest() != expected[name]:
            problems.append(f'{name} hash differs from the registered {expected[name][:16]}')
    return problems


def raw_diagnostics(d):
    """What each endpoint reported, read from its raw report whether or not the observation summarizes.

    Missing values stay missing (None); relative pause times and CPU loads are
    computed only when their timing evidence exists.
    """
    out = {}
    measured_start = None
    cfg_path = d / 'competitor-config.json'
    if cfg_path.exists():
        cfg = json.loads(cfg_path.read_text())
        measured_start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    receipt = json.loads((d / 'receipt.json').read_text()) if (d / 'receipt.json').exists() else None
    wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9 if receipt else None
    for name in REPORTS:
        path = d / f'{name}.json'
        try:
            rec = json.loads(path.read_text()) if path.exists() else None
        except ValueError:
            out[name] = dict(report='unreadable')
            continue
        if rec is None:
            out[name] = dict(report='missing')
            continue
        result = rec.get('result') or {}
        control = result.get('control') or {}
        res = rec.get('resources') or {}
        who = name.split('-')[0]
        entry = dict(error=rec.get('error'), errors=result.get('errors'), control_error=control.get('error'),
                     control_counts={k: control.get(k) for k in ['offered', 'replies', 'skipped', 'unresolved']},
                     cpu_seconds=res.get('cpu_seconds'), peak_rss_bytes=res.get('peak_rss_bytes'),
                     cpu_load=res['cpu_seconds'] / wall / l6rules.ALLOTTED_CORES[who] if wall and res.get('cpu_seconds') is not None else None)
        pause = result.get('bulk_pause_observed_ns')
        if name == 'competitor-send':
            entry['pause_observed_ns'] = pause
            entry['pause_observed_s'] = [(t - measured_start) / 1e9 for t in pause] if pause is not None and measured_start else None
        out[name] = entry
    out['receipt'] = 'present' if receipt else 'missing'
    return out


def trace_state(d):
    """For a trace-arm observation: whether the overlay left a complete trace (dumped, no overflow, one sender) (#792's)."""
    path = d / 'focal-trace.json'
    if not path.exists():
        return dict(complete=False, reason='no trace file' + (' (dump error recorded)' if (d / 'focal-trace.json.error').exists() else ''))
    data = json.loads(path.read_text())
    ok = data.get('overflow') == 0 and data.get('foreign_events') == 0 and data.get('rows')
    return dict(complete=bool(ok), rows=len(data.get('rows') or []), overflow=data.get('overflow'),
                foreign_events=data.get('foreign_events'), until_unix_ns=data.get('until_unix_ns'))


def observation(d, variant, expected):
    """The registered verdict for one observation directory, shared by the counted stage and smoke.

    Returns dict(usable, contaminated, reasons, contamination, udp, raw). The
    correctness gate runs first and stops the stage on a failure.
    """
    correctness(d)
    raw = raw_diagnostics(d)
    receipt_path = d / 'receipt.json'
    if not receipt_path.exists():
        return dict(usable=False, contaminated=False, reasons=['no receipt'], contamination=None, udp=None, raw=raw)
    receipt = json.loads(receipt_path.read_text())
    reasons = []
    try:
        summary = run.summarize(d)
    except Exception as e:  # noqa: BLE001 - recorded as the reason
        summary, reasons = None, [f'summary: {e!r}']
    for who, h in (receipt.get('binary_sha256') or {}).items():
        want = expected[f"{variant if who == 'focal' else 'reno'}-fixture"]
        if h != want:
            reasons.append(f'{who} fixture hash differs from the registered {want[:16]}')
    if receipt.get('relay_sha256') != expected['relay2']:
        reasons.append('relay hash differs from the registered one')
    wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9
    reasons = l6rules.health(summary, receipt, wall) + reasons
    facts_path, cpu_path = d / 'host-facts.json', d / 'host-cpu.json'
    udp = None
    if facts_path.exists():
        facts = json.loads(facts_path.read_text())
        udp = {k: facts['udp_after'].get(k, 0) - facts['udp_before'].get(k, 0) for k in l6rules.UDP_ERRORS}
        reasons += l6rules.udp_reasons(udp)
    else:
        reasons.append('host facts missing')
    if facts_path.exists() and cpu_path.exists():
        c = l6rules.contamination(json.loads(cpu_path.read_text()), facts['measure_window_unix_ns'], run.FIXTURE_CPUS, run.SIBLING_CPUS)
    else:
        c = dict(contaminated=None, reason='host samples missing')
    trace = None
    if variant == 'cand-trace':
        trace = trace_state(d)
        if not trace['complete']:
            reasons.append(f"incomplete trace: {trace.get('reason') or trace}")
    usable, dirty, reasons = l6rules.verdict(reasons, c)
    return dict(usable=usable, contaminated=dirty, reasons=reasons, contamination=c, udp=udp, raw=raw, trace=trace)


def judge(phase, pair, workload):
    """(usable, contaminated, detail) for one triple on disk; detail holds each arm's verdict and diagnostics,
    the trace arm's included in pairs 1-3. Usable and contaminated are the triple's (#777's), as classified."""
    expected = registered_hashes()
    detail = {arm: observation(run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}', run.ARMS[arm][0], expected) for arm in arms_of(pair)}
    triple = [detail[arm] for arm in ARMS]
    return all(v['usable'] for v in triple), any(v['contaminated'] for v in triple), detail


def trace_clean(detail):
    """True when the pair has no trace arm, or its trace observation is usable and uncontaminated (#792's rule)."""
    t = detail.get('trace')
    return t is None or (t['usable'] and not t['contaminated'])


def follow():
    """Rerun unusable or contaminated pair-workloads once (#714), recording every decision: first #777's pass over
    the triples, at most MAX_RERUN_TRIPLES; then a trace pass, with only the slots left, for pairs whose triple
    was clean but whose trace was not (#808)."""
    decisions, reruns = [], 0
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in workloads(pair):
            usable, contaminated, detail = judge('main', pair, workload)
            entry = dict(pair=pair, workload=workload, usable=usable, contaminated=contaminated, trace_clean=trace_clean(detail), detail=detail)
            if not usable or contaminated:
                if reruns < MAX_RERUN_TRIPLES:
                    reruns += 1
                    triple('rerun', pair, workload)
                    again = judge('rerun', pair, workload)
                    entry['rerun'] = dict(zip(['usable', 'contaminated', 'detail'], again), trace_clean=trace_clean(again[2]), trigger='triple')
                else:
                    entry['rerun'] = 'cap reached'
            decisions.append(entry)
            run.write(run.ART / 'follow-decisions.json', decisions)
    for entry in decisions:
        if entry['usable'] and not entry['contaminated'] and not entry['trace_clean']:
            if reruns < MAX_RERUN_TRIPLES:
                reruns += 1
                triple('rerun', entry['pair'], entry['workload'])
                again = judge('rerun', entry['pair'], entry['workload'])
                entry['rerun'] = dict(zip(['usable', 'contaminated', 'detail'], again), trace_clean=trace_clean(again[2]), trigger='trace')
            else:
                entry['trace_rerun'] = 'cap reached'
            run.write(run.ART / 'follow-decisions.json', decisions)
    return decisions


def threads_in_sets(processes):
    """Every process has a non-empty, error-free thread snapshot, each thread inside its CPU set."""
    want = lambda r: run.AFFINITY[r].replace('2,3', '2-3').replace('6,7', '6-7').replace('4,5', '4-5')
    return set(processes) == set(run.AFFINITY) and all(
        not p.get('error') and p.get('threads') and all(t['cpus'] == want(r) for t in p['threads'].values())
        for r, p in processes.items())


def smoke():
    """Three full-length observations: the candidate and frozen Reno (#777's) and the trace arm (#792's), excluded from every statistic.

    Each must be usable under the shared registered verdict (observation(): the
    correctness gate, health, hashes, host UDP errors and an evaluable
    contamination test; an affirmatively contaminated smoke observation is
    reported, not failed), with every thread inside its CPU set.
    """
    out, expected = {}, registered_hashes()
    for workload, arm in [('stream', 'cand'), ('datagram', 'reno'), ('stream', 'trace')]:
        d = run.OBS / f'smoke-L6-{workload}-p0-{arm}'
        key = f'{workload}-{arm}'
        error = None
        try:
            run_case('smoke', workload, 0, arm, 9990)
        except Exception as e:  # noqa: BLE001 - the failed observation is judged and kept; the stage still fails
            error = repr(e)
        v = observation(d, run.ARMS[arm][0], expected)
        facts_path = d / 'host-facts.json'
        facts = json.loads(facts_path.read_text()) if facts_path.exists() else {}
        threads = threads_in_sets(facts['mid']['processes']) if facts.get('mid') else False
        out[key] = dict(error=error, threads_in_cpu_sets=threads, sockets=(facts.get('mid') or {}).get('sockets'), **v)
    passed = all(v['error'] is None and v['usable'] and v['threads_in_cpu_sets'] for v in out.values())
    run.write(run.ART / 'smoke.json', dict(passed=passed, observations=out))
    print('smoke', 'pass' if passed else 'FAIL', flush=True)
    if not passed:
        raise SystemExit('smoke failed: a prerequisite gap')


def main():
    stage = sys.argv[1]
    run.ensure_credentials()
    if stage == 'hostfacts':
        (run.ART / 'hostfacts').mkdir(parents=True, exist_ok=True)
        run.write(run.ART / 'hostfacts' / f'{sys.argv[2]}.json', host_facts())
    elif stage == 'binaries':
        problems = check_binaries(run.ART / 'bin', registered_hashes())
        run.write(run.ART / 'binaries.json', dict(passed=not problems, problems=problems))
        print('binaries', 'pass' if not problems else problems, flush=True)
        if problems:
            raise SystemExit('deployed binaries differ from the registered ones')
    elif stage == 'prereq':
        import prereq
        wait_quiet()
        with running('prereq'):
            prereq.main()
    elif stage == 'smoke':
        wait_quiet()
        smoke()
    elif stage == 'dev':
        workload, arm = sys.argv[2], sys.argv[3]
        run_case('dev', workload, 0, arm, 9991, tag=sys.argv[4] if len(sys.argv) > 4 else '')
    elif stage == 'main':
        for pair in range(1, l6rules.PAIRS + 1):
            for workload in workloads(pair):
                triple('main', pair, workload)
    elif stage == 'follow':
        follow()
    else:
        raise SystemExit(__doc__)


if __name__ == '__main__':
    main()
