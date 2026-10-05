# Linux re-demonstration of the surviving BBRv3 revision

**Date:** October 5, 2026, America/New_York. **Scope:** [Re-demonstrate the surviving BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/715), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies Ticket B of D4 in the [accepted Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md#d4--next-test-the-per-packet-leads-by-intervention-then-re-demonstrate-on-linux), on the "A keeps one or more changes" route. Measurement and attribution only: no production merge, transport source change, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-redemonstration`, based on `fb3261f4` (the [per-packet intervention record](../2026-10-03-bbr-per-packet-interventions/README.md), which contains the surviving revision `d0fabc4d`). The [Linux diagnostic](https://github.com/the-sarge/quic-go-fast/blob/4323dae8/docs/audits/2026-10-03-bbr-linux-diagnostic/README.md) (`4323dae8`) supplies the readiness stage, fixture, host layout and aids this record reruns.

**Status: complete.** The registration sections (from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order)) were written, with the analysis code and its synthetic cases, and committed (`a7bbe25f`) before any readiness or attribution observation existed. They are unchanged. The [Answer](#answer), results, [Deviations](#deviations) and later sections were added afterwards.

## Question

On owned Linux hardware, what are the readiness flags of the surviving revision `d0fabc4d` (r6), and what do registered discriminating stages show about the loopback bottleneck, the Linux S5 goodput gap, receiver CPU, memory and the preservation cells?

r6 holds all five per-packet interventions: four kept by rule (r5, `2d777bb0`) and the capability snapshot kept by operator decision. r6 as a whole is unmeasured. On S5, r5 runs 0.899 (STREAM) and 0.912 (DATAGRAM) of `fc4c1bf1`'s sender user instructions per forward packet. The remaining excess over frozen Reno is 6,736 and 5,219 user instructions per packet, unattributed.

## Answer

**Not ready on Linux, but much closer, and no preservation flag remains.** On `minimax`, the surviving revision `d0fabc4d` keeps its useful S6 benefit: 11.39× (STREAM) and 10.81× (DATAGRAM) Reno's goodput, in all five pairs each. Compared with `fc4c1bf1` in #712, the flags change as follows.

- **Loopback STREAM now passes every limit.** Goodput is 0.962 (was 0.770) and sender CPU 1.085 (was 1.261). Loopback DATAGRAM improves but still raises goodput (0.858, was 0.795) and sender CPU (1.111, was 1.208).
- **No Reno-on-candidate preservation flag is raised.** #712's two cells are not raised on `d0fabc4d`: S5 DATAGRAM sender RSS is 1.002 and loopback DATAGRAM control p95 is 0.721. So the conditional preservation stages did not run. In the same cell, the A/A frozen-Reno arm itself crossed the latency limit (1.615), which confirms that a 20-reply p95 cannot judge that cell.
- **S5 goodput is unchanged, and on Linux it is sender timing, not the model.** It is 0.917 (STREAM) and 0.899 (DATAGRAM). The timeline stage gives **sender timing** in both workloads (DATAGRAM 4/4 blocks, STREAM 3/4 plus one mixed). The model asks for about capacity: its bandwidth estimate is 1.007–1.009× the bottleneck in Cruise, and its own shortfall is 0.010–0.019 of capacity. The sender reaches its full-quantum pacing deadline late about 25,000–26,000 times in 30 s, roughly 0.11 ms each, about 2.8–3.1 s per run. The pacer discards that credit. This accounts for 0.045–0.048 (STREAM) and 0.072–0.074 (DATAGRAM) of capacity, of a 0.078–0.112 delivery deficit. On the Mac the same instrument gives **model** in both workloads, with about 250 late events per run and negligible timing loss. That is a **different** result. It cannot by itself establish a Linux-specific cause.
- **The loopback bottleneck is inconclusive under the registered rule.** In every block, both BBR arms' busiest sender serial stage sits between 0.70 and 0.85 cores (send queue 0.66–0.81, connection loop 0.57–0.71), and the receiver is unsaturated (0.33–0.40). Frozen Reno's send queue saturates at 0.88. Descriptively, `d0fabc4d` runs 0.871 (STREAM) and 0.926 (DATAGRAM) of `fc4c1bf1`'s sender cycles per packet, with 1.255× and 1.070× its goodput. That fits a sender limit, but it does not meet the saturation test, so no verdict is drawn.
- **Remaining CPU flags are inside the measured window.** S5 STREAM sender CPU 1.183, S5 receiver CPU 1.164 (STREAM) and 1.108 (DATAGRAM), and loopback DATAGRAM sender CPU 1.111 are all raised and unresolved. Their excess lies inside the window: window ratios are 1.117–1.194, and the share outside the window (0.21) matches the share of time outside it. S5 DATAGRAM sender CPU now passes (1.047, was 1.108).
- **Memory.** S5 DATAGRAM sender RSS now passes (1.096). S6 STREAM receiver RSS (1.759) is delivery data in both heap-site blocks. The other raised memory cells (S5 STREAM sender 1.235 and receiver 1.457, S6 STREAM sender 1.640, S6 DATAGRAM sender 1.403) are steady-state excesses whose two heap-site blocks disagree, so they are unresolved. S6 STREAM control p95 (1.588) is again attributed to the selected ProbeBW Up policy.

## Readiness results

Plain builds, five blocks per path and workload, 110 counted observations, all exiting cleanly and passing receiver integrity. S5 STREAM blocks 4 and 5 held observations contaminated by a qemu VM (up to 0.66 foreign cores mean, 1.31 in one second). Under the registered rule, both blocks were rerun once with the same seeds on a quiet host. Both reruns were clean and are counted. Under #712's rule (originals counted, `summary-original.json`), **no flag changes status**; the largest movement is S5 STREAM receiver CPU, 1.148 against 1.164. No other observation was contaminated, and there were no UDP errors and no relay stalls. Ratios are the median [min–max] of per-block ratios against the same block's frozen Reno. Bold marks a raised flag; parentheses give the blocks crossing the limit. A/A per-block ranges are in [summary.json](summary.json) beside every ratio.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | 0.962 | 1.085 | 1.031 | 0.958 | 1.078 | 1.035 (1) |
| Loopback STREAM | Reno on candidate | 0.999 | 1.003 | 1.001 | 1.018 | 0.989 | 0.932 (2) |
| Loopback DATAGRAM | Candidate | **0.858 (5)** | **1.111 (5)** | 1.052 | 0.931 | 1.060 | 0.652 |
| Loopback DATAGRAM | Reno on candidate | 0.995 | 1.006 | 1.009 | 0.992 | 1.031 | 0.721 (1) |
| S5 STREAM | Candidate | **0.917 (5)** | **1.183 (5)** | **1.164 (5)** | **1.235 (5)** | **1.457 (5)** | 1.056 |
| S5 STREAM | Reno on candidate | 1.000 | 1.018 | 1.008 | 0.997 | 1.000 | 1.003 |
| S5 DATAGRAM | Candidate | **0.899 (5)** | 1.047 | **1.108 (4)** | 1.096 (2) | 1.058 | 0.716 |
| S5 DATAGRAM | Reno on candidate | 1.000 | 1.009 | 0.995 | 1.002 (2) | 0.995 | 0.991 |
| S6 STREAM | Candidate | benefit 11.39× [9.44–13.22] | 0.119 | 0.084 | **1.640 (5)** | **1.759 (5)** | **1.588 (5)** |
| S6 DATAGRAM | Candidate | benefit 10.81× [9.73–12.57] | 0.127 | 0.096 | **1.403 (5)** | 1.076 | 1.167 (2) |

The A/A frozen-Reno arm crosses one limit: loopback DATAGRAM control p95, 1.615 (three blocks; A/A range 0.48–2.21). Every other A/A median lies within 1.00 ± 0.04.

Absolute medians (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4677.8 | 3.97 / 2.75 | 16.74 / 15.36 | 0.127 / 0.067 | — |
| Loopback STREAM | Candidate | 4494.9 | 4.31 / 2.83 | 15.96 / 16.45 | 0.145 / 0.080 | — |
| Loopback DATAGRAM | Reno | 4637.3 | 4.53 / 3.02 | 18.02 / 19.50 | 0.409 / 0.089 | — |
| Loopback DATAGRAM | Candidate | 3980.9 | 5.03 / 3.18 | 16.81 / 20.23 | 0.256 / 0.095 | — |
| S5 STREAM | Reno | 95.9 | 10.11 / 8.49 | 18.88 / 17.63 | 186.4 / 181.2 | 0.111% |
| S5 STREAM | Candidate | 87.9 | 11.96 / 9.80 | 22.95 / 25.61 | 199.9 / 102.2 | 0.209% |
| S5 DATAGRAM | Reno | 94.4 | 11.38 / 8.75 | 18.63 / 14.88 | 178.0 / 172.0 | 0.402% |
| S5 DATAGRAM | Candidate | 84.9 | 11.88 / 9.68 | 20.42 / 15.80 | 126.2 / 101.7 | 0.031% |
| S6 STREAM | Reno | 6.1 | 107.70 / 117.18 | 14.13 / 14.13 | 128.2 / 105.0 | 0% |
| S6 STREAM | Candidate | 70.3 | 13.07 / 10.28 | 23.17 / 26.17 | 200.3 / 102.1 | 0.326% |
| S6 DATAGRAM | Reno | 5.9 | 119.16 / 118.59 | 14.81 / 14.55 | 122.5 / 103.3 | 0% |
| S6 DATAGRAM | Candidate | 66.7 | 14.65 / 10.96 | 20.62 / 15.66 | 146.3 / 102.1 | 0.039% |

### CPU flags beside Ticket A's counter effects

The readiness columns are separate runs of the same stage (#712 on `fc4c1bf1`, this record on `d0fabc4d`). Ticket A's figures are r5 ÷ `fc4c1bf1` on S5 measured-window counters (#714 m6). r6 adds intervention 4, whose own effect (m4) was 0.993 / 0.998 in user instructions per packet and 0.969 / 0.986 in user cycles.

| Cell | `fc4c1bf1` (#712) | `d0fabc4d` (this record) | Ticket A: user instructions/packet; measured-window sender cycles/GiB vs Reno |
| --- | --- | --- | --- |
| S5 STREAM sender | 1.237 | **1.183** | 0.899; 1.224 → 1.139 |
| S5 DATAGRAM sender | 1.108 | 1.047 (passes) | 0.912; 1.135 → 1.093 |
| Loopback STREAM sender | 1.261 | 1.085 (passes) | not measured; `lbneck` cycles per packet 0.871 of `fc4c1bf1` |
| Loopback DATAGRAM sender | 1.208 | **1.111** | not measured; `lbneck` 0.926 |
| S5 STREAM receiver | 1.155 | **1.164** | receiver cycles/GiB vs Reno 1.038 → 1.001 |
| S5 DATAGRAM receiver | 1.131 | **1.108** | 1.022 → 1.011 |
| Loopback STREAM receiver | 1.189 | 1.031 (passes) | not measured |

- **The S5 DATAGRAM divergence is gone.** #714 saw r5's whole-run DATAGRAM sender CPU rise while its window cycles fell. On r6 the whole-run cell passes at 1.047, with intervention 4 added; this record does not separate the two changes.
- **The receiver gap between CPU time and cycles stays open.** The S5 receiver CPU flags remain at 1.11–1.16 in CPU time, with the excess inside the window. Yet #714 measured the receiver's window cycles per GiB at 1.001–1.011 of Reno's on r5. CPU time and cycles diverge at the receiver; frequency or idle effects are possible but not tested.

## Attribution results

All four unconditional stages ran: `lbneck` (24 observations), `s5timeline` (8), `timeline` (16) and `heapsites` (16), plus the macOS `s5timeline` (4). Every block was clean and none was rerun. No escalation trigger fired. `presrss` and `preslat` were not triggered, because no Reno-on-candidate flag was raised. That is 64 Linux and 4 macOS attribution observations, within the caps of 120 and 10. [stages.py](stages.py) writes every result to [attribution.json](attribution.json).

### Loopback bottleneck: inconclusive

Medians over four blocks. Cores used per goroutine stage; per-packet figures per transmitted packet at the sender.

| Workload, arm | Goodput (Mbit/s) | Sender: send queue / connection loop | Receiver: read loop / connection loop | Sender cycles/packet | Sender user instructions/packet | Process cores, sender / receiver |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM, Reno | 4727 | **0.88** / 0.52 | 0.40 / 0.37 | 19,875 | 29,584 | 1.95 / 1.34 |
| STREAM, `fc4c1bf1` | 3569 | 0.66 / 0.70 | 0.33 / 0.38 | 24,727 | 38,323 | 1.83 / 1.23 |
| STREAM, `d0fabc4d` | 4487 | 0.80 / 0.67 | 0.38 / 0.39 | 21,498 | 33,636 | 2.00 / 1.32 |
| DATAGRAM, Reno | 4311 | **0.88** / 0.51 | 0.45 / 0.37 | 20,415 | 30,200 | 2.06 / 1.43 |
| DATAGRAM, `fc4c1bf1` | 3567 | 0.74 / 0.68 | 0.38 / 0.38 | 24,044 | 36,763 | 2.00 / 1.29 |
| DATAGRAM, `d0fabc4d` | 3807 | 0.80 / 0.58 | 0.39 / 0.37 | 22,258 | 33,703 | 1.98 / 1.32 |

- **Rule outcome.** Both BBR arms' senders are *ambiguous* (a serial stage between 0.70 and 0.85) in all 16 arm-blocks, and the receivers are *unsaturated* in all of them. The rule's verdict is **inconclusive** for both workloads. The escalation needs exactly one cell without a majority; here every cell agrees on ambiguous, so it did not fire, and the stage ends as registered.
- **Descriptive, not a verdict.** Frozen Reno, the fastest arm, is the only one whose send-queue goroutine saturates. In the BBR arms neither serial stage saturates, though both are busy, and goodput rises as `d0fabc4d` removes per-packet cycles. That pattern would fit two coupled serial stages handing work to each other through the send credit, each waiting on the other part of the time. It is a hypothesis this stage did not test. Packets per useful GiB are equal across arms (785,500–786,000 STREAM; 905,000–905,600 DATAGRAM), so no arm sends more packets per useful byte. The hottest sender thread is 0.34–0.39 busy in every arm; Go moves goroutines between threads.

### S5 goodput: sender timing on Linux

Per block, fractions of the bottleneck's capacity over the measured window, from the [timeline overlay](timeline/).

| Workload | Delivery deficit | Attributed idle | Model | Timing | Unexplained | Verdicts |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | 0.078–0.087 | 0.069–0.077 | 0.015–0.019 | 0.045–0.048 | 0.010–0.011 | timing, mixed, timing, timing → **timing** |
| DATAGRAM | 0.098–0.112 | 0.089–0.101 | 0.010–0.019 | 0.072–0.074 | 0.007–0.008 | timing ×4 → **timing** |
| macOS STREAM | 0.023–0.025 | 0.017–0.018 | 0.015–0.016 | 0.000 | 0.002 | model ×2 → **model** |
| macOS DATAGRAM | 0.020–0.028 | 0.017–0.022 | 0.015–0.020 | 0.000 | 0.001–0.002 | model ×2 → **model** |

- **Stage verdict: sender timing.** Both workloads agree, so the model-behaviour hypothesis is not supported for the Linux gap, and the competitor is. The macOS comparison is **different** in both workloads.
- **What the timing is.** On Linux the sender records 25,171–26,360 late arrivals at a full-quantum deadline per 30 s window, about 84–88% of the roughly 1,000 quantum releases per second at 100 Mbit/s. Total lateness is 2.8–3.1 s per run, about 0.11–0.12 ms per event (maximum 0.26–1.16 ms). On the Mac it is 231–262 events and about 20 ms per run.
- **Model terms.** The quantum is one millisecond of credit at this rate (`max(2·M, rate/1000)`), and the pacer discards credit beyond it. Lateness per release therefore turns directly into lost sending at 100 Mbit/s.
- **Other time.** Local credit waits hold 0.5% (STREAM) and 1.3% (DATAGRAM) of the time.
- **Why it is sender timing, not CPU.** The connection loop uses about 0.12 cores on S5, so this is not a CPU limit. This record does not separate timer wake latency from scheduling on the connection goroutine.
- **The model asks for capacity.** The bandwidth estimate is 1.007–1.009× the bottleneck in Cruise in every block. The pacer's own 0.99 margin, Down and ProbeRTT make up the model's 0.010–0.019.
- **Loss responses** are rare: the long inflight bound never decreased in any counted window, and STREAM overflow is 0.1–0.2% of capacity.
- **Phases.** STREAM spends 22–25% of the window in Up and DATAGRAM 8–10%; ProbeRTT takes 2–4%.
- **Instrument perturbation is negligible.** Instrumented goodput is 0.9998 (STREAM) and 0.998 (DATAGRAM) of the readiness candidate's.
- **This is not yet a demonstrated defect.** It is a discriminating attribution between two registered explanations. Whether a longer credit horizon or another pacing change would recover the gap, and what that does to D08's bounds and queue behaviour, is untested here.

### Receiver CPU (and sender CPU): inside the measured window

| Cell | Flag | Window CPU-time ratio, median [range] | Outcome |
| --- | --- | --- | --- |
| S5 STREAM receiver | 1.164 | 1.177 [1.122–1.184] | **inside window** |
| S5 DATAGRAM receiver | 1.108 | 1.117 [1.086–1.137] | **inside window** |
| S5 STREAM sender (descriptive) | 1.183 | 1.194 [1.164–1.204] | inside window |
| Loopback DATAGRAM sender (descriptive) | 1.111 | 1.110 [1.106–1.112] | inside window |

The positive excess outside the window is 0.21 of the total in every cell, about the share of the run that lies outside the window. The excess rate is uniform through the run, not a Startup or shutdown cost. This is localization only.

### Memory

Every raised candidate memory cell is a steady-state excess: its measured-window footprint is 1.28–2.08× Reno's.

| Flag | Ratio | Block 1 / block 2 (excess MiB) | Classification |
| --- | --- | --- | --- |
| S6 STREAM receiver RSS | 1.759 | delivery 81% / delivery 72% (5.5, 3.1) | **Delivery data** |
| S5 STREAM sender RSS | 1.235 | bookkeeping 86% / unresolved (delivery 63%) (2.6, 2.1) | **Unresolved** |
| S5 STREAM receiver RSS | 1.457 | unresolved (delivery 57%) / delivery 85% (6.3, 6.6) | **Unresolved** |
| S6 STREAM sender RSS | 1.640 | unresolved / unresolved (4.2, 5.9) | **Unresolved** |
| S6 DATAGRAM sender RSS | 1.403 | unresolved (bookkeeping 66%) / bookkeeping 100% (3.0, 2.7) | **Unresolved** |

The largest single bookkeeping site in almost every block is `recoveryEvidence.sent`, at 1.16–1.70 MiB. The other bookkeeping sites are `beginCongestionFeedback` (0.78–0.81 MiB) and r2's `deliveryRecords` (0.53–0.74 MiB). The largest delivery site is the packet-buffer pool, `wire.init.0.func1`, at 1.5–6.0 MiB. On Linux, S5 STREAM receiver RSS was delivery data under `fc4c1bf1` (#712); here one block falls just short of 70%. Heap-site resolution is about 0.5 MiB per site.

### D2's three cells, re-attributed on `d0fabc4d`

| Cell | Mac (`fc4c1bf1`, covered) | Linux `fc4c1bf1` (#712) | Linux `d0fabc4d` |
| --- | --- | --- | --- |
| S6 control p95 | 1.547 / 1.481, Up policy | STREAM 1.541, Up policy | STREAM **1.588**: 98.5–99.3% of samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay 0 ms; S5 matched-load p95 1.056 / 0.716. **Up policy**, at a slightly larger magnitude. DATAGRAM passes (1.167). |
| S6 STREAM receiver RSS | 1.574, delivery | 1.754, unresolved | **1.759, delivery**; larger than the Mac cell |
| S5 sender RSS | 1.203 / 1.132, bookkeeping | 1.202 unresolved / 1.144 bookkeeping | STREAM **1.235, unresolved**; DATAGRAM passes (1.096) |

Under D2, a larger magnitude or changed composition gets no clearance from a cause label alone. Whether to except any cell is for the next decision.

## Flag report for the next decision

For [Decide whether the intervention-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/716), on candidate `d0fabc4d`, Linux (`minimax`):

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 11.39× (STREAM) and 10.81× (DATAGRAM) Reno goodput, 5/5 pairs each, at 0.119 / 0.127× Reno sender CPU per useful GiB. |
| **Passed** | Every loopback STREAM limit. Loopback DATAGRAM receiver CPU, RSS and p95. S5 DATAGRAM sender CPU and both S5 DATAGRAM RSS cells. S5 control p95. S6 CPU, S6 DATAGRAM receiver RSS and p95. **Every Reno-on-candidate cell.** |
| **Raised with attribution** | S6 STREAM control p95 1.588: selected ProbeBW Up policy. S6 STREAM receiver RSS 1.759: delivery data. |
| **Raised, attributed by a discriminating stage but not a demonstrated defect** | S5 goodput 0.917 / 0.899: sender timing (pacing lateness after full-quantum deadlines) on Linux; model on macOS (different). |
| **Raised and unresolved** | Loopback DATAGRAM goodput 0.858 and sender CPU 1.111 (bottleneck stage inconclusive). S5 STREAM sender CPU 1.183. S5 receiver CPU 1.164 / 1.108 (inside the window). Memory: S5 STREAM sender 1.235 and receiver 1.457, S6 STREAM sender 1.640, S6 DATAGRAM sender 1.403. |
| **Raised (preservation)** | None. |

## Deviations

1. **Readiness wrapper bug (analysis only).** [stages.py](stages.py) `readiness` first ran the rule pass and then the original-blocks pass. Since `analyze.py` always writes `summary.json`, the second pass overwrote it. Found when the receiver split could not read `summary.json`, before any readiness flag was reported. Fixed by running the original pass first. No rule, observation or flag value changed.
2. **Order.** The macOS `s5timeline` ran while the Linux readiness stage was running, not last as listed under [Order](#order). It shares no host, seed or result with the Linux stages.

## Preservation

- **Payload integrity.** All 190 retained Linux observations exited cleanly and passed receiver integrity: 110 readiness, 8 readiness reruns, 64 attribution, and 8 excluded prerequisite and smoke runs. So did the 5 macOS observations, including the excluded smoke run.
- **Default Reno.** No Reno-on-candidate readiness flag is raised. Medians lie within 1.018 on CPU and 1.031 on RSS, and goodput lies at 0.995–1.000.
- **No code change.** No transport source changed. The gates for `d0fabc4d` are #714's (Mac and native Linux, `gates/r6.log`, `gates/r6-native.log`). No recorded translation, cap, evidence contract, controller version or default changed. The timeline overlay exists only in the `cand-timeline` build.
- **No host change.** `perf_event_paranoid`, socket-buffer limits, offloads, the governor and every other host setting were left unchanged. `perf` ran with `sudo`, attached passively.

## Limits

- **Platform and topology.** One Linux host, loopback endpoints and a userspace relay. This certifies no other platform, no real carrier path and no production readiness. New-revision Mac readiness is unmeasured; the Mac runs here are four timeline observations only.
- **Repetition.** Five blocks per readiness cell; two to four per attribution stage. Heap-site resolution is about 0.5 MiB.
- **Timing attribution.** The timeline stage separates model shortfall from sender timing at 10 ms granularity with a fluid link model. Its attributed idle share falls 0.008–0.011 short of the delivery deficit. It does not separate timer wake latency from goroutine scheduling, and it tests no pacing change.
- **Engagement.** No queue CE-marked any packet, so the CE response stays unengaged. C4 items 1 and 3 and the low-rate pacing floor were not examined.

## Assets and reconstruction

- **Results:** [summary.json](summary.json) (registered readiness, with reruns), [summary-original.json](summary-original.json) (#712's rule on original blocks) and [attribution.json](attribution.json) (prerequisites, readiness blocks, CPU split, `lbneck`, `s5timeline` with macOS, memory and D2).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json): every observation, reruns, the macOS runs, prerequisites, stage logs, build receipts, thread samples, timelines and folded per-stage samples (`*.stages.json`) in place of the root-owned `perf.data`. Binaries, credentials and exported source trees are omitted.
- **To reconstruct**, in a fresh owned worktree of this branch:
  1. Run `build.py`, then `sync.sh` to a Linux host with the same CPU layout.
  2. Under `taskset -c 1-3`, run `prereq.py` (imported after `art`), then `matrix.py` `smoke`, `perfsmoke`, `ecnsmoke` and `xsmoke`, then the stages in [Order](#order), then `stages.py fold`.
  3. Anywhere, run `stages.py all` and `rules_test.py`. `mac_matrix.py` runs the macOS timeline on a Mac.

## What stays fixed and what changes

- **Fixed (D4):** #712's readiness stage: arms, paths, durations, seeds, rotation, flag limits, median rule and A/A reporting; frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; the fixture with the causal-diagnosis heap patch; the relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter); the launcher; the endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz); `minimax`'s core layout and host settings; `GOTOOLCHAIN=go1.27.0`; #711's memory and S6-latency rules.
- **Changed, all recorded:**
  - *Candidate:* `cand` is `d0fabc4d`. `prev` is `fc4c1bf1`, used only by the loopback-bottleneck stage.
  - *Builds:* [build.py](build.py) is #712's, with the changes listed in its docstring. Rebuilt here, frozen Reno, `fc4c1bf1`, the Reno diagnostic build, both relays, the calibration aid and the launcher are **byte-identical** to #712's binaries (`cda8feb1…`, `2b005781…`, `36752495…`, `033d467c…`, `24ee941d…`, `4218b433…`, `a6146d62…`). `d0fabc4d`'s fixture is `b5defc02…`; #714 packed no r6 build receipt to compare.
  - *Contamination reruns:* the operator's #714 rule, adopted prospectively here (see [Readiness](#registration-readiness)).
  - *New measurement-only overlay:* [timeline/](timeline/), used only by the S5 goodput stage.
  - *New aids:* [art.py](art.py), [matrix.py](matrix.py), [stage_run.py](stage_run.py), [rules.py](rules.py) with [rules_test.py](rules_test.py), [stages.py](stages.py), [mac_matrix.py](mac_matrix.py), [sync.sh](sync.sh). Copied byte-identical and checked by `build.py`: #712's [run.py](run.py), [analyze.py](analyze.py), [attribution.py](attribution.py), [localize.py](localize.py), [localize_test.py](localize_test.py), [prereq.py](prereq.py), [pack.py](pack.py), [calibrate/](calibrate/), [launch/](launch/), [relay/](relay/), [overlay/](overlay/), [counting/](counting/), [model-overlay/](model-overlay/); and #711's macOS runner as [mac/run.py](mac/run.py).

## Prerequisites

D4 requires the applicable #712 prerequisites before any comparison; a failure ends the ticket as a prerequisite gap. All were run on October 5 before this registration was committed, on a quiet host (largest foreign-CPU mean 0.004 cores). Every run is excluded from every statistic. [stages.py](stages.py) `prereq` writes them to `attribution.json`.

| Check | Criterion | Result |
| --- | --- | --- |
| ECN calibration ([prereq.py](prereq.py)) | Unadapted relay strips ECN (negative control); adapted relay preserves all four codepoints both ways | Unadapted: all 800 packets per direction arrive Not-ECT. Adapted: 200 of each codepoint arrive unchanged, both ways. **Pass** |
| ECN engagement (`smoke`, `perfsmoke`, `ecnsmoke`) | Forward and reverse packets after the handshake carry ECT(0) on S5/S6; BBR ECN tracker frames in the candidate sender's samples | All forward packets but 4 and reverse but 2 are ECT(0) on S5 and S6. Tracker frames in every candidate sender profile (98–753 samples). **Pass** |
| Native I/O (`perfsmoke`) | One send syscall and one transmit per forward packet: no GSO or `sendmmsg` batching | 0.998 and 0.998 per measured-window forward packet. **Pass** |
| Resources | Endpoint and relay threads inside their CPU sets; 8 MiB effective socket buffers; no UDP errors | All threads inside 8–11, 12–15 and 4–5; no `RcvbufErrors`, `SndbufErrors`, `InErrors` or `MemErrors`. **Pass** |
| Instruments | `perf stat` events at ≥ 95% running; `perf record` resolves Go frames; the three registered goroutine stages appear at both endpoints | 100% running. `conn_loop`, `send_worker` and `read_loop` resolved at both endpoints. **Pass** |
| Attribution runner (`xsmoke`) | Thread sampling present; timeline overlay covers the measured window | 30 thread samples per run; timeline coverage 30.0 s, bandwidth estimate 12.3 MB/s of QUIC payload, about the 12.5 MB/s wire bottleneck. **Pass** |
| macOS timeline (`mac_matrix.py smoke`) | The darwin overlay writes a usable timeline | Coverage 30.0 s. **Pass** |

One prerequisite metric was corrected before registration: the first native-I/O figure (0.760) divided measured-window counters by whole-run relay packets. It now uses #714's measured-window forward packets.

## Registration: readiness

#712's readiness stage, unchanged except for the candidate revision: arms Reno, A/A Reno, candidate (`d0fabc4d`, BBRv3) and Reno on candidate on loopback and S5, and Reno, A/A and candidate on S6; five blocks per path and workload; seeds 9001–9005 (S5) and 9101–9105 (S6); 110 observations through #712's unchanged `run_case` ([matrix.py](matrix.py) stages `loopback`, `s5`, `s6`). Flags are #712's ([analyze.py](analyze.py), unchanged): per path, workload and measure, the median of five per-block ratios against the same block's frozen Reno, against goodput 0.95 (loopback, S5), CPU per useful GiB 1.10, peak RSS 1.10 and control p95 1.20; on S6 the useful benefit is repeatable when the candidate beats Reno in all five pairs. Reno on candidate is held to the same limits as a preservation check. The A/A arm's per-block ratios are reported beside every ratio and never override the rule.

**Contamination.** #712's test is unchanged ([localize.py](localize.py) `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample). Its handling follows the operator's #714 rule, adopted here before any data and recorded as a departure from D4's "same contamination rule": a block (all arms of one path, workload and block) that holds a contaminated or unusable observation is rerun once with the same seed under the separate root `observations-rerun`, and the rerun replaces the original only if it is usable and uncontaminated ([rules.py](rules.py) `choose_block`). `summary.json` applies that rule. `summary-original.json` applies #712's rule to the original blocks (contaminated observations retained and counted), and any flag that differs between the two is reported. At most ten readiness blocks are rerun; beyond that, the remaining contaminated blocks are reported under #712's rule.

**Reporting.** Ticket A's counter effects (#714) are reported beside the readiness CPU flags. CPU that falls but stays above its limit without a cause remains unresolved. These are Linux results; new-revision Mac readiness stays unmeasured.

## Registration: attribution stages

Each stage below names its question, hypothesis, competitor, distinguishing observations, comparison inventory, maximum, escalation trigger and inconclusive ending, as D4 requires. The rule functions are in [rules.py](rules.py), exercised on 43 synthetic cases in [rules_test.py](rules_test.py), all passing, including D3's five required cases. The analysis is [stages.py](stages.py). New seeds are used except where a stage reuses #711's. Contaminated or unusable blocks follow the same rerun rule, and reruns count toward the stage caps. Attribution never sets a flag value. **Caps:** at most 120 Linux and 10 macOS observations across all attribution stages, reruns and escalations included.

| Stage | Runs | Observations (maximum) |
| --- | --- | --- |
| `lbneck` | loopback; Reno, `fc4c1bf1`, `d0fabc4d`; both workloads; 4 blocks | 24 (30 with escalation) |
| `s5timeline` | S5; `d0fabc4d` with the timeline overlay; both workloads; 4 blocks, seeds 9601–9604 | 8 (12 with escalation) |
| Receiver CPU | Readiness observations only | 0 |
| `timeline`, `heapsites` | #712's runs and seeds (9301–9302, 9401–9402; 9311–9312, 9411–9412) | 32 |
| `presrss` | Conditional: per raised Reno-on-candidate RSS cell, 6 blocks × 2 arms | 12 per cell, at most 2 cells |
| `preslat` | Conditional: per raised Reno-on-candidate loopback latency cell, 6 blocks × 2 arms, 120 s window | 12, at most 1 cell |
| D2 cells | From `timeline`, `heapsites` and readiness | 0 |
| Total | | ≤ 110 Linux plus reruns, ≤ 120 |
| macOS `s5timeline` | `d0fabc4d` with the overlay; 2 blocks × 2 workloads, seeds 9601–9602; plus the excluded smoke | 4 + 1 |

### Stage `lbneck`: the loopback bottleneck

- **Question.** What limits loopback goodput for the BBR builds?
- **Hypothesis.** A sender limited by per-packet work: a serial sender goroutine stage runs out of CPU.
- **Competitors.** A receiver-side limit (a serial receiver stage saturates instead), or a limit at neither endpoint (flow control, pacing or the model; these are not separated here).
- **Runs.** Loopback, 5 s warmup and 20 s measured, both workloads, four blocks, arms frozen Reno, `fc4c1bf1` and `d0fabc4d` (BBRv3), #712's rotation. Each observation attaches `perf stat` (#712's sixteen events) and `perf record -e cycles -F 4999 --call-graph fp` to both endpoints for the measured window, and samples every endpoint thread's schedstat and CPU ticks each second ([stage_run.py](stage_run.py)).
- **Measures.** Sender instructions and cycles (user and kernel) per transmitted packet (`net:net_dev_xmit`), packets per useful GiB, process cores, hottest-thread busy share, run-queue wait, and per goroutine stage the cores used: the stage's share of the endpoint's cycle samples times the endpoint's on-CPU seconds in the window, divided by the window. Stages are classified by the sample's user frames ([rules.py](rules.py) `STAGES`): `conn_loop` (`(*Conn).run`), `send_worker` (`(*sendQueue).Run`), `read_loop` (`(*Transport).listen`), `transport_send`, `app` (`main.`), else `runtime`. The serial stages are `conn_loop` and `send_worker` at the sender, `read_loop` and `conn_loop` at the receiver. A goroutine runs on at most one thread at a time, so its cores cannot exceed one. Thread-level shares are reported but do not decide: Go moves goroutines between threads, and the smoke run's hottest sender thread was 0.35 while the process used 2.0 cores.
- **Distinguishing observations and rule.** Per observation, an endpoint is *saturated* when a serial stage uses ≥ 0.85 cores, *unsaturated* when every serial stage is below 0.70, and otherwise *ambiguous*. Per arm and workload, a state needs at least three of four blocks. For `fc4c1bf1` and `d0fabc4d` together:
  - **sender per-packet work** when the sender is saturated and the receiver unsaturated in both arms, and, if `d0fabc4d`'s sender cycles per packet are below 0.98 of `fc4c1bf1`'s (median), its goodput is higher in the median and in at least three of four blocks;
  - **receiver-side limit** when the receiver is saturated and the sender unsaturated in both arms;
  - **neither endpoint saturated** when both are unsaturated in both arms;
  - **inconclusive** otherwise: both saturated, an ambiguous majority, the arms disagreeing, or fewer sender cycles per packet without more goodput. Reciprocal goodput and CPU movement alone never yields a verdict. Frozen Reno's state is reported as context.
- **Escalation.** If exactly one arm-workload cell lacks a three-of-four majority, one more block of that workload is run (three observations), once per workload. Otherwise none.
- **Inconclusive ending.** After the escalation, an inconclusive workload stays inconclusive and the stage ends.

### Stage `s5timeline`: the Linux S5 goodput gap

- **Question.** Why does the candidate deliver less than Reno on S5 on Linux?
- **Hypothesis.** Model behaviour: the model asks for less than the bottleneck can carry (its pacing rate across ProbeBW phases, ProbeRTT, or window limits).
- **Competitor.** Sender timing: the model asks for about capacity, but the sender does not emit on time, through pacing lateness after a full-quantum deadline or local send-credit waits.
- **Instrument.** The measurement-only [timeline overlay](timeline/), built into `cand-timeline` only, hooks code that runs only when BBR is selected. After each `Feedback` it records the model's phase, bandwidth estimate, pacing rate, window, inflight bounds, minimum RTT and bytes in flight, at most once per 10 ms and at every phase change. Per 10 ms bucket it records how each `sendBounded` opportunity ended and the time spent in each resulting state (paced, window-limited, credit-limited, hard-blocked, supply-exhausted, immediate continuation), bytes and packets handed off, and pacing lateness: the time from a paced stop's deadline to the first opportunity at or after it. The pacer refills its credit to one quantum by that deadline and discards credit beyond it, so lateness after the deadline is capacity the sender lost. Sends are permitted earlier, whenever one packet's credit exists, so earlier wakeups lose nothing. The connection goroutine writes a snapshot every 500 ms.
- **Runs.** S5, 10 s warmup and 30 s measured, both workloads, four blocks, seeds 9601–9604, `cand-timeline` only. Reno's relay data come from the readiness stage.
- **Rule** ([rules.py](rules.py) `timeline_decomposition`, `timeline_verdict`). Per observation, over the measured window, against the bottleneck's wire capacity C (100 Mbit/s, IP packet bytes): the link is a fluid server, idle in each 10 ms bucket for C minus the queue at the bucket's start plus the bytes sent in it, never below zero. Each bucket's idle capacity is split in order: first the **model** share (the needed rate above the pacer's rate, which is 0.99 of the controller's rate, or above the actual send rate when the window governed the bucket), then the **timing** share (lateness plus credit-wait time at the pacer's rate, clamped to the needed rate), and the rest is **unexplained**. With S the attributed idle share: *model* when model ≥ 0.5 S and timing < 0.25 S; *timing* when timing ≥ 0.5 S and model < 0.25 S; *mixed* when both are ≥ 0.25 S; otherwise *unexplained*. *No deficit* when the delivery deficit is under 0.02. *Unusable* with under 28 s of coverage, more than 5% of buckets without a queue sample, or an attributed idle share that differs from the delivery deficit by more than half the deficit plus 0.01. Per workload, the verdict shared by at least three of four usable blocks (with at least three usable). The stage verdict is **model behaviour** or **sender timing** only when both workloads agree; otherwise it is reported per workload as inconclusive.
- **Descriptive, not decisive.** Phase time shares, bandwidth estimate over capacity in Cruise, loss responses (inflight-bound decreases, bottleneck overflow), lateness count and maximum, state shares, and instrumented goodput against the readiness candidate's.
- **macOS.** The same instrument on the operator's Mac (#711's runner and relay), two blocks per workload, seeds 9601–9602, verdict needing both blocks. It shows only whether each workload's verdict is the **same** or **different**. Hardware and scheduling change with the OS, so a difference cannot by itself establish a Linux-specific cause.
- **Escalation.** A workload with exactly two of four agreeing usable blocks gets two more blocks (seeds 9605–9606), once.
- **Inconclusive ending.** Otherwise the stage ends; an inconclusive workload stays inconclusive.

### Receiver CPU: measured window against whole run

- **Question.** Where in the run does the receiver's whole-run CPU excess lie?
- **Method.** No new runs. For every readiness cell, from each observation's per-second CPU-tick samples (`host-cpu.json`), linearly interpolated at the measured window's start and end: CPU seconds before, inside and after the window, each per useful GiB, as candidate ÷ frozen Reno per block ([rules.py](rules.py) `cpu_split`, `receiver_localization`). For a raised receiver CPU flag: **inside window** when the window ratio alone exceeds 1.10 (median); **outside window** when it does not and at least half of the positive excess lies outside the window; otherwise **mixed**. This is localization, not cause. The same split is reported for sender cells, descriptively, because #714 saw DATAGRAM whole-run CPU diverge from measured-window cycles. Tick resolution is 10 ms, and sampling is once a second.

### Stages `timeline` and `heapsites`: memory

#711's rules, runs and seeds, unchanged, through [attribution.py](attribution.py) (`reno-diag` against `cand-diag-counted`, S5 and S6, both workloads, two blocks each). Every raised candidate memory flag is classified as a Startup transient, steady-state excess or mixed, and then by heap site as delivery data or BBR bookkeeping, with 70% of the positive excess to lead. Both heap-site blocks must agree for a classification; otherwise it is unresolved. One pattern change, made before any data: r2 replaced the delivery-record map with `ackhandler.(*deliveryRecords)`, a name the bookkeeping pattern did not contain, so the pattern now also names it. Without the change, the same delivery records counted as bookkeeping on `fc4c1bf1` would count as "other" on `d0fabc4d`.

### Stage `presrss`: Reno-on-candidate RSS (conditional, D3)

- **Trigger.** Runs only for a raised Reno-on-candidate RSS cell, at most two cells.
- **Question.** Does Reno built from the candidate retain more memory than frozen Reno, or does the cell reflect RSS-level variation?
- **Hypothesis.** Retention: higher retained heap. **Competitor:** RSS-level effects without retained heap, such as the two-level RSS outcomes #712 saw in both Reno arms.
- **Runs.** The cell's path and workload, six blocks (seeds 9801–9806 on S5, 9901–9906 on S6, none on loopback), arms `reno-diag` and `cand-diag` with Reno, heap profiles every second.
- **Rule** ([rules.py](rules.py) `rss_preservation`). Per block, peak RSS, retained heap (in-use heap at the measured-window heap peak) and heap-site excess by #711's groups. **Retention excess** when the median retained ratio exceeds 1.10 and at least five of six blocks are above 1, with the leading site group reported. **RSS excess without retained heap** when retention is within 1.10 and either the median RSS ratio exceeds 1.10 or candidate runs have at least two more high-memory outcomes (peak RSS above 1.10× the stage's frozen-Reno median) than frozen Reno. **Not reproduced** when the RSS ratio is within 1.10, high-memory outcomes differ by at most one, and retention is within 1.10. Otherwise **inconclusive**; fewer than four usable blocks is an **evidence gap**. Instruction counts play no part, and no outcome rests on A/A range membership. The readiness frequency of high-memory outcomes per arm is reported beside it. The readiness flag stays as recorded whatever this stage finds.
- **Escalation.** None. **Inconclusive ending:** reported as inconclusive.

### Stage `preslat`: Reno-on-candidate control latency (conditional, D3)

- **Trigger.** Runs only for a raised Reno-on-candidate control p95 cell on loopback, at most one cell. A raised cell on S5 or S6 has no stage and is reported as an evidence gap.
- **Question.** Does Reno built from the candidate have a longer control-response tail than frozen Reno, at a sample size that can estimate a tail?
- **Why a longer window.** The fixture sends one control request per second, so a 20 s loopback run holds 20 replies and its p95 is close to its maximum. The endpoint contract is unchanged; only the measured window grows to 120 s (`loopback-long`), giving 120 replies per run.
- **Runs.** The cell's workload, six blocks, arms frozen Reno and Reno on candidate.
- **Rule** ([rules.py](rules.py) `latency_preservation`). Adequacy: at least 100 replies in every counted run, 500 per arm and four usable blocks; otherwise an **evidence gap**. The ratio is the pooled p95 (Reno on candidate ÷ frozen Reno) with a 95% block-bootstrap interval. **Regression reproduced** when the ratio exceeds 1.20, the interval's lower bound exceeds 1.0, and at least four of six blocks are above 1. **Not reproduced** when the interval's upper bound is at most 1.20. Otherwise **inconclusive**. Absolute p50 and p95 are reported. A regression confined to half the blocks counts as reproduced, which is conservative for a preservation check.
- **Escalation.** None. **Inconclusive ending:** reported as inconclusive.

### D2 cells

Re-attributed on `d0fabc4d` under their registered rules, with no new runs. S6 STREAM control p95 is attributed to the selected ProbeBW Up policy when at least 70% of forward-queue samples above 25 ms fall in Up or the following Down, Cruise and Refill median queue delay is below 1 ms, and the readiness S5 matched-load p95 is within 1.20. S6 STREAM receiver RSS and S5 sender RSS come from the memory stages. Under D2, a larger magnitude, a different cause or a changed composition gets no clearance from a cause label alone.

### Other preservation cells

A raised Reno-on-candidate CPU or goodput flag has no stage in this ticket. It is reported as an unattributed preservation flag, beside #714's measurement of Reno on r5 within frozen Reno's A/A range.

## Disclosures made before data

- **The timeline rule was revised after an excluded smoke run.** The first form charged pacing lateness at the full pacing rate, including lateness that a standing bottleneck queue absorbs. On the excluded `xsmoke` S5 DATAGRAM run its terms summed to more than the delivery deficit (residual −0.037). A second form counted only buckets with an empty queue at their end and attributed only half the deficit (0.052 of 0.098). The registered fluid-link form above reconciles (0.088 of 0.098). That smoke run reads "timing" under all three forms, so the revision changed consistency, not that run's verdict. The thresholds (0.5, 0.25, 0.02) and the stage thresholds for `lbneck` (0.85, 0.70) were written before any smoke run.
- **Smoke values seen.** The excluded smoke runs gave `d0fabc4d` 4,328–4,485 Mbit/s on loopback STREAM, 85–88 Mbit/s on S5 and 62–66 Mbit/s on S6 DATAGRAM. The `xsmoke` loopback run had its `send_worker` stage at 0.79 cores and `conn_loop` at 0.67 at the sender, with the receiver's serial stages at 0.38–0.39 cores. On the Mac smoke run (S5 STREAM) the delivery deficit was 0.017. None of these enters any statistic.

## Order

`prereq.py`, `smoke`, `perfsmoke`, `ecnsmoke`, `xsmoke` and the macOS smoke (done); then `loopback`, `s5`, `s6`; any readiness reruns; `lbneck`, `s5timeline`, `timeline-S5`, `timeline-S6`, `heapsites-S5`, `heapsites-S6`; the conditional `presrss` and `preslat` cells, as the readiness flags require; attribution reruns and escalations; the macOS `s5timeline`. All Linux stages run sequentially under `taskset -c 1-3` in an operator-booked quiet window. Then `stages.py fold` on `minimax`, and `stages.py all` and `rules_test.py` anywhere.
