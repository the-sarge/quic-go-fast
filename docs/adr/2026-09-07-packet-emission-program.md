# Packet emission architecture program — 2026-09-07

**Identity:** QGF-PE-2026-09. **Status:** E1 complete with inconclusive feasibility; E2 ready; E3–E6 blocked. **Normative scope:** Track identity, cross-slice dependencies, frontier and binding policy. **Audit history:** [Handoff audit](../audits/2026-09-07-packet-emission-handoff.md).

## Outcome and scope

Deepen packet emission around the existing packer while preserving public behavior and the current Go execution model. [ADR 0004](0004-packet-emission-ownership.md) records the decision; the track plan owns the exact contracts. The earlier QGF-2026-09 datagram/handshake program remains complete and is not reopened.

## Tracks and frontier

| Track | Plan | Parent issue | Slices | Dependencies | Status |
| --- | --- | --- | --- | --- | --- |
| E — Packet emission | [Plan](2026-09-07-packet-emission-plan.md) | [#24](https://github.com/the-sarge/quic-go-fast/issues/24) | E1–E6, one intended PR each | E3 requires positive E1 and E2; E4 and E5 require E3; E6 requires E4 and E5 | E1 complete, inconclusive; E2 is frontier |

E1 completed with [inconclusive feasibility evidence](../audits/e1-emission/README.md) through [PR #33](https://github.com/the-sarge/quic-go-fast/pull/33); no experimental runtime was adopted. E2 (queue ownership prefactor) remains independently ready. E4 (handshake/ACK/PTO) and E5 (probes/path handoff) can proceed independently after E3. Shared-file edits require serialized integration and validation of the later candidate, not invented dependency edges. E1's positive feasibility disposition is required before E3; the inconclusive disposition leaves E3–E6 blocked until a scoped re-handoff revises or closes them.

The current dispatchable frontier is E2. Parent issues are never implementation tasks. The tracking issue owns the live mapping, so issue fields do not independently require a new plan commit after publication.

## Outcomes requiring no implementation

Keep the existing Go application interface, connection loop and send worker. Defer a full sans-I/O engine, application write-credit changes, receive refcounts, per-path policy redesign, custom congestion, package splitting, neutral trace events and HTTP/3 changes. No speed improvement or physical-link qualification is promised. An evidence-only E1 disposition is valid and does not justify promoting prototype code.

## Binding rules

The shared REVIEW-LOOP.md and CONTRACT-CLOSURE.md baselines supplied by `$implement-architecture-slice` govern every slice, including independent finding dispositions, representation/artifact gates, finite evidence and review budgets, precise-root family handling and scoped approach stops. There are no repository overlay copies. Use the inherited workflows actually present in this fork, not assumed portfolio `ci-*` names or draft-skip behavior. The plan's Operating Discipline defines the repository-specific CI mapping and docs publication gate.

Each slice uses its own dedicated worktree and feature branch, one draft PR, exact-head local validation, same-head hosted checks, a head-guarded squash merge, then `$append-dev-journal`, then completion of its task. Diagnose failed checks before reruns. Keep contracts in plans and current state/pointers in issues and OmniFocus. Never confuse a plan commit or task state with code-head certification. Clear context between dispatched child issues.

This handoff publishes documents and tracking only. It does not start the prototype, runtime implementation, performance captures, or an implementation agent.
