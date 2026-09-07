# Paired comparison tables

Percent changes use geometric means of within-pair candidate/base ratios; brackets are bootstrap 95% intervals across whole pairs. Percentage-point changes use paired arithmetic differences. Absolute values are medians across the same complete pairs.

| Offered | Pairs | Receiver allocation B/record, base → candidate | Change (%) | Receiver CPU change (%) | Combined CPU change (%) |
| --- | --- | --- | --- | --- | --- |
| 1 Gbps | 20 | 2454.5 → 1298.9 | -47.079 [-47.081, -47.077] | -3.34 [-3.74, -2.91] | -2.06 [-2.40, -1.70] |
| 4 Gbps | 20 | 2449.5 → 1295.1 | -47.125 [-47.127, -47.123] | -1.45 [-1.56, -1.33] | -0.48 [-0.60, -0.36] |

| Offered | Goodput Gbps, base → candidate | Goodput change (%) | Exact echo p99 µs, base → candidate | p99 change (%) |
| --- | --- | --- | --- | --- |
| 1 Gbps | 0.967857 → 0.967232 | -0.067 [-0.082, -0.052] | 513.51 → 525.06 | +1.02 [-2.41, +4.71] |
| 4 Gbps | 3.797638 → 3.797145 | +0.008 [-0.153, +0.169] | 2116.89 → 2175.49 | +4.04 [-0.20, +8.45] |

| Offered | Missing admitted change (pp) | Generator-missed change (pp) | Missing echo change (pp) |
| --- | --- | --- | --- |
| 1 Gbps | -0.00077 [-0.00146, -0.00010] | +0.06548 [+0.05086, +0.08041] | +0.00084 [-0.00174, +0.00344] |
| 4 Gbps | -0.02253 [-0.02752, -0.01749] | +0.01297 [-0.13902, +0.16606] | -0.02625 [-0.06450, +0.01109] |

Goodput and delivery are transport-record observations, not file completion. Echo RTT includes admission and both endpoints; it is not one-way latency. Missing probes remain explicit. All 40 planned pairs are complete.
