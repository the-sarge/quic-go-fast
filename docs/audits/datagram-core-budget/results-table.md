# Core-budget diagnostic tables

All changes compare whole runs. Intervals are descriptive bootstrap 95% intervals from four planned pairs; they do not replace the previous acceptance gate.

## Core-budget effects: four versus two

| Binary class | Variant | Complete pairs | p99 change [95% interval] | Goodput change [95% interval] | Combined CPU / delivery change [95% interval] |
| --- | --- | ---: | ---: | ---: | ---: |
| original | base | 4 | -66.90% [-68.41, -65.32] | +6.02% [+5.78, +6.29] | +19.44% [+19.33, +19.55] |
| original | candidate | 4 | -68.40% [-69.53, -67.24] | +5.80% [+5.63, +5.92] | +19.02% [+18.85, +19.15] |
| timing | base | 4 | -70.14% [-71.64, -68.02] | +5.48% [+5.30, +5.66] | +19.80% [+19.66, +19.92] |
| timing | candidate | 4 | -69.41% [-71.82, -67.66] | +6.03% [+5.81, +6.26] | +19.01% [+18.78, +19.31] |

## Run-group medians

| Class | Variant | Cores / endpoint | Runs | Echo p50 (ms) | Echo p99 (ms) | Goodput (Gbps) | Sender cores used | Receiver cores used | Missing / attempted echoes | Rejected echoes |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| original | base | 2 | 4 | 0.884 | 2.058 | 3.769 | 1.528 | 0.979 | 21 / 22531 | 1 |
| original | base | 4 | 4 | 0.126 | 0.680 | 3.995 | 1.981 | 1.196 | 6 / 23975 | 0 |
| original | candidate | 2 | 4 | 0.966 | 2.209 | 3.774 | 1.535 | 0.968 | 21 / 22411 | 0 |
| original | candidate | 4 | 4 | 0.123 | 0.693 | 3.995 | 1.983 | 1.170 | 3 / 23973 | 0 |
| timing | base | 2 | 4 | 0.836 | 2.164 | 3.785 | 1.527 | 0.981 | 19 / 22656 | 0 |
| timing | base | 4 | 4 | 0.127 | 0.658 | 3.995 | 1.975 | 1.197 | 2 / 23956 | 0 |
| timing | candidate | 2 | 4 | 1.068 | 2.208 | 3.769 | 1.535 | 0.971 | 26 / 22628 | 0 |
| timing | candidate | 4 | 4 | 0.128 | 0.691 | 3.996 | 1.988 | 1.175 | 1 / 23983 | 0 |

## Timing-only admission and return observations

Medians across per-run values. Prelock is contained in the request leg; separate stage p99s do not sum to round-trip p99. Tail medians refer to each run’s slowest 1% selected by original successful RTT.

| Variant | Cores | Prelock p99 (ms) | Return p99 (ms) | Tail prelock median (ms) | Tail return median (ms) | Tail prelock share | Tail return share |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| base | 2 | 1.234 | 1.107 | 1.123 | 1.360 | 42.4% | 53.2% |
| base | 4 | 0.416 | 0.322 | 0.610 | 0.062 | 81.1% | 6.9% |
| candidate | 2 | 1.229 | 1.151 | 1.118 | 1.378 | 43.8% | 54.7% |
| candidate | 4 | 0.444 | 0.332 | 0.713 | 0.087 | 83.1% | 8.2% |

## Secondary candidate-versus-baseline comparisons

| Class / cores | Complete pairs | p99 change [95% interval] | Receiver allocation / delivery change [95% interval] | Receiver CPU / delivery change [95% interval] |
| --- | ---: | ---: | ---: | ---: |
| original-p2-candidate-vs-base | 4 | +6.94% [+5.33, +9.28] | -47.13% [-47.13, -47.12] | -1.34% [-1.40, -1.24] |
| original-p4-candidate-vs-base | 4 | +2.08% [-1.38, +5.09] | -47.09% [-47.09, -47.09] | -2.17% [-2.29, -2.03] |
| timing-p2-candidate-vs-base | 4 | +3.20% [-7.07, +17.08] | -47.13% [-47.14, -47.13] | -0.65% [-0.81, -0.53] |
| timing-p4-candidate-vs-base | 4 | +5.72% [-1.39, +13.34] | -47.09% [-47.09, -47.09] | -1.97% [-2.08, -1.82] |

## Individual primary pairs

| Class / variant | Pair | p99 change, four vs two cores |
| --- | ---: | ---: |
| original-base-p4-vs-p2 | r01 | -67.87% |
| original-base-p4-vs-p2 | r02 | -64.60% |
| original-base-p4-vs-p2 | r03 | -66.03% |
| original-base-p4-vs-p2 | r04 | -68.93% |
| original-candidate-p4-vs-p2 | r01 | -69.19% |
| original-candidate-p4-vs-p2 | r02 | -67.16% |
| original-candidate-p4-vs-p2 | r03 | -67.32% |
| original-candidate-p4-vs-p2 | r04 | -69.86% |
| timing-base-p4-vs-p2 | r01 | -70.24% |
| timing-base-p4-vs-p2 | r02 | -71.04% |
| timing-base-p4-vs-p2 | r03 | -66.94% |
| timing-base-p4-vs-p2 | r04 | -72.10% |
| timing-candidate-p4-vs-p2 | r01 | -68.21% |
| timing-candidate-p4-vs-p2 | r02 | -68.99% |
| timing-candidate-p4-vs-p2 | r03 | -72.93% |
| timing-candidate-p4-vs-p2 | r04 | -67.20% |
