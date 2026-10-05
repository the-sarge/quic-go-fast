# BBRv3 qualification decision after the per-packet interventions

Status: proposed decision set for [Decide whether the intervention-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/716), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-05. Revised after a multi-agent consideration (RAS run `20261005T224747-d0a0f4291c4f339e8616722e`); the [disposition table](#consideration-dispositions) records every finding. Awaiting final operator approval. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-intervention-qualification-decision`, based on `36f7ce01` (the [Linux re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) record). That record contains the [per-packet intervention record](../2026-10-03-bbr-per-packet-interventions/README.md) and the surviving revision `d0fabc4d` (r6). The earlier decisions and Mac records live on sibling record branches and are linked by commit.

## Question

On the surviving revision's evidence, with both preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration, open a separate contract-change design decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged D1 advancement condition; preservation flags still block. Dispose of any D2 extension, any newly attributed cost and the remaining blockers. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability.

## Normative constraints (quoted)

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": "the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload)"; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers." D2: "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "D1's condition 3 is **unchanged**: a raised preservation flag blocks advancement." D6: options include "a **bounded, evidence-supported redesign of BBR-specific integration** (the failure of five selected interventions does not show that every contract-preserving redesign is unavailable)" and "a separate contract-change design decision, if a needed change is demonstrated"; "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Any necessary contract change is a separate explicit design decision. A throughput improvement does not excuse a correctness regression." Out of scope includes "weakening calibration/correctness gates" and "altering unrelated host/kernel/security settings".

