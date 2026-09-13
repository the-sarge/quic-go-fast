# Receive STREAM lifetime implementation plan

**Date:** 2026-09-13
**Status:** Complete; R1, R2 and R3 delivered
**Track:** R in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

`ReceiveStream.cancelReadImpl` signals local cancellation before preserving an earlier remote reset error, so Read and Peek waiting for the reliable prefix wake without further traffic. Parser failures, skipped connection dispatch, stream-map lookup rejection and ReceiveStream admission rejection now dispose incoming owning STREAM frames; successful handoffs transfer ownership to the next concrete owner. The wire parser copies STREAM data into its own frame allocation (`internal/wire/stream_frame.go:59–84`); this lifetime is distinct from incoming UDP storage. `frameSorter.Push` consumes its incoming callback on every outcome, including distinct duplicate, trimming-copy and first gap-limit rejection paths. The current first-error lifecycle does not establish that trimming-copy and gap-limit failure occur together. Keep disposal authority inside the sorter rather than encoding assumptions about internal callback consumption at callers. ReceiveStream now takes and clears consumed callbacks, retires current and gapped queued storage at terminal transitions, and seals late STREAM storage admission after existing final-size validation. Retirement preserves consumed EOF while clearing the final-frame flag for discarded unread FIN data.

## Decision

Keep successive concrete owners and the existing ReceiveStream mutex. Separate wakeup, incoming handoff, and stored-data retirement into three independently green PRs. Cancellation must wake existing waiters while preserving the first error and control-frame semantics. A consuming handoff transfers or disposes ownership on each return path, without assuming `PutBack` is idempotent. Terminal retirement takes and clears callbacks before invocation, disposes queued entries including gaps, and seals storage admission while preserving required final-size validation and already-observable EOF. Preserve readable reliable-prefix bytes until consumed or locally abandoned. Flow-control Abandon is distinct from storage retirement. No new persisted representation is introduced.

**Rejected alternatives — do not do this:** Do not make caller error-based PutBack the ownership contract; the sorter owns whether callbacks were consumed or transferred on its internal paths. Do not require an unreachable trim-copy-then-first-gap-error test or reuse a sorter after its fatal error. Do not identify copied STREAM storage with the UDP slab. Do not clear unread FIN data while leaving a flag that manufactures EOF. Do not silently clamp reads at reliableSize as part of cleanup, reset error/completion state merely to free memory, or change crypto stream semantics.

**Non-goals:** No stream API, scheduler, flow-control algorithm, reset semantics redesign, global receive owner, pool instrumentation framework, retained-packet ownership change, or unbounded-memory-leak claim.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| R1 | Complete | Wake readers when local cancellation follows partial reset | None | None |
| R2 | Complete | Consume incoming STREAM frame ownership through dispatch | None | None |
| R3 | Complete | Retire stored STREAM data at terminal transitions | None | None |

## Implementation slices

### R1 — Wake readers when local cancellation follows partial reset

**What it delivers:** Make a blocked Read or Peek observe local cancellation without requiring more traffic or a deadline, including after a remote partial reset notification was already consumed.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** ReceiveStream.cancelReadImpl under ReceiveStream.mutex owns the local-cancel transition and waiter notification. Existing first-error, STOP_SENDING, Abandon, and completion owners remain.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Only cancellation signaling and colocated tests. Preserve remote StreamError identity/code, no duplicate STOP_SENDING, one completion notification, duplicate cancellation, shutdown and established EOF behavior. No wire/API or buffer-layout change; no untraced effects accepted.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal local-cancel wakeup contract over existing ReceiveStream states and its Read/Peek entrypoints; representation owner is ReceiveStream state under its mutex. Protocol parsing remains existing wire code. Evidence is semantic regression coverage, not exhaustive scheduling proof.

