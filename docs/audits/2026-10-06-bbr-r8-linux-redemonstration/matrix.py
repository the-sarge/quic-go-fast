#!/usr/bin/env python3
"""Finite #736 order on minimax; observations run sequentially, never concurrently.

Adapted from #715's matrix (36f7ce01). Readiness stages (`loopback`, `s5`,
`s6`) are #712's and #715's: the same arms, paths, seeds and rotation, through
run.py's unchanged run_case, with `cand` now the surviving revision r8
(95f5b6b7). The attribution stages are registered in README.md before any of
their observations ran. Recorded changes from #715: no `lbneck` stage and no
`prev` arm; the heap-site stages run four blocks (D5); and a conditional
`recvharness` stage measures the S5 receiver with and without #735's `perf
stat` instruments (task-clock, APERF, MPERF and per-core idle snapshots, injected
here exactly as #735's matrix did, and only for that stage). A rerun of a
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
DIAG = [('reno-diag', 'reno', ''), ('cand-diag-counted', 'bbrv3', '')]
PRESRSS = [('reno-diag', 'reno', ''), ('cand-diag', 'reno', '')]
PRESLAT = [('reno', 'reno', ''), ('cand', 'reno', '')]
TIMELINE = [('cand-timeline', 'bbrv3', '')]
# recvharness: each arm plain (no perf, as readiness) and with #735's perf stat instruments.
RECVHARNESS = [('reno', 'reno', ''), ('cand', 'bbrv3', ''), ('reno', 'reno', 'perf'), ('cand', 'bbrv3', 'perf')]

# ---- #735's receiver instruments, injected only for recvharness (verbatim from #735's matrix.py, f3840c83) ----
RECEIVER_EVENTS = ['task-clock', 'msr/aperf/', 'msr/mperf/']
ROLE_CPUS = {'send': range(8, 12), 'receive': range(12, 16)}
_idle = {}


def idle_snapshot():
    out = {}
    for cpu in range(8, 16):
        base = pathlib.Path(f'/sys/devices/system/cpu/cpu{cpu}/cpuidle')
        out[cpu] = {s.name: dict(name=(s / 'name').read_text().strip(), usage=int((s / 'usage').read_text()),
                                 time_us=int((s / 'time').read_text())) for s in sorted(base.glob('state*'))}
    return dict(unix_ns=time.time_ns(), cpus=out)


_start_perf, _stop_perf = run.start_perf, run.stop_perf


def start_perf(kind, pid, out):
    role = out.name.split('.')[0]
    snap = idle_snapshot()
    res = _start_perf(kind, pid, out)
    _idle[id(res[1])] = (out.with_name(f'{role}.idle.json'), role, snap)
    return res


def stop_perf(proc):
    res = _stop_perf(proc)
    path, role, before = _idle.pop(id(proc))
    after = idle_snapshot()
    keep = lambda s: {c: v for c, v in s['cpus'].items() if c in ROLE_CPUS[role]}
    run.write(path, dict(role=role, cpus=list(ROLE_CPUS[role]), start=dict(before, cpus=keep(before)), stop=dict(after, cpus=keep(after))))
    return res


def receiver_instruments():
    run.PERF_STAT_EVENTS = run.PERF_STAT_EVENTS + RECEIVER_EVENTS
    run.start_perf, run.stop_perf = start_perf, stop_perf


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


def recv_case(stage, variant, workload, b, controller, path=None, seed=None, tag='', **kw):
    """One recvharness observation: the `perf` tag attaches #735's perf stat to both endpoints for the measured window."""
    return run.run_case(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, perf='stat' if tag == 'perf' else None)


def rotated(arms, block):
    k = (block - 1) % len(arms)
    order = arms[k:] + arms[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def block(stage, path, arms, b, seed=None, runner=None, only=None, **kw):
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
    'timeline-S5': ('timeline', 'S5', DIAG, 2, 9300, None, {}, None),
    'timeline-S6': ('timeline', 'S6', DIAG, 2, 9400, None, {}, None),
    'heapsites-S5': ('heapsites', 'S5', DIAG, 4, 9310, None, dict(heap_series=True), None),
    'heapsites-S6': ('heapsites', 'S6', DIAG, 4, 9410, None, dict(heap_series=True), None),
}
# Conditional receiver-harness stage, one per raised S5 receiver CPU cell.
for workload in ['stream', 'datagram']:
    STAGES[f'recvharness-{workload}'] = ('recvharness', 'S5', RECVHARNESS, 4, 10600, recv_case, {}, workload)
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
    if stage.startswith('recvharness') or stage == 'rsmoke' or (stage == 'rerun' and argv[2].startswith('recvharness')):
        receiver_instruments()
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
    elif stage == 'rsmoke':
        # Receiver-instrument check for the conditional recvharness stage (#735's events and idle snapshots); excluded.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [recv_case('rsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', path='S5', seed=9994, tag='perf'),
                recv_case('rsmoke' + attempt, 'reno', 'datagram', 1, 'reno', path='S5', seed=9994, tag='perf')]
    elif stage == 'escalate':
        # A registered escalation: one more block of one workload (lbneck block 5; s5timeline blocks 5-6).
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
