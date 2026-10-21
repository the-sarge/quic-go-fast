# Initial Request-Header Ownership Implementation Plan

**Date:** 2026-10-20
**Status:** Accepted after independent slice audit; not implemented
**Track:** A20-H — 4 of 5 in A20
**Depends on:** Nothing; independent slices as stated below
**Related:** [Program index](2026-10-20-architecture-deepening-program.md), [no-code decisions](2026-10-20-architecture-decisions.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Source capture and slice audit](../audits/2026-10-20-architecture-handoff/README.md)

## Goal

Deepen the server’s concrete initial-request-header operation so no charged buffer, lazy decoder or release obligation escapes to the outer handler. Preserve the current role outcomes and keep timer/activation outside.

## Current Shape (verified 2026-10-20)

[`http3/server_conn.go:213`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/server_conn.go#L213) calls the capacity-charged reader and releases partial capacity after a read failure; [`http3/server_conn.go:232`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/server_conn.go#L232) consumes lazy decoding before release, qlog and refusal. [`http3/server_conn.go:254`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/server_conn.go#L254) separately prevents activation when headerTimer.Stop loses. [`http3/server_conn.go:369`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/http3/server_conn.go#L369) grows the charged buffer as bytes arrive. Existing real-stream budget and timeout tests protect these behaviors. N01 allows a concrete role operation after repeated real changes while rejecting a generic callback bundle.

## Decision

Enter a private request-role operation after first-frame HEADERS validation. It owns declared-length validation, capacity-charged reading, lazy decoder consumption and role outcomes, returning a decoded request or nil after a handled failure. An inner lexical read/decode scope releases the charge before logging, resets or a potentially blocking 431 write. The outer handler retains timer creation, the post-decode Stop/activation gate, caller deadlines, body/application lifetime, TLS/address decoration and idle bookkeeping. Preserve actual current error dispositions, including an explicit future QPACK dependency change rather than pretending P1/P2 correct initial-header compression errors.

**Rejected alternative (do not do this): Defer release to the whole request handler or refusal operation.** Retains quota through application execution or blocked logging/output. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Return raw bytes, a lazy iterator or release token.** Leaves the caller’s lifetime obligation in the interface. **Revisit if:** none: rejected on merit.

**Rejected alternative (do not do this): Unify request/response/trailer readers through callbacks.** Different quotas, error policy and partial-EOF semantics make the interface shallow. **Revisit if:** a separately accepted shared-role change with concrete duplication pressure.

**Rejected alternative (do not do this): Move the timeout activation decision into decoding or delete it.** Successful reads do not prove that timeout cancellation has not already won. **Revisit if:** none: rejected on merit.

**Owner's design:** Adopt the server role’s synchronous complete-lifetime operation. A local read/decode closure alone solves release pairing but leaves role choreography with the outer handler. A stage enum returned for the caller to map is credible but keeps those decisions split; the concrete handled-outcome operation removes more obligations without callback machinery. No new execution context or public interface.

**Non-goals:** No aggregate trailer quota, decoder replacement, protocol error policy change, application lifetime change, shared response/trailer reader, generic quota token or qlog lifetime redesign. Preserve N01 and [header logging contracts](2026-09-13-architecture-decisions.md#n01--standalone-headers-consolidation).

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| H1 | new | Own the initial request header charge through lazy decode | None | n/a |

## Implementation Slices

### Slice H1 — Own the initial request header charge through lazy decode

**What it delivers:** Deepen the server’s concrete initial-request-header operation so no charged buffer, lazy decoder or release obligation escapes to the outer handler. Preserve the current role outcomes and keep timer/activation outside.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None. Recommended after protocol repairs for review clarity, but P1/P2 affect body/trailer paths and are not a technical prerequisite for this independently green initial-header refactor.

**Single owner after merge:** RawServerConn’s concrete header-role operation owns temporary quota lifetime and its existing role dispositions; the outer request handler owns timer/activation and application lifetime. pendingHeaderBytes remains the existing shared atomic ledger.

**Authority completeness:** No persisted fact, schema, public configuration or restart format becomes authoritative. Existing constructors and QUIC/HTTP parsers retain their contracts; this slice changes only the named runtime owner and its supported consumers.

**Transitional-seam budget:** None introduced. No compatibility adapter, duplicate mutable representation or double-open lifetime is permitted; there is no successor cleanup slice.

**Blast radius:** Synchronous extraction in server_conn.go with quota release timing, decoder consumption, qlog and refusal I/O. No new worker, lock, decoder abstraction, protocol policy or persisted data. Preserve capacity rather than length charging and the timer.Stop gate; preserve short-circuit semantic decoding. If preserving timing requires a generalized lease abstraction, stop.

**Artifact classification:** Runtime behavior is shipped behavior; its selected guard/cleanup is required safety enforcement. Regression tests and temporary diagnostics are verification aids, with no general maintained harness approved. This plan, its issue/task pointers and review receipts are process metadata.

**Representation contract:** Existing initial request HEADERS frames parsed by frameParser and qpack. Those owners retain wire grammar; the concrete request role owns charge lifetime. Universal role-local ownership/release invariant over that domain, with finite failure/success tests rather than arbitrary scheduler or protocol-language proof.

**Contract closure:** Not triggered. Quota and activation consequences are material, but the accepted change is a synchronous ownership extraction with unchanged ledger and timer owners, and the finite focused cases below reasonably cover release and activation behavior.

**Evidence budget:** Reuse budget exhaustion/recovery, cancellation, malformed decode, size refusal and allocation cases; add at most two missing timing cases for release before blocked qlog or refusal. The blocked-qlog case must also cover the post-successful-decode timer gate: valid headers decode, qlog blocks, charge is already zero, the configured timeout/reset occurs, logging resumes, and handler activation remains absent. Existing timeout tests with incomplete input do not cover this gate. One ordinary package and race run. At most one temporary release-bypass mutation if inherited coverage is the only discriminator; no new maintained mutation harness. One initial review plus at most one replacement.

**TDD and preservation evidence:** Before extracting, identify existing cases that establish release after lazy decode and before potentially blocked continuation. Use the blocked-qlog timing case to verify both early charge release and no activation after timeout wins following successful decode; existing incomplete-header timeout cases do not discharge that second invariant. Then move the complete lifetime while keeping the outer timer gate; preserve existing wire dispositions exactly.

**Dispatch context budget:** Current H1, N01, server_conn.go handleRequestStream/readHeaderBlock and role tests, requestFromHeaders/parseHeaders lazy behavior, qlog scoped-work contract and allocation case. At most 16k context tokens and a server-role/test diff. No full historical designs or unrelated body cleanup.

**Slice decision audit:** Splitting charge/release from role extraction leaves caller coordination temporarily duplicated without an independent outcome. Merging protocol changes would mix policy and refactor. Reject both, none: rejected on merit. Protocol-first is recommended sequencing, not a blocker: the code roles and preservation tests permit either independently green order. Caller-stage enum rejected on retained obligations, not footprint.

**Stop conditions:** Stop on changed quota timing, decoder ownership, error policy, caller deadlines, public scope, timeout activation or inactivity behavior; if raw buffers/cleanup tokens must escape; or if the initial role cannot remain separate from response/trailer policy.


## Finite Semantic Coverage

| Semantic class | Disposition and owner | Evidence |
| --- | --- | --- |
| Successful lazy decode | Role releases capacity, then logs; outer handler may activate | Existing budget success and partial logging |
| Partial read/cancellation or aggregate refusal | Role releases acquired capacity and preserves role result | Existing budget/cancellation cases |
| Decode/semantic failure or size refusal | Role consumes until failure, releases before log/refusal | Invalid decode, semantic and size tests |
| Blocked qlog or 431 continuation | Charge already released | At most two focused timing cases |
| Timeout has won despite successful decode | Outer timer gate forbids activation | New valid-header blocked-qlog case: timeout/reset before recorder release, then no handler activation |
| Response and trailer roles | Explicit non-goal; existing behavior retained | Affected package regression suite |

Evidence status is planned, not yet run for an implementation. Completion means satisfying this terminating evidence plan, not enumerating arbitrary syntax or schedules.

## Acceptance Criteria

- [ ] For the existing initial request HEADERS role, the outer handler receives only a decoded request or handled failure; no buffer charge, lazy decoder or cleanup token crosses that seam.
- [ ] Capacity acquired on success, partial read failure, aggregate refusal or decode/semantic failure is released at the existing point after decode/read termination and before qlog or refusal I/O.
- [ ] Encoded/decoded limits, partial-field logging, qlog-disabled allocation behavior, caller deadlines, initial-header inactivity and timeout-stop-before-activation remain intact.
- [ ] No protocol disposition, response/trailer quota, public interface or application lifetime is changed by this refactor.

Each universal statement is limited to the slice’s declared domain and owner, with the guarantee and terminating evidence in that slice. No broader protocol, concurrency or platform certification is implied.

## Validation Gates

Preserve TestRawServerConnPendingHeaderBudget, TestRawServerConnHeaderDeadlineWithActiveRequest, TestRawServerConnReadHeaderTimeoutConfig, TestHeaderCollectionAllocations and role logging tests. Write a focused release-before-blocked-qlog/refusal case only where the existing suite cannot distinguish the moved lifetime. Run `go test ./http3 -count=1`, `go test -race ./http3 -count=1`, `go vet ./http3`, `go mod tidy -diff`, and applicable hosted checks. No benchmark campaign or parser fuzz expansion.

## Operating Discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern, composed with the existing [repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). The shared files are `REVIEW-LOOP.md`, `CONTRACT-CLOSURE.md`, and `DESIGN-COST.md` in the active architecture skill package’s sibling `_shared/` directory. Use those baselines directly; do not copy them into this repository. No repository-specific contract-closure overlay exists or is needed for this program.

Apply representation and artifact gates, independently disposition findings, and use one initial fresh review plus at most one replacement after accepted fixes. Verification is limited to accepted source findings. Apply the shared precise-root comparison and approach stops; a common package or lifecycle topic does not establish a repeated root. Do not grow the evidence plan to exhaust reviewer suggestions or recursively certify fixtures. Any replacement design must be compared by obligations, contract changes, then footprint.

Use a dedicated worktree and feature branch. Certify the exact pushed head using the focused gates below and the repository overlay; require applicable existing hosted checks on that head, then mark ready and squash merge with a matching head. This repository has no `task preflight` or `ci-*` workflow. Diagnose failed checks before reruns; only demonstrated infrastructure failure permits a same-head rerun. After the primary work reaches remote main, run `append-dev-journal` without RAS, revalidate deferred findings against the merge, and reconcile issue and OmniFocus pointers. Plan/mirror state does not substitute for code-head certification.

This document packages future work. The handoff run does not implement or dispatch it. Changes to representation ownership, accepted outcome, topology, adjacent scope, irreversible authority, or the one-PR boundary require a decision or a new scoped architecture handoff.
