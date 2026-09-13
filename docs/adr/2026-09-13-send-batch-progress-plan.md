# Send-batch defensive progress implementation plan

**Date:** 2026-09-13
**Status:** Accepted; not implemented
**Track:** B in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

`send_queue.go:65–78` specifies accepted-prefix progress. `sendBatchEntries` releases group buffers and signals availability (`:224` onward), but invalid counts are clamped to zero (`:244`) and a nil error leads to individual retries. The existing Darwin adapter already rejects malformed counts and latches batching off (`sys_conn_sendmsg_x_qual_darwin.go:168`). A scripted adapter reproduced fallback after -1 and over-offered counts; this is not a production adapter or network duplicate reproduction.

## Decision

At the send worker boundary, treat progress below zero or above the currently offered remainder length as fatal before fallback. Preserve an existing non-nil error as the fatal cause; synthesize an internal error only when invalid progress arrives with nil error. Keep zero with nil error valid for fallback. Preserve valid short-progress retry starting at the first unaccepted entry, unknown-progress no-resend behavior, group release and queued Close draining.

**Rejected alternatives — do not do this:** Do not normalize malformed progress to zero, resend uncertain data, add a platform latch here, or build a generalized send-outcome framework.

**Non-goals:** No socket backend changes, platform qualification, MTU or emission policy changes.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| B1 | New; frontier | Fail closed on impossible send-batch progress | None | None |

## Implementation slices

### B1 — Fail closed on impossible send-batch progress

**What it delivers:** Reject malformed adapter progress without per-packet fallback while releasing owned buffers and preserving valid partial progress.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** sendQueue worker owns adapter result interpretation and group buffer release; adapter-specific capability state stays with each adapter.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Worker error and retry boundaries only; preserve packet ordering, error identity, buffer release and Close drain. No new API/wire behavior; intended change is defensive handling of a contract violation.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal numeric count boundary over batchSender results: integers below 0, within [0,n], or above n, paired with nil/non-nil error. batchSender contract is authoritative; no open protocol grammar.

**Contract closure:** Not triggered: the numeric classes at one result-validation branch fit ordinary focused tests. The table records finite regression cases; it is not a semantic-closure requirement. Invariant: Reject malformed adapter progress without per-packet fallback while releasing owned buffers and preserving valid partial progress. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Negative or over-offered count, nil error | Fatal internal error, no resend, release group | sendBatchEntries | Two red cases | Required at implementation |
| Invalid count with existing error | Keep existing fatal cause and no resend | sendBatchEntries | Two cases | Required at implementation |
| Valid short or full count; unknown-progress error | Preserve existing retry/error semantics | sendBatchEntries | Existing batch tests | Required at implementation |

**Evidence budget:** Four new invalid-count cases, existing valid progress/release/Close tests; one root package run and focused race run if worker concurrency is changed. No platform or live-wire experiment; no mutation needed after baseline red. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Write invalid-count/no-resend regressions against real sendBatchEntries and existing fakeBatchSendConn; assert group releases. Run `go test . -run "^TestSendQueue" -count=1` and affected root package suite once.

**Dispatch context budget:** This slice, send_queue.go contract and worker, send_queue_test.go fake and progress tests, Darwin guard only for preservation context. One worker branch and finite numeric classes. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** No further split is coherent: error and no-resend must change together. Merging coalesced receive or platform work has no shared invariant. No blockers.

**Stop conditions:** Stop if fixing malformed results requires changing valid adapter progress semantics, emission ownership, or accepting unknown data for retry. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

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
