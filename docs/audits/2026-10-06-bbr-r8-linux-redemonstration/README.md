# Linux re-demonstration of the timing- and CPU-tested BBRv3 revision

**Date:** October 6, 2026, America/New_York. **Scope:** [Re-demonstrate the timing- and CPU-tested BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/736), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D5 ("Ticket E") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md#d5--ticket-e-linux-re-demonstration-then-a-fresh-decision), on the route "D keeps nothing → E re-demonstrates C's survivor if C kept a change". Measurement and attribution only: no production merge, transport source change, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r8-linux-redemonstration`, based on `f3840c83` (the [per-packet CPU-lead record](../2026-10-06-bbr-per-packet-cpu-leads/README.md)). The revision under test is **r8, `95f5b6b7`**: `d0fabc4d` plus the Linux netpoller kick at the BBR pacing deadline. The [pacing-wake ticket](../2026-10-05-bbr-pacing-wake/README.md) kept it by operator decision after a negative registered outcome (its deviation 3), and the [per-packet CPU-lead ticket](../2026-10-06-bbr-per-packet-cpu-leads/README.md) kept no lead on top of it. Its open cost carries forward: run-loop wakes per useful GiB rose 6–16% on S5. The [#715 re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) (`36f7ce01`) supplies the readiness stage, attribution stages, fixture, host layout and aids that this record reruns.

