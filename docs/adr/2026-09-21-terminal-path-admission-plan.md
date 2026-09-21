# Terminal path admission implementation plan

**Date:** 2026-09-21
**Status:** Complete
**Track:** P, 1 of 3 in QGF-ARCH-20260921
**Depends on:** Nothing; this slice is on the independent frontier.
**Related:** [Program index](2026-09-21-architecture-deepening-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0006](0006-explicit-external-packet-io.md), [N01–N20](2026-09-13-architecture-decisions.md).
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions.
**Audit history:** [Slice audit and source/history dispositions](../audits/2026-09-21-architecture-handoff/README.md).

## Goal

A path is an alternative route for an existing connection. Once the application abandons it, asking to probe it again must not recreate work. Keep the repair inside the existing outgoing path manager.

## Current shape

Close-then-Probe recreates dispatchable state. Additionally, two simultaneous Close calls can both pass the initial check and both close the same channel. These are source-derived failures of the same terminal-state coordination, not two reasons to build a new subsystem.

Verified source anchors: `path_manager_outgoing.go:38`; `path_manager_outgoing.go:92`; `path_manager_outgoing.go:149`; `path_manager_outgoing.go:235`.

## Grilled decisions

### P1 · Is a redesign necessary?

No. Repair the existing manager’s admission and closure operations. A check at the beginning of Probe alone would leave the race between checking and queueing.

### P2 · Who decides whether the path is closed?

The existing manager lock serializes that decision with map and queue changes. Retain the existing abandonment signal on the Path handle; close it only while holding that lock. Do not add a second competing state flag or an ever-growing list of retired path IDs.

### P3 · What must happen together?

Initial terminal check, creation/reset of probe state, and initial queue admission form one locked operation. Closing rejects the active path first; otherwise it removes live state and pending queue entries, retires the path’s connection-ID allocation when applicable, and closes the abandonment signal exactly once. Retrying must check terminality and live membership under the same lock.

### P4 · Does Close stop every packet immediately?

No. It stops new admission and prevents pending manager work from dispatching. A frame already handed to packet emission may still be sent. Keep Close from becoming a wait-for-network-drain operation; that would need a much larger design and could delay shutdown.

### P5 · What wins if closing and probing overlap?

Whichever obtains the manager lock first determines admission. If Close wins, Probe returns ErrPathClosed without creating work. If admission wins, Close can remove its still-pending work. A probe that completed validation before closure may report success; it must not restore a closed path afterward.

### P6 · What about sender wakeups?

Keep scheduling notifications outside the lock. A notification issued late by already-admitted work is harmless when the manager has no eligible path. Do not confuse a wakeup with packet admission. A new Probe invoked after Close returns should neither admit work nor request a fresh wakeup.

### P7 · What happens on repeated or simultaneous Close?

Return success after the first successful terminal transition, without double-closing the signal or repeating retirement. Closing the currently active path continues to return the existing error and must leave that path usable.

### P8 · Is cancelling a probe the same as closing a path?

No. Preserve context cancellation as ending that wait, not permanently abandoning the path. Preserve future reprobes and previously earned switch eligibility. Do not add cancellation of every queued network action or change context-error precedence for an already-running probe.

### P9 · Could an old response complete a new probe?

Keep the existing sequential-reprobe rules: discard previous challenges and queued retries when starting the next attempt, while keeping earlier switch eligibility. Preserve the tests that distinguish old and current responses.

### P10 · Should simultaneous Probe calls gain a new contract?

No new busy error, coalescing rule, or attempt-epoch framework. Preserve the current re-fetching of wait signals; if access moves behind the manager seam, copy the current signal handles under the lock on each wait-loop iteration. Do not permanently snapshot one attempt and silently strand an older caller. Terminal Close must wake every waiter; this repair makes no new promise about which overlapping probe receives validation success.

### P11 · Does this reopen prior path and performance work?

Only the terminal-admission decision in N20. Keep migration eligibility, path generation, connection-ID policy, MTU, sender ownership, and packet emission unchanged. Existing manager callbacks remain the dependencies; no new generic lifecycle adapter is needed.

### Private interface direction

