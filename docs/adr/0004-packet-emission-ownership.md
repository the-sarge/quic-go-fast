---
status: accepted
---

# Deepen packet emission while preserving the Go execution model

A private packet-emission module will own send-capacity coordination, construction through the existing packet packer, recovery registration, accounting, logging, and outgoing buffer handoff. Clearer ownership and easier behavioral testing can justify adoption without a speed gain, subject to the plan’s performance decisions and compatibility gates. The connection goroutine remains the protocol-state owner, and the asynchronous send worker retains socket I/O; a full sans-I/O redesign is a separate decision.

Keep the existing registration point before asynchronous I/O and preserve scheduling, congestion, handshake, path and public-interface semantics. The module must begin before destructive packing; a wrapper around finished packets would leave the difficult ordering contract with callers. Local cleanup corrections needed to establish outgoing ownership are included only with focused failing regressions. Receive ownership, application credit, path-policy reorganization and neutral observability are excluded.

Performance preservation is required in the intended four-core-per-endpoint deployment domain. Two-core operation is not an adoption requirement; historical results at that budget remain diagnostic. This scope choice does not relax compatibility or correctness and does not establish that the existing prototype is qualified. The plan owns the exact workloads and evidence limits.

The [packet-emission plan](2026-09-07-packet-emission-plan.md) is the normative execution contract, composed with ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), and [0003](0003-follow-stable-upstream-releases.md). It uses a disposable feasibility experiment followed by independently green migration slices; each slice keeps one authoritative state owner and a bounded temporary interface. Removing the broad packer interface and mock follows replacement coverage. Two production adapters are not required to justify a useful concrete module.

The owner has authorized staged adoption with the recorded E1 timing and host-isolation uncertainty accepted. E1 evidence remains inconclusive and unchanged; the decision satisfies its migration prerequisite without authorizing another feasibility campaign. Later slices retain their bounded correctness and performance gates. See the [adoption decision](../audits/2026-09-07-emission-adoption-decision.md).
