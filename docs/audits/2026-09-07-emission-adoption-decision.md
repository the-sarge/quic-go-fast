# Packet-emission adoption decision — 2026-09-07

The owner agreed to the recommendation to proceed with adoption in stages, beginning with independently useful buffer-lifetime fixes and then the planned sending-path migrations. This is explicit acceptance of the remaining E1 feasibility uncertainty, not a claim that qualification passed.

The decision followed review of both campaigns: the bounded extraction worked in its exercised scope; focused packet/batch allocation counts were unchanged; connection size increased by 48 bytes; throughput remained close to baseline; stream churn repeatedly measured roughly 3–4% slower; low-load probe-p99 uncertainty exceeded its bound; and capture tooling could execute on measured endpoint cores. Churn causation and the magnitude of tooling interference remain unresolved. Linux loopback measurements do not qualify a physical NIC or link.

The owner accepted the ownership and behavioral-testing benefits as sufficient to proceed. The prior positive-E1 prerequisite is replaced by this decision. E1 remains complete with its original inconclusive receipts and inert prototype. No further E1 campaign is authorized. The E2–E6 implementation boundaries, dependency order, correctness obligations, later performance comparisons and review budgets remain unchanged.

The [normative plan](../adr/2026-09-07-packet-emission-plan.md#adoption-decision), [ADR 0004](../adr/0004-packet-emission-ownership.md) and [program frontier](../adr/2026-09-07-packet-emission-program.md) carry the current decision. The E2 product PR publishes this explicitly authorized prerequisite change alongside its own completion/frontier transition; it does not adopt the E1 experimental runtime.
