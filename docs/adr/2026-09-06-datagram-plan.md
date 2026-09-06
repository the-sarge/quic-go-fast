# Datagram receive efficiency implementation plan

**Date:** 2026-09-06. **Status:** D1 complete; D2 ready. **Track:** D of QGF-2026-09. **Depends on:** Nothing. **Related:** [Program](2026-09-06-fork-program.md), [compatibility](0001-upstream-compatibility.md). **Audit history:** [Handoff audit](../audits/2026-09-06-handoff.md).

## Goal and current shape

Make bounded receive admission own the allocation decision and reusable queue storage, preserving the existing application interface. Source anchors refer to v0.62.0: `datagram_queue.go:12` limits receive storage to 128 messages; `:93` copies before admission; `:113` slices from the front without clearing references; `connection.go:2144` validates the frame before delivery; `connection.go:3065` is the public receive seam. `internal/utils/ringbuffer/ringbuffer.go:51` clears a popped slot. Existing `datagram_queue_test.go:75` onward covers receipt, blocking, cancellation and closure.

## Decision and preservation

Keep `datagramQueue` as the queue owner and `rcvMx` as its only admission/dequeue lock. Keep the 128-message limit, drop-new behavior on overflow, FIFO among admitted messages, owning payload copy on admission, caller-owned returned bytes, current cancellation/close precedence, and notification semantics. Keep zero-value queue construction lazy. Never reuse a payload after handing it to the application.

**Rejected alternative (do not do this):** Increase queue limits to manufacture a throughput win, replace mutexes with unmeasured channels/lock-free machinery, pool caller-owned returned byte slices, alter send backpressure, or claim the old receive slice grows without bound. **Non-goals:** Send API, multi-frame packetization, frame/parser rewrite, transport settings, physical sweeps, new shared benchmark framework, jumbo/multipath, or eliminating all datagram allocations.

## Slice graph

| Slice | Disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| D1 | complete; runtime improvement | Full-queue admission drops without payload allocation/copy | None | None |
| D2 | new; independently evaluate upstream #5557 | Receive storage reuse and cleared popped references | None | None |

## Common contract and evidence

Representation domain: valid decoded DATAGRAM frames passed through the existing wire parser/connection validator, including empty and 1071-byte payloads, with queue occupancy 0..128; ordinary `ReceiveDatagram(ctx)` callers. The wire parser owns protocol syntax, the queue owns bounded admission and payload lifetime. The functional guarantee is a canonical internal domain invariant backed by source ownership and example-level regressions; benchmark results describe only the recorded workload/environment. No external parser or universal timing proof is introduced.

Artifacts: queue changes are shipped behavior; existing/new focused tests and Go benchmarks are verification aids; result receipts and dispositions are process metadata. No standalone maintained verification tool is introduced or promoted to a product dependency. Performance evidence is a finite patch acceptance check, not a new framework that must be completed before product work.

For each candidate, keep one 1071-byte steady drain case, one burst-to-capacity/drain case, and one full-queue overflow case. First characterize them on the exact comparison base, then run the same benchmarks against the candidate. Use 10 samples at 200 ms per case and `-benchmem` with fixed Go version, GOMAXPROCS and host; 10 samples answer whether a timing regression exceeds noise and support benchstat's default comparison. Allocation counters, not sampled profile weights, establish allocation changes. Run one paired base/candidate set; one replacement pair is allowed only for documented host contention. No physical network, high-rate campaign, new profiler installation or broad parameter sweep is required here. Report ns/op, B/op and allocs/op separately; reductions in metadata or dropped-message work are not file-throughput claims.

Reject/rework within the same contract if the expected allocation improvement is absent or a reproducible >5% timing regression appears in a preserved non-target case. If one replacement cannot resolve contamination or the tradeoff, return a no-change/inconclusive receipt instead of tuning thresholds or broadening scope. If a patch is rejected, retain only useful ordinary characterization tests and a receipt in its one PR; no runtime mutation is required to close its bounded experiment.

## Implementation slices

### D1 — Avoid allocation for receive overflow

**Current state:** Complete; runtime improvement accepted. [Bounded evidence receipt](../audits/2026-09-06-d1-overflow.md).

**What it delivers:** Move capacity admission under `rcvMx` ahead of allocation/copy. If full, unlock and retain existing conditional discard logging; if admitted, make the owning copy and append while holding the same lock, then preserve notification. This keeps admission atomic without an unlocked check/reservation race.

**Single owner after merge:** `datagramQueue.HandleDatagramFrame` under `rcvMx` owns admission; `Receive` owns removal under the same lock. No durable fact is introduced. **Authority completeness:** Not applicable; no persistence or new security authority. **Transitional seams:** None.

**Existing-work disposition:** New; no fork PR exists. The upstream ring proposal is independent of this allocation-order change. **Blast radius:** Receiver lock hold time includes one bounded payload copy; full-queue behavior, concurrent dequeue and logger branches are traced. Public methods, packet validity, queue limit, send path, dependencies and buffer-return ownership are unchanged. No untraced production consumer is accepted.

**Representation/artifacts:** Common contract above. **Contract closure:** Not triggered: this is one bounded admission owner with focused full/not-full and ownership cases; multiple callers alone do not require a closure matrix.

