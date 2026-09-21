# Endpoint-owned managed lease binding implementation plan

**Date:** 2026-09-21
**Status:** Complete via [#495](https://github.com/the-sarge/quic-go-fast/pull/495)
**Track:** L, 3 of 3 in QGF-ARCH-20260921
**Depends on:** Nothing; this slice is on the independent frontier.
**Related:** [Program index](2026-09-21-architecture-deepening-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0006](0006-explicit-external-packet-io.md), [N01–N20](2026-09-13-architecture-decisions.md).
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions.
**Audit history:** [Slice audit and source/history dispositions](../audits/2026-09-21-architecture-handoff/README.md).

## Goal

The managed endpoint already controls who may use its socket and when a lease ends. Let it also own the decision that binds a lease for QUIC, instead of requiring Transport to manipulate its private state.

## Current shape

The ownership split is real, but no failure in the current binding transition was established. The benefit is reducing the number of places a maintainer must understand to change a lease rule. The simplest safe design is smaller than a general registration transaction.

Verified source anchors: `external_packet_io.go:106`; `managed_packet_endpoint.go:102`; `managed_packet_endpoint.go:123`; `managed_packet_endpoint.go:267`.

## Grilled decisions

### L1 · Is it worthwhile without a known bug?

Yes, as a small separate cleanup after the correctness repairs. It concentrates lifecycle rules in their existing owner. It does not warrant a new packet-I/O architecture or equal urgency with the first two changes.

### L2 · What is the recommended operation?

A private method on the exact managed lease binds it for QUIC and returns a small set of setup facts. The method hides endpoint locking, generation validation, active-I/O checks, receive setup, and the phase transition.

### L3 · What stays with Transport?

Its registration lock and slot, exact outer-resource identity check, exact factory-lease check, supplied checked send callback, and publication of the resulting registration. External registration and fixed-peer policy continue to compose.

### L4 · What do the returned facts contain?

The stable pointer to immutable endpoint buffer-setup evidence, a copied normalization-enabled flag, and copied receive diagnostics. They confer no raw-socket or descriptor authority. Live send counters remain in the existing registration object.

### L5 · Must the endpoint lock remain held until publication?

No, provided Transport keeps its registration lock through the whole operation and nothing fallible remains after endpoint binding succeeds. Initialization and other registration attempts pass through that lock. Capture the facts under the endpoint lock; release it; publish the registration before releasing the Transport lock.

### L6 · What if lease Close happens in that interval?

Treat binding as having happened when the endpoint commits the QUIC phase, followed by Close. Close may finish before registration returns even with today’s deferred unlocks. A returned setup record is not a promise that the lease stays alive; old-lease operations still fail their generation checks and cannot use a replacement lease.

### L7 · Is a publication callback or transaction object safer?

Not necessary here. A private synchronous publication callback would retain today’s exact internal lock span but add a rule about what callbacks may do while locked. A prepare/commit/abort object adds still more states and misleading rollback expectations. Prefer the ordinary returned facts under the proven locking conditions.

### L8 · What can fail and what must remain unconsumed?

Transport validation comes first, then lease state checks and fallible receive setup. On ordinary returned errors, preserve prior registration-slot and QUIC-binding state without additional consumption: previously free slots remain free, and occupied slots remain occupied. After the phase commits, only infallible registration assembly/publication remains. Do not promise reversal of every benign native setup side effect on an earlier failure.

### L9 · Can later initialization failure undo binding?

No. Preserve eager binding: once registration succeeds, the lease remains in its QUIC phase until lease Close, even if Transport initialization fails. Transport.Close still does not release the lease or own the endpoint socket.

### L10 · What socket rules are untouchable?

Preserve packet-I/O-lock then endpoint-lock ordering; normalizer installation before receive-format activation; Linux/Windows fallback differences; persistent normalization across leases; caller policy adapters; deadlines; close/join behavior; and exact lease provenance. Do not reject wrapper reads after binding—they are how the registered transport operates.

### L11 · What would make this cleanup no longer worthwhile?

If it requires a generic transaction, new exported types, raw socket handoff, a broader activation plan, or changes to native capability lifetimes, stop expanding it. The approved direction is only a small existing-owner extraction. Its benefit depends on actually removing endpoint-field and lock choreography from Transport.

### Private interface direction

```go
// Private intent. Transport already holds packetIO.mutex.
setup, err := exactLease.bindQUIC()
// On success: endpoint phase is committed, setup facts captured.
// Publish using setup; no further validation or fallible setup.
// Release packetIO.mutex only after publication.
// bindQUIC releases the endpoint lock before returning.
```

Names are illustrative private names. The behavior, ownership and ordering above are normative. No new public interface is authorized.

### Rejected directions — do not do this

- **Not needed: endpoint holds lock through a publication callback.** Preserves exact internal timing but no externally observable guarantee was found that requires it. Adds callback restrictions to a one-caller interface.
- **Rejected: prepare / commit / abort framework.** Receive-format responsibility persists across leases. An abort abstraction could falsely suggest reversible socket activation and adds multiple states for a synchronous operation.

**Non-goals:** No generalized lifecycle, activation, configuration-policy, or transaction framework; no new exported types, raw-socket authority, HTTP/3/root configuration merger, performance claims, or unrelated repairs. Preserve the decisions above and the accepted source owners.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| L1 | Complete via [#495](https://github.com/the-sarge/quic-go-fast/pull/495) | Endpoint-owned managed lease binding through its real entrypoints and tests | None | None introduced |

## Implementation slices

### Slice L1 — Endpoint-owned managed lease binding

**Stable identity:** `QGF-ARCH-20260921/L1`. One intended PR; GitHub child: [#488](https://github.com/the-sarge/quic-go-fast/issues/488).

**What it delivers:** The managed endpoint already controls who may use its socket and when a lease ends. Let it also own the decision that binds a lease for QUIC, instead of requiring Transport to manipulate its private state. Acceptance criteria below define the complete one-PR outcome.

**Existing-work disposition:** New slice. No open implementation PR supplies a dependency. Preserve merged behavior at the cited source; related closed repairs and open adjacent investigations are dispositioned in the linked audit. No local or unmerged branch is an accepted baseline.

**Blocked by:** None. Recommended program order is priority, not a dependency.

**Single owner after merge:** The managed endpoint/lease owns lease validity, active-I/O exclusion, native receive preparation and the QUIC phase transition. Transport owns outer-resource authority, registration-slot validation and publication under packetIO.mutex. Setup facts are copied under the endpoint mutex and publication is infallible before releasing the Transport lock. Existing lease generation checks own subsequent I/O authority.

**Authority completeness:** No new persisted fact or restart representation. Typed in-memory constructors, validation, consumers and terminal cleanup on the changed seam are included. Durable storage/restart round-trip gates are not applicable; do not invent persistence tests.

**Transitional-seam budget:** Zero new temporary seams. No dual authority, temporary adapter, migration branch, or double-open lifetime is allowed. Existing compatibility behavior is permanent supported behavior, not a temporary seam needing a later slice.

**Blast radius:** Registration and endpoint lock ordering, startup/Close exclusion, phase commitment before infallible publication, current generation checks, native receive format/normalization ordering, immutable setup references and diagnostics. Send callbacks/counters, socket close ownership, deadlines, wrapper policy, reused leases, and native fallback behavior are preserved. No new persisted state, platform code path, authority source, dependency, or performance claim; broader ECN/DF work is excluded. No identified untraced effect is accepted. A newly discovered effect outside this traced surface invokes the stop conditions.

**Artifact classification:** Runtime behavior and private refactoring are shipped behavior; the accepted admission/bounds/authority checks are required safety enforcement. Tests, fixtures and existing CI are verification aids; no new analyzer, mutation harness or maintained verification product is approved. Plans, review receipts, journal and tracking pointers are process/traceability metadata. Required proportionate regression evidence does not authorize recursive completeness requirements for its aids.

**Representation contract:** An exact factory managed lease, exact Transport outer resource (including explicitly supplied policy adapters), existing registration slot, and endpoint generations. Exact identity and existing generation validation are authoritative. Promotion through embedding confers no authority. Native setup stays within the existing Linux/Windows implementations and their supported fallback contracts. The enforcement guarantee is universal within this supported typed domain; finite tests are representative evidence, not an exhaustive proof. Each quantified acceptance criterion is scoped to this domain and named owner.

**Contract closure:** Not triggered. Failure has material consequences, but the changed operations and supported semantic distinctions can reasonably be covered by the ordinary focused tests and finite budget below. Multiple callers or concurrent states alone do not satisfy the shared second trigger. The table below is a focused preservation plan, not a new closure policy. If source/review evidence establishes both shared triggers, apply the shared policy within this same outcome; a required larger family is stop-for-decision.

**Evidence budget:** At most 6 new deterministic ordering/failure cases; reuse existing invalid-lease, exact-resource, busy-I/O, initialization-failure, exchange and reuse coverage. No mutation required; at most one optional bypass of the generation/binding guard if inherited evidence is insufficient. Existing hosted Linux/Windows jobs supply native preservation evidence; no new qualification or platform campaign. One fully briefed fresh review, at most one replacement after accepted fixes. One exact-head local certification and the applicable existing hosted check suite per candidate. No extra repetitions, timing thresholds, fuzzing, platform cross-products or new hosted experiments are approved. Diagnose failure before reruns; stop when the bounded evidence passes and dispositions permit completion.

**TDD and preservation evidence:** Write the missing focused regression/characterization cases first, observe their pre-change behavior, then repair/refactor and retain relevant existing tests. Ship tests and implementation together in one green PR. No separately landed failing-test PR.

| Semantic case | Required disposition | Evidence |
| --- | --- | --- |
| Invalid slot/resource/lease or active I/O | Reject without additional state consumption; preserve prior slot and lease state | Existing registration/error tests |
| Valid lease and successful native setup | Endpoint commits; Transport publishes infallibly under its lock | Existing exchange and setup tests |
| Bind competes with Close/init/other registration | One legal operation order; no partial registration or replacement-lease authority | Budgeted deterministic ordering cases |
| Transport initialization fails after bind | Lease remains bound until lease Close | Existing failed-init test |
| Native format, fallback, reuse, deadlines and wrappers | Preserve interpretation-before-activation and existing authority | Existing managed/native tests plus hosted OS coverage |

**Dispatch context budget:** One fresh agent context. Supply this current plan (including its sole slice), program binding rules, referenced ADR clauses, shared baselines and repository overlay; then only these source ranges/files: external_packet_io.go:16–165,215–260; managed_packet_endpoint.go; managed_packet_io_test.go; managed_packet_buffers.go:14–23,95–137; managed_packet_receive_linux.go; managed_packet_receive_windows.go; transport.go:386–412,492–514; fixed_peer_test.go:279–311. Target at most 24,000 input tokens (including focused test extracts), leaving the rest of the fresh context for implementation, review disposition and verification. Read only the relevant existing-work row in the linked audit; there is no unresolved prior implementation review or governing contract diff. Do not replay the five report rounds or duplicate historical plans. If additional owners or an input bundle beyond this budget become necessary, re-slice before implementation.

**Slice decision audit:** A separate descriptor-only prefactor would add an unused representation and no delivered ownership improvement. One small bind-and-publish extraction plus preservation tests is independently green. Merging with configuration or path repair provides no invariant dependency. Native activation or metadata redesign would require a separate future decision, not a successor hidden inside this slice. There are no blocking edges: priority and shared-package test execution are not code dependencies.

**Stop conditions:** Stop for a decision if the accepted outcome, representation owner, authority, public contract, native capability scope, or one-PR/context boundary must change; if a required invariant cannot fit the declared finite evidence budget; or if the shared precise-root review/verification stop triggers after its required side-by-side comparison. No repeated-root conclusion may be based solely on a shared module or lifecycle. A failing existing test requires diagnosis; do not rerun, quarantine, relax timeouts, or absorb unrelated repairs automatically.

## Acceptance criteria

- [x] L1: Yes, as a small separate cleanup after the correctness repairs. It concentrates lifecycle rules in their existing owner. It does not warrant a new packet-I/O architecture or equal urgency with the first two changes.
- [x] L2: A private method on the exact managed lease binds it for QUIC and returns a small set of setup facts. The method hides endpoint locking, generation validation, active-I/O checks, receive setup, and the phase transition.
- [x] L3: Its registration lock and slot, exact outer-resource identity check, exact factory-lease check, supplied checked send callback, and publication of the resulting registration. External registration and fixed-peer policy continue to compose.
- [x] L4: The stable pointer to immutable endpoint buffer-setup evidence, a copied normalization-enabled flag, and copied receive diagnostics. They confer no raw-socket or descriptor authority. Live send counters remain in the existing registration object.
- [x] L5: No, provided Transport keeps its registration lock through the whole operation and nothing fallible remains after endpoint binding succeeds. Initialization and other registration attempts pass through that lock. Capture the facts under the endpoint lock; release it; publish the registration before releasing the Transport lock.
- [x] L6: Treat binding as having happened when the endpoint commits the QUIC phase, followed by Close. Close may finish before registration returns even with today’s deferred unlocks. A returned setup record is not a promise that the lease stays alive; old-lease operations still fail their generation checks and cannot use a replacement lease.
- [x] L7: Not necessary here. A private synchronous publication callback would retain today’s exact internal lock span but add a rule about what callbacks may do while locked. A prepare/commit/abort object adds still more states and misleading rollback expectations. Prefer the ordinary returned facts under the proven locking conditions.
- [x] L8: Transport validation comes first, then lease state checks and fallible receive setup. On ordinary returned errors, preserve prior registration-slot and QUIC-binding state without additional consumption: previously free slots remain free, and occupied slots remain occupied. After the phase commits, only infallible registration assembly/publication remains. Do not promise reversal of every benign native setup side effect on an earlier failure.
- [x] L9: No. Preserve eager binding: once registration succeeds, the lease remains in its QUIC phase until lease Close, even if Transport initialization fails. Transport.Close still does not release the lease or own the endpoint socket.
- [x] L10: Preserve packet-I/O-lock then endpoint-lock ordering; normalizer installation before receive-format activation; Linux/Windows fallback differences; persistent normalization across leases; caller policy adapters; deadlines; close/join behavior; and exact lease provenance. Do not reject wrapper reads after binding—they are how the registered transport operates.
- [x] L11: If it requires a generic transaction, new exported types, raw socket handoff, a broader activation plan, or changes to native capability lifetimes, stop expanding it. The approved direction is only a small existing-owner extraction. Its benefit depends on actually removing endpoint-field and lock choreography from Transport.
- [x] ConfigureManagedPacketIOV1 no longer takes the endpoint mutex or reads active I/O, lease.quic, the receiver, or receive-state fields. It handles Transport authority and the returned result.
- [x] Focused evidence and affected-package validation pass at the exact pushed head, with successful applicable hosted checks on that same head and no unresolved stop-for-decision disposition.

The typed-domain representation contract above bounds every “all,” “no,” “only,” “each” and “exactly” claim in these criteria. The case table and evidence budget terminate verification; tests do not claim complete schedule enumeration.

## Validation gates

```sh
go test . -run '^TestManagedPacket|^TestFixedPeer' -count=1
go test -race . -run '^TestManagedPacket|^TestFixedPeer' -count=1
go test . -count=1
go vet .
go mod tidy -diff
```

Include the newly named regression tests in the focused selections; the commands above select existing families, not permission to omit a newly added case. The full applicable existing hosted suite remains required by the repository overlay, including native operating-system coverage and existing benchmarks; it is not a new performance claim or an extra measurement campaign. Run each local gate once on the final candidate unless changes or diagnosed failure require fresh certification. Additional package vet gates apply if another package is actually changed within the accepted contract.

## Operating discipline

The shared `../_shared/REVIEW-LOOP.md` review-loop and `../_shared/CONTRACT-CLOSURE.md` contract-closure baselines supplied by the active architecture skills govern, composed with [the repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). These resource paths resolve relative to the architecture-handoff skill, not this repository; implementing agents use the installed skill copies. No repository contract-closure overlay exists or is needed.

The composed policy covers representation/artifact gates, finite evidence and review budgets, semantic-family closure where triggered, review-and-verification-aware precise-root stops, exact-head local certification, same-head hosted CI, squash merge, post-merge journal and pointer-based tracking. This repository runs checks for drafts and uses its existing workflows, not a portfolio ci.yml/preflight fiction. Follow the overlay and diagnose before reruns. After merge, append-dev-journal, reconcile child/parent/tracker and complete the slice task; do not complete an unimplemented parent.
