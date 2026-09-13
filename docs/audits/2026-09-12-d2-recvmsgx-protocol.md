# D2 macOS recvmsg_x receive-batching adoption protocol

Precommitted measurement protocol for Slice D2 of the [Darwin batch plan](../adr/2026-09-11-darwin-batch-plan.md) (Track D, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the D2 implementation branch before any collection run; the results document records the exact candidate head measured. D2 is a bounded experiment with pre-accepted dispositions: **adopt** (merge the PR) only on a Pass; **retire** (close the PR unmerged, zero residue in the tree) on Fail or Inconclusive.

## Question

Does batching UDP receives through the private `recvmsg_x` syscall on a qualified Darwin kernel reduce receive syscalls per delivered datagram at a bulk-transfer receiver, with noninferior throughput and bounded memory, while the disabled and unqualified configurations preserve today's single-datagram read behavior — and does the fail-closed qualification behave as designed (every listed kernel major engages, every unlisted major falls back)?

## Artifact classes

The measurement harness (`d2_measurement_test.go`, build tag `d2bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test — shipped only if adopted — is the D2 batch receive path, qualification (shared allowlist + receive-shape self-check), `QUIC_GO_DISABLE_RECVMSG_X` kill switch, latch, and counters.

## Declared batch size and memory budget

The declared receive batch size is **8** (matching the Linux receive batch). Engaged per-connection receive memory rises from one in-flight pool buffer to at most 8: 8 × 1452 B payload buffers + 8 × 128 B control buffers + the reusable msghdr/iovec/sockaddr scratch (≈ 800 B), ≈ **12.6 KiB per connection** versus ≈ 1.6 KiB with the capability off. The capability-off path pulls exactly one pool buffer per read (asserted by unit test). Process-level budget: engaged-cell median peak RSS ≤ disabled-cell median peak RSS + 8 MiB.

## Fixed configuration

- **Base commit:** `81639180` (default-branch HEAD at branch creation — the D1-era baseline: `sendmsg_x` batch send merged, receive path single-datagram).
- **Candidate:** the D2 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's own disabled/unqualified cells, which execute the preserved single-datagram receive path, and unit suites separately assert the capability-off read path is identical.
- **Host:** Mac16,5 (Apple M4 Max, 16 cores, 128 GiB), macOS product 26.6.2 (build 25G83), **Darwin kernel 25.6.0 arm64** — the qualified host for allowlist entry Darwin 25 (shared with D1).
- **Toolchain:** Go toolchain recorded in the results document; identical flags for every cell; one binary built per collection with `go test -c -tags d2bench`.
- **CPU affinity:** macOS exposes no user-space CPU pinning; the policy is: no concurrent builds or agent tasks started during collection, load averages recorded before and after collection, and material contamination reported. The paired per-round design absorbs shared drift.
- **Workload:** one QUIC connection between two transports in one process; the **client is the measured receiver**. Both endpoints bind dual-stack wildcard sockets (`net.ListenUDP("udp", nil)`, the production shape whose v4-mapped source decoding the receive self-check qualifies); the client dials `127.0.0.1`, so every measured receive carries a v4-mapped source. The server bulk-sends 256 MiB of 1071-byte application records on one unidirectional stream (32-record generation batches); the client reads to completion. Flow-control windows raised (16/24 MiB max). The D1 send-batching capability is engaged and asserted identical in **every** cell, so the receive path is the only varying factor. The externally owned application sweep stays out of scope.
- **Syscall counting:** per-connection counters at the measured receiver's `recvmsg_x` wrapper seam. Engaged-path receive syscalls = delivering `recvmsg_x` calls + parked EAGAIN attempts + delegated fallback reads; each delegated fallback read counts as one syscall, a **floor**, because the runtime poller's internal retries are invisible at this seam. The asymmetry (engaged attempts fully visible, fallback attempts undercounted) biases **against** the candidate and is accepted. Delivered datagrams = batched datagrams + fallback reads. The counter identity (one increment per visible call, structural bounds checked per call) is asserted by unit tests and is the counting method of record.

## Cells

| Cell | Receive-batch capability | Mechanism |
|---|---|---|
| engaged | on | qualified host (Darwin 25 listed), receive self-check passes |
| disabled | off | `QUIC_GO_DISABLE_RECVMSG_X=1` kill switch |
| unqualified | off | injected unlisted kernel major (test hook `recvmsgXKernelMajor`), self-check never runs |

The harness asserts each cell's capability state before measuring: the engaged cell requires `recvmsgXAvailable()` true after synchronous qualification; disabled and unqualified require it false with zero batch reads and nonzero fallback reads after transfer. The unqualified cell is the protocol's "every unlisted entry falls back" assertion; the engaged cell is "every listed entry engages". An available-but-unexercised cell does not apply to the receive side (an engaged receive capability is exercised by any inbound bulk traffic); the opt-out-tag configuration is compile-time absence, verified by the cross-build gates and by running the ordinary test suite under `-tags quic_go_no_private_syscalls`, not by this harness (whose file requires the active tag).

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all three cells, one fresh process per cell invocation, with cell order rotated by round. No concurrent builds or agent tasks are started during collection. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: wall time and throughput; batch reads, batched datagrams, EAGAIN waits, fallback reads; receive syscalls; delivered datagrams; receive syscalls per delivered datagram; datagrams per batch read; batched fraction; Go allocation totals, GC count, heap in use; peak RSS (`ru_maxrss`); and the capability state.

## Metrics and predeclared bounds

- **Primary — receive syscalls per delivered datagram** at the measured receiver, engaged vs disabled, paired per round. Minimum useful effect: geometric-mean ratio ≤ 0.75 (≥ 25 % fewer receive syscalls per datagram). A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds) gives a 95 % interval; pass requires the interval's upper bound ≤ 0.75. The bootstrap describes this sample set, not a universal timing guarantee.
- **Engagement:** datagrams per delivering batch read (the plan's engagement metric) and the fraction of delivered datagrams that arrived in batch reads, both in the engaged cell. Pass requires mean datagrams per batch read ≥ 2.0 and batched fraction ≥ 25 %. Zero engagement across cells is approach evidence for retirement.
- **Throughput noninferiority:** engaged vs disabled per-round ratio; the 95 % bootstrap interval's lower bound must be ≥ 0.95 (no more than 5 % regression).
- **Memory budget:** engaged-cell median peak RSS ≤ disabled-cell median peak RSS + 8 MiB. Allocation counts and GC cycles reported for context.
- **Preservation/qualification:** disabled and unqualified cells must report zero batch reads, nonzero fallback reads, and complete correctly; behavioral identity of the capability-off read path is owned by the unit suites, not by timing.

## Disposition rule (mechanical)

- **Pass → adopt (merge the PR):** primary interval upper bound ≤ 0.75, engaged-cell engagement bounds met, throughput interval lower bound ≥ 0.95, memory within budget, preservation/qualification cells as specified.
- **Fail → retire (close the PR unmerged):** primary point ratio > 0.90, or batched fraction < 5 %, or throughput point ratio < 0.90, or memory budget exceeded with no in-budget mitigation, or a preservation/qualification cell asserting wrongly.
- **Inconclusive → retire (close the PR unmerged):** anything else, including material host contamination. An inconclusive result is reported as such and does not adopt; retirement is the pre-accepted disposition, not an operator escalation.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract. The shared allowlist entry remains a tested version floor, never a ceiling.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. Adopt or retire per the mechanical rule; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
