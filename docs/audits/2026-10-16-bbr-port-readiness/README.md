# Readiness of the merged BBRv3 production port on Linux

**Date:** October 16, 2026, America/New_York. **Scope:** slice 6 of [Port the corrected BBRv3 sender (r9) into main's experimental opt-in sender](https://github.com/the-sarge/quic-go-fast/issues/748), under its normative [port contract](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/agents/bbr-production-port.md#after-pr-5-readiness-and-advancement-disposition), step 1 of "After PR 5: readiness and advancement disposition". Measurement only: no code change, qualification, advancement decision, default-controller change, release, paid resource, host-setting change or ledger change.

Branch `codex/bbr-port-readiness`, based on `abfa5456`, the candidate itself. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) record, this record stays off `main`. The [#739 re-demonstration of r9](https://github.com/the-sarge/quic-go-fast/blob/138223ac/docs/audits/2026-10-08-bbr-r9-linux-redemonstration/README.md) (`138223ac`, on `codex/bbr-r9-linux-redemonstration`) supplies the readiness stage, the S5 timeline, the preservation stages, the fixture, the host layout and the aids that this record reruns.

**Status: complete.** The sections from [The candidate](#the-candidate) to [Order](#order) are the registration. They were written, with the aids adapted and the builds made, before any prerequisite, smoke, readiness or attribution observation of this record, including the changes from its one review (`ras consider`). They were committed as `0d142c9e` and `893aedc2`, and are unchanged. The [Answer](#answer), results, [Deviations](#deviations) and later sections were added afterwards.

## Question

On owned Linux hardware, what are the readiness flags of the merged port head (`main` at `abfa5456`), what does the S5 timeline show, and does any raised Reno-on-candidate preservation cell reproduce under its registered stage? Beside each cell: what did r9 (`81e9dc8c`) record in #739?

## Answer

**On `minimax`, the merged port head raises ten cells. Nine are candidate cells: the eight S5 and S6 memory and control p95 cells r9 also raised, and loopback STREAM control p95. One is a Reno-on-candidate preservation cell, loopback DATAGRAM control p95, whose conditional stage is *inconclusive*. Every goodput and CPU cell passes, and the S6 benefit holds.** Nothing here is cleared: every raised cell goes to slice 7's operator disposition. Under the contract, the raised preservation flag blocks advancement.

- **Useful benefit holds.** On S6 the candidate's goodput is 14.28× Reno's (STREAM) and 14.42× (DATAGRAM), beating Reno in five of five pairs each. r9 recorded 14.53× and 14.74×.
- **Every goodput and CPU cell passes, on every path, as on r9.**
  - [#738](https://github.com/the-sarge/quic-go-fast/issues/738)'s open cost: S5 DATAGRAM sender CPU per useful GiB is **0.948** of frozen Reno's. r9 recorded 0.953, r8 0.909.
  - [#734](https://github.com/the-sarge/quic-go-fast/issues/734)'s wake increase, read through CPU per useful GiB (the operator's choice): on loopback and S5, sender CPU is 0.927–1.028 and receiver CPU 0.938–1.029 of frozen Reno's, all under 1.10. The wake count itself was not measured.
- **The eight S5 and S6 cells r9 raised stay raised, at about r9's magnitudes.**
  - S5: STREAM sender RSS 1.217 and receiver RSS 1.482; DATAGRAM sender RSS 1.157.
  - S6: STREAM sender RSS 1.656 and receiver RSS 1.832; DATAGRAM sender RSS 1.466; control p95 1.581 (STREAM) and 1.461 (DATAGRAM).
- **Loopback STREAM control p95 is raised at 1.656**, crossing in four of five blocks. That is 0.213 ms against frozen Reno's 0.145 ms; r9 recorded 1.485.
- **Two cells r9 raised are not raised here: loopback receiver RSS.** It is 1.095 (STREAM) and 1.098 (DATAGRAM), against r9's 1.120 and 1.148. Each median is under 1.10, but two of five blocks cross the limit in each. This is the rule's outcome on different runs from r9's, not a measured improvement.
- **One preservation cell is raised: Reno on candidate, loopback DATAGRAM control p95, at 1.504**, crossing in three of five blocks. r9 recorded 0.744.
  - The frozen-Reno A/A arm crosses the same cell at 1.519 (per-block 0.69–5.51). That is context only and never overrides the rule.
  - `preslat` ran with six clean blocks. Its outcome is ***inconclusive***: pooled p95 ratio 0.913, with a 95% block-bootstrap interval of 0.611–1.494, whose upper end is above the 1.20 limit.
  - **The cell stays a raised, blocking preservation flag.** It is not attributed to the port or to `main`'s other changes.
- **S5 timing stays model-bound.** The S5 timeline reads **model behaviour** in both workloads, in four of four blocks each. Timing is 0.0001–0.0003 of capacity, as on r9.

## Readiness results

Plain builds, five blocks per path and workload: 110 counted observations, all exiting cleanly and passing receiver integrity. **No observation was contaminated or unusable**, so no block was rerun, and `summary.json` equals `summary-original.json` in every flag. The failure handling added after the registration review never engaged.

How to read the table:

- Each ratio is the median of per-block ratios against the same block's frozen Reno.
- **Bold** marks a raised flag, and parentheses give the number of blocks crossing the limit.
- Brackets give r9's value from #739, bold where r9 raised it. It is descriptive context from runs made on October 8.
- Every cell has all five blocks.
- A/A per-block ranges are beside every ratio in [summary.json](summary.json) and [r9-context.json](r9-context.json).

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | 1.160 [1.148] | 1.014 [1.007] | 0.938 [0.937] | 0.974 [0.954] | 1.095 (2) [**1.120**] | **1.656 (4)** [**1.485**] |
| Loopback STREAM | Reno on candidate | 0.998 [1.003] | 1.003 [1.000] | 1.003 [1.000] | 0.998 [1.006] | 1.002 [0.998] | 0.880 [0.930] |
| Loopback DATAGRAM | Candidate | 0.995 [1.001] | 1.028 [1.022] | 0.968 [0.964] | 0.949 [0.956] | 1.098 (2) [**1.148**] | 0.695 (2) [0.868] |
| Loopback DATAGRAM | Reno on candidate | 0.999 [0.999] | 1.002 [1.000] | 0.998 [1.002] | 0.996 [0.998] | 0.987 [1.044] | **1.504 (3)** [0.744] |
| S5 STREAM | Candidate | 0.972 [0.967] | 0.927 [0.937] | 1.029 [1.040] | **1.217 (5)** [**1.235**] | **1.482 (5)** [**1.518**] | 1.096 [1.061] |
| S5 STREAM | Reno on candidate | 1.000 [1.000] | 1.002 [1.003] | 0.997 [0.991] | 0.992 [0.999] | 0.974 [0.988] | 0.997 [0.993] |
| S5 DATAGRAM | Candidate | 0.972 [0.972] | 0.948 [0.953] | 0.992 [0.988] | **1.157 (5)** [**1.159**] | 1.072 [1.070] | 1.037 [1.021] |
| S5 DATAGRAM | Reno on candidate | 1.000 [1.000] | 0.995 [0.993] | 0.999 [0.999] | 1.006 [1.023] | 1.008 [0.990] | 1.005 [1.003] |
| S6 STREAM | Candidate | benefit 14.28× [12.32–15.61], 5/5 [14.53×, 5/5] | 0.089 [0.086] | 0.073 [0.073] | **1.656 (5)** [**1.673**] | **1.832 (5)** [**1.898**] | **1.581 (5)** [**1.562**] |
| S6 DATAGRAM | Candidate | benefit 14.42× [12.21–16.26], 5/5 [14.74×, 5/5] | 0.088 [0.090] | 0.076 [0.078] | **1.466 (5)** [**1.542**] | 1.090 (2) [1.095] | **1.461 (5)** [**1.459**] |

The A/A frozen-Reno arm crosses one limit: loopback DATAGRAM control p95, 1.519 (three blocks; per-block range 0.69–5.51). This is the cell its arm crossed in #739 (1.599) and #715 (1.615). Every other A/A cell passes.

Absolute medians, with r9's in brackets (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Receiver heap peak (measured) | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4692.8 [4695.2] | 3.98 / 2.75 [3.98 / 2.75] | 16.77 / 15.27 [16.27 / 14.48] | 3.22 [3.27] | 0.145 / 0.074 [0.134 / 0.070] | — [—] |
| Loopback STREAM | Candidate | 5459.6 [5390.4] | 4.04 / 2.58 [4.01 / 2.57] | 16.37 / 16.79 [15.52 / 16.20] | 4.33 [4.46] | 0.213 / 0.138 [0.199 / 0.121] | — [—] |
| Loopback STREAM | Reno on candidate | 4679.0 [4705.6] | 3.99 / 2.76 [3.98 / 2.75] | 16.75 / 15.31 [16.20 / 14.46] | 3.29 [3.17] | 0.124 / 0.071 [0.112 / 0.071] | — [—] |
| Loopback DATAGRAM | Reno | 4661.9 [4655.0] | 4.54 / 3.02 [4.55 / 3.02] | 18.21 / 20.05 [17.61 / 18.72] | 4.60 [4.49] | 0.422 / 0.082 [0.314 / 0.099] | — [—] |
| Loopback DATAGRAM | Candidate | 4637.9 [4654.5] | 4.67 / 2.92 [4.66 / 2.92] | 17.73 / 21.53 [16.83 / 21.72] | 6.86 [6.94] | 0.304 / 0.172 [0.253 / 0.174] | — [—] |
| Loopback DATAGRAM | Reno on candidate | 4658.7 [4647.3] | 4.55 / 3.02 [4.55 / 3.02] | 18.33 / 19.68 [17.69 / 19.42] | 4.83 [4.43] | 0.364 / 0.094 [0.604 / 0.092] | — [—] |
| S5 STREAM | Reno | 95.9 [95.9] | 10.40 / 8.80 [10.39 / 8.79] | 18.80 / 17.88 [17.97 / 16.94] | 3.42 [3.38] | 182.7 / 176.0 [187.2 / 181.4] | 0.094% [0.113%] |
| S5 STREAM | Candidate | 93.2 [92.7] | 9.64 / 9.03 [9.74 / 9.12] | 22.73 / 26.50 [22.18 / 25.71] | 14.36 [14.62] | 198.9 / 102.1 [199.6 / 102.3] | 0.217% [0.214%] |
| S5 STREAM | Reno on candidate | 95.9 [95.9] | 10.42 / 8.71 [10.42 / 8.71] | 18.64 / 17.42 [17.98 / 16.68] | 3.40 [3.43] | 182.6 / 176.6 [184.8 / 178.5] | 0.093% [0.102%] |
| S5 DATAGRAM | Reno | 94.4 [94.4] | 11.54 / 8.93 [11.47 / 8.95] | 18.63 / 14.88 [17.88 / 14.23] | 3.28 [3.26] | 177.5 / 170.7 [179.6 / 172.1] | 0.398% [0.403%] |
| S5 DATAGRAM | Candidate | 91.8 [91.8] | 10.92 / 8.86 [10.89 / 8.86] | 21.70 / 15.94 [20.86 / 15.14] | 4.18 [4.23] | 187.3 / 102.1 [183.7 / 102.3] | 0.025% [0.027%] |
| S5 DATAGRAM | Reno on candidate | 94.4 [94.4] | 11.47 / 8.91 [11.44 / 8.94] | 18.98 / 14.99 [18.51 / 14.12] | 3.31 [3.33] | 178.6 / 172.3 [179.2 / 172.8] | 0.406% [0.413%] |
| S6 STREAM | Reno | 6.1 [6.1] | 109.87 / 121.18 [114.12 / 123.25] | 13.88 / 14.63 [13.25 / 13.50] | 3.47 [3.55] | 125.5 / 105.6 [127.7 / 105.5] | 0.000% [0.000%] |
| S6 STREAM | Candidate | 87.9 [88.3] | 9.80 / 8.87 [9.81 / 8.99] | 22.98 / 26.89 [22.18 / 26.34] | 14.98 [14.38] | 199.3 / 102.1 [199.5 / 102.1] | 0.239% [0.250%] |
| S6 DATAGRAM | Reno | 5.9 [5.9] | 122.97 / 121.07 [119.42 / 118.89] | 14.63 / 14.59 [13.78 / 13.88] | 3.34 [3.35] | 126.6 / 104.5 [125.8 / 104.8] | 0.000% [0.000%] |
| S6 DATAGRAM | Candidate | 85.7 [86.2] | 10.85 / 9.29 [10.79 / 9.41] | 21.75 / 15.85 [21.24 / 15.19] | 4.20 [4.18] | 184.6 / 102.0 [187.3 / 102.0] | 0.029% [0.047%] |

**Loopback latency, per block.** These are sub-millisecond quantities at 20 replies per run:

- *Candidate, loopback STREAM:* per-block ratios 0.86–1.80. The candidate's p95 is 0.195–0.260 ms in every block, against frozen Reno's 0.118–0.227 ms.
- *Reno on candidate, loopback DATAGRAM:* per-block ratios 0.58–2.49. The arm's median p95 (0.364 ms) is below frozen Reno's (0.422 ms). The rule's per-block median is 1.504 because frozen Reno's own per-block p95 ranges 0.170–0.630 ms.

None of this changes either flag.

## Carried open costs and preservation

- **#738's S5 DATAGRAM sender CPU per useful GiB:** 0.948 of frozen Reno's, passing, with no block crossing. r9 recorded 0.953 in #739, and r8 0.909 in #736. Those are different runs, so they are descriptive context.
- **#734's run-loop wake increase:** its guarded cost, CPU per useful GiB, passes on every path:
  - sender 0.927–1.028 on loopback and S5, and 0.088–0.089 on S6;
  - receiver 0.938–1.029 on loopback and S5, and 0.073–0.076 on S6.
  The wake count was not measured.
- **Reno on candidate.** One cell is raised: loopback DATAGRAM control p95, 1.504.
  - Every other Reno-on-candidate cell lies within 0.974–1.008 on goodput, CPU and RSS, and within 0.880–1.005 on control p95. r9's were 0.988–1.044 and 0.744–1.003.
  - `presrss` did not trigger, because no Reno-on-candidate RSS cell is raised. No Reno-on-candidate cell was left out by a limit or by the cap.

| Cell | Readiness | `preslat` (registered) | Candidate ÷ Reno on candidate (descriptive) | r9 (#739) |
| --- | --- | --- | --- | --- |
| Loopback DATAGRAM control p95, Reno on candidate | **1.504 (3)**; A/A 0.69–5.51 | ***inconclusive***: pooled p95 0.475 ms ÷ 0.520 ms = 0.913; 95% interval 0.611–1.494; per block 0.49–1.84; 6 of 6 blocks clean, 120 s window | 0.837 (0.28–1.64) over five blocks | 0.744 (2), not raised |

**Reading the `preslat` outcome:**

- The stage was adequate: six usable blocks, at least 100 replies per run, and at least 500 per arm.
- Its point estimate is under 1. But its interval reaches above the 1.20 limit, so the rule says *inconclusive*, not *not reproduced*.
- Under the registration, no outcome would have ended the cell's blocking state. Under #739's D3 mapping, only an adequate *not reproduced* outcome would have done so, and this is not that outcome.
- No arm here measures `main` without the port, so the cell is not attributed.

## S5 timeline: model behaviour

The stage ran straight after readiness (`s5timeline`, 8 observations, all clean, none rerun). There was no escalation: no workload had exactly two of four agreeing blocks. The figures are per block, as fractions of the bottleneck's capacity over the measured window, from the [timeline overlay](timeline/), in [attribution.json](attribution.json) `s5timeline` and [r9-context.json](r9-context.json).

| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | 0.023–0.037 | 0.025–0.038 | 0.014–0.024 | 0.0002–0.0003 | 0.010–0.015 | model ×4 → **model** |
| DATAGRAM | 0.022–0.035 | 0.020–0.034 | 0.012–0.024 | 0.0001–0.0002 | 0.007–0.010 | model ×4 → **model** |
| r9 STREAM (#739) | 0.028–0.034 | 0.028–0.038 | 0.017–0.025 | 0.0002–0.0003 | 0.011–0.014 | model ×4 → model |
| r9 DATAGRAM (#739) | 0.020–0.030 | 0.020–0.030 | 0.010–0.020 | 0.0001–0.0002 | 0.009–0.010 | model ×3, no deficit ×1 → model |

- **Stage verdict: model behaviour**, in both workloads, as on r9.
- **Lateness.** Late arrivals at a full-quantum deadline per 30 s run: 27,264–27,498 (STREAM) and 28,505–28,946 (DATAGRAM). They total 0.215–0.227 s per run, with maxima of 0.18–1.61 ms. r9 recorded 27,259–29,008 events and 0.217–0.226 s.
- **Phase shares** are r9's within a few points. Cruise is 0.54–0.62 (STREAM) and 0.59–0.67 (DATAGRAM); Down 0.16–0.24; Up 0.13–0.17 (STREAM) and 0.09–0.11 (DATAGRAM); ProbeRTT 0.02–0.05.
- **Connection-goroutine states.** The connection goroutine is pacer-bound for 0.92–0.93 (STREAM) and 0.95–0.97 (DATAGRAM) of the window, as on r9.
- **Instrument perturbation is negligible.** Instrumented goodput is 1.002 (STREAM) and 1.001 (DATAGRAM) of the readiness candidate's.

**Receiver CPU split.** No S5 receiver CPU cell is raised, so the registered split classifies nothing. [attribution.json](attribution.json) `cpu_split` holds every cell's descriptive figures.

## Flag report for slice 7

For slice 7 of [#748](https://github.com/the-sarge/quic-go-fast/issues/748), the operator's advancement disposition: candidate `main` at `abfa5456`, Linux (`minimax`). Every cell is presented with its absolute values above. Nothing is pre-cleared, and no r9 readiness exception covers any cell.

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 14.28× (STREAM) and 14.42× (DATAGRAM) Reno goodput, 5/5 pairs each, at 0.089 / 0.088× Reno's sender CPU per useful GiB. |
| **Not raised** | Every goodput and CPU cell on every path. Loopback sender RSS (both), loopback receiver RSS (both; medians 1.095 and 1.098, two blocks crossing each), loopback DATAGRAM control p95, S5 control p95 (both), S5 DATAGRAM receiver RSS, S6 DATAGRAM receiver RSS. Every Reno-on-candidate cell but one. |
| **Raised, candidate (9)** | S5 STREAM sender RSS 1.217 and receiver 1.482; S5 DATAGRAM sender 1.157; S6 STREAM sender 1.656 and receiver 1.832; S6 DATAGRAM sender 1.466; S6 control p95 1.581 (STREAM) and 1.461 (DATAGRAM); loopback STREAM control p95 1.656. |
| **Raised, preservation (1)** | Reno on candidate, loopback DATAGRAM control p95, 1.504. `preslat`: *inconclusive* (0.913; 0.611–1.494). Not attributed. **Blocks advancement** under the contract. |
| **Not measured** | The run-loop wake count; the candidate on macOS and Windows; the port's share of any Reno-on-candidate difference (no `main`-without-port arm). |

## Deviations

None. No rule, threshold, seed, arm, cap or stage changed after data. The registration (`0d142c9e`) and its review changes (`893aedc2`) were committed before the first observation.

**Disclosed operational facts.**

- The pre-start check passed (`PRESTART-EMPTY`, 09:30:00 UTC).
- The prerequisite review ran on `minimax` at about 09:36 UTC, during the first loopback readiness block, under `nice -n 19 taskset -c 1-3` (runner cores), as in #739.
- Two watchers polled `minimax` over ssh: every 60 s until the prerequisites finished, then every 120 s.
- After `DRIVER-DONE` (11:41:26 UTC), with no stage running, `stages.py fold`, `readiness` and `s5timeline` ran on `minimax` under `taskset -c 1-3` to read the timeline verdicts for the escalation decision. The observations were then copied to the Mac, where `stages.py prereq`, `stages.py all` and `r9context.py` ran.
- The registration's Order omitted `stages.py prereq` after `stages.py all`, whose step list does not include it. It was run as #739's reconstruction note says, and changes no figure.
- The contamination test found no foreign load in any counted observation. All 25 quiet-host gates passed on their first 60 s window.
- `resume.py mark` ran three times and marked nothing. No stage stopped, and no `follow.py` step failed.

**Timing.** Prerequisites took 5 minutes, readiness 85 minutes (09:35–10:59 UTC), `s5timeline` 10 minutes and `preslat` 32 minutes: 2 h 11 min in total. The registered estimate was about 1 h 50 min without conditional stages and up to about 2 h 50 min with all of them.

## Preservation

- **Payload integrity.** All 137 retained observations exited cleanly and passed receiver integrity: 110 readiness, 8 timeline, 12 `preslat`, and 7 excluded prerequisite and smoke runs.
- **Default Reno.** One Reno-on-candidate cell is raised (above). The others lie within 0.974–1.008 on goodput, CPU and RSS.
- **No code change.** No transport source changed. No recorded translation, cap, evidence contract, controller version or default changed. The overlays exist only in their measurement builds.
- **No host change.** `perf_event_paranoid`, socket-buffer limits, offloads, the governor, EPP, boost and idle states were left unchanged. All 12 `hostfacts` snapshots match the first (powersave, EPP performance, boost on, every idle state enabled).

## Limits

- **Platform and topology.** One Linux host, with loopback endpoints and a userspace relay. This certifies no other platform, no real carrier path and no production readiness.
- **Repetition.** Five blocks per readiness cell, four timeline blocks per workload, six `preslat` blocks.
- **Loopback latency.**
  - Loopback control p95 is a sub-millisecond quantity at 20 replies per run. The frozen-Reno A/A arm alone ranges 0.69–5.51 per block on loopback DATAGRAM, and 0.43–1.05 on loopback STREAM.
  - Nothing here explains the raised loopback STREAM cell or the raised Reno-on-candidate loopback DATAGRAM cell.
- **Comparison with r9.** r9's values come from runs on a different day, and the candidate's root code and module versions differ from r9's. A difference from r9 is descriptive and is not attributed.
- **Engagement.** No queue CE-marked any packet, so the CE response stays unengaged, as on r9.

## Assets and reconstruction

- **Results:**
  - [summary.json](summary.json): registered readiness.
  - [summary-original.json](summary-original.json): #712's rule; identical flags.
  - [attribution.json](attribution.json): prerequisites, readiness blocks, CPU split, `s5timeline`, `preslat`, and `presrss` as not triggered.
  - [r9-context.json](r9-context.json): r9's values beside every cell, the S5 timeline beside r9's, and candidate ÷ Reno on candidate for the raised preservation cell.
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json) holds every observation, prerequisite, `driver.log`, `follow-decisions.json`, `resume-marks.json`, `hostfacts` snapshot and build receipt. Binaries, credentials, exported source trees and root-owned perf data are omitted.
- **To reconstruct,** in a fresh owned worktree of this branch:
  1. Run `build.py`, then `sync.sh` to a Linux host with the same CPU layout.
  2. Run [driver.sh](driver.sh) there, or its steps by hand under `taskset -c 1-3`.
  3. Run `stages.py fold` there.
  4. Run `stages.py prereq`, `stages.py all` and `r9context.py` anywhere.

## The candidate

**`main` at `abfa5456d99a9299825d04454a2e7ed12dff3326`**, chosen by the operator on October 16, 2026. It is the merged port: slices 1–5 ([#749](https://github.com/the-sarge/quic-go-fast/pull/749), [#754](https://github.com/the-sarge/quic-go-fast/pull/754), [#757](https://github.com/the-sarge/quic-go-fast/pull/757), [#763](https://github.com/the-sarge/quic-go-fast/pull/763), [#766](https://github.com/the-sarge/quic-go-fast/pull/766)), plus [#752](https://github.com/the-sarge/quic-go-fast/issues/752)'s discovery-scratch release ([#770](https://github.com/the-sarge/quic-go-fast/pull/770), `6a587c12`; declared deviation 4). Beyond the journal commit `3821a61c` named in the slice brief, it adds only [#772](https://github.com/the-sarge/quic-go-fast/pull/772) (when closing a socket the transport created fails, `Transport.Close` still waits for the receive loop), [#774](https://github.com/the-sarge/quic-go-fast/pull/774) (an integration-test change) and their journal entries (#773, #775). Later `main` commits are not measured.

What the candidate's code is, relative to r9 and to frozen Reno (`e4f322cb`), checked with `git diff` before writing:

- **BBR packages.** `internal/congestion` and `internal/ackhandler` differ from r9 only by declared deviations, all but one in test files: deviation 2's De Morgan rewrite in `bbr_ecn_equivalence_test.go`; deviation 3's race-mode test budget; and deviation 4. Deviation 4 is the one production change: `internal/ackhandler/congestion_dispatch.go`, 4 lines added and 1 removed, plus its regression test.
- **Root package, against r9.** 20 non-test root files differ: the three declared merge seams (`connection.go`, `packet_emission.go`, `send_queue.go`), and 17 files that `main` changed outside r9's lineage (`candidate_authority.go`, `setup_reservation.go`, `receive_phase.go`, `packet_policy.go`, `frame_sorter.go`, `receive_stream.go`, `stream.go`, `streams_map.go`, `datagram_queue.go`, `transport.go`, `server.go`, `client.go`, `closed_conn.go`, `external_packet_io.go`, `mtu_discoverer.go`, `send_conn.go`, `send_conn_sendmsg_x_darwin.go`). Every root file `main` has not changed since `e4f322cb` equals r9's.
- **Dependencies.** `main`'s `go.mod` requires newer `golang.org/x` modules than frozen Reno's. The fixture module's `replace` points at the exported tree, so the candidate fixture links `x/crypto` v0.58.0, `x/net` v0.61.0 and `x/sys` v0.49.0, where frozen Reno's links v0.57.0, v0.59.0 and v0.48.0. Each build receipt records `go version -m`.

The port is a new revision. **No readiness exception granted to r9 (decisions D2 and D3 of the [r9 advancement decision](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md)) covers it.**

## Baselines

- **Readiness baseline: frozen Reno, `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`.** Every ratio is the arm's value over the same block's frozen Reno value. The frozen Reno fixture is rebuilt byte-identical to #739's (`cda8feb1…`), so this record's ratios use the same denominator binary as r9's.
- **A/A: frozen Reno run twice per block.** Its per-block ratios are reported beside every ratio and never override the rule (#712's reporting, unchanged).
- **r9's recorded values, as context only.** Beside every cell, this record reports #739's value for r9 on the same cell (median, raised flag, blocks crossing), its absolute medians and its S5 timeline. They are taken mechanically from `138223ac`'s `summary.json` and `attribution.json` by [r9context.py](r9context.py). r9's values come from different runs on a different day (October 8). The comparison is descriptive: no rule compares this record with r9, and a difference from r9 neither raises nor clears a cell.
- **Not a baseline: Reno on candidate as a descriptive comparator, only for a raised Reno-on-candidate cell:** candidate ÷ Reno on candidate (below). It has no limit and no A/A range. It attributes neither cell and re-baselines no candidate cell.

## How a Reno-on-candidate difference is reported

The Reno-on-candidate arm runs the candidate fixture with Reno selected (the default). Against frozen Reno, it measures **`main`'s Reno at `abfa5456` against frozen Reno**. That difference bundles at least three things this stage cannot separate:

1. the port's Reno-reachable merge seams, which the contract requires to keep Reno's values (an eight-entry queue, no pacing kick);
2. every non-BBR change `main` carries since `e4f322cb`: the 17 root files and the module versions listed above; and
3. any non-seam effect of the port on Reno, such as code placement in a different binary. Loopback STREAM is sensitive to placement at about 3% (#738, #744).

So:

- **Rule, unchanged.** Reno on candidate is held to the readiness limits against frozen Reno, as #712 registered it: goodput 0.95, CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20, with the median of five per-block ratios.
- **A raised Reno-on-candidate cell stays raised.** It is reported with its value, blocks crossing, A/A range and absolute medians, as a **raised preservation flag, not attributed to the port or to `main`'s other changes**. The contract states: "A raised preservation flag blocks advancement." Presenting the cell for a decision under the unchanged criteria is slice 7's job, not this record's. This record never labels such a cell noise, explained or excepted.
- **Conditional stages as registered.** `presrss` and `preslat` run exactly as #739 registered them (below). They test whether a cell reproduces, not what causes it. Their outcome is reported beside the readiness value, which stays as recorded. #739 applied the [D3 mapping](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d3--the-reno-on-candidate-loopback-latency-cell-resolved-for-r8-on-linux-by-accepted-operator-interpretation), under which an adequate *not reproduced* `preslat` outcome ended the latency cell's blocking state for the named revision. Whether that mapping applies to the ported revision is left to slice 7, so **this record ends no cell's blocking state**.
- **Candidate ÷ Reno on candidate (descriptive), only for a raised Reno-on-candidate cell.** [r9context.py](r9context.py) reports the median (minimum, maximum) over blocks of candidate ÷ Reno on candidate for the same measure. Both arms are ratios against the same block's frozen Reno, so frozen Reno cancels, and the figure compares BBR with Reno on the candidate's own root code. It uses the readiness observations only. It sets no flag, has no limit or A/A range, attributes neither cell, and does not re-baseline the candidate's own cell.
- No arm measures `main` without the port, so the port's share of a Reno-on-candidate difference is **not examined** here.

The candidate's own ratios (BBRv3 on the candidate against frozen Reno) are read the same way: they measure `main`'s BBR, on `main`'s root code, against frozen Reno, as #739's measured r9's BBR on r9's root code against frozen Reno.

## What stays fixed and what changes

- **Fixed (port contract, step 1):** #739's readiness stage, unchanged except for the candidate revision: the same arms, paths, durations, seeds, rotation, flag limits, median rule, A/A reporting, contamination rule (the operator's #714 rerun rule, as #715 adopted it, with the cap of ten readiness block reruns), quiet-host gate (#738's `wait_quiet`, before every counted block) and core layout. Also fixed: frozen Reno; the fixture with the causal-diagnosis heap patch; the relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter); the launcher; the endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz); `minimax`'s core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings; `GOTOOLCHAIN=go1.27.0` for every build (the toolchain is part of the fixed fixture; `main`'s CI pin, Go 1.27.2, is not used here); #715's timeline rule; and #739's preservation stages and outcome rules, except that D3's mapping is not applied (below).
- **Changed, all recorded:**
  - *Candidate:* `cand` is `abfa5456`, the merged port head.
  - *D3's mapping is not applied.* Under it, #739 let an adequate *not reproduced* `preslat` outcome end the latency cell's blocking state for r9 on Linux. Here, no preservation outcome ends a blocking state, because the port contract says no readiness exception granted to r9 carries over. Slice 7 decides.
  - *Descriptive context.* r9's #739 values replace #736's r8 values as the context beside each cell, and #738's prior r9 figures. For a raised Reno-on-candidate CPU or goodput cell, r9's Reno-on-candidate value from #739 replaces #738's Reno-on-r9 cycles as context.
  - *Builds:* [build.py](build.py) is #739's with five recorded changes (its docstring): the candidate; this record's artifact directory (`.local/bbr-port-readiness`); #739's expected hash for r9's `cand` fixture dropped; a byte-identity check of every copied aid against #739's record; and `go version -m` in each build receipt. It asserts that every binary rebuilt from a revision and overlays a prior record already built is **byte-identical** to it. Frozen Reno (`cda8feb1…`), `reno-diag` (`36752495…`), both relays (`033d467c…`, `24ee941d…`), the calibration aid (`4218b433…`) and the launcher (`a6146d62…`) match #739's binaries. The candidate variants are new: `cand` (`d3f05ad6…`), `cand-diag` (`f53f2714…`, for `presrss` only) and `cand-timeline` (`19cd9ff6…`, for `s5timeline` only). The `-diag` and timeline overlays apply to the candidate unchanged: `build.py` asserts that each patch site occurs exactly once. `connection.go` is a merge seam, but both diag sites there occur once, unchanged. `packet_emission_bbr.go`, `internal/congestion/bbr_sender.go` and `flow_controller_connection.go` equal r9's.
  - *Aids:* every aid except five is copied **byte-identical** from #739's record and checked by `build.py`, including #739's adapted [matrix.py](matrix.py) and [follow.py](follow.py). Those two are kept verbatim, so their docstrings still name #739 and r9; the candidate is set only by `build.py`'s binaries.
    - Adapted, with only paths and their header comments changed: [art.py](art.py) (artifact directory) and [sync.sh](sync.sh) (artifact and remote directories).
    - [driver.sh](driver.sh) changes its remote directory and log path. After this registration's review, it also gained a check before start and handling for a failed observation (see [Unattended run](#order)).
    - [stages.py](stages.py), after this registration's review, leaves an unusable observation out of every analysis view (two lines; its docstring). #739's aids assumed every counted observation was usable, and `analyze.py` and `summarize` stop on a failed one. `chosen` still records every attempt, so the rerun rule and block status are unchanged.
    - A dry run on #739's retained observations (`raw.tar.gz` at `138223ac`), before any observation of this record, checked the change:
      - With the data unchanged, `stages.py readiness` reproduces #739's readiness list exactly.
      - With one candidate observation's exit code set to failed and one reference observation's receipt removed, `resume.py mark` marked the receipt-less attempt. `follow.py`'s pending reruns then listed both blocks as *unusable*.
      - The analysis completed. The failed candidate's cell had four blocks. The missing reference dropped that block for every arm, because each ratio needs its reference.
    - The aid tests pass: `rules_test.py` (43 tests), `harness_test.py` (10), `receiver_test.py` (11) and `localize_test.py` (14).
  - *New, for a failed observation:* [resume.py](resume.py), with [resume_test.py](resume_test.py) (3 tests, passing; runner and quiet-host gate stubbed). It completes a stage after a failed observation without replacing any attempt. Its `mark` mode gives an attempt without a receipt a marker receipt (see [Unattended run](#order)).
  - *New, descriptive only:* [r9context.py](r9context.py), with [r9context_test.py](r9context_test.py) (8 tests, passing), writes `r9-context.json` from this record's `summary.json` and `attribution.json` and r9's at `138223ac`. It runs after `stages.py all`, sets no flag and reads no observation.
- **Gates.** No transport source changes here.
  - The candidate's tree is #774's merge `70ec8bb5` plus one journal entry (`docs/DEV-JOURNAL.md` only). `70ec8bb5`'s tree equals #774's pull-request head `bbaa5738`, whose 33 hosted checks (unit, integration, lint and cross-compile) passed.
  - Slice 5's Linux kick test passed on `minimax` under `taskset -c 1-3`.
  - No gate is rerun.

## Registration review

One `ras consider` of this registration ran before any data: run `20261016T053350-9337927278ff908e13ee843d`, on `0d142c9e`, with six reviewers and adjudication. The synthesis queued five fixes; all are made above:

1. **Failure handling.** The failure handling was overstated. #739's driver stops at a failed or timed-out observation, and its analysis cannot load one.
2. **Timeline context.** `r9context.py` now carries the lateness, phase-share and goroutine-state figures, with a test.
3. **Escalation order.** The driver runs the conditional stages before the operator's escalation; this is disclosed, with the cap behaviour.
4. **Preservation flag wording.** The contract's sentence is now quoted verbatim, without an added "until".
5. **Factual inventories.** Deviation 2, the journal commits, and the Up policy as the attribution behind the S6 exceptions.

The optional clarifications it listed are also made: `presrss` loopback and S6 wording; the selection order; the not-run labels beside *evidence gap*; D3 in the changed list; the comparator wording; and the check before start.

**One departure from the synthesis, on item 1.** The synthesis asked only to disclose that a failure ends unattended operation, and advised against adding a recovery method. This record instead registers one, before any data: `resume.py`, its `mark` mode, the driver's handling, and the two-line `stages.py` view change. The reasons:

- With every observation usable, the analysis output is identical to #739's (the dry run above).
- `run.py`, `analyze.py`, `rules.py`, `matrix.py` and `follow.py` are unchanged, and so are every arm, seed, limit, rule and cap.
- Without it, a single timeout would stop the window and leave cells unmeasured. Recovery would then be improvised after data exist, which would be a deviation.

**Residual limitation.** More than three failed attempts in one stage stop the order. Any cell left unmeasured or incomplete is reported as such, never as passing.

## Prerequisites

#739's checks (which are #736's), through [stages.py](stages.py) `prereq`, on a quiet host, each run excluded from every statistic: ECN calibration ([prereq.py](prereq.py)), ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`), native I/O (`perfsmoke`: one send syscall and one transmit per measured-window forward packet), resources (threads inside their CPU sets, 8 MiB effective socket buffers, no UDP errors), instruments (`perf stat` at ≥ 95% running; `perf record` resolves Go frames) and the timeline overlay (`xsmoke`). The criteria are [#736's](https://github.com/the-sarge/quic-go-fast/blob/3bf2245e/docs/audits/2026-10-06-bbr-r8-linux-redemonstration/README.md#prerequisites). The smoke runs use the candidate. A failure ends this record as a reported prerequisite gap. `matrix.py hostfacts` records the governor, EPP, boost, idle states and other host settings read-only at the start and end of every stage.

## Registration: readiness

#712's readiness stage as #715, #736 and #739 ran it, unchanged except for the candidate revision:

- **Arms:** Reno, A/A Reno, candidate (BBRv3) and Reno on candidate, on loopback and S5; Reno, A/A and candidate on S6.
- **Runs:** five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations through #712's unchanged `run_case` ([matrix.py](matrix.py) stages `loopback`, `s5`, `s6`).
- **Flags:** #712's ([analyze.py](analyze.py), unchanged). Per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against these limits: goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20. On S6, the useful benefit is repeatable when the candidate beats Reno in all five pairs.
- **Preservation check:** Reno on candidate is held to the same limits; see [How a Reno-on-candidate difference is reported](#how-a-reno-on-candidate-difference-is-reported).

**Contamination.** #712's test ([localize.py](localize.py) `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean, or above 0.50 in any one-second sample), with #715's handling:

- A block holding a contaminated or unusable observation (a block is all arms of one path, workload and block number) is rerun once with the same seed, under `observations-rerun`.
- The rerun replaces the original only if it is usable and uncontaminated ([rules.py](rules.py) `choose_block`).
- `summary.json` applies that rule. `summary-original.json` applies #712's rule to the original blocks, and any flag that differs between the two is reported.
- At most ten readiness blocks are rerun. Beyond that, the remaining contaminated blocks are reported under #712's rule.

**Reporting.**

- **Every cell is reported:** its median ratio, its blocks crossing, its A/A range, its raised flag, its absolute medians, and r9's value from #739 beside it.
- **Every raised cell stays raised, with absolute values.** Nothing is pre-cleared: r9's readiness exceptions do not carry over. Those were the S6 control p95 cells (both workloads), an attributed exception with the Up policy as its attribution, and the unattributed eight memory cells and loopback STREAM control p95. No cell here is labelled as covered by one, and the record states no advancement verdict.
- **Carried open costs.**
  - [#738](https://github.com/the-sarge/quic-go-fast/issues/738)'s S5 DATAGRAM sender CPU rise is a readiness cell, S5 DATAGRAM sender CPU per useful GiB. It is reported beside r9's 0.953 (#739) and r8's 0.909 (#736).
  - [#734](https://github.com/the-sarge/quic-go-fast/issues/734)'s run-loop wake increase is reported through the cost its guard protects: sender and receiver CPU per useful GiB on every path. The operator chose this on October 16, 2026. The wake count itself is not a readiness measure under #739 and is **not measured** here.
  - The open memory and loopback-latency questions are readiness cells (peak RSS, control p95). They are measured here on the candidate, and no exception covers them.
- **Incomplete cells.** A cell with fewer than five counted blocks (an unusable block that stayed unusable after its rerun, or a missing reference) is reported as **incomplete (n of 5)** beside its median and flag. The flag is still computed by the unchanged rule. An incomplete cell that does not cross its limit is reported as "not raised on n of 5 blocks", never as passing or cleared.
- A CPU flag that falls but stays above its limit without a cause remains unresolved.
- These are Linux results. The candidate's macOS and Windows behaviour stays unmeasured, and supported-platform correctness remains required.

## Registration: S5 timeline and receiver split

**Stage `s5timeline`.** #739's stage, unchanged ([#739's registration](https://github.com/the-sarge/quic-go-fast/blob/138223ac/docs/audits/2026-10-08-bbr-r9-linux-redemonstration/README.md#registration-s5-timeline-and-receiver-split), which is #736's and #715's):

- **Question:** what bounds S5 delivery. **Hypothesis:** model behaviour. **Competitor:** sender timing.
- **Instrument:** #715's measurement-only [timeline overlay](timeline/), in `cand-timeline`.
- **Runs:** S5, both workloads, four blocks, seeds 9601–9604.
- **Rule:** [rules.py](rules.py) `timeline_decomposition`, `timeline_verdict`, `workload_verdict` and `stage_verdict`; *no deficit* under 0.02; the unusable tests; the three-of-four majority.
- **Escalation:** a workload with exactly two of four agreeing usable blocks gets blocks 5–6 (seeds 9605–9606), once.
- **Ending:** inconclusive if no verdict.
- **Descriptive figures:** lateness events and total lateness per run, phase shares, connection-goroutine states, and instrumented ÷ readiness goodput. r9's values from #739 are reported beside them.

The stage describes S5 whether or not an S5 goodput cell is raised.

**Receiver CPU split.** #715's split, unchanged and with no new runs ([rules.py](rules.py) `cpu_split`, `receiver_localization`), applied to the readiness observations. A raised S5 receiver CPU cell is classified as **inside window**, **outside window** or **mixed**; other cells are reported descriptively. This is localization, not cause. A raised S5 receiver CPU cell has no causal stage here, so it is reported as unresolved, with its split.

## Registration: preservation (conditional)

#739's stages and outcome rules, unchanged except that D3's mapping is not applied ([#715's registration](https://github.com/the-sarge/quic-go-fast/blob/36f7ce01/docs/audits/2026-10-05-bbr-linux-redemonstration/README.md#stage-presrss-reno-on-candidate-rss-conditional-d3)):

- **`presrss`**, for each (path, workload) pair with a raised Reno-on-candidate RSS cell (at most two pairs): six blocks of `reno-diag` and `cand-diag` with Reno at both endpoints and heap series; seeds 9801–9806 on S5, 9901–9906 on S6, none on loopback (as #715 registered it; loopback runs unseeded, as in readiness). S6 has no Reno-on-candidate arm, so only loopback and S5 cells can trigger the stage, and the S6 wording is inherited; [rules.py](rules.py) `rss_preservation` (*retention excess*, *RSS excess without retained heap*, *not reproduced*, *inconclusive*, *evidence gap*).
- **`preslat`**, for a raised Reno-on-candidate loopback control p95 cell (at most one): six blocks of `reno` and `cand` with Reno, a 120 s window, at least 100 replies per run and 500 per arm; [rules.py](rules.py) `latency_preservation` (pooled p95 ratio with a 95% block-bootstrap interval: *regression reproduced*, *not reproduced*, *inconclusive*, *evidence gap*). The readiness value stays as recorded whatever the stage finds. As registered above, this record ends no blocking state; slice 7 decides whether D3's mapping applies to the ported revision.
- A raised Reno-on-candidate p95 cell on S5 has no stage. `stages.py` labels it *no registered stage*. It is reported as a raised preservation flag that keeps blocking, with no stage (an evidence gap in #739's wording). S6 has no Reno-on-candidate arm. A raised Reno-on-candidate CPU or goodput cell has no stage either. It is reported as an unattributed preservation flag that keeps blocking, beside r9's Reno-on-candidate value for the same cell from #739, as context.

**Cap.** At most **60** Linux attribution observations, reruns and escalations included:

| Stage | Runs | Observations (maximum) |
| --- | --- | --- |
| `s5timeline` | S5; `cand-timeline`; both workloads; 4 blocks, seeds 9601–9604 | 8 (12 with escalation) |
| Receiver CPU split | Readiness observations only | 0 |
| `presrss` | Conditional: per (path, workload) pair with a raised Reno-on-candidate RSS cell, 6 blocks × 2 arms | 12 per pair, at most 2 pairs |
| `preslat` | Conditional: per raised Reno-on-candidate loopback latency cell, 6 blocks × 2 arms, 120 s window | 12, at most 1 cell |
| Total | | ≤ 48 plus reruns, ≤ 60 |

**Which cells run when more are raised than the caps allow** ([follow.py](follow.py), unchanged):

- `presrss` counts (path, workload) pairs, not cells. A pair whose sender and receiver RSS cells are both raised shares one 12-observation run.
- `follow.py` runs the first two pairs, and for `preslat` the first loopback workload, in Python's sorted order: `S5` sorts before `loopback`, and `datagram` before `stream`.
- A raised cell left out by the at-most-two or at-most-one limit is recorded under `not_run` in `follow-decisions.json` and reported as **not run (limit)**. A cell the cap of 60 stops is recorded under `not_run_cap` and reported as **not run (cap)**.
- For a cell that was not run, `stages.py` computes *evidence gap* from zero blocks. The record reports the not-run reason first and the *evidence gap* beside it, and states that no stage ran for that cell, so a missing stage does not read as an inadequate one.
- Either way, the cell stays a raised, blocking preservation flag.

Contaminated or unusable attribution blocks follow the readiness rerun rule, and reruns count toward the cap. If the cap binds, stages run in the order listed, and any that cannot run end as **not run (cap)**. Attribution never sets a flag value.

**Escalation order, inherited from #739's driver.** The driver runs `follow.py conditional` straight after `s5timeline`. The `s5timeline` escalation is decided by the operator's session, so it runs after the conditional stages and their reruns. Its observations count against the cap of 60 at that point. If the cap would be exceeded, the escalation ends as **not run (cap)**, and the record reports that the run order, not the listed priority, decided it. On #739's counts this cannot bind: `s5timeline` 8 plus both conditional stages at their limits (36) is 44, so binding would need 13 attribution reruns.

## Stages not run

- **Memory attribution (`timeline`, `heapsites`), #740's memory instrument, and #741's or #744's stages.** The contract's step 1 names #739's readiness, `s5timeline` and conditional preservation stages only. Raised memory and control p95 cells are reported as **raised**, for slice 7.
- **`recvharness`, #715's loopback-bottleneck stage and any loopback hand-off stage.** #739 did not run them, and the contract does not list them. Raised loopback goodput or CPU cells, and a raised S5 receiver CPU cell, are reported as unresolved.
- **A run-loop wake count.** Not a readiness measure under #739 (above).
- **A `main`-without-port arm.** Not part of #739's stage. The port's share of any Reno-on-candidate difference is not examined.
- **macOS and Windows.** The candidate's behaviour there is unmeasured, not inferred.

## Disclosures made before data

- No observation of this record existed when this registration was written: no prerequisite, smoke, readiness or attribution run. The builds above were made before writing it; building produces no observation.
- **The candidate has no prior readiness figure.** During slice 5, `minimax` ran the Linux kick test under `taskset -c 1-3`. That was a correctness test, and it produced no goodput, CPU, RSS or latency figure.
- **r9's #739 values are known**, and they are the context named above. Nothing in this registration depends on them: the stages, limits and rules are #739's, unchanged. Known in advance: on r9, every goodput and CPU cell passed, no Reno-on-candidate cell was raised, and eleven cells were raised. Those were the eight S5 and S6 memory and control p95 cells; loopback receiver RSS (STREAM, DATAGRAM); and loopback STREAM control p95.
- **Interpretation known in advance.** Loopback STREAM goodput is sensitive to code placement at about 3% (#738, #744). Small RSS cells cannot be reconciled with `ru_maxrss` within a few hundred KiB (#740). The candidate's root code and module versions differ from r9's (above). The readiness rule applies regardless.

## Time estimate

All aids exist and every binary is built, so no instrument development is expected. On a quiet host (#739's figures):

| Step | Estimate |
| --- | --- |
| Prerequisites and smoke checks | about 15 minutes |
| Readiness (110 observations) | about 70 minutes |
| Quiet-host gating (one minute before each of 15 blocks) | at least 15 minutes |
| `s5timeline` | about 10 minutes |
| Conditional `preslat` | about 30 minutes, if triggered |
| Conditional `presrss` | about 15 minutes per cell, if triggered |
| **Total** | **about 1 h 50 min without conditional stages; up to about 2 h 50 min with all of them** |

Foreign load extends the gate's waits and adds same-seed reruns. Each readiness block rerun costs about 5–9 minutes.

## Order

**On the Mac:**

1. `build.py` (done), the four aid tests, `r9context_test.py` and `resume_test.py` (done).
2. `sync.sh`.

**On `minimax`,** sequentially under `taskset -c 1-3`, with no build or test running there while a stage measures, and `matrix.py hostfacts` before and after each stage:

1. `prereq.py` (imported after `art`), then `smoke`, `perfsmoke`, `ecnsmoke` and `xsmoke`.
2. The readiness stages: `loopback`, `s5`, `s6`; then any readiness reruns.
3. `s5timeline`, and any escalation.
4. The conditional `presrss` and `preslat` cells, as the readiness flags require; then attribution reruns.
5. `stages.py fold`.

**Then, anywhere:** `stages.py all`, `r9context.py`, `rules_test.py`, `receiver_test.py` and `harness_test.py`.

**Unattended run.** By operator choice (October 16, 2026), the order runs unattended from 05:30 America/New_York on October 16, 2026 (09:30 UTC), started by a systemd user timer on `minimax` that runs [driver.sh](driver.sh). This is #739's arrangement, with two additions made after this registration's review: the check before start, and the handling of a failed observation (both below).

- **Check before start.** The driver first checks that the artifact directory on `minimax` has no `observations*`, `prereq`, `hostfacts-*`, `follow-decisions.json` or stage summary. If any exists, it stops and logs `PRESTART-NOT-EMPTY`. Otherwise it logs `PRESTART-EMPTY`. `run_case` silently reuses an existing clean observation, so a leftover one would otherwise count as this record's.
- **What the driver runs:** the prerequisites and smoke checks, the readiness stages, `s5timeline` and the conditional stages, in the order above. The exception is the `s5timeline` escalation, which stays with the operator's session and so runs after the conditional stages (see [Escalation order](#registration-preservation-conditional)).
- **What [follow.py](follow.py) decides:** it applies the registered rules that would otherwise need a person between stages. These are same-seed reruns of contaminated or unusable blocks (the #714 rule, capped at ten readiness blocks and at the attribution cap), and the conditional preservation stages that the readiness flags trigger.
- **Where decisions go:** every decision `follow.py` takes is written to `follow-decisions.json`.
- **Dry run:** `follow.py` is byte-identical to #739's, which was dry-run before #739's data and then ran #739's order unattended. The failure handling was dry-run on #739's retained observations (above).
- **A failed observation does not stop the order.** This handling was added after this registration's review, before any data. #739's `run_case` and `run_case_x` write a failed observation's receipt and then raise. Running the stage again also stops, at the kept failed attempt.
  - If a stage stops on a failed observation, the driver runs [resume.py](resume.py) for that stage. It runs the stage's registered blocks through `matrix.py`'s unchanged `block` and `STAGES`. It keeps the failed attempt as it is, never replaces an attempt, and runs every observation that does not exist yet.
  - Before each `follow.py` step, the driver runs `resume.py mark`. An attempt that ended before its receipt was written (a timeout, or an endpoint that never became ready) gets a marker receipt with `exit_codes` null and `incomplete`, and nothing else changes. Without it, `stages.py` and `follow.py`, which find attempts by their receipt, would never see that block. Each marking is logged to `resume-marks.json`.
  - `follow.py`'s unchanged rule then reruns the failed block once, with the same seed. The kept failed attempt has a receipt (its own or the marker), so `stages.py` sees that block as *unusable*. An unusable observation is left out of every analysis view, and its cell reports fewer blocks.
  - More than three failed attempts in one stage stop the order for the operator's session: a systematic failure is not run through the window.
  - If a `follow.py` step fails (for example, a rerun observation fails), the driver logs `FOLLOW-FAILED`, runs `resume.py mark`, and runs the same step once more. A second failure is logged as `FOLLOW-FAILED-AGAIN`, and the driver continues; the operator's session then runs `resume.py mark` and the same `follow.py` step by hand. Each step only reruns blocks that have one attempt, so no block is rerun twice.
  - A rerun that fails is not counted toward `follow.py`'s cap of ten, because `follow.py` records a rerun only after it succeeds. The record reports every rerun attempt from the observation directories.
- **What stays with the operator's session:**
  - The prerequisite review. The driver continues past the smoke checks unless one fails outright, so a review failure afterwards still ends the record as a prerequisite gap, and any later observations are reported as excluded.
  - The `s5timeline` escalation, which depends on that stage's verdicts.
