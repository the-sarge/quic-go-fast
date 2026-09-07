# Paired comparison tables

Percent changes use geometric means of within-pair candidate/base ratios; brackets are bootstrap 95% intervals across whole pairs. Percentage-point changes use paired arithmetic differences. Absolute values are medians across the same complete pairs.

| Offered | Pairs | Receiver allocation B/record, base → candidate | Change (%) | Receiver CPU change (%) | Combined CPU change (%) |
| --- | --- | --- | --- | --- | --- |
| 100 Mbps | 10 | 2466.9 → 1308.2 | -46.97 [-46.99, -46.95] | -2.71 [-3.09, -2.34] | -1.20 [-1.56, -0.83] |
| 1 Gbps | 10 | 2480.2 → 1315.2 | -46.97 [-46.99, -46.94] | -4.76 [-5.44, -4.09] | -2.13 [-2.71, -1.56] |
| 4 Gbps | 9 | 2464.2 → 1303.7 | -47.10 [-47.11, -47.09] | -2.15 [-2.40, -1.86] | -0.92 [-1.19, -0.63] |

| Offered | Goodput Gbps, base → candidate | Goodput change (%) | Exact echo p99 µs, base → candidate | p99 change (%) |
| --- | --- | --- | --- | --- |
| 100 Mbps | 0.095419 → 0.095457 | +0.036 [-0.039, +0.104] | 107.93 → 92.57 | -8.46 [-18.42, +3.49] |
| 1 Gbps | 0.968201 → 0.967649 | -0.050 [-0.086, -0.014] | 545.38 → 573.22 | +2.88 [-2.25, +8.48] |
| 4 Gbps | 3.810427 → 3.818544 | +0.055 [-0.461, +0.430] | 3923.81 → 4081.04 | -1.86 [-13.65, +6.66] |

| Offered | Missing admitted change (pp) | Generator-missed change (pp) | Missing echo change (pp) |
| --- | --- | --- | --- |
| 100 Mbps | +0.00000 [+0.00000, +0.00000] | -0.03307 [-0.09665, +0.03693] | +0.00000 [+0.00000, +0.00000] |
| 1 Gbps | +0.00036 [-0.00559, +0.00652] | +0.04706 [+0.01831, +0.07555] | -0.02065 [-0.05162, +0.00000] |
| 4 Gbps | -0.06235 [-0.07966, -0.04458] | +0.00640 [-0.33991, +0.48227] | +0.05157 [-0.24238, +0.33318] |

Goodput and delivery are transport-record observations, not file completion. Echo RTT includes admission and both endpoints; it is not one-way latency. Missing probes remain explicit. The 4 Gbps setup failure and its unpaired candidate are retained outside matched estimates.
