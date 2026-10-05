# BBRv3 qualification decision after the per-packet interventions

Status: proposed decision set for [Decide whether the intervention-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/716), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-05, pending a multi-agent consideration and final approval. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-intervention-qualification-decision`, based on `36f7ce01` (the [Linux re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) record). That record contains the [per-packet intervention record](../2026-10-03-bbr-per-packet-interventions/README.md) and the surviving revision `d0fabc4d` (r6). The earlier decisions and Mac records live on sibling record branches and are linked by commit.

## Question

On the surviving revision's evidence, with both preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration, open a separate contract-change design decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged D1 advancement condition; preservation flags still block. Dispose of any D2 extension, any newly attributed cost and the remaining blockers. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability.

## Normative constraints (quoted)

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": "the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload)"; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers." D2: "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "D1's condition 3 is **unchanged**: a raised preservation flag blocks advancement." D6: options include "a **bounded, evidence-supported redesign of BBR-specific integration** (the failure of five selected interventions does not show that every contract-preserving redesign is unavailable)" and "a separate contract-change design decision, if a needed change is demonstrated"; "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Any necessary contract change is a separate explicit design decision. A throughput improvement does not excuse a correctness regression." Out of scope includes "weakening calibration/correctness gates" and "altering unrelated host/kernel/security settings".

[BBRv3 design](../../designs/bbrv3.md), D08 (pacing): "Pace registration opportunities with a token/debt balance capped at Q, initialized to Q, debited by actual UDP payload bytes admitted and replenished at the effective rate. Deadline uses exact deficit/rate rounded up to the monotonic clock unit … At high rates the 64KiB quantum needs sub-millisecond opportunities, while actual OS wakeup precision remains a measured limitation. … Delayed wakeups cannot accumulate more than Q. … low actual OS wakeup precision may lower throughput but never authorizes a larger unreported burst." The quantum-release amendment adopted under [#710](https://github.com/the-sarge/quic-go-fast/issues/710) is part of the candidate.

## Evidence base

All readiness figures are from the [Linux re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) and its `summary.json`/`attribution.json`, on `d0fabc4d` against frozen Reno `e4f322cb` on `minimax` (AMD Ryzen AI MAX+ 395, Linux 7.0, four isolated physical cores per endpoint): 110 counted readiness observations and 64 Linux plus 4 macOS attribution observations, all exiting cleanly with receiver integrity. Two S5 STREAM blocks were contaminated by a VM and rerun clean under the registered rule; under #712's rule on the original blocks, no flag changes status. Ratios are medians of five per-block ratios against the same block's Reno, with blocks crossing the limit in parentheses.

### Advancement condition, checked on `d0fabc4d` (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 11.39× [9.44–13.22] STREAM, 10.81× [9.73–12.57] DATAGRAM, 5/5 pairs each; sender CPU per useful GiB 0.119 / 0.127× Reno | Yes |
| 2. Every flag passes, or is attributed and excepted | Two cells attributed (S6 STREAM p95, S6 STREAM receiver RSS) and one attributed by a discriminating stage (S5 goodput); none excepted | No |
| 3. No unresolved flag, no preservation flag | Nine unresolved candidate cells; **no** preservation cell | No (unresolved cells) |
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
| Unresolved | Memory: S5 STREAM sender, S5 STREAM receiver, S6 STREAM sender, S6 DATAGRAM sender RSS | various | **1.235, 1.457, 1.640, 1.403** | excess about 2.1–6.6 MiB per cell |

The A/A frozen-Reno arm crosses one limit, loopback DATAGRAM control p95 (1.615, range 0.48–2.21); every other A/A median lies within 1.00 ± 0.04.

### What the record establishes and what it leaves open

- **S5 goodput on Linux is sender timing, not the model.** In Cruise the bandwidth estimate is 1.007–1.009× the bottleneck, and the model's own shortfall is 0.010–0.019 of capacity. The sender reaches a full-quantum pacing deadline late 25,171–26,360 times per 30 s window (84–88% of quantum releases), about 0.11–0.12 ms each, 2.8–3.1 s per run. Because D08 caps credit at one quantum, that lateness is lost sending: 0.045–0.048 (STREAM) and 0.072–0.074 (DATAGRAM) of capacity, of a 0.078–0.112 delivery deficit. The connection loop uses about 0.12 cores, so this is not a CPU limit. On the Mac the same instrument gives **model**, with 231–262 late events and about 20 ms per run. The record does not separate timer delivery from goroutine scheduling, and it tests no pacing change.
- **The loopback bottleneck is inconclusive.** Both BBR arms' busiest sender serial stage sits at 0.70–0.85 cores and the receiver is unsaturated; only frozen Reno's send queue saturates (0.88). `d0fabc4d` runs 0.871 / 0.926 of `fc4c1bf1`'s sender cycles per packet with 1.255× / 1.070× its goodput. Packets per useful GiB are equal across arms. The stage did not test whether the BBR sender waits on pacing deadlines.
- **The remaining CPU excess lies inside the measured window,** uniform through the run (window ratios 1.117–1.194). On S5, r5 already ran 0.899 / 0.912 of `fc4c1bf1`'s sender user instructions per packet; the residual excess over Reno was 6,736 / 5,219 user instructions per packet, unattributed. Congestion and BBR ECN feedback were deferred from #714 and stay in that residual.
- **The receiver's CPU time and cycles disagree.** S5 receiver CPU time per useful GiB is 1.11–1.16× Reno inside the window, while #714 measured receiver window cycles per GiB at 1.001–1.011 of Reno on r5. Equal cycles taking longer would mean a lower effective clock rate or idle-state effects; the record says these are possible but untested, and the two figures come from different runs and revisions.
- **The unresolved memory cells are steady-state and mixed.** Each has two heap-site blocks that disagree under the unchanged 70% rule (for example, bookkeeping 86% against delivery 63%). Heap-site resolution is about 0.5 MiB per site. The largest bookkeeping site is `recoveryEvidence.sent` (1.16–1.70 MiB); the largest delivery site is the packet-buffer pool (1.5–6.0 MiB).

### Platform facts checked for this decision (source inspection, not measurement)

- **Go's timer wake granularity differs by OS.** Under the pinned `GOTOOLCHAIN=go1.27.0`, the Linux netpoller blocks in `epoll_wait` with a millisecond timeout: any delay under 1 ms becomes 1 ms, and longer delays are truncated to whole milliseconds (`runtime/netpoll_epoll.go`, `netpoll`). The darwin netpoller passes a nanosecond `timespec` to `kevent` (`runtime/netpoll_kqueue.go`). Timers are also run by busy Ps at scheduling points, so this sets a bound on an idle wake, not the observed lateness.
- **The BBR pacing deadline wakes the connection through the connection's single Go timer.** On `d0fabc4d`, `connection.go` folds `pacingDeadline` into the timer deadline and calls `c.timer.Reset(monotime.Until(deadline))`.

These facts make one hypothesis concrete: **the Linux-only goodput flags (S5 and loopback DATAGRAM) come from runtime timer wake precision interacting with D08's one-quantum credit cap.** They show capability, not cause. The competitor is scheduling delay on the connection goroutine after a timely timer fire.

### Mac records reviewed

- **[C1–C4 correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) (#671).** Loopback-competent on the Mac; at matched WAN load, ECN mark-range rebuilding and per-datagram pacing wakeups left sender CPU about 1.75× Reno. Both were corrected (#709, #710). Relevant here: more frequent pacing wakeups carry a measured CPU cost, so a remedy that adds wakeups trades one flag for another.
- **[WAN-corrected re-demonstration](https://github.com/the-sarge/quic-go-fast/blob/dc252f8a/docs/audits/2026-10-03-bbr-wan-redemonstration/README.md) (#711, `fc4c1bf1`).** Loopback and S5 goodput pass on the Mac (0.975 / 0.985; 0.969 / 0.979); S5 sender CPU (1.170 / 1.320), S5 DATAGRAM receiver CPU (1.138), three mixed memory cells and two preservation CPU cells were raised. Its D2 exception covers `fc4c1bf1` on macOS only.
- **New-revision Mac evidence** is four S5 timeline observations, all "model" with negligible timing loss. New-revision Mac readiness is unmeasured.

The Mac record is consistent with the timer-precision hypothesis (goodput passes where wakes are nanosecond-precise) but does not establish it: hardware, scheduler and OS change together.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced** on `d0fabc4d`. Condition 3 fails with nine unresolved candidate cells, and condition 2 fails because no attributed cell is excepted. Advancing would waive limits for costs that are not explained or not remedied.

BBRv3 is **not declined**. The S6 benefit is repeatable on both platforms and both revisions (Linux `d0fabc4d` 10.81–11.39×, all ten pairs above Reno). Unresolved cells fell from fifteen to nine, loopback STREAM now passes every limit, and no default-Reno preservation cell is raised.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | None. Four of #714's five interventions were kept by rule; the fifth was inconclusive on instructions and kept by operator decision. |
| Missing evidence | Loopback DATAGRAM goodput and sender CPU; S5 STREAM sender CPU; both S5 receiver CPU cells; the four memory cells. The S5 goodput cell is attributed to sender timing but its remedy is untested, so its readiness is missing evidence too. |
| Algorithm trade-off or design behaviour | S6 STREAM control p95 (selected ProbeBW Up policy); S6 STREAM receiver RSS (delivery data at 11× Reno's rate). |
| Demonstrated contract incompatibility | None. No change so far has required a contract change. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception extended or granted now

No exception is granted, and the macOS D2 exception is not extended. With nine unresolved cells, no exception could satisfy D1. Recorded for the next decision, without clearance:

- **S6 STREAM control p95:** 1.588 on Linux `d0fabc4d`, the same Up-policy cause (98.5–99.3% of samples above 25 ms in Up or the following Down), slightly larger than the Mac's excepted 1.547.
- **S6 STREAM receiver RSS:** 1.759, delivery data in both blocks, larger than the Mac's excepted 1.574.
- **S5 goodput:** attributed to sender timing. This is an implementation-integration cost on the declared platform, not an algorithm trade-off. It is a candidate for remedy, not for exception, while a contract-preserving remedy remains untested.

The macOS D2 exception stays on record for `fc4c1bf1`, unchanged.

### D3 — Ticket C: test whether pacing-wake lateness explains the Linux goodput flags

One task ticket on `minimax`, from `d0fabc4d`, under #712's core layout and host settings, with no host-setting change.

**Stage 1 — Discriminate the source of lateness (attribution only).**
- *Question.* Why does the Linux sender reach full-quantum pacing deadlines late?
- *Hypothesis.* Runtime timer wake precision: the timer fires late.
- *Competitor.* Connection-goroutine scheduling: the timer fires on time, but the connection loop runs late.
- *Observations.* Per paced stop: the requested deadline, the timer fire time and the time the connection loop starts the send opportunity, so that lateness splits into timer delivery and goroutine scheduling. The ticket also runs a bare Go timer loop at the same cadence on the same cores, without the transport, as a control for what the runtime itself delivers.
- *Paths.* S5, both workloads, plus loopback DATAGRAM with the same timeline overlay, which tests whether loopback's deficit is also timing.
- *Registration.* Rule, thresholds, synthetic cases, maximum observations and an explicit inconclusive ending, all committed before any data.

**Stage 2 — One contract-preserving intervention (only if Stage 1 attributes the lateness to timer delivery).**
- *Change.* A BBR-only wake path with sub-millisecond precision on Linux. D08's deadline computation, one-quantum credit cap and quantum release stay unchanged. Reno's path and other platforms are unchanged. The mechanism is registered after Stage 1 and before any comparative data.
- *Contracts.* The change names the contracts it can affect: ownership and lifetime, cancellation, ordering, pacing and congestion limits, credit and queue bounds, and supported-platform behaviour. Its equivalence domain against `d0fabc4d` is the same congestion inputs, decisions and recorded events on the same traces; only emission timing may differ. #714's gates, race checks and native-coverage rules apply.
- *Keep rule.* The change is kept only when:
  - S5 lateness and the S5 delivery deficit fall beyond the A/A range;
  - pacing wakeups per useful GiB and sender CPU per useful GiB do not rise beyond the A/A range;
  - receiver integrity holds; and
  - Reno on the new revision stays inside frozen Reno's A/A range.
  
  Bottleneck overflow is reported beside the result.

**Diagnostic-only arm (never kept).** A build that raises the credit cap from Q to 2Q. It breaks D08, so it can never be adopted from this ticket. It bounds how much of the S5 deficit lost credit explains, and it measures the added burst and overflow cost. Any credit-horizon change would be a separate D08 design decision, informed by this arm.

**If Stage 1 finds goroutine scheduling, or is inconclusive,** no intervention is built. The result goes to the next decision, together with whether loopback is timing-limited, as evidence for or against a bounded redesign of the connection-loop/send-queue hand-off.

**Bounds.** At most one kept revision. At most 120 Linux observations in total, reruns included, plus the bare-timer control. Owned hardware only, frozen one-ticket aids.

### D4 — Ticket D: the remaining per-packet CPU cost and the receiver CPU measure

One task ticket, blocked by Ticket C, from Ticket C's surviving revision (`d0fabc4d` if nothing was kept).

- **Descriptive profile first.** A `perf` profile of the surviving revision on S5 against frozen Reno, by #712's groups, selects at most four leads. Congestion and BBR ECN feedback, deferred from #714, is eligible. A profile selects leads; it never attributes a cause.
- **Interventions by #714's rules unchanged:** registered hypotheses, a contract and equivalence domain per change, the keep rule, the not-allowed list (no parameter or model change, no change to declared bounds, no send-path batching, no change to Reno's path), stops, and conditional net effects. At most one revision per lead, plus a cumulative check against the starting revision.
- **Receiver CPU-time against cycles.** In the same runs, the receiver's task-clock, cycles and instructions per useful GiB, the effective clock rate per arm, and idle-state and wake counts where the instruments allow. The hypothesis is a lower effective clock rate or idle effects. The competitor is extra receiver work that the cycle count misses. The governor and every host setting stay unchanged. The readiness measure stays CPU time: an attribution here never changes the limit or clears the flag. It only labels the cell for a later exception decision.
- **Bounds.** At most 300 Linux observations, reruns included.

### D5 — Ticket E: Linux re-demonstration, then a fresh decision

**Ticket E**, blocked by Ticket D, reruns #715's readiness stage on the surviving revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout. Its attribution stages are:
- the S5 timeline on S5, plus loopback DATAGRAM if Ticket C showed it is timing-limited;
- #711's memory rules, unchanged, with four heap-site blocks per raised cell instead of two. A descriptive per-group decomposition (delivery, bookkeeping, other, in MiB) is reported for every raised memory cell. Classification still requires every usable block to meet the unchanged 70% rule. The decomposition never classifies a cell;
- D3's preservation rules, if any preservation cell is raised;
- the D2 cells, re-attributed;
- Ticket D's receiver measure, if it was informative.

Attribution caps and registration follow #715. If C and D keep nothing, Ticket E runs only the registered attribution stages that the new evidence justifies, or closes as not run with the reason; a readiness rerun on unchanged `d0fabc4d` would duplicate #715.

**Then a fresh grilling ticket**, blocked by Ticket E, re-asks this question on whatever C, D and E find, including nothing kept, evidence gaps and prerequisite gaps. Its options are the same as this ticket's: advance; another resolution step; a bounded integration redesign; a separate contract-change decision (for example, a D08 credit-horizon amendment, if Ticket C's diagnostic arm and a failed contract-preserving remedy support one); a declared exception; or declining work on this candidate.

### D6 — No paid experiment and no campaign

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) is unchanged. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D7 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. #673 is re-wired to be blocked by the new decision ticket. The leading open cost is a sender-timing and runtime-precision hypothesis that BBRv1's pacing would share, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the S6 benefit.** Nine cells are unresolved.
- **Decline.** No intervention failed, the open costs have testable hypotheses, and the benefit holds on both platforms.
- **Grant exceptions now.** They cannot unlock advancement, and any new revision needs re-attribution.
- **Except the S5 goodput cell as a platform limitation.** A contract-preserving remedy is untested; D08 calls OS wake precision "a measured limitation", which this decision measures rather than accepts.
- **Adopt a longer credit horizon, an earlier deadline or a different quantum now.** Each changes D08; the diagnostic arm informs a later design decision instead.
- **Change a host setting such as timer slack, the governor or isolation.** That is outside the map, and it would not carry to consumers.
- **Redesign the connection-loop/send-queue hand-off now.** The loopback stage was inconclusive; Ticket C tests the cheaper timing explanation first.
- **Another round of interventions before Ticket C.** Pacing changes can move per-useful-GiB CPU and goodput together. Settling timing first keeps Ticket D's measurements interpretable.
- **Run Tickets C and D in parallel.** They would produce two diverging revisions on one shared measurement host.
- **Change the memory rule, or let the per-group decomposition classify a cell.** It is reported, not ruled.
- **Count cycles instead of CPU time as the readiness measure.** That would weaken a gate.
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

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The Go runtime and connection-timer facts come from source inspection and are not measurements.