[BBRv3 design](../../designs/bbrv3.md), D08 (pacing): "Pace registration opportunities with a token/debt balance capped at Q, initialized to Q, debited by actual UDP payload bytes admitted and replenished at the effective rate. When credit cannot cover the next paced admission, the deadline is the exact time for credit to reach Q, rounded up to the monotonic clock unit … At high rates the 64KiB quantum needs sub-millisecond opportunities, while actual OS wakeup precision remains a measured limitation. … Delayed wakeups cannot accumulate more than Q. … low actual OS wakeup precision may lower throughput but never authorizes a larger unreported burst." The quantum-release amendment adopted under [#710](https://github.com/the-sarge/quic-go-fast/issues/710) is part of the candidate.

## Evidence base

All readiness figures are from the [Linux re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) and its `summary.json`/`attribution.json`, on `d0fabc4d` against frozen Reno `e4f322cb` on `minimax` (AMD Ryzen AI MAX+ 395, Linux 7.0, four isolated physical cores per endpoint): 110 counted readiness observations and 64 Linux plus 4 macOS attribution observations, all exiting cleanly with receiver integrity. Two S5 STREAM blocks were contaminated by a VM and rerun clean under the registered rule; under #712's rule on the original blocks, no flag changes status. Ratios are medians of five per-block ratios against the same block's Reno, with blocks crossing the limit in parentheses.

### Advancement condition, checked on `d0fabc4d` (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 11.39× [9.44–13.22] STREAM, 10.81× [9.73–12.57] DATAGRAM, 5/5 pairs each; sender CPU per useful GiB 0.119 / 0.127× Reno | Yes |
| 2. Every flag passes, or is attributed and excepted | Two cells attributed (S6 STREAM p95, S6 STREAM receiver RSS) and two S5 goodput cells (STREAM, DATAGRAM) attributed by a discriminating stage; none excepted | No |
| 3. No unresolved flag, no preservation flag | Nine unresolved candidate cells; **no** preservation cell on Linux `d0fabc4d` | No (unresolved cells) |
| 4. Mac records reviewed, platforms named | Reviewed below; any advancement would name Linux only | Reviewed |

### Flags on `d0fabc4d`, against `fc4c1bf1` on the same host (#712)

| Status | Cell | `fc4c1bf1` (#712) | `d0fabc4d` | Absolute, Reno → candidate |
| --- | --- | --- | --- | --- |
| Now passes | Loopback STREAM, every limit | goodput 0.770, sender CPU 1.261, receiver CPU 1.189 | 0.962, 1.085, 1.031 | 4677.8 → 4494.9 Mbit/s |
| Now passes | S5 DATAGRAM sender CPU; S5 DATAGRAM sender RSS | 1.108; 1.144 | 1.047; 1.096 | 11.38 → 11.88 s/GiB |
| Now passes | Every Reno-on-candidate cell | two raised (S5 DATAGRAM sender RSS 1.152; loopback DATAGRAM p95 1.226) | 1.002; 0.721; all medians within 1.018 CPU, 1.031 RSS | — |
| Attributed | S6 STREAM control p95 — selected ProbeBW Up policy | 1.541 | **1.588 (5)** | 128.2 → 200.3 ms |
| Attributed | S6 STREAM receiver RSS — delivery data (81% / 72%) | 1.754, unresolved | **1.759 (5)** | 14.13 → 26.17 MiB |
| Attributed by a discriminating stage, no remedy tested | S5 goodput — sender timing on Linux | 0.913 / 0.901 | **0.917 (5) / 0.899 (5)** | 95.9 → 87.9 / 94.4 → 84.9 Mbit/s |
| Unresolved | Loopback DATAGRAM goodput; sender CPU | 0.795; 1.208 | **0.858 (5); 1.111 (5)** | 4637.3 → 3980.9 Mbit/s; 4.53 → 5.03 s/GiB |
| Unresolved | S5 STREAM sender CPU | 1.237 | **1.183 (5)** | 10.11 → 11.96 s/GiB |
| Unresolved | S5 receiver CPU, STREAM / DATAGRAM | 1.155 / 1.131 | **1.164 (5) / 1.108 (4)** | 8.49 → 9.80 / 8.75 → 9.68 s/GiB |
| Unresolved | Memory: S5 STREAM sender, S5 STREAM receiver, S6 STREAM sender, S6 DATAGRAM sender RSS | various | **1.235, 1.457, 1.640, 1.403** | readiness RSS median differences 4.07, 7.98, 9.04, 5.81 MiB (diagnostic heap-site excess 2.1–6.6 MiB per block) |

The A/A frozen-Reno arm crosses one limit, loopback DATAGRAM control p95 (1.615, range 0.48–2.21); every other A/A median lies within 1.00 ± 0.04.

### What the record establishes and what it leaves open

- **S5 goodput on Linux is sender timing, not the model.** In Cruise the bandwidth estimate is 1.007–1.009× the bottleneck, and the model's own shortfall is 0.010–0.019 of capacity. The sender reaches a full-quantum pacing deadline late 25,171–26,360 times per 30 s window (84–88% of quantum releases), about 0.11–0.12 ms each, 2.8–3.1 s per run. Because D08 caps credit at one quantum, that lateness is lost sending: 0.045–0.048 (STREAM) and 0.072–0.074 (DATAGRAM) of capacity, of a 0.078–0.112 delivery deficit. The connection loop uses about 0.12 cores, so this is not a CPU limit. On the Mac the same instrument gives **model**, with 231–262 late events and about 20 ms per run. The record does not separate timer delivery from goroutine scheduling, and it tests no pacing change.
- **The loopback bottleneck is inconclusive.** Both BBR arms' busiest sender serial stage sits at 0.70–0.85 cores and the receiver is unsaturated; only frozen Reno's send queue saturates (0.88). `d0fabc4d` runs 0.871 / 0.926 of `fc4c1bf1`'s sender cycles per packet with 1.255× / 1.070× its goodput. Packets per useful GiB are equal across arms. The stage did not test whether the BBR sender waits on pacing deadlines.
- **The remaining CPU excess lies inside the measured window,** uniform through the run: window ratios 1.110–1.194 across the raised CPU cells, and 1.117 / 1.177 for the S5 receiver cells, whose whole-run flags are 1.108 / 1.164. On S5, r5 already ran 0.899 / 0.912 of `fc4c1bf1`'s sender user instructions per packet; the residual excess over Reno was 6,736 / 5,219 user instructions per packet, unattributed. Congestion and BBR ECN feedback were deferred from #714 and stay in that residual.
- **The receiver's CPU time and cycles disagree.** S5 receiver CPU time per useful GiB is 1.11–1.16× Reno inside the window, while #714 measured receiver window cycles per GiB at 1.001–1.011 of Reno on r5. Equal cycles taking longer would mean a lower effective clock rate or idle-state effects; the record says these are possible but untested, and the two figures come from different runs and revisions.
- **The unresolved memory cells are steady-state and mixed.** Under the unchanged 70% rule, each has two heap-site blocks that disagree or remain unresolved: for example, bookkeeping 86% against delivery 63% for the S5 STREAM sender, and both blocks unresolved for the S6 STREAM sender. Heap-site resolution is about 0.5 MiB per site. The largest bookkeeping site is `recoveryEvidence.sent` (1.16–1.70 MiB); the largest delivery site is the packet-buffer pool (1.5–6.0 MiB).

### Platform facts checked for this decision (source inspection, not measurement)

- **Go's timer wake granularity differs by OS.** Under the pinned `GOTOOLCHAIN=go1.27.0`, the Linux netpoller blocks in `epoll_wait` with a millisecond timeout: any delay under 1 ms becomes 1 ms, and longer delays are truncated to whole milliseconds (`runtime/netpoll_epoll.go`, `netpoll`). The darwin netpoller passes a nanosecond `timespec` to `kevent` (`runtime/netpoll_kqueue.go`). Timers are also run by busy Ps at scheduling points, so this sets a bound on an idle wake, not the observed lateness.
- **The BBR pacing deadline wakes the connection through the connection's single Go timer.** On `d0fabc4d`, `connection.go` folds `pacingDeadline` into the timer deadline and calls `c.timer.Reset(monotime.Until(deadline))`. The same timer carries other deadlines, the loop also wakes for packets and other events, and it processes that work before the send opportunity. So deadline-to-opportunity lateness mixes timer delivery, goroutine scheduling and loop work, and neither the `Timer.C` value nor its receipt time marks when the runtime fired the timer.

These facts make one hypothesis concrete: **the Linux-only goodput flags (S5 and loopback DATAGRAM) come from runtime timer wake precision interacting with D08's one-quantum credit cap.** They show capability, not cause. The competitor is scheduling delay on the connection goroutine after a timely timer fire.

### Mac records reviewed

- **[C1–C4 correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) (#671).** Loopback-competent on the Mac; at matched WAN load, ECN mark-range rebuilding and per-datagram pacing wakeups left sender CPU about 1.75× Reno. Both were corrected (#709, #710). Relevant here: more frequent pacing wakeups carry a measured CPU cost, so a remedy that adds wakeups trades one flag for another.
- **[WAN-corrected re-demonstration](https://github.com/the-sarge/quic-go-fast/blob/dc252f8a/docs/audits/2026-10-03-bbr-wan-redemonstration/README.md) (#711, `fc4c1bf1`).** Loopback and S5 goodput pass on the Mac (0.975 / 0.985; 0.969 / 0.979); S5 sender CPU (1.170 / 1.320), S5 DATAGRAM receiver CPU (1.138), three mixed memory cells and two preservation CPU cells were raised. Its D2 exception covers `fc4c1bf1` on macOS only.
- **New-revision Mac evidence** is four S5 timeline observations, all "model" with negligible timing loss. New-revision Mac readiness is unmeasured.

The Mac record is consistent with the timer-precision hypothesis (goodput passes where wakes are nanosecond-precise) but does not establish it: hardware, scheduler and OS change together.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced** on `d0fabc4d`. Condition 3 fails with nine unresolved candidate cells, and condition 2 fails because no attributed cell is excepted. Advancing would waive limits for costs that are not explained or not remedied.

BBRv3 is **not declined**. The S6 benefit is repeatable on Linux for both `fc4c1bf1` (12.07× / 11.09×) and `d0fabc4d` (11.39× / 10.81×), all twenty pairs above Reno, and historically on macOS for `fc4c1bf1` (12.31× / 14.09×). New-revision macOS S6 and readiness are unmeasured. On Linux, unresolved cells fell from fifteen to nine, loopback STREAM now passes every limit, and no default-Reno preservation cell is raised on `d0fabc4d`. The macOS preservation CPU cells on `fc4c1bf1` remain historical evidence: neither cleared nor re-measured.

Non-advancement is forced by condition 3. Continuing rather than declining or excepting is an operator choice, made because every open cost now has a testable hypothesis or a measurement gap that owned hardware can close.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | None. Four of #714's five interventions were kept by rule; the fifth was inconclusive on instructions and kept by operator decision. |
| Missing evidence | Loopback DATAGRAM goodput and sender CPU; S5 STREAM sender CPU; both S5 receiver CPU cells; the four memory cells. |
| Newly attributed cost, remedy untested | Both S5 goodput cells: sender timing on Linux. An implementation-integration cost on the declared platform; whether it can be removed is missing evidence. |
| Attributed behaviour (not shown to be an algorithm necessity) | S6 STREAM control p95 (selected ProbeBW Up policy); S6 STREAM receiver RSS (delivery-storage retention at 11× Reno's delivered rate). |
| Demonstrated contract incompatibility | None current. #711 found the macOS `fc4c1bf1` S5 sender RSS excess design-bounded, removable only by a change to the ledger and record bounds; that is a recorded design-bound question, not a demonstrated need, and the current mixed memory cells do not establish one. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception extended or granted now

No exception is granted, and the macOS D2 exception is not extended. With nine unresolved cells, no exception could satisfy D1. Recorded for the next decision, without clearance:

- **S6 STREAM control p95:** 1.588 on Linux `d0fabc4d`, the same Up-policy cause (98.5–99.3% of samples above 25 ms in Up or the following Down), slightly larger than the Mac's excepted 1.547.
- **S6 STREAM receiver RSS:** 1.759, delivery data in both blocks (81% / 72%), larger than the Mac's excepted 1.574. Attributed delivery-storage retention; it does not establish an unavoidable algorithm cost.
- **S5 sender RSS (the third D2 cell):** STREAM 1.235 is **unresolved** on `d0fabc4d` (bookkeeping 86% in one block, delivery 63% in the other), a changed composition from the Mac's bookkeeping attribution, so no clearance carries. DATAGRAM passes (1.096).
- **S5 goodput (newly attributed, not a D2 cell):** sender timing on Linux. An implementation-integration cost on the declared platform, not an algorithm trade-off. It is a candidate for remedy, not for exception, while a contract-preserving remedy remains untested.

The macOS D2 exception stays on record for `fc4c1bf1`, unchanged.

### D3 — Ticket C: test whether pacing-wake lateness explains the Linux goodput flags

One task ticket on `minimax`, from `d0fabc4d`, under #712's core layout and host settings, with no host-setting change. Every rule, threshold, synthetic case and inventory below is committed in the ticket's record before any comparative data, with an explicit inconclusive ending for each stage.

**Stage 0 — Instrumentation preflight (no attribution).** The record's overlay measures deadline-to-opportunity lateness only. The connection resets one shared timer for several deadlines, wakes for non-timer reasons, and processes packets and lifecycle work before it sends; `Timer.C` values and receipt times do not mark when the runtime fired the timer. So before any attribution, the ticket must show an instrument that independently observes, per paced stop:
- the deadline requested and the timer generation armed for it, including resets, superseded timers and deadlines folded into an earlier one;
- when the runtime fired that timer;
- when the connection goroutine became runnable and when it began running;
- the wake source (pacing timer, other timer, packet, application or other);
- connection-loop work done before the send opportunity; and
- when the send opportunity began.

The preflight passes only when synthetic delayed-delivery, delayed-scheduling, delayed-handler, reset and non-timer-wake cases are each recovered by the analysis, and when instrumented goodput stays within the record's perturbation check (at least 0.99 of the uninstrumented candidate). The registration names the instrument only after this is demonstrated. If no instrument passes, Stage 1 is not run, and the ticket ends with an **instrumentation gap**.

**Stage 1 — Discriminate the source of lateness (attribution only).**
- *Question.* Why does the Linux sender reach full-quantum pacing deadlines late on S5?
- *Hypothesis.* Timer delivery: the runtime fires the pacing timer late.
- *Competitors.* Goroutine scheduling: the timer fires on time but the connection goroutine runs late. Loop work: the goroutine runs on time but other connection work precedes the opportunity. Mixed causes are allowed for.
- *Runs.* S5, both workloads, four blocks, `d0fabc4d` instrumented. Late events are split by the boundaries above. The stage verdict requires the same leading cause in both workloads by a registered share rule; ambiguous boundaries, missing observations or disagreement end it as inconclusive.
- *Context control.* A bare Go timer loop at the same cadence on the same cores, without the transport, bounded to ten 30 s runs. It shows what the runtime delivers in isolation; it is context, never proof of the loaded transport's cause.
- *Loopback.* The S5 fluid-link decomposition needs a fixed bottleneck and relay queue samples, which loopback lacks, so it is **not** applied there. A separate registered loopback rule compares, for `d0fabc4d` loopback DATAGRAM over four blocks, the share of the window spent in paced waits, lateness, credit waits and loop work with the same instrument. Its outcomes distinguish **timing exposure** (time observed waiting on late wakes) from **timing-limited goodput** (exposure large enough, by the registered threshold, to account for the deficit), with an inconclusive ending. It is validated on synthetic cases without relay samples: paced waits, credit waits, processing delay and mixed causes. At about 4 Gbit/s, one Q per millisecond cannot be the whole explanation, so a timing-exposure finding alone establishes no cause.

**Stage 2 — One contract-preserving intervention, only if Stage 1 attributes S5 lateness to timer delivery.**
- *Change.* A BBR-only wake path with sub-millisecond precision on Linux. D08's deadline computation, one-quantum credit cap, quantum release and 2Q pending bound stay unchanged. Reno's path and other platforms are unchanged. The mechanism is registered after Stage 1 and before comparative data, and it addresses the delay Stage 1 found.
- *Equivalence, in two parts.*
  - **Fixed-input policy equality.** A frozen-policy oracle feeds identical timestamped model, credit and opportunity inputs to `d0fabc4d` and the new revision and requires identical admissions, deadlines, credit and recorded decisions.
  - **Live execution.** Opportunity times change by design, so registration times, admitted bytes, delivery samples, ACK feedback and later decisions may differ live. The change must still keep truthful clocks, D08 deadlines and admission bounds, pending-work accounting, ownership and lifetime, cancellation, ordering and receive fairness.
- *Gates before any comparative data.* The oracle; wake-path lifecycle tests covering cancellation, stale and reset wakes, non-pacing opportunities, rate and quantum decreases and authorized exemptions; bound checks that distinguish admission, pending work and wire bursts under changing rates; and #714's inherited correctness, race and native-coverage gates. Performance never stands in for a correctness gate.
- *Arms.* On S5, both workloads, six blocks: frozen Reno, A/A frozen Reno, `d0fabc4d` instrumented, a second `d0fabc4d` instrumented (the contemporaneous **BBR A/A** control), the new revision instrumented, and Reno on the new revision.
- *Metrics.* Per block, paired against the same block's `d0fabc4d` arm:
  - the delivery deficit (1 − goodput ratio to the same block's frozen Reno), as a difference;
  - pacing lateness seconds per window, as a ratio;
  - pacing-timer wakes per useful GiB, kept separate from send opportunities, as a ratio; and
  - sender CPU per useful GiB, as a ratio.

  The BBR A/A arm gives the noise range of each paired statistic. Zero lateness in the predecessor makes the lateness ratio undefined; that block counts as unusable for that metric.
- *Keep rule.* Kept only when, in **both** workloads, with at least five of six usable blocks:
  - the median deficit difference shows a reduction beyond the BBR A/A range and lateness falls beyond it;
  - the medians of wakes and sender CPU per useful GiB are not above the BBR A/A maximum (the operator's conservative no-increase requirement);
  - receiver integrity holds; and
  - Reno on the new revision passes #714's preservation metric (user-plus-kernel cycles per useful GiB at each endpoint, workloads pooled, median inside the frozen-Reno A/A range).
- *Other outcomes.* **Negative** when either workload regresses beyond the BBR A/A range on any keep metric or preservation fails. **Null** when nothing moves beyond the range. **Inconclusive** with too few usable blocks or a changed wake-source mix that the registration cannot account for. Bottleneck overflow is reported beside every outcome. Loopback is measured only diagnostically and never affects keeping.
- *Synthetic cases before data.* Improvement in both workloads, one-workload regression, insufficient observations, zero lateness, changed wake-source mix and preservation failure.

**Diagnostic-only 2Q arm (never kept).**
- *When it runs.* Only alongside Stage 2, as one more arm in the same blocks, paired against the same `d0fabc4d` arm.
- *What it changes.* The pacing-credit cap rises from Q to 2Q, which breaks D08. The 2Q pending-work bound is unchanged. The arm records queue-limited admissions, wire bursts and bottleneck overflow.
- *What it may conclude.* It reports this treatment's measured response, not a general bound on recoverable deficit. Recovery in this arm alone does not show that a D08 change is necessary.
- *What it can never do.* It never becomes Ticket D's input, and it authorizes nothing. A credit-horizon amendment would need its own design decision, and the next decision may consider one only after a contract-preserving remedy has failed.

**Inventory and endings.**
- *Observation counts.* Stage 1 is 8 observations on S5 and 4 on loopback. Stage 2 with the diagnostic arm is 7 arms × 2 workloads × 6 blocks = 84. Together that is 96, and reruns are capped at 24, so the whole ticket stays within 120 Linux observations. The bare-timer control is bounded separately as above.
- *Endings.* Every ending reports to the next ticket with an identified surviving revision (`d0fabc4d` unless a change is kept) and its unresolved evidence:
  - kept;
  - null;
  - negative;
  - inconclusive;
  - instrumentation gap;
  - scheduling or loop-work cause found, so no intervention is built;
  - a mechanism that cannot be built contract-preservingly (one unbuildable mechanism does not show that every contract-preserving approach is unavailable);
  - failed gates; or
  - exhausted budget.
- *Feed to the next decision.* A scheduling or loop-work finding, together with the loopback rule's outcome, is the evidence for or against a bounded redesign of the connection-loop/send-queue hand-off.

### D4 — Ticket D: the remaining per-packet CPU cost and the receiver CPU measure

One task ticket, blocked by Ticket C, from Ticket C's surviving revision (`d0fabc4d` if nothing was kept).

- **Descriptive profile first.** A `perf` profile of the surviving revision on S5 against frozen Reno, by #712's groups, selects at most three leads. Congestion and BBR ECN feedback, deferred from #714, is eligible. A profile selects leads; it never attributes a cause.
- **Interventions by #714's rules unchanged:** registered hypotheses, a contract and equivalence domain per change, the keep rule, the not-allowed list (no parameter or model change, no change to declared bounds, no send-path batching, no change to Reno's path), stops, and conditional net effects. At most one revision per lead, plus a cumulative check against the starting revision.
- **Receiver CPU-time against cycles.** In the same runs, the receiver's task-clock, cycles and instructions per useful GiB, the effective clock rate per arm, and idle-state and wake counts where the instruments allow. The hypothesis is a lower effective clock rate or idle effects. The competitor is extra receiver work that the cycle count misses. The governor and every host setting stay unchanged. The readiness measure stays CPU time: an attribution here never changes the limit or clears the flag. It only labels the cell for a later exception decision.
- **Inventory, required checks reserved first.** At most 300 Linux observations, reruns included:
  - profile: 2 arms × 2 workloads × 2 blocks = 8;
  - cumulative check of the final revision against the starting revision, in #714's form: 60;
  - reruns: up to 52;
  - leads: 3 × 60 = 180, at #714's 5 arms × 2 workloads × 6 blocks each.

  The reserved checks cannot be displaced by optional leads; if the budget runs short, leads are dropped, never the cumulative check. The receiver measure uses the same runs and adds none.
- **Endings.** Every ending reports to Ticket E with an identified surviving revision and its unresolved evidence:
  - leads kept;
  - all null, negative or stopped;
  - unusable counters;
  - failed gates; or
  - exhausted budget.

  If nothing is kept, the survivor is Ticket C's survivor.

### D5 — Ticket E: Linux re-demonstration, then a fresh decision

**Ticket E**, blocked by Ticket D, reruns #715's readiness stage on the surviving revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout. Its attribution stages are:
- the S5 timeline on S5 under #715's rule, and, only if Ticket C's loopback rule found **timing-limited goodput**, that loopback rule on loopback DATAGRAM (never the S5 decomposition);
- #711's memory rules, unchanged, with four heap-site blocks per raised cell instead of two. A descriptive per-group decomposition (delivery, bookkeeping, other, in MiB) is reported for every raised memory cell. Classification still requires every usable block to meet the unchanged 70% rule. The decomposition never classifies a cell;
- D3's preservation rules, if any preservation cell is raised;
- the D2 cells, re-attributed;
- Ticket D's receiver measure, if it was informative.

Attribution caps and registration follow #715. If C and D keep nothing, Ticket E runs only the registered attribution stages that the new evidence justifies, or closes as not run with the reason; a readiness rerun on unchanged `d0fabc4d` would duplicate #715.

**Terminal routing.** Every path reaches the fresh decision with an identified revision and preserved evidence, and none implies readiness.

| Outcome | Route |
| --- | --- |
| C keeps a change | D starts from C's survivor; E re-demonstrates D's survivor. |
| C ends null, negative, inconclusive, with an instrumentation gap, with a non-timer cause, with an unbuildable mechanism, with failed gates or with an exhausted budget | D starts from `d0fabc4d`; C's findings, including the loopback rule's outcome, go to the decision. |
| D keeps one or more leads | E re-demonstrates D's survivor. |
| D keeps nothing | E re-demonstrates C's survivor if C kept a change; otherwise E runs only justified attribution stages or closes as not run. |
| A correctness or contract failure in C or D | The change is not kept; the failure is reported to the decision. |
| A prerequisite gap or unusable instruments in E | The affected stage ends as a gap, and E closes to the decision. |
| Flags persist | Reported as unresolved to the decision. |

**Then a fresh grilling ticket**, blocked by Ticket E, re-asks this question on whatever C, D and E find, including nothing kept, evidence gaps and prerequisite gaps. Its options are the same as this ticket's: advance; another resolution step; a bounded integration redesign; a separate contract-change decision (for example, a D08 credit-horizon amendment, if Ticket C's diagnostic arm and a failed contract-preserving remedy support one); a declared exception; or declining work on this candidate.

### D6 — No paid experiment and no campaign

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) is unchanged. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D7 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. #673 is re-wired to be blocked by the new decision ticket. The leading open cost is a sender-timing and runtime-precision hypothesis that BBRv1's pacing would share, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the S6 benefit.** Nine cells are unresolved.
- **Decline.** No intervention failed, the open costs have testable hypotheses, and the S6 benefit holds on Linux for both revisions and historically on macOS.
- **Grant exceptions now.** They cannot unlock advancement, and any new revision needs re-attribution.
- **Except the S5 goodput cell as a platform limitation.** A contract-preserving remedy is untested; D08 calls OS wake precision "a measured limitation", which this decision measures rather than accepts.
- **Adopt a longer credit horizon, an earlier deadline or a different quantum now.** Each changes D08; the diagnostic arm informs a later design decision instead.
- **Change a host setting such as timer slack, the governor or isolation.** That is outside the map, and it would not carry to consumers.
- **Redesign the connection-loop/send-queue hand-off now.** The loopback stage was inconclusive; Ticket C tests the cheaper timing explanation first.
- **Another round of interventions before Ticket C.** Pacing changes can move per-useful-GiB CPU and goodput together. Settling timing first keeps Ticket D's measurements interpretable.
- **Run Tickets C and D in parallel.** They would produce two diverging revisions on one shared measurement host.
- **Change the memory rule, or let the per-group decomposition classify a cell.** It is reported, not ruled.
- **Count cycles instead of CPU time as the readiness measure.** That would weaken a gate.
- **Mandate finer heap-profile sampling.** Mixed classifications have not been shown to come from sampling resolution. More blocks test reproducibility; finer sampling stays an optional registered instrument change, with a perturbation check.
- **Insert a redesign checkpoint before Ticket D.** Ticket D addresses independent CPU questions, and the routing already carries Ticket C's evidence to the decision; an earlier checkpoint would be an efficiency option, not a requirement.
- **Require new-revision Mac readiness before Linux-only advancement.** The accepted D1 allows declared-platform advancement with Mac records reviewed.
- **Any paid run.**

## Map changes on acceptance

- A resolution comment on #716, the issue closed, and one Decisions-so-far line on #666.
- New task tickets C, D and E and a new grilling decision ticket, wired C → D → E → decision. #673 is re-wired from #716 to the new decision ticket.
- Fog updated:
  - the Linux S5 timing question becomes Ticket C;
  - the coupled-stage loopback hypothesis stays fog behind Ticket C;
  - a possible D08 credit-horizon amendment is added as fog;
  - the production-slicing, bookkeeping-bound and adoption-evidence fog stays.

## Consideration dispositions

RAS run `20261005T224747-d0a0f4291c4f339e8616722e` (five reviewers, adjudicated) kept the verdict: non-advancement is forced, and continuing without exceptions is a defensible operator choice. It raised nine fixes, and all are accepted. Decisive figures were checked against [summary.json](../2026-10-05-bbr-linux-redemonstration/summary.json) (readiness medians and crossings, all matching) and [attribution.json](../2026-10-05-bbr-linux-redemonstration/attribution.json) (`cpu_split`, `memory.cells`, `d2`). The design quotation was checked against `docs/designs/bbrv3.md` on this branch.

| Fix | Disposition | Where | Verification and remaining dissent |
| --- | --- | --- | --- |
| F1 — High: independent timing observability | Accepted | D3 Stage 0 and Stage 1; platform facts | Added an instrumentation preflight with synthetic delayed-delivery, delayed-scheduling, delayed-handler, reset and non-timer-wake cases and a perturbation check; loop work is a named competitor; the bare-timer loop is context only; Stage 2 runs only on a timer-delivery finding. The `Timer.C`-as-fire-time instrument is not adopted. No timer-slack or idle-state cause is asserted. |
| F2 — High: Stage 2 controls and statistics | Accepted | D3 Stage 2 arms, metrics, keep rule, outcomes | Added a contemporaneous BBR A/A arm; paired differences and ratios against the same block's `d0fabc4d`; zero-lateness handling; five of six usable blocks; both workloads; keep, negative, null and inconclusive outcomes; wakes kept separate from opportunities; #714's Reno preservation metric named; loopback diagnostic only. The operator's no-increase requirement on wakes and CPU is retained. |
| F3 — High: policy equality against live timing | Accepted | D3 Stage 2 equivalence and gates | Fixed-input policy equality via a frozen-policy oracle, separate from permitted live timing and feedback differences; lifecycle and bound tests listed; inherited gates must pass before comparative data. No normative overflow threshold or constant-rate burst formula is invented. |
| F4 — Medium: observation capacity and endings | Accepted | D3 inventory and endings; D4 inventory and endings; D5 terminal routing | Ticket C: 12 + 84 + up to 24 reruns ≤ 120, with the bare-timer control bounded to ten 30 s runs. Ticket D: profile 8 + cumulative 60 + reruns 52 + three leads at 60 = 300, so the lead maximum falls from four to three to keep the cap. Every outcome is routed C → D → E → decision. |
| F5 — Medium: 2Q diagnostic branch and inference | Accepted | D3 diagnostic arm | Runs only alongside Stage 2, paired against the same `d0fabc4d` arm, within the 120 cap. The pending bound stays 2Q. Admissions, bursts and overflow are recorded. It reports a measured response, not a recoverable bound, and recovery alone does not show a D08 change is necessary. It is never kept and never becomes Ticket D's input. |
| F6 — Medium: separate loopback rule | Accepted | D3 Stage 1 loopback; D5 Ticket E | The S5 decomposition is prohibited on loopback. A loopback-specific rule separates timing exposure from timing-limited goodput, with synthetic cases lacking relay samples. Ticket E runs it only after a timing-limited finding. |
| F7 — Medium: revision and platform scope | Accepted | D1; advancement table; Rejected | The S6 benefit is now stated per revision and platform; the absence of preservation flags is scoped to Linux `d0fabc4d`; the macOS `fc4c1bf1` preservation cells are kept as historical evidence. |
| F8 — Medium: D2 recap and classification | Accepted | D1 classification table; D2 | Added the third D2 cell, S5 sender RSS (STREAM unresolved with changed composition, DATAGRAM passing). S5 goodput is labelled as a newly attributed cost. S6 receiver RSS is described as delivery-storage retention, not algorithm necessity. #711's bookkeeping-bound concern is recorded with its scope. Intervention 4's inconclusive result and operator keep stay as recorded. |
| F9 — Low: numbers and quotation | Accepted | Advancement table; flag table; evidence bullets; D08 quotation | Two S5 goodput cells; readiness RSS differences 4.07 / 7.98 / 9.04 / 5.81 MiB, with heap-site excess labelled diagnostic; window ratios 1.110–1.194, and 1.117 / 1.177 for the S5 receiver cells; memory blocks "disagree or remain unresolved". The D08 quotation now matches the amended pacing paragraph verbatim: the first draft quoted the pre-amendment wording from `main`. |

Not acted on, as the synthesis advised: making the verdict a logical necessity, mandating finer heap sampling, mandating an early redesign checkpoint, treating the timer hypothesis as established, an exclusive four-bucket classification, and any new exception, weaker preservation rule, cycles-for-CPU-time substitution, equal-throughput Reno matching, paid run, host-setting change or BBRv1 work.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The Go runtime and connection-timer facts come from source inspection and are not measurements.
