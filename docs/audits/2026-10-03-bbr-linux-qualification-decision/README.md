# BBRv3 qualification decision after the Linux diagnostic

Status: **proposed** decision set for [Decide whether the Linux-measured BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/713), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Awaiting operator approval. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-qualification-decision`, based on `4323dae8` (the [Linux diagnostic](../2026-10-03-bbr-linux-diagnostic/README.md) record). That record contains the [previous qualification decision](../2026-10-03-bbr-qualification-decision/README.md), the [WAN-corrected re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) and the candidate `fc4c1bf1`.

## Question

On the combined Mac record and the Linux diagnostic evidence, should BBRv3 advance to further qualification, continue resolving its implementation, or be declined? Apply the advancement condition in D1 of the previous decision. Dispose of any extension of the scoped D2 exception, any newly attributed cost, and the remaining blockers. Classify the reason as implementation, algorithm trade-off or missing evidence. If advancing, choose the smallest qualification that would change an adoption decision, and present the cost of any paid experiment before authorization.

## Normative constraints (quoted)

Previous decision, D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": the S6 benefit holds; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results". And: "If it is not met … D5 still runs and decides among another resolution step, a declared exception, or declining, with the reason classified."

Previous decision, D2: "The exception does **not** carry over automatically. On Linux, or on any new revision, each corresponding cell is re-attributed under its registered rule … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone." "Any necessary contract change is a separate explicit design decision." Out of scope: "weakening calibration/correctness gates".

## Evidence base

Both records measure the same candidate, `fc4c1bf1`, against frozen Reno `e4f322cb`, with the same fixture, paths, limits and median-of-five-block rule. The Mac record ran on one Apple M4 Max; the Linux record on `minimax` (AMD Ryzen AI MAX+ 395, Linux 7.0), with four isolated physical cores per endpoint and no contaminated observation. Ratios are medians of per-block ratios against the same block's Reno.

### Advancement condition, checked

| D1 condition | Mac (`fc4c1bf1`) | Linux (`fc4c1bf1`) | Met? |
| --- | --- | --- | --- |
| 1. Repeatable S6 benefit | 12.31× / 14.09×, 5/5 each | 12.07× [9.52–13.37] / 11.09× [8.94–12.40], 5/5 each | Yes, on both |
| 2. Every flag passes or is attributed and excepted | Three cells excepted (D2); five unresolved | Three attributed, none excepted; many unresolved | No |
| 3. No unresolved flag, no preservation flag | Five unresolved, one preservation CPU flag | Fifteen unresolved cells, two preservation flags | No |
| 4. Mac flags reviewed, platforms named | Reviewed below | — | Reviewed |

### Flags by kind, both platforms

| Kind | Mac | Linux | Same? |
| --- | --- | --- | --- |
| S6 control p95 | 1.547 / 1.481, Up policy (excepted) | STREAM 1.541, Up policy; DATAGRAM passes (1.134) | Same cause, same or smaller |
| Loopback goodput | passes (0.975 / 0.985) | **0.770 / 0.795**, unresolved | New on Linux |
| S5 goodput | passes (0.969 / 0.979) | **0.913 / 0.901**, unresolved | New on Linux |
| Sender CPU per GiB | S5 1.170 / 1.320, unresolved | loopback 1.261 / 1.208, S5 1.237 / 1.108, unresolved | Persists, now localized to user space |
| Receiver CPU per GiB | S5 DATAGRAM 1.138, unresolved | loopback STREAM 1.189, S5 1.155 / 1.131, unresolved | Persists |
| Memory, attributed | S6 STREAM receiver (delivery), S5 sender (bookkeeping) | S5 DATAGRAM sender (bookkeeping), S5 STREAM receiver (delivery) | Partly; compositions changed |
| Memory, unresolved | three mixed cells | S5 STREAM sender, S6 STREAM sender and receiver, S6 DATAGRAM sender | Persists |
| Preservation (Reno on candidate) | S5 DATAGRAM CPU 1.192 / 1.158 | S5 DATAGRAM sender RSS 1.152; loopback DATAGRAM p95 1.226 | Different cells; Mac CPU flag does not recur |

### What the Linux record adds

- **The extra CPU appears only when BBR is selected.** Reno built from the candidate executes frozen Reno's work within 0.2% on every counter. The candidate sender executes 1.188× / 1.123× Reno's instructions per useful GiB on S5, in every block, while A/A Reno stays at 1.000.
- **It is user-space work.** User instructions are 1.358× / 1.267× and user cycles 1.494× / 1.336×. Kernel instructions are 0.978× / 0.944×, and wakeups, context switches and futex calls are below Reno's. That is about 11,000 extra user instructions per packet on Reno's 31,600. This rules out the Mac record's leading hypothesis, kernel send and wake work.
- **It is spread out.** Under the registered rule, no group held half the excess (BBR and bookkeeping 0.39–0.44, other user code 0.29–0.37, runtime scheduling 0.14–0.21, allocation and GC 0.03–0.16), so localization was inconclusive and no discrimination stage ran. Descriptively, the largest increases sit in the BBR-enabled per-packet path: mutex and channel synchronization (about 0.9–1.1 G of the sender's 4.8–5.0 Gcycles/GiB excess), the per-packet delivery-record map in `captureCongestionSend` (about 0.3 G), recovery-evidence lookups, per-packet capability queries, send-credit reservations, and congestion and BBR ECN feedback. These are leads, not causes.
- **Allocation is a minor share.** Allocation and GC hold 0.03–0.05 of the sender excess. That bounds the Mac record's two allocation sites to a small part of the cost.
- **Loopback goodput tracks sender CPU, descriptively.** Both arms use about 2.3 cores of sender CPU on loopback. A sender limited by per-packet work at 1.26× / 1.21× the cost per GiB would reach about 0.79 / 0.83 of Reno's goodput, near the measured 0.770 / 0.795. This was not tested.
- **S5 goodput is not a CPU limit.** At about 85 Mbit/s and 12.6 s/GiB, the S5 sender uses about 0.12–0.13 cores. Reno's S5 goodput is the same on both hosts (95.9 / 94.4 Mbit/s), but the candidate's is lower on Linux (87.5 / 85.0) than on the Mac (92.9 / 92.4). The counted Linux runs spend 59–80% of the window in Cruise, 9–26% in Up, 5–10% in Down, 2–3% in Refill and 2–4.5% in ProbeRTT. No stage attributes the gap.
- **Both preservation flags look like noise, but the rule cannot say so.** In the S5 DATAGRAM sender RSS cell, frozen Reno and Reno on candidate both land at the same two discrete levels (about 18.4 and 20.9 MiB). The A/A ratio spans 0.92–1.12, and Reno on candidate spans 0.861–1.154. In the loopback DATAGRAM p95 cell, values are 0.2–0.3 ms, and the A/A ratio spans 0.36–4.27. The registered preservation rule has a noise clause for CPU only. Any other raised preservation flag stays unattributed.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced**. Conditions 2 and 3 fail on Linux. Fifteen cells are raised and unresolved, and two preservation flags are raised. Advancing would waive limits for costs that are not explained.

BBRv3 is **not declined**. The useful benefit is now repeatable on two platforms: 11–14× Reno's receiver-verified goodput on S6, in all twenty pairs, at an eighth to a third of Reno's sender CPU per useful GiB. The largest open cost now looks like overhead in our integration rather than a property of the algorithm. It appears only when BBR is selected, it is user-space work, and it is spread across integration machinery rather than concentrated in model arithmetic.

**Reason, classified per flag group:**

| Group | Classification | Basis |
| --- | --- | --- |
| Sender and receiver CPU; loopback goodput | **Implementation**, supported but not yet shown by intervention | BBR-only, user-space, integration-shaped excess. Loopback goodput matches a CPU-limited sender, descriptively. |
| S5 goodput on Linux | **Missing evidence** | Not a CPU limit and not seen on the Mac; no stage attributed it. Model behaviour and a Linux timing effect are both open. |
| Unresolved memory cells | **Missing evidence** | Mixed heap-site composition under the unchanged 70% rule. |
| Preservation flags | **Missing evidence**, likely measurement noise | Executed work equals frozen Reno's; values sit inside A/A ranges. The rule has no clause to classify them. |
| S6 STREAM p95; S5 DATAGRAM sender RSS; S5 STREAM receiver RSS | **Algorithm trade-off or design bound** | ProbeBW Up policy; declared bookkeeping bounds; loss-driven reassembly at the delivered rate. |

The verdict does not predict that the next step will succeed. It records that the evidence now points at a specific, testable implementation cost, and that resolving it is the cheapest way to learn whether a competent BBRv3 is ready.

### D2 — No exception extended or granted now

The D2 exception is **not extended** to Linux, and **no new exception** is granted. No exception could make `fc4c1bf1` advance while unresolved flags remain. The next step builds a new revision, and under D2's own rule every cell must be re-attributed on that revision anyway.

Recorded for the next decision, without clearance:
- **S6 STREAM control p95 on Linux:** 1.541, the same Up-policy cause, at the same or smaller magnitude than the Mac cell. It is the strongest candidate for an extension if the next revision reproduces it.
- **S5 DATAGRAM sender RSS on Linux:** 1.144 (+2.5–3.0 MiB), design-bounded bookkeeping. Same cause as the Mac cell, slightly larger.
- **S5 STREAM receiver RSS on Linux:** 1.441 (17.6 → 25.3 MiB). Newly attributed to delivery data, as reassembly behind 0.25% overflow loss. Unresolved on the Mac.
- **S6 STREAM receiver RSS** and **S5 STREAM sender RSS** changed composition on Linux, so they are not clearance candidates under D2's terms.

The macOS D2 exception stays on record for `fc4c1bf1`, unchanged.

### D3 — Preservation flags: register a noise clause; allow an exception only with it

The two Linux preservation flags stay **raised and unattributed**. The next readiness record must register, before its data, a noise clause for Reno-on-candidate RSS and latency cells, modelled on the existing CPU clause. A cell is **attributed to measurement noise** when both of these hold:

- the counters stage shows Reno on candidate's instructions per GiB within [0.97, 1.03] of frozen Reno's, at both endpoints;
- the cell's readiness ratio lies inside the same cell's A/A per-block range in the same record.

The flag stays raised either way and is reported with its values.

**Judgment call.** D1's condition 3 is amended for the next decision. A preservation flag attributed to measurement noise under that pre-registered clause may be covered by a D2-form operator exception (revision, platform, cell, magnitude). Any other raised preservation flag still blocks advancement. No limit changes. The amendment only lets a noise-attributed flag be weighed rather than block automatically. Without it, a measure whose own A/A arm crosses the limit (loopback STREAM A/A p95 reached 1.294) could block advancement forever, however unchanged default Reno is.

### D4 — Next: remove the BBR-enabled per-packet work by measured interventions, then re-demonstrate on Linux

Two task tickets, in sequence.

**Ticket A — Reduce the BBR-enabled per-packet path's work, attributing each change by intervention.** A new branch from `fc4c1bf1`.

- **Scope.** The five descriptive leads, each as a separate hypothesis registered before any change:
  - per-packet synchronization crossings (mutex and channel);
  - the per-packet delivery-record map;
  - recovery-evidence lookups;
  - per-packet capability queries;
  - send-credit reservations, including the `&sendReservation{}` allocation.
- **Method.** Each change is one commit, contract-preserving and semantics-preserving. Each carries equivalence evidence against `fc4c1bf1`, in the form #706 and #709 used: the same congestion inputs, decisions and recorded events on the same traces. The existing gate tests must also pass.
- **Measurement.** Each commit's effect is measured on `minimax` with the #712 counters stage: S5, both workloads, six blocks, user instructions and cycles per packet at both endpoints, against the preceding commit and frozen Reno. The #712 A/A noise is about ±0.1% in instructions, so a lead's measured share is its demonstrated cost. Positive, null and negative results are all reported.
- **Keep rule.** A change is kept only when it is equivalent, the targeted work falls, and receiver integrity and Reno on candidate's counters (within 0.2%) hold.
- **Not allowed.** No parameter or model change, no change to declared bounds, no batching of the send path (GSO or `sendmmsg`), and no change that alters Reno's path.
- **Stop.** A change that needs a contract change, or that cannot be made equivalent, stops that lead. It is reported to the next decision and not forced. The ticket reports the residual excess per packet against Reno, attributed or not.

**Ticket B — Re-demonstrate the per-packet-corrected candidate on Linux.** Blocked by A. It reruns #712's readiness stage on `minimax` for the resulting revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout. Every stage below is registered before its data:

- **Loopback goodput.** Does it recover in proportion to the fall in sender CPU? The comparison is the new revision against `fc4c1bf1`, which discriminates a CPU-limited sender from other causes.
- **S5 goodput.** A timeline of phase time, delivery-rate estimate against the bottleneck, pacing lateness and loss-response events. If the Linux timeline alone cannot separate model behaviour from a Linux timing effect, a bounded macOS S5 candidate timeline is run with the same instrument. That comparison is attribution only and sets no flag.
- **Receiver CPU.** A split of the measured window from the whole run.
- **Memory.** #711's heap-site rules, unchanged.
- **Preservation.** D3's noise clause.
- **D2 cells.** Re-attributed on the new revision.

The readiness results are Linux results. A Mac readiness rerun is not included. Any advancement would name Linux only, unless the operator asks for macOS coverage.

**Bounds.** Owned hardware only, with no paid resource or Q2 ledger charge. Frozen one-ticket aids, not a maintained framework. Ticket B's readiness stage is #712's size: 110 observations, about 63 minutes of fixture time. Ticket A may build at most one revision per lead.

### D5 — No paid experiment and no campaign

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused. The ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) and its evidence are unchanged. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D6 — A fresh qualification decision follows Ticket B, whatever it finds

A new grilling ticket, blocked by Ticket B, re-asks this question on the new revision's Linux evidence, with both preserved Mac records reviewed. It runs on every outcome. If Ticket A finds that the excess cannot be removed without a contract change, that decision weighs a separate contract-change design decision against declining for implementation reasons.

### D7 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. #673 is re-wired to be blocked by the D6 decision ticket. The open cost sits in per-packet integration work that BBRv1 would share, so a version change is not a remedy now.

## Rejected

- **Advance on the two-platform S6 benefit.** It would waive the CPU, goodput, memory and preservation limits for unexplained costs.
- **Decline BBRv3.** The largest open cost is shaped like avoidable implementation overhead, and the benefit is robust on both platforms.
- **Extend D2 to Linux now.** It cannot unlock advancement, and the next revision needs re-attribution anyway.
- **Another localization round on `fc4c1bf1` with finer groups.** The excess is diffuse, so finer partitions would likely still lack a majority lead. Interventions discriminate directly, and the kept changes are the correction.
- **Fix only the two allocation sites.** Linux bounds allocation and GC to 0.03–0.05 of the sender excess.
- **Batch the paced send path (GSO or `sendmmsg`).** It changes both arms' send path and would offset BBR's per-packet work rather than remove it.
- **Tune Cruise headroom, ProbeBW Up or ProbeRTT to close the S5 goodput gap.** That is speculative tuning before attribution.
- **Shrink the bookkeeping bounds.** It is a design-bound change without a consumer requirement behind it.
- **Clear the preservation flags by A/A overlap alone, without a pre-registered clause.** That would be a post-hoc rule.
- **Rerun Mac readiness on `fc4c1bf1`.** It would add no information.
- **Any paid run.**

## Map changes on acceptance

- A resolution comment on #713, the issue closed, and one Decisions-so-far line on #666.
- New task tickets A and B and the D6 grilling ticket, wired A → B → D6. #673 is re-wired from #713 to the D6 ticket.
- Fog updated: the per-packet leads and the Linux S5 goodput question become tickets; the preservation noise clause is recorded in D3 and Ticket B; the bookkeeping-bound and adoption-evidence fog stays.

## Consideration dispositions

None yet. A `/ras-consider` pass is offered before approval because D3 amends an advancement condition and D4 commits to a correction approach.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its descriptive figures come from the two records cited above; it takes no new measurement.
