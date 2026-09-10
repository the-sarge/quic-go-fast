# Maintained emission test entrypoints Implementation Plan

**Date:** 2026-09-08. **Status:** T1 implementation complete. **Track:** T in QGF-AD-2026-09. **Depends on:** No other track. **Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stops. **Audit history:** [Handoff receipt](https://github.com/the-sarge/quic-go-fast/blob/3db9121a2c91bce8acb7b6f871adfcaf51bc560b/docs/audits/2026-09-08-architecture-handoff/README.md). **Related:** [Program](2026-09-08-architecture-deepening-program.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md).

## Goal

Make the remaining connection-level sending scenarios evidence for the shipped composition while retaining legitimate lower-level tests and frozen experiment interpretation.

## Current Shape (verified 2026-09-08)

`connection_emission_test.go:264` defines test-only Conn send orchestration via finish(sendAny(...)); `connection.go:2470` applies the production result including blocked state. Eight untagged callers occur in emission, handshake and probe test files; two tagged callers occur in connection_emission_experiment_test.go and connection_probe_experiment_test.go. Existing real-loop/full-queue/fallback/PTO tests already cover production composition, so the entire shipped path is not untested.

## Decision

Migrate only the finite maintained caller set, configuring genuine send eligibility and preserving real packer/recovery/queue behavior and scenario-local assertions. Keep direct low-level tests when their claim is module-local. Move historical composition unchanged behind its existing experiment tag if needed; compile it without running a new campaign.

**Rejected directions — do not do this:** Do not alter production scheduling, constructors, path ownership or result representation; do not weaken assertions to accommodate dispatch; do not absorb #73/#72 fixture convention work or general mock cleanup. Do not reinterpret old experiment measurements as if the entrypoint had always been the same.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| T1 | Complete | Exercise shipped send orchestration in maintained connection tests | None | None introduced |

## Implementation Slices

### T1 — Exercise shipped send orchestration in maintained connection tests

**Status:** Implementation complete; no T-track successors. **Size:** S; one intended PR. **Blocked by:** None.

**What it delivers:** Migrate the eight untagged sendPackets/emitPackets callsites across connection_emission_test.go, connection_emission_handshake_test.go and connection_probe_emission_test.go onto triggerSending or the real loop where required. Preserve scenario-local assertions and real packing/recovery/queue behavior.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** The shipped packetEmission.send/Conn.triggerSending composition owns connection-level sending. Maintained test fixtures control only eligibility/protection and observe behavior; they do not compose a second send algorithm.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Maintained tests are the accepted deliverable of T1: payoff is confidence in shipped orchestration, domain is this finite caller set, owner is the packet-emission maintainer, retirement occurs when an equivalent production-boundary test replaces a scenario. They are verification aids, not a new required product framework. Historical tagged shim is intentional compatibility for frozen experiments, not a transitional production representation requiring a deletion slice.

**Blast radius:** Test eligibility and assertion meaning, ordinary/GSO output, errors, receive fairness and tagged experiment interpretation are traced. Existing #73 is adjacent and remains separate; its #72 conventions blocker does not gate this exact entrypoint migration. No global fixture redesign. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** Maintained test source is the explicit T1 verification-aid deliverable with payoff/domain/owner/retirement above. The tagged historical helper is frozen experiment compatibility. Census receipts and plans are process metadata; shipped behavior remains byte-identical.

**Representation contract:** The finite existing eight untagged helper consumers and two tagged experiment consumers identified by symbol/callsite census. Canonical finite source-set guarantee; Go compilation resolves callers. This does not claim full protocol or scheduler coverage.

**Contract closure:** Not triggered for this finite transition set: named guarded operations are reasonably covered by ordinary focused tests; multiple callers alone do not trigger closure. P1 uses the authoritative archive representation and a finite content comparison; T1 is a bounded test migration without a new shipped safety owner. Ordinary focused checks suffice; do not recursively impose closure on verification aids.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Eight maintained consumers | Characterize then migrate the same fairness/output/error/MTU/probe claims without removing assertions. | Named slice owner; regression/characterization required before completion |
| Worker capacity and wakeup | Retain existing real-loop coverage where asynchronous capacity is part of the claim. | Named slice owner; regression/characterization required before completion |
| Historical experiments | Keep two tagged callers compatible by moving the historical shim unchanged behind emission_experiment when needed; compile without running it. | Named slice owner; regression/characterization required before completion |
| Production preservation | Exact byte comparison of production Go, module and workflow files; no runtime changes. | Named slice owner; regression/characterization required before completion |
| Existing composed coverage | Retain fallback, PTO, blocked-state, fatal partial construction and registration-order coverage. | Named slice owner; regression/characterization required before completion |

**Evidence budget:** 5 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Run the finite callsite characterization and affected emission/handshake/probe test families once, one focused race invocation, and `go test -tags emission_experiment -run '^$' .` to compile without executing historical experiments. Verify production/module/workflow byte identity. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 24k dispatch input tokens: T1/common rules, relevant ADR 0004/E6 preservation clauses, the three maintained files, two tagged callers, triggerSending and send/dispatch/sendAny, plus current diff. No entire E1–E6 narrative or raw benchmark archive.

**Slice decision audit:** The eight maintained consumers form a small closed caller migration; splitting them leaves parallel orchestration in ordinary tests for little context benefit. The two tagged callers must remain buildable in the same PR. Do not combine production construction/readiness refactors or #73 fixture/MTU/dial cleanup. There are no convenience-only blockers.

**Acceptance criteria:**

- [ ] Deliver the end-to-end behavior above through its actual owners and consuming callers.

- [ ] Implement the declared internal invariant without a second terminal authority or unbudgeted product seam.

- [ ] Satisfy the listed semantic-cell evidence and preserve the traced behavior within the representation domain.

- [ ] Complete the shared bounded review, exact-head local and same-head hosted gates, merge, post-merge journal and pointer updates.

**Quantifier scope:** “Every”, “one”, “exactly”, “no” and other universal statements in this slice apply only to the declared representation domain and named owners. The finite table and tests terminate evidence; they are not an exhaustive concurrency or external-parser proof.

**Stop conditions:** Apply shared representation, precise-root and budget stops. Also stop if the required correction changes the public API, wire behavior, retry eligibility, initiating shared-dial context, public shutdown wait/socket-ownership semantics, the separately scoped #67 wait, or requires a new global receive/pool module; if a packet refcount is concurrently shared beyond its declared owner; if a producer outlives its declared stop barrier; if a required semantic family cannot fit this single PR/context; or if a frozen evidence rewrite is required. Re-audit the affected contract rather than extend the checklist.

## Validation Gates

The exact per-slice evidence tables and command families above are terminating. Preserve existing regression tests on changed surfaces. The source contract is normative; raw diagnostics and historical failure logs are evidence rather than reusable acceptance harnesses. Do not require a permanent verification aid before a shipped correction unless its explicit maintained-deliverable contract says so.

## Operating Discipline

The shared `REVIEW-LOOP.md` and `CONTRACT-CLOSURE.md` baselines supplied by `$implement-architecture-slice` govern, composed with ADRs 0001–0004 and any repository-specific overlays actually present at dispatch. There are currently no `docs/REVIEW-LOOP.md` or `docs/CONTRACT-CLOSURE.md` overlays; do not create or synchronize copies. Apply artifact and representation gates, semantic-family closure only when triggered, independent fix-now/defer/reject/stop-for-decision dispositions, finite evidence and one initial review plus at most one replacement. Diagnose failures before reruns; repeated-root conclusions require the shared side-by-side invariant/central-owner/semantic-class comparison across review and verification history.

The repository uses inherited `unit.yml`, `integration.yml`, `lint.yml`, cross-compilation and interop workflows, not portfolio `ci.yml`/`ci-*`; they can run on drafts. Open a draft, finish the bounded review and local exact-head certification, push and verify clean state, mark ready, then inspect the latest applicable actual hosted checks on that exact live head. Runtime local certification uses ordinary package tests/static checks plus the slice’s focused regressions; use existing required Linux/macOS/Windows support checks rather than adding a platform matrix. Documentation-only local certification is scope/anchors/links/Markdown structure and `git diff --check`. All applicable triggered checks must succeed, not be silently treated as skipped. A same-head rerun requires a diagnosed external infrastructure fault; unresolved repository failures do not become passing evidence.

Respect live branch protection and merge with `gh pr merge --squash --match-head-commit <exact-head>`. Only after the primary work merges, use `$append-dev-journal` without RAS through a separate docs PR; then revalidate pending deferred findings and complete the matching OmniFocus slice task. Keep parent issues and task notes pointer-based. Exact code-head certification is distinct from the accepted plan commit and administrative mirror state. Shared-file merges require integration/revalidation, not invented blocking edges.

No new verification framework, permanent analyzer, CPU reservation, sustained traffic experiment, security campaign, new support platform or release publication is authorized by these slices. Historical emission adoption uncertainty and the closed E1–E6 evidence budgets remain unchanged. No implementation is dispatched by this handoff.
