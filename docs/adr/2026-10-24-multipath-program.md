# QUIC multipath #646 program

**Identity:** `QGF-MP646-20261024`. **Status:** N6, N7, N8, N9, N10 and N11 complete; 36 of 46 slices complete. **Tracking issue:** [#646](https://github.com/the-sarge/quic-go-fast/issues/646). Track plans own substantive contracts. Issues and OmniFocus own live mappings/state; historical receipts stay behind audit links. Public endpoint activation and multipath negotiation remain disabled until R19 #1039. This package reports frontier work and dispatches no implementation.

## Tracks and frontier

| Track | Normative plan | Parent | Slices | Current state |
| --- | --- | --- | ---: | --- |
| E — native endpoint | [Endpoint plan](2026-10-24-multipath-endpoint-plan.md) | [#1012](https://github.com/the-sarge/quic-go-fast/issues/1012) | 9 | 9/9 complete; N3 supplies affected combined native requalification |
| R — Reno primary/backup | [Reno plan](2026-10-24-multipath-reno-plan.md) | [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013) | 20 | 17/20 complete; R17 #1037/R18 #1038 wait for N3 #1202; R19 #1039 requires both |
| B — BBR/aggregation | [BBR plan](2026-10-24-multipath-bbr-plan.md) | [#1014](https://github.com/the-sarge/quic-go-fast/issues/1014) | 6 | 0/6; B1 #1040 retains R19 #1039 and Q2 #599 prerequisites |
| N — endpoint efficiency/admission | [Efficiency and pipeline plan](2026-11-04-native-endpoint-efficiency-plan.md) | [#1193](https://github.com/the-sarge/quic-go-fast/issues/1193) | 11 | 10/11 complete; N11 #1240 is complete in PR #1278; N3 #1202 is the frontier |

N0 #1199, N1 #1200, N2 #1201, N4 #1213, N6 #1235, N7 #1237, N8 #1238, N9 #1236, N10 #1239 and N11 #1240 are complete. Existing N3 #1202 is the frontier for synchronized control/pump and combined qualification after its completed predecessors. Six new slices N6–N11 replace the unbounded remainder; withdrawn N5 is unused. N6→N7→N8 and N9→N10 are complete; Completed N11 #1240 joins their request and recovery branches before frontier N3 #1202. Shared source overlap requires reconciliation, not extra blocking edges. No diagnostic branch is an established baseline. The [replacement slice audit](../audits/2026-11-05-native-pipeline-slice-handoff.md) records the current source census, independent reviews and precise disposition of older requirements.

R17 #1037 and R18 #1038 become startable after N3's runtime change and native evidence merge. They can proceed independently but must serialize shared measurement-host occupancy. R18 consumes the existing N0 fixture and retains 36 counted observations with offered auxiliary/background progress reporting. Parent #1193 stays open until all eleven children and fixed-candidate R18 satisfy the ordinary tolerances. [Admission tracker #1141](https://github.com/the-sarge/quic-go-fast/issues/1141) retains its independent criteria through N2, N3's combined contention evidence and R18; neither acceptance tracker blocks its own runtime or qualification child.

Other cross-track edges remain unchanged: R9 consumes E5; R16 consumes E6; B1 #1040 consumes R19 #1039 and its [single-path Q2 prerequisite](2026-10-24-multipath-bbr-plan.md#goal-and-current-shape), [#599](https://github.com/the-sarge/quic-go-fast/issues/599). Full Q2 remains deferred/incomplete; a bounded GridCast application gate is not a replacement receipt. The independent [operation-release follow-up (#1192)](https://github.com/the-sarge/quic-go-fast/issues/1192) is complete in [PR #1195](https://github.com/the-sarge/quic-go-fast/pull/1195); its ordering remains preserved.

## Sizes, evidence and no-code outcomes

There are 46 intended slice PRs: nine E, twenty R, six B and eleven N. Thirty-six are complete. New N6–N11 are ownership-complete implementation slices, not six independent performance hypotheses or six required benchmark campaigns. The current [clean feasibility receipt](../audits/2026-11-05-native-protection-placement-prototype.md) supports the selected design on one ordinary cell; source identities, historical failed/contaminated results and accounting remain in audits. Each real implementation and final integration still needs its own exact-head evidence.

The [Reno ledger](2026-10-24-multipath-reno-plan.md#candidate-and-run-ledger) remains historical accounting. The [scoped owner waiver](2026-11-04-native-endpoint-efficiency-plan.md#owner-waiver-and-finite-operational-scope) governs this repair and dependent R17 #1037/R18 #1038 qualification without a replacement cumulative cap. Required inventories, thresholds, contamination classification, accepted venues and per-invocation timeouts remain intact. Every attempt is charged. Other tracks retain their existing scope/authority.

No implementation is needed to retain Reno default, reject unsupported custom-I/O/platform/v2 profiles, preserve settled ADR/public compatibility, distinguish local backpressure from path failure, or require comparative benefit before recommending adoption. The [five-stack comparison for issue #1232](../audits/2026-11-05-send-pipeline-comparison-1232.md) supplies design input; foreign prepared-as-sent semantics and incompatible blocking/outcome assumptions are not adopted. Native ownership, independent interop, ordinary costs and BBR evidence remain distinct.

## Binding rules

Every audited slice maps once to its plan contract, one child issue/intended PR and one OmniFocus task. Parent issues are trackers. Dispatch each verified frontier child to a fresh agent with `$implement-architecture-slice`; clear context between children and reconcile current main in an isolated worktree. The package itself dispatches none.

The shared review-loop and contract-closure baselines compose with [the repository execution overlay](../REVIEW-LOOP.md). Preserve single mutation ownership, authority-complete facts, bounded temporary seams, declared representation domains, proportionate finite evidence and precise-root approach stops. Review, exact-head local certification and applicable hosted checks precede squash merge, DEV-JOURNAL and pointer reconciliation.

Docs merge before issue/task publication. Child issues record the exact reachable default-branch plan commit; mirrors carry pointers/state without copied contracts or exact plan SHAs. Readiness is administrative metadata, not code certification. Immediately before reporting frontier readiness, verify the recorded plan commit remains reachable from current remote main and contains the exact accepted slices.
