#!/usr/bin/env python3
"""Finite #744 order on minimax; observations run sequentially, never concurrently.

Adapted from #740's matrix (ddbac0a3). Recorded changes: the stages are #744's
(D3): the excluded Stage 0 synthetic cases (`syn`, on `loopback-syn`, a
loopback path with a 60 s measured window added here), the counted Stage 1
(`s1`: six arms, `loopback-long` STREAM, six blocks), and Stage 2's arms, which
run only after Stage 2's mechanism section is registered (`s2-loopback`,
`s2-long`, `s2-s5`). Every observation goes through #715's unchanged
`stage_run.run_case_x`. Host facts, the quiet-host gate (verbatim from #738),
the rotation and the same-seed rerun rule are #740's, unchanged. A failed
observation is retained, recorded in failures.json and leaves its block
unusable for the rerun rule; the stage continues. A stage can be resumed by
running it again: completed observations are reused, never rerun. Launch under
`taskset -c 1-3` (run.HARNESS_CPUS).

Usage: matrix.py <stage> [attempt]
       matrix.py hostfacts LABEL
       matrix.py rerun <stage> <workload> <block>
"""
import pathlib
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
import stage_run  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# The synthetic cases' path: loopback with a 60 s measured window (about 60 control replies per run).
stage_run.PATHS['loopback-syn'] = dict(warmup_ms=5000, measure_ms=60000)

# (variant, controller, tag)
RENO, CAND, AA, CAND8 = ('reno', 'reno', ''), ('cand', 'bbrv3', ''), ('cand', 'bbrv3', 'aa'), ('cand8', 'bbrv3', '')
CTL, CTL8 = ('cand-ctl', 'bbrv3', ''), ('cand8-ctl', 'bbrv3', '')
S1_ARMS = [RENO, CAND, AA, CAND8, CTL, CTL8]
SYN_ARMS = [CTL, ('cand-ctl', 'bbrv3', 'aa')] + [(f'syn-{c}-ctl', 'bbrv3', '') for c in ('sq', 'rq', 'sw', 'rl', 'rw', 'un')]
# Stage 2 (registered after Stage 1; run only when its section exists): NEW is the treatment, plain.
NEW, RENONEW, RENOAA = ('new', 'bbrv3', ''), ('new', 'reno', ''), ('reno', 'reno', 'aa')
S2_LOOPBACK = [RENO, RENOAA, CAND, AA, NEW, RENONEW]
S2_LONG = [RENO, CAND, AA, NEW]
S2_S5 = [CAND, AA, NEW]


def host_facts():
    """Read-only record of host settings (verbatim from #735's matrix.py)."""
    cpu = lambda n, f: (pathlib.Path(f'/sys/devices/system/cpu/cpu{n}/{f}').read_text().strip()
                        if pathlib.Path(f'/sys/devices/system/cpu/cpu{n}/{f}').exists() else None)
    out = {}
    for n in [1, 4, 5] + list(range(8, 16)):
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


def rotated(arms, block):
    k = (block - 1) % len(arms)
    order = arms[k:] + arms[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def block(stage, path, arms, b, seed=None, workloads=('stream',), perf=None):
    wait_quiet()
    rows = []
    for workload in (workloads if b % 2 else tuple(reversed(workloads))):
        for variant, controller, tag in rotated(arms, b):
            try:
                rows.append(run_case_x(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, perf=perf))
            except Exception as e:  # noqa: BLE001
                # A failed observation is retained as it is (never replaced); it makes its block unusable, and
                # follow.py's same-seed rerun rule then reruns the block. The failure is recorded, not suppressed.
                failure(dict(stage=stage, path=path, workload=workload, block=b, variant=variant, controller=controller, tag=tag,
                             error=repr(e), unix_ns=time.time_ns()))
    return rows


def failure(entry):
    import json
    log = run.ART / 'failures.json'
    out = json.loads(log.read_text()) if log.exists() else []
    out.append(entry)
    log.write_text(json.dumps(out, indent=1) + '\n')
    print('FAILED', entry, flush=True)


# stage -> (phase, path, arms, blocks, seed base or None, workloads, perf)
STAGES = {
    'syn': ('syn', 'loopback-syn', SYN_ARMS, 3, None, ('stream',), None),
    's1': ('s1', 'loopback-long', S1_ARMS, 6, None, ('stream',), None),
    # Stage 2 (D3): runs only after its mechanism section is registered and the `new` builds exist.
    's2-loopback': ('s2', 'loopback', S2_LOOPBACK, 6, None, ('stream', 'datagram'), 'stat'),
    's2-long': ('s2', 'loopback-long', S2_LONG, 6, None, ('stream',), None),
    's2-s5': ('s2', 'S5', S2_S5, 4, 9880, ('stream', 'datagram'), None),
}


def run_stage(name, blocks=None):
    phase, path, arms, n, base, workloads, perf = STAGES[name]
    if name.startswith('s2'):
        assert (run.ART / 'bin/new-fixture').exists(), 'Stage 2 needs its registered `new` build'
    rows = []
    for b in blocks or range(1, n + 1):
        rows += block(phase, path, arms, b, seed=base + b if base else None, workloads=workloads, perf=perf)
    return rows


def main(argv):
    stage = argv[1]
    run.ART.mkdir(parents=True, exist_ok=True)
    if stage == 'hostfacts':
        out = run.ART / f'hostfacts-{argv[2]}.json'
        assert not out.exists(), out
        run.write(out, host_facts())
        return
    run.ensure_credentials()
    if stage == 'dev':
        # Development only, excluded from every statistic: phase dev<attempt>, the given arms on a path.
        # matrix.py dev <attempt> <path> <blocks> <variant:controller:tag> ...
        attempt, path, nblocks = argv[2], argv[3], int(argv[4])
        arms = [tuple((a.split(':') + ['', ''])[:3]) for a in argv[5:]]
        rows = []
        for b in range(1, nblocks + 1):
            rows += block('dev' + attempt, path, arms, b)
    elif stage == 'rerun':
        # One same-seed rerun of a contaminated or unusable block, under a separate root.
        name, workload, b = argv[2], argv[3], int(argv[4])
        run.OBS = run.ART / 'observations-rerun'
        phase, path, arms, n, base, _, perf = STAGES[name]
        rows = block(phase, path, arms, b, seed=base + b if base else None, workloads=(workload,), perf=perf)
        stage = f'rerun-{name}-{workload}-p{b}'
    else:
        rows = run_stage(stage)
    out = run.ART / f'{stage}-summary.json'
    if out.exists():  # a resumed stage keeps the first summary and writes the resumed one beside it
        out = run.ART / f'{stage}-summary-resumed-{time.time_ns()}.json'
    run.write(out, [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
