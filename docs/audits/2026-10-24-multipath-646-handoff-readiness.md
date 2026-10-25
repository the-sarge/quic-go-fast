# Issue #646: architecture-handoff readiness

**Status:** readiness assessment only; no dispatch plan accepted or published. The owner requested architecture-handoff for both Reno-first and later BBR/aggregation under OmniFocus task `glR8nyMhqbd`, then asked whether the request was premature. Current design: `fb0a8d467b83cef67582bf5daac069f2304c310b`; production baseline: `f35df9d7b016a06174288901f3337ccc2fd7347c`. This assessment does not authorize implementation, dispatch or a qualification campaign.

## Assessment

Starting the skill's self-grilling and readiness phase is appropriate. Publishing a dispatch-ready program now would be premature: the design explicitly retains D0 acceptance gates, and its latest response to nine recommendations has not received independent review. The handoff must close design choices before auditing concrete slices. Neither the forecast's candidate slice count nor a successful documentation check establishes that decomposition.

Native qualification results and measured consumer benefit are not automatically prerequisites to packaging. Qualification can be a bounded downstream slice, and Q2 can be an explicit dependency. Before packaging, however, the supported domain, test method, terminating evidence, acceptance policy and sequencing must be settled. Do not create an unbounded research or campaign obligation disguised as one implementation PR.

## Decision inventory

| Area | Current state | Work needed before an audited dispatch plan |
| --- | --- | --- |
| Program scope | Owner selected both Reno-first and BBR/aggregation. | Preserve two explicit milestones; no need to ask again. |
| Local timeout policy | Owner selected nonfatal backpressure for ordinary and multipath peers. | Carry it into the single completion owner and finite acceptance cases; do not reopen it as path failure. |
| Native I/O ownership | Latest design defines independent control/expiry, exclusive native writer, route adapters, conservative probe charging and separate grant-return observation. | Independently audit the revised lifetime, claim/revocation, early-feedback and shutdown contracts. No runtime qualification is claimed. |
| Public surface | Additive API and constructor matrix remain proposed. | Settle signatures, error identities, handle modes and explicit-grant/teardown compatibility. Keep ADR 0001 intact. |
| Finite resources and time | B/default/maximum, tolerance and participant/request/control/completion/diagnostic caps remain D0 parameters. | Produce a coherent bounded parameter proposal and an explicit qualification method. Values require technical justification, not invented measured performance. |
| Backup behavior | Primary/backup is preferred; local backpressure alone cannot promote. Network failure/recovery thresholds, all-backup behavior and liveness budget remain open. | Settle the observable policy and avoid promising a failover latency unsupported by the mechanism. |
| Aggregation and BBR | Later milestone is in scope; earliest-delivery versus round-robin remains an experiment candidate. | Settle bounded policy-selection evidence, shared-bottleneck acceptance and path-scoped sampling/ECN ownership before fixing the later slice graph. |
| Ordinary-peer costs | Design requires acceptable throughput, tail latency and CPU costs for ordinary peers on opted-in endpoints, separately from default-off behavior. | Present a concrete acceptance policy for owner choice; do not silently turn the gate into zero overhead or unlimited regression. |
| Protocol, peer and reuse | Draft-21 and pinned picoquic candidates are documented, including the negotiated-zero oracle gap. Selective Go-source adaptation is still a proposal. | Finish a bounded module-to-owner adaptation mapping and freeze the chosen pins plus independent evidence limitations. |
| Q2 sequencing | Q2 #599 remains outstanding; the design prefers a pinned/completed Q2 baseline before core multipath merges. | Make the blocking edge and permitted independent work explicit. Packaging need not wait for Q2, but must not silently grant qualification readiness. |
| Benefit timing and maintenance | The design permits choosing exploratory implementation before benefit evidence or requiring current-code baselines first. ADR 0003 remains accepted. | Record the benefit-timing decision; preserve stable-upstream policy unless explicitly changed. No fork-policy change follows from invoking handoff. |
| Slice granularity | 21–47 Reno-first and 25–58 full are unaudited forecasts. | Draft and independently audit real vertical slices, context/evidence budgets, transitional seams and necessary blocking edges only after the governing choices are coherent. |

## Recommended order

1. Complete a bounded D0 design pass: make evidence-backed technical choices and present only unresolved product/acceptance choices to the owner.
2. Obtain one bounded independent consideration of the resulting current design, including both milestones and the nine resolved I/O recommendations. Disposition findings against that contract; do not begin an implementation or automatic fixer loop.
3. Resume architecture-handoff: draft current plans and slice graph, perform its separate slice-decision audit, then publish and merge docs before creating issue and OmniFocus dispatch pointers.

## State at this assessment

The existing design draft PR is [#1010](https://github.com/the-sarge/quic-go-fast/pull/1010). Issue [#646](https://github.com/the-sarge/quic-go-fast/issues/646) remains a sizing/design exploration with no child program. The supplied OmniFocus parent has no children and an empty note. No plan, issue, readiness label or OmniFocus task was published or changed by this handoff attempt. No implementation or dispatch agent was launched. A dedicated feature worktree was created for preparatory documentation.

The normative current design's remaining choices are in [the bounded-I/O contract](../designs/quic-multipath.md#bounded-native-io-and-endpoint-ownership), [scheduling policy](../designs/quic-multipath.md#scheduling-retransmission-and-datagram-policy) and [decision gates](../designs/quic-multipath.md#consumer-evidence-and-decision-gates). Prior review evidence and its limitations remain in [the third consideration response](2026-10-24-multipath-646-third-consideration-response.md).

## Subsequent owner direction

The owner chose to finish D0 and one final consideration before resuming handoff, retaining both milestones. The owner then accepted exploratory implementation before measured consumer benefit, with benefit required before recommending adoption, and accepted the D0 draft's ordinary-peer/aggregation tolerances plus the future 180-run/eight-host-hour qualification ceiling. These choices authorize the design package and future budget, not implementation or experiment execution now. See the [current D0 contract](../designs/quic-multipath-d0.md); the inventory above records why the original immediate-publication framing was premature.

The subsequent [final consideration response](2026-10-24-multipath-646-final-consideration-response.md) closes the D0 decisions, and the [independent slice audit](2026-10-24-multipath-646-slice-audit.md) accepts the 31-slice graph. The [program](../adr/2026-10-24-multipath-program.md) is the current package; this assessment remains the historical reason immediate publication was premature. Native and performance qualification are still future evidence, not accomplished results.
