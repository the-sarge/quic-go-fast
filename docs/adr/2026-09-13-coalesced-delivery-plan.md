# Coalesced receive delivery implementation plan

**Date:** 2026-09-13
**Status:** Complete
**Track:** C in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

Unix `sys_conn_oob.go:151–159,260–305` and Windows `sys_conn_windows.go:159–167,192–227` repeat pending-view pop/clear, coalesced slab adoption and remaining-view cleanup. The splitter is already shared in `coalesced_slab.go:61–85`. Unix cleanup additionally owns unread preposted native-batch buffers; Windows does not. `getCoalescedSlab` allocates backing storage, so using it after a filled receive would add a copy/allocation.

## Decision

Introduce one private concrete per-reader holder with next/accept/discard operations. accept consumes the already-filled coalesced buffer and requires no pending prior delivery. Initialize the full sibling reference count before publishing the first view, copy packet metadata by value, and preserve sender reference, time, ECN and packetInfo. Nonempty coalesced/offload delivery uses a slab even for absent metadata or one segment; the holder normalizes nonpositive segment size to the whole nonempty payload before calling split. This normalization currently belongs to platform readers; coalescedSlab.split itself rejects nonpositive sizes. Preserve empty and non-offload bypass. discard releases only pending siblings, never a previously returned view. Holder mutation stays reader/post-read-cleanup confined; only slab reference count is atomic across consumers. Retain front-slice representation initially.

**Rejected alternatives — do not do this:** Do not allocate a second slab backing store, introduce a ring/cursor project, move Unix native batch cleanup into the shared holder, unify syscalls/capability policy, or claim new throughput gains. Sharing a splitter alone does not close repeated delivery lifetime logic.

**Non-goals:** No receive engine, general atomic storage, batch-depth/retention-budget change, new private syscall, D2 revival, two-core qualification or performance campaign. Existing Windows W3/shared-fixture and incoming I7 scopes remain closed except this narrow optional consolidation.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| C1 | Complete | Share coalesced receive pending-view lifetime | None | None |

## Implementation slices

### C1 — Share coalesced receive pending-view lifetime

**What it delivers:** Use one holder for nonempty coalesced buffer adoption, ordered view delivery and pending-view disposal on both platform implementations.

**Existing-work disposition:** Completed slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** Concrete per-reader holder owns pending views; returned views own their slab references independently; existing coalescedSlab owns atomic shared backing lifetime. Native readers keep syscall and native-buffer owners.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Unix/Windows wrappers, metadata, sibling ordering and cleanup concurrent with already-returned releases. Preserve Unix preposted buffer cleanup. No public API/protocol/storage-budget change; allocation preservation forbids second backing allocation.

**Artifact classification:** Runtime changes are shipped behavior; ownership, wakeup and rejection guards that enforce the accepted invariant are required safety enforcement. Tests and existing fixtures are verification aids. Plan, audit, frozen diagnostics and issue/task pointers are process or traceability metadata (diagnostic source remains a non-maintained verification aid). No new maintained blocking verification-aid exception is approved.

**Representation contract:** Universal holder contract over nonempty buffers and positive splitter segment sizes owned by coalesced_slab and nonpositive-to-whole-payload normalization transferred from the readers into the holder; existing native parsers own control-message syntax. Supported domain includes singleton, multiple and short-tail views. Empty/nonoffload input stays outside holder admission.

**Contract closure:** Triggered: the lifecycle/ownership or retry failure is material and independently reachable states cross the bounded enforcement seams below. Invariant: Use one holder for nonempty coalesced buffer adoption, ordered view delivery and pending-view disposal on both platform implementations. Actors are normal application calls and peer inputs already accepted by existing parsing/validation; no new hostile grammar domain.

| Semantic class | Disposition | Enforcement owner | Finite evidence | Status |
|---|---|---|---|---|
| Singleton, missing/nonpositive segment size, multiple segments and short tail | Adopt filled backing and initialize all sibling refs before publication | Holder accept | One per distinct split class | Covered: `TestCoalescedDeliverySplits` |
| Empty or ordinary receive | Preserve bypass | Platform reader | Existing reader cases | Covered: `TestGROReadEmptyDatagram`, `TestWindowsUROReadEmptyDatagram` and existing ordinary-reader tests |
| Repeated next with full metadata | FIFO and clear consumed slot | Holder next | Shared fixture | Covered: `TestCoalescedDeliveryMetadataAndClearedSlots` |
| Release first view before/after discard; repeat discard | Only pending references released; returned bytes valid | Holder discard and slab release | Both relative orders | Covered: `TestCoalescedDeliveryDiscard` |
| Sibling releases concurrently | Safe shared reference lifetime | coalescedSlab | Existing race fixture | Covered: `TestCoalescedSlabConcurrentRelease` under Linux race detector |
| Unix native pending buffers on reader cleanup | Release native buffers independently of holder | Unix reader cleanup | Existing native cleanup regression | Covered: `TestGROReleaseReadBuffersReleasesPendingViews` and `TestOOBReaderBatchProgressionAndCleanup` |

**Evidence budget:** At most eight shared characterization additions; reuse slab fixtures. Existing named native wrapper tests are separately budgeted preservation evidence: Linux TestGROReadSplitsCoalescedRead, TestGROReadEmptyDatagram, TestGROReadPacketInfoInheritance and TestGROReleaseReadBuffersReleasesPendingViews; Windows TestWindowsUROReadEmptyDatagram, TestWindowsUROReadPacketInfoInheritance and TestWindowsUROReleaseReadBuffersReleasesPendingViews. One focused Linux race run (shared concurrent releases), one native Windows focused run for its different wrapper, and local Darwin focused tests for Unix integration. Existing hosted platform jobs can supply native evidence once, no platform Cartesian expansion. No performance run unless a concrete extra allocation is detected; then stop for a bounded decision. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** Characterize split, metadata and cleanup behavior before extraction. Run `go test . -run "Coalesced|GRO|ReceiveOffload|WindowsURO(Read|ReleaseReadBuffers)" -count=1`, verify selector actually includes the existing platform tests, and affected root suite. Native Linux race and Windows tests use the same actual applicable test names; cross compilation alone is not native behavior evidence.

**Dispatch context budget:** This slice; coalesced_slab.go/tests, two oob platform implementations, existing coalesced fixtures and incoming I7/Windows W3 invariant paragraphs only. Three small owning methods, two callers and one preserved native cleanup branch. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Extracting only pop or splitter would leave adoption/refcount/cleanup decisions duplicated. Splitting platforms leaves a temporary duplicate owner without delivered consolidation; both wrappers fit one PR. No blockers: existing slab and completed offload work are merged.

**Stop conditions:** Stop if adoption needs another backing allocation, shared mutable reader state crosses goroutines, metadata ownership changes, native cleanup disappears, or required native evidence is unavailable. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

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
