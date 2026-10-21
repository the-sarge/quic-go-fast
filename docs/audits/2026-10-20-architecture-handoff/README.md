# A20 architecture handoff audit history

This file is process/traceability metadata, not an implementation contract or a maintained verifier. The current [program](../../adr/2026-10-20-architecture-deepening-program.md) and five plans are normative. Do not append review chronology to plan slices.

## Source and existing-work capture

- Grilled report source: `d791891fda2b7b0a2d295002b52a9edf1f8a8c7a`; final Astra v7 incorporated Sol’s post-tracking admission and network-owned shutdown, and Opus’s protocol and BBR documentation findings.
- Current planning base: `5f73ddc4de39268992f0b9ab33eed70c472955b7`. The intervening merged #855/#920 changes are unprocessed/datagram queue admission, not implementation of any selected A20 outcome. The M1 test must use the current baseline fixture behavior.
- Initial checkout remained read-only. Docs worktree: `/Volumes/worktrees/quic-go-fast/architecture-handoff-v7`, branch `codex/architecture-handoff-v7`.
- Supplied OmniFocus parent `lLz8Ew7Vzvj`, “2026-10-20 - architecture improvement”, was empty at capture. This is a new program, not a legacy rebaseline.
- Open PR census found #835 (BBR campaign documentation) and #922 (#855 journal), neither implements these tracks. Open issue/title searches found no overlapping QPACK or logging-retry child; the Simnet title matches #406/#410 are closed worker-failure-reporting tasks, not owned-delivery cancellation. No unmerged implementation is treated as a baseline.
- Existing publication blocker: #921 records CI’s vulnerable Go toolchain pins; the PR-side lint check of unrelated docs PR #922 fails. The docs PR must obtain its own check result; this observation grants no bypass or unrelated implementation authority.

## Diagnostic evidence carried forward

The earlier temporary Go overlay diagnostics ran against `d791891f` with Go 1.27.2 darwin/arm64. Simnet: 16 unread packets allowed Close, while 33 delivery attempts into the 32-slot endpoint durably blocked it under synctest; closing endpoints released workers. HTTP/3: real QUIC/TLS ClientConn.RoundTrip paused after OpenStreamSync and before TrackStream; close then resume yielded ordinary wrapped error/retry-ineligible without qlog and errConnUnusable/retry-eligible with qlog, with no post-close recorder events. Both families also ran under `-race` with the same observations and no reported race. These are characterizations, not tested fixes. They did not complete a second Transport dial or attribute a historical CI incident. Implementers must write normal repository regressions as specified by the plans; temporary files are not normative dependencies.

## Representation decision during handoff

Read qpack v0.6.0 decoder.go and varint.go plus the upstream default decoder and latest-release metadata. Invalid table references and integer overflow are private errors, and the latest release was still v0.6.0. A small consumer-only classifier cannot claim the required scope reliably. We explicitly defer that portion, prefer an upstream typed error contract, reject implicit parser ownership, and retain P1/P2 as independently green repairs. This is a newly resolved representation gate, not a hidden implementation assignment. H1 has no protocol prerequisite because its initial-header lifetime can preserve the present disposition independently.

## Audited slice graph

R1, S1, P1, P2, H1 and M1 each have zero blockers; P1/P2 share a file and HTTP/3 work is recommended serially, not falsely blocked. S1/M1 are parallel-safe with one HTTP/3 slice. Each maps to one future child and PR. QPACK and opportunistic send checks have no child. Further split/merge choices, owner design, context budgets and semantic evidence are in each exact slice.

## Independent slice audit

Three fresh read-only reviewers audited the complete six-slice graph against source commit `5f73ddc4de39268992f0b9ab33eed70c472955b7`. No reviewer ran implementation, builds, tests, measurements or repository mutations. The primary agent independently accepted and corrected the findings below before committing the plans. All six slices pass; no additional dependency, split, parser owner or evidence expansion is justified.

| Review | Outcome | Independently dispositioned findings and evidence |
| --- | --- | --- |
| HTTP/3 admission and header lifetimes | R1 passes; H1 passes with bounded evidence correction | **fix-now:** existing timeout tests do not discriminate successful decode followed by a lost timer stop. H1 now uses one already-budgeted blocked-qlog case to check early quota release and absence of handler activation after timeout. R1's complete Transport retry can use the existing private newClientConn seam and real connections, with no production hook. |
| Protocol disposition and QPACK boundary | P1/P2 pass after contract wording corrections; QPACK deferral passes | **fix-now:** CancelWrite after Close can abort outstanding delivery, so P2 invokes the existing tracking cancellation methods without extra half-open state and limits peer-reset assertions to observable directions. **fix-now:** finite authoritative parser/validator results are covered by focused tests; multiple consumers alone do not trigger formal closure. P2 wraps only semantic exits inside parseTrailers, preserving shared validators, qlog, size and decoder behavior. Removed copied connection-close wording from P2. |
| Simnet, BBR observation and full graph | S1, M1 and zero-edge graph pass | S1's private cancellable delivery path preserves endpoint ownership and excludes arbitrary synchronous callbacks. M1 can observe the actual installed closure through DeliveryIdle()/DeliveryStats().Idle after ObserveDeliveryStop. Incorporated that concrete consumer pointer without adding runtime hooks or evidence. |

A narrow follow-up with the lifetime reviewer confirmed the same closure-policy correction for R1/H1: their synchronous changed owners and existing finite cases do not meet the baseline's extra condition that ordinary focused tests are insufficient. Their tables remain ordinary semantic test plans. Representation, artifact and future finding-disposition gates remain in force; this is not a relaxation of the accepted behavior.

Each finding is within the documentation contract and was resolved by a small plan edit. There is no repeated runtime failure root or reason to expand the slice boundary. The fresh slice audits and targeted correction review satisfy the independent source-contract review; lightweight docs checks certify the final text, with no extra RAS review/verify cycle under the shared docs-only policy.

Before documentation commit, the branch was fast-forwarded to `e0145b2e` after verifying the sole intervening change was the #922 journal entry. The pinned source facts and audit remain valid. The documentation PR must still satisfy exact-head local certification and applicable hosted checks; no issue, readiness label, OmniFocus write or implementation dispatch is allowed before merged-plan verification.
