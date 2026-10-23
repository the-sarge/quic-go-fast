# Adaptive application loss thresholds

Status: approved by the operator through Planit on 2026-10-23 for [#971](https://github.com/the-sarge/quic-go-fast/issues/971). The #851 publication hold is satisfied by published v0.62.1-fast.8; prerequisite #970 is merged. The separately approved [#1002 fixture prerequisite](windows-startup-port-selection-1002.md) and its journal are merged. This is the current implementation and review contract. Review chronology, dispositions and exact-head receipts belong in the PR discussion.

## Outcome and invariants

Only admitted, surviving ordinary application-loss receipts train path-local loss policy. One such receipt disables application packet-distance loss until migration. Initial and Handshake retain three-packet and 9/8 RTT thresholds; 0-RTT and 1-RTT share application identity and policy. The effective delay is `max(TimerGranularity, multiplier * max(latestRTT, smoothedRTT))`, retaining the existing duration rounding. Application multipliers progress through 9/8, 5/4, 3/2 and 2, capped at 2.

Growth requires observed reordering strictly greater than the current effective delay, measured as receive time minus clamped ACK delay minus send time. At most one increase is allowed per ACK. The first application registration after initialization or migration establishes the eligible packet-number boundary. An increase clears that boundary until the next application registration establishes a new one. A later send never makes a previously registered loss eligible. This uses existing monotonic per-space registration facts, without shared wire numbering, BBR ordinals or a new retained ledger.

The handler validates ACKs and admits retained receipts, applies eligible RTT updates, then adapts before both ordinary and PTO-retired classification. Late-only ACKs do not sample RTT; a larger delay recomputes outstanding loss deadlines and reschedules the alarm. Reno's restoration callbacks remain after current-ACK CE validation and genuine-loss classification. A transient, metadata-only receipt batch holds those already admitted receipts within the synchronous ACK call. Matching diagnostic records are consumed before strict three-PTO send-age cleanup, including newly declared losses and late-only or reordered ACKs.

Migration resets learning, freshness and history. Retry, key discard and rejected 0-RTT retain existing disposal fences. Excluded probes, ACK-only records, expired or evicted history and rejected ACKs cannot train policy. Genuine loss remains detectable by time, with flight/frame effects once only. PTO alone does not establish loss; persistent-congestion duration remains three PTOs. The legacy 64-record history does not replace the independent 32,768-outcome and 4,096-retained-delivery authorities.

## Boundary and design cost

One product PR changes the sent-packet handler, the space-aware delay call in congestion dispatch, colocated maintained tests, domain/design documentation and Unreleased notes. [ADR 0013](../adr/0013-adaptive-loss-thresholds.md) narrowly amends ADR 0009/D04 and #617's effective-delay wording. Preserve ADRs 0001/0003/0004, ADR 0011's typed prefixes, ADR 0012's Reno undo, controller selection, public/wire contracts, connection-owned registration, asynchronous socket-I/O ownership, BBR model/sidecar ownership, frame/pool lifetimes and DATAGRAM non-retransmission.

The design adds constant-sized handler state and one bounded transient receipt batch, with no new execution context, retained store, per-packet field, dependency or public configuration. The material cost is delayed genuine-loss classification after adaptation. Reusing BBR's optional store was rejected because it couples generic recovery to controller-specific evidence; revisit only under an accepted evidence-ownership redesign. Importing upstream's shared numbering/history rewrite changes closed transport contracts; revisit only if those contracts are reopened. Splitting packet suppression and time growth was rejected on merit because intermediate policy states provide no useful independent outcome. No side channel or cross-context hand-off is introduced.

Excluded work: new Reno undo behavior, #961 implementation changes, controller models/selection, wider stores, performance claims, the deferred 36-run comparison, native qualification and consumer rollout. Frozen assessment evidence remains unchanged reference material, not a maintained test dependency. This contract does not update published release history.

## Representation and artifact contract

Supported inputs are structurally valid decoded `wire.AckFrame` ranges delivered through the existing connection-owned handler lifecycle. Wire parsing owns range syntax, the handler owns unsent/skipped admission, registration, policy and lifecycle, and the independent recovery reducer owns PTO/persistent outcomes. Arbitrary malformed in-memory ACKs and calls into already discarded spaces remain outside the caller contract. Freshness, exclusions and once-only effects are universal runtime requirements within that domain; finite regressions are behavioral evidence, not exhaustive proof over protocol histories.

Contract closure is narrowly triggered for stale-proof admission and same-ACK classification: independently reachable ordinary, late-only and lifecycle paths can otherwise corrupt restoration or persistent-congestion restart. The finite matrix below bounds the semantic cases. It does not create a parser, syntax-variant campaign or recursive harness obligation.

Policy is shipped behavior. Admission, age, freshness and reset guards are required safety enforcement. The listed maintained regressions are explicitly approved merge-gating verification aids: their payoff is preventing stale adaptation, premature classification and duplicate retirement; recovery maintainers own them until equivalent coverage replaces them or the policy is retired. Contracts, review receipts, issue/task pointers and journals are process metadata.

## Finite acceptance evidence

| Semantic class | Required disposition and owner | Maintained evidence |
| --- | --- | --- |
| No proof; Initial/Handshake; migration | Handler preserves default packet/time policy and resets path learning | `TestAdaptiveLossSpacesAndMigration` |
| Shared 0/1-RTT history | Application proof trains the common policy | `TestAdaptiveLossSharedApplicationSpace`; existing shared-space and disposal tests |
| Fresh application flight after proof, both controllers | No packet-distance loss; timer later retires genuine losses once | `TestAdaptiveLossPacketThreshold` |
| Every transition and 2 RTT cap | One increase per ACK, qualifying post-increase sends and bounded growth | `TestAdaptiveLossGrowthAndCap` |
| Multiple old receipts, duplicates, no fresh send, old receipt after fresh send | No repeated or retroactive eligibility | `TestAdaptiveLossLateOnlyDeadline`, `TestAdaptiveLossOldReceiptAfterFreshSend` |
| Equality, strict excess, ACK-delay clamp and granularity | Handler compares against the effective delay, with strict growth boundary | `TestAdaptiveLossTimingBoundaries` |
| Frozen late-only deadline | 1.625 s becomes 1.750 s; old deadline loses nothing; new deadline retires genuine losses without an RTT sample | `TestAdaptiveLossLateOnlyDeadline` |
| Legitimate RTT update before adaptation | New RTT can make observed reordering insufficient to grow | `TestAdaptiveLossUsesCurrentRTT` |
| Same-ACK ordinary/PTO classification, ordinary and late-only ACKs, both controllers | Adapted delay prevents premature loss/persistent evidence; later valid evidence still reports once | `TestAdaptiveLossBeforeBothClassifiers`; existing three-PTO persistent-span tests |
| Expired, evicted, unsent or skipped evidence | No packet suppression or time growth | `TestAdaptiveLossUnavailableEvidence`; existing admission and strict-expiry regressions |
| MTU/path/ACK-only receipts, lifecycle disposal and old paths | Exclusions and fences prevent fabricated adaptation | Extended `TestSpuriousLossPacketEligibility`; existing `TestSpuriousLossDisposal`, `TestSpuriousLossMigration`, `TestRenoUndoLifecycle` |
| Genuine loss or validated CE with old proof | Preserve current Reno undo ordering and completeness, including genuine losses classified by a late-only ACK | `TestAdaptiveLossLateOnlyGenuineLoss`, `TestRenoUndoNewLossBeforeOldProof`, real ECN and Reno undo suites; genuine-loss ages exceed the effective delay |
| BBR episodes, ordinals, delivery, caps, persistent restart/dedup and bounds | Existing independent owners retain their contracts | Recovery/congestion suites, including delivery, ECN, PTO, memory and work/equivalence tests |
| Frame, pool, emission and DATAGRAM effects | Existing owners retain once-only actions and unreliable DATAGRAM semantics | Existing real-handler reuse, root controller/DATAGRAM/emission and relevant integration tests |

## Terminating validation and execution

Use `/Volumes/worktrees/quic-go-fast/adaptive-loss-thresholds-971` and `codex/adaptive-loss-thresholds-971`; initiating checkout is read-only. Verify worktree, branch, HEAD and changes before edits and commits. Test at approved real registration, ACK and timeout seams, observing timers, frame callbacks and controller feedback.

Run focused regressions, `go test ./internal/ackhandler ./internal/congestion ./internal/wire`, `go test -race ./internal/ackhandler ./internal/congestion`, changed-package vet, formatting, pinned golangci-lint, generation/compiler assertions, `go mod tidy -diff`, diff and added-link checks. Run relevant root controller/DATAGRAM/emission tests and existing 0-RTT/Retry/migration integration tests. Existing hosted workflows own their configured OS/toolchain coverage. No benchmark, mutation campaign, additional platform matrix or repeated-run target is authorized.

Open a draft PR. Brief one initial RAS review with the issue acceptance criteria verbatim and this bounded contract; independently disposition substantive findings, verify accepted fixes and allow at most one replacement review. Required boundary expansion, representation mismatch or a repeated semantic failure at the same precise invariant and owner stops for an operator decision. Deferred/rejected findings do not expand the contract; no automated fixer is permitted.

After review, certify the exact pushed head/base and clean tree with the local gates, require applicable existing hosted checks successful on that live head, mark ready and squash merge with `--match-head-commit`, following the [repository overlay](../REVIEW-LOOP.md). There is no `task preflight`, `ci.yml` or `ci-*` gate. If main advances, update and repeat required review, certification and CI. Only after product merge reaches remote main, append the journal without RAS, revalidate deferred findings against the merged revision and reconcile issue/relevant OmniFocus pointers.
