# G2 Linux UDP_GRO adoption protocol

Precommitted measurement protocol for Slice G2 of the [Linux GRO plan](../adr/2026-09-11-linux-gro-plan.md) (Track G, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the G2 implementation branch before any collection run; the results document records the exact candidate head measured.

## Question

Does enabling UDP_GRO on transport-owned Linux sockets reduce receive syscalls per delivered datagram in a bulk QUIC transfer, with noninferior throughput and bounded memory, while the disabled and unavailable configurations preserve today's behavior?

## Artifact classes

The measurement harness (`gro_measurement_test.go`, build tag `grobench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the G2 probe, split, kill-switch, and retention-activation code.

## Fixed configuration

- **Base commit:** `71a1ae8` (default-branch HEAD at branch creation; no GRO use).
- **Candidate:** the G2 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree, so base-vs-candidate comparison is expressed through the candidate's own disabled/unavailable cells, which execute the preserved (base-identical) receive path; unit suites separately assert byte-identical behavior with GRO off.
- **Host:** minimax — AMD Ryzen AI MAX+ 395, 16 cores / 32 threads, Linux `7.0.0-30-generic` x86_64 (Ubuntu), amd-pstate powersave governor unchanged and recorded.
- **Toolchain:** Go 1.27.1 linux/amd64; identical flags for every cell; binary built once per collection with `go test -c -tags grobench`.
- **CPU affinity:** `taskset -c 12,13,28,29` (two distinct physical cores and their SMT siblings, found idle at precheck), `GOMAXPROCS=4`. Affinity is not exclusive reservation; host load is recorded before and after collection and material contamination is reported.
- **Workload:** one QUIC connection over IPv4 loopback between two transports in one process; the server bulk-sends 512 MiB of 1071-byte application records on one unidirectional stream, generating records in 32-record batches so the sender outpaces the packetizer the way a bulk sender does and the peer's GSO path forms real segment bursts; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The externally owned application sweep stays out of scope.
- **Syscall counting:** the harness wraps the receive socket's `ReadBatch` (one call is one `recvmmsg`) and counts every `ReadPacket()` return (one delivered datagram); GRO coalescing is read per kernel message from the `UDP_GRO` control message. One validation run of the engaged cell is cross-checked against `strace -f -c -e trace=recvmmsg` and recorded in the results.
- **Peer GSO state:** fixed per cell by `QUIC_GO_DISABLE_GSO`, asserted and reported by the harness (`gso_cap`).

## Cells

| Cell | Receive socket | GRO | Peer GSO |
|---|---|---|---|
| engaged | transport-owned | on (probed) | on |
| available-but-unexercised | transport-owned | on (probed) | off (`QUIC_GO_DISABLE_GSO=1`) |
| disabled | transport-owned | off (`QUIC_GO_DISABLE_GRO=1`) | on |
| unavailable | caller-supplied (never probed) | off by ownership rule | on |

The harness asserts each cell's GRO capability matches this table before measuring; the unavailable cell also demonstrates the caller-owned-socket rule operationally (no `UDP_GRO` setsockopt, asserted by unit test `TestGRONotEnabledOnCallerSuppliedSocket`).

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all four cells, one fresh process per cell invocation, with cell order rotated by round. No concurrent builds or agent tasks are started on the selected CPUs during collection. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits: wall time and throughput; receive syscalls; delivered datagrams; syscalls per delivered datagram; the distribution of coalesced segments per kernel message; coalesced messages and datagrams; Go allocation totals, GC count, heap in use; and peak RSS (`VmHWM`).

## Metrics and predeclared bounds

- **Primary — receive syscalls per delivered datagram**, engaged vs disabled, paired per round. Minimum useful effect: geometric-mean ratio ≤ 0.75 (≥ 25 % fewer receive syscalls per datagram). A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds) gives a 95 % interval; pass requires the interval's upper bound ≤ 0.75. The bootstrap describes this sample set, not a universal timing guarantee.
- **Engagement:** fraction of delivered datagrams arriving in coalesced (> 1 segment) messages in the engaged cell. Report the per-message distribution. Zero engagement across GSO-enabled cells is the plan's approach-evidence failure.
- **Throughput noninferiority:** engaged vs disabled per-round ratio; the 95 % bootstrap interval's lower bound must be ≥ 0.95 (no more than 5 % regression). The unexercised cell is reported for context and must show no material regression against disabled (medians within 10 %, reflecting its different sender path).
- **Memory budget:** engaged-cell median peak RSS ≤ disabled-cell median peak RSS + 16 MiB (declared allowance for the 8 × 64 KiB ≈ 512 KiB per-socket prepost growth plus coalesced-tier pool residency). Per-connection retention pinning is bounded by constants: `MaxConnRetainedCoalescedBytes` ≈ 2 MiB per connection and per transport/server owner. Go allocation counts and GC cycles are reported against the disabled cell for context.
- **Preservation:** disabled and unavailable cells must report `gro_cap=false` and complete correctly; behavioral identity of the GRO-off receive path is owned by the unit and race suites, not by timing.

## Disposition rule (mechanical)

- **Pass:** primary interval upper bound ≤ 0.75, engaged-cell engagement > 50 % of datagrams, throughput interval lower bound ≥ 0.95, memory within budget.
- **Fail:** primary point ratio > 0.90, or engagement < 5 % in every GSO-enabled cell, or throughput point ratio < 0.90, or memory budget exceeded with no in-budget mitigation.
- **Inconclusive:** anything else, including material host contamination; an inconclusive result is reported as such and does not adopt.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
