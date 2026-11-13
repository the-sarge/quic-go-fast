# BBR and aggregation implementation plan

**Status:** accepted design and audited slices; 4/8 complete. [B1 #1040](#b1) partitioned BBR delivery sampling, pacing and local credit into private per-path contexts; [B2 #1041](#b2) gave those contexts their own ECN validation, migration fence and bounded recovery evidence. [B3 #1042](#b3) connected those contexts to private primary/backup operation through the complete path lifecycle; its [applicability record](../audits/2026-11-13-multipath-b3-applicability.md) carries the Reno and native evidence forward. [B4 #1043](#b4) added the private aggregate scheduling policy and its fixture-only byte-round-robin comparison arm. By [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1044#issuecomment-6859584262), [B7](#b7) delivers the receiver's same-path feedback preference. Its prefactor [B8](#b8), which bounds completed ACK-only sent history, is the frontier. B7 waits on B8, and [B5 #1044](#b5) waits on B7. Dispatch requires the verified default-branch publication and a frontier child issue. **Program:** `QGF-MP646-20261024`. **Track:** B, 3 of 3. **Date:** 2026-10-24. **Normative scope:** current outcomes, boundaries, invariants, evidence, blockers and stops. **Audit history:** [final consideration response](../audits/2026-10-24-multipath-646-final-consideration-response.md) and [slice audit](../audits/2026-10-24-multipath-646-slice-audit.md). **Parent issue:** pending.

## Binding contract and operating discipline

This is a current contract, not dispatch permission. Implementation occurs only when the operator dispatches a verified frontier child through `$implement-architecture-slice`. Follow that skill's shared review-loop and contract-closure baselines, composed with [the repository execution overlay](../REVIEW-LOOP.md). No repository closure overlay exists. Use the shared design-cost reference for scoped design choices. One fully briefed fresh review and at most one replacement govern each implementation PR; independently disposition findings and stop on accepted representation/ownership/boundary failures. Do not run an automatic fixer. Exact pushed-head local certification, applicable same-head hosted checks, squash merge, then append-dev-journal and tracking reconciliation are required. Diagnose failures before reruns.

The native endpoint ownership domain includes every listed ordinary QUIC v1/v2, negotiated multipath, control/stateless and public non-QUIC write on an exact granted native Linux amd64 UDP socket. The narrower negotiated multipath wire domain is opt-in draft-21 QUIC v1 with two live/validating paths. Existing parser/AEAD/configuration owners validate external representations; internal event constructors carry immutable identities. The runtime ownership and authority rules are universal within that domain. The evidence below is finite regression/qualification evidence, not a completeness proof. Unknown representations, unqualified platforms and excluded configurations cannot acquire support by passing an example.

Preserve [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0004](0004-packet-emission-ownership.md), accepted BBR/ECN decisions, default Reno spurious-loss undo and adaptive-loss contracts. Do not introduce custom I/O providers/wrappers, restricted/network-admitted/candidate-group or send-thread-QoS combinations, multipath v2, FEC, duplicate DATAGRAM delivery, public scheduling callbacks, arbitrary server socket association, coupled congestion control or address discovery. Ordinary unregistered endpoints preserve their existing contracts. Local queue/native timeouts remain nonfatal backpressure for ordinary and multipath peers on granted endpoints. A qualified-native contract violation faults/quarantines the endpoint without freeing a live borrow.

**Existing-work disposition:** new runtime slices; no unmerged implementation is accepted as a dependency. Retain exploration/sizing as history; the linked slice audit records disposition of prior documents. Q2 #599 remains an external unfinished qualification slice; no result, exception or execution readiness transfers to this program, and it is not a prerequisite of any B slice.

**Evidence discipline:** write the named failing or characterization cases first. For runtime slices, at most 16 new deterministic scenario tests (R2/R16 at most 20) cover the listed semantic families, one positive per behavior and one negative per distinct supported failure owner. Run affected-package tests and vet; Go/source or dependency changes also require `go mod tidy -diff`. One focused race run is budgeted where the slice crosses execution contexts; no repeated timing stress is implied. At most one discriminating central-guard mutation per owner is allowed only if needed to prove inherited coverage reaches that guard. No recursive fixture-completeness obligation, syntax-variant matrix, new platform or additional operational campaign enters through review. The exact operational inventory/ceiling is below. If the accepted family cannot fit, stop and re-audit rather than silently grow the slice.

**Authority and preservation:** there is no persisted schema or restart round trip in this library program; each new runtime fact must cover construction, validation, all listed destructive/security consumers, cancellation/close and fresh-instance reconstruction in the same slice. A private activation gate is a transitional seam, not permission to negotiate incomplete protocol behavior with arbitrary peers. Intermediate fixtures may activate only their declared complete scenario; ordinary public construction remains unchanged. Do not let a prefactor change controller meaning or ordering. Never widen a temporary seam; only its named removal slice may delete it after every migration dependency is complete.

**Artifact classification:** runtime mechanisms are shipped behavior; native authority, CID/AEAD, generation, resource and disposal guards are required safety enforcement. Unit/integration tests, traces and bounded fixtures are verification aids. The regression coverage named by these accepted safety contracts and the pinned required peer job are maintained blocking deliverables: payoff is catching ownership/wire regressions, domain is the declared profile and semantic cases, owner is the transport test suite, and retirement requires equivalent replacement or feature removal. Qualification receipts, plans, journals and issue/task state are process metadata. No extra analyzer/harness becomes blocking by implication.

**Contained-design comparison:** changing the owner to do synchronous native I/O would remove a handoff but violate the accepted responsive connection loop. Requiring all PacketConn implementations to cancel would change a closed compatibility contract. Retain one endpoint control/writer boundary, with connection-owned policy and fact-only completion. Do not duplicate protocol decisions in route adapters. Revisit only if those public/responsiveness contracts are explicitly changed; all other rejected slice split/merge alternatives below have `none: rejected on merit` unless their stated context or contract stop is reached.

## Goal and current shape

Reuse the complete Reno multipath identity/lifecycle seam for BBR, then evaluate one private aggregation policy. Full single-path Q2 #599 is deferred and, by owner decision on 2026-11-12, is not a prerequisite of any B slice. Single-path BBRv3 already ships experimental, opt-in and unqualified, and the B-track correctness, native, ordinary-cost and comparative evidence below qualifies only the multipath candidate. B6 states that the underlying BBRv3 controller remains unqualified while #599 is deferred. Reno stays default.

Verified source seams: `internal/ackhandler/congestion_dispatch.go` couples delivery sampling to feedback; `internal/congestion/feedback.go:70` permits invalid delivery samples and `:138`/`:150` distinguish RTT evidence; retained/recovery indexes and `bbr_ecn.go` own controller history and CE accounting. They currently share one active path context. The path registry and endpoint contracts from the preceding tracks supply identity and lifetime, not BBR model policy.

## BBR and scheduling contract

BBR integration follows the complete Reno-first ownership model and the existing BBR controller contracts; BBR multipath support is gated by this track's own evidence, not by Q2 #599. Each path owns its controller, sampler, pacing, loss and ECN state; shared application delivery remains connection-owned. A tuple migration resets the appropriate RTT/controller/PMTU context without resetting its wire PN sequence or cumulative ECN accounting. Preserve old-generation evidence and the accepted ECN accounting fence before revalidation. No aggregate-controller shim or silent Reno substitution is permitted.

Only same-send/return-path ACK evidence trains a path's propagation-RTT and delivery-rate model initially. A cross-path ACK still retires packet flight/logical obligations and updates loss/recovery facts, but does not create a new bandwidth or min-RTT sample. Keep conservative PTO timing from the last valid path-local RTT or the standard initial RTT while that evidence is absent; request/prefer same-path feedback without requiring peer compliance. [B7](#b7) delivers the preference at this fork's receiver; draft-21 defines no frame that requests it of a peer. A controller can therefore remain supply/evidence limited on a peer using only cross-path ACKs. Cross-path-only feedback gets a deterministic correctness regression, not a throughput-support claim in the accepted campaign. Count delivered bytes once independently of sampling eligibility; invalidate rate intervals that bridge excluded cross-path feedback and require a fresh path-local anchor before the next valid rate sample. Never let an excluded ACK re-enter a later bandwidth sample through cumulative delivered counters.

Use one fixed private `aggregate` policy after BBR integration, implemented internally for Reno or BBR alongside `primary-backup`, with public aggregate support gated specifically for BBR; do not publish a scheduler interface. Among validated active paths with credit, pacing, PMTU and authority, estimate arrival as next eligible send time plus queued eligible bytes divided by the path's conservative delivery-rate estimate plus half its current valid path-local RTT. With no valid rate sample, use paced byte-round-robin among eligible paths; ties use wire ID. Backup paths stay outside aggregation while an active eligible path exists. Selection precedes destructive packing; the same-path retransmission preference and per-message DATAGRAM eligibility remain enforced. The comparison policy is byte-round-robin, confined to the evidence fixture, not another maintained public option.

This is a bounded experiment choice, not a claim that earliest estimated arrival wins. If the accepted comparison fails, retain BBR primary/backup and keep `aggregate` unsupported; do not silently swap algorithms under the same public policy name. Reopening the policy requires a scoped design decision. Coupled congestion control and exact application-symbol steering remain outside the program.

The rate-estimate owner is the existing path-local BBR delivery sampler; same-path eligible samples alone provide estimates. Reno's default controller has no delivery sampler (`internal/ackhandler/sent_packet_handler.go:140,182`), so Reno aggregation intentionally always takes the paced byte-round-robin no-sample fallback. Do not add a Reno estimator, enable BBR-only sampling stores for Reno, or fabricate rates from ACK spacing. This is internal behavior of the one aggregate policy, not another public scheduler. Reno aggregation is retained only as an informational fixture arm and remains unsupported in public configuration under this program. BBR aggregation is the only aggregation combination the selected comparative campaign can qualify.

## Qualification and adoption policy

The owner accepted the following policy for the eventual program; acceptance does not dispatch implementation or experiments in this session:

1. Allow an exploratory implementation program before consumer benefit has been demonstrated. Public support remains opt-in and experimental until correctness, independent interop, native completion and ordinary-peer cost gates pass. Recommending adoption additionally requires the comparative benefit evidence; no campaign or implementation is launched by accepting this document.
2. On a matched ordinary-only unregistered versus opted-in comparison, require at least 95% of baseline useful goodput, no more than 110% of baseline CPU per useful byte, and p99 control/send-completion latency no worse than the larger of 110% of baseline or baseline plus 1 ms. Also require no timeout-only connection closes, bounded memory and the declared fair progress. These are accepted planning tolerances, not measurements or pre-existing SLOs.
3. For aggregation, require at least a 20% useful-goodput gain over the better single path on the balanced disjoint-path cell, at least 95% of two matched independent connections' useful goodput there, and no more than a 10% reduction in a competing ordinary Reno flow's useful goodput relative to the two-independent-connection comparison on the shared-bottleneck cell. Report asymmetric STREAM completion and DATAGRAM useful delivery separately; a failed cell blocks the corresponding support claim, not an automatic expansion of experiments. No claim of fairness equivalent to one congestion-controlled flow is made for uncoupled multipath.

Changing these tolerances, benefit timing or the operational ceiling requires an explicit owner decision. Failed evidence does not silently relax them.

## Contract closure: BBR feedback

Material controller/ownership corruption and independent ACK/loss/migration/retirement paths trigger closure. The invariant is that each transmission updates only its own path/generation model while logical delivery counts once. B1–B3 own the supported feedback domain; existing frame parser owns syntax. The guarantee is universal ownership within that domain with finite representative tests, not throughput assurance for arbitrary path conditions.

| Semantic class | Owner and expected disposition | Finite evidence |
| --- | --- | --- |
| Same-path versus cross-carrier ACK | Path sampler trains only eligible intervals; delivered totals settle once | Same PN/two paths, excluded cross-path interval, fresh anchor |
| Old generation and CE after tuple change | Retained recovery and ECN owner preserve cumulative accounting/fence | Late ACK/CE and revalidation |
| Loss, abandon and local completion | Recovery settles each flight once; local timeouts remain backpressure | Loss then ACK, retained abandonment, local uncertainty |
| Shared store saturation | Connection aggregate budget grants bounded per-path use | Two-path exhaustion without lost ownership evidence |
| Aggregate scheduling | Scheduler chooses immutable eligible context before packing | Rate fallback, PMTU/credit/pacing and stable ties |
| Receiver ACK carriage | Emission carries each due acknowledgment on its own path while that path is home; a non-home or refused target falls back to any carrier in the same call under today's rules | [B7](#b7) carriage matrix |
| Completed ACK-only sent history | The path's recovery owner retires completed non-ack-eliciting records beyond `MaxNonAckElicitingAcks` while the path has no ack-eliciting record | [B8](#b8) retirement matrix |

The fixture-only byte-round-robin comparison is a maintained verification aid solely for the accepted aggregation comparison: payoff is discriminating scheduler benefit, domain is the three fixed topologies/six modes, owner is the transport integration suite, retirement is completion of this one qualification decision or feature removal. It is not a public option or a perpetual tuning program. No cross-path-only-feedback throughput claim is included.

## Program budget and prior-evidence validity

B5 uses 54 of the shared 118 regular run instances. The [Reno program ledger](2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing) owns the cumulative 210-run/eight-host-hour ceiling and execution preflight. Inspect B1–B4, B7 and B8 changes against prior native/ordinary evidence. Unchanged owners/domain may retain receipts with a source-based applicability record; changed behavior needs the affected existing cases rerun within the same ceiling. Do not automatically replay an entire matrix or inherit a stale result. If more scope/budget is necessary, stop for a decision.

### Controller-matched aggregation arms

The fixed aggregation inventory is six arms × three topologies × three observations = 54 run instances. Workload, socket count, offered load, path shaping and competing traffic are matched as applicable. All ordinary single/pair baseline arms use BBR so the aggregation support comparison isolates the selected BBR multipath policy.

| Arm | Controller / policy | Claim |
| --- | --- | --- |
| A | One ordinary BBR connection on path A | Single-path baseline. |
| B | One ordinary BBR connection on path B | Other single-path baseline; choose the better measured A/B result for the threshold. |
| C | Two independent ordinary BBR connections, one per path | Independent-connections baseline. |
| D | Reno multipath aggregate, intentional paced byte-round-robin fallback | Informational only; cannot qualify public Reno aggregation or its adoption. |
| E | BBR multipath aggregate, selected arrival estimate with fallback | Candidate for the accepted aggregation thresholds. |
| F | BBR multipath, fixture-forced byte-round-robin | Scheduler comparison only; not a public policy. |

Balanced disjoint-path thresholds compare E with max(A,B) and C. Shared-bottleneck harm compares the same ordinary Reno competitor's useful goodput alongside E versus alongside C. Unequal-delay/rate STREAM completion and DATAGRAM useful delivery are reported separately with the declared semantics; no new numeric threshold is invented. Valid negative comparative evidence completes B5 and leaves aggregation unsupported; B6 can publish qualified BBR primary/backup. Missing/invalid prerequisites stop qualification. Passing these BBR comparisons does not qualify Reno aggregation. Supporting that combination later requires a separately accepted controller-matched campaign; this handoff does not add it.

### Candidate and run ledger

| Delivery | Candidate / scope | Planned charge |
| --- | --- | ---: |
| E6 | Endpoint candidate, native request-class boundary; applicability checked against the later Reno candidate | 12 native instances |
| R17 | Complete Reno primary/backup candidate, both independent-peer roles | 16 interop instances |
| R18 | Same Reno candidate, two endpoint profiles × six ordinary cells × three observations | 36 ordinary observations |
| B5 | Complete BBR/aggregation candidate, six controller-matched arms × three topologies × three observations | 54 aggregation instances |
| R1 and R14 | One ordinary control smoke per role in R1; one two-path STREAM smoke per role after complete close handling in R14 | 4 setup/smoke instances |
| B5 affected revalidation | BBR primary/backup: useful-transfer, abandon/reopen and same-ID migration interop scenarios, each in both roles | 6 existing-cell instances |
| B5 ordinary revalidation | BBR primary/backup background in mixed ordinary/multipath bulk and ordinary-under-stateless-pressure cells, two endpoint profiles × three observations | 12 existing-cell observations |
| Remaining reserve | Cause-recorded failed attempts, affected existing native/ordinary cells or necessary repeats; never new semantic cells or pass-seeking retries | About 18 after B5's scheduled instances; the live remainder on #646 binds |

The regular inventory remains 118; the planned four early smokes plus 18 BBR revalidation observations make 140 scheduled instances. The live ledger sets the reserve. The [last capped accounting on #646](https://github.com/the-sarge/quic-go-fast/issues/646#issuecomment-6650984913) charged 120 instances. Under the cumulative 210-run/eight-host-hour ceiling, raised from 180 by [owner decision](https://github.com/the-sarge/quic-go-fast/issues/646#issuecomment-6859592089) and recorded in the [Reno program ledger](2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing), that leaves 90. After B5's 72 scheduled instances (54 aggregation plus 18 revalidation), about 18 remain. The live remainder recorded on #646 binds first. A "run instance" is one candidate/arm observation; paired ordinary profiles consume two, and a control warmup within an observation is included in its duration, not hidden extra capacity. Charge host-hours as summed occupied host time, including setup and failed attempts. Preflight must pin candidate/comparison SHAs, dependency/container/kernel/toolchain identities, workload seeds, remaining run/host-hour budget and per-invocation timeout; do not start an invocation whose maximum reserved cost exceeds either remainder.

The 18 listed repeats are required BBR-candidate evidence, not optional reuse of the Reno receipt. Revalidate any additionally affected existing native/ordinary cell within the remaining reserve with its concrete source-change reason; unchanged owners/domain may retain an earlier receipt only through an explicit applicability record against the current candidate. If changed semantics require more or different evidence than the fixed inventory/remaining budget, stop for a scoped decision. BBR primary/backup support needs its own complete correctness and the applicable/revalidated native, interop and ordinary-cost evidence; aggregate support additionally needs arm E's comparative pass. Q2 evidence never automatically transfers. No execution is authorized by this ledger.

## Slice graph

| Slice | Disposition | Delivery | Blocked by |
| --- | --- | --- | --- |
| [B1](#b1) | complete | Partition BBR delivery sampling and send credit | R19 |
| [B2](#b2) | complete | Preserve BBR ECN, recovery and migration evidence | B1 |
| [B3](#b3) | complete | Exercise BBR primary/backup through complete lifecycle | B2, [BBR window-policy fix #1329](https://github.com/the-sarge/quic-go-fast/issues/1329) |
| [B4](#b4) | complete | Add the private aggregate scheduling policy | B3 |
| [B5](#b5) | new | Measure aggregation benefit and shared-bottleneck cost | B7 |
| [B6](#b6) | new | Publish the supported BBR and aggregation outcome | B5 |
| [B7](#b7) | new | Prefer same-path ACK carriage at the multipath receiver | B4, B8 |
| [B8](#b8) | new | Bound completed ACK-only sent history | None |

## Implementation slices

<a id="b1"></a>

### B1 — Partition BBR delivery sampling and send credit

**Delivers:** Adapt per-path BBR sampler/pacing and local credit under the existing shared logical delivery owner and aggregate store ceilings. Cross-path ACKs settle delivery/flight without training path-local RTT/rate; exclude bridging intervals and re-anchor valid sampling. Keep public BBR+multipath disabled. Preserve E2 pending-local eligibility and immutable registration-time sampling provenance; exclude unresolved/uncertain timing intervals and re-anchor.

**Blocked by:** R19. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Each path controller owns its model; connection delivery owner counts useful logical bytes; sampler owns eligible intervals. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Private BBR contexts only until B6. No Reno substitution or aggregate shim.

**Blast radius and context:** internal/ackhandler/delivery_sampler.go, congestion_dispatch.go, delivery_records.go, delivery_sampling.go, local_send_credit.go, bbr_send_policy.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: same PN independent samples; cross-path-only ACK no sample; fresh local anchor after excluded interval; old generation feedback; credit exhaustion does not imply network loss; aggregate memory bound; pending local packet below ACK watermark; early ACK excluded from timing sample. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Endpoint plan Pending-local recovery eligibility; BBR and scheduling contract (feedback/credit); Contract closure: BBR feedback; Reno identity/resource and endpoint completion interface receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Sampling eligibility and consumers must change together. R19 supplies complete Reno identities and qualification baseline; no need to merge ECN/recovery algorithms. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Local evidence cannot remain path scoped without changing accepted BBR algorithm or multiplying aggregate capacities.

<a id="b2"></a>

### B2 — Preserve BBR ECN, recovery and migration evidence

**Delivers:** Integrate per-path BBR recovery/retained indexes and ECN validation while cumulative wire counters persist across same-ID migration. Enforce the accepted ECN counter fence before revalidation, bounded retained old-generation outcomes and exact-once logical/flight disposal.

**Blocked by:** B1. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Path recovery owns its records; per-path ECN validator owns marking attribution; connection owns shared capacity and logical delivery. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Private BBR contexts remain; no global-controller proxy or new counter epoch on wire.

**Blast radius and context:** internal/ackhandler/bbr_recovery.go, recovery_index.go, retained_index.go, bbr_ecn.go, ecn.go; path migration integration. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: cross-path ACK ECN target; old-generation CE after migration; loss then late ACK; abandonment while retained; two-path store exhaustion; unchanged single-path BBR qualification regressions. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** BBR and scheduling contract (ECN/migration); Contract closure: BBR feedback; accepted BBR/ECN ADRs. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** ECN fence and retained feedback must cover the same migration generation in one PR. B1 supplies sampler identity; split only if feedback interface must change outside this contract. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Counter continuity cannot be preserved or accepted BBR/ECN decisions must be reopened.

<a id="b3"></a>

### B3 — Exercise BBR primary/backup through complete lifecycle

**Delivers:** Connect BBR contexts to private primary/backup operation and run focused lifecycle preservation through real APIs: opening, failover, migration, retirement, local timeout and close. Record which Reno/native evidence remains applicable and what changed surfaces require requalification within the same ledger.

**Blocked by:** B2 and the owner-approved [BBR window-policy fix #1329](https://github.com/the-sarge/quic-go-fast/issues/1329). **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection policy stays controller-agnostic; BBR adapter owns controller construction and disposal. Emission selects the chosen route's BBR send policy (pacing, local credit, supply gap) at send time; it does not choose the route, and scheduling choice stays with [B4](#b4). Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Public BBR+multipath remains disabled until B6; no duplicate API or alternate timeout policy.

**Blast radius and context:** congestion_control.go, connection.go, internal/ackhandler construction; send-time selection of the route's BBR send policy (pacing, local credit, supply gap) in packet_emission_bbr.go, bbr_send_policy.go and the supply-gap and pending-byte consumers in delivery_sampling.go, plus their tests; the path admission and final-reclamation `addBBRPath`/`removeBBRPath` call sites in multipath_admission.go, plus their tests; the private multipath credit activation guard in multipath_credit.go, plus its tests; integrationtests/self. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Scope amendment:** by [owner decision on 2026-11-12](https://github.com/the-sarge/quic-go-fast/issues/1042#issuecomment-6844439624), send-time BBR send-policy selection, which B1 #1040 handed to this slice, joins its blast radius, including the supply-gap and pending-byte consumers emission calls at send time. By [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1042#issuecomment-6844725977), the multipath_admission.go admission and reclamation call sites join it too; they are controller construction and disposal, already owned by this slice's BBR adapter line. The existing network-suspect promotion scenario already covers sending on the backup after failover, so the context and evidence budgets are unchanged. Neither amendment changes B3's accepted outcome, representation or design. A scoped re-audit on 2026-11-13 adds one item to the blast radius: removing the BBR refusal from the private credit activation guard in multipath_credit.go, which today refuses every BBR connection, so private primary/backup cannot run. It adds no selector, flag or API. The public capability gate keeps refusing BBR multipath until [B6](#b6), and the budgets stay unchanged.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: two-path primary/backup useful transfer; failed/uncertain local write; network suspect promotion; same-ID migration; old ACK after retirement; single-path BBR and Reno preservation. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** BBR and scheduling contract; Program budget and prior-evidence validity; completed lifecycle/constructor receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Separate complete lifecycle integration from internal BBR stores to catch ownership gaps before aggregation. B2 provides required recovery/ECN contract. Do not merge scheduler experiment. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Requalification requires new semantic cells or a new budget rather than the accepted affected-case policy.

<a id="b4"></a>

### B4 — Add the private aggregate scheduling policy

**Delivers:** Implement deterministic estimated-arrival selection over eligible paths, conservative no-rate paced-byte-round-robin fallback, backup exclusion, stable wire-ID ties and same-path reliable retry preference. Add fixture-only byte-round-robin comparison; no public scheduler callback or payload classification. Only BBR sampling supplies a rate estimate; Reno deliberately uses the no-sample paced byte-round-robin fallback and remains fixture-only for aggregation.

**Blocked by:** B3. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Private scheduler owns choice only; emission retains packing/credit/registration and per-message eligibility. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Readiness and reservation agreement:** a path's view is ready only when the opportunity it would run is admitted locally. The BBR opportunity planner in `sendBounded` decides that opportunity's step and reservation from the route's recovery, projected pacing, MTU-probe due state and GSO capability, without spending or reserving. Execution runs that plan, and readiness consumes the same plan, evaluated at the same instant within an opportunity. The route's local credit must admit the plan's reservation: its limit, its demand (a GSO batch or a single packet) and its isolation (a due MTU probe). The native construction-capacity owner must also report capacity for the route, through a non-reserving read. Readiness may be conservative, for example toward a steered DATAGRAM's single packet, but never optimistic. Local completions only add room. The scheduler reads the resulting value only. The family is finite: credit limit after feedback, GSO-batch demand, isolated MTU probe and construction capacity. Admission probes precede selection. Announcements, [B7](#b7)'s ACK-only turn, the held opportunity and connection-level packet-size recovery follow it and keep their existing order. None of them is a step of the planned opportunity, and readiness makes no claim about them. Readiness evaluates the plan before the opportunity's send-time preparation, which it never runs. Preparation changes the plan's inputs in two ways only. A genuine-idle restart, which requires no flight and no pending local bytes, may re-rate the controller, including a ProbeRTT exit; any reservation the refreshed plan then takes is admitted, because the route's credit holds nothing. A packet-size refresh moves only the window's floor and cap. Readiness therefore admits the plan's demand ceiling, its reservation before the congestion-allowance bound, so a window raised at preparation can't enlarge the demand past what readiness admitted. A window lowered there stops the opportunity on congestion, which is the network's verdict, not a local refusal. Readiness adds no reselection, run-loop wakeup or concurrency. Contract closure is not triggered: a refusal is transient local backpressure, not a material safety consequence.

**Temporary seams:** Both policies remain privately selected until B6. Fixture comparison never becomes a public maintained policy.

**Scope amendment:** by [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1043#issuecomment-6855724427), after review found readiness and the opportunity's reservation disagreeing in two distinct cases, the BBR opportunity's plan/execute split in packet_emission_bbr.go and a non-reserving construction-capacity read in native_ordinary.go join this slice. The scheduler still owns choice only. There is no public API, no new concurrency or wakeup, and no change to B5's arms or budgets. By [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1044#issuecomment-6859584262), [B7](#b7)'s ACK-only turn joins the turns that follow selection; it is not a step of the planned opportunity, and readiness makes no claim about it.

**Blast radius and context:** private scheduler and its view construction in the path registry (multipath_admission.go, with the round-robin ledger in multipath_credit.go and multipath_policy.go), packet_emission.go, packet_emission_path.go, datagram_queue.go; the BBR opportunity planner in packet_emission_bbr.go and bbr_send_policy.go; read-only rate and credit accessors in internal/congestion/bbr_sender.go and local_send_credit.go; the non-reserving construction-capacity read in native_ordinary.go and its local-sender interface in packet_emission_local.go; comparison fixture. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: balanced rates; asymmetric delay/rate; no eligible rate; pacing/credit/PMTU constraints; backup exclusion; deterministic ties; no destructive pop before selection; Reno without estimator allocation; controller-specific no-sample fallback; readiness/reservation agreement, with one regression per refusal owner (local credit: limit, GSO batch and isolated MTU probe; native construction capacity). One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** BBR and scheduling contract (scheduling); Contract closure: BBR feedback; Reno packing/PMTU eligibility interface receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Production selection and a tiny comparison implementation fit one slice; splitting evidence driver is B5. B3 supplies valid BBR inputs and complete lifecycle. A public scheduler would add forbidden obligations. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Selection needs live protocol pointers or new public callbacks, duplication or coupled congestion control; readiness needs reselection, a run-loop wakeup or new concurrency; or a third distinct counterexample appears at the readiness/reservation agreement after the planner lands.

<a id="b5"></a>

### B5 — Measure aggregation benefit and shared-bottleneck cost

**Delivers:** Run the accepted three topologies and six modes, report useful STREAM/DATAGRAM outcomes separately and test owner aggregation/coexistence thresholds. Revalidate changed ordinary/native surfaces without silently inheriting earlier evidence; all repeated cases consume the same remaining program budget. A complete valid positive or negative comparison completes this evidence slice. Negative benefit/coexistence results deny the affected aggregate support claim; they are distinct from incomplete or invalid evidence. Use the six named controller-matched arms, required 18 existing-cell BBR revalidation observations and candidate-specific applicability records in the single cumulative ledger.

**Blocked by:** [B7](#b7), which itself follows B4. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Transport integration suite owns matched workload/ledger and support recommendation; runtime policy is frozen. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** None. A failed aggregate comparison leaves aggregate unsupported; it cannot silently choose another algorithm.

**Blast radius and context:** aggregation fixture and receipt; shared qualification ledger. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Example-level evidence over the fixed scenario/toolchain domain, parsed by the pinned peer/fixture's existing protocol implementation. This is the explicitly maintained verification deliverable described above; receipts are metadata. Runtime closure is not recursively imposed on the fixture. Missing/incorrect case accounting is a failed claim, not evidence of product failure.

**TDD / evidence budget:** 54 regular instances: three topologies, six modes, three observations, 60 seconds after 10-second warmup; any justified affected-case repeat charged inside 210/eight hours; valid negative result completes the evidence delivery and denies aggregate support. Build focused driver/accounting regressions first; operational attempts charge the single program ledger. Stop after the fixed cases or first unresolved prerequisite/budget failure; no repeat-until-pass.

**Context imports:** Qualification and adoption policy; Program budget and prior-evidence validity; Reno program qualification ledger. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** One fixed comparison answers the adoption question; no open-ended tuning loop. B4 supplies both selected and fixture-only policies. Do not merge production enabling. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Comparison is incomplete/invalid, prerequisites are missing, newly required semantic scope exceeds the contract or the remaining operational budget is insufficient. A valid threshold failure is a completed negative capability result, not this stop.

<a id="b6"></a>

### B6 — Publish the supported BBR and aggregation outcome

**Delivers:** Enable BBR primary/backup only when its correctness/native/ordinary cost evidence is current. Enable aggregate only on passing its accepted comparisons; otherwise document unsupported aggregate without implementation substitution. Publish exact tested domain and adoption evidence, delete private activation selectors and keep Reno default. Consume B5's valid result even when negative: publish the qualified primary/backup outcome and explicit unsupported aggregate state, without changing the algorithm or thresholds. Publish only combinations supported by matching evidence: Reno primary/backup from R19, qualified BBR primary/backup, and BBR aggregate only on comparative pass. Reno aggregate remains unsupported. State that the BBRv3 controller itself remains experimental and unqualified while full Q2 #599 is deferred; this slice qualifies only the multipath candidate.

**Blocked by:** B5. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Common capability gate owns supported combinations; controller/scheduler selection does not bypass route or platform qualification. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Remove B-track private activation; no dormant alternate public policy or continuing tuning program.

**Blast radius and context:** config.go, congestion_control.go, API/support documentation; fixture activation cleanup. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: enabled/unsupported combinations match evidence; default Reno unchanged; public smoke; absent private bypass; receipt identity/support statement check. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Qualification and adoption policy; Program budget and prior-evidence validity; B5 capability result and evidence applicability receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** A distinct enablement PR keeps failed evidence from granting support. B5 supplies final claim boundaries and affected-case evidence. Further splitting would separate one capability table from its documented contract. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Evidence cannot support the exact enabled combinations or owner thresholds need relaxation.

<a id="b7"></a>

### B7 — Prefer same-path ACK carriage at the multipath receiver

**Delivers:** A multipath receiver carries each protocol path's acknowledgment (ACK for path zero, PATH_ACK for any other path) on that same path whenever that path can carry it. A peer's path-local controller then gets the same-path feedback that the [BBR and scheduling contract](#bbr-and-scheduling-contract) trains on. When the path can't carry its own acknowledgment, any other path may carry it, as today. The rule applies to every multipath connection, whatever its own controller or the peer's, because a receiver can't observe the sender's controller. It adds no public API, configuration or wire frame, and no ack-eliciting traffic, and correctness never depends on peer compliance. Draft-21 §5.5 permits sending PATH_ACK on the path where the acknowledged packets arrived, and the draft has no frame for requesting it from a peer. A peer that returns only cross-path acknowledgments therefore stays the unsupported case above. B7 makes no throughput claim; [B5](#b5) measures the effect.

**Blocked by:** B4 (complete) and [B8](#b8). **Existing work:** new slice; no unmerged implementation accepted. The [#1345](https://github.com/the-sarge/quic-go-fast/issues/1345) fix (PR #1351, merged as `ba811bfcc`) is the source baseline for the turns and the packer.

**Verified shape (at `ba811bfcc`):**

- `composeNextPacket` (`packet_packer.go:832`) takes path zero's ACK and calls `appendPathAcks` (`:1411`), but it doesn't know the carrier. The route that fixes the carrier is already resolved at `shortHeaderIdentity` (`:595`). When no ACK is due, a packet that carries data still takes any target with new ranges (`onlyIfQueued` false, `:862`, `:870`).
- `AppendPathAckFrames` (`internal/ackhandler/received_packet_handler.go:217`) takes the first queued target in map order, one PATH_ACK per packet. A packet's `ackTargets` holds path zero and one other (`ack_targets.go:8`); an overflow panics (`:49`).
- Every packet therefore carries whatever acknowledgments are due on whichever route the opportunity uses. Selection picks a pending PTO's path, then the primary (`packet_emission_path.go:53`). Every newly live path starts as a local backup (`multipath_admission.go:542`), so a primary/backup receiver returns every PATH_ACK on its primary.
- Selection views only members that pass `primaryEligible` (`multipath_admission.go:237`, `multipath_policy.go:43`), path zero included, and then drops fencing members (`multipath_admission.go:265`).
- Each receive space keeps its own queued flag and `max_ack_delay` alarm (`internal/ackhandler/received_packet_tracker.go:108–159`). One connection wakeup merges the alarms (`received_packet_handler.go:104`).
- The sender passes the carrier path (`connection.go:2552`). A BBR context excludes a cross-path ACK from timing (`internal/ackhandler/sent_packet_handler.go:589`); Reno contexts ignore the carrier.
- ACK_FREQUENCY isn't negotiated (`connection.go:599`). The local acknowledgment threshold applies per receive space.
- One packer counter drives the periodic ack-eliciting PING. The field is at `packet_packer.go:149`. `datagramPayload` resets it with no route (`:577`). `maybeGetAppDataPacket` reads, increments and resets it (`:818`, `:824`, `:827`). `composeNextPacket` reserves the PING byte from it (`:842`). `packPingPacket1RTT` resets it (`:1046`).
- Signaling turns already run outside the selected opportunity: admission probes, then selection, then the announcement a cutover owes (`packet_emission_path.go:224`, `packet_emission_bbr.go:98`). `triggerSending` reschedules a call that asks to retry (`connection.go:3031`). `sendBounded` attributes supply to the selected route unless a turn returns `pathTurnSignaling` (`packet_emission_bbr.go:170`).
- Path-validation and MTU probes carry only their own frames, never acknowledgments (`packet_packer.go:1119`, `:1062`).

**Carrier decision:** Packet emission owns which carrier takes each acknowledgment. The decision runs on the connection goroutine under one rule:

- *Home.* A target's own path is home when it passes the same eligibility selection uses: `primaryEligible` and not fencing (`multipath_migration.go:41`). For path zero, `zeroCarries` (`:46`) must also hold. One registry accessor reads it and also refreshes the route to its current tuple.
  - A path is not home when it is validating, retiring or abandoned, fencing, suspect, on a failed endpoint, or lacks a destination CID. This applies to path zero too: a suspect path zero is not home, although `zeroCarries` ignores suspicion. Draft §3.4.3 requires an abandoned path's acknowledgments to leave on another path.
  - Local and peer backup status don't matter. An acknowledgment is due only because the peer is sending on that path. Peer status is advisory here (`multipath_policy.go:34`), and [Reno plan](2026-10-24-multipath-reno-plan.md#public-surface-and-compatibility) item 5 already lets ACK traffic use a backup.
- *Released set.* Before its first packing, each emission call reads the non-home targets into a released set. Refused turns then add to it. The set lives for that one call.
- *Turn.* After the announcement turn and before the held opportunity, emission may give one due home target an ACK-only turn on its own route, unless that path is already the opportunity's route. The turn follows the announcement's shape:
  - switch the local route and check native construction capacity;
  - under BBR, take the route's local credit (the ACK exemption bypasses pacing, never local byte ownership, `packet_emission_bbr.go:153`);
  - pack, register and hand off;
  - restore the selected route and steering.

  The turn packs acknowledgment frames only. It never takes stream, retransmission or DATAGRAM payload, and never adds the periodic PING. It neither reads nor writes the packer's PING counter. Its packet is therefore never ack-eliciting, never in flight, and never arms a PTO. B8's retirement bounds its sent records on a path that carries nothing ack-eliciting. A turn that sends ends the call and requests continuation under both controllers, as a sent announcement does. Under BBR it also returns `pathTurnSignaling`, so the call's delivery observation attributes nothing to the selected route. When the send queue is full, `sendBounded` drops the retry (`packet_emission_bbr.go:164–166`), and the connection's capacity wakeup continues the loop instead (`connection.go:864–871`).
- *Alternation.* A sent turn sets one emission fact. The next emission call skips its turn because the fact is set, and it clears the fact unconditionally, whatever the outcome of that call. The call can run the selected or held opportunity, packet-size recovery or a client probe, or it can end hard-blocked, local-blocked or queue-full. Due home targets wait during that one call without being released. If any is waiting when the call clears the fact, the call requests a retry; when that call ends queue-full, the capacity wakeup continues instead. A due ACK can be queued by threshold, missing packets or ECN-CE with no alarm armed, so the alarm alone doesn't wake the loop; the retry does. Turns and other calls therefore alternate at worst one to one, and recurring receives on another path can't starve the selected route's acknowledgments or credit updates. No timer or concurrency is added.
- *Release.* A home target's turn may be refused because the shared send queue is full, its route has no native construction capacity, BBR local credit refuses it, or its recovery reports `SendNone`. A refused target joins the released set. A released target may ride any carrier under today's rules, in the same call.
- *History room.* Before a turn, emission asks the path's recovery a read-only question, `ackTurnRoom(now)`. The answer is no when either of these holds:
  - the path's sent span, plus the turn's reservation (`max(1, next packet number − highest packet number)`, which covers the turn's packet and any skip), would exceed `(MaxTrackedSentPackets − 1) / (maxRecoveryPaths + 1)`. That share is 4,999, out of a shared ceiling of 25,000. It is one equal share for each application space a connection can hold, and the bound is strict: five spaces at their share hold 24,995 records, below the `SendNone` threshold;
  - the path's oldest outstanding ack-eliciting record is older than one of that path's PTOs.

  On a no, the target is released, as on a refusal, so its acknowledgment falls back to today's carriage. Recovery reports a fact; emission alone decides. Because the turns on every path stop at their share, the turns alone can't fill the ceiling, even when an outstanding record pins a path's front and B8 can't shrink its span. A path whose return direction fails while it carries our data trips the age clause within one PTO, before suspicion.
- *Selection.* Every short-header packet's ACK selection receives its carrier (the route's path ID) and the call's released set. The receive owner first selects the carrier's own target, under today's piggyback rules. It selects another target only when that target is released, again under today's rules. The existing capacity rule still applies: ACK(0) and at most one PATH_ACK, with the rest left queued. Released targets are served in turn. When a packet gives its one PATH_ACK slot to the carrier's own target while a released target is due, the receive owner marks the released target as passed over. The next packet that may carry it gives it the slot ahead of the carrier's own target. The mark is one fact on the target's receive tracker, cleared when that target is consumed, so a released target waits at most one packet for the slot. The receive owner stays the sole owner of acknowledgment construction and consumption.
  - The due read that the turn uses and the consumption that `GetAckFrame` performs share one predicate, so a due target always packs.
  - The packer passes the carrier and released set through and decides nothing.
  - When `pathCredit` is nil, emission releases every target, which is today's behavior. That covers ordinary connections, single-path BBR and private fixtures without admission.
  - Before confirmation, coalesced packets and 0-RTT packets use path zero as the carrier. No other path exists before confirmation, and `ackTargets` ignores the other packet-number spaces.
  - qlog already attributes each packet's path through its packet context.

**Invariant, relative to today:** B7 changes only which carrier takes an acknowledgment, plus two bounded waits.

- A home target that is not yet due no longer piggybacks on another path's data packet. It leaves when its own space's due rule fires (the threshold or the `max_ack_delay` alarm), or earlier on a packet on its own path.
- After a sent turn, a due home target may wait for one emission call, which then requests a retry.

Otherwise each due home target leaves in its own turn. A refused or non-home target leaves within the same call under today's rules. Those rules can still defer it as they do today, through the capacity rule, a full queue or `SendNone`. The threshold, `max_ack_delay` and ACK Delay obligations of each space are unchanged.

**Timing and preservation:**

- ACK-only packets keep their per-route congestion and pacing exemption. They charge their route's native capacity and BBR credit, and their bytes count in that path's round-robin ledger, as every handoff's do.
- A PTO probe still leaves on the probing path, which is then the opportunity's route.
- A peer's Reno contexts ignore the carrier. A Reno path's RTT samples now come from that path's own return direction.
- Under primary/backup sending, data stays on the primary, whose acknowledgment was already on the same path. Only the acknowledgments of probes on backup paths move.
- **Backup paths** (the [Reno primary/backup and failure policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy)). Because the turn adds no ack-eliciting packet, it adds no periodic backup traffic, arms no PTO there, and can't make a backup suspect. It also creates no liveness evidence of its own, so freshness and promotion keep their existing inputs. A backup's own ack-eliciting traffic remains whatever the existing policy sends.
- **Sent and receive history.** The peer acknowledges a turn's packets only after a later ack-eliciting packet on that path. Until then, B8 bounds the path's completed sent records and moves their targets into the bounded carrier store, where a later ACK prunes them only while the carrier remains retained. The target's receive history is bounded by the existing range cap (`MaxNumAckRanges`), and a lossy path's PATH_ACK can grow to that cap. This is an efficiency cost, not a correctness one.
- **One-way failure.** Suppose a home path's return direction fails while the peer's packets still arrive on it. Its acknowledgments are then lost, and the peer's own policy observes the loss: its PTOs and suspicion on that path, and then its failover. That is the peer's failure policy acting on true path evidence, which today's cross-path carriage masks.

**Rejected alternatives (do not do this):**

- **The receive owner alone decides.** It can't see whether a path can carry, and mirroring registry state into it adds a second, stale copy. **Revisit if:** none: rejected on merit.
- **The packer orders targets by carrier and adds no turn.** A primary/backup receiver never sends on its backup, so nothing is delivered. **Revisit if:** none: rejected on merit.
- **Make due acknowledgments a scheduler input** (the owner's design). It would remove the turn and the alternation fact, but it adds obligations:
  - It adds an ACK work class to every policy's views.
  - It forces B4's readiness to range over exempt ACK-only plans as well as the reservations of ordinary opportunities.
  - It ends the scheduler's rule of choosing only ordinary work.

  The turn adds one signaling step in the existing announcement shape, plus one alternation fact. **Revisit if:** a later scheduling redesign makes control frames scheduler inputs.
- **Hold a home target's acknowledgment across a refused turn.** It either spins on a stale alarm or adds ACK delay. **Revisit if:** none: rejected on merit.
- **Count the periodic PING per carrier, so each path's ACK-only packets elicit acknowledgment.** It would move the accepted Reno backup-traffic policy ("No automatic periodic backup traffic is added"). It would add PTOs that send data on a backup, plus suspicion and liveness evidence there. B8 bounds the sent history instead. **Revisit if:** the owner explicitly permits periodic backup PINGs.
- **Honor local or peer backup status for carriage.** Status is advisory here, and draft §5.5 permits arrival-path acknowledgments. **Revisit if:** the published RFC forbids ACK-only packets on a backup path.
- **Prefer same-path carriage only when the peer runs BBR.** The receiver can't observe the peer's controller. **Revisit if:** none: rejected on merit.
- **Send each acknowledgment both on its own path and across paths.** Mixed carriers still invalidate BBR rate intervals, and the duplicates add overhead. **Revisit if:** none: rejected on merit.
- **Request same-path feedback from the peer on the wire.** Draft-21 has no frame for it, and ACK_FREQUENCY neither applies nor is supported. **Revisit if:** a negotiated extension defines such a request.

**Single owner and authority completeness:**

- Emission owns the carrier decision: the turn, the alternation fact, and the per-call released set. It also releases every target when `pathCredit` is nil.
- The registry owns home eligibility, through the one predicate it shares with selection.
- Path recovery reports history room through `ackTurnRoom`, a read-only fact. The decision stays with emission.
- The receive owner owns acknowledgment construction, consumption and the carrier capacity rule.
- The packer keeps its one periodic-PING counter, which the turn never touches, and decides nothing.

No persisted state is introduced. The alternation fact is constructed with emission and cleared unconditionally by the next emission call; close sends nothing further. Cover those constructors and consumers in this slice.

**Temporary seams:** none. The released set lives for one call and is never stored.

**Blast radius and context:**

- The carrier-aware selection in `internal/ackhandler/received_packet_handler.go`.
- In `packet_packer.go`, the carrier and released-set threading through `composeNextPacket`, `appendPathAcks` and their 1-RTT callers, and an ACK-only packing mode for the turn that bypasses the PING counter.
- The ACK turn and the alternation fact in `packet_emission_path.go`, and their places in the Reno loop (`packet_emission.go`) and `boundedPathTurns` (`packet_emission_bbr.go`).
- The home accessor beside selection eligibility in `multipath_admission.go` and `multipath_policy.go`.
- The read-only `ackTurnRoom` accessor on path recovery in `internal/ackhandler/sent_packet_handler.go`. It reads the history span, the next packet number, `FirstOutstanding` and the path's PTO, and it mutates nothing. `FirstOutstanding` stops at the first outstanding record, so it scans at most the path's share.
- Their tests: `internal/ackhandler/received_path_test.go`, `packet_packer_path_ack_test.go`, `multipath_bbr_test.go`, `multipath_reliable_test.go` and `multipath_policy_test.go`, plus one two-connection self test.

Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice. Everything runs on the connection goroutine. The only crossing is the existing native-completion wakeup after a refused turn.

**Representation, artifacts and closure:**

- **Domain:** negotiated draft-21 multipath connections on the binding profile. Carriers are live member routes. Targets are path zero and the admitted nonzero receive spaces (at most `maxRecoveryPaths`, retained spaces included).
- **Guarantee:** universal within that domain, with finite representative tests. It is not a throughput guarantee.
- **Artifact classes:** runtime is shipped behavior, and the release and alternation rules are required safety enforcement. Named tests are maintained regression aids.
- **Closure is triggered.** A withheld acknowledgment stalls the peer's recovery, and home, refusal, lifecycle and turn states reach the rule independently.
- **Invariant:** the one stated above, relative to today. Emission's turn, release and alternation rules enforce it.

| Semantic class | Expected disposition | Enforcement owner | Evidence |
| --- | --- | --- | --- |
| Home target on the packet's own path | Carried under today's piggyback rules | Receive owner selection | Own-target piggyback test |
| Due home target on another path | ACK-only turn on its route. The selected packet excludes it, including ACK(0) on a nonzero carrier | Emission turn | Backup-path turn test; path-zero-on-nonzero-carrier test |
| Non-due home target on another path | Withheld from that path's data packet until due, or until a packet on its own path | Receive owner selection | Withheld-piggyback test |
| Turn refused: queue, native capacity, BBR credit or `SendNone` | Released; carried in the same call under today's rules | Emission release | Refusal table test, one case per owner |
| Not home: validating, retiring/abandoned, fencing, suspect (path zero included), failed endpoint, no destination CID | Released | Registry home predicate | Non-home table test, one case per state |
| Turn just sent | Requests continuation, and under BBR also returns `pathTurnSignaling`; the next call skips its turn and clears the fact whatever its outcome. If a due home target waits, that call requests a retry; a queue-full ending continues through the capacity wakeup | Emission alternation | Sustained-receive test; held-ACK retry test |
| Turn packets on a path with no ack-eliciting traffic | Stay bounded by B8's retirement | B8's recovery owner | Sustained-receive test keeps both paths sendable |
| Home path whose front is pinned, or whose turn would take its span past its strict share | Released before the turn; falls back to today's carriage | Emission, reading `ackTurnRoom` | History-room test, including the boundary |
| Announcement packet before the turn | Its carrier's own target, plus released targets | Receive owner selection | Covered by the selection tests; the announcement path is unchanged |
| Packets carrying no ACK: steered DATAGRAM-only, isolated MTU probe, path-validation probe | Unchanged; no ACK selection runs | Packer, existing | Existing tests |
| `pathCredit` nil | Every target released; unchanged | Emission | Existing bundling test, unchanged |
| Carrier capacity | Own target first, except that a due released target passed over once takes the next packet's PATH_ACK slot; the rest stays queued | Receive owner | Capacity case in the selection test, including a continuously replenished own target X and a released target Y |

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Write these 13 first:

1. A primary/backup receiver sends a due backup-path PATH_ACK as an ACK-only packet on the backup route, and the primary's packet doesn't carry it.
2. Path zero's ACK rides a nonzero carrier only when released, and the capacity rule holds. With path zero retired, carrier X's own target replenished between single-packet calls, and released target Y due, Y leaves on the packet after it was passed over.
3. The carrier's own target still piggybacks on data, and a non-due home target is withheld from another path's data packet.
4. Each non-home state releases its target, as one table test including suspect path zero.
5. Each refusal owner releases its target into the same call, with no stale alarm, as one table test.
6. The turn packs acknowledgments only:
   - it is never ack-eliciting and never touches the PING counter;
   - it debits no pacing and charges the route's credit;
   - it returns `pathTurnSignaling`, so the selected route's supply observation is unchanged.
7. Alternation:
   - Under sustained receive on a backup, the selected route's pending acknowledgment and flow-control credit update still leave between turns.
   - The call after a turn clears the fact even when it ends hard-blocked or queue-full.
   - A threshold-queued home target that waited through that call is sent by its requested retry, with no alarm armed, on a call that doesn't end queue-full.
   - Sustained turns on a backup with no ack-eliciting traffic leave both paths sendable, relying on B8.
8. After a same-ID migration, the turn uses the current tuple.
9. An alarm-due acknowledgment leaves in its turn, carrying its own space's ACK Delay.
10. Two fork connections, with a BBR aggregate sender and a primary/backup receiver: each path's acknowledgments arrive on that path, and both sender paths get valid rate samples.
11. Reno two-path primary/backup transfer and failover are unchanged, and the backup gains no ack-eliciting packet, PTO, suspicion or liveness refresh from the turn.
12. Ordinary connections are unchanged; the existing bundling test stays as it is.
13. History room: when a home path's return direction fails while it carries our data, its turns stop and fall back to the selected carrier within one PTO of the oldest unacknowledged record. At the boundary, a turn that would take the span to exactly 4,999 is allowed, and one that would take it past 4,999 falls back.

One focused race run covers the native-completion wakeup. The required independent multipath peer job on the exact PR head re-establishes the R17 interop matrix.

B7's PR lists only the owners and surfaces it changed: receive selection, packer threading, the emission turn, and the home accessor. B5's existing candidate-specific applicability records decide which Reno and native evidence carries forward to its candidate, and charge any affected cell to the existing reserve. B7 adds no cell, campaign, budget or record.

**Context imports:** BBR and scheduling contract (feedback); Contract closure: BBR feedback (receiver ACK carriage row); Program budget and prior-evidence validity; [Reno R6](2026-10-24-multipath-reno-plan.md#r6) carrier/target interface; B4's readiness and reservation agreement, for turn ordering. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:**

- *Split.* The strongest split is an inert prefactor that passes the carrier through with every target released, followed by the turn and home rule. It is rejected. The prefactor delivers nothing alone, and withholding a home target is safe only in the same PR as the turn that serves it and the alternation that bounds it. Authority completeness requires they ship together, and both fit one context.
- *Merge.* Merging all of B7 into B5 is rejected, because B5 freezes runtime policy and measures it. Only the applicability record merges: B5 already owns candidate-specific applicability records.
- *Edges.* B4 is a genuine blocker: the turn's place beside selection and the end-to-end scenario's aggregate sender both come from B4. B8 is a genuine blocker: without its bound, never-ack-eliciting turns fill the shared sent-history ceiling and stop every path. B5 genuinely depends on B7, because without it arm E measures the cross-path feedback this contract excludes from support claims. B6 is transitive and needs no edge.

Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop.

**Stop:**

- The turn needs a scheduler, view or readiness change, a new wakeup or timer, new concurrency, or reselection inside the opportunity.
- Bounding turn recurrence needs more than the one alternation fact and the retry request of the call that clears it.
- The turn must become ack-eliciting, or must add traffic on backup paths beyond acknowledgments.
- Turn packets on a path with no ack-eliciting traffic don't stay bounded by B8.
- `ackTurnRoom` needs more than a read of recovery state, or turns can still fill the shared ceiling behind a pinned front.
- The due read and the consumption can't share one predicate.
- The carrier capacity must grow beyond two targets.
- Any public API, configuration or wire change is needed.
- A second behaviorally distinct counterexample appears at the release or alternation rule.

<a id="b8"></a>

### B8 — Bound completed ACK-only sent history

**Delivers:** Each path's sent history stays bounded when that path sends only non-ack-eliciting packets. Today the periodic PING bounds a single path. When a path's application history holds no ack-eliciting record and holds more than `protocol.MaxNonAckElicitingAcks` completed non-ack-eliciting records, the recovery owner retires the oldest of them until that many remain. Retirement moves each record's `ackTargets` into the existing bounded carrier store. Where there is no periodic PING, B8 bounds a growth that has no bound today. On a path that the periodic PING reaches, retirement can dispose of a few ACK-only records early, before acknowledgment or loss would remove them. That happens after a selective ACK of the PING, or while a PTO extracts the PING before its replacement registers. It changes no wire, ownership or congestion behavior. A later ACK may find a retired number instead of a live record, and pruning then depends on carrier retention. It adds no wire traffic, timer, API or configuration, and it is independently green. [B7](#b7) relies on it, because B7's ACK-only turns are never ack-eliciting.

**Blocked by:** none. It uses the R6/R7 carrier history and the B2 recovery contexts, all complete. **Existing work:** new slice; no unmerged implementation accepted.

**Verified shape (at `ba811bfcc`):**

- Every registered packet enters its path's sent history, ack-eliciting or not (`internal/ackhandler/sent_packet_handler.go:458`).
- A record leaves the history only when the peer acknowledges it or loss detection declares it lost. A non-ack-eliciting record that is declared lost gets the carrier, completion and history disposal (`:1184–1191`):
  - `captureCongestionLoss` records a never-in-flight loss (`congestion_dispatch.go:188`). Under BBR that call marks the outcome lost, sets `unconfirmedLoss` and feeds the lost-run index that persistent congestion reads (`bbr_recovery.go:230–235`, `:430–534`);
  - `retainAckCarrier` (`:1661`) moves its `ackTargets` into the carrier store and expires each carrier after three PTOs (`sent_packet_handler.go:1668`). The store evicts its oldest carrier at capacity (`lost_packet_tracker.go:99–110`). Capacity is 64 on an ordinary connection; under multipath it is 32 for path zero and 8 for each nonzero path (`sent_path.go:104`);
  - `retireLocal` releases the local record, and `DeclareLost` removes the history entry.

  Spurious-loss, congestion and ECN consequences apply only to ack-eliciting or outstanding records.
- Loss detection needs a later acknowledgment on the path, and with no ack-eliciting record outstanding the loss timer doesn't arm (`:1045`). A path whose peer never acknowledges it therefore keeps every ACK-only record.
- The tracked count is each history's span, holes included (`sent_packet_history.go:190`, `sent_packet_handler.go:1389`). It is shared across paths (`:1374`), and at `MaxTrackedSentPackets` every path returns `SendNone` (`:1434`).
- Removing the oldest records shrinks the span (`sent_packet_history.go` `Remove`/`cleanupStart`). Skipped packet numbers live in a separate list (`:22`) that acknowledgment processing checks first (`sent_packet_handler.go:847`). Records that aren't in the history are not visited (`sent_packet_history.go:128`), so an ACK covering one yields no acknowledged packet.
- Pending native completion records hold their token and slot in the history until `CompleteLocalPacket` resolves them (`local_completion.go:254`).
- The periodic PING counter is one global packer counter (`packet_packer.go:149`, `:818`). On a single path it caps a run of ACK-only packets at `MaxNonAckElicitingAcks`. With aggregate routes, a path can already exceed that cap today.

**Retirement rule:** The path's recovery context owns the decision, on the connection goroutine. It runs the same retirement wherever the trigger can newly hold:

- when it registers a non-ack-eliciting packet;
- when a local completion resolves one;
- when acknowledgment, loss or PTO handling removes the path's last ack-eliciting record. Later ACK-only records can't be declared lost past `largestAcked`, and the alarm is cancelled once nothing is outstanding (`sent_packet_handler.go:1045`, `:1124`).

- **Trigger:** the path's application history holds no ack-eliciting record, and it holds more than `MaxNonAckElicitingAcks` completed non-ack-eliciting records.
  - A record is completed when it has no local registration, or when its native outcome is `LocalPacketAccepted`.
  - A `LocalPacketUncertain` record is never retired. Retiring it would run `retireLocal`'s terminal ECN reconciliation (`local_completion.go:340`, `local_ecn.go:370`), which fails local ECN for an unacknowledged ECT-marked uncertain record, although retirement is no evidence of non-delivery. Skipping that reconciliation would leave the ECN owner's record unreconciled, which changes the ECN owner's terminal contract. Uncertain records therefore keep today's exits: acknowledgment or loss. Only uncertain native submissions create them. A native timeout or capacity error stays nonfatal local backpressure and leaves the path usable (`native_endpoint.go:1424–1429`), so turns can continue after one. B8 doesn't bound uncertain records; B7's strict history-room share bounds the span they pin.
  - Pending records are never retired. Their tokens, disposal and completion authority stay untouched; they pin the span only until native completion resolves them.
- **Disposal:** retire the oldest completed records, in send order, through a non-loss disposal:
  - take the congestion packet record and mark its recovery outcome disposed, through the existing `discardCongestionPacket` (`congestion_dispatch.go:280`; `recoveryEvidence.discard`, `bbr_recovery.go:279`, `outcomeDisposed`), which also disposes the delivery-sampler record;
  - keep its targets with `retainAckCarrier`, release it with `retireLocal`, and remove the history entry.

  Retirement never calls `captureCongestionLoss`, so no record turns lost, `unconfirmedLoss` stays unset, and the lost-run index doesn't change. It writes no loss trace and has no congestion, ECN or spurious-loss consequence.
- **Late acknowledgments:** an ACK that later covers a retired number is ignored. If the record's carrier is still retained and unexpired, the carrier store prunes its targets once. Otherwise those targets' receive history stays within its existing range cap. Retirement never touches skipped numbers, so an ACK for a skipped number remains a protocol violation.
- **While an ack-eliciting record remains:** acknowledgment, loss or PTO handles it, and the path's suspicion follows, as today. Records behind it wait for those exits, as ACK-only records already wait behind an outstanding PING. On a single path that costs only that connection. In multipath the span count is shared, so [B7](#b7) stops sending turns on a path whose span or oldest ack-eliciting record crosses its stated bound.

**Rejected alternatives (do not do this):**

- **Make each path's ACK-only packets ack-eliciting** (a per-carrier PING), so the peer acknowledges them. It would move the accepted Reno backup-traffic policy ("No automatic periodic backup traffic is added"). It would arm PTOs that send data on a backup, and it would create suspicion and liveness evidence there. **Revisit if:** the owner explicitly permits periodic backup PINGs.
- **Count non-nil records instead of the span.** That changes what the shared ceiling and multipath admission reserve. **Revisit if:** none: rejected on merit.
- **Don't register non-ack-eliciting packets.** That breaks carrier pruning, local completion and ECN accounting. **Revisit if:** none: rejected on merit.
- **Retire by age only.** At high receive rates, ACK-only records can approach the shared ceiling within three PTOs. **Revisit if:** none: rejected on merit.

**Single owner and authority completeness:**

- The path's recovery context (`sentPacketHandler`) owns retirement.
- The history performs removal.
- The carrier store holds retired targets until their expiry.
- Local completion keeps its token authority.

No persisted state is introduced. Retirement runs on the existing registration and completion paths, and path removal and close keep their existing disposal. Cover those consumers in this slice.

**Temporary seams:** none.

**Blast radius and context:** `internal/ackhandler/sent_packet_handler.go`, `sent_packet_history.go`, `local_completion.go`, `congestion_dispatch.go` (read only, to confirm the never-in-flight capture), `lost_packet_tracker.go` (carrier store, unchanged), and their tests. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice. Native completion is the only crossing between execution contexts, and it already reaches the connection goroutine through `CompleteLocalPacket`.

**Representation, artifacts and closure:**

- **Domain:** application sent histories on every connection: path zero, admitted nonzero paths, and the single path of an ordinary connection.
- **Guarantee:** universal within that domain, with finite representative tests.
- **Artifact classes:** runtime is shipped behavior; the retirement guard is required safety enforcement; tests are maintained regression aids.
- **Closure is triggered.** Exhausting the shared count stops every path, and pending, completed, skipped, retained and ECN-marked records reach the rule independently.
- **Invariant:** a path with no ack-eliciting record never holds more than `MaxNonAckElicitingAcks` completed non-ack-eliciting records; pending and uncertain records keep their existing exits. Retiring a record never alters a pending completion, a skipped-number check, an ECN verdict or a congestion signal.

| Semantic class | Expected disposition | Enforcement owner | Evidence |
| --- | --- | --- | --- |
| ACK-only stream on a path with no ack-eliciting record | Retire the oldest completed records beyond the bound; the span shrinks; paths stay sendable | Recovery retirement | Sustained exhaustion scenario on two paths |
| Pending native completion record | Not retired; resolves through its token as today | Local completion | Pending-record case |
| Retired record's targets | Enter the bounded carrier store; a later ACK prunes them once while the carrier is retained and unexpired; an evicted or expired carrier leaves the capped receive history | Carrier store | Late-ACK pruning case, retained and evicted |
| ACK covering a retired number | Ignored | Acknowledgment processing | Same case |
| ACK covering a skipped number | Still a protocol violation | Skipped list | Same case |
| History holding an ack-eliciting record | No retirement while it remains; removing the last one runs retirement | Recovery retirement | No-retirement case, then retirement on its removal |
| ECN-marked ACK-only record | An accepted or unregistered record is retired with no ECN validation or ledger effect. An uncertain record is not retired, so terminal ECN reconciliation never runs on retirement | Recovery retirement | ECN case |
| BBR context | Disposal marks the outcome disposed; no lost outcome, `unconfirmedLoss` or lost-run change | Recovery retirement, through `discardCongestionPacket` | BBR case |
| Single path with the periodic PING | May retire a few ACK-only records early, after a selective ACK of the PING or during a PTO's extraction of it; wire, ownership and congestion behavior unchanged | Packer PING, existing | Characterization case |

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with:

1. Sustained ACK-only registration and completion on two paths, more than `MaxTrackedSentPackets` packets with no peer acknowledgment: completed history stays within the bound and both paths keep a sending mode.
2. A pending completion is never retired and still resolves.
3. A retired record's targets are pruned by a later ACK while the carrier is retained, and not after eviction. An ACK covering the retired number is ignored, and an ACK covering a skipped number still fails.
4. A history holding an ack-eliciting record retires nothing. When an ACK removes that last record behind more than `MaxNonAckElicitingAcks` completed ACK-only records, retirement runs at that removal.
5. ECN: an ECN-marked accepted record that is retired leaves ECN validation and the local or BBR ledger unchanged. An ECT-marked `LocalPacketUncertain` record is not retired, and local ECN stays capable.
6. A BBR context's retirement marks the outcome disposed and leaves `unconfirmedLoss` and the lost-run index unchanged.
7. Single-path receive with the periodic PING covers two sequences. In the first, ACK-only 0–18, PING 19 and ACK-only 20–38 are sent, and an ACK covers only the PING; retirement disposes of the records left beyond the bound. In the second, a PTO extracts the PING with completed records on either side. In both, no congestion, ECN or spurious-loss outcome changes, and a later ACK of a retired number is ignored.
8. Path removal with retired carriers keeps its existing reclamation.

One focused race run covers native completion.

**Context imports:** Contract closure: BBR feedback (shared store saturation row); [Reno R6](2026-10-24-multipath-reno-plan.md#r6) carrier/target interface; this slice's matrix. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:**

- *Split.* The strongest split separates retirement at registration from retirement at completion. It is rejected: both sites enforce one bound on one store, and neither is complete alone.
- *Merge.* Merging into B7 is rejected. The recovery owner and the carriage owner are different owners, the merged slice would exceed one context, and B8 is green and useful on its own.
- *Edges.* There is no blocking edge. B8 blocks B7 genuinely: B7's never-ack-eliciting turns are unbounded without it.

Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop.

**Stop:**

- Retirement would need to change the span-based count, the shared ceiling or the carrier store's bound.
- Retirement would need to touch a pending completion or skipped-number handling, or to run or skip terminal ECN reconciliation for an uncertain record.
- Retirement would need `captureCongestionLoss`, or would otherwise carry a congestion, ECN, spurious-loss or BBR loss signal, for a never-in-flight record.
- A path with no ack-eliciting record can't be kept within the bound without new traffic.
- A second behaviorally distinct counterexample appears at the retirement rule.

## Acceptance and validation

The track is complete only when every slice's narrow end-to-end outcome, owner invariant, removal seam and named evidence are satisfied, with no open stop-for-decision. Universal statements range only over the binding runtime domain and the semantic classes owned by each slice; source inspection must map every supported producer/consumer to that owner. No unsupported platform/peer/provider claim follows from a passing fixture. Finite tests terminate as specified; missing evidence stays missing.

Default-disabled ordinary traffic, opted-in ordinary traffic and negotiated multipath are separate populations. Preserve unchanged default behavior with characterization/regression coverage; measure the changed opted-in ordinary cost against its explicit tolerances. Hosted repository workflows remain authoritative on the exact live PR head. A receipt for an earlier candidate or a mirror's plan SHA is not code-head certification.

A newly discovered untraced shared-state, concurrency, public API, failure, performance, security or dependency effect is a slice stop until scoped re-audit establishes that it fits the existing outcome and one-PR budget. Do not silently merge adjacent work. Public support and adoption claims require their distinct named gates; the owner permits exploratory implementation before consumer benefit, not unsupported claims.

A valid negative B5 result satisfies the evidence-delivery criterion; support enablement follows its result, not an all-modes-pass condition. Invalid or missing evidence remains blocking.
