#!/usr/bin/env python3
"""Finite #715 order on minimax; observations run sequentially, never concurrently.

Adapted from the #712 matrix (4323dae8). Readiness stages (`loopback`, `s5`,
`s6`) are #712's: the same arms, paths, seeds and rotation, through run.py's
unchanged run_case, with `cand` now the surviving revision d0fabc4d. The
attribution stages are registered in README.md before any of their
observations ran. A rerun of a contaminated or unusable block (the operator's
#714 rule, prospective here) runs the whole block again with the same seed
under a separate observation root, `observations-rerun`, so no original is
replaced. Launch under `taskset -c 1-3` (run.HARNESS_CPUS).

Usage: matrix.py <stage> [attempt]
       matrix.py rerun <stage> <workload> <block>
       matrix.py escalate <stage> <workload> <block>
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# (variant, controller, tag); tag 'aa' is the second frozen-Reno run of a block.
FOUR = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', ''), ('cand', 'reno', '')]
S6_ARMS = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand', 'bbrv3', '')]
DIAG = [('reno-diag', 'reno', ''), ('cand-diag-counted', 'bbrv3', '')]
LBNECK = [('reno', 'reno', ''), ('prev', 'bbrv3', ''), ('cand', 'bbrv3', '')]
PRESRSS = [('reno-diag', 'reno', ''), ('cand-diag', 'reno', '')]
PRESLAT = [('reno', 'reno', ''), ('cand', 'reno', '')]
TIMELINE = [('cand-timeline', 'bbrv3', '')]


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
    'lbneck': ('lbneck', 'loopback', LBNECK, 4, None, run_case_x, dict(perf='both', threads=True), None),
    's5timeline': ('s5timeline', 'S5', TIMELINE, 4, 9600, run_case_x, {}, None),
    'timeline-S5': ('timeline', 'S5', DIAG, 2, 9300, None, {}, None),
    'timeline-S6': ('timeline', 'S6', DIAG, 2, 9400, None, {}, None),
    'heapsites-S5': ('heapsites', 'S5', DIAG, 2, 9310, None, dict(heap_series=True), None),
    'heapsites-S6': ('heapsites', 'S6', DIAG, 2, 9410, None, dict(heap_series=True), None),
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
        # Attribution-runner instrument check (goroutine stages, thread sampling, timeline overlay); excluded.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run_case_x('xsmoke' + attempt, 'cand', 'stream', 1, 'bbrv3', perf='both', threads=True),
                run_case_x('xsmoke' + attempt, 'cand-timeline', 'datagram', 1, 'bbrv3', path='S5', seed=9995)]
    elif stage == 'escalate':
        # A registered escalation: one more block of one workload (lbneck block 5; s5timeline blocks 5-6).
        name, workload, b = argv[2], argv[3], int(argv[4])
        assert (name, b) in {('lbneck', 5), ('s5timeline', 5), ('s5timeline', 6)}, (name, b)
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
