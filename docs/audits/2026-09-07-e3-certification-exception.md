# E3 historical-failure exception — 2026-09-07

## Operator decision

The operator approved the proposed path in the E3 implementation conversation: track the unresolved randomized timeout separately, accept uncertainty around that one historical failure, allow one fresh full-suite certification on the final candidate, and merge E3 only if that run and the required same-head checks pass. A new failure stops progress for diagnosis; repeated attempts until green are not authorized. The user also authorized resuming the existing implementation workflow after publishing this scoped decision.

This is an explicit, narrow exception to the shared review-loop rule that an unresolved required test failure cannot be discharged by passing repetitions. The original failure is retained as failed and unexplained, not dismissed as infrastructure or proven harmless. An E3 regression remains possible. The separate investigation is nonblocking only because the operator accepted that uncertainty; its future work is not used to promise missing E3 certification.

## Evidence and scope

[PR #41's original stop](https://github.com/the-sarge/quic-go-fast/pull/41#issuecomment-5575062061) records the failed macOS Go 1.27.0 full suite and passing independent checks, Linux suite and 33 hosted checks. The retained remote runtime head is `e2b5c7f471214dc05ef08f0d2decd2d41b3f9686`. The completed initial RAS review has no accepted fixes or deferred findings; E3's P3/P4 campaign and focused allocation gates passed without consuming its replacement allowance.

The [bounded campaign receipt](https://github.com/the-sarge/quic-go-fast/pull/41#issuecomment-5575649115) records 100 exact-subtest invocations, one disposable observation patch, all passing, no informative capture and no cause. The failing path was `TestHandshakeWithPacketLoss/drop_1/3_of_packets_in_direction_to_server/retry:_false/server_speaks_first#02`. The selected scenario uses post-quantum TLS, a short certificate chain, no retry, 5000-byte server-first transfer and a two-minute simulated timeout. The fixture's random-loss callback affects both actual directions despite the label; that behavior remains unchanged. The observations are diagnostic only. No causal replay allowance was spent because no captured failure supported attribution.

The clean retained local implementation worktree is at `6e184c3954e6d5300d0cb31744d5f849c9f6df56`, after merging the preceding docs-only handoff. Its Go/module bytes match the remote runtime head. Retain that work and one product PR; reconcile this decision's docs before freezing the final candidate. Raw evidence remains in the durable local directory named by the campaign receipt. Current policy belongs in the normative [E3 slice](../adr/2026-09-07-packet-emission-plan.md#e3--own-normal-and-gso-packet-emission); this document owns decision rationale and historical receipts.

## Scoped slice audit

| Gate | Disposition |
| --- | --- |
| Outcome and existing work | Retain E3's ordinary/GSO owner, caller-buffer cleanup, tests and PR #41. One passing fresh certification remains mandatory before merge. |
| Blockers | E2 and accepted E1 uncertainty are satisfied. E4/E5 remain blocked by E3; E6 requires E4/E5. The separate timeout investigation is not a new architecture child or blocker. |
| Ownership and authority | One packer, recovery instance and queue on the connection goroutine. No runtime, representation, durable-state or security authority changes. |
| Temporary seams | Preserve the five synchronous policy operations and typed state slots, short-header registration bridge, and legacy handshake/ACK/PTO/probe/close/token/queue paths. E4/E5 narrow them; E6 removes them. |
| Preservation and TDD | Retain all tests and scenario semantics. This docs/evidence decision adds no code correction to make red. The one uncached full suite plus exact-head focused/static and hosted gates must pass; any new failure stops. |
| Artifact and representation | Docs/receipt/follow-up are process metadata; tests and observations are verification aids, with no maintained-aid commitment. Existing packer/recovery and simnet types retain representation ownership. Universal ownership duties and example-level observations are unchanged. |
| Closure and evidence | E3's existing outgoing-buffer matrix remains covered by the retained outcome tests. No new invariant or diagnostic-aid closure is created. Exactly one fresh macOS full suite; no renewed capture or performance campaign and no reset of review budgets. |
| Blast radius and unknowns | No runtime changes. Historical timeout causality and whether E3 contributed remain unknown and explicitly accepted by the operator. Physical-link/application performance and unmigrated ownership families retain their existing non-goal status. |
| Context fit | At most 30k continuation input tokens of current contract, bounded docs diff, compact unresolved evidence and relevant source/results. No full historical plan or campaign ingestion. |
| Strongest split/merge cases | A separate investigation is coherent because it owns accepted historical uncertainty, not a promised future prerequisite for E3's new certification. Merging with E4 would widen runtime ownership without causal evidence. Retain one independently gated E3 product PR. |

The author audits this scope against both shared baselines. Independent read-only slice audit passed against pinned base `02ae25e24bc5104a88dacec4b65c457efffa31f8`, with no actionable findings. It confirmed the bounded exception, frozen-head gate, unchanged runtime/test and ownership boundaries, and publication-before-certification order. Source/link/format and whitespace inspection terminate this docs-only local gate; inspect all applicable triggered hosted checks before guarded squash publication. No product certification runs until the decision is published and its child pointer synchronized.
