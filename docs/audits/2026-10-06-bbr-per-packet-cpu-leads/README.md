# Remaining BBR per-packet CPU leads and the receiver CPU measure

**Date:** October 6, 2026, America/New_York. **Scope:** [Test the remaining BBR per-packet CPU leads and the receiver CPU measure](https://github.com/the-sarge/quic-go-fast/issues/735), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D4 ("Ticket D") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md#d4--ticket-d-the-remaining-per-packet-cpu-cost-and-the-receiver-cpu-measure). Experimental revisions and counters only: no production merge, readiness flag, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-per-packet-cpu-leads`, based on `04094bd7` (the [pacing-wake record](../2026-10-05-bbr-pacing-wake/README.md)). The starting revision is **r8, `95f5b6b7`**: `d0fabc4d` plus the Linux netpoller kick at the BBR pacing deadline, kept by operator decision after a negative registered outcome (that record's deviation 3). It carries an open cost: run-loop wakes per useful GiB up 6–16% on S5.

**Status: registration.** The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) are written, with the analysis code and its synthetic cases, before any counted observation of this ticket. Excluded smoke and probe runs that preceded them are disclosed in [Disclosures made before data](#disclosures-made-before-data).

## Question

On r8, which of at most three profile-selected BBR per-packet CPU leads can be removed by contract-preserving intervention, and does a lower effective clock rate or idle effect explain the receiver's CPU-time excess over its near-Reno cycles?

## What stays fixed and what changes

- **Fixed (D4):** frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; r8 as the starting revision; #712's fixture with the causal-diagnosis heap patch, relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter), launcher, endpoint contract (GOMAXPROCS from the four-core affinity, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream), core layout (sender 8–11, receiver 12–15, relay 4–5, runner and `perf` 1–3, SMT siblings idle) and host settings on `minimax`; `GOTOOLCHAIN=go1.27.0`; S5 timing (10 s warmup, 30 s measured); every model parameter, declared bound, recorded QUIC translation, evidence contract and default. The governor (`powersave`, `amd-pstate-epp` active, EPP `performance`), boost, idle states (POLL, C1, C2, C3, none disabled) and `cpuidle` governor (`menu`) are recorded read-only by `matrix.py hostfacts` at the start and end of every stage and are never changed.
- **Builds:** [build.py](build.py), adapted from #714's. `reno`, `relay-linux` and the launcher rebuild byte-identical to #715's and #734's binaries (`cda8feb1…`, `24ee941d…`, `a6146d62…`), and `r8` byte-identical to #734's plain `new` build (`b7171142…`). Copied byte-identical from #714 (`04094bd7`) and checked by `build.py`: [run.py](run.py), [localize.py](localize.py), [intervene.py](intervene.py), [launch/](launch/), [relay/](relay/), [model-overlay/next.go](model-overlay/next.go).
- **Changed, all recorded:** at most three intervention revisions, one commit each, in selection order; this ticket's measurement arms; and three instrument additions, made in [matrix.py](matrix.py) without editing the runner:
  - `perf stat` also counts `task-clock` (software) and `msr/aperf/`, `msr/mperf/` (the MSR PMU). None uses a general-purpose counter, so #712's sixteen events still count without multiplexing.
  - When `perf` starts and stops on an endpoint, the idle-state counters (`cpuidle/state*/usage`, `time`) of that endpoint's four cores are read from sysfs into `<role>.idle.json`.
  - A read-only host-facts snapshot per stage.
- **New aids:** [lead_profile.py](lead_profile.py) with [lead_profile_test.py](lead_profile_test.py) (7 cases), [receiver.py](receiver.py) with [receiver_test.py](receiver_test.py) (11 cases), [measure.py](measure.py), [sync.sh](sync.sh).

## Registration: profile and lead selection

- **Runs** ([matrix.py](matrix.py) `profile`). S5, both workloads, two blocks (seeds 10001–10002), arms frozen Reno and r8, plain builds, #712's rotation. `perf record -e cycles -F 4999 --call-graph fp` on both endpoints for the measured window (#712's `callgraphs` settings). 8 observations.
- **Groups (descriptive).** #712's `classify_stack` groups and `localize` rule, unchanged, at both endpoints, r8 against frozen Reno ([lead_profile.py](lead_profile.py)). Reported, never decisive.
- **Sites.** Every BBR-only function: #712's `BBR` pattern plus the functions defined in the root package's BBR-only files at r8 (`bbr_*.go`, `local_send_credit.go`, `packet_emission_bbr.go`, `pacing_kick_linux.go`) and their closures (`ROOT_BBR_ONLY`). A site's cost is its inclusive cycles per useful GiB at the r8 sender; each sample counts once per site on its stack. Reno never executes these functions, so this is their whole cost. Congestion and BBR ECN feedback, deferred from #714, are BBR-only and eligible.
- **Selection** (`select_leads`). Sites are ranked by median cost over the four workload-blocks. Walking the ranking:
  - selection **ends** at the first site whose median is below 0.15 Gcycles per useful GiB;
  - a **container** (more than 0.25 of r8's sender cycles in every block) is skipped;
  - a site absent from any block is skipped;
  - a site with half or more of its cycles in samples that also hold an already-selected lead **overlaps** it and is skipped;
  - otherwise the site's **eligibility** is judged from source at r8, in rank order, and recorded with its reason in `eligibility.json`: eligible when one change confined to code that does not run when Reno is selected can reduce the site's per-packet work, the change avoids #714's not-allowed list, and an equivalence domain and oracle can be stated against r8. An ineligible site (for example, one whose cost is required model computation, or one too broad for one change) is skipped with its reason, and its callees stay eligible.
  - At most three sites are selected.
- **A profile selects leads; it never attributes a cause.** The 30 leaf functions with the largest median sender excess (any code) and the group table are reported beside the selection.
- **Usability.** A profile observation is usable when it exits cleanly, passes receiver integrity and `perf` stops normally. A block holding a contaminated or unusable observation is rerun once with the same seed (`profilererun`), replacing the original only if usable and uncontaminated. If either workload has no usable block, the ticket ends with **unusable counters**.

## Registration: interventions

#714's rules, unchanged ([#714 record](../2026-10-03-bbr-per-packet-interventions/README.md#registration-interventions)):

- Each intervention is one commit on the current surviving revision and names its hypothesis, the contracts it can affect, its equivalence domain against its predecessor, the oracles and gates that must pass before it is measured, and why Reno's path is untouched. Each lead's entry is written in [Lead registrations](#lead-registrations) after the profile and before any of that lead's measurement data.
- **Not allowed:** a parameter, model or declared-bound change; send-path batching (GSO or `sendmmsg`); any change to code executed when Reno is selected; a change to a recorded translation, evidence contract, controller version or default; weakening any existing test, gate or work bound. At most one revision per lead.
- **Branch mechanics.** A change that is not kept is reverted by a separate commit, so the next intervention builds on the survivor and the failed change stays in history. A change whose gates or equivalence fail is never measured. A lead whose change needs a contract change, or cannot be made equivalent, stops and is reported; independent leads continue after a null.
- **Gates** before measurement, recorded in `gates/lK.log`: #714's [gates.sh](gates.sh) set (vet; ackhandler and congestion with and without `-race`; the `bbrworkcount` tag; the root BBR, emission, credit, delivery, ECN, continuation, pacing and capability subset with and without `-race`; the full root package), the lead's named oracles, and on `minimax` the native root subset, `-race` on the lead's tests and the pacing-kick tests, and the full root package natively, run under `taskset -c 1-3` only between stages.

## Registration: measurement

- **Runs** ([matrix.py](matrix.py)). Measurement `mK` for lead K: S5, both workloads, six blocks, seeds `10000 + 10K + block`, #712's rotation, five arms (frozen Reno, A/A Reno, the predecessor with BBRv3, the new revision with BBRv3, Reno on the new revision), plain builds, `perf stat` on both endpoints for the measured window. 60 observations.
- **Cumulative check `mc`** in #714's form: the same five arms with r8 as the predecessor and the final surviving revision as the new revision, seeds 10041–10046. It runs only if the surviving revision differs from r8 and from every new revision already measured against r8.
- **Rules:** #714's, unchanged, applied by its own [intervene.py](intervene.py) through [measure.py](measure.py) `outcome`: usability (every event at ≥ 95% running time, now including the three added events); the primary metric, sender user instructions per forward packet, classified against the A/A range (reduction, increase, null, inconclusive); the cycles disagreement check; the workload combination; the pooled preservation cells (Reno on the new revision, cycles per useful GiB at each endpoint); integrity; and the outcome (keep, negative, null, inconclusive). Only a kept change survives.
- **Contamination and reruns** (#714 deviation 5, prospective here; `choose_block`). #712's test (foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample). A block holding an unusable or contaminated observation is rerun once with the same seed under the phase suffix `rerun`, replacing the original only if usable and uncontaminated. Contaminated observations are retained and reported.
- **Reported effects:** the conditional net effect per lead (median per-block change in sender user instructions per forward packet), the residual excess over frozen Reno per packet for the predecessor and new revisions, and every metric's ratios at both endpoints. No readiness flag is set here; a counter result implies no whole-run CPU readiness conclusion.

## Registration: receiver CPU time against cycles

- **Question.** At the S5 receiver, the readiness CPU time per useful GiB was 1.11–1.16× Reno inside the measured window (#715), while #714 measured receiver window cycles per GiB at 1.001–1.011 of Reno on r5. What explains CPU time exceeding near-equal cycles?
- **Hypothesis:** a lower effective clock rate or idle effects. **Competitor:** receiver work the cycle count misses.
- **Measures,** per receiver observation over the measured window, per useful GiB: T = `task-clock` (the scheduled run time that `getrusage` CPU time sums); I = instructions and C = PMC cycles, user plus kernel; A = APERF and M = MPERF, counted per task by the MSR PMU (APERF counts actual clocks and MPERF reference clocks while the core is in C0, read at the same scheduling points as `task-clock`). Then exactly, T = I × (C/I) × (A/C) × (M/A) × (T/M), and per block, against the same block's frozen Reno, the log ratios partition ln(T ratio) ([receiver.py](receiver.py)):

  | Component | Log ratio | Explanation |
  | --- | --- | --- |
  | work | ln(I ratio) | counted receiver work |
  | cpi | ln((C/I) ratio) | counted receiver work (cycles per instruction) |
  | uncounted | ln((A/C) ratio) | **competitor:** clocks inside the receiver's scheduled time that PMC cycles miss |
  | clock | −ln((A/M) ratio) | **hypothesis:** a lower average clock while running |
  | closure | ln((T/M) ratio) | task-clock not matched by C0 reference clocks; a check, not an explanation |

- **Rule** (`verdict`), per workload, on usable blocks (at least four; otherwise **evidence gap**):
  1. **Unusable counters** when the median closure of the arm or of the A/A arm exceeds 0.01 in magnitude.
  2. **No in-window CPU-time excess** when the median ln(T ratio) does not exceed the A/A arm's maximum.
  3. Otherwise, per block with a positive excess, each explanation's share of ln(T ratio); medians over blocks. Two or more explanations at 0.25 or more is **mixed**. One at 0.5 or more with every other below 0.25 **leads**, and is the verdict (**clock rate**, **uncounted receiver work** or **counted receiver work**) if its median component lies above the A/A arm's maximum for that explanation; otherwise **inconclusive**. Anything else is **inconclusive**. These are #715's share thresholds.
- **Decisive runs.** The first lead measurement (`m1`): its predecessor arm is r8, against frozen Reno, with its A/A arm. The same rule is applied descriptively to every other measured arm (the predecessor and new revisions of every measurement, and Reno on the new revision) to show whether the verdict repeats. The measure uses the lead measurements' runs and adds none: if no lead reaches measurement, the receiver measure is **not measured**. Its usability additionally needs the three added events at ≥ 95% running time.
- **Descriptive, never decisive:** CPU time and cycles ratios; whole-run CPU per GiB; APERF ÷ MPERF per arm; uncounted clocks per context switch; context switches and migrations per GiB; per idle state, entries per useful GiB and residency over the receiver's four cores.
- **Limits stated now.** The readiness measure stays CPU time: an attribution here never changes the limit or clears the flag; it labels the cell for a later exception decision. Counting attaches `perf` to every thread, which adds cost at every context switch; that cost is inside T, A and M and outside C, so it inflates the uncounted component in every arm. Readiness runs have no `perf` attached. Per-switch figures are reported for both arms so the reader can see how much of the uncounted component tracks switching.

## Inventory, contamination and endings

- **Observations** (D4; at most 300 Linux observations, reruns included): profile 8; cumulative check 60, reserved; reruns up to 52, reserved; leads 3 × 60 = 180. A lead measurement starts only if the budget left after it still holds the reserved cumulative check. If the budget runs short, leads are dropped, never the cumulative check. Smoke and probe runs are excluded from every statistic and from the cap and are listed in the record.
- **Endings.** Every ending reports to [Re-demonstrate the timing- and CPU-tested BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/736) with an identified surviving revision (r8 if nothing is kept) and its unresolved evidence: leads kept; all null, negative or stopped; unusable counters; failed gates; or exhausted budget. The receiver verdict, or its absence, is reported with it.

## Profile result and lead selection

Run on October 6–7 (UTC) on the booked host; [profile.json](profile.json). All eight observations exited cleanly, passed receiver integrity and stopped `perf` normally. No observation was contaminated (largest foreign mean 0.004 cores), so no rerun was used.

- **Groups (descriptive).** At the r8 sender the excess over frozen Reno is +3.81 (STREAM) and +5.37 (DATAGRAM) Gcycles per useful GiB, on 30.0–35.4 in total. No group holds half of it in every block, so #712's rule reads **inconclusive**, as on #712. Median shares: BBR 0.36 / 0.26, other user 0.28 / 0.20, runtime scheduling 0.14 / 0.24, kernel wake and scheduling 0.11 / 0.22. At the receiver the rule reads **no repeatable excess**: r8's receiver runs −0.82 / −1.03 Gcycles per GiB **below** Reno in the median. This is a profile, not the receiver rule.
- **Sites.** The ranking, with each eligibility judgement made from source at r8 in rank order before any lead's measurement, is in [eligibility.json](eligibility.json) and `profile.json` `sites.decisions`:

  | Rank | Site (Gcycles/GiB, median) | Judgement |
  | --- | --- | --- |
  | 1 | `(*packetEmission).sendBounded` (4.76) | ineligible: too broad; 3.68 is `boundedDatagrams` |
  | 2 | `(*packetEmission).boundedDatagrams` (3.68) | ineligible: shared packing (`appendPacket` 3.08) and the send-queue hand-off |
  | 3 | `(*sentPacketHandler).captureCongestionSend` (0.425) | **lead 1** |
  | 4 | `(*packetEmission).handoff` (0.395) | ineligible: shared `sendQueue.Send` (0.336) |
  | 5 | `(*packetEmission).opportunityCapabilities` (0.264) | ineligible: the shared capability query, already once per opportunity |
  | 6 | `(*sentPacketHandler).finishCongestionFeedback` (0.220) | ineligible: too broad (model computation spread over about ten functions) |
  | 7 | `(*sendReservation).release` (0.192) | ineligible: the credit mutex itself; fewer acquisitions change admission timing or need a lock-free multi-field ledger |
  | 8 | `(*sentPacketHandler).appendRetainedAck` (0.191) | **lead 2** |
  | 9 | `(*sentPacketHandler).beginCongestionFeedback` (0.181) | **lead 3** |

  Leads 2 and 3 are the congestion-feedback work deferred from #714. Every judgement and its reason is recorded; a profile selects, it attributes nothing.

## Lead registrations

Written after the profile and before any lead's measurement data. Each is one commit on the current survivor, measured as `mK` with predecessor the survivor at that point. All three change only `internal/ackhandler` code reached through `congestionEvents`, which is nil when Reno is selected; none touches a parameter, model, bound, translation, evidence contract or default, and none batches the send path.

### Lead 1. Registration-record construction (`l1`, measured as `m1`)

- **Hypothesis.** Part of the BBR-only per-packet user work is copying each registration's 152-byte `PacketInfo`: built on the stack, copied by value into recovery evidence, into the delivery-record slab (argument and composite literal) and into the send event.
- **Change.** `deliveryRecords.insert(k)` gives map-`set` semantics but returns a pointer to the record's `PacketInfo` inside the slab, valid until the table's next mutation. `captureCongestionSend` builds the record once in that slot when the live limit allows (otherwise on the stack, as now), passes `recoveryEvidence.sent` a pointer (it only reads), lets the sampler fill `Delivery` in place, and sends the controller the same value `SendEvent` as before. Call order is unchanged.
- **Contracts.** Bounded delivery evidence: the live limit, insertion gating and slab bound are unchanged. Ownership: the pointer is used only within one registration, with no table mutation in between. The controller still receives a value record. Ordering, ECN, loss: none.
- **Equivalence domain.** For every registration, identical recovery outcome, sampler state, live delivery record and send event to r8's.
- **Oracles.** `TestDeliveryRecordsMapEquivalence` extended with in-place inserts against Go's map; #706's twin (`TestRecoveryEquivalenceScenarios`, `TestRecoveryEquivalenceHistories`), which drives the real handler against the frozen reducer; the delivery-sampler tests; #709's frozen ECN fuzz.

### Lead 2. Feedback ordering (`l2`, measured as `m2`)

- **Hypothesis.** Part of the excess is sorting every ACK's assembled acked list by ordinal, an insertion sort over 152-byte records, although assembly almost always yields ordinal order.
- **Change.** The dispatch records, while appending acked records (in `beginCongestionFeedback` and for retained discoveries in `appendRetainedAck`), whether each ordinal exceeds the previous. `appendRetainedAck` sorts only when that order was broken.
- **Equivalence domain.** Ordinals are unique per registration, so a list already in ordinal order is the unique sorted result: the event list is identical to r8's after every ACK.
- **Oracles.** A randomized comparison of the tracked list against the always-sorted list over assembly sequences with in-order, out-of-order and retained appends; #706's twin; the delivery-sampler tests; #709's frozen ECN fuzz.

### Lead 3. Acked-record move (`l3`, measured as `m3`)

- **Hypothesis.** Part of the excess is moving each acked record out of the slab: `take` copies it out by value, zeroes the 160-byte slab record, and the caller copies it again into the event list.
- **Change.** `deliveryRecords.takeAppend(k, dst)` appends the record directly into the event list and frees the slot by clearing only its key (the free marker that `grow` reads). `beginCongestionFeedback` uses it; other callers keep `take`.
- **Contracts.** Bounded delivery evidence and table semantics unchanged; a freed record's stale contents are never read before `insert` or `set` overwrites them whole, and `PacketInfo` holds no pointers, so nothing is retained for the collector.
- **Equivalence domain.** Identical event lists, live records, count and free-list order to r8's for every operation sequence.
- **Oracles.** `TestDeliveryRecordsMapEquivalence` extended with `takeAppend`; #706's twin; the delivery-sampler tests; #709's frozen ECN fuzz.

## Disclosures made before data

All of these runs are excluded from every statistic and retained or summarised here.

- **Smoke** (`smoke`, seeds 9998, October 6). r8 STREAM 94.1 Mbit/s (sender CPU 11.7 s/GiB) and frozen Reno DATAGRAM 94.4 Mbit/s. Every event, the three added ones included, counted at 100% running time at both endpoints; the idle snapshots were written. Receiver counters on these two runs (different arms and workloads, so not a comparison): APERF exceeded PMC cycles by 32% (Reno DATAGRAM) and 27% (r8 STREAM); MPERF ÷ task-clock was 2.82 and 2.81 GHz.
- **Clock probe (scratch, not retained).** A Go UDP receiver woken about 2,800 times a second on cores 12–15, with `perf stat` attached: `cycles` equalled `cycles:u` + `cycles:k`; APERF was about 1.87× PMC cycles; MPERF ÷ task-clock (2.88 GHz) matched the per-task TSC rate. A busy loop under the same counting gave APERF ≈ PMC cycles (within 1%) and MPERF ÷ task-clock ≈ 2.99 GHz. **So PMC cycles miss part of a wake-heavy task's scheduled time, and APERF counts it.** This finding shaped the receiver rule's `uncounted` component before any counted observation.

## Order

`build.py relay reno r8`, `sync.sh`; on `minimax`, under `taskset -c 1-3`: `matrix.py hostfacts`, `matrix.py profile` (and any reruns), `lead_profile.py fold`; then anywhere, `lead_profile.py` with the eligibility judgement recorded in `eligibility.json`. For each selected lead, in order: build `lK` on the survivor, run its gates, write its registration and append it to `measurements.json` with the current predecessor, sync, `matrix.py mK PREV NEW` (and any rerun blocks), `measure.py outcome mK`, then keep or revert. Then `mc` if its condition holds. Then `measure.py receiver`, `receiver_test.py`, `lead_profile_test.py`. `matrix.py hostfacts` before and after each stage.

## Deviations

Recorded as they occurred; each says whether it preceded the outcome it affects.

1. **Analysis-driver crash (before any outcome).** The first `measure.py outcome m1` stopped with a `TypeError`: the driver had placed the receiver figures inside #714's per-endpoint metric dictionary, and #714's descriptive loop divides every key in it. The driver now hides those figures from #714's analysis (`measure.py` `analyze_workload`); no rule function changed, and no outcome had been computed.
2. **Contamination despite the booking (m1).** A CI `sha256sum` job and a `qemu` VM ran on the host during DATAGRAM blocks 3 and 4 of `m1` although the operator had booked it quiet. Under the registered rule both blocks were rerun once with the same seeds (`m1rerun`, 10 observations from the rerun reserve).
3. **A latent generator issue in #714's delivery-records model test (lead 3, before its measurement).** The test's registration step checks the packet-number range and then applies a random skip, which can step past QUIC's maximum (2⁶²−1); `packDelivery` cannot represent such numbers. #714's random stream never reaches that path. Lead 3's extension first drew its choice from the same stream, which shifted later draws so that one seed did, and the oracle failed on that invalid key. The extension now draws from its own stream, so the generator's histories are exactly #714's; the generator itself is unchanged. A mutant that appends the wrong record is killed by the model test and by #706's twin.
