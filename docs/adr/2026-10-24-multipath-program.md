# QUIC multipath #646 program

**Identity:** `QGF-MP646-20261024`. **Status:** owner-approved supplemental N-track handoff; implementation in progress, with 28 of 39 slices complete. **Tracking issue:** [#646](https://github.com/the-sarge/quic-go-fast/issues/646). The track plans own current outcomes, invariants, evidence and stops. Issues and OmniFocus own current state and live child mappings; historical review and qualification receipts remain behind the plans' audit links. Public endpoint activation and multipath negotiation remain disabled until R19 #1039.

## Tracks and frontier

| Track | Normative plan | Parent | Slices | Current state |
| --- | --- | --- | ---: | --- |
| E — native endpoint | [Endpoint plan](2026-10-24-multipath-endpoint-plan.md) | [#1012](https://github.com/the-sarge/quic-go-fast/issues/1012) | 9 | 9/9 complete; N3 supplies affected native requalification after its control change |
| R — Reno primary/backup | [Reno plan](2026-10-24-multipath-reno-plan.md) | [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013) | 20 | 17/20 complete; R17 #1037 and R18 #1038 on hold until N3 merges; R19 #1039 still gated by both |
| B — BBR/aggregation | [BBR plan](2026-10-24-multipath-bbr-plan.md) | [#1014](https://github.com/the-sarge/quic-go-fast/issues/1014) | 6 | 0/6; B1 #1040 requires R19 #1039 and its existing Q2 #599 receipt |
| N — endpoint efficiency/admission | [Efficiency plan](2026-11-04-native-endpoint-efficiency-plan.md) | [#1193](https://github.com/the-sarge/quic-go-fast/issues/1193) | 4 | 2/4 complete: N0 #1199 and N1 #1200; N2 #1201 is the frontier; N3 #1202 still requires N2 |

N0 #1199 published the ordinary-cost driver without running a campaign. N1 #1200 completed sparse participant traversal under the existing endpoint owner. N2 #1201 implements the approved public/stateless admission contract for issue #1141 and is the remaining frontier. Reconcile its endpoint code/tests with current main. N3 consumes all three and removes control command/ack rendezvous while preserving independent timer/native progress. R17 #1037 and R18 #1038 become startable after N3's combined runtime change and native evidence merge, not after parent #1193 closes.

Parent #1193 stays open until its four children and the fixed-candidate R18 result satisfy the ordinary tolerances. Issue [#1141](https://github.com/the-sarge/quic-go-fast/issues/1141) remains a linked acceptance tracker through N2's implementation, N3's combined native contention evidence and R18's affected ordinary/auxiliary workload. It is not a second implementation dispatch or a runtime blocker. This preserves independent acceptance criteria without creating a qualification cycle.

R17 #1037 and R18 #1038 may proceed independently after N3; shared native/performance host occupancy is serialized and charged. R18 consumes N0's merged driver and retains its existing 36 counted observations, including offered auxiliary traffic and background progress reporting. No unmerged fixture branch is an established baseline. The independent follow-up [issue #1192](https://github.com/the-sarge/quic-go-fast/issues/1192) is complete in [PR #1195](https://github.com/the-sarge/quic-go-fast/pull/1195); its accepted operation-release ordering is preserved.

Cross-track edges retained from the accepted program: R9 consumes E5; R16 consumes E6; B1 #1040 consumes R19 #1039 and the existing [single-path Q2 prerequisite](2026-10-24-multipath-bbr-plan.md#goal-and-current-shape), [#599](https://github.com/the-sarge/quic-go-fast/issues/599). Full Q2 remains deferred and incomplete; the bounded GridCast application gate is not a replacement receipt or automatic qualification of a later candidate. Completed E/R work is retained. Current within-track edges live in the slice plans; issue relationships record the live frontier.

## Sizes, evidence and no-code outcomes

There are 39 audited intended PRs: nine endpoint, 20 Reno, six BBR/aggregation and four supplemental endpoint slices. N0 extracts the fixture publication from R18; R18 remains its existing qualification/receipt slice. N1, N2 and N3 are new runtime slices. Forecast edit counts and historical review chronology are not dispatch contracts.

The [Reno ledger](2026-10-24-multipath-reno-plan.md#candidate-and-run-ledger) remains the single operational ceiling. The [efficiency audit receipt](../audits/2026-11-04-native-endpoint-efficiency-handoff.md) records 101/180 instances and 1,381/28,800 host-seconds already used. N allocates at most 19 additional setup/diagnostic/affected-native instances within that ceiling; the prior 124 scheduled remaining instances plus 19 require 143 against 79 currently available. The resulting 64-instance program shortfall is explicit; no ceiling increase, skipped qualification or whole-program execution readiness follows from this handoff. The bounded Reno continuation can fit if its recorded allowance is preserved; every execution rechecks the actual ledger.

No implementation is needed to retain Reno default, reject unsupported custom-I/O/platform/v2 profiles, preserve upstream/ADR compatibility, distinguish local backpressure from path failure, or require comparative benefit before recommending adoption. Native ownership, independent interop, ordinary costs and BBR/aggregation evidence remain distinct gates. New support claims require their actual accepted evidence.

## Binding rules

Every audited slice maps once to a current normative plan contract, one child issue, one intended PR and one OmniFocus task. Parent issues are trackers, not implementation dispatches. Use `$implement-architecture-slice` on one verified frontier child in a fresh context; respect actual blockers and reconcile against current main in isolated worktrees. Shared source ownership does not authorize conflicting edits or add convenience-only dependencies.

The shared review-loop and contract-closure baselines compose with [the repository execution overlay](../REVIEW-LOOP.md). Preserve single mutation ownership, authority-complete facts, bounded temporary seams, finite evidence, declared representation domains and precise-root approach stops. Review, exact-head local certification and applicable hosted checks precede squash merge; then append-dev-journal and reconcile child/task/frontier state.

Docs merge before child publication. Children record the exact reachable default-branch plan commit; task notes contain pointers/state without copied contracts or exact plan SHAs. Mirror state is administrative metadata, never code-head certification. This package dispatches no implementation or experiment.
