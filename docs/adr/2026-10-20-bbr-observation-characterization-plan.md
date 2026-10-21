# BBR Pending-Bytes Documentation and Characterization Implementation Plan

**Date:** 2026-10-20
**Status:** M1 complete; no successors unlocked
**Track:** A20-M — 5 of 5 in A20
**Depends on:** Nothing; independent slices as stated below
**Related:** [Program index](2026-10-20-architecture-deepening-program.md), [no-code decisions](2026-10-20-architecture-decisions.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Source capture and slice audit](../audits/2026-10-20-architecture-handoff/README.md)

## Goal

Correct the design description and directly characterize the existing production installation’s all-generation pending-byte observation, including current construction exclusion. Add no runtime accounting refactor.

## Current Shape (verified 2026-10-20)

[`docs/designs/bbrv3.md:75`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/docs/designs/bbrv3.md#L75) says current-generation pending work. [`bbr_controller.go:18`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/bbr_controller.go#L18) reads total credit.pending and excludes the current construction reservation. [`local_send_credit.go:34`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/local_send_credit.go#L34) resets current-generation accounting without erasing older debt. [`congestion_control_migration_test.go:16`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/congestion_control_migration_test.go#L16) already tests migration fencing, but does not directly specify this observation’s installation. [`delivery_sampling.go:16`](https://github.com/the-sarge/quic-go-fast/blob/5f73ddc4de39268992f0b9ab33eed70c472955b7/delivery_sampling.go#L16) intentionally observes current-generation supply gaps; that is a different contract.

## Decision

Update the design wording to describe pending local work of any generation after excluding the packet currently being constructed. Add a focused test through the actual enableBBR installation and a consumer-visible sampling or feedback result, with older-generation debt retained. The existing sentPacketHandler DeliveryIdle/DeliveryStats().Idle consumer exposes the installed pending closure (internal/ackhandler/delivery_sampler.go:203–220), so a direct test can distinguish total pending from current-generation pending without a production hook. Do not recreate the closure in a fixture and call that production coverage. Keep runtime code and existing independently modeled fixtures unchanged unless a separately audited bug is found.

**Rejected alternative (do not do this): Introduce named credit views or a snapshot while correcting the text.** Changes the production implementation for a documentation/coverage outcome. **Revisit if:** a real credit consumer or accounting-rule change.

**Rejected alternative (do not do this): Rewrite fixture closures en masse.** Erases purpose-specific current-generation tests and does not test the real installation. **Revisit if:** an individually demonstrated fixture-contract mismatch.

**Rejected alternative (do not do this): Start a BBR qualification campaign.** No production binary changes are selected. **Revisit if:** a separately accepted shipped BBR change requiring qualification.

**Owner's design:** The ledger already owns reservation, completion and generation reset. Retain that owner. Document and observe the real controller installation rather than add an adapter, test-only production hook or second interpretation. No contained mechanism is needed.

**Non-goals:** No BBR behavior, bounds, production helper, fixture mass migration, retained store, generation convention, CI/measurement campaign or performance claim change.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| M1 | complete | Document and characterize installed all-generation pending bytes | None | n/a |

## Implementation Slices

### Slice M1 — Document and characterize installed all-generation pending bytes

**What it delivers:** Correct the design description and directly characterize the existing production installation’s all-generation pending-byte observation, including current construction exclusion. Add no runtime accounting refactor.

**Existing-work disposition:** New slice. The open-PR and issue census found no implementation PR or prior child for this accepted outcome; the audit record names the current source and search receipt. Retain the merged implementation as the behavior baseline except the explicit change here. Historical temporary diagnostics are evidence, not an implementation branch or maintained dependency.

**Blocked by:** None; maintenance is independent and not a prerequisite for any repair.

**Single owner after merge:** The existing localSendCredit owner and production enableBBR installation remain unchanged. Tests observe behavior; docs describe it.

**Authority completeness:** No new authoritative runtime or persisted fact. The production closure remains authoritative; the document is corrected to match it.

**Transitional-seam budget:** None introduced. No compatibility adapter, duplicate mutable representation or double-open lifetime is permitted; there is no successor cleanup slice.

**Blast radius:** Documentation and root-package tests only. Existing #855 queue admission changes are baseline, not a cause to rewrite emission. The test must use actual installation while respecting queue ownership and generation reset. If a production seam or accounting fix is necessary, stop and re-audit rather than widening this test-only slice.

**Artifact classification:** Design wording is process/domain traceability metadata. Characterization is a verification aid explicitly approved as this small maintained deliverable: payoff is detecting regression of the actual installation’s old-generation-debt semantics; domain is enableBBR’s pending observation and its named consumer; owner is the root QUIC test package; retire/update it when that production contract is deliberately superseded. It does not block unrelated shipped slices or require a generic maintained harness.

**Representation contract:** Internal typed credit generations, reservations and real installation; no external language. Example-level characterization of three states: no older debt, older debt after generation reset, and current construction reservation excluded. No universal migration/performance claim.

**Contract closure:** Not triggered: this is bounded documentation and verification-aid maintenance with example-level observation, not a new runtime enforcement owner. Do not recursively impose a fixture completeness proof.

**Evidence budget:** Three semantically distinct states above, one DeliveryIdle()/DeliveryStats().Idle assertion each after ordinary application exhaustion through ObserveDeliveryStop. At most one temporary mutation from total pending to current-generation pending if necessary to establish that the chosen assertion discriminates the installed closure; no mutation framework or fixture census. One focused run, root suite, focused race run and ordinary CI; one review plus at most one replacement.

**TDD and preservation evidence:** Write a passing production characterization before changing the description. Demonstrate that the older-debt assertion would distinguish a current-only installation using the bounded optional mutation if the direct consumer assertion is not self-evident. Then correct only the relevant paragraph.

**Dispatch context budget:** Current M1, bbr_controller.go, local_send_credit.go, congestion_dispatch.go and the actual DeliveryIdle()/DeliveryStats().Idle consumer in internal/ackhandler/delivery_sampler.go:196–220, existing configured emission fixture and migration tests, design paragraph. At most 12k context tokens; governing diff restricted to one design paragraph and a focused test. No full BBR campaign or retained-store history.

**Slice decision audit:** Splitting the paragraph correction and its focused production evidence adds two PR contexts for one contract clarification; keep together. Merging runtime views/store/arithmetic widens the accepted outcome and evidence cost. Reject both, none: rejected on merit. No blockers; BBR readiness registration is not a prerequisite for a test/docs-only change.

**Stop conditions:** Stop if the named production semantics cannot be observed without a new production testing interface, if the code contradicts the accepted current behavior, or if accounting/queue/performance changes are needed. Do not silently promote the characterization to a runtime fix.


## Acceptance Criteria

- [x] The maintained design describes all-generation pending work and the current packet-construction exclusion without changing runtime semantics.
- [x] A focused characterization exercises actual enableBBR installation and distinguishes older-generation debt from a current-only observation via a real consumer result.
- [x] The change contains only the precise design text and normal test code; no production accounting rewrite, new maintained harness or measurement campaign is added.

Each universal statement is limited to the slice’s declared domain and owner, with the guarantee and terminating evidence in that slice. No broader protocol, concurrency or platform certification is implied.

## Validation Gates

Use TestBBRInstalledPendingObservation as the proposed focused test name. Run it once, then `go test . -count=1`, the focused case once with `-race`, `go vet .`, `go mod tidy -diff`, `git diff --check`, and applicable hosted checks. No qualification cell, benchmark campaign, statistical repetition or additional platform obligation is introduced.

## Operating Discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern, composed with the existing [repository execution overlay](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). The shared files are `REVIEW-LOOP.md`, `CONTRACT-CLOSURE.md`, and `DESIGN-COST.md` in the active architecture skill package’s sibling `_shared/` directory. Use those baselines directly; do not copy them into this repository. No repository-specific contract-closure overlay exists or is needed for this program.

Apply representation and artifact gates, independently disposition findings, and use one initial fresh review plus at most one replacement after accepted fixes. Verification is limited to accepted source findings. Apply the shared precise-root comparison and approach stops; a common package or lifecycle topic does not establish a repeated root. Do not grow the evidence plan to exhaust reviewer suggestions or recursively certify fixtures. Any replacement design must be compared by obligations, contract changes, then footprint.

Use a dedicated worktree and feature branch. Certify the exact pushed head using the focused gates below and the repository overlay; require applicable existing hosted checks on that head, then mark ready and squash merge with a matching head. This repository has no `task preflight` or `ci-*` workflow. Diagnose failed checks before reruns; only demonstrated infrastructure failure permits a same-head rerun. After the primary work reaches remote main, run `append-dev-journal` without RAS, revalidate deferred findings against the merge, and reconcile issue and OmniFocus pointers. Plan/mirror state does not substitute for code-head certification.

M1 is complete. It unlocks no successors. Changes to representation ownership, accepted outcome, topology, adjacent scope, irreversible authority, or the one-PR boundary require a decision or a new scoped architecture handoff.
