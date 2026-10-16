#!/usr/bin/env python3
"""Finite #739 order on minimax; observations run sequentially, never concurrently.

Adapted from #736's matrix (3bf2245e). Readiness stages (`loopback`, `s5`,
`s6`) are #712's, #715's and #736's: the same arms, paths, seeds and rotation,
through run.py's unchanged run_case, with `cand` now the surviving revision r9
(81e9dc8c). The other stages are the ones D5 of the #737 decision lists:
`s5timeline` and the conditional `presrss` and `preslat`, unchanged from #736.
Recorded changes from #736: the memory stages (`timeline-*`, `heapsites-*`)
belong to #740 under D6 and are removed with their `DIAG` arms, and the
conditional `recvharness` stage, which D5 does not list, is removed with
#735's receiver instruments and the `rsmoke` check that served only it.
Before every counted block the runner waits for a quiet host (`wait_quiet`,
verbatim from #738's matrix, 070e1597): it triggers on host state only, never
on an outcome, and the registered contamination test still judges every
observation. A rerun of a
contaminated or unusable block (the operator's #714 rule) runs the whole block
again with the same seed under a separate observation root,
`observations-rerun`, so no original is replaced. Launch under
`taskset -c 1-3` (run.HARNESS_CPUS).

Usage: matrix.py <stage> [attempt]
       matrix.py hostfacts LABEL
       matrix.py rerun <stage> <workload> <block>
       matrix.py escalate <stage> <workload> <block>
"""
import pathlib
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# (variant, controller, tag); tag 'aa' is the second frozen-Reno run of a block.
FOUR = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', ''), ('cand', 'reno', '')]
S6_ARMS = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', '')]
PRESRSS = [('reno-diag', 'reno', ''), ('cand-diag', 'reno', '')]
PRESLAT = [('reno', 'reno', ''), ('cand', 'reno', '')]
TIMELINE = [('cand-timeline', 'bbrv3', '')]

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


def block(stage, path, arms, b, seed=None, runner=None, only=None, **kw):
    wait_quiet()
    rows = []
    for workload in (['stream', 'datagram'] if b % 2 else ['datagram', 'stream']):
        if only and workload != only:
            continue
        for variant, controller, tag in rotated(arms, b):
            if runner is None:
                rows.append(run.run_case(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, **kw))
            else:
                rows.append(runner(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, **kw))
    return rows


# stage -> (phase, path, arms, blocks, seed base or None, runner, options, workloads)
STAGES = {
    # Readiness, #712's registration unchanged.
    'loopback': ('main', 'loopback', FOUR, 5, None, None, {}, None),
    's5': ('main', 'S5', FOUR, 5, 9000, None, {}, None),
    's6': ('main', 'S6', S6_ARMS, 5, 9100, None, {}, None),
    # Attribution (README.md, "Registration: attribution stages").
    's5timeline': ('s5timeline', 'S5', TIMELINE, 4, 9600, run_case_x, {}, None),
}
# Conditional preservation stages, one per raised cell: presrss-<path>-<workload>, preslat-<workload>.
for path, base in [('loopback', None), ('S5', 9800), ('S6', 9900)]:
    for workload in ['stream', 'datagram']:
        STAGES[f'presrss-{path}-{workload}'] = ('presrss', path, PRESRSS, 6, base, None, dict(heap_series=True), workload)
for workload in ['stream', 'datagram']:
    STAGES[f'preslat-{workload}'] = ('preslat', 'loopback-long', PRESLAT, 6, None, run_case_x, {}, workload)


def run_stage(name, blocks=None, phase_suffix=''):
    phase, path, arms, n, base, runner, opts, workload = STAGES[name]
    rows = []
    for b in blocks or range(1, n + 1):
        rows += block(phase + phase_suffix, path, arms, b, seed=base + b if base else None, runner=runner, only=workload, **opts)
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
    if stage == 'smoke':
        # Harness check only; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run.run_case('smoke' + attempt, 'cand', 'stream', 1, 'bbrv3'),
                run.run_case('smoke' + attempt, 'cand', 'datagram', 1, 'bbrv3', path='S6', seed=9999)]
    elif stage == 'perfsmoke':
        # Instrument and native I/O check only; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run.run_case('perfsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='stat'),
                run.run_case('perfsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='record', tag='record')]
    elif stage == 'ecnsmoke':
        # Endpoint ECN engagement on loopback and S6 (BBR ECN tracker frames in the sender's samples); excluded.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run.run_case('ecnsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', perf='record'),
                run.run_case('ecnsmoke' + attempt, 'cand', 'datagram', 1, 'bbrv3', path='S6', seed=9996, perf='record')]
    elif stage == 'xsmoke':
        # Attribution-runner instrument check (timeline overlay); excluded.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run_case_x('xsmoke' + attempt, 'cand-timeline', 'datagram', 1, 'bbrv3', path='S5', seed=9995)]
    elif stage == 'escalate':
        # A registered escalation: one more block of one workload (s5timeline blocks 5-6).
        name, workload, b = argv[2], argv[3], int(argv[4])
        assert (name, b) in {('s5timeline', 5), ('s5timeline', 6)}, (name, b)
        phase, path, arms, n, base, runner, opts, _ = STAGES[name]
        rows = block(phase, path, arms, b, seed=base + b if base else None, runner=runner, only=workload, **opts)
        stage = f'escalate-{name}-{workload}-p{b}'
    elif stage == 'rerun':
        # One same-seed rerun of a contaminated or unusable block, under a separate root.
        name, workload, b = argv[2], argv[3], int(argv[4])
        run.OBS = run.ART / 'observations-rerun'
        phase, path, arms, n, base, runner, opts, _ = STAGES[name]
        rows = block(phase, path, arms, b, seed=base + b if base else None, runner=runner, only=workload, **opts)
        stage = f'rerun-{name}-{workload}-p{b}'
    else:
        rows = run_stage(stage)
    run.write(run.ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
