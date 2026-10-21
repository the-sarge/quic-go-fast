# Simulated-Network Delivery Shutdown Implementation Plan

**Date:** 2026-10-20
**Status:** S1 complete; owned endpoint delivery is canceled before link joins
**Track:** A20-S — 2 of 5 in A20
**Depends on:** Nothing; independent slices as stated below
**Related:** [Program index](2026-10-20-architecture-deepening-program.md), [no-code decisions](2026-10-20-architecture-decisions.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Source capture and slice audit](../audits/2026-10-20-architecture-handoff/README.md)

## Goal

Make Simnet.Close terminate the network-owned delivery workers that can block on endpoints created by Simnet.NewEndpoint. The network module owns the shutdown signal and joins; callers retain endpoint ownership and live delivery retains backpressure.

## Current Shape (verified 2026-10-20)

[`testutils/simnet/simnet.go:44`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/testutils/simnet/simnet.go#L44) closes links sequentially; [`testutils/simnet/simlink.go:72`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/testutils/simnet/simlink.go#L72) joins workers after queue closure, while [`testutils/simnet/simlink.go:79`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/testutils/simnet/simlink.go#L79) can be blocked in RecvPacket. [`testutils/simnet/simconn.go:114`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/testutils/simnet/simconn.go#L114) waits on the 32-slot endpoint or endpoint closure. [`transport_test.go:107`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/transport_test.go#L107) returns network-only cleanup; Transport does not close caller-owned connections. The earlier synctest diagnostic reaches a durable block at 33 delivery attempts, also under race detection.

## Decision

Give network-created endpoint delivery a cancellation signal owned by Simnet. Close broadcasts it before joining any link, so a delivery blocked on endpoint capacity can end without endpoint.Close. Use a concrete private cancellable delivery path; do not widen PacketReceiver or Router interfaces. Preserve standalone endpoints, caller ownership of endpoint closure and already-buffered data, and live-network backpressure. Network shutdown may abandon its still-undelivered work; it must not consume existing endpoint buffers. Do not claim arbitrary user callbacks are interruptible or add a concurrent/repeated Start/Close contract.

**Rejected alternative (do not do this): Only repair endpoint-first helper cleanup.** Useful hygiene but leaves worker shutdown dependent on another owner’s endpoint closure. **Revisit if:** only if a future explicit public contract requires endpoint-first shutdown.

**Rejected alternative (do not do this): Close caller endpoints from network Close.** Takes ownership of caller resources and changes buffered-data availability. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Make active delivery nonblocking, enlarge buffers or spawn per-packet goroutines.** Changes simulation behavior or hides the blocked-worker condition. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Make arbitrary callbacks cancellable through a new exported protocol.** Outside the bounded network-created endpoint contract. **Revisit if:** a separately accepted custom-receiver shutdown requirement.

**Owner's design:** Adopt the network owner’s cancellation-before-join operation. It adds one shutdown signal to existing owned delivery waits, not another worker or policy state machine. Endpoint-first cleanup uses a different owner to release workers and does not remove that external ordering obligation. A universal receiver cancellation interface would change closed external callback contracts, so it remains rejected.

**Non-goals:** No packet loss/latency/MTU redesign, queue-size change, public callback-interface change, endpoint auto-close, arbitrary custom callback guarantee, general cancellation framework, additional Start/Close concurrency promise or production transport change.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| S1 | complete ([#928](https://github.com/the-sarge/quic-go-fast/issues/928)) | Cancel owned endpoint delivery before joining Simnet links | None | n/a |

## Implementation Slices

### Slice S1 — Cancel owned endpoint delivery before joining Simnet links

**What it delivers:** Make Simnet.Close terminate the network-owned delivery workers that can block on endpoints created by Simnet.NewEndpoint. The network module owns the shutdown signal and joins; callers retain endpoint ownership and live delivery retains backpressure.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None. This infrastructure repair is not an artificial blocker for unrelated product slices.

**Single owner after merge:** Simnet owns the cancellation transition; each existing delivery worker observes the signal and exits; SimConn continues to own its receive buffer and endpoint-closed state. Queue owners retain queue closure.

**Authority completeness:** No persisted fact, schema, public configuration or restart format becomes authoritative. Existing constructors and QUIC/HTTP parsers retain their contracts; this slice changes only the named runtime owner and its supported consumers.

**Transitional-seam budget:** One network-owned shutdown signal is a permanent part of the owned delivery lifetime, not a temporary compatibility seam. No duplicate mutable closure state or second receiver state machine. Exported custom callbacks remain outside the cancellation guarantee; no removal slice.

**Blast radius:** Private simulation lifecycle and queue/endpoint ordering only. Preserve receive-attempt statistics, buffered delivery, backpressure, virtual time and ordinary endpoint Close. Signal all links before joins. A concurrently racing delivery may linearize before cancellation or be abandoned; do not guarantee a new packet after Close starts. Arbitrary external callbacks are an explicit non-goal. Trace finalization via root helpers without making them the sole fix.

**Artifact classification:** Simnet is a verification aid explicitly selected as a maintained deliverable: payoff is terminating simulated-test teardown under endpoint saturation; domain is started networks and their NewEndpoint delivery; owner is testutils/simnet; retirement is when Simnet is replaced or the owned blocking delivery contract is removed. Its own regression tests are ordinary verification aids, not a recursively certified harness. No shipped product slice is blocked by completion of this aid. Plans/receipts are metadata.

**Representation contract:** Domain: Simnet Start followed by Close using its NewEndpoint-created endpoints and existing supported router/queue behavior. Internal typed packets and lifecycle channels are owned by simnet. Universal owned-delivery cancellation invariant; finite synctest examples, not proof against arbitrary callbacks or every scheduler.

**Contract closure:** Not triggered for the verification aid: the selected bounded delivery cancellation is demonstrated by ordinary focused cases. Do not recursively impose product closure or a generic harness completeness claim. The maintained-aid exception above makes this outcome reviewable on its own, not a new product gate.

**Evidence budget:** One unsaturated control; one saturated unread destination; one multi-link case that would block the first sequential join; endpoint-first cleanup; live backpressure; caller access to buffered data after network Close. At most six semantic cases using deterministic synctest; one focused/package/race run and existing CI. No mutation or wall-clock repetition. One review plus at most one replacement.

**TDD and preservation evidence:** Begin with network Close remaining blocked under saturation; change the same case to require completion without endpoint Close. Add the multi-link ordering and ownership assertions, then retain existing timing/count tests.

**Dispatch context budget:** Current S1, Simnet/SimConn/SimulatedLink/queue definitions, their focused tests, root newSimnetLink and existing transport-close ownership. At most 12k tokens of selected source/context; no unrelated proxy or production connection changes. No unresolved implementation history.

**Slice decision audit:** Splitting signal plumbing from observing it would leave an inert incomplete shutdown path. Keep the whole owned lifecycle and test in one small PR. Merging the root helper’s unrelated cleanup audits or retry work adds owners; reject, revisit none: rejected on merit. There are no blocking slices. Compare the network-owned design above before preferring a smaller endpoint-first edit.

**Stop conditions:** Stop if the repair requires a public callback change, closing caller endpoints, changing active delivery/drop/timing semantics, extra per-packet execution contexts, or guaranteeing arbitrary callbacks. Do not broaden the outcome to explain unrelated CI hangs.


## Acceptance Criteria

- [x] Within a started Simnet whose destinations were created by NewEndpoint, Close signals owned delivery cancellation before the first link join, so a full unread endpoint does not prevent its workers from completing.
- [x] The endpoint remains caller-owned and previously buffered packets are not consumed by network shutdown; live delivery remains blocking until capacity, endpoint closure or network shutdown.
- [x] Standalone/custom receiver behavior and simulation timing, MTU, accounting conventions and existing endpoint-first cleanup remain intact.

Each universal statement is limited to the slice’s declared domain and owner, with the guarantee and terminating evidence in that slice. No broader protocol, concurrency or platform certification is implied.

## Validation Gates

Write TestSimnetShutdownSaturatedEndpoint first against unchanged code using synctest’s durable-block observation. Run `go test ./testutils/simnet -count=1`, focused root Simnet users selected from `transport_test.go` and `send_stream_test.go`, `go test -race ./testutils/simnet -count=1`, `go vet ./testutils/simnet`, and `go mod tidy -diff`, plus existing hosted checks. If root helper code changes, include affected root tests and vet under the repository overlay. No wall-clock stress or platform matrix beyond the existing CI is added.

## Operating Discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern, composed with the existing [repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). The shared files are `REVIEW-LOOP.md`, `CONTRACT-CLOSURE.md`, and `DESIGN-COST.md` in the active architecture skill package’s sibling `_shared/` directory. Use those baselines directly; do not copy them into this repository. No repository-specific contract-closure overlay exists or is needed for this program.

Apply representation and artifact gates, independently disposition findings, and use one initial fresh review plus at most one replacement after accepted fixes. Verification is limited to accepted source findings. Apply the shared precise-root comparison and approach stops; a common package or lifecycle topic does not establish a repeated root. Do not grow the evidence plan to exhaust reviewer suggestions or recursively certify fixtures. Any replacement design must be compared by obligations, contract changes, then footprint.

Use a dedicated worktree and feature branch. Certify the exact pushed head using the focused gates below and the repository overlay; require applicable existing hosted checks on that head, then mark ready and squash merge with a matching head. This repository has no `task preflight` or `ci-*` workflow. Diagnose failed checks before reruns; only demonstrated infrastructure failure permits a same-head rerun. After the primary work reaches remote main, run `append-dev-journal` without RAS, revalidate deferred findings against the merge, and reconcile issue and OmniFocus pointers. Plan/mirror state does not substitute for code-head certification.

This document packages future work. The handoff run does not implement or dispatch it. Changes to representation ownership, accepted outcome, topology, adjacent scope, irreversible authority, or the one-PR boundary require a decision or a new scoped architecture handoff.
