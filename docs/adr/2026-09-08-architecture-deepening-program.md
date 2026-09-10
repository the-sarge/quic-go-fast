# Architecture Deepening Program — 2026-09-08

**Identity:** QGF-AD-2026-09. **Status:** In progress; H1/H2/H3 and I1/I2/I3/I4/I5/I6/I7 implementations complete; other slices pending. **Normative scope:** Stable track identity, cross-track edges, accepted no-code outcomes and binding rules. Each exact plan slice owns the implementation contract. **Audit history:** [Handoff receipt](../audits/2026-09-08-architecture-handoff/README.md); [scoped construction addition](../audits/2026-09-08-emission-construction/README.md); [H3 fixture audit](../audits/2026-09-09-http3-fixture-completion.md).

## Tracks, dependencies and frontier

| Track | Outcome | Normative plan | Parent issue | Slices | Blocked by |
| --- | --- | --- | --- | --- | --- |
| H | Full pooled HTTP/3 exchange lifetime and conditional eviction | [H plan](2026-09-08-http3-lifetime-plan.md) | #86 | H1, H2, H3 | Complete |
| I | Incoming packet-buffer lifetime | [I plan](2026-09-08-incoming-lifetime-plan.md) | pending | I1–I8 | I2 requires I1; I4 requires I3; I8 requires I2 and I5 |
| P | Published module payload | [P plan](2026-09-08-module-payload-plan.md) | pending | P1 | None |
| T | Maintained emission test entrypoints | [T plan](2026-09-08-emission-entrypoint-tests-plan.md) | pending | T1 | None |
| C | Complete initial packet-emission assembly | [C plan](2026-09-08-emission-construction-plan.md) | pending | C1, C2 | C2 requires C1 |

The current implementation frontier is I8, P1, T1 and C1. C2 is blocked as shown; I1, I2, I3, I4, I5, I6 and I7 are complete, and I8’s blockers are closed. The program comprises fifteen intended PRs and fifteen fresh implementation contexts; five remain after H1, H2, H3, I1, I2, I3, I4, I5, I6 and I7. H1 reuses existing issue #68; issue links pending in this index are resolved by the program tracker after publication and need not be backfilled into another authoritative plan commit.

H1, H2 and H3 implementations are complete. H3’s merged test-only correction enables reconciliation of pending H2 journal #139. I1, I2, I3, I4, I5, I6 and I7 are complete. The remaining I owners, including I8, are ready as listed. P1 can run in parallel because it changes distribution rather than runtime. T1 and C follow correctness by preference. C1 supplies the specific constructor-owned coverage required by C2; broad T1 migration does not technically block C. Future readiness changes need their own evidence decision. There are no cross-track technical blockers. H3 changes exchange fixtures and does not widen H1/H2 runtime ownership; I slices share connection/server/transport files; T1 and existing #73 touch nearby tests; C shares connection.go with I and constructor fixtures with T. Keep path/early-error changes in their current tracks and revalidate constructor ordering when integrating. Use separate worktrees and serialize shared-file integration/revalidation; shared filenames alone are not dependency edges.

## Outcomes closed with no code

- Active-path/packet-size coordination (C-011/C-017) is deferred. Different client/server ordering is not a demonstrated race; path generation, effective initial size, discovery state, application estimate and congestion accounting remain distinct connection-owned facts. Reopen only for a concrete transition defect or material new duplication.
- Broad emission path binding (C-021 and the path member of C-007) remains deferred. Shared references do not imply competing policy owners. The narrower construction member of C-007/C-028 is reopened only as C1/C2: preserve recovery/path bindings and replace staged initial assembly. No initialization race or performance gain is claimed.
- Send-readiness consolidation (C-020 and outcome C-007) is deferred. result.available is live and the blocked projection depends on progress/context, not only stop reason. Preserve pre-packing capacity ordering; no sealed result algebra or new performance campaign is scheduled.
- Broad receive modules, new HTTP/3 pool frameworks and moving audit evidence to a new service are rejected. The retained plans capture the narrower useful outcomes and explicit rejected directions.

## Existing work and stable boundaries

H1 updates #68 rather than duplicating it and moves its existing OmniFocus task under the H track. Original audit provenance is retained in the handoff receipt. No open implementation PR or partial implementation was found for these tracks; source at the inspected default branch is the baseline, not an unmerged branch. The C addition is scoped to this existing program, not a rebaseline of the completed packet-emission program or the unrelated code-smell audit. H/I/P/T contracts remain intact; C adds two new slices with no cross-track technical blockers. No new domain vocabulary or ADR reversal is required. Existing #67, #71–#84 (except the H1 contract of #68), #18, #44 and #46 retain their separate scopes and identities. In particular #73/#72 helper work and #67 pre-stream SETTINGS cancellation are not silently absorbed.

H3 is the scoped fixture-observation addition described by the current H plan and linked audit. Retain merged H1/H2 product behavior and the append-only H2 journal PR #139; the latter awaits reconciliation with the merged H3 correction. This addition creates no other cross-track blocker or domain/ADR change.

## Rules that bind every track

ADRs 0001–0004 and [0005](0005-incoming-packet-lifetime.md), the root domain glossary, and the shared REVIEW-LOOP/CONTRACT-CLOSURE baselines supplied by $implement-architecture-slice govern. Each plan composes the repository’s inherited workflow gates and finite evidence. Preserve public API/wire/module identity, connection-goroutine protocol state, socket-worker responsibilities, explicit single owners and historical evidence. Upstream acceptance is welcome but not required by ADR 0003.

Current plan contracts contain boundaries, outcomes, invariant/representation owners, semantic dispositions, budgets, blockers and stops. Raw reports and chronological corrections are audit metadata. Child issues pin the exact verified default-branch plan commit; issue and OmniFocus mirrors contain only current state and pointers. Exact code-head certification is a separate implementation gate.

The per-slice ritual is bounded review → exact-head local and same-head hosted checks → guarded squash merge → append-dev-journal after merge → revalidate deferred findings → complete that slice’s OmniFocus task. Every child uses $implement-architecture-slice in a fresh context. A representation/owner/topology/adjacent-scope/product/irreversible-authority/one-PR change returns for decision or architecture-handoff; an in-boundary local re-audit follows the child skill. The current handoff publishes this package and reports the frontier; it dispatches nothing.
