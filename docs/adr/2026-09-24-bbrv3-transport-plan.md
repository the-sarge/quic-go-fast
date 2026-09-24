# Transport feedback and send ownership implementation plan

**Date:** 2026-09-24
**Status:** T1/T2/T3 implemented; T4/T5 ready
**Track:** T in `QGF-BBR3-20260924`
**Depends on:** Slice edges below; no implicit track-wide dependency
**Related:** [Program](2026-09-24-bbrv3-program.md), [accepted design](../designs/bbrv3.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0004](0004-packet-emission-ownership.md), [0007](0007-managed-ecn-qualification.md), [0009](0009-opt-in-bbrv3.md)
**Normative scope:** Current outcome, boundaries, invariants, evidence, blockers and stop conditions
**Audit history:** [Handoff audit](../audits/2026-09-24-bbrv3-handoff/README.md)
**Parent issue:** [#584](https://github.com/the-sarge/quic-go-fast/issues/584)

## Goal

Typed feedback, bounded delivery evidence and local send limits through existing recovery/emission seams; default Reno remains behaviorally unchanged.

## Current shape (verified 2026-09-24)

Recovery registers post-send flight before its congestion callback (`internal/ackhandler/sent_packet_handler.go:252`), returns early for no newly tracked ACK (`:398`), detects loss (`:787`), extracts PTO frames (`:1041`) and reconstructs Reno on path migration (`:1120`). Packet emission registers before handoff (`packet_emission.go:283`); path probes explicitly use Not-ECT (`:499,511`), and coalesced emission preserves its supplied mark (`:441`). The queue owns dequeued/in-service batches until write/cleanup (`send_queue.go:147,201,225,273`). The existing pacer adds a 5/4 gain and 1ms minimum (`internal/congestion/pacer.go:21,92`). Source anchors use the inspected code tree named in the audit; verify semantic seams at dispatch rather than treating moving line numbers as authority.

## Decision and existing-work disposition

The [design specification](../designs/bbrv3.md) is normative for algorithm/interface details; this plan is normative for slice boundaries, ownership, evidence and dispatch. Their contracts compose, with no implied permission to weaken either. All D01–D13 decisions remain selected; the actual-marking-ledger correction closes a source-proven representation hole. At the original handoff there was no prior implementation slice to retain; the design-only PR was reworked into the publication and superseded, never treated as a shipped dependency. Current retained-work authority lives in each slice’s existing-work disposition.

**Rejected alternatives (do not do this):** Do not expose incomplete BBR under a public selector, copy a TCP/QUICHE controller wholesale, move registration/model ownership to workers, infer marking from packet-number interval alone, or alter Reno to simplify a BBR adapter. Do not create a global controller switch, new wire parameter, raw ECN authority, unbounded evidence store, fleet purchase, cross-repository implementation or rollout plan.

**Non-goals:** Changing the default controller; GridCast/wiremux implementation; new platform ECN capability; external controller plugins; unlimited tuning/research; native-performance claims before the accepted campaign; widening the Config positional-literal exception accepted in ADR 0009.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| [T1](#t1) | Complete ([#587](https://github.com/the-sarge/quic-go-fast/issues/587)) | Capture logical congestion feedback without changing Reno | None | None; see slice budget |
| [T2](#t2) | Complete ([#588](https://github.com/the-sarge/quic-go-fast/issues/588)) | Deliver bounded registration-time sampling through real recovery | T1, T3 | None; see slice budget |
| [T3](#t3) | Complete ([#589](https://github.com/the-sarge/quic-go-fast/issues/589)) | Bound paced local sends across the complete worker lifetime | None | None; see slice budget |
| [T4](#t4) | Ready | Validate bounded actual ECN marking through path changes | T1, T3 | None; see slice budget |
| [T5](#t5) | Ready | Emit bounded persistent-congestion and recovery evidence | T2 | None; see slice budget |

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

<a id="t1"></a>

### T1 — Capture logical congestion feedback without changing Reno

**What it delivers:** Extract a private event-dispatch seam through SentPacket, accepted ACK handling and timer-driven loss. Preserve current Reno callback order, original prior-flight snapshots, RTT updates, frame actions and early-return behavior. A disabled rich-event sink can be installed only through private constructor/test plumbing; it receives complete value records and generation/ordinal identity. Default constructors retain the legacy path. No sampler, BBR formulas or public selector is added.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** None.

**Single owner after merge:** The connection-owned sentPacketHandler owns protocol facts and logical event construction; its legacy adapter translates the existing Reno notifications without a second recovery state machine.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** One private rich-dispatch branch is inert for ordinary callers. Its test-only activation boundary remains until B6 wires complete public selection; it is not a second mutable controller. No old callbacks are removed while Reno depends on them.

**Blast radius:** ACK/frame ordering, three packet-number spaces, timer-only losses, pooled packet/scratch lifetimes and Reno diagnostics. No new socket, public API, packet format or dependency. All effects are confined to event capture/dispatch. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Already decoded wire.AckFrame and transport-owned packet/loss facts; internal/wire remains the protocol parser. Rich values are private, synchronously borrowed and contain no retained frame/buffer pointers. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| ACK plus loss/CE and frame callbacks | Preserve legacy ordering; rich sink receives one complete event | sentPacketHandler dispatch | ordered callback characterization | Covered by `TestCongestionDispatchPreservesRenoOrder and TestCongestionEventPriorAndPostFlight` |
| Timer-only loss | No fabricated ACK/round progress | sentPacketHandler dispatch | loss alarm fixture | Covered by `TestCongestionEventTimerOnlyLoss` |
| Cross-space/equal-time identities | Distinct ordinals, unchanged transport identity | sentPacketHandler registration | identity fixture | Covered by `TestCongestionEventSpaceAndOrdinal` |
| Disposed/pooled records | No retained borrowed ownership | event capture boundary | pool reuse fixture | Covered by `TestCongestionEventBorrowLifetime and TestCongestionLegacyLateOnlyAck` |

**Evidence budget:** At most 6 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestCongestionDispatchPreservesRenoOrder`; `TestCongestionEventPriorAndPostFlight`; `TestCongestionEventTimerOnlyLoss`; `TestCongestionEventSpaceAndOrdinal`; `TestCongestionEventBorrowLifetime`; `TestCongestionLegacyLateOnlyAck`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “State ownership and private boundaries”, “Byte domains, identities and clocks”, “Event order, losses and retained evidence”; the source ranges and test inventory keyed `T1` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Separating schema from actual dispatch would create an unexercised horizontal interface; include real caller capture and legacy characterization together. Adjacent merge: Merging sampling would enlarge the first change from behavior-preserving event capture to lifetime/late-ACK semantics; keep that risk in T2. Dependency evidence: No blocker: current recovery already owns every required input.

**Stop conditions:** Stop if event capture requires moving frame/recovery mutation to a worker, reordering Reno, retaining pooled payload ownership or a public generic controller interface. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="t2"></a>

### T2 — Deliver bounded registration-time sampling through real recovery

**What it delivers:** Implement the optional BBR sidecar from packet registration through live and late-only ACK discovery, exactly-once delivery sampling, limitation/idle attribution, explicit retirement, expiry wakeups and cleanup. Cover real loss, PTO extraction, key discard, Retry, rejected 0-RTT, path reset and close in this slice. Excluded ACK-only/path/MTU sample classes retain their specified recovery semantics. The private rich path is exercised by real ackhandler and emission tests; default Reno allocates no sidecar.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** T1, T3.

**Single owner after merge:** Connection-owned sampler owns snapshots, delivery totals, sample generations and tombstone expiry; recovery remains sole flight/frame owner. Emission reports limitation reasons rather than mutating the sampler.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** T1 rich selection remains private until B6; the sampler is fully usable through that seam, not an authority that depends on later lifecycle fixes. No duplicate payload retention or worker timestamp model is introduced.

**Blast radius:** Recovery registration/retirement, late-only early return in rich mode, emission no-data reasons, idle wakeups, Retry and path reset. BBR-only allocation and bounded ACK processing cost; no default pacing or validator changes. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Typed current-generation packet records and decoded ACK ranges; rates are QUIC packet bytes over monotonic intervals, not application goodput. Fixed live ceiling 25,000 and 4,096 tombstones, expiration min(3 unbacked-off PTO,30s). Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Live/late first ACK, duplicate ACK | Count once with valid snapshot; never refund flight twice | sampler event consumer | identity and late-only tests | Covered by `TestDeliverySamplerPacketIdentity`, `TestDeliverySamplerCompressedAndLimitedAck`, `TestDeliverySamplerLateOnlyAck`, `TestDeliverySamplerRawRTT` |
| Loss versus PTO/space/Retry/rejection/close | Retain or dispose with the declared reason, no fabricated delivery | sampler retirement operation | table across materially distinct reasons | Covered by `TestDeliverySamplerPTOAndLossRetirement`, `TestDeliverySamplerSpaceRetryAndPathDisposal`, `TestDeliverySamplerRenoHasNoSidecar` |
| Expiry/eviction and idle timer | Bound memory and reject unavailable evidence | sampler expiry owner | expiry/pressure fixture | Covered by `TestDeliverySamplerIdleExpiry`, `TestDeliverySamplerBoundedEviction`, connection-loop subcase in `TestDeliverySamplerNoDataReasons` |
| Path/sample generation and equal PN | Reject stale model update | sampler generation guard | one optional guard bypass must fail stale fixture | Covered by `TestDeliverySamplerSpaceRetryAndPathDisposal`; optional mutation not used |
| Application/flow control versus queue/cwnd/pacer stops | Preserve send-time limitation and genuine idle definition | emission reason + sampler marker | reason table | Covered by `TestDeliverySamplerNoDataReasons`, `TestDeliverySamplerCompressedAndLimitedAck`, `TestDeliverySamplerIdleExpiry` |

**Evidence budget:** At most 10 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestDeliverySamplerPacketIdentity`; `TestDeliverySamplerCompressedAndLimitedAck`; `TestDeliverySamplerLateOnlyAck`; `TestDeliverySamplerPTOAndLossRetirement`; `TestDeliverySamplerSpaceRetryAndPathDisposal`; `TestDeliverySamplerIdleExpiry`; `TestDeliverySamplerBoundedEviction`; `TestDeliverySamplerNoDataReasons`; `TestDeliverySamplerRawRTT`; `TestDeliverySamplerRenoHasNoSidecar`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Byte domains, identities and clocks”, “Delivery sampling and limitation”, “Event order, losses and retained evidence”, “Lifecycle and migration”; the source ranges and test inventory keyed `T2` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Keeping tombstones without late-only discovery or complete disposal would leave an unreachable feature or leak; those transitions form one ownership unit. Adjacent merge: Merging controller phases would mix sample correctness with algorithm dynamics and exceed a bounded diagnosis context. Dependency evidence: T1 supplies the sole logical event/ordinal interface; sampling must not build a parallel ACK walker. T3 supplies zero-pending-work authority for sampling origins and genuine idle; flight or an empty queue cannot substitute for worker-owned debt.

**Stop conditions:** Stop if packet history must become unbounded, loss-adjusted flight is reused as unresolved evidence, or any disposal source cannot reach the one retirement owner. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="t3"></a>

### T3 — Bound paced local sends across the complete worker lifetime

**What it delivers:** Add private policy-specific pacing/quantum and pending-byte credit through real ordinary/GSO/coalesced emission, asynchronous queue/worker groups, accepted-prefix fallback, stopped-send disposal, MTU probes, close and migration debt. The BBR policy uses the selected Q/2Q contract; legacy Reno retains its exact rate/burst behavior. Test activation is private; no public BBR selector exists.

**Existing-work disposition:** Implemented in [product PR #604](https://github.com/the-sarge/quic-go-fast/pull/604), retaining its implementation through the scoped control-admission re-audit. The [linked audit receipt](../audits/2026-09-24-bbrv3-handoff/t3-control-admission.md) owns trigger history and review receipts.

**Blocked by:** None.

**Single owner after merge:** Connection/emission owns admission and BBR pacing; one synchronized reservation/completion ledger owns local byte credit. Worker completion returns credit without changing recovery/model facts.

**Central control-opportunity invariant:** A refused noncontrol reservation must not suppress an eligible bounded ACK or its ACK/PTO deadlines while control credit remains. One synchronized ledger predicate governs reservation admissibility and rearming for ordinary, control and isolated-probe requests; emission uses one control-opportunity disposition for both ordinary refusal and failed acquisition of probe isolation. Rearm the original request only when that same predicate can admit it, rather than turning release of an unused ACK reservation into an immediate self-wakeup. Worker completions remain lossless when wakeups coalesce. A physically full queue, insufficient control credit, or an already-held isolated reservation remains hard-blocked. The connection consumes that disposition without converting every byte-credit wait into a hard block. Probe intent remains untouched until admission, and ordinary old-generation debt remains blocking. Isolation requires total tracked pending bytes, including admitted control bytes, to reach zero. While a due probe awaits isolation, ordinary admission stays suspended so pending work can drain; eligible bounded control continues. Rearm when the shared predicate admits isolation, without promising time-bounded completion amid worker or control activity.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** One private send-policy alternative is selected only by tests until B1 internal controller and B6 public activation. It owns complete buffer lifetime from the first slice; later slices do not repair credit cleanup.

**Blast radius:** Producer/worker concurrency, batching, GSO fallback, unknown progress, queue shutdown, old-generation debt, sub-millisecond BBR deadlines, ACK/loss timer selection during local waits and memory pressure. No raw-socket authority, API callback or native offload capability is added. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Factory-produced queue entries and existing definite-prefix/unknown-progress send results. Credit counts UDP payload bytes across reserved/queued/in-service storage; it makes no wire-departure guarantee. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Reserved/queued/dequeued/in-service | Charge one reservation until actual local completion | credit ledger | stall plus concurrent drain/refill | Covered by `TestBBRPendingCreditWorkerOwned`, `TestBBRPendingCreditConcurrentDrainRefill` |
| Accepted prefix/rejected suffix/unknown progress | Return exact credit; never resend accepted or uncertain bytes | queue completion boundary | failure table | Covered by `TestBBRPendingCreditPartialAndUnknownProgress` |
| Stopped enqueue/close/fatal cleanup | Return credit once and release owned buffer | queue completion boundary | shutdown race fixture | Covered by `TestBBRPendingCreditStoppedAndClose` |
| Rate decrease/path change | Retain committed excess as debt | admission owner | debt fixture | Covered by `TestBBRPendingCreditRateDecrease`, `TestBBRPendingCreditMigrationDebt` |
| GSO/coalescing/MTU probe | Charge physical payload and bounded exception | emission admission | real-packer fixture | Covered by `TestBBRPendingCreditWorkerOwned`, `TestBBRPendingCreditGSOFallback`, `TestBBRPendingCreditMTUException` |
| Ordinary refusal with old-generation debt or current-generation byte pressure, while control credit remains | Try bounded ACK admission; preserve ACK/PTO deadlines and completion wakeup; keep ordinary data blocked | shared ledger admissibility/rearm predicate and emission control-opportunity disposition | `TestBBRPendingCreditMigrationDebt`, including delayed ACK through the connection loop and current-generation GSO refusal when one control packet still fits | Covered by `TestBBRPendingCreditMigrationDebt` |
| Due probe cannot acquire isolation because total tracked bytes (ordinary or control) remain pending | Try bounded ACK admission; preserve ACK/PTO deadlines and unconsumed probe intent; rearm only when isolation becomes admissible | same shared ledger predicate and emission control-opportunity disposition | `TestBBRPendingCreditMTUException`: due ACK, no-ACK wait without self-wakeup, and admission after pending bytes complete | Covered by `TestBBRPendingCreditMTUException` |
| Control credit exhausted, queue full, or isolated reservation already held | Hard-block until actual capacity changes; never bypass an active isolated probe | shared credit predicate plus existing queue capacity guard | existing queue subcases plus emission-level hard-block observations for active isolation and less than one control packet of credit in the named MTU/migration tests; no new test function | Covered by `TestEmissionResultQueueWakeup`, `TestBBRPendingCreditMTUException` |

**Evidence budget:** At most 10 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. The initial and replacement product reviews are consumed. After this scoped re-audit is published, verify the replacement review’s accepted finding against the corrected exact head; no further fresh product review is authorized. Final certification reruns after candidate changes are required checks, not a statistical repetition campaign. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRPacingQuantumAndDeadline`; `TestBBRPendingCreditWorkerOwned`; `TestBBRPendingCreditConcurrentDrainRefill`; `TestBBRPendingCreditPartialAndUnknownProgress`; `TestBBRPendingCreditStoppedAndClose`; `TestBBRPendingCreditGSOFallback`; `TestBBRPendingCreditRateDecrease`; `TestBBRPendingCreditMTUException`; `TestBBRPendingCreditMigrationDebt`; `TestLegacyRenoPacingPreserved`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “State ownership and private boundaries”, “Pacing and pending local work”, “Lifecycle and migration”; the source ranges and test inventory keyed `T3` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; only the unresolved control-admission receipt linked above (within the 3,000-token history allowance). Add the current `packet_emission_bbr.go` reservation/refusal helpers, `local_send_credit.go`, the connection’s wait/timer consumer, and the two named MTU/migration test functions in retained PR #604 to that inventory; these supplement rather than replace its inputs. Measure the complete resolved manifest before resuming product work; omit resolved review reports. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Splitting admission from completion would make credit authoritative before its destructive consumers exist; keep full lifetime in one PR. Adjacent merge: Merging event capture would touch unrelated recovery ownership and block an otherwise independent front. Dependency evidence: No blocker: existing emission and queue already have the required owned buffer boundaries and pathGeneration.

**Stop conditions:** If verification finds another in-contract counterexample escaping the accepted shared reservation/control-opportunity predicate, stop for an operator decision rather than adding another local refusal patch or expanding this evidence budget. Stop if completion needs per-packet protocol mutation on the worker, accepted-prefix semantics change, or native capability/packet authority expands. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="t4"></a>

### T4 — Validate bounded actual ECN marking through path changes

**What it delivers:** Implement BBR-only ECN result dispatch and the actual-codepoint/ordinal ledger, eligible advancing late-only feedback, bounded failure to Not-ECT and the migration counter fence/revalidation path. Keep cumulative counts continuous and distinguish validation from congestion policy. Cover ordinary marked packets, path-probe and coalescing holes, skipped numbers, ledger splits and loss/disposal before late ACK. Legacy Reno keeps its original validator and early-return order.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** T1, T3.

**Single owner after merge:** The connection-owned BBR ECN ledger/validator is the sole owner of sent marking authority, accepted counters and validation epoch. Worker metadata never grants ECN policy authority.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** Legacy and BBR validator policies coexist intentionally for different selected algorithms, not as competing owners of one connection. Private algorithm selection remains inaccessible to callers until B6.

**Blast radius:** Actual emission marking, ACK validation, cumulative cross-path counters, client queue replacement/server rebind, anti-amplification and metadata capability gating. No public ECN fields, new platform support or wire encoding. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Decoded ACK_ECN packet counters from internal/wire plus actual registered sent codepoints. 4,096 range-record budget; range compression requires codepoint/generation/ACK-status equality and affine ordinal mapping. Missing knowledge fails marking closed. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Ordinary ECT versus Not-ECT/path/coalesced holes/skips | Validate actual sent marks, never infer from epoch membership | ECN marking ledger | hole-class table | Required before slice completion |
| Advancing late-only/reordered/duplicate feedback | Consume eligible deltas once; defer nonadvancing counts | BBR ECN validator | late-only plus reorder fixture | Required before slice completion |
| Invalid counters/budget overflow | Not-ECT fallback without fabricated clean evidence | BBR ECN validator | negative and split-budget fixture | Required before slice completion |
| Testing versus established all-CE | Validation test semantics versus usable congestion | BBR ECN validator | state fixture | Required before slice completion |
| Counter fence present/old loss/new unavailable path | Revalidate only with full accounting; otherwise continue Not-ECT | ECN path-transition owner | migration table | Required before slice completion |

**Evidence budget:** At most 10 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRECNActualMarkingHoles`; `TestBBRECNCoalescedAndPathProbeMarking`; `TestBBRECNAdvancingLateOnlyFeedback`; `TestBBRECNReorderedAndInvalidCounters`; `TestBBRECNRangeBudgetFallback`; `TestBBRECNTestingVersusCapableCE`; `TestBBRECNMigrationCounterFence`; `TestBBRECNMissingOldMarkedPacket`; `TestBBRECNRepeatedMigration`; `TestLegacyECNDispatchPreserved`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Classic ECN response”, “Lifecycle and migration”; the source ranges and test inventory keyed `T4` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Splitting marking history from validation would leave late feedback dependent on the invalid interval inference; migration likewise cannot inherit capability without the counter owner. Adjacent merge: Merging the CE controller response would combine protocol validation with model tuning; the typed result seam makes these independently verifiable. Dependency evidence: T1 supplies the BBR-specific logical ACK path without modifying Reno. T2 is not a blocker: marking evidence is explicitly independent of sampler retention. T3 supplies the complete old-generation local-debt evidence needed before ECN revalidation.

**Stop conditions:** Stop if a range-only inference replaces actual marking, a mixed-path delta is attributed to its receiving address, or the 4,096-record budget cannot fail safely. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="t5"></a>

### T5 — Emit bounded persistent-congestion and recovery evidence

**What it delivers:** Add the ordered send-outcome ledger, cross-space ACK-confirmed persistent-congestion detection, response deduplication and exact loss-episode membership/undo eligibility to the rich event seam. Inputs cover ACK-only receipts, excluded/disposed gaps, real losses, late ACKs and missing tombstones. Emit value events only; this slice does not change Reno or select BBR actions.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** T2.

**Single owner after merge:** Recovery owns loss-episode membership and the 32,768-entry outcome ledger; sampler supplies retained delivery facts without determining transport loss.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** Rich events remain private until B6; no new recovery action is exposed before B5 composes it with the complete model. Detection itself is complete and tested through real recovery.

**Blast radius:** Loss timing, packet spaces, ACK-only breaks, missing history, Retry/path resets, episode expiry and non-growing memory. No timeout algorithm change or TCP-RTO-on-PTO behavior. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** Its private/runtime code is shipped behavior; the listed central guards are required safety enforcement; proposed tests/traces are verification aids; certification and pointers are process metadata. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Typed sent outcomes and already validated ACK/loss facts; RFC 9002 duration uses measured-at-send evidence, threshold three and conservative gap handling. Guarantee concerns emitted evidence, not inferred losses in unobserved history. Guarantee level: universal enforcement of the stated typed-domain invariants by the named owner, supported by the finite semantic cases below; no exhaustive protocol/performance proof.

**Contract closure:** Triggered by material lifecycle/compatibility/congestion-safety consequences plus the independently reachable classes below. The precise invariant is the slice delivery and single-owner obligation, not a claim of parser or benchmark completeness.

| Semantic class | Disposition | Central enforcement owner | Terminating evidence | Status |
| --- | --- | --- | --- | --- |
| Measured lost endpoints with complete intervening coverage | Emit one proved persistent event | outcome ledger reducer | positive cross-space trace | Required before slice completion |
| ACKed/unresolved/disposed/evicted gap | Break candidate proof | outcome ledger reducer | gap table | Required before slice completion |
| Repeated report/PTO without ACK | No repeated reset evidence or RTO inference | event deduplication | timer/repeated fixture | Required before slice completion |
| All-spurious versus mixed/superseded/evicted episode | Undo eligible only for exact complete latest episode | episode owner | membership fixture | Required before slice completion |

**Evidence budget:** At most 8 new focused table-driven test functions; one representative positive and one materially distinct negative per listed behavior. One focused race run for changed concurrent/connection seams; no statistical repetition, new platform cross-product or native benchmark in this implementation slice. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `TestBBRPersistentCongestionAcrossSpaces`; `TestBBRPersistentCongestionMeasuredAtSend`; `TestBBRPersistentCongestionAckOnlyBreak`; `TestBBRPersistentCongestionGapAndEviction`; `TestBBRPersistentCongestionDeduplication`; `TestBBRRecoveryEpisodeAllSpurious`; `TestBBRRecoveryEpisodeSupersededOrMissing`; `TestBBRPTODoesNotFabricateLoss`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Event order, losses and retained evidence”, “Lifecycle and migration”; the source ranges and test inventory keyed `T5` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Detection and its coverage/retention must land together; emitting authority before gap and deduplication handling would be unsafe. Adjacent merge: Merging model restart/undo would entangle transport evidence with policy state; B5 consumes these already checked facts. Dependency evidence: T2 provides bounded late-ACK discovery and reasoned retirement; without it the all-spurious and gap semantics would be invented twice.

**Stop conditions:** Stop if PTO count substitutes for persistent-congestion duration, unknown history becomes proof, or a detector needs unbounded per-packet retention. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

## Acceptance criteria

- Each listed slice delivers its stated behavior through the named real seam and is independently green without an unmerged successor.
- The stated owner enforces the declared typed-domain invariant, and all listed materially distinct semantic classes have the required disposition/evidence within the finite budget.
- Every retained temporary seam has the stated coherent intermediate contract and named removal slice; no public caller can select incomplete BBR.
- Existing Reno/API/wire/ownership/platform behavior remains unchanged except the explicitly accepted opt-in behavior and Config literal compatibility exception.
- No accepted requirement depends on an unapproved maintained verification tool, unknown host, invented measurement or untraced authority.

These criteria range only over the per-slice supported domain, named parser/representation owners and guarantee level. They do not claim universal measured performance. Their terminating evidence is the slice table and focused gates, not an open-ended search for counterexamples.

## Validation gates

Before commit, run scoped new/affected tests and affected-package `go test`, plus `go vet` for changed Go packages and `go mod tidy -diff` for Go/dependency changes. T2/T3/T4 and B6 require one focused `go test -race` over their named concurrency/lifetime cases; other slices use race only when they modify a concurrent seam, as identified in the implementation preflight. Pin exact test regexes/packages in that preflight from the proposed names; do not broaden into unbounded stress. Existing hosted unit, integration, lint and cross-compilation jobs run for drafts and must be successful at the exact merged head. Q1/Q2 additionally use the accepted campaign's finite native/operational gates and ledger. Docs-only certification uses the repository overlay's clean exact-head diff/link/graph checks and independent contract review.
