#!/usr/bin/env python3
"""Finite #738 order on minimax; observations run sequentially, never concurrently.

Adapted from #734's matrix.py (04094bd7). Fixture observations go through
#715's unchanged run_case_x (stage_run.py), so the roles, affinity (#712's
core layout), launcher, contamination sampling, receipts and refusal to
replace an attempt are #715's. A rerun of a contaminated or unusable block
(the operator's #714 rule) runs the whole block again under
`observations-rerun`. Launch under `taskset -c 1-3` (run.HARNESS_CPUS).

Before every block the runner waits until the host is quiet (`wait_quiet`):
no fixture runs then, so any busy time on the fixture cores and their SMT
siblings is foreign. It triggers on host state only, never on an outcome; the
registered contamination test still judges every observation.

Usage: matrix.py <stage> [comma-separated blocks [workload]]
       matrix.py rerun <stage> <workload> <block>
"""
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# (variant, controller, tag)
PRE = [('cand', 'bbrv3', ''), ('cand-hand-timeline', 'bbrv3', '')]
SYN = [(f'syn-{c}-timeline', 'bbrv3', '') for c in ['slowworker', 'busyloop', 'delaysignal', 'cwnd', 'mixed']]
ACT = [('cand4q-hand-timeline', 'bbrv3', '')]
# Stage 1 (README, "Stage 1"): frozen Reno (plain), r8 instrumented, a second r8 instrumented (BBR A/A),
# d0fabc4d instrumented, the diagnostic 4Q arm instrumented.
S1 = [('reno', 'reno', ''), ('cand-hand-timeline', 'bbrv3', ''), ('cand-hand-timeline', 'bbrv3', 'aa'),
      ('prev-hand-timeline', 'bbrv3', ''), ('cand4q-hand-timeline', 'bbrv3', '')]


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


def block(stage, path, arms, b, seed=None, only=None, perf=None):
    wait_quiet()
    rows = []
    for workload in (['stream', 'datagram'] if b % 2 else ['datagram', 'stream']):
        if only and workload != only:
            continue
        for variant, controller, tag in rotated(arms, b):
            rows.append(run_case_x(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, perf=perf))
    return rows


# stage -> (phase, path, arms, blocks, seed base or None, workload or None)
STAGES = {
    # Development checks before registration; excluded from every statistic, disclosed.
    'devsmoke': ('devsmoke', 'loopback', PRE, 1, None, None),
    'devsyn': ('devsyn', 'loopback', SYN, 1, None, None),
    # Stage 0 (README, "Stage 0").
    'synthetic': ('synthetic', 'loopback', SYN, 1, None, None),
    'preflight': ('preflight', 'loopback', PRE, 5, None, None),
    'activation': ('activation', 'loopback', ACT, 1, None, None),
    # Stage 1 (README, "Stage 1").
    's1': ('s1', 'loopback', S1, 4, None, None),
}


def main(argv):
    stage = argv[1]
    run.ensure_credentials()
    dev = {'devsmoke': PRE, 'devsyn': SYN, 'devact': ACT,
           'devparts': [('cand', 'bbrv3', '')] + [(f'dev-p{k}-timeline', 'bbrv3', '') for k in (1, 3, 7, 9)] + [('cand-hand-timeline', 'bbrv3', '')],
           'devmask': [('cand', 'bbrv3', ''), ('dev-m0-timeline', 'bbrv3', ''), ('cand-hand-timeline', 'bbrv3', '')],
           'devsynb': SYN + [('syn-devmixedb-timeline', 'bbrv3', '')],
           'devsynm': [('syn-mixed-timeline', 'bbrv3', ''), ('syn-devmixedb-timeline', 'bbrv3', ''), ('syn-delaysignal-timeline', 'bbrv3', '')],
           'devsynx': [('syn-mixed-timeline', 'bbrv3', ''), ('syn-devmixedb-timeline', 'bbrv3', '')]}
    if any(stage.startswith(k) for k in dev) and stage not in STAGES:
        # Development checks before registration, any attempt suffix; excluded from every statistic, disclosed.
        arms = next(v for k, v in sorted(dev.items(), key=lambda kv: -len(kv[0])) if stage.startswith(k))
        reps = int(argv[2]) if len(argv) > 2 else 1
        rows = []
        for r in range(1, reps + 1):
            rows += block(stage, 'loopback', arms, r)
    elif stage == 'rerun':
        name, workload, b = argv[2], argv[3], int(argv[4])
        run.OBS = run.ART / 'observations-rerun'
        phase, path, arms, n, base, _ = STAGES[name]
        rows = block(phase, path, arms, b, seed=base + b if base else None, only=workload)
        stage = f'rerun-{name}-{workload}-p{b}'
    else:
        phase, path, arms, n, base, only = STAGES[stage]
        blocks = [int(x) for x in argv[2].split(',')] if len(argv) > 2 else range(1, n + 1)
        if len(argv) > 3:
            only = argv[3]  # a workload whose Stage 0 failed is not run in Stage 1
        rows = []
        for b in blocks:
            rows += block(phase, path, arms, b, seed=base + b if base else None, only=only)
    run.write(run.ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
