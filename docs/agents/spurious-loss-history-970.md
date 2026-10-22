# Application spurious-loss discovery and history

Status: approved by the operator on 2026-10-22 through Planit for [#970](https://github.com/the-sarge/quic-go-fast/issues/970), the detection/history child of [#962](https://github.com/the-sarge/quic-go-fast/issues/962). This is the current implementation and review contract. Review chronology and exact candidate receipts belong in the PR discussion.

## Outcome and ownership

Each retained, eligible ordinary application loss covered by an admitted ACK is consumed and reported once, including ACKs with no newly live packets and reordered ACKs. The sent-packet handler owns admission, discovery and lifecycle decisions. The legacy tracker owns only packet-number/send-time metadata; it has no frame, payload or pooled-packet references.

Both existing `ReceivedAck` branches process legacy history after unsent/skipped validation. The ordinary branch retains its existing RTT, ECN, loss and frame-retirement sequence. The empty-live branch retains its independent BBR feedback sequence. Legacy discovery does not create RTT samples, relax legacy ECN eligibility, retire flight or call frame handlers. It subtracts the existing capped ACK delay for diagnostic time reordering, consumes covered records and applies the existing strict three-PTO send-age cleanup, including on late-only or reordered receipts. Consumption still precedes age cleanup, as it did before this correction.

`detectLostPackets` admits only application-space packets satisfying the existing `packet.Outstanding()` predicate: ordinary ACK-eliciting packets, excluding MTU and path probes. ACK-only records and path placeholders remain outside legacy history. Their ordinary retirement, independent path-probe storage and callbacks keep their existing owners. The tracker retains its 64-record bound, oldest-record overflow and shared 0/1-RTT packet identity.

`MigratedPath` resets legacy history alongside the existing path lifecycle reset. Retry, key discard and rejected-0-RTT disposal retain their supported fences and do not resurrect records. BBR ordinals, delivery accounting, recovery episodes, persistent-congestion evidence and reset dispatch remain authoritative, with independent 32,768-outcome and 4,096-retained-delivery bounds. Legacy history does not substitute for those stores.

## Boundary and design cost

One product PR changes the sent-packet handler, legacy tracker, colocated maintained tests, this contract and Unreleased release notes. It adopts the behaviors of upstream #5895, #5900, #5903 and #5906 within the fork's ownership model. The [frozen assessment](https://github.com/the-sarge/quic-go-fast/blob/5b9c88ee7f6d70fd455bc0635bee20046fc8d0fc/docs/audits/issue-962-loss-recovery/README.md) is reference evidence only and stays unchanged.

The correction adds a synchronous scan of an already bounded history to valid ACK paths that previously skipped it. It adds no execution context, mirrored state, public API, wire format, dependency or capacity. Keeping the two existing feedback branches avoids moving BBR's distinct accounting and ECN order. Reusing BBR's retained store was rejected because it would couple generic detection to optional controller evidence; revisit only under an explicitly approved ownership redesign. Splitting these related corrections across product PRs was rejected on merit because the intermediate states add no useful independent outcome.

Adaptive thresholds (#971), Reno undo (#972), changes to #961's registration-prefix design, shared numbering/history rewrites, controller model changes, default selection, performance claims and qualification campaigns are excluded. Preserve [ADR 0001](../adr/0001-upstream-compatibility.md), [ADR 0003](../adr/0003-follow-stable-upstream-releases.md), [ADR 0004](../adr/0004-packet-emission-ownership.md), [ADR 0009](../adr/0009-opt-in-bbrv3.md), [ADR 0011](../adr/0011-recovery-registration-prefix.md), and the [maintained-code conventions](conventions.md).

## Representation and artifact contract

The supported domain is structurally valid decoded `wire.AckFrame` ranges delivered through the existing connection-owned handler lifecycle. Wire parsing owns range representation; the handler owns unsent/skipped admission; the tracker owns retained application identities. Arbitrary malformed in-memory ACK structs and ACKs delivered into already-discarded spaces are outside that caller contract. Existing wire-parser tests cover malformed wire ranges without adding a second parser here.

Exactly-once consumption, packet exclusions and path reset are universal runtime requirements within that domain. The finite regressions below provide behavioral evidence, not an exhaustive proof over all protocol histories. Broader contract closure is not triggered: the centralized owners and focused semantic cases cover the proposed change without an open-ended matrix.

Runtime discovery is shipped behavior. Admission, eligibility and lifecycle guards are required safety enforcement. Maintained regressions are verification aids explicitly approved as merge gates: their payoff is preventing missed/stale feedback and frame or flight lifetime regressions; recovery-package maintainers own them, and they remain until equivalent coverage replaces them or the behavior is retired. Their own completeness does not create recursive verification obligations. This contract, review receipts and journal are process metadata.

## Acceptance evidence

The real-handler regressions live in [spurious_loss_test.go](../../internal/ackhandler/spurious_loss_test.go). They use real packet registration, ACKs and lifecycle calls, with observable frame callbacks, qlog reports and rich feedback values.

| Semantic case | Required disposition | Evidence |
| --- | --- | --- |
| Late-only receipt plus previously acknowledged largest packet | Consume one retained loss without flight/frame retirement or a new RTT sample | `TestSpuriousLossLateOnlyACK` |
| Reordered receipt with or without a newly live packet; overlapping later receipt | Consume only newly covered retained losses; preserve legitimate RTT eligibility and once-only frame actions | `TestSpuriousLossReorderedACK`; existing `TestSentPacketHandlerSpuriousLoss` is the ordinary control |
| Unsent or intentionally skipped number alongside valid retained/live records | Reject before feedback or retirement; a later valid ACK still discovers the retained loss | `TestSpuriousLossACKAdmission`; existing wire ACK parsing tests own malformed ranges |
| Ordinary, MTU, path and ACK-only loss/receipt | Report ordinary loss only; preserve flight subtraction and independent callbacks | `TestSpuriousLossPacketEligibility` |
| Legacy ECN late-only, reordered-live and unchanged-largest-live ACKs | Ignore their otherwise-invalid ECN counts; later eligible advancing feedback still validates | `TestSpuriousLossLegacyECNEligibility` |
| Migration with an old-loss receipt beside a fresh-path ACK | Suppress old-path evidence; fresh-path losses remain discoverable | `TestSpuriousLossMigration` |
| Shared 0/1-RTT, Retry, rejected 0-RTT and Initial/Handshake discard | Preserve identity and disposal without stale reports or callback resurrection | `TestSpuriousLossSharedApplicationSpace`, `TestSpuriousLossDisposal` |
| Overflow beyond 64 records, strict three-PTO cleanup and duplicate processing | Retain the bounded suffix; consume once; clean up even on duplicate ACKs with no live records | `TestSpuriousLossHistoryBound`, `TestSpuriousLossHistoryExpiry`; existing tracker tests |
| Rich feedback on ordinary and late-only ACK branches | Preserve late delivery, ordinals, flight and RTT eligibility without duplicate delivery | `TestSpuriousLossRichFeedback` |
| Retired DATAGRAM frame storage reused before a late receipt | Discover metadata without touching reused frame storage or repeating callbacks | `TestSpuriousLossDoesNotRevisitRetiredDatagram`; existing DATAGRAM packing/ownership tests |
| BBR all-spurious/mixed/superseded/missing evidence, CE/ProbeRTT caps, persistent congestion and PTO/lifecycle controls | Preserve current controller and evidence behavior and bounds | Existing recovery/congestion suites, including `bbr_recovery_test.go`, `bbr_pto_test.go`, delivery sampler, ECN, memory and controller tests |

## Terminating validation and review

Use the approved dedicated feature worktree. Run the focused regressions red before each production correction, then recovery/congestion/wire suites, recovery/congestion race tests, relevant root DATAGRAM packing/ownership and controller loopback tests, affected-package vet, formatting, pinned golangci-lint, `go mod tidy -diff`, diff checks and added relative-link checks. No new benchmark campaign, platform matrix, mutation campaign or repeated-run target is authorized. Hosted workflows supply the existing platform checks.

Open a draft PR. Use one fully briefed fresh RAS review, independent finding dispositions, verification of accepted fixes and at most one replacement review. Quote the issue acceptance criteria verbatim in the review briefing. A required scope expansion, representation mismatch or repeated semantic failure at the same precise invariant and owner stops for an operator decision; deferred or rejected findings do not expand this contract.

After review, certify the exact pushed head/base and clean tree, require applicable existing hosted checks successful on that live head, mark ready and squash merge with `--match-head-commit`, following the [repository overlay](../REVIEW-LOOP.md). If main advances, update the branch and repeat the required review, certification and CI gates. Only after the product merge reaches remote main, append the dev journal without RAS, revalidate deferred findings against the merged revision, and reconcile issue and relevant OmniFocus pointers. The parent remains open for #971 and #972.
