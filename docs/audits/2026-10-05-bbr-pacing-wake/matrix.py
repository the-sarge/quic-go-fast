#!/usr/bin/env python3
"""Finite #734 order on minimax; observations run sequentially, never concurrently.

Fixture observations go through #715's unchanged run_case_x (stage_run.py), so
the roles, affinity (#712's core layout), launcher, relay, contamination
sampling, receipts and refusal to replace an attempt are #715's. A rerun of a
contaminated or unusable block (the operator's #714 rule) runs the whole block
again with the same seed under `observations-rerun`. After each fixture stage,
`wake.py analyze` runs the analysis on minimax. Launch under `taskset -c 1-3`
(run.HARNESS_CPUS).

Usage: matrix.py <stage> [attempt | comma-separated blocks]
       matrix.py rerun <stage> <workload> <block>
       matrix.py escalate <workload> <block>
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import art  # noqa: F401,E402  (redirects run.ART/OBS)
import run  # noqa: E402
from stage_run import run_case_x  # noqa: E402

# (variant, controller, tag)
PRE = [('cand', 'bbrv3', ''), ('cand-wake-timeline', 'bbrv3', '')]
PRELB = [('cand', 'bbrv3', ''), ('cand-lb-timeline', 'bbrv3', '')]
WAKE = [('cand-wake-timeline', 'bbrv3', '')]
LB = [('cand-lb-timeline', 'bbrv3', '')]
# Stage 2 (README, "Stage 2 mechanism and arms"): frozen Reno, A/A frozen Reno, d0fabc4d instrumented (base),
# d0fabc4d instrumented again (BBR A/A), the new revision instrumented, Reno on the new revision, the 2Q arm.
S2 = [('reno', 'reno', ''), ('reno', 'reno', 'aa'), ('cand-wake-timeline', 'bbrv3', ''), ('cand-wake-timeline', 'bbrv3', 'aa'),
      ('new-wake-timeline', 'bbrv3', ''), ('new', 'reno', ''), ('cand2q-wake-timeline', 'bbrv3', '')]


def rotated(arms, block):
    k = (block - 1) % len(arms)
    order = arms[k:] + arms[:k]
    return list(reversed(order)) if block % 2 == 0 else order


def block(stage, path, arms, b, seed=None, only=None, perf=None):
    rows = []
    for workload in (['stream', 'datagram'] if b % 2 else ['datagram', 'stream']):
        if only and workload != only:
            continue
        for variant, controller, tag in rotated(arms, b):
            rows.append(run_case_x(stage, variant, workload, b, controller, path=path, seed=seed, tag=tag, perf=perf))
    return rows


# stage -> (phase, path, arms, blocks, seed base or None, workload[, perf])
STAGES = {
    # Stage 0 perturbation checks (README, "Stage 0"): plain against instrumented, same blocks.
    'preflight': ('preflight', 'S5', PRE, 3, 9850, None),
    'preflightlb': ('preflightlb', 'loopback', PRELB, 3, None, 'datagram'),
    # Stage 1 (README, "Stage 1"); escalation blocks 5-6 use seeds 9865-9866.
    's1': ('s1', 'S5', WAKE, 4, 9860, None),
    's1lb': ('s1lb', 'loopback', LB, 4, None, 'datagram'),
    # Stage 2: perf stat on both endpoints of every arm, as in #714.
    's2': ('s2', 'S5', S2, 6, 9870, None, 'stat'),
}


def main(argv):
    stage = argv[1]
    run.ensure_credentials()
    if stage == 'smoke':
        # Instrument check only; excluded from every statistic.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run_case_x('smoke' + attempt, 'cand-wake-timeline', 'stream', 1, 'bbrv3', path='S5', seed=9799),
                run_case_x('smoke' + attempt, 'cand-wake-timeline', 'datagram', 1, 'bbrv3', path='loopback')]
    elif stage == 's2smoke':
        # Stage 2 instrument check of the new and 2Q arms; excluded.
        rows = [run_case_x('s2smoke', v, 'stream', 1, 'bbrv3', path='S5', seed=9798, perf='stat')
                for v in ['new-wake-timeline', 'cand2q-wake-timeline']]
    elif stage == 'lbsmoke':
        # Loopback perturbation check of the trace-off recorder against the plain build; excluded.
        attempt = argv[2] if len(argv) > 2 else ''
        rows = [run_case_x('lbsmoke' + attempt, v, 'datagram', 1, 'bbrv3', path='loopback', tag=t)
                for v, t in [('cand', 'a'), ('cand-lb-timeline', 'a'), ('cand', 'b'), ('cand-lb-timeline', 'b')]]
    elif stage == 'escalate':
        # The registered Stage 1 escalation: two more blocks (5, 6) of one workload, once.
        workload, b = argv[2], int(argv[3])
        assert b in (5, 6)
        phase, path, arms, n, base, _, *_ = STAGES['s1']
        rows = block(phase, path, arms, b, seed=base + b, only=workload)
        stage = f'escalate-s1-{workload}-p{b}'
    elif stage == 'rerun':
        name, workload, b = argv[2], argv[3], int(argv[4])
        run.OBS = run.ART / 'observations-rerun'
        phase, path, arms, n, base, _, *perf = STAGES[name]
        rows = block(phase, path, arms, b, seed=base + b if base else None, only=workload, perf=perf[0] if perf else None)
        stage = f'rerun-{name}-{workload}-p{b}'
    else:
        phase, path, arms, n, base, only, *perf = STAGES[stage]
        blocks = [int(x) for x in argv[2].split(',')] if len(argv) > 2 else range(1, n + 1)
        rows = []
        for b in blocks:
            rows += block(phase, path, arms, b, seed=base + b if base else None, only=only, perf=perf[0] if perf else None)
    run.write(run.ART / f'{stage}-summary.json', [dict(id=r['id'], goodput_mbps=r['goodput_mbps']) for r in rows])


if __name__ == '__main__':
    main(sys.argv)
