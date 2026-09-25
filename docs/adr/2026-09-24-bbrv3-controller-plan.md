# Complete opt-in BBRv3 sender implementation plan

**Date:** 2026-09-24
**Status:** B1 complete; B2 ready; B3–B6 blocked by their declared predecessors
**Track:** B in `QGF-BBR3-20260924`
**Depends on:** Slice edges below; no implicit track-wide dependency
**Related:** [Program](2026-09-24-bbrv3-program.md), [accepted design](../designs/bbrv3.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0004](0004-packet-emission-ownership.md), [0007](0007-managed-ecn-qualification.md), [0009](0009-opt-in-bbrv3.md)
**Normative scope:** Current outcome, boundaries, invariants, evidence, blockers and stop conditions
**Audit history:** [Handoff audit](../audits/2026-09-24-bbrv3-handoff/README.md)
**Parent issue:** pending

## Goal

A complete private draft-derived controller, then one public activation slice after every accepted safety contract is effective.

## Current shape (verified 2026-09-24)

Public Config has no controller selector (`interface.go:102`); Clone copies the struct (`config.go:12`) but populateConfig reconstructs it explicitly (`:97`). Connection creation and migration choose Reno (`internal/ackhandler/sent_packet_handler.go:120,1120`). The current controller interface has per-packet callbacks without rich event/space/disposal identity (`internal/congestion/interface.go:9`). BBR protocol differences and source pins are in the accepted design; there is no implemented BBR model to grandfather. Source anchors use the inspected code tree named in the audit; verify semantic seams at dispatch rather than treating moving line numbers as authority.

## Decision and existing-work disposition

The [design specification](../designs/bbrv3.md) is normative for algorithm/interface details; this plan is normative for slice boundaries, ownership, evidence and dispatch. Their contracts compose, with no implied permission to weaken either. All D01–D13 decisions remain selected; the actual-marking-ledger correction closes a source-proven representation hole. There is no prior implementation slice to retain. The open design-only PR is reworked into this publication and superseded after this package merges; it is never treated as a shipped dependency.

**Rejected alternatives (do not do this):** Do not expose incomplete BBR under a public selector, copy a TCP/QUICHE controller wholesale, move registration/model ownership to workers, infer marking from packet-number interval alone, or alter Reno to simplify a BBR adapter. Do not create a global controller switch, new wire parameter, raw ECN authority, unbounded evidence store, fleet purchase, cross-repository implementation or rollout plan.

