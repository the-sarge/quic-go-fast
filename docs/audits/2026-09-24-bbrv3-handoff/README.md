# BBRv3 handoff audit

This is frozen planning evidence for [program QGF-BBR3-20260924](../../adr/2026-09-24-bbrv3-program.md), not a second implementation contract. Runtime tests and native qualification have not run; no implementation was dispatched.

## Inputs and existing-work disposition

Inspected source: `c2157d0804ab6bfeef35fc570b7723a79fd456e9`. The original [Wayfinder map #552](https://github.com/the-sarge/quic-go-fast/issues/552) and [design investigation #558](https://github.com/the-sarge/quic-go-fast/issues/558) supply the accepted decisions and bounded campaign. The user delegated technical choices and accepted the recommendations. This handoff explicitly preserves the ADR 0009 positional-Config-literal exception; keyed configuration construction and ordinary upstream behavior remain the compatibility target.

[Draft PR #582](https://github.com/the-sarge/quic-go-fast/pull/582), head `bd95f6690f68f2d67762823ac8cb4cb09b5d1c72`, contains design documentation only. Its diff and issue/PR history were inspected, with no prior PR comments or reviews. Disposition: **rework and supersede**, incorporating its design/ADR/glossary with the correction below. It is not an established implementation dependency. Close the superseded draft after this publication merges; the resulting default-branch commit, not this source head, becomes dispatch authority.

## Independent design and graph audit

The read-only `design_audit` agent reviewed the pinned source and original design, then every proposed slice and the revised marking representation. Root independently checked the evidence and accepted the following bounded corrections before publication:

| Finding | Disposition and evidence | Resolution |
| --- | --- | --- |
| Capable ECN epoch does not imply contiguous ECT marking | Fix-now: `packet_emission.go:437–511` and `sent_packet_handler.go:287–304,951–958` permit Not-ECT path-probe/coalescing holes in the application packet-number space. The marking ledger is required safety authority. | Actual codepoint/generation/accounting status and affine ordinal ranges, explicit holes/skips, 4,096-range budget, proved-prefix compaction and fail-closed exhaustion. T4 covers validation/migration through the real seam. |
| T2 sampling/idle needs complete local pending evidence | Fix-now: design requires zero pending worker-owned work; queue emptiness or loss-adjusted flight cannot establish that fact. | T3 blocks T2. |
| T4 migration fence needs old-generation local completion | Fix-now: accepted counter fence requires every old local queue entry to complete before revalidation. | T3 blocks T4. |
| Context inventory omitted client path switching and callback replacement | Fix-now: bounded dispatch inputs must include real constructors and transition callers. | T3/T4/B6 include `connection.go:924–940`; B6 includes `server.go:867–956` and transport constructors. |

These are distinct precise invariants/owners: actual ECN marking authority, sampler idle-origin gating, ECN migration drain gating, and process-context completeness. No previous accepted review family purported to close a later counterexample at the same central enforcement seam; no repeated-root conclusion is claimed.

Final independent verdict: all 13 slices pass after those corrections; no additional split or merge is required. T1 delivers inert real dispatch; T2 keeps retention/late discovery/disposal together; T3 keeps admission/completion/destruction together; T4 keeps marking/validation/counter continuity together; T5 emits complete recovery evidence. B1 is private complete Startup/Drain with declared temporary Cruise removed by B2; B2–B5 each add one complete reducer behavior; B6 alone activates public selection after safety predecessors. Q1 may stop for unavailable infrastructure within eight preparation hours. Q2 is one frozen-candidate campaign with a finite ledger and no inline controller fixes. Each plan records the strongest split/merge alternatives and genuine dependency evidence.

The minimal graph removes the redundant direct T3 → B1 edge because T3 → T2 → B1 already enforces it. Frontier is T1/T3/Q1. T2/T4 become parallel after T1 and T3; T5/B1 become parallel after T2. Shared files require worktree/rebase discipline, not fabricated dependencies.

## Context and terminating evidence

The [input inventory](slice-inputs.json) contains source ranges, design-section names, existing test signatures and measured current input sizes. Every source file/range and design heading resolves against the inspected tree. The measurement includes the current slice, common sections/gates, named design sections, relevant ADRs/overlay and listed source/test declarations. All fit below the 35,000 estimated-token ceiling; the remaining reserve is explicitly shared by selected test bodies, predecessor-owned symbols, pinned algorithm excerpts, governing diff and at most 3,000 history tokens. Remeasure after predecessors merge; do not dispatch a manifest that exceeds the ceiling.

| Slice | Current estimated tokens | Reserve below 35,000 |
| --- | --- | --- |
| T1 | 17,356 | 17,644 |
| T2 | 25,540 | 9,460 |
| T3 | 20,098 | 14,902 |
| T4 | 19,736 | 15,264 |
| T5 | 20,060 | 14,940 |
| B1 | 17,283 | 17,717 |
| B2 | 13,838 | 21,162 |
| B3 | 15,441 | 19,559 |
| B4 | 15,580 | 19,420 |
| B5 | 17,537 | 17,463 |
| B6 | 25,750 | 9,250 |
| Q1 | 18,736 | 16,264 |
| Q2 | 17,936 | 17,064 |

Planning acceptance terminates after independent slice/contract review, finite relative-link/graph/source-range checks, exact pushed-head docs certification, and applicable same-head hosted CI. This publication changes only documentation and process metadata; it does not claim runtime correctness or measured performance. A docs PR receives one fully briefed fresh review and at most one replacement; cheap high-confidence docs corrections use the shared no-rerun policy. Review run IDs, final certification and merge receipts belong in PR discussion rather than the normative plans.