**Status: complete.** The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) were written, with the analysis code and its synthetic cases, before any prerequisite, smoke, readiness or attribution observation of this ticket, and committed (`333087fc`). They are unchanged. The [Answer](#answer), results, [Deviations](#deviations) and later sections were added afterwards.

## Question

On owned Linux hardware, what are the readiness flags of r8 (`95f5b6b7`), and what do registered stages show about S5 timing, memory composition, the D2 cells and any raised preservation cell?

## Answer

**Not ready on Linux. S5 is now clean, but loopback and memory flags remain, and one preservation cell was raised and did not reproduce.** On `minimax`, r8 delivers a larger S6 benefit than `d0fabc4d`: 14.02× (STREAM) and 14.58× (DATAGRAM) Reno's goodput, in all five pairs each. Compared with `d0fabc4d` in #715:

- **Every S5 goodput and CPU cell now passes.** Goodput is 0.972 in both workloads (was 0.917 / 0.899). Sender CPU per useful GiB is 0.935 / 0.909 (was 1.183 / 1.047), and receiver CPU is 1.055 / 1.007 (was 1.164 / 1.108). The readiness harness and #735's counted harness now agree that the S5 receiver has no CPU flag, so the conditional `recvharness` stage did not run.
- **S5 timing is no longer the gap; the model is.** The timeline stage reads **model behaviour** in all eight blocks. The sender still reaches about 27,300–28,800 full-quantum deadlines late per 30 s, but each by only about 7 µs: 0.19–0.22 s per run, against 2.8–3.1 s on `d0fabc4d`. Timing now accounts for 0.0002–0.001 of capacity. The remaining delivery deficit against capacity, 0.029–0.036, is mostly the model's own: 0.019–0.024, from the pacer's 0.99 margin, Down and ProbeRTT. That matches what #715 found on macOS for `d0fabc4d`.
- **Loopback STREAM goodput is newly raised** at 0.943 (four of five blocks; A/A 0.997–1.000; was 0.962). Loopback DATAGRAM keeps its goodput (0.854) and sender CPU (1.111) flags. No registered stage examines either, so both are unresolved.
- **One Reno-on-candidate preservation cell was raised and then not reproduced.** Loopback DATAGRAM control p95 for Reno built from r8 read 2.588 in readiness (three blocks; A/A 0.47–1.62). The registered 120 s latency stage (`preslat`, D3) reads **not reproduced**: pooled p95 ratio 0.716, 95% interval 0.487–1.180, absolute p95 0.444 ms against frozen Reno's 0.620 ms. The readiness flag stays as recorded. Every other Reno-on-candidate cell passes.
- **Memory.** Seven candidate memory cells are raised, all steady-state excesses, and all are **unresolved** under D5's four-block rule: in each, at least one heap-site block misses the 70% rule or names another group. The descriptive decomposition leans toward bookkeeping in the sender cells and S6 DATAGRAM receiver, and toward delivery data in the STREAM receiver cells.
- **S6 control p95** is raised in both workloads. STREAM (1.550) is again attributed to the selected ProbeBW Up policy. DATAGRAM (1.469, newly raised; was 1.167) shows the same queue pattern descriptively, but D2's rule covers only STREAM, so it is unresolved.

## Readiness results

Plain builds, five blocks per path and workload, 110 counted observations, all exiting cleanly and passing receiver integrity. **No observation was contaminated**, so no block was rerun and `summary.json` equals `summary-original.json` in every flag. There were no UDP errors and no relay stalls. Ratios are the median of per-block ratios against the same block's frozen Reno. Bold marks a raised flag; parentheses give the blocks crossing the limit; brackets give #715's `d0fabc4d` value. A/A per-block ranges are in [summary.json](summary.json) beside every ratio.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | **0.943 (4)** [0.962] | 1.095 (1) [1.085] | 1.044 [1.031] | 0.969 [0.958] | 1.087 (1) [1.078] | 1.171 (2) [1.035] |
| Loopback STREAM | Reno on candidate | 0.995 | 1.004 | 1.002 | 1.024 | 1.010 | 0.885 |
| Loopback DATAGRAM | Candidate | **0.854 (5)** [0.858] | **1.111 (5)** [1.111] | 1.054 [1.052] | 0.919 [0.931] | 1.045 [1.060] | 0.526 [0.652] |
| Loopback DATAGRAM | Reno on candidate | 1.001 | 0.999 | 1.002 | 1.013 | 0.984 | **2.588 (3)** [0.721] |
| S5 STREAM | Candidate | 0.972 [0.917] | 0.935 [1.183] | 1.055 [1.164] | **1.261 (5)** [1.235] | **1.565 (5)** [1.457] | 1.081 [1.056] |
| S5 STREAM | Reno on candidate | 1.000 | 1.006 | 0.999 | 1.046 (1) | 1.025 (1) | 1.006 |
| S5 DATAGRAM | Candidate | 0.972 [0.899] | 0.909 [1.047] | 1.007 [1.108] | **1.173 (3)** [1.096] | 1.086 (1) [1.058] | 0.987 [0.716] |
| S5 DATAGRAM | Reno on candidate | 1.000 | 1.002 | 1.002 | 1.066 (1) | 1.028 | 1.017 |
| S6 STREAM | Candidate | benefit 14.02× [12.18–15.51] [11.39×] | 0.087 | 0.073 | **1.656 (5)** [1.640] | **1.891 (5)** [1.759] | **1.550 (5)** [1.588] |
| S6 DATAGRAM | Candidate | benefit 14.58× [12.44–16.22] [10.81×] | 0.088 | 0.078 | **1.464 (5)** [1.403] | **1.118 (4)** [1.076] | **1.469 (5)** [1.167] |

The A/A frozen-Reno arm crosses no limit. Its widest ranges are loopback control p95: STREAM 0.68–1.85 and DATAGRAM 0.47–1.62.

Absolute medians (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4705.1 | 3.97 / 2.73 | 15.86 / 14.62 | 0.128 / 0.068 | — |
| Loopback STREAM | Candidate | 4434.4 | 4.34 / 2.86 | 15.32 / 15.89 | 0.164 / 0.073 | — |
| Loopback DATAGRAM | Reno | 4655.4 | 4.52 / 3.02 | 17.48 / 19.02 | 0.381 / 0.093 | — |
| Loopback DATAGRAM | Candidate | 3977.0 | 5.03 / 3.18 | 16.09 / 19.96 | 0.229 / 0.094 | — |
| Loopback DATAGRAM | Reno on candidate | 4659.0 | 4.52 / 3.02 | 17.73 / 18.99 | 0.606 / 0.103 | — |
| S5 STREAM | Reno | 95.9 | 10.15 / 8.46 | 17.82 / 16.82 | 182.9 / 176.9 | 0.095% |
| S5 STREAM | Candidate | 93.2 | 9.48 / 8.93 | 22.36 / 25.97 | 198.7 / 102.5 | 0.210% |
| S5 DATAGRAM | Reno | 94.4 | 11.34 / 8.73 | 18.00 / 14.08 | 181.1 / 175.4 | 0.432% |
| S5 DATAGRAM | Candidate | 91.8 | 10.26 / 8.78 | 20.90 / 15.32 | 179.0 / 102.4 | 0.026% |
| S6 STREAM | Reno | 6.1 | 112.18 / 122.56 | 13.50 / 13.75 | 125.4 / 105.7 | 0% |
| S6 STREAM | Candidate | 87.5 | 9.65 / 8.91 | 22.36 / 26.01 | 198.7 / 102.2 | 0.264% |
| S6 DATAGRAM | Reno | 5.9 | 120.28 / 117.68 | 14.00 / 13.59 | 121.6 / 103.5 | 0% |
| S6 DATAGRAM | Candidate | 86.3 | 10.61 / 9.16 | 20.99 / 15.20 | 177.9 / 102.0 | 0.037% |

### S5 CPU and goodput beside the earlier r8 measurements

These are context from other runs and harnesses, not evidence for a flag.

| Cell | `d0fabc4d` (#715) | r8 (this record) | #734 Stage 2 (r8 ÷ `d0fabc4d`, `perf stat`) | #735 (r8 ÷ Reno, `perf stat`) |
| --- | --- | --- | --- | --- |
| S5 goodput | 0.917 / 0.899 | 0.972 / 0.972 | deficit 0.088 / 0.113 → 0.026 / 0.030 | — |
| S5 sender CPU/GiB | 1.183 / 1.047 | 0.935 / 0.909 | −22% / −13% | whole run 0.90–0.91 / 1.00 |
| S5 receiver CPU/GiB | 1.164 / 1.108 | 1.055 / 1.007 | — | whole run 0.91–0.93; in-window task-clock 0.88–0.91 |

The readiness harness, with no `perf` attached, reads the same direction as the counted harnesses. The receiver CPU ratio is somewhat higher without `perf` (1.055 / 1.007 against 0.91–0.93), but both are under the limit, so `recvharness` did not trigger and this record does not test the difference.

## Attribution results

Run on October 7 (UTC), straight after readiness: `s5timeline` (8 observations), `timeline` (16), `heapsites` (32) and the conditional `preslat-datagram` (12). That is 68 Linux attribution observations, within the cap of 150. Every block was clean and none was rerun or escalated. `presrss` was not triggered (no Reno-on-candidate RSS cell raised), nor was `recvharness` (no S5 receiver CPU cell raised). [stages.py](stages.py) writes every result to [attribution.json](attribution.json).

### S5 timeline: model behaviour

Per block, fractions of the bottleneck's capacity over the measured window, from the [timeline overlay](timeline/).

| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | 0.029–0.036 | 0.031–0.039 | 0.019–0.024 | 0.0002–0.0003 | 0.012–0.015 | model ×4 → **model** |
| DATAGRAM | 0.030–0.035 | 0.029–0.034 | 0.019–0.024 | 0.0008–0.0010 | 0.008–0.009 | model ×4 → **model** |
| `d0fabc4d` STREAM (#715) | 0.078–0.087 | 0.069–0.077 | 0.015–0.019 | 0.045–0.048 | 0.010–0.011 | timing |
| `d0fabc4d` DATAGRAM (#715) | 0.098–0.112 | 0.089–0.101 | 0.010–0.019 | 0.072–0.074 | 0.007–0.008 | timing |

- **Stage verdict: model behaviour**, in both workloads. Under #715's rule the model accounts for at least half the attributed idle capacity and timing for under a quarter.
- **Lateness.** 27,326–27,465 (STREAM) and 28,680–28,805 (DATAGRAM) late arrivals at a full-quantum deadline per 30 s window. That is about as many events as on `d0fabc4d` (25,171–26,360), but total lateness is 0.19–0.22 s per run (2.8–3.1 s on `d0fabc4d`), about 7 µs per event, with maxima of 0.09–3.7 ms. The kick does not prevent the wake from coming after the deadline. It shortens the delay to a few microseconds, which costs almost no credit.
- **What the model share is.** The bandwidth estimate in Cruise is 1.006–1.007× the bottleneck. The pacing rate is 0.996× capacity in the median, with the pacer's 0.99 margin, Down (17–20% of the window) and ProbeRTT (3.4–4.3%) below capacity and Up (STREAM 13–17%, DATAGRAM 9–10%) above it. These make up the model's 0.019–0.024. No long inflight bound decreased in any window.
- **States.** The connection goroutine spends 0.92 (STREAM) and 0.95 (DATAGRAM) of the window waiting after paced stops, with congestion-window waits 0.053 / 0.024 and credit waits 0.002 / 0.011.
- **Instrument perturbation is negligible.** Instrumented goodput is 0.999 (STREAM) and 0.998 (DATAGRAM) of the readiness candidate's.
- **Context.** The S5 goodput cell passes, so this stage describes a passing cell. The remaining 0.03 deficit against capacity, and the 0.028 against frozen Reno, are the selected model's probing and margin, not a sender defect. #715's macOS timeline on `d0fabc4d` also read model; that comparison is from a different revision and is not a registered result here.

### Receiver CPU split

No receiver CPU cell is raised. The descriptive split puts the raised loopback DATAGRAM sender CPU cell **inside the window** (window ratio 1.109; 0.21 of the positive excess outside it, about the run's share outside the window), as on `d0fabc4d`.

### Memory: all raised cells unresolved

Each raised candidate memory cell is a steady-state excess: its measured-window footprint is 1.21–2.27× Reno's in both timeline blocks. Heap-site blocks (excess MiB at the measured-window heap peak) and the descriptive decomposition (median MiB per group over four blocks):

| Flag | Ratio | Per-block classification | Excess MiB per block | Decomposition: bookkeeping / delivery / other | Classification |
| --- | --- | --- | --- | --- | --- |
| S5 STREAM sender RSS | 1.261 | bookkeeping, unresolved, unresolved, bookkeeping | −0.0, −1.0, 6.7, 1.5 | 2.46 / −1.50 / 0.27 | **Unresolved** |
| S5 STREAM receiver RSS | 1.565 | unresolved ×3, delivery | 5.3, 7.7, 0.7, 6.0 | 1.13 / 4.02 / −0.02 | **Unresolved** |
| S5 DATAGRAM sender RSS | 1.173 | unresolved, bookkeeping ×3 | −0.5, 2.4, 3.2, 3.2 | 2.59 / 0.00 / −0.02 | **Unresolved** |
| S6 STREAM sender RSS | 1.656 | unresolved ×4 | 5.6, 5.9, 4.7, 2.7 | 2.03 / 2.25 / 0.00 | **Unresolved** |
| S6 STREAM receiver RSS | 1.891 | unresolved, unresolved, delivery, unresolved | 8.7, 2.6, 6.2, 6.7 | 1.16 / 5.25 / 0.02 | **Unresolved** |
| S6 DATAGRAM sender RSS | 1.464 | bookkeeping, unresolved, bookkeeping, unresolved | 4.2, 2.2, 2.4, 4.4 | 2.15 / 0.75 / 0.02 | **Unresolved** |
| S6 DATAGRAM receiver RSS | 1.118 | bookkeeping ×2, unresolved, bookkeeping | 1.2, 1.7, 1.5, 1.6 | 1.42 / 0.25 / 0.00 | **Unresolved** |

- **Why everything is unresolved.** D5's rule needs every usable block to meet the 70% rule with the same group. In no cell do all four blocks agree. Two cells have a block with a negative or near-zero heap-site excess at the heap peak, so the heap peak does not carry the RSS excess there. Heap-site resolution is about 0.5 MiB per site.
- **Descriptively,** bookkeeping leads the decomposition in every sender cell except S6 STREAM (an even split) and in the S6 DATAGRAM receiver. Delivery data leads in both STREAM receiver cells. The bookkeeping sites are #715's: `recoveryEvidence.sent`, `beginCongestionFeedback` and `deliveryRecords`. The decomposition classifies nothing.
- **Compared with #715.** Two cells that #715 classified or passed change: S6 STREAM receiver RSS was *delivery data* over two blocks (1.759) and is unresolved over four (1.891); S5 DATAGRAM sender RSS and S6 DATAGRAM receiver RSS passed on `d0fabc4d` (1.096, 1.076) and are raised on r8 (1.173, 1.118).

### Preservation: loopback DATAGRAM control latency not reproduced

`preslat-datagram` (D3; six blocks, 120 s window, at least 100 replies per run): pooled p95 ratio (Reno on r8 ÷ frozen Reno) **0.716**, 95% block-bootstrap interval **0.487–1.180**. Per block 0.92, 1.83, 0.64, 0.56, 1.08 and 0.38, so two of six are above 1. Absolute p95 0.444 ms against 0.620 ms; p50 0.085 ms against 0.088 ms. The interval's upper bound is at most 1.20, so the rule reads **not reproduced**. The readiness flag (2.588, 20 replies per run) stays raised as recorded. In #715, frozen Reno's own A/A arm crossed this cell's limit at 1.615.

### D2 cells, re-attributed on r8

| Cell | Linux `d0fabc4d` (#715) | Linux r8 |
| --- | --- | --- |
| S6 control p95 | STREAM 1.588, Up policy; DATAGRAM passes (1.167) | STREAM **1.550**: 98.2–99.3% of samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay 0.23 ms; S5 matched-load p95 1.081 / 0.987. **Up policy**, slightly smaller. DATAGRAM **1.469**, raised: descriptively the same pattern (98.3–98.9% in Up or Down; Cruise and Refill 0.15–0.20 ms) but outside D2's rule, so **unresolved**. |
| S6 STREAM receiver RSS | 1.759, delivery | **1.891, unresolved** (one of four blocks delivery; decomposition delivery-led) |
| S5 sender RSS | STREAM 1.235 unresolved; DATAGRAM passes (1.096) | STREAM **1.261, unresolved**; DATAGRAM **1.173, unresolved** |

Under D2, a larger magnitude or changed composition gets no clearance from a cause label alone.

## Flag report for the next decision

For [Decide whether the timing- and CPU-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/737), on candidate r8 (`95f5b6b7`), Linux (`minimax`):

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 14.02× (STREAM) and 14.58× (DATAGRAM) Reno goodput, 5/5 pairs each, at 0.087 / 0.088× Reno sender CPU per useful GiB. |
| **Passed** | Every S5 goodput, CPU and control p95 cell. Loopback STREAM CPU, RSS and p95; loopback DATAGRAM receiver CPU, RSS and p95. S5 DATAGRAM receiver RSS. S6 CPU. Every Reno-on-candidate cell except one. |
| **Raised with attribution** | S6 STREAM control p95 1.550: selected ProbeBW Up policy. |
| **Raised and unresolved** | Loopback STREAM goodput 0.943 (new). Loopback DATAGRAM goodput 0.854 and sender CPU 1.111 (inside the window). S6 DATAGRAM control p95 1.469 (new; descriptively the Up pattern). Memory: S5 STREAM sender 1.261 and receiver 1.565, S5 DATAGRAM sender 1.173 (new), S6 STREAM sender 1.656 and receiver 1.891, S6 DATAGRAM sender 1.464 and receiver 1.118 (new). |
| **Raised (preservation)** | Loopback DATAGRAM Reno-on-candidate control p95 2.588 (three blocks, 20 replies per run): **not reproduced** by D3's 120 s stage (0.716, interval 0.487–1.180). |
| **Not measured here** | The open cost carried from #734 (run-loop wakes per useful GiB, +6–16% on S5); r8 readiness on macOS. |

## Deviations

1. **Unbooked host.** The registration's [Order](#order) called for an operator-booked quiet window. The operator chose to start without a booking, under the registered contamination rule. A qemu VM ran during the first three excluded smoke runs (up to 0.94 foreign cores); it exited before readiness began, and no counted observation was contaminated. A small unpinned wait script polled the stage log every 20 s to queue the conditional stage; the contamination test found no foreign load in any counted block.

No rule, threshold, seed, arm or cap changed after data.

## Preservation

- **Payload integrity.** All 187 retained observations exited cleanly and passed receiver integrity: 110 readiness, 68 attribution and 9 excluded prerequisite and smoke runs.
- **Default Reno.** Every Reno-on-candidate cell passes except loopback DATAGRAM control p95, which D3's stage did not reproduce. Medians lie within 1.006 on CPU and 1.066 on RSS, and goodput lies at 0.995–1.001.
- **No code change.** No transport source changed. r8's gates are #734's. No recorded translation, cap, evidence contract, controller version or default changed. The overlays exist only in their measurement builds.
- **No host change.** `perf_event_paranoid`, socket-buffer limits, offloads, the governor, EPP, boost and idle states were left unchanged; all 20 `hostfacts` snapshots are identical.

## Limits

- **Platform and topology.** One Linux host, loopback endpoints and a userspace relay. This certifies no other platform, no real carrier path and no production readiness. r8's macOS readiness is unmeasured; r8's change is Linux-only.
- **Repetition.** Five blocks per readiness cell; two to six per attribution stage. Heap-site resolution is about 0.5 MiB.
- **Loopback.** No registered stage examined the loopback goodput and CPU flags. Whether the kick or the send-queue/connection-loop hand-off underlies the new loopback STREAM goodput flag is untested.
- **Engagement.** No queue CE-marked any packet, so the CE response stays unengaged. C4 items 1 and 3 and the low-rate pacing floor were not examined.

## Assets and reconstruction

- **Results:** [summary.json](summary.json) (registered readiness; no rerun was needed), [summary-original.json](summary-original.json) (#712's rule; identical flags) and [attribution.json](attribution.json) (prerequisites, readiness blocks, CPU split, `s5timeline`, memory, `preslat`, `presrss` and `recvharness` as not triggered, D2).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json): every observation, prerequisite, stage log, stage driver, `hostfacts` snapshot and build receipt. Binaries, credentials and exported source trees are omitted.
- **To reconstruct**, in a fresh owned worktree of this branch: run `build.py`, then `sync.sh` to a Linux host with the same CPU layout; under `taskset -c 1-3`, run `prereq.py` (imported after `art`), the smoke stages, then the stages in [Order](#order) with `matrix.py hostfacts` around each; then `stages.py fold` there, and `stages.py all`, `rules_test.py`, `receiver_test.py` and `harness_test.py` anywhere.

## What stays fixed and what changes

- **Fixed (D5):** #715's readiness stage, unchanged except for the candidate revision: the same arms, paths, durations, seeds, rotation, flag limits, median rule, A/A reporting, contamination rule (the operator's #714 rerun rule, as #715 adopted it) and core layout. Frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; the fixture with the causal-diagnosis heap patch; the relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter); the launcher; the endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz); `minimax`'s core layout (sender 8–11, receiver 12–15, relay 4–5, runner and `perf` 1–3, SMT siblings idle) and host settings; `GOTOOLCHAIN=go1.27.0`; #711's memory and S6-latency rules; #715's timeline rule; D3's preservation rules as #715 registered them.
- **Changed, all recorded:**
  - *Candidate:* `cand` is r8, `95f5b6b7`. There is no `prev` arm, because this ticket has no loopback-bottleneck stage (D5 does not list one).
  - *Builds:* [build.py](build.py) is #715's with the changes in its docstring. Rebuilt here, frozen Reno (`cda8feb1…`), `reno-diag` (`36752495…`), both relays (`033d467c…`, `24ee941d…`), the calibration aid (`4218b433…`) and the launcher (`a6146d62…`) are **byte-identical** to #715's binaries, and `cand` is byte-identical to #734's and #735's r8 build (`b7171142…`). The r8 variants are `cand-diag` (`1e017bf4…`), `cand-diag-counted` (`43cc1fcb…`) and `cand-timeline` (`bc8f12e5…`). The `-diag`, `-counted` and timeline overlays apply to r8 unchanged; r8 touches none of the lines they patch.
  - *Heap-site blocks:* four per path and workload instead of two (D5), seeds 9311–9314 (S5) and 9411–9414 (S6). Blocks 1–2 reuse #711's and #715's seeds.
  - *New conditional stage:* `recvharness`, below, which acts on #735's receiver measure because that measure was informative.
  - *Not run, with reasons:* #715's loopback-bottleneck stage (`lbneck`) and macOS timeline, and #734's loopback timing rule; see [Stages not run](#stages-not-run).
  - *Aids:* copied byte-identical and checked by `build.py`: #712's [run.py](run.py), [analyze.py](analyze.py), [attribution.py](attribution.py), [localize.py](localize.py), [localize_test.py](localize_test.py), [prereq.py](prereq.py), [pack.py](pack.py), [calibrate/](calibrate/), [launch/](launch/), [relay/](relay/), [overlay/](overlay/), [counting/](counting/), [model-overlay/](model-overlay/); #715's [rules.py](rules.py), [rules_test.py](rules_test.py), [stage_run.py](stage_run.py), [timeline/](timeline/) and [go.mod](go.mod); #735's [receiver.py](receiver.py) and [receiver_test.py](receiver_test.py). Adapted from #715 with recorded changes: [art.py](art.py), [matrix.py](matrix.py), [stages.py](stages.py), [sync.sh](sync.sh). New: [harness.py](harness.py) with [harness_test.py](harness_test.py) (10 cases, all passing).
- **Gates.** r8 is unchanged from #734, whose gates passed before any of its measurement: Mac (`gates/r8.log`) and native Linux (`gates/r8-native.log`, `gates/r8-native-full.log`) in the [pacing-wake record](../2026-10-05-bbr-pacing-wake/gates/). No transport source changes here, so no gate is rerun.

## Prerequisites

D5 requires the applicable #712 checks before any comparison; a failure ends the ticket as a reported prerequisite gap. They are #715's, through [stages.py](stages.py) `prereq`, on a quiet host, each run excluded from every statistic:

| Check | Criterion |
| --- | --- |
| ECN calibration ([prereq.py](prereq.py)) | Unadapted relay strips ECN (negative control); adapted relay preserves all four codepoints both ways |
| ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`) | Forward and reverse packets after the handshake carry ECT(0) on S5/S6; BBR ECN tracker frames in the candidate sender's samples |
| Native I/O (`perfsmoke`) | One send syscall and one transmit per measured-window forward packet: no GSO or `sendmmsg` batching |
| Resources | Endpoint and relay threads inside their CPU sets; 8 MiB effective socket buffers; no UDP errors |
| Instruments | `perf stat` events at ≥ 95% running; `perf record` resolves Go frames |
| Timeline overlay (`xsmoke`) | The overlay covers the measured window, with a bandwidth estimate near the bottleneck |
| Receiver instruments (`rsmoke`) | `task-clock`, `msr/aperf/`, `msr/mperf/` at ≥ 95% running alongside #712's sixteen events; idle snapshots written for both endpoints |

#715's goroutine-stage and thread-sampling checks served only its loopback-bottleneck stage and are not repeated. The `rsmoke` check serves only the conditional `recvharness` stage; if it fails, that stage ends as an instrument gap and the others proceed. `matrix.py hostfacts` records the governor, EPP, boost, idle states and other host settings read-only at the start and end of every stage.

## Registration: readiness

#712's readiness stage as #715 ran it, unchanged except for the candidate revision: arms Reno, A/A Reno, candidate (r8, BBRv3) and Reno on candidate on loopback and S5, and Reno, A/A and candidate on S6; five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations through #712's unchanged `run_case` ([matrix.py](matrix.py) stages `loopback`, `s5`, `s6`). Flags are #712's ([analyze.py](analyze.py), unchanged): per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20; on S6 the useful benefit is repeatable when the candidate beats Reno in all five pairs. Reno on candidate is held to the same limits as a preservation check. The A/A arm's per-block ratios are reported beside every ratio and never override the rule.

**Contamination.** #712's test ([localize.py](localize.py) `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample), with #715's handling: a block (all arms of one path, workload and block) holding a contaminated or unusable observation is rerun once with the same seed under `observations-rerun`, and the rerun replaces the original only if it is usable and uncontaminated ([rules.py](rules.py) `choose_block`). `summary.json` applies that rule; `summary-original.json` applies #712's rule to the original blocks, and any flag that differs between the two is reported. At most ten readiness blocks are rerun; beyond that, the remaining contaminated blocks are reported under #712's rule.

**Reporting.** Beside each flag: #715's value on `d0fabc4d`, and, for S5 CPU and goodput, #734's Stage 2 and #735's counted figures for r8. These come from other runs and other harnesses, so they are context, not evidence for a flag. A CPU flag that falls but stays above its limit without a cause remains unresolved. These are Linux results; new-revision Mac readiness stays unmeasured, and supported-platform correctness remains required.

## Registration: attribution stages

Each stage names its question, hypothesis, competitor, distinguishing observations, maximum, escalation trigger and inconclusive ending, following #715. The analysis is [stages.py](stages.py); the rules are #715's [rules.py](rules.py) (43 synthetic cases, passing) and this ticket's [harness.py](harness.py) (10 cases, passing). Contaminated or unusable blocks follow the readiness rerun rule, and reruns count toward the cap. Attribution never sets a flag value.

**Cap.** At most **150** Linux attribution observations, reruns and escalations included. #715's cap was 120; D5 doubled the heap-site blocks (16 more observations) and added the receiver measure, so the cap rises by the new stages' maxima. If the cap binds, conditional stages run in the order listed below, and any that cannot run end as **not run (cap)**.

| Stage | Runs | Observations (maximum) |
| --- | --- | --- |
| `s5timeline` | S5; `cand-timeline`; both workloads; 4 blocks, seeds 9601–9604 | 8 (12 with escalation) |
| `timeline` | #711's runs and seeds: S5 9301–9302, S6 9401–9402; `reno-diag` and `cand-diag-counted` | 16 |
| `heapsites` | S5 9311–9314, S6 9411–9414; same arms, heap profiles every second | 32 |
| Receiver CPU split | Readiness observations only (#715's rule) | 0 |
| `recvharness` | Conditional: per raised S5 receiver CPU cell, 4 blocks × 4 arms, seeds 10601–10604 | 16 per cell, at most 2 cells |
| `presrss` | Conditional: per raised Reno-on-candidate RSS cell, 6 blocks × 2 arms (D3) | 12 per cell, at most 2 cells |
| `preslat` | Conditional: per raised Reno-on-candidate loopback latency cell, 6 blocks × 2 arms, 120 s window (D3) | 12, at most 1 cell |
| D2 cells | From `timeline`, `heapsites` and readiness | 0 |
| Total | | ≤ 124 plus reruns, ≤ 150 |

### Stage `s5timeline`: S5 timing

#715's stage, unchanged: question, hypothesis (model behaviour), competitor (sender timing), instrument (#715's measurement-only [timeline overlay](timeline/) in `cand-timeline`), runs, rule ([rules.py](rules.py) `timeline_decomposition`, `timeline_verdict`, `workload_verdict`, `stage_verdict`; the fluid-link decomposition; *no deficit* under 0.02; the unusable tests; the three-of-four majority), descriptive figures, escalation (a workload with exactly two of four agreeing usable blocks gets blocks 5–6, seeds 9605–9606, once) and inconclusive ending. See [#715's registration](../2026-10-05-bbr-linux-redemonstration/README.md#stage-s5timeline-the-linux-s5-goodput-gap). The overlay's lateness count is the quantity #734's kick shortened. The descriptive figures add lateness events and total lateness per run beside #715's `d0fabc4d` values. The macOS comparison is not run ([Stages not run](#stages-not-run)).

### Receiver CPU: measured window against whole run

#715's split, unchanged and with no new runs ([rules.py](rules.py) `cpu_split`, `receiver_localization`): for a raised S5 receiver CPU flag, **inside window** when the window ratio alone exceeds 1.10 (median); **outside window** when it does not and at least half of the positive excess lies outside the window; otherwise **mixed**. Sender cells are reported descriptively. This is localization, not cause.

### Stage `recvharness`: the readiness and counted receiver harnesses (conditional)

#735's receiver measure was informative. On r8 with `perf stat` attached, the S5 receiver used 0.88–0.91× frozen Reno's in-window task-clock per useful GiB and 0.91–0.93× its whole-run CPU per useful GiB. The readiness measure is whole-run CPU per useful GiB with no `perf` attached. #735 stated that attaching `perf` adds a cost at every context switch, and its r8 receiver switched 0.80–0.81× as often per GiB as Reno's.

- **Trigger.** Runs only for a raised S5 receiver CPU cell of the candidate (STREAM, DATAGRAM, or both), after the readiness stage. If none is raised, the two harnesses agree that the cell passes, and the stage does not run.
- **Question.** Does the readiness flag come from the harness, or is it an excess that #735's counted runs did not reproduce?
- **Hypothesis: perf attachment.** Attaching `perf` to both endpoints lowers the candidate-to-Reno receiver CPU ratio, because its per-switch cost lands more heavily on Reno's receiver, which switches more often per GiB. **Competitor: excess under both harnesses.** The excess appears with and without `perf` in the same blocks; #735's lower figure then reflects its own runs, not the instrument.
- **Runs.** The raised workload on S5, four blocks, seeds 10601–10604, #712's rotation over four arms: frozen Reno and r8 (BBRv3), each plain (no `perf`, as in readiness) and each with #735's `perf stat` (#712's sixteen events plus `task-clock`, `msr/aperf/` and `msr/mperf/`, with per-core idle snapshots, attached to both endpoints for the measured window; [matrix.py](matrix.py) injects them only for this stage, exactly as #735's matrix did).
- **Measures,** per usable block (all four arms present, usable and uncontaminated): P = plain candidate ÷ plain Reno whole-run receiver CPU per useful GiB (the readiness measure); Q = the same ratio between the `perf` arms; W = the `perf` arms' in-window task-clock ratio (#735's measure, used only when its three events reach 95% running).
- **Rule** ([harness.py](harness.py) `harness_verdict`), at least three usable blocks, otherwise an **evidence gap**:
  - **not reproduced** when the median P is at most 1.10;
  - **perf attachment** when the median P exceeds 1.10, the median Q does not, and P exceeds Q in all usable blocks but at most one;
  - **excess under both harnesses** when both medians exceed 1.10;
  - otherwise **inconclusive**.
- **Descriptive, never decisive:** W; #735's component decomposition of the `perf` arms (work, cpi, uncounted, clock, closure; [receiver.py](receiver.py) `components`) with no A/A arm, so it gives no #735 verdict; context switches per GiB; APERF ÷ MPERF; idle-state residency; the sender's P and Q; and goodput ratios in both conditions.
- **Limits.** The readiness flag stays as recorded whatever this stage finds. A *perf attachment* outcome labels the disagreement between the two harnesses. It does not show that the readiness measure is wrong, and it clears nothing.
- **Escalation.** None. **Inconclusive ending:** reported as inconclusive.

### Stages `timeline` and `heapsites`: memory

#711's rules, runs and seeds, unchanged, through [attribution.py](attribution.py) (`reno-diag` against `cand-diag-counted`, S5 and S6, both workloads), with #715's pattern addition (`ackhandler.(*deliveryRecords)` counts as bookkeeping). The timeline stage keeps #711's two blocks. The heap-site stage runs **four blocks** (D5) through [stages.py](stages.py) `heapsites4`, a copy of `attribution.heapsites` whose only change is the block list. Every raised candidate memory flag is classified first as a Startup transient, steady-state excess or mixed (timeline), then by heap site as delivery data or BBR bookkeeping, with 70% of the positive excess to lead in each block. A steady-state or mixed cell is classified only when **every usable block** meets the 70% rule with the same group, and at least three blocks are usable ([harness.py](harness.py) `heap_cell`); otherwise it is **unresolved**. A **descriptive decomposition** (the median excess MiB per group, delivery, bookkeeping and other, over the usable blocks) is reported for every raised memory cell. The decomposition never classifies a cell.

### Stages `presrss` and `preslat`: preservation (conditional, D3)

#715's stages and rules, unchanged ([#715's registration](../2026-10-05-bbr-linux-redemonstration/README.md#stage-presrss-reno-on-candidate-rss-conditional-d3)): `presrss` for a raised Reno-on-candidate RSS cell (at most two; six blocks; seeds 9801–9806 on S5, 9901–9906 on S6; [rules.py](rules.py) `rss_preservation`), `preslat` for a raised Reno-on-candidate loopback control p95 cell (at most one; six blocks; a 120 s window; `latency_preservation`). A raised Reno-on-candidate p95 cell on S5 or S6 has no stage and is reported as an evidence gap. A raised Reno-on-candidate CPU or goodput cell has no stage here either. It is reported as an unattributed preservation flag that keeps blocking, beside #734's and #735's measurements of Reno on r8 within frozen Reno's A/A range.

### D2 cells

Re-attributed on r8 under their registered rules, with no new runs. S6 STREAM control p95 is attributed to the selected ProbeBW Up policy when at least 70% of forward-queue samples above 25 ms fall in Up or the following Down, Cruise and Refill median queue delay is below 1 ms, and the readiness S5 matched-load p95 is within 1.20. S6 STREAM receiver RSS and S5 sender RSS come from the memory stages. Under D2, a larger magnitude, a different cause or a changed composition gets no clearance from a cause label alone.

## Stages not run

- **#734's loopback timing rule.** D5 runs it only if #734 found *timing-limited goodput*. It found **no timing exposure** in all four blocks: the sender never reached a paced stop on loopback DATAGRAM. r8's kick acts only at a pacing deadline, so it has nothing to act on there.
- **#715's loopback-bottleneck stage (`lbneck`).** D5 does not list it. The loopback DATAGRAM flags, if raised, are reported as unresolved.
- **macOS timeline.** r8's change is Linux-only: on other platforms `pacingKick` is a no-op type (`pacing_kick_other.go`), so the darwin build behaves as `d0fabc4d`, whose macOS timeline #715 measured (model, both workloads). Rerunning it would duplicate #715.

## Disclosures made before data

- No observation of this ticket existed when this registration was written: no prerequisite, smoke, readiness or attribution run.
- **Prior r8 figures known when writing.** #734 Stage 2 (S5, against `d0fabc4d`, `perf stat` attached): goodput deficit against frozen Reno 0.026 (STREAM) and 0.030 (DATAGRAM); lateness 0.08–0.09 of `d0fabc4d`'s; sender CPU per useful GiB −22% / −13%. #735 (S5, `perf stat` attached): r8's receiver 0.88–0.91× Reno's in-window task-clock and 0.91–0.93× its whole-run CPU per GiB; the sender 1.16–1.18× Reno's cycles per GiB but 0.90–1.00× its whole-run CPU per GiB. None is a readiness value. The `recvharness` trigger and rule were written knowing them, and its thresholds are #712's limit (1.10) and #715's three-of-four majority.

## Order

`prereq.py`, `smoke`, `perfsmoke`, `ecnsmoke`, `xsmoke`, `rsmoke`; then `loopback`, `s5`, `s6`; any readiness reruns; `s5timeline`, `timeline-S5`, `timeline-S6`, `heapsites-S5`, `heapsites-S6`; the conditional `recvharness`, `presrss` and `preslat` cells, as the readiness flags require; attribution reruns and escalations. Every Linux stage runs sequentially under `taskset -c 1-3` in an operator-booked quiet window, with `matrix.py hostfacts` before and after each stage, and no build or test runs on `minimax` while a stage measures. Then `stages.py fold` on `minimax`, and `stages.py all`, `rules_test.py`, `receiver_test.py` and `harness_test.py` anywhere.
