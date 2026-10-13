#!/usr/bin/env python3
"""Operating characteristics of the registered depth rule on the synthetic generator (README "Synthetic cases").

Six blocks of 120 lognormal replies (sigma 0.42, the p95/p50 of #740's latcand arms) per arm, block
variation shared by the arms, 200 seeds per true r9 ÷ depth-8 ratio. Prints each ending's count.
"""
import collections

import ctlrules as C
import ctlrules_test as T

for true in (1.0, 1.075, 1.15, 1.3):
    c = collections.Counter(C.depth_reading(T.plain(seed, 0.16 * true, 0.16))[0] for seed in range(1000, 1200))
    print(f'true ratio {true}: ' + ', '.join(f'{k} {v}' for k, v in sorted(c.items())))
