# Per-packet interventions on the BBR-enabled sender path

**Date:** October 3, 2026, America/New_York. **Scope:** [Test five selected per-packet leads in the BBR-enabled path by intervention](https://github.com/the-sarge/quic-go-fast/issues/714), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies Ticket A of D4 in the [accepted Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md#d4--next-test-the-per-packet-leads-by-intervention-then-re-demonstrate-on-linux). Experimental revisions and counters only: no production merge, readiness flag, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-per-packet-interventions`, based on the candidate `fc4c1bf1`. The [Linux diagnostic](https://github.com/the-sarge/quic-go-fast/blob/4323dae8/docs/audits/2026-10-03-bbr-linux-diagnostic/README.md) (`4323dae8`) supplies the fixture, host layout and retained call graphs this record builds on.

**Status: registration.** Everything below was written and committed before any intervention changed code and before any observation of this ticket existed. Results are appended in later commits; nothing registered here changes after an outcome is seen, and deviations are recorded as deviations.

## Question

Which of five selected per-packet leads in the BBR-enabled user-space path account for the candidate sender's extra work, as shown by contract-preserving interventions measured on Linux counters, and which revision survives? On S5, the #712 counters stage found about 11,300 (STREAM) and 8,700 (DATAGRAM) extra user instructions per forward packet at the sender, on frozen Reno's 31,600 and 33,300, appearing only when BBR is selected. Localization was inconclusive, so the leads are hypotheses, not causes.

## Evidence base for the leads

[leads.py](leads.py) sizes each lead from #712's retained S5 sender call graphs ([leads.json](leads.json)): median over three blocks of inclusive sender cycles per useful GiB at the lead's sites, candidate minus Reno. Inclusive sums overlap (for example, the ECN-mode callback includes a capability query), so they size the leads and partition nothing. The total sender excess is 5.0 (STREAM) and 4.6 (DATAGRAM) Gcycles per GiB.

| Lead | Sites, STREAM / DATAGRAM (Gcycles/GiB) |
| --- | --- |
| 1. Synchronization crossings | connection-loop `select` 0.61 / 0.73; timer re-arm 0.40 / 0.41; ECN-mode credit callback 0.26 / 0.35; pending-bytes credit callback 0.13 / 0.13 |
| 2. Delivery-record map | `captureCongestionSend` 0.77 / 0.87; `beginCongestionFeedback` 0.38 / 0.40; map operations under the dispatch 0.50 / 0.56 |
| 3. Recovery-evidence lookups | `recoveryEvidence` methods 0.67 / 0.42, of which `lowerBound` 0.25 / 0.12 |
| 4. Capability queries | `capabilities` 0.43 / 0.54 |
| 5. Send-credit reservations | `localSendCredit.reserve` 0.18 / 0.22; `sendReservation.resize` 0.19 / 0.25 |

Source reading at `fc4c1bf1` explains why these sites are per packet on the BBR path:

- `sendBounded` performs at most one handoff per opportunity and asks for continuation with `retry`. Each datagram therefore signals `sendingScheduled`, re-arms the connection timer and enters the loop's `select` before the next. Reno's `withoutGSO` loop sends a batch per opportunity. This shape is an implementation choice of the T3 slice ([#604](https://github.com/the-sarge/quic-go-fast/pull/604)), not one of the design decisions D01–D13. The design names receive-work yield as a valid stop, and D08 as amended lets each wakeup release a full quantum.
- The BBR ECN mode query calls its path callback on every call. The callback takes the credit mutex for the generation and drain state and queries endpoint capabilities, but `mode` reads generation and drain state only while draining.
- `captureCongestionSend` calls the pending-bytes callback, which takes the credit mutex, before testing the sampler conditions that make its result irrelevant in steady state.
- The delivery records are a `map[congestionPacketKey]congestion.PacketInfo`. The 16-byte struct key takes generic hashing, and `PacketInfo` is 152 bytes, above Go's 128-byte inline limit, so every new entry is also a separate heap object.
- The recovery-evidence ring holds up to 32,768 outcomes. `lowerBound` binary-searches a space's whole slot deque, about 15 probes, twice per ACK range and once per lost, discarded or PTO-retired packet. `sent` writes the `latestPackets` map on every registration.
- `capabilities()` is called two or three times per datagram: GSO on the data path, GSO again in `boundedDatagrams`, and ECN inside every ECN-mode query. Each call takes the managed endpoint mutex once or twice. Reno queries once per batch.
- `reserve` allocates a `sendReservation` per packet. Connection-side releases (the handoff resize and unused construction allowance) also signal the credit's wakeup channel, although only worker completions can wake a waiter.

## What stays fixed and what changes

- **Fixed:** frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` and the predecessor chain from `fc4c1bf1`; #712's fixture with the causal-diagnosis heap patch, relay (frozen v1 queue model, #711 delivery observability, Linux ECN adapter), launcher, endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream), core layout and `perf stat` events; `GOTOOLCHAIN=go1.27.0`; every model parameter, declared bound, recorded QUIC translation, evidence contract and default.
- **Changed, all recorded:** five intervention revisions, one commit each, in the registered order below; the measurement arms of D4; aids copied from #712 with the changes listed in [Assets](#assets).

## Registration: interventions

Each intervention is one commit on the current surviving revision. Each names the contracts it can affect, its equivalence domain against `fc4c1bf1`, the oracles and gates that must pass before it is measured, and why Reno's path is untouched. Effects are conditional net effects in this order.

**Branch mechanics.** Revision `rK` is intervention K on the surviving revision. A change that is not kept is reverted by a separate commit, so the next intervention builds on the survivor and the failed change stays in history. A change whose gates or equivalence fail is never measured. A lead whose change needs a contract change, or cannot be made equivalent, stops and is reported; independent leads continue after a null.

**Not allowed in any intervention:** a parameter, model or declared-bound change; send-path batching (GSO or `sendmmsg`); any change to code executed when Reno is selected; a change to a recorded translation, evidence contract, controller version or default; weakening any existing test, gate or work bound. Congestion and BBR ECN feedback are deferred by D4 and are not touched.

### 1. Synchronization crossings

- **Hypothesis.** Part of the BBR-only per-packet user work is synchronization done per datagram where Reno's path does it per batch or not at all: the continuation channel signal, timer re-arm and `select` between consecutive BBR datagrams, and two credit-mutex reads whose results are usually unused.
- **Change.**
  - (a) After a successful ordinary data handoff, `sendBounded` returns `deadlineSendImmediately` instead of `retry`. This is the continuation Reno's receive-yield stop already uses. The connection loop still makes one full pass per datagram: close check, received-packet processing, delivery expiry, loss timer, keep-alive and idle checks, capacity. It skips only the channel token, timer re-arm and `select`. Every other `sendBounded` stop is unchanged.
  - (b) The BBR ECN path callback takes a `drain` argument and reads the credit generation and drain state only while the tracker is draining. The capability bit is still read on every call.
  - (c) `captureCongestionSend` evaluates its side-effect-free sampler conditions before the pending-bytes callback.
- **Contracts.** Ordering and receive fairness: receive work, timers and close are still serviced between consecutive datagrams. Pacing and congestion limits: every opportunity re-checks recovery, pacing and credit as before. Cancellation and close: the close channel is checked at the top of every pass. Credit and queue bounds: unchanged. ECN: the drain fence reads the same state while draining. Native capabilities: none.
- **Equivalence domain.** Within each opportunity, every call from emission into recovery, the controller, the packer and the queue has the same arguments in the same order. At the connection, the new revision's schedule is one the predecessor admits: after a `retry` result, the predecessor's `select` may immediately take the `sendingScheduled` branch, and the new revision always takes exactly that branch, without re-arming a timer the branch does not consult. (b) and (c) evaluate side-effect-free reads lazily; every decision is unchanged.
- **Oracles.** #709's frozen ECN tracker oracle (`TestBBRECNFeedback*Equivalence`, `FuzzBBRECNFeedbackFrozenEquivalence`), adapted only for the callback's argument, plus a count of credit reads showing they occur only while draining. #706's twin (`TestRecoveryEquivalenceScenarios`, `TestRecoveryEquivalenceHistories`) for (c). New emission tests: a successful ordinary handoff returns the immediate continuation with no `sendingScheduled` token and still one datagram per opportunity; a paced stop still returns its deadline. A new connection-loop test: a received packet, an expired loss timer and a close request are each serviced between two consecutive BBR datagrams.
- **Invariant argument (synchronization).** The ECN tracker's `draining` flag and the fields read under it are connection-owned; the credit fields it needs are read under the credit mutex exactly as before whenever `draining` is set, and the decision never reads them otherwise. Skipping the `select` cannot lose a wakeup: channel tokens left unconsumed (timer, received packet, scheduled send) remain buffered and are taken at the next `select`.
- **Reno.** (a) is inside `sendBounded`, (b) inside the BBR ECN tracker and (c) behind `congestionEvents != nil`; none runs when Reno is selected.

### 2. Delivery-record map

- **Hypothesis.** Per-packet insert, lookup and delete in the delivery-record map (generic struct-key hashing and one heap object per insert) account for part of the excess.
- **Change.** Replace the map with a connection-owned open-addressing table keyed by packed (space, packet number), with Fibonacci hashing, linear probing and backward-shift deletion, holding indices into a dense record slab with a free list. Capacity doubles past 7/8 load, so 25,000 live records fit in 32,768 slots. Path and Retry reset release it, as they recreate the map today. The 25,000-record live limit, insertion gating and every caller are unchanged. `DeliveryStats` keeps its count-based `RecordBytes` formula, so the reported value is unchanged.
- **Contracts.** Bounded delivery evidence: the live-count limit is unchanged, and memory stays bounded by 32,768 slots plus at most 25,000 records. Ordering: iteration order changes only in space discard, whose disposal is a commutative subtraction. Ownership and lifetime: connection-owned, released on reset. ECN and loss: none.
- **Equivalence domain.** Map semantics for every operation sequence the dispatch issues: insert of an absent key, lookup, delete, count and iteration as a set.
- **Oracles.** A model-based randomized test against Go's map (growth, deletion chains, all three spaces, sparse and maximal packet numbers, full table). #706's twin, whose live-record comparison against the frozen dispatch's map reads the new table through a map view. Existing dispatch and sampler tests.
- **Reno.** The dispatch exists only when BBR is selected.

### 3. Recovery-evidence lookups

- **Hypothesis.** The ring's binary searches and the per-registration `latestPackets` map write account for part of the excess.
- **Change.** Each per-space slot deque keeps its first and last packet number and the total skipped-number gap inside it. `lowerBound(pn)` computes the exact index bracket `[pn − first − gaps, pn − first]` from that metadata and binary-searches only inside it. A deque with no skipped numbers resolves in at most one probe; no query probes more than the predecessor's bound of `bits.Len(n)`. `latestPackets` and `boundaryPackets` become three-element arrays with presence flags.
- **Contracts.** Bounded service work: #706's work-scaling bounds are unchanged and still asserted (`TestRecoveryServiceWorkScaling`). Ordering, ECN and loss: none, since every result is unchanged.
- **Equivalence domain.** `lowerBound` returns the predecessor's index for every query; every episode boundary decision is unchanged.
- **Oracles.** A property test of the new `lowerBound` against the predecessor's binary search over random deques with skips, evictions and resets. #706's twin, with its index check extended to recompute the new metadata. #706's work-scaling test, unchanged.
- **Reno.** Recovery evidence exists only when BBR is selected.

### 4. Capability queries

- **Hypothesis.** Two or three `capabilities()` queries per BBR datagram, each taking the managed endpoint mutex once or twice, account for part of the excess.
- **Change.** One memoized capability snapshot per BBR opportunity, taken on first use inside `sendBounded` and dropped when it returns. Both GSO decisions and the ECN-mode callback read it during the opportunity. Outside an opportunity the callback queries live, as before. The capability implementations, shared with Reno, are unchanged.
- **Contracts.** Native capabilities: capabilities can change concurrently (a GSO error, managed ECN or receive state, lease DF). The snapshot is never older than the current opportunity, and every later opportunity re-reads. ECN: marking uses a capability bit that an in-opportunity live query could have returned.
- **Equivalence domain.** With static capabilities, all decisions are identical. Within one opportunity on the connection goroutine, the predecessor's queries can differ only if a concurrent change lands between them; the snapshot equals the predecessor's result under the schedule in which that change lands after the opportunity.
- **Oracles.** A test with a capability-switching connection showing one query per opportunity and a change taking effect at the next opportunity. Existing `TestBBRECNCoalescedAndPathProbeMarking` and `TestBBRPendingCreditGSOFallback`.
- **Reno.** `sendBounded` and the BBR ECN callback run only when BBR is selected.

### 5. Send-credit reservations

- **Hypothesis.** Per-packet reservation allocation and connection-side wakeup signalling account for part of the excess.
- **Change.**
  - (a) Reservation objects are recycled through a free list owned by the credit and guarded by its existing mutex, so `reserve` no longer allocates in steady state. A reservation returns to the list when it completes. `sendMetadata` keeps its pointer, so Reno's queue entries are unchanged.
  - (b) Connection-side releases (the handoff resize and unused construction allowance) no longer signal the wakeup channel. Worker completions, stopped-queue sends and close drains signal as before.
- **Contracts.** Ownership and lifetime: each reservation completes exactly once. Credit and queue bounds: ledger arithmetic is unchanged. Cancellation and close: stopped-queue sends and close drains complete as before. Concurrency: completions run under the credit mutex as before.
- **Invariant arguments.**
  - (a) Recycling makes a second completion of one reservation unsafe where the predecessor tolerated it as a no-op. A queued reservation is completed only in `queueEntry.release`, after `packetBuffer.Release`, which panics on a second release. Every connection-side completion clears its only reference immediately. So no reservation completes twice, and none is reused while referenced.
  - (b) The connection is the only waiter. Every wait on the credit channel follows, on the connection goroutine and after the last connection-side release, a drain under the credit mutex: a refused `reserve` drains before returning, and `waitForReservation` drains before re-arming. A token from a connection-side release is therefore always drained unobserved, and omitting it changes no wait. Worker completions remain lossless when wakeups coalesce.
- **Equivalence domain.** Identical ledger state (pending, current, generation, isolation) and return values for every operation sequence. Identical wakeup-token presence at every point where a wait can begin.
- **Oracles.** A frozen copy of the predecessor ledger driven beside the new ledger on randomized sequences of reserve, resize, complete, generation reset and wait, comparing state, returns and wakeup tokens at wait points. An ownership test: across worker completion, stopped-queue send and close drain, pending returns to zero and no reservation is reused while referenced. Existing credit tests, including `TestBBRPendingCreditConcurrentDrainRefill` under `-race`, partial and unknown progress, stopped and close, GSO fallback, MTU exception and migration debt.
- **Reno.** Local send credit exists only when BBR is selected; Reno's queue entries carry a nil credit as before.

### Gates for every revision

Run on the Mac before measurement and recorded in `gates/rK.log`. `go vet ./...`; `go test ./internal/ackhandler/... ./internal/congestion/...` with and without `-race`; the root package's BBR, emission, credit and delivery tests with and without `-race`; the full root package; and the intervention's named oracles. On `minimax`, the root BBR, emission and ECN tests run natively on Linux. Native coverage on other platforms (Windows, BSD, macOS managed I/O beyond the Mac gates) cannot run here and is recorded as a gap; compilation is not counted as native behaviour.

## Registration: measurement

Written with the analysis in [intervene.py](intervene.py) and its synthetic cases in [intervene_test.py](intervene_test.py) (13 cases, all passing) before any observation of this ticket existed.

### Runs

- **Host and layout:** `minimax` with #712's core layout: sender CPUs 8–11, receiver 12–15, relay 4–5, runner and `perf` 1–3, SMT siblings idle. Host settings unchanged.
- **Path and timing:** S5 (100/100 Mbit/s, 100 ms RTT, one-BDP drop-tail queue), 10 s warmup and 30 s measured, both workloads.
- **Arms, per measurement:** frozen Reno, A/A Reno (a second frozen-Reno run, tag `aa`), the predecessor revision with BBRv3, the new revision with BBRv3, and Reno on the new revision. Plain builds.
- **Blocks and seeds:** six blocks; measurement `mK` uses seeds `9700 + 10K + block`. #712's rotation of arm order per block and workload order by block parity. Observations run sequentially.
- **Instruments:** `perf stat` on both endpoints for the measured window, #712's sixteen events.
- **Inventory:** measurements `m1`–`m5` for the five leads, each 60 observations, about 40 minutes. `m6` is the cumulative check of the surviving revision against `fc4c1bf1`; it runs only if the surviving revision differs from `fc4c1bf1` and from every new revision already measured against `fc4c1bf1`. At most 360 observations, plus at most one rerun block per measurement.

### Metrics

Per endpoint and observation: user and kernel instructions and cycles, per forward packet and per useful GiB. Forward packets are those the relay model received in the measured window, written plus overflowed (#712's `forward_packets`). Also reported: CPU seconds per GiB, context switches, wakeups, futex, send syscalls and transmits per packet, and goodput.

### Rules

- **Usability.** A block is usable when all ten of its observations in a workload count every event at ≥ 95% running time (#712's `counter_quality`). A block with an unusable observation is rerun once with the same seed, under the phase suffix `rerun`; the original is retained and reported, and the rerun replaces it only if usable. A workload with fewer than four usable blocks is an **evidence gap**.
- **Targeted work.** The primary metric is sender user instructions per forward packet, the quantity in which #712 found the excess. Per block, the effect is new ÷ predecessor and the A/A ratio is A/A ÷ frozen Reno, for the same metric. With `E` the median effect and the A/A range `[min, max]` over usable blocks:
  - **reduction** if `E < min` and the effect is below 1 in every usable block;
  - **increase** if `E > max` and the effect is above 1 in every usable block;
  - **null** if `E` lies inside the range;
  - otherwise **inconclusive**.
- **Disagreement.** Sender user cycles per forward packet are the secondary metric, classified against their own A/A range. A reduction in instructions while the cycles median rises above its A/A range, or an increase while it falls below, is **inconclusive**. Cycles inside their range do not block an instruction result.
- **Workloads.** A reduction in either workload, with the other a reduction or null, is a reduction. A reduction with an increase or an inconclusive workload is inconclusive. Otherwise an increase in either is an increase, an inconclusive in either is inconclusive, and both null is null.
- **Preservation.** Reno on the new revision must stay inside frozen Reno's A/A range. The metric is cycles per useful GiB, user plus kernel, at each endpoint. Both workloads' blocks are pooled per endpoint, giving two cells of up to twelve ratios, and the median of Reno-on-new ÷ frozen Reno must lie inside the A/A ratios' `[min, max]`. The registered choice and its reasons:
  - **Instructions fail the unchanged control.** On #712's counters stage, Reno built from the unchanged candidate lies outside the A/A range in 4 of 28 per-workload counter cells. The sender instruction cell is 1.0008 against an A/A range of 0.9983–1.0004: a fixed offset of about 0.1% from the candidate's BBR hooks, inside an unusually tight range.
  - **Per-workload cells fail by chance.** By simulation, the median of six draws falls outside the range of six other draws from the same distribution 9.9% of the time, so four such cells would fail an unchanged Reno path 34% of the time. Pooled twelve-block cells fail 0.7% of the time each, 1.4% for both.
  - **The pooled cells still detect regressions.** They detect a 2σ shift 72% of the time and a 3σ shift 97% of the time; with cycles noise of about 0.3–0.5%, that is a Reno regression of roughly 1–1.5%.
  - **The unchanged control passes.** Both pooled cells pass #712's Reno-on-candidate data (`test_empirical_null_from_712`).
  - **What it cannot catch.** A Reno change below about 1% of cycles would go undetected here, so no intervention may touch Reno's path by construction (see each intervention).
- **Integrity.** Every observation must exit cleanly and pass receiver integrity (#712's `summarize` checks).
- **Outcome.**
  - **Keep:** the change's gates and equivalence oracles passed before measurement, targeted work is a reduction, every observation passed integrity, and both preservation cells are inside.
  - **Negative:** targeted work is an increase, an integrity failure occurred, or Reno on the new revision lies above the A/A range.
  - **Null:** targeted work is null and nothing negative occurred.
  - **Inconclusive:** any other case, including a reduction with Reno on the new revision outside the range below it.
  Only a kept change survives.
- **Contamination.** Reported per observation with #712's rule (foreign CPU on fixture cores and siblings, mean above 0.10 cores or any one-second sample above 0.50). Contaminated observations are retained and counted. The outcome is recomputed without blocks holding one; if it changes, the outcome is reported as contamination-sensitive and still stands as registered.
- **Reported effects.** For each lead: the median per-block change in sender user instructions per forward packet (new − predecessor), the conditional net effect; the residual excess of the predecessor and new revisions over frozen Reno per packet; and every metric's ratios at both endpoints. Effects are not intrinsic leaf costs, and no counter result implies a whole-run CPU readiness conclusion. No readiness flag is set here.

### Order

`smoke` (excluded harness check), then for each lead in order: build `rK` on the survivor, run its gates, register its measurement in `measurements.json` with the then-current predecessor, run `mK`, run `intervene.py mK`, and keep or revert. Then `m6` if its condition holds. Everything runs under `taskset -c 1-3`.

## Checkpoint for Ticket B

This ticket closes with the surviving revision (possibly `fc4c1bf1` unchanged), each lead's outcome and conditional effect, the residual excess per packet against Reno, and the unresolved evidence. [Re-demonstrate the surviving BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/715) follows it.

## Assets

- **Copied from #712 (`4323dae8`), byte-identical and checked by `build.py`:** [run.py](run.py), [localize.py](localize.py), [launch/main.go](launch/main.go), [relay/](relay/), [model-overlay/next.go](model-overlay/next.go).
- **Adapted:** [build.py](build.py): plain builds only, named revisions, byte-identity checks against #712. [matrix.py](matrix.py): D4's five arms, six blocks, per-measurement seeds, one rerun block; it redirects `run.py`'s artifact directory.
- **New:**
  - [leads.py](leads.py), [leads.json](leads.json): lead sizing from #712's retained call graphs.
  - [intervene.py](intervene.py), [intervene_test.py](intervene_test.py): registered outcome rules and their synthetic cases.
  - [measurements.json](measurements.json): each measurement's predecessor and new revision, appended before that measurement runs.
  - [go.mod](go.mod): keeps these aids out of the root module's packages.

## Deviations

Recorded as they occurred; each says whether it preceded the data it affects.

1. **Smoke observations lost.** A plain `rsync --delete` from the Mac removed the two excluded smoke observations and the generated credentials on `minimax` before they were archived. Their printed summaries survive: both were contaminated by other host work (foreign CPU 0.55 and 0.90 cores mean), with 43,555 and 33,390 sender user instructions per packet. The harness check was rerun as `smoke2` (clean: 42,608 and 33,334), and [sync.sh](sync.sh) now protects observations, summaries and credentials. Before any comparative observation.
2. **Tagged work gate omitted.** #706's work-scaling test builds only with `-tags bbrworkcount`, so the registered gate list skipped it. It was added to [gates.sh](gates.sh) and run for r1 (`gates/r1-workcount.log`) while m1 was running, before its outcome was known.
3. **Loader bug.** The loader read the controller from `meta.json`, which #712's runner never writes. Found by a dry run on m1's incomplete blocks and fixed before any outcome was computed; no rule function changed.
4. **Duration.** Each measurement takes about 64 minutes of fixture time, not the estimated 40. The registered bound is the observation count, which is unchanged.
5. **Contaminated blocks are rerun (operator decision after m3).** Under the registered rule, m3 was **inconclusive**: STREAM a reduction (0.982, −714 user instructions per packet), but one DATAGRAM block above 1. Blocks 5 and 6 of both workloads were contaminated: a qemu VM and a self-hosted CI runner started on the host, up to 0.87 foreign cores on average. The DATAGRAM block above 1 (block 6, 1.0038) was the most contaminated; without blocks 5 and 6 the outcome would have been keep. The registered rule let a host event unrelated to the change discard it, conflating compromised evidence with an absent effect. The operator decided that a block containing an unusable **or contaminated** observation is rerun once with the same seed on a quiet host, replacing the original only if the rerun is usable and uncontaminated ([intervene.py](intervene.py) `choose_block`, with synthetic cases). This is adopted after m3's outcome was seen, so it is post-outcome for m3, where blocks 5 and 6 are rerun (two rerun blocks rather than one). It is prospective for m4–m6. The first m3 analysis is retained in [outcomes.json](outcomes.json) as `m3-first`. A provisional intervention 4 built on r2 after the first revert was gated (`gates/r4-on-r2-discarded.log`) and discarded unmeasured.
6. **A second rerun of m3's still-contaminated blocks (operator decision).** CI jobs and a VM returned during the deviation-5 rerun, so only the STREAM block 5 rerun was clean. On a host the operator had cleared, the three still-contaminated blocks (STREAM 6, DATAGRAM 5 and 6) were rerun once more with the same seeds under the phase `m3rerun2`. `choose_block` takes the first clean of original, rerun and second rerun, and otherwise the first usable. Decided after m3's outcomes were seen; it applies only to m3.