**Contract closure:** Not triggered for R1: one cancel transition is covered by focused Read/Peek regressions and existing terminal guards. The table is ordinary finite preservation evidence, not a closure requirement. Invariant: Make a blocked Read or Peek observe local cancellation without requiring more traffic or a deadline, including after a remote partial reset notification was already consumed. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Partial remote reset with missing reliable bytes; Read and Peek waiting | Wake and return existing remote error | cancelReadImpl | Two regressions | Covered |
| Fresh local cancel / repeated cancel / shutdown / already observed EOF | Preserve first error, control frame and completion semantics | cancelReadImpl and existing terminal guards | Existing cancellation tests plus one table only if coverage absent | Covered |

**Evidence budget:** First write two red subtests (Read, Peek) using final size 10, reliable size 6, missing bytes, remote code 7 and local code 9. At most four preservation rows if existing cases are insufficient. One affected-package run and one focused race run; no repeat count or wall-clock SLA. No mutation needed because baseline red demonstrates the missed guard. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Use deterministic waiting/synctest and real ReceiveStream. Verify the pre-fix failure without depending on arbitrary timeouts; ensure helper goroutines terminate. Run `go test . -run "^TestReceiveStream" -count=1` and the same selector with `-race`.

**Dispatch context budget:** This slice, track invariants, current receive_stream.go and relevant receive_stream_test.go cases, and the two frozen diagnostic subtests only. Roughly one source file and one test file; no previous full report required. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Splitting Read and Peek would duplicate one transition fix. Merging storage cleanup would mix a demonstrated wakeup defect with pool lifetime risk. No blocker is necessary: this works with current storage behavior.

**Stop conditions:** Stop if waking requires replacing the retained remote error, additional STOP_SENDING, a new scheduler, or dependence on a storage refactor. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Acceptance criteria:**

- [x] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [x] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [x] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

### R2 — Consume incoming STREAM frame ownership through dispatch

**What it delivers:** Close owning STREAM frame exits from parser allocation through packet processing, stream lookup, receive admission and sorter insertion; transfer or dispose the callback once on each outcome.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** At any moment one successive owner: parser until successful return; connection until handoff; stream map until handoff; ReceiveStream until Push; frameSorter.Push owns its incoming callback on every outcome. Existing sorter entries own retained callbacks until replacement or Pop.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Wire parser failure paths, connection skip path, stream map lookup, ReceiveStream validation, sorter callback paths and tests; crypto Push passes nil and must remain valid. Preserve parse errors/order, qlog metadata snapshots, final-size validation, duplicate/overlap behavior and stream completion. No public API, schema or UDP pool change. Callback double return is material corruption risk.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal ownership contract over actual owning wire.StreamFrame values and existing frameSorter.Push callers, with authoritative wire parser and concrete callback handoffs. Non-pooled PutBack stays a no-op; pooled PutBack is non-idempotent. The guarantee is enforced by ownership at each seam, with finite class evidence rather than syntax enumeration.

**Contract closure:** Triggered: the lifecycle/ownership or retry failure is material and independently reachable states cross the bounded enforcement seams below. Invariant: Close owning STREAM frame exits from parser allocation through packet processing, stream lookup, receive admission and sorter insertion; transfer or dispose the callback once on each outcome. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Parser capacity and offset-overflow failures after allocation | Dispose before returning original error | wire parser | One case per post-allocation error | Covered |
| Earlier packet-frame failure skips a subsequently parsed STREAM | Snapshot qlog metadata then dispose | connection packet loop | One skipped STREAM case | Covered |
| Lookup error or deleted stream | Dispose without handing off | stream map | One case per disposition | Covered |
| Shutdown, local cancel or highest-received/final-size rejection | Dispose or transfer without changing validation | ReceiveStream | One per reachable early-return class | Covered |
| Accepted unique data / duplicate / replaced entry | Retain or release at the sorter owner | frameSorter.Push | Existing sorter cases with callback counts | Covered |
| Trim-to-copy success; ordinary first gap-limit failure | Release copied-away input on success; release rejected incoming data on first fatal error | frameSorter.Push internals | One trimming success and one gap-limit failure | Covered |
| Crypto nil callback | Preserve behavior | frameSorter.Push | Existing crypto tests | Covered |

