# What raises BBRv3's loopback control latency on r9

**Date:** October 13, 2026, America/New_York. **Scope:** [Test whether BBRv3's 32-entry send queue raises its loopback control latency](https://github.com/the-sarge/quic-go-fast/issues/744), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D3 ("Ticket J") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/af90a739/docs/audits/2026-10-13-bbr-r9-qualification-decision/README.md#d3--ticket-j-what-raises-the-loopback-stream-control-latency-and-one-remedy-if-the-queue-is-responsible). Measurement, attribution and, only if the depth raises latency through queue wait, one contract-preserving change: no production merge, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r9-loopback-latency`, based on `af90a739` (the accepted decision), whose tree's code is exactly r9 plus a glossary. The revision under test is **r9, `81e9dc8c`**: r8 plus #738's 32-entry BBR send queue.

**Status: registration. No comparative observation exists.** Everything below was written before any Stage 0 or Stage 1 observation of this ticket. The excluded development runs that preceded it are listed in [Disclosures made before data](#disclosures-made-before-data). Accepting the decision did not authorize measurement; the operator's approval of this registration does.

## Question

On r9 (`81e9dc8c`), does the 32-entry BBR send queue raise the candidate's loopback STREAM control-stream p95 (1.485 in readiness; 1.849 [1.672–2.014] at an adequate sample in #740)? Where in the request's path does the rise sit? And does one contract-preserving remedy reduce it without giving back the loopback goodput the queue bought?

## What stays fixed and what changes

- **Fixed (D3):** r9 as the starting revision; frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; #712's fixture with the causal-diagnosis heap patch, launcher, endpoint contract (GOMAXPROCS from the four-core affinity, tracing disabled, 1,400-byte packets, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, a reliable control stream at 1 Hz), core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings on `minimax`; `GOTOOLCHAIN=go1.27.0`; #715's `loopback-long` timing (5 s warmup, 120 s measured: about 120 control replies per run); #712's contamination test with the operator's #714 rerun rule; #738's quiet-host wait.
- **Builds** ([build.py](build.py), adapted from #740's). `reno` and `cand` (plain r9) rebuild byte-identical to #740's binaries (`cda8feb1…`, `4fa75322…`), and so does the launcher (`a6146d62…`); `build.py` asserts it. Copied byte-identical from #740's record (`ddbac0a3`) and checked by `build.py`: [run.py](run.py), [stage_run.py](stage_run.py), [localize.py](localize.py) with [localize_test.py](localize_test.py), [pack.py](pack.py), [rules.py](rules.py) with [rules_test.py](rules_test.py), [go.mod](go.mod), [launch/](launch/), [model-overlay/](model-overlay/).
  - `cand8`: **r9 at depth 8**, r9's source with `bbrSendQueueCapacity = 8`. A diagnostic, never kept. Its exported tree differs from `cand`'s in that one line.
  - `cand-ctl` and `cand8-ctl`: the two revisions with the control-path recorder ([The instrument](#the-instrument)).
  - `syn-*-ctl`: `cand-ctl` with one registered injection each ([Stage 0](#registration-stage-0-recorder-synthetic-cases-and-perturbation)).
- **Identity** (D3: "The registration checks each build's identity and the queue capacity it selects at run time"):
  - every binary's SHA-256 and patched-source hashes are in its receipt (`.local/bbr-r9-loopback-latency/bin/*-build.json`);
  - the plain trees run `TestCtlDepthIdentity` (`bbrSendQueueCapacity` and `sendQueueCapacityFor` are 32 and 8 for BBR, 8 for Reno) with r9's five `TestBBRSendQueueDepth*` tests (`build.py identity`). Both trees pass: `cand` and `cand8` each pass 8 of 8;
  - every recorder run records the capacity of each queue entry it tags, at both endpoints, and is unusable unless that is 32 (`cand-ctl`, `syn-*`) or 8 (`cand8-ctl`).
- **Registered binaries** (SHA-256 prefixes): listed in [Assets](#assets) once the registration commit rebuilds them.
- **Runner:** every observation goes through #715's unchanged `stage_run.run_case_x` ([matrix.py](matrix.py)). The recorder needs no runner change: a recorder build writes `ctl-send.json` and `ctl-receive.json` beside each endpoint's `-output`.
- **New measurement-only code:** the recorder ([ctl/ctlrec.go](ctl/ctlrec.go)) and its hooks (`build.py` `ctl_hooks`), the extraction ([ctl.py](ctl.py)), the rules ([ctlrules.py](ctlrules.py)) with their synthetic cases ([ctlrules_test.py](ctlrules_test.py), [ctlrules_oc.py](ctlrules_oc.py)), the stage analysis ([ctlstages.py](ctlstages.py)), and the aids [art.py](art.py), [matrix.py](matrix.py), [follow.py](follow.py), [driver.sh](driver.sh) and [sync.sh](sync.sh). No tracked transport source changes; every instrumented or diagnostic build is an exported tree under `.local`.

## The instrument

D3 asks for a per-request recorder covering both endpoints' send queues, storing segment times rather than aggregates.

**What it records.** The fixture's control request is a 32-byte message on stream 0 carrying its id; the receiver echoes it on the same stream. Both endpoints run the same binary and stamp `runtime.nanotime` (CLOCK_MONOTONIC). On one host that clock is shared by both processes, so their stamps compare directly. Each endpoint logs these events ([ctl/ctlrec.go](ctl/ctlrec.go)):

| Event | Where | Fields |
| --- | --- | --- |
| write | `SendStream.Write`, stream 0, 32 bytes | the stream's write offset, the message id |
| pack | `popStreamFrame` / `popRetransmissionFrame`, stream 0 | frame offset range, retransmission flag, pack sequence |
| enqueue | `sendQueue.Send` for the entry carrying the last popped stream-0 frame | entries and bytes already queued ahead, queue capacity, entry bytes |
| dequeue | the send worker takes that entry (both send paths) | — |
| submit | that entry's write has returned (stamped in `release`, before the buffer is recycled) | — |
| arrive | `ReceiveStream.handleStreamFrame`, stream 0 | the packet's socket-read time (`rcvTime`, converted to the shared clock) and the handling time |
| read | `ReceiveStream.Read`, stream 0 | the read position after the read |
| note | the fixture's sender, per reply | the id, whether the request was offered in the measured window, the fixture's own send and receive stamps |

**Segments.** [ctl.py](ctl.py) joins one request's events across the two logs by stream offset and id. It splits the request's latency into fifteen consecutive segments that sum to the recorder's total, from the sender's write to the sender's read of the reply:

| Segment | Meaning |
| --- | --- |
| `s_pack` | sender: application write to packing (the frame carrying the request's last byte is popped) |
| `s_entry` | sender: packing to queue entry |
| **`s_queue`** | **sender: send-queue wait** (queue entry to the worker's dequeue), with entries and bytes ahead |
| `s_submit` | sender: dequeue to submission (the write returned) |
| `t_req` | **residual**: submission to the receiver's socket read (kernel loopback, and anything unplaced) |
| `r_backlog` | receiver: socket read to the frame handled by the connection loop (receive backlog) |
| `r_wake` | receiver: frame handled to the application read |
| `r_app` | receiver: application read to the reply's write |
| `r_pack`, `r_entry` | receiver: the reply's packing and queue entry |
| **`r_queue`** | **receiver: send-queue wait** for the reply, behind ACK-only entries, with entries and bytes ahead |
| `r_submit` | receiver: the reply's dequeue to submission |
| `t_rep` | **residual**: the reply's submission to the sender's socket read |
| `s_backlog`, `s_wake` | sender: the reply's receive backlog and application wake |

Bytes ahead count queued entries not yet dequeued. They exclude an entry that the worker is already submitting, as entries ahead do.

**Cost control.** On ordinary packets the recorder adds a stream-ID compare in the frame paths and one pointer compare per dequeue and release. It also adds a plain byte counter in `Send`, which only the connection loop touches, and one uncontended atomic add per dequeue, which only the worker touches. The connection loop reads the worker's counter only when it tags a control entry, so it never pulls that cache line on an ordinary packet. Clock reads and the mutex-guarded event log run only for stream-0 events: about ten per request, one request a second.

**Recorder usability** (per observation, [ctl.py](ctl.py) `usable`). An observation is usable when it exits cleanly with receiver integrity (#712's `summarize`) and:
- no event was dropped;
- every tagged entry's queue capacity is the build's (32, or 8 for `cand8-ctl`), at both endpoints;
- at least 95% of the measured requests, and at least 100, have a complete chain;
- no segment of a complete request is below −1 µs;
- for at least 99% of complete requests, the recorder's total is within 20 µs of the fixture's own latency for that reply.

A measured request is one the fixture's sender offered inside the measured window (its `note`). Requests whose control frame was retransmitted, or whose message was split across frames, stay in the analysis and are counted in the results.

## Registration: Stage 0, recorder, synthetic cases and perturbation

Stage 0 validates the recorder and the mechanism estimator; it attributes nothing. **It passes when every runtime case and every data-level case below is recovered and the perturbation check passes.** If it fails, the mechanism reading ends as an **instrumentation gap**, and the plain-arm depth reading still stands (D3). Stage 1 runs either way.

### Runtime synthetic cases

Each case is `cand-ctl` plus one measurement-only injection ([ctl/ctlrec.go](ctl/ctlrec.go) constants, `build.py` `SYNTH`). [matrix.py](matrix.py) `syn` runs them on `loopback-syn`: loopback STREAM, 5 s warmup and 60 s measured, so about 60 replies per run. There are three blocks of eight arms: the base `cand-ctl`, a second `cand-ctl` (the near-zero case) and the six injections, in #712's rotation. Each case is read against the same block's base by the Stage 1 mechanism estimator, unchanged except for two settings: a minimum of three blocks, and no consistency interval, since there are no plain arms. The injections are compiled in and act by role (sender or receiver), so one binary runs both endpoints.

| Case | Injection | Recovered when ([ctlstages.py](ctlstages.py) `SYN`) |
| --- | --- | --- |
| Deep sender queue | at the sender, the worker holds the tagged entry 300 µs before stamping its dequeue | **queue wait**, targets the sender only, median `s_queue` rise ≥ 210 µs |
| Deep receiver queue | the same at the receiver, for the reply | **queue wait**, targets the receiver only, median `r_queue` rise ≥ 210 µs |
| Slow worker | the sender's worker spins 20 µs before every submission | **queue wait**, the sender among the targets, median `s_submit` rise ≥ 14 µs |
| Busy receiver loop | the receiver's connection loop spins 1 µs for every received packet | **load or other**, with `r_backlog` named |
| Delayed reply write | the receiver's fixture spins 300 µs between reading a request and writing its reply | **load or other**, `r_app` named and no queue segment named, median `r_app` rise ≥ 210 µs |
| Unplaceable delay | at the sender, the tagged entry's submission is stamped, then the worker spins 300 µs before its write | **load or other**, with only residual segments named (`t_req` among them): no placed segment receives it |
| Near-zero p95 rise | none: a second base arm | **inconclusive** |

The unplaceable case stamps the submission before the delay, so from the recorder's point of view the packet left on time and arrived late. It stands for a delay in a stage the recorder does not observe, such as the kernel.

### Data-level synthetic cases

[ctlrules_test.py](ctlrules_test.py) builds per-request segment rows with known causes: six blocks of 120 requests per arm, block-level variation shared by both arms. D3 names every case it builds:
- a deep sender queue, a deep receiver queue and a slow worker read **queue wait**, each targeting its endpoint;
- a busy receiver loop reads **load or other** with `r_backlog` named, and a delayed reply write reads **load or other** with `r_app` named;
- an unplaceable delay reads **load or other** with only the residual named;
- a near-zero p95 rise reads **inconclusive**, because the rise's interval does not exclude zero;
- **the second consideration's first counterexample** reads **load or other** with `r_app` named. In it, a 60 µs shift in the reply's application segment raises the p95 while 1% of requests carry 2 ms of sender queue wait. The upper-tail mean decomposition credits the queue; the test asserts it does so, which is why tail means are supporting evidence only;
- **the second counterexample** reads **queue wait**. In it, a 60 µs queue rise raises the p95 while the reference arm carries 3 ms receive-backlog spikes on 3% of requests, so the upper-tail mean falls. The test asserts the fall;
- a 60 µs + 60 µs split between queue and application reads **mixed**;
- intervals too wide to summarize read **inconclusive**, not mixed;
- identity: replacing an arm's queue wait with its own leaves its p95 unchanged.

They also cover the depth, perturbation, Stage 2 bound, efficacy and outcome rules. All 29 tests pass, as do #715's 43 copied rule tests ([rules_test.py](rules_test.py)).

**Operating characteristics of the depth rule** ([ctlrules_oc.py](ctlrules_oc.py)). The synthetic generator uses six blocks of 120 lognormal replies per arm. Its σ of 0.42 matches the p95/p50 of about 2.0 in #740's `latcand` arms, and the block variation is shared. Over 200 seeds per true r9 ÷ depth-8 ratio:

| True ratio | depth raises latency | non-increase | inconclusive |
| --- | --- | --- | --- |
| 1.0 | 9 | 175 | 16 |
| 1.075 | 51 | 69 | 80 |
| 1.15 | 141 | 10 | 49 |
| 1.3 | 195 | 0 | 5 |

The block bootstrap D3 requires is anti-conservative with six blocks. It reads "depth raises latency" about 4.5% of the time when depth has no effect, against a nominal 2.5%. It reads "non-increase" about 5% of the time at a true 15% rise, which is the margin. The rule is applied as registered; these rates are disclosed so the readings can be weighed.

### Perturbation (D3: "on the measured quantity")

D3 asks the registration to state where the check is scored. **It is scored on the counted Stage 1 arms**, where each block holds both revisions plain and with the recorder ([ctlrules.py](ctlrules.py) `perturbation`). Per revision (r9 and r9 at depth 8):
- **goodput**: the median over usable blocks of instrumented ÷ plain `loopback-long` STREAM goodput must be at least **0.99** (D3);
- **control p95**: the pooled p95 ratio, instrumented ÷ plain, must lie within **[1/1.15, 1.15]**. The tolerance is set from #740's interval half-width of about ±9% at this sample (1.672–2.014 around 1.849) and from the lead's 1.42 r9 ÷ r8 ratio: a 15% shift cannot create or erase an effect of that size.

Both revisions must pass both checks. If either fails, the mechanism reading ends as an **instrumentation gap** and the plain-arm depth reading still stands.

## Registration: Stage 1, attribution

- **Arms** ([matrix.py](matrix.py) `s1`). `loopback-long` STREAM, six blocks, #712's rotation: frozen Reno, r9, a second r9 (the BBR A/A control) and r9 at depth 8, all plain; and r9 and r9 at depth 8 with the recorder. 6 arms × 6 blocks = **36 observations**.
- **Usable block.** All six observations are usable (recorder arms under [Recorder usability](#the-instrument)) and uncontaminated, after the rerun rule ([ctlstages.py](ctlstages.py) `chosen`).
- **Depth reading (plain arms)** ([ctlrules.py](ctlrules.py) `depth_reading`). The pooled p95 ratio r9 ÷ depth 8, with a 95% block-bootstrap interval (2,000 resamples of the usable blocks, seed 744, #715's index convention). The r9 A/A ratio (second r9 ÷ r9) gets its interval from the same resamples. The endings are evaluated in this order:
  1. **depth raises latency**: the interval's lower bound is above 1, at least five usable blocks have a per-block ratio above 1, and the point estimate exceeds the A/A interval's upper bound;
  2. **non-increase**: the interval's upper bound is below **1.15**. This is an equivalence test on the direct comparison alone, so a wider A/A interval can never make it easier to reach. The 15% margin is under a fifth of the cell's 85% excess over frozen Reno;
  3. otherwise **inconclusive**: compatible with noise, which is not a finding of no effect.

  An equivalence within the margin is reported either way. Fewer than five usable blocks, a run under 100 replies, or an arm under 500 pooled replies is an **evidence gap**.
- **Mechanism reading (recorder arms)** ([ctlrules.py](ctlrules.py) `mechanism_reading`). The estimator is a **counterfactual p95**. Pool r9's per-request rows over the usable blocks. Replace each request's send-queue wait at both endpoints (`s_queue`, `r_queue`) with depth 8's queue wait at the same quantile rank: each segment is rank-matched separately, with mid-ranks (i + 0.5)/n and linear interpolation. Recompute the p95. The **share** is the fraction of the p95 rise (r9-ctl's p95 minus r9-depth8-ctl's p95, on the recorder's totals) that the replacement removes, with a block-bootstrap interval over replicates whose rise is positive (seed 745). The share is computed only when:
  - Stage 0 passes, including perturbation (otherwise **instrumentation gap**);
  - the recorder arms' pooled p95 ratio (the fixture's latencies) lies inside the plain arms' 95% interval for r9 ÷ depth 8 (otherwise **inconclusive**);
  - the rise's interval excludes zero (otherwise **inconclusive**);
  - at least 97.5% of replicates have a positive rise (otherwise **inconclusive**).

  The endings:
  - **queue wait**: the share's lower bound is at least 0.70;
  - **load or other**: its upper bound is below 0.30;
  - **mixed**: otherwise, when the interval excludes both 0 and 1, so both components are shown;
  - otherwise **inconclusive** (an unstable share).

  The reading is **endpoint-resolved**. The same estimator replaces only the sender's queue wait, then only the receiver's, and reports each share with its interval. Each segment, the two residuals included, gets its own counterfactual share. A segment is **named** when its share's lower bound reaches 0.30. A depth-caused rise in a placed segment, such as packing delay or receive backlog, is reported by that segment. A rise named only in the residuals (`t_req`, `t_rep`) is reported as **unplaced**. Upper-tail mean differences are reported as supporting evidence only.
- **Baseline reading.** #715's `latency_preservation` rule, unchanged, for depth 8 against frozen Reno. Its outcomes map as follows:
  - *not reproduced*: a depth-8 configuration would clear the cell;
  - *regression reproduced*: a **BBR baseline** component;
  - *inconclusive* or a gap: the baseline is unresolved.

  It describes the depth-8 configuration only and never decides a survivor's cell, which follows D4.
- **What Stage 1 cannot do.** It reports r9 against frozen Reno descriptively only: the same rule's ratio and interval, with no outcome label. It cannot revise #740's *regression reproduced* verdict.
- **Descriptive.** For every arm: goodput, pooled p50 and p95, and per-segment p50, p95 and mean. Per endpoint: entries and bytes ahead, the recorder facts (complete, retransmitted and split requests), and the per-block ratios.
- **Disclosed before data.** #738's descriptive figures ([latency-lead.json](../2026-10-13-bbr-r9-qualification-decision/latency-lead.json)), from instrumented builds at 20 replies per run: r9 ÷ r8 p95 1.42 [1.14–1.82], p50 1.54 [1.35–1.74]; BBR A/A p95 1.04 [0.66–1.28]. They are a lead, not an attribution.

## Registration: Stage 2, one contract-preserving remedy (conditional)

**Gate.** Stage 2 runs only if the depth reading is **depth raises latency** and the mechanism reading is **queue wait**. Otherwise it is **not attempted** and its 120 observations are released.

**The named remedy class (D3).** Exactly one class: **a bytes-ahead bound on BBR's send queue in place of its entry count**. It keeps D08's 2Q bound, the credit-return points, ownership, packet ordering and Reno's eight-entry queue unchanged. One entry is always admissible. The channel's 32 entries remain the physical maximum.

**The bound, set by a registered rule from Stage 1** ([ctlrules.py](ctlrules.py) `stage2_bound`). For each targeted endpoint e, from the Stage 1 recorder arms:
- W9 and W8 are the median queue waits at e's control-entry enqueues in r9-ctl and r9-depth8-ctl;
- D is r9's drain rate at e: the median of bytes ahead ÷ queue wait over r9's control enqueues with bytes ahead above zero;
- T = √(W8 × W9), the time ahead halfway (in log terms) between the two depths, and B = T × D;
- B is rounded down to whole 1,400-byte packets and clipped to [depth 8's median bytes ahead + one packet, r9's median bytes ahead − one packet].

If that interval is empty, no bound fits between the two depths and the remedy is **not attempted**. D3 does not admit restoring depth 8 or choosing a depth by trial, and this rule does neither: B comes from Stage 1's recorded bytes ahead and drain rate, before any Stage 2 data.

**Targeting** ([ctlrules.py](ctlrules.py) `targets`). The targeted endpoints are those whose own queue-wait share has a lower bound of at least 0.30, or both endpoints when neither does. One binary runs both endpoints and has no role, so one bound applies to every BBR queue: the sender's B if the sender is targeted and its bound is attempted, otherwise the receiver's. The Stage 2 mechanism section states which queue Stage 1 placed the rise in and how the bound acts there, including on the receiver's ACK-only entries.

**Not admissible here (D3).**
- A depth chosen by trial: speculative tuning.
- Restoring depth 8: it gives back the loopback goodput the queue bought.
- Draining the queue faster through batched sends: r9 has no batched send path on Linux.

**Registered after Stage 1, before any Stage 2 data,** in its own section, as #738 did: the computed bound and its inputs; the implementation, BBR-only in `send_queue.go`'s admission (`WouldBlock` and the full-queue signal); and its engagement counter, a measurement-only overlay counting byte-bound refusals at each endpoint.

**Gates before comparative data** (D3, as the previous decision's D4 Stage 2):
- a fixed-input policy-equality oracle for admissions and credit accounting;
- lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown;
- bound checks: queued bytes never exceed B plus one entry, and pending work never exceeds 2Q;
- #714's correctness, race and native-coverage gates;
- detection of an injected second contract violation.

A failure ends as **gates failed**, and a build failure as **unbuildable**.

**Activation.** An excluded activation run, `loopback-long` STREAM, must show the bound refusing admissions at each targeted endpoint on the Linux path. Every counted run of the new revision rechecks it. An inert or mistargeted treatment is uninformative and counts for nothing toward D6's prerequisite.

**Arms** ([matrix.py](matrix.py) `s2-loopback`, `s2-long`, `s2-s5`):
- loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r9, a second r9, the new revision, and Reno on the new revision, with `perf stat` (#712's events) on both endpoints of every arm for #714's preservation metric (72 observations);
- `loopback-long` STREAM, six blocks: frozen Reno, r9, a second r9 and the new revision (24 observations);
- S5 screen, both workloads, four blocks, seeds 9881–9884: r9, a second r9 and the new revision, plain (24 observations).

Stage 2 totals 120 observations, released if Stage 2 does not run.

**Keep rule and outcomes** ([ctlrules.py](ctlrules.py) `stage2_latency`, `stage2_outcome`). Each outcome is mutually exclusive, and they are classified in this order:
1. **Not attempted**, **unbuildable**, **gates failed**, **inert** or **mistargeted**: no usable treatment.
2. **Efficacy** on `loopback-long`: the pooled p95 ratio new ÷ r9 with its block-bootstrap interval (seed 746), against the r9 A/A ratio's interval from the same resamples:
   - **effective**: the upper bound is below 1, and the point estimate is below the A/A interval's lower bound;
   - **partial**: exactly one of those holds (non-disposing);
   - **demonstrated worsening**: the lower bound is above 1;
   - **null**: the lower bound is at least 1/1.15, so the new revision is not lower than r9 by more than the 15% margin. A wider interval can only make this harder to reach;
   - otherwise **inconclusive** (non-disposing): an interval such as 0.80–1.15 is inconclusive, not null.

   With fewer than five usable blocks, the reading is an evidence gap, and the outcome is inconclusive.
3. **Guards, for an effective treatment.** Each needs at least five of six usable blocks. Movements are medians against the BBR A/A [min, max], as #738's `_movement`:
   - loopback goodput, new ÷ r9, in each workload, not lower than the A/A range;
   - sender CPU per useful GiB, new ÷ r9, in each workload, not above the A/A maximum;
   - every observation exits cleanly with receiver integrity;
   - Reno on the new revision passes #714's preservation metric (#738's `preservation_cell`: user-plus-kernel cycles per useful GiB at each endpoint, workloads pooled, median inside frozen Reno's A/A range);
   - the S5 screen (#738's `s5_screen`, r9 as reference) shows no regression in either workload.

   The endings:
   - **kept** when every guard holds;
   - **effective, guard-failed** when a guard shows a regression beyond its range, with the trade recorded;
   - **inconclusive** when a guard's evidence is insufficient, such as an S5 screen with fewer than three usable blocks.

Only **null**, **demonstrated worsening** or **effective, guard-failed** disposes of the class, and only on an activated, correctly targeted, gate-passing implementation. The disposal is recorded separately from the outcome label. Stage 2's reading of the new revision against frozen Reno on `loopback-long` is selection evidence and is reported. It does not decide the survivor's cell, which D4 assigns to Ticket K.

## Inventory, contamination and endings

- **Observations.** Stage 1 has 36; Stage 2 has 120, released if it does not run. Together that is 156. Reruns are capped at 24, so the ticket stays within **180** Linux observations ([follow.py](follow.py) `CAP`, `RERUN_CAP`). The following are excluded from every statistic and from the cap, and listed in the record: the Stage 0 synthetic runs (24, plus their reruns), the Stage 2 activation runs, and the development runs.
- **Contamination.** #712's test (`localize.contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean, or 0.50 in any one-second sample) is handled by the operator's #714 rule. A block holding a contaminated or unusable observation is rerun once with the same seed under `observations-rerun`. The rerun replaces the block only if it is usable and uncontaminated (`rules.choose_block`). Reruns are triggered by host state, never by outcome. Every block waits first for 60 s of quiet fixture cores (#738's `wait_quiet`).
- **Endings.** The ticket ends with:
  - Stage 1's depth reading (depth raises latency, non-increase, inconclusive or evidence gap);
  - its mechanism reading (queue wait, load or other, mixed, inconclusive, instrumentation gap or evidence gap), endpoint- and segment-resolved;
  - its baseline reading (not reproduced, BBR baseline, inconclusive or gap);
  - a remedy outcome (Stage 2's, or not attempted), with the class's disposal recorded separately;
  - a named surviving revision: r9 unless a change is kept.

  D6's routing table of the accepted decision applies to these endings unchanged.

## Deviations

None.

## Limits

One Linux host and loopback endpoints, STREAM only for the attribution. The recorder observes stream-0 events. Kernel, socket and read-loop time between a submission and the peer's socket read is a residual it cannot place. Its timestamps compare across endpoints only because both processes run on one host. The depth comparison changes one constant, so placement differences between `cand` and `cand8` are minimal. Placement differences between plain and recorder builds are part of what the perturbation check measures. Stage 1 is evidence about r9 and its depth-8 control only.

## Assets

- **Results:** `results.json` (`stage0_synthetic_runtime`, `stage1`, and Stage 2 if it runs), written by [ctlstages.py](ctlstages.py).
- **Raw data:** `raw.tar.gz` with `raw-manifest.json`: every observation, development runs included, plus stage logs and build receipts. Binaries, credentials and exported trees are omitted; `build.py` rebuilds them.
- **To reconstruct:** in a fresh owned worktree of this branch, run `build.py`, `build.py identity` and `sync.sh`, then the [Order](#order) on a Linux host with the same core layout. Anywhere: `ctlstages.py all`, `ctlrules_test.py` and `rules_test.py`.

## Disclosures made before data

All of these runs are excluded from every statistic.

- **A Mac smoke of `cand-ctl`** (one 8 s loopback STREAM run of a darwin build, outside the registered surface; batched sends are available on Darwin). The chain was complete for all 8 measured requests, and the recorder's total matched the fixture's latency within about 1 µs. Medians: the sender's queue wait was 228 µs of a 337 µs total, behind about 30 entries (42.6 KB); the receiver's queue was empty. It was r9 alone, with no depth-8 or Reno arm, so it is not comparative. It shows only that the macOS sender's queue wait can dominate at this depth.
- **Development runs on `minimax`** (phases `dev*`): _to be listed before the registration commit._

## Order

On the Mac: `build.py`, `build.py identity`, `ctlrules_test.py`, `rules_test.py`, then `sync.sh`. On `minimax`, under `taskset -c 1-3`, with no build or test running there, [driver.sh](driver.sh) runs unattended:
1. `matrix.py syn`, then `follow.py reruns syn`;
2. `matrix.py s1`, then `follow.py reruns s1`.

Host facts are recorded before and after each stage. Pull the observations back, excluding `*.perf.data`, then run `ctlstages.py syn` and `ctlstages.py s1`. If Stage 1 opens the Stage 2 gate, its mechanism section is registered here before any Stage 2 data. Its gates and activation run next, followed by `matrix.py s2-loopback`, `s2-long` and `s2-s5`, with any reruns.