**Non-goals:** Changing the default controller; GridCast/wiremux implementation; new platform ECN capability; external controller plugins; unlimited tuning/research; native-performance claims before the accepted campaign; widening the Config positional-literal exception accepted in ADR 0009.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| [B1](#b1) | Complete | Drive private BBR Startup and Drain through transport feedback | T2 | None; see slice budget |
| [B2](#b2) | Ready | Complete draft ProbeBW cycling and congestion bounds | B1 | B1 private terminal Cruise |
| [B3](#b3) | new | Integrate guarded ProbeRTT and genuine idle restart | B2 | None; see slice budget |
| [B4](#b4) | new | Apply persistent classic-ECN response in every BBR phase | B3, T4 | None; see slice budget |
| [B5](#b5) | new | Compose loss undo and persistent-congestion restart | B4, T5 | None; see slice budget |
| [B6](#b6) | new | Expose complete per-connection BBR selection and verify migration | B5 | Private-only activation gate |

## Operating discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern. Their local sources are `/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md` and `/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md`; the repository [execution overlay](../REVIEW-LOOP.md) adds only the current upstream-style hosted-CI, local certification, squash merge and post-merge journal requirements. Do not copy shared policies into this repository or invent `task preflight`/`ci-*` jobs.

For each slice, use one fresh fully briefed review and at most one replacement under the shared finding-disposition/approach-stop rules. Quote its current criteria, domain, guarantee, artifacts and finite evidence budget. Independently disposition findings; no automated fixer. Apply the exact invariant-and-owner comparison before calling findings repeated roots, including verification history. One scope-appropriate local race run is required where this slice changes concurrent queue/connection behavior, not a new repetition campaign. Diagnose failures before reruns. After accepted fixes, certify the exact pushed head, require same-head applicable hosted checks, mark ready and squash with a head guard; append the journal only after merge, then reconcile native blockers/readiness and complete the matching OmniFocus slice task.

Test functions are an upper budget for new focused functions, not a demand to duplicate existing coverage. Each named table has one representative positive per behavior and one negative per materially distinct failure; no full Cartesian product. A guard mutation is optional only where explicitly named and must be one discriminating bypass per central owner, reverted before commit. All other mutation budgets are zero. No extra fuzz/security/platform/timing/repetition expansion is implicit. Verification aids receive proportional review and no recursive closure obligation. Repository-provided hosted matrix remains mandatory; it is not replaced by these local budgets.

## Common representation, artifacts and context rules

The actors are ordinary callers selecting a built-in controller, connection-owned transport processing, a concurrent local worker and a peer whose packets pass the existing authoritative QUIC parsers. Malformed wire syntax remains internal/wire/handshake validation's responsibility; these slices do not implement a new parser or prove peer honesty. Typed internal invariants are enforced over their declared supported domain; terminating tests are finite evidence rather than an exhaustive proof. Unknown or old evidence cannot acquire authority. No new persisted runtime schema is introduced; constructor/copy, path restart and destructive-consumer closure replaces database migration obligations.

Runtime behavior is shipped behavior; generation, bounds, validation and lifetime checks are required safety enforcement. Tests, deterministic traces and temporary fault injectors are verification aids; none is an approved maintained general-purpose product or an excuse to expand the shipped contract. Plans, receipts, context manifests and task pointers are process/traceability metadata. Native campaign-only aids are frozen evidence under the accepted campaign, not an independently maintained emulator/verification framework. If a new maintained aid is necessary, stop and declare payoff, supported domain, owner, retirement policy and budget before putting it on the critical path.

Every slice receives only its current contract, this plan's common sections, named design sections, required ADR paragraphs, named source ranges, relevant existing test declarations and any bounded governing diff. The [audit input inventory](../audits/2026-09-24-bbrv3-handoff/slice-inputs.json) records the initial source ranges and proposed tests. At dispatch resolve named predecessor-owned functions in the merged tree; use a semantic symbol map after line drift, never entire historical plan versions. Limit initial input to 35,000 estimated tokens (`max(bytes/4, whitespace_words*2)`) and relevant unresolved review/history to 3,000 within that ceiling, leaving at least 65,000 of a 100,000-token fresh context for implementation, review fixes and verification. Stop before coding and re-slice if a measured bounded manifest exceeds the ceiling. A predecessor's whole diff is not automatically dispatch input. New tests are implementation output, not initial context.

Public BBR selection remains absent until B6. T/B predecessor slices are independently green private behaviors through real module entrypoints, with constructors selectable only by package-private test plumbing. Ordinary client/server constructors retain Reno. The private controller is not a partially shipped public feature, no temporary algorithm may be labeled qualified, and B6 removes the activation restriction only after all safety behavior is merged. Freeze release/adoption claims until qualification; this does not forbid independently merging private implementation slices.

## Implementation slices

<a id="b1"></a>

### B1 — Drive private BBR Startup and Drain through transport feedback

**What it delivers:** Add the private built-in BBR constructor and event reducer through actual rich recovery input and send allowance output. Implement Startup/Drain, initial/min/max windows, loss bounds, measured bandwidth/RTT, aggregation and limitation semantics needed for that phase, terminating at a non-probing Cruise state. Private tests drive real registration/ACK/loss/emission; all public constructors still choose Reno. Reset/close dispose the private model safely from the start.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** T2 (which requires T3).

**Single owner after merge:** One connection-owned BBR reducer owns model/phase/bounds; sampler and recovery own their existing facts. The BBR pacer consumes the reducer output without additional gain.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** Temporary non-probing Cruise terminal is private and coherent for Startup/Drain testing; B2 replaces it with full ProbeBW. No public name can select this incomplete policy. Private constructor gating is removed by B6 after all prerequisites.

**Blast radius:** New private controller arithmetic, event ordering, BBR-only pacing/flight allowances and reset/cleanup. No public selection, transport protocol or default change. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** The T1/T2 typed event domain, checked byte/time arithmetic and bounded path-local state. Claim exact selected phase behavior within that domain; no capacity/performance proof. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. Recovery fact admission is path/model-local; optional delivery/rate/RTT eligibility is sample-local. The reducer must preserve real-loss ranges and recovery-episode eligibility across sampling fences, whether the fence first arrives through registration or feedback. A reset establishes a lower sample-generation bound for the fresh model; advancing the sampling high-water mark must not erase current-model recovery facts. Retry/path reset and close retain their destructive model lifecycle semantics. Old-model events cannot regain authority. One reducer quantization operation floors both Drain inflight and ACK window targets at max(4M, 2Q, target); this does not change the pacer's Q cap or local 2Q queue admission. The precise invariant is typed-domain authority and output composition, not parser or benchmark completeness. [Scoped re-audit and operator decision](https://github.com/the-sarge/quic-go-fast/pull/627#issuecomment-5824503500) records the causal history outside this contract.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| ACK/sample/real loss input | One model update and declared initial/phase allowance | BBR reducer | `TestBBRTransportFeedbackToAllowance`, Startup/Drain and arithmetic tables | Covered |
| Registration advances sampling with pending recovery | Preserve current-model losses/episode state; admit live earlier-sample losses | BBR reducer fact admission | `TestBBRStartupLossRanges` | Covered |
| 0-RTT sampling fence before a new registration | Admit same-path recovery while excluding stale delivery/rate/RTT | BBR reducer fact admission | `TestBBRPrivateResetAndClose` | Covered |
| Clock/sample validity and model disposal | Optional evidence cannot suppress recovery; old-model facts cannot mutate fresh/disposed state | BBR reducer fact admission and lifecycle reset | Existing loss/lifecycle tables with negative cases | Covered |
| Low BDP dominated by offload allowance | Shared 2Q/four-M floor for window and Drain target | BBR reducer quantization | `TestBBRDrainFlightAndRoundExit`, Q=4950 and post-event flight=8000 | Covered |
| Limited versus capacity sample | No false plateau from supply limitation | BBR reducer | `TestBBRStartupLimitedSamples` | Covered |
| Reset/close | Fresh/disposed model, no stale sample update | BBR lifecycle entry | `TestBBRPrivateResetAndClose`; default Reno constructor table | Covered |

**Evidence budget:** At most 8 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. The operator-approved scoped continuation permits verification of the triggering replacement review followed by one terminal fresh review. Any new required root or unresolved approach stop returns for decision; no automatic budget renewal. This is the B1-only approved exception to the common review budget. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRStartupPlateau`; `TestBBRStartupLimitedSamples`; `TestBBRStartupLossRanges`; `TestBBRDrainFlightAndRoundExit`; `TestBBRInitialWindowAndArithmetic`; `TestBBRTransportFeedbackToAllowance`; `TestBBRPrivateResetAndClose`; `TestPublicConstructionStillReno`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Baseline algorithm and ProbeRTT disposition”, “Byte domains, identities and clocks”, “Delivery sampling and limitation”; the source ranges and test inventory keyed `B1` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Startup without Drain and allowance output would be a horizontal model stub; the bounded Startup-to-Cruise path is the first complete private behavior. Adjacent merge: Merging ProbeBW would add cycling, round randomization and loss attribution dynamics; keep it in B2. Dependency evidence: T2 provides complete sampled feedback and T3 gives the actual pacing/queue enforcement; neither can be faked by the model.

**Stop conditions:** Stop if ordinary public connections can select this partial controller, default Reno changes, or required Startup/Drain logic cannot fit one fresh context. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="b2"></a>

### B2 — Complete draft ProbeBW cycling and congestion bounds

**What it delivers:** Replace B1 private non-probing Cruise with full draft Down/Cruise/Refill/Up behavior, ACK-phase max-bandwidth aging, aggregation compensation, slope/cap growth, real-loss bounds and randomized coexistence scheduling. Exercise event-to-send-allowance traces including delayed and limited feedback; retain production gating.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** B1.

**Single owner after merge:** The existing BBR reducer is the sole phase/filter/bound owner.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** Remove B1 terminal-Cruise simplification. No duplicate model or alternate cycle implementation remains. Private activation persists until B6.

**Blast radius:** BBR-private phase timing, random-number injection, bound interaction and integer packet-equivalent conversion; no additional transport callback or native capability. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Typed BBR events and injected deterministic randomness; packet-equivalent round trigger uses current M. Reference authority is draft-06, not QUICHE default options. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Not triggered for this slice: the change is a bounded single-owner controller-cycle extension; focused phase traces cover its declared behavior and no independent lifecycle/public authority is added.

**Evidence budget:** At most 10 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRProbeBWTransitions`; `TestBBRProbeBWPlateauAndRiskyReprobe`; `TestBBRProbeBWLossBoundAttribution`; `TestBBRProbeBWAckPhaseAging`; `TestBBRProbeBWAppLimitedFilter`; `TestBBRProbeBWCoexistenceUnits`; `TestBBRProbeBWRandomizedWait`; `TestBBRProbeBWAggregationBudget`; `TestBBRProbeBWDelayedFeedback`; `TestBBRProbeBWCapsAcrossPhases`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Baseline algorithm and ProbeRTT disposition”, “Event order, losses and retained evidence”, “Pacing and pending local work”; the source ranges and test inventory keyed `B2` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Splitting by phase leaves transitions and shared bound ownership dependent on successors; this four-phase reducer is one semantic unit. Adjacent merge: Merging RTT probing combines independent timer/filter invariants; B3 can build on completed steady-state behavior. Dependency evidence: B1 supplies the actual model/transport seam and Startup exit into Cruise; B2 replaces its explicit temporary terminal.

**Stop conditions:** Stop if the draft state machine needs a second mutable owner, reference options silently replace baseline behavior, or event semantics must widen beyond T1/T2. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="b3"></a>

### B3 — Integrate guarded ProbeRTT and genuine idle restart

**What it delivers:** Add the accepted five/ten-second filter policy, pre-update saved half-BDP cap, 200ms-plus-completed-round exit, qualified raw RTT input and genuine idle restart through the real model and emission limitation events. Compose window restoration with existing loss bounds and queue debt; handle every normal exit and disposal/reset.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** B2.

**Single owner after merge:** BBR reducer owns ProbeRTT filters/cap/round state; existing sampler/emission own limitation and pending evidence.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** No new temporary representation. Public activation stays gated until B6.

**Blast radius:** BBR min filters, send/ACK entry and exit, idle/resume, packet-size changes and model reset; no recovery RTT/PTO estimator change. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Eligible monotonic RTT/events in the current model generation; stored estimates are observations, not guaranteed propagation truth. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Entry with expired/new RTT | Saved cap cannot grow from triggering inflation | BBR ProbeRTT reducer | aged queue/rising-base fixture | Required before slice completion |
| ACK and send/idle exit | Both hold and qualifying round required | single ProbeRTT exit predicate | long-round and send-resume cases | Required before slice completion |
| Other bounds/reset/size changes | Restore only through current caps; dispose without false completion | BBR output composition | restore/reset table | Required before slice completion |

**Evidence budget:** At most 8 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRProbeRTTExpiredQueuedSample`; `TestBBRProbeRTTRisingBase`; `TestBBRProbeRTTAlreadyInflatedCap`; `TestBBRProbeRTTLongRoundGate`; `TestBBRProbeRTTIdleAndResume`; `TestBBRProbeRTTRestoreBounds`; `TestBBRProbeRTTSizeAndReset`; `TestBBRProbeRTTSendSideExitGate`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Delivery sampling and limitation”, “Baseline algorithm and ProbeRTT disposition”, “Lifecycle and migration”; the source ranges and test inventory keyed `B3` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Splitting filter entry from all exit/restoration paths would introduce a partially authoritative probe state. Adjacent merge: Merging CE response adds separate validated-signal epochs and tuning; keep it independently traceable in B4. Dependency evidence: B2 supplies the real ProbeBW return states, filters and output bounds that ProbeRTT must preserve.

**Stop conditions:** Stop if a normal exit bypasses the common round gate or preserving the cap requires replacing the accepted filter cadence. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="b4"></a>

### B4 — Apply persistent classic-ECN response in every BBR phase

**What it delivers:** Consume validated T4 events and implement half-retention rate/flight caps, response boundaries, Startup/Up exit, stricter simultaneous-loss composition and bounded clean-round recovery. Caps survive ProbeRTT restoration, lower-bound resets, idle and spurious-loss eligibility. Apply after the normal model update in the one send-allowance path; no CE-created losses.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** B3, T4.

**Single owner after merge:** One BBR reducer owns CE caps/response epochs and final allowance composition; T4 alone owns validation and actual marking evidence.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** No second CE state machine or temporary policy candidate. Public activation remains blocked until B6; 0.7 is not silently shipped.

**Blast radius:** BBR phase transitions, cap floors, clean-round latches, ECN failure recovery and diagnostics; no wire counters, loss statistics or platform capability changes. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Validated typed CE events with known current path/ordinal anchors; invalid/missing/ambiguous evidence is excluded. Guarantee is enforcement of this experimental local response, not standardization/fairness. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Validated new epoch / same epoch / unknown anchor | Cut, suppress or exclude by explicit authority | BBR CE reducer | epoch table | Required before slice completion |
| Startup/Refill/Up/Down/Cruise/ProbeRTT | Declared transition, persistent cap | BBR final allowance owner | phase table | Required before slice completion |
| Loss/ProbeRTT/idle/failed validation | Stricter cap; no accidental release or invented loss | BBR final allowance owner | composition table; optional one cap-bypass mutation | Required before slice completion |
| Clean versus dirty/limited/ambiguous round | Gradual release only with whole-round evidence | BBR CE reducer | round-latch fixture | Required before slice completion |

**Evidence budget:** At most 10 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRCESparseAndSustained`; `TestBBRCEPhaseEffects`; `TestBBRCEBoundarySuppression`; `TestBBRCESimultaneousLoss`; `TestBBRCECapsSurviveProbeAndIdle`; `TestBBRCECleanRoundRecovery`; `TestBBRCEFailureRecovery`; `TestBBRCEFloorComposition`; `TestBBRCEUnknownAnchor`; `TestBBRCEIsNotLoss`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Classic ECN response”, “Baseline algorithm and ProbeRTT disposition”, “Pacing and pending local work”; the source ranges and test inventory keyed `B4` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Per-phase CE patches would allow one update path to erase a cap; the shared final allowance owner must be complete in one slice. Adjacent merge: Merging recovery undo would add episode-history mechanics already separately budgeted; B5 verifies its interaction with the completed CE owner. Dependency evidence: T4 is the only validation authority; B3 supplies every normal phase/restoration path on which CE persistence must be demonstrated.

**Stop conditions:** Stop if validated CE must be mapped to fabricated lost packets, two different owners compose allowances, or the response needs a new pre-implementation empirical choice. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="b5"></a>

### B5 — Compose loss undo and persistent-congestion restart

**What it delivers:** Consume T5 episode/persistent events in the complete BBR reducer. Implement all-spurious loss-bound undo and saved-window restoration through independent CE/ProbeRTT caps, plus two-M new-generation persistent restart with no old-ACK rebound. Exercise PTO, superseded/expired evidence, same-path Retry/key disposal and path reset against the full policy.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** B4, T5.

**Single owner after merge:** BBR reducer owns policy reaction; T5 remains sole loss-evidence/episode authority and T2 owns sampler generation records.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** No new seam; connect already complete private event semantics. B6 alone removes public activation gate.

**Blast radius:** BBR recovery/window restoration, plateau state, persistent restart, generation changes, CE preservation and cleanup. Reno and transport loss timers remain unchanged. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Eligible T5 typed evidence, bounded retained history and current-generation ACKs. No inference from missing records or PTO count. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Complete all-spurious episode / incomplete evidence | Undo only proven loss-derived state | BBR recovery event consumer | all/mixed/evicted fixture | Required before slice completion |
| Undo with CE/ProbeRTT | Never remove independent cap | BBR final allowance owner | composition test | Required before slice completion |
| Persistent span and repeated/old feedback | One two-M fresh-generation restart; no rebound | BBR lifecycle reducer | restart/old-ACK fixture | Required before slice completion |
| PTO versus Retry/close/path reset | No RTO inference, explicit disposal | BBR lifecycle reducer | reason table | Required before slice completion |

**Evidence budget:** At most 8 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRUndoAllSpuriousOnly`; `TestBBRUndoKeepsCEAndProbeCaps`; `TestBBRUndoMissingAndSuperseded`; `TestBBRPersistentRestartTwoPackets`; `TestBBRPersistentRestartOldAckExcluded`; `TestBBRPersistentRestartDeduplicated`; `TestBBRPTOAndRetryPolicy`; `TestBBRFullLifecycleDisposal`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Event order, losses and retained evidence”, “Classic ECN response”, “Lifecycle and migration”; the source ranges and test inventory keyed `B5` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Separating saved-bound restoration from its persistent/reset consumers risks reviving abandoned state; the reducer interaction is one behavior. Adjacent merge: Merging public activation increases compatibility and real-connection blast radius; B6 gets a complete internal policy to expose. Dependency evidence: T5 supplies trusted episode evidence; B4 supplies independent CE caps whose preservation is part of undo correctness.

**Stop conditions:** Stop if undo needs missing history, persistent restart revives old state, or recovery policy requires changing default Reno. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="b6"></a>

### B6 — Expose complete per-connection BBR selection and verify migration

**What it delivers:** Add SetCongestionControlV1 and getters with the accepted names/errors, private immutable Config selection, all clone/population/per-client paths, client/server construction and selected-identity-preserving migration. Remove the private-only activation restriction and prove complete BBR behavior through real loopback STREAM/DATAGRAM/control traffic and actual queue/migration boundaries. Retain ECN fence/fallback and old pending debt; ordinary callers still default to Reno.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** B5.

**Single owner after merge:** Config preparation owns immutable selected identity; each connection creates its sole BBR model; reset uses the selected built-in factory. Emission/validator retain T3/T4 ownership.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Public activation additionally closes every copy and caller path in this slice.

**Transitional-seam budget:** Remove B1–B5 private-only activation gate. Private constructors remain implementation details rather than new public generic factories. No transitional seam remains.

**Blast radius:** Public method compatibility, Config shape, getters/concurrency, effective copy and callback replacement, Dial/Listen/reconnect/migration, resource cost and optional tracing; the bounded testdata consumer and isolated module-resolution checks below. No GridCast/wiremux edits, transport parameters or platform feature claims. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Compatibility evidence shape:** B6 extends the existing small structural-consumer pattern in [testdata/externalpacketio](../../testdata/externalpacketio/README.md) with a `testdata/congestioncontrol` source fixture. Copy the same fixture into two isolated temporary main modules requiring real `github.com/quic-go/quic-go v0.62.0`; only the fork module replaces it with the absolute candidate worktree. Use the repository-approved Go toolchain, `go mod tidy` and three total executions: upstream default (ordinary compilation plus explicit unsupported selection), fork Reno and fork BBR. Assertions use only structural interfaces and ordinary strings. Record commands and module-resolution receipts. The B6 implementer owns this finite compatibility evidence and its small fixture; it is a verification aid like existing testdata, not a new runner, workflow, runtime dependency or maintained general-purpose verification product.

**Representation contract:** Canonical exact strings reno/bbrv3 validated by the setter; keyed Config compatibility with the explicitly accepted positional-literal exception. Existing QUIC parsers and supported transport paths retain representation ownership. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Unset/nil/known/unknown Config selection | Default or validated immutable choice, no mutation on failure | Config setter/preparation | setter/copy table | Required before slice completion |
| Clone/populate/callback complete replacement | No silent selection loss or shared live model | Config preparation | copy/callback fixture | Required before slice completion |
| Client/server and path replacement/rebind | Fresh selected model; queue debt and ECN fence remain effective | connection factory/reset | real connection and migration tests | Required before slice completion |
| Explicit unsupported upstream capability | Visible adapter error; default upstream still builds | structural consumer fixture | upstream/fork matrix | Required before slice completion |

**Evidence budget:** The three isolated consumer executions described above plus at most 8 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestCongestionControlV1ValidationAndCopy`; `TestCongestionControlV1PerClientReplacement`; `TestCongestionControlV1IndependentConnections`; `TestCongestionControlV1RealStreamAndDatagram`; `TestCongestionControlV1MigrationKeepsIdentity`; `TestCongestionControlV1QueuedMigrationAndECNFence`; `TestCongestionControlV1UpstreamCapabilityDetection`; `TestCongestionControlV1DefaultReno`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Selection and compatibility contract”, “State ownership and private boundaries”, “Lifecycle and migration”, “Consumer adoption and diagnostics”; the source ranges and test inventory keyed `B6` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Exposing the setter separately would promise an incomplete/unavailable BBR policy; construction, copies and migration are one public capability. Adjacent merge: Merging qualification would turn one implementation PR into a multi-platform campaign and conflate correctness with adoption judgment. Dependency evidence: B5 transitively completes every T/B prerequisite. Public selection cannot precede any accepted safety contract.

**Stop conditions:** Stop if default behavior changes, optional assertions require a fork-only public Go type, migration exposes stale authority or the admitted Config compatibility exception expands. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

## Acceptance criteria

- Each listed slice delivers its stated behavior through the named real seam and is independently green without an unmerged successor.
- The stated owner enforces the declared typed-domain invariant, and all listed materially distinct semantic classes have the required disposition/evidence within the finite budget.
- Every retained temporary seam has the stated coherent intermediate contract and named removal slice; no public caller can select incomplete BBR.
- Existing Reno/API/wire/ownership/platform behavior remains unchanged except the explicitly accepted opt-in behavior and Config literal compatibility exception.
- No accepted requirement depends on an unapproved maintained verification tool, unknown host, invented measurement or untraced authority.

These criteria range only over the per-slice supported domain, named parser/representation owners and guarantee level. They do not claim universal measured performance. Their terminating evidence is the slice table and focused gates, not an open-ended search for counterexamples.

## Validation gates

Before commit, run scoped new/affected tests and affected-package `go test`, plus `go vet` for changed Go packages and `go mod tidy -diff` for Go/dependency changes. T2/T3/T4 and B6 require one focused `go test -race` over their named concurrency/lifetime cases; other slices use race only when they modify a concurrent seam, as identified in the implementation preflight. Pin exact test regexes/packages in that preflight from the proposed names; do not broaden into unbounded stress. Existing hosted unit, integration, lint and cross-compilation jobs run for drafts and must be successful at the exact merged head. Q1/Q2 additionally use the accepted campaign's finite native/operational gates and ledger. Docs-only certification uses the repository overlay's clean exact-head diff/link/graph checks and independent contract review.
