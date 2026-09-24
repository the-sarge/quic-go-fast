# Bounded recovery outcome ACK-processing cost — issue 618

**Disposition: recommend a separately scoped production change using bounded lookups in the existing packet-space key map.** The single disposable candidate lowered mean ACK-call time in every one of the 18 paired comparisons, with aggregate reductions of 31.6–73.5% across the six cells. No production change is merged with this receipt. These are local synthetic diagnostics, not deployment utilization, native qualification, activation requirements or product SLOs.

The [accepted brief](https://github.com/the-sarge/quic-go-fast/issues/618#issuecomment-5820598213), [PTO addendum](https://github.com/the-sarge/quic-go-fast/issues/618#issuecomment-5823109493), [current contract](contract.md) and [verbatim acceptance criteria](acceptance.md) define the finite boundary. Baseline source is `f47e09e3a5b4f9f8efc5de0ea73a01307d3a01f4`. The source already includes the PTO confirmation pass from #622.

## Corrected paired results

Each entry lists the three sample means in microseconds; each sample accumulates at least two seconds of timed `sentPacketHandler.ReceivedAck` calls. Reductions use operation-weighted means. Exactly one packet is newly acknowledged per call in both range shapes. All 18 pairs completed; sample order alternates baseline/candidate, candidate/baseline, baseline/candidate within each cell.

| Retained outcomes | ACK ranges | Baseline means (µs) | Candidate means (µs) | Mean reduction | Candidate p99 (µs; 9,999 individual calls) |
| ---: | ---: | --- | --- | ---: | ---: |
| 1,024 | 1 | 3.77 / 3.58 / 3.88 | 2.37 / 2.38 / 3.02 | 31.6% | 14.75 |
| 1,024 | 32 | 6.85 / 6.83 / 6.85 | 2.56 / 2.82 / 2.37 | 62.4% | 4.25 |
| 8,192 | 1 | 24.58 / 26.69 / 25.33 | 12.64 / 12.65 / 12.85 | 50.2% | 17.54 |
| 8,192 | 32 | 45.99 / 46.50 / 48.21 | 12.84 / 12.73 / 12.51 | 72.9% | 20.33 |
| 32,768 | 1 | 103.73 / 103.26 / 98.94 | 46.96 / 48.30 / 47.88 | 53.2% | 61.96 |
| 32,768 | 32 | 174.90 / 179.44 / 184.48 | 47.56 / 48.07 / 46.97 | 73.5% | 58.67 |

The 8,192- and 32,768-outcome baselines repeatedly exceed the 10 µs mean diagnostic trigger. Their candidate means still exceed 10 µs: the candidate removes the dominant narrow-ACK lookup cost, not the other ring passes. Every paired candidate mean is lower than its baseline, and even each cell’s slowest candidate mean is lower than its fastest baseline mean. This is stronger than the observed sample-to-sample variation for the measured means, without claiming a confidence interval or performance outside these six traces.

The 1,024/contiguous candidate has a tail caveat: per-sample p99 values are 3.834, 3.333 and 28.916 µs, producing the pooled 14.750 µs value above. That third-sample excursion is not repeated in the other two samples, but its cause is not established. Do not claim a uniform tail improvement. The shared host was not quiet or CPU-pinned; no retry was used to seek cleaner numbers. Baseline tail data below comes from the earlier diagnostic capture rather than paired tail collection, so it is not a controlled before/after p99 comparison.

## Original diagnostic baseline and allocation receipts

The initial run completed all six cells, three samples each, in 140.975 seconds including fixture setup, warm-up and the baseline profile. A later `go vet` check found that its snapshot helper copied structs containing atomic counters. The original source, output and warning are preserved unchanged. The fixture used one worker with private statistics, but copying atomic-bearing structs is not an acceptable maintained pattern. The paired runs above use a corrected helper with atomic loads/stores in both binaries; those corrected means own the recommendation. The initial timings and profile remain explicitly diagnostic evidence rather than a clean standalone certification.

| Outcomes | Ranges | Initial mean (µs) | Initial p99 (µs; 9,999 calls) | Initial sample means (µs) |
| ---: | ---: | ---: | ---: | --- |
| 1,024 | 1 | 3.51 | 5.00 | 3.49 / 3.48 / 3.57 |
| 1,024 | 32 | 6.81 | 8.75 | 7.05 / 6.65 / 6.74 |
| 8,192 | 1 | 25.28 | 42.58 | 25.54 / 24.97 / 25.34 |
| 8,192 | 32 | 46.15 | 58.04 | 46.08 / 46.32 / 46.06 |
| 32,768 | 1 | 100.77 | 118.17 | 100.52 / 100.32 / 101.48 |
| 32,768 | 32 | 181.67 | 219.50 | 180.91 / 180.52 / 183.61 |

Allocation deltas cover only the ACK call; fixture construction, reset, assertions and output are excluded. Reported nonzero values are rare single 32-byte allocation events, not a per-ACK allocation on every operation. They are retained rather than rounded into an assertion of allocation freedom.

| Outcomes | Ranges | Paired baseline allocations/ACK | Paired baseline bytes/ACK | Candidate allocations/ACK | Candidate bytes/ACK |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1,024 | 1 | 0.00000125 | 0.00003988 | 0.00000043 | 0.00001364 |
| 1,024 | 32 | 0.00000228 | 0.00007297 | 0.00000000 | 0.00000000 |
| 8,192 | 1 | 0.00000850 | 0.00027203 | 0.00000636 | 0.00020340 |
| 8,192 | 32 | 0.00001563 | 0.00050003 | 0.00000423 | 0.00013540 |
| 32,768 | 1 | 0.00003398 | 0.00108722 | 0.00000000 | 0.00000000 |
| 32,768 | 32 | 0.00000000 | 0.00000000 | 0.00001584 | 0.00050697 |

Both implementations’ one timer-overhead check reported p99 42 ns (the initial median and minimum were zero at this host’s timer resolution). No overhead subtraction was applied. ACK means are many timer ticks, but this does not remove scheduling, GC or instrumentation effects. Raw operation totals, sample durations, allocation counts, bytes, run variation and individual tail values are retained in the JSON/CSV files.

## Trace validity and attribution

The trace registers `N` ordinary 1-RTT 40-byte ack-eliciting PING packets through the actual handler. It ACKs `0..N-2` in batches of at most 32 before registering packet `N-1`, so occupancy is retained history, not tens of thousands of illegally outstanding packets. The contiguous ACK covers `[N-32,N-1]`; the fragmented ACK contains singleton packet numbers `floor(j*(N-1)/31)` for `j=0..31`. Both newly acknowledge only `N-1`. ACK bytes are decoded by the existing wire parser before use. ECN validation and the private ECN ledger remain disabled. This deliberately samples ordinary no-loss history, not a loss storm or every history-state mixture.

Each timed call starts from a snapshot of that legitimately reached state. Checks outside timing confirm the exact outcome count, one fresh acknowledged packet, a real 1-RTT acknowledgment, RTT update, zero loss and empty flight afterward. The outcome history is verified before capture. The positive PTO (11 ms), nonempty receipt witness and ACK feedback ensure ACK classification, PTO confirmation and persistent-span evaluation execute. Source inspection and the baseline profile confirm all three passes; the workload does not use the drained duplicate-ACK shortcut. A bounded sink consumes event values without accumulating slices. Reno processing remains in the timed boundary.

The baseline’s sole 10-second profile uses the highest-mean cell, 32,768 outcomes / 32 ranges. Of 7.94 seconds of samples under the ACK label, 5.68 seconds (71.5%) are cumulative in `recoveryEvidence.ack`, 1.30 seconds (16.4%) in `confirmPTO`, and 0.95 seconds (12.0%) in `feedback`. ACK range search is visibly inside the lookup cost. The candidate’s sole profile uses its highest-mean cell, 32,768 / one range: 5.91 seconds under the ACK label, with 0.08 seconds in lookup, 3.39 seconds in PTO confirmation and 2.41 seconds in feedback. These are different selected cells, not a matched profile-speedup experiment. They explain the remaining candidate cost. Pprof’s printed percentages use the whole-profile denominator; the percentages just stated use the filtered ACK total.

Snapshot restoration copies the outcome ring immediately before timing and therefore warms it. `runtime.ReadMemStats` surrounds each call outside elapsed timing; reset, memory-stat reads and GC can still influence subsequent calls. The same corrected setup is used in each pair, but this does not reproduce deployed cache state or ACK arrival patterns. Host contention is recorded in [host-before.json](host-before.json) and [host-after.json](host-after.json): Apple M4 Max, 16 logical CPUs, 128 GiB RAM, macOS 27.0 build 26A428, Go 1.27.1 darwin/arm64, one connection worker and `GOMAXPROCS=1`, without affinity or power-state control. Neither elapsed time nor these profiles establishes exact deployed CPU service time. Ten microseconds at a hypothetical 10,000 ACKs/s is arithmetically 10% of one core only if those microseconds are CPU service time; this investigation does not establish that workload.

## Disposable candidate and preservation

The [single candidate patch](candidate.patch.gz) changes only `recoveryEvidence.ack`: count decoded ACK cardinality against a count-sized budget; when it fits, look up at most `count` packet numbers through the existing space-qualified `keys` map; otherwise use the original bounded ring scan. The same receipt/state update preserves disposed outcomes, receipt eligibility, duplicate witnesses and maximum acknowledged ordinal. No extra retained memory, index entries, registration/update work, expiry owner or reset/disposal path is introduced. The pre-existing map and its bounded eviction remain authoritative. The patch does not change PTO confirmation, persistent-span reduction, the 32,768 cap, Reno, public API, wire behavior, dependencies or ownership.

The candidate passed the full ackhandler and congestion suites, including the eight T5 functions: `TestBBRPersistentCongestionAcrossSpaces`, `TestBBRPersistentCongestionMeasuredAtSend`, `TestBBRPersistentCongestionAckOnlyBreak`, `TestBBRPersistentCongestionGapAndEviction`, `TestBBRPersistentCongestionDeduplication`, `TestBBRRecoveryEpisodeAllSpurious`, `TestBBRRecoveryEpisodeSupersededOrMissing`, and `TestBBRPTODoesNotFabricateLoss`. The corrected candidate binary also passed the complete ackhandler suite. Existing eviction, disposal, reset, spurious-loss, current-path receipt and cross-space cases remain the correctness evidence; no new regression was needed for this disposable comparison. Corrected scratch code passed `go vet`; module tidiness passed. Logs retain the earlier scratch-only vet failure as well as the successful corrected check.

A production proposal should start from this measured bounded lookup, with a fresh accepted contract and normal review. Do not simply land the scratch patch. These six traces each enumerate 32 packet numbers; they do not measure dense ACKs near the lookup cutoff, wider-range fallback performance, adversarial distributions, loss-heavy mixtures, other platforms or real traffic. In particular, `count` map lookups may be slower than a sequential ring scan for dense ACKs; the fixed cutoff was not tuned. The remaining two scans, and any more ambitious classified-prefix design, were not optimized. The current evidence supports separately scoping the demonstrated narrow-ACK improvement, not claiming that this candidate is a universally better production policy.

## Budget, artifacts and completion boundary

There were exactly six initial cells, three initial samples per cell, one candidate, three alternating pairs per cell, at most 9,999 tail durations per cell per implementation, one timer check per implementation and one 10-second CPU profile per implementation. No timing retry, second candidate, new host, extra timing cell, native campaign or threshold change occurred. Initial capture execution was 140.975 seconds; the full paired driver including candidate profiling was 372.385 seconds, for 513.360 seconds total recorded execution. Even the deliberately conservative interval from investigation start at 22:24:21 UTC through the final profile ending around 22:43:46 UTC is under 20 minutes, below the 30-minute measurement cap. Report preparation finished on 2026-09-24, well before the four-hour deadline at 02:24:21 UTC on September 25.

The [reproduction recipe](reproduction.md) documents exact boundaries and commands. [measurements.jsonl](measurements.jsonl), [summary.json](summary.json), [comparison-summary.json](comparison-summary.json), [comparison-execution.json](comparison-execution.json), individual pair JSON/logs and the exact CSV bytes in [tails.tar.gz](tails.tar.gz) retain raw and derived results (24 CSV members, individually hashed in `tails.sha256`). [cpu.pprof](cpu.pprof), [candidate-cpu.pprof](candidate-cpu.pprof) and their text summaries retain bounded attribution. Scratch source versions and build identities are retained with hashes in `SHA256SUMS`; temporary Go files, binaries and production edits are removed before commit. The evidence is frozen, outside the Go build and the parent module’s dependency payload; it is not installed as a maintained harness or CI timing gate. Review, exact-head certification and merge receipts belong to the PR discussion rather than this normative report.
