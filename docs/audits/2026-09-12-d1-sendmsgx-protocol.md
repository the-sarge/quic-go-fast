# D1 macOS sendmsg_x batch send adoption protocol

Precommitted measurement protocol for Slice D1 of the [Darwin batch plan](../adr/2026-09-11-darwin-batch-plan.md) (Track D, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the D1 implementation branch before any collection run; the results document records the exact candidate head measured.

## Question

Does batching UDP sends through the private `sendmsg_x` syscall on a qualified Darwin kernel reduce send syscalls per QUIC packet in a bulk transfer, with noninferior throughput and bounded memory, while the disabled and unqualified configurations preserve today's per-packet behavior — and does the fail-closed qualification behave as designed (every listed kernel major engages, every unlisted major falls back)?

## Artifact classes

The measurement harness (`d1_measurement_test.go`, build tag `d1bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the D1 batch path, qualification (allowlist + self-check), kill switch, latch, and counters.

## Fixed configuration

- **Base commit:** `5c0a459c` (default-branch HEAD at branch creation; no `sendmsg_x` use).
- **Candidate:** the D1 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's own disabled/unqualified cells, which execute the preserved per-packet send path, and unit suites separately assert the capability-off call sequence is identical.
- **Host:** Mac16,5 (Apple M4 Max, 16 cores, 128 GiB), macOS product 26.6.2 (build 25G83), **Darwin kernel 25.6.0 arm64** — the qualifying host for allowlist entry Darwin 25. This namespace pair (product 26.x / Darwin 25.x) is exactly the mapping `qualifiedDarwinKernelMajors` records.
- **Toolchain:** Go 1.27.0 darwin/arm64; identical flags for every cell; one binary built per collection with `go test -c -tags d1bench`.
- **CPU affinity:** macOS exposes no user-space CPU pinning (no `taskset` equivalent); the policy is: no concurrent builds or agent tasks started during collection, load averages recorded before and after collection, and material contamination reported. This is a developer workstation, not an idle lab host; the paired per-round design absorbs shared drift, and contamination is judged against the recorded load.
- **Workload:** one QUIC connection between two transports in one process; the **server is the measured sender**, bound to a dual-stack wildcard socket (`net.ListenUDP("udp")`, the production shape whose v4-mapped destination encoding the self-check qualifies); the client binds `udp4` loopback and dials `127.0.0.1`, so every measured send exercises the v4-mapped `msg_name` path. The server bulk-sends 256 MiB of 1071-byte application records on one unidirectional stream, generated in 32-record batches so the sender outpaces the packetizer the way a bulk sender does; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The externally owned application sweep stays out of scope.
- **Syscall counting:** the engaged/fallback counters plus a counting wrapper at the sender's `rawConn.WritePacket` seam. On darwin every `WritePacket` call is one `sendmsg`, and every batch submission counter increment is one `sendmsg_x` syscall (the counter increments exactly once per raw syscall, bounds-checked in `sendmsgXSubmit`). Send syscalls = per-packet writes + batch submissions; packets sent = per-packet writes + batch-accepted packets. One validation run of the engaged cell is cross-checked against `dtrace`/`ktrace` only if available without SIP changes; otherwise the counter identity (one increment per raw call, asserted by unit tests) is the counting method of record.

## Cells

| Cell | Batch capability | Mechanism |
|---|---|---|
| engaged | on | qualified host (Darwin 25 listed), self-check passes |
| disabled | off | `QUIC_GO_DISABLE_SENDMSG_X=1` kill switch |
| unqualified | off | injected unlisted kernel major (test hook `sendmsgXKernelMajor`), self-check never runs |

The harness asserts each cell's capability state before measuring: the engaged cell requires `sendmsgXAvailable()` true after synchronous qualification; disabled and unqualified require it false with zero batch submissions and nonzero fallback packets after transfer. The unqualified cell is the protocol's "every unlisted entry falls back" assertion; the engaged cell is "every listed entry engages". An available-but-unexercised cell does not apply to the send side (an available capability is exercised by any bulk sender); the opt-out-tag configuration is compile-time absence, verified by the cross-build gates and by running the ordinary test suite under `-tags quic_go_no_private_syscalls`, not by this harness (whose file requires the active tag).

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all three cells, one fresh process per cell invocation, with cell order rotated by round. No concurrent builds or agent tasks are started during collection. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: wall time and throughput; per-packet sends; batch submissions and batch-accepted packets; send syscalls; packets sent; send syscalls per packet; packets per batch submission; batched-packet fraction; fallback packets; Go allocation totals, GC count, heap in use; peak RSS (`ru_maxrss`); and the capability state.

## Metrics and predeclared bounds

- **Primary — send syscalls per packet sent**, engaged vs disabled, paired per round. Minimum useful effect: geometric-mean ratio ≤ 0.75 (≥ 25 % fewer send syscalls per packet). A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds) gives a 95 % interval; pass requires the interval's upper bound ≤ 0.75. The bootstrap describes this sample set, not a universal timing guarantee.
- **Engagement:** packets per batch submission (the plan's engagement metric) and the fraction of sent packets that traveled in batch submissions, both in the engaged cell. Pass requires mean packets per accepted-work submission ≥ 2.0 and batched fraction ≥ 25 %. Zero engagement across cells is the slice's approach-evidence stop condition.
- **Throughput noninferiority:** engaged vs disabled per-round ratio; the 95 % bootstrap interval's lower bound must be ≥ 0.95 (no more than 5 % regression).
- **Memory budget:** engaged-cell median peak RSS ≤ disabled-cell median peak RSS + 8 MiB (the batch path adds only per-connection scratch: one msghdr/iovec array pair and a destination sockaddr). Allocation counts and GC cycles are reported for context.
- **Preservation/qualification:** disabled and unqualified cells must report zero batch submissions, nonzero fallback packets, and complete correctly; behavioral identity of the capability-off send path is owned by the unit suites, not by timing.

## Disposition rule (mechanical)

- **Pass:** primary interval upper bound ≤ 0.75, engaged-cell engagement bounds met, throughput interval lower bound ≥ 0.95, memory within budget, preservation/qualification cells as specified.
- **Fail:** primary point ratio > 0.90, or batched fraction < 5 %, or throughput point ratio < 0.90, or memory budget exceeded with no in-budget mitigation, or a preservation/qualification cell asserting wrongly.
- **Inconclusive:** anything else, including material host contamination; an inconclusive result is reported as such and does not adopt.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract. The recorded allowlist entry is the tested version floor, never a ceiling.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
