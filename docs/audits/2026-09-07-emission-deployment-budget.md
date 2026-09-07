# Packet-emission deployment-budget decision — 2026-09-07

The owner clarified after E1 closure: “2-core operation does not matter and is not relevant to my intended deployment”. The current [performance contract](../adr/2026-09-07-packet-emission-plan.md#performance-contract) therefore uses four dedicated physical cores and four Go processors per endpoint, continuing the per-endpoint interpretation used by E1 P3. Two-core performance is diagnostic only. Compatibility, correctness, ownership, the Go execution model and the performance margins within the supported domain are unchanged.

## Basis and scope

The earlier [transmission design](https://github.com/the-sarge/quic-go-fast/blob/a8e40d490c112f5821922f4273fee548235cb4d6/docs/plans/transmission-design.md#frozen-comparisons) selected two cores as a scheduling-sensitive regression condition, based on earlier measurements. It did not establish a deployment need for two-core operation. Making that condition a mandatory adoption gate was an agent-selected design choice under delegated authority; the owner's explicit deployment clarification supersedes it.

This is a prospective contract correction, not a rerun, changed measurement or retrospective passing result. The [E1 receipt](e1-emission/README.md), archived manifests, analyses, raw samples and historical journal remain unchanged. E1 merged in [PR #33](https://github.com/the-sarge/quic-go-fast/pull/33) with an inconclusive disposition under its original contract.

## Evidence disposition

| Evidence or obligation | Current disposition |
| --- | --- |
| Historical P2: 4 Gbps DATAGRAM, two cores per endpoint | Diagnostic only; its latency and missed-probe results no longer block adoption. No two-core diagnosis or rerun is required. |
| Historical P1/P4/P5/P6: other traffic cases, two cores per endpoint | Diagnostic only, including both passes and failures. They do not qualify four-core versions of those workloads. |
| Historical P3: 4 Gbps DATAGRAM, four cores per endpoint | Passed all original margins for the frozen E1 prototype. Applicable evidence for that candidate and workload; it cannot certify a revised runtime candidate. |
| Current matrix | P1/P3/P4/P5/P6 all use four cores per endpoint; retire P2 from acceptance to avoid duplicating P3. Remaining four-core P1/P4/P5/P6 cells are unmeasured. |
| Existing handshake/churn/transfer benchmarks | Retain the shared-process four-core budget and 5% time bound. Stream churn's central ratio was 1.0393 and its one-sided 95% upper bound was 1.1061; the latter exceeds 1.05. This result remains relevant. |
| Prototype result interface | Progress and stop fields remain unused by the caller, capacity resumption remains outside the result and recovery-blocking outcomes are collapsed. The budget clarification does not resolve these limitations. |
| Focused allocation observations | Matching recorded counts retain their incomplete original invocation provenance. The budget clarification does not supply missing metadata. |
| Future slice measurements | E3 uses P3/P4; E4 and E5 use P3 with their existing focused gates; E6 uses the current full matrix. E2 queue microbenchmarks use four cores and four Go processors for their process. |

## Current program state and validation boundary

E1 stays complete, E2 stays independently ready, and E3–E6 stay blocked. Resumed feasibility requires a scoped re-handoff defining a candidate and the remaining finite evidence; this correction does not reopen E1, create another child, reset the original unused replacement allowance or start measurements. [Tracker #31](https://github.com/the-sarge/quic-go-fast/issues/31) is the live frontier.

This change consists only of process and traceability metadata. Local validation is a bounded comparison of the owner decision, all active performance references, the E1 evidence dispositions, source links and Markdown/whitespace integrity. No runtime, host reservation, fixture or archive changes are included. Contract closure is not triggered: no production authority or lifecycle enforcement changes. Hosted checks triggered for this documentation change are inspected on the final head before merge.
