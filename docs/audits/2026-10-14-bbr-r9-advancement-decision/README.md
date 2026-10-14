# BBRv3 advancement decision on r9

Status: decision set for [Decide whether the latency- and memory-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/747), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). The agent recommended parking BBRv3 as not advanced. The operator chose instead to advance it by explicit decision, and approved the set below in outline on 2026-10-14. Revised after one multi-agent consideration, whose findings are dispositioned [below](#consideration-dispositions). **Awaiting the operator's final acceptance.** Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

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
| Loopback STREAM control p95 | 1.485 (4) | 0.134 → 0.199 ms | At the adequate sample (#740): 1.849 [1.672–2.014], pooled 0.140 → 0.258 ms | Regression reproduced (#740); a depth effect and a depth-8 BBR baseline (#744, below); remedy not attempted |
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
- **L, [#746](https://github.com/the-sarge/quic-go-fast/issues/746#issuecomment-6173016346):** not run, by operator decision on 2026-10-14. Its reasons: even complete attribution would clear no cell, because by #741's arithmetic BBR's fixed bookkeeping alone exceeds the 10% allowance at the four S5 and S6 sender cells; #740 spent 124 observations on the same cells and labeled none; and the per-connection worst case is bounded by configuration (code reading). The memory cells therefore stay as #740 left them.

### The memory cost in absolute terms (descriptive)

- **Size.** On the readiness instrument (`ru_maxrss`), the largest median per-block excess is 12.28 MiB and the largest single block 13.36 MiB, both at the S6 STREAM receiver. #740's own runs, measured with an external 1 Hz `smaps_rollup` sampler, read up to 16.24 MiB at that cell in one block; those are different runs on a different instrument.
- **What is known.** Every candidate endpoint carries explicit BBR structures: about 2.0–2.15 MiB per WAN sender and 1.18–1.35 MiB per receiver, including the 1.14 MiB outcome ring (#740). Where #740's smaller-ring arm kept traffic comparable, removing ring bytes removed about 1.0–2.7 resident bytes per explicit byte. #741's estimate that the explicit structures alone exceed the allowance at the four WAN sender cells is, in its own words, "an extrapolation, not a measurement": the loopback senders pass (0.954 / 0.956) although they carry about 1.23 MiB of the same structures.
- **What is not known.** At the STREAM receivers, most of the excess is unexplained. #740's logical receive occupancy excess (0.0 MiB at S5, median 0.30 MiB at S6) weighs against unread or out-of-order data, and in some blocks the live-heap excess is well below the resident excess.
- **#746's configuration bounds** (code reading, not measurement): received data up to the connection flow-control window (15 MiB by default, `DefaultMaxReceiveConnectionFlowControlWindow`; the fixture uses 128 MiB), sent data up to about 10,000 tracked packets, BBR's bookkeeping caps, and roughly 2× for Go heap headroom; most of that ceiling is shared with Reno. These bound what a connection may hold; they are not a measured bound on the resident excess.
- **Consumer context, this record's own comparison** ([GridCast workload research](../2026-09-23-gridcast-bbr-workload.md)): an Attempt does not imply several concurrent QUIC data connections, though several user processes or Transfers can contend in unknown numbers. The default Sender working budget is 137 MiB and the receiver reservation 1 GiB. These are accounting reservations, not RSS limits, and the concurrency count is unknown, so they do not establish that the resident cost is affordable. The connection-count question passes to any adoption decision.

### The loopback control-latency cost in absolute terms (descriptive)

- **Size.** The rise is 0.065 ms in readiness (0.134 → 0.199), 0.118 ms at #740's adequate sample (0.140 → 0.258) and 0.081 ms in #744's descriptive reading (0.137 → 0.218). Reno's loopback p95 is about 0.14 ms, so the 20% limit allows about 0.03 ms.
- **Not noise.** The rise reproduces at an adequate sample (#740, 1.849 [1.672–2.014]), although the readiness-sized frozen-Reno A/A comparison crossed the same limit on the loopback DATAGRAM cell.
- **Two measured components.**
  - *A depth effect*: r9 ÷ r9 at depth 8 is 1.187 [1.058–1.551]. This is a demonstrated rise against depth 8, not a reading against matched Reno, so it is not compared with the 1.20 limit.
  - *A BBR baseline*: depth 8 ÷ frozen Reno is 1.345 [1.075–1.490], above the limit. Restoring depth 8 would therefore not clear the cell.
  - *Unknown*: the one named remedy class, a bytes- or time-ahead bound on BBR's queue, was not attempted, so whether it could clear the cell is unknown. The p95 rise is not assumed to decompose additively into these components.

### Mac records reviewed

As in the r9 decision. The [first decision's D2](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md#d2--a-scoped-operator-exception-for-three-attributed-flags) exception covers `fc4c1bf1` on macOS only (S6 control p95 1.547 / 1.481, +77.5 / +60.3 ms; S6 STREAM receiver RSS 1.574; S5 sender RSS 1.203 / 1.132); it is unchanged and is not extended. r9's Mac readiness is unmeasured. r8's netpoller kick is Linux-only; r9's queue depth applies on every platform.

### `main` today (source inspection)

`main` (`d3648918`) publishes BBRv3 as an experimental opt-in sender in `v0.62.1-fast.7`. Its README says "BBRv3 is unqualified and has known performance regressions; Reno remains the default." Its `internal/congestion` and `internal/ackhandler` trees are identical to the frozen component `e4f322cb` (`git diff --quiet`), so none of this map's corrections is on `main`.

## Decisions

### D1 — Verdict: advance r9 on Linux, by an explicit operator amendment of the advancement condition

Under the **unchanged** condition, r9 is **not advanced**: condition 2 fails with two attributed but unexcepted cells, and condition 3 fails with nine unresolved cells. That verdict stands and is not rewritten.

The operator then decides, explicitly and after reviewing r9's data, to **amend the advancement condition for r9 on Linux only**:
- condition 2's cause-attribution requirement is waived for the nine cells in D3;
- for the loopback STREAM control p95 cell, so is the remedy-before-exception rule;
- those nine cells receive **unattributed readiness exceptions**, a term defined in `CONTEXT.md` on this branch.

**What the amendment changes and what it doesn't.**
- *What stays:* the nine cells remain **raised and causally unresolved**, with every evidence label #740 and #744 assigned.
- *What changes:* their blocking effect on readiness is waived for this recorded r9/Linux decision only. Condition 3's unresolved-flag clause is amended for them, and only for them.
- *Unchanged:* condition 3's preservation clause holds in full, and no Reno-on-candidate cell is raised. Condition 1, condition 4, every readiness limit and every correctness and preservation gate are also unchanged.

With D2's attributed exceptions and D3's unattributed ones, every raised r9 cell on Linux is passing or excepted, and no preservation flag is raised. So BBRv3 is **advanced: r9 (`81e9dc8c`) deserves further qualification, on Linux**.

The operator weighed:
- **The benefit.** It is repeatable: 14.5× / 14.7× on S6, and forty Linux pairs above Reno across four revisions. Every goodput and CPU cell passes, and no preservation flag is raised.
- **The acceptance plan's framing.** The four limits are "review flags, not automatic rejection rules", and the owner judges after reviewing the data and its absolute values.
- **Diminishing returns.**
  - *Memory:* two attribution designs (#740 labeled no cell; #746 was not run, for the reasons quoted above).
  - *Latency:* the loopback cell has two measured components, and restoring depth 8 would not clear it. The operator chose neither a further cause stage nor the untested remedy.
- **The measured costs.**
  - *Memory:* a largest median excess of 12.28 MiB per endpoint (readiness maximum 13.36 MiB; 16.24 MiB in one #740 block on another instrument).
  - *Latency:* about a tenth of a millisecond of loopback control latency at its adequate sample.

**What advancement means.** r9 is worth qualifying. It is not a qualification, an adoption decision, a production merge or a default-controller change, and it covers r9 on Linux only. It is an operator judgment about costs, not a finding that they were explained.

**Is this a weakened gate?** The map rules out "weakening calibration/correctness gates". These four limits are readiness review flags, not calibration or correctness gates: no limit changes, every correctness and preservation gate holds, and r9's failure under the unchanged condition stays on record.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | None new. #738's 32-entry queue stays negative under its registered rule, kept by operator deviation 4. |
| Untested remedies | J's bytes- or time-ahead bound class: not attempted, because the mechanism read mixed, not queue wait. Whether it could clear the cell is unknown. |
| Missing evidence | The causes of the eight memory cells (one mixed, five inconclusive, one evidence gap, one instrumentation gap; L not run); the rest of the loopback latency mechanism; the cause of the depth-8 BBR baseline. |
| Attributed behaviour | S6 control p95, both workloads: the selected ProbeBW Up policy. |
| Demonstrated contract incompatibility | None. |
| Algorithm suitability | No finding against BBRv3. The unattributed exceptions are judgments about costs, not findings about the algorithm. |

### D2 — Attributed exceptions: S6 control p95, both workloads

Granted for r9 on Linux, covering #739's readiness evidence, at no more than STREAM 1.562 (+71.7 ms; 127.7 → 199.5 ms) and DATAGRAM 1.459 (+61.5 ms; 125.8 → 187.3 ms), as differences of arm medians.
- **Attribution:** the selected draft-06 ProbeBW Up policy, under #740's per-run rule. In all eight runs per workload, 98.6–99.4% (STREAM) and 98.5–100% (DATAGRAM) of samples above 25 ms fall in Up or in the Down entered from Up.
- **The weighing the r9 decision's D2 requires.** Readiness has five per-run p95 estimates per arm, each from 30 replies (150 replies per arm), and every block crosses the limit in both workloads. The timeline evidence covers every #740 run, so the attribution does not rest on one small sample. Thirty-reply p95 estimates remain imprecise individually.
- **Comparison with macOS.** STREAM's ratio is slightly above the macOS exception's 1.547, at a smaller absolute rise (+71.7 against +77.5 ms). DATAGRAM's ratio is below 1.481, at a slightly larger absolute rise (+61.5 against +60.3 ms).
- **Scope.** No tuning of Up. The macOS exception for `fc4c1bf1` is not extended. Like every exception here, these pre-clear no later measurement (D3, "Scope").

### D3 — Unattributed exceptions: eight memory cells and the loopback STREAM control p95

Granted for r9 on Linux only. Each cell stays **raised** in every report, with its ending and absolute values. None is a cause-based exception: the r9 decision's routing made these cells ineligible for one, and that remains true. The operator grants a different kind, by explicit decision.

| Cell | Evidence covered | Accepted magnitude |
| --- | --- | --- |
| S5 STREAM sender RSS | #739 readiness | 1.235; median excess 4.17 MiB |
| S5 STREAM receiver RSS | #739 readiness | 1.518; median excess 8.77 MiB |
| S5 DATAGRAM sender RSS | #739 readiness | 1.159; median excess 2.86 MiB |
| S6 STREAM sender RSS | #739 readiness | 1.673; median excess 8.92 MiB |
| S6 STREAM receiver RSS | #739 readiness | 1.898; median excess 12.28 MiB |
| S6 DATAGRAM sender RSS | #739 readiness | 1.542; median excess 7.46 MiB |
| Loopback STREAM receiver RSS | #739 readiness | 1.120; median excess 1.73 MiB |
| Loopback DATAGRAM receiver RSS | #739 readiness | 1.148; median excess 2.82 MiB |
| Loopback STREAM control p95 | #740's adequate-sample stage, and #739's readiness reading | 1.849 (+0.118 ms pooled p95, 0.140 → 0.258 ms) at #740's adequate sample. Readiness read 1.485 (0.134 → 0.199 ms); that is a historical reading, not an extra cap |

**Scope.** These exceptions dispose of the recorded evidence named in the table, for this advancement decision. They pre-accept nothing in later measurements:
- **not in qualification runs**, including runs on unchanged r9;
- **not on any other revision or platform.**

A later registration states how newly raised flags reach the operator for disposition. This decision neither predicts those outcomes nor pre-authorizes any exception.

**Memory.**
- **Why without attribution.** #746's reasons, quoted in the evidence above, plus #740's failure to label any cell. This record's own consumer comparison is descriptive; it does not establish affordability.
- **Not a dependence on composition.** These one-off exceptions do not depend on a known composition or on the whole-cell rule's constituent clearance, which remain unestablished.
- **What stays open.** The STREAM receivers' excess is unexplained. How often BBR approaches the shared receive ceiling in typical use, and the consumer's connection count, are adoption questions.
- **Not a standing budget.** The operator considered a standing per-connection budget and chose this one-off exception instead ([Rejected](#rejected)).

**Loopback STREAM control p95.**
- **The rule waived.** The remedy-before-exception rule (the intervention decision's D2; the r9 decision's "remedy … not attempted" row).
- **Why it is waived.** The operator accepts the whole measured cost at #740's adequate sample. The record reports absolute values beside the ratios, as the acceptance plan requires. Restoring depth 8 would not clear the cell (1.345 against frozen Reno). Whether the untested bound class could clear it is unknown, and the operator chose not to test it here.
- **What happens to the remedy.** The class stays **untested, not disposed of**, and passes to the production port as a lead.
- **The limit itself.** The 20% latency limit is unchanged, and no absolute tolerance rule is adopted.

**Applicable routing, as disposed.**

| Route (r9 decision D6, #740) | Disposition here |
| --- | --- |
| J: depth raises latency; mechanism mixed (an instrumentation gap as registered) | Evidence unchanged. The operator accepts the whole recorded latency cost instead of a further cause stage. |
| J: remedy not attempted | Untested, not disposed of. The waiver covers the remedy-before-exception rule for this cell only, and the class passes to the port as a lead. |
| J: BBR baseline | Describes the depth-8 configuration; decides no r9 cell. |
| J: class disposed of, combined with the baseline reading | Not triggered: the class was never tested. |
| #740's operator decision 2 (loopback STREAM control p95, *regression reproduced*) | The result stands. Its advancement block is superseded, for this cell, by the unattributed exception, r9 on Linux only. |
| K: not run | No new readiness evidence; #739's flags are the latest. |
| L: not run; memory endings as #740 left them | No new attribution. Cause-based eligibility, including under the whole-cell rule, remains unestablished. |
| S6 control p95, survivor r9 | Standing attribution, plus D2's attributed exceptions. |
| Flags persist | Reported here as unresolved. Their blocking effect is waived by this decision only. |
| Preservation | No Reno-on-candidate cell is raised. Condition 3's preservation clause is unchanged. |

### D4 — The smallest qualification that would change an adoption decision: L6 coexistence, named and not started

**Choice.** The acceptance plan's L6: a competing QUIC Reno flow sharing a 100/100 Mbps, 100 ms bottleneck.
- Long rows (60 + 300 s), both workloads, as the plan requires, five pairs;
- the plan's equal-RTT, unequal-RTT and departure/re-entry phases;
- reporting each participant's useful delivery and control latency.

**Why L6.** The adoption question is whether real traffic should use BBRv3 instead of Reno. The local evidence covers loopback, S5 and S6 on Linux, and the S6 benefit is not in doubt. What could reverse an adoption call is BBRv3 displacing the traffic that shares its path, and no measured path exercises that. L6 tests it directly.

**L8 is conditional.** L8 (the same candidate, with independent state) runs only if the operator establishes that GridCast will run several BBR connections over one bottleneck. The GridCast research leaves concurrent Transfers to be measured "after the intended sharing/concurrency model is established".

**The other candidates, deferred** (D8):
- **S8, CE response.** The cheapest: the relay already models marking. But classic-ECN marking on GridCast's paths is unestablished, and GridCast's ordinary Windows path has no ECN, so a result is less likely to change the call.
- **L4 / L5, low-rate pacing.** A narrow regime, and L4 needs TCP competitors.
- **C4 items 1 and 3.** Model-conformance edges, better answered by deterministic tests than by a qualification run.
- **L7, TCP CUBIC.** The most realistic competitor, but it needs kernel TCP through the emulated bottleneck on owned hardware. The Q1 campaign's cloud receipt for L7 does not establish that on `minimax`.

**What a result would mean.** Registered fairness classifications inform the operator's adoption judgment; any adoption threshold needs the operator's acceptance before data.
- *Adverse* (BBRv3 displaces the Reno flow beyond the registered criterion): evidence against adopting the revision as it stands. Its cause needs diagnosis before any claim about the algorithm.
- *Acceptable*: removes this obstacle only. Adoption still needs the other deferred evidence and the operator's judgment.
- *Inconclusive*: leaves the question open.

**Required evidence**, binding on whichever effort runs it; its estimators belong to that effort's reviewed registration:
- **Fairness criteria fixed before data.** The plan sets no numeric coexistence flag, so the registration states what counts as acceptable sharing, for example the competitor's useful delivery against a Reno-against-Reno baseline under the same schedule, with an explicit inconclusive ending.
- **A calibrated shared bottleneck.** The relay in #739's record forwards one sender/target pair (source inspection). A multi-flow relay with per-flow propagation delay, a competitor runner and per-participant metrics are measurement-only aids to build and calibrate. The Q1 campaign's gateway, competitor schedule and TCP mode are candidates for reuse, but their cloud receipt does not establish owned-host reuse.
- **Unchanged limits.** The readiness limits, preservation rules and correctness gates apply unchanged, and the qualified revision is named. No exception in D2 or D3 covers any qualification measurement, on r9 or on the port.
- **The revision whose adoption is in question.** Any new revision re-measures. If the production port (D5) lands first, the port effort measures its readiness and obtains an explicit advancement disposition from the operator before qualification runs on it; r9's amendment and exceptions do not transfer.

**Cost and authorization.** Owned Linux hardware (`minimax`), with no paid resource and no ledger charge. A rough whole-effort estimate for L6, for scheduling only:

| Part | Estimate |
| --- | --- |
| Fixture build, calibration and development runs (the uncertain part) | 4–8 h |
| Registration review | 1–2 h |
| Counted runs (2 workloads × 2 focal controllers × 5 pairs × 360 s) | 2 h on `minimax` |
| Analysis | 1–2 h |
| **Total** | about 8–14 h, 3–4 h of it on `minimax` |

A conditional L8 would add 2 h of counted runs and about 1 h of analysis. This decision does not start either; the operator starts L6 when an adoption call is wanted. No paid experiment is proposed, so none needs a decision value or cost now.

### D5 — Next: a separate production port, not started here

The operator's next step is a **separate Planit effort** to carry r9's correction into `main`'s experimental opt-in sender, still labeled unqualified, so that the opt-in sender is no longer the frozen component. This decision neither starts that effort nor authorizes a merge. At acceptance, an unstarted proposal card for it links this record.

The port inherits from this map:
- r9's kept changes over the frozen component: C1–C4, the bounded ECN feedback, quantum-release pacing with its D08 amendment and its drop-tail overflow disclosure, the kept per-packet changes, the Linux-only netpoller kick, and the 32-entry BBR send queue;
- two open costs:
  - #734's run-loop wake increase (6–16% per useful GiB on S5);
  - #738's S5 DATAGRAM sender CPU rise (+5% against r8; 0.953 of frozen Reno in readiness, passing);
- the untested bytes- or time-ahead bound class;
- the two BBR-only per-packet allocation sites the map's fog names as production-slicing leads;
- the production follow-ups [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630), [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636) and [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613);
- the undo-sentence amendment, if spurious-undo phase restoration is adopted;
- the glossary entries that live on record branches;
- the open memory and loopback-latency questions above.

**Untriggered conditional leads.** Two items behind the r9 decision's memory routing are untriggered: a bookkeeping representation remedy (how fixed bookkeeping is held, its GC headroom), and a design-bound decision on the bookkeeping bounds. The earlier ring trigger was not met, and L, whose outputs were their route, did not run. Considering either still needs that named evidence, or a separate explicit amendment of the prerequisite. D3's memory exceptions supply neither evidence nor permission to change a bound.

**The port's binding requirements:**
- **It is a new revision.** No exception in D2 or D3 covers it. The port effort measures its readiness and obtains an explicit advancement disposition from the operator before any qualification runs on it, and any raised cell needs a fresh decision under the applicable criteria.
- **The map's preservation rules apply in full**, including default Reno and every correctness gate.
- **Any contract change** remains a separate explicit design decision.

### D6 — BBRv1 closed out of scope

The BBRv1 fallback is untriggered: BBRv3 was advanced, not declined. Under D8 of the r9 decision, [Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) close as out of scope, with one line in the map's Out of scope section.

This answers the destination's BBRv1 question: BBRv1 is not investigated. No finding about its suitability is made, and implementing or qualifying it would need a later explicit decision.

### D7 — No paid experiment, no campaign, no Mac readiness requirement

- **No paid resource.** None is proposed or authorized, the cloud heartbeat stays paused, and the ledger and its reservations are untouched.
- **Q2 is unchanged.** [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) stays incomplete and separate.
- **No Mac readiness requirement.** New-revision Mac readiness is not required for Linux-only advancement. Native evidence on the platforms the GridCast research tentatively names as most important (macOS/arm64, Linux/amd64 and Windows/amd64) remains an adoption question.

### D8 — The map reaches its destination

The destination is met:
- the original BBRv3 regression is explained, and a local correction that addresses its supported causes is demonstrated (r9's remaining costs are accepted in D2 and D3, not explained);
- the qualification question is decided: advanced, for r9 on Linux;
- the BBRv1 question is decided: not investigated, as an untriggered fallback.

The remaining fog moves to Out of scope, because each item lies beyond this destination:
- **Coexistence (L6, then a conditional L8):** D4, not started.
- **The other adoption evidence:** CE response (S8), low-rate pacing (L4/L5), TCP CUBIC coexistence (L7), deterministic tests for C4 items 1 and 3, and native macOS and Windows evidence. These are deferred, unassigned until an adoption call is wanted.
- **The production port and its inputs:** D5's proposal card, carrying the two untriggered conditional leads with their status.

The map then closes.

## Rejected

- **Park BBRv3 as not advanced** (the agent's recommendation). It follows the unchanged condition, but the operator judged r9's measured costs acceptable on Linux, given its benefit, the plan's review-flag framing and the diminishing returns of further attribution.
- **Decline further work.** The implementation is no longer inefficient (every goodput and CPU cell passes), and declining would route to BBRv1.
- **A standing per-connection memory budget**, for example 15 MiB per endpoint, one default connection receive window. The operator preferred a one-off exception for r9's recorded evidence, so later revisions and qualification runs get no pre-accepted allowance.
- **A standing absolute latency tolerance**, for example 1 ms on paths whose Reno p95 is below 1 ms. The operator chose a scoped exception instead, and the 20% limit is unchanged.
- **A further cause stage for the loopback latency, or testing the bound class now.** A choice the r9 decision left to this one; the operator chose the exception.
- **Running L (#746), or re-running #740's removal arms.** Even full success clears no cell.
- **Shrinking the bookkeeping now.** That is a design-bound change whose evidence prerequisite is unmet (D5).
- **Running L6 and L8 together, unconditionally.** L8 adds two counted hours without a distinct adoption question until concurrent BBR Transfers are established.
- **Running the coexistence qualification now, on r9.** Its evidence would have to be repeated on the ported revision, so it waits until an adoption call is wanted.
- **Any paid run, Q2 resumption, host-setting change or BBRv1 work.**

## Map changes on acceptance

- A resolution comment on #747, the issue closed, and one Decisions-so-far line on #666. The line records that r9 is advanced by an operator amendment and that the verdict under the unchanged condition is preserved.
- #673 and #674 closed as out of scope, with one Out of scope line.
- Not yet specified cleared into Out of scope lines, following D8.
- An unstarted proposal card for the production port (D5), linking this record and carrying its inputs, the two untriggered conditional leads and the port's binding requirements.
- The map closed: destination reached.
- Glossary: **Readiness exception** added to `CONTEXT.md` on this branch.

## Consideration dispositions

RAS run `20261014T173943-ceb823d43043fb2459dc9274` (`ras consider`, kind design) on draft `2ba439d6`. Its context was the r9 decision record, the acceptance plan and [memory-excess.json](memory-excess.json), with the ticket text, the J/K/L resolutions and the operator's verbatim choices quoted in the prompt.
- **Reviewers:** five completed (claude-fable, claude-opus, codex-astra, codex-sol, grok). agy-argon failed in the review round with a structured-delivery protocol error and produced no findings.
- **Adjudication and synthesis:** five adjudicators produced 43 finding clusters and 215 adjudications, and the synthesis completed.
- **Verdict:** the synthesis preserved the operator's verdict ("it does not reopen that verdict") and found no correctness, protocol-compliance or preservation-gate violation.

Every fix below is accepted. The synthesis's memory arithmetic check reproduces 12.27734375 MiB (largest median), 13.35546875 MiB (largest block) and 2.0 counted hours per Long row.

| Fix | Disposition | Where |
| --- | --- | --- |
| F1 — The memory exceptions must not pre-accept qualification runs | Fixed. The exceptions dispose only of the recorded evidence. Qualification runs, including runs on unchanged r9, and other revisions get nothing pre-accepted, and a later registration routes new flags to the operator. Repeated in D4; the glossary says exceptions do not pre-clear later measurements. | D3 Scope; D4; `CONTEXT.md` |
| F2 — Reconcile unresolved evidence with the waived blocking effect | Fixed. The nine cells stay raised and causally unresolved, their blocking effect is waived for this decision only, and the preservation clause is intact. An applicable-routing table disposes of every relevant row. | D1; D3 routing table |
| F3 — An unsupported remedy-impossibility claim and a wrong depth comparison | Fixed. "Cannot clear" is removed: restoring depth 8 would not clear the cell, and the untested class's effect is unknown. The 1.187 depth effect is measured against depth 8, not matched Reno, and is not compared with the limit. Both "each above the limit" claims are removed. | Evidence; D1; D3 |
| F4 — The memory summary and its provenance | Fixed. 12.28 / 13.36 / 16.24 MiB are reported with their provenance; the receive window is no longer presented as a bound; #746's reasons are separated from this record's consumer comparison, which is marked as not establishing affordability; #741's extrapolation caveat and the passing loopback senders are restored. | Evidence; D1; D3 |
| F5 — The port-to-qualification handoff | Fixed. The port effort measures its readiness and obtains an explicit advancement disposition before qualification, and raised cells need a fresh decision; carried in the proposal card. | D4; D5 |
| F6 — The unmet bookkeeping-bound prerequisite | Fixed. The bookkeeping remedy and design-bound decision are untriggered conditional leads, and the memory exceptions supply neither evidence nor permission. | D5; Rejected |
| F7 — The proposal card and the routing of deferred work | Fixed. The card is an acceptance action. D5 adds #734's wake increase and #738's CPU rise. Coexistence routes to D4, and the other adoption evidence is listed as deferred and unassigned. | D5; D8; Map changes |
| F8 — Why this qualification is the smallest | Fixed. L6 is selected and L8 made conditional on established concurrent BBR Transfers. Adverse, acceptable and inconclusive implications are stated; adoption thresholds need operator acceptance; the cost is recounted (2 h counted for L6); Q1 gateway reuse is unestablished on owned hardware. | D4; Rejected |
| F9 — The latency acceptance magnitude | Fixed. 1.849 (+0.118 ms) at #740's adequate sample is the accepted magnitude, and 1.485 is labeled a historical readiness reading. The small-denominator clause is no longer a basis; the plan's absolute-value reporting is cited and the acceptance is attributed to the operator. | D3 |
| F10 — Closing and comparison wording | Fixed. The "explained" regression is scoped to the original one, r9's costs are accepted rather than explained, and BBRv1 is an untriggered fallback with no suitability finding. DATAGRAM is compared at +61.5 against +60.3 ms, the thirty-reply wording is softened, and the three-platform wording is restored. | D2; D6; D7; D8 |

Not acted on, as the synthesis advised:
- reversing advancement or asking for renewed approval;
- requiring a further cause stage;
- treating whole-cell eligibility as vacuously satisfied;
- changing the memory estimators;
- reading the fog section as a TODO;
- using Linux r9 occupancy against the macOS attribution;
- starting any experiment, port, paid resource or further review round.

## Limits

This is a planning decision. It claims no qualification, readiness on another platform or adoption, and authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited. The memory excesses in MiB are arithmetic on #739's recorded peaks. The relay's single-flow design, `main`'s identity with the frozen component and the configuration bounds come from source inspection, not measurement. The unattributed exceptions record the operator's judgment, not an explanation of the costs they cover.
