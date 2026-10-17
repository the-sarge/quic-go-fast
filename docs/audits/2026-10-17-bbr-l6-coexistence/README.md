# BBRv3 L6: coexistence of the production port with a competing Reno flow

**Date:** October 17, 2026, America/New_York. **Scope:** [#777](https://github.com/the-sarge/quic-go-fast/issues/777), the acceptance plan's L6, carried by D5 of the [BBR port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md) from [#747's D4](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md#d4--the-smallest-qualification-that-would-change-an-adoption-decision-l6-coexistence-named-and-not-started). Measurement only: no code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change.

Branch `codex/bbr-l6-coexistence`, based on `abfa5456`, the candidate itself. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) and [#748](https://github.com/the-sarge/quic-go-fast/issues/748) record, this record stays off `main`; a dev-journal entry on `main` points to it.

**Status: decisions accepted; no fixture built and no observation made.** This commit records the operator's decisions before any fixture, calibration, development or counted run of this record exists. The registration follows in later commits, before any counted observation.

## Question

When BBRv3 shares a bottleneck with a competing QUIC Reno flow, does it displace that flow beyond a fairness criterion fixed before any data?

## Accepted decisions (before any data)

The agent presented one consolidated plan with six recommended decisions on October 17, 2026, at about 01:50 America/New_York. The operator answered "Approve, threshold 0.70": every recommendation stands except D2's threshold, which the operator set at 0.70 instead of the recommended 0.80.

- **D1, the revision: `main` at `abfa5456d99a9299825d04454a2e7ed12dff3326`**, the revision the [port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md) advanced on Linux. Later `main` commits are not measured. `main` has since gained [#779](https://github.com/the-sarge/quic-go-fast/pull/779) (`81a72481`), a production change to the handshake packet-size fallback, so a later commit would be a new revision needing its own readiness measurement and advancement disposition first.
- **D2, the fairness criterion, fixed before data.** For each workload (STREAM, DATAGRAM), pair and period, the ratio is the competitor's useful delivery when the focal flow is BBRv3 divided by its useful delivery when the focal flow is matched Reno in the same pair. The classified periods are the measured intervals t = 0–100 s (equal round trips), t = 100–200 s (the competitor on the 25 ms round trip) and t = 270–300 s (after the competitor resumes).
  - *Acceptable:* in every classified period of both workloads, at least four of five pairs are at or above **0.70**.
  - *Adverse:* in any classified period of either workload, at least four of five pairs are below 0.70.
  - *Inconclusive:* any other result, including a period with fewer than four usable pairs on either side of the threshold.
  - Reported beside the classification and never changing it: the competitor's control-response latency (flagged when more than 20% higher than with matched Reno, the acceptance plan's control-latency tolerance), and the focal flow's own useful delivery, control p95, CPU time per useful GiB and peak RSS against matched Reno under the unchanged readiness limits. No readiness exception covers any of them.
  - The registration owns the estimator details (period binning, the treatment of a too-small denominator, usability). It may not change the threshold, the periods or the three outcomes without the operator.
- **D3, an A/A arm.** Each pair also runs matched Reno a second time. Its ratios are reported beside each period and never override the classification.
- **D4, no L8.** It is not established that GridCast will run several BBR connections over one bottleneck.
- **D5, process.** This record branch with a pointer entry on `main`; one `ras consider` of the registration before any counted observation, with at most one replacement consideration after fixes; operator acceptance of the registration before the counted stage; the `minimax` quiet window (or an unattended run) requested only when the stages are ready.
- **D6, the competitor and matched Reno: frozen Reno, `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`**, the readiness baseline every readiness record used. No Reno-on-candidate arm runs, so this record measures no preservation cell.

## Known before these decisions

- The [Q1 campaign](../2026-09-25-bbrv3-q1-campaign/README.md) ran L6, L7 and L8 once each on cloud hardware as orchestration examples, with **Reno** as the focal flow, and judged them only by process exits and gateway gates. The agent saw per-period figures derived from those archived records: in the L6 example, the competitor delivered about 47, 63 and 53 Mbit/s in the three classified periods and the focal Reno about 49, 32 and 43 Mbit/s. The L8 example had the frozen (pre-correction) BBRv3 as the *competitor*. No run anywhere has had BBRv3 as the focal flow of a coexistence case.
- At this path's bandwidth-delay product (about 857 full-size packets), Reno recovers from a window reduction over minutes, so a Reno-against-Reno split within a 100 s period varies between runs. With five pairs, *inconclusive* is a realistic outcome.

## The candidate and the reference

- **Candidate: `cand`, `main` at `abfa5456`, selecting BBRv3** (D1). The fixture binary is `d3f05ad685f92732…`, byte-identical to the one [slice 6's readiness record](https://github.com/the-sarge/quic-go-fast/blob/9b38f0ab/docs/audits/2026-10-16-bbr-port-readiness/README.md) measured.
- **Matched Reno, the A/A arm and the competitor: `reno`, frozen Reno `e4f322cb`** (D6). The fixture binary is `cda8feb1b2fb004d…`, byte-identical to the readiness denominator of #715, #736, #738, #739 and slice 6.
- **The fixture** is the [Q1 campaign](../2026-09-25-bbrv3-q1-campaign/README.md)'s frozen `fixture/` with the causal-diagnosis heap patch (`798f4998`), as in every readiness record; its source is identical at both revisions. [build.py](build.py) rebuilds both binaries and asserts their hashes.
- **What a ratio compares.** Every arm runs the same competitor binary under the same schedule. A ratio of the competitor's delivery therefore compares what the competitor gets next to `main`'s BBRv3 at `abfa5456` with what it gets next to frozen Reno. The focal arms also differ in every non-controller change `main` carries since `e4f322cb` (slice 6's record lists them); the port contract keeps unselected Reno unchanged, and the record makes no claim that separates those changes from the controller.

## Path: the frozen Q1 model's L6

[relay2](relay2/main.go) applies the frozen Q1 packet-path model ([model.go](../2026-09-25-bbrv3-q1-campaign/model/model.go), [scenarios.go](../2026-09-25-bbrv3-q1-campaign/model/scenarios.go), built from the frozen tree) with scenario `L6`, unchanged:

- **Each direction:** 100 Mbit/s, serialization charged on full IPv4 bytes, one drop-tail FIFO of 1,251,220 bytes (857 × 1,460, one bandwidth-delay product rounded up to whole packets), and propagation delay applied after the queue. No random loss and no marking: the seed changes nothing (recorded per pair).
- **Shared:** both flows' packets enter the same FIFO in each direction.
- **Delay:** 50 ms one way (100 ms round trip) for the focal flow always, and for the competitor except while a competitor packet's service finishes in [100 s, 200 s) of model time, when it is 12.5 ms (25 ms round trip), in both directions. Model time zero is the measured start, as Q1's router used it; warm-up is negative model time.
- **Known properties of the model at the competitor's delay changes** (source inspection and [relay2's tests](relay2/main_test.go), before any observation):
  - At 100 s, competitor packets finishing service after the boundary overtake earlier ones still in propagation, up to 37.5 ms of the competitor's traffic. QUIC's packet-threshold loss detection is likely to declare some overtaken packets lost, so the competitor probably takes a window reduction at the start of the short-path period, in every arm.
  - At 200 s the competitor's deliveries pause for 37.5 ms; nothing is reordered.
  - relay2 counts reordered deliveries per flow and direction, and the record reports them per period.

## The two-flow relay

[relay2/main.go](relay2/main.go) is slice 6's relay (#711's v3 with #712's Linux ECN adapter) with the recorded changes listed in its header:

- two socket pairs, one per flow, feeding the model's two shared queues, with the competitor flag set by the socket a packet arrives on, in both directions;
- deliveries routed by that flag to the flow's own endpoint;
- per-flow counters (received, admitted, dropped, delivered, reordered, ECN, send errors, truncated reads), a per-flow arrival sequence kept in the relay-owned header headroom, and 100 ms samples of per-flow queue occupancy, drops and delivered bytes, with the largest forward lateness in each interval;
- model time zero at the measured start.

The event loop, the 10 ms sampler wakeup, the timer and the arrival channel are unchanged. [model-overlay/flows.go](model-overlay/flows.go) is a new read-only accessor splitting queue occupancy by flow; [model-overlay/next.go](model-overlay/next.go) is slice 6's, copied.

Its tests ([main_test.go](relay2/main_test.go), six) cover the competitor's delay at every boundary in both directions, overflow charged to the arriving flow in the shared queue, per-flow counters summing to the model's totals, reordering at 100 s and none at 200 s, the headroom sequence leaving payloads unchanged, and routing over real loopback sockets. Two injected faults (the competitor flag dropped at admission; forward deliveries routed to the other flow) each failed the tests, and the source was restored. The tests and `go vet` pass on `minimax`.

## Endpoints and timing

- **Endpoint contract,** unchanged from the readiness records: `GOMAXPROCS=4` (hard-coded in the fixture), tracing disabled, initial packet size 1,400 with path MTU discovery off, 64/128 MiB flow-control windows, the managed packet-I/O lease, 16,384-byte STREAM records, 1,200-byte DATAGRAMs, and the reliable control stream (32-byte request and reply, offered once per second, at most one outstanding) beside each bulk workload.
- **The competitor:** frozen Reno selecting Reno, a reliable stream of 16,384-byte records, and Q1's accepted bulk pause. It offers no bulk data during measured t = 240–270 s on the same connection, keeping its controller state, while its control probe continues. The fixture accepts only that pause, only with 60 s warm-up and 300 s measured.
- **Timing:** both flows start together 10 s after launch; 60 s warm-up and 300 s measured, as the acceptance plan's Long rows require. Each endpoint's useful delivery is counted in 300 one-second bins of the measured window.
- **Periods** (measured seconds): equal 0–100, short 100–200, back 200–240, absent 240–270, resumed 270–300. Equal, short and resumed are classified (D2); back and absent are reported only.

## Host

`minimax` (AMD Ryzen AI MAX+ 395), with its host settings unchanged and recorded by `matrix.py hostfacts` before and after each stage:

| Process | Physical cores |
| --- | --- |
| Focal sender | 8–11 |
| Focal receiver | 12–15 |
| Relay | 4–5 |
| Competitor sender | 2–3 |
| Competitor receiver | 6–7 |
| Runner | 1 |

Every SMT sibling (n + 16) and core 0 stay out of every affinity set. Each competitor endpoint has two cores against the fixture's four Go processors; the acceptance plan asks for separate background-flow CPU, and the record reports each endpoint's CPU load (see [Usability](#observation-usability-and-reruns)). Binaries build with `GOTOOLCHAIN=go1.27.0`.

## Arms, pairs and order

Per pair and workload, a **triple** of three observations, each with the same competitor:

| Arm | Focal fixture | Focal controller |
| --- | --- | --- |
| `reno` | frozen Reno | Reno (matched reference) |
| `cand` | `abfa5456` | BBRv3 |
| `aa` | frozen Reno | Reno (A/A, D3) |

Five pairs. Each pair runs both workloads, STREAM first in odd pairs, after one quiet-host gate per triple. Within a triple the arms run in a seeded random order (`random.Random('777-<pair>-<workload>')`, [matrix.py](matrix.py) `order`), fixed here:

| Pair | First workload, arm order | Second workload, arm order |
| --- | --- | --- |
| 1 | STREAM: reno, aa, cand | DATAGRAM: aa, cand, reno |
| 2 | DATAGRAM: reno, aa, cand | STREAM: aa, reno, cand |
| 3 | STREAM: cand, aa, reno | DATAGRAM: aa, reno, cand |
| 4 | DATAGRAM: cand, reno, aa | STREAM: reno, aa, cand |
| 5 | STREAM: cand, aa, reno | DATAGRAM: reno, aa, cand |

Thirty counted observations. Seeds 7701–7705 by pair.

## Measures

For each participant (focal and competitor), each observation and each period, from [run.py](run.py) `summarize`:

- **Useful delivery,** Mbit/s, from the receiver's one-second bins (bytes inside the measured window, fixture header excluded).
- **Control latency** p50 and p95 per period (ceil-index quantiles, as the readiness summaries), each reply assigned to the period of the opportunity that offered it. With no skipped and no unresolved request, the assignment is exact. Otherwise it is reconstructed from offer times and latencies, and it is used only if the reconstructed offered, skipped, replied and unresolved counts equal the fixture's own; else that observation's per-period control latency is unusable. Whole-window control p95, replies, skipped, unresolved and missed opportunities are reported too.
- **CPU seconds per useful GiB and peak RSS** per endpoint (the fixture's `getrusage` figures, through slice 6's launcher).
- **From relay2,** per period: each flow's forward drops and mean queue occupancy, the largest combined queue, the largest forward lateness, and the reorder counts.

## Classification: D2 and its estimator

- **Pair ratio,** per workload, pair and classified period: the competitor's useful delivery in the `cand` observation ÷ the same in the `reno` observation of that triple ([l6rules.py](l6rules.py) `pair_ratio`). A `reno` value below 5 Mbit/s (5% of the bottleneck) leaves the pair's ratio for that period not computable; its absolute difference is reported, as the acceptance plan asks for a too-small denominator. A triple that is unusable after its rerun leaves every ratio of that pair and workload not computable.
- **Period outcome** (`classify_period`): *acceptable* when at least four of the five pairs are at or above 0.70; *adverse* when at least four are below 0.70; *inconclusive* otherwise, including when not computable pairs leave fewer than four on either side.
- **Overall outcome** (`overall`): *adverse* if any of the six classified cells (two workloads × three periods) is adverse; *acceptable* only if all six are acceptable; *inconclusive* otherwise.
- **What each outcome means** (#777): *adverse*, evidence against adopting the revision as it stands, whose cause needs diagnosis before any claim about the algorithm; *acceptable*, removes this one obstacle only; *inconclusive*, leaves the question open.

## Reported beside the classification

None of these changes the classification or ends in a decision here.

- **The A/A ratio** (`aa` ÷ `reno`) beside every pair ratio, in every period.
- **The competitor's control latency:** per pair and period, `cand` p95 ÷ `reno` p95, flagged when the median over usable pairs (at least three) is above 1.20, with the pairs crossing.
- **The focal flow against matched Reno** under the unchanged readiness limits, whole measured window, median of per-pair ratios: control p95 (1.20), sender and receiver CPU seconds per useful GiB (1.10), and sender and receiver peak RSS (1.10), each with its A/A ratios. No readiness exception covers any of them. The goodput limit is for clean paths and is not applied to a contested one.
- **Descriptive:** each participant's delivery in every period including back and absent, the combined delivery, the focal flow's delivery ratio, the relay's per-flow drops, queue occupancy and reorders, and absolute medians beside every ratio.

## Observation usability and reruns

An observation is **unusable** ([l6rules.py](l6rules.py) `health`) when any of the following holds; the reason is recorded:

- any of the five processes exits non-zero, or `summarize` rejects it: a receiver report differing between the endpoints, a controller other than configured, bins not summing to the useful bytes, relay errors, send errors, truncated reads, propagation overflow, or a relay model zero other than the measured start;
- the competitor's observed pause starts or ends more than 0.5 s from 240 s or 270 s;
- relay2's largest arrival lag, or its largest forward or reverse lateness, exceeds 5 ms (Q1's native gateway gate);
- either participant's control stream reports an error;
- any endpoint's CPU time over the observation is at least 80% of its allotted cores (four focal, two competitor): a fixture limit, not controller behavior.

An observation is **contaminated** under #712's test, unchanged except for this record's CPUs: foreign CPU on the fixture cores and their siblings during the measured window above 0.10 cores mean or 0.50 cores in any second.

**Reruns** (the operator's #714 rule, [matrix.py](matrix.py) `follow`): a triple holding an unusable or contaminated observation is rerun once, all three arms, with the same seed, as phase `rerun`; the rerun replaces the original only when all three arms are usable and uncontaminated. At most four triples are rerun. A triple that stays unusable or contaminated leaves its pair's ratios not computable for that workload; it is never replaced by a third attempt. #738's quiet-host gate (60 s under 0.05 cores mean and 0.30 peak on the fixture cores and siblings) runs before every triple.

## Correctness gates

Corrupt or duplicated useful content at either receiver is a transport correctness failure ([l6rules.py](l6rules.py) `correctness`). It is never rerun, excepted or classified: the stage stops (`matrix.py` checks after every observation) and the record reports it.

## Prerequisites

Run first, on a quiet host, each excluded from every statistic; a failure ends the record as a reported prerequisite gap.

- **Path calibration** ([prereq.py](prereq.py), criteria in its docstring), relay2 driven by [calibrate2](calibrate2/main.go)'s stand-in endpoints:
  - ECN codepoints preserved per flow and direction;
  - no packet at the other flow's endpoint while both flows run;
  - one-way delays per flow and direction in warm-up (50 ms), in the short-path window (competitor 12.5 ms, focal 50 ms) and across both switch points (100 s, 200 s), each median in [D, D + 2 ms] and every probe in [D, D + 5 ms];
  - a combined delivered rate within 1% of 100 Mbit/s under overload from both flows, in each direction, with the shared queue reaching capacity less one packet and both flows losing packets.
- **Smoke** (`matrix.py smoke`): two full-length observations (STREAM `cand`, DATAGRAM `reno`). Each must be usable under the rules above, with every thread inside its CPU set and no UDP receive-buffer, send-buffer or input errors. Each endpoint's CPU load, the relay's lag and lateness, and the competitor's observed pause are recorded.
