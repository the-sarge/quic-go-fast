# E2C terminal-authority scoped re-audit

This is execution history for [E2C (#1086)](https://github.com/the-sarge/quic-go-fast/issues/1086), parent [#1012](https://github.com/the-sarge/quic-go-fast/issues/1012), product [PR #1104](https://github.com/the-sarge/quic-go-fast/pull/1104). The [current E2C contract](../adr/2026-10-24-multipath-endpoint-plan.md#e2c) owns the outcome, matrix and terminating evidence. This receipt does not dispatch another child or change program ordering.

## Trigger and preserved state

The initial RAS review `20261102T155156-0e5c5165c7152bcb8ea5c05e` reviewed `d8019d011234a6fa9fdc3af8d172171eed85520b` and identified seven distinct defects. The [initial disposition](https://github.com/the-sarge/quic-go-fast/pull/1104#issuecomment-6582025041) accepted all seven within the current contract. The first correction was `45c1460d010eecb4ff7ff6a94c09306c2eebdbec`; its verification resolved the original clusters and exposed a distinct BBR missing-range consumer defect. The [precise-root comparison](https://github.com/the-sarge/quic-go-fast/pull/1104#issuecomment-6582470503) distinguished that concrete owner from local-table compaction. Correction `a7ef755ab14145769a1fe24313c693e1e67a6a8b` is preserved on `codex/e2c-local-ecn`; the [correction receipt](https://github.com/the-sarge/quic-go-fast/pull/1104#issuecomment-6582519884) records its evidence.

Re-verification at that head resolved the BBR compaction integration and the other source families, but reported C-003/C-009 still open. Real-handler test overlays for both Reno and BBR used an uncertain marked ACK-only packet followed by cumulative reports requiring its possible transmission. The packet was protocol-retired without reaching the ECN terminal operation, leaving one unresolved identity, pending constraints, continued ECT(0) and no BBR CE delta. The production candidate and repository files were unchanged by those verifier overlays. Implementation edits, product RAS cycles, certification and new product CI initiation are frozen until this amendment is authoritative.

## Precise repeated-root comparison

| Evidence | Exact accepted invariant | Concrete enforcement owner | Distinct semantic class | Why the first family had to cover it |
| --- | --- | --- | --- | --- |
| Initial C-003/C-009 | Uncertain marked-packet retirement must end unsupported ECN deferral without inventing transmission or loss credit | The connection-owned local ECN terminal decision, previously reached through `localECNEvidence.lost` | ACK-eliciting uncertain packet reaches loss/retirement notification but remains unresolved | The accepted typed packet domain includes ordinary and ACK-only registrations, and terminal reconciliation is independent of controller loss accounting |
| Verification at `a7ef755ab14145769a1fe24313c693e1e67a6a8b` | Same invariant | Same intended terminal decision; `IsAckEliciting` filters its notification before the owner is reached | An uncertain marked ACK-only packet is protocol-retired without notifying that owner | The earlier accepted terminal family required authority-complete admission to the one decision, rather than only an ACK-eliciting caller fix |

This is a second semantic counterexample at the same accepted terminal owner. It triggers the shared approach-stop rule. The automatic stop does not select a different product boundary: the existing child, typed representation, outcome and one independently green product PR remain viable, so the original invocation authorizes this scoped re-audit. No operator product or representation decision is needed.

## Owner design and gate results

The connection-owned registration already records protocol ownership through `packet`, authenticated delivery through `acked`, and native submission outcome through `outcome`. One ECN terminal predicate consumes those facts when the shared `retireLocal` operation settles protocol ownership and when a native completion changes the outcome. It covers either event order and has no ACK-elicitation gate. Qualified network-loss notification stays a separate policy and does not become the authority for local terminal reconciliation.

The retirement census is bounded to existing `retireLocal` callers: authenticated ACK removal; network loss of every retained packet; PTO extraction; Retry; Initial/Handshake/0-RTT disposal; migration; and local close. Proven-unsubmitted completion already has its own resolved native disposition and cannot become an uncertain terminal record. The reconciler validates the existing ECN record's registration identity, ignores unmarked/non-ECN/closed evidence, and leaves native borrow and completion-token lifetime with E2A. A retirement-before-outcome path is represented by the existing cleared protocol pointer, not another flag. All these fields keep their original meanings.

| Gate | Current disposition |
| --- | --- |
| Representation and guarantee | Same private typed registrations, authenticated ACKs, native outcomes and lifecycle operations; existing parsers own external formats; universal guards inside that domain, finite regression evidence |
| Artifact classes | Runtime qualification is shipped behavior/safety enforcement; the existing three regression scenarios remain maintained verification aids owned by the transport suite and retired only by replacement or feature removal; this amendment/receipt and mirrors are metadata |
| Evidence | Extend existing uncertain-outcome/lifecycle variants for ACK-only loss, PTO and migration before/after native return; reuse early-ACK, consumed-ordinal and close/fresh-instance evidence; no new function, platform, race, mutation or operational campaign |
| Single owner and authority | Shared retirement records protocol terminal state; one ECN predicate handles terminal uncertainty from retirement and completion, independent of ACK elicitation and congestion policy |
| Transitional seams and preservation | Reuse E2A registration identity/epoch/token and E2B facts; retain default/legacy ECN sequence, controller formulas, counter fences, exact-once delivery and native borrow lifetime |
| Blast radius | Local completion/retirement hooks, existing ECN consumers and selected handler tests only; no endpoint, socket, external format, worker, new store or public activation; no admitted untraced effect |
| Context fit | Current slice/shared E2 contract plus the bounded retirement/ECN symbols and selected tests fit the existing 24k dispatch target; complete historical plans and review chronology stay outside dispatch |
| Split/merge | Another child would leave this ECN lifecycle incomplete; merging endpoint integration would cross an independently green owner boundary; reject both on merit |

By the shared design-cost order, a central predicate reuses an existing lifecycle fact and removes an admission obligation tied to caller packet content. Moving only the loss notification outside `IsAckEliciting` would repair the observed ACK-only loss but retain separate PTO, migration and late-completion obligations; reject on merit. Adding a mirrored terminal flag or separate callbacks per lifecycle caller would add redundant state or scattered policy; reject on merit. The chosen design changes no public/closed contract and creates no execution context or resource cap.

## Publication and resumption

Publish this current contract through the repository's normal docs PR path and verify it reaches main before synchronizing the exact child plan pointer. Parent/program issues and OmniFocus notes retain only current state, blockers/frontier and pointers. The product PR remains the owner of eventual E2C completion and E2 readiness; this amendment does not mark the slice complete, close its child, or create a routine frontier follow-up.

After publication, reconcile the preserved product branch with main, emit the revised execution receipt, implement the central terminal correction, and verify the triggering source review at the exact pushed head before using the one unused replacement fresh review. The initial review is consumed; no product fresh-review budget is added. Existing exact-head local and hosted checks still follow. Append-dev-journal starts only after the product PR is merged.
