# Linux re-demonstration of the surviving BBRv3 revision

**Date:** October 5, 2026, America/New_York. **Scope:** [Re-demonstrate the surviving BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/715), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies Ticket B of D4 in the [accepted Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md#d4--next-test-the-per-packet-leads-by-intervention-then-re-demonstrate-on-linux), on the "A keeps one or more changes" route. Measurement and attribution only: no production merge, transport source change, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-redemonstration`, based on `fb3261f4` (the [per-packet intervention record](../2026-10-03-bbr-per-packet-interventions/README.md), which contains the surviving revision `d0fabc4d`). The [Linux diagnostic](https://github.com/the-sarge/quic-go-fast/blob/4323dae8/docs/audits/2026-10-03-bbr-linux-diagnostic/README.md) (`4323dae8`) supplies the readiness stage, fixture, host layout and aids this record reruns.

**Status: registration.** Everything below was written, with the analysis code and its synthetic cases, before any readiness or attribution observation of this ticket existed. The only observations so far are the excluded prerequisite and smoke runs listed under [Prerequisites](#prerequisites).

## Question

On owned Linux hardware, what are the readiness flags of the surviving revision `d0fabc4d` (r6), and what do registered discriminating stages show about the loopback bottleneck, the Linux S5 goodput gap, receiver CPU, memory and the preservation cells?

r6 holds all five per-packet interventions: four kept by rule (r5, `2d777bb0`) and the capability snapshot kept by operator decision. r6 as a whole is unmeasured. On S5, r5 runs 0.899 (STREAM) and 0.912 (DATAGRAM) of `fc4c1bf1`'s sender user instructions per forward packet. The remaining excess over frozen Reno is 6,736 and 5,219 user instructions per packet, unattributed.

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
