# RAS Consideration Synthesis

Source: docs/designs/quic-multipath-d0.md

Reviewed candidate: `3b5399df8da2888a9f6e7748dbe75aaf825cb12e`. HEAD matched; checkout was clean and remained unchanged. This queue authorizes design corrections only, not implementation, experiments or dispatch.

Original reporting agents are identified below. The supplied adjudications have no author labels; their dissent is preserved without inventing attribution.

## Fix First

1. **F1 — Select destination-specific native-error policy. Severity: high. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:73`; core outcome contract at `docs/designs/quic-multipath.md:165`. **Reported by:** claude-opus, C-018.

   **Evidence:** The core delegates destination-error consequences to a separately accepted policy. D0 defines known-accepted PTO suspicion and endpoint-fatal fanout, but does not supply that policy. At the pinned source, `send_queue.go:246` propagates non-size errors, and `connection.go:1018` demonstrates connection destruction on queue failure. Definite local rejection cannot advance the selected PTO-based failure detector.

   **Required fix:** Define a connection-owned, path/tuple-generation-qualified transition for destination-specific errors, separate from timeout/backpressure, PMTU feedback and endpoint failure. Make the affected multipath path ineligible/suspect and apply the existing survivor-selection rules without destroying healthy survivors. With no eligible survivor, specify bounded recovery and the existing no-progress deadline. State ordinary/non-negotiated behavior explicitly. Determine submission certainty independently of error policy; an error category alone must not authorize unsubmitted disposal.

   **Required verification:** Paper-trace a destination failure with a survivor and without one, alongside the existing timeout and size-error branches. Specify future deterministic cases verifying correct attribution, no fabricated congestion loss, survivor continuity and the selected last-path behavior.

   **Dissent retained:** Some proposed fixes require immediate connection failure when no eligible path remains. That is unnecessary and conflicts with D0’s existing bounded recovery/no-progress policy.

2. **F2 — Define recovery eligibility before local completion consumption. Severity: medium. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:17`; `docs/designs/quic-multipath.md:135` and `:168`. **Reported by:** codex-astra, C-009.

   **Evidence:** `packet_emission.go:229` registers before output. `internal/ackhandler/sent_packet_handler.go:313` records registration time as `SendTime`; loss detection at `:919` evaluates registered packets below the ACK watermark. A locally expired packet A can therefore produce congestion-loss evidence if a later accepted packet D’s ACK is processed before A’s disposal completion.

   **Required fix:** Define connection-owned pending-local recovery eligibility. Preserve pre-I/O registration, PN consumption and reservations, while preventing unresolved local submissions from generating network-loss evidence. Specify transitions for accepted, uncertain, proven-unsubmitted and authentic early-ACK outcomes, including exact-once reconciliation. Neither the endpoint writer nor public sentinels become recovery authorities.

   **Required verification:** Specify a deterministic case where D’s ACK is processed while A’s queued-expiry completion remains unconsumed; A must produce no network-loss event. Include early ACK before native completion in the same owner’s transition table.

   **Dissent retained:** One adjudication proposes only draining published completions before ACK/loss processing. That closes the cited published-expiry ordering but does not establish eligibility for submissions whose outcomes are still unpublished. An ordering-based alternative must cover those states too.

