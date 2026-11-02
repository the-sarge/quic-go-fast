# QUIC multipath #646 program

**Identity:** `QGF-MP646-20261024`. **Status:** implementation in progress; R1 complete (1/31 slices). **Scope:** both Reno-first primary/backup and BBR/aggregation. **Tracking issue:** [#646](https://github.com/the-sarge/quic-go-fast/issues/646). The track plans are the current implementation contracts. Historical design/sizing and review notes explain decisions; issues and OmniFocus own current status/pointers rather than mirrored contracts.

## Tracks and frontier

| Track | Plan | Parent | Slices | Current state |
| --- | --- | --- | ---: | --- |
| E — native endpoint | [Endpoint plan](2026-10-24-multipath-endpoint-plan.md) | pending | 6 | E1 frontier; E2 also blocked by Q2 #599 |
| R — Reno primary/backup | [Reno plan](2026-10-24-multipath-reno-plan.md) | [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013) | 19 | R1 complete in [PR #1071](https://github.com/the-sarge/quic-go-fast/pull/1071); R2 frontier; R7 also blocked by Q2 #599 |
| B — BBR/aggregation | [BBR plan](2026-10-24-multipath-bbr-plan.md) | pending | 6 | B1 blocked by R19 |

Current frontier is E1 (endpoint owner, #1015) and R2 (wire parser, #1022), with isolated ownership. R1 #1021 is complete; its [receipt and cumulative operational ledger](../audits/2026-11-01-multipath-r1-receipt.md) record 10 charged instances and 452 host-seconds, leaving 170 instances and 28,348 host-seconds before later concurrent charges. R14 #1034 remains blocked by R12 #1032, so R1 completion creates no newly ready successor. All other descendants remain blocked by their actual slice edges. Q2 #599 remains outstanding; this transition does not grant it readiness.

Cross-track edges: R9 requires E5 (which requires E3); R16 requires E6; B1 requires R19. Within-track edges live in the slice tables. Shared ownership still requires isolated branches/worktrees and current-main source review before a later dispatch; parallel-safe does not permit conflicting ownership or bypassing a direct blocker.

## Sizes and outcomes

There are 31 audited one-PR slices: six endpoint, 19 Reno and six BBR/aggregation. Of these, 27 build runtime/support and four deliver qualification evidence (E6, R17, R18, B5). Reno-first has 22 construction/support plus three qualification slices; BBR adds five plus one. These are actual contract boundaries, not a conversion from line density. The retained engineering forecast is 7,770–15,130 production and 11,500–24,000 test/support edits for Reno including endpoint work; BBR/aggregation adds 1,500–3,300 and 4,000–9,500. Native/interop/performance execution has its separate accepted ceiling in the Reno ledger. Actual slice boundaries and execution attempts are different units; a campaign PR does not establish production size.

No implementation is needed to retain Reno default, reject restricted/custom-I/O/v2 profiles, preserve upstream/ADR policy, distinguish local backpressure from path failure, or keep benefit required before recommending adoption. These are binding boundaries, not future optional tickets. The owner permits exploratory implementation before benefit is measured, and selected both milestones for packaging.

## Binding rules

Every child maps once to a current track-plan slice, one intended PR and one OmniFocus task. Dispatch only a verified frontier child to a fresh agent with `$implement-architecture-slice`; clear context between children. Parent issues are trackers, not implementation tasks. Shared review-loop/contract-closure baselines compose with [the repository overlay](../REVIEW-LOOP.md). Each PR preserves single ownership, authority-complete facts, bounded temporary seams, finite evidence and the accepted representation domain. Review → same-head certification/CI → merge → append-dev-journal → reconcile child/task and real frontier.

Docs must merge before child publication. Exact plan commit belongs in child issues; task notes contain pointers/state only. Current mirror state is administrative and never substitutes for code-head certification. No implementation or experiment is dispatched by this package.
