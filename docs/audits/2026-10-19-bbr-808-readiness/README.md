# Readiness of the combined BBRv3 revision on Linux (#808)

**Date:** October 19, 2026, America/New_York. **Scope:** step 1 of [#808](https://github.com/the-sarge/quic-go-fast/issues/808), one readiness and L6 campaign on the revision that carries #742, #800, #803, #743 and #824. Measurement only: no code change, qualification, advancement decision, default-controller change, release, paid resource, host-setting change or ledger change. The campaign's decisions, the advancement disposition and the requirements this campaign discharges are in the [campaign record](../2026-10-19-bbr-808-campaign/README.md).

Branch `codex/bbr-808-campaign`, based on `main` at `e7eb3f10`; the candidate is `main` at `d94b3ec2` (below). Unlike the #666 and #748 records, this record lands on `main` by one docs-only pull request (campaign decision D9); its raw archive stays on a pinned branch off `main`, and `raw-manifest.json` on `main` holds its checksums. [#748 slice 6's readiness record](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md) (`9b38f0ab`, on `codex/bbr-port-readiness`) supplies the stage, the aids and the host layout that this record reruns.

**Status: complete.** The sections from [The candidate](#the-candidate) to [Order](#order) are the registration, written with the aids adapted and the binaries built before any prerequisite, smoke, readiness or attribution observation of this record, and reviewed twice before data ([Registration review](#registration-review)). The [Answer](#answer) and the sections after [Results](#readiness-results) were added after the stage; the registration is unchanged. The advancement disposition is in the [campaign record](../2026-10-19-bbr-808-campaign/README.md#advancement-disposition).

## Question

On owned Linux hardware, what are the readiness flags of the combined revision (`main` at `d94b3ec2`), what does the S5 timeline show, and does any raised Reno-on-candidate preservation cell reproduce under its registered stage? Beside each cell: what did slice 6 record for the port (`abfa5456`)?

## Answer

**On `minimax`, the combined revision (`main` at `d94b3ec2`) raises ten cells, as the port did. Nine are candidate cells: the same nine memory and control p95 cells slice 6 raised. The tenth is a Reno-on-candidate preservation cell, but a different one: S5 DATAGRAM sender RSS, at 1.157. Its registered `presrss` stage reads *not reproduced*. Every goodput and CPU cell passes, the S6 benefit holds, and the cell slice 7 ruled on is not raised (0.720).**

- **Useful benefit holds.** On S6 the candidate's goodput is 14.14× Reno's (STREAM) and 13.74× (DATAGRAM), beating Reno in five of five pairs each. Slice 6 recorded 14.28× and 14.42×.
- **Every goodput and CPU cell passes,** on every path. #738's S5 DATAGRAM sender CPU per useful GiB is 0.965 of frozen Reno's (slice 6: 0.948). #734's wake increase, read through CPU per useful GiB, passes everywhere (sender 0.929–1.025, receiver 0.940–1.053 on loopback and S5).
- **The nine candidate cells**, with slice 6's values in brackets:
  - S5: STREAM sender RSS 1.192 [1.217] and receiver RSS 1.417 [1.482]; DATAGRAM sender RSS 1.181 [1.157].
  - S6: STREAM sender RSS 1.640 [1.656] and receiver RSS 1.481 [1.832]; DATAGRAM sender RSS 1.481 [1.466]; control p95 1.414 [1.581] (STREAM) and 1.415 [1.461] (DATAGRAM).
  - Loopback STREAM control p95: 1.981 [1.656], crossing in all five blocks. The candidate's own p95 is unchanged at 0.215 ms (slice 6: 0.213 ms); frozen Reno's was lower this time, 0.115 ms against 0.145 ms. The frozen-Reno A/A arm also crosses this cell's limit (1.222).
  - Six are lower than slice 6's and three higher: S5 DATAGRAM sender RSS (+0.024), S6 DATAGRAM sender RSS (+0.015) and loopback STREAM control p95.
- **The preservation cell: Reno on candidate, S5 DATAGRAM sender RSS, 1.157,** crossing in three of five blocks (18.38 → 21.26 MiB; median excess 2.88 MiB, largest 3.63 MiB). Its A/A range is 0.914–1.070, so the median lies above it. Two facts read beside it:
  - **It is raised only under the rerun rule.** Three of its five S5 DATAGRAM blocks were contaminated by GridCast CI and rerun with the same seed; the reruns replaced them. On the original blocks (`summary-original.json`, #712's rule) the cell reads 1.009, not raised.
  - **`presrss` reads *not reproduced*:** six clean blocks with heap series, RSS ratio 1.001, retained-heap ratio 1.020, no high-memory outcome in either arm. The stage uses the `-diag` builds, as #715 registered it, while readiness uses the plain builds, where Reno on candidate had four high-memory outcomes against frozen Reno's one.
  - This is the cell #712 raised at 1.152; #715, #736, #739 and slice 6 did not raise it (1.002, 1.066, 1.023, 1.006).
- **What #800 changed is visible on the bottleneck paths,** though readiness has no rule for it. BBRv3 STREAM's forward overflow fell from 0.217% to 0.023% on S5 and from 0.239% to 0.029% on S6. Its receiver heap peak fell from 14.36 to 4.56 MiB (S5) and from 14.98 to 9.41 MiB (S6). S6 STREAM control p95 fell from 199.3 to 179.4 ms.
- **What #824 changed is visible on loopback DATAGRAM:** the candidate's and Reno on candidate's DATAGRAM loss is 0% by median, against frozen Reno's 0.063% (its 128-entry queue).
- **S5 timing stays model-bound.** The S5 timeline reads model behaviour in both workloads, as on slice 6 and r9.

## Readiness results

Plain builds, five blocks per path and workload: 110 counted observations, all exiting cleanly and passing receiver integrity. Four S5 blocks (DATAGRAM 1–3, STREAM 2) held contaminated observations, from GridCast's CI on the host; each was rerun once with the same seed, every rerun came back usable and uncontaminated, and the reruns replaced them. One flag differs between `summary.json` and `summary-original.json`: the Reno-on-candidate S5 DATAGRAM sender RSS cell, raised at 1.157 under the rerun rule and 1.009 (not raised) on the original blocks.

How to read the table: each ratio is the median of per-block ratios against the same block's frozen Reno; **bold** marks a raised flag, with the blocks crossing in parentheses; brackets give slice 6's value (bold where raised there); the A/A per-block range follows each ratio. Every cell has all five blocks. Generated by [tables.py](tables.py) from [summary.json](summary.json) and [port-context.json](port-context.json); `tables.py --check` confirms each table here.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | 1.160 [1.160]; A/A 0.994–1.002 | 1.015 [1.014]; A/A 0.998–1.005 | 0.940 [0.938]; A/A 1.000–1.002 | 0.968 [0.974]; A/A 0.996–1.045 | 1.094 (1) [1.095]; A/A 0.986–1.012 | **1.981 (5)** [**1.656**]; A/A 0.796–1.700 |
| Loopback STREAM | Reno on candidate | 0.998 [0.998]; A/A 0.994–1.002 | 1.003 [1.003]; A/A 0.998–1.005 | 1.001 [1.003]; A/A 1.000–1.002 | 1.010 [0.998]; A/A 0.996–1.045 | 1.005 [1.002]; A/A 0.986–1.012 | 0.879 (2) [0.880]; A/A 0.796–1.700 |
| Loopback DATAGRAM | Candidate | 0.996 [0.995]; A/A 0.994–1.005 | 1.025 [1.028]; A/A 0.998–1.006 | 0.967 [0.968]; A/A 0.991–1.011 | 0.961 [0.949]; A/A 0.962–1.024 | 1.069 (1) [1.098]; A/A 0.930–1.008 | 0.776 (1) [0.695]; A/A 0.260–1.542 |
| Loopback DATAGRAM | Reno on candidate | 0.996 [0.999]; A/A 0.994–1.005 | 1.000 [1.002]; A/A 0.998–1.006 | 0.994 [0.998]; A/A 0.991–1.011 | 1.026 [0.996]; A/A 0.962–1.024 | 0.996 [0.987]; A/A 0.930–1.008 | 0.720 (2) [**1.504**]; A/A 0.260–1.542 |
| S5 STREAM | Candidate | 0.971 [0.972]; A/A 1.000–1.000 | 0.929 [0.927]; A/A 0.991–1.006 | 1.053 [1.029]; A/A 0.983–1.020 | **1.192 (4)** [**1.217**]; A/A 0.797–1.028 | **1.417 (5)** [**1.482**]; A/A 0.899–1.029 | 1.037 [1.096]; A/A 0.976–1.153 |
| S5 STREAM | Reno on candidate | 1.000 [1.000]; A/A 1.000–1.000 | 1.004 [1.002]; A/A 0.991–1.006 | 0.995 [0.997]; A/A 0.983–1.020 | 1.004 [0.992]; A/A 0.797–1.028 | 1.006 [0.974]; A/A 0.899–1.029 | 1.039 [0.997]; A/A 0.976–1.153 |
| S5 DATAGRAM | Candidate | 0.972 [0.972]; A/A 1.000–1.000 | 0.965 [0.948]; A/A 0.988–1.009 | 1.005 [0.992]; A/A 0.996–1.012 | **1.181 (4)** [**1.157**]; A/A 0.914–1.070 | 1.058 [1.072]; A/A 1.000–1.015 | 1.000 [1.037]; A/A 0.971–1.016 |
| S5 DATAGRAM | Reno on candidate | 1.000 [1.000]; A/A 1.000–1.000 | 1.003 [0.995]; A/A 0.988–1.009 | 1.005 [0.999]; A/A 0.996–1.012 | **1.157 (3)** [1.006]; A/A 0.914–1.070 | 1.001 [1.008]; A/A 1.000–1.015 | 1.047 [1.005]; A/A 0.971–1.016 |
| S6 STREAM | Candidate | benefit 14.14× [12.06–15.69], 5/5 [14.28×, 5/5] | 0.087 [0.089]; A/A 0.962–1.073 | 0.075 [0.073]; A/A 0.965–1.055 | **1.640 (5)** [**1.656**]; A/A 1.000–1.035 | **1.481 (5)** [**1.832**]; A/A 0.983–1.034 | **1.414 (5)** [**1.581**]; A/A 0.965–1.036 |
| S6 DATAGRAM | Candidate | benefit 13.74× [12.29–15.75], 5/5 [14.42×, 5/5] | 0.093 [0.088]; A/A 0.974–1.035 | 0.081 [0.076]; A/A 0.965–1.056 | **1.481 (5)** [**1.466**]; A/A 0.981–1.016 | 1.100 (2) [1.090]; A/A 0.996–1.015 | **1.415 (5)** [**1.461**]; A/A 0.945–1.055 |

Absolute medians, with slice 6's in brackets (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Receiver heap peak (measured) | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4692.4 [4692.8] | 3.98 / 2.75 [3.98 / 2.75] | 16.96 / 15.28 [16.77 / 15.27] | 3.24 [3.22] | 0.115 / 0.070 [0.145 / 0.074] | — |
| Loopback STREAM | Candidate | 5449.1 [5459.6] | 4.04 / 2.58 [4.04 / 2.58] | 16.27 / 16.75 [16.37 / 16.79] | 4.62 [4.33] | 0.215 / 0.128 [0.213 / 0.138] | — |
| Loopback STREAM | Reno on candidate | 4681.9 [4679.0] | 4.00 / 2.75 [3.99 / 2.76] | 17.15 / 15.40 [16.75 / 15.31] | 3.24 [3.29] | 0.118 / 0.068 [0.124 / 0.071] | — |
| Loopback DATAGRAM | Reno | 4670.5 [4661.9] | 4.54 / 3.03 [4.54 / 3.02] | 18.16 / 19.84 [18.21 / 20.05] | 4.64 [4.60] | 0.490 / 0.101 [0.422 / 0.082] | — |
| Loopback DATAGRAM | Candidate | 4633.3 [4637.9] | 4.65 / 2.94 [4.67 / 2.92] | 17.55 / 21.41 [17.73 / 21.53] | 6.99 [6.86] | 0.291 / 0.167 [0.304 / 0.172] | — |
| Loopback DATAGRAM | Reno on candidate | 4658.8 [4658.7] | 4.55 / 3.02 [4.55 / 3.02] | 18.36 / 19.75 [18.33 / 19.68] | 4.86 [4.83] | 0.352 / 0.105 [0.364 / 0.094] | — |
| S5 STREAM | Reno | 95.9 [95.9] | 10.31 / 8.71 [10.40 / 8.80] | 18.63 / 17.62 [18.80 / 17.88] | 3.46 [3.42] | 178.511 / 173.127 [182.698 / 175.951] | 0.098% [0.094%] |
| S5 STREAM | Candidate | 93.1 [93.2] | 9.60 / 9.11 [9.64 / 9.03] | 22.51 / 24.97 [22.73 / 26.50] | 4.56 [14.36] | 185.143 / 102.113 [198.943 / 102.078] | 0.023% [0.217%] |
| S5 STREAM | Reno on candidate | 95.9 [95.9] | 10.34 / 8.67 [10.42 / 8.71] | 18.76 / 17.72 [18.64 / 17.42] | 3.43 [3.40] | 188.333 / 182.504 [182.622 / 176.561] | 0.116% [0.093%] |
| S5 DATAGRAM | Reno | 94.4 [94.4] | 11.43 / 8.91 [11.54 / 8.93] | 18.38 / 14.95 [18.63 / 14.88] | 3.23 [3.28] | 176.189 / 170.932 [177.523 / 170.655] | 0.390% [0.398%] |
| S5 DATAGRAM | Candidate | 91.7 [91.8] | 11.02 / 8.93 [10.92 / 8.86] | 21.70 / 15.77 [21.70 / 15.94] | 4.18 [4.18] | 178.161 / 102.379 [187.289 / 102.146] | 0.025% [0.025%] |
| S5 DATAGRAM | Reno on candidate | 94.4 [94.4] | 11.45 / 8.93 [11.47 / 8.91] | 21.26 / 14.90 [18.98 / 14.99] | 3.31 [3.31] | 184.441 / 177.620 [178.620 / 172.333] | 0.460% [0.406%] |
| S6 STREAM | Reno | 6.2 [6.1] | 111.69 / 120.05 [109.87 / 121.18] | 13.88 / 14.38 [13.88 / 14.63] | 3.46 [3.47] | 124.170 / 104.654 [125.512 / 105.597] | 0.000% [0.000%] |
| S6 STREAM | Candidate | 87.9 [87.9] | 9.71 / 9.00 [9.80 / 8.87] | 22.51 / 21.30 [22.98 / 26.89] | 9.41 [14.98] | 179.397 / 102.178 [199.259 / 102.093] | 0.029% [0.239%] |
| S6 DATAGRAM | Reno | 6.1 [5.9] | 119.48 / 116.21 [122.97 / 121.07] | 14.60 / 14.38 [14.63 / 14.59] | 3.32 [3.34] | 124.559 / 104.027 [126.644 / 104.530] | 0.000% [0.000%] |
| S6 DATAGRAM | Candidate | 84.9 [85.7] | 10.97 / 9.40 [10.85 / 9.29] | 21.76 / 15.98 [21.75 / 15.85] | 4.16 [4.20] | 182.021 / 102.100 [184.585 / 102.022] | 0.044% [0.029%] |

## S5 timeline: model behaviour

`s5timeline` ran straight after readiness (8 observations, all clean, none rerun). No workload had exactly two of four agreeing blocks, so there was no escalation. Instrumented goodput is 0.997 (STREAM) and 1.004 (DATAGRAM) of the readiness candidate's.

| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | 0.028–0.036 | 0.032–0.039 | 0.019–0.024 | 0.0001–0.0001 | 0.013–0.015 | model ×4 → **model** |
| DATAGRAM | 0.019–0.024 | 0.019–0.024 | 0.010–0.015 | 0.0001–0.0001 | 0.008–0.009 | model ×3, no deficit ×1 → **model** |
| slice 6 STREAM | 0.023–0.037 | 0.025–0.038 | 0.014–0.024 | 0.0002–0.0003 | 0.010–0.015 | model ×4 → **model** |
| slice 6 DATAGRAM | 0.022–0.035 | 0.020–0.034 | 0.012–0.024 | 0.0001–0.0002 | 0.007–0.010 | model ×4 → **model** |

Late arrivals at a full-quantum deadline per 30 s run: 28,103–28,268 (STREAM) and 28,799–28,988 (DATAGRAM), totalling 0.207–0.222 s per run (slice 6: 27,264–28,946 and 0.215–0.227 s). The connection goroutine is pacer-bound for 0.95–0.97 of the window. Phase shares are slice 6's within a few points: Cruise 0.62–0.68, Down 0.16–0.21, Up 0.09–0.11, ProbeRTT 0.02–0.05.

**Receiver CPU split.** No S5 receiver CPU cell is raised; [attribution.json](attribution.json) `cpu_split` holds the descriptive figures.

## Preservation

| Stage | Cell | Outcome | Detail |
| --- | --- | --- | --- |
| `presrss` | S5/datagram/send | *not reproduced* | rss_ratio 1.001, retained_ratio 1.02, high_cand 0, high_reno 0, blocks 6 |

`presrss` ran for the one triggered pair (S5, DATAGRAM): six blocks of `reno-diag` and `cand-diag` with Reno at both endpoints, all clean, no rerun. `preslat` did not trigger: no Reno-on-candidate loopback control p95 cell is raised. The readiness value stays as recorded: 1.157, raised. Readiness's high-memory outcomes in this cell (peak RSS above 1.10× frozen Reno's median, 20.21 MiB): frozen Reno 1, A/A 0, Reno on candidate 4. Every other Reno-on-candidate cell lies within 0.994–1.026 on goodput, CPU and RSS, and within 0.720–1.047 on control p95.

## DATAGRAM loss (descriptive)

From [datagram-loss.json](datagram-loss.json) ([dgloss.py](dgloss.py)): on S5 and S6 the relay's drops account for every lost DATAGRAM (−1 is one dropped packet that carried no DATAGRAM, as #824 described). On loopback (no relay), the frozen Reno arms lose 0.055–0.063% at the receiver; the candidate and Reno on candidate lose none by median.

| Path | Arm | DATAGRAM loss, median (max) | Lost, median | Lost beyond the relay's drops, median |
| --- | --- | --- | --- | --- |
| S5 | cand-bbrv3 | 0.0250% (0.0306%) | 96 | 0 |
| S5 | cand-reno | 0.4605% (0.4878%) | 1831 | 0 |
| S5 | reno-reno | 0.3899% (0.4105%) | 1549 | 0 |
| S5 | reno-reno-aa | 0.3802% (0.4152%) | 1510 | 0 |
| S6 | cand-bbrv3 | 0.1385% (0.2799%) | 487 | -1 |
| S6 | reno-reno | 0.0922% (0.1126%) | 30 | 0 |
| S6 | reno-reno-aa | 0.0956% (0.1120%) | 30 | 0 |
| Loopback | cand-bbrv3 | 0.0000% (0.0000%) | 0 | — (no relay) |
| Loopback | cand-reno | 0.0000% (0.0049%) | 0 | — (no relay) |
| Loopback | reno-reno | 0.0629% (0.0734%) | 7761 | — (no relay) |
| Loopback | reno-reno-aa | 0.0549% (0.0631%) | 6747 | — (no relay) |

## Flag report for the advancement disposition

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 14.14× (STREAM) and 13.74× (DATAGRAM) Reno goodput, 5/5 pairs each, at 0.087 / 0.093× Reno's sender CPU per useful GiB. |
| **Not raised** | Every goodput and CPU cell on every path. Loopback sender and receiver RSS (both workloads), loopback DATAGRAM control p95, S5 control p95 (both), S5 DATAGRAM receiver RSS, S6 DATAGRAM receiver RSS (1.100, two blocks crossing). Every Reno-on-candidate cell but one, including the loopback DATAGRAM control p95 (0.720). |
| **Raised, candidate (9)** | S5 STREAM sender RSS 1.192 and receiver 1.417; S5 DATAGRAM sender 1.181; S6 STREAM sender 1.640 and receiver 1.481; S6 DATAGRAM sender 1.481; S6 control p95 1.414 (STREAM) and 1.415 (DATAGRAM); loopback STREAM control p95 1.981. |
| **Raised, preservation (1)** | Reno on candidate, S5 DATAGRAM sender RSS, 1.157 (raised under the rerun rule only; 1.009 on the original blocks). `presrss`: *not reproduced*. Not attributed. **Blocks advancement** under the contract. |
| **Not measured** | The run-loop wake count; macOS and Windows; #742's and #743's send-path changes (the fixture never GSO-batches; the side stage measures #743 on BBRv3). |

## Deviations

None. No rule, threshold, seed, arm, cap or stage changed after data.

**Disclosed operational facts.**
- The pre-start check passed (`PRESTART-EMPTY`, 06:26:52 UTC).
- The prerequisite review ran on `minimax` at about 06:33 UTC, during the first loopback block, under `nice -n 19 taskset -c 1-3` (runner cores), as in slice 6. All prerequisites passed: the unadapted relay strips ECN and the adapted one preserves all four codepoints; forward and reverse packets after the handshake carry ECT(0) on S5 and S6, and BBR ECN tracker frames appear in the sender samples; 0.999 send syscalls and transmits per forward packet; every thread inside its CPU set; 8 MiB effective socket buffers; no UDP errors; `perf stat` at 100% running and `perf record` resolving Go frames; the timeline overlay covering the 30 s window at a bandwidth near the bottleneck.
- The quiet-host gate waited 36 one-minute windows in all, mostly during GridCast CI (a `transfer.test` runner and a Windows GARM VM) and one wiremux Windows CI VM. Four S5 blocks were still contaminated and were rerun.
- After `STAGE.DONE` (09:27:35 UTC) the observations were copied to the Mac, where `stages.py prereq`, `stages.py all`, `portcontext.py`, `dgloss.py` and `tables.py` ran.
- `resume.py mark` marked nothing; no stage stopped.

**Timing.** Prerequisites 5 minutes; readiness 2 h 16 min including the quiet-host waits and four block reruns (06:32–08:48 UTC); `s5timeline` 10 minutes; `presrss` 15 minutes; the side stage 14 minutes. 3 h 01 min in all, against the registered 2 h 15 min to 3 h 20 min.

## Integrity and host

- **Payload integrity.** Every retained observation exited cleanly and passed receiver integrity: 110 readiness, 16 reruns, 8 timeline, 12 `presrss`, and the 7 excluded prerequisite and smoke runs.
- **No code change** and **no host change**: all 20 `hostfacts` snapshots match the first (powersave, EPP performance, boost on, every idle state enabled).

## Limits

Slice 6's, unchanged: one Linux host with loopback endpoints and a userspace relay; five blocks per readiness cell; loopback control p95 is a sub-millisecond quantity at 20 replies per run; a difference from slice 6 is descriptive (different runs, and the candidate's code differs by #742, #743, #800, #803, #812, #824 and the later `main` changes listed above).

## The candidate

**`main` at `d94b3ec21e7d58e0ebcc5ff2dca012f856dd2cd7`.** The registration first named `e7eb3f10`, the `main` commit when it was written (campaign decision D1). Before any data, four production commits landed on `main`, two of them on the shared send path (#836, #844). As D1 requires, the agent stopped and asked. On October 19, 2026, at about 02:30 America/New_York, the operator chose to re-pin the candidate to `d94b3ec2`, so the advancement would cover the `main` that Q2 would qualify. Its production code is identical to `7d05a887` (the last commit changes the journal only). Later `main` commits are not measured.

Production changes since the port head `abfa5456`, and whether each reaches this stage's fixture:

| Change | Merged | Production files | Reaches the readiness fixture? |
| --- | --- | --- | --- |
| [#800](https://github.com/the-sarge/quic-go-fast/issues/800): supply limitation only after a pacing-credit refill; draft-06's loss prefix | `1f9ceb6e` | `bbr_send_policy.go`, `delivery_sampling.go`, `packet_emission_bbr.go`, `internal/ackhandler/delivery_sampler.go`, `internal/congestion/bbr_probe.go` | Yes: every BBRv3 arm |
| [#803](https://github.com/the-sarge/quic-go-fast/issues/803): retire ECN ledger records below an ACK's lowest acknowledged packet | `843fde80` | `internal/ackhandler/bbr_ecn.go` | Yes: every BBRv3 arm |
| [#812](https://github.com/the-sarge/quic-go-fast/issues/812): diagnostics report the latest stop and the active limitation separately | `30d823c6` | `internal/ackhandler/bbr_diagnostics.go`, `delivery_sampler.go` | Diagnostic text only; the fixture runs with tracing disabled |
| [#824](https://github.com/the-sarge/quic-go-fast/issues/824): 1,024-entry received-DATAGRAM queue, counted discards, cleared slots | `42b9b5de` | `datagram_queue.go`, `connection.go` | Yes: every DATAGRAM receiver, Reno and BBRv3 |
| [#742](https://github.com/the-sarge/quic-go-fast/issues/742): a lone GSO packet without a segment size | `592e98cb` | `packet_emission.go`, `packet_emission_bbr.go` | **No**: the fixture never GSO-batches (below) |
| [#743](https://github.com/the-sarge/quic-go-fast/issues/743): equal-size GSO batches of short DATAGRAM packets | `9b52dca8` | `packet_emission.go`, `packet_emission_bbr.go`, `packet_emission_gso.go`, `packet_packer.go`, `connection.go` | **No**: as above |
| [#809](https://github.com/the-sarge/quic-go-fast/issues/809): the GSO-error fallback only for writes with a segment size | `501e80bd` | `send_conn.go` | **No**: as above |
| [#756](https://github.com/the-sarge/quic-go-fast/issues/756), [#786](https://github.com/the-sarge/quic-go-fast/issues/786), [#780](https://github.com/the-sarge/quic-go-fast/issues/780): handshake size fallback | `81a72481`, `57f9df04`, `814bd485` | `config.go`, `connection.go`, `interface.go`, `transport.go`, `internal/ackhandler/` | **No**: each applies only to an unset `InitialPacketSize`; the fixture sets 1,400 |
| [#844](https://github.com/the-sarge/quic-go-fast/pull/844): bounded path-probe history, probes retired atomically | `7d05a887` | `internal/ackhandler/sent_packet_handler.go`, `sent_packet_history.go`, `interfaces.go`, `packet_emission.go` | Partly: the sent-packet history every connection keeps changed shape; the probe paths themselves do not run (the fixture never migrates) |
| [#836](https://github.com/the-sarge/quic-go-fast/pull/836): per-path amplification limits on migration probes | `36fd7bc5` | `path_manager.go`, `packet_emission.go`, `packet_packer.go`, `connection.go` | **No** in effect: migration probes only; the fixture never migrates |
| [#838](https://github.com/the-sarge/quic-go-fast/pull/838), [#839](https://github.com/the-sarge/quic-go-fast/pull/839): HTTP/3 header buffering and bounds | `a14c2eda`, `28a01df8` | `http3/` | **No**: the fixture does not use HTTP/3 |

**Why GSO never engages here.** The frozen fixture sends through the managed packet-I/O lease (`ConfigureManagedPacketIOV1` with no batch callback). In a development pair on `minimax` (below), the candidate's loopback DATAGRAM sender made 8,786,578 `sendmsg` calls for 8,786,534 transmits, and 8,964,248 for 8,964,205 with `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true`: one packet per call either way. Slice 6's native-I/O prerequisite read 0.999 sends per packet on S5 for the same reason. So this stage measures #800, #803 and #824 on the port (and #844's history change, which every connection carries); it cannot measure #742 or #743. The campaign's [side stage](../2026-10-19-bbr-808-gso-receive/README.md) is where #743 is measured on BBRv3 (campaign decision D8, and the operator's amendment of D3).

The candidate is a new revision. **No readiness exception granted to the port (slice 7's [D3 and D4](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md#d3--unattributed-readiness-exceptions-the-nine-candidate-cells)) covers it.**

## Baselines

- **Readiness baseline: frozen Reno, `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`.** Every ratio is the arm's value over the same block's frozen Reno value. The frozen Reno fixture rebuilds byte-identical to slice 6's (`cda8feb1…`), so this record's ratios use the same denominator binary as slice 6's, #739's, #736's and #715's.
- **A/A: frozen Reno run twice per block,** reported beside every ratio; never overrides the rule.
- **Slice 6's recorded values, as context only.** [portcontext.py](portcontext.py) puts slice 6's value beside every cell (median, raised flag, blocks crossing), its absolute medians and its S5 timeline, read mechanically from `9b38f0ab`'s `summary.json` and `attribution.json`. They come from runs on October 16; the comparison is descriptive, and a difference from slice 6 neither raises nor clears a cell.

## How a Reno-on-candidate difference is reported

Slice 6's section, unchanged ([How a Reno-on-candidate difference is reported](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#how-a-reno-on-candidate-difference-is-reported)). The Reno-on-candidate arm measures `main`'s Reno at `d94b3ec2` against frozen Reno, so it bundles the port's Reno-reachable merge seams, every non-BBR change `main` carries since `e4f322cb`, and code placement. Since slice 6, one more change reaches Reno in this fixture: **#824's received-DATAGRAM queue** (1,024 entries instead of 128, with delivered slots cleared). A raised Reno-on-candidate cell stays a raised preservation flag, **not attributed** to #824, the port or `main`'s other changes; no stage here separates them. The contract's sentence stands: "A raised preservation flag blocks advancement." Presenting it for a decision is the advancement disposition's job, not this record's.

## What stays fixed and what changes

- **Fixed: slice 6's stage, entirely** ([What stays fixed and what changes](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#what-stays-fixed-and-what-changes)): the arms, paths, durations, seeds, rotation, flag limits, median rule, A/A reporting, contamination rule and its cap of ten readiness block reruns, quiet-host gate, core layout, frozen Reno, the fixture with the causal-diagnosis heap patch, the relay, the launcher, the endpoint contract, `GOTOOLCHAIN=go1.27.0` for every build, #715's timeline rule, and #739's preservation stages and outcome rules without D3's mapping (no preservation outcome ends a blocking state here).
- **Changed, all recorded:**
  - *Candidate:* `cand` is `d94b3ec2` (re-pinned from `e7eb3f10` by the operator before any data).
  - *Context:* slice 6's values replace r9's beside each cell ([portcontext.py](portcontext.py), slice 6's `r9context.py` with the prior record and its key names changed; [portcontext_test.py](portcontext_test.py), 8 tests).
  - *Builds:* [build.py](build.py) is slice 6's with four recorded changes (its docstring): the candidate; the artifact directory (`.local/bbr-808-readiness`); every copied aid checked byte-identical against slice 6's record instead of the older ones; and `REGISTERED`, this record's new binaries.
  - *Paths:* [art.py](art.py) and [sync.sh](sync.sh). One sync serves all three #808 stages, which share `~/agents/quic-808` on `minimax`. It excludes every host-written path (each stage's observations, calibration, host-fact snapshots, development runs, JSON and logs, the credentials, the markers and the side stage's yield request), so `--delete` never removes one and the Mac never overwrites one; a second sync to a local target that held each of them left all of them in place.
  - *Unattended order:* [driver.sh](driver.sh) is slice 6's with its paths, the `RUNNING` and `STAGE.DONE` markers at the worktree root (agreed with the wiremux sessions that share the host, as #792's driver used them), `stages.py fold` before the side stage (so nothing reads perf data while the side stage measures), and the side stage last. `STAGE.DONE` means the host is free, and its text says how the order ended: `complete`, `readiness complete; side stage failed` (the driver then exits non-zero), or `stopped` for any earlier exit.
  - *New, presentation only:* [tables.py](tables.py) generates the result tables from `summary.json`, `port-context.json`, `attribution.json` and `datagram-loss.json`, each ratio with slice 6's value and the A/A range beside it, and `tables.py --check` confirms the README holds them verbatim. Run on slice 6's data, it reproduces slice 6's published tables.
  - *New, descriptive:* [dgloss.py](dgloss.py) reports DATAGRAM loss in every readiness DATAGRAM cell from the fixture's own counts (sent messages against distinct messages received, inside and outside the measured window), beside the relay's forward drops on S5 and S6 (campaign decision D8). It sets no flag. The frozen fixture does not read `DatagramReceiveDropsV1`; the side stage reports it.
- **Copied byte-identical from slice 6** (checked by `build.py`): `run.py`, `analyze.py`, `attribution.py`, `localize.py`, `localize_test.py`, `prereq.py`, `pack.py`, `rules.py`, `rules_test.py`, `stage_run.py`, `stages.py`, `matrix.py`, `follow.py`, `resume.py`, `resume_test.py`, `harness.py`, `harness_test.py`, `receiver.py`, `receiver_test.py`, `go.mod`, and the Go aids and overlays (`calibrate/`, `launch/`, `relay/`, `overlay/`, `timeline/`, `counting/`, `model-overlay/`). `matrix.py` and `follow.py` keep #739's docstrings, and `stages.py` slice 6's.
- **Binaries** (SHA-256; `build.py` asserts each):

| Binary | SHA-256 | Source |
| --- | --- | --- |
| `reno-fixture` | `cda8feb1b2fb004d…` | frozen Reno; byte-identical to slice 6's |
| `reno-diag-fixture` | `36752495934b2f91…` | frozen Reno with the `-diag` overlay; byte-identical to slice 6's |
| `relay-v3`, `relay-linux` | `033d467c36157a04…`, `24ee941d2c63c259…` | frozen tree; byte-identical to slice 6's |
| `calibrate`, `launch` | `4218b433f3d7d04c…`, `a6146d62e16339f2…` | frozen tree; byte-identical to slice 6's |
| `cand-fixture` | `e50b799c0cefa591…` | `d94b3ec2` (new) |
| `cand-diag-fixture` | `5947fc186427380e…` | `d94b3ec2` with the `-diag` overlay, for `presrss` only (new) |
| `cand-timeline-fixture` | `f1807744d5a9f9ee…` | `d94b3ec2` with the timeline overlay, for `s5timeline` only (new) |

Every overlay patch site occurs exactly once on `d94b3ec2` (`build.py` asserts it). A second build from a fresh artifact directory reproduced all three candidate binaries byte for byte.

- **Gates.** No transport source changes here. `d94b3ec2`'s production tree equals `7d05a887`'s, #844's merge, whose push CI on `main` passed after its pull request's hosted checks.
- **Dry run.** Before any observation of this record, the adapted analysis ran on slice 6's retained raw data (`raw.tar.gz` at `9b38f0ab`): `stages.py readiness` reproduced slice 6's `summary.json` byte for byte, and `stages.py preslat` and `s5timeline` reproduced slice 6's outcomes exactly.
- **Aid tests pass:** `rules_test.py` (43), `harness_test.py` (10), `receiver_test.py` (11), `localize_test.py` (14), `resume_test.py` (3), `portcontext_test.py` (8).

## Prerequisites

Slice 6's, unchanged ([Prerequisites](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#prerequisites)): ECN calibration, ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`), native I/O (`perfsmoke`: one send syscall and one transmit per measured-window forward packet), resources, instruments, and the timeline overlay (`xsmoke`), each excluded from every statistic, on a quiet host. A failure ends this record as a reported prerequisite gap. `matrix.py hostfacts` records the host settings at the start and end of every stage.

## Registration: readiness

Slice 6's stage, unchanged except the candidate ([Registration: readiness](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#registration-readiness)):

- **Arms:** Reno, A/A Reno, candidate (BBRv3) and Reno on candidate, on loopback and S5; Reno, A/A and candidate on S6.
- **Runs:** five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations.
- **Flags:** per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20. On S6 the useful benefit is repeatable when the candidate beats Reno in all five pairs.
- **Preservation check:** Reno on candidate is held to the same limits.
- **Contamination:** #712's test, #715's handling (a block with a contaminated or unusable observation is rerun once with the same seed; the rerun replaces it only if usable and uncontaminated; at most ten readiness block reruns), `summary.json` under that rule and `summary-original.json` under #712's.
- **Reporting:** every cell with its median, blocks crossing, A/A range, raised flag, absolute medians and slice 6's value beside it. Every raised cell stays raised, with absolute values; nothing is pre-cleared; an incomplete cell is reported as *incomplete (n of 5)*. Carried open costs: [#738](https://github.com/the-sarge/quic-go-fast/issues/738)'s S5 DATAGRAM sender CPU per useful GiB beside slice 6's 0.948; [#734](https://github.com/the-sarge/quic-go-fast/issues/734)'s wake increase through CPU per useful GiB on every path (the wake count is not measured).

## Registration: S5 timeline and receiver split

Slice 6's, unchanged ([Registration: S5 timeline and receiver split](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#registration-s5-timeline-and-receiver-split)): `s5timeline`, S5, both workloads, four blocks (seeds 9601–9604), #715's rule with the three-of-four majority, one escalation of blocks 5–6 for a workload with exactly two agreeing blocks, decided in the operator's session; the receiver CPU split on the readiness observations.

## Registration: preservation (conditional)

Slice 6's, unchanged ([Registration: preservation (conditional)](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md#registration-preservation-conditional)): `presrss` for each (path, workload) pair with a raised Reno-on-candidate RSS cell (at most two), `preslat` for a raised Reno-on-candidate loopback control p95 cell (at most one), the cap of 60 attribution observations, `follow.py`'s selection order, and the outcome rules. The readiness value stays as recorded whatever a stage finds, and **this record ends no cell's blocking state**: the advancement disposition decides.

## Registration: DATAGRAM loss (descriptive)

[dgloss.py](dgloss.py), after `stages.py all`, writes `datagram-loss.json`: per readiness DATAGRAM cell, the median and largest loss over its selected blocks, and on S5 and S6 how much of it the relay dropped. It is reported beside the readiness table and sets no flag (campaign decision D8).

## Stages not run

- Slice 6's list, unchanged: memory attribution, `recvharness`, a run-loop wake count, a `main`-without-port arm, macOS and Windows.
- **No arm that switches #743 off.** It was registered in the approved plan (D3) and dropped by the operator on October 18, 2026 (about 23:50 America/New_York), before any data, because the fixture never GSO-batches, so the switch could isolate nothing.
- **The GSO send path.** #742's and #743's BBR changes are not exercised by this fixture; the side stage measures #743 on BBRv3.

## Disclosures made before data

- **No counted, prerequisite, smoke or attribution observation of this record existed** when this registration was written.
- **Development runs on `minimax`** (excluded from every statistic, kept with the raw data), made on October 19, 2026, at about 03:48 UTC while a GridCast GARM Windows VM was running on the host:
  - (both on `e7eb3f10`'s binaries, before the re-pin; neither the managed lease nor the fixture changed since) loopback DATAGRAM, Reno on candidate (`cand`), 20 s, with `perf stat`: 4,166.4 Mbit/s, sender CPU 5.22 s per GiB, peak RSS 18.14 / 17.81 MiB, control p95 0.532 ms; 8,786,578 `sendmsg` calls;
  - the same with `QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS=true`: 4,252.5 Mbit/s, 5.12 s per GiB, 18.06 / 19.52 MiB, 0.413 ms; 8,964,248 `sendmsg` calls.
  These showed that #743 never engages in this fixture, which led to the operator's D3 amendment. Nothing else in this registration changed after them.
- **Slice 6's values are known** and are the context named above. Known in advance: slice 6 raised ten cells on the port (nine candidate cells and the Reno-on-candidate loopback DATAGRAM control p95), and slice 7 excepted them for `abfa5456` only.
- **The agent's expectation** (stated to the operator in the plan, not a result): readiness probably raises the nine memory and latency cells again, so advancing would again need explicit operator exceptions; a Reno-on-candidate loopback DATAGRAM cell may also cross its limit, with #824 a known contributor.
- **Interpretation known in advance:** loopback STREAM goodput moves about 3% with code placement (#738, #744); small RSS cells cannot be reconciled with `ru_maxrss` within a few hundred KiB (#740); the frozen-Reno A/A arm has raised the loopback DATAGRAM control p95 cell in three of five readiness records on this host.

## Time estimate

All aids exist and every binary is built. On a quiet host (slice 6's measured figures): prerequisites 5–15 minutes; readiness 110 observations about 85 minutes; quiet-host gating at least 15 minutes; `s5timeline` about 10 minutes; `preslat` about 30 minutes and `presrss` about 15 minutes per cell, if triggered; then the side stage, about 30 minutes. **About 2 h 15 min without conditional stages; up to about 3 h 20 min with all of them.** Foreign load extends the gates' waits; each readiness block rerun costs about 5–9 minutes.

## Order

**On the Mac:** `build.py` (done), the aid tests (done), the side stage's `build.sh` (done), then `sync.sh`.

**On `minimax`,** unattended under `taskset -c 1-3`, started by a systemd user timer that runs [driver.sh](driver.sh), with nothing else built or tested there while a stage measures:

1. The check before start: no `observations*`, `prereq`, `hostfacts-*`, `follow-decisions.json`, stage summary or `STAGE.DONE` may exist (`PRESTART-EMPTY`), since `run_case` silently reuses an existing clean observation. Then `RUNNING` is created; it is removed when the driver exits.
2. `prereq.py` (imported after `art`), `smoke`, `perfsmoke`, `ecnsmoke`, `xsmoke`, `stages.py fold`.
3. The readiness stages `loopback`, `s5`, `s6`, then `follow.py reruns main`.
4. `s5timeline`, then `follow.py reruns s5timeline`.
5. `follow.py conditional` (`presrss`, `preslat` and their reruns, as the flags require).
6. `stages.py fold`, `DRIVER-DONE`.
7. The side stage ([stage.sh](../2026-10-19-bbr-808-gso-receive/stage.sh)), then `STAGE.DONE`.

Slice 6's failure handling is unchanged: a failed observation does not stop the order (`resume.py` completes the stage and `resume.py mark` gives a receipt-less attempt a marker); each follow-up step is retried once; more than three failed attempts in one stage stop the order for the operator's session.

**Stays with the operator's session:** the prerequisite review (a failure ends the record as a prerequisite gap); the `s5timeline` escalation; then, anywhere, `stages.py prereq`, `stages.py all`, `portcontext.py`, `dgloss.py` and `tables.py`.

**Booking.** Before arming the timer: ListAgents for busy wiremux sessions, `pgrep -af nativerig` and the wiremux yield files; the GridCast GARM CI processes (`Runner.Worker`, `transfer.test`, the `infra-garm` Windows VM) idle for about a minute; then the yield handshake if a wiremux campaign holds the host. The quiet-host gate before every block still applies. Contamination triggers one same-seed rerun and never decides an outcome.

## Registration review

**First consideration:** RAS run `20261019T040140-c62fb98010fcd16a2fccdea8` (`ras consider`, kind plan, the operator's configured agents and adjudication), on `ad279037`'s registration: the four registration READMEs concatenated, with the adapted aids as context (L6 `matrix.py`, `analyze.py`, `build.py`, `driver.sh`; the side stage's `analyze.py`, `runner.py`, `stage.sh`; readiness `build.py`, `driver.sh`, `sync.sh`, `dgloss.py`, `portcontext.py`), and the governing constraints quoted in the prompt. Four of six reviewers completed (codex-astra, codex-sol, grok, agy-argon). The two Claude reviewers failed within seconds because the local Claude CLI had reached its session limit, a tool failure rather than a review result. The synthesis queued nine fixes; each was checked against the code and dispositioned here:

| Item | Disposition | Change |
| --- | --- | --- |
| C-003: the side stage checked controllers only in used rounds, so a mismatch in a replaced or excluded round vanished, and a mismatch in a used round still came with a K3 reading | fix-now | `analyze.py` checks both endpoints of every counted and rerun run before selection, keeping each one's identity; any mismatch makes `status` "incorrect" with no reading ([analyze_test.py](../2026-10-19-bbr-808-gso-receive/analyze_test.py): replaced, excluded, discarded-rerun and retained cases, plus a clean control) |
| C-002: the ECN reading covered only the observations selected for the classification and the traces, so a usable rerun candidate that lost ECN could be missed | fix-now | L6 `analyze.py` reads ECN from every usable BBRv3 observation in every attempt; a contaminated one is reported but does not decide (`ecn_decision`, tested) |
| C-008: the switch estimate treated more than 3 Not-ECT packets as a switch, but BBRv3 flows that keep ECN send 4 (all twenty slice 6 S5 and S6 runs), so it would have reported a false switch near 300 s and ECN as not on | fix-now | ECN stays on when every deciding flow's Not-ECT share is at most 0.01% and no trace records a failure; #793's timestamp estimate is computed only above that share, as the time of a switch (tests: 3 and 4 handshake packets drained and not drained, the share boundary, a switching flow, a trace failure at a low share). The L6 registration's #803 reading says so |
| C-006: a failed side stage still printed SIDE-DONE, wrote STAGE.DONE and exited 0 | fix-now | `STAGE.DONE` (the host is free) now says `complete`, `readiness complete; side stage failed` (the driver exits 1), or `stopped` on any earlier exit; checked with stub stages for each path, `RUNNING` removed in each |
| C-011: the shared sync would have deleted L6's `hostfacts/` snapshots and the side stage's yield request | fix-now | `sync.sh` excludes every host-written path, including both and the development-run directories; a second sync to a local target holding each kept all of them |
| C-007: the "application-limited" share counted flow-control and ProbeRTT marks too | fix-now | the application-only share (`checks_by_reason['application'] / checks`) is reported beside the aggregate supply-limited share, for this record and for #792's figures (tests for each reason, mixed reasons and no checks) |
| C-001: `rerun.jsonl` existed only if a round was rerun, so the documented analysis command could fail | fix-now | `stage.sh` creates it empty before any rerun; a missing file stays an error (tested) |
| C-004: with every side-stage round excluded, the analysis crashed | fix-now | `status` "incomplete", the exclusion notes, and no reading (tested) |
| C-010: L6 `build.py` did not byte-check the copied Python aids and checker fixtures its README lists | fix-now | `COPIED_PY` checks all ten, `mechanism792.py` against #792's `analyze.py`; all pass |

The synthesis's items not to act on were declined, in agreement with it: a supposed unavoidable conflict between the readiness development runs and the clean-start check (they live in `dev/`, which the check does not read); the universal "always" form of C-008's claim; a ban on a terminal marker; and reopening settled decisions.

After the fixes: every aid test passes (readiness: `rules_test` 43, `harness_test` 10, `receiver_test` 11, `localize_test` 14, `resume_test` 3, `portcontext_test` 8; side stage: `analyze_test` 8; L6: `l6rules_test` 28, `draft06check_test` 40, `reconcile_test` 13, `runchanges_test` 2, `analyze_test` 9); both builds pass their identity assertions; and the dry runs reproduce slice 6's `summary.json` byte for byte and #777's cells, period ratios and focal flags exactly, with #792's trace figures and #793's switch times.

**Replacement consideration:** RAS run `20261019T051454-f4cc7f489a97fbff5cd2b3dd` (`ras consider`, the same agents and adjudication), on `43e43cbc`'s registration with the first round's dispositions quoted as settled. All six reviewers completed and produced 14 findings in 14 clusters. Five of six adjudicators finished. The operator chose not to wait for the sixth (agy-argon) and its synthesis, so each raw finding was checked against the code and dispositioned from the findings and the five adjudications:

| Cluster | Disposition | Change |
| --- | --- | --- |
| C-005: a trace-only rerun used #777's rerun cap, so traces in pairs 1–3 could leave a dirty triple in pair 4 or 5 unrerun and change the classification | fix-now | L6 `matrix.follow` runs #777's triple pass first, unchanged, then trace-only reruns with only the slots left (within #777's cap of four); each rerun records its trigger. `choose_triple` keeps a clean original triple whatever its rerun shows, so a trace rerun never changes the classified data (tests with the cap reached by triples first and by traces) |
| C-001, C-013, C-010: readiness's `STAGE.DONE` ("host free") stayed in place through L6, L6's `RUNNING` existed only during observations, and a signal left the markers wrong | fix-now | the L6 driver moves readiness's `STAGE.DONE` into its artifacts, holds `RUNNING` for the whole order (an observation leaves a held marker in place), and on any exit, signals included, writes `L6.DONE` and `STAGE.DONE` with how the order ended and removes `RUNNING`; checked with stubs for completion, a failed step and a signal. The readiness driver also exits through its trap on a signal |
| C-009: the ECN rule let a contaminated but usable observation that stopped marking be ignored | fix-now | one-way rule: any usable observation that stopped marking, contaminated or not, means ECN does not stay on, and at least one uncontaminated observation is needed to show it does. This restores the accepted D7 wording ("in any usable L6 observation"). The first round's narrowing was the agent's disposition, not an operator decision; two adjudicators judged reopening it out of scope, two agreed, one asked for clarity |
| C-003, C-012, C-014, C-004: the side stage could accept a partial round, or a stopped order, as complete, and a failed rerun listing passed silently | fix-now | a round counts only with all eight arms, `counted.DONE` must exist, and `stage.sh` stops if listing the contaminated rounds fails (tests for a partial original, a partial rerun and an unfinished order) |
| C-002: with no clean trace, the checker and clamp figures read as passing | fix-now | per-workload trace counts; absent figures are reported as absent, never as zero or passing |
| C-006: the readiness tables lacked the A/A range beside each ratio | fix-now | `tables.py` puts the A/A per-block range in every ratio cell; it is listed in the recorded changes and the Order |
| C-007: the STREAM diagnostic's contamination flag omitted the `ksoftirqd` term of the registered measure | fix-now | `perfdiag.py` subtracts it, as `runner.py` does |
| C-011: the new trace-rerun, completion and trace-selection paths had no test | fix-now | tests for both rerun passes, `decided`, `pick_trace` and `ecn_failed_at` on a checker fixture trace |

No further review round is run (campaign decision D10). After these changes every aid test passes (readiness as above; side stage `analyze_test` 11; L6 `analyze_test` 14 with the others unchanged), the L6 aid identity checks pass, and the L6 dry run again reproduces #777's cells, period ratios and focal flags exactly.
