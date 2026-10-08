# Memory and latency attribution of the BBRv3 candidate r9

**Date:** October 8, 2026, America/New_York. **Scope:** [Attribute the BBRv3 candidate's memory and S6 latency flags on the final revision](https://github.com/the-sarge/quic-go-fast/issues/740), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D6 ("Ticket H") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d6--ticket-h-attribute-the-memory-and-s6-latency-cells-on-the-final-survivor), plus two operator decisions recorded below. Measurement only: no production merge, transport source change, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-r9-memory-attribution`, based on `138223ac` (the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md), #739), whose tree's code is exactly r9. The revision under test is **r9, `81e9dc8c`**: r8 plus #738's 32-entry BBR send queue. No BBR revision newer than r9 exists. The operator notes that the BBRv3 implementation is still under active performance development. Every result here describes r9 on `minimax` only. None is a verdict on BBRv3 or on later revisions.

**Status: registration.** Everything below was written, with the aids built, the gates run and the instruments checked, before any comparative observation of this ticket. Results, deviations and later sections will be added after the data, below a marker, and these sections will stay unchanged.

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
- **Sampler** ([mem/mem_series.go](mem/mem_series.go)). Once a second, and at stop, each endpoint appends one JSON line to `{role}.mem.jsonl` with:
  - `smaps_rollup` (Rss, Pss, Anonymous and the other fields) and `/proc/self/status` (VmHWM, VmRSS, RssAnon, RssFile);
  - the Go runtime's disjoint `/memory/classes/…` quantities, GC cycles, heap goal and live heap;
  - the BBR structure bytes;
  - #710's receive occupancy counters.
  It writes from preallocated buffers, so its own footprint is small and constant. It replaces #710's 10 ms series, which held every sample in memory until exit.
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

Released heap is excluded. The readiness cell's peak (`ru_maxrss`, which the launcher keeps per endpoint) is the largest sampled Rss plus a **sampling gap**. Each block's excess is reconciled class by class at each endpoint's own peak sample, so the classes, the residual and the gap sum to the peak excess exactly (`closure`, tested).

A block is **unexplained** when |residual + gap| exceeds **25%** of its peak excess. A cell with fewer than three explained usable blocks is an **evidence gap** and gets no causal label. The heap goal is context only. The BBR structure bytes are explicit accounting, reported beside each cell; they are part of heap objects.

**GC alignment** (see [departure A](#interpretations-and-departures-from-d6-for-approval)). Heap objects at one sample include garbage not yet collected, which moves between the objects and free classes with the GC cycle. With the fixture's default GOGC=100, a live-heap excess L raises resident heap by about 2L. The rule therefore reads the heap at GC alignment, using three shares:
- **heap share**: (objects + unused + free) excess ÷ E;
- **live share**: 2 × the excess of `/gc/heap/live` (live heap at the last completed mark) at the peak sample ÷ E;
- **window live share**: the same quantity as the median over the measured window's per-second samples, aligned by seconds since each run's configured start.

A cell's excess sits in **traffic-dependent live heap** only when all three reach 0.70. A warmup-only excess fails the window share.

**Synthetic cases (D6), measured on `minimax`** with the unchanged sampler, three blocks each, against a `none` case with a 4 MiB live baseline and steady garbage ([synthetic/](synthetic/), [memrules_test.py](memrules_test.py) `Synthetic`, all passing):

| Case | Peak excess (MiB) | What the accounting shows | Rule outcome |
| --- | --- | --- | --- |
| Known retained allocation (8 MiB) | 15.5–16.0 | Live +7.99 to +8.01; heap, live and window shares 1.00–1.03 | Live heap, explained |
| Warmup-only spike (24 MiB) | 47.0–47.5 | Live share at the peak 1.01–1.02; window live share 0.00 | Not live heap (window) |
| Goroutine-stack growth | 15.25–15.5 | Stacks +8.0; live share 0.00 | Not live heap |
| Four times the allocation rate at a fixed live heap | 0.32–1.09 | Live +0.07 to +0.16 | Not live heap; unexplained in all three |
| Runtime accounting fails (8 MiB anonymous mmap) | 7.69–8.00 | Residual +7.73 to +8.09 | Unexplained; **no causal label** even with arms that would otherwise give one (tested) |

**Perturbation.** D6 requires instrumented goodput of at least 0.99 of plain. The check is the median goodput of the instrumented candidate arm over the four Stage 1 blocks, divided by the median of #739's plain readiness candidate over its blocks 1–4. Those blocks use the same seeds, so the relay impairments match. It applies per path and workload, loopback included (operator decision 1). A failing path and workload ends its raised cells as **evidence gap (instrument perturbation)**, and the rule's own label is kept beside it for the record. The frozen-Reno arm's ratio is reported, not gated.

**Instrument checks** (development runs; see [Disclosures](#disclosures-made-before-data)):
- sampling covers every second of every run;
- the receiver-controller arm's receiver reports Reno, holds no ring bytes and logs no phases, while its sender holds the candidate's ring;
- the S6 candidate logs 51–54 phase transitions per run.

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
- A removal **counts** only beyond the A/A variation: its lower bound is f − v and its upper bound f + v.

A block is **usable** when its counted attempt is clean (every arm usable and none contaminated) and E exceeds **0.5 MiB**. At least three usable, explained blocks are needed ([memrules.py](memrules.py) `label_cell`, tested in `Labels`). The labels are checked in this order:

1. **Evidence gap**: fewer than three usable blocks, or fewer than three explained ones.
2. **Sender bookkeeping (ring)**, sender cells only: in at least three blocks, the ring arm is engaged and comparable and f_ring − v ≥ 0.70.
3. **Receiver-side controller state**, receiver cells only: in at least three blocks, the receiver-controller arm is comparable and f_rx − v ≥ 0.70.
4. **Localized to traffic-dependent live heap**. This needs at least three comparable blocks, and at least three blocks in which:
   - f_rx + v < 0.30 (the upper bound below 30%);
   - the heap, live and window live shares are all ≥ 0.70;
   - for sender cells, no engaged, comparable ring arm removes f_ring − v ≥ 0.30.
   The sub-label is **delivery retention** when, in at least three of those blocks, the receive occupancy excess at the peak sample (bytes received but unread, from the occupancy hooks: the reassembly-buffer evidence D6 allows) is at least 0.70 of the live-heap excess. Otherwise the cause is **unresolved**.
5. **Mixed**: a treatment's lower bound f − v ≥ 0.30 in at least three blocks, without a label above.
6. **Inconclusive (headroom-dominant accounting; no discriminating comparison)**: heap share ≥ 0.70 and live share < 0.30 in at least three blocks. **Runtime headroom is never assigned**, because no arm or registered comparison discriminates it ([departure F](#interpretations-and-departures-from-d6-for-approval)).
7. **Treatment unusable** or **treatment inert**, for a sender cell whose ring treatment is unusable or inert.
8. **Inconclusive (receiver-controller arm confounded)** with fewer than three comparable blocks; otherwise **inconclusive**.

The perturbation gate then applies.

**Comparability** ([memrules.py](memrules.py) `traffic`, `aa_band` and `comparable`). The measures are:
- useful goodput;
- control replies;
- on WAN paths, forward and reverse relay packets per useful GiB and the forward overflow fraction.

For each measure, the band is the largest A/A deviation (candidate A/A against candidate) over the cell's blocks: relative for goodput and packets, with a floor of 1%; absolute for overflow, with a floor of 0.0005; and ±1 reply for control. An arm is comparable in a block when every measure is inside the band ([departure G](#interpretations-and-departures-from-d6-for-approval)).
- The relay does not report reordering, so reordering is not checked.
- Loopback has no relay, so only goodput and control replies are checked there.
- The receiver-controller arm also has to be engaged: the receiver reports Reno and holds no ring bytes.

**Reported descriptively for every cell**, raised or not, without classifying: the median excess per class, the live and window live excess, the explicit BBR structure bytes, E, v, and the ring and receiver-controller removals.

## S6 control p95, both workloads

D2's Up-policy rule, unchanged ([memrules.py](memrules.py) `queue_by_phase` and `up_policy`, arithmetic copied from #736's `attribution.timeline_entry` and `stages.d2`). Its inputs are the S6 candidate and A/A runs (eight per workload), the phase log, and the relay's 10 ms forward queue samples over the measured window. A cell is attributed to the **selected ProbeBW Up policy** when every run meets all three conditions:
- at least 70% of queue samples above 25 ms fall in Up or Down;
- the median of the Cruise and Refill median queue delays is below 1 ms;
- the readiness S5 matched-load p95 is within 1.20 (#739: STREAM **1.061**, DATAGRAM **1.021**).

Otherwise the cell is **unresolved**, and an evidence gap without inputs. The same thresholds apply to DATAGRAM.

Disclosed before data: #736's S6 DATAGRAM figures on r8 (98.3–98.9% in Up or Down; Cruise and Refill 0.15–0.20 ms; S5 matched-load 0.987) already met those thresholds. A phase association neither explains the revision-to-revision rise (1.167 → 1.469 → 1.459) nor establishes algorithm necessity. Readiness p95 at 30 replies per WAN run is a screen, and a later exception decision must weigh tail-sample sparsity.

## Inventory and cap

| Stage | Observations |
| --- | --- |
| Excluded: prerequisites (`smoke`, `perfsmoke`, `ecnsmoke`; #739's checks) and `memsmoke` (5 activation and instrument runs) | 11, outside the cap |
| `mem-s5`, `mem-s6` | 80 (64 without the ring) |
| `mem-loopback` (operator decision 1) | 32 |
| `latcand-stream` (operator decision 2) | 12 |
| Same-seed reruns (#714 rule, [follow.py](follow.py)) | Within the cap |
| **Cap** | **152** counted observations, originals and reruns |

D6's 100 (80 + 20 reruns) is scaled to 140 for operator decision 1, in proportion, plus 12 for decision 2. If the cap binds, remaining reruns are not run and are recorded (`reruns_not_run_cap`), and the blocks they would have replaced stay under #712's rule.

## Disclosures made before data

- **No comparative observation of this ticket existed** when this registration was written. Instrument development used these runs, all excluded from every statistic and listed in the raw archive:
  - `memsmokedev` (first overlay build) and `memsmokedev2` (the race-free overlay): one S5 STREAM run each of `reno-mem`, `cand-mem`, `cand-mem-ring` and `cand-mem` receiver-controller (seed 9993), and one S6 DATAGRAM `cand-mem` run (seed 9992). Only instrument function was examined: coverage, receiver-controller engagement, ring bytes and entries, the predicted reduction and the phase count.
  - The fixture's one-line console summary, printed by #739's runner for every run, also showed the last S6 run's goodput and RSS. No candidate-versus-Reno figure was computed or examined.
  - A code-path dry run of [memstages.py](memstages.py) `cells`, on `memsmokedev` S5 STREAM copies relabelled as four identical blocks. It printed only key names and the first 12 characters of the labels the duplicates produced. With identical blocks the A/A variation is zero by construction, so these carry no information about r9.
- **Synthetic cases ran twice.** The first set came from the first overlay build. The second came from the race-free overlay and is the committed set. The test for the higher-allocation-rate case first asserted a peak excess below 0.5 MiB. The second set gave 1.09 MiB in one block, so the test now asserts what D6 requires: a live excess under 0.25 MiB and no live-heap reading. Both sets agree otherwise.
- **Gate attempts.**
  - [gates/attempt1](gates/attempt1/) is the ring tree before its two declared-domain adaptations.
  - [gates/attempt2](gates/attempt2/) holds the first overlay. Its native race subset found a data race in the overlay's own call counter: a plain global, incremented by every connection in a multi-connection test process. The overlay was made race-free (an atomic counter, and a phase log with an atomic last phase and a lock taken only on a phase change).
  - `cand-mem-stale-tail.log` holds steps that a superseded gate job appended after its log was moved. It is not evidence.
  - The registered gate logs are the ones in [gates/](gates/).
- **Prior figures known when writing:** #739's readiness and timeline values for r9, and #736's for r8, including the memory medians above, the A/A ranges and the descriptive heap-peak figures. #736's descriptive heap-site decomposition on r8 found bookkeeping of about 2.2–2.6 MiB leading three of four sender cells, delivery data of 4.0–5.3 MiB leading the STREAM receiver cells, and about 1.1–1.4 MiB of bookkeeping at the receivers, because BBRv3 runs at both endpoints.
- **Interpretation known in advance.**
  - *The ring label is out of reach by arithmetic.* The 4,096-entry ring removes about 1.0 MiB per endpoint, and the sender cells' readiness excesses are 2.98–8.93 MiB, so f_ring is at most about 0.34 (S5 DATAGRAM). No sender cell can reach 0.70, and the ring arm can at most produce **mixed** or block localization. It still runs if its preflight passes, because it is the only causal test that the fixed ring's explicit bytes are resident and removable, and that bears on the design-bound question the map defers.
  - *For sender cells, the receiver-controller arm leaves the sender unchanged.* Its removal is expected to be near zero there, so a sender cell's localization rests mainly on the GC-aligned accounting and the ring bound. D6 applies the receiver-controller arm to every cell, and so does this registration.
  - *Removing the receiver's BBR state may change ACK-path behavior.* That is what comparability checks.
  - Loopback STREAM goodput moves about 3% with code placement alone (#738), so its perturbation check may fail on placement rather than on instrument cost. Under operator decision 1, the loopback cells would then end as an instrumentation gap.

## Interpretations and departures from D6, for approval

- **A. GC-aligned heap reading.** D6's "at least 70% of the excess in heap objects" is read as heap share, live share and window live share all ≥ 0.70. On the synthetic retained case, the literal objects-at-sample share was 0.52 in one of three blocks for a known 8 MiB live cause, because garbage moves between objects and free. The GC-aligned live share was 1.00–1.08.
- **B. Phase log in place of the timeline overlay.** D6 names the timeline overlay for the S6 inputs. That overlay grows its arrays without bound and marshals them to JSON every 500 ms on the connection goroutine, which would inflate the candidate's resident memory in a memory study. The phase log records the same transitions D2's rule used on #711, #715 and #736 (the counting overlay's `c4Phase`), in a fixed BSS array, with nothing marshalled until stop.
- **C. Ring detectability threshold of 0.5 MiB.** D6 derives it from the BBR A/A arm's RSS range, but no BBR A/A observation exists before Stage 1. The only paired A/A on this host and fixture is #739's frozen-Reno A/A: across S5 and S6, both endpoints, 40 paired |differences|, the median is 0.23 MiB and the upper quartile 0.41 MiB. The threshold is set at 0.5 MiB, above that quartile. The contemporaneous BBR A/A variation then enters every block's rule through v.
- **D. Ring comparability and the sender localization bound.** The ring arm counts only where its traffic is inside the A/A band. A sender cell is not localized to live heap where an engaged, comparable ring arm removes at least 30% beyond A/A. D6 states neither condition, and both make labels harder to reach, not easier.
- **E. Stage 1 seeds are #739's readiness seeds,** so perturbation compares matched impairments.
- **F. Runtime headroom is never assigned**, because D6 requires a discriminating comparison and none is registered. Headroom-dominant accounting ends as inconclusive.
- **G. Comparability band.** "Within the BBR A/A range" is read as within the largest A/A deviation over the cell's blocks, with stated floors. Reordering is not measured.
- **H. Block usability.** Only clean counted attempts count for fractions. A block that stays contaminated after its rerun is unusable for this rule.

## Time estimate

All aids, builds and gates are in place. On a quiet host:
- prerequisites and `memsmoke`: about 20 minutes;
- `mem-s5` and `mem-s6`: 80 observations at about 45 s, plus one quiet-host gate of at least a minute per block call, about 70 minutes;
- `mem-loopback`: 32 observations at about 30 s, plus gates, about 20 minutes;
- `latcand-stream`: 12 runs at about 130 s, plus gates, about 30 minutes.

**About 2 h 20 min without reruns.** Each WAN rerun block costs about 5 minutes and each loopback-long block about 5 minutes. Foreign load lengthens the gate's waits.

## Order

On the Mac: `build.py` (done), `gate_trees.py`, `gates.sh` (done), `memrules_test.py`, `rules_test.py`, `localize_test.py`, `receiver_test.py` and `harness_test.py` (done); then `sync.sh`. On `minimax`, `gates-native.sh` (done, before data). Then, sequentially under `taskset -c 1-3`, with no build or test running there while a stage measures, [driver.sh](driver.sh):
1. host facts, `prereq.py`, `smoke`, `perfsmoke`, `ecnsmoke` and `memsmoke`, then `stages.py fold`;
2. `memstages.py preflight`, which writes `ring-preflight.json` and fixes the ring arm before any Stage 1 observation;
3. `mem-s5`, `mem-s6`, `mem-loopback` and `latcand-stream`;
4. `follow.py reruns`.

Then `memstages.py all` and `stages.py prereq` anywhere.
