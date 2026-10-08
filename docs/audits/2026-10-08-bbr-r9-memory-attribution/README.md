# Memory and latency attribution of the BBRv3 candidate r9

**Date:** October 8, 2026, America/New_York. **Scope:** [Attribute the BBRv3 candidate's memory and S6 latency flags on the final revision](https://github.com/the-sarge/quic-go-fast/issues/740), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D6 ("Ticket H") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d6--ticket-h-attribute-the-memory-and-s6-latency-cells-on-the-final-survivor), plus two operator decisions recorded below. Measurement only: no production merge, transport source change, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r9-memory-attribution`, based on `138223ac` (the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md), #739), whose tree's code is exactly r9. The revision under test is **r9, `81e9dc8c`**: r8 plus #738's 32-entry BBR send queue. No BBR revision newer than r9 exists. The operator notes that the BBRv3 implementation is still under active performance development. Every result here describes r9 on `minimax` only. None is a verdict on BBRv3 or on later revisions.

**Status: registration, revised after two considerations.** Everything below was written, with the aids built, the gates run and the instruments checked, before any comparative observation of this ticket. The first registration (`c0c33ebf`) and its revision (`2bade4e5`) were each gated by a multi-agent consideration before any data. All findings were dispositioned and the registration revised ([Consideration and revisions](#consideration-and-revisions)). Results, deviations and later sections will be added after the data, below a marker, and these sections will stay unchanged.

## Question

On r9, what makes up each raised memory cell's resident-memory excess, and do both S6 control p95 cells meet the Up-policy attribution rule? By operator decision, the same attribution covers r9's two new loopback receiver RSS cells, and the candidate's loopback STREAM control p95 cell gets an adequate-sample check.

## Cells

From #739's readiness ([summary.json](../2026-10-08-bbr-r9-linux-redemonstration/summary.json); medians of five per-block ratios against frozen Reno; checked against the JSON). The excesses are the absolute readiness medians, candidate minus frozen Reno.

| Cell | Ratio | Excess (MiB) | In scope by |
| --- | --- | --- | --- |
| S5 STREAM sender RSS | 1.235 | 4.21 | D6 |
| S5 STREAM receiver RSS | 1.518 | 8.77 | D6 |
| S5 DATAGRAM sender RSS | 1.159 | 2.98 | D6 |
| S6 STREAM sender RSS | 1.673 | 8.93 | D6 |
| S6 STREAM receiver RSS | 1.898 | 12.84 | D6 |
| S6 DATAGRAM sender RSS | 1.542 | 7.46 | D6 |
| Loopback STREAM receiver RSS | 1.120 | 1.72 | Operator decision 1 |
| Loopback DATAGRAM receiver RSS | 1.148 | 3.00 | Operator decision 1 |
| S6 STREAM control p95 | 1.562 | — | D6 |
| S6 DATAGRAM control p95 | 1.459 | — | D6 |
| Loopback STREAM control p95 | 1.485 (0.199 against 0.134 ms) | — | Operator decision 2 |

S5 DATAGRAM receiver RSS (1.070) and S6 DATAGRAM receiver RSS (1.095) pass; they are reported descriptively from the same runs and are not labeled.

## Operator decisions recorded before data

Both decisions were relayed on October 8, 2026, from the #739 session. No comparative observation of this ticket existed then.

1. **Loopback receiver memory.** Add loopback arms (frozen Reno, the candidate, the BBR A/A and the receiver-controller arm; no ring arm, because both cells are receiver cells), both workloads, four blocks, with the Stage 0 overlay, under the same rules and thresholds as S5 and S6. The perturbation floor must also pass on loopback. If it fails there, the loopback cells end as an **instrumentation gap** and the S5 and S6 work stands unchanged. Loopback STREAM moves about 3% with code placement alone (#738), so this check is expected to be tight. The cap rises in proportion.
2. **Loopback STREAM control p95.** Rerun #715's registered `preslat` design and rule unchanged, with the candidate (`cand`, BBRv3) in place of Reno on candidate. That is six blocks of frozen Reno against r9 on the `loopback-long` path (120 s window), STREAM only, plain builds, scored with [rules.py](rules.py) `latency_preservation`: at least 100 replies per run and 500 per arm, at least four usable blocks, and a pooled p95 ratio with a 95% block-bootstrap interval against the 1.20 limit. **Mapping, accepted before data:** an adequate *not reproduced* outcome ends this cell's blocking state for r9 on Linux only, as in D3. *Regression reproduced*, *inconclusive*, *evidence gap* or unusable leave it blocking and unresolved. The stage finds no cause either way, and the readiness value 1.485 stays as recorded.

## What stays fixed and what changes

- **Fixed:** #739's fixture, relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter), launcher, endpoint contract (GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz), path durations (loopback 5 + 20 s; S5 and S6 10 + 30 s; `loopback-long` 5 + 120 s), `minimax`'s core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings, `GOTOOLCHAIN=go1.27.0`, the block rotation, #738's quiet-host gate (`wait_quiet`), #712's contamination test and the operator's #714 same-seed rerun rule. Frozen Reno is `e4f322cb`.
- **Aids copied byte-identical from #739's record (`138223ac`)**, checked by [build.py](build.py): `run.py`, `analyze.py`, `attribution.py`, `localize.py`, `localize_test.py`, `prereq.py`, `pack.py`, `rules.py`, `rules_test.py`, `stage_run.py`, `stages.py`, `receiver.py`, `receiver_test.py`, `harness.py`, `harness_test.py`, `go.mod`, `calibrate/`, `launch/`, `relay/`, `overlay/diag_occupancy.go` and `model-overlay/next.go`. Rebuilt `reno` (`cda8feb1…`), `cand` (`4fa75322…`), both relays, the calibration aid and the launcher are asserted byte-identical to #739's binaries.
- **Adapted, with recorded changes:** [build.py](build.py), [matrix.py](matrix.py), [follow.py](follow.py), [driver.sh](driver.sh), [art.py](art.py) and [sync.sh](sync.sh) (this ticket's artifact directory).
- **New:** the memory overlay ([mem/](mem/)), [mem_run.py](mem_run.py), [memrules.py](memrules.py) and [memrules_test.py](memrules_test.py), [memstages.py](memstages.py), [synthetic.py](synthetic.py) with its recorded cases ([synthetic/](synthetic/)), [gate_trees.py](gate_trees.py), [gates.sh](gates.sh) and [gates-native.sh](gates-native.sh).

## Stage 0: memory accounting instrument

**Overlay (measurement only; [mem/](mem/), applied by build.py to exported trees, never to a tracked source).**
- **Sampler** ([mem/mem_series.go](mem/mem_series.go)). Once a second, and at stop, each endpoint appends one JSON line to `{role}.mem.jsonl`. A failed read is written as −1. Each line holds:
  - `smaps_rollup` (Rss, Pss, Anonymous and the other fields) and `/proc/self/status` (VmHWM, VmRSS, RssAnon, RssFile);
  - the Go runtime's disjoint `/memory/classes/…` quantities, GC cycles, heap goal and live heap;
  - the BBR structure bytes;
  - #710's receive occupancy counters (descriptive only);
  - the connection's public `ConnectionStats()` packet counters (packets sent, received and lost; atomic in both revisions). The fixture registers its connection at the start of each session.
  The sampler writes from preallocated buffers, so its own footprint is small and constant. It replaces #710's 10 ms series, which held every sample in memory until exit.
- **BBR structure accounting** ([mem/mem_ackhandler.go](mem/mem_ackhandler.go), candidate builds only). On the first registration and every 256th, it publishes the outcome ring's `recordBytes()`, entries, occupancy and evictions, the delivery records' slot-table bytes, slab capacity bytes and free-list bytes, the feedback scratch buffer's capacity bytes, and the retained deliveries. It does this through atomics, with the call counter atomic as well.
- **Phase log** ([mem/mem_phase.go](mem/mem_phase.go), candidate builds only). It records BBR phase transitions after each feedback event as `[unix ns, phase]`, with the semantics of the counting overlay's `c4Phase`, which supplied D2's inputs on #711, #715 and #736. The log is a fixed array in BSS, written to the series file at stop. See [departure B](#interpretations-and-departures-from-d6-for-approval).
- **Receiver-controller switch.** `MEM_LOCAL_CONTROLLER=reno` selects the receiver's controller in place of the run configuration's, so the shared configuration and the peers' configuration check are unchanged. The fixture's own result then reports the receiver's effective controller. [mem_run.py](mem_run.py)'s `summarize_m` is #712's `summarize` with one assertion changed: the sender must report the configured controller, and the receiver must report the controller recorded in `meta.json`.

**Builds** ([build.py](build.py)), all `-trimpath -buildvcs=false`, linux/amd64:
- `reno-mem`: frozen Reno with the sampler and occupancy hooks, plus a Reno stub with no BBR structures;
- `cand-mem`: r9 with the whole overlay;
- `cand-mem-ring`: `cand-mem` with the ring treatment;
- `synthetic`.

**Accounting rule** ([memrules.py](memrules.py)). At one sample, resident memory (`Rss`) is partitioned exactly into:
- **file**: Rss − Anonymous (the binary's text and data);
- **heap objects**, **heap unused** and **heap free**;
- **stacks**: heap and OS stacks;
- **metadata**: mcache, mspan and other metadata;
- **other runtime**: other and profiling buckets;
- a **residual**: Anonymous minus those runtime classes.

Released heap is excluded. Only samples with every accounting value read are used. The readiness cell's peak is the endpoint's own `getrusage(RUSAGE_SELF)` maximum resident size (`ru_maxrss`, read by the fixture; the launcher only keeps the runner's high-water mark out of it). The synthetic program reads it the same way. That peak is the largest valid sampled Rss plus a signed **sampling gap**, and each block's excess is reconciled class by class at each endpoint's own peak sample, so the classes, the residual and the gap sum to the peak excess exactly (`closure`, tested).

**The sampling gap is mostly negative, and larger than kB rounding.** In the committed synthetic runs, the sampled Rss exceeds `ru_maxrss`:
- in 15 of 18 runs;
- with gaps from −0.84 to +0.29 MiB, median −0.41 MiB;
- with paired (case minus baseline) gap excesses from −0.77 to +0.41 MiB, median absolute 0.39 MiB.

The kernel mechanism is not established. `ru_maxrss` therefore can read below residency the sampler saw moments earlier. The rule keeps charging |gap| in full. For a small cell this alone can exhaust the 25% allowance: at loopback STREAM receiver's E ≈ 1.72 MiB the allowance is 0.43 MiB. Such a cell may end as an evidence gap from the instrument discrepancy alone, which is not a finding about r9.

Closure is bookkeeping, not proof that the classes explain residency at another moment. A block is therefore **unexplained** when |residual excess| + |gap excess| exceeds **25%** of its peak excess; the two terms cannot cancel. A cell is an **evidence gap** with no causal label when fewer than three usable blocks are explained, or when the median unexplained share over its usable blocks exceeds 25% ([departure J](#interpretations-and-departures-from-d6-for-approval)). The heap goal is context only. The BBR structure bytes are explicit accounting, reported beside each cell; they are part of heap objects.

**Heap reading** ([departure A](#interpretations-and-departures-from-d6-for-approval)). Heap objects at one sample include garbage not yet collected, which moves between the objects and free classes with the GC cycle. Under the fixture's default GOGC=100, a pure live-heap cause L leaves between L and 2L of heap objects while it raises resident heap by about 2L, so its objects share of the excess averages about 0.75, close to D6's 0.70. D6's condition, "at least 70% of the excess in heap objects", aligned "to the window and to GC cycles", is read as the **objects window share**: the median over the measured window's per-second samples of the heap-objects excess, divided by E.

A cell's excess sits in **traffic-dependent live heap** only when all three of these reach 0.70:
- the objects window share;
- the **live share**: 2 × the excess of `/gc/heap/live` (live heap at the last completed mark) at the peak sample ÷ E;
- the **window live share**: the same quantity as the window median.

The window measures use only valid samples whose own timestamps fall inside each run's measured window. They are aligned by whole seconds since each run's configured start and need **80% coverage** of the window's seconds in both runs; otherwise the block is unusable.

**Instrument validity per run** (second consideration, F1). Before block selection, every arm's two endpoint series must each hold valid samples in at least 80% of the measured window's seconds ([memrules.py](memrules.py) `instrument_valid`). An instrument-invalid observation makes its attempt unusable. That makes the block eligible for its capped same-seed rerun, and keeps it out of calibration, perturbation and S6 scoring. A low baseline excess is a fraction-eligibility matter, never an instrument failure, and never requests a rerun. The doubled-live shares are a model and never suffice alone: a 0.70 doubled-live share admits an actual live excess of only 0.35 E, which the measured objects share has to back. A warmup-only excess fails the window share.

**Sensitivity, recorded before data.** On the synthetic 8 MiB retained case this reading localizes three of three blocks in the committed set (objects window share 0.71–0.86) and two of three in the previous set (0.62–0.81). The literal peak-sample reading localized one of three. A true live-heap cause can therefore still end inconclusive, and an inconclusive ending is not evidence against live heap.

**Synthetic cases (D6), measured on `minimax`** with the unchanged sampler, three blocks each, against a `none` case with a 4 MiB live baseline and steady garbage ([synthetic/](synthetic/), the committed set; [memrules_test.py](memrules_test.py) `Synthetic`, all passing):

| Case | Peak excess (MiB) | What the accounting shows | Rule outcome |
| --- | --- | --- | --- |
| Known retained allocation (8 MiB) | 15.25–16.00 | Live +7.98 to +8.01; objects window share 0.71–0.86; live and window live shares 1.00–1.05 | Live heap in 3 of 3 blocks; explained |
| Warmup-only spike (24 MiB) | 47.00–47.75 | Live share at the peak 1.00–1.02; window live share 0.00; objects window share 0.00 | Not live heap |
| Goroutine-stack growth | 14.75–16.00 | Stacks +8.00 to +8.06; live share 0.00–0.01 | Not live heap |
| Four times the allocation rate at a fixed live heap | 0.45–0.58 | Live +0.04 to +0.14; unexplained share 0.77–1.49 | Not live heap; unexplained |
| Runtime accounting fails (8 MiB anonymous mmap) | 7.50–8.25 | Residual +8.03 to +8.24 | Unexplained; **no causal label**, even with arms that would otherwise give one (tested) |

**Perturbation** ([departure K](#interpretations-and-departures-from-d6-for-approval)). D6 requires instrumented goodput of at least 0.99 of plain. For each path, workload and build (`cand-mem` and `reno-mem`), the check divides:
- the median goodput of the instrumented arm over the **clean** Stage 1 blocks, by
- the median of #739's plain readiness arm over its blocks 1–4 (the same seeds, so the relay impairments match).

At least three clean blocks are needed on each side; otherwise the check is **unvalidated**, which fails it. A failing or unvalidated check on either build ends that path and workload's raised memory cells, and its S6 control p95 cell, as **instrumentation gap (perturbation)**. The rule's own label is recorded beside it. This applies to loopback too (operator decision 1), and a failure on one path or workload never affects another.

Disclosed limitation: the denominator is #739's plain runs from earlier the same day, so session drift between the two runs is confounded with instrument cost. The check is evaluated after Stage 1, not before.

**Instrument checks** (development runs; see [Disclosures](#disclosures-made-before-data)):
- sampling covers every second of every run;
- the receiver-controller arm's receiver reports Reno, holds no ring bytes and logs no phases, while its sender holds the candidate's ring;
- the S6 candidate logs 51–54 phase transitions per run;
- the packet counters advance at both endpoints of every arm, the Reno receiver's included.
- The candidate's `PacketsLost` stays 0 while frozen Reno's counts losses on the same path: the BBR loss path does not update that public counter. Sender-declared loss is therefore recorded but not compared.

## Stage 0: ring treatment

- **Treatment.** `cand-mem-ring` sets `maxRecoveryOutcomes` from 32,768 to **4,096**, the smallest the indexes support (a power of two, at least 4,096). Its one deviation is earlier eviction of retained outcomes. It is diagnostic and never kept, and a response authorizes nothing.
- **Gates.** #714's gate set, as #738 ran it, on the exported tree ([gates.sh](gates.sh), with `go vet`, the ackhandler and congestion suites, race, `bbrworkcount`, the root subset with race, and the full root package), and natively on `minimax` ([gates-native.sh](gates-native.sh)). The frozen reference reducer shares the constant, so the recovery-equivalence and indexing tests check the 4,096-entry ring against the frozen reducer at 4,096.
- **Declared equivalence domain.** The suites' scenarios and generated histories under a 4,096-entry ring. Two test-only adaptations, applied by `build.py ring()` and recorded:
  - `TestSlotSetSearches` skips suffix offsets below zero (it starts suffixes at `capacity − 5000`);
  - `TestBBRPTOEvidenceBounds/retained eviction` registers about twice `maxDeliveryRetained` outcomes after the persistent-congestion evidence. At 4,096 entries that evidence is evicted, which is the treatment's own deviation, so the case now asserts the eviction (`OutcomeEvicted > 0`) and the absent span.
  Without the adaptations, these were the only two failures ([gates/attempt1](gates/attempt1/)).
- **Injected second violations** ([gate_trees.py](gate_trees.py)). The suite must fail on each:
  - `ring-m1`: eviction no longer reports the evicted ordinal as missing. It was caught by `TestRecoveryEquivalenceScenarios` (eviction of a run's first endpoint) and `TestRecoveryEquivalenceHistories`.
  - `ring-m2`: eviction leaves the slot's eligibility bit set. It was caught by `TestRecoveryLowerBoundEquivalence`, the ring-wrap scenario and the histories.
- **Preflight** ([memstages.py](memstages.py) `preflight`, after the registered `memsmoke` activation run). The predicted reduction is the activation run's sender ring bytes under the candidate minus under the treatment, from the explicit accounting. The development runs measured 1,198,232 − 150,360 bytes = **0.999 MiB**. The **detectability threshold is 0.5 MiB** ([departure C](#interpretations-and-departures-from-d6-for-approval)). The decision is:
  - **unusable** if a gate fails or a mutant goes undetected;
  - **inert** if the activation run shows no reduction or the ring is not 4,096 entries;
  - **not run** if the predicted reduction does not exceed the threshold;
  - **run** otherwise.
  A treatment that is not run releases its 16 observations.
- **Engagement**, rechecked in every counted run: the ring arm holds 4,096 entries and fewer ring bytes than the same block's candidate. The ring arm counts only in blocks where it is engaged and its traffic is comparable ([departure D](#interpretations-and-departures-from-d6-for-approval)).
- **Delivery records get no capacity treatment** (D6). Their bytes and the scratch buffer's are reported from the explicit accounting only.

## Stage 1: arms

Every arm carries the Stage 0 overlay. Rotation, quiet-host gating and the rerun rule are #739's.

| Stage | Path | Arms | Blocks and seeds | Observations |
| --- | --- | --- | --- | --- |
| `mem-s5` | S5 | `reno-mem` (frozen Reno); `cand-mem` (candidate); `cand-mem` A/A; `cand-mem-ring` (if the preflight says run); `cand-mem` receiver-controller (`rxreno`) | 4; 9001–9004 (#739's readiness seeds) | 40 (32 without the ring) |
| `mem-s6` | S6 | The same | 4; 9101–9104 | 40 (32) |
| `mem-loopback` | Loopback | Frozen Reno, candidate, A/A, receiver-controller (no ring) | 4 | 32 |
| `latcand-stream` | `loopback-long` | `reno` (frozen Reno), `cand` (BBRv3), plain builds | 6 | 12 |

The Stage 1 seeds are #739's readiness seeds, so each block's relay impairments match the readiness block of the same number ([departure E](#interpretations-and-departures-from-d6-for-approval)). The **receiver-controller arm** is the candidate build at both endpoints, with the receiver's controller set to Reno and the sender unchanged.

## Statistics and rule

Per cell (path, workload, endpoint) and block, from the counted attempt:
- **E**, the baseline excess: the candidate's peak RSS minus frozen Reno's.
- **v**, the A/A variation: |A/A candidate − candidate| ÷ E.
- A treatment's **removal fraction** f: (candidate − treatment arm) ÷ E.
- f − v and f + v form an empirical envelope, not a statistical confidence interval.

A block is **usable** when all of these hold:
- its counted attempt is clean;
- every expected arm is present and usable (an arm that left no receipt counts as missing);
- the memory series are valid with window coverage;
- E exceeds **0.5 MiB**.

Registered blocks with no attempt are listed as **missing**. An attempt is judged on the exact set of arm names (not their count), each arm's receipt, #712's usability test, instrument validity and #712's contamination test. Every read is guarded, so a malformed or failed artifact is recorded with its reason instead of stopping the analysis. Every expected cell ends with a registered label, an evidence gap included, even when a stage did not run ([memstages.py](memstages.py) `discover`, `attempt`, `choose` and `assemble`, tested in [memstages_test.py](memstages_test.py) with #739's real readers on failed and malformed artifacts). Each block row records its **fraction eligibility** (usable and E > 0.5 MiB) with the reason, consistent with the label's usable count.

The labels are checked in this order ([memrules.py](memrules.py) `label_cell`, tested in [memrules_test.py](memrules_test.py)):

1. **Evidence gap**: fewer than three usable blocks; or fewer than three explained ones, or a median unexplained share above 25%.
2. **Sender bookkeeping (ring)**, sender cells only: in at least three blocks, the ring arm is engaged and comparable and f_ring − v ≥ 0.70.
3. **Receiver-side controller state**, receiver cells only: in at least three blocks, the receiver-controller arm is engaged and comparable and f_rx − v ≥ 0.70.
4. **Localized to traffic-dependent live heap (cause unresolved)**. This needs at least three comparable blocks, and at least three blocks in which:
   - f_rx + v < 0.30;
   - the heap reading above holds (objects window share, live share and window live share all ≥ 0.70);
   - for **sender cells**, the ring diagnostic is valid in that block (run, engaged and comparable) and bounds the ring's share: f_ring + v < 0.30.

   A sender cell whose ring diagnostic is not run, inert, unusable or confounded is never localized ([departure I](#interpretations-and-departures-from-d6-for-approval)), so more A/A variation can never turn a competing ring share into localization (tested). **Delivery retention is never named.** No measurement in these runs places resident bytes in delivery storage. Flow-control occupancy (bytes received but unread) includes offset holes and can drain before a sample, so it is reported descriptively only.
5. **Mixed**: an engaged, comparable ring arm, or the receiver-controller arm, has a lower bound f − v ≥ 0.30 in at least three blocks, without a label above.
6. **Inconclusive (heap unused and free classes hold the excess; no discriminating comparison)**: (unused + free) excess ≥ 0.70 of E in at least three blocks. **Runtime headroom is never assigned**, because no arm or registered comparison discriminates it (departure F).
7. **Treatment unusable** or **treatment inert**, for a sender cell whose ring treatment is unusable or inert.
8. **Inconclusive (receiver-controller arm confounded or uncalibrated)** with fewer than three comparable blocks; **inconclusive (ring diagnostic not run)** for a sender cell whose ring was not run; otherwise **inconclusive**.

The perturbation gate then applies.

Registered limitation (consideration C-018): D6 bounds removal from above, not memory change in both directions. A receiver-controller arm that adds memory (negative removal) still satisfies the upper bound (tested).

**Comparability** ([memrules.py](memrules.py) `traffic`, `aa_band` and `comparable`; [departure G](#interpretations-and-departures-from-d6-for-approval)). D6's categories and the measures registered for them:

| D6 category | Measure | Paths |
| --- | --- | --- |
| Useful delivery | Goodput | All |
| Packets per useful GiB | Sender `PacketsSent` over the measured window ÷ useful GiB | All |
| Feedback traffic | Receiver `PacketsSent` over the measured window ÷ useful GiB | All |
| Control traffic | Control replies | All |
| Forward overflow | Relay forward overflow ÷ received | WAN |
| Reordering | **Not measured** | — |

The relay model can reorder: its own test is `TestPostServiceDelayCanReorderWithoutPausingService`. Loopback has no relay and no bottleneck overflow. Sender-declared loss is degenerate for BBR (above).

The band is computed per measure from **clean, usable calibration pairs only** (candidate against candidate A/A), and needs at least three such pairs; otherwise the cell is **uncalibrated** and no treatment arm is comparable. For each measure, the band is the largest A/A deviation over those pairs: relative for goodput and packets, with a floor of 1%; absolute for overflow, with a floor of 0.0005; and ±1 reply for control. The floors depart from a literal observed range, so that a pair of identical A/A runs does not reject every arm.

An arm is comparable in a block when every registered measure is present and inside the band. A rejected block can neither widen nor narrow the band (tested).

**Reported descriptively for every cell**, raised or not, without classifying: the median excess per class, the live and objects window excesses, flow-control occupancy, the explicit BBR structure bytes, E, v, and the ring and receiver-controller removals.

## S6 control p95, both workloads

D2's Up-policy rule, read strictly ([memrules.py](memrules.py) `queue_integrity`, `queue_by_phase` and `up_policy`; [departure L](#interpretations-and-departures-from-d6-for-approval)). Its inputs, per workload, are the S6 candidate and A/A runs of every block, so **eight runs**, all from clean blocks with usable arms, together with each run's phase log and the relay's 10 ms forward queue samples over the measured window.

**Timeline integrity** (second consideration, F2), per run:
- the phase log exists, its records are well-formed phases, and its timestamps are ordered;
- the queue samples inside the window are ordered and valid;
- at least 90% of the window's 10 ms ticks have a sample;
- no gap between consecutive samples, or at either window edge, exceeds 100 ms. Ticker jitter is allowed.

Any missing or invalid run, a run without a phase log, a failed integrity check, or a failed perturbation check for that workload is an **evidence gap**.

A cell is attributed to the **selected ProbeBW Up policy** when every run meets all three conditions:
- at least 70% of queue samples above 25 ms fall in Up, or in a Down entered directly from Up;
- the Cruise median and the Refill median queue delays are each below 1 ms, with both phases present;
- the readiness S5 matched-load p95 is within 1.20 (#739: STREAM **1.061**, DATAGRAM **1.021**).

Samples with no logged phase stay in the 25 ms denominator. A complete timeline (integrity met) that never enters Cruise or Refill, or that has no sample above 25 ms, does not meet the conditions and is **unresolved**, as under the prior rule. Missing evidence is an evidence gap (second consideration, F6).

Changes from #736's code: Down counted only after Up; unknown-phase samples kept in the denominator; Cruise and Refill each below 1 ms rather than the median of their medians; and no truncation at the last work dump. #736 truncated because its phases were dumped every 250 ms, while this log covers the whole run. The same thresholds apply to DATAGRAM.

Disclosed before data: #736's S6 DATAGRAM figures on r8 (98.3–98.9% in Up or Down; Cruise and Refill 0.15–0.20 ms; S5 matched-load 0.987) met the earlier reading's thresholds. A phase association neither explains the revision-to-revision rise (1.167 → 1.469 → 1.459) nor establishes algorithm necessity. Readiness p95 at 30 replies per WAN run is a screen, and a later exception decision must weigh tail-sample sparsity.

**`latcand-stream`** (operator decision 2) uses #715's `latency_preservation` unchanged. At about 120 replies per run, 500 replies per arm need **at least five** usable blocks, so a stage with only four usable blocks ends as an evidence gap.

## Inventory and cap

| Stage | Observations |
| --- | --- |
| Excluded: prerequisites (`smoke`, `perfsmoke`, `ecnsmoke`; #739's checks) and `memsmoke` (5 activation and instrument runs) | 11, outside the cap |
| `mem-s5`, `mem-s6` | 80 with the ring arm, 64 without |
| `mem-loopback` (operator decision 1) | 32 |
| `latcand-stream` (operator decision 2) | 12 |
| **Originals** | **124** with the ring, **108** without |
| Same-seed reruns (#714 rule, [follow.py](follow.py)) | At most **28** observations |
| **Cap** | **152** counted observations (136 when the ring is not run, since the rerun limit holds) |

D6's 100 (80 + 20 reruns) is scaled by 1.4 to 140 for operator decision 1 (112 originals and 28 reruns), plus 12 for decision 2. The ring's 16 observations are released when it is not run; they do not become rerun allowance.

A block is rerun once, with the same seed, when its counted attempt is contaminated, unusable, missing an arm or absent, while the rerun total and the overall total stay within their caps. A block that cannot be rerun under the caps is recorded (`reruns_not_run_cap`) and stays unusable.

**Halt and resume.** A failed observation is retained as it is, recorded in `failures.json`, and leaves its block unusable; the stage continues ([matrix.py](matrix.py) `block`). The same applies to the `memsmoke` activation runs.

**Preflight decision.** The preflight writes `ring-preflight.json` once and refuses to revise it.
- Missing or invalid activation evidence (the candidate or ring run) makes the ring **unusable**.
- Valid evidence with no reduction makes it **inert**.
- The instrument prerequisite is separate. All five `memsmoke` runs must be present, usable and instrument-valid; the receiver-controller arm engaged; phases logged on S6; and packet counters advancing at both endpoints. If it fails, the preflight exits with status 3 and the driver stops before Stage 1. The ticket then ends as a **prerequisite gap**, whatever the ring decision.

**Resume.** If the driver itself stops (for example, the quiet-host gate gives up after six hours), the operator session resumes by rerunning the stopped stage and the steps after it. Completed observations are reused and never rerun, retained attempts are never replaced, and the caps count what exists on disk.

## Disclosures made before data

- **No comparative observation of this ticket existed** when this registration was written. Instrument development used these runs, all excluded from every statistic and listed in the raw archive:
  - `memsmokedev` (first overlay build), `memsmokedev2` (the race-free overlay) and `memsmokedev3` (the overlay with packet counters): one S5 STREAM run each of `reno-mem`, `cand-mem`, `cand-mem-ring` and `cand-mem` receiver-controller (seed 9993), and one S6 DATAGRAM `cand-mem` run (seed 9992). Only instrument function was examined: coverage, receiver-controller engagement, ring bytes and entries, the predicted reduction, the phase count and the packet counters (which showed the candidate's `PacketsLost` at zero).
  - The fixture's one-line console summary, printed by #739's runner for every run, also showed the last S6 run's goodput and RSS. No candidate-versus-Reno figure was computed or examined.
  - A code-path dry run of [memstages.py](memstages.py) `cells`, on `memsmokedev` S5 STREAM copies relabelled as four identical blocks. It printed only key names and the first 12 characters of the labels the duplicates produced. With identical blocks the A/A variation is zero by construction, so these carry no information about r9.
- **Synthetic cases ran three times,** once per overlay build. The third set is committed.
  - The test for the higher-allocation-rate case first asserted a peak excess below 0.5 MiB. The second set gave 1.09 MiB in one block, so the test now asserts what D6 requires: a live excess under 0.25 MiB and no live-heap reading.
  - The heap reading (departure A) was revised after the consideration, using the second set. The literal peak-sample objects share localized the retained case in one of three blocks, and the window median in two of three. The window median was chosen for its D6 wording ("aligned to the window and to GC cycles") and recorded with its sensitivity. The third set gives three of three, and the test asserts at least two.
  - The synthetic cases are not comparative data about r9.
- **Gate attempts.**
  - [gates/attempt1](gates/attempt1/) is the ring tree before its two declared-domain adaptations.
  - [gates/attempt2](gates/attempt2/) holds the first overlay. Its native race subset found a data race in the overlay's own call counter: a plain global, incremented by every connection in a multi-connection test process. The overlay was made race-free (an atomic counter, and a phase log with an atomic last phase and a lock taken only on a phase change).
  - `cand-mem-stale-tail.log` holds steps that a superseded gate job appended after its log was moved. It is not evidence.
  - [gates/attempt3](gates/attempt3/) holds the race-free overlay before the packet counters were added. All its gates passed, and both injected violations were caught.
  - The registered gate logs are the ones in [gates/](gates/).
- **Prior figures known when writing:** #739's readiness and timeline values for r9, and #736's for r8, including the memory medians above, the A/A ranges and the descriptive heap-peak figures. #736's descriptive heap-site decomposition on r8 found bookkeeping of about 2.2–2.6 MiB leading three of four sender cells, delivery data of 4.0–5.3 MiB leading the STREAM receiver cells, and about 1.1–1.4 MiB of bookkeeping at the receivers, because BBRv3 runs at both endpoints.
- **Interpretation known in advance.**
  - *The ring label is out of reach by arithmetic.* The 4,096-entry ring removes about 1.0 MiB per endpoint, and the sender cells' readiness excesses are 2.98–8.93 MiB, so f_ring is at most about 0.34 (S5 DATAGRAM). No sender cell can reach 0.70. The ring arm can produce **mixed** where it removes at least 30% beyond A/A (likely only for S5 DATAGRAM, at about 0.34), and it is **required** for a sender cell's localization, which needs it to bound the ring's share below 30%. It is also the only causal test that the fixed ring's explicit bytes are resident and removable, which bears on the design-bound question the map defers.
  - *Without the ring arm, no sender cell can be localized.* If the preflight does not say `run`, every sender cell ends at best as **inconclusive (ring diagnostic not run)**.
  - *For sender cells, the receiver-controller arm leaves the sender unchanged.* Its removal is expected to be near zero there, so a sender cell's localization rests mainly on the GC-aligned accounting and the ring bound. D6 applies the receiver-controller arm to every cell, and so does this registration.
  - *Removing the receiver's BBR state may change ACK-path behavior.* That is what comparability checks.
  - Loopback STREAM goodput moves about 3% with code placement alone (#738), so its perturbation check may fail on placement rather than on instrument cost. Under operator decision 1, the loopback cells would then end as an instrumentation gap.

## Interpretations and departures from D6, for approval

- **A. Heap reading.** "At least 70% of the excess in heap objects" is read as the window-median heap-objects excess, aligned to the window and to GC cycles, and backed by GC-aligned doubled live heap at the peak and over the window. The first registration's heap share plus doubled live was rejected by the consideration: a headroom-heavy excess (objects 3.5 and unused 6.5 of E 10) passed it. That case fails this reading (tested). Sensitivity is recorded above.
- **B. Phase log in place of the timeline overlay.** D6 names the timeline overlay for the S6 inputs. That overlay grows its arrays without bound and marshals them to JSON every 500 ms on the connection goroutine, which would inflate the candidate's resident memory in a memory study. The phase log records the same transitions D2's rule used on #711, #715 and #736 (the counting overlay's `c4Phase`), in a fixed BSS array, with nothing marshalled until stop. It is part of every candidate memory build, so the perturbation check covers it.
- **C. Ring detectability threshold of 0.5 MiB.** D6 derives it from the BBR A/A arm's RSS range, but no BBR A/A observation exists before Stage 1. The proxy is #739's frozen-Reno A/A: 40 paired |peak RSS differences| across S5 and S6, both endpoints, five blocks each. Their median is 0.23 MiB, their upper quartile (Python `statistics.quantiles`, n=4, exclusive method) 0.41 MiB, and their maximum 2.0 MiB (S5 DATAGRAM sender, block 1); 6 of 40 exceed 0.5 MiB.

  The same threshold serves twice: the ring preflight (predicted reduction 0.999 MiB) and block usability for fractions (E > 0.5 MiB). The proxy is a Reno, not a BBR, tail, and a reduction near 1 MiB is detectable against its typical pair but not against its largest. The contemporaneous BBR A/A variation then enters every block's rule through v.
- **D. Ring comparability.** The ring arm counts only where its traffic is inside the A/A band.
- **E. Stage 1 seeds are #739's readiness seeds,** so perturbation compares matched impairments. That does not remove session drift.
- **F. Runtime headroom is never assigned**, because D6 requires a discriminating comparison and none is registered.
- **G. Comparability measures and band.** The table above maps each D6 category to its measure. **Reordering is not measured**, and loopback has no overflow measure. This is a narrower rule than D6's: reordering is left unestablished rather than assumed unchanged. The band is the largest clean-pair A/A deviation, with floors, and needs three clean calibration pairs.
- **H. Block usability.** Only clean counted attempts with every expected arm and valid memory series count, for fractions, calibration, perturbation and S6 scoring.
- **I. Sender localization needs a valid ring bound** (consideration item 11): f_ring + v < 0.30 in each localizing block, and never without a valid ring diagnostic.
- **J. Cell aggregation of unexplained blocks.** At least three explained usable blocks, and a median unexplained share over the usable blocks of at most 25%. A cell with three explained and one unexplained block can therefore be labeled; one with two unexplained cannot.
- **K. Perturbation gates both builds,** on clean blocks, with at least three on each side, and an unvalidated check fails.
- **L. D2 read strictly** (Down only after Up; unknown phases in the denominator; Cruise and Refill each below 1 ms; all eight runs required). The first registration claimed #736's arithmetic unchanged; that claim was wrong, and these changes make the rule stricter.

## Consideration and revisions

The first registration (`c0c33ebf`) was gated before any data by `ras consider` (run `20261008T154923-d4f838f89528d7c3f5ee84ac`). The prompt quoted D6, D2's rule and both operator decisions verbatim. Every item was fixed, before any comparative data:

| Item | Finding | Disposition |
| --- | --- | --- |
| 1 | The A/A band drew on rejected observations | Fixed: clean calibration pairs only, at least three, otherwise uncalibrated; tested with a rejected outlier |
| 2 | S6 scoring accepted incomplete or invalid inputs | Fixed: eight runs from clean blocks, a phase log in each, and the perturbation gate; otherwise an evidence gap |
| 3 | Doubled live heap admitted headroom as live heap | Fixed: departure A revised; the counterexample is tested |
| 4 | Residual and gap could cancel | Fixed: \|residual\| + \|gap\|; both directions and the boundary tested |
| 5 | Flow-control occupancy named delivery retention | Fixed: occupancy is descriptive; delivery retention is never named |
| 6 | Comparability omitted D6 categories | Fixed: packet counters added for all arms and paths; reordering left unestablished as a narrower rule (G) |
| 7 | D2 mismatches (Down without Up, unknown phases, the Cruise/Refill aggregation, quiet queues) | Fixed: departure L; fixtures for each case |
| 8 | The window evidence accepted out-of-window or sparse samples | Fixed: actual timestamps inside the window, 80% coverage, valid metrics |
| 9 | Malformed or failed observations crashed the analysis or halted the driver | Fixed: validated loading, missing and incomplete blocks, failures recorded, resume procedure; [memstages_test.py](memstages_test.py) |
| 10 | Perturbation used rejected observations and gated one build | Fixed: departure K; limitation disclosed |
| 11 | The ring veto weakened as uncertainty grew | Fixed: departure I; monotonicity tested |
| 12 | Threshold, aggregation and cap needed explicit approval | Fixed: departures C and J and the inventory table; the cap is 152 with 28 reruns, both branches tested |

**Second consideration** (run `20261008T163921-46a8a643e7fd18818e03084a`, of `2bade4e5`, the same verbatim governing text). It confirmed departures A–L and the inventory arithmetic. It found seven defects needing no new data, all fixed before any comparative data:

| Item | Finding | Disposition |
| --- | --- | --- |
| F1 | Instrument validity was not applied to every arm or to selection | Fixed: per-run validity for every arm and endpoint, the exact arm set, propagation to reruns, calibration, perturbation and S6; tested |
| F2 | S6 could attribute from a nearly empty timeline | Fixed: registered timeline integrity; the three-sample reproduction, truncation, interior gaps, malformed phases and jitter are tested through the rule and `p95()` |
| F3 | Failed artifacts could stop selection and latency analysis | Fixed: guarded discovery and reads; latency reads clean blocks only; tested with real readers |
| F4 | A failed activation run stopped the driver without a registered ending | Fixed: failures recorded; `unusable` versus `inert`; a separate instrument prerequisite with exit status 3; the decision is never revised; tested |
| F5 | The sampling-gap disclosure was wrong | Fixed: provenance corrected, distribution and small-cell sensitivity disclosed; the conservative charge is kept |
| F6 | A complete timeline without Cruise or Refill read as an evidence gap | Fixed: it is unresolved, and missing evidence is a gap; tested |
| F7 | Block rows reported usability inconsistent with the label | Fixed: fraction eligibility with its reason; boundary values tested |

Not acted on, as its synthesis advised: C-011, C-010, C-008, C-012, C-018 and C-013, each disputed or a wording matter.

Not acted on from the first consideration, as its synthesis advised:
- C-018 (the negative-removal limitation) is recorded above.
- No new sender intervention is added (C-025), and no larger allocation-rate campaign (C-017).
- A/A runs stay in S6 scoring (C-046), and the inherited latency adequacy thresholds stay unchanged (C-024).
- The rotation is unchanged; its unequal temporal spacing is a limitation (C-042).
- The 152 cap is kept (C-032).

## Time estimate

All aids, builds and gates are in place. On a quiet host:
- prerequisites and `memsmoke`: about 20 minutes;
- `mem-s5` and `mem-s6`: 80 observations at about 45 s, plus one quiet-host gate of at least a minute per block call, about 70 minutes;
- `mem-loopback`: 32 observations at about 30 s, plus gates, about 20 minutes;
- `latcand-stream`: 12 runs at about 130 s, plus gates, about 30 minutes.

**About 2 h 20 min without reruns.** Each WAN rerun block costs about 5 minutes and each loopback-long block about 5 minutes. Foreign load lengthens the gate's waits.

## Order

On the Mac: `build.py` (done), `gate_trees.py`, `gates.sh` (done), `memrules_test.py`, `memstages_test.py`, `rules_test.py`, `localize_test.py`, `receiver_test.py` and `harness_test.py` (done); then `sync.sh`. On `minimax`, `gates-native.sh` (done, before data). Then, sequentially under `taskset -c 1-3`, with no build or test running there while a stage measures, [driver.sh](driver.sh):
1. host facts, `prereq.py`, `smoke`, `perfsmoke`, `ecnsmoke` and `memsmoke`, then `stages.py fold`;
2. `memstages.py preflight`, which writes `ring-preflight.json` and fixes the ring arm before any Stage 1 observation;
3. `mem-s5`, `mem-s6`, `mem-loopback` and `latcand-stream`;
4. `follow.py reruns`.

Then `memstages.py all` and `stages.py prereq` anywhere.
