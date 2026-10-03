# BBRv3 qualification decision after the Linux diagnostic

Status: accepted decision set for [Decide whether the Linux-measured BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/713), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Revised after a multi-agent consideration (RAS run `20261003T153443-60fb5c667038cb480396720f`); the [disposition table](#consideration-dispositions) records every finding. Accepted by the operator on 2026-10-03. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-qualification-decision`, based on `4323dae8` (the [Linux diagnostic](../2026-10-03-bbr-linux-diagnostic/README.md) record). That record contains the [previous qualification decision](../2026-10-03-bbr-qualification-decision/README.md), the [WAN-corrected re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) and the candidate `fc4c1bf1`.

## Question

On the combined Mac record and the Linux diagnostic evidence, should BBRv3 advance to further qualification, continue resolving its implementation, or be declined? Apply the advancement condition in D1 of the previous decision. Dispose of any extension of the scoped D2 exception, any newly attributed cost, and the remaining blockers. Classify the reason as implementation, algorithm trade-off or missing evidence. If advancing, choose the smallest qualification that would change an adoption decision, and present the cost of any paid experiment before authorization.

## Normative constraints (quoted)

Previous decision, D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": the S6 benefit holds; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results". And: "If it is not met … D5 still runs and decides among another resolution step, a declared exception, or declining, with the reason classified."

Previous decision, D2: "The exception does **not** carry over automatically. On Linux, or on any new revision, each corresponding cell is re-attributed under its registered rule … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

Map #666 Notes: "do not treat a woefully inefficient implementation as a reason to dismiss BBRv3." "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Preserve default Reno, protocol and payload integrity, packet/buffer ownership, bounded queue and delivery evidence, pacing and congestion limits, receive fairness, ECN/loss correctness and supported-platform behavior. Any necessary contract change is a separate explicit design decision." Out of scope: "weakening calibration/correctness gates".

## Evidence base

Both records measure the same candidate, `fc4c1bf1`, against frozen Reno `e4f322cb`, with the same fixture, paths, limits and median-of-five-block rule. The Mac record ran on one Apple M4 Max; the Linux record on `minimax` (AMD Ryzen AI MAX+ 395, Linux 7.0), with four isolated physical cores per endpoint and no contaminated observation. Ratios are medians of per-block ratios against the same block's Reno; they are not quotients of arm medians.

### Advancement condition, checked

| D1 condition | Mac (`fc4c1bf1`) | Linux (`fc4c1bf1`) | Met? |
| --- | --- | --- | --- |
| 1. Repeatable S6 benefit | 12.31× / 14.09×, 5/5 each | 12.07× / 11.09×, 5/5 each | Yes, on both |
| 2. Every flag passes or is attributed and excepted | Three attributed cells excepted (D2) | Three attributed cells, none excepted | No |
| 3. No unresolved flag, no preservation flag | Six unresolved cells, two preservation cells | Fifteen unresolved cells, two preservation cells | No |
| 4. Mac flags reviewed, platforms named | Reviewed below | — | Reviewed |

### Flags by kind, both platforms

| Kind | Mac | Linux | Same? |
| --- | --- | --- | --- |
| S6 control p95 | 1.547 / 1.481, Up policy (excepted) | STREAM 1.541, Up policy; DATAGRAM passes (1.134) | Same cause, same or smaller |
| Loopback goodput | passes (0.975 / 0.985) | **0.770 / 0.795**, unresolved | New on Linux |
| S5 goodput | passes (0.969 / 0.979) | **0.913 / 0.901**, unresolved | New on Linux |
| Sender CPU per GiB | S5 1.170 / 1.320, unresolved | loopback 1.261 / 1.208, S5 1.237 / 1.108, unresolved | Persists |
| Receiver CPU per GiB | S5 DATAGRAM 1.138, unresolved | loopback STREAM 1.189, S5 1.155 / 1.131 (three cells), unresolved | Persists |
| Memory, attributed | S6 STREAM receiver (delivery), S5 sender (bookkeeping) | candidate S5 DATAGRAM sender (bookkeeping), S5 STREAM receiver (delivery) | Partly; compositions changed |
| Memory, unresolved | three mixed cells | S5 STREAM sender, S6 STREAM sender and receiver, S6 DATAGRAM sender | Persists |
| Preservation (Reno on candidate) | S5 DATAGRAM sender and receiver CPU 1.192 / 1.158 | S5 DATAGRAM sender RSS 1.152; loopback DATAGRAM control p95 1.226 | Different cells; Mac CPU flags do not recur |

### What the Linux record adds

- **The extra sender work appears only when BBR is selected.** Reno built from the candidate executes frozen Reno's work within 0.2% in instructions. The candidate sender executes 1.188× / 1.123× Reno's instructions per useful GiB on S5, in every block, while A/A Reno stays near 1.000.
- **It executes in user space.** User instructions are 1.358× / 1.267× and user cycles 1.494× / 1.336×. Kernel instructions are 0.978× / 0.944×, and wakeups, context switches and futex calls are below Reno's. Per forward packet that is about 11,300 (STREAM) and 8,700 (DATAGRAM) extra user instructions, on Reno's 31,600 and 33,300. This rules out, on Linux, the Mac record's leading hypothesis of extra kernel send and wake work. It says where the work executes, not why.
- **Localization was inconclusive.** Under the registered rule, no group held half the excess (BBR and bookkeeping 0.39–0.44, other user code 0.29–0.37, runtime scheduling 0.14–0.21 at the sender, allocation and GC 0.03–0.16). No discrimination stage ran and no defect was demonstrated. The BBR group combines model and bookkeeping work. Descriptively, the largest S5 sender increases sit in mutex and channel synchronization (about 0.9–1.1 G of the 4.8–5.0 Gcycles/GiB excess), the per-packet delivery-record map in `captureCongestionSend` (about 0.3 G), recovery-evidence lookups, per-packet capability queries, send-credit reservations, and congestion and BBR ECN feedback. These are leads, not causes. Loopback was not localized.
- **Allocation is a minor share at the S5 sender.** Allocation and GC hold 0.03–0.05 of the sender excess.
- **Loopback goodput and sender CPU move together, but that is not mechanism evidence.** Both arms use about 2.3 cores of sender CPU on loopback. CPU seconds per useful GiB and goodput share delivered bytes in their calculation, so at similar CPU occupancy their reciprocal movement is partly arithmetic. A sender limited by per-packet work is a hypothesis, not a finding.
- **S5 goodput is unlikely to be a sender CPU limit.** At about 85 Mbit/s and 12.6 s/GiB, the S5 sender uses about 0.12–0.13 cores. Reno's S5 goodput is the same on both hosts (95.9 / 94.4 Mbit/s); the candidate's is lower on Linux (87.5 / 85.0) than on the Mac (92.9 / 92.4). The counted Linux runs spend 59–80% of the window in Cruise, 9–26% in Up, 5–10% in Down, 2–3% in Refill and 2–4.5% in ProbeRTT. No stage attributes the gap.
- **The receiver's whole-run CPU ratio (1.08–1.11) exceeds its measured-window cycle ratio (1.03–1.04).** The two differ in metric and observation boundary, so part of the receiver flag may lie outside the measured window; the fraction is not established.

### The two Linux preservation flags

- **Reno-on-candidate S5 DATAGRAM sender RSS, 1.152.** Its median lies **above** the A/A arm's per-block range (0.918–1.122), so it is not even noise-compatible on the A/A test. The diagnostic's prose says both cells' A/A ranges "reach or exceed" their values; `summary.json` shows that holds for the latency cell but not this one. The historical record is not rewritten; this decision notes the discrepancy. The heap-site stage did not profile the preservation arm, so nothing explains this cell. The candidate's own S5 DATAGRAM sender RSS (1.144) is a separate cell: its heap-site blocks put about 86% and 82% of the positive excess in bookkeeping, above the registered 70% criterion, so that attribution stands independently.
- **Reno-on-candidate loopback DATAGRAM control p95, 1.226.** Its median lies inside the A/A range (0.358–4.269), so it is noise-compatible. But that range is so wide that a fourfold regression would also fit, and each loopback run has only 20 control replies, so its p95 is close to the run's maximum reply time. Absolute values are 0.29–1.50 ms (median 0.537 ms) against frozen Reno's 0.13–0.57 ms. The A/A arm on loopback STREAM itself crosses the limit (1.294). These show measurement inadequacy, not a cause.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced**. Conditions 2 and 3 fail on Linux. Fifteen candidate cells are raised and unresolved, and two preservation cells are raised. Advancing would waive limits for costs that are not explained.

BBRv3 is **not declined**. The useful benefit is repeatable on two platforms. Platform and workload medians are 11.09–14.09× Reno's receiver-verified goodput on S6, with improvement in all twenty pairs (individual pairs 8.94–16.16×). Sender CPU per useful GiB is an eighth (Linux) to a third (Mac) of Reno's. The open costs lack causal attribution. The map rules out dismissing BBRv3 because the current implementation is inefficient, and nothing here shows an algorithm defect.

**Reason, classified per flag group:**

| Group | Classification | Basis and hypothesis |
| --- | --- | --- |
| S5 sender CPU | **Missing evidence** | Localization inconclusive. Execution in user space and BBR-only selection motivate the hypothesis of avoidable integration overhead, which Ticket A tests. |
| Loopback sender CPU and loopback goodput | **Missing evidence** | Not localized. The hypothesis is a sender limited by per-packet work, which Ticket B tests with a discriminating comparison. |
| Receiver CPU (three cells) | **Missing evidence** | Small measured-window excess; an unquantified part may lie outside the window. |
| S5 goodput on Linux | **Missing evidence** | Unlikely to be a sender CPU limit and not seen on the Mac. Model behaviour and a platform effect are both open. |
| Unresolved memory cells | **Missing evidence** | Mixed heap-site composition under the unchanged 70% rule. |
| Preservation cells | **Missing evidence** | RSS is outside its A/A range and unexplained; latency is noise-compatible but measured too sparsely to judge. |
| S6 STREAM p95; candidate S5 DATAGRAM sender RSS; S5 STREAM receiver RSS | **Algorithm trade-off or design bound** | ProbeBW Up policy; declared bookkeeping bounds; loss-driven reassembly at the delivered rate. |

No group is classified as a demonstrated implementation defect. The verdict does not predict that the next step will succeed. It records that the open costs now have specific, testable hypotheses, and that testing them is the cheapest way to learn whether a competent BBRv3 is ready.

### D2 — No exception extended or granted now

The D2 exception is **not extended** to Linux, and **no new exception** is granted. No exception could make `fc4c1bf1` advance while unresolved flags remain, and under D2's own rule every cell must be re-attributed on any new revision.

Recorded for the next decision, without clearance:
- **S6 STREAM control p95 on Linux:** 1.541, the same Up-policy cause, at the same or smaller magnitude than the Mac cell.
- **Candidate S5 DATAGRAM sender RSS on Linux:** 1.144, design-bounded bookkeeping. The paired ratio is slightly larger than the Mac cell's (1.132), and the reported absolute bookkeeping excess is smaller (2.5–3.0 MiB against 3.1 MiB). A larger ratio prevents automatic carry-over; it does not prohibit a newly considered exception.
- **Candidate S5 STREAM receiver RSS on Linux:** 1.441 (17.6 → 25.3 MiB). Newly attributed to delivery data, as reassembly behind 0.25% overflow loss. Unresolved on the Mac.
- **S6 STREAM receiver RSS** and **candidate S5 STREAM sender RSS** changed composition on Linux.

The macOS D2 exception stays on record for `fc4c1bf1`, unchanged.

### D3 — Preservation flags stay blocking; resolve them with metric-specific evidence

D1's condition 3 is **unchanged**: a raised preservation flag blocks advancement. Instruction agreement and A/A overlap are recorded as **noise-compatible or inconclusive**, never as noise attribution, and they never clear or except a preservation flag. Historical flags and verdicts are preserved.

The next readiness record must register, before its data, metric-specific measurement-adequacy and discrimination rules for any raised preservation cell:
- **RSS:** a bounded heap-site and retention comparison of Reno on candidate against frozen Reno, together with the frequency of high-memory outcomes across blocks. Equal instruction counts or allocation totals do not exclude higher retained memory.
- **Latency:** enough control replies per run for a usable tail estimate, with absolute differences and variability reported on the relevant path. If the fixture cannot provide an adequate tail sample, the cell is reported as an **evidence gap**, not cleared.

The rules must be exercised before data on synthetic cases: unchanged controls; unchanged instructions with increased retention; a repeatable latency regression inside a wide A/A envelope; sparse samples; and unusable measurements. None may pass solely by range membership.

Any later proposal to let a preservation flag be covered by an exception would be a separate, explicitly identified operator decision. It would need a demonstrated cause, and it could never cover protocol, payload, ownership, ECN/loss or other correctness contracts.

### D4 — Next: test the per-packet leads by intervention, then re-demonstrate on Linux

Two task tickets in sequence, with a checkpoint between them.

**Ticket A — Test five selected per-packet leads by intervention on the BBR-enabled path.** A new branch from `fc4c1bf1`.

- **Scope: five selected leads**, each a hypothesis registered before any change:
  1. per-packet synchronization crossings (mutex and channel);
  2. the per-packet delivery-record map;
  3. recovery-evidence lookups;
  4. per-packet capability queries;
  5. send-credit reservations, including the `&sendReservation{}` allocation.

  Congestion and BBR ECN feedback are deferred. They are controller-feedback semantics rather than integration plumbing, and their cost stays in the reported residual, unattributed.
- **Contracts per intervention.** Each hypothesis names the contracts it can affect and how they are validated. Where applicable: ownership and lifetime, cancellation and errors, ordering, credit and queue bounds, receive fairness, pacing and congestion limits, ECN/loss behaviour and native capabilities. Each change also names its equivalence domain against `fc4c1bf1`: the same congestion inputs, decisions and recorded events on the same traces, in the form #706 and #709 used, with the exact oracle records and commands linked. Existing gates, including #709/#710 equivalence, ownership and race checks, are preserved. Synchronization and send-credit changes add an invariant argument plus targeted race and ownership tests. Native coverage that cannot run is recorded as a gap; compilation is not counted as native behaviour.
- **Measurement registration**, written and exercised on synthetic null, negative, noisy and conflicting-counter cases before any data:
  - **Arms:** frozen Reno, A/A Reno, the predecessor revision, the new revision, and Reno on the new revision.
  - **Path and blocks:** S5, both workloads, six blocks, on `minimax` with #712's core layout.
  - **Metrics and normalization:** user and kernel instructions and cycles per forward packet and per useful GiB, at both endpoints, under `perf stat`.
  - **Aggregation and controls:** median per-block ratios, with a contemporaneous A/A noise range.
  - **Usability:** every event at ≥ 95% running time, with at most one rerun of a block whose instruments are unusable.
  - **Disagreement:** a rule for when instructions and cycles disagree.
  - **Outcomes:** keep, null, negative or inconclusive.

  Effects are **conditional net effects** of each change in its registered order. They are not intrinsic leaf-function costs. Ticket A sets no readiness flag, and a counter result alone never implies a whole-run CPU readiness conclusion.
- **Keep rule.** A change is kept only when it is equivalent, its contract validation passes, the targeted work falls beyond the A/A range, receiver integrity holds, and Reno on the new revision stays inside the A/A range of frozen Reno.
- **Not allowed.** No parameter or model change, no change to declared bounds, no send-path batching (GSO or `sendmmsg`), and no change to Reno's path.
- **Stop.** A lead whose change needs a contract change, or cannot be made equivalent, stops; it is reported and not forced. Independent leads continue after a null.
- **Inventory.** At most one revision per lead. Each measurement is 60 observations at 40 s, about 40 minutes of fixture time. Five leads, plus a cumulative check of the surviving revision against `fc4c1bf1`, come to at most 360 observations (about 4 hours), plus at most one rerun block per measurement.
- **Checkpoint for B.** Ticket A closes with the surviving revision (which may be `fc4c1bf1` unchanged), each lead's outcome and measured conditional effect, the residual excess per packet against Reno, and the unresolved evidence.

**Ticket B — Re-demonstrate the surviving revision on Linux.** Blocked by A.

- **Prerequisites first.** Before any comparison, rerun the applicable #712 prerequisites: ECN calibration and engagement, native I/O, resources and instruments. A failed prerequisite ends the ticket as a reported prerequisite gap.
- **Readiness stage.** If A kept at least one change, rerun #712's readiness stage on the surviving revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout. That is 110 observations, about 63 minutes. If A kept nothing, the readiness rerun is skipped, because it would duplicate #712.
- **Attribution stages.** Each stage below runs only if its registration names, before data, the question, hypothesis, competitor, distinguishing observations, comparison inventory, maximum observations, escalation trigger and inconclusive termination. Ticket B's attribution stages are capped at 120 Linux observations and 10 macOS observations in total.
  - **Loopback bottleneck.** A paired comparison of `fc4c1bf1` and the surviving revision on loopback, measuring instructions and cycles per packet, packets per useful GiB, sender demand, and thread-level saturation and scheduling. The hypothesis is a sender limited by per-packet work. A named competitor (for example, a receiver-side or flow-control limit) is distinguished by which side saturates. Reciprocal goodput and CPU movement alone yields no causal verdict. Ambiguous or conflicting evidence ends the stage as inconclusive. If A kept nothing, this stage localizes loopback on `fc4c1bf1` only.
  - **S5 goodput.** A Linux timeline of phase time, the delivery-rate estimate against the bottleneck, pacing lateness and loss-response events. The hypothesis is model behaviour; the competitor is a platform timing effect. A bounded macOS S5 timeline with the same instrument can show only whether the behaviour is the same or differs across the two hosts. Because hardware and scheduling change with the OS, a difference cannot by itself establish a Linux-specific cause.
  - **Receiver CPU.** A split of the measured window from the whole run. This is localization, not cause.
  - **Memory.** #711's heap-site rules, unchanged.
  - **Preservation.** D3's rules.
  - **D2 cells.** Re-attributed on the surviving revision.
- **Reporting.** Ticket A's counter effects are reported beside the readiness CPU flags. CPU that falls but stays above its limit without a cause remains unresolved.
- **Platform.** The readiness results are Linux results. New-revision Mac readiness stays unmeasured, and the Mac exception does not transfer. Any advancement would name Linux only, unless the operator asks for macOS coverage. Supported-platform correctness remains required either way.

**Terminal routing.** Every path releases D6 with an identified revision and preserved evidence, and none implies readiness.

| Outcome | Route |
| --- | --- |
| A keeps one or more changes | B runs prerequisites, readiness and registered attribution on the surviving revision. |
| A keeps nothing (all null, all stopped, or not equivalent) | B runs prerequisites and only the separately justified, registered attribution stages on `fc4c1bf1`, or closes as not run with the reason; D6 then decides on A's findings. |
| A or B has a correctness or contract failure | The change is not kept; the failure is reported to D6. |
| Unusable counters or a failed prerequisite | The affected stage ends as an evidence gap or prerequisite gap, and the ticket closes to D6. |
| Flags persist on the surviving revision | Reported as unresolved to D6. |

**Bounds.** Owned hardware only, with no paid resource or Q2 ledger charge. Frozen one-ticket aids, not a maintained framework.

### D5 — No paid experiment and no campaign

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused. The ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) and its evidence are unchanged. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D6 — A fresh qualification decision follows Ticket B, whatever it finds

