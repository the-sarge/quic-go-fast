# T5 current-path receipt re-audit

This audit retains [child #591](https://github.com/the-sarge/quic-go-fast/issues/591), [product PR #614](https://github.com/the-sarge/quic-go-fast/pull/614) and its one-product-PR outcome. The [current T5 contract](../../adr/2026-09-24-bbrv3-transport-plan.md#t5) is normative. This artifact holds trigger evidence and the routing receipt, not another implementation plan.

## Trigger and precise-root comparison

Replacement review `20260924T180524-62a453f2eff97f25380948ee` found that a valid current-path MTU-probe ACK was excluded from episode exit. Verification first cleared the repair at `b20d219295930a6ee85642d8a05a2456fb65ba8a`. Reverification at `eb1af85419c6d2482b34c6a14cae70c68b9e281a` (only a deterministic Reno-order fixture changed between those heads) found that the repair also admitted alternate-path probe ACKs. The new concern is `sha256:v2:5e15041d47ae75db2e4d4fc8ad5c12fc848782a5dbd2529134e78c004fd23509`.

| Comparison | Earlier counterexample | Later counterexample |
| --- | --- | --- |
| Exact invariant | Validated current-path receipts may cross the episode boundary without acquiring persistent-loss authority | A receipt from an alternate path must not cross that current-path boundary |
| Central enforcement seam | Recovery's ACK-to-exit admission, projected from ledger entries and live/retained ACK metadata | The same two projections admitted proof-excluded probes without distinguishing their path role |
| Semantic class | Same-path MTU-probe receipt: false negative | Alternate-path probe receipt with the same generation: false positive |
| Earlier family obligation | Separate receipt authority from persistent-congestion endpoint eligibility | That separation had to preserve the current-path condition in both projections |

This is the second semantic counterexample at that exact invariant and owner, so the fix/verify cycle stops. It is not a parser representation mismatch. The accepted child, outcome, existing typed metadata and one-product-PR boundary remain intact, so the authorized scoped re-audit applies rather than a new architecture handoff.

## Bounded source reconstruction

At `eb1af85419c6d2482b34c6a14cae70c68b9e281a`, `packet_emission.go:296` passes the packer's path-probe classification to recovery; `serverProbe` and `clientProbe` at lines 522–542 use a supplied destination. `mtuProbe` uses the ordinary queue/handoff at lines 546–560. `captureCongestionSend` copies the path-probe flag and the current generation into `PacketInfo`. The two admission sites in `internal/ackhandler/bbr_recovery.go` accepted proof-excluded ledger entries and generation-matching ACK metadata, respectively. A generation identifies a path epoch; it does not distinguish an alternate-address probe registered during that epoch.

Only these producers, the two receipt consumers, and their disposal/reset seams are involved. Worker I/O, public controller selection, BBR policy, wire parsing and native capability are unchanged.

## Re-audit gates and disposition

- Representation and owner: already validated internal packet receipts, `PathProbe` and `PathGeneration`, owned by connection/emission registration and consumed by one recovery eligibility predicate. The universal typed-domain invariant is current-path receipt authority; no address parser, new public representation or peer-honesty guarantee is introduced.
- Closure: triggered because a false exit can authorize inappropriate restoration and the ledger and retained-ACK projections are independently reachable. The current normative T5 matrix records the bounded semantic classes. Ordinary/ACK-only/MTU receipts are current-path candidates; alternate probes, prior generations and disposed evidence cannot gain exit authority. Persistent-congestion exclusions stay independent.
- Central enforcement: one predicate defines current-generation, non-path-probe eligibility. Both ledger receipt classification and retained/live ACK admission consume it. Ledger caching is valid only within its reset/disposal lifetime; this condition is part of the contract, not an inferred test assumption.
- Artifacts: predicate and lifetime guards are required safety enforcement; private events are shipped behavior; the existing test table is a verification aid, not a new maintained deliverable; this audit and mutable pointers are traceability metadata.
- Authority completeness and preservation: construction carries the existing flags; ACK validation remains upstream; ledger and retained consumers share the predicate; reset/disposal removes old authority. Reno callbacks, protocol parsing, buffers, pacing, ECN and platform boundaries are preserved.
- Evidence: one differential MTU versus alternate-path-probe receipt table in `TestBBRRecoveryEpisodeAllSpurious`, plus the already admitted eight T5 functions and existing reset/disposal/gap fixtures. No new test function, guard mutation, fuzz, repetition, platform or benchmark campaign. Verify the triggering source review at the repaired exact head, certify locally and require same-head hosted checks. No third fresh product review is authorized.
- Context and seams: the relevant current contract, this comparison, the short producer/consumer ranges and the unresolved verification observation fit the remaining bounded context; no whole prior plan or review chronology is needed. The public activation restriction remains until B6; policy remains in B5. No new transitional seam or untraced effect is accepted.
- Split/merge: a separate child would leave the accepted T5 receipt authority unsafe; merging B5 or B6 would introduce policy/public scope without helping this predicate. Retain one independently green product PR and repair the central receipt family in it.

## Resume boundary

Publish this normative correction to main through the docs path, then synchronize child #591's exact plan pointer and refresh pointer-only mirrors. Reconcile PR #614 with that main commit. Only then resume code edits: make the alternate-path regression red, implement the shared predicate, preserve the positive MTU case, run the finite gates and verify the triggering replacement review against the pushed head. Any further counterexample must be independently dispositioned under the shared precise-root and stop policy; this receipt does not preauthorize endless patching.
