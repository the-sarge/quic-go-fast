# G2 Linux UDP_GRO adoption results

Results for the [G2 GRO adoption protocol](2026-09-11-g2-gro-protocol.md), collected 2026-09-11 on minimax per the predeclared collection procedure, with the deviations recorded below. **Disposition: pass** (all four mechanical gates; no fail condition).

## Provenance

- **Candidate measured:** `7306e2c6719c4bf9f0638747b2afaa78b7dea707` (the G2 PR head containing the protocol; product code identical to the reviewed candidate).
- **Base:** `71a1ae80` (default-branch HEAD at branch creation).
- **Host/toolchain:** minimax, AMD Ryzen AI MAX+ 395, Linux 7.0.0-30-generic x86_64, Go 1.27.1, amd-pstate powersave unchanged. Affinity `taskset -c 12,13,28,29`, `GOMAXPROCS=4`. Load average 0.17 (1-min) at precheck, 0.92 immediately after collection (the collection itself is the dominant contributor); no competing workload was started.
- **Collection:** 11 rounds × 4 cells, one fresh process per invocation, cell order rotated per round, 512 MiB per transfer; round 0 discarded as warmup, rounds 1–10 analyzed. Raw JSON lines are reproducible from the committed harness (`go test -c -tags grobench`, invocation per the protocol).

## Medians (rounds 1–10)

| Cell | Throughput | Receive syscalls/datagram | Engagement (fraction of datagrams coalesced) | Peak RSS | Go allocs / GCs per run |
|---|---|---|---|---|---|
| engaged | 1354.7 MB/s | 0.0361 | 1.000 | 21,766 KiB | 128.5 MB / 54 |
| available-but-unexercised | 534.4 MB/s | 0.4400 | 0.000 | 19,324 KiB | 103.5 MB / 67 |
| disabled | 1079.7 MB/s | 0.1373 | 0.000 | 20,050 KiB | 49.7 MB / 27 |
| unavailable | 1069.5 MB/s | 0.1374 | 0.000 | 19,536 KiB | 51.6 MB / 34 |

Aggregate engaged-cell coalescing distribution (segments per kernel message, rounds 1–10): dominated by 11-segment (149,522 messages) and 14-segment (155,864) reads; single-segment messages are 1,443 of ~316k. Every cell asserted its predeclared GRO/GSO capability state before measuring.

## Predeclared gates

- **Primary — receive syscalls per delivered datagram, engaged/disabled:** geometric-mean paired ratio **0.263**, 95 % bootstrap interval **[0.2595, 0.2663]** (fixed seed, 10,000 resamples). Gate: upper bound ≤ 0.75 → **pass** (a 73.7 % reduction; the disabled path already amortizes ~7 datagrams per `recvmmsg`, and GRO reaches ~28).
- **Engagement:** median 100 % of delivered datagrams arrived in coalesced messages with peer GSO on → **pass** (> 50 %).
- **Throughput noninferiority, engaged/disabled:** ratio **1.259**, 95 % interval **[1.245, 1.272]**. Gate: lower bound ≥ 0.95 → **pass** (a 25.9 % improvement, not merely noninferiority).
- **Memory:** engaged median peak RSS exceeds disabled by **1,716 KiB**, within the declared 16 MiB budget (512 KiB of it is the predeclared prepost growth) → **pass**. Context metrics: engaged runs allocate more (128.5 MB vs 49.7 MB total; 54 vs 27 GCs) from retention copies and per-read slab/view allocation; the throughput gate shows the net effect remains strongly positive. Retained-bytes pinning stays constant-bounded (`MaxConnRetainedCoalescedBytes` ≈ 2 MiB per owner).

**Mechanical disposition: pass** (all four gate conditions met; no fail condition triggered).

## Deviations from the predeclared protocol and binding plan (recorded 2026-09-11, review round 1)

1. **Unmeasured memory dimensions.** The [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md) names third-tier pool misses, queued bytes, and post-close retention among the memory dimensions each slice's protocol measures. This protocol and harness measured allocations, GC cycles, heap in use, and peak RSS only; **third-tier pool misses, queued bytes, and post-close retention were not measured**. They are bounded by tests and constants rather than measurements: exactly-once slab recycling and retention-copy behavior by the race-enabled closure tests, queued-byte pinning by the count bounds and per-owner `MaxConnRetainedCoalescedBytes` budgets, and post-close release by `releaseReadBuffers` coverage. Tests and constant bounds are not measurements; the omission is a protocol-authoring gap recorded here, not a claim of coverage. The four mechanical adoption gates (syscalls per datagram, engagement, throughput, RSS budget) did not depend on the unmeasured dimensions.
2. **Failed predeclared context check.** The protocol's unexercised-vs-disabled 10 % context bound failed (0.495×); see the next section. The original protocol text is preserved unchanged; the explanatory diagnostic below is post-collection, non-predeclared support, not a retroactive replacement of the predeclared comparator, and the mechanical gate results are unaffected.

## Unexercised-cell note and diagnostic (deviation 2 detail)

The protocol's context check asked the unexercised cell to sit within 10 % of the disabled cell's median. Observed: 534.4 vs 1079.7 MB/s (0.495×). That predeclared comparator was mis-specified: the two cells differ in the **sender's** GSO state, so the comparison measures peer send capability, not the GRO receive path. A three-run post-collection diagnostic (reported as such, outside the fixed collection statistics) with GSO **and** GRO both disabled measured 537.1–540.9 MB/s and 0.422–0.433 syscalls/datagram — statistically indistinguishable from the unexercised cell (534.4 MB/s, 0.440). The GRO-on receive path therefore shows no material regression when the peer never coalesces (≲ 1 % throughput, ~3 % syscalls/datagram against its like-for-like baseline); the 2× spread is the known cost of per-packet sends without GSO, present at base. This does not affect the mechanical disposition, whose gates compare cells with identical sender state.

## Syscall-count validation

One engaged-cell validation run (128 MiB) under `strace -f -c -e trace=recvmmsg`: strace observed 9,236 `recvmmsg` calls process-wide (2,583 returning errors — deadline/EAGAIN wakeups) while the harness counted 4,677 successful client-socket reads; the remainder (~1,976 successful calls) is the server socket receiving ACKs plus errored wakeups on both sockets. The harness's 1 ReadBatch = 1 `recvmmsg` accounting is consistent with the kernel-observed totals.

## Outcome

The engaged offload reduces receive syscalls per delivered datagram by ~74 % and raises loopback bulk throughput by ~26 % with memory inside budget; disabled and unavailable configurations are unchanged within noise of each other and preserve behavior (capability assertions plus the unit/race suites). Per the protocol's termination rule, this is the single fixed collection; adoption gates for Slice G2 are met.
