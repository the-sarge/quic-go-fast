# Architecture deepening program — 2026-09-21

**Program:** `QGF-ARCH-20260921`
**Status:** In progress; P complete, C1 and L1 remain on the independent frontier
**Normative scope:** Track identity, plan pointers, graph, frontier and binding rules.
**Audit history:** [Source/history dispositions and slice audit](../audits/2026-09-21-architecture-handoff/README.md).

## What this is

Three grilled, bounded changes. The track plans are the normative contracts; issues and OmniFocus contain current state and pointers. Each slice is one intended PR and one fresh implementation context. Publication of this package does not dispatch implementation.

## Tracks, dependencies and frontier

| Track | Plan | Parent issue | Blocked by | Slices | Status |
| --- | --- | --- | --- | --- | --- |
| P | [Terminal path admission](2026-09-21-terminal-path-admission-plan.md) | [#483](https://github.com/the-sarge/quic-go-fast/issues/483) | None | P1 | Complete |
| C | [Configuration numeric preparation](2026-09-21-configuration-preparation-plan.md) | pending | None | C1 | FRONTIER after verified default-branch publication |
| L | [Endpoint-owned managed lease binding](2026-09-21-managed-lease-binding-plan.md) | pending | None | L1 | FRONTIER after verified default-branch publication |

There are no cross-track blocking edges. P1, C1 and L1 are parallel-safe in dedicated worktrees: their intended product edit surfaces are respectively outgoing path state, root configuration preparation/call sites, and managed lease binding. All use root-package tests; that is not a runtime dependency. Rebase and re-audit any actual overlap if main advances. Recommended operator order is P1, then C1, then L1 because correctness repairs precede maintenance; this is not a readiness gate. All are small-to-medium single-context slices, with L1 the smallest.

## Outcomes closed with no code

No generic probe-attempt framework, immutable configuration compiler, public Clone change, merged HTTP/3/root defaults, lease transaction framework, raw socket handoff, or broader Transport activation program. No new performance campaign. Existing N01–N20 dispositions remain except the specifically bounded terminal-admission repair recorded in the P plan. Existing domain terms suffice; no new public domain concept or ADR is needed.

## Binding rules

The active architecture skills’ shared review-loop and contract-closure baselines govern with [docs/REVIEW-LOOP.md](../REVIEW-LOOP.md) and [maintained-code conventions](../agents/conventions.md). No copied baseline or new overlay is introduced. Track plans bind preservation, representation, ownership, artifact classes, finite evidence/context budgets, and stops.

Dispatch only an audited frontier child bearing the architecture-handoff marker, implement-architecture-slice pointer, and verified reachable default-branch plan commit. Parent issues and this program are not implementation work items. One child maps to one PR and one OmniFocus slice task. Stop/re-audit on topology, authority, representation, outcome or one-PR changes; do not widen a contract by appending reviewer examples.

Per implementation slice: review loop → exact-head certification and hosted checks → squash merge → append-dev-journal → reconcile live issue/frontier pointers → complete that slice task. Readiness and task state are not code certification. Program mapping fields remain pending until issues exist; the tracker owns live mapping and no additional normative plan commit is needed solely to backfill them.