3. **F3 — Bound retained migration generations and transport attachments. Severity: medium. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:36` and `:42`. **Reported by:** codex-astra, C-003.

   **Evidence:** Four retained protocol-path records bound retired wire paths, not expressly old generations of a still-live path. Serial migration admission does not bound completed cutovers awaiting retention expiry. `conn_id_generator.go:199` registers active CIDs on every distinct added runner, independently of CID-record count.

   **Required fix:** Select an explicit finite connection-wide allowance covering retained tuple-generation metadata and transport-routing attachments. Reserve capacity before committing migration; defer or return the selected resource-limit result when full. Release charges only after required retention, submission fences, routing removal and captured callbacks resolve. Account for reused transports without losing generation-specific obligations. Do not shorten protocol retention.

   **Required verification:** Trace repeated completed migrations before the first retention expiry, exhaustion, cancellation and eventual detachment. Every retained generation and distinct routing attachment must have a charge and a single release owner.

   **Dissent retained:** Adjudications propose different numerical caps. None is established by measurements; the author must select the finite limit explicitly rather than silently treating the protocol-path cap as sufficient.

4. **F4 — Make public backpressure semantics consistently cause-only. Severity: medium. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:57–63`. **Reported by:** claude-opus, grok and claude-fable; C-002, C-007 and C-011 consolidated.

   **Evidence:** The Backpressure row lists capacity refusal and proven-unsubmitted expiry, while Deadline includes native deadlines and matches Backpressure. The following paragraph correctly says backpressure conveys no delivery certainty. These descriptions invite incompatible public `WriteTo` retry decisions.

   **Required fix:** Align the table with the existing cause-only contract: Backpressure identifies local backpressure and does not prove non-submission or safe replay. Define the intended `errors.Is` match sets explicitly, including uncertain deadlines. Forbid inferring non-submission from Backpressure or zero returned bytes alone. Keep accepted-prefix, uncertain-remainder and unsubmitted-tail enums as internal disposal authority.

   **Required verification:** Add a paper classification table for refusal, queued expiry, accepted-prefix timeout and uncertain native timeout. Specify a future negative case proving that an uncertain timeout never authorizes replay or unsubmitted disposal merely because it matches local-cause sentinels.

   **Dissent retained:** Several adjudications prefer excluding uncertain deadlines from Backpressure or adding a new sentinel. Those are alternatives, not required fixes. `errors.Is` is not universally transitive through arbitrary custom `Is` implementations; the defect is the contradictory documented meaning. The internal-disposal corruption claimed by some reports is already prohibited by the core contract.

5. **F5 — Make primary succession and recovered-backup state explicit. Severity: medium. Confidence: medium.**

   **Location:** `docs/designs/quic-multipath-d0.md:65`, `:71` and `:75`. **Reported by:** grok, claude-opus and claude-fable; C-004, C-015 and C-019 consolidated.

   **Evidence:** Promotion, recovery “as backup,” application local use and the public primary flag are not connected by an explicit transition rule. Implementations can disagree about whether promotion transfers the flag and which field recovery changes.

   **Required fix:** State that promotion and primary abandonment assign the designated primary to the selected eligible survivor. Specify recovery as `suspect → live` with automatic local-use demotion to `backup`, consistent with the selected recovery policy, and document how a subsequent `SetLocalUseV1` call overrides that state. Preserve active-before-backup preference and healthy-current selection in the all-backup case. Define the no-eligible-path flag behavior. Preserve the separately selected legacy RTT rule: path zero while live, otherwise the designated primary, then the retained terminal snapshot.

   **Required verification:** Use one transition table covering initial state, suspicion, promotion, recovery, a later explicit active request, all-backup selection and primary abandonment. Each state must yield deterministic scheduling and snapshots.

   **Dissent retained:** Some reviewers assume local use can change only through application calls and propose primary-first scheduling or traffic-following RTT. D0 makes neither promise and explicitly selects active preference, recovered-backup behavior and path-zero-first RTT. The original high-severity “two schedulers” claim is narrowed to an observable contract ambiguity.

6. **F6 — Name the delivery-rate estimate owner for Reno aggregation. Severity: medium. Confidence: high on the missing mapping.**

   **Location:** `docs/designs/quic-multipath-d0.md:81–85`. **Reported by:** claude-fable, C-010.

   **Evidence:** The aggregate scheduler requires a path delivery-rate estimate under Reno and BBR. The pinned default constructor passes a nil congestion-event sink at `internal/ackhandler/sent_packet_handler.go:140`; dispatch is created conditionally at `:182`. Existing delivery-observation integration is BBR-specific.

   **Required fix:** Identify the producer, storage budget, update events and ownership of the scheduler’s estimate under each controller. For Reno, explicitly select either a path-local estimate independent of BBR controller selection or intentional use of the already-declared no-sample fallback. Apply cross-path exclusion and fresh-anchor rules to any estimator. Preserve Reno undo/adaptive-loss ownership and keep ordinary unregistered behavior unchanged.

   **Required verification:** Map eligible ACK evidence through the selected estimator to scheduler input for both controllers. Specify one representative valid-sample case and one excluded-cross-path case if a Reno estimator is selected; otherwise document that Reno remains in fallback.

   **Dissent retained:** Current code does not prove future Reno estimation is impossible. The declared byte-round-robin fallback is not a silent policy substitution or a second public option. Removing Reno aggregation and alleging duplicate measurement cells are not justified by this finding alone.