**TDD/preservation:** Before changing runtime code, add `TestDatagramReceiveOverflowNoAllocation` using a prefilled queue, preallocated valid frame and disabled debug logging with `testing.AllocsPerRun`; it must show the old per-drop allocation and pass with zero queue-path allocation after the patch. Add one capacity-plus-one FIFO/drain case and one input-buffer reuse case. Reuse existing receive/block/cancel/close tests, including a bounded concurrent producer/drainer case only if the existing suite lacks it. At most four new logical regression cases; no guard mutation.

**Evidence budget:** Common paired benchmarks and local gates; D1 must remove the dropped-message payload allocation/copy while preserving admitted-message ownership. No receive metadata-allocation claim is required.

**Dispatch context budget:** This slice plus common track/program policy, ADR 0001, `datagram_queue.go`, its tests, and the bounded connection handoff/public method excerpts; at most 800 source lines excluding generated mocks. Implementation, local disposition and verification fit one fresh context. Include only another merged queue slice's diff if it landed first.

**Slice decision audit:** Splitting test/benchmark setup from the admission change would create a verification-only predecessor without an independently required product outcome. Merging D1 with D2 would hide which mechanism changes costs; both can deliver independently. No blocking edge is necessary. **Stop:** A safe patch would require changing queue limits, public ownership, notification semantics or the lock topology; otherwise disposition evidence inside the common budget.

### D2 — Reuse receive queue storage

**What it delivers:** Replace the receive `[][]byte` with the existing lazy `ringbuffer.RingBuffer[[]byte]`, using `Len`, `PushBack`, and `PopFront` under the unchanged lock. Queue admission still limits live entries to 128; do not preallocate 128 slots for every idle connection. The existing ring clears removed references and retains bounded metadata for reuse.

**Single owner after merge:** The same queue admission/dequeue owner; ring storage mechanics remain in the existing internal ring module. **Authority completeness:** Not applicable; no durable authority. **Transitional seams:** None; no parallel slice/ring representation or new shared queue abstraction.

**Existing-work disposition:** Rework the idea from upstream PR #5557, inspected at `15e5ac2a2b5ca358600520179b63afd7f2200196`, against the release baseline. Its original memory-leak claim is rejected, and its diff alone supplies no benchmark or regression evidence. Do not cherry-pick it as an established dependency or modify its upstream discussion.

**Blast radius:** FIFO and payload liveness across wrap/grow/drain/reuse; idle-connection allocation; concurrent receive/close behavior; shared ring consumers remain unchanged because no ring implementation edit is planned. No API, protocol, send queue, dependency or queue-limit change. No untraced production consumer is accepted.

**Representation/artifacts:** Common contract above. **Contract closure:** Not triggered: ordinary focused queue transitions and the existing ring owner cover the risk; no new ownership protocol or persistent state machine is introduced.

**TDD/preservation:** Characterize many refill/drain cycles through the queue interface with distinct payloads, crossing the 128-entry boundary and wrap positions. Add an input reuse/returned-payload retention case if D1 has not supplied it; do not duplicate existing coverage. Verify `PopFront`'s existing clear at the source owner and retain inherited ring FIFO/wrap tests; no GC-finalizer timing or shared ring implementation change. At most three new logical cases. Benchmarks must distinguish initial growth from warmed steady reuse. The pre-patch characterization passes; metadata-cost evidence is the expected failing performance hypothesis, not a fabricated functional bug.

**Evidence budget:** Common paired benchmarks and local gates; demonstrate reduced metadata allocation/churn in warmed receive cycles while preserving the necessary one payload copy per admitted message. No unbounded-leak or universal heap-size claim. No guard mutation.

**Dispatch context budget:** Current slice plus common policy, queue source/tests, existing ring source/tests and the public receive handoff; at most 1100 source lines excluding mocks. Include D1's bounded diff if merged; do not require its completion. **Slice decision audit:** Splitting a new ring abstraction from adoption is unnecessary because the ring exists. Combining D1 obscures attribution. No genuine blocker. **Stop:** If benefit requires modifying shared ring semantics, preallocating all queues, returning pooled caller bytes, or changing admission limits, stop and re-audit rather than expanding this slice.

## Acceptance criteria and validation

- [x] D1 has a merged runtime improvement or an explicit bounded no-change disposition, with its allocation and preservation evidence.
- [ ] D2 has a merged runtime improvement or an explicit bounded no-change disposition, with its metadata-cost and preservation evidence.
- [ ] Each receipt identifies exact base/candidate/toolchain/workload and avoids inferring application throughput from microbenchmarks.

Focused gates for each runtime candidate: `go test -count=1 -run 'TestDatagram' .`, `go test -race -count=1 -run 'TestDatagram' .`, and `go test -count=1 ./internal/utils/ringbuffer`. Run `go test -count=1 ./...` once on its final reviewed code head; no repeated whole-suite runs without a candidate change or diagnosed failure. Run the new named Go benchmarks with `-run '^$' -bench '^BenchmarkDatagramReceive' -benchmem -count=10 -benchtime=200ms` on paired versions. The implementation must choose benchmark names under that prefix. Inherited same-head unit/lint/integration checks provide broader platform evidence when configured; no new physical platform matrix is added by this track.

## Operating discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice`, as bound by the [program](2026-09-06-fork-program.md). One initial review and at most one replacement; exact-head local certification, applicable same-head hosted CI, matched-head merge, post-merge journal, then pointer updates. No repository-specific overlay exists. Later performance families require a new scoped decision, not an expanded D1/D2 checklist.
