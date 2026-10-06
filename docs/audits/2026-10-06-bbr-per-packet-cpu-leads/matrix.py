#!/usr/bin/env python3
"""Finite #735 order on minimax; observations run sequentially, never concurrently.

Adapted from #714's matrix (04094bd7). run.py is #712's runner, byte-identical;
three things are injected here instead of editing it, all registered in
README.md before any counted observation:

- the artifact directory is this ticket's;
- `perf stat` counts three more events for the receiver measure: `task-clock`
  (a software event) and `msr/aperf/`, `msr/mperf/` (the MSR PMU). None uses a
  general-purpose counter, so #712's sixteen events still count without
  multiplexing;
- when `perf` starts and stops on an endpoint, the per-CPU idle-state counters
  (`cpuidle/state*/usage`, `time`) of that endpoint's cores are read from sysfs
  into `<role>.idle.json`. Read-only; no host setting changes.

Stages: `smoke` (excluded harness check), `profile` (two arms, `perf record`,
both workloads, two blocks), and measurements `m1`..`m3` (one per lead) and
`mc` (cumulative check), each with #714's five arms, six blocks and `perf stat`.
Launch under `taskset -c 1-3` (run.HARNESS_CPUS).

Usage: matrix.py smoke [attempt]
       matrix.py hostfacts LABEL
       matrix.py profile [rerun-block [suffix [workload]]]
       matrix.py m<k>|mc PREV NEW [rerun-block [suffix [workload]]]   e.g. matrix.py m1 r8 l1
"""
import json
import pathlib
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-per-packet-cpu-leads'
run.OBS = run.ART / 'observations'
RECEIVER_EVENTS = ['task-clock', 'msr/aperf/', 'msr/mperf/']
run.PERF_STAT_EVENTS = run.PERF_STAT_EVENTS + RECEIVER_EVENTS

# Registered seeds: profile 10001-10002; measurement mK uses 10000 + 10*K + block; mc uses 10041-10046.
SEED_BASE = {'profile': 10000, 'm1': 10010, 'm2': 10020, 'm3': 10030, 'mc': 10040}
BLOCKS = {'profile': 2, 'm1': 6, 'm2': 6, 'm3': 6, 'mc': 6}
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


run.start_perf, run.stop_perf = start_perf, stop_perf


def host_facts():
    """Read-only record of the settings D4 keeps unchanged."""
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


def arms(prev, new):
    """(variant, controller, tag): frozen Reno, A/A Reno, predecessor, new revision, Reno on the new revision."""
    return [('reno', 'reno', ''), ('reno', 'reno', 'aa'), (prev, 'bbrv3', ''), (new, 'bbrv3', ''), (new, 'reno', '')]


PROFILE_ARMS = [('reno', 'reno', ''), ('r8', 'bbrv3', '')]


def rotated(a, block):
    # #712's rotation: start position advances per block; even blocks run reversed.
    k = (block - 1) % len(a)
    order = a[k:] + a[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def block_rows(stage, a, block, seed, perf, suffix='', only=None):
    rows = []
    for workload in (['stream', 'datagram'] if block % 2 else ['datagram', 'stream']):
        if only and workload != only:
            continue
        for variant, controller, tag in rotated(a, block):
            rows.append(run.run_case(stage + suffix, variant, workload, block, controller, path='S5', seed=seed, tag=tag, perf=perf))
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
        # Harness and instrument check only; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run.run_case('smoke' + attempt, 'r8', 'stream', 1, 'bbrv3', path='S5', seed=9998, perf='stat'),
                run.run_case('smoke' + attempt, 'reno', 'datagram', 1, 'reno', path='S5', seed=9998, perf='stat')]
    else:
        if stage == 'profile':
            a, perf, rest = PROFILE_ARMS, 'record', argv[2:]
        else:
            assert stage in SEED_BASE, stage
            a, perf, rest = arms(argv[2], argv[3]), 'stat', argv[4:]
        if rest:
            # A rerun of one block under the registered rule; the original is retained.
            block = int(rest[0])
            suffix = rest[1] if len(rest) > 1 else 'rerun'
            only = rest[2] if len(rest) > 2 else None
            rows = block_rows(stage, a, block, SEED_BASE[stage] + block, perf, suffix=suffix, only=only)
        else:
            rows = []
            for block in range(1, BLOCKS[stage] + 1):
                rows += block_rows(stage, a, block, SEED_BASE[stage] + block, perf)
    out = run.ART / f'{stage}-summary.json'
    if out.exists():
        out = run.ART / f'{stage}-{len(list(run.ART.glob(stage + "-summary*.json")))}-summary.json'
    run.write(out, [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
