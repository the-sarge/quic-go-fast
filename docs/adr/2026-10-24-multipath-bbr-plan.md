# BBR and aggregation implementation plan

**Status:** accepted design and audited slices; not implemented. Dispatch requires the verified default-branch publication and a frontier child issue. **Program:** `QGF-MP646-20261024`. **Track:** B, 3 of 3. **Date:** 2026-10-24. **Normative scope:** current outcomes, boundaries, invariants, evidence, blockers and stops. **Audit history:** [final consideration response](../audits/2026-10-24-multipath-646-final-consideration-response.md) and [slice audit](../audits/2026-10-24-multipath-646-slice-audit.md). **Parent issue:** pending.

## Binding contract and operating discipline

This is a current contract, not dispatch permission. Implementation occurs only when the operator dispatches a verified frontier child through `$implement-architecture-slice`. Follow that skill's shared review-loop and contract-closure baselines, composed with [the repository execution overlay](../REVIEW-LOOP.md). No repository closure overlay exists. Use the shared design-cost reference for scoped design choices. One fully briefed fresh review and at most one replacement govern each implementation PR; independently disposition findings and stop on accepted representation/ownership/boundary failures. Do not run an automatic fixer. Exact pushed-head local certification, applicable same-head hosted checks, squash merge, then append-dev-journal and tracking reconciliation are required. Diagnose failures before reruns.

The native endpoint ownership domain includes every listed ordinary QUIC v1/v2, negotiated multipath, control/stateless and public non-QUIC write on an exact granted native Linux amd64 UDP socket. The narrower negotiated multipath wire domain is opt-in draft-21 QUIC v1 with two live/validating paths. Existing parser/AEAD/configuration owners validate external representations; internal event constructors carry immutable identities. The runtime ownership and authority rules are universal within that domain. The evidence below is finite regression/qualification evidence, not a completeness proof. Unknown representations, unqualified platforms and excluded configurations cannot acquire support by passing an example.

Preserve [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0004](0004-packet-emission-ownership.md), accepted BBR/ECN decisions, default Reno spurious-loss undo and adaptive-loss contracts. Do not introduce custom I/O providers/wrappers, restricted/network-admitted/candidate-group or send-thread-QoS combinations, multipath v2, FEC, duplicate DATAGRAM delivery, public scheduling callbacks, arbitrary server socket association, coupled congestion control or address discovery. Ordinary unregistered endpoints preserve their existing contracts. Local queue/native timeouts remain nonfatal backpressure for ordinary and multipath peers on granted endpoints. A qualified-native contract violation faults/quarantines the endpoint without freeing a live borrow.

**Existing-work disposition:** new runtime slices; no unmerged implementation is accepted as a dependency. Retain exploration/sizing as history; the linked slice audit records disposition of prior documents. Q2 #599 remains an external unfinished qualification slice; no result, exception or execution readiness transfers to this program.

**Evidence discipline:** write the named failing or characterization cases first. For runtime slices, at most 16 new deterministic scenario tests (R2/R16 at most 20) cover the listed semantic families, one positive per behavior and one negative per distinct supported failure owner. Run affected-package tests and vet; Go/source or dependency changes also require `go mod tidy -diff`. One focused race run is budgeted where the slice crosses execution contexts; no repeated timing stress is implied. At most one discriminating central-guard mutation per owner is allowed only if needed to prove inherited coverage reaches that guard. No recursive fixture-completeness obligation, syntax-variant matrix, new platform or additional operational campaign enters through review. The exact operational inventory/ceiling is below. If the accepted family cannot fit, stop and re-audit rather than silently grow the slice.

**Authority and preservation:** there is no persisted schema or restart round trip in this library program; each new runtime fact must cover construction, validation, all listed destructive/security consumers, cancellation/close and fresh-instance reconstruction in the same slice. A private activation gate is a transitional seam, not permission to negotiate incomplete protocol behavior with arbitrary peers. Intermediate fixtures may activate only their declared complete scenario; ordinary public construction remains unchanged. Do not let a prefactor change controller meaning or ordering. Never widen a temporary seam; only its named removal slice may delete it after every migration dependency is complete.

