# Packet emission architecture program — 2026-09-07

**Identity:** QGF-PE-2026-09. **Status:** Staged adoption authorized with E1 uncertainty accepted; E2, E3, E4 and E5 complete; E6a/E6b/E6c complete; E6 ready. **Normative scope:** Track identity, cross-slice dependencies, frontier and binding policy. **Audit history:** [Handoff audit](../audits/2026-09-07-packet-emission-handoff.md).

## Outcome and scope

Deepen packet emission around the existing packer while preserving public behavior and the current Go execution model. [ADR 0004](0004-packet-emission-ownership.md) records the decision; the track plan owns the exact contracts. The earlier QGF-2026-09 datagram/handshake program remains complete and is not reopened.

## Tracks and frontier

| Track | Plan | Parent issue | Slices | Dependencies | Status |
| --- | --- | --- | --- | --- | --- |
| E — Packet emission | [Plan](2026-09-07-packet-emission-plan.md) | [#24](https://github.com/the-sarge/quic-go-fast/issues/24) | Nine slices: E1–E5, E6a/E6b/E6c, E6; one intended PR each | E3 requires the accepted adoption decision and completed E2; E4 and E5 require E3; E6a requires E4; E6b requires E3/E4; E6c requires E5; E6 requires E4/E5 and E6a/E6b/E6c | E1 complete, uncertainty accepted; E2/E3/E4/E5 complete; E6a/E6b/E6c complete; E6 ready |

E1's [four-core evidence](../audits/e1-emission-four-core/README.md) remains inconclusive. The owner has [authorized staged adoption with that uncertainty accepted](../audits/2026-09-07-emission-adoption-decision.md); this satisfies the E1 migration prerequisite without changing the measurements or authorizing another feasibility campaign. E2's queue lifetime corrections and [E3's ordinary/GSO emission](../audits/e3-packet-emission/README.md) are complete. [E4’s handshake/ACK/PTO emission](../audits/e4-handshake-emission/README.md) is also complete. [E5’s probe emission and path handoff](../audits/e5-probe-emission/README.md) is complete. E6a/E6b/E6c are complete; E6 is ready. Shared-file merges remain serialized and revalidated without invented dependencies.

Performance qualification uses the plan's four-core deployment domain; two-core results remain diagnostic only. The current implementation frontier is E6. E6a’s [preservation receipt](../audits/e6a-handshake-consumers/README.md) records the completed handshake/lifecycle migration. E6b’s [preservation receipt](../audits/e6b-scheduling-consumers/README.md) records the completed scheduling/batching migration. E6c’s [preservation receipt](../audits/e6c-path-consumers/README.md) records completed connection-ID/path consumer removal. E6’s prefactor prerequisites are complete. The [re-slice audit](../audits/2026-09-08-e6-contract-reslice.md) records the existing-work disposition. The [historical-timeout decision](../audits/2026-09-07-e3-certification-exception.md) and nonblocking [investigation #44](https://github.com/the-sarge/quic-go-fast/issues/44) retain the accepted uncertainty. Later slices retain their correctness and bounded performance gates. Parent issues are never implementation tasks, and the tracking issue owns the live mapping.

## Outcomes requiring no implementation

Keep the existing Go application interface, connection loop and send worker. Defer a full sans-I/O engine, application write-credit changes, receive refcounts, per-path policy redesign, custom congestion, package splitting, neutral trace events and HTTP/3 changes. No speed improvement or physical-link qualification is promised. E1 remains evidence only; production migration requires its own reviewed and validated slices.

## Binding rules

The shared REVIEW-LOOP.md and CONTRACT-CLOSURE.md baselines supplied by `$implement-architecture-slice` govern every slice, including independent finding dispositions, representation/artifact gates, finite evidence and review budgets, precise-root family handling and scoped approach stops. There are no repository overlay copies. Use the inherited workflows actually present in this fork, not assumed portfolio `ci-*` names or draft-skip behavior. The plan's Operating Discipline defines the repository-specific CI mapping and docs publication gate.

Each slice uses its own dedicated worktree and feature branch, one draft PR, exact-head local validation, same-head hosted checks, a head-guarded squash merge, then `$append-dev-journal`, then completion of its task. Diagnose failed checks before reruns. Keep contracts in plans and current state/pointers in issues and OmniFocus. Never confuse a plan commit or task state with code-head certification. Clear context between dispatched child issues.

This handoff publishes documents and tracking only. It does not start the prototype, runtime implementation, performance captures, or an implementation agent.
