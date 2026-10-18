#!/usr/bin/env python3
"""Finite #792 order on minimax; observations run sequentially, never concurrently.

Adapted from #777's matrix.py (71e88789). `host_facts`, `wait_quiet` (#738's
quiet-host gate), `correctness`, `raw_diagnostics`, `observation` (the shared
per-observation verdict), `threads_in_sets` and the stage layout are #777's,
with #777's `l6rules.py` and calibration (`prereq.py`) used unchanged from its
record directory. Recorded changes:
- arms `trace` (the port with the measurement-only overlay) and `cand` (the
  plain port, byte-identical to #777's), both BBRv3 next to the frozen Reno
  competitor; no matched-Reno or A/A arm (#777 measured those);
- three pairs; each pair runs both workloads (stream first in odd pairs), each
  pair-workload runs its two arms in a seeded random order after one quiet-host
  gate;
- a pair-workload holding an unusable or contaminated observation is rerun
  once, both arms, with the same seed (`follow`, the operator's #714 rule), at
  most two;
- smoke is one full-length `trace` observation (STREAM), which must also leave
  a complete trace;
- while any observation runs, the file RUNNING exists at the worktree root (the
  marker agreed with the wiremux sessions that share minimax).
Launch under `taskset -c 1` (run.HARNESS_CPUS).

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

HERE = Path(__file__).resolve().parent
L6 = HERE.parent / '2026-10-17-bbr-l6-coexistence'
sys.dont_write_bytecode = True  # never write __pycache__ into #777's frozen directory
sys.path.insert(0, str(L6))     # #777's l6rules and prereq, unchanged
sys.path.insert(0, str(HERE))   # this record's run.py comes first
import run  # noqa: E402
import l6rules  # noqa: E402

assert Path(run.__file__).parent == HERE and Path(l6rules.__file__).parent == L6
ARMS = ['trace', 'cand']
PAIRS = 3
SEED_BASE = 7920          # L6 has no random loss, so the relay seed changes nothing; recorded per pair
MAX_RERUNS = 2
MARKER = run.ROOT / 'RUNNING'


@contextlib.contextmanager
def running(name):
    MARKER.write_text(f'{name} {time.time_ns()}\n')
    try:
        yield
    finally:
        MARKER.unlink(missing_ok=True)


def run_case(*args, **kw):
    with running('-'.join(str(a) for a in args[:4])):
        return run.run_case(*args, **kw)


def host_facts():
    """Read-only record of host settings (verbatim from #777's matrix.py)."""
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


# ---- #738's quiet-host gate (verbatim from #777's matrix.py) ----
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
    """The registered seeded random arm order of one pair-workload."""
    return random.Random(f'792-{pair}-{workload}').sample(ARMS, len(ARMS))


def workloads(pair):
    return ['stream', 'datagram'] if pair % 2 else ['datagram', 'stream']


def name(phase, pair, workload, arm):
    return f'{phase}-L6-{workload}-p{pair}-{arm}'


def block(phase, pair, workload):
    wait_quiet()
    for arm in order(pair, workload):
        try:
            run_case(phase, workload, pair, arm, SEED_BASE + pair)
        except Exception as e:  # a failed observation stays on disk; follow decides
            print(f'{phase} p{pair} {workload} {arm} failed: {e!r}', flush=True)
        correctness(run.OBS / name(phase, pair, workload, arm))


REPORTS = ['focal-send', 'focal-receive', 'competitor-send', 'competitor-receive']
BINARIES = ['reno-fixture', 'cand-fixture', 'cand-trace-fixture', 'relay2', 'calibrate2', 'launch']


def correctness(d):
    """Stop the stage on a correctness-gate failure in observation directory d (#777's l6rules.correctness)."""
    reports = {}
    for report in REPORTS:
        path = d / f'{report}.json'
        try:
            reports[report] = json.loads(path.read_text()) if path.exists() else None
        except ValueError:
            reports[report] = None  # a truncated report is an unusable observation, not a verdict
    failures = l6rules.correctness(reports)
    if failures:
        run.write(run.ART / 'correctness-failure.json', dict(observation=d.name, failures=failures))
        raise SystemExit(f'correctness failure in {d.name}: {failures}; stage stopped')


def registered_hashes():
    # The registered pins live in this record's build.py, loaded by path: #777's prereq.py puts its
    # own directory first on sys.path, where a plain `import build` would find #777's build.py.
    import importlib.util
    spec = importlib.util.spec_from_file_location('build792', HERE / 'build.py')
    build = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build)
    return {'reno-fixture': build.EXPECTED[('reno', 'fixture')], 'cand-fixture': build.EXPECTED[('cand', 'fixture')],
            'cand-trace-fixture': build.REGISTERED[('cand-trace', 'fixture')], 'relay2': build.EXPECTED[('relay', 'relay2')],
            'calibrate2': build.EXPECTED[('relay', 'calibrate2')], 'launch': build.EXPECTED[('relay', 'launch')]}


def check_binaries(bin_dir, expected):
    """Problems with the deployed binaries against the registered hashes (empty when all match)."""
    problems = []
    for binary in BINARIES:
        path = bin_dir / binary
        if not path.exists():
            problems.append(f'{binary} missing')
        elif hashlib.sha256(path.read_bytes()).hexdigest() != expected[binary]:
            problems.append(f'{binary} hash differs from the registered {expected[binary][:16]}')
    return problems


