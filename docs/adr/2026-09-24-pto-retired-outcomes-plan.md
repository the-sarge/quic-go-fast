# ACK-confirmed PTO-retired outcomes

Status: approved standalone implementation contract for [issue #617](https://github.com/the-sarge/quic-go-fast/issues/617), with [accepted design decisions](https://github.com/the-sarge/quic-go-fast/issues/617#issuecomment-5820960624). This is one implementation PR, not a T5 revision, audited program slice or new B5 blocker. Historical T5 evidence and program status remain unchanged.

## Outcome and ownership

Recovery may classify explicitly PTO-retired originals for persistent-congestion proof when a validated current ACK supplies surviving higher-numbered same-space current-path registration evidence and the ordinary recovery time threshold is satisfied. PTO alone is non-loss. Receipts win in the same event, the threshold uses legitimately updated RTT, and duplicate witnesses are evaluated per ACK with no witness cache. The outcome ledger owns this classification, independently of delivery retention. Preserve the 32,768-outcome and 4,096-tombstone limits and existing expiry policy.

`QueueProbePacketAt` marks retirement even when optional sampling has no record. `recoveryEvidence.ack` admits surviving receipts and returns the highest eligible packet number, with retained/live ACK metadata admitted through the same current-path predicate. `ReceivedAck` invokes classification after receipt admission and legitimate RTT update on both ordinary and late-only paths. `recoveryEvidence.confirmPTO` makes one bounded additional pass; the existing reducer owns cross-space spans, gaps, measured endpoints and deduplication. The shared `sentPacketHandler.lossDelay` retains ordinary recovery's exact calculation and rounding. Existing disposal and reset owners invalidate evidence; no new lifetime owner is introduced.

## Acceptance criteria

- [ ] A real-recovery small-flight PTO/backoff trace followed by sufficient later same-space ACK evidence classifies eligible originals and emits the expected persistent span. Include a case where older delivery tombstones have expired but the required recovery outcomes survive.
- [ ] PTO alone, insufficient age, invalid ACK input, wrong-space receipts and alternate-path receipts cannot establish the new classification. Reuse existing recovery time-threshold semantics; ACK-triggered reevaluation after insufficient age is specified and tested.
- [ ] A direct original ACK in the confirming event prevents loss classification. Late originals and duplicate ACKs preserve delivery accounting and report deduplication. Newly completed proof can reach the sink through a valid ACK with no newly live or sampled delivery records.
- [ ] Delivery expiry/eviction and outcome eviction have distinct outcomes: surviving authoritative outcome evidence may classify; missing, disposed, excluded or abandoned-path evidence never bridges a gap. Existing history and expiry bounds are unchanged.
- [ ] Persistent endpoint eligibility and cross-space span rules remain intact. Retry, path reset, key discard and rejected 0-RTT do not admit stale outcomes or witnesses; existing current-path MTU-receipt versus alternate-path-probe distinctions remain intact.
- [ ] Stream-only, DATAGRAM-only and mixed payload coverage demonstrates shared packet-level detection with no duplicate flight subtraction, frame callback or application DATAGRAM retransmission. The new classifications do not change ordinary lost-byte totals or create/extend loss episodes; existing persistent-event undo invalidation still applies.
- [ ] Update the maintained feedback contract and accepted design documentation to explain the focused classification semantics and residual bounded-history limitations. Preserve frozen T5 evidence and completed-program status.

## Preservation, boundary and non-goals

Changes are limited to ackhandler recovery/feedback plumbing, six focused test functions, the internal feedback contract and maintained design documentation. Preserve default Reno, public activation and API, packet parsing, ordinary time/packet thresholds, stream retransmission, DATAGRAM unreliability, delivery accounting and retention, flight/frame ownership, lost-packet tracking, ECN callbacks, episode membership and exits, persistent-event undo invalidation and all existing lifecycle fences. New classifications do not enter `FeedbackEvent.Lost`, delivery loss volume or episode membership. Late originals break future spans without retracting emitted reports or resurrecting delivery snapshots.

No controller reaction, packet-distance classification of retired outcomes, new timer, queue, goroutine, dependency, frame/payload retention, larger history or benchmark campaign. No inference across missing/disposed/excluded evidence; no detection guarantee without a later qualifying ACK. A complete surviving suffix remains usable. Added blast radius is fixed-size recovery metadata and bounded per-ACK traversal. The threshold extraction also touches ordinary recovery and must preserve its behavior exactly. No throughput/latency claim follows from a scan bound. Any newly discovered effect outside this boundary requires a decision.

## Representation, artifacts and semantic evidence

Supported inputs are existing typed registration records and decoded ACKs admitted by current recovery validation. Protocol parsing remains owned by the existing wire parser and recovery ACK validator. Guarantee: universal enforcement of the declared invariants within that typed, bounded domain, supported by finite regression evidence, not exhaustive network-outage detection. Recovery owns classification; the sampler never determines transport loss. Normalized space identity preserves original encryption-level disposal identity.

Runtime classification is shipped behavior. Receipt, lifecycle and bound guards are required safety enforcement. Tests are verification aids, not an approved maintained general-purpose harness or new product dependency. Plans, review records, certification and journal entries are process metadata. There is no authorization to recursively strengthen verification aids.

Lifecycle authority and accounting consequences trigger a bounded semantic coverage argument across independently reachable ACK, retirement and disposal paths. The following six families terminate this work; reuse existing preservation coverage rather than multiplying syntax, platform or timing variants. No guard mutation is required: explicit behavioral negatives directly exercise admission and disposal, and the accepted evidence budget excludes a mutation campaign.

| Semantic class | Owner and disposition | Required evidence |
| --- | --- | --- |
| Real small-flight PTO/backoff, including expired delivery snapshots | Recovery classification and reducer: emit the surviving eligible span, without ordinary loss | `TestBBRPTOConfirmedSmallFlight` |
| Known later same-space ordinary/ACK-only/MTU witness; insufficient age, invalid ACK, wrong-space/path, absent witness | ACK admission and shared time threshold: classify only eligible current-event evidence; duplicate receipt can later qualify | `TestBBRPTOConfirmationAdmission` |
| Same-event original, late original, duplicate receipt, empty sampled event | Receipt-before-classification and reducer: acknowledge first, preserve delivery, suppress repeated reports, deliver new proof | `TestBBRPTOReceiptOrdering` plus admission's empty-event duplicate case |
| Optional sampling unavailable, delivery pressure/expiry, outcome eviction, unresolved/excluded gap, complete surviving suffix | Bounded recovery ledger: use surviving evidence only, never reconstruct a gap | `TestBBRPTOEvidenceBounds` plus small-flight expiry |
| Retry, migration, close, key discard, rejected 0-RTT, shared application space, measured endpoints and cross-space spans | Existing disposal/reset owners and reducer: preserve fences and endpoint rules | `TestBBRPTOConfirmationLifecycle` and existing measured-at-send coverage |
| Stream-only, DATAGRAM-only, mixed packets, late receipt and genuine persistent event | Existing transport accounting and callback owners remain authoritative; confirmation adds no ordinary loss effects | `TestBBRPTOAccountingPreservation` and existing episode/undo coverage |

## Terminating validation and review

Use real recovery entrypoints and the existing feedback recorder; the accepted seams are registration, PTO timeout/extraction, ACK processing and lifecycle operations. First demonstrate the small-flight regression fails, implement vertically, then run the six focused families once on the final candidate and affected-package tests once with race instrumentation. Run `go vet ./internal/ackhandler ./internal/congestion`, `go mod tidy -diff`, formatting and `git diff --check`. Repeat only to diagnose or validate actual changes/failures. No new stress, statistical repetition, fuzzing, platform expansion or network benchmark.

Review budget: one fully briefed RAS review; independently disposition every substantive finding; fix and verify accepted in-contract findings; at most one replacement review. Review criteria are quoted verbatim above. Context consists of this current contract, the linked final design brief, changed source and tests, referenced preservation invariants, and only relevant unresolved review history. Keep chronology and run receipts in PR discussion rather than expanding this contract.

Follow [the repository execution overlay](../REVIEW-LOOP.md): exact pushed head/base and clean-tree certification, affected-package race validation, vet, module tidiness and applicable existing hosted PR checks. This repository has no `task preflight`, `ci.yml` or `ci-*` gates; draft PR checks run normally. Mark ready after certification/checks and squash-merge the exact checked head. Default-branch advancement requires updating the candidate and renewing required gates. Only after the implementation merge, append the dev journal without RAS, revalidate deferred findings and complete the linked OmniFocus task.

## Stop conditions

Stop for broader accounting, retention authority, larger history, a materially larger evidence campaign, inability to preserve the declared invariants, or a representation/precise-root approach failure under the shared review policy. Preserve the branch and request an amended decision rather than expanding scope. Out-of-scope review findings are independently deferred or rejected; they do not change this contract.
