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

Usage: matrix.py prereq | smoke | main | follow
       matrix.py dev WORKLOAD ARM [LABEL]
       matrix.py hostfacts LABEL
"""
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


def correctness(d):
    """Stop the stage on a correctness-gate failure in observation directory d (l6rules.correctness)."""
    receivers = {}
    for who in ['focal', 'competitor']:
        path = d / f'{who}-receive.json'
        if path.exists():
            receivers[who] = json.loads(path.read_text())['result']['receiver']
    failures = l6rules.correctness(receivers)
    if failures:
        run.write(run.ART / 'correctness-failure.json', dict(observation=d.name, failures=failures))
        raise SystemExit(f'correctness failure in {d.name}: {failures}; stage stopped')


def judge(phase, pair, workload):
    """(usable, contaminated, reasons) for one triple on disk."""
    usable, contaminated, reasons = True, False, {}
    for arm in ARMS:
        d = run.OBS / f'{phase}-L6-{workload}-p{pair}-{arm}'
        correctness(d)
        receipt_path = d / 'receipt.json'
        if not receipt_path.exists():
            usable, reasons[arm] = False, ['no receipt']
            continue
        receipt = json.loads(receipt_path.read_text())
        try:
            summary = run.summarize(d)
        except Exception as e:
            summary, reasons[arm] = None, [f'summary: {e!r}']
        facts = json.loads((d / 'host-facts.json').read_text())
        wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9
        r = l6rules.health(summary, receipt, wall) + reasons.get(arm, [])
        c = l6rules.contamination(json.loads((d / 'host-cpu.json').read_text()), facts['measure_window_unix_ns'],
                                  run.FIXTURE_CPUS, run.SIBLING_CPUS)
        reasons[arm] = r + (['contaminated'] if c['contaminated'] else [])
        usable = usable and not r
        contaminated = contaminated or bool(c['contaminated'])
    return usable, contaminated, reasons


def follow():
    """Rerun each unusable or contaminated triple once (#714), at most MAX_RERUN_TRIPLES, recording every decision."""
    decisions, reruns = [], 0
    for pair in range(1, l6rules.PAIRS + 1):
        for workload in workloads(pair):
            usable, contaminated, reasons = judge('main', pair, workload)
            entry = dict(pair=pair, workload=workload, usable=usable, contaminated=contaminated, reasons=reasons)
            if not usable or contaminated:
                if reruns < MAX_RERUN_TRIPLES:
                    reruns += 1
                    triple('rerun', pair, workload)
                    entry['rerun'] = dict(zip(['usable', 'contaminated', 'reasons'], judge('rerun', pair, workload)))
                else:
                    entry['rerun'] = 'cap reached'
            decisions.append(entry)
            run.write(run.ART / 'follow-decisions.json', decisions)
    return decisions


def smoke():
    """Two full-length observations on the candidate and frozen Reno, excluded from every statistic."""
    out = {}
    for workload, arm in [('stream', 'cand'), ('datagram', 'reno')]:
        d = run.OBS / f'smoke-L6-{workload}-p0-{arm}'
        try:
            row = run.run_case('smoke', workload, 0, arm, 9990)
        finally:
            correctness(d)
        receipt = json.loads((d / 'receipt.json').read_text())
        facts = json.loads((d / 'host-facts.json').read_text())
        wall = (receipt['end_unix_ns'] - receipt['start_unix_ns']) / 1e9
        threads_ok = all(t['cpus'] == run.AFFINITY[r].replace('2,3', '2-3').replace('6,7', '6-7').replace('4,5', '4-5')
                         for r, p in facts['mid']['processes'].items() for t in p['threads'].values())
        udp = {k: facts['udp_after'][k] - facts['udp_before'][k] for k in ['InErrors', 'RcvbufErrors', 'SndbufErrors']}
        out[f'{workload}-{arm}'] = dict(health=l6rules.health(row, receipt, wall), threads_in_cpu_sets=threads_ok, udp_errors=udp,
                                        sockets=facts['mid']['sockets'],
                                        cpu_load={f'{who}-{role}': row[who][role]['total_cpu_seconds'] / wall / l6rules.ALLOTTED_CORES[who]
                                                  for who in ['focal', 'competitor'] for role in ['send', 'receive']},
                                        relay=dict(max_arrival_lag_ms=row['relay']['max_arrival_lag_ms'],
                                                   forward_max_late_ms=row['relay']['forward']['max_late_ms'],
                                                   reverse_max_late_ms=row['relay']['reverse']['max_late_ms']),
                                        pause_s=row['competitor']['bulk_pause_observed_s'])
    passed = all(not v['health'] and v['threads_in_cpu_sets'] and not any(v['udp_errors'].values()) for v in out.values())
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
    elif stage == 'prereq':
        import prereq
        prereq.main()
    elif stage == 'smoke':
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
