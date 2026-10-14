# BBRv3 advancement decision on r9

Status: decision set for [Decide whether the latency- and memory-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/747), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). The agent recommended parking BBRv3 as not advanced. The operator chose instead to advance it by explicit decision, and approved the set below in outline on 2026-10-14. **Draft for one multi-agent consideration and the operator's final acceptance.** Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-r9-advancement-decision`, based on `47150c7b` (the [loopback control-latency record](../2026-10-13-bbr-r9-loopback-latency/README.md), #744), whose tree's code is exactly r9 plus records. Its history contains the [r9 decision](../2026-10-13-bbr-r9-qualification-decision/README.md) (#741), the [r9 memory and latency attribution](../2026-10-08-bbr-r9-memory-attribution/README.md) (#740) and the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) (#739). The candidate is **r9, `81e9dc8c`**: r8 (`95f5b6b7`) plus a 32-entry send queue for BBR connections.

## Question

From #747: on the evidence from the latency, re-demonstration and memory tickets, with the preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration, open a separate contract-change or design-bound decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged advancement condition; preservation flags still block; dispose of the cells the r9 decision's D2 recorded, on every outcome of its terminal routing table. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability. If advancing, choose the smallest qualification that would change an adoption decision, and present any paid experiment's decision value, required evidence and concrete cost before authorization. Unless this decision declines further work on BBRv3, it closes the two BBRv1 tickets as out of scope.

## Normative constraints (quoted)

[Acceptance plan](https://github.com/the-sarge/quic-go-fast/blob/a4ecfc8de39eaa673755bf864fb7fdefbc109e80/docs/audits/2026-09-23-bbrv3-acceptance-plan.md), on the four limits: "These are review flags, not automatic rejection rules. Show absolute values and run variability alongside ratios. If a baseline denominator is zero or too small to produce a useful ratio, show the absolute difference and explain the limitation. Coexistence, packet loss, event recovery, startup and completion remain visible even where no numeric advisory flag has been chosen." Also: "Correctness and protocol compliance are hard gates." "The owner judges adoption after reviewing the data; neither a minimum improvement nor a statistical-significance threshold is an automatic performance gate."

Map #666 Notes: "The operator accepted the existing regression limits as local candidate-readiness criteria … with a repeatable useful benefit on at least one relevant difficult path before further qualification." "These criteria apply to a competent candidate; failure triggers diagnosis and does not by itself reject BBRv3." "Separate implementation readiness from algorithm suitability." "Any necessary contract change is a separate explicit design decision." "Any proposed paid qualification must present its decision value, required evidence and concrete cost before a new operator decision." Out of scope: "No rewriting failed verdicts, weakening calibration/correctness gates, …".

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1, the advancement condition: "2. every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell (D2 sets the form); 3. no flag is raised and unresolved, and no preservation flag is raised". D2: "Attribution explains a flag; it does not clear it." "Each stays **raised**, with its attribution and absolute values, in every report." "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "a raised preservation flag blocks advancement." D6: "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

[Intervention qualification decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md), D2, on an implementation-integration cost: "It is a candidate for remedy, not for exception, while a contract-preserving remedy remains untested."

[r9 decision](https://github.com/the-sarge/quic-go-fast/blob/2138a829/docs/audits/2026-10-13-bbr-r9-qualification-decision/README.md), D2: "Both are the leading exception candidates. An exception decision must weigh readiness's 30 replies per WAN run and #740's per-run timeline evidence." D6 routing rows that apply here:
- "J: depth raises latency; mechanism load or other, mixed, inconclusive or a gap | Unresolved and blocking; a further cause stage is the next decision's choice."
- "J: remedy partial, inconclusive, inert, mistargeted, gates failed, unbuildable or not attempted | Untested. Unresolved and blocking; a candidate for remedy, not for exception."
- "S6 control p95, survivor r9 | D2's attributions on r9 stand unchanged: attributed behaviour, eligible for an exception decision."
- "L: mixed; inconclusive; instrument-sensitive; gap | Unresolved; not eligible for a cause-based exception."
- "Flags persist | Reported as unresolved to the decision."

D8, operator decision of 2026-10-13: "Unless the new decision ticket declines further work on BBRv3, it closes both as out of scope, with one line in the map's Out of scope section."

## Evidence base

Readiness figures are from the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) ([summary.json](../2026-10-08-bbr-r9-linux-redemonstration/summary.json)): 110 observations on r9 against frozen Reno `e4f322cb` on `minimax`, all clean. Ratios are medians of five per-block ratios against the same block's frozen Reno, with the number of blocks crossing the limit in parentheses. Memory excesses in MiB are medians of per-block differences, candidate minus the same block's frozen Reno, computed by [memory_excess.py](memory_excess.py) into [memory-excess.json](memory-excess.json); they are descriptive and do not change any flag.

### The unchanged advancement condition on r9 (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.53× [12.25–15.74] STREAM, 14.74× [11.34–16.32] DATAGRAM, 5/5 pairs each; forty Linux pairs above Reno across `fc4c1bf1`, `d0fabc4d`, r8 and r9 | Yes |
| 2. Every flag passes, or is attributed and excepted | Eleven raised candidate cells: two attributed (S6 control p95) but not excepted, nine without a cause attribution | No |
| 3. No unresolved flag, no preservation flag | Nine unresolved cells; no Reno-on-candidate cell raised | No (unresolved cells) |
| 4. Mac records reviewed, platforms named | Reviewed below; Linux only | Reviewed |

### Raised cells on r9

| Cell | r9 ratio | Frozen Reno → r9, arm medians | Excess, median per block [max] | Ending |
| --- | --- | --- | --- | --- |
| S6 STREAM control p95 | 1.562 (5) | 127.7 → 199.5 ms | +71.7 ms (difference of arm medians) | Attributed: selected ProbeBW Up policy (#740) |
| S6 DATAGRAM control p95 | 1.459 (5) | 125.8 → 187.3 ms | +61.5 ms (difference of arm medians) | Attributed: selected ProbeBW Up policy (#740) |
| Loopback STREAM control p95 | 1.485 (4) | 0.134 → 0.199 ms | At the adequate sample (#740): 1.849 [1.672–2.014], pooled 0.140 → 0.258 ms | Regression reproduced (#740); two components (#744, below); remedy not attempted |
| S5 STREAM sender RSS | 1.235 (5) | 17.97 → 22.18 MiB | 4.17 [4.58] MiB | Mixed: the ring removes 0.48–0.57 (#740) |
| S5 STREAM receiver RSS | 1.518 (5) | 16.94 → 25.71 MiB | 8.77 [9.44] MiB | Inconclusive (#740) |
| S5 DATAGRAM sender RSS | 1.159 (4) | 17.88 → 20.86 MiB | 2.86 [3.65] MiB | Evidence gap (#740) |
| S6 STREAM sender RSS | 1.673 (5) | 13.25 → 22.18 MiB | 8.92 [9.95] MiB | Inconclusive (#740) |
| S6 STREAM receiver RSS | 1.898 (5) | 13.50 → 26.34 MiB | 12.28 [13.36] MiB | Inconclusive (#740) |
| S6 DATAGRAM sender RSS | 1.542 (5) | 13.78 → 21.24 MiB | 7.46 [7.88] MiB | Inconclusive (#740) |
| Loopback STREAM receiver RSS | 1.120 (4) | 14.48 → 16.20 MiB | 1.73 [1.98] MiB | Inconclusive (#740) |
| Loopback DATAGRAM receiver RSS | 1.148 (5) | 18.72 → 21.72 MiB | 2.82 [3.36] MiB | Instrumentation gap (#740) |

Every goodput and CPU cell passes: loopback goodput 1.148 / 1.001, S5 goodput 0.967 / 0.972, S5 DATAGRAM sender CPU per useful GiB 0.953, and S6 sender CPU per useful GiB 0.086 / 0.090 of Reno. The other memory cells pass: S5 and S6 DATAGRAM receivers (1.070, 1.095) and both loopback senders (0.954, 0.956). No Reno-on-candidate cell is raised. The frozen-Reno A/A arm crosses one cell, loopback DATAGRAM control p95 (1.599, three blocks), which is context only.

### What the last three tickets added

- **J, [#744](https://github.com/the-sarge/quic-go-fast/issues/744#issuecomment-6172099042)** (36 counted observations, all clean):
  - *Depth raises latency*: pooled control p95, r9 ÷ r9 at depth 8, 1.187 [1.058–1.551]; 0.218 against 0.184 ms.
  - *Mechanism*: the registered ending is an instrumentation gap (recorder goodput 0.981 of plain). Read beside it under the operator's decision 1, it is mixed: queue share of the p95 rise 0.63 [0.38–0.82], almost all of it the sender's queue wait (0.60), with the sender's application wake at 0.26. The sender queue held a median of 9 entries ahead of a control request at r9, against 1 at depth 8.
  - *BBR baseline*: depth 8 against frozen Reno reads *regression reproduced*, 1.345 [1.075–1.490] (0.184 against 0.137 ms).
  - *Remedy*: not attempted, because the gate needs a queue-wait reading. The bytes- or time-ahead bound class is untested, not disposed of. Depth 8 gives back loopback goodput (0.96 of Reno, against r9's 1.17).
- **K, [#745](https://github.com/the-sarge/quic-go-fast/issues/745#issuecomment-6172104597):** not run, under D4, because J kept no change.
- **L, [#746](https://github.com/the-sarge/quic-go-fast/issues/746#issuecomment-6173016346):** not run, by operator decision on 2026-10-14. Even complete attribution would clear no cell: by #741's arithmetic, BBR's fixed bookkeeping alone exceeds the 10% allowance at the four S5 and S6 sender cells. #740 spent 124 observations on the same cells and labeled none. The memory cells therefore stay as #740 left them.

### The memory cost in absolute terms (descriptive)

- **Size.** The largest per-cell excess is 12.28 MiB, at the S6 STREAM receiver (worst block 13.36 MiB) on the readiness instrument (`ru_maxrss`). #740's own runs, measured with an external 1 Hz `smaps_rollup` sampler, read up to 16.24 MiB at that cell in one block; those are different runs on a different instrument.
- **What is known.** Every candidate endpoint carries explicit BBR structures: about 2.0–2.15 MiB per WAN sender and 1.18–1.35 MiB per receiver, including the 1.14 MiB outcome ring (#740). Where #740's smaller-ring arm kept traffic comparable, removing ring bytes removed about 1.0–2.7 resident bytes per explicit byte.
- **What is not known.** At the STREAM receivers, most of the excess is unexplained. #740's logical receive occupancy excess (0.0 MiB at S5, median 0.30 MiB at S6) weighs against unread or out-of-order data, and in some blocks the live-heap excess is well below the resident excess.
- **Bounds by configuration** (#746, code reading, not measurement): received data up to the connection flow-control window (15 MiB by default, `DefaultMaxReceiveConnectionFlowControlWindow`; the fixture uses 128 MiB); about 10,000 tracked sent packets; BBR's bookkeeping caps. Most of that ceiling is shared with Reno.
- **Consumer context** ([GridCast workload research](../2026-09-23-gridcast-bbr-workload.md)): an Attempt does not imply several concurrent QUIC data connections, though several user processes or Transfers can contend in unknown numbers. The default Sender working budget is 137 MiB and the receiver reservation 1 GiB; these are accounting reservations, not RSS limits, and concurrency counts were not supplied.

### The loopback control-latency cost in absolute terms (descriptive)

- **Size.** The rise is 0.065 ms in readiness (0.134 → 0.199), 0.118 ms at #740's adequate sample (0.140 → 0.258) and 0.081 ms in #744's descriptive reading (0.137 → 0.218). Reno's loopback p95 is about 0.14 ms, so the 20% limit allows about 0.03 ms.
- **Not noise.** The rise reproduces at an adequate sample (#740), although the readiness-sized frozen-Reno A/A comparison crossed the same limit on the loopback DATAGRAM cell.
- **Two components, each above the limit on its own.** The depth increment (r9 ÷ depth 8, 1.187) and a BBR baseline at depth 8 (1.345). The only named remedy class bounds the queue, so it addresses the depth increment alone, and the r9 decision's combined row already says "A depth-only exception cannot cover the whole cell".

### Mac records reviewed

As in the r9 decision. The [first decision's D2](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md#d2--a-scoped-operator-exception-for-three-attributed-flags) exception covers `fc4c1bf1` on macOS only (S6 control p95 1.547 / 1.481, S6 STREAM receiver RSS 1.574, S5 sender RSS 1.203 / 1.132); it is unchanged and is not extended. r9's Mac readiness is unmeasured. r8's netpoller kick is Linux-only; r9's queue depth applies on every platform.

### `main` today (source inspection)

`main` (`d3648918`) publishes BBRv3 as an experimental opt-in sender in `v0.62.1-fast.7`. Its README says "BBRv3 is unqualified and has known performance regressions; Reno remains the default." Its `internal/congestion` and `internal/ackhandler` trees are identical to the frozen component `e4f322cb` (`git diff --quiet`), so none of this map's corrections is on `main`.

## Decisions

### D1 — Verdict: advance r9 on Linux, by an explicit operator amendment of the advancement condition

Under the **unchanged** condition, r9 is **not advanced**: condition 2 fails with two attributed but unexcepted cells, and condition 3 fails with nine unresolved cells. That verdict stands and is not rewritten.

The operator then decides, explicitly and after reviewing r9's data, to **amend the advancement condition for r9 on Linux only**:
- condition 2's cause-attribution requirement is waived for the nine cells in D3;
- for the loopback STREAM control p95 cell, so is the remedy-before-exception rule;
- those nine cells receive **unattributed readiness exceptions**, a term defined in `CONTEXT.md` on this branch.

The following are **unchanged**:
- condition 1, condition 3's preservation clause and condition 4;
- every readiness limit;
- every correctness and preservation gate;
- the routing labels #740 and #744 assigned.

With D2's attributed exceptions and D3's unattributed ones, every raised r9 cell on Linux is passing or excepted, and no preservation flag is raised. So BBRv3 is **advanced: r9 (`81e9dc8c`) deserves further qualification, on Linux**.

The operator weighed:
- **The benefit.** It is repeatable: 14.5× / 14.7× on S6, and forty Linux pairs above Reno across four revisions. Every goodput and CPU cell passes, and no preservation flag is raised.
- **The acceptance plan's framing.** The four limits are "review flags, not automatic rejection rules", and the owner judges after reviewing the data and its absolute values.
- **Diminishing returns.**
  - *Memory:* two attribution designs (#740 labeled no cell; #746 was not run because even full success clears none).
  - *Latency:* the loopback cell has two components, each above its limit.
- **The absolute costs.** Up to about 12 MiB per endpoint of a single connection, which is less than one default connection receive window, and about a tenth of a millisecond of loopback control latency.

**What advancement means.** r9 is worth qualifying. It is not a qualification, an adoption decision, a production merge or a default-controller change, and it covers r9 on Linux only. It is an operator judgment about costs, not a finding that they were explained.

**Is this a weakened gate?** The map rules out "weakening calibration/correctness gates". These four limits are readiness review flags, not calibration or correctness gates: no limit changes, every correctness and preservation gate holds, and r9's failure under the unchanged condition stays on record.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | None new. #738's 32-entry queue stays negative under its registered rule, kept by operator deviation 4. |
| Untested remedies | J's bytes- or time-ahead bound class: not attempted, because the mechanism read mixed, not queue wait. |
| Missing evidence | The causes of the eight memory cells (one mixed, five inconclusive, one evidence gap, one instrumentation gap; L not run); the rest of the loopback latency mechanism; the cause of the depth-8 BBR baseline. |
| Attributed behaviour | S6 control p95, both workloads: the selected ProbeBW Up policy. |
| Demonstrated contract incompatibility | None. |
| Algorithm suitability | No finding against BBRv3. The unattributed exceptions are judgments about costs, not findings about the algorithm. |

### D2 — Attributed exceptions: S6 control p95, both workloads

Granted for r9 on Linux, at no more than STREAM 1.562 (+71.7 ms; 127.7 → 199.5 ms) and DATAGRAM 1.459 (+61.5 ms; 125.8 → 187.3 ms), as differences of arm medians.

- **Attribution:** the selected draft-06 ProbeBW Up policy, under #740's per-run rule. In all eight runs per workload, 98.6–99.4% (STREAM) and 98.5–100% (DATAGRAM) of samples above 25 ms fall in Up or in the Down entered from Up.
- **The weighing the r9 decision's D2 requires.** Readiness took 30 replies per WAN run, 150 per arm over five blocks, and every block crosses the limit in both workloads, so the raise is not an artefact of a small sample. The timeline evidence covers every #740 run.
- **Comparison with macOS.** STREAM's ratio is slightly above the macOS exception's 1.547, at a smaller absolute rise (+71.7 against +77.5 ms); DATAGRAM's is below 1.481.
- **No tuning of Up.** The macOS exception for `fc4c1bf1` is not extended.

### D3 — Unattributed exceptions: eight memory cells and the loopback STREAM control p95

Granted for r9 on Linux only, at no more than the magnitudes below. Each cell stays **raised** in every report, with its ending and absolute values. None is a cause-based exception: the r9 decision's routing made these cells ineligible for one, and that remains true. The operator grants a different kind, by explicit decision.

| Cell | At no more than (ratio) | At no more than (median excess per block) |
| --- | --- | --- |
| S5 STREAM sender RSS | 1.235 | 4.17 MiB |
| S5 STREAM receiver RSS | 1.518 | 8.77 MiB |
| S5 DATAGRAM sender RSS | 1.159 | 2.86 MiB |
| S6 STREAM sender RSS | 1.673 | 8.92 MiB |
| S6 STREAM receiver RSS | 1.898 | 12.28 MiB |
| S6 DATAGRAM sender RSS | 1.542 | 7.46 MiB |
| Loopback STREAM receiver RSS | 1.120 | 1.73 MiB |
| Loopback DATAGRAM receiver RSS | 1.148 | 2.82 MiB |
| Loopback STREAM control p95 | 1.485 in readiness; 1.849 at the adequate sample | +0.118 ms pooled p95 (#740) |

**Memory.**
- **Why without attribution.** These are #746's reasons. Attribution cannot make the sender cells pass, and two designs have failed to label the cells or were judged not worth running. Per-connection memory is bounded by configuration, and the costs are small next to the consumer's working-budget reservations.
- **What stays open.** The STREAM receivers' excess is unexplained. How often BBR approaches the shared receive ceiling in typical use is an adoption question, carried to the qualification and the production port (D4, D5).
- **Not a standing budget.** The operator considered a standing per-connection budget and chose this one-off exception instead ([Rejected](#rejected)). A later revision gets no pre-accepted memory allowance.

**Loopback STREAM control p95.**
- **The rule waived.** The remedy-before-exception rule (the intervention decision's D2; the r9 decision's "remedy … not attempted" row).
- **Why it is waived.** The depth-8 baseline alone reads 1.345 against frozen Reno, so the one named remedy class cannot clear the cell, because it addresses only the depth increment. The absolute cost is about a tenth of a millisecond on loopback, a path where the acceptance plan directs absolute differences when a ratio's denominator is too small to be useful.
- **What happens to the remedy.** The bytes- or time-ahead bound class stays **untested, not disposed of**, and passes to the production port as an input.
- **The limit itself.** The 20% latency limit is unchanged, and no absolute tolerance rule is adopted.

### D4 — The smallest qualification that would change an adoption decision: coexistence, named and not started

**Choice.** Coexistence on a shared bottleneck, under the acceptance plan's L6 (a competing QUIC Reno flow) and L8 (the same BBR candidate, with independent state):
- 100/100 Mbps, 100 ms, Long rows (60 + 300 s);
- both workloads, five pairs;
- the plan's equal-RTT, unequal-RTT and departure/re-entry phases;
- reporting each participant's useful delivery and control latency.

**Why this one.** The adoption question is whether real traffic should use BBRv3 instead of Reno. The local evidence covers loopback, S5 and S6 on Linux, and the S6 benefit is not in doubt. What could reverse an adoption call is BBRv3 displacing the traffic that shares its path, and no measured path exercises that. The alternatives compare as follows:
- **S8, CE response.** The cheapest: the relay already models marking. But classic-ECN marking on GridCast's paths is unestablished, and GridCast's ordinary Windows path has no ECN, so a result is less likely to change the call.
- **L4 / L5, low-rate pacing.** A narrow regime, and L4 needs TCP competitors.
- **C4 items 1 and 3.** Model-conformance edges, better answered by deterministic tests than by a qualification run.
- **L7, TCP CUBIC.** The most realistic competitor, but it needs kernel TCP through the emulated bottleneck on owned hardware. That is a feasibility question for the qualification effort, which may add it after L6 and L8.

**Required evidence**, binding on whichever effort runs it; its estimators belong to that effort's reviewed registration:
- **Fairness criteria fixed before data.** The plan sets no numeric coexistence flag, so the registration states what counts as acceptable sharing, for example the competitor's useful delivery against a Reno-against-Reno baseline under the same schedule, with an explicit inconclusive ending.
- **A calibrated shared bottleneck.** The relay in #739's record forwards one sender/target pair (source inspection). A multi-flow relay with per-flow propagation delay, a competitor runner and per-participant metrics are measurement-only aids to build and calibrate; the Q1 campaign's competitor schedule and TCP mode are available for reuse.
- **Unchanged limits.** The readiness limits, preservation rules and correctness gates apply unchanged, and the qualified revision is named.
- **The revision whose adoption is in question.** Any new revision re-measures. If the production port (D5) lands first, qualification runs on the port, and no exception in D2 or D3 carries over to it.

**Cost and authorization.** Owned Linux hardware (`minimax`), with no paid resource and no ledger charge. A rough whole-effort estimate, for scheduling only:

| Part | Estimate |
| --- | --- |
| Fixture build and development runs (the uncertain part) | 4–8 h |
| Registration review | 1–2 h |
| Counted runs (two Long rows, 40 runs of 6 minutes) | about 4 h on `minimax` |
| Analysis | 1–2 h |
| **Total** | about 10–16 h, 5–6 h of it on `minimax` |

This decision does not start the qualification; the operator starts it when an adoption call is wanted. No paid experiment is proposed, so none needs a decision value or cost now.

### D5 — Next: a separate production port, not started here

The operator's next step is a **separate Planit effort** to carry r9's correction into `main`'s experimental opt-in sender, still labeled unqualified, so that the opt-in sender is no longer the frozen component. This decision neither starts that effort nor authorizes a merge.

The port inherits from this map:
- r9's kept changes over the frozen component: C1–C4, the bounded ECN feedback, quantum-release pacing with its D08 amendment and its drop-tail overflow disclosure, the kept per-packet changes, the Linux-only netpoller kick, and the 32-entry BBR send queue;
- the untested bytes- or time-ahead bound class;
- the two BBR-only per-packet allocation sites the map's fog names as production-slicing leads;
- the production follow-ups [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630), [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636) and [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613);
- the undo-sentence amendment, if spurious-undo phase restoration is adopted;
- the glossary entries that live on record branches;
- the open memory and loopback-latency questions above.

The port's binding requirements:
- **It is a new revision.** No exception in D2 or D3 covers it, and any claim that it matches r9's readiness needs its own measurement.
- **The map's preservation rules apply in full**, including default Reno and every correctness gate.
- **Any contract change** remains a separate explicit design decision.

### D6 — BBRv1 closed out of scope

BBRv3 is not declined, so under D8 of the r9 decision [Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) close as out of scope, with one line in the map's Out of scope section. This answers the destination's BBRv1 question: BBRv1 is not investigated. Implementing or qualifying it would need a later explicit decision.

### D7 — No paid experiment, no campaign, no Mac readiness requirement

- **No paid resource.** No paid resource is proposed or authorized, the cloud heartbeat stays paused, and the ledger and its reservations are untouched.
- **Q2 is unchanged.** [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) stays incomplete and separate.
- **No Mac readiness requirement.** New-revision Mac readiness is not required for Linux-only advancement. macOS and Windows native evidence remain adoption questions for the qualification effort; they are GridCast's priority platforms.

### D8 — The map reaches its destination

The destination is met:
- the regression is explained;
- a local correction that addresses the supported causes is demonstrated;
- the qualification question is decided: advanced, for r9 on Linux;
- the BBRv1 question is decided: not investigated.

The remaining fog moves to Out of scope, because each item lies beyond this destination:
- production slicing goes to D5's effort;
- the adoption evidence (coexistence, CE response, low-rate pacing, C4 items 1 and 3, other platforms) goes to D4's qualification;
- the bookkeeping remedy, the bookkeeping design-bound decision and the loopback remedy class are inputs to the port.

The map then closes.

## Rejected

- **Park BBRv3 as not advanced** (the agent's recommendation). It follows the unchanged condition, but the operator judged r9's remaining costs acceptable on Linux, given its benefit, the plan's review-flag framing, the diminishing returns of further attribution and the small absolute costs.
- **Decline further work.** The implementation is no longer inefficient (every goodput and CPU cell passes), and declining would route to BBRv1.
- **A standing per-connection memory budget**, for example 15 MiB per endpoint, one default connection receive window. The operator preferred a one-off exception for r9's measured cells, so later revisions get no pre-accepted allowance.
- **A standing absolute latency tolerance**, for example 1 ms on paths whose Reno p95 is below 1 ms. The operator chose a scoped exception instead, and the 20% limit is unchanged.
- **A further cause stage for the loopback latency.** The cell's two components each exceed the limit, and the operator chose the exception.
- **Running L (#746), or re-running #740's removal arms.** Even full success clears no cell.
- **Shrinking the bookkeeping now.** That is a design-bound change, and belongs to the port's design if anywhere.
- **Running the coexistence qualification now, on r9.** Its evidence would have to be repeated on the ported revision, so it waits until an adoption call is wanted.
- **Any paid run, Q2 resumption, host-setting change or BBRv1 work.**

## Map changes on acceptance

- A resolution comment on #747, the issue closed, and one Decisions-so-far line on #666.
- #673 and #674 closed as out of scope, with one Out of scope line.
- Not yet specified cleared into Out of scope lines: the production port, the coexistence qualification and the other adoption evidence, and the port's inputs.
- One Notes line recording that r9 is advanced on Linux with attributed and unattributed exceptions.
- The map closed: destination reached.
- Glossary: **Readiness exception** added to `CONTEXT.md` on this branch.

## Consideration dispositions

To be added after the multi-agent consideration.

## Limits

This is a planning decision. It claims no qualification, readiness on another platform or adoption, and authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited. The memory excesses in MiB are arithmetic on #739's recorded peaks. The relay's single-flow design, `main`'s identity with the frozen component and the configuration bounds come from source inspection, not measurement. The unattributed exceptions record the operator's judgment, not an explanation of the costs they cover.
