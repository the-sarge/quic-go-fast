# BBRv3 qualification decision after the WAN-corrected re-demonstration

Status: proposed decision set for [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Not yet accepted by the operator. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-qualification-decision`, based on `dc252f8a` (the [WAN-corrected re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) record, which contains the adopted C1–C4 + D1 + D2 candidate `fc4c1bf1` and its predecessors' records).

## Question

The ticket asks whether the causal explanation, the implementation-fidelity assessment and the local correction justify one of three outcomes: advancing BBRv3, continuing to resolve its implementation, or declining further BBRv3 qualification. If advancing, it asks for the smallest additional evidence that would change an adoption decision, with any paid experiment's concrete cost shown before authorization. If declining or unresolved, it asks whether the reason concerns our implementation, algorithm trade-offs or missing evidence.

## Normative constraints (quoted)

Acceptance plan ([2026-09-23-bbrv3-acceptance-plan.md](../2026-09-23-bbrv3-acceptance-plan.md)): "These are review flags, not automatic rejection rules. Show absolute values and run variability alongside ratios." And: "The owner judges adoption after reviewing the data; neither a minimum improvement nor a statistical-significance threshold is an automatic performance gate."

Map #666 Notes: "The operator accepted the existing regression limits as local candidate-readiness criteria … with a repeatable useful benefit on at least one relevant difficult path before further qualification. … These criteria apply to a competent candidate; failure triggers diagnosis and does not by itself reject BBRv3." And: "Separate implementation readiness from algorithm suitability." And: "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." And: "Any necessary contract change is a separate explicit design decision."

Map #666 Notes on cost: "Any proposed paid qualification must present its decision value, required evidence and concrete cost before a new operator decision." The Not-yet-specified section adds: "If the qualification decision requires the S5 sender CPU excess attributed first, a discriminating comparison needs counters or a profiler that separate user from kernel and wake work, which the macOS Go profiler did not; the host for that is a decision, not assumed."

The [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md) (D5): "every other class stays a raised flag carried to the qualification decision." (D6): the S6 latency and RSS flags "are raised with attribution and go to the qualification decision."

## Evidence base

All figures are from the [re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) on candidate `fc4c1bf1`: one Apple M4 Max, loopback endpoints, the frozen userspace relay, five paired blocks per readiness cell, 176 clean observations, all passing receiver integrity. Ratios are medians of per-block ratios against the same block's frozen Reno; absolute values are arm medians.

| Status | Flag | Ratio | Absolute (Reno → candidate) |
| --- | --- | --- | --- |
| Useful benefit | S6 goodput, STREAM / DATAGRAM | 12.31× / 14.09×, 5/5 pairs each | 6.1 → 85.6 / 5.9 → 85.9 Mbit/s |
| Passed | S6 sender CPU per useful GiB | 0.317 / 0.320 | 88.9 → 28.4 / 117.4 → 38.2 s/GiB |
| Passed | S5 goodput | 0.969 / 0.979 | 95.9 → 92.9 / 94.4 → 92.4 Mbit/s |
| Passed | S5 control p95 (p50) | 1.074 / 1.003 | p50 173 → 102 / 171 → 101 ms |
| Raised with attribution | S6 control p95 — selected draft-06 ProbeBW Up policy | 1.547 / 1.481 | 122.7 → 200.2 / 125.3 → 185.6 ms |
| Raised with attribution | S6 STREAM receiver RSS — loss-driven reassembly | 1.574 | 20.1 → 31.6 MiB |
| Raised with attribution | S5 sender RSS — design-bounded bookkeeping | 1.203 / 1.132 | 23.6 → 28.3 / 23.8 → 26.9 MiB |
| Raised, unresolved | S5 sender CPU per useful GiB | 1.170 / 1.320 | 27.3 → 28.8 / 27.8 → 36.7 s/GiB |
| Raised, unresolved | S5 DATAGRAM receiver CPU | 1.138 | 38.1 → 43.4 s/GiB |
| Raised, unresolved | Mixed memory: S5 STREAM receiver, S6 STREAM sender, S6 DATAGRAM sender RSS | 1.407 / 1.393 / 1.374 | 23.3 → 32.8 / 19.4 → 27.8 / 19.7 → 27.5 MiB |
| Raised (preservation) | Reno on candidate, S5 DATAGRAM sender / receiver CPU | 1.192 / 1.158 | instructions per GiB 0.994 / 0.990 of frozen Reno |

Measurement limits that bear on the unresolved flags:

- **Host noise is as large as the CPU flags.** Frozen Reno's own S5 sender CPU spans 26.4–38.4 s/GiB (DATAGRAM) across blocks, and non-fixture host load ranged from about 300% to 1,300% CPU, including Lightroom. Reno built from the candidate executes the same instructions per GiB as frozen Reno, yet its readiness CPU ratio reached 1.19.
- **The macOS profiler cannot discriminate the CPU excess.** In both arms, 97–100% of CPU samples land in four syscall and scheduler leaf frames. The candidate sender executes 13–14% more instructions per forward packet, with the same packet count as Reno. macOS instruction counts include kernel send and wake work, so "more instructions" does not separate BBR computation from wake cost.
- **The two known allocation sites are bounded to a minor share.** Delivery-record map churn in `captureCongestionSend` and `&sendReservation{}` in `localSendCredit.reserve` add about 245 B per packet, roughly a tenth of the instruction excess.
- **Engagement gaps.** The CE response is unengaged (S5 and S6 never mark). C4 items 1 and 3 were not re-examined. The low-rate pacing floor (Q = 2M) is unmeasured.

Linux host facts, checked 2026-10-03 for this decision: `minimax` (the repository's qualified Linux measurement host: AMD Ryzen AI MAX+ 395, 32 logical CPUs, kernel 7.0.0-30-generic, Go 1.27.1, idle load average 0.00) has `perf` 7.0.14 and `tc` (iproute2 6.19.0). `kernel.perf_event_paranoid` is 4, which blocks unprivileged CPU events; non-interactive `sudo` is available, so `sudo perf` can count user and kernel events separately without changing that setting. It is owned hardware with no cloud charge.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not declined**. Where the algorithm is meant to help, it does, repeatably: 12–14× Reno's receiver-verified goodput on injected-loss S6 in all ten pairs, at about a third of Reno's sender CPU per useful GiB. On clean S5 it holds goodput within 3.1% of Reno and halves median control latency. No measurement shows the algorithm is unsuitable.

BBRv3 is **not advanced** to further qualification. The S5 sender CPU flag (1.170 STREAM, 1.320 DATAGRAM) is raised and unresolved, and advancing would waive the operator's CPU limit for a cost nobody can yet explain.

**Reason it remains unresolved:** primarily **missing evidence** — the measurement host's noise floor is as large as the CPU flags, and its profiler cannot separate user from kernel and wake work. Secondarily a possible **implementation** cost: the 13–14% per-packet instruction excess, of which the known allocation churn explains about a tenth. The attributed flags in D2 are **algorithm trade-offs** or **design bounds**, not reasons to stop.

**Competence for the next decision** means: on the Linux host, every flag either passes, or is raised with an attribution the operator has accepted as an algorithm trade-off or design bound (D2), with no raised-and-unresolved flag and no raised preservation flag. Meeting it lets the next decision consider advancing; it is not itself a qualification.

### D2 — The three attributed flags are accepted as disclosed trade-offs

The operator accepts these as costs that do not block advancing. They remain **raised** in every report, with their attribution and absolute values; none becomes a pass.

- **S6 control p95 (1.547 / 1.481): algorithm trade-off.** 98–100% of forward-queue samples above 25 ms fall in ProbeBW Up or the following Down, the draft-06 policy selected in the design; Cruise and Refill median queue delay is 0.45–0.50 ms. Reno shows no comparable queue there because it uses about 6% of the link. On matched-load S5 there is no p95 excess. No tuning of Up.
- **S6 STREAM receiver RSS (1.574): algorithm behaviour.** The receiver holds frames behind losses while delivering 14× Reno's data at 0.37% forward loss.
- **S5 sender RSS (1.203 / 1.132, about +3–5 MiB per endpoint): design bound.** The design's 32,768-entry send-outcome ledger (1.16 MiB) and its other declared bookkeeping bounds fill within seconds at WAN rates; the design calls these "chosen memory/accuracy trade-offs". The bounds are not reopened now. Whether a few MiB per connection matters depends on the consumer's connection count, which is a production-design question recorded in the map's fog.

### D3 — Next: re-measure and attribute on the Linux host

One task ticket re-measures the **same candidate `fc4c1bf1`** on `minimax`, so the host is the only changed variable.

- **Arms and paths.** Loopback, S5 and S6, both workloads, five paired blocks: frozen Reno, candidate, Reno on candidate, and an A/A frozen-Reno arm in every block so the host's own noise range is measured, not assumed. Same fixture, relay, durations, seeds and flag limits as the re-demonstration. The relay stays the frozen userspace model (not `netem`), so Mac and Linux results differ only by host.
- **Isolation.** Each endpoint process and the relay pinned to disjoint core sets with `taskset`, following the host's established pinning precedent (cores 12, 13 and SMT siblings 28, 29 for a single process), with the sets recorded; `GOMAXPROCS=4` per endpoint, governor unchanged, load recorded before and after each run.
- **Attribution.** `sudo perf stat` counts user and kernel instructions and cycles separately per endpoint (`instructions:u`, `instructions:k`, `cycles:u`, `cycles:k`), plus context switches. `sudo perf record` with call graphs locates any user-space excess by function. No host setting changes: `perf_event_paranoid` stays at 4.
- **Pre-registered outcomes**, written into the ticket's record before its stages run:
  - *User-space excess in BBR or BBR-only allocation code* (a named group reaching a majority of the candidate's user-instruction excess): an implementation defect. It is fixed on a new identified revision — the two known allocation sites are eligible — and every readiness comparison is rerun on that revision.
  - *Kernel send or wake excess* (kernel instructions or cycles carry the majority): a pacing or platform cost, raised with attribution and carried to the next decision against the D08 pacing amendment. Not fixed in this ticket.
  - *Within the Linux A/A range:* reported as at the host's noise floor; the flag passes or fails on its Linux readiness median as registered.
  - *Anything else:* unresolved.
- **Mixed memory flags and the preservation flag** are re-measured under the same registered rules (the 70% memory rule; the preservation rule's instruction band of [0.97, 1.03] plus the A/A range).
- **Reporting.** Linux results are the input to the next decision. The Mac results stay as recorded, labelled by platform; Linux results do not overwrite or reinterpret them.
- **Aids.** The ticket copies and adapts the re-demonstration's aids for Linux (for example `perf stat` instead of `/usr/bin/time -l`) as new one-ticket aids. The frozen records are not edited.

### D4 — No paid experiment and no campaign

No paid qualification is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. A paid campaign would measure a candidate that has not yet met D1's competence condition; the open question is answerable on owned hardware. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) and its evidence stay unchanged.

### D5 — A fresh qualification decision follows the Linux ticket

A new grilling ticket, blocked by the Linux ticket, re-asks this question on the Linux evidence. If it advances BBRv3, it chooses the smallest qualification. The evidence most likely to change an adoption decision, recorded in the map's fog rather than ticketed now:

- **Coexistence** (acceptance rows L6/L7): whether BBRv3 displaces competing Reno or CUBIC flows on a shared bottleneck — a known BBR trade-off that a consumer sharing links would weigh heavily.
- **CE response** (S8): no measured path has engaged it.
- **Low-rate pacing** (L4/L5 maritime rows): the two-packet floor regime is unmeasured.
- **C4 items 1 and 3:** need a path that triggers all-spurious undo or rejected-rate rounds.

### D6 — The BBRv1 fallback tickets stay parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open. The first is re-wired to be blocked by the D5 decision ticket instead of this one. They matter only if BBRv3 is eventually not advanced; nothing so far points at a BBRv3 algorithm defect that BBRv1 would avoid.

## Rejected

- **Decline BBRv3.** The evidence attributes the remaining flags to measurement limits, a possible implementation cost and accepted algorithm or design trade-offs — not to algorithm unsuitability.
- **Advance now on the strength of the S6 benefit.** It would waive the CPU limit for an unexplained excess.
- **More attribution on the Mac.** Its noise floor and profiler cannot discriminate the remaining hypothesis.
- **Fixing the two allocation sites before measuring on Linux.** They bound to about a tenth of the excess; fixing them first would change two variables at once. They stay eligible under D3's defect outcome and remain production-slicing leads either way.
- **Lowering `perf_event_paranoid` on `minimax`.** It is a host security setting outside this map; `sudo perf` suffices.
- **Using `netem` for the Linux re-measurement.** It changes the emulator as well as the host; kernel emulation belongs to a qualification topology, if one is chosen.
- **Shrinking the bookkeeping bounds or tuning ProbeBW Up now.** Both are contract or design changes without a consumer requirement behind them.
- **Any paid run.**

## Map changes on acceptance

- Resolution comment on #672, issue closed, and one Decisions-so-far line on #666.
- New task ticket for D3, then the D5 grilling ticket blocked by it. #673 is re-wired from #672 to the D5 ticket.
- Fog updated: the Linux-host decision is removed (now ticketed); D2's bookkeeping-bound question and D5's adoption-evidence sketch are added.

## Limits

This is a planning decision. It does not claim readiness, establish any platform's qualification, or authorize a production merge, paid resource, campaign resumption, default-controller change or ledger change. The Linux host facts above are a capability check, not a measurement.
