# Architecture deepening program — 2026-09-13

**Identity:** A13
**Status:** In progress; R1 and R2 complete

## What this is

Seven narrow tracks preserve the existing architecture while repairing specific ownership and lifecycle seams. Their current implementation plans are normative. This overview owns only stable identities, graph/frontier, outcomes closed with no code and binding pointers. Issues own the live issue/task mapping. Audit history is [linked separately](../audits/2026-09-13-architecture-handoff/README.md).

## Tracks, dependencies and frontier

| Track | Plan | Parent issue | Hard prerequisites | Slices | Current state |
|---|---|---|---|---|---|
| R — Receive STREAM lifetime | [Plan](2026-09-13-receive-stream-lifetime-plan.md) | pending | None | R1, R2, R3 | R1 and R2 complete; R3 frontier |
| S — HTTP/3 server admission | [Plan](2026-09-13-http3-server-admission-plan.md) | pending | None | S1 | Frontier |
| B — Send-batch defensive progress | [Plan](2026-09-13-send-batch-progress-plan.md) | pending | None | B1 | Frontier |
| H — HTTP/3 response completion | [Plan](2026-09-13-http3-response-completion-plan.md) | pending | None | H1 | Frontier |
| C — Coalesced receive delivery | [Plan](2026-09-13-coalesced-delivery-plan.md) | pending | None | C1 | Frontier |
| Q — HTTP/3 datagram queue references | [Plan](2026-09-13-http3-datagram-references-plan.md) | pending | None | Q1 | Frontier |
| M — Unused packet-handler mock | [Plan](2026-09-13-unused-packet-handler-mock-plan.md) | pending | None | M1 | Frontier |

There are no hard cross-track or within-track edges. Current frontier: R3, S1, B1, H1, C1, Q1, M1. Every slice is one intended PR in one fresh context. R1/R2/R3 are medium, medium, and medium respectively; S1 and C1 are medium; B1 and H1 are small; Q1 and M1 are very small. Context boundaries are defined in each slice, not by these relative estimates.

Recommended remaining attention order: S1, R3, B1, H1, C1, Q1, M1. This is prioritization, not a dependency graph. Prefer serial scheduling of R1/R2/R3 because they overlap receive_stream.go and tests; they are independently green. Distinct tracks may proceed in parallel on dedicated worktrees, with normal merge conflict checking. Shared generated-test or broad package tests do not themselves create dependency edges. Do not label a convenience order as blocked.

## Outcomes closed with no code

[N01–N20 decision ledger](2026-09-13-architecture-decisions.md) owns deferred HEADERS/registry extraction, the secondary leads, and settled closed-program guardrails. These decisions have zero child issues and zero implementation tasks. Do not reopen unrelated completed programs or absorb the independent foundational architecture research map.

## Rules binding the program

Follow each exact slice contract, ADRs 0001–0005, [maintained conventions](../agents/conventions.md), and the shared review-loop/contract-closure baselines composed with the [repository execution overlay](../REVIEW-LOOP.md). Preserve existing behavior except each explicitly accepted repair; every touched fact/transition has one owner, no new persisted authority is granted, and there are no temporary duplicate state machines. Stop rather than silently expand a representation, one-PR boundary or evidence budget.

Plans must be merged and reachable on remote main before child issues are ready. Each child carries the architecture-handoff marker, `$implement-architecture-slice` and the exact reachable plan commit. Parent issues, tracker and OmniFocus mirror state/pointers only; they do not duplicate contracts. Dispatch is an operator action after this handoff. Per slice: review loop → merge → append-dev-journal → complete slice task. Reconcile frontier when state changes; exact implementation-head certification is separate from administrative plan/mirror state.