```go
// Private intent; existing public Path methods remain.
pm.beginProbe(path)          // terminal check + initial queue admission
pm.retryProbe(path)          // queue only if still live
pm.closePath(path)           // terminal state + removal + wake waiters
// Current wait signals are read under the same manager lock.
```

Names are illustrative private names. The behavior, ownership and ordering above are normative. No new public interface is authorized.

### Rejected directions — do not do this

- **Rejected: early check / sync.Once only.** An early check can race with Close. A one-time channel close avoids a panic but does not coordinate queued work and map membership.
- **Deferred: path state plus independent probe-attempt objects.** Useful if overlapping attempts become a separate supported feature. It introduces replacement and cancellation semantics that this repair does not need.

**Non-goals:** No generalized lifecycle, activation, configuration-policy, or transaction framework; no new exported types, raw-socket authority, HTTP/3/root configuration merger, performance claims, or unrelated repairs. Preserve the decisions above and the accepted source owners.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| P1 | Complete | Terminal path admission through its real entrypoints and tests | None | None introduced |

## Implementation slices

### Slice P1 — Terminal path admission

**Stable identity:** `QGF-ARCH-20260921/P1`. One intended PR; GitHub child: [#486](https://github.com/the-sarge/quic-go-fast/issues/486).

**What it delivers:** A path is an alternative route for an existing connection. Once the application abandons it, asking to probe it again must not recreate work. Keep the repair inside the existing outgoing path manager. Acceptance criteria below define the complete one-PR outcome.

**Existing-work disposition:** New slice. No open implementation PR supplies a dependency. Preserve merged behavior at the cited source; related closed repairs and open adjacent investigations are dispositioned in the linked audit. No local or unmerged branch is an accepted baseline.

**Blocked by:** None. Recommended program order is priority, not a dependency.

**Single owner after merge:** The existing outgoing path manager, under its existing mutex, owns initial/retry admission, terminal channel closure, map/queue membership and disposal. The Path handle retains terminal identity without manager tombstones. Connection-ID retirement remains with the existing ID manager; packet emission owns already-handed-off frames. Contexts retain ownership of caller cancellation.

**Authority completeness:** No new persisted fact or restart representation. Typed in-memory constructors, validation, consumers and terminal cleanup on the changed seam are included. Durable storage/restart round-trip gates are not applicable; do not invent persistence tests.

**Transitional-seam budget:** Zero new temporary seams. No dual authority, temporary adapter, migration branch, or double-open lifetime is allowed. Existing compatibility behavior is permanent supported behavior, not a temporary seam needing a later slice.

**Blast radius:** One manager mutex and existing ID callbacks; mutation of path map/queue and abandonment channel; preserved context, switch, reprobe, and packet-handoff ordering. Public methods and errors stay stable except the intended prevention of post-close work/double-close panic. No new persistent state, threads, network protocol, scheduling policy, or measured performance claim. Queue purge is bounded by existing pending work; assess complexity locally. Broader overlapping-probe semantics and connection-wide shutdown are not expanded. No identified untraced effect is accepted. A newly discovered effect outside this traced surface invokes the stop conditions.

**Artifact classification:** Runtime behavior and private refactoring are shipped behavior; the accepted admission/bounds/authority checks are required safety enforcement. Tests, fixtures and existing CI are verification aids; no new analyzer, mutation harness or maintained verification product is approved. Plans, review receipts, journal and tracking pointers are process/traceability metadata. Required proportionate regression evidence does not authorize recursive completeness requirements for its aids.

**Representation contract:** Factory-created Path handles belonging to their existing manager; supported initial/retry/close/switch operations, concurrent Close and Close-versus-probe orderings, and sequential successful reprobes. Overlapping probes retain current signal re-fetch behavior with no new success/supersession guarantee. Typed Go state and the manager lock/channel owner are authoritative; no external grammar is parsed. The enforcement guarantee is universal within this supported typed domain; finite tests are representative evidence, not an exhaustive proof. Each quantified acceptance criterion is scoped to this domain and named owner.

**Contract closure:** Not triggered. Failure has material consequences, but the changed operations and supported semantic distinctions can reasonably be covered by the ordinary focused tests and finite budget below. Multiple callers or concurrent states alone do not satisfy the shared second trigger. The table below is a focused preservation plan, not a new closure policy. If source/review evidence establishes both shared triggers, apply the shared policy within this same outcome; a required larger family is stop-for-decision.

**Evidence budget:** At most 10 new deterministic cases for close-before-probe, cancelled-probe reuse, both initial/retry admission orderings, repeated/concurrent Close, active-path rejection, pending discard, overlapping Probe waiters followed by Close, and existing reprobe preservation. Existing tests discharge already-covered cases. No mutation required; at most one optional bypass of the central terminal-admission guard if inherited evidence cannot demonstrate enforcement. One fully briefed fresh review, at most one replacement after accepted fixes. One exact-head local certification and the applicable existing hosted check suite per candidate. No extra repetitions, timing thresholds, fuzzing, platform cross-products or new hosted experiments are approved. Diagnose failure before reruns; stop when the bounded evidence passes and dispositions permit completion.

**TDD and preservation evidence:** Write the missing focused regression/characterization cases first, observe their pre-change behavior, then repair/refactor and retain relevant existing tests. Ship tests and implementation together in one green PR. No separately landed failing-test PR.

| Semantic case | Required disposition | Evidence |
| --- | --- | --- |
| Closed before new initial probe | Reject with ErrPathClosed; no new eligible work/ID/activation | Deterministic regression with post-Close scheduling baseline |
| Close competes with admitted initial/retry work | One mutex order; remove still-pending work, no terminal resurrection | Both admission/close orderings |
| Repeated/concurrent Close; active path | Close once/idempotently; reject active closure without terminal mutation | Concurrent Close plus existing active-path coverage |
| Cancellation, prior validation and reprobe | Keep cancellation distinct; preserve sticky switch eligibility and stale-response rejection | Existing reprobe tests and narrow regression |
| Overlapping Probe waiters followed by Close | All waiters terminate; signal re-fetch is race-free; no particular validation-success winner is required | One deterministic case within the ten-case budget, included in the focused race gate |
| Already handed-off frame / late wakeup | No packet recall/join promise; harmless late wakeup allowed | Source trace and no expansion of packet-emission tests |

**Dispatch context budget:** One fresh agent context. Supply this current plan (including its sole slice), program binding rules, referenced ADR clauses, shared baselines and repository overlay; then only these source ranges/files: path_manager_outgoing.go; path_manager_outgoing_test.go; connection.go:2540–2565,2862–2913; conn_id_manager.go:261–301. Target at most 24,000 input tokens (including focused test extracts), leaving the rest of the fresh context for implementation, review disposition and verification. Read only the relevant existing-work row in the linked audit; there is no unresolved prior implementation review or governing contract diff. Do not replay the five report rounds or duplicate historical plans. If additional owners or an input bundle beyond this budget become necessary, re-slice before implementation.

**Slice decision audit:** Splitting initial admission, retry admission and Close would leave one terminal invariant only partly enforced, or introduce temporary duplicate ownership. A separately merged characterization-only PR delivers no repair. Keeping this bounded owner, callers and tests in one slice is coherent. Merging configuration/lease work adds unrelated invariants without removing a dependency. There are no blocking edges: priority and shared-package test execution are not code dependencies.

**Stop conditions:** Stop for a decision if the accepted outcome, representation owner, authority, public contract, native capability scope, or one-PR/context boundary must change; if a required invariant cannot fit the declared finite evidence budget; or if the shared precise-root review/verification stop triggers after its required side-by-side comparison. No repeated-root conclusion may be based solely on a shared module or lifecycle. A failing existing test requires diagnosis; do not rerun, quarantine, relax timeouts, or absorb unrelated repairs automatically.

## Acceptance criteria

- [x] P1: No. Repair the existing manager’s admission and closure operations. A check at the beginning of Probe alone would leave the race between checking and queueing.
- [x] P2: The existing manager lock serializes that decision with map and queue changes. Retain the existing abandonment signal on the Path handle; close it only while holding that lock. Do not add a second competing state flag or an ever-growing list of retired path IDs.
- [x] P3: Initial terminal check, creation/reset of probe state, and initial queue admission form one locked operation. Closing rejects the active path first; otherwise it removes live state and pending queue entries, retires the path’s connection-ID allocation when applicable, and closes the abandonment signal exactly once. Retrying must check terminality and live membership under the same lock.
- [x] P4: No. It stops new admission and prevents pending manager work from dispatching. A frame already handed to packet emission may still be sent. Keep Close from becoming a wait-for-network-drain operation; that would need a much larger design and could delay shutdown.
- [x] P5: Whichever obtains the manager lock first determines admission. If Close wins, Probe returns ErrPathClosed without creating work. If admission wins, Close can remove its still-pending work. A probe that completed validation before closure may report success; it must not restore a closed path afterward.
- [x] P6: Keep scheduling notifications outside the lock. A notification issued late by already-admitted work is harmless when the manager has no eligible path. Do not confuse a wakeup with packet admission. A new Probe invoked after Close returns should neither admit work nor request a fresh wakeup.
- [x] P7: Return success after the first successful terminal transition, without double-closing the signal or repeating retirement. Closing the currently active path continues to return the existing error and must leave that path usable.
- [x] P8: No. Preserve context cancellation as ending that wait, not permanently abandoning the path. Preserve future reprobes and previously earned switch eligibility. Do not add cancellation of every queued network action or change context-error precedence for an already-running probe.
- [x] P9: Keep the existing sequential-reprobe rules: discard previous challenges and queued retries when starting the next attempt, while keeping earlier switch eligibility. Preserve the tests that distinguish old and current responses.
- [x] P10: No new busy error, coalescing rule, or attempt-epoch framework. Preserve the current re-fetching of wait signals; if access moves behind the manager seam, copy the current signal handles under the lock on each wait-loop iteration. Do not permanently snapshot one attempt and silently strand an older caller. Terminal Close must wake every waiter; this repair makes no new promise about which overlapping probe receives validation success.
- [x] P11: Only the terminal-admission decision in N20. Keep migration eligibility, path generation, connection-ID policy, MTU, sender ownership, and packet emission unchanged. Existing manager callbacks remain the dependencies; no new generic lifecycle adapter is needed.
- [x] Transport callers and Path methods stop coordinating separate “insert, enqueue, then notice abandonment” steps. No extra lifecycle framework survives the deletion test.
- [x] Focused evidence and affected-package validation pass at the exact pushed head, with successful applicable hosted checks on that same head and no unresolved stop-for-decision disposition.

The typed-domain representation contract above bounds every “all,” “no,” “only,” “each” and “exactly” claim in these criteria. The case table and evidence budget terminate verification; tests do not claim complete schedule enumeration.

## Validation gates

```sh
go test . -run '^TestPathManagerOutgoing' -count=1
go test -race . -run '^TestPathManagerOutgoing' -count=1
go test . -count=1
go vet .
go mod tidy -diff
```

Include the newly named regression tests in the focused selections; the commands above select existing families, not permission to omit a newly added case. The full applicable existing hosted suite remains required by the repository overlay, including native operating-system coverage and existing benchmarks; it is not a new performance claim or an extra measurement campaign. Run each local gate once on the final candidate unless changes or diagnosed failure require fresh certification. Additional package vet gates apply if another package is actually changed within the accepted contract.

## Operating discipline

The shared `../_shared/REVIEW-LOOP.md` review-loop and `../_shared/CONTRACT-CLOSURE.md` contract-closure baselines supplied by the active architecture skills govern, composed with [the repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). These resource paths resolve relative to the architecture-handoff skill, not this repository; implementing agents use the installed skill copies. No repository contract-closure overlay exists or is needed.

The composed policy covers representation/artifact gates, finite evidence and review budgets, semantic-family closure where triggered, review-and-verification-aware precise-root stops, exact-head local certification, same-head hosted CI, squash merge, post-merge journal and pointer-based tracking. This repository runs checks for drafts and uses its existing workflows, not a portfolio ci.yml/preflight fiction. Follow the overlay and diagnose before reruns. After merge, append-dev-journal, reconcile child/parent/tracker and complete the slice task; do not complete an unimplemented parent.
