# Linux re-demonstration of the timing- and CPU-tested BBRv3 revision

**Date:** October 6, 2026, America/New_York. **Scope:** [Re-demonstrate the timing- and CPU-tested BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/736), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D5 ("Ticket E") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md#d5--ticket-e-linux-re-demonstration-then-a-fresh-decision), on the route "D keeps nothing → E re-demonstrates C's survivor if C kept a change". Measurement and attribution only: no production merge, transport source change, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r8-linux-redemonstration`, based on `f3840c83` (the [per-packet CPU-lead record](../2026-10-06-bbr-per-packet-cpu-leads/README.md)). The revision under test is **r8, `95f5b6b7`**: `d0fabc4d` plus the Linux netpoller kick at the BBR pacing deadline. The [pacing-wake ticket](../2026-10-05-bbr-pacing-wake/README.md) kept it by operator decision after a negative registered outcome (its deviation 3), and the [per-packet CPU-lead ticket](../2026-10-06-bbr-per-packet-cpu-leads/README.md) kept no lead on top of it. Its open cost carries forward: run-loop wakes per useful GiB rose 6–16% on S5. The [#715 re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) (`36f7ce01`) supplies the readiness stage, attribution stages, fixture, host layout and aids that this record reruns.

**Status: registered; no observation of this ticket exists yet.** The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) were written, with the analysis code and its synthetic cases, before any prerequisite, smoke, readiness or attribution observation of this ticket.

## Question

On owned Linux hardware, what are the readiness flags of r8 (`95f5b6b7`), and what do registered stages show about S5 timing, memory composition, the D2 cells and any raised preservation cell?

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
