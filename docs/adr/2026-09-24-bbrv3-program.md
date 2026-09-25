# BBRv3 implementation program

**Program:** `QGF-BBR3-20260924`
**Status:** T1/T2/T3/T4/T5 and B1/B2/B3/B4 complete; current frontier B5 and Q1
**OmniFocus parent:** `omnifocus:///task/aaS2XrzGGmp`
**Tracking issue:** [#600](https://github.com/the-sarge/quic-go-fast/issues/600)
**Audit history:** [Handoff audit](../audits/2026-09-24-bbrv3-handoff/README.md)

## What this is

This program packages the accepted [opt-in BBRv3 design](../designs/bbrv3.md) into one intended PR and one fresh implementation context per slice. Track plans hold the current contracts; issues and OmniFocus hold live state/pointers. The operator chooses dispatch. This handoff launches no implementation.

## Outcomes requiring no implementation

Keep Reno as default, pinned algorithm authority, receiver-defined goodput, existing metadata/ownership boundaries and the already accepted evidence budget. Reject public partial BBR, silent upstream changes, a global selector, extra raw ECN authority and cross-repository rollout. Native host/tool availability is not established by a planning label. The prior Wayfinder map is complete planning evidence, not an implemented controller.

## Tracks, dependencies and frontier

| Track | Plan | Parent issue | Slice count | State |
| --- | --- | --- | --- | --- |
| T: Transport feedback and send ownership | [Transport feedback and send ownership](2026-09-24-bbrv3-transport-plan.md) | [#584](https://github.com/the-sarge/quic-go-fast/issues/584) | 5 | T1/T2/T3/T4/T5 complete |
| B: Complete opt-in BBRv3 sender | [Complete opt-in BBRv3 sender](2026-09-24-bbrv3-controller-plan.md) | [#585](https://github.com/the-sarge/quic-go-fast/issues/585) | 6 | B1/B2/B3/B4 complete; B5 ready; B6 blocked |
| Q: Qualification readiness and evidence | [Qualification readiness and evidence](2026-09-24-bbrv3-qualification-plan.md) | [#586](https://github.com/the-sarge/quic-go-fast/issues/586) | 2 | Q1 ready |

Current frontier: **B5 and Q1**. T1, T2, T3, T4, T5, B1, B2, B3 and B4 are complete. B5 composes loss undo and persistent-congestion restart with the private controller’s independent CE caps; Q1 inspects readiness without changing controller code. B6 remains blocked by B5. Use dedicated worktrees and reconcile shared interface touches before merge. No whole-track sequential ordering is implied.

| Slice | Exact blockers |
| --- | --- |
| T1 — Capture logical congestion feedback without changing Reno | None |
| T2 — Deliver bounded registration-time sampling through real recovery | T1, T3 |
| T3 — Bound paced local sends across the complete worker lifetime | None |
| T4 — Validate bounded actual ECN marking through path changes | T1, T3 |
| T5 — Emit bounded persistent-congestion and recovery evidence | T2 |
| B1 — Drive private BBR Startup and Drain through transport feedback | T2 |
| B2 — Complete draft ProbeBW cycling and congestion bounds | B1 |
| B3 — Integrate guarded ProbeRTT and genuine idle restart | B2 |
| B4 — Apply persistent classic-ECN response in every BBR phase | B3, T4 |
| B5 — Compose loss undo and persistent-congestion restart | B4, T5 |
| B6 — Expose complete per-connection BBR selection and verify migration | B5 |
| Q1 — Establish the bounded native campaign prerequisites | None |
| Q2 — Run the accepted qualification campaign and publish evidence | Q1, B6 |

After T1 and T3, T2 and T4 are logically parallel (both may touch recovery, so review/rebase shared edits); Q1 remains independent. B1 requires T2 (and therefore T3); T5 can run alongside B1 after T2. B2 → B3 → B4 → B5 → B6 is ordered by actual phase/cap/recovery activation obligations; T4 also blocks B4 and T5 blocks B5. Q2 requires Q1 and B6. No convenience-only dependency is added to serialize shared filenames.

## Binding rules

Plans and the accepted design compose with ADRs 0001/0002/0004/0007/0009, the shared review-loop/contract-closure baselines and the repository execution overlay. Single-owner, authority-complete, context-sized and bounded evidence gates apply. Private/inert predecessor slices must not expose incomplete BBR to public callers. No consumer changes, default switch, fleet purchase or rollout is authorized. Required correctness remains a hard gate; performance adoption remains human judgment.

Each child issue records the exact merged default-branch plan commit and `$implement-architecture-slice`. Parent issues are not dispatch targets. Per slice: accepted review loop → same-head certification/CI → guarded merge → append-dev-journal → update blockers/frontier → complete its OmniFocus task. Child implementation may perform same-child scoped re-audits only within the representation/outcome/one-PR boundary; topology or adjacent-scope changes return to architecture handoff.