**Evidence budget:** At most 12 new focused ownership cases across the seven rows, reuse existing sorter tests. Callback counts at sorter seams; for concrete wire.StreamFrame.PutBack at parser/map/stream exits, use bounded temporary test-only observation of the release seam if needed, removed before merge. Do not infer release from sync.Pool reuse or GC timing, and do not add permanent production callbacks solely for testing. The post-allocation overflow case must use a pooled frame; the existing tiny nonpooled overflow case does not exercise pool return. No global pool tracker. One root/internal-wire/crypto affected-package run, one focused race run where concurrent ReceiveStream handoff is exercised. No mutation campaign, fuzz corpus or parser aliases. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Write failing release-count tests first for post-allocation errors, skipped dispatch, early returns and distinct trimming-success/gap-error outcomes. Then run `go test . ./internal/wire -count=1`; run `go test -race . -run "TestReceiveStream|TestFrameSorter|TestCryptoStream" -count=1`.

**Dispatch context budget:** This slice and track invariants; the six named source files, wire pool contract, connection_logging.go snapshot, crypto_stream.go Push caller and nearby tests. One finite ownership chain, not whole connection architecture. Expected implementation plus focused review fits one fresh context; stop if new owners emerge. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Splitting parser cleanup is independently possible but would leave the accepted incoming-chain outcome incomplete across multiple audit handoffs; this bounded chain fits one PR. Merging terminal retirement would add a separate state machine. No R1 prerequisite: callback ownership does not require changing cancellation notification.

**Stop conditions:** Stop if preserving existing sorter behavior requires caller error-based disposal, another shared owner, a new public parser contract, or if the six-file handoff cannot be reviewed in one context. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Implementation evidence:** [R2 ownership receipt](../audits/2026-09-13-architecture-handoff/r2-ownership-evidence.md).

**Acceptance criteria:**

- [x] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [x] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [x] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

### R3 — Retire stored STREAM data at terminal transitions

**What it delivers:** Release current and queued frame storage when it becomes unreadable, without altering read errors, established EOF, reliable-prefix delivery, final-size accounting or completion notifications.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** ReceiveStream under its mutex owns the terminal predicate and current callback. frameSorter owns queued callbacks and its private discard operation. Pop transfers a callback to ReceiveStream; cleanup takes and clears it before invocation.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Read/Peek dequeue/resume and EOF, CancelRead, remote reset including reliable-size reduction, shutdown, and late STREAM admission. Preserve existing reads that may copy beyond reliableSize before checking it; do not introduce a clamp. Empty terminal Pop remains safe even when a waiter resumes directly into dequeue. Crypto use of sorter remains unchanged.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal terminal-disposal contract over existing ReceiveStream current/queued copied STREAM storage states, authoritative ReceiveStream state and sorter entries. No API promises about global pool occupancy or garbage-collection timing. Storage retirement does not create new persisted state.

**Contract closure:** Triggered: the lifecycle/ownership or retry failure is material and independently reachable states cross the bounded enforcement seams below. Invariant: Release current and queued frame storage when it becomes unreadable, without altering read errors, established EOF, reliable-prefix delivery, final-size accounting or completion notifications. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Normal consumption and actual EOF | Take and clear current callback; retain previously observable EOF | ReceiveStream current-release seam | Consume/repeat EOF regression | Covered |
| Local cancel or shutdown with current unread FIN and gapped queue | Discard storage; clear misleading final-frame flag; preserve intended error | ReceiveStream terminal retirement and sorter discard | One local-cancel and one shutdown case | Covered |
| Remote reset with unread reliable prefix | Retain data still readable | ReceiveStream terminal predicate | Prefix delivery preservation | Covered |
| Prefix consumed or lower duplicate reliableSize makes reset effective | Retire remaining storage without extra completion | ReceiveStream reset/read transitions | One consumption and one reduced-prefix case | Covered |
| Late STREAM after terminal retirement | Preserve required highest-received/final-size validation; dispose without repopulation | ReceiveStream admission | Valid late data and conflicting final size | Covered |
| Repeat cleanup and waiter resumption after queue discard | No double callback; safe empty Pop; same terminal outcome | Current-release and sorter discard | Repeated cleanup/resumption case | Covered |