A new grilling ticket, blocked by Ticket B, re-asks this question on the surviving revision's evidence, with both preserved Mac records reviewed. It runs on every outcome in the routing table. Its options include:
- advance;
- another resolution step;
- a **bounded, evidence-supported redesign of BBR-specific integration** (the failure of five selected interventions does not show that every contract-preserving redesign is unavailable);
- a separate contract-change design decision, if a needed change is demonstrated;
- a declared exception;
- declining further work on this candidate.

It must distinguish four things: failed selected interventions, missing evidence, demonstrated contract incompatibility, and algorithm suitability. Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable.

### D7 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. #673 is re-wired to be blocked by the D6 decision ticket. No evidence shows that a version change would avoid the open costs, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the two-platform S6 benefit.** It would waive the CPU, goodput, memory and preservation limits for unexplained costs.
- **Decline BBRv3.** The open costs lack causal attribution, and the benefit is robust on both platforms.
- **Extend D2 to Linux now.** It cannot unlock advancement, and any new revision needs re-attribution anyway.
- **Treat instruction agreement or A/A overlap as noise attribution, or let it enable an exception (the first draft of D3).** It cannot exclude retained memory or latency regressions, and the RSS cell fails it anyway.
- **An automatic sub-millisecond latency exemption, or clearance by membership in observed RSS levels.** Neither establishes preservation.
- **Another localization round on `fc4c1bf1` with finer groups.** The excess is diffuse; interventions discriminate directly.
- **Fix only the two allocation sites.** Allocation and GC hold 0.03–0.05 of the S5 sender excess.
- **Batch the paced send path (GSO or `sendmmsg`).** It changes both arms' send path and would offset per-packet work rather than remove it.
- **Tune Cruise headroom, ProbeBW Up or ProbeRTT to close the S5 goodput gap.** That is speculative tuning before attribution.
- **Shrink the bookkeeping bounds.** It is a design-bound change without a consumer requirement.
- **A savings threshold for Ticket A, or halting independent leads after a null.** Finite interventions are the checkpoints; residual magnitude is not attribution.
- **Require Mac readiness for Linux-only advancement.** The accepted D1 allows declared-platform advancement with Mac flags reviewed.
- **Rerun Mac readiness on `fc4c1bf1`, or Linux readiness when A keeps nothing.** Neither adds information.
- **Any paid run.**

