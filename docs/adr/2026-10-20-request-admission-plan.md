# HTTP/3 Request Admission Implementation Plan

**Date:** 2026-10-20
**Status:** Accepted after independent slice audit; not implemented
**Track:** A20-R — 1 of 5 in A20
**Depends on:** Nothing; independent slices as stated below
**Related:** [Program index](2026-10-20-architecture-deepening-program.md), [no-code decisions](2026-10-20-architecture-decisions.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Source capture and slice audit](../audits/2026-10-20-architecture-handoff/README.md)

## Goal

Make the pre-send client request-admission decision independent of qlog configuration. Keep the stream-tracking and recorder modules intact; the client module gains locality by completing admission and cleanup before exposing the request stream to sending code.

## Current Shape (verified 2026-10-20)

[`http3/client.go:152`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/client.go#L152) contains pre/post-open GOAWAY checks and calls TrackStream before constructing a RequestStream. [`http3/conn.go:121`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/conn.go#L121) owns tracking admission; [`http3/conn.go:351`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/conn.go#L351) owns qlog sealing and completion accounting. [`http3/transport.go:303`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/transport.go#L303) permits unusable-connection retry before the ordinary replay checks. [`http3/state_tracking_stream.go:41`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/state_tracking_stream.go#L41) owns tracked half-completion. Earlier real QUIC/TLS diagnostics observed different retry eligibility with qlog enabled and disabled under close-before-track scheduling; their bounded evidence is in the audit record.

## Decision

After TrackStream succeeds, perform one final connection-context lifetime check before constructing or returning RequestStream. If ended, retain the cause and cancel both directions through the tracked wrapper outside connection locks, then return the normalized cause through the existing open-error route. roundTrip retains ownership of errConnUnusable; public OpenRequestStream does not acquire that private classification. Preserve request cancellation and GOAWAY precedence. Closure after this check is an ordinary request-phase failure, not a promise of atomic admission against all later closure. Supplied connections may inherit a cancelable parent: use the same context lifetime already used by requestLifetime, without asserting that the QUIC protocol loop has terminated.

**Rejected alternative (do not do this): Change shared qlog admission or install a close callback in both logging modes.** Broadens control/server/datagram admission to fix a client pre-send decision. Keep the existing #345 owner. **Revisit if:** a separately accepted shared-admission defect.

**Rejected alternative (do not do this): Only check before tracking.** Leaves the observed closure-during-tracking window. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Reclassify errors after writing or retry regardless of body ownership.** An error after sending is not evidence that the peer never processed the request. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Cancel only the raw stream or directly edit tracking counters.** Bypasses the tracked lifetime owner and risks leaks or duplicate completion. **Revisit if:** none: rejected on merit.

**Owner's design:** Adopt the client owner’s synchronous admission operation. A broader rawConn usability gate would remove a client check but changes the shared producer contract and final-event obligations outside this outcome. The chosen operation borrows existing tracked cleanup and context lifetime without a new lock, callback, worker or duplicated state.

**Non-goals:** No retry-policy redesign, public QUIC context change, qlog replacement, server admission change, general stream lifecycle owner, response/body completion refactor or performance campaign. Preserve [#345](../agents/stream-qlog-lifetime-345.md), GOAWAY and [HTTP/3 exchange lifetime](2026-09-08-http3-lifetime-plan.md).

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| R1 | new | Reject an ended client connection after tracking | None | n/a |

## Implementation Slices

### Slice R1 — Reject an ended client connection after tracking

**What it delivers:** Make the pre-send client request-admission decision independent of qlog configuration. Keep the stream-tracking and recorder modules intact; the client module gains locality by completing admission and cleanup before exposing the request stream to sending code.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None.

**Single owner after merge:** ClientConn owns the final pre-send decision; stateTrackingStream retains half-completion and rawConn retains map/work-count/recorder ownership. No owner is duplicated.

**Authority completeness:** No persisted fact, schema, public configuration or restart format becomes authoritative. Existing constructors and QUIC/HTTP parsers retain their contracts; this slice changes only the named runtime owner and its supported consumers.

**Transitional-seam budget:** None introduced. No compatibility adapter, duplicate mutable representation or double-open lifetime is permitted; there is no successor cleanup slice.

**Blast radius:** Changes only the pre-request admission result and cleanup. Trace normal/0-RTT request-stream callers, public raw/client entrypoints, cancellation/GOAWAY, supplied parent contexts, the Transport classifier and untouched body ownership. No wire format, persisted state, server lifetime or additional goroutine changes. All affected paths are named; any newly discovered independent admission route is a stop until traced.

**Artifact classification:** Runtime behavior is shipped behavior; its selected guard/cleanup is required safety enforcement. Regression tests and temporary diagnostics are verification aids, with no general maintained harness approved. This plan, its issue/task pointers and review receipts are process metadata.

**Representation contract:** Domain: existing ClientConn/RawClientConn OpenRequestStream and Transport RoundTrip paths, with connection lifetime ended before the new final check and request input not yet sent. Standard-library/QUIC/HTTP parsers own representations; the client check owns the internal lifetime decision. Universal method-local invariant in this supported domain, evidenced by bounded schedules rather than exhaustive concurrency proof.

**Contract closure:** Not triggered. Cleanup consequences are material, but the accepted change is one synchronous client admission check using unchanged tracking owners, and the finite focused cases below reasonably cover its supported routes and precedence states.

**Evidence budget:** One live admission control; ended-before-final-check with qlog off/on; full Transport redial with untouched non-rewindable input; public raw/client supplied-context behavior; canceled-request and GOAWAY precedence; one later-write-failure preservation case. Reuse existing qlog final-event cases. At most eight named semantic cases, with qlog as the one justified paired axis; run focused, package and race gates once. No guard mutation is required because the new failing regression directly exercises the missing check. One review plus at most one replacement.

**TDD and preservation evidence:** Start with the qlog differential and a full redial assertion through Transport. Then characterize public/supplied entrypoints and the existing precedence cases before inserting the check. Keep existing body cleanup and final-event tests.

**Dispatch context budget:** Current R1 section, this plan’s decision and matrix, #345 current contract, client.go open/roundTrip, conn.go TrackStream/clearStream/admission, state_tracking_stream.go, transport.go retry path and relevant request/GOAWAY tests. Dispatch briefing target at most 16k tokens of selected source/context; governing diff limited to those HTTP/3 paths and tests. Read current code, not complete historical reports; no unresolved implementation review history.

**Slice decision audit:** Splitting characterization from repair would leave the accepted broken behavior and no independent delivered outcome; keep the failing test and repair together. Merging with header or protocol work adds different owners and outcomes; reject both alternatives, revisit none: rejected on merit. There are no genuine prerequisite slices. Compare the owner’s synchronous design above; qlog-lock admission is rejected on scope, not line count.

**Stop conditions:** Stop if the check requires changing rawConn sealing, retry/body rules, root context semantics, server admission or public ownership; if tracked cleanup cannot discharge the existing work obligation without new state; or if the real Transport test cannot establish untouched input under the bounded schedule. Apply the shared precise-root stop if review and verification establish it.


## Finite Semantic Coverage

| Semantic class | Disposition | Enforcement owner | Evidence |
| --- | --- | --- | --- |
| Live connection at final check | Admit unchanged | ClientConn | Live control |
| Ended before final check, qlog on/off | Cleanup tracked stream and return cause | ClientConn; existing tracking cleanup | Paired diagnostic-derived regression |
| Transport untouched input | Existing unusable retry, one redial | roundTrip and existing Transport classifier | Real second-dial regression |
| Public/supplied client connection | Same normalized cause and context lifetime, no private retry wrapper | ClientConn public opening path | Public/supplied case |
| Request canceled or GOAWAY | Preserve precedence and existing refusal | Existing owners | Existing cancellation/GOAWAY cases |
| Closure after admitted write begins | No new replay permission | Existing request lifetime/retry classifier | Write-phase preservation case |
| Already admitted recording | Final events finish before recorder closure | rawConn | Existing #345 tests |

Evidence status is planned, not yet run for an implementation. Completion means satisfying this terminating evidence plan, not enumerating arbitrary syntax or schedules.

## Acceptance Criteria

- [ ] Within the supported pre-send client-admission domain, a connection whose context is already ended at the final post-tracking check is rejected before request sending in both logging modes; preserve the existing unusable-connection route for untouched input.
- [ ] Tracked cleanup for that rejected attempt removes its own stream entry and balances its existing work obligation; no new shared qlog state or direct counter mutation is introduced.
- [ ] A full Transport regression observes one new dial and a successful retried untouched non-rewindable request; cancellation and GOAWAY retain their existing results and later write-phase failures do not gain new replay permission.
- [ ] Public raw/client request-stream entrypoints preserve their error surface, admitted final events, body ownership and supplied-connection context semantics.

Each universal statement is limited to the slice’s declared domain and owner, with the guarantee and terminating evidence in that slice. No broader protocol, concurrency or platform certification is implied.

## Validation Gates

Write TestRequestAdmissionAfterTracking first, using real QUIC/TLS and bounded test-owned scheduling rather than a production hook. Cover the full Transport redial and public/supplied connection variants with the finite cases below. Run `go test ./http3 -run "TestRequestAdmissionAfterTracking|TestClientConnGoAway|TestQlogLifetime" -count=1`, `go test ./http3 -count=1`, `go test -race ./http3 -count=1`, `go vet ./http3`, and `go mod tidy -diff` at the candidate head, plus existing hosted checks. No extra repetitions, native cross-product or mutation campaign.

## Operating Discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern, composed with the existing [repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). The shared files are `REVIEW-LOOP.md`, `CONTRACT-CLOSURE.md`, and `DESIGN-COST.md` in the active architecture skill package’s sibling `_shared/` directory. Use those baselines directly; do not copy them into this repository. No repository-specific contract-closure overlay exists or is needed for this program.

Apply representation and artifact gates, independently disposition findings, and use one initial fresh review plus at most one replacement after accepted fixes. Verification is limited to accepted source findings. Apply the shared precise-root comparison and approach stops; a common package or lifecycle topic does not establish a repeated root. Do not grow the evidence plan to exhaust reviewer suggestions or recursively certify fixtures. Any replacement design must be compared by obligations, contract changes, then footprint.

Use a dedicated worktree and feature branch. Certify the exact pushed head using the focused gates below and the repository overlay; require applicable existing hosted checks on that head, then mark ready and squash merge with a matching head. This repository has no `task preflight` or `ci-*` workflow. Diagnose failed checks before reruns; only demonstrated infrastructure failure permits a same-head rerun. After the primary work reaches remote main, run `append-dev-journal` without RAS, revalidate deferred findings against the merge, and reconcile issue and OmniFocus pointers. Plan/mirror state does not substitute for code-head certification.

This document packages future work. The handoff run does not implement or dispatch it. Changes to representation ownership, accepted outcome, topology, adjacent scope, irreversible authority, or the one-PR boundary require a decision or a new scoped architecture handoff.
