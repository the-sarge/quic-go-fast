# Bounded-work BBR ECN feedback with frozen equivalence

Status: experimental build for [Build the bounded-work BBR ECN feedback correction with frozen equivalence](https://github.com/the-sarge/quic-go-fast/issues/709), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). It implements D1 of the [WAN-rate transport correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md#d1-ecn-feedback-work-bounded-rewrite-with-unchanged-semantics). No production merge, endpoint measurement, pacing change, paid resource, campaign or ledger change. [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613) remains the production owner.

Branch `codex/bbr-ecn-bounded-feedback`, based on the full C1–C4 candidate `8a0e0962` (`codex/bbr-model-conformance`).

| Revision | Content |
| --- | --- |
| `c1028b4d` | Production rewrite of `feedback`, frozen reference and equivalence oracle |
| `76bbdcfe` | Work-accounting, allocation tests and benchmark |
| `c75d3145` | Capacity bound in the oracle and split-growth capacity case; benchmarks ran here |
| `777ce739` | Independent-review gaps closed (cases, a mutant, range-count work test); oracle, mutation sweep and gates ran here |

Production code (`internal/ackhandler/bbr_ecn.go`, `delivery_sampler.go`) is identical across all four revisions, apart from one source comment added in `777ce739`.

## Answer

**Yes.** BBR ECN feedback processing is work-bounded, and the oracle shows it equivalent to the frozen tracker over the declared domain. No ledger rule, cap or validation order changed, and there is no ECN contract change.

- Over 917,000 generated oracle operations and two fuzz runs (9.4 million and 6.5 million executions), results and the full semantic snapshot matched after every operation. The named cap, edge-merge and combined-failure cases also matched.
- 42 of 45 semantic mutants are killed. The 3 survivors are dispositioned as observationally equivalent within the domain.
- An independent review found no blocking defect and agreed with both the equivalence argument and the right-neighbour argument.
- Steady-state advancing ACKs allocate nothing. The frozen tracker allocates 1–10 times per ACK cycle, up to 320 KB.
- With the ACK and its window fixed, inspections and rewrites stay flat as unaffected records grow from 16 to 4,000. Binary search grows logarithmically. Suffix relocation and prefix compaction grow linearly and are counted separately, as D1 accepted.

## Shape built

`feedback` (`internal/ackhandler/bbr_ecn.go`):

1. **Window.** Two binary searches find the records overlapping `[LowestAcked, LargestAcked]`. The window adds one unchanged neighbour on each side.
2. **Merge cursor.** One pass walks the window records in ascending order and the ACK ranges from the end, also ascending. It skips ranges wholly below the current record and visits only the ranges that intersect it. The same pass accumulates newly ACKed ECT counts and the largest-acknowledged anchor exactly as the frozen loop does. It also emits the split parts into tracker-owned scratch, which is reset per ACK. The scratch uses the same merge predicate as `appendRange`, factored into `extendsRange`.
3. **Validation, unchanged and in frozen order.** Counter consistency (`failCounters`), then missing anchor (`failEvidence`), then delta coverage (`failCounters`), then the budget. Nothing in the ledger changes before all four pass.
4. **Budget.** `len(ranges) − window + len(scratch) > 4096` fails with `failEvidence` and leaves the ledger, accepted counts and watermark as they were. This matches the frozen outcome when its intermediate `next` reached 4,096: the frozen `next` only grows, so it fails exactly when the merged post-split length exceeds the cap. The check runs before compaction, as in the frozen tracker.
5. **Splice.** The scratch replaces the window in place. A length change moves the unaffected suffix with one `copy`. Growth doubles capacity, clamped to 4,096, and happens only when the ledger outgrows its previous capacity. `compact` is the frozen code plus a move counter.

One deviation from D1's wording: the counting pass and the rewrite into scratch are a single cursor pass, not two. This is not observable, because scratch is private and the ledger is only spliced after validation.

The tracker carries `ecnLedgerWork` counters (search probes, inspections, rewrites, relocated records, compaction moves) for the work evidence. A production version may drop them. `CloseDelivery` now releases scratch along with the ledger.

## Domain, invariant and the right neighbour

The domain is D1's caller-valid sequences. Registrations come in increasing packet-number order, with skips allowed. Ordinals are increasing and nonzero, with gaps where Initial/Handshake registrations break affinity. Codepoints are Not-ECT, ECT(0), ECT(1) or unsupported. ACK frames have wire-decoded ranges: descending, disjoint, at least one missing number apart. The domain also includes losses, path resets, drains and close.

The ledger is canonical: no two adjacent records are mergeable. Merges are transitive under affine ordinals, so a merged record inherits its parts' mergeability with the next record. A split can therefore only create merges with the window's immediate neighbours. The oracle confirms that left-edge merges occur, including a merge that bridges a fully ACKed record into the left neighbour.

**The right neighbour can never merge within the domain.** This is a finding the record did not anticipate:

- The right neighbour starts above `LargestAcked`, and `LargestAcked` exceeds the watermark (lower ACKs return early). Only ACKs whose largest is at or below the watermark have ever set a record's ACK status, so the right neighbour is always unacked.
- The window's last output either is ACKed or keeps the anchor record's tail properties, so it cannot merge with the neighbour.
- The mutation sweep agrees. Dropping the right neighbour (N2) changes no result or snapshot; only the exact work-count assertion catches it. W3/W4, which drop it in a sub-case, survive.
- The independent review reached the same conclusion on two grounds: the ACK-status argument above, and an unchanged tail that is no more mergeable than its parent record. No in-domain ledger can make the right neighbour mergeable, so the named "right edge" case pins outcomes but cannot discriminate a missing neighbour. The argument depends on registered packet numbers always exceeding the watermark, which the caller guarantees.

The build keeps the right neighbour as D1 specifies. It costs one inspection and one rewrite. Production can drop it with this argument as its justification.

## Equivalence oracle

`internal/ackhandler/bbr_ecn_frozen_test.go` holds the C1–C4 tracker as `frozenBBRECNTracker`: every method, verbatim apart from the type name. It was diff-checked against `8a0e0962`. `internal/ackhandler/bbr_ecn_equivalence_test.go` drives both trackers with identical operations: `mode`, `sentPacket`, `feedback`, `lostPacket`, `resetPath`, drain/capability changes and close. After every operation it requires the following to be equal:

- every `ECNResult`, including pre-validation fields on failure paths, and every `mode()` return;
- the semantic snapshot: records (bounds, ordinal, generation, codepoint, ACK status), compacted counts and ordinal, sent, accepted and offsets, watermark, generation, fence, state, draining, closed, testing slots, testing sent and lost counts, evidence-lost and counter-failed.

It excludes the path function's identity, capacity, scratch and work counters. It separately requires capacity to be at most 4,096, because the cap also bounds memory.

**Generated sequences** (seeded PCG; the same driver backs `FuzzBBRECNFeedbackFrozenEquivalence`). Counts below are from `oracle-and-work.txt`.

| Regime | Seeds × ops | Accepted ACKs | Deferred | Grew / shrank / compacted | Counter failures | Max ledger |
| --- | --- | --- | --- | --- | --- | --- |
| Mixed: modelled receiver, plus chaotic frames in a third of seeds | 1,500 × 300 | 21,470 | 23,623 | 7,772 / 3,922 / 4,720 | 702, plus 9 missing anchors | 100 |
| Fragmented frames (up to 400 ranges) | 300 × 600 | 6,276 | 8,197 | 2,374 / 1,298 / 948 | 220, plus 1 missing anchor | 166 |
| Near cap: pinned hole, mark runs | 6 × 12,000 | 4,107 | 5,155 | 1,213 / 1,469 / 0 | 0 | 4,096 (6 insertion-cap failures) |
| Randomized cap: pinned ledgers 0–5 records under the cap, island ACKs | 200 seeds | 145 fitting | — | — | — | 178 split-budget overflows |

How the generator models traffic:

- **Modelled receiver.** It delivers packets late or never and acknowledges its received set, with occasional truncation to the newest ranges. Frames reach the sender reordered and duplicated. About 0.5% of frames carry corrupted counters.
- **Chaotic frames.** Arbitrary ranges below a largest that is usually a sent packet and occasionally any number up to the last sent, so a skipped number can be the largest (a missing anchor). Ranges can cover skipped and never-sent packet numbers.

An early generator invented frames no receiver sends: a later frame acknowledged packets an earlier frame had already counted while omitting them. The frozen tracker treats that as a counter failure, by RFC 9000's newly-acknowledged rule, which killed nearly every run. Truncation was made rare so runs stay live. The frozen behaviour is preserved, not evaluated.

**Named cases** (`TestBBRECNFeedbackAdversarialEquivalence`, `TestBBRECNFeedbackCapEquivalence`), each also asserting its own claimed outcome:

- packet-number gaps and skipped numbers;
- Not-ECT and ECT(1) holes, plus a missing anchor above the ledger;
- a missing anchor at an interior skipped number, with the window left unspliced;
- non-affine ordinals;
- generation reset, fence and drain;
- reordered, duplicate and lower-largest ACKs;
- a 1,000-range ACK frame splitting one record into 2,001, then one frame merging and compacting them all;
- left-edge merge, bridging merge through a fully ACKed record, and an interior right merge;
- the right edge never merging, for both endings of the anchor record;
- insertion to 4,095, 4,096 and 4,097 records;
- an ACK-induced split to 4,095, 4,096 and 4,097 records;
- merge at the cap, for both registration and ACK;
- overflow before compaction despite a reclaimable ACKed prefix;
- combined invalid counters and an over-cap split (counter consistency wins);
- a delta-coverage failure and an over-cap split (delta coverage wins: `counterFailed`, not `evidenceLost`);
- split growth past a split-sized capacity (capacity stays at or below 4,096).

## Mutation sweep

`mutate.py` applies 45 single-site mutants and runs every ECN test in the package against each one. The mutants cover window bounds, neighbour extension, the merge cursor, the scratch-site merge predicate (registration keeps the real one), the projected-length check, validation order and atomicity, and the splice. Full results are in `mutation-results.tsv`: **42 killed**. L6 and L8, which move the budget check above counter consistency and between the anchor and delta-coverage checks, are each killed by their combined-failure case. The anchor and budget checks both call `failEvidence`, so their relative order is not observable.

| Survivor | Mutation | Disposition |
| --- | --- | --- |
| W2 | End search uses `searchRanges(largest)` instead of `largest+1` | **Equivalent everywhere.** Both forms, after the `first <= largest` adjustment, yield the index just past the record containing `LargestAcked`, or the first record above it when none contains it. |
| W3 | The record containing `LargestAcked` is not added to the window end | **Equivalent within the domain.** When that record extends past `LargestAcked`, neighbour extension still includes it, and only the true right neighbour is dropped. See [the right-neighbour argument](#domain-invariant-and-the-right-neighbour). |
| W4 | End adjustment uses `first < largest` | **Equivalent within the domain**, for the same reason, in the sub-case where the record containing `LargestAcked` starts at it. |

S5 (splice growth ignoring the cap clamp) initially survived because capacity is representation-only. It is now killed by the oracle's capacity bound and the split-growth capacity case.

## Work accounting

`TestBBRECNFeedbackWorkStaysFlat` uses one ACK with three ranges that splits a 20-packet ECT(0) record. A hole pins the prefix, so compaction is isolated.

| Unaffected records | Ledger | Search probes | Inspections | Rewrites | Relocated | Compaction moves |
| --- | --- | --- | --- | --- | --- | --- |
| suffix 0 | 7 | 3 | 5 | 7 | 0 | 0 |
| suffix 16 | 23 | 9 | 6 | 8 | 15 | 0 |
| suffix 256 | 263 | 17 | 6 | 8 | 255 | 0 |
| suffix 1,024 | 1,031 | 21 | 6 | 8 | 1,023 | 0 |
| suffix 4,000 | 4,007 | 24 | 6 | 8 | 3,999 | 0 |
| prefix 16 | 22 | 8 | 5 | 7 | 0 | 0 |
| prefix 1,024 | 1,030 | 20 | 5 | 7 | 0 | 0 |
| suffix 1,024, prefix resolved | 1,028 | 22 | 5 | 6 | 1,023 | 1,028 |

`TestBBRECNFeedbackWorkScalesWithWindow` holds 1,000 unaffected records on each side and varies the ACK's range count over one record:

| ACK ranges | Inspections | Rewrites | Relocated |
| --- | --- | --- | --- |
| 1 | 4 | 4 | 999 |
| 4 | 7 | 10 | 999 |
| 16 | 19 | 34 | 999 |
| 64 | 67 | 130 | 999 |
| 256 | 259 | 514 | 999 |

Inspections are window records plus intersecting ranges (`3 + n`), never their product. The frozen tracker would inspect all 2,003 records against every range.

A non-empty suffix contributes exactly one window record, the right neighbour, which is rewritten rather than relocated. The split grows the window, so the rest of the suffix moves once in a single bulk `copy`. When the ACK resolves the prefix, compaction moves every retained record, exactly as the frozen `compact` did. For comparison, the frozen tracker inspected every record against every ACK range and rewrote the whole ledger on every advancing ACK. That is 4,007 × 3 inspections and about 4,007 writes, plus a fresh slice, in the 4,000-record row.

## Allocations and microbenchmark

`TestBBRECNFeedbackSteadyStateAllocations` measures advancing ACK cycles of 10 ECT(0) packets after warm-up, with `testing.AllocsPerRun` over 1,000 cycles:

| Scenario | Frozen | Bounded |
| --- | --- | --- |
| In order | 1 | **0** |
| In order behind 1,024 retained records | 9 | **0** |
| Reordered splits behind 1,024 retained records | 9 | **0** |

`BenchmarkBBRECNFeedback` measures the same cycles: 5,000 iterations × 5 runs on an Apple M4 Max, go1.27.1, `bench.txt`. The host was loaded (load average 12–18), so ranges are wide. This is a tracker microbenchmark, not endpoint CPU.

| Retained records / order | Frozen ns/op | Frozen B/op, allocs | Bounded ns/op | Bounded B/op, allocs |
| --- | --- | --- | --- | --- |
| 0 / in order | 136–173 | 320, 1 | 80–108 | 0, 0 |
| 0 / reordered | 122–153 | 320, 1 | 105–283 | 0, 0 |
| 256 / in order | 5,278–8,511 | 41,026, 7 | 96–174 | 0, 0 |
| 256 / reordered | 5,991–17,723 | 41,025, 7 | 116–119 | 0, 0 |
| 4,000 / in order | 47,609–96,360 | 327,754, 10 | 95–252 | 0, 0 |
| 4,000 / reordered | 55,140–97,034 | 327,746, 10 | 119–126 | 0, 0 |

With an empty ledger the two are comparable, and the reordered bounded range overlaps the frozen one under host load. The gain is in allocation and in independence from retained ledger size.

## Independent review

A read-only review of `c75d3145` found no blocking defect and could not construct a diverging sequence. It confirmed four things:

- the window-plus-neighbours argument and the cursor's exact correspondence to the frozen intersections;
- the budget equivalence to the frozen intermediate `next` reaching 4,096;
- snapshot completeness against every tracker field;
- that the frozen reference matches the frozen source line for line.

`777ce739` closed the gaps it raised:

- the delta-coverage versus budget order, with a case and mutant L8;
- the missing anchor at an interior skipped number, in a case and in chaotic generation;
- work scaling with the ACK's range count;
- wording about the right neighbour's inertness.

Gaps it raised that remain open:

- The oracle mirrors `CloseDelivery` by hand.
- The frozen reference shares `ecnMarkRange`, `maxECNMarkRanges`, `ecnState` and `numECNTestingPackets` with production, so a change to those would move both trackers.
- The capacity bound covers the ledger, not scratch. Scratch is bounded by the window plus about two entries per ACK range and is retained until close.
- The allocation test uses one stream shape.
- The five work counters sit on a production struct.

## Gates

`gates.log`, at `777ce739`:

- `go vet ./...` is clean.
- `internal/ackhandler` and `internal/congestion` pass with and without `-race`. The race run of `internal/ackhandler` takes about 370 s, mostly the oracle under the race detector. A production adoption should scale the oracle down under `-race` or `-short` (it already honours `-short`).
- Root BBR/ECN tests pass (39 pass, 1 platform skip), and so does the full root package (648 pass, 7 platform skips).
- Two 180 s fuzz campaigns ran 9.4 million executions at `c75d3145` and 6.5 million at `777ce739`, with no divergence.

## Limits and returned items

- No endpoint performance was measured. Whether this removes the S5 sender-CPU excess attributed to ECN churn is for [Re-demonstrate the WAN-corrected BBRv3 candidate in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/711), on the revision that [Adopt quantum-release BBR pacing and explain the STREAM receiver heap rise](https://github.com/the-sarge/quic-go-fast/issues/710) builds on top of this one.
- Suffix relocation and prefix compaction moves remain linear in retained records under the cap, as D1 accepted for this experiment. The production question stays with the re-demonstration's CPU result and production slicing. Retained occupancy is not bounded by recovery flight.
- The right-neighbour finding is offered to production, not acted on.
- The equivalence claim covers the declared domain. Generated sequences cover it by sampling, not exhaustively. Chaotic frames extend beyond it to ranges that cover never-sent packet numbers.

## Files

- `README.md` (this record), `environment.txt`, `gates.log`
- `oracle-and-work.txt`: oracle coverage, allocation and work-accounting logs
- `bench.txt`: raw benchmark output
- `mutate.py`, `mutation-results.tsv`: mutation sweep and results
