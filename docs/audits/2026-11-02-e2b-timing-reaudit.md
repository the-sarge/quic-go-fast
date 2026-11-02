# E2B timing identity re-audit

Scope: [E2B #1085](https://github.com/the-sarge/quic-go-fast/issues/1085), product [PR #1097](https://github.com/the-sarge/quic-go-fast/pull/1097), parent [#1012](https://github.com/the-sarge/quic-go-fast/issues/1012), program [#646](https://github.com/the-sarge/quic-go-fast/issues/646). The [current E2B contract](../adr/2026-10-24-multipath-endpoint-plan.md#e2b) owns the normative outcome and evidence. This artifact records the trigger, dispositions and design decision.

## Trigger and routing

Initial RAS run `20261102T060633-3a89548200e906ffe1894a94` reviewed `9c517857cf0576ca358f98e666daa6cb39695eb6` with the configured full panel. [Independent dispositions and resume receipt](https://github.com/the-sarge/quic-go-fast/pull/1097#issuecomment-6570733392) record C-008 as a stop routed through the authorized same-child re-audit. The implementation branch is preserved at `8266fc49ad9f2d16bcbd4fc6c3651c935d529987`; that local merge incorporates R5 #1025 without changing E2B's product boundary. No product fixes, verification or certification resume before publication and child-pointer synchronization.

The precise invariant is exact-once logical delivery across timing-only exclusions. The concrete owner is the dispatch/sampler interaction among `captureCongestionSend`, `fenceTiming`, origin restoration and generation-based delivery accounting. Eviction during registration and a completion fence followed by nonzero pending-credit admission are behaviorally distinct classes at that same owner. Both create originless registration and later increment logical `sampleGeneration`; the accepted timing-only family had to cover both. A callback-order repair alone cannot close the family. This establishes the shared approach stop without equating unrelated timing or allocation concerns.

A temporary overlay reproduced the eviction class: the final ACK left delivery at 39,321,600 bytes instead of 39,322,800. Supplied panel adjudications independently reproduced the pending-credit class. These are accepted semantic variants within the existing ordered-completion and timing-fence scenarios, not new scenario functions or an operational campaign.

## Gate result and selected owner design

Retain child #1085 and product PR #1097. The end-to-end outcome, typed registration/outcome and authenticated-ACK representation, parser ownership, universal in-domain invariants, one connection execution context and one independently green product PR remain unchanged. Lifecycle and qualification guards are shipped behavior/safety enforcement; the three approved scenarios remain maintained verification aids with the existing transport-suite owner and replacement-or-feature-removal retirement rule. Plans and receipts remain process metadata.

The central repair makes local origin loss and restoration use only timing identity. Existing lifecycle identity continues to control logical delivery, and raw registration snapshots retain their provenance. This avoids a second delivery counter, controller formula or caller convention. The derived qualification cursor and indexes retain their registration-order/monotone-eligibility design and finite per-registration work. Construction, completion, ACK/loss/PTO, eviction, retained evidence, key disposal, Retry, close and fresh-instance paths stay within the named owners. No additional execution context, external representation, socket/ECN integration, activation seam or untraced effect is admitted.

Move the new local qualification facts into a bounded slot-indexed table allocated only for local construction. It shares the existing outcome ring's identity, capacity and lifecycle; it is not an independent state machine. This removes the measured 768 KiB unconditional ring growth and pointer-bearing allocation on legacy rich connections. Source/layout accounting and the inherited equivalence/work-bound suites are sufficient evidence; no GC-latency, throughput, memory or platform campaign is authorized. Existing PacketInfo timing metadata remains allowed, with avoidable padding removed.

Rejected alternatives: callback reordering alone leaves the pending-credit origin-restoration class open; separate logical delivery accounting would add another authority and alter an accepted identity; keeping inline local pointers imposes avoidable legacy cost; another child would leave this child non-green; merging E2C or endpoint integration would cross owners and context boundaries. Revisit: none, rejected on merit. The selected repair changes the existing owner rather than adding caller ordering or another handoff.

## Disposition ledger

| Finding | Independent disposition | Current rationale |
| --- | --- | --- |
| C-008 | Stop routed to scoped re-audit; fix after publication | Two semantic classes violate timing-only exclusion at the same logical-generation owner; the normative matrix now closes that family |
| C-005 | Fix-now | Isolate optional local allocation and preserve legacy layout within existing owners and ceilings |
| C-001 | Fix-now | Resolve existing E2C #1086 issue identity without amending its substantive contract |
| C-003 | Fix-now, corrected on the preserved product branch | Combined R5 #1025 and E2B #1085 completion is 8/34; product PR owns that state transition |
| C-010 | Reject | A known-accepted packet ACKed inside a fresh interval contributes real cumulative delivery; the observed rate alone does not establish a violated send/ACK interval bound. Its old snapshot cannot anchor timing. A new restriction on which registrations may contribute delivered bytes would strengthen the accepted sampling policy without a discriminating counterexample |
| C-002, C-004, C-006, C-007, C-009 | Reject as duplicates | Carry evidence under the canonical findings; no separate work |
| C-011 | Reject | Supported local registration hits the shared 25,000-entry guard before the alleged optional live-record overflow; supplied fill tests confirm refusal without the claimed transition |

## Finite evidence and resume

The normative matrix retains three scenario functions. Add the two admitted orderings there, asserting unchanged logical generation, exact-once delivered/lost totals, excluded timing and fresh-anchor recovery. Reuse recovery/delivery equivalence, indexed work bounds, BBR/Reno preservation and repository checks. No new race, stress, mutation, native/platform, analyzer or recursive fixture obligation is introduced. No accepted matrix cell is deferred; proof of all encodings and separate controller-policy changes remain outside scope.

Publish this contract through the repository docs path and require its commit on main before updating the child issue's exact plan pointer. Refresh parent/program and OmniFocus current-state pointers without copying the plan SHA. Reconcile the preserved product branch with main, implement the central fixes and verify the triggering initial review at that exact pushed head. Only then run the one remaining replacement review, followed by final certification and hosted checks. Product PR #1097 retains all completion/frontier changes; journal work starts only after that product merge.