**Artifact classification:** runtime mechanisms are shipped behavior; native authority, CID/AEAD, generation, resource and disposal guards are required safety enforcement. Unit/integration tests, traces and bounded fixtures are verification aids. The regression coverage named by these accepted safety contracts and the pinned required peer job are maintained blocking deliverables: payoff is catching ownership/wire regressions, domain is the declared profile and semantic cases, owner is the transport test suite, and retirement requires equivalent replacement or feature removal. Qualification receipts, plans, journals and issue/task state are process metadata. No extra analyzer/harness becomes blocking by implication.

**Contained-design comparison:** changing the owner to do synchronous native I/O would remove a handoff but violate the accepted responsive connection loop. Requiring all PacketConn implementations to cancel would change a closed compatibility contract. Retain one endpoint control/writer boundary, with connection-owned policy and fact-only completion. Do not duplicate protocol decisions in route adapters. Revisit only if those public/responsiveness contracts are explicitly changed; all other rejected slice split/merge alternatives below have `none: rejected on merit` unless their stated context or contract stop is reached.

## Goal and current shape

Reuse the complete Reno multipath identity/lifecycle seam for BBR, then evaluate one private aggregation policy. A completed BBR single-path Q2 receipt is a prerequisite, not certification of this candidate. Reno stays default.

Verified source seams: `internal/ackhandler/congestion_dispatch.go` couples delivery sampling to feedback; `internal/congestion/feedback.go:70` permits invalid delivery samples and `:138`/`:150` distinguish RTT evidence; retained/recovery indexes and `bbr_ecn.go` own controller history and CE accounting. They currently share one active path context. The path registry and endpoint contracts from the preceding tracks supply identity and lifetime, not BBR model policy.

## BBR and scheduling contract

BBR integration follows the complete Reno-first ownership model and the existing BBR qualification gate. Each path owns its controller, sampler, pacing, loss and ECN state; shared application delivery remains connection-owned. A tuple migration resets the appropriate RTT/controller/PMTU context without resetting its wire PN sequence or cumulative ECN accounting. Preserve old-generation evidence and the accepted ECN accounting fence before revalidation. No aggregate-controller shim or silent Reno substitution is permitted.

Only same-send/return-path ACK evidence trains a path's propagation-RTT and delivery-rate model initially. A cross-path ACK still retires packet flight/logical obligations and updates loss/recovery facts, but does not create a new bandwidth or min-RTT sample. Keep conservative PTO timing from the last valid path-local RTT or the standard initial RTT while that evidence is absent; request/prefer same-path feedback without requiring peer compliance. A controller can therefore remain supply/evidence limited on a peer using only cross-path ACKs. Cross-path-only feedback gets a deterministic correctness regression, not a throughput-support claim in the accepted campaign. Count delivered bytes once independently of sampling eligibility; invalidate rate intervals that bridge excluded cross-path feedback and require a fresh path-local anchor before the next valid rate sample. Never let an excluded ACK re-enter a later bandwidth sample through cumulative delivered counters.

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

The fixture-only byte-round-robin comparison is a maintained verification aid solely for the accepted aggregation comparison: payoff is discriminating scheduler benefit, domain is the three fixed topologies/six modes, owner is the transport integration suite, retirement is completion of this one qualification decision or feature removal. It is not a public option or a perpetual tuning program. No cross-path-only-feedback throughput claim is included.

## Program budget and prior-evidence validity

