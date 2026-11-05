# Local recovery summary handoff audit

Date: 2026-11-05. Scope: N track under [parent #1193](https://github.com/the-sarge/quic-go-fast/issues/1193), stopped [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) and [program #646](https://github.com/the-sarge/quic-go-fast/issues/646). The [efficiency plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md) owns current contracts. This artifact owns source identities, causal limits, existing-work disposition and audit receipts. This handoff launches no implementation or experiment.

## Authority, source and existing work

The owner explicitly invoked `$architecture-handoff` after the stopped combined-candidate receipt. This authorizes a scoped N-track revision and updates to its existing program/track/child pointers and OmniFocus tree; it does not reopen unrelated tracks. The existing program parent is `omnifocus:///task/glR8nyMhqbd`, N parent `omnifocus:///task/lm6KOC8oaof`, and N3 task `omnifocus:///task/mNz74Y5XKdb`. The two existing chat/implementation checkouts remain untouched; drafting uses the dedicated `codex/1202-local-recovery-handoff` worktree under `/Volumes/worktrees/quic-go-fast/`.

Source basis: remote main `b39389b152a728232b7ec68ed1060c213c6649a8`, fetched before drafting. Read the current issue bodies/comments for #1202, #1193, #1213 and #646, and [N4 PR #1227](https://github.com/the-sarge/quic-go-fast/pull/1227), including its review/certification/merge receipts. N4's documented review correction concerned wording, not an unresolved runtime finding. Read the current ownership, recovery-registration and compatibility guardrails, shared review/closure/design-cost baselines and repository execution overlay.

| Existing work | Disposition | Evidence and limitation |
| --- | --- | --- |
| Merged N0 #1199, N1 #1200, N2 #1201, N4 #1213 | Retain | Driver, sparse endpoint traversal, auxiliary admission and capacity accounting remain accepted behavior. No reimplementation or discarded closure obligation. |
| N3 #1202 combined candidate `59b1dddc650be03a98cbd8d7f8e5fb3f34967e0e` | Retain/rework source after N5; not a baseline dependency | Clean preserved branch `codex/1202-combined-control`, `/Volumes/worktrees/quic-go-fast/1202-combined-control`; changes only `native_endpoint.go`, `native_endpoint_test.go`, and `native_control_test.go` relative to source basis. No PR or RAS review was opened. |
| Earlier N3 source `d2654e143d93cee59b5547be56b52f6e6ca2fa10` | Preserve historical source | Previous candidate, superseded as reapplication source by the combined candidate; never overwrite it. |
| [Combined execution and scoped re-audit receipt (#1202)](https://github.com/the-sarge/quic-go-fast/issues/1202#issuecomment-6663588710) | Retain failed qualification and attribution for their exact binaries | Twelve E6 cases passed; clean STREAM GSO-on failed; GSO-off was not run. The separate attribution observation is not qualification. |

The program already underwent legacy rebaseline in the [original N handoff](2026-11-04-native-endpoint-efficiency-handoff.md), retained by the [N4 handoff](2026-11-05-native-recovery-accounting-handoff.md). This revision keeps shipped behavior/safety and existing measured gates, removes no product obligation, adds no recursive proof/harness obligation, and leaves unrelated E/R/B contracts intact. The obsolete N3-ready administrative state is replaced with N5-first ordering. Historical observations remain here or behind receipts; normative slices and mirrors contain current contracts/pointers only.

## Evidence and causal limits

The clean STREAM GSO-on pair reported 1,641.435938 MB/s unregistered and 702.539366 MB/s granted: useful-goodput ratio 0.428002915 against the 0.95 floor, CPU/useful-byte ratio 1.817517591 against the 1.10 ceiling. Application send-call and echo latency passed. Both observations were uncontaminated; no timeout-only close or write error explains the result. These are single diagnostic observations, not R18 #1038 qualification.

The separate unchanged-binary profile observation used 77.054 host-seconds and was uncontaminated; analysis ran locally after measurement. Its 91.60 sampled CPU-seconds include setup/warmup/cleanup. Cumulative samples overlap: `RegisterLocalPacket` 9.31 seconds, `CompleteLocalPacket` 7.95, `localPTOTimeAndSpace` 6.46 (7.05%), `nativeEndpoint.do` 8.26 and `endpointState.progress` 4.96. Endpoint mutex stacks accounted for about 80 ms of aggregate sampled wait. This supplies little evidence of dominant lock blocking, and does not establish either full causality or that removing a roughly 7% sampled function can close the much larger throughput/CPU gap. Required work remains in packing/encryption, syscalls, recovery/ECN/congestion and endpoint scheduling.

The old unresolved-capacity scan is absent. Registration and completion still ask the handler for a loss alarm; local PTO folds every current packet history even when the eligible maximum has not changed. This redundant query work has a narrow independent outcome. Batch-completion policy, skipped loss processing, endpoint wake suppression, a new native writer or ordinary-route bypass are not selected. One result currently maps to ordered per-packet facts in `native_ordinary.go:756–770`; each fact can change congestion, ECN, loss and held undo in `local_completion.go:199–236`. Moving those decisions to a batch boundary would be a separate semantic contract, not this optimization.

Raw evidence is preserved at `/Users/josh/diagnostics/quic-go-fast/issue-1202-combined-2026-11-05` and the corresponding `/home/josh/diagnostics/quic-go-fast/issue-1202-combined-2026-11-05` on minimax. Ordinary binary SHA-256: `50c41aa8c7d0b363ac26366b46fed978405bb63c455753e2f9c0de4ffb1a08b4`; source archive SHA-256: `df69923b55d323bd6d2305abfb280add059ed216a02b534a2a0404a444e0a3c3`. This execution charged 18 instances / 245.989 host-seconds, bringing the recorded program total to 138 / 1,951.930. The existing scoped waiver remains; this documentation run adds no native observations or new cumulative cap.

Local candidate evidence includes focused native tests, focused race, vet, tidy and a later full root pass. An initial `TestNativeRouteLoopback` timed out during the post-switch exchange; isolated and later full passes did not establish cause or repair. The test shares one ten-second context across several network phases (`native_shutdown_test.go:1021–1083`). Carry that unresolved limitation into N3 and diagnose recurrence rather than citing another pass as a fix.

## Full lifecycle and precise-root comparison

The PTO predicate is `Outstanding() && networkEligible()` with a maximum of `recoveryTime()` (`local_completion.go:271–280,314–336`). `Outstanding` depends on frames/probe status (`packet.go:41–46`). Completion time is only bounded below by its own registration (`local_completion.go:196`); the unordered-receipt test explicitly consumes an earlier return later (`local_completion_test.go:137–181`). Therefore latest PN, latest arrival and an unconditional `lastAckElicitingPacketTime` substitution are invalid shortcuts.

| Semantic transition | Central source seam | Summary obligation |
| --- | --- | --- |
| Pending registration | `local_completion.go:143–178`; `sent_packet_handler.go:430–440` | Adds no eligible maximum. Local-mode construction is enforced at `local_completion.go:81–86` and `sent_packet_handler.go:350–353`. |
| Accepted/uncertain live completion | `local_completion.go:202–236` | Canonical outcome/time and token validation first; update a clean summary only for a live outstanding packet. |
| Early ACK and later fact | `local_completion.go:208–216,240–257` | Retired packet cannot reenter; distance qualification has a different lifetime. |
| ACK removal and packet pooling | `sent_packet_handler.go:655–657,857–861`; `sent_packet_history.go:188–217` | Invalidate by PN before slot clear/pool reuse, without retaining a packet pointer. |
| Network loss, PTO dequeue, unsubmitted | `sent_packet_handler.go:1115–1126,1444–1451`; `local_completion.go:218–227`; `sent_packet_history.go:304–323` | Invalidate maximum at history removal before clearing frames; unsubmitted never becomes eligible. |
| Key drop, 0-RTT rejection, Retry | `sent_packet_handler.go:237–293,1485–1520` | Whole replacement discards summary; partial application removal preserves survivors. |
| Close, migration, path reclamation | `local_completion.go:353–370`; `sent_packet_handler.go:1541–1553`; `sent_path.go:101–106`; `sent_packet_history.go:236–248` | Replace/reset or remove via existing owner; preserve no-query-before-replacement ordering during disposal. |

No additional production mutation of outstandingness was found outside construction, history removal followed by frame clearing, or disposal of a whole history. The implementation must stop if it discovers one. Equal timestamps may retain either PN as witness: only the maximum time leaves the history, and removing that witness dirties the summary before use.

| Comparison | Earlier N4 capacity family | New N5 PTO family |
| --- | --- | --- |
| Exact invariant | Count retained history slots plus unresolved identities outside those slots once | A valid summary equals max recovery time of outstanding eligible packets; dirty summaries are rebuilt before use |
| Enforcement owner/seam | `numTrackedPackets` / `unresolvedLocalOutsideHistory`, membership append/consume/trim | History maximum query, completion eligibility insertion and removal/reset invalidation |
| Semantic classes | Interior holes, detached identities, token consumption and epochs | Eligibility, timestamp ordering, maximum retirement, space availability and surviving candidate |
| Did the earlier accepted family own this later case? | No: resource cardinality does not compute PTO | No repeated-root conclusion; a distinct query in the same module is not the same invariant |

Two failed STREAM results likewise do not establish two semantic counterexamples at one safety guard. This handoff does not claim N4 closure failed.

## Design and graph decision audit

The adopted owner design stores scalar max time, witness PN and dirty state in history (approximately 24 bytes/history on a 64-bit target, actual layout to be reported). It adds no packet field, pointer-retaining sidecar, lock, worker or heap allocation. Clean queries and eligible-candidate comparison use constant work. A dirty query performs the exact existing full fold; maximum retirement may cause that refill. This residual O(H) behavior is part of the accepted contract, not hidden by the word optimization. Keep this state separate from N4's token membership and from `qualifyLocal`'s packet-distance facts.

| Alternative | Disposition and reason | Revisit condition |
| --- | --- | --- |
| Scalar lazy maximum in history | Adopt; one derived state inside the owner with bounded update/removal seams | Current contract |
| Pointer to cached packet | Reject; smaller storage adds pointer/pool-retention obligations that scalar PN/time avoids | None: rejected on merit |
| Heap/tree with per-entry index | Reject now; stronger worst-case bound costs ordering, deletion, reset and allocation obligations | Mixed-retirement evidence defeats the lazy benefit, or an accepted worst-case latency requirement |
| Latest PN/arrival or existing last-sent timestamp | Reject; not equivalent for unordered local returns or retired maxima | Explicit change to completion semantics, outside this plan |
| Eager rebuild on every mutation or reverse full scan | Reject; moves or retains redundant population work | None: rejected on merit |
| External alarm cache, batch policy or asynchronous recovery worker | Reject; creates cross-owner invalidation or changes per-fact policy order | Separate audited contract supported by causal evidence |
| Split N5 by space or separate insertion/removal PRs | Reject; incomplete derived state or duplicated same-owner lifecycle | None: rejected on merit |
| Merge N5 with N3 #1202 | Reject; N5 is independently green on main and needs no endpoint mutex change | None: rejected on merit |
| Relax N3 #1202's combined performance gate | Reject; failed candidate remains unqualified | None: rejected on merit |

N5 has no unmerged blockers. N3 #1202 keeps its identity and one-PR endpoint migration, but consumes N5 before combined qualification. N0/N1/N2/N4 remain its completed predecessors. R17 #1037 and R18 #1038 remain blocked by N3; parent #1193 and admission tracker #1141 remain open through their separate criteria. There are 41 intended slices, 30 complete, with N 4/6. No existing issue is replaced or renumbered. N5 is the only new issue/task. No parallel N implementation is implied, and shared measurement-host use remains serialized after N3.

The N3 source audit finds coherent ownership: all private transactions serialize at candidate `native_endpoint.go:302–315`; immutable native facts publish at `:1159–1162`; the sole progress owner handles timer and terminal orchestration at `:879–919`. Terminal closes channels/joins/returns the grant outside control, avoiding registry/control inversion with `native_transport.go:44–59`. The five retained tests and watchdog adaptation remain source requiring fresh review. No endpoint topology replacement is justified by current evidence. Another failed combined result requires whole-pipeline causal diagnosis, not automatically selecting the next sampled hot function.

## Independent draft audit

Two independent read-only audits passed before commit. The recovery-design auditor confirmed scalar summary semantics, all eligibility/removal/replacement paths, bounded evidence and fresh-context fit. The retained-candidate/graph auditor accepted the dependencies, unchanged N3 gate, causal limits, waiver and counts, and found one `fix-now` documentation defect: the no-rescan promise lacked a clean-summary qualifier, even though registration can trigger a timer query after an earlier maximum removal. The decision and N5 acceptance now state that pending registration does not invalidate a clean summary and that any dirty query may perform the admitted refill. The auditor rechecked both corrections and passed with no remaining substantive findings. No product change, new gate or extra evidence budget resulted; the audit launched no implementation. Documentation publication uses the repository's independent source/contract review path, added-relative-link and graph checks, clean exact-head certification, applicable hosted checks and squash merge. No additional RAS gate is introduced. After merge, append the journal, publish exact default-branch child pointers, mirror the existing OmniFocus tree, and report the frontier without dispatching it.