def raw_diagnostics(d):
    """What each endpoint reported, read from its raw report whether or not the observation summarizes (#777's)."""
    out = {}
    measured_start = None
    cfg_path = d / 'competitor-config.json'
    if cfg_path.exists():
        cfg = json.loads(cfg_path.read_text())
        measured_start = cfg['start_unix_ns'] + cfg['warmup_ms'] * 1_000_000
    receipt = json.loads((d / 'receipt.json').read_text()) if (d / 'receipt.json').exists() else None
    wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9 if receipt else None
    for report in REPORTS:
        path = d / f'{report}.json'
        try:
            rec = json.loads(path.read_text()) if path.exists() else None
        except ValueError:
            out[report] = dict(report='unreadable')
            continue
        if rec is None:
            out[report] = dict(report='missing')
            continue
        result = rec.get('result') or {}
        control = result.get('control') or {}
        res = rec.get('resources') or {}
        who = report.split('-')[0]
        entry = dict(error=rec.get('error'), errors=result.get('errors'), control_error=control.get('error'),
                     control_counts={k: control.get(k) for k in ['offered', 'replies', 'skipped', 'unresolved']},
                     cpu_seconds=res.get('cpu_seconds'), peak_rss_bytes=res.get('peak_rss_bytes'),
                     cpu_load=res['cpu_seconds'] / wall / l6rules.ALLOTTED_CORES[who] if wall and res.get('cpu_seconds') is not None else None)
        pause = result.get('bulk_pause_observed_ns')
        if report == 'competitor-send':
            entry['pause_observed_ns'] = pause
            entry['pause_observed_s'] = [(t - measured_start) / 1e9 for t in pause] if pause is not None and measured_start else None
        out[report] = entry
    out['receipt'] = 'present' if receipt else 'missing'
    return out


def trace_state(d):
    """For a trace-arm observation: whether the overlay left a complete trace (dumped, no overflow, one sender)."""
    path = d / 'focal-trace.json'
    if not path.exists():
        return dict(complete=False, reason='no trace file' + (' (dump error recorded)' if (d / 'focal-trace.json.error').exists() else ''))
    data = json.loads(path.read_text())
    ok = data.get('overflow') == 0 and data.get('foreign_events') == 0 and data.get('rows')
    return dict(complete=bool(ok), rows=len(data.get('rows') or []), overflow=data.get('overflow'),
                foreign_events=data.get('foreign_events'), until_unix_ns=data.get('until_unix_ns'))


def observation(d, variant, expected):
    """#777's shared per-observation verdict, plus the trace's completeness for the trace arm."""
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
    trace = None
    if variant == 'cand-trace':
        trace = trace_state(d)
        if not trace['complete']:
            reasons.append(f"incomplete trace: {trace.get('reason') or trace}")
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
    usable, dirty, reasons = l6rules.verdict(reasons, c)
    return dict(usable=usable, contaminated=dirty, reasons=reasons, contamination=c, udp=udp, raw=raw, trace=trace)


def judge(phase, pair, workload):
    """(usable, contaminated, detail) for one pair-workload on disk; detail holds each arm's verdict and diagnostics."""
    expected = registered_hashes()
    detail = {arm: observation(run.OBS / name(phase, pair, workload, arm), run.ARMS[arm][0], expected) for arm in ARMS}
    return all(v['usable'] for v in detail.values()), any(v['contaminated'] for v in detail.values()), detail


def follow():
    """Rerun each unusable or contaminated pair-workload once (#714), at most MAX_RERUNS, recording every decision."""
    decisions, reruns = [], 0
    for pair in range(1, PAIRS + 1):
        for workload in workloads(pair):
            usable, contaminated, detail = judge('main', pair, workload)
            entry = dict(pair=pair, workload=workload, usable=usable, contaminated=contaminated, detail=detail)
            if not usable or contaminated:
                if reruns < MAX_RERUNS:
                    reruns += 1
                    block('rerun', pair, workload)
                    entry['rerun'] = dict(zip(['usable', 'contaminated', 'detail'], judge('rerun', pair, workload)))
                else:
                    entry['rerun'] = 'cap reached'
            decisions.append(entry)
            run.write(run.ART / 'follow-decisions.json', decisions)
    return decisions


def threads_in_sets(processes):
    """Every process has a non-empty, error-free thread snapshot, each thread inside its CPU set (#777's)."""
    want = lambda r: run.AFFINITY[r].replace('2,3', '2-3').replace('6,7', '6-7').replace('4,5', '4-5')
    return set(processes) == set(run.AFFINITY) and all(
        not p.get('error') and p.get('threads') and all(t['cpus'] == want(r) for t in p['threads'].values())
        for r, p in processes.items())


def smoke():
    """One full-length trace observation, excluded from every statistic; usable under the shared verdict,
    with a complete trace and every thread inside its CPU set."""
    out, expected = {}, registered_hashes()
    for workload, arm in [('stream', 'trace')]:
        d = run.OBS / name('smoke', 0, workload, arm)
        error = None
        try:
            run_case('smoke', workload, 0, arm, 9990)
        except Exception as e:  # noqa: BLE001 - the failed observation is judged and kept; the stage still fails
            error = repr(e)
        v = observation(d, run.ARMS[arm][0], expected)
        facts_path = d / 'host-facts.json'
        facts = json.loads(facts_path.read_text()) if facts_path.exists() else {}
        threads = threads_in_sets(facts['mid']['processes']) if facts.get('mid') else False
        out[f'{workload}-{arm}'] = dict(error=error, threads_in_cpu_sets=threads, sockets=(facts.get('mid') or {}).get('sockets'), **v)
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
        import prereq  # #777's calibration, unchanged; it writes under this record's ART
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
        for pair in range(1, PAIRS + 1):
            for workload in workloads(pair):
                block('main', pair, workload)
    elif stage == 'follow':
        follow()
    else:
        raise SystemExit(__doc__)


if __name__ == '__main__':
    main()
