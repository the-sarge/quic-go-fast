# Architecture Deepening Program — 2026-10-20 (A20)

**Status:** Slice audit passed; publication and dispatch state belong to the live tracker; no implementation dispatched by this handoff
**Stable identity:** A20
**Normative plans:** The track documents below; live mapping belongs to the program issue
**Audit history:** [Capture, scope decisions and audit receipts](../audits/2026-10-20-architecture-handoff/README.md)

## What this is

Package the accepted architecture review as context-sized, independently green future PRs. Plan slices are normative. Issues and OmniFocus contain current state and pointers, not mirrored contracts. This is a new program under the supplied OmniFocus parent; it does not rebaseline the September programs or the existing BBR campaigns.

## Outcomes that require no implementation

The [decision register](2026-10-20-architecture-decisions.md) captures the accepted guardrails, deferred candidates and rejected alternatives from the grilled reports. Those dispositions are not a hidden backlog. No domain glossary addition is needed: the existing HTTP/3 exchange and pending local send bytes concepts suffice.

The QPACK error-scope correction is held for an upstream typed-error classification contract. Its current dependency does not expose a supported distinction between connection-level invalid references and stream-level decoder limits. No parser fork, error-text classifier or unimplementable child is authorized. The protocol plan records the accepted owner design and precise revisit trigger. The other protocol repairs are independently useful.

## Tracks, dependencies and frontier

| # | Track | Plan | Parent issue | Blocked by | Slices | State |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | A20-R: HTTP/3 Request Admission | [A20-R](2026-10-20-request-admission-plan.md) | pending | None | 1 | Audit passed |
| 2 | A20-S: Simulated-Network Delivery Shutdown | [A20-S](2026-10-20-simnet-shutdown-plan.md) | pending | None | 1 | Audit passed |
| 3 | A20-P: HTTP/3 Detected Protocol Error Dispositions | [A20-P](2026-10-20-http3-protocol-errors-plan.md) | pending | None | 2 | Audit passed |
| 4 | A20-H: Initial Request-Header Ownership | [A20-H](2026-10-20-request-header-ownership-plan.md) | pending | None | 1 | Audit passed |
| 5 | A20-M: BBR Pending-Bytes Documentation and Characterization | [A20-M](2026-10-20-bbr-observation-characterization-plan.md) | pending | None | 1 | Audit passed |

There are six audited implementation slices: R1, S1, P1, P2, H1 and M1. The independent audit confirmed that no slice-to-slice blocking edge is justified. QPACK has no child slice until its representation contract can pass the gate. Every child maps to one intended PR and one fresh implementation context.

Recommended order is R1 → S1 → P1/P2 → H1, with M1 as independent maintenance. These arrows express priority only. S1 and M1 are parallel-safe with HTTP/3 work; P1 and P2 share Stream.Read, while R1/H1 share the HTTP/3 package and tests, so serialize or carefully rebase their changes for review clarity. Do not encode file overlap or preferred sequencing as a false dependency. H1 preserves the behavior at its implementation baseline, and the deferred QPACK change does not block that resource-lifetime refactor.

After the audited docs are merged and their default-branch commit verified, R1, S1, P1, P2, H1 and M1 can be the frontier. Before that publication gate, none is dispatchable. Parent issues are never implementation dispatch targets. Publication and current frontier belong in the live tracker after it exists; pending links do not require another authoritative plan commit merely for backfill.

## Rules that bind every track

Follow the shared review-loop, contract-closure and design-cost baselines supplied by the active architecture skills, plus the existing [repository execution overlay](../REVIEW-LOOP.md). Do not copy or strengthen shared policy. Track plans supply domains, single owners, artifact classifications, finite evidence, context budgets and stop conditions. Preserve settled ADRs, frozen evidence and [maintained-code conventions](../agents/conventions.md). Use the current slice plus bounded governing diff and relevant unresolved history, not duplicate full historical plans.

No performance campaign, active experiment revision, production parser replacement or upstream communication is authorized by this handoff. Protect exact measured targets and apply actual per-change requirements; earlier exceptions do not pre-certify new changes. Existing CI/toolchain failures are publication gates, not reasons to bypass required checks or change unrelated code in this docs PR.

Each future child follows review loop → exact-head local certification and hosted checks → merge → append-dev-journal → reconcile issues and complete its OmniFocus task. Plan SHA and mirror state are administrative pointers, separate from code-head certification. Dispatch only verified frontier children through `$implement-architecture-slice` to fresh contexts; this handoff dispatches none.