7. **F7 — Label aggregation comparators and controller-specific qualification claims. Severity: medium. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:85–95` and `:109`. **Reported by:** claude-opus, C-014.

   **Evidence:** Six aggregation arms include one pair of single-path baselines and one independent-connections baseline, alongside Reno and BBR multipath. Their controllers and the combinations qualified by gate 3 are unspecified. One baseline set cannot be controller-matched to both simultaneously.

   **Required fix:** Add a six-arm table naming controller, scheduler, comparator and support claim. Preserve the 54 aggregation observations and accepted thresholds. A coherent bounded mapping uses BBR single-path/independent baselines and the BBR fixture comparator to evaluate BBR aggregate, with Reno multipath reported separately and unmatched Reno aggregation claims remaining unqualified. State the consequence of each controller’s pass/failure; an informational result cannot qualify another combination.

   **Required verification:** For every claimed pass, identify the matched baseline and shared-bottleneck competitor comparison. Recalculate `6 × 3 × 3 = 54`. Cross-check the selected Reno estimator/fallback from F6.

   **Dissent retained:** Adding arms to reach 81 observations or dropping Reno from the design is not required. Supporting implementation exploration and qualifying an adoption/support claim are distinct.

8. **F8 — Assign evidence to milestone candidates and budget affected revalidation. Severity: medium. Confidence: high on the accounting gap.**

   **Location:** `docs/designs/quic-multipath-d0.md:103–113`; core M7 closure requirement. **Reported by:** claude-opus, C-016.

   **Evidence:** The regular inventory contains 64 non-aggregation and 54 aggregation runs, but does not assign them to milestone revisions. The core requires affected closure work after M6 changes shared behavior.

   **Required fix:** Add a milestone/candidate/cell ledger distinguishing Reno-first evidence from M6 evidence. Identify the bounded subset of existing semantic cells requiring post-M6 revalidation and charge it, failures and diagnostics against the remaining 62 instances and eight host-hours. State that relevant code changes are valid recorded revalidation causes. Define BBR-specific coverage and remaining gaps without automatically expanding the matrix. Missing applicable evidence or exhausted budget stops the corresponding claim.

   **Required verification:** Reconcile every claimed milestone gate with its candidate revision, applicability rationale and charged runs/time. Preserve Q2’s blocking edge and prohibit automatic evidence transfer.

   **Dissent retained:** The current wording requires a recorded cause, not specifically a failed test. It does not prove revalidation is forbidden or that Reno-first must have a separate public release before M6.

9. **F9 — Specify participant-exhaustion refusal behavior. Severity: low. Confidence: high.**

   **Location:** `docs/designs/quic-multipath-d0.md:27`. **Reported by:** claude-opus and claude-fable; C-006 and C-012 consolidated.

   **Evidence:** Reservation before setup makes the 4,096-participant ceiling apply to ordinary peers on opted-in endpoints. The limit is explicit; its local-error and server-refusal mappings are not. `server.go:1035` already provides bounded pre-construction refusal with drop-on-full behavior; `:1187` sends `CONNECTION_REFUSED`.

   **Required fix:** Explicitly describe the ordinary-peer admission consequence. Select local `ErrMultipathResourceLimitV1` and the existing best-effort server refusal route through the capped response class, with drop when unavailable. Require corresponding usage documentation when the API is implemented.

   **Required verification:** Specify one cap-plus-one admission case, including response-class exhaustion and unchanged unregistered behavior.

   **Dissent retained:** Configurability, a larger ceiling, lazy reservation and another performance cell are unnecessary. Ordinary wire compatibility does not promise unlimited connection admission.

10. **F10 — Separate native traversal evidence from tolerance evidence. Severity: low. Confidence: medium.**

    **Location:** `docs/designs/quic-multipath-d0.md:24` and `:105`. **Reported by:** claude-fable, C-008.

    **Evidence:** D0 distinguishes injected poll-path traversal from actual pressure, but does not explicitly assign evidence for the 20 ms supported-environment tolerance.

    **Required fix:** Identify the already-planned non-injected native observations supplying timing evidence. Record injected traversal, outcome classification and instrumentation configuration separately. Deterministic tests establish timer/ownership behavior, not native wall-clock qualification.

    **Required verification:** Require the existing fixture report to distinguish observed poll traversal, actual pressure, native return and completion-publication timing. Inconclusive execution remains a qualification stop within the existing budget.

    **Dissent retained:** No inspected measurement establishes that injection cannot meet the bound. Do not disable overdue enforcement or automatically exempt failures to obtain a pass.

## Do Not Act On

- **C-005/C-013 — Mandatory completion-pool enlargement or partitioning. Reported by claude-fable/claude-opus; rejected as a contract violation, high confidence.** Several adjudications endorse stronger isolation, but D0 expressly conditions fairness on available completion capacity. Four records per connection is a maximum, not an entitlement reserved for all 4,096 participants. Shared exhaustion is possible; the 128 in-flight plus 128 retained example establishes temporary exhaustion, not permanent starvation after those in-flight records drain. Do not add per-participant or class reservations without selecting a stronger requirement.

- **C-020/C-021 — Automatically reopen overdue endpoints or add another timeout. Reported by claude-opus/claude-fable; defer to the existing qualification gate, medium confidence.** Substantial dissent argues that scheduling delay can trigger irreversible faults. The combined contract, however, bounds completion through publication, assumes a responsive qualified runtime and explicitly distinguishes returned timeout backpressure from unresolved overdue ownership. No supplied measurement demonstrates a violation inside that qualified domain. Preserve quarantine, late-return reconciliation and successful-join requirements. Qualification failure must stop support; it must not be hidden by an arbitrary second bound or wakeup heuristic.

- **C-001 — Unify address types. Reported by claude-fable; rejected as a blocking finding, high confidence.** Mutable input addresses and immutable snapshot values serve different purposes. No routing or authorization failure from comparing public representations is established. Normalization documentation can accompany implementation; no signature change is required.

- **C-017 — Established initiation-anchored key-update violation. Reported by grok; rejected as stated, high confidence.** The core already anchors spacing after confirmation, and D0 does not expressly supersede that anchor. Naming confirmation in D0 is a permissible editorial clarification, not a new protocol repair. Do not infer initiation-time spacing from shorthand.

- **Unsupported escalation and duplicate work.** Do not count C-007/C-011, C-012, C-015/C-019 or C-021 as additional fixes beyond their consolidated roots. Do not reopen accepted performance thresholds, budget, benefit timing, platform scope or nonfatal local-backpressure policy. Missing planned evidence is not current qualification and does not authorize extra campaigns.

These findings are not established repetitions of the prior nine resolved items merely because they concern I/O or lifecycle. F2 concerns recovery eligibility before consumption; F3 concerns accumulated migration retention; F4 concerns public classification. They have distinct enforcement seams from endpoint expiry, terminal publication and grant-return ownership.

## Verification

For the design correction, run documentation checks only:

```sh
git status --short --branch
git rev-parse HEAD
git diff --check 3b5399df8da2888a9f6e7748dbe75aaf825cb12e -- docs/designs/quic-multipath-d0.md docs/designs/quic-multipath.md
```

- Check changed relative links, tables and one-physical-line-per-paragraph formatting.
- Complete the paper traces and ownership/accounting tables specified in F1–F10.
- Reconcile `12 + 16 + 36 + 54 = 118`, all revalidation charges, the 180-instance ceiling and eight host-hours.
- Preserve ordinary/unregistered behavior, uncertainty handling, DATAGRAM disposal, native-borrow quarantine, Q2 sequencing and the later independent slice audit.
- Record future deterministic tests in the design; do not run Go tests, native experiments, benchmarks or interop campaigns during this design-only disposition.
- Require an independent author disposition for every consolidated finding. No unresolved stop-for-decision may be described as dispatch-ready.

## Resolution Reply

Use one row per F1–F10; record any disagreement with the rejected claims separately.

| Finding | Disposition: fix-now / defer / reject / stop-for-decision | Contract basis and change location | Verification result or future gate | Remaining blocker |
| --- | --- | --- | --- | --- |
| F… | … | … | … | … |

## Infrastructure Warnings

- finding_adjudication_recovery_diagnostic: agent "agy-argon" R2 $candidate[0]: semantic_validation: semantic_rejected: adjudication scope validation failed: run_id mismatch: got "20261024T234308-04d528a09-a2f1d127234a766cc01678768fe6ec85" want "20261024T234308-04d528a09a905a64ef3a9c49"
