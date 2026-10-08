#!/usr/bin/env python3
"""Finite #740 order on minimax; observations run sequentially, never concurrently.

Adapted from #739's matrix (138223ac). Recorded changes: the stages are #740's
(D6): the excluded `memsmoke` instrument and activation check, and Stage 1's
`mem-s5`, `mem-s6` and, by operator decision, `mem-loopback`, through
mem_run.run_case_m; and, by operator decision, `latcand-stream` (#715's
`preslat` stage with the candidate in place of Reno on candidate, plain builds,
through stage_run.run_case_x). The readiness, timeline and preservation stages
are removed with their arms. Stage 1 seeds are the readiness seeds (9001-9004,
9101-9104), so each block's relay impairments match #739's readiness block of
the same number. The ring arm runs only when the registered preflight
(`memstages.py preflight`, ring-preflight.json) says `run`. The prerequisite
smokes (`smoke`, `perfsmoke`, `ecnsmoke`), host facts, the quiet-host gate
(verbatim from #738), the rotation and the same-seed rerun rule are #739's,
unchanged. Launch under `taskset -c 1-3` (run.HARNESS_CPUS).

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
from mem_run import run_case_m  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# (variant, controller, tag): frozen Reno; the candidate; the BBR A/A candidate; the ring treatment;
# the receiver-controller arm (candidate build at both endpoints, receiver controller Reno).
RENO, CAND, AA = ('reno-mem', 'reno', ''), ('cand-mem', 'bbrv3', ''), ('cand-mem', 'bbrv3', 'aa')
RING, RXRENO = ('cand-mem-ring', 'bbrv3', ''), ('cand-mem', 'bbrv3', 'rxreno')
LOOPBACK_ARMS = [RENO, CAND, AA, RXRENO]


def wan_arms():
    """D6's five arms, or four when the registered ring preflight did not say `run`."""
    import json
    pre = run.ART / 'ring-preflight.json'
    assert pre.exists(), 'run memstages.py preflight first'
    ring = json.loads(pre.read_text())['decision'] == 'run'
    return [RENO, CAND, AA] + ([RING] if ring else []) + [RXRENO]


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


# stage -> (phase, path, arms (or a function of none), blocks, seed base or None, runner, options, workloads)
STAGES = {
    'mem-s5': ('mem', 'S5', wan_arms, 4, 9000, run_case_m, {}, None),
    'mem-s6': ('mem', 'S6', wan_arms, 4, 9100, run_case_m, {}, None),
    'mem-loopback': ('mem', 'loopback', LOOPBACK_ARMS, 4, None, run_case_m, {}, None),
    # Operator decision (relayed 2026-10-08): #715's preslat design for the candidate's loopback STREAM control p95.
    'latcand-stream': ('latcand', 'loopback-long', [('reno', 'reno', ''), ('cand', 'bbrv3', '')], 6, None, run_case_x, {}, 'stream'),
}


def run_stage(name, blocks=None, phase_suffix=''):
    phase, path, arms, n, base, runner, opts, workload = STAGES[name]
    arms = arms() if callable(arms) else arms
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
    elif stage == 'memsmoke':
        # Stage 0 instrument and activation check: every -mem arm once on S5 STREAM, and the candidate on S6
        # DATAGRAM for the phase log; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run_case_m('memsmoke' + attempt, v, 'stream', 1, c, path='S5', seed=9993, tag=t) for v, c, t in [RENO, CAND, RING, RXRENO]]
        rows.append(run_case_m('memsmoke' + attempt, 'cand-mem', 'datagram', 1, 'bbrv3', path='S6', seed=9992))
    elif stage == 'rerun':
        # One same-seed rerun of a contaminated or unusable block, under a separate root.
        name, workload, b = argv[2], argv[3], int(argv[4])
        run.OBS = run.ART / 'observations-rerun'
        phase, path, arms, n, base, runner, opts, _ = STAGES[name]
        arms = arms() if callable(arms) else arms
        rows = block(phase, path, arms, b, seed=base + b if base else None, runner=runner, only=workload, **opts)
        stage = f'rerun-{name}-{workload}-p{b}'
    else:
        rows = run_stage(stage)
    run.write(run.ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
