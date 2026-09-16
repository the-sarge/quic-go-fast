# Packet-I/O evidence revision audit — 2026-09-15

## Scope and existing-work disposition

This scoped architecture-handoff revision responds to the owner's instruction to reuse the fork's existing performance testing and to retain the wiremux wrapper after P02. It changes future evidence and blocking edges, not runtime code or the complete delivery destination. E01, Q01, W01/W02, R01-A/B, R02 and P01-A/B retain their completed status and original certification. No implementation agent is dispatched.

Reviewed wiremux main `df978a44ffb836186836165fac49476d98dfd2ea`, fork main `58afb1d21c5feaadc4235fe961cbd5f0a7a9d339` and existing issue/PR state. E01-L/W/D (#1524/#1525/#1526) were open with no captures or implementation comments; retire them as superseded, using not-planned issue closure and dropped OmniFocus tasks after publication. Do not fabricate baseline receipts or complete them as measured. Preserve their stable IDs and historical issue pointers.

P02 #1528 is already in progress; [PR #1551](https://github.com/GridSwarm/wiremux/pull/1551) at inspected head `b94a09a01a57ae00430154ad581c6c0fd43a1106` is a documentation-only retention decision. Its 60 physical-Mac captures did not establish a resource benefit or qualified preservation; coarse latency resolution and incomplete timing qualification remain limitations. A separate two-capture TLS timing pilot is not pooled. The owner selected retention. The experimental native path is not adopted; existing runtime wrappers remain. Keep its original immutable protocol, raw archives and conclusions. P02's owner still owns review, validation, merge and journal; this revision neither marks it complete nor re-dispatches it. There is no unresolved runtime diff to grandfather or repeated-root conclusion in this revision.

## Reused primary evidence

| Capability | Existing receipt and measured revision | What transfers; what does not |
| --- | --- | --- |
| Linux GRO | [G2](https://github.com/the-sarge/quic-go-fast/blob/5b2e7d2f1f13a796fd56918b8f8d528f8f90ca0d/docs/audits/2026-09-11-g2-gro-results.md), `7306e2c6719c4bf9f0638747b2afaa78b7dea707` | Native Linux x86_64 loopback protocol, engagement, syscall ratio 0.263 and throughput ratio 1.259; not full-Wire or wrapper performance |
| Windows USO | [W2](https://github.com/the-sarge/quic-go-fast/blob/5b2e7d2f1f13a796fd56918b8f8d528f8f90ca0d/docs/audits/2026-09-11-w2-uso-results.md), `1af918b1d4c9b1d96f54072a72609fbb444981c5` | Existing segmented-send adoption, submission ratio 0.0815 and loopback throughput ratio 3.01; not application throughput |
| Windows URO | [W3](https://github.com/the-sarge/quic-go-fast/blob/5b2e7d2f1f13a796fd56918b8f8d528f8f90ca0d/docs/audits/2026-09-11-w3-uro-results.md), recorded `6b480b91` | Proven separate Windows receiver/Linux sender venue, native coalescing and guest/host limits; same-host URO failure need not be rediscovered |
| Darwin batch send | [D1](https://github.com/the-sarge/quic-go-fast/blob/5b2e7d2f1f13a796fd56918b8f8d528f8f90ca0d/docs/audits/2026-09-12-d1-sendmsgx-results.md), recorded `1af9bb81` | Qualified Darwin 25 arm64 success path, send-call ratio 0.1262 and loopback throughput ratio 1.1733; no new OS/architecture claim or current full-Wire result |

These receipts disclose their own review-time source changes and finite budgets. Reuse follows a source-delta check, not an assumption that old measured commits equal current main. Their old toolchains are historical provenance, not permission to build future work below today's approved floor. Existing parser/allocation receipts and fallback tests likewise remain usable within their recorded domains; none substitutes for new external-wrapper or managed-handback evidence.

## Obligation disposition and slice audit

| Affected obligation | Disposition and genuine dependency | Bounded remaining evidence |
| --- | --- | --- |
| E01 protocol | Retain completed documentation and historical identities; amend future collection rules only | Current docs/source/protocol consistency review |
| E01-L/W/D | Retire standalone baseline campaigns; they own no product behavior | Existing adoption receipts plus explicit retirement ledger; no replacement slices |
| Q02/Q03/Q04/Q05 | Retain product contracts, single kernel owner and Q01 dependency; remove baseline edges | Named focused native positive/failure/engagement regressions; no standalone performance campaign |
| R01-L/W | Retain R01-B and Q03/Q04 dependencies, persistent normalization and generation owner | Queued coalesced handback, ordinary next read/lease, cancellation and terminal close on native platforms |
| R03 | Retain raw-return decision and R01-L/W dependencies | Existing one hypothesis/at most one plausible native experiment per platform; rejection remains a complete outcome |
| P02 | Retain wrapper decision; preserve ongoing documentation PR and original experiment | Existing PR's docs and completion gates, no new experiment or runtime path |
| E02-L/W/D | Retain all assembled runtime blockers including P02 completion; remove baseline edges | Representative compatibility/lifecycle checks and one control/candidate campaign, three paced cells × ten pairs, max 60 captures/platform |
| E02 aggregation | Retain platform receipts and R03 decision dependencies | Reconcile native support, opt-in/default decisions and unavailable/inconclusive/blocked rows once; no captures |
| L01/L02, C01 children, Z01 | Retain complete release, consumer, rollback and closeout sequence | Existing release/consumer gates; Z01 also verifies retired baseline dispositions |

The strongest case for keeping standalone baselines is current full-Wire comparability. A final same-source matched control/candidate campaign supplies that evidence without old-source campaigns or triples; it deliberately makes no upstream-versus-entire-fork claim. Splitting native correctness from each implementation would allow unused extensions to count as delivered, so retain those checks in Q/R slices. Merging all platform work would combine independent owners and venues, so keep platform slices. Splitting another statistics/harness project would put an unapproved verification aid on the critical path; unavailable metrics terminate as explicit limitations with ordinary defaults. Observed regressions and absent native qualification still block their affected support rows.

One stable child maps to one intended product/evidence PR; the three retired IDs require no implementation PR. Track W keeps 19 stable IDs with 16 required delivery contracts and three retired records; Track Q keeps 13. No API, security, packet ownership, lifecycle, dependency selection or release contract changes. Documentation, manifests, receipts and mirrors are process metadata; existing tests/tools are verification aids. No new maintained aid or parser is approved. Existing representation owners/guarantees and finite context budgets remain; load only the current slice, applicable design rows and this governing evidence diff. No new contract-closure matrix or repeated-root claim is triggered by documentation changes.

## Review and publication receipt

Independent read-only slice/contract audit passed before commit: all 32 stable identities, retired dispositions, acyclic blockers, receipt provenance and complete downstream sequence checked. One accepted docs-only finding aligned the design Destination/D02 summaries with settled wrapper retention; targeted readback verified the correction. No substantive blocker or replacement review remains. Local certification uses each repository's documentation path, followed by its exact-head hosted checks and matched-head merge. Only after both plans are on default branches may issue plan pointers, removed dependency edges, readiness labels and OmniFocus state be synchronized. Completed slices and P02's active execution pointers remain intact. The new fork frontier is Q02–Q05; no implementation is dispatched.
