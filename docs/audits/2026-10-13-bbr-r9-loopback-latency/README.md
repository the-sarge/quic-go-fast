# What raises BBRv3's loopback control latency on r9

**Date:** October 13, 2026, America/New_York. **Scope:** [Test whether BBRv3's 32-entry send queue raises its loopback control latency](https://github.com/the-sarge/quic-go-fast/issues/744), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D3 ("Ticket J") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/af90a739/docs/audits/2026-10-13-bbr-r9-qualification-decision/README.md#d3--ticket-j-what-raises-the-loopback-stream-control-latency-and-one-remedy-if-the-queue-is-responsible). Measurement, attribution and, only if the depth raises latency through queue wait, one contract-preserving change: no production merge, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r9-loopback-latency`, based on `af90a739` (the accepted decision), whose tree's code is exactly r9 plus a glossary. The revision under test is **r9, `81e9dc8c`**: r8 plus #738's 32-entry BBR send queue.

**Status: registration, revised after the first consideration. No comparative observation exists.** Everything below was written before any Stage 0 or Stage 1 observation of this ticket. The draft (`f36fd10b`) went through one `ras consider` round; its findings are dispositioned in [Consideration dispositions](#consideration-dispositions). The excluded development runs are listed in [Disclosures made before data](#disclosures-made-before-data). Accepting the decision did not authorize measurement; the operator's approval of this registration does.

## Question

On r9 (`81e9dc8c`), does the 32-entry BBR send queue raise the candidate's loopback STREAM control-stream p95 (1.485 in readiness; 1.849 [1.672–2.014] at an adequate sample in #740)? Where in the request's path does the rise sit? And does one contract-preserving remedy reduce it without giving back the loopback goodput the queue bought?

## What stays fixed and what changes

- **Fixed (D3):** r9 as the starting revision; frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; #712's fixture with the causal-diagnosis heap patch, launcher, endpoint contract (GOMAXPROCS from the four-core affinity, tracing disabled, 1,400-byte packets, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, a reliable control stream at 1 Hz), core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings on `minimax`; `GOTOOLCHAIN=go1.27.0`; #715's `loopback-long` timing (5 s warmup, 120 s measured: about 120 control replies per run); #712's contamination test with the operator's #714 rerun rule; #738's quiet-host wait.
- **Builds** ([build.py](build.py), adapted from #740's). `reno` and `cand` (plain r9) rebuild byte-identical to #740's binaries (`cda8feb1…`, `4fa75322…`), and so do the launcher (`a6146d62…`) and, for Stage 2's S5 screen, `relay-linux` (`24ee941d…`); `build.py` asserts it. Copied byte-identical from #740's record (`ddbac0a3`) and checked by `build.py`: [run.py](run.py), [stage_run.py](stage_run.py), [localize.py](localize.py) with [localize_test.py](localize_test.py), [pack.py](pack.py), [rules.py](rules.py) with [rules_test.py](rules_test.py), [go.mod](go.mod), [launch/](launch/), [model-overlay/](model-overlay/), [relay/](relay/). Copied byte-identical from #738's record (`791208b5`): [rules738.py](rules738.py), its `rules.py`, for the pinned Stage 2 guard helpers `preservation_cell` and `s5_screen`.
  - `cand8`: **r9 at depth 8**, r9's source with `bbrSendQueueCapacity = 8`. A diagnostic, never kept. Its exported tree differs from `cand`'s in that one line, so code placement barely differs.
  - `cand-ctl` and `cand8-ctl`: the two revisions with the control-path recorder ([The instrument](#the-instrument)).
  - `syn-*-ctl`: `cand-ctl` with one registered injection each ([Stage 0](#registration-stage-0-prerequisite-of-the-mechanism-reading)).
- **Identity and provenance** (D3: "The registration checks each build's identity and the queue capacity it selects at run time"):
  - every build writes a receipt (binary SHA-256, source label, patched-source hashes), and the committed copy in [builds/](builds/) is what the analysis checks observations against;
  - `build.py identity` runs `TestCtlDepthIdentity` (`bbrSendQueueCapacity` and `sendQueueCapacityFor` are 32 and 8 for BBR, 8 for Reno) and r9's five `TestBBRSendQueueDepth*` tests in the plain trees. It is fatal unless every expected test ran and passed, and unless the tested tree's `send_queue.go` is the one the build receipt hashed, which binds the result to the measured binary ([builds/identity.json](builds/identity.json)). The plain binaries carry no runtime capacity log: they stay byte-identical (`cand`) or one constant away from it (`cand8`), and this executable-bound test is their capacity evidence;
  - every recorder run records the capacity of each queue entry it tags, at both endpoints, and is unusable unless that is 32 (`cand-ctl`, `syn-*`) or 8 (`cand8-ctl`);
  - an observation is usable only if its executable's hash and its source label match the committed receipt for its variant, and its relay's hash matches when it has one ([ctlstages.py](ctlstages.py) `provenance`).
- **Runner:** every observation goes through #715's unchanged `stage_run.run_case_x` ([matrix.py](matrix.py)). A recorder build writes `ctl-send.json` and `ctl-receive.json` beside each endpoint's `-output`, so the runner needs no change. `matrix.py` adds one path, `loopback-syn` (5 s warmup, 60 s measured), for the synthetic cases only.
- **New measurement-only code:** the recorder ([ctl/ctlrec.go](ctl/ctlrec.go), written as `zz_ctlrec.go` in exported trees so it links after the transport's code) and its hooks (`build.py` `ctl_hooks`); the extraction ([ctl.py](ctl.py)); the rules ([ctlrules.py](ctlrules.py)) with their synthetic cases ([ctlrules_test.py](ctlrules_test.py)) and operating characteristics ([ctlrules_oc.py](ctlrules_oc.py)); the stage analysis ([ctlstages.py](ctlstages.py)) with its integration cases ([ctlstages_test.py](ctlstages_test.py)); the inventory ([inventory.py](inventory.py)); and the aids [art.py](art.py), [matrix.py](matrix.py), [follow.py](follow.py), [driver.sh](driver.sh) and [sync.sh](sync.sh). No tracked transport source changes; every instrumented or diagnostic build is an exported tree under `.local`.

## The instrument

D3 asks for a per-request recorder covering both endpoints' send queues, storing segment times rather than aggregates.

**What it records.** The fixture's control request is a 32-byte message on stream 0 carrying its id; the receiver echoes it on the same stream. Both endpoints run the same binary and stamp `runtime.nanotime` (CLOCK_MONOTONIC). On one host that clock is shared by both processes, so their stamps compare directly. Each endpoint logs these events ([ctl/ctlrec.go](ctl/ctlrec.go)):

| Event | Where | Fields |
| --- | --- | --- |
| write | `SendStream.Write`, stream 0, 32 bytes | the stream's write offset, the message id |
| pack | `popStreamFrame` / `popRetransmissionFrame`, stream 0 | frame offset range, retransmission flag, pack sequence |
| enqueue | `sendQueue.Send`, for the entry carrying the last popped stream-0 frame | entries queued ahead and their bytes (one coherent snapshot), queue capacity, entry bytes, the endpoint's cumulative enqueued bytes |
| dequeue | the send worker takes that entry (either send path) | — |
| submit | the worker is about to hand the entry to the socket, **before** the write | — |
| done | the entry's write returned (stamped in `release`, before the buffer is recycled) | — |
| discard | a tagged entry was released unsent (the queue had stopped) | — |
| arrive | `ReceiveStream.handleStreamFrame`, stream 0 | the packet's socket-read time (`rcvTime`, on the shared clock) and the handling time |
| read | `ReceiveStream.Read`, stream 0 | the read position after the read |
| note | the fixture's sender, per reply | the id, whether the request was offered in the measured window, the fixture's own send and receive stamps |

**Causal boundaries.** The submission boundary is the start of the socket write, so a peer's receipt always follows it. The write's own duration (submit to done) overlaps the transit and is reported separately per endpoint; it is not a segment.

**Message completion and the delivering transmission** ([ctl.py](ctl.py)). A message is complete at its reader when every one of its 32 bytes has been handled, accumulating frames in handling order. Reordered halves therefore cannot move time into the reader's wake segment, and a duplicate cannot replace the first completion. The completing frame's byte range names the transmission that delivered it. When that range was packed more than once (a retransmission) or matches no pack, the delivering transmission is **ambiguous**. The request is kept: its sender-side segments come from the first transmission carrying its last byte, and the remaining time, including any loss recovery, goes to the residual. Ambiguous, retransmitted and split requests are counted in the results.

**Segments.** One request's events join across the two logs by stream offset and id. They split its latency into fifteen consecutive segments that sum to the recorder's total, from the sender's write to the sender's read of the reply:

| Segment | Meaning |
| --- | --- |
| `s_pack` | sender: application write to packing (the delivering frame is popped) |
| `s_entry` | sender: packing to queue entry |
| **`s_queue`** | **sender: send-queue wait** (queue entry to the worker's dequeue), with entries and bytes ahead |
| `s_submit` | sender: dequeue to submission start |
| `t_req` | **residual**: submission start to the receiver's socket read (the write itself, kernel loopback, anything unplaced) |
| `r_backlog` | receiver: socket read to the completing frame handled by the connection loop (receive backlog) |
| `r_wake` | receiver: message complete to the application read |
| `r_app` | receiver: application read to the reply's write |
| `r_pack`, `r_entry` | receiver: the reply's packing and queue entry |
| **`r_queue`** | **receiver: send-queue wait** for the reply, behind ACK-only entries, with entries and bytes ahead |
| `r_submit` | receiver: the reply's dequeue to submission start |
| `t_rep` | **residual**: the reply's submission start to the sender's socket read |
| `s_backlog`, `s_wake` | sender: the reply's receive backlog and application wake |

**Queue occupancy.** When the connection loop tags an entry, it reads the channel's length once: that is the entries ahead. It sums the sizes of that many most recently enqueued entries from a 64-slot ring of entry sizes that only it writes: that is the bytes ahead. The queue is FIFO, so the queued entries are exactly the most recently enqueued ones, and both figures describe the same snapshot, the channel's count at that read. An entry the worker has already taken is excluded from both. The worker writes nothing for this.

**Clock conversion.** `rcvTime` is a process-relative monotonic time. Its offset to `runtime.nanotime` is calibrated by bracketing `monotime.Now()` between two `nanotime` reads and keeping the tightest of 101 samples. This is done at start and again at flush. The offset, its half-width and the second calibration are recorded.

**Cost control.** On ordinary packets the recorder adds a stream-ID compare in the frame paths, one store into the connection loop's private ring and two counter increments in `Send`, and, at the worker's dequeue, submission and release, one load of an in-flight tag count. That count is zero except while a control entry is queued, and these hooks inline to that load and a branch. Clock reads and the mutex-guarded event log run only for stream-0 events, about ten per request at each endpoint, one request a second. The log is a fixed 4,096-event array in BSS, not a heap allocation, so it leaves the Go heap and its GC goal unchanged ([Disclosures](#disclosures-made-before-data) give the reason). Two tag slots hold the entries in flight. A tag lost because both are busy is counted (`tags_exhausted`), and its request ends incomplete.

**Recorder usability** (per observation, [ctl.py](ctl.py) `usable`). An observation is usable when it exits cleanly with receiver integrity (#712's `summarize`), passes [provenance](#what-stays-fixed-and-what-changes), and:
- no event was dropped;
- each endpoint's clock calibration has a half-width of at most 1 µs at start and at flush, and its two offsets agree within 2 µs;
- every tagged entry's queue capacity is the build's (32, or 8 for `cand8-ctl`), at both endpoints;
- at least 95% of the measured requests have a complete chain, and at least 100 do (50 on the synthetic cases' 60 s path);
- no segment of a complete request is below −1 µs;
- for at least 99% of complete requests, the recorder's total is within 20 µs of the fixture's own latency for that reply.

A measured request is one the fixture's sender offered inside the measured window (its `note`). An incomplete measured request keeps its fixture latency in the mechanism reading ([Missing chains](#registration-stage-1-attribution)).

## Registration: Stage 0, prerequisite of the mechanism reading

Stage 0 validates the recorder and the mechanism estimator; it attributes nothing. **The mechanism reading runs only when all three of these pass:**
1. the data-level synthetic cases;
2. the runtime synthetic cases;
3. the perturbation check.

The first two must have passed with receipts whose fingerprint matches the current analysis code, rules, tests, recorder source and recorder builds ([ctlstages.py](ctlstages.py) `stage0_prerequisite`). A missing, failed or stale receipt, or a measured perturbation failure, ends the mechanism reading as an **instrumentation gap**. Fewer than five usable recorder-mask blocks for perturbation ends it as an **evidence gap**. Either way the Stage 2 gate stays closed, and the plain-arm depth and baseline readings still stand (D3). Stage 1 runs regardless.

### Runtime synthetic cases

Each case is `cand-ctl` plus one measurement-only injection ([ctl/ctlrec.go](ctl/ctlrec.go) constants, `build.py` `SYNTH`). [matrix.py](matrix.py) `syn` runs them on `loopback-syn`: loopback STREAM, 5 s warmup, 60 s measured, so about 60 replies per run. There are **four blocks of eight arms**: the base `cand-ctl`, a second `cand-ctl` (the near-zero case) and the six injections, in #712's rotation. Each case is read against the same block's base by the Stage 1 mechanism estimator, unchanged except for three settings: a minimum of three usable blocks, no consistency interval (there are no plain arms), and the prerequisite taken as met. The injections are compiled in and act by role (sender or receiver), so one binary runs both endpoints. The geometry was chosen from the calibration below, before any synthetic run.

| Case | Injection | Recovered when ([ctlstages.py](ctlstages.py) `SYN`) |
| --- | --- | --- |
| Deep sender queue | at the sender, the worker holds the tagged entry 300 µs before stamping its dequeue | **queue wait**, targets the sender only, median `s_queue` rise ≥ 210 µs |
| Deep receiver queue | the same at the receiver, for the reply | **queue wait**, targets the receiver only, median `r_queue` rise ≥ 210 µs |
| Slow worker | the sender's worker spins 20 µs before every submission | **queue wait**, the sender among the targets, median `s_submit` rise ≥ 14 µs |
| Busy receiver loop | the receiver's connection loop spins 2 µs for every received packet | **load or other** or **mixed** (never queue wait), with `r_backlog` named and a median `r_backlog` rise ≥ 50 µs. A busy loop also makes the reply wait behind bursts of ACK-only entries, so some receiver queue wait is a real by-product |
| Delayed reply write | the receiver's fixture spins 300 µs between reading a request and writing its reply | **load or other**, `r_app` named and no queue segment named, median `r_app` rise ≥ 210 µs |
| Unplaceable delay | at the sender, the tagged entry's submission start is stamped, then the worker spins 300 µs before its write | **load or other**, reported **unplaced** (only residual segments named, `t_req` among them) |
| Near-zero p95 rise | none: a second base arm | **inconclusive**. This validates that an A/A comparison receives no label; the reason is recorded |

The unplaceable case stamps the submission start before the delay, so from the recorder's point of view the packet left on time and arrived late. It stands for a delay in a stage the recorder does not observe, such as the kernel. A synthetic block is usable under the same rules as Stage 1's blocks, with `loopback-syn`'s 50-request minimum. Contaminated or unusable synthetic blocks get the same one same-seed rerun, outside the cap.

**Calibration of the recovery predicates** ([ctlrules_oc.py](ctlrules_oc.py) `syn`). Data-level analogs of each injection were run at 60 requests per run, 100 seeds per case and geometry, through the actual predicates (on the segments the predicates name):

| Blocks (minimum usable) | Deep sender queue | Deep receiver queue | Slow worker | Busy receiver loop | Delayed reply write | Unplaceable delay | Near-zero rise |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 3 (2) | 100 | 100 | 100 | 100 | 100 | 100 | 94 (6 read queue wait) |
| 4 (3) | 100 | 100 | 100 | 100 | 100 | 100 | 97 (3 read queue wait) |
| 5 (4) | 100 | 100 | 100 | 100 | 100 | 100 | 96 (4 read queue wait) |

Every injected case is recovered in every seed at every geometry. The near-zero case falsely reads queue wait in 3–6% of seeds whatever the block count (the few-block bootstrap again, with queue wait the noisiest segment), so more blocks buy nothing measurable. **Four blocks are registered.** A sound instrument is therefore expected to fail Stage 0 on the near-zero case about 3% of the time, which would end the mechanism reading as an instrumentation gap; that cost is accepted rather than loosening the case.

### Data-level synthetic cases

[ctlrules_test.py](ctlrules_test.py) builds per-request segment rows with known causes: six blocks of 120 requests per arm, with block-level variation shared by both arms. D3 names every case it builds:
- a deep sender queue, a deep receiver queue and a slow worker read **queue wait**, each targeting its endpoint;
- a busy receiver loop reads **load or other** with `r_backlog` named, and a delayed reply write reads **load or other** with `r_app` named;
- an unplaceable delay reads **load or other**, unplaced, with only the residual named;
- a near-zero p95 rise reads **inconclusive**, because the rise's interval does not exclude zero;
- **the second consideration's first counterexample** reads **load or other** with `r_app` named. In it, a 60 µs shift in the reply's application segment raises the p95 while 1% of requests carry 2 ms of sender queue wait. The upper-tail mean decomposition credits the queue; the test asserts it does, which is why tail means are supporting evidence only;
- **the second counterexample** reads **queue wait**. In it, a 60 µs queue rise raises the p95 while the reference arm carries 3 ms receive-backlog spikes on 3% of requests, so the upper-tail mean falls. The test asserts the fall;
- a 60 µs + 60 µs split between queue and application reads **mixed**, and intervals too wide to summarize read **inconclusive**, not mixed;
- **missing chains**:
  - the first consideration's reversal reads **inconclusive** rather than queue wait. In it, every request carries 100 µs more queue wait and 6% carry 600 µs more application time. The true reading with every chain is mixed, but five in six of the slow requests lose their chains;
  - random missingness at 2% keeps a strong queue-wait label;
  - missingness concentrated at each endpoint widens that share's envelope.

The depth, perturbation, Stage 2 bound, common bound, null margin, efficacy, guard and outcome rules, including empty and invalid inputs, are covered too. [ctlstages_test.py](ctlstages_test.py) adds extraction and integration cases:
- recorder logs generated from known segments read back exactly, including reordered halves, a lost original (ambiguous, recovery in the residual), duplicate delivery, a clock outside its limits, the wrong capacity and too few requests;
- in fake observation trees:
  - six clean plain blocks with every recorder arm invalid keep the depth and baseline readings;
  - empty stages write explicit gaps;
  - swapped `cand`/`cand8` binaries are unusable;
  - missing, failed or stale Stage 0 receipts close the prerequisite;
  - a contaminated block's clean rerun replaces it;
  - the inventory refuses reruns that are ineligible or over either cap.

The `datalevel` step ([ctlstages.py](ctlstages.py)) runs `ctlrules_test.py` and `rules_test.py` and records the result with its fingerprint. All tests pass: 38 rule cases, 13 extraction and integration cases (one checks operator decision 1 end to end), and #715's 43 copied rule tests.

**Operating characteristics of the depth rule** ([ctlrules_oc.py](ctlrules_oc.py) `depth`). The synthetic generator uses six blocks of 120 lognormal replies per arm, with σ 0.42, the p95/p50 of about 2.0 in #740's `latcand` arms, and shared block variation. Over 200 seeds per true r9 ÷ depth-8 ratio:

| True ratio | depth raises latency | non-increase | inconclusive |
| --- | --- | --- | --- |
| 1.0 | 9 | 175 | 16 |
| 1.075 | 51 | 69 | 80 |
| 1.15 | 141 | 10 | 49 |
| 1.3 | 195 | 0 | 5 |

The block bootstrap D3 requires is anti-conservative with six blocks. It reads "depth raises latency" about 4.5% of the time when depth has no effect, against a nominal 2.5%, and "non-increase" about 5% of the time at a true 15% rise, which is the margin. The rule is applied as registered; these rates are disclosed so the readings can be weighed.

### Perturbation (D3: "on the measured quantity")

D3 asks the registration to state where the check is scored. **It is scored on the counted Stage 1 arms**, in blocks chosen under the recorder mask, where each block holds both revisions plain and with the recorder ([ctlrules.py](ctlrules.py) `perturbation`). For each revision (r9 and r9 at depth 8):
- **goodput**: the median over usable blocks of instrumented ÷ plain `loopback-long` STREAM goodput must be at least **0.99** (D3);
- **control p95**: the pooled p95 ratio, instrumented ÷ plain, must lie within **[1/1.15, 1.15]**. The tolerance is set from #740's interval half-width of about ±9% at this sample (1.672–2.014 around 1.849) and from the lead's 1.42 r9 ÷ r8 ratio: a 15% shift cannot create or erase an effect of that size.

Each revision's state is **pass**, **fail** (a measured breach with at least five blocks), or **insufficient** (fewer than five blocks). Any fail makes the mechanism reading an instrumentation gap. Otherwise, any insufficient makes it an evidence gap.

**Operator decision 1 (recorded 2026-10-14, before any comparative observation).** The development runs ([Disclosures](#disclosures-made-before-data)) put the enabled recorder at about 0.975 of plain goodput without perf attached, and above its quiet twin with perf attached, at the same work per GiB. Under D3's floor the mechanism reading would then probably end as an instrumentation gap. That gap would come from a run-state sensitivity rather than the recorder's work. The latency clause, the one that guards the measured quantity, passed in every development pool (1.04–1.07). The operator decided:
- D3's check is scored and reported unchanged, and its registered ending stands.
- When it fails on the goodput clause alone, with the latency clause passing for both revisions and every other prerequisite met, the mechanism reading is also computed and reported beside the registered ending. It is labeled as read under a disclosed goodput perturbation, by operator decision, with the measured ratios beside it.
- That reading may open Stage 2, whose keep rule is judged on plain arms only.

The alternatives offered were to keep D3 strictly, which ends the mechanism reading and Stage 2 on a goodput failure, or to spend more development on the recorder, which is uncertain because the gap is not work. The analysis implements it as `mechanism_decision1` ([ctlstages.py](ctlstages.py) `s1`, `ctlrules.goodput_only_failure`); `stage2_gate_via` names which reading opened the gate. It departs from D3's text ("If either fails, the mechanism reading ends as an instrumentation gap") only by adding a reported reading; the registered ending is unchanged.

## Registration: Stage 1, attribution

- **Arms** ([matrix.py](matrix.py) `s1`). `loopback-long` STREAM, six blocks, #712's rotation: frozen Reno, r9, a second r9 (the BBR A/A control) and r9 at depth 8, all plain; and r9 and r9 at depth 8 with the recorder. 6 arms × 6 blocks = **36 observations**.
- **Masks and block choice** ([ctlstages.py](ctlstages.py) `chosen`). A block is rerun once, with the same seed, when its original attempt holds any contaminated or unusable observation (#714). Each analysis then chooses the block's attempt under its own mask with `rules.choose_block`. The **plain mask** (frozen Reno, r9, the second r9, depth 8) serves the depth and baseline readings. The **recorder mask** (r9 and depth 8, plain and with the recorder) serves perturbation and the mechanism reading. A recorder-only fault therefore never removes clean plain evidence.
- **Depth reading (plain mask)** ([ctlrules.py](ctlrules.py) `depth_reading`). The pooled p95 ratio r9 ÷ depth 8, with a 95% block-bootstrap interval (2,000 resamples of the usable blocks, seed 744, #715's index convention). The r9 A/A ratio (second r9 ÷ r9) gets its interval from the same resamples. The endings are evaluated in this order:
  1. **depth raises latency**: the interval's lower bound is above 1, at least five usable blocks have a per-block ratio above 1, and the point estimate exceeds the A/A interval's upper bound;
  2. **non-increase**: the interval's upper bound is below **1.15**. This is an equivalence test on the direct comparison alone, so a wider A/A interval can never make it easier to reach. The 15% margin is under a fifth of the cell's 85% excess over frozen Reno;
  3. otherwise **inconclusive**: compatible with noise, which is not a finding of no effect.

  An equivalence within the margin is reported either way. As in #715's rule, a block with a plain run under 100 replies is not counted; fewer than five counted blocks, or an arm under 500 pooled replies, is an **evidence gap**.
- **Mechanism reading (recorder mask)** ([ctlrules.py](ctlrules.py) `mechanism_reading`). The estimator is a **counterfactual p95**. Pool r9's complete per-request rows over the usable blocks. Replace each request's send-queue wait at both endpoints (`s_queue`, `r_queue`) with depth 8's queue wait at the same quantile rank: each segment is rank-matched separately, with mid-ranks (i + 0.5)/n and linear interpolation. Recompute the p95. The **share** is the fraction of the p95 rise that the replacement removes, with a block-bootstrap interval over replicates whose rise is positive (seed 745). The rise is r9-ctl's p95 minus depth8-ctl's p95, taken on the full sample of measured requests.
  - **Missing chains.** An incomplete measured request keeps its fixture latency, so the rise and its interval use every measured request. For the share, each arm's incomplete requests are placed wholly inside the evaluated segments, or wholly in the residual. Four combinations, one per arm, are evaluated over the same resamples. The share's interval is their envelope: the smallest lower bound and the largest upper bound. **A label holds only if no placement of the missing requests could change it.** With no incomplete request there is one combination.
  - **When the share is computed.** Only when:
    - Stage 0's prerequisite passes (otherwise **instrumentation gap** or **evidence gap**);
    - the recorder arms' pooled p95 ratio (the fixture's latencies) lies inside the plain arms' 95% interval for r9 ÷ depth 8 (otherwise **inconclusive**);
    - the rise's interval excludes zero (otherwise **inconclusive**);
    - at least 97.5% of replicates have a positive rise (otherwise **inconclusive**).
  - **Endings.**
    - **queue wait**: the share's lower bound is at least 0.70;
    - **load or other**: its upper bound is below 0.30;
    - **mixed**: otherwise, when the interval excludes both 0 and 1, so both components are shown;
    - otherwise **inconclusive** (an unstable share).
  - **Endpoint and segment resolution.** The same estimator replaces only the sender's queue wait, then only the receiver's, and reports each share with its envelope. Each segment, the two residuals included, gets its own counterfactual share. A segment is **named** when its share's lower bound reaches 0.30. A depth-caused rise in a placed segment, such as packing delay or receive backlog, is reported by that segment. A rise named only in the residuals (`t_req`, `t_rep`) is reported as **unplaced**: a qualifier on the ending, not a new ending, and no physical cause is assigned to it. Upper-tail mean differences are reported as supporting evidence only.
- **Baseline reading (plain mask).** #715's `latency_preservation` rule, unchanged, for depth 8 against frozen Reno. Its outcomes map as follows:
  - *not reproduced*: a depth-8 configuration would clear the cell;
  - *regression reproduced*: a **BBR baseline** component;
  - *inconclusive* or a gap: the baseline is unresolved.

  It describes the depth-8 configuration only and never decides a survivor's cell, which follows D4.
- **What Stage 1 cannot do.** It reports r9 against frozen Reno descriptively only: the same rule's ratio and interval, with no outcome label. It cannot revise #740's *regression reproduced* verdict.
- **Descriptive.** For every arm: goodput, pooled p50 and p95, and per-segment p50, p95 and mean. Per endpoint: entries and bytes ahead and their p95, write durations, the recorder facts (complete, incomplete, ambiguous, retransmitted and split requests, exhausted tags, channel-length disagreements), and per-block ratios.
- **Disclosed before data.** #738's descriptive figures ([latency-lead.json](../2026-10-13-bbr-r9-qualification-decision/latency-lead.json)), from instrumented builds at 20 replies per run: r9 ÷ r8 p95 1.42 [1.14–1.82], p50 1.54 [1.35–1.74]; BBR A/A p95 1.04 [0.66–1.28]. They are a lead, not an attribution.

## Registration: Stage 2, one contract-preserving remedy (conditional)

**Gate.** Stage 2 runs only if the depth reading is **depth raises latency** and the mechanism reading is **queue wait**, either the registered reading or, under operator decision 1, the reading taken beside a goodput-only perturbation failure. Otherwise it is **not attempted** and its 120 observations are released.

**The named remedy class (D3).** Exactly one class: **a bytes- or time-ahead bound on BBR's send queue in place of its entry count**. This registration uses its bytes member: one entry is always admissible, and the channel's 32 entries remain the physical maximum. It keeps D08's 2Q bound, the credit-return points, ownership, packet ordering and Reno's eight-entry queue unchanged. A null, worsening or guard-failed outcome disposes of the whole named class, not only its bytes member.

**The bound, set by a registered rule from Stage 1's bytes ahead and goodput** ([ctlrules.py](ctlrules.py) `stage2_bound`). For each endpoint e, from Stage 1's recorder-mask blocks:
- b9, b8: the median bytes ahead at e's control enqueues in r9-ctl and r9-depth8-ctl. m: e's median entry size in r9-ctl, the bound's granularity (1,400 bytes at the sender; the receiver's ACK-only entries are smaller);
- G9, G8: the plain r9 and depth-8 arms' median `loopback-long` STREAM goodput, in useful bytes per second (plain mask);
- k9, k8: e's wire bytes sent per useful byte delivered in each recorder arm. Each is e's send rate over the measured requests (cumulative enqueued bytes between its first and last control enqueue, per second) divided by that recorder arm's goodput, with the median over blocks. This is the conversion D3's goodput needs at the receiver, whose queue carries ACKs rather than payload;
- e's send rate in each arm is R = G × k. The time its median bytes ahead takes to drain is τ = b / R. Then T = √(τ8 × τ9), halfway in log terms between the depths, and **B = T × R9** bytes;
- B is rounded down to whole entries of size m and clipped to [b8 + m, b9 − m]. An empty interval, a non-positive input or a missing rate means **not attempted** at e.

D3 does not admit restoring depth 8 or choosing a depth by trial, and this rule does neither: B comes from Stage 1's recorded bytes ahead, goodput and send rates, before any Stage 2 data.

**Targeting and the common bound** ([ctlrules.py](ctlrules.py) `targets`, `common_bound`). The targeted endpoints are those whose own queue-wait share has a lower bound of at least 0.30, or both endpoints when neither does. One binary runs both endpoints and has no role, so one bound applies to every BBR queue. With one target, it is that endpoint's B. With two, it must be feasible at both: the sender's B clipped into the intersection of both endpoints' intervals, or the receiver's when the sender's is not attempted. An empty intersection, or a targeted endpoint whose bound is not attempted, means Stage 2 is **not attempted**. A target is never dropped to obtain engagement.

**Registered after Stage 1, before any Stage 2 data**, in its own section and in [stage2.json](stage2.json), as #738 did:
- the computed bound and its inputs;
- the implementation: BBR-only, in `send_queue.go`'s admission (`WouldBlock` and the full-queue signal clear when queued bytes reach B);
- the new revision's SHA;
- the gate, build and activation results, and the resulting status.

The analysis already reads all of these ([ctlstages.py](ctlstages.py) `s2`).

**Engagement counter.** The `new` build carries a measurement-only counter at every endpoint. It counts admissions, refusals by the byte bound and refusals by the entry count, and writes `eng-<role>.json` (`admissions`, `byte_refusals`, `entry_refusals`) at exit. The same build runs every Stage 2 arm of the new revision, so the counter's cost is common to them.

**Not admissible here (D3).**
- A depth chosen by trial: speculative tuning.
- Restoring depth 8: it gives back the loopback goodput the queue bought.
- Draining the queue faster through batched sends: r9 has no batched send path on Linux.

**Gates before comparative data** (D3, as the previous decision's D4 Stage 2):
- a fixed-input policy-equality oracle for admissions and credit accounting;
- lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown;
- bound checks: queued bytes never exceed B plus one entry, and pending work never exceeds 2Q;
- #714's correctness, race and native-coverage gates;
- detection of an injected second contract violation.

A failure ends as **gates failed**, and a build failure as **unbuildable**.

**Activation and status.** One excluded activation run (`loopback-long` STREAM, phase `act`) must show at least one byte-bound refusal at every targeted endpoint. The status is:
- **inert** when no targeted endpoint is engaged;
- **mistargeted** when some, but not all, targeted endpoints are engaged, or only a non-targeted one is;
- usable otherwise.

**Every counted `loopback-long` run of the new revision rechecks engagement.** A run without a byte-bound refusal at a targeted endpoint is unusable, so its block takes the #714 rerun. A block that stays unusable is not counted, and fewer than five counted blocks makes efficacy an evidence gap, which classifies as inconclusive. The loopback and S5 guard arms record the counter descriptively and do not require engagement, because the guards measure side effects whether or not the bound binds there.

**Arms** ([matrix.py](matrix.py) `s2-loopback`, `s2-long`, `s2-s5`):
- loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r9, a second r9, the new revision, and Reno on the new revision, with `perf stat` (#712's events) on both endpoints of every arm for #714's preservation metric (72 observations);
- `loopback-long` STREAM, six blocks: frozen Reno, r9, a second r9 and the new revision (24 observations);
- S5 screen, both workloads, four blocks, seeds 9881–9884: r9, a second r9 and the new revision (24 observations; `relay-linux` as built above).

Stage 2 totals 120 observations, released if Stage 2 does not run.

**Keep rule and outcomes** ([ctlrules.py](ctlrules.py) `stage2_latency`, `guard_states`, `stage2_outcome`; [ctlstages.py](ctlstages.py) `s2`). Each outcome is mutually exclusive, and they are classified in this order:
1. **Not attempted**, **unbuildable**, **gates failed**, **inert** or **mistargeted**: no usable treatment. An unrecognized status reads inconclusive.
2. **Efficacy** on `loopback-long`: the pooled p95 ratio new ÷ r9 with its block-bootstrap interval (seed 746), against the r9 A/A ratio's interval from the same resamples:
   - **effective**: the upper bound is below 1, and the point estimate is below the A/A interval's lower bound;
   - **partial**: exactly one of those holds (non-disposing);
   - **demonstrated worsening**: the lower bound is above 1;
   - **null**: the lower bound is at least 1/m₂, so the new revision is shown not to recover more than half of the smallest plausible depth increment;
   - otherwise **inconclusive** (non-disposing): an interval such as 0.80–1.15 is inconclusive, not null.

   The **null margin** is m₂ = min(1.15, 1 + ½ × (L − 1)), where L is Stage 1's depth-interval lower bound. A wider interval can only make null harder to reach. With fewer than five usable blocks, efficacy is an evidence gap, and the outcome is inconclusive.
3. **Guards, for an effective treatment.** Each guard is **pass**, **regression** or **insufficient**. Missing or invalid evidence is insufficient, never pass. Loopback movements are medians against the BBR A/A [min, max] over at least five of six usable blocks, as #738's `_movement`:
   - loopback goodput, new ÷ r9, not lower than the A/A range, in each workload;
   - sender CPU per useful GiB, new ÷ r9, not above the A/A maximum, in each workload;
   - integrity: every counted Stage 2 observation exited cleanly with receiver integrity. Provenance, recorder or engagement failures are not integrity failures;
   - Reno on the new revision passes #714's preservation metric (#738's `preservation_cell`: user-plus-kernel cycles per useful GiB at each endpoint, workloads pooled, median inside frozen Reno's A/A range; outside it either way fails, as #714 and #738 read it), with at least five pooled block ratios per endpoint;
   - the S5 screen shows no regression in either workload (#738's `s5_screen`, r9 as reference, at least three usable blocks).

   The endings:
   - **kept** when every guard passes;
   - **effective, guard-failed** when any guard shows a regression, even if another is insufficient (both recorded, with the trade);
   - **inconclusive** when no guard regresses but one is insufficient or missing.

**Calibration** ([ctlrules_oc.py](ctlrules_oc.py) `stage2`, 200 seeds each, six blocks of 120 lognormal replies per arm; recovery is the fraction of the depth increment, taken at L, that the new revision removes):

| Depth increment L | Recovered | m₂ | effective | partial | null | demonstrated worsening | inconclusive |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1.10 | none | 1.05 | 1 | 13 | 43 | 8 | 135 |
| 1.10 | half | 1.05 | 17 | 69 | 7 | 1 | 106 |
| 1.10 | all | 1.05 | 88 | 70 | 2 | 0 | 40 |
| 1.30 | none | 1.15 | 1 | 13 | 159 | 8 | 19 |
| 1.30 | half | 1.15 | 158 | 34 | 2 | 0 | 6 |
| 1.30 | all | 1.15 | 199 | 1 | 0 | 0 | 0 |

With a small increment the margin is narrow, so a remedy that recovers nothing usually reads inconclusive (non-disposing) rather than null, and one that recovers all of it reads null 1% of the time. With no true change, the six-block interval reads "demonstrated worsening" 4% of the time and "effective" 0.5%, the same anti-conservatism as the depth rule; disclosed, not corrected, because D3 fixes the interval.

Only **null**, **demonstrated worsening** or **effective, guard-failed** disposes of the class, and only on an activated, correctly targeted, gate-passing implementation. The disposal is recorded separately from the outcome label. The survivor is the new revision only if kept; otherwise it is r9. Stage 2's reading of the new revision against frozen Reno on `loopback-long` is selection evidence and is reported. It does not decide the survivor's cell, which D4 assigns to Ticket K.

## Inventory, contamination and endings

- **Observations.** Stage 1 has 36; Stage 2 has 120, released if it does not run. Together that is 156. Reruns are capped at 24, so the ticket stays within **180** Linux observations. The following are excluded from every statistic and from the cap, and listed in the record: the Stage 0 synthetic runs (32, plus their reruns), the Stage 2 activation runs (phase `act`), and the development runs (phases `dev*`).
- **Enforcement at the launch boundary** ([inventory.py](inventory.py)). Every rerun, whether `follow.py` or a direct `matrix.py rerun` asks for it, passes `admit_rerun`. The stage and block must be registered, the block's original attempt must be not clean with no rerun yet, and, for counted stages, the retained counted observations plus the rerun's arms must stay within 180, and counted reruns within 24. Every retained directory counts, failed or partial launches included. Reruns are allocated in a fixed order: stage (`syn`, `s1`, `s2-loopback`, `s2-long`, `s2-s5`), then workload, then block. A refused rerun is recorded in `follow-decisions.json`.
- **Contamination.** #712's test (`localize.contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean, or 0.50 in any one-second sample) is handled by the operator's #714 rule, as [Masks and block choice](#registration-stage-1-attribution) states. Reruns are triggered by host state or unusability, never by outcome. Every block waits first for 60 s of quiet fixture cores (#738's `wait_quiet`).
- **Endings.** The ticket ends with:
  - Stage 1's depth reading (depth raises latency, non-increase, inconclusive or evidence gap);
  - its mechanism reading (queue wait, load or other, mixed, inconclusive, instrumentation gap or evidence gap), endpoint- and segment-resolved, with the unplaced qualifier where it applies;
  - its baseline reading (not reproduced, BBR baseline, inconclusive or gap);
  - a remedy outcome (Stage 2's, or not attempted), with the class's disposal recorded separately;
  - a named surviving revision: r9 unless a change is kept.

  D6's routing table of the accepted decision applies to these endings unchanged. Every analysis step writes its result to `results.json`, explicit gaps included.

## Deviations

1. **Operator decision 1, before data** (2026-10-14): a goodput-only perturbation failure still yields a reported mechanism reading that may open Stage 2, beside D3's registered instrumentation-gap ending ([Perturbation](#perturbation-d3-on-the-measured-quantity)). Decided after the development runs and before any comparative observation.

## Limits

One Linux host and loopback endpoints, STREAM only for the attribution. The recorder observes stream-0 events. Kernel, socket and read-loop time between a submission start and the peer's socket read is a residual it cannot place, and the socket write itself overlaps it. Its timestamps compare across endpoints only because both processes run on one host. The depth comparison changes one constant, so placement differences between `cand` and `cand8` are minimal. Placement differences between plain and recorder builds are part of what the perturbation check measures. Six-block bootstrap intervals are anti-conservative (see the calibrations). Stage 1 is evidence about r9 and its depth-8 control only.

## Consideration dispositions

**First consideration**, RAS run `20261013T225805-437280c7a0572b13cde64d05`, on the draft `f36fd10b` (five reviewers with adjudication). Its synthesis said the registration was not ready for comparative measurement and listed 19 items:

| # | Finding | Disposition | Where |
| --- | --- | --- | --- |
| 1 | Stage 0 not enforced before attribution | Fixed. Fingerprinted data-level and runtime receipts plus perturbation are the mechanism reading's prerequisite; missing, failed or stale means instrumentation gap | `ctlstages.stage0_prerequisite`, [Stage 0](#registration-stage-0-prerequisite-of-the-mechanism-reading) |
| 2 | Missing chains could censor the p95 | Fixed. Incomplete requests keep their fixture latencies; the share is an envelope over every placement; the reversal example is a test | `ctlrules.share_interval`, `Missing` tests |
| 3 | Stage 2 guards fail open | Fixed. Explicit pass, regression and insufficient; empty or invalid evidence never keeps or disposes; an efficacy gap reads inconclusive | `ctlrules.guard_states`, `stage2_outcome` |
| 4 | A recorder fault erased plain evidence; empty runs crashed | Fixed. Separate plain and recorder masks; insufficient versus failed perturbation; explicit gaps | `ctlstages.chosen`, `s1`; integration tests |
| 5 | Synthetic runs could never be usable | Fixed. A 50-request minimum on `loopback-syn` only; Stage 1 keeps 100 | `ctl.MIN_COMPLETE` |
| 6 | Identity not bound to observations | Fixed. Fatal identity tests bound to the receipt's source hash; per-observation provenance; the plain-binary capacity contract stated | `build.py identity`, `ctlstages.provenance` |
| 7 | Submission stamped after the write | Fixed. The submission start precedes the write; completion is separate | `ctlrec.go`, `ctl.py` |
| 8 | Arrival chosen by the last byte, not message completion | Fixed. Byte-coverage completion, the delivering transmission by exact range, and an ambiguity policy with time in the residual | `ctl._completion`, `_leg` |
| 9 | Reruns and caps not enforced at launch | Fixed. `inventory.admit_rerun` in `matrix.py rerun`; every stage, including Stage 2, is supported | `inventory.py`, `follow.py` |
| 10 | The bound ignored goodput | Fixed. τ = b ÷ (G × k), with goodput converted per endpoint | `ctlrules.stage2_bound` |
| 11 | Byte granularity and multiple targets | Fixed. Per-endpoint entry granularity; a common bound must be feasible at every target, or Stage 2 is not attempted | `ctlrules.common_bound` |
| 12 | No Stage 2 adapter or activation contract | Fixed. A full adapter; the engagement counter, criterion and recheck registered; `stage2.json` contents fixed | `ctlstages.s2`, [Stage 2](#registration-stage-2-one-contract-preserving-remedy-conditional) |
| 13 | No pinned relay | Fixed. `relay-linux` is rebuilt and hash-asserted; provenance checks it | `build.py relay` |
| 14 | Incoherent occupancy snapshot | Fixed. The channel's length, read once, and the matching ring of entry sizes give one snapshot; the worker writes nothing for it | `ctlrec.go` |
| 15 | Clock conversion uncertainty unbounded | Fixed. Bracketed calibration at start and flush, with registered limits | `ctlrec.go`, `ctl.usable` |
| 16 | The null margin could dispose on a small increment | Fixed. m₂ is tied to Stage 1's depth-interval lower bound and capped at 1.15; calibrated | `ctlrules.stage2_null_margin` |
| 17 | Runtime synthetic calibration | Fixed. Calibrated at 3, 4 and 5 blocks through the actual predicates; four blocks registered | `ctlrules_oc.py syn` |
| 18 | Evidence inventory placeholders | Fixed. Build receipts and identity committed; development runs listed below | [builds/](builds/), [Disclosures](#disclosures-made-before-data) |
| 19 | Bookkeeping and unplaced reporting | Fixed. Tag exhaustion counted, discards logged, and an explicit unplaced qualifier | `ctlrec.go`, `ctlrules.mechanism_reading` |

Not acted on, as the synthesis advised:
- C-011: the narrower meaning of mixed stays, since unstable shares read inconclusive under D3;
- C-012: injection saturation is validated by the runtime cases, not presumed;
- C-035: the perturbation floor stays at D3's 0.99;
- no change to D3's structure, gate, inventory or remedy class;
- no clamping or silent exclusion.

## Assets

- **Results:** `results.json` (`stage0_data_level`, `stage0_synthetic_runtime`, `stage1`, `stage2`), written by [ctlstages.py](ctlstages.py).
- **Builds:** [builds/](builds/): every build receipt and the identity result.
- **Raw data:** `raw.tar.gz` with `raw-manifest.json`: every observation, development and activation runs included, plus stage logs and build receipts. Binaries, credentials and exported trees are omitted; `build.py` rebuilds them.
- **To reconstruct:** in a fresh owned worktree of this branch, run `build.py`, `build.py identity` and `sync.sh`, then the [Order](#order) on a Linux host with the same core layout. Anywhere: `ctlstages.py all`, `ctlrules_test.py`, `ctlstages_test.py` and `rules_test.py`.

## Disclosures made before data

All of these runs are excluded from every statistic.

- **Mac smokes of `cand-ctl`** (two short loopback STREAM runs of darwin builds, outside the registered surface; batched sends are available on Darwin): the first and the final recorder. Every measured request had a complete chain, and the recorder's total matched the fixture's latency within about 1 µs. In the first (8 s, 8 requests), the sender's queue wait was 228 µs of a 337 µs median total, behind about 30 entries. These are r9 alone, with no depth-8 or Reno arm, so not comparative.
- **Development runs on `minimax`** (phases `dev1`–`dev9`, under the registered runner, excluded from every statistic and retained under `observations/`). **No development run had a depth-8 or frozen-Reno arm**, so none is comparative. Each compares r9 plain with recorder builds of r9, or runs the synthetic builds.
  - `dev1`: queued, then stopped before it started, because the host was busy and the recorder was being revised.
  - `dev2` (2 blocks) and `dev4` (2 blocks), `loopback-long`, with the first recorder, which kept its log on the Go heap: instrumented ÷ plain goodput 0.98, 0.969, 0.968 and 0.974; pooled p95 ratio 1.47 and 1.04. In `dev4`, a **quiet build** with every hook present but logging never enabled read 1.003 and 0.993 (p95 0.95). The first recorder failed both perturbation limits.
  - `dev5` and `dev7` (3 blocks each), the final recorder (log in BSS, worker-side hooks reduced to one load): goodput 0.979, 0.997, 0.980 and 0.983, 0.984, 0.986; pooled p95 ratio 1.07 and 1.05. Its quiet build read 1.018, 1.017, 1.012 (p95 0.98). The enabled and quiet builds run identical per-packet code; enabled logging adds about ten stream-0 events per second at each endpoint (counted). The about 3% gap between them follows the binary's code layout: the quiet build's smaller `init` shifted every later function in the package. That matches #738's measured placement sensitivity of about 3% on loopback STREAM. The recorder was then written as `zz_ctlrec.go`, so its functions link after the transport's.
  - `dev8` and `dev9` (3 blocks each), the final recorder linked last: instrumented ÷ plain goodput 0.976 and 1.000 in `dev8` (its second block was hit by foreign load and is not read), then 0.975, 0.971, 0.979 in `dev9`. The quiet twin, linked last too and so sharing the transport's layout, read 1.003, 1.000, 1.005. Link order therefore does not explain the gap.
  - `dev10` (2 blocks, `perf stat` on both endpoints): the enabled recorder and its quiet twin. **With perf attached the order reversed:** the enabled recorder read 1.079 and 1.023 of the quiet twin's goodput. Their work per useful GiB matched within about 0.5% at both endpoints: sender user instructions 25.80 and 25.85 against 25.93 and 25.94 G/GiB, receiver 22.27 and 22.34 against 22.48 and 22.46. The enabled recorder used slightly fewer cycles. **The recorder adds no measurable work**, and the goodput differences of ±3% between builds follow a run-state sensitivity of this loopback fixture that small timing differences flip, the same class of effect #738 measured.
  - `dev3` (1 block, the first recorder) and `dev6` (2 blocks, the final recorder), `loopback-syn`, the synthetic builds against the base. Each injection landed in its segment: the deep sender and receiver queues added about 300 µs to `s_queue` and `r_queue`; the slow worker 20 µs to `s_submit` and about 670 µs to `s_queue`; the delayed reply 300 µs to `r_app`; the unplaceable delay 300 µs to `t_req` only. At 1 µs per packet the busy receiver loop raised `r_backlog` by 30–40 µs, but the p95 rose little, because the base's tail sits in the reply's transit. At 2 µs it raised `r_backlog` by 80–100 µs, `r_pack` by about 60 µs and `r_queue` by about 30 µs, a by-product of ACK bursts, with goodput 33% lower. **The registered strength was therefore raised to 2 µs, and its predicate accepts mixed** (never queue wait), as #738 tuned its injections. No other strength or predicate changed after a development run.
- **Values seen for r9 before registration** (the final recorder, 6 `loopback-long` runs, 720 requests, all complete; development only). The sender's queue held a median of **9 entries ahead** (95th percentile 27; 12.6 KB), not a full 32, with a queue wait median of 22.5 µs (p95 55.7). The reply's transit to the sender's socket read had a median of 15 µs but a p95 of 92 µs. The sender's application wake had a median of 35 µs (p95 61). The total had a median of 129 µs and a p95 of 247 µs. These describe r9 alone and say nothing about depth. They do hint that the p95 tail is not mainly queue wait, which is why they are disclosed. No rule, threshold or arm changed after they were seen. The perturbation tolerance, margins and estimator were registered in the draft `f36fd10b` before any development run produced recorder output.
- **Calibrations** ([ctlrules_oc.py](ctlrules_oc.py)), synthetic data only: the depth-rule, Stage 2 and synthetic-geometry tables above. The synthetic geometry (four blocks) and the Stage 2 null margin were chosen from them.

## Order

On the Mac: `build.py`, `build.py identity`, `ctlrules_test.py`, `ctlstages_test.py`, `rules_test.py`, then `sync.sh`. On `minimax`, under `taskset -c 1-3`, with no build or test running there, [driver.sh](driver.sh) runs unattended:
1. `matrix.py syn`, then `follow.py reruns syn`;
2. `matrix.py s1`, then `follow.py reruns s1`.

Host facts are recorded before and after each stage. Pull the observations back, excluding `*.perf.data`, then run `ctlstages.py datalevel`, `syn` and `s1`. If Stage 1 opens the Stage 2 gate, the next steps are, in order:
1. register Stage 2's mechanism section and [stage2.json](stage2.json), before any Stage 2 data;
2. build and gate the new revision;
3. run its activation;
4. run `matrix.py s2-loopback`, `s2-long` and `s2-s5`, then `follow.py reruns s2-loopback s2-long s2-s5`;
5. run `ctlstages.py s2`.
