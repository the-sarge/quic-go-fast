# #871 memory result

Candidate: `8095c72244755f0c1e24f4158253bf09cfbe6e17`. Plain Reno at both endpoints; all arms are descriptive. No cause is attributed.

High-memory threshold: 19.736 MiB (1.10 × frozen-Reno arm median).

| Arm | Peak RSS MiB, median | Paired RSS median (min–max) | High-memory outcomes | Registered RSS outcome |
| --- | ---: | --- | ---: | --- |
| reno-reno | 17.941 | 1.000 (1.000–1.000) | 1/5 | not raised |
| reno-reno-aa | 17.504 | 0.989 (0.974–1.139) | 2/5 | not raised |
| port-reno | 19.984 | 1.114 (0.941–1.182) | 3/5 | raised |
| prior-reno | 18.793 | 1.059 (0.870–1.205) | 2/5 | not raised |
| cand-reno | 18.168 | 0.999 (0.889–1.038) | 0/5 | not raised |

Placement: not reproduced on this candidate; rise present at or before the port; nonmonotonic outcomes; no causal attribution.

Every array below is in block order 1–5. The frozen A/A row beside each metric is the noise reference; range membership does not override the registered median rule.

## Sender peak_rss_mib

| Arm | Absolute MiB by block | Paired ratios by block | Excess MiB by block | Median ratio (min–max) |
| --- | --- | --- | --- | --- |
| reno-reno | 17.941; 20.441; 17.691; 18.004; 17.754 | 1.000; 1.000; 1.000; 1.000; 1.000 | 0.000; 0.000; 0.000; 0.000; 0.000 | 1.000 (1.000–1.000) |
| reno-reno-aa | 17.504; 21.250; 17.504; 20.504; 17.293 | 0.976; 1.040; 0.989; 1.139; 0.974 | -0.438; 0.809; -0.188; 2.500; -0.461 | 0.989 (0.974–1.139) |
| port-reno | 19.984; 19.234; 20.734; 17.484; 20.984 | 1.114; 0.941; 1.172; 0.971; 1.182 | 2.043; -1.207; 3.043; -0.520; 3.230 | 1.114 (0.941–1.182) |
| prior-reno | 18.070; 17.777; 21.320; 20.320; 18.793 | 1.007; 0.870; 1.205; 1.129; 1.059 | 0.129; -2.664; 3.629; 2.316; 1.039 | 1.059 (0.870–1.205) |
| cand-reno | 17.918; 18.168; 17.480; 18.172; 18.422 | 0.999; 0.889; 0.988; 1.009; 1.038 | -0.023; -2.273; -0.211; 0.168; 0.668 | 0.999 (0.889–1.038) |

## Sender heap_peak_measured_mib

| Arm | Absolute MiB by block | Paired ratios by block | Excess MiB by block | Median ratio (min–max) |
| --- | --- | --- | --- | --- |
| reno-reno | 5.715; 6.187; 5.886; 5.941; 6.111 | 1.000; 1.000; 1.000; 1.000; 1.000 | 0.000; 0.000; 0.000; 0.000; 0.000 | 1.000 (1.000–1.000) |
| reno-reno-aa | 5.887; 6.130; 6.049; 6.146; 5.935 | 1.030; 0.991; 1.028; 1.034; 0.971 | 0.172; -0.057; 0.163; 0.205; -0.176 | 1.028 (0.971–1.034) |
| port-reno | 5.891; 5.848; 6.255; 5.824; 6.131 | 1.031; 0.945; 1.063; 0.980; 1.003 | 0.176; -0.339; 0.369; -0.117; 0.020 | 1.003 (0.945–1.063) |
| prior-reno | 5.874; 6.022; 6.122; 5.710; 6.015 | 1.028; 0.973; 1.040; 0.961; 0.984 | 0.160; -0.165; 0.236; -0.231; -0.096 | 0.984 (0.961–1.040) |
| cand-reno | 5.920; 6.106; 5.823; 5.943; 5.861 | 1.036; 0.987; 0.989; 1.000; 0.959 | 0.205; -0.082; -0.062; 0.001; -0.250 | 0.989 (0.959–1.036) |

## Sender heap_peak_warmup_mib

| Arm | Absolute MiB by block | Paired ratios by block | Excess MiB by block | Median ratio (min–max) |
| --- | --- | --- | --- | --- |
| reno-reno | 6.909; 8.624; 6.571; 6.857; 6.800 | 1.000; 1.000; 1.000; 1.000; 1.000 | 0.000; 0.000; 0.000; 0.000; 0.000 | 1.000 (1.000–1.000) |
| reno-reno-aa | 6.173; 8.438; 6.542; 8.151; 6.816 | 0.894; 0.978; 0.996; 1.189; 1.002 | -0.735; -0.186; -0.029; 1.294; 0.016 | 0.996 (0.894–1.189) |
| port-reno | 7.924; 7.509; 8.621; 6.337; 8.054 | 1.147; 0.871; 1.312; 0.924; 1.184 | 1.015; -1.115; 2.049; -0.520; 1.254 | 1.147 (0.871–1.312) |
| prior-reno | 6.456; 6.499; 8.434; 8.160; 7.160 | 0.935; 0.754; 1.284; 1.190; 1.053 | -0.452; -2.125; 1.863; 1.302; 0.360 | 1.053 (0.754–1.284) |
| cand-reno | 5.948; 6.002; 6.104; 6.612; 6.028 | 0.861; 0.696; 0.929; 0.964; 0.886 | -0.961; -2.622; -0.467; -0.246; -0.772 | 0.886 (0.696–0.964) |

The operator must disposition this result before selecting the #887 preservation reference. This record grants no acceptance or advancement exception.
