#!/usr/bin/env python3
"""Finite #777 order on minimax; observations run sequentially, never concurrently.

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

ARMS = ['reno', 'cand', 'aa']
SEED_BASE = 7700          # L6 has no random loss, so the relay seed changes nothing; recorded per pair
MAX_RERUN_TRIPLES = 4


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
    """The registered seeded random arm order of one pair-workload triple."""
    return random.Random(f'777-{pair}-{workload}').sample(ARMS, len(ARMS))


def workloads(pair):
    return ['stream', 'datagram'] if pair % 2 else ['datagram', 'stream']


def triple(phase, pair, workload):
    wait_quiet()
    rows = {}
    for arm in order(pair, workload):
        try:
            rows[arm] = run.run_case(phase, workload, pair, arm, SEED_BASE + pair)
        except Exception as e:  # a failed observation stays on disk; follow decides
            print(f'{phase} p{pair} {workload} {arm} failed: {e!r}', flush=True)
            rows[arm] = None
        correctness(run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}')
    return rows


REPORTS = ['focal-send', 'focal-receive', 'competitor-send', 'competitor-receive']
BINARIES = ['reno-fixture', 'cand-fixture', 'relay2', 'calibrate2', 'launch']


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
    return {'reno-fixture': build.EXPECTED[('reno', 'fixture')], 'cand-fixture': build.EXPECTED[('cand', 'fixture')],
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


def judge(phase, pair, workload):
    """(usable, contaminated, detail) for one triple on disk; detail holds each arm's reasons and context."""
    usable, contaminated, detail = True, False, {}
    expected = registered_hashes()
    for arm in ARMS:
        d = run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}'
        correctness(d)
        receipt_path = d / 'receipt.json'
        if not receipt_path.exists():
            usable, detail[arm] = False, dict(reasons=['no receipt'])
            continue
        receipt = json.loads(receipt_path.read_text())
        reasons = []
        try:
            summary = run.summarize(d)
        except Exception as e:  # noqa: BLE001 - recorded as the reason
            summary, reasons = None, [f'summary: {e!r}']
        variant = run.ARMS[arm][0]
        for who, h in (receipt.get('binary_sha256') or {}).items():
            want = expected[f"{variant if who == 'focal' else 'reno'}-fixture"]
            if h != want:
                reasons.append(f'{who} fixture hash differs from the registered {want[:16]}')
        if receipt.get('relay_sha256') != expected['relay2']:
            reasons.append('relay hash differs from the registered one')
        facts_path, cpu_path = d / 'host-facts.json', d / 'host-cpu.json'
        wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9
        reasons = l6rules.health(summary, receipt, wall) + reasons
        if facts_path.exists() and cpu_path.exists():
            c = l6rules.contamination(json.loads(cpu_path.read_text()), json.loads(facts_path.read_text())['measure_window_unix_ns'],
                                      run.FIXTURE_CPUS, run.SIBLING_CPUS)
        else:
            c = dict(contaminated=None, reason='host samples missing')
        ok, dirty, reasons = l6rules.verdict(reasons, c)
        detail[arm] = dict(reasons=reasons, contamination=c,
                           competitor_pause_s=summary['competitor']['bulk_pause_observed_s'] if summary else None,
                           cpu_load={f'{who}-{role}': summary[who][role]['total_cpu_seconds'] / wall / l6rules.ALLOTTED_CORES[who]
                                     for who in ['focal', 'competitor'] for role in ['send', 'receive']} if summary else None)
        usable = usable and ok
        contaminated = contaminated or dirty
    return usable, contaminated, detail


def follow():
    """Rerun each unusable or contaminated triple once (#714), at most MAX_RERUN_TRIPLES, recording every decision."""
    decisions, reruns = [], 0
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in workloads(pair):
            usable, contaminated, detail = judge('main', pair, workload)
            entry = dict(pair=pair, workload=workload, usable=usable, contaminated=contaminated, detail=detail)
            if not usable or contaminated:
                if reruns < MAX_RERUN_TRIPLES:
                    reruns += 1
                    triple('rerun', pair, workload)
                    entry['rerun'] = dict(zip(['usable', 'contaminated', 'detail'], judge('rerun', pair, workload)))
                else:
                    entry['rerun'] = 'cap reached'
            decisions.append(entry)
            run.write(run.ART / 'follow-decisions.json', decisions)
    return decisions


def threads_in_sets(processes):
    """Every process has a non-empty, error-free thread snapshot, each thread inside its CPU set."""
    want = lambda r: run.AFFINITY[r].replace('2,3', '2-3').replace('6,7', '6-7').replace('4,5', '4-5')
    return set(processes) == set(run.AFFINITY) and all(
        not p.get('error') and p.get('threads') and all(t['cpus'] == want(r) for t in p['threads'].values())
        for r, p in processes.items())


def smoke():
    """Two full-length observations on the candidate and frozen Reno, excluded from every statistic."""
    out = {}
    for workload, arm in [('stream', 'cand'), ('datagram', 'reno')]:
        d = run.OBS / f'smoke-L6-{workload}-p0-{arm}'
        key = f'{workload}-{arm}'
        try:
            row = run.run_case('smoke', workload, 0, arm, 9990)
        except Exception as e:  # noqa: BLE001 - the failed report is kept; the stage still fails
            out[key] = dict(error=repr(e))
            continue
        finally:
            correctness(d)
        receipt = json.loads((d / 'receipt.json').read_text())
        facts = json.loads((d / 'host-facts.json').read_text())
        wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9
        udp = {k: facts['udp_after'][k] - facts['udp_before'][k] for k in ['InErrors', 'RcvbufErrors', 'SndbufErrors']}
        out[key] = dict(health=l6rules.health(row, receipt, wall), threads_in_cpu_sets=threads_in_sets(facts['mid']['processes']),
                        udp_errors=udp, sockets=facts['mid']['sockets'],
                        cpu_load={f'{who}-{role}': row[who][role]['total_cpu_seconds'] / wall / l6rules.ALLOTTED_CORES[who]
                                  for who in ['focal', 'competitor'] for role in ['send', 'receive']},
                        relay=dict(max_arrival_lag_ms=row['relay']['max_arrival_lag_ms'],
                                   forward_max_late_ms=row['relay']['forward']['max_late_ms'],
                                   reverse_max_late_ms=row['relay']['reverse']['max_late_ms']),
                        pause_s=row['competitor']['bulk_pause_observed_s'])
    passed = all('error' not in v and not v['health'] and v['threads_in_cpu_sets'] and not any(v['udp_errors'].values())
                 for v in out.values())
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
        prereq.main()
    elif stage == 'smoke':
        wait_quiet()
        smoke()
    elif stage == 'dev':
        workload, arm = sys.argv[2], sys.argv[3]
        run.run_case('dev', workload, 0, arm, 9991, tag=sys.argv[4] if len(sys.argv) > 4 else '')
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
