# HTTP/3 datagram queue references implementation plan

**Date:** 2026-09-13
**Status:** Accepted; not implemented
**Track:** Q in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

`http3/state_tracking_stream.go:151–177` returns queued data before recvErr and advances its front slice without clearing the consumed slot. Queue capacity is bounded at 32; this is avoidable retained reachability, not evidence of unbounded growth. TestDatagramReceiving covers FIFO, maximum depth, blocking and cancellation.

## Decision

Clear the consumed queue slot before advancing the existing slice. Preserve FIFO, capacity, blocking/cancellation and draining already queued data before recvErr, including after closeReceive.

**Rejected alternatives — do not do this:** Do not drain the queue at terminal receive, change queue ordering, add a ring/cursor, or claim an allocation/throughput improvement.

**Non-goals:** No datagram negotiation, public API, terminal-drain, or queue-capacity change.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| Q1 | New; frontier | Clear consumed HTTP/3 datagram queue slots | None | None |

## Implementation slices

### Q1 — Clear consumed HTTP/3 datagram queue slots

**What it delivers:** Remove a queue backing-array reference when its datagram is returned, preserving the existing receive contract.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** Existing stateTrackingStream receive mutex and receive queue own slot mutation; caller owns returned data.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** One queue pop path; errors remain after queued data. No ordering, public schema, capacity or dependency changes.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal slot clearing for the single queue pop seam; finite canonical slice index zero. Existing queue implementation owns representation; no claim about GC timing.

**Contract closure:** Not triggered: a single synchronous operation/deletion is covered by ordinary focused evidence; multiple independently reachable materially risky lifecycle paths are not being redesigned.

**Evidence budget:** Existing TestDatagramReceiving and HTTP/3 package tests; at most one meaningful retained-reference regression if existing tests cannot observe ownership. No new helper framework, GC timing, race repetition or benchmark. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Run `go test ./http3 -run "^TestDatagramReceiving$" -count=1` and affected package suite once. Do not add a test merely mirroring assignment syntax.

**Dispatch context budget:** This slice, stateTrackingStream ReceiveDatagram/receive/close methods and existing queue tests; one assignment and ownership reasoning. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Cannot split one release operation. Combining with mock deletion would mix unrelated packages and owners. No blockers.

**Stop conditions:** Stop if clearing requires copying caller data, changes queue draining before error, or requires a new queue representation. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Acceptance criteria:**

- [ ] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [ ] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [ ] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

## Validation gates

Use each slice’s named focused commands and finite evidence. Follow the execution overlay for exact-head local certification and applicable hosted CI. Record actual test names if current naming differs, and verify selectors execute tests. Do not claim native behavior from compilation alone.

## Operating Discipline

The shared [review-loop baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md) and [contract-closure baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md), supplied by `$implement-architecture-slice`, govern directly. Apply the repository-specific [execution overlay](../REVIEW-LOOP.md), [maintained conventions](../agents/conventions.md), and ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md), and [0005](0005-incoming-packet-lifetime.md). Do not seed a repository copy of either shared baseline.

One fully briefed initial review and at most one replacement review; independently disposition findings before fixes. Stop on a representation mismatch, an established repeated precise root, or required boundary expansion. Compare exact invariant, concrete enforcement seam, semantic classes, and why the earlier accepted family owned the later case before calling findings one repeated root. No recursive proof obligations on tests or frozen diagnostics. No new performance campaign, random stress expansion, unsupported platform cross-product, or timing SLA. Ordinary focused regression tests remain required evidence of the accepted behavior; no new verification framework is a maintained blocking deliverable.

After the exact reviewed head passes local certification and applicable existing hosted checks, squash merge, append the development journal, reconcile issue/frontier pointers, and complete the corresponding OmniFocus slice task. Do not complete the program parent until its actual children are complete. Existing open investigations and completed programs keep their scopes.
