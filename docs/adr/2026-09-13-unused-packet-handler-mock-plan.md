# Unused packet-handler mock implementation plan

**Date:** 2026-09-13
**Status:** Complete
**Track:** M in architecture deepening program A13
**Depends on:** No hard prerequisites
**Normative scope:** Current contract only
**Audit history:** [Evidence and decisions](../audits/2026-09-13-architecture-handoff/README.md)
**Program:** [A13](2026-09-13-architecture-deepening-program.md)

## Goal and current shape

Before M1, `mockgen.go` generated MockPacketHandler in `mock_packet_handler_test.go`; maintained-source search found no consumers. M1 removed that generated output, its directive and the build-tagged PacketHandler alias. The live packetHandler interface, ConnRunner callbacks and other used mocks remain unchanged. Frozen audit manifests can legitimately mention the removed generated file.

## Decision

Remove the unused generated mock and its generation directive plus now-unused build-tagged PacketHandler alias in the same patch after rechecking maintained consumers. Preserve actual packetHandler behavior, other generated mocks and frozen evidence bytes/paths.

**Rejected alternatives — do not do this:** Do not delete a live mock based on call count, bundle routing registry extraction, or rewrite frozen manifests so a search becomes empty.

**Non-goals:** No production interface/callback removal, generator replacement or broad generated-file sweep.

## Slice graph

| Slice | State | Delivery | Blocked by | Temporary seam removal |
|---|---|---|---|---|
| M1 | Complete | Remove the unused MockPacketHandler generator output | None | None |

## Implementation slices

### M1 — Remove the unused MockPacketHandler generator output

**What it delivers:** Delete unused generated test output and the input directive so normal regeneration stays stable.

**Existing-work disposition:** New slice. See the [existing-work audit](../audits/2026-09-13-architecture-handoff/existing-work.md); no unmerged PR or branch is a prerequisite. Recheck the exact changed surface before implementation if main advanced.

**Blocked by:** None.

**Single owner after merge:** mockgen.go owns generation intent; no runtime state owner changes.

**Authority completeness:** No new persisted authoritative facts, serialization, restart format or destructive persisted consumer. Existing in-memory constructors and terminal consumers on this slice's surface remain included.

**Transitional-seam budget:** None introduced. Existing out-of-scope behavior remains coherent without an unmerged successor; there is no temporary adapter or duplicate state machine requiring a removal slice.

**Blast radius:** Test build and generator reproducibility only. Frozen historical evidence is excluded from edits, not excluded from honest reporting.

**Artifact classification:** Generated mock output and its generator directive/build-tagged alias are verification aids and generation input. There is no shipped runtime behavior or safety-enforcement change. Frozen audit files and this plan are traceability metadata. No maintained verification-aid exception or new test infrastructure is approved.

**Representation contract:** Canonical maintained-source reference domain determined by Go files and generator directives; guarantee is no maintained MockPacketHandler use at the accepted head. Frozen prose references are explicitly non-consumers.

**Contract closure:** Not triggered: a single synchronous operation/deletion is covered by ordinary focused evidence; multiple independently reachable materially risky lifecycle paths are not being redesigned.

**Evidence budget:** One reference census with rg, one documented generation command after committing the intended deletion (the check script requires a clean tree), one root package test run and diff check. No new tests or analyzer deliverable. Terminate when the listed cases and applicable gates pass with no unresolved stop-for-decision finding; passing examples are evidence for the named enforcing representation, not a completeness proof.

**TDD and preservation evidence:** First run `rg -n "MockPacketHandler|NewMockPacketHandler" --glob "*.go"` and distinguish definitions from consumers. Recheck PacketHandler alias references; remove directive, alias and output together; run the existing generation script and `go test . -count=1`.

**Dispatch context budget:** This slice, mockgen.go, generated mock, search output and generation script; deletion fits one fresh context. Include the shared operating baselines and bounded governing diff if this contract changes. Do not supply whole historical reports.

**Slice decision audit:** Generated output and directive cannot be split without regeneration drift. Combining a routing refactor changes the outcome. No blockers.

**Stop conditions:** Stop if a maintained consumer now exists; keep the mock and re-audit whether replacement coverage fits rather than deleting the consumer. Shared representation, repeated-root, artifact and one-PR boundary stops also apply.

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
