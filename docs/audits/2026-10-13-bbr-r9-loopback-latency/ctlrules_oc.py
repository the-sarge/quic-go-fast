#!/usr/bin/env python3
"""Operating characteristics of registered rules on the synthetic generators (README "Synthetic cases").

depth:  the depth rule, six blocks of 120 lognormal replies per arm (sigma 0.42, the p95/p50 of #740's latcand
        arms), block variation shared by the arms, 200 seeds per true r9 ÷ depth-8 ratio.
syn:    simplified analogs of the runtime synthetic cases at 60 requests per run, for 3, 4 and 5 blocks, 100 seeds
        per case (minimum usable blocks: the count minus one). Each analog adds a constant to the segments its
        injection is expected to move, and its predicate checks the ending and, where the runtime predicate does,
        the endpoint target or a named segment; the runtime predicates' median-rise clauses are not simulated.
stage2: the Stage 2 efficacy classifier with the Stage-1-tied null margin, six blocks of 120 replies per arm,
        for depth increments L and recovered fractions f of the absolute increment (new ÷ r9 = 1 - f (1 - 1/L)),
        200 seeds each.

Usage: ctlrules_oc.py depth|syn|stage2|all
       ctlrules_oc.py syn <blocks> ...
"""
import collections
import sys

import ctlrules as C
import ctlrules_test as T
from ctl import QUEUE_SEGMENTS

SEGS = ['s_pack', 's_entry', 's_queue', 's_submit', 'r_pack', 'r_entry', 'r_queue', 'r_app', 'r_backlog', 't_req']


def depth():
    for true in (1.0, 1.075, 1.15, 1.3):
        c = collections.Counter(C.depth_reading(T.plain(seed, 0.16 * true, 0.16))[0] for seed in range(1000, 1200))
        print(f'depth true ratio {true}: ' + ', '.join(f'{k} {v}' for k, v in sorted(c.items())), flush=True)


CASES = {  # data-level analog of each runtime injection, and its recovery predicate (ctlstages.SYN, on these segments)
    'sq': (dict(add=dict(s_queue=300)), 'queue wait', lambda d: C.targets(d) == ['sender']),
    'rq': (dict(add=dict(r_queue=300)), 'queue wait', lambda d: C.targets(d) == ['receiver']),
    'sw': (dict(add=dict(s_submit=20, s_queue=600)), 'queue wait', lambda d: 'sender' in C.targets(d)),
    'rl': (dict(add=dict(r_backlog=90, r_pack=60, r_queue=30)), ('load or other', 'mixed'), lambda d: 'r_backlog' in d['named_segments']),
    'rw': (dict(add=dict(r_app=300)), 'load or other', lambda d: 'r_app' in d['named_segments']),
    'un': (dict(add=dict(t_req=300)), 'load or other', lambda d: d['unplaced']),
    'aa': (dict(), 'inconclusive', lambda d: True),
}


def syn(geometries=(3, 4, 5)):
    for blocks in geometries:
        for case, (kw, expected, check) in CASES.items():
            ok = 0
            outs = collections.Counter()
            for seed in range(2000, 2100):
                bl = T.pair(seed, a_kw=kw, blocks=blocks, n=60)
                out, d = C.mechanism_reading(bl, T.WIDE, T.PASS, SEGS, QUEUE_SEGMENTS, min_blocks=blocks - 1)
                outs[out] += 1
                ok += (out in expected if isinstance(expected, tuple) else out == expected) and check(d)
            print(f'syn blocks {blocks} case {case}: recovered {ok}/100 ' + ', '.join(f'{k} {v}' for k, v in sorted(outs.items())), flush=True)


def stage2():
    for depth_lo, recovered in ((1.10, 0.0), (1.10, 0.5), (1.10, 1.0), (1.30, 0.0), (1.30, 0.5), (1.30, 1.0)):
        m = C.stage2_null_margin(depth_lo)
        inc = depth_lo  # the depth increment r9 ÷ depth 8, taken at the interval's lower bound
        new = 0.26 * (1 - recovered * (1 - 1 / inc))
        c = collections.Counter(C.stage2_latency(T.Stage2().lat(new, seed=seed), m)[0] for seed in range(3000, 3200))
        print(f'stage2 increment {inc} recovered {recovered} (margin {m:.3f}): ' + ', '.join(f'{k} {v}' for k, v in sorted(c.items())),
              flush=True)


if __name__ == '__main__':
    steps = dict(depth=depth, syn=syn, stage2=stage2)
    if sys.argv[1] == 'syn' and len(sys.argv) > 2:
        syn(tuple(int(x) for x in sys.argv[2:]))
    else:
        for k in (list(steps) if sys.argv[1] == 'all' else sys.argv[1:]):
            steps[k]()
