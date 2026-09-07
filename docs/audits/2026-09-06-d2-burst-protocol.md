# Controlled burst diagnosis

This follow-up is authorized by the user's request to isolate cores, separate pacing, compare the three burst rates, and profile separately. The [previous paced result](2026-09-06-d2-paced-results.md) supplies the symptom: lower-rate burst p99 regressed while high-rate burst delivery and p99 improved. This is diagnostic work, not a runtime adoption gate or a reopening of completed D2. Preserve the previous evidence unchanged.

## Competing explanations

1. In-process busy pacing changes receiver scheduling: moving the pacer out should materially change wakeup/tail behavior and the candidate/base pattern.
2. Reduced allocation/GC churn helps most under pressure: head-index should retain its per-delivery allocation saving, with corresponding allocation/GC profile changes at high load.
3. Head-index adds lock/compaction cost: a regression should survive external pacing on exclusive cores, with queue/mutex evidence supporting that attribution.

The prior repeated comparison is the feedback loop; a performance signal is not a functional test failure. This investigation may end by narrowing or rejecting a cause rather than changing runtime code. Do not manufacture a functional regression test or claim a causal fix without evidence.

## Isolation and controls

Temporarily create one cgroup v2 partition with exclusive access to physical cores 8, 9, and 10 and their SMT siblings 24, 25, and 26. Use a `root` partition to retain ordinary load balancing within it; pin the measured process to 8/9 and the external pacer to 10. Leave the siblings unused by experimental processes. Verify the valid partition and exclusion from the parent effective CPU mask before every sample. Monitor all logical CPUs. This prevents other user processes/VMs from scheduling on the reserved CPUs; it does not promise elimination of interrupts, shared caches, or platform power effects. Restore the partition in a finally block and verify the CPUs are returned after collection. No governor, service, IRQ affinity, boot configuration, or persistent host setting changes are needed. The kernel's [cpuset partition documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html#cpuset) defines the exclusivity mechanism.

Keep two producer modes in the common harness: the existing absolute-deadline busy pacer inside the measured process, and an external busy pacer on core 10. Both use the same queue consumer, payload checks, timestamp/histogram instrumentation, queue warmup, GC start, and burst emission. The external pacer sends one 32-byte Unix datagram tick per burst; payload creation stays in the queue process. The measured producer blocks for ticks instead of spinning. This adds local IPC overhead and changes the producer's readiness; it is a scheduling intervention, not a pure CPU-cost subtraction or a real QUIC network transfer.

The external pacer is open-loop and skips missed deadline slots. Nonblocking IPC rejects are counted separately. The receiver expires ticks that arrive at least one burst interval late, preventing backlog from turning into catch-up bursts. Report all three kinds of missed offer separately from queue drops. Cross-process scheduling uses CLOCK_MONOTONIC; queue latency retains the original Go monotonic clock and starts immediately before the enqueue attempt, after tick receipt. End-marker and sequence checks verify all tick accounting. The external mode's process CPU excludes the pacer but includes IPC and common instrumentation.

## Fixed evidence budget

Test only bursts of 32 at 10,000, 100,000, and 500,000 datagrams/sec, with 1,071-byte records and no programmed receiver pauses. Use native Go 1.26.0, `GOMAXPROCS=2` for the queue process and 1 for the external pacer, and precompiled identical harnesses differing only in `datagram_queue.go`. Before collection, run native focused tests and short internal/external race smokes on both variants. Fix harness defects before freezing source and hashes.

Timing: 20 interleaved pairs for each rate and pacing mode, two seconds per sample, 240 samples total. Alternate variant order each round and rotate the six rate/mode cases. Preserve every sample. Report qualification against 98% of requested offered rate, all generator/IPC/expiry misses, queue loss, p50/p99 latency, bytes/allocations per delivery, GC, and measured-process CPU. Use the previous seeded 10,000-resample paired bootstrap method for exploratory intervals. Compare variants within each mode, then inspect how the pattern changes across modes. The old shared-host run is historical context, not a contemporaneous control that isolates host contention.

Profiles: after timing collection, run all 12 rate/mode/variant combinations in three separate passes: CPU profiles at four seconds, block/mutex profiles at two seconds with full event sampling, and execution traces at two seconds. Keep these 36 diagnostic invocations out of timing analysis; instrumentation can distort rates and latencies. Derive scheduler, synchronization, syscall, and network delay profiles from traces, and preserve their raw traces and profile files. Use CPU attribution, mutex stacks, and scheduler profiles together; a lower total wait profile can simply reflect fewer delivered messages. No additional parameter sweep, profile-driven runtime edit, or timing rerun is implied.

The collector must stop on process/accounting/isolation failure and restore the reservation. No result-based exclusions or sample extensions. If a cause remains unresolved, state which explanation the evidence supports or weakens and what is still unmeasured. Any successful isolation claim requires both recorded partition receipts and observed host activity. Publish a reviewable evidence draft; production remains unchanged.
