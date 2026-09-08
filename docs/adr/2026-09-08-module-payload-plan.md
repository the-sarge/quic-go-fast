# Published module payload Implementation Plan

**Date:** 2026-09-08. **Status:** Accepted; not implemented. **Track:** P in QGF-AD-2026-09. **Depends on:** No other track. **Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stops. **Audit history:** [Handoff receipt](../audits/2026-09-08-architecture-handoff/README.md). **Related:** [Program](2026-09-08-architecture-deepening-program.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0003](0003-follow-stable-upstream-releases.md), [0004](0004-packet-emission-ownership.md).

## Goal

Reduce dependency archive cost while keeping historical audit provenance accessible and unchanged in Git.

## Current Shape (verified 2026-09-08)

`go.mod:1` preserves github.com/quic-go/quic-go identity. `docs/adr/0002-adopt-through-module-replacement.md` requires replacement adoption. Audit evidence lies under docs/audits with no nested module at this baseline; current ADRs such as `0004-packet-emission-ownership.md:15` link there. The bounded measurement is in the audit receipt; do not treat its original size as a future fixed threshold.

## Decision

Use Go’s own nested-module archive exclusion with a small inert marker. Keep a reader-facing immutable evidence index in the shipped parent module. Current normative pointers can become pinned repository links; historical journal and archived receipt bytes remain unchanged. Module readers use the index for historical checkout-relative references.

**Rejected directions — do not do this:** Do not rename the root module, change dependencies, move raw captures to a new service, rewrite historical journal paragraphs, improve archived scripts, add a release job or claim runtime performance improvement.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| P1 | New | Exclude archived audits from the published module payload | None | None introduced |

## Implementation Slices

### P1 — Exclude archived audits from the published module payload

**Status:** Accepted contract; implementation pending. **Size:** S; one intended PR. **Blocked by:** None.

**What it delivers:** Add an inert nested docs/audits/go.mod packaging boundary; keep existing evidence bytes and locations in Git. Provide docs/audit-evidence.md outside the excluded subtree with immutable repository links, and repair audit pointers in current normative documents. Preserve historical journal paragraphs exactly.

**Existing-work disposition:** New slice; no open implementation PR or partial implementation is adopted.

**Single owner after merge:** Go module-zip construction owns inclusion/exclusion. The nested marker declares the boundary; frozen evidence identity is owned by immutable commit links. Current plans remain outside the boundary and own their contracts.

**Authority completeness:** No newly authoritative persisted fact or restart schema is introduced. The slice includes creation, validation and every in-scope terminal/destructive consumer of its new internal representation; constructors and teardown cannot be postponed to another slice.

**Transitional-seam budget:** No duplicate product representation, generic mutation API or double-open lifetime is introduced. Existing adjacent defects remain separately owned and do not act as successor-dependent placeholders. Any frozen experiment helper explicitly retained by T1 is outside the product representation.

**Decision details:** Use module github.com/quic-go/quic-go/docs/audits with no dependencies or exported library role and a Go version compatible with the root. Do not use .gitignore or export-ignore as a module-zip boundary. Do not relocate raw evidence, rewrite old journal entries, alter licenses, publish a release or add a storage service. The prior measured 95.01% reduction is historical evidence; measure the actual candidate once rather than enforcing that exact percentage on a later documentation tree.

**Blast radius:** Published archive contents, root identity/dependencies, package enumeration, nested-module tooling and immutable provenance are traced. Git clone size and runtime speed are unchanged. All 42 originally measured Markdown receipts are excluded too; do not promise historical journal-relative links work inside the module cache. Preserve them and provide the index route. No effect may be left implicitly untraced; a newly discovered required ownership boundary invokes the stop below.

**Artifact classification:** The marker is shipped packaging behavior; index/pointers are process metadata; frozen captures and disposable comparison scripts are verification aids, not newly maintained deliverables.

**Representation contract:** The complete root module archive produced by the authoritative golang.org/x/mod/zip implementation for the actual candidate Git tree. Universal exclusion of the declared nested-module subtree according to Go rules; no handwritten ignore grammar. Frozen existing audit files and append-only journal bytes have exact manifest comparisons.

**Contract closure:** Not triggered for this finite transition set: named guarded operations are reasonably covered by ordinary focused tests; multiple callers alone do not trigger closure. P1 uses the authoritative archive representation and a finite content comparison; T1 is a bounded test migration without a new shipped safety owner. Ordinary focused checks suffice; do not recursively impose closure on verification aids.

| Semantic class | Accepted disposition | Owner / evidence status |
| --- | --- | --- |
| Base/candidate module zip | One comparison: audit subtree absent in candidate and compressed payload reduced. | Named slice owner; regression/characterization required before completion |
| Source/package identity | Runtime file contents, root module path, root dependency bytes and package list unchanged. | Named slice owner; regression/characterization required before completion |
| Frozen evidence | Existing audit files and historical journal prefix byte-identical; only new marker/index/current links change. | Named slice owner; regression/characterization required before completion |
| Durable references | Check each changed pinned link against its Git tree; module readers use shipped index for historical checkout-relative links. | Named slice owner; regression/characterization required before completion |
| Consumer build | Compile root packages and one local replace-and-pin consumer against generated module content. | Named slice owner; regression/characterization required before completion |

**Evidence budget:** 5 semantic cells as listed, one representative positive and one materially different negative per applicable owner; listed alternatives are subcases, not a Cartesian product or permission for repetition. No mandatory mutation: at most one central guard bypass per enforcement owner only if inherited coverage otherwise leaves that guard unobserved. No fuzz campaign, arbitrary stress loop, new timing deadline, expanded platform matrix or sustained performance campaign. One initial fully briefed review and at most one replacement under the shared baseline. Stop when the listed evidence and required certification pass with no unresolved stop-for-decision finding; more confidence is not a completion criterion.

**TDD and preservation evidence:** Use one base/candidate zip and manifest comparison, `go list ./...` package comparison, `go test -run '^$' ./...` compile, and one local replacement-consumer build. Check links by pinned Git tree rather than a network crawler. No runtime performance experiment. These names select repository families or planned test cells, not claims that unwritten tests already exist. Runtime hot-path slices may invoke existing relevant benchmarks with benchmem once as diagnostic evidence; no performance qualification or speed claim is created, and no new per-packet heap representation is permitted.

**Dispatch context budget:** At most 16k dispatch input tokens: P1/common rules, ADRs 0001–0003, archive manifest and changed-link inventory, small Go zip API reference and governing diff. Do not load raw captures, profiles or full historical plans.

**Slice decision audit:** A marker without reader-facing evidence pointers breaks discoverability; keep marker, index and current-link repairs together. Moving evidence or maintaining a general archive framework is unnecessary. P1 is independent of H/I/T; new audit metadata can merge normally without a semantic blocker. There are no convenience-only blockers.

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
