# Linux Coalesced Receive (GRO) Implementation Plan

**Date:** 2026-09-11
**Status:** Accepted; complete — G1 (#236), G2 (#239)
**Track:** G, 1 of 3 in the 2026-09-11 datapath offload program
**Depends on:** Nothing — safe to start first
**Related:** [Datapath offload plan](2026-09-11-datapath-offload-plan.md); ADRs [0001](0001-upstream-compatibility.md), [0003](0003-follow-stable-upstream-releases.md), [0005](0005-incoming-packet-lifetime.md) (amended 2026-09-11)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions
**Audit history:** [Handoff receipt](../audits/2026-09-11-datapath-handoff/README.md); [discovery](../audits/2026-09-11-keibisoft-gso-discovery.md); [stack comparison](../audits/2026-09-11-quic-stack-datapath-comparison.md)

## Goal

The sys-layer receive path becomes deeper: one socket read can deliver several UDP datagrams (Linux UDP_GRO), while everything above `ReadPacket()` keeps its one-call-one-datagram contract. Receive syscalls per delivered datagram drop on GRO-capable kernels with no caller-visible change.

## Current Shape (verified 2026-09-11)

`oobConn.ReadPacket()` pops one message per call from an eight-message `recvmmsg` batch (`sys_conn_oob.go:143`, `sys_conn_helper_linux.go:24`). There is no UDP_GRO use anywhere. The buffer pool accepts exactly two capacities, 1452 and 20480 bytes (`buffer_pool.go:56-66`, `internal/protocol/protocol.go:114`); `packetBuffer` reference counting is documented non-concurrent and `Release()` panics with live siblings (`buffer_pool.go:12-15,42-50`). Retention queues are count-bounded: 32 undecryptable (`internal/protocol/params.go:12`) and 256 unprocessed (`internal/protocol/params.go:43`) packets per connection. The transport records socket ownership as `createdConn` (`transport.go:163`). GSO send capability is probed per connection (`sys_conn_oob.go:131`, `sys_conn_helper_linux.go:66-77`) with the `QUIC_GO_DISABLE_GSO` kill switch.

## Decision

Split coalesced receives inside the sys layer, backed by an atomically reference-counted coalesced slab authorized by the 2026-09-11 amendment to ADR 0005, with the storage rules, socket-ownership guard, probes, kill switches, and adoption gates fixed in the [datapath offload plan](2026-09-11-datapath-offload-plan.md). That plan is binding for this track; this document adds only the slice decomposition.

**Rejected alternative (do not do this):** Splitting above the sys layer or passing super-buffers to the packet parser — QUIC's short-header rule silently swallows trailing segments. Reusing the non-atomic `packetBuffer` refcount for cross-connection sibling views — it is documented non-concurrent. Growing `MaxLargePacketBufferSize` for everyone instead of a third tier.

**Non-goals:** UDP_GRO on caller-supplied sockets; io_uring; changing Linux GSO send; any public API change; the externally owned application sweep.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
|---|---|---|---|---|
| G1 | Complete (#236) | Coalesced-storage contract (slab, third tier, split helper, retention copy), behaviorally inert | None | n/a (introduces inert-machinery seam; activated by G2) |
| G2 | Complete (#239) | Linux UDP_GRO receive end to end with adoption evidence | None (G1 complete) | Activates G1 machinery (inert seam closed) |

## Implementation Slices

### Slice G1 — Coalesced-storage contract

**What it delivers:** The platform-neutral storage layer for coalesced receives: an atomically reference-counted coalesced slab type; a third 65535-byte pool tier (`buffer_pool.go` currently rejects unknown capacities); a split helper that, given a filled slab and a segment size, yields per-datagram `packetBuffer`-compatible views; the platform-neutral retention-queue copy logic — a view entering a count-bounded retention queue is copied into the smallest fitting existing tier, and an oversized view keeps its slab charged at full slab size against a per-connection retained-bytes budget — wired into the undecryptable and unprocessed queues but dormant, since no runtime path can create a slab view until a platform producer (G2, W3) exists; and the shared routing closure-test fixtures those producers reuse. All exercised by unit and race tests. Behaviorally inert at runtime: no producer wires the socket path, matching the expand step of expand–migrate–contract; it grants no authority.

**Existing-work disposition:** New slice. No open PR, branch, or partial implementation exists for this track; the KeibiSoft fork is an external MIT reference for Track D only.

**Blocked by:** None.

**Single owner after merge:** The buffer pool owns slab allocation and recycling; the slab owns the lifetime of its segment views; each retention queue owns its copies and its retained-bytes budget accounting. No second mutation owner is introduced.

**Authority completeness:** No persisted fact becomes authoritative; the machinery is inert until G2.

**Transitional-seam budget:** One seam — merged machinery with no producer. Coherent because it is tested, inert, and grants no authority; removed (activated) by G2, and W3 becomes its second producer. G1 must not widen any existing seam.

**Blast radius:** Additive files plus two bounded shared-file touches, both traced: `buffer_pool.go` accepts the third capacity, and the `packetBuffer` release path gains a slab hook so a slab view's release decrements the slab's atomic count — a hot-path edit on all platforms whose nil-hook case preserves today's behavior exactly (full suite plus existing pool tests gate it); and the dormant retention-queue copy wiring in connection receive queues, unreachable until a producer exists. Untraced effects: none remaining.

**Artifact classification:** Slab/tier/split/retention code: shipped behavior (inert until activated). Race and unit tests: verification aid, non-blocking beyond normal CI. No maintained-aid exception requested.

**Representation contract:** Internal storage representation owned by the buffer pool and slab; universal guarantee over the internal domain (any segment count 1..slab capacity, any segment size 1..65507); terminating evidence is the closure matrix below.

**Contract closure:** Triggered — concurrency corruption is a material consequence and sibling release orders are independently reachable paths. Invariant: every slab is released to the pool exactly once, after its last view releases, and no view's bytes are recycled while reachable. Enforcement owner: the slab's atomic count (the routing invariant is owned separately by the producer slices' split/dispatch path). Semantic classes and dispositions: (1) all views released sequentially → slab recycled once — covered by unit test; (2) views released concurrently from distinct goroutines → recycled once, race-clean — covered by race test; (3) a view copied for retention then released → copy independent, slab count decremented — covered; (4) oversized view retained → slab held, budget charged, released on queue eviction — covered; (5) double release of one view → panic (existing contract preserved) — covered by negative test. One guard mutation: delete the atomic decrement's recycle condition and observe class 1/2 tests fail.

**Evidence budget:** One positive test per class above, one negative (double release), one guard mutation, race detector on classes 2–4. No platform, timing, or repetition scope.

**TDD and preservation evidence:** Write the class 1–5 tests first against the empty types. Preservation: existing pool tests unchanged and green; no runtime path touched, full suite green.

**Dispatch context budget:** This slice contract, the storage rules and ADR 0005 amendment text in the datapath offload plan, and `buffer_pool.go` (~100 lines). Implementation plus review comfortably fits one fresh context; no governing diff needed.

**Slice decision audit:** Strongest split: slab separate from retention-copy rule — rejected, the retention rule is meaningless without the slab and both are small. Strongest merge: fold into G2 — rejected, G2 then carries storage, syscall wiring, and adoption evidence in one context, which is doubtful; the inert expand step is the sanctioned shape. Blocking edges: none claimed.

**Stop conditions:** If per-datagram views cannot present as `packetBuffer`-compatible without touching ADR 0005's non-atomic contract for ordinary storage, stop — that contradicts the amendment's boundary and needs operator review.

### Slice G2 — Linux UDP_GRO receive end to end

**What it delivers:** UDP_GRO enabled by a runtime probe on transport-owned sockets only (`transport.go:163` `createdConn`), the GRO segment-size control message parsed in the Linux OOB path, coalesced reads split through G1's helper with each view inheriting its read's ancillary data, the `QUIC_GO_DISABLE_GRO` kill switch — activation of G1's dormant retention-copy wiring follows automatically once slab views exist — and the track's adoption evidence: the precommitted protocol and results pair under `docs/audits` defined by the datapath offload plan, including the mixed-connection-ID, short-header-remainder, and rejected-sibling race tests and the engagement, syscall, throughput, and memory measurements.

**Existing-work disposition:** New slice.

**Blocked by:** G1.

**Single owner after merge:** `oobConn` owns splitting and probe state; the buffer pool owns slabs (per G1); retention queues own their copies. Socket-option mutation has one owner: transport socket setup, gated on `createdConn`.

**Authority completeness:** The GRO capability flag becomes authoritative for the receive path; its constructor (probe), validation (kill switch, probe failure fallback), restart round trip (per-connection re-probe; no persistence), and consumers (split path only) are all inside this slice. No destructive consumer exists.

**Transitional-seam budget:** None introduced; closes G1's inert-machinery seam for Linux. W3 later adds the second producer without changing this slice's contract.

**Blast radius:** `sys_conn_oob.go` is shared by Linux, Darwin, and FreeBSD — all changes gate on the probed capability so non-Linux behavior is byte-identical; verified by the full suite on the CI matrix. Memory: per-socket prepost grows to 8 × 64 KiB ≈ 512 KiB when GRO probes true (`sys_conn_helper_linux.go:24`), within the budgets the protocol declares. ECN and pktinfo ancillary data are per-message and inherited by every view — covered by a positive test. Untraced effects: none identified beyond the declared memory growth.

**Artifact classification:** Probe, split wiring, kill switch, retention activation: shipped behavior. Race/correctness tests: verification aid. Protocol and results documents: process/traceability metadata, required by the program's adoption rule (an accepted program gate, not a maintained-aid exception).

**Representation contract:** Input domain is kernel-delivered GRO buffers (equal-size segments, short tail permitted) on transport-owned sockets; the kernel plus the cmsg parser own the representation; guarantee is universal over that domain; terminating evidence is the correctness gate plus protocol measurements.

**Contract closure:** Triggered — mixed-connection-ID routing has material concurrency consequence across independently reachable terminal paths. Two invariants with singular owners: every slab releases exactly once (owner: G1 slab, already closed in G1), and every segment view reaches exactly one routing outcome (owner: the `oobConn` split/dispatch path, closed here using G1's shared fixtures). Classes: sibling views to two established connections; to an Initial; to unknown-CID/stateless-reset; rejected at admission while a sibling is queued; queue overflow with a sibling in flight; short tail segment. Each has a race-enabled test; dispositions covered. Guard mutation: skip the retention copy and observe the queue test fail.

**Evidence budget:** The closure tests above; protocol measurements per the datapath offload plan (engaged, available-but-unexercised, disabled, unavailable; peer GSO fixed and reported; statistical method, confidence bounds, minimum useful effect, memory budgets, CPU-affinity policy predeclared). One review plus at most one replacement.

**TDD and preservation evidence:** Closure tests written first and failing against the unsplit path. Preservation: with GRO unavailable or disabled, receive behavior is byte-identical — full suite plus the disabled-configuration protocol cell detect regressions.

**Dispatch context budget:** This slice contract, G1's merged API, the storage/gating/gate rules in the datapath offload plan, `sys_conn_oob.go` and `sys_conn_helper_linux.go` (~475 lines combined), and the protocol template. Fits one fresh context; the governing diff is G1's merged PR if the implementer needs it.

**Slice decision audit:** Strongest split: separate the adoption evidence into its own docs-only slice — rejected, the program's gate requires protocol-before-measurement and results-before-adoption, and measurement runs against this PR's head; splitting evidence from code creates an unmergeable ordering. Strongest merge: with G1 — rejected as above. Blocking edge G1: genuine — the split helper and slab are this slice's storage substrate.

**Stop conditions:** Engagement metric is zero across protocol cells with peer GSO enabled (offload never engages — approach evidence failure); probe succeeds but split output violates the closure invariant in any class (storage contract failure); memory measurements exceed declared budgets with no in-budget mitigation.

## Acceptance Criteria

- [x] On a GRO-capable Linux kernel with a GSO-enabled peer, coalesced reads engage (coalesced segments per read > 1 observed and reported) and receive syscalls per delivered datagram decrease per the protocol's predeclared threshold — [protocol](../audits/2026-09-11-g2-gro-protocol.md) and [results](../audits/2026-09-11-g2-gro-results.md): engagement ~100%, syscalls per datagram ratio 0.263 against the 0.75 gate.
- [x] With GRO unavailable, disabled via `QUIC_GO_DISABLE_GRO`, or the socket caller-supplied, receive behavior and socket options are unchanged (negative criterion: no `UDP_GRO` setsockopt is issued on caller-supplied sockets) — unit-asserted (`TestGRONotEnabledOnCallerSuppliedSocket`, `TestGRODisabledByEnv`) plus the results' disabled/unavailable cells.
- [x] All contract-closure classes pass under `go test -race` (`coalesced_routing_closure_test.go`, `coalesced_retention_holds_test.go`, `sys_conn_gro_linux_test.go`).

Universal criteria: domains, owners, guarantee levels, and terminating evidence are declared per-slice above; no criterion claims coverage beyond the kernel-delivered GRO domain.

## Validation Gates

`go test ./...` and `go test -race ./...` on the CI matrix; the G2 closure tests focused; the precommitted protocol's measurement cells on the qualified Linux host.

## Operating Discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` for every slice/PR; no repository-specific overlay exists. Track-specific stops: the datapath offload plan's storage and gating rules are binding; a change to the split seam's ownership, the ADR 0005 amendment boundary, or the caller-owned-socket rule returns for decision. Vocabulary: coalesced receive, receive batch, and segmented send are defined in the root `CONTEXT.md`.
