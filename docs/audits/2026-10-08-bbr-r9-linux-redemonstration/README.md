# Linux re-demonstration of the loopback-tested BBRv3 revision

**Date:** October 8, 2026, America/New_York. **Scope:** [Re-demonstrate the loopback-tested BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/739), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D5 ("Ticket G") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d5--ticket-g-re-demonstrate-only-if-ticket-f-keeps-a-change), on the route "F keeps a change → G re-demonstrates F's survivor". Measurement only: no production merge, transport source change, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r9-linux-redemonstration`, based on `070e1597` (the [loopback hand-off record](../2026-10-07-bbr-loopback-handoff/README.md)), whose tree's code is exactly r9. The revision under test is **r9, `81e9dc8c`**: r8 (`95f5b6b7`) plus a 32-entry send queue for BBR connections. [Test whether the send-credit hand-off limits BBRv3's loopback goodput](https://github.com/the-sarge/quic-go-fast/issues/738) kept it by operator decision after a negative registered outcome (its deviation 4). The [#736 re-demonstration of r8](../2026-10-06-bbr-r8-linux-redemonstration/README.md) (`3bf2245e`) supplies the readiness stage, the S5 timeline, the preservation stages, the fixture, the host layout and the aids that this record reruns.

**Status: complete.** The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) were written, with the aids adapted and the builds made, before any prerequisite, smoke, readiness or attribution observation of this ticket, and committed (`e50be7b7`, `2fea8559`, `365a3f44`). They are unchanged. The [Answer](#answer), results, [Deviations](#deviations) and later sections were added afterwards.

## Question

On owned Linux hardware, what are the readiness flags of r9 (`81e9dc8c`), what does the S5 timeline show, and does any raised Reno-on-candidate preservation cell reproduce under its registered stage?

## Answer

**Not ready on Linux, but every goodput and CPU cell now passes; the remaining flags are memory and control-stream latency.** On `minimax`, r9 keeps the S6 benefit (14.53× STREAM and 14.74× DATAGRAM Reno's goodput, five of five pairs each) and raises no Reno-on-candidate cell. Compared with r8 in #736:

- **Both loopback goodput flags and the loopback DATAGRAM sender CPU flag are gone.** Loopback goodput is 1.148 (STREAM) and 1.001 (DATAGRAM) of frozen Reno's (was 0.943 / 0.854), and loopback DATAGRAM sender CPU per useful GiB is 1.022 (was 1.111). The 32-entry queue's loopback effect from #738 holds in plain readiness builds.
- **S5 stays clean, and #738's open CPU cost lands under the limit.** S5 goodput is 0.967 / 0.972, and S5 DATAGRAM sender CPU per useful GiB is **0.953** of frozen Reno's (r8 0.909; #738's cross-record estimate was about 0.95). Every S5 CPU cell passes.
- **S5 timing stays model-bound.** The S5 timeline reads **model behaviour** in both workloads (STREAM four of four blocks; DATAGRAM three of four, one *no deficit*). Timing is 0.0001–0.0003 of capacity, with about 7–8 µs per late deadline, as on r8.
- **Three new raised cells, all on loopback, where r9's goodput rose.** Receiver RSS 1.120 (STREAM, four blocks) and 1.148 (DATAGRAM, five blocks), and STREAM control p95 1.485 (four blocks; A/A 0.81–1.09; 0.199 ms against frozen Reno's 0.134 ms). No registered stage here examines them, so all three are **unresolved**.
- **One r8 cell passes on r9:** S6 DATAGRAM receiver RSS, 1.095 (two blocks crossing; was 1.118).
- **The eight S5 and S6 memory and latency cells that #740 will attribute stay raised at about r8's magnitudes.** S5 STREAM sender 1.235 and receiver 1.518, S5 DATAGRAM sender 1.159, S6 STREAM sender 1.673 and receiver 1.898, S6 DATAGRAM sender 1.542, and S6 control p95 1.562 (STREAM) and 1.459 (DATAGRAM).
- **Preservation.** No Reno-on-candidate cell is raised, so neither `presrss` nor `preslat` ran. The loopback DATAGRAM Reno-on-candidate p95 reads 0.744 (r8 read 2.588); frozen Reno's own A/A arm crossed that cell at 1.599 (range 0.47–4.44), which is context only.

## Readiness results

Plain builds, five blocks per path and workload, 110 counted observations, all exiting cleanly and passing receiver integrity. **No observation was contaminated**, so no block was rerun and `summary.json` equals `summary-original.json` in every flag. Ratios are the median of per-block ratios against the same block's frozen Reno. Bold marks a raised flag; parentheses give the blocks crossing the limit; brackets give #736's r8 value. A/A per-block ranges are in [summary.json](summary.json) beside every ratio.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | 1.148 [**0.943**] | 1.007 [1.095] | 0.937 [1.044] | 0.954 [0.969] | **1.120 (4)** [1.087] | **1.485 (4)** [1.171] |
| Loopback STREAM | Reno on candidate | 1.003 | 1.000 | 1.000 | 1.006 | 0.998 | 0.930 |
| Loopback DATAGRAM | Candidate | 1.001 [**0.854**] | 1.022 [**1.111**] | 0.964 [1.054] | 0.956 [0.919] | **1.148 (5)** [1.045] | 0.868 [0.526] |
| Loopback DATAGRAM | Reno on candidate | 0.999 | 1.000 | 1.002 | 0.998 | 1.044 (1) | 0.744 (2) [**2.588**] |
| S5 STREAM | Candidate | 0.967 [0.972] | 0.937 [0.935] | 1.040 [1.055] | **1.235 (5)** [**1.261**] | **1.518 (5)** [**1.565**] | 1.061 [1.081] |
| S5 STREAM | Reno on candidate | 1.000 | 1.003 | 0.991 | 0.999 | 0.988 | 0.993 |
| S5 DATAGRAM | Candidate | 0.972 [0.972] | 0.953 [0.909] | 0.988 [1.007] | **1.159 (4)** [**1.173**] | 1.070 [1.086] | 1.021 [0.987] |
| S5 DATAGRAM | Reno on candidate | 1.000 | 0.993 | 0.999 | 1.023 (1) | 0.990 | 1.003 |
| S6 STREAM | Candidate | benefit 14.53× [12.25–15.74] [14.02×] | 0.086 | 0.073 | **1.673 (5)** [**1.656**] | **1.898 (5)** [**1.891**] | **1.562 (5)** [**1.550**] |
| S6 DATAGRAM | Candidate | benefit 14.74× [11.34–16.32] [14.58×] | 0.090 | 0.078 | **1.542 (5)** [**1.464**] | 1.095 (2) [**1.118**] | **1.459 (5)** [**1.469**] |

The A/A frozen-Reno arm crosses one limit: loopback DATAGRAM control p95, 1.599 (three blocks; per-block range 0.47–4.44). #715's A/A arm crossed the same cell (1.615). Every other A/A cell passes.

Absolute medians (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Receiver heap peak (measured) | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4695.2 | 3.98 / 2.75 | 16.27 / 14.48 | 3.27 | 0.134 / 0.070 | — |
| Loopback STREAM | Candidate | 5390.4 | 4.01 / 2.57 | 15.52 / 16.20 | 4.46 | 0.199 / 0.121 | — |
| Loopback DATAGRAM | Reno | 4655.0 | 4.55 / 3.02 | 17.61 / 18.72 | 4.49 | 0.314 / 0.099 | — |
| Loopback DATAGRAM | Candidate | 4654.5 | 4.66 / 2.92 | 16.83 / 21.72 | 6.94 | 0.253 / 0.174 | — |
| S5 STREAM | Reno | 95.9 | 10.39 / 8.79 | 17.97 / 16.94 | 3.38 | 187.2 / 181.4 | 0.113% |
| S5 STREAM | Candidate | 92.7 | 9.74 / 9.12 | 22.18 / 25.71 | 14.62 | 199.6 / 102.3 | 0.214% |
| S5 DATAGRAM | Reno | 94.4 | 11.47 / 8.95 | 17.88 / 14.23 | 3.26 | 179.6 / 172.1 | 0.403% |
| S5 DATAGRAM | Candidate | 91.8 | 10.89 / 8.86 | 20.86 / 15.14 | 4.23 | 183.7 / 102.3 | 0.027% |
| S6 STREAM | Reno | 6.1 | 114.12 / 123.25 | 13.25 / 13.50 | 3.55 | 127.7 / 105.5 | 0% |
| S6 STREAM | Candidate | 88.3 | 9.81 / 8.99 | 22.18 / 26.34 | 14.38 | 199.5 / 102.1 | 0.250% |
| S6 DATAGRAM | Reno | 5.9 | 119.42 / 118.89 | 13.78 / 13.88 | 3.35 | 125.9 / 104.8 | 0% |
| S6 DATAGRAM | Candidate | 86.2 | 10.79 / 9.41 | 21.24 / 15.19 | 4.18 | 187.3 / 102.0 | 0.047% |

**#738's open costs, measured here against frozen Reno.**
- *S5 DATAGRAM sender CPU per useful GiB:* **0.953**, passing, with no block crossing (r8 0.909 in #736; #738's cross-record estimate 0.909 × 1.050 ≈ 0.95). Different runs, so the r8 comparison is context, not a paired result; it agrees with #738's paired +5%.
- *Control-stream latency with the deeper queue:* loopback STREAM p95 is newly raised (above). Loopback DATAGRAM p95 passes (0.868), and S5 p95 passes in both workloads (1.061 / 1.021).
- *Memory with the deeper queue:* both loopback receiver RSS cells are newly raised. Descriptively, the receiver's measured-window heap peak is 1.19 MiB (STREAM) and 2.45 MiB (DATAGRAM) above frozen Reno's, while loopback sender RSS falls (0.954 / 0.956). The S5 and S6 memory cells move by −0.05 to +0.08 against r8.
- *S6:* the benefit holds or grows slightly (14.53× / 14.74× against 14.02× / 14.58×), at 0.086 / 0.090× Reno's sender CPU per useful GiB.

**Descriptive receiver CPU split.** No S5 receiver CPU cell is raised, so the registered split classifies nothing; [attribution.json](attribution.json) `cpu_split` holds every cell's window ratio and outside-window share.

## S5 timeline: model behaviour

Run straight after readiness (`s5timeline`, 8 observations, all clean, none rerun, no escalation: no workload had exactly two of four agreeing blocks). Per block, fractions of the bottleneck's capacity over the measured window, from the [timeline overlay](timeline/); [attribution.json](attribution.json) `s5timeline`.

| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | 0.028–0.034 | 0.028–0.038 | 0.017–0.025 | 0.0002–0.0003 | 0.011–0.014 | model ×4 → **model** |
| DATAGRAM | 0.020–0.030 | 0.020–0.030 | 0.010–0.020 | 0.0001–0.0002 | 0.009–0.010 | model ×3, no deficit ×1 → **model** |
| r8 STREAM (#736) | 0.029–0.036 | 0.031–0.039 | 0.019–0.024 | 0.0002–0.0003 | 0.012–0.015 | model |
| r8 DATAGRAM (#736) | 0.030–0.035 | 0.029–0.034 | 0.019–0.024 | 0.0008–0.0010 | 0.008–0.009 | model |

- **Stage verdict: model behaviour**, in both workloads, unchanged from r8.
- **Lateness.** 27,259–27,419 (STREAM) and 28,709–29,008 (DATAGRAM) late arrivals at a full-quantum deadline per 30 s, totalling 0.217–0.226 s per run (about 8 µs per event; maxima 0.05–1.1 ms). r8's were 27,326–28,805 events and 0.19–0.22 s.
- **The model share** is the same as r8's: the bandwidth estimate in Cruise is 1.006–1.007× the bottleneck, the median pacing rate is 0.995–0.997× capacity, Down holds 17–23% of the window and ProbeRTT 2–5%, Up 10–17%, and no long inflight bound decreased in any window.
- **Instrument perturbation is negligible.** Instrumented goodput is 1.004 (STREAM) and 1.001 (DATAGRAM) of the readiness candidate's.

## Flag report for the next tickets

For [Attribute the BBRv3 candidate's memory and S6 latency flags on the final revision](https://github.com/the-sarge/quic-go-fast/issues/740) and then [Decide whether the loopback- and memory-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/741), on candidate r9 (`81e9dc8c`), Linux (`minimax`):

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 14.53× (STREAM) and 14.74× (DATAGRAM) Reno goodput, 5/5 pairs each, at 0.086 / 0.090× Reno sender CPU per useful GiB. |
| **Passed** | Every goodput and CPU cell on every path. Loopback sender RSS (both), loopback DATAGRAM control p95, S5 control p95 (both), S5 DATAGRAM receiver RSS, S6 DATAGRAM receiver RSS. Every Reno-on-candidate cell. |
| **Raised, for #740 under D6** | S5 STREAM sender RSS 1.235 and receiver 1.518; S5 DATAGRAM sender 1.159; S6 STREAM sender 1.673 and receiver 1.898; S6 DATAGRAM sender 1.542; S6 control p95 1.562 (STREAM) and 1.459 (DATAGRAM). |
| **Raised, new, outside D6's stated inventory** | Loopback receiver RSS 1.120 (STREAM) and 1.148 (DATAGRAM); loopback STREAM control p95 1.485. D6's Stage 1 arms name S5 and S6, and its p95 rule covers S6 only. #740's registration must either include the loopback memory cells or report them as unattributed; the loopback p95 cell has no stage under D6 and goes to the decision as **unresolved**. |
| **Raised (preservation)** | None. |
| **Not measured here** | r9's macOS readiness (D8); r8's run-loop wake increase. |

D6's Stage 1 inventory (80 observations) was sized before these flags. The ring treatment, the receiver-controller arm and the S6 p95 rule apply to the S5 and S6 cells unchanged.

## Deviations

None. No rule, threshold, seed, arm, cap or stage changed after data. The unattended driver, `follow.py`, the quiet-host gate and the 05:30 start were all registered and committed before the first observation (`2fea8559`, `365a3f44`).

**Disclosed operational facts.** The prerequisite review ran on `minimax` at 09:35–09:37 UTC, during the first loopback readiness block, under `nice -n 19 taskset -c 1-3` (runner cores) to avoid pulling data mid-stage. A watcher polled the driver log over ssh every 60 s (before readiness) and then every 120 s. The contamination test found no foreign load in any counted observation, and every quiet-host gate passed on its first 60 s window.

## Preservation

- **Payload integrity.** All 125 retained observations exited cleanly and passed receiver integrity: 110 readiness, 8 timeline and 7 excluded prerequisite and smoke runs.
- **Default Reno.** No Reno-on-candidate cell is raised. Medians lie within 0.991–1.003 on goodput and CPU, and within 0.988–1.044 on RSS.
- **No code change.** No transport source changed. r9's gates are #738's. No recorded translation, cap, evidence contract, controller version or default changed. The overlays exist only in their measurement builds.
- **No host change.** `perf_event_paranoid`, socket-buffer limits, offloads, the governor, EPP, boost and idle states were left unchanged; every `hostfacts` snapshot matches the first.

## Limits

- **Platform and topology.** One Linux host, loopback endpoints and a userspace relay. This certifies no other platform, no real carrier path and no production readiness. r9's macOS readiness is unmeasured.
- **Repetition.** Five blocks per readiness cell; four timeline blocks per workload.
- **Loopback.** Nothing here explains the new loopback receiver RSS or STREAM control p95 cells. The coincidence with r9's higher loopback goodput (STREAM +15% over Reno) is descriptive, not a cause. Loopback control p95 is a sub-millisecond quantity at 20 replies per run (A/A 0.81–1.09 here, and 0.47–4.44 on DATAGRAM).
- **Engagement.** No queue CE-marked any packet, so the CE response stays unengaged. C4 items 1 and 3 and the low-rate pacing floor were not examined.

## Assets and reconstruction

- **Results:** [summary.json](summary.json) (registered readiness), [summary-original.json](summary-original.json) (#712's rule; identical flags) and [attribution.json](attribution.json) (prerequisites, readiness blocks, CPU split, `s5timeline`, and `presrss` and `preslat` as not triggered).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json): every observation, prerequisite, driver log, `follow-decisions.json`, `hostfacts` snapshot and build receipt. Binaries, credentials and exported source trees are omitted.
- **To reconstruct**, in a fresh owned worktree of this branch: run `build.py`, then `sync.sh` to a Linux host with the same CPU layout; run [driver.sh](driver.sh) there (or its steps by hand under `taskset -c 1-3`); then `stages.py fold` there, and `stages.py prereq` and `stages.py all` anywhere.

## What stays fixed and what changes

- **Fixed (D5):** #736's readiness stage, unchanged except for the candidate revision: the same arms, paths, durations, seeds, rotation, flag limits, median rule, A/A reporting, contamination rule (the operator's #714 rerun rule, as #715 adopted it, with #736's cap of ten readiness block reruns) and core layout. Frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; the fixture with the causal-diagnosis heap patch; the relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter); the launcher; the endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz); `minimax`'s core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings; `GOTOOLCHAIN=go1.27.0`; #715's timeline rule; D3's preservation rules as #715 registered them, with the [D3 mapping](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d3--the-reno-on-candidate-loopback-latency-cell-resolved-for-r8-on-linux-by-accepted-operator-interpretation) accepted for the latency cell.
- **Changed, all recorded:**
  - *Candidate:* `cand` is r9, `81e9dc8c`.
  - *Builds:* [build.py](build.py) is #736's with the changes in its docstring. It asserts that every binary rebuilt from a revision and overlays a prior record already built is **byte-identical** to it: frozen Reno (`cda8feb1…`), `reno-diag` (`36752495…`), both relays (`033d467c…`, `24ee941d…`), the calibration aid (`4218b433…`) and the launcher (`a6146d62…`) match #736's binaries, and `cand` (`4fa75322…`) matches #738's r9 build (`new`). The new r9 variants are `cand-diag` (`a55f234a…`, for `presrss` only) and `cand-timeline` (`b5cbb7cd…`, for `s5timeline` only). The `-diag` and timeline overlays apply to r9 unchanged; r9 touches none of the lines they patch (`build.py` asserts each patch site occurs exactly once).
  - *Stages:* only D5's: readiness, `s5timeline`, and the conditional `presrss` and `preslat`. The memory stages (`timeline`, `heapsites`) and the D2 re-attribution are [Attribute the BBRv3 candidate's memory and S6 latency flags on the final revision](https://github.com/the-sarge/quic-go-fast/issues/740)'s under D6, and #736's conditional `recvharness` is not listed by D5; see [Stages not run](#stages-not-run). [matrix.py](matrix.py) drops them, with their arms, #735's receiver instruments and the `rsmoke` check that served only `recvharness`; [stages.py](stages.py) leaves their steps out of `all`, with their code unchanged.
  - *Quiet-host gate:* before every counted block, [matrix.py](matrix.py) runs #738's `wait_quiet`, verbatim: it waits until the fixture cores and their SMT siblings carry under 0.05 foreign cores mean, and 0.30 in any one-second sample, for 60 s. It triggers on host state only, never on an outcome; the registered contamination test still judges every observation. `minimax` is shared with CI runners and VMs, and #738 added the gate after a fully contaminated development run.
  - *Aids:* copied byte-identical from #736's record and checked by `build.py`, through that record's existing checks against #712, #715 and #735 and a new check against #736: [run.py](run.py), [analyze.py](analyze.py), [attribution.py](attribution.py), [localize.py](localize.py), [localize_test.py](localize_test.py), [prereq.py](prereq.py), [pack.py](pack.py), [calibrate/](calibrate/), [launch/](launch/), [relay/](relay/), [overlay/](overlay/), [counting/](counting/), [model-overlay/](model-overlay/), [rules.py](rules.py), [rules_test.py](rules_test.py), [stage_run.py](stage_run.py), [timeline/](timeline/), [go.mod](go.mod), [receiver.py](receiver.py), [receiver_test.py](receiver_test.py), [harness.py](harness.py) and [harness_test.py](harness_test.py). Adapted from #736, with recorded changes: [art.py](art.py) and [sync.sh](sync.sh) (this ticket's artifact directory), [build.py](build.py), [matrix.py](matrix.py) and [stages.py](stages.py). `rules_test.py`, `harness_test.py`, `receiver_test.py` and `localize_test.py` pass.
- **Gates.** r9 passed every gate before #738's Stage 2 data, on the Mac (`gates/gate-new.log`) and natively on `minimax` (`gates/gate-new-native.log`), and #734's frozen-policy oracle gives the same hash as r8 (`gates/oracle.json`), all in the [loopback hand-off record](../2026-10-07-bbr-loopback-handoff/gates/). No transport source changes here, so no gate is rerun.

## Prerequisites

#736's checks, through [stages.py](stages.py) `prereq`, on a quiet host, each run excluded from every statistic: ECN calibration ([prereq.py](prereq.py)), ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`), native I/O (`perfsmoke`: one send syscall and one transmit per measured-window forward packet), resources (threads inside their CPU sets, 8 MiB effective socket buffers, no UDP errors), instruments (`perf stat` at ≥ 95% running; `perf record` resolves Go frames) and the timeline overlay (`xsmoke`). The criteria are [#736's](../2026-10-06-bbr-r8-linux-redemonstration/README.md#prerequisites). `rsmoke` served only `recvharness` and is not run. A failure ends the ticket as a reported prerequisite gap. `matrix.py hostfacts` records the governor, EPP, boost, idle states and other host settings read-only at the start and end of every stage.

## Registration: readiness

#712's readiness stage as #715 and #736 ran it, unchanged except for the candidate revision: arms Reno, A/A Reno, candidate (r9, BBRv3) and Reno on candidate on loopback and S5, and Reno, A/A and candidate on S6; five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations through #712's unchanged `run_case` ([matrix.py](matrix.py) stages `loopback`, `s5`, `s6`). Flags are #712's ([analyze.py](analyze.py), unchanged): per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20; on S6 the useful benefit is repeatable when the candidate beats Reno in all five pairs. Reno on candidate is held to the same limits as a preservation check. The A/A arm's per-block ratios are reported beside every ratio and never override the rule.

**Contamination.** #712's test ([localize.py](localize.py) `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample), with #715's handling: a block (all arms of one path, workload and block) holding a contaminated or unusable observation is rerun once with the same seed under `observations-rerun`, and the rerun replaces the original only if it is usable and uncontaminated ([rules.py](rules.py) `choose_block`). `summary.json` applies that rule; `summary-original.json` applies #712's rule to the original blocks, and any flag that differs between the two is reported. At most ten readiness blocks are rerun; beyond that, the remaining contaminated blocks are reported under #712's rule.

**Reporting.** Beside each flag, #736's value for r8 on the same host, as context. For the open costs r9 carries from #738:
- **S5 DATAGRAM sender CPU per useful GiB** is a readiness cell. Its readiness value against frozen Reno is reported beside #736's r8 value (0.909) and #738's cross-record estimate (0.909 × 1.050 ≈ 0.95), which is not a measurement.
- **Control-stream latency, memory and S6** are readiness cells, measured here for the first time on r9.
- r8's run-loop wake increase is not a readiness measure and is not measured here.

A CPU flag that falls but stays above its limit without a cause remains unresolved. These are Linux results; r9's macOS readiness stays unmeasured (D8), and supported-platform correctness remains required.

## Registration: S5 timeline and receiver split

**Stage `s5timeline`.** #736's stage, unchanged ([#736's registration](../2026-10-06-bbr-r8-linux-redemonstration/README.md#stage-s5timeline-s5-timing), which is #715's): question (what bounds S5 delivery), hypothesis (model behaviour), competitor (sender timing), instrument (#715's measurement-only [timeline overlay](timeline/) in `cand-timeline`), runs (S5, both workloads, four blocks, seeds 9601–9604), rule ([rules.py](rules.py) `timeline_decomposition`, `timeline_verdict`, `workload_verdict`, `stage_verdict`; *no deficit* under 0.02; the unusable tests; the three-of-four majority), escalation (a workload with exactly two of four agreeing usable blocks gets blocks 5–6, seeds 9605–9606, once) and inconclusive ending. Descriptive figures: lateness events and total lateness per run, phase shares, connection-goroutine states and instrumented ÷ readiness goodput, beside #736's r8 values. The stage describes S5 whether or not an S5 goodput cell is raised.

**Receiver CPU split.** #715's split, unchanged and with no new runs ([rules.py](rules.py) `cpu_split`, `receiver_localization`), applied to the readiness observations: for a raised S5 receiver CPU cell, **inside window**, **outside window** or **mixed**; other cells descriptively. This is localization, not cause. A raised S5 receiver CPU cell has no causal stage here (D5 does not list `recvharness`), so it is reported as unresolved with its split.

## Registration: preservation (conditional, D3)

#736's stages and rules, unchanged ([#715's registration](../2026-10-05-bbr-linux-redemonstration/README.md#stage-presrss-reno-on-candidate-rss-conditional-d3)):

- **`presrss`**, for each raised Reno-on-candidate RSS cell (at most two): six blocks of `reno-diag` and `cand-diag` with Reno at both endpoints and heap series; seeds 9801–9806 (S5) or 9901–9906 (S6); [rules.py](rules.py) `rss_preservation` (*retention excess*, *RSS excess without retained heap*, *not reproduced*, *inconclusive*, *evidence gap*).
- **`preslat`**, for a raised Reno-on-candidate loopback control p95 cell (at most one): six blocks of `reno` and `cand` with Reno, a 120 s window, at least 100 replies per run and 500 per arm; [rules.py](rules.py) `latency_preservation` (pooled p95 ratio with a 95% block-bootstrap interval: *regression reproduced*, *not reproduced*, *inconclusive*, *evidence gap*). **Under D3's accepted mapping, only an adequate *not reproduced* outcome ends the cell's blocking state, for r9 on Linux only**; every other outcome leaves it blocking. The readiness value stays as recorded whatever the stage finds.
- A raised Reno-on-candidate p95 cell on S5 or S6 has no stage and is reported as an evidence gap. A raised Reno-on-candidate CPU or goodput cell has no stage either; it is reported as an unattributed preservation flag that keeps blocking, beside #738's Reno-on-r9 cycles per useful GiB (sender 0.996, receiver 0.997 of frozen Reno, inside its A/A range) as context.

**Cap.** At most **60** Linux attribution observations, reruns and escalations included:

| Stage | Runs | Observations (maximum) |
| --- | --- | --- |
| `s5timeline` | S5; `cand-timeline`; both workloads; 4 blocks, seeds 9601–9604 | 8 (12 with escalation) |
| Receiver CPU split | Readiness observations only | 0 |
| `presrss` | Conditional: per raised Reno-on-candidate RSS cell, 6 blocks × 2 arms | 12 per cell, at most 2 cells |
| `preslat` | Conditional: per raised Reno-on-candidate loopback latency cell, 6 blocks × 2 arms, 120 s window | 12, at most 1 cell |
| Total | | ≤ 48 plus reruns, ≤ 60 |

Contaminated or unusable attribution blocks follow the readiness rerun rule, and reruns count toward the cap. If the cap binds, stages run in the order listed, and any that cannot run end as **not run (cap)**. Attribution never sets a flag value.

## Stages not run

- **Memory attribution (`timeline`, `heapsites`) and the D2 re-attribution.** D6 assigns them to #740, on r9 against this record's flags, with a new memory-accounting instrument; heap-site shares left every cell unresolved on four records. Raised memory and S6 control p95 cells are reported here as **raised, attribution pending (#740)**.
- **`recvharness`.** D5 lists the readiness stage, the preservation stages and the S5 timeline only. On r8 the stage did not trigger: #736's readiness and #735's counted harness agreed that the S5 receiver had no CPU flag. A raised S5 receiver CPU cell on r9 is reported as unresolved, with the descriptive split.
- **#715's loopback-bottleneck stage and any loopback hand-off stage.** D5 does not list them; #738 attributed the loopback limit on r8. Raised loopback goodput or CPU cells are reported as unresolved.
- **macOS.** D8: no new-revision Mac readiness is required. r9's queue change is not platform-specific, so its macOS behaviour is unmeasured, not inferred.

## Disclosures made before data

- No observation of this ticket existed when this registration was written: no prerequisite, smoke, readiness or attribution run. The builds above were made before writing it; building produces no observation.
- **Prior r9 figures known when writing**, all from #738 on `minimax`, with `perf stat` attached, against frozen Reno or r8 in the same block, and none a readiness value:
  - loopback, instrumented arms (median of six blocks ÷ frozen Reno): r9 DATAGRAM 1.026 and STREAM 1.135, against r8's 0.864 and 0.938; sender CPU per useful GiB 0.910 and 0.936 of r8's; Reno on r9 1.003 and 1.009;
  - S5 screen (four blocks, r9 ÷ r8): goodput 1.008 (DATAGRAM) and 1.003 (STREAM); sender CPU per useful GiB 1.050 (DATAGRAM, 1.043–1.061 in all blocks) and 1.006 (STREAM);
  - Reno on r9, cycles per useful GiB against frozen Reno: sender 0.996, receiver 0.997.
- **#736's r8 readiness values** are known and are the comparison context named above. Nothing in this registration depends on them: the stages, limits and rules are #736's, unchanged.
- **Interpretation known in advance.** #738's loopback arms and readiness differ in instrumentation (the hand-off recorder cost about 2% of loopback goodput) and in `perf` attachment; loopback STREAM is sensitive to code placement at about 3% (#738). The readiness rule applies regardless.

## Time estimate

All aids exist and every binary is built, so no instrument development is expected. On a quiet host: prerequisites and smokes about 15 minutes; readiness about 70 minutes (#736's 110 observations) plus at least 15 minutes of quiet-host gating (one minute before each of 15 blocks); `s5timeline` about 10 minutes; conditional `preslat` about 30 minutes and `presrss` about 15 minutes per cell. **About 1 h 50 min without conditional stages, and up to about 2 h 50 min with all of them.** Foreign load extends the gate's waits and adds same-seed reruns; each readiness block rerun costs about 5–9 minutes.

## Order

On the Mac: `build.py` (done), `rules_test.py`, `harness_test.py`, `receiver_test.py`, `localize_test.py` (done); `sync.sh`. Then on `minimax`, sequentially under `taskset -c 1-3`, with no build or test running there while a stage measures: `prereq.py` (imported after `art`), `smoke`, `perfsmoke`, `ecnsmoke`, `xsmoke`; then `loopback`, `s5`, `s6`; any readiness reruns; `s5timeline` and any escalation; the conditional `presrss` and `preslat` cells, as the readiness flags require; attribution reruns. `matrix.py hostfacts` before and after each stage. Then `stages.py fold` on `minimax`, and `stages.py all`, `rules_test.py`, `receiver_test.py` and `harness_test.py` anywhere.

**Unattended run.** By operator choice, the order runs unattended from 05:30 America/New_York on October 8, 2026 (09:30 UTC), started by a systemd user timer on `minimax` that runs [driver.sh](driver.sh). The driver runs the prerequisites and smoke checks, the readiness stages, `s5timeline` and the conditional stages in the order above, and [follow.py](follow.py) applies the registered rules that would otherwise need a person between stages: same-seed reruns of contaminated or unusable blocks (the #714 rule, capped at ten readiness blocks and at the attribution cap), and the conditional preservation stages that the readiness flags trigger. Every decision it takes is written to `follow-decisions.json`. Before any data, `follow.py` was dry-run on the Mac against #736's retained observations with the runner stubbed out: a readiness block marked contaminated received exactly one rerun, and the conditional step derived #736's trigger (`preslat-datagram`) from its flags. Two things stay with the operator's session: the prerequisite review (the driver continues past the smoke checks unless one fails outright, so a review failure afterwards still ends the ticket as a prerequisite gap, and any later observations are reported as excluded), and the `s5timeline` escalation, which depends on that stage's verdicts.
