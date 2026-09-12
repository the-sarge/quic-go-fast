# D1 macOS sendmsg_x batch send adoption results

Results for the precommitted [D1 protocol](2026-09-12-d1-sendmsgx-protocol.md) (Slice D1, Track D, program QGF-DP-2026-09). Raw per-invocation JSON lines: [collection data](2026-09-12-d1-sendmsgx-collection.jsonl).

## Disposition: PASS — adopt

Every predeclared bound is met by the mechanical rule. The batch path adopts; the recorded allowlist entry (Darwin kernel major 25 ↔ macOS product 26.6.2) is the tested version floor.

## Configuration as run

- **Candidate head:** `1af9bb81` (harness commit on the D1 branch; the shipped code under test is unchanged since `4649c463`). Base `5c0a459c`.
- **Host, toolchain, workload:** exactly as fixed in the protocol — Mac16,5 (M4 Max, Darwin 25.6.0 arm64, macOS 26.6.2), Go 1.27.0 darwin/arm64, one binary (`go test -c -tags d1bench`), 256 MiB of 1071-byte records per invocation, server on a dual-stack wildcard socket sending to a v4-mapped loopback destination.
- **Collection:** 11 rounds × 3 cells, round 0 discarded, cell order rotated per round, one fresh process per invocation, no concurrent builds or agent tasks started. Load averages 6.23 (1-min, before) → 8.28 (after); the rise is dominated by the collection itself on this 16-core host, the paired per-round design absorbs shared drift, and no round shows an outlier consistent with material contamination (engaged syscalls-per-packet spread is 0.1252–0.1282).
- **Syscall counting:** the counter identity of record (one counter increment per raw syscall, unit-asserted). `dtrace` was not used (SIP unchanged on this host).

## Primary metric — send syscalls per packet (engaged vs disabled, paired)

Geometric-mean ratio **0.1262**, fixed-seed 10,000-resample bootstrap 95 % interval **[0.1256, 0.1270]**. Pass bound: upper ≤ 0.75. **Pass** — an ~8× reduction in send syscalls per packet (median engaged cell: 24,601 send syscalls for 195,836 packets, 0.1256 syscalls/packet; disabled is identically 1.0 by construction of the per-packet path).

## Engagement

Mean packets per batch submission **7.985** (min round 7.972; the cap is `maxSendBatch` = 8); batched-packet fraction mean **0.9988** (min 0.9968). Pass bounds: ≥ 2.0 and ≥ 25 %. **Pass** — the bulk sender keeps the queue full, so nearly every packet travels in a full batch.

## Throughput noninferiority

Engaged/disabled per-round ratio geometric mean **1.1733**, 95 % interval **[1.1371, 1.2121]**. Pass bound: lower ≥ 0.95. **Pass** — the engaged cell is ~17 % faster on this workload (engaged 173.9–196.6 MB/s, disabled 153.5–167.4 MB/s), a throughput improvement, not merely noninferiority. Loopback microbenchmark throughput is not application throughput.

## Memory

Engaged median peak RSS **25.2 MiB** vs disabled **25.4 MiB** (delta −0.2 MiB; budget +8 MiB). **Pass.** Engaged-cell median mallocs 2,775,078 vs disabled 2,882,971; GC cycles comparable (median 45).

## Preservation and qualification cells

- **disabled** (`QUIC_GO_DISABLE_SENDMSG_X=1`): zero batch submissions and nonzero fallback packets in all 10 rounds; transfers complete correctly. **Pass.**
- **unqualified** (injected unlisted kernel major): zero batch submissions, nonzero fallback packets, all rounds; throughput median 163.4 MB/s, indistinguishable from disabled. **Pass** — every unlisted entry falls back.
- **listed entry engages:** the engaged cell (Darwin 25, allowlisted, self-check passed in-process every invocation) batched in all 10 rounds. **Pass.**
- The opt-out tag and iOS configurations are compile-time absence, verified by the cross-compile gates (`cross-compile.sh` ios library builds, opt-out tag builds) and by the ordinary suite under `-tags quic_go_no_private_syscalls`; behavioral identity of the capability-off path is owned by the unit suites (`TestSendmsgXDisabledPathInert`, send-queue closure tests).

## Termination

The fixed collection ran once; this document publishes the disposition and the investigation stops. No additional platforms, repetitions, or workloads were run or are requested.