B5 uses 54 of the shared 118 regular run instances. The [Reno program ledger](2026-10-24-multipath-reno-plan.md#program-qualification-ledger-and-sequencing) owns the cumulative 180-run/eight-host-hour ceiling and execution preflight. Inspect B1–B4 changes against prior native/ordinary evidence. Unchanged owners/domain may retain receipts with a source-based applicability record; changed behavior needs the affected existing cases rerun within the same ceiling. Do not automatically replay an entire matrix or inherit a stale result. If more scope/budget is necessary, stop for a decision.

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
| Remaining reserve | Cause-recorded failed attempts, affected existing native/ordinary cells or necessary repeats; never new semantic cells or pass-seeking retries | 40 instances maximum |

The regular inventory remains 118; the planned four early smokes plus 18 BBR revalidation observations make 140 scheduled instances, leaving at most 40 inside the cumulative 180-run/eight-host-hour ceiling. A "run instance" is one candidate/arm observation; paired ordinary profiles consume two, and a control warmup within an observation is included in its duration, not hidden extra capacity. Charge host-hours as summed occupied host time, including setup and failed attempts. Preflight must pin candidate/comparison SHAs, dependency/container/kernel/toolchain identities, workload seeds, remaining run/host-hour budget and per-invocation timeout; do not start an invocation whose maximum reserved cost exceeds either remainder.

The 18 listed repeats are required BBR-candidate evidence, not optional reuse of the Reno receipt. Revalidate any additionally affected existing native/ordinary cell within the 40-run reserve with its concrete source-change reason; unchanged owners/domain may retain an earlier receipt only through an explicit applicability record against the current candidate. If changed semantics require more or different evidence than the fixed inventory/remaining budget, stop for a scoped decision. BBR primary/backup support needs its own complete correctness and the applicable/revalidated native, interop and ordinary-cost evidence; aggregate support additionally needs arm E's comparative pass. Q2 evidence never automatically transfers. No execution is authorized by this ledger.

## Slice graph

| Slice | Disposition | Delivery | Blocked by |
| --- | --- | --- | --- |
| [B1](#b1) | new | Partition BBR delivery sampling and send credit | R19 |
| [B2](#b2) | new | Preserve BBR ECN, recovery and migration evidence | B1 |
| [B3](#b3) | new | Exercise BBR primary/backup through complete lifecycle | B2 |
| [B4](#b4) | new | Add the private aggregate scheduling policy | B3 |
| [B5](#b5) | new | Measure aggregation benefit and shared-bottleneck cost | B4 |
| [B6](#b6) | new | Publish the supported BBR and aggregation outcome | B5 |

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

**Blocked by:** B2. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection policy stays controller-agnostic; BBR adapter owns controller construction and disposal. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Public BBR+multipath remains disabled until B6; no duplicate API or alternate timeout policy.

**Blast radius and context:** congestion_control.go, connection.go, internal/ackhandler construction; integrationtests/self. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

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

**Temporary seams:** Both policies remain privately selected until B6. Fixture comparison never becomes a public maintained policy.

**Blast radius and context:** private scheduler, packet_emission.go, datagram_queue.go; comparison fixture. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: balanced rates; asymmetric delay/rate; no eligible rate; pacing/credit/PMTU constraints; backup exclusion; deterministic ties; no destructive pop before selection; Reno without estimator allocation; controller-specific no-sample fallback. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** BBR and scheduling contract (scheduling); Contract closure: BBR feedback; Reno packing/PMTU eligibility interface receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Production selection and a tiny comparison implementation fit one slice; splitting evidence driver is B5. B3 supplies valid BBR inputs and complete lifecycle. A public scheduler would add forbidden obligations. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Selection needs live protocol pointers or new public callbacks, duplication or coupled congestion control.

<a id="b5"></a>

### B5 — Measure aggregation benefit and shared-bottleneck cost

**Delivers:** Run the accepted three topologies and six modes, report useful STREAM/DATAGRAM outcomes separately and test owner aggregation/coexistence thresholds. Revalidate changed ordinary/native surfaces without silently inheriting earlier evidence; all repeated cases consume the same remaining program budget. A complete valid positive or negative comparison completes this evidence slice. Negative benefit/coexistence results deny the affected aggregate support claim; they are distinct from incomplete or invalid evidence. Use the six named controller-matched arms, required 18 existing-cell BBR revalidation observations and candidate-specific applicability records in the single cumulative ledger.

**Blocked by:** B4. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Transport integration suite owns matched workload/ledger and support recommendation; runtime policy is frozen. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** None. A failed aggregate comparison leaves aggregate unsupported; it cannot silently choose another algorithm.

**Blast radius and context:** aggregation fixture and receipt; shared qualification ledger. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Example-level evidence over the fixed scenario/toolchain domain, parsed by the pinned peer/fixture's existing protocol implementation. This is the explicitly maintained verification deliverable described above; receipts are metadata. Runtime closure is not recursively imposed on the fixture. Missing/incorrect case accounting is a failed claim, not evidence of product failure.

**TDD / evidence budget:** 54 regular instances: three topologies, six modes, three observations, 60 seconds after 10-second warmup; any justified affected-case repeat charged inside 180/eight hours; valid negative result completes the evidence delivery and denies aggregate support. Build focused driver/accounting regressions first; operational attempts charge the single program ledger. Stop after the fixed cases or first unresolved prerequisite/budget failure; no repeat-until-pass.

**Context imports:** Qualification and adoption policy; Program budget and prior-evidence validity; Reno program qualification ledger. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** One fixed comparison answers the adoption question; no open-ended tuning loop. B4 supplies both selected and fixture-only policies. Do not merge production enabling. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Comparison is incomplete/invalid, prerequisites are missing, newly required semantic scope exceeds the contract or the remaining operational budget is insufficient. A valid threshold failure is a completed negative capability result, not this stop.

<a id="b6"></a>

### B6 — Publish the supported BBR and aggregation outcome

**Delivers:** Enable BBR primary/backup only when its correctness/native/ordinary cost evidence is current. Enable aggregate only on passing its accepted comparisons; otherwise document unsupported aggregate without implementation substitution. Publish exact tested domain and adoption evidence, delete private activation selectors and keep Reno default. Consume B5's valid result even when negative: publish the qualified primary/backup outcome and explicit unsupported aggregate state, without changing the algorithm or thresholds. Publish only combinations supported by matching evidence: Reno primary/backup from R19, qualified BBR primary/backup, and BBR aggregate only on comparative pass. Reno aggregate remains unsupported.

**Blocked by:** B5. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Common capability gate owns supported combinations; controller/scheduler selection does not bypass route or platform qualification. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Remove B-track private activation; no dormant alternate public policy or continuing tuning program.

**Blast radius and context:** config.go, congestion_control.go, API/support documentation; fixture activation cleanup. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: enabled/unsupported combinations match evidence; default Reno unchanged; public smoke; absent private bypass; receipt identity/support statement check. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Qualification and adoption policy; Program budget and prior-evidence validity; B5 capability result and evidence applicability receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** A distinct enablement PR keeps failed evidence from granting support. B5 supplies final claim boundaries and affected-case evidence. Further splitting would separate one capability table from its documented contract. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Evidence cannot support the exact enabled combinations or owner thresholds need relaxation.

## Acceptance and validation

The track is complete only when every slice's narrow end-to-end outcome, owner invariant, removal seam and named evidence are satisfied, with no open stop-for-decision. Universal statements range only over the binding runtime domain and the semantic classes owned by each slice; source inspection must map every supported producer/consumer to that owner. No unsupported platform/peer/provider claim follows from a passing fixture. Finite tests terminate as specified; missing evidence stays missing.

Default-disabled ordinary traffic, opted-in ordinary traffic and negotiated multipath are separate populations. Preserve unchanged default behavior with characterization/regression coverage; measure the changed opted-in ordinary cost against its explicit tolerances. Hosted repository workflows remain authoritative on the exact live PR head. A receipt for an earlier candidate or a mirror's plan SHA is not code-head certification.

A newly discovered untraced shared-state, concurrency, public API, failure, performance, security or dependency effect is a slice stop until scoped re-audit establishes that it fits the existing outcome and one-PR budget. Do not silently merge adjacent work. Public support and adoption claims require their distinct named gates; the owner permits exploratory implementation before consumer benefit, not unsupported claims.

A valid negative B5 result satisfies the evidence-delivery criterion; support enablement follows its result, not an all-modes-pass condition. Invalid or missing evidence remains blocking.