**Evidence budget:** At most 10 new semantic cases, using callback counts and existing real-stream tests; one root package run and one focused race run. No memory benchmark or GC timing assertion. At most one guard bypass if inherited coverage alone must demonstrate the late-admission guard; otherwise no mutation. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Write red retained-callback and unread-FIN error-precedence tests before cleanup. Run `go test . -run "TestReceiveStream|TestFrameSorter|TestCryptoStream" -count=1` and with `-race`. Final affected root package suite once.

**Dispatch context budget:** This slice, common track invariants, receive_stream.go, frame_sorter.go and their terminal/reset tests. Include the current incoming callback contract (R2 only if already merged), not R2 history. One terminal lifecycle and two production files. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Splitting current and queued disposal would leave partially retired state and duplicate terminal predicates. Merging R1/R2 adds distinct ownership transitions. No hard dependency: implement against the current Push contract and release terminally rejected frames directly before Push. Prefer R1→R2→R3 to reduce overlapping-file conflicts, but R3 can merge green first without taking on R1/R2 defects.

**Stop conditions:** Stop if cleanup cannot preserve first error and already-observed EOF, requires reusing a discarded sorter, drops unread reliable bytes, changes flow credit/completion, or depends on an unmerged handoff change. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

**Implementation evidence:** `TestReceiveStreamReleasesConsumedStorage`, `TestReceiveStreamRetiresCancelledStorage` (four cases), `TestReceiveStreamRetiresReliablePrefixStorage` (two cases, including a blocked Peek resumed by prefix reduction), `TestReceiveStreamRetiredStorageRejectsLateData`, and `TestReceiveStreamRetiredStorageResumesWaiter` (Read/Peek) cover the ten-case budget. Existing reset, flow-control, completion, EOF and crypto tests remain preservation evidence.

**Acceptance criteria:**

- [x] Deliver the behavior stated in this slice's What it delivers field at its named owner.
- [x] Preserve the explicitly listed existing behavior and satisfy the finite evidence budget.
- [x] Introduce no temporary second owner or unapproved public/API/storage representation change.

Universal wording in these criteria is bounded by this slice's Representation contract and semantic classes; no external syntax or unknown consumer census is implied.

## Validation gates

Use each slice’s named focused commands and finite evidence. Follow the execution overlay for exact-head local certification and applicable hosted CI. Record actual test names if current naming differs, and verify selectors execute tests. Do not claim native behavior from compilation alone.

## Operating Discipline

The shared [review-loop baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md) and [contract-closure baseline](/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md), supplied by `$implement-architecture-slice`, govern directly. Apply the repository-specific [execution overlay](../REVIEW-LOOP.md), [maintained conventions](../agents/conventions.md), and ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md), and [0005](0005-incoming-packet-lifetime.md). Do not seed a repository copy of either shared baseline.

One fully briefed initial review and at most one replacement review; independently disposition findings before fixes. Stop on a representation mismatch, an established repeated precise root, or required boundary expansion. Compare exact invariant, concrete enforcement seam, semantic classes, and why the earlier accepted family owned the later case before calling findings one repeated root. No recursive proof obligations on tests or frozen diagnostics. No new performance campaign, random stress expansion, unsupported platform cross-product, or timing SLA. Ordinary focused regression tests remain required evidence of the accepted behavior; no new verification framework is a maintained blocking deliverable.

After the exact reviewed head passes local certification and applicable existing hosted checks, squash merge, append the development journal, reconcile issue/frontier pointers, and complete the corresponding OmniFocus slice task. Do not complete the program parent until its actual children are complete. Existing open investigations and completed programs keep their scopes.
