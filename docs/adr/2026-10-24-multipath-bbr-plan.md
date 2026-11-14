# BBR and aggregation implementation plan

**Status:** accepted design and audited slices; 6/7 complete. [B1 #1040](#b1) partitioned BBR delivery sampling, pacing and local credit into private per-path contexts; [B2 #1041](#b2) gave those contexts their own ECN validation, migration fence and bounded recovery evidence. [B3 #1042](#b3) connected those contexts to private primary/backup operation through the complete path lifecycle; its [applicability record](../audits/2026-11-13-multipath-b3-applicability.md) carries the Reno and native evidence forward. [B4 #1043](#b4) added the private aggregate scheduling policy and its fixture-only byte-round-robin comparison arm. By [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1044#issuecomment-6859584262), [B7 #1357](#b7) delivered the receiver's same-path feedback preference. By a [later owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1356#issuecomment-6866681570), its former prefactor [B8 #1356](#b8) closed with no code, and B7's acknowledgment-only packets carry the periodic PING that single-path connections already send. [B5 #1044](#b5) ran the controller-matched aggregation comparison and the BBR primary/backup revalidation; its [receipt](../audits/2026-11-14-multipath-b5-receipt.md) records the result and the evidence applicability. [B6 #1045](#b6) is ready. Dispatch requires the verified default-branch publication and a frontier child issue. **Program:** `QGF-MP646-20261024`. **Track:** B, 3 of 3. **Date:** 2026-10-24. **Normative scope:** current outcomes, boundaries, invariants, evidence, blockers and stops. **Audit history:** [final consideration response](../audits/2026-10-24-multipath-646-final-consideration-response.md) and [slice audit](../audits/2026-10-24-multipath-646-slice-audit.md). **Parent issue:** pending.

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
| ACK-only sent history on a carrier path | The packer's periodic PING, counted per carrier, makes at most every twentieth consecutive non-ack-eliciting packet on a path ack-eliciting, so acknowledgment and loss bound that path's history as they bound a single path today | [B7](#b7) carriage matrix |

The fixture-only byte-round-robin comparison is a maintained verification aid solely for the accepted aggregation comparison: payoff is discriminating scheduler benefit, domain is the three fixed topologies/six modes, owner is the transport integration suite, retirement is completion of this one qualification decision or feature removal. It is not a public option or a perpetual tuning program. No cross-path-only-feedback throughput claim is included.

## Program budget and prior-evidence validity

B5 uses 54 of the shared 118 regular run instances. The [Reno program ledger](2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing) owns the cumulative 210-run/eight-host-hour ceiling and execution preflight. Inspect B1–B4 and B7 changes against prior native/ordinary evidence. Unchanged owners/domain may retain receipts with a source-based applicability record; changed behavior needs the affected existing cases rerun within the same ceiling. Do not automatically replay an entire matrix or inherit a stale result. If more scope/budget is necessary, stop for a decision.

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
| [B5](#b5) | complete | Measure aggregation benefit and shared-bottleneck cost | B7 |
| [B6](#b6) | new | Publish the supported BBR and aggregation outcome | B5 |
| [B7](#b7) | complete | Prefer same-path ACK carriage at the multipath receiver | B4 |

[B8](#b8) closed with no code by owner decision and is no longer a slice.

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

**Temporary seams:** A private evidence selection on the Config's multipath request admits BBR at the capability gate and gives the path registry its scheduling policy at activation. Its zero value is the public profile, nothing public reaches it, and [B6](#b6) removes it. A failed aggregate comparison leaves aggregate unsupported; it cannot silently choose another algorithm.

**Scope amendment:** by scoped preflight re-audit on 2026-11-13, [B4's interface receipt](https://github.com/the-sarge/quic-go-fast/issues/1043#issuecomment-6858180792) and current source confirm that only in-package unit fixtures set the scheduler and that the public gate refuses BBR multipath. No real connection could therefore run arms D–F or the BBR revalidation cells. The private evidence selection above, and the controller choice the existing R17 and R18 fixtures need for those cells, join this slice. There is no public API; the default policy and the gate's answer to every public Config are unchanged, and the arms, observations and budgets are unchanged. The 18 revalidation observations keep each existing cell's flows, profiles and counts and change only the controller. The six interop instances run the R17 matrix's useful-transfer, abandon/reopen and same-ID migration scenarios with the quic-go endpoint on BBR primary/backup, in both roles. In the 12 ordinary observations, every connection the cell creates runs BBR in both endpoint profiles, so each comparison stays controller-matched. In mixed bulk, those are the measured ordinary flow, the opted-in profile's multipath background on BBR primary/backup and the control profile's two independent background connections. Stateless pressure creates only the measured ordinary flow, so it revalidates BBR on the granted endpoint under that pressure without a background.

**Blast radius and context:** aggregation fixture and receipt; shared qualification ledger; the private evidence selection in multipath_api.go and its copy into the registry at activation in multipath_credit.go, plus their tests; the BBR controller choice for the revalidation cells in the R17 matrix fixture (multipath_picoquic_matrix_test.go, interop/multipath/run.py) and the R18 ordinary-cost fixture (ordinary_cost_test.go, interop/multipath/ordinary_cost.py). Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

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

**Delivers:** A multipath receiver carries each protocol path's acknowledgment (ACK for path zero, PATH_ACK for any other path) on that same path whenever that path can carry it. A peer's path-local controller then gets the same-path feedback that the [BBR and scheduling contract](#bbr-and-scheduling-contract) trains on. When the path can't carry its own acknowledgment, any other path may carry it, as today. The rule applies to every multipath connection, whatever its own controller or the peer's, because a receiver can't observe the sender's controller. It adds no public API, configuration or wire frame. Its only ack-eliciting traffic is the periodic PING that a single path already sends, now counted per carrier, plus a bare-PING probe when a PTO armed only by such PINGs fires off the primary. B7 adds them only on a path the peer is sending on, as the [narrowed Reno backup-traffic policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy) permits. Correctness never depends on peer compliance. Draft-21 §5.5 permits sending PATH_ACK on the path where the acknowledged packets arrived, and the draft has no frame for requesting it from a peer. A peer that returns only cross-path acknowledgments therefore stays the unsupported case above. B7 makes no throughput claim; [B5](#b5) measures the effect.

**Blocked by:** B4 (complete). **Existing work:** new slice; no unmerged implementation accepted. The dropped [B8](#b8)'s work is abandoned and is no baseline. The [#1345](https://github.com/the-sarge/quic-go-fast/issues/1345) fix (PR #1351, merged as `ba811bfcc`) is the source baseline for the turns and the packer.

**Verified shape (at `ba811bfcc`; the PING, probe and backup-policy sites re-verified at `41366b962`):**

- `composeNextPacket` (`packet_packer.go:832`) takes path zero's ACK and calls `appendPathAcks` (`:1411`), but it doesn't know the carrier. The route that fixes the carrier is already resolved at `shortHeaderIdentity` (`:595`). When no ACK is due, a packet that carries data still takes any target with new ranges (`onlyIfQueued` false, `:862`, `:870`).
- `AppendPathAckFrames` (`internal/ackhandler/received_packet_handler.go:217`) takes the first queued target in map order, one PATH_ACK per packet. A packet's `ackTargets` holds path zero and one other (`ack_targets.go:8`); an overflow panics (`:49`).
- Every packet therefore carries whatever acknowledgments are due on whichever route the opportunity uses. Selection picks a pending PTO's path, then the primary (`packet_emission_path.go:53`). Every newly live path starts as a local backup (`multipath_admission.go:542`), so a primary/backup receiver returns every PATH_ACK on its primary.
- Selection views only members that pass `primaryEligible` (`multipath_admission.go:237`, `multipath_policy.go:43`), path zero included, and then drops fencing members (`multipath_admission.go:265`).
- Each receive space keeps its own queued flag and `max_ack_delay` alarm (`internal/ackhandler/received_packet_tracker.go:108–159`). One connection wakeup merges the alarms (`received_packet_handler.go:104`).
- The sender passes the carrier path (`connection.go:2552`). A BBR context excludes a cross-path ACK from timing (`internal/ackhandler/sent_packet_handler.go:589`); Reno contexts ignore the carrier.
- ACK_FREQUENCY isn't negotiated (`connection.go:599`). The local acknowledgment threshold applies per receive space.
- One connection-wide packer counter drives the periodic ack-eliciting PING. The field is at `packet_packer.go:149`. `datagramPayload` resets it with no route (`:577`). `maybeGetAppDataPacket` reads it (`:818`), resets it after adding the PING (`:822`), increments it (`:824`) and resets it for an ack-eliciting packet (`:827`). `composeNextPacket` reserves the PING byte from it (`:842`). `packPingPacket1RTT` resets it (`:1046`). `MaxNonAckElicitingAcks` is 19 (`internal/protocol/params.go:80`), so on a single path every twentieth consecutive ACK-only packet carries a PING. Because any route's ack-eliciting packet resets the one counter, a path whose only packets are ACK-only never reaches it while another path carries data.
- A 1-RTT PTO probe (`sendProbePacket`, `packet_emission.go:564`) first extracts the probing path's oldest outstanding record (`QueueProbePacketAt`, `internal/ackhandler/sent_packet_handler.go:1496`), then packs through `packPTOProbePacket1RTT` (`packet_packer.go:1002`). That packer takes stream, retransmission and DATAGRAM payload from the connection's queues (`maybeGetAppDataPacket`, `:1011`), so a probe on a backup can carry any path's data. `packPingPacket1RTT` (`:1033`) packs a PING with queued ACKs and leaves the framer, retransmission and DATAGRAM queues untouched. Selection gives a path with a pending PTO the opportunity under both policies (`packet_emission_path.go:53`, `multipath_scheduler.go:82`).
- Backup-path policy reads acknowledged ack-eliciting traffic. An ACK newly acknowledging an ack-eliciting, non-path-probe packet records its tuple (`internal/ackhandler/sent_packet_handler.go:703`). `noteAck` (`multipath_policy.go:80`) then calls `noteLiveness` (`:107`), which clears the PTO attempts, refreshes `livenessAt` and recovers a suspect path as a backup. `notePTO` (`:65`) counts a qualified PTO expiration, and the third makes the path suspect (`markSuspect`, `:125`), which drains its outstanding work and starts bounded revalidation. `reselect` (`:153`) promotes a backup only while `fresh` (`:55`) holds, and otherwise revalidates it first.
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

  The turn packs acknowledgment frames, plus the periodic PING when its carrier's count reaches the per-carrier bound under *Periodic PING and bare-PING probe* below. It never takes stream, retransmission or DATAGRAM payload. A turn without the PING is never ack-eliciting, never in flight and arms no PTO. A turn that carries the PING is ack-eliciting and in flight on its route, and it arms that path's PTO, exactly as the PING on today's ACK-only packet does. It keeps the ACK-only packet's congestion and pacing exemption, as that PING does today. A turn that sends ends the call and requests continuation under both controllers, as a sent announcement does. Under BBR it also returns `pathTurnSignaling`, so the call's delivery observation attributes nothing to the selected route. When the send queue is full, `sendBounded` drops the retry (`packet_emission_bbr.go:164–166`), and the connection's capacity wakeup continues the loop instead (`connection.go:864–871`).
- *Alternation.* A sent turn sets one emission fact. The next emission call skips its turn because the fact is set, and it clears the fact unconditionally, whatever the outcome of that call. The call can run the selected or held opportunity, packet-size recovery or a client probe, or it can end hard-blocked, local-blocked or queue-full. Due home targets wait during that one call without being released. If any is waiting when the call clears the fact, the call requests a retry; when that call ends queue-full, the capacity wakeup continues instead. A due ACK can be queued by threshold, missing packets or ECN-CE with no alarm armed, so the alarm alone doesn't wake the loop; the retry does. Turns and other calls therefore alternate at worst one to one, and recurring receives on another path can't starve the selected route's acknowledgments or credit updates. No timer or concurrency is added.
- *Release.* A home target's turn may be refused because the shared send queue is full, its route has no native construction capacity, BBR local credit refuses it, or its recovery reports `SendNone`. A refused target joins the released set. A released target may ride any carrier under today's rules, in the same call.
- *History room.* Before a turn, emission asks the path's recovery a read-only question, `ackTurnRoom(now)`. The answer is no when either of these holds:
  - the path's sent span, plus the turn's reservation (`max(1, next packet number − highest packet number)`, which covers the turn's packet and any skip), would exceed `(MaxTrackedSentPackets − 1) / (maxRecoveryPaths + 1)`. That share is 4,999, out of a shared ceiling of 25,000. It is one equal share for each application space a connection can hold, and the bound is strict: five spaces at their share hold 24,995 records, below the `SendNone` threshold;
  - the path's oldest outstanding ack-eliciting record is older than one of that path's PTOs.

  On a no, the target is released, as on a refusal, so its acknowledgment falls back to today's carriage. Recovery reports a fact; emission alone decides. Because the turns on every path stop at their share, the turns alone can't fill the ceiling, even when an outstanding record pins a path's front. A path whose return direction fails while it carries our data trips the age clause within one PTO, before suspicion.

  The age clause also reads the turn's own PINGs, which are that path's ack-eliciting records when it carries nothing else. Normally the peer acknowledges a PING within one round trip plus its ACK delay, which is inside one PTO, so the clause stays open. When a PING stays unacknowledged for one PTO, the path's targets are released and leave on the selected carrier. The PTO probe that follows extracts the oldest outstanding record, so turns can resume while the oldest remaining record is younger than one PTO. That alternation ends when an acknowledgment restores liveness, or when the third qualified expiration makes the path suspect and therefore not home. Throughout, the span clause still caps the path at its share.
- *Selection.* Every short-header packet's ACK selection receives its carrier (the route's path ID) and the call's released set. The packet's eligible targets are the carrier's own target and the released targets, each under today's piggyback rules. No other target is eligible. The existing capacity rule still applies: ACK(0) and at most one PATH_ACK, with the rest left queued. Service is bounded. Every due eligible nonzero target leaves within `maxRecoveryPaths` packets that may carry it, even when the other targets are replenished arbitrarily. That includes the carrier's own target and every released target. The receive owner alone decides the order. It keeps at most one service-order fact per receive tracker. Path zero keeps its own ACK(0) slot in every packet and never competes for the PATH_ACK slot. *Reference design (non-normative):* first-come-first-served by pass-over time. Whenever a packet gives the slot to one target while another due target that may ride it waits, the waiting target is marked as passed over. This applies to the carrier's own target too. Marked targets go first, the earliest mark first, and ties go to the lower wire ID. A mark clears when its target is consumed. The implementation may choose another order only if it meets this invariant and the stop below. The receive owner stays the sole owner of acknowledgment construction and consumption.
  - The due read that the turn uses and the consumption that `GetAckFrame` performs share one predicate, so a turn's packet always carries an acknowledgment. The turn's own target takes the PATH_ACK slot unless bounded service gives the slot to an earlier-waiting released target. In that case the own target leaves within the service bound.
  - The packer passes the carrier and released set through and decides nothing.
  - When `pathCredit` is nil, emission releases every target, which is today's behavior. That covers ordinary connections, single-path BBR and private fixtures without admission.
  - Before confirmation, coalesced packets and 0-RTT packets use path zero as the carrier. No other path exists before confirmation, and `ackTargets` ignores the other packet-number spaces.
  - qlog already attributes each packet's path through its packet context.

**Periodic PING and bare-PING probe:** These carry out the [narrowed Reno backup-traffic policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy). The packer owns the PING; emission owns the probe's packing.

- *Per-carrier count.* The packer's existing counter is extended per carrier; there is no second counting rule. Every site that reads or writes `numNonAckElicitingAcks` today (`packet_packer.go:577`, `:818`, `:822`, `:824`, `:827`, `:842`, `:1046`) applies the same rule to the count of the packet's carrier:
  - Path zero, the nil route, keeps the existing field (`:149`). An ordinary connection has only that carrier, so its behavior is unchanged.
  - Each nonzero route keeps its own count in a new `pathRoute` field. It starts at zero when its member is constructed and dies with that member. `refreshRoute` (`multipath_admission.go:274`) updates the route in place and leaves the count alone, so a same-ID migration keeps it. Only the packer reads or writes it.
  - On every carrier, every twentieth consecutive non-ack-eliciting packet on that carrier carries a PING: one PING per at most `MaxNonAckElicitingAcks` + 1 packets on that path, the bound a single path uses today. Any ack-eliciting packet on that carrier resets its count, and a packet on another carrier doesn't touch it. Path-validation and MTU probes leave every count unchanged, as today.
  - On the opportunity's route, including a held route, ACK-only packets count as they do today. The only change is that another route's data no longer resets the count. On a path that isn't selected, the only ACK-only packets are turns, and a turn runs only for a due home target, which exists only because the peer sent ack-eliciting packets on that path. B7 therefore adds a PING on a backup only while the peer is actively sending on it.
- *Bare-PING probe.* A PTO grants two 1-RTT probes (`internal/ackhandler/sent_packet_handler.go:1288`), and each one is a separate `sendProbePacket` call. On a path other than the designated primary, each call first reads, before any extraction, one more read-only fact from that path's recovery, `onlyPingsOutstanding`:
  - It ranges over every outstanding ack-eliciting record of the path's application history, including one still pending local completion, which `QueueProbePacketAt` can't extract (`sent_packet_handler.go:1502–1510`, `local_completion.go:356`).
  - It is true when none of those records holds a stream frame, and every frame each holds is a PING or an acknowledgment. Path zero's ACK is kept apart from a record's frames, but a PATH_ACK is a `*wire.AckFrame` in the record's frames (`received_packet_handler.go:240`, `packet_packer.go:1419`). So a turn's PATH_ACK-plus-PING record qualifies, and so do earlier bare probes and a control-only announcement's PING with its acknowledgments. Any other frame fails it.
  - With no outstanding ack-eliciting record at all, it is true, so a probe still owed on such a path never falls back to data.
  - It inspects every such record, stops at the first one that fails, and mutates nothing. Its scan is bounded by the path's history span, which the shared `MaxTrackedSentPackets` ceiling bounds; a former primary's span can exceed the 4,999-record turn share, and the predicate still reads all of it. The turn share stays a limit on turns only.

  When it holds, that call governs both of today's packing sites. It extracts at most one network-eligible record through `QueueProbePacketAt`, as today. Whether or not one was extracted, it packs through the existing `packPingPacket1RTT`, never through `packPTOProbePacket` at `packet_emission.go:594` or its nothing-extracted fallback at `:601`. Both probes of a qualifying PTO are therefore payload-free, including a second probe that finds the first one's PING still pending. Each probe is a PING that carries no stream, retransmission, DATAGRAM or other control frame. It may carry only the acknowledgments that B7's selection lets that carrier take. Those are ACK traffic, which the Reno policy already allows on a backup. Queued data stays in the connection's queues and leaves on the primary's next ordinary opportunity.
  - The designated primary keeps today's probe, which may carry data, as on a single path. That includes a primary that is locally backup in the all-backup state, because it carries the ordinary work. Under primary/backup, every other path is a local backup or otherwise unselected. Under aggregate, an active non-primary path that holds only PINGs is idle, and its data comes through ordinary scheduling.
  - A probe on a path that holds outstanding data keeps today's packing. The existing policy put that data there, and B7 doesn't change it.
  - B7 itself therefore never puts stream or DATAGRAM data on a backup. Its turns take no payload, and a PTO that only its PINGs arm sends a bare PING.

**Invariant, relative to today:** B7 changes only which carrier takes an acknowledgment, plus two bounded waits. On a path that carries turns, it also adds the periodic PING a single path already sends. Its probe rule is about content, not about turns: every 1-RTT PTO probe off the designated primary is a bare PING when the path's outstanding ack-eliciting records hold only PING and acknowledgment frames, or there are none. That also replaces today's data-capable probe for an existing control-only announcement PING or periodic PING on such a path. While no path is designated primary, no path has the primary's exception. The designated primary, a path holding outstanding data, and ordinary connections keep today's probe.

- A home target that is not yet due no longer piggybacks on another path's data packet. It leaves when its own space's due rule fires (the threshold or the `max_ack_delay` alarm), or earlier on a packet on its own path.
- After a sent turn, a due home target may wait for one emission call, which then requests a retry.

Otherwise each due home target leaves in its own turn, or within the bounded slot service when an earlier-waiting released target takes that turn's slot. A refused or non-home target leaves within the same call under today's rules. Those rules can still defer it as they do today, through the capacity rule, a full queue or `SendNone`. The threshold, `max_ack_delay` and ACK Delay obligations of each space are unchanged.

**Timing and preservation:**

- ACK-only packets keep their per-route congestion and pacing exemption, including one that carries the periodic PING, as today. They charge their route's native capacity and BBR credit, and their bytes count in that path's round-robin ledger, as every handoff's do.
- A PTO probe still leaves on the probing path, which is then the opportunity's route. Off the primary, a probe whose path holds only PINGs is the bare PING above.
- A peer's Reno contexts ignore the carrier. A Reno path's RTT samples now come from that path's own return direction.
- Under primary/backup sending, data stays on the primary, whose acknowledgment was already on the same path. Only the acknowledgments of probes on backup paths move.
- **Backup paths** (the [Reno primary/backup and failure policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy), narrowed by [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1356#issuecomment-6866681570)). On a backup the peer has never sent ack-eliciting packets on, no turn runs, so B7 adds nothing to it: no packet, PTO, suspicion or liveness. While the peer is sending there, the turn's PING is ordinary current-tuple evidence under the existing policy, with the effects below. When the peer goes idle after turns, no new turn runs, but a PING already sent keeps its existing consequences: a late acknowledgment is liveness, and its PTO sends bare probes, whose qualified expirations can still make the path suspect. Suspicion's revalidation is bounded per candidate by `pathTimeout`. While the path stays suspect and the existing guards permit, the service pass may start a new candidate after one expires (`multipath_admission.go:735–739`), as for any suspect path today. Peer silence ends no tail that has already started, and clearing the last outstanding PING doesn't end a suspect path's revalidation.
  - *Liveness.* An ACK that newly acknowledges the PING, on whichever path it arrives, records the backup's tuple (`sent_packet_handler.go:703`). `noteAck` then calls `noteLiveness`, which clears its PTO attempts and refreshes `livenessAt`. An acknowledged PING on a newly cut-over tuple also discharges an owed announcement, as any acknowledged packet on that tuple does. This is true delivery evidence for that tuple: our PING reached the peer, and the peer's packets arrive there.
  - *Failover without revalidation.* `fresh` keeps such a backup fresh while its PINGs are acknowledged within max(10 seconds, three path PTOs). When the primary fails, `reselect` may then promote it without revalidating it first. A backup the peer is idle on goes stale as today and is revalidated before promotion. The promotion rule is unchanged; only a supply of genuine evidence is added, and it comes only from a path the peer is using.
  - *Suspicion.* An unacknowledged PING arms the backup's PTO. Each qualified expiration counts in `notePTO`, and the third makes the backup suspect. A suspect backup is not home, so its turns stop and its acknowledgments fall back to cross-path carriage. `markSuspect` drains its outstanding records, which are PINGs with nothing to reschedule, and starts the bounded revalidation that validation traffic on a backup already permits. A backup's suspicion never displaces a healthy primary. As today, an ACK of a drained PING is not liveness, so the path's `PATH_RESPONSE` recovers it as a backup, with its probe credit cleared.
  - *Data.* No stream or DATAGRAM byte reaches a backup through B7: turns take no payload, and the PTO probes their PINGs arm are bare PINGs.
- **Sent and receive history.** The peer acknowledges a turn's packets when it acknowledges that path's next PING, at most `MaxNonAckElicitingAcks` turns later, or any earlier ack-eliciting packet there. Acknowledgment and loss detection then dispose of the path's records, and a lost ACK-only record's targets enter the bounded carrier store, exactly as on a single path today. The PING therefore bounds the path's sent history as it bounds a single path. The target's receive history is bounded by the existing range cap (`MaxNumAckRanges`), and a lossy path's PATH_ACK can grow to that cap. This is an efficiency cost, not a correctness one.
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
- **Keep turns never ack-eliciting and retire completed ACK-only sent history instead** (the dropped [B8](#b8)). A retired number leaves the sent history but still reaches acknowledgment, loss and ECN processing, and four review counterexamples shared that root. **Revisit if:** the owner withdraws the bounded backup PING, or a design keeps acknowledgment, loss and ECN processing whole for retired numbers.
- **Keep one connection-wide PING count and let turns use it.** Another route's data resets that count, so a backup carrying only turns would never reach it. Aggregate routes can already exceed the single-path cap per path through it. **Revisit if:** none: rejected on merit.
- **Add a second count for turns only, beside the connection-wide one.** Two counting rules would share one packer, and other ACK-only runs on a route would stay unbounded per path. **Revisit if:** none: rejected on merit.
- **Put the PING on every turn.** It sends up to twenty times the single-path rate of ack-eliciting traffic on a backup, beyond the owner's bound. **Revisit if:** none: rejected on merit.
- **Make every PTO probe off the primary a bare PING, whatever the path holds.** That changes completed Reno probe packing for a backup holding data that the existing policy put there, which the owner's decision doesn't cover. **Revisit if:** the owner narrows backup probes further.
- **Honor local or peer backup status for carriage.** Status is advisory here, and draft §5.5 permits arrival-path acknowledgments. **Revisit if:** the published RFC forbids ACK-only packets on a backup path.
- **Prefer same-path carriage only when the peer runs BBR.** The receiver can't observe the peer's controller. **Revisit if:** none: rejected on merit.
- **Send each acknowledgment both on its own path and across paths.** Mixed carriers still invalidate BBR rate intervals, and the duplicates add overhead. **Revisit if:** none: rejected on merit.
- **A fixed own-first or released-first priority for the PATH_ACK slot.** Under replenishment, either order can starve a target. The contract therefore states a bounded-service invariant and leaves the exact order to the implementation. **Revisit if:** none: rejected on merit.
- **Request same-path feedback from the peer on the wire.** Draft-21 has no frame for it, and ACK_FREQUENCY neither applies nor is supported. **Revisit if:** a negotiated extension defines such a request.

**Single owner and authority completeness:**

- Emission owns the carrier decision: the turn, the alternation fact, and the per-call released set. It also releases every target when `pathCredit` is nil. It decides a probe's packing, from the designated primary and `onlyPingsOutstanding`.
- The registry owns home eligibility, through the one predicate it shares with selection, and the designated primary.
- Path recovery reports two read-only facts, history room through `ackTurnRoom` and `onlyPingsOutstanding`. The decisions stay with emission.
- The receive owner owns acknowledgment construction, consumption and the carrier capacity rule.
- The packer owns the periodic PING: one counting rule, one count per carrier. It decides nothing about carriage.
- The existing backup policy owns suspicion, liveness and promotion, and B7 changes none of its rules.

No persisted state is introduced. The alternation fact is constructed with emission and cleared unconditionally by the next emission call; close sends nothing further. A route's PING count is constructed zero with its member, kept across a same-ID migration, and removed with the member. Cover those constructors and consumers in this slice.

**Temporary seams:** none. The released set lives for one call and is never stored.

**Blast radius and context:**

- The carrier-aware selection in `internal/ackhandler/received_packet_handler.go`.
- In `packet_packer.go`, the carrier and released-set threading through `composeNextPacket`, `appendPathAcks` and their 1-RTT callers, an ACK-only packing mode for the turn, and the per-carrier PING count at the seven existing counter sites. The count's field on `pathRoute` is in `packet_emission_path.go`.
- The ACK turn and the alternation fact in `packet_emission_path.go`, and their places in the Reno loop (`packet_emission.go`) and `boundedPathTurns` (`packet_emission_bbr.go`). The bare-PING choice in `sendProbePacket` (`packet_emission.go`).
- The home accessor beside selection eligibility in `multipath_admission.go` and `multipath_policy.go`.
- The read-only `ackTurnRoom` and `onlyPingsOutstanding` accessors on path recovery in `internal/ackhandler/sent_packet_handler.go`. `ackTurnRoom` reads the history span, the next packet number, `FirstOutstanding` and the path's PTO, and it mutates nothing. `FirstOutstanding` stops at the first outstanding record, so it scans at most the path's share.
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
| ACK-only packets on a carrier, turns included | Every twentieth consecutive non-ack-eliciting packet on that carrier carries the PING; another carrier's packets don't reset the count; ordinary connections unchanged | Packer, one count per carrier | Turn packing test; sustained-receive test keeps both paths sendable; existing ordinary PING tests |
| PTO off the designated primary on a path whose outstanding ack-eliciting records hold only PING and acknowledgment frames, or none, pending records included | Both probes are bare PINGs with no stream, retransmission or DATAGRAM payload, at both packing sites; queued data leaves on the primary | Emission, reading `onlyPingsOutstanding` | Bare-probe test |
| PTO on the designated primary, or on a path holding outstanding data | Today's probe packing | Emission, existing | Bare-probe test, negative cases |
| Acknowledged turn PING on a backup | Liveness: attempts cleared, `livenessAt` refreshed; the backup stays fresh and may be promoted without revalidation | Existing backup policy (`noteAck`, `noteLiveness`, `fresh`, `reselect`) | Backup-evidence test |
| Unacknowledged turn PINGs on a backup | Three qualified PTO expirations make it suspect: not home, turns stop, acknowledgments fall back, revalidation starts, the primary stays | Existing backup policy (`notePTO`, `markSuspect`) | Backup-evidence test |
| Backup the peer has never sent on | No turn, PING, PTO, suspicion or liveness from B7; stale as today | Emission turn, through the home and due rules | Reno primary/backup test |
| Backup the peer goes idle on after turns | No new turn; an outstanding PING keeps its acknowledgment, bare probes and qualified PTOs, and suspicion's revalidation runs under the existing per-candidate bound | Existing backup policy | Backup-evidence test |
| Home path whose front is pinned, including by its own unacknowledged PING, or whose turn would take its span past its strict share | Released before the turn; falls back to today's carriage | Emission, reading `ackTurnRoom` | History-room test, including the boundary |
| Announcement packet before the turn | Its carrier's own target, plus released targets | Receive owner selection | Covered by the selection tests; the announcement path is unchanged |
| Packets carrying no ACK: steered DATAGRAM-only, isolated MTU probe, path-validation probe | Unchanged; no ACK selection runs | Packer, existing | Existing tests |
| `pathCredit` nil | Every target released; unchanged | Emission | Existing bundling test, unchanged |
| Carrier capacity | One PATH_ACK per packet, with ACK(0) in its own slot. Every due eligible nonzero target leaves within `maxRecoveryPaths` packets that may carry it, under arbitrary replenishment of the others. The rest stays queued | Receive owner, with at most one service-order fact per tracker | Capacity case in the selection test: X with Y; X with Y and Z; Y and Z refilled between calls while X waits |

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Write these 15 first:

1. A primary/backup receiver sends a due backup-path PATH_ACK as an ACK-only packet on the backup route, and the primary's packet doesn't carry it.
2. Path zero's ACK rides a nonzero carrier only when released, and the capacity rule holds. With path zero retired, carrier X's own target replenished between single-packet calls, and released target Y due, Y leaves within the bound. With two released targets Y and Z due, X, Y and Z each leave within `maxRecoveryPaths` packets. That still holds adversarially: refill Y and Z between calls while X waits, and X, Y and Z each still leave within the bound. That includes the case where X's own turn carries an earlier-waiting Y.
3. The carrier's own target still piggybacks on data, and a non-due home target is withheld from another path's data packet.
4. Each non-home state releases its target, as one table test including suspect path zero.
5. Each refusal owner releases its target into the same call, with no stale alarm, as one table test.
6. The turn packs acknowledgments, plus the periodic PING on its carrier's count:
   - nineteen consecutive turns on a backup are not ack-eliciting, and the twentieth carries a PING; data packets on the primary between them don't reset the backup's count, and an ack-eliciting packet on the backup does;
   - it never takes stream, retransmission or DATAGRAM payload, even with all three queued;
   - with or without the PING, it debits no pacing and charges the route's credit;
   - it returns `pathTurnSignaling`, so the selected route's supply observation is unchanged.
7. Alternation:
   - Under sustained receive on a backup, the selected route's pending acknowledgment and flow-control credit update still leave between turns.
   - The call after a turn clears the fact even when it ends hard-blocked or queue-full.
   - A threshold-queued home target that waited through that call is sent by its requested retry, with no alarm armed, on a call that doesn't end queue-full.
   - Sustained turns on a backup with no other ack-eliciting traffic, beyond `MaxTrackedSentPackets` packets, leave both paths sendable: the peer acknowledges each PING, and the backup's sent history stays within one PING interval plus what is in flight.
8. After a same-ID migration, the turn uses the current tuple.
9. An alarm-due acknowledgment leaves in its turn, carrying its own space's ACK Delay.
10. Two fork connections on the qualified linux/amd64 profile, with a Reno primary/backup sender whose data reaches both paths through existing public means, such as a local-use change that moves its primary, and a primary/backup receiver: each path's acknowledgments arrive on that path. The public gate refuses BBR multipath until [B6](#b6), and the private hook that selects the aggregate policy on a real connection belongs to [B5](#b5), so B7 adds neither. The sender half, that same-path feedback gives each BBR sender path valid rate samples, stays B4's existing aggregate fallback regression, and B5's arm E exercises the end-to-end BBR pair.
11. Reno two-path primary/backup transfer and failover are unchanged. With the peer idle on the backup throughout, the backup gains no packet, PTO, suspicion or liveness refresh from B7.
12. Ordinary connections are unchanged; the existing bundling test and the existing periodic-PING tests stay as they are.
13. History room: when a home path's return direction fails while it carries our data, its turns stop and fall back to the selected carrier within one PTO of the oldest unacknowledged record. The same holds when that record is the turn's own PING on a backup. At the boundary, a turn that would take the span to exactly 4,999 is allowed, and one that would take it past 4,999 falls back.
14. Bare-PING probe: a backup whose only outstanding ack-eliciting record is a real registered turn carrying PATH_ACK and PING, with stream data, a retransmission and a DATAGRAM queued, sends both probes of its PTO as PINGs with no payload. The second probe is sent while the first is still pending local completion, so it takes the nothing-extracted branch. The queued data stays queued and leaves on the primary. As negative cases, a PTO on the designated primary, and one on a backup holding outstanding stream data, keep today's packing.
15. Backup evidence, with the peer sending on a backup:
    - When the peer acknowledges the turn's PING, the backup's PTO attempts clear and its liveness refreshes. When the primary then fails, the backup is promoted without revalidation.
    - When the PINGs go unacknowledged, the third qualified PTO expiration makes the backup suspect. Its targets are then released, revalidation starts, and the healthy primary stays. Its `PATH_RESPONSE` recovers it as a backup.
    - When the peer stops sending right after a PING-carrying turn, the already-queued acknowledgment work drains and then no further turn or turn PING leaves on the backup. The outstanding PING keeps its consequences: a late acknowledgment refreshes liveness, or bare probes and qualified PTO expirations make the path suspect and start the existing per-candidate revalidation.

One focused race run covers the native-completion wakeup. The required independent multipath peer job on the exact PR head re-establishes the R17 interop matrix.

B7's PR lists only the owners and surfaces it changed: receive selection, packer threading and the per-carrier PING count, the emission turn and probe packing, the home accessor, and the two recovery reads. B5's existing candidate-specific applicability records decide which Reno and native evidence carries forward to its candidate, and charge any affected cell to the existing reserve. B7 adds no cell, campaign, budget or record.

**Context imports:** BBR and scheduling contract (feedback); Contract closure: BBR feedback (receiver ACK carriage and carrier-path sent-history rows); Program budget and prior-evidence validity; [Reno R6](2026-10-24-multipath-reno-plan.md#r6) carrier/target interface; the [Reno primary/backup and failure policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy), for the narrowed backup-traffic rule; B4's readiness and reservation agreement, for turn ordering. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:**

- *Split.* The strongest split is an inert prefactor that passes the carrier through with every target released, followed by the turn and home rule. It is rejected. The prefactor delivers nothing alone, and withholding a home target is safe only in the same PR as the turn that serves it and the alternation that bounds it. Authority completeness requires they ship together, and both fit one context.
- *Merge.* Merging all of B7 into B5 is rejected, because B5 freezes runtime policy and measures it. Only the applicability record merges: B5 already owns candidate-specific applicability records.
- *Edges.* B4 is a genuine blocker: the turn's place beside selection and the sender-half regression that item 10 relies on both come from B4. The per-carrier PING is part of this slice, so its turns need no prefactor to bound the sent history. B5 genuinely depends on B7, because without it arm E measures the cross-path feedback this contract excludes from support claims. B6 is transitive and needs no edge.

Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop.

**Stop:**

- The turn needs a scheduler, view or readiness change, a new wakeup or timer, new concurrency, or reselection inside the opportunity.
- Bounding turn recurrence needs more than the one alternation fact and the retry request of the call that clears it.
- The turn must become ack-eliciting beyond the per-carrier periodic PING, or B7 must add traffic on a backup beyond acknowledgments, that PING and its bare-PING probe.
- Stream, retransmission or DATAGRAM data reaches a backup through a turn or through a PTO that only PINGs armed.
- The per-carrier PING doesn't keep a turn path's sent history bounded, or needs a timer or a second counting rule.
- The backup's suspicion, liveness or promotion needs a rule change, rather than the existing policy acting on the PING's evidence.
- `ackTurnRoom` needs more than a read of recovery state, or turns can still fill the shared ceiling behind a pinned front.
- `onlyPingsOutstanding` needs more than a read of recovery state.
- Bounded service needs more than one service-order fact per receive tracker, or the bound can't hold under replenishment.
- The due read and the consumption can't share one predicate.
- The carrier capacity must grow beyond two targets.
- Any public API, configuration or wire change is needed.
- A second behaviorally distinct counterexample appears at the release or alternation rule.

## Outcomes closed with no code

<a id="b8"></a>

### B8 — Bound completed ACK-only sent history (closed with no code)

**Disposition:** dropped by [owner decision on 2026-11-13](https://github.com/the-sarge/quic-go-fast/issues/1356#issuecomment-6866681570). B8 #1356 closes as not planned, and no B8 code merges. It is no longer a slice, and nothing is blocked by it. This record replaces its earlier contract, including the text #1359 merged, and carries no normative B8 requirement.

**Rejected representation:** retire completed non-ack-eliciting records from a path's sent history while that path holds no ack-eliciting record, so that [B7](#b7)'s turns could stay never ack-eliciting. Four review counterexamples shared one root: a retired number leaves the sent history but still reaches acknowledgment, loss and ECN processing. The [fourth-counterexample stop](https://github.com/the-sarge/quic-go-fast/issues/1356#issuecomment-6866552161) and the [dispositions on PR #1360](https://github.com/the-sarge/quic-go-fast/pull/1360#issuecomment-6866551847) record the evidence, and PR #1358's comments record the earlier ones.

**Replacement:** B7's acknowledgment-only packets carry the periodic PING per carrier, and a PTO that only those PINGs arm sends a bare PING, under the [narrowed Reno backup-traffic policy](2026-10-24-multipath-reno-plan.md#primary-backup-and-failure-policy). The PING bounds each carrier's sent history as it bounds a single path today.

**Existing work:** #1359 merged and is superseded by this record. Draft PRs #1358 and #1360 and the unpushed commit `3b9de05aa` are abandoned by the owner decision and are no baseline.

**Revisit if:** the owner withdraws the bounded backup PING, or a later design keeps acknowledgment, loss and ECN processing whole for retired numbers.

## Acceptance and validation

The track is complete only when every slice's narrow end-to-end outcome, owner invariant, removal seam and named evidence are satisfied, with no open stop-for-decision. Universal statements range only over the binding runtime domain and the semantic classes owned by each slice; source inspection must map every supported producer/consumer to that owner. No unsupported platform/peer/provider claim follows from a passing fixture. Finite tests terminate as specified; missing evidence stays missing.

Default-disabled ordinary traffic, opted-in ordinary traffic and negotiated multipath are separate populations. Preserve unchanged default behavior with characterization/regression coverage; measure the changed opted-in ordinary cost against its explicit tolerances. Hosted repository workflows remain authoritative on the exact live PR head. A receipt for an earlier candidate or a mirror's plan SHA is not code-head certification.

A newly discovered untraced shared-state, concurrency, public API, failure, performance, security or dependency effect is a slice stop until scoped re-audit establishes that it fits the existing outcome and one-PR budget. Do not silently merge adjacent work. Public support and adoption claims require their distinct named gates; the owner permits exploratory implementation before consumer benefit, not unsupported claims.

A valid negative B5 result satisfies the evidence-delivery criterion; support enablement follows its result, not an all-modes-pass condition. Invalid or missing evidence remains blocking.