## Map changes on acceptance

- A resolution comment on #713, the issue closed, and one Decisions-so-far line on #666.
- New task tickets A and B and the D6 grilling ticket, wired A → B → D6. #673 is re-wired from #713 to the D6 ticket.
- Fog updated: the per-packet leads and the Linux S5 goodput question become tickets. The bookkeeping-bound and adoption-evidence fog stays.

## Consideration dispositions

RAS run `20261003T153443-60fb5c667038cb480396720f` (five reviewers, adjudicated) retained the continue-resolving verdict, the absence of an exception extension and the absence of a paid run, and raised ten fixes. All are accepted. Decisive figures were checked against [summary.json](../2026-10-03-bbr-linux-diagnostic/summary.json) and [attribution.json](../2026-10-03-bbr-linux-diagnostic/attribution.json).

| Fix | Disposition | Where | Verification and remaining dissent |
| --- | --- | --- | --- |
| 1 — D3 noise and exception rule | Accepted | D3; Rejected | The exception-enabling amendment is withdrawn and condition 3 is unchanged. Instruction agreement and A/A overlap are now only noise-compatible or inconclusive. Metric-specific RSS and latency rules, with synthetic cases, are required before data. The adjudicated view that a prospective amendment is not categorically forbidden is kept as a separately identified future decision with a demonstrated cause. |
| 2 — Preservation RSS evidence | Accepted | Evidence base, preservation flags; D1 table | Verified: Reno-on-candidate median 1.1515, A/A maximum 1.1224; candidate bookkeeping shares about 86% and 82%. The diagnostic's prose discrepancy is noted, not rewritten. The candidate and preservation cells are labelled separately. |
| 3 — Causal classifications | Accepted | Evidence base; D1 classification table; D7 | All CPU, loopback and receiver groups are now missing evidence with stated hypotheses. The model-arithmetic claim is removed. The receiver's out-of-window share is unquantified. BBRv1 stays parked on the no-evidence ground. |
| 4 — Contract and platform validation | Accepted | Ticket A, contracts; Ticket B, platform | Per-intervention contract and equivalence domains, invariant arguments and race tests for synchronization and credit changes, linked oracles, native-coverage gaps recorded. |
| 5 — Ticket A measurement contract | Accepted | Ticket A, measurement registration and inventory | Verified A/A spreads (receiver user instructions about 0.999–1.004) contradict the first draft's ±0.1% claim, which is removed. Arms, metrics, controls, usability, outcomes and an inventory of at most 360 observations are set. Effects are conditional net effects. Feedback deferral is explained. |
| 6 — Loopback discrimination | Accepted | Evidence base; Ticket B, loopback bottleneck | The reciprocal fit is labelled arithmetic-adjacent. A paired comparison with saturation evidence and a named competitor is required. |
| 7 — Ticket B attribution contracts | Accepted | Ticket B | Each stage has a registration contract, a cap of 120 Linux and 10 macOS observations, and an inconclusive ending. The limits of the Mac comparison are stated. The receiver split is labelled localization. |
| 8 — Terminal routing | Accepted | Ticket A checkpoint; terminal routing table | No duplicate readiness rerun when A keeps nothing. Prerequisites are re-checked. Every path reaches D6. |
| 9 — D6 redesign option | Accepted | D6 | Bounded integration redesign is added. Declining an inefficient candidate is distinguished from unsuitability, without forbidding every implementation-related decline. |
| 10 — Numerical corrections | Accepted | Throughout | Checked: S6 11.09–14.09× (pairs 8.94–16.16×); extra user instructions 11,300 / 8,700 per packet; loopback preservation p95 0.29–1.50 ms (median 0.537 ms), 20 replies per run; six against fifteen unresolved cells, two preservation cells each; bookkeeping ratio 1.144 against 1.132, absolute 2.5–3.0 against 3.1 MiB. |

Not acted on, as the synthesis advised: requiring Mac readiness for Linux-only advancement, invalidating the candidate's bookkeeping attribution, sub-millisecond or level-set exemptions, savings thresholds, requiring every flag to resolve before D6, and claiming that the race gates or #706 evidence are absent.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above; it takes no new measurement.
