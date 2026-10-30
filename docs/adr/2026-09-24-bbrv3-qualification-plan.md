# Qualification readiness and evidence implementation plan

**Date:** 2026-09-24
**Status:** Q1 complete; full Q2 deferred by owner decision on 2026-10-30, incomplete and not an application-default prerequisite
**Track:** Q in `QGF-BBR3-20260924`
**Depends on:** Slice edges below; no implicit track-wide dependency
**Related:** [Program](2026-09-24-bbrv3-program.md), [accepted design](../designs/bbrv3.md), ADRs [0001](0001-upstream-compatibility.md), [0002](0002-adopt-through-module-replacement.md), [0004](0004-packet-emission-ownership.md), [0007](0007-managed-ecn-qualification.md), [0009](0009-opt-in-bbrv3.md)
**Normative scope:** Current outcome, boundaries, invariants, evidence, blockers and stop conditions
**Audit history:** [Handoff audit](../audits/2026-09-24-bbrv3-handoff/README.md)
**Parent issue:** [#586](https://github.com/the-sarge/quic-go-fast/issues/586); execution child [#599](https://github.com/the-sarge/quic-go-fast/issues/599)

## Owner amendment — GridCast application-default prerequisite

On 2026-10-30 the owner explicitly replaced full Q2 as the GridCast app-default prerequisite with a [bounded application release gate](https://github.com/GridCastIO/gridcast/blob/dad90224fd38f9b8e132dc26c95130eb0eb59840/docs/adr/2026-10-30-bbr-app-default-release-gate.md). Q2 is deferred, not passed or retired as evidence. Preserve its 520-case inventory, candidate/reference choices, prior charges, operational receipts and unresolved results for any separately authorized resumption. Neither Q2's staged pilot nor its billing/resource reconciliation is a prerequisite to the smaller application gate. The historical dispatch conditions below apply only if full Q2 is resumed; they do not reactivate it.

The application gate uses released fast.9 and actual GridCast transfers, including a limited coexistence screen informed by the adverse #808 result. It does not qualify this fork generally, transfer old readiness exceptions, change algorithm/ECN/ownership contracts, or make Q2's different selected candidate an app dependency. Reno remains the fork default and BBR remains experimental and generally unqualified. A later scoped GridCast default is its owner's release decision under the replacement plan. No execution, spending, ledger reset or guard change is authorized here. #619/#645 retain their existing deferred scope and are not application-default prerequisites.

## Goal

A bounded native campaign with honest prerequisites, unchanged accounting and a human adoption decision; no consumer edits or rollout.

## Historical preparation baseline (verified 2026-09-24)

The existing benchmark is a loopback transfer fixture (`integrationtests/self/benchmark_test.go:1`), not proof of a calibrated native impairment topology. The accepted campaign file and counted JSON inventory specify 460 core runs/36 hours, 60 completion runs and the 48-hour ceiling; they explicitly leave actual hosts, emulator/tool choice and runnable commands to preparation. No available-host or successful-campaign claim is made by this handoff. Source anchors use the inspected code tree named in the audit; verify semantic seams at dispatch rather than treating moving line numbers as authority.

The paragraph above records original preparation inputs. Q1 completed in [#642](https://github.com/the-sarge/quic-go-fast/pull/642); its frozen artifacts retain their original limits and observations. The active budget below supersedes earlier ceilings without rewriting those records.

## Current dispatch status and evidence gates

Q1 (#598) and B6 (#597) are complete; the original slice dependencies are satisfied. Full Q2 qualification remains outstanding. Comparative readiness/coexistence evidence exists from [#808](https://github.com/the-sarge/quic-go-fast/issues/808), and fast.8 gates [#871](https://github.com/the-sarge/quic-go-fast/issues/871), [#887](https://github.com/the-sarge/quic-go-fast/issues/887) and [#990](https://github.com/the-sarge/quic-go-fast/issues/990) are complete. These revision-specific records do not complete the accepted 520-case Q2 campaign or establish native qualification across platforms. Reno remains the default; BBRv3 remains experimental, opt-in and unqualified.

The [October 18 sequencing decision](https://github.com/the-sarge/quic-go-fast/issues/599#issuecomment-6253457972) and [October 21 readiness requirement](https://github.com/the-sarge/quic-go-fast/issues/599#issuecomment-6319172869) remain governing constraints. Closed prerequisite issues do not select a Q2 candidate or certify current `main`. Before restoring execution readiness, publish and review a bounded execution preflight resolving:

1. **Candidate and references.** Pin the candidate, controller identities, fixture/tool versions and blocking versus descriptive comparison references. [#887's decision](https://github.com/the-sarge/quic-go-fast/issues/887#issuecomment-6354232820) names bundle Reno `8095c72244755f0c1e24f4158253bf09cfbe6e17` as blocking and old Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` as descriptive for bundle readiness. Preserve that scope; a different Q2 reference needs an explicit decision. The [October 28 preflight decision](2026-10-28-bbrv3-q2-execution-preflight.md#accepted-operator-decisions) selects candidate `23c83b9896980511286ca8b81cd632f7ec56afed`: the 520 qualification cases compare its BBR and Reno, while prerequisite readiness uses bundle Reno `8095c72244755f0c1e24f4158253bf09cfbe6e17` as blocking and old Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` as descriptive. This transfers no historical exception or advancement and authorizes no extra cases. [#971](https://github.com/the-sarge/quic-go-fast/issues/971) changed shared recovery after fast.8 and was not measured by #990.
2. **Applicable readiness and advancement.** Map the selected revision and each native platform to required readiness evidence and its advancement disposition, retaining the adverse L6 coexistence result and all raised flags. [#990's decision](https://github.com/the-sarge/quic-go-fast/issues/990#issuecomment-6357898036) permits publication of the exact fast.8 candidate on its Linux evidence; it grants no Q2 qualification, macOS/Windows advancement or transfer to a different revision or later measurement. Its fixture did not exercise the native batch cap or gluing. Exceptions remain confined to their recorded cells, magnitudes, revision and platform. Resolve missing requirements with bounded evidence and the applicable decision before execution; this document waives no gate and launches no readiness work.
3. **Inventory and fixture coverage.** Reconcile the accepted 460 core and 60 completion cases against the selected sender, default Reno, shared recovery and packet-I/O paths. Retain required platforms, workloads, seeds, pair ordering, timing, competing-flow regimes, ECN distinctions and useful-delivery accounting. Record each endpoint's ACK mode/threshold and affected peer controller; fast.8's local ACK API does not authorize a BBR opt-in or an ACK-mode sweep. Do not count historical readiness observations as Q2 cases or silently add, remove or replace cases. If the inventory cannot execute as specified within the remaining budget, return a scoped decision instead of weakening the contract.
4. **Resources, clocks and accounting.** Refresh native hosts, isolation/contention, capabilities, tools, emulator calibration, clock validity, cleanup ownership and runnable commands. Reconcile cumulative charges against authoritative receipts and the ledger, then verify the effective controller guard before paid launch. Report actual remaining allowances separately from forecasts; old receipts do not establish current availability or credit.
5. **Authority and dispatch.** Retain this slice's one-owner, one-PR outcome, evidence/context ceilings and hard correctness gates. Link the accepted preflight, applicable operator decisions and exact merged plan revision from #599 before restoring execution readiness. Unresolved candidate/reference choices or missing authority remain pending. The operator separately chooses dispatch. This reconciliation launches no campaign, provisions no resource, changes no live ledger/guard and authorizes no additional spending or cases.

[#619](https://github.com/the-sarge/quic-go-fast/issues/619) remains supplemental work after Q2 and program #600 complete, not a qualification blocker or a draw on this budget. It remains the evidence gate for [#645](https://github.com/the-sarge/quic-go-fast/issues/645), subject to the explicit owner override recorded there. Shipping #856's local receiver mode discharges neither issue.

## Active budget authority

The [September 26 amendment](https://github.com/the-sarge/quic-go-fast/issues/599#issuecomment-5846546900) authorized $150 cumulative cloud cost and 96 cumulative experiment-hours. The operator's [October 28 reconciliation](2026-10-28-bbrv3-q2-execution-preflight.md#accepted-operator-decisions) explicitly retains **$200 cumulative cloud cost and 192 cumulative experiment-hours**, reconciling the historical hour increase recorded by [infra #671](https://github.com/the-sarge/infra/pull/671) and the separately disclosed $200 ledger claim that the operator confirmed during preflight review. The separate **72 cumulative preparation-hour** ceiling is unchanged. These are total ceilings, not fresh allowances. All prior usage, failures, failed starts, unused reservations, launch and teardown remain charged under the existing accounting contract. This reconciliation resets no budget, transfers no credit from another campaign and grants no fresh allowance or dispatch.

The inventory remains **460 core plus 60 completion cases**, with the existing repetitions and timing. Q1's frozen forecast of 69.984667 experiment-hours and $96.083333 is historical, not remaining credit. The [October 28 preflight](2026-10-28-bbrv3-q2-execution-preflight.md#accounting-and-effective-enforcement) found at least 79.534667 experiment-hours and $87.085 in retained reservations, before unreconciled later usage/billing; its old-rate full-inventory warning estimate is about 124.801334 hours/$153.148333 before fresh readiness or later charges. That fits the amended ceilings arithmetically but is not a current forecast or available credit. Neither figure authorizes launch or additional spending. Preserve frozen receipts and their original limits. Before launch, reconcile the current ledger and remaining forecast and verify that the merged, staged infrastructure controller enforces the approved ceilings while retaining prior charges. The preflight observed 192/$150/72 source guards in the staged controller and current infra main. The cloud guard remains below the reconciled $200 authority: separately authorize and verify any guard alignment before a launch relies on the higher cap. Effective launch inputs, complete accounting and current resources still require verification. No live guard or ledger was changed by this reconciliation. Missing accounting, an incompatible guard or insufficient remaining budget prevents launch pending resolution; correctness and evidence requirements are not reduced to fit.

## Decision and existing-work disposition

The [design specification](../designs/bbrv3.md) is normative for algorithm/interface details; this plan is normative for slice boundaries, ownership, evidence and dispatch. Their contracts compose, with no implied permission to weaken either. Retain the accepted design decisions, completed T/B and Q1 slices, and their historical evidence. Reconcile subsequent production changes and operator decisions through the current execution gates; do not treat the original handoff's candidate assumptions as current certification.

**Rejected alternatives (do not do this):** Do not expose incomplete BBR under a public selector, copy a TCP/QUICHE controller wholesale, move registration/model ownership to workers, infer marking from packet-number interval alone, or alter Reno to simplify a BBR adapter. Do not create a global controller switch, new wire parameter, raw ECN authority, unbounded evidence store, fleet purchase, cross-repository implementation or rollout plan.

**Non-goals:** Changing the default controller; GridCast/wiremux implementation; new platform ECN capability; external controller plugins; unlimited tuning/research; native-performance claims before the accepted campaign; widening the Config positional-literal exception accepted in ADR 0009.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| [Q1](#q1) | complete | Establish the bounded native campaign prerequisites | None | None; see slice budget |
| [Q2](#q2) | deferred; not an app-default prerequisite | Run the accepted qualification campaign and publish evidence | Q1, B6 | None; see slice budget |

## Operating discipline

The shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` govern. Their local sources are `/Users/josh/.dotfiles/agents/.agents/skills/_shared/REVIEW-LOOP.md` and `/Users/josh/.dotfiles/agents/.agents/skills/_shared/CONTRACT-CLOSURE.md`; the repository [execution overlay](../REVIEW-LOOP.md) adds only the current upstream-style hosted-CI, local certification, squash merge and post-merge journal requirements. Do not copy shared policies into this repository or invent `task preflight`/`ci-*` jobs.

For each slice, use one fresh fully briefed review and at most one replacement under the shared finding-disposition/approach-stop rules. Quote its current criteria, domain, guarantee, artifacts and finite evidence budget. Independently disposition findings; no automated fixer. Apply the exact invariant-and-owner comparison before calling findings repeated roots, including verification history. One scope-appropriate local race run is required where this slice changes concurrent queue/connection behavior, not a new repetition campaign. Diagnose failures before reruns. After accepted fixes, certify the exact pushed head, require same-head applicable hosted checks, mark ready and squash with a head guard; append the journal only after merge, then reconcile native blockers/readiness and complete the matching OmniFocus slice task.

Test functions are an upper budget for new focused functions, not a demand to duplicate existing coverage. Each named table has one representative positive per behavior and one negative per materially distinct failure; no full Cartesian product. A guard mutation is optional only where explicitly named and must be one discriminating bypass per central owner, reverted before commit. All other mutation budgets are zero. No extra fuzz/security/platform/timing/repetition expansion is implicit. Verification aids receive proportional review and no recursive closure obligation. Repository-provided hosted matrix remains mandatory; it is not replaced by these local budgets.

The [Q1 budget proposal](../audits/2026-09-25-bbrv3-q1-campaign/budget-proposal.md) and [Infra PR #643](https://github.com/the-sarge/infra/pull/643) record historical preparation and guard decisions. Apply the active budget authority above to future execution; older ceilings and forecasts are not current credit. For Mac endpoints, the operator pauses available user/sync workloads and accepts remaining macOS media analysis/indexing with recorded CPU contention (clarified 2026-09-26). GOMAXPROCS=4, identical Reno/BBRv3 settings and separate gateway resources remain required. Neither physical-core isolation nor efficiency-core placement is established; recheck availability and contention at execution.

## Common representation, artifacts and context rules

The actors are ordinary callers selecting a built-in controller, connection-owned transport processing, a concurrent local worker and a peer whose packets pass the existing authoritative QUIC parsers. Malformed wire syntax remains internal/wire/handshake validation's responsibility; these slices do not implement a new parser or prove peer honesty. Typed internal invariants are enforced over their declared supported domain; terminating tests are finite evidence rather than an exhaustive proof. Unknown or old evidence cannot acquire authority. No new persisted runtime schema is introduced; constructor/copy, path restart and destructive-consumer closure replaces database migration obligations.

Runtime behavior is shipped behavior; generation, bounds, validation and lifetime checks are required safety enforcement. Tests, deterministic traces and temporary fault injectors are verification aids; none is an approved maintained general-purpose product or an excuse to expand the shipped contract. Plans, receipts, context manifests and task pointers are process/traceability metadata. Native campaign-only aids are frozen evidence under the accepted campaign, not an independently maintained emulator/verification framework. If a new maintained aid is necessary, stop and declare payoff, supported domain, owner, retirement policy and budget before putting it on the critical path.

Every slice receives only its current contract, this plan's common sections, named design sections, required ADR paragraphs, named source ranges, relevant existing test declarations and any bounded governing diff. The [audit input inventory](../audits/2026-09-24-bbrv3-handoff/slice-inputs.json) records the initial source ranges and proposed tests. At dispatch resolve named predecessor-owned functions in the merged tree; use a semantic symbol map after line drift, never entire historical plan versions. Limit initial input to 35,000 estimated tokens (`max(bytes/4, whitespace_words*2)`) and relevant unresolved review/history to 3,000 within that ceiling, leaving at least 65,000 of a 100,000-token fresh context for implementation, review fixes and verification. Stop before coding and re-slice if a measured bounded manifest exceeds the ceiling. A predecessor's whole diff is not automatically dispatch input. New tests are implementation output, not initial context.

B6 completed the public opt-in selector after the original private T/B slices; ordinary client/server constructors retain Reno. Public availability and fast.8's explicitly scoped experimental release do not establish qualification or consumer adoption. Q2 must identify and assess the complete selected production controller, including changes after the original handoff, without treating earlier private-slice or release checks as its comparative evidence.

## Implementation slices

<a id="q1"></a>

### Q1 — Establish the bounded native campaign prerequisites

**Completion:** [PR #642](https://github.com/the-sarge/quic-go-fast/pull/642); [native readiness receipt](../audits/2026-09-25-bbrv3-q1-campaign/readiness-receipt.json). Q1 satisfied the original preparation dependency; no comparative campaign was run by Q1. Its receipt does not discharge the current Q2 execution gates above.

**What it delivers:** Within the operator-extended 72 preparation hours, inventory usable native platform pairs/resources and existing fixture/emulator capabilities; pin a runnable campaign manifest with source/host/tool versions, calibrated command choices, ownership and expected runtime. Select the L3 fixed queue basis and final packet-rounded capacities. Publish one readiness receipt and finite missing-prerequisite list if not ready; do not pretend unavailable hosts, calibration or missing tooling exist. This slice completes only when the core campaign is runnable inside its accepted budget.

**Existing-work disposition:** New slice. The unmerged design is reworked documentation only; no implementation is assumed.

**Blocked by:** None.

**Single owner after merge:** The preparation agent owns the frozen manifest/receipt; runtime/emulator truth comes from native tools and observed host capabilities, not plan labels.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** None. This creates campaign traceability, not a second runtime configuration authority or maintained general-purpose analyzer.

**Blast radius:** Native host availability/isolation, fixture useful-delivery counters, directional queue/rate/mark calibration, finite resources and budget. No BBR code, fleet purchases, credentials sharing, consumer changes or production traffic. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** This slice changes process/traceability records and uses campaign verification aids only; it ships no controller behavior. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** The existing acceptance manifest/schema as a planning inventory plus tool-produced host/calibration facts. Readiness is example-level evidence for declared hosts/configurations; no universal platform qualification claim. Guarantee level: example-level evidence for the explicitly declared environment.

**Contract closure:** Not triggered for this slice: this is finite readiness/measurement evidence, not shipped safety authority; do not recursively impose closure on campaign aids.

**Evidence budget:** Apply the active budget authority above: 192 cumulative experiment-hours including failed runs, launch and teardown, $200 cumulative cloud cost, and the separate 72 cumulative preparation-hour ceiling. Retain prior charges and the accepted 460 core plus 60 completion cases. Q1 completed under its recorded historical ceilings; preserve its frozen evidence. Reconcile actual usage, remaining forecast and the enforced guard before launch; historical forecasts are not available credit. No additional cases are authorized here. Required safety validation cannot be demoted on budget exhaustion. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `Receiver counter integrity/duplicate fixture check`; `Directional rate/RTT/finite-queue/CE calibration`; `Native pair and CPU-isolation inventory`; `Experiment ledger arithmetic and command-cost check`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Qualification and budget”, “Consumer adoption and diagnostics”; the source ranges and test inventory keyed `Q1` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: Splitting by host before discovering actual availability would invent independently runnable work; keep one bounded readiness inventory. Adjacent merge: Merging execution would hide the preparation-budget stop and prevent early reporting of unavailable infrastructure. Dependency evidence: No blocker: readiness and Reno fixture inspection can run while implementation is developed. A BBR binary is not required to determine infrastructure readiness.

**Stop conditions:** Stop at 72 cumulative preparation hours with an explicit gap if usable hosts, existing tooling or a runnable manifest are missing. Do not build a new maintained emulator/runner or enlarge the budget; return the missing prerequisite for a scoped handoff or operator resource decision. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

<a id="q2"></a>

### Q2 — Run the accepted qualification campaign and publish evidence

**Current state:** Deferred by the October 30 owner amendment; incomplete and not an app-default prerequisite. Only a separate decision to resume full Q2 reactivates its dispatch gates and cumulative-budget reconciliation.

**What it delivers:** Run the accepted 460 core and 60 completion cases with the complete candidate, native platform pairs and receiver-defined useful delivery, respecting one experiment ledger and hard correctness gates. Publish immutable raw records, per-scenario comparisons, advisory flags, limitations and an adoption decision packet. Produce evidence only: no default switch, consumer rollout or automatic adoption decision.

**Existing-work disposition:** Retain the accepted Q2 outcome, inventory and frozen Q1 preparation as provenance. Account for the shipped BBR port, later recovery/default-Reno changes and revision-specific readiness decisions through the execution preflight. Prior readiness records are evidence to assess, not substitute Q2 runs or implicit qualification.

**Blocked by:** Q1 and B6 are complete. The current execution gates above and separate operator dispatch remain; closed native dependency edges do not certify those conditions.

**Single owner after merge:** The campaign runner/agent owns the ledger and immutable receipt; native endpoints own observed useful delivery, with the declared clock contract. The owner retains adoption authority.

**Authority completeness:** This delivery includes its construction/test activation, accepted input validation, reset/restart and terminal consumers. No newly authoritative runtime fact is left for a successor to make safe. Any successor adds a new behavior through the established owner, not a repair for missing authority closure.

**Transitional-seam budget:** None. Frozen experiment aids/results are retained evidence, not shipped runtime or a recursively certified benchmark product.

**Blast radius:** 192 cumulative experiment-hour and $200 cumulative cloud ceilings, the separate 72 cumulative preparation-hour ceiling, five paired repetitions, native platform/offload/ECN distinctions, host contention, control latency and resource cost. No extra statistical/platform cross-product or unbudgeted sensitivity. No untraced effect is accepted; newly discovered effects outside this boundary invoke the stop conditions.

**Artifact classification:** This slice changes process/traceability records and uses campaign verification aids only; it ships no controller behavior. Common aid restrictions apply; no maintained-aid exception is granted.

**Representation contract:** Accepted named model scenarios and pinned native endpoint/emulator configurations. Results are example-level modeled-path observations; no claim to certify real satellite/cellular services or all WANs. Guarantee level: example-level evidence for the explicitly declared environment.

**Contract closure:** Not triggered for this slice: this is finite readiness/measurement evidence, not shipped safety authority; do not recursively impose closure on campaign aids.

**Evidence budget:** Apply the active budget authority above: 192 cumulative experiment-hours including failed runs, launch and teardown, $200 cumulative cloud cost, and the separate 72 cumulative preparation-hour ceiling. Retain prior charges and the accepted 460 core plus 60 completion cases. Q1 completed under its recorded historical ceilings; preserve its frozen evidence. Reconcile actual usage, remaining forecast and the enforced guard before launch; historical forecasts are not available credit. No additional cases are authorized here. Required safety validation cannot be demoted on budget exhaustion. One fresh review and at most one replacement. Terminate when the named evidence, scope-specific local gates and same-head hosted CI pass with no unresolved stop-for-decision.

**TDD and preservation evidence:** First write characterization/failing cases for: `Pinned required correctness/calibration commands before comparative runs`; `460 core runs with original pair/seed/timing contract`; `60 completion observations including timeouts`; `Per-run integrity/controller/ECN/resource record and global ledger`. Use existing real-packer and recovery fixtures rather than mocks of production decision logic. Preserve default Reno, protocol parsing, payload/buffer ownership and existing platform capability boundaries on every changed surface.

**Dispatch context budget:** This slice plus common sections; design sections “Qualification and budget”, “Consumer adoption and diagnostics”; the source ranges and test inventory keyed `Q2` in the audit input inventory; only relevant predecessor-owned declarations and focused test declarations; zero unresolved implementation review history at publication. Initial input ceiling 35,000 tokens with 3,000 maximum for relevant history inside it. No whole historical reports. The bounded producer/consumer set and one owner leave the remaining context for implementation and review.

**Slice decision audit:** Further split: The accepted cumulative ledger and matched comparisons span platforms; splitting execution would require a new budget/ledger ownership contract. Data gathering can use isolated resources in one campaign while analysis remains one bounded context. Adjacent merge: Merging implementation would prevent candidate pinning and combine algorithm fixes with measured results; B6 must be merged first. Dependency evidence: Q1 supplies historical preparation provenance and B6 supplied complete selection; the current execution preflight establishes applicability to the selected candidate and resources.

**Stop conditions:** Do not launch with unresolved current execution gates. Stop on correctness/calibration failure, exhausted budget, lost host isolation or missing required native cell. Preserve invalid/incomplete runs. Do not repair product code inside this evidence slice, substitute cross-compilation or declare adoption on the owner's behalf. Apply shared representation and precise-root approach stops; an evidence/context overrun is a re-slice decision, not permission to expand this PR.

## Acceptance criteria

- Each listed slice delivers its stated behavior through the named real seam and is independently green without an unmerged successor.
- The stated owner enforces the declared typed-domain invariant, and all listed materially distinct semantic classes have the required disposition/evidence within the finite budget.
- Every retained temporary seam has the stated coherent intermediate contract and named removal slice; no public caller can select incomplete BBR.
- Existing Reno/API/wire/ownership/platform behavior remains unchanged except the explicitly accepted opt-in behavior and Config literal compatibility exception.
- No accepted requirement depends on an unapproved maintained verification tool, unknown host, invented measurement or untraced authority.

These criteria range only over the per-slice supported domain, named parser/representation owners and guarantee level. They do not claim universal measured performance. Their terminating evidence is the slice table and focused gates, not an open-ended search for counterexamples.

## Validation gates

Before commit, run scoped new/affected tests and affected-package `go test`, plus `go vet` for changed Go packages and `go mod tidy -diff` for Go/dependency changes. T2/T3/T4 and B6 require one focused `go test -race` over their named concurrency/lifetime cases; other slices use race only when they modify a concurrent seam, as identified in the implementation preflight. Pin exact test regexes/packages in that preflight from the proposed names; do not broaden into unbounded stress. Existing hosted unit, integration, lint and cross-compilation jobs run for drafts and must be successful at the exact merged head. Q1/Q2 additionally use the accepted campaign's finite native/operational gates and ledger. Docs-only certification uses the repository overlay's clean exact-head diff/link/graph checks and independent contract review.
