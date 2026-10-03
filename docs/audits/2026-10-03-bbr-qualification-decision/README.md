# BBRv3 qualification decision after the WAN-corrected re-demonstration

Status: proposed decision set for [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Revised after a multi-agent consideration (RAS run `20261003T070954-c7b81c3ac6a0d7ebc5741518`); the [disposition table](#consideration-dispositions) records every finding. Not yet accepted by the operator. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-qualification-decision`, based on `dc252f8a` (the [WAN-corrected re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) record, which contains the adopted C1–C4 + D1 + D2 candidate `fc4c1bf1` and its predecessors' records).

## Question

The ticket asks whether the causal explanation, the implementation-fidelity assessment and the local correction justify one of three outcomes: advancing BBRv3, continuing to resolve its implementation, or declining further BBRv3 qualification. If advancing, it asks for the smallest additional evidence that would change an adoption decision, with any paid experiment's concrete cost shown before authorization. If declining or unresolved, it asks whether the reason concerns our implementation, algorithm trade-offs or missing evidence.

## Normative constraints (quoted)

Acceptance plan ([2026-09-23-bbrv3-acceptance-plan.md](../2026-09-23-bbrv3-acceptance-plan.md)): "These are review flags, not automatic rejection rules. Show absolute values and run variability alongside ratios." And: "The owner judges adoption after reviewing the data; neither a minimum improvement nor a statistical-significance threshold is an automatic performance gate." And, for Linux: "Each endpoint receives four CPU cores, with identical allocations for Reno and BBRv3. Reserve emulator and background-traffic resources outside those allocations."

Map #666 Notes: "The operator accepted the existing regression limits as local candidate-readiness criteria … with a repeatable useful benefit on at least one relevant difficult path before further qualification. … These criteria apply to a competent candidate; failure triggers diagnosis and does not by itself reject BBRv3." And: "Separate implementation readiness from algorithm suitability." And: "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." And: "Any necessary contract change is a separate explicit design decision."

Map #666 Notes on cost: "Any proposed paid qualification must present its decision value, required evidence and concrete cost before a new operator decision." The Not-yet-specified section adds: "If the qualification decision requires the S5 sender CPU excess attributed first, a discriminating comparison needs counters or a profiler that separate user from kernel and wake work, which the macOS Go profiler did not; the host for that is a decision, not assumed." Out of scope includes "altering unrelated host/kernel/security settings, or building a maintained benchmark framework".

The [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md) (D5): "every other class stays a raised flag carried to the qualification decision." (D6): the S6 latency and RSS flags "are raised with attribution and go to the qualification decision."

## Evidence base

All figures are from the [re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) and its `summary.json`/`attribution.json`, on candidate `fc4c1bf1`: one Apple M4 Max, loopback endpoints, the frozen userspace relay. 176 retained observations — 80 readiness, 92 attribution and 4 excluded smoke — all clean and all passing receiver integrity. Ratios are medians of per-block ratios against the same block's frozen Reno, with [min–max] and the blocks crossing a limit out of five; they are not quotients of arm medians. Absolute values are arm medians.

| Status | Measure (STREAM / DATAGRAM unless one workload named) | Limit | Paired ratio | Absolute, Reno → candidate |
| --- | --- | --- | --- | --- |
| Useful benefit | S6 goodput | none (benefit) | 12.31× [12.10–14.94] / 14.09× [12.31–16.16], 5/5 pairs each | 6.1 → 85.6 / 5.9 → 85.9 Mbit/s |
| Passed | S6 sender CPU per useful GiB | ≤ 1.10 | 0.317 / 0.320 | 88.9 → 28.4 / 117.4 → 38.2 s/GiB |
| Passed (median) | S5 goodput | ≥ 0.95 | 0.969 [0.936–0.973] (1) / 0.979 [0.929–0.986] (1) | 95.9 → 92.9 / 94.4 → 92.4 Mbit/s |
| Passed (median) | Loopback goodput | ≥ 0.95 | 0.975 / 0.985 [0.714–1.019] (2) | 1316 → 1289 / 1180 → 1126 Mbit/s |
| Passed | S5 control p95 | ≤ 1.20 | 1.074 (1) / 1.003 | p95 183.3 → 200.2 / 181.4 → 183.4 ms; p50 173.4 → 102.2 / 171.2 → 100.7 ms |
| Raised with attribution | S6 control p95 — selected draft-06 ProbeBW Up policy | ≤ 1.20 | 1.547 [1.497–1.678] (5) / 1.481 [1.418–1.626] (5) | 122.7 → 200.2 / 125.3 → 185.6 ms |
| Raised with attribution | S6 STREAM receiver RSS — loss-driven reassembly | ≤ 1.10 | 1.574 (5) | 20.1 → 31.6 MiB |
| Raised with attribution | S5 sender RSS — design-bounded bookkeeping | ≤ 1.10 | 1.203 (4) / 1.132 (4) | 23.6 → 28.3 / 23.8 → 26.9 MiB |
| Raised, unresolved | S5 sender CPU per useful GiB | ≤ 1.10 | 1.170 [1.012–1.466] (3) / 1.320 [0.906–1.566] (3) | 27.3 → 28.8 / 27.8 → 36.7 s/GiB |
| Raised, unresolved | S5 DATAGRAM receiver CPU per useful GiB | ≤ 1.10 | 1.138 [0.765–1.532] (3) | 38.1 → 43.4 s/GiB |
| Raised, unresolved | Mixed memory: S5 STREAM receiver, S6 STREAM sender, S6 DATAGRAM sender RSS | ≤ 1.10 | 1.407 (5) / 1.393 (5) / 1.374 (5) | 23.3 → 32.8 / 19.4 → 27.8 / 19.7 → 27.5 MiB |
| Raised (preservation) | Reno on candidate vs frozen Reno, S5 DATAGRAM sender / receiver CPU | ≤ 1.10 | 1.192 [0.901–1.304] (3) / 1.158 [0.924–1.453] (3) | instructions per GiB 0.994 / 0.990 of frozen Reno |

What the record measures, and what it leaves unknown:

- **The extra sender work is measured; its cause is not.** In the `counters` stage the candidate sender executed 1.132× (STREAM) and 1.139× (DATAGRAM) Reno's instructions per useful GiB, above Reno in all six blocks, while A/A Reno stayed at 1.002/1.007. Per forward packet that is 1.09–1.17× Reno, with the same packet count. Counter-stage CPU time was 1.093 (STREAM, inside the A/A range 0.939–1.103) and 1.203 (DATAGRAM, above the A/A range 0.988–1.088). Instructions and CPU seconds are different measures, and host noise does not explain the instruction excess.
- **The macOS instruments cannot locate it.** macOS instruction counts include kernel send and wake work, and in both arms 97–100% of CPU profile samples land in four syscall and scheduler leaf frames. So the record cannot say whether the excess is BBR computation, allocation, runtime spinning, kernel send or kernel wake work.
- **The allocation sites are a lead with an unvalidated size estimate.** Delivery-record map churn in `captureCongestionSend` and `&sendReservation{}` in `localSendCredit.reserve` add about 245 B per packet. The record's post-hoc sizing, which assumes a few instructions per allocated byte, puts them near a tenth of the instruction excess. That is an estimate, not a demonstrated bound or a discriminating comparison.
- **The host is noisy.** Frozen Reno's own S5 sender CPU spans 26.4–38.4 s/GiB (DATAGRAM) across blocks, non-fixture host load ranged from about 300% to 1,300% CPU (including Lightroom), and relay stalls (wakeups over 5 ms late) reached 87 per run in both arms. That spread is relevant to the CPU-time flags and the preservation flag, but it does not attribute any of them.
- **Loss.** S5 STREAM overflow is 0.068% for Reno against 0.347% [0.279–0.922] for the candidate, consistent with quantum release (#710); S5 DATAGRAM is 0.328% against 0.033%. On S6 STREAM the candidate's forward loss is 0.266% overflow plus 0.102% injected, so most of it is queue overflow from the candidate's own sending. That is recorded, not attributed to a defect or to coexistence harm.
- **Engagement gaps.** The CE response is unengaged (S5 and S6 never mark). C4 items 1 and 3 were not re-examined. The low-rate pacing floor (Q = 2M) is unmeasured.

Linux host facts, checked 2026-10-03 for this decision: `minimax` (the repository's Linux measurement host: AMD Ryzen AI MAX+ 395, 16 physical cores with SMT siblings at CPU n+16, kernel 7.0.0-30-generic, Go 1.27.1 installed, idle load average 0.00) has `perf` 7.0.14 and `tc` (iproute2 6.19.0). `kernel.perf_event_paranoid` is 4, which blocks unprivileged CPU events; non-interactive `sudo` is available. It is owned hardware with no cloud charge. These facts show capability only; nothing has been measured there for this effort.

Two portability facts, checked by code inspection: the frozen relay reads received ECN only from an `IP_RECVTOS` control message (`relay/main.go`, `readECN`), but Linux delivers it as `IP_TOS` (`sys_conn_helper_linux.go`, against `IP_RECVTOS` in `sys_conn_helper_darwin.go`). Unadapted on Linux, the relay would forward every packet as Not-ECT and silently disable ECN for both arms. And the copied build aid pins `GOTOOLCHAIN=go1.27.0`.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not declined**. Where the algorithm is meant to help, it does, repeatably: a paired median of 12.31× (STREAM) and 14.09× (DATAGRAM) Reno's receiver-verified goodput on injected-loss S6, at least 12.1× in every one of the ten pairs, at about a third of Reno's sender CPU per useful GiB. On clean S5 its median goodput is within 3.1% of Reno and its median control latency is about 41% lower. The record gives no sufficient basis to decline further investigation of the algorithm.

BBRv3 is **not advanced** to further qualification. Five flags are raised and unresolved, and one default-Reno preservation flag is raised: S5 sender CPU (1.170 / 1.320), S5 DATAGRAM receiver CPU (1.138), the three mixed memory flags (1.407 / 1.393 / 1.374) and the preservation CPU flag (1.192 / 1.158). Advancing would waive limits for costs that are not yet explained.

**Reason it remains unresolved:**
- **Missing causal attribution, and so unresolved implementation readiness.** The extra sender instructions are measured; what they are, and whether they are avoidable, is not. The Mac instruments cannot separate BBR computation from allocation, runtime spinning, kernel send or kernel wake work.
- **A possible implementation cost.** The instruction excess may be avoidable per-packet work; the two allocation sites are a lead whose share is only estimated.
- **Not the algorithm.** The three attributed flags (D2) are algorithm behaviour or declared design bounds, not evidence of unsuitability.

Going to Linux first is a **sequencing** decision: it is the cheapest owned surface with instruments that can separate user from kernel work. It does not predict that the Linux measurements will pass or fail.

**Advancement condition for the next decision.** The D5 decision may consider advancing only when, for one identified candidate revision on the declared platform(s):

1. the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload);
2. every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell (D2 sets the form);
3. no flag is raised and unresolved, and no preservation flag is raised; and
4. the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers.

Meeting this condition lets D5 consider advancing; it is not a qualification. If it is not met — including when a diagnostic is inconclusive, or a new cost is attributed but not yet accepted — D5 still runs and decides among another resolution step, a declared exception, or declining, with the reason classified.

### D2 — A scoped operator exception for three attributed flags

Attribution explains a flag; it does not clear it. The criteria stay unchanged. Separately, the operator grants an explicit, bounded **exception**: these three flags do not count against advancement, **only** for these cells, on candidate `fc4c1bf1`, on the macOS record above, at no more than these magnitudes. Each stays **raised**, with its attribution and absolute values, in every report.

| Cell | Ratio (limit) | Absolute cost | Attribution | Basis |
| --- | --- | --- | --- | --- |
| S6 control p95, STREAM / DATAGRAM | 1.547 / 1.481 (1.20) | +77.5 / +60.3 ms at p95 | Selected draft-06 ProbeBW Up policy: 98–100% of forward-queue samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay 0.45–0.50 ms; no p95 excess on matched-load S5 | Algorithm trade-off. Reno has no comparable queue there because it uses about 6% of the link. No tuning of Up. |
| S6 STREAM receiver RSS | 1.574 (1.10) | +11.5 MiB | Loss-driven reassembly: frames held behind 0.37% forward loss while delivering 14× Reno's data | Algorithm behaviour at the delivered rate. |
| S5 sender RSS, STREAM / DATAGRAM | 1.203 / 1.132 (1.10) | +4.7 / +3.1 MiB per endpoint | Design-bounded bookkeeping: the 32,768-entry send-outcome ledger (1.16 MiB) and other declared bounds fill within seconds at WAN rates; the design calls them "chosen memory/accuracy trade-offs" | Design bound. Not reopened now; whether a few MiB per connection matters depends on the consumer's connection count, a production-design question kept in the map's fog. |

The exception does **not** carry over automatically. On Linux, or on any new revision, each corresponding cell is re-attributed under its registered rule, and its cost is shown to the operator. D5 then decides whether to extend the exception to that platform and revision. A larger magnitude, a different cause, or a changed composition (for example, a different heap-site mix) gets no clearance from a cause label alone. Newly attributed costs, such as a CPU cost localized to pacing wakeups, are not covered by this exception and need their own disposition.

### D3 — Next: a Linux diagnostic on owned hardware

One task ticket measures and attributes the same candidate `fc4c1bf1` on `minimax`. It is a diagnostic, and its readiness results are Linux results; it does not re-run or reinterpret the preserved Mac observations.

**What stays fixed and what changes.**
- *Fixed:* candidate and frozen-Reno revisions; fixture and workloads; the relay's frozen queue model; paths, durations, seeds, flag limits and the registered memory and preservation rules; the Go toolchain pin `GOTOOLCHAIN=go1.27.0`.
- *Changes, all recorded:* OS, architecture and native packet-I/O path (actual offload and GSO/GRO engagement measured, not assumed); scheduler and affinity; instruments (`perf` instead of `/usr/bin/time -l` and the macOS Go profiler); and the arm inventory below.
- A Linux pass does not show that the Mac excess was noise, and a Linux attribution does not transfer to macOS without its own evidence.

**Prerequisites, each recorded with its result, before any comparative observation.** A failed prerequisite stops collection and is reported as a prerequisite gap.
- *ECN portability.* A documented Linux adapter in newly copied one-ticket aids, so the relay reads `IP_TOS` as well as `IP_RECVTOS`. The queue model stays unchanged, and the adapter diff is retained. Calibrate forwarding of Not-ECT, ECT(0), ECT(1) and CE in both directions. Show that QUIC ECN validation succeeds and the candidate's BBR ECN tracker is active on loopback, S5 and S6, as it was on the Mac. The relay is required to preserve its declared behaviour, not to be byte-identical.
- *Resources.* Each endpoint gets four physical cores, with their SMT siblings kept off every arm's affinity set, and the relay gets separate physical cores. Allocations are identical for every arm. Core-to-CPU mapping is verified with `lscpu`; actual process and thread affinity is recorded; host contention is sampled during each run, not only before and after. A contamination rule is registered before collection, and affected observations are retained.
- *Privilege.* Endpoints and the relay run as the ordinary user. `sudo perf` attaches to their process IDs for the measured window only, so no endpoint takes a root-only path such as forced socket buffers. Effective socket buffer sizes and endpoint credentials are recorded. `perf_event_paranoid` stays at 4.
- *Instruments.* A smoke check that user and kernel instruction and cycle counters and call-graph sampling work for these processes. If they don't, the attribution stages are registered as inconclusive rather than substituted.

**Readiness stage (plain builds, no profiler attached).** Five paired blocks per path and workload, controller order rotated per block. Arms:
- *loopback and S5:* frozen Reno, candidate, Reno on candidate, and an A/A frozen-Reno arm;
- *S6:* frozen Reno, candidate and an A/A frozen-Reno arm.

Flags use the unchanged readiness rule: the median of five per-block ratios against the limit. A/A membership is reported beside every ratio but never overrides that rule; the registered preservation rule keeps its own A/A clause.

**Attribution stages (attribution only, never flag values).** These stages are registered in the ticket's record, with analysis code committed and exercised on synthetic cases, before any stage observation exists.
1. *Localization.* `perf stat` user and kernel instructions and cycles plus context switches, and `perf record` call graphs, split the candidate's excess per useful GiB into disjoint groups:
   - BBR and bookkeeping code;
   - allocation and GC;
   - user-space runtime scheduling and spinning;
   - kernel send;
   - kernel wake and scheduling.

   The record fixes the primary metric, the normalization, how instructions and cycles interact when they disagree, how negative or missing deltas are handled, block aggregation and repeatability, and an explicit inconclusive outcome. Localization says where work executes. It does not by itself classify a cause.
2. *Discrimination.* For a leading group, name the hypothesis and a competing explanation, and run a bounded comparison or intervention that separates them. For example: a diagnostic-only revision without the suspected allocation, or a pacing-wakeup counter set against Reno at the same packet rate. A hot function, a kernel majority or a declared bound alone never satisfies this step.
3. *Disposition.* Only a demonstrated, contract-preserving implementation defect is fixed in this ticket: on one new identified revision, with the attribution comparison and every readiness stage rerun on it. A fix is confirmed only when the targeted excess falls while receiver integrity, the existing gate tests and the preservation checks hold. The two known allocation sites are eligible only through this route. A pacing or platform cost, a design bound, a contract-change cause or an unresolved cause is reported to D5, not fixed or pre-accepted.

The mixed memory flags and the preservation flag are re-measured and re-classified under their registered rules, the 70% memory rule unchanged. D2's three cells are re-attributed on Linux for D5.

**Size and stopping.** The readiness stage is 110 observations: 40 loopback at 25 s and 70 WAN at 40 s, about 63 minutes of fixture time before setup, prerequisites and attribution stages. It runs on owned hardware and is not charged to the Q2/cloud ledger. The ticket allows at most one fix revision. A second candidate defect, a needed contract change, or a prerequisite that cannot be met stops the ticket, which reports to D5. It cannot grow into a campaign, a platform cross-product or a maintained framework.

### D4 — No paid experiment and no campaign

No paid qualification is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. A paid campaign would measure a candidate that has not yet met D1's advancement condition; the open question is answerable on owned hardware. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) and its evidence stay unchanged.

### D5 — A fresh qualification decision follows the Linux ticket, whatever it finds

A new grilling ticket, blocked by the D3 ticket, re-asks this ticket's question on the combined Mac and Linux evidence, whether D3 ends in passes, failures, new attributions, inconclusive diagnostics or a prerequisite gap. It disposes any extension of the D2 exception, any newly attributed cost, and the remaining blockers. If it advances BBRv3, it chooses the smallest qualification and presents any paid cost first. The evidence most likely to change an adoption decision is recorded in the map's fog rather than ticketed now:

- **Coexistence** (acceptance rows L6/L7): whether BBRv3 displaces competing Reno or CUBIC flows on a shared bottleneck — a known BBR trade-off that a consumer sharing links would weigh heavily.
- **CE response** (S8): no measured path has engaged it.
- **Low-rate pacing** (L4/L5 maritime rows): the two-packet floor regime is unmeasured.
- **C4 items 1 and 3:** need a path that triggers all-spurious undo or rejected-rate rounds.

### D6 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. The first is re-wired to be blocked by the D5 decision ticket instead of this one. They matter only if BBRv3 is eventually not advanced; the record's open costs are unattributed or attributed to declared design choices, not to a BBRv3 algorithm defect that BBRv1 would demonstrably avoid.

## Rejected

- **Decline BBRv3.** The open costs lack causal attribution, and the attributed ones are algorithm behaviour or declared design bounds; that is no basis to decline.
- **Advance now on the strength of the S6 benefit.** It would waive the CPU, memory and preservation limits for unexplained costs.
- **Clearance by attribution alone,** or a new numeric ceiling invented to clear the candidate. D2 is an explicit, bounded exception instead.
- **More attribution on the Mac.** Its instruments cannot separate the hypotheses.
- **Fixing the two allocation sites before measuring on Linux.** That would change two variables at once and assumes a cause the evidence has not shown. They stay eligible under D3's disposition route and remain production-slicing leads either way.
- **Running endpoints as root, or lowering `perf_event_paranoid`.** The first adds root-only paths, the second changes a host security setting outside this map; attaching `sudo perf` to unprivileged processes needs neither.
- **Using `netem` for the diagnostic.** It changes the emulator as well as the host; kernel emulation belongs to a qualification topology, if one is chosen.
- **Shrinking the bookkeeping bounds or tuning ProbeBW Up now.** Both are contract or design changes without a consumer requirement behind them.
- **Any paid run.**

## Map changes on acceptance

- Resolution comment on #672 (including the scope of the D2 exception), issue closed, and one Decisions-so-far line on #666.
- New task ticket for D3, then the D5 grilling ticket blocked by it. #673 is re-wired from #672 to the D5 ticket.
- Fog updated: the Linux-host question is removed (now ticketed); D2's bookkeeping-bound question and D5's adoption-evidence sketch are added.

## Consideration dispositions

RAS run `20261003T070954-c7b81c3ac6a0d7ebc5741518` (five reviewers, adjudicated) supported the continue verdict and raised ten fixes. All are accepted; adjudication dissent is preserved below where it shaped the revision.

| Fix | Disposition | Where | Notes |
| --- | --- | --- | --- |
| 1 — Scoped D2 exception | Accepted | D1 condition 2; D2 | D2 is now an explicit operator exception scoped to revision, platform, cells and magnitudes, with no automatic transfer to Linux or a new revision. Following the adjudication dissent, it is an exception within the operator's authority under the acceptance plan, not an amendment to the criteria. |
| 2 — Linux ECN portability | Accepted | Evidence base; D3 prerequisites | Verified: `readECN` matches only `IP_RECVTOS`; Linux uses `IP_TOS`. Adapter in copied aids plus bidirectional codepoint calibration and endpoint ECN engagement before collection. |
| 3 — Causal classification | Accepted | D3 attribution steps 1–3 | Execution location is now a localization stage; a cause needs a named competitor and a discriminating comparison. Kernel majority no longer pre-classifies, and fixes require a demonstrated, contract-preserving defect. |
| 4 — Deterministic rules | Accepted in scope | D3 attribution preamble and step 1 | The decision fixes what the ticket's registration must specify (metric, normalization, disagreement handling, negative or missing deltas, aggregation, inconclusive outcome, synthetic tests); the classifier itself is registered in that ticket before data. Readiness, attribution and operator exception are separate outputs, and A/A never overrides the readiness median. The dissenting 1.03 instruction gate and A/A host-rejection thresholds are not adopted. |
| 5 — Resources and instrumentation | Accepted | D3 resources and privilege | Verified: CPUs 12, 13, 28 and 29 are two physical cores plus siblings. Now four physical cores per endpoint, siblings excluded, separate relay cores, in-run contention sampling, unprivileged endpoints with `perf` attached by PID, and socket buffers and credentials recorded. |
| 6 — Platform scope | Accepted | D3 fixed/changes; D1 condition 4 | Toolchain pin `go1.27.0` kept (verified in `build.py`); changed variables are listed. A Linux pass does not reinterpret the Mac flags, and advancement names its platforms. |
| 7 — Advancement completeness | Accepted | D1 conditions 1–4 and fallthrough; D5 | S6 benefit is required on the same revision and platform. All unresolved and preservation flags are named. D5 runs on every outcome, and new attributed costs need their own disposition. The dissent calling competence unreachable is noted, not adopted. |
| 8 — Evidence interpretation | Accepted | Evidence base bullets; D1 | "Missing evidence" is now missing causal attribution; instructions and CPU seconds are distinguished; the allocation tenth is an unvalidated estimate; "no measurement shows unsuitable" is now "no sufficient basis to decline"; Linux-first is stated as sequencing. |
| 9 — Numbers and variability | Accepted | Evidence table; D1; D2 table | Added p95 and p50 absolutes ("about 41% lower", not halved); paired medians with ranges and crossing counts; the 176 = 80 + 92 + 4 breakdown; per-GiB 1.132/1.139 against per-packet 1.09–1.17; goodput minima and the 0.714 loopback block; overflow and relay stall figures. |
| 10 — Diagnostic size and stopping | Accepted | D3 size and stopping | 110 readiness observations, about 63 minutes of fixture time; at most one fix revision; explicit stop-and-report conditions; kept off the Q2 ledger. |

## Limits

This is a planning decision. It does not claim readiness, establish any platform's qualification, or authorize a production merge, paid resource, campaign resumption, default-controller change or ledger change. The Linux host and code facts above are capability and portability checks, not measurements.
