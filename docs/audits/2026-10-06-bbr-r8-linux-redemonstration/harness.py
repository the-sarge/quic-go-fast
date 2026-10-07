#!/usr/bin/env python3
"""#736 registered rules that #715's rules.py does not hold.

Written, with synthetic cases in harness_test.py, before any counted
observation existed. Registered in README.md.

recvharness: the readiness receiver CPU measure (whole-run CPU seconds per
useful GiB, getrusage, no perf attached) against #735's counted harness (the
same runs with `perf stat` attached to both endpoints for the measured window).
Per usable block, against the same block's frozen Reno:
    P = plain candidate / plain Reno           (the readiness measure)
    Q = perf candidate / perf Reno, whole run  (the readiness measure under perf)
    W = perf candidate / perf Reno, in-window task-clock (#735's measure; descriptive)

heap_cell: #711's heap-site classification over four blocks (D5): every usable
block must meet the unchanged 70% rule with the same group.
"""
import statistics

LIMIT = 1.10
MIN_BLOCKS = 3


def harness_verdict(blocks, limit=LIMIT, min_blocks=MIN_BLOCKS):
    """blocks: [{'P': float, 'Q': float, 'W': float or None}] for one workload, usable blocks only."""
    if len(blocks) < min_blocks:
        return dict(verdict='evidence gap', blocks=len(blocks))
    med = lambda k: statistics.median(b[k] for b in blocks)
    out = dict(blocks=len(blocks), P=med('P'), Q=med('Q'), P_minus_Q=[b['P'] - b['Q'] for b in blocks])
    ws = [b['W'] for b in blocks if b.get('W') is not None]
    out['W'] = statistics.median(ws) if ws else None
    lower = sum(b['P'] > b['Q'] for b in blocks)
    if not out['P'] > limit:
        return dict(out, verdict='not reproduced')
    if not out['Q'] > limit and lower >= len(blocks) - 1:
        return dict(out, verdict='perf attachment')
    if out['Q'] > limit:
        return dict(out, verdict='excess under both harnesses')
    return dict(out, verdict='inconclusive')


def heap_cell(classes, min_blocks=MIN_BLOCKS):
    """classes: per usable heap-site block, #711's per-block classification ('delivery', 'bookkeeping' or 'unresolved')."""
    if len(classes) < min_blocks:
        return 'unresolved (evidence gap)'
    if len(set(classes)) == 1 and classes[0] in ('delivery', 'bookkeeping'):
        return classes[0]
    return 'unresolved'
