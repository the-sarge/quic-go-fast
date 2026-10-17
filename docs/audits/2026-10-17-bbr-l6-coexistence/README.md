# BBRv3 L6: coexistence of the production port with a competing Reno flow

**Date:** October 17, 2026, America/New_York. **Scope:** [#777](https://github.com/the-sarge/quic-go-fast/issues/777), the acceptance plan's L6, carried by D5 of the [BBR port advancement decision](https://github.com/the-sarge/quic-go-fast/blob/ebfd9339/docs/audits/2026-10-16-bbr-port-advancement-decision/README.md) from [#747's D4](https://github.com/the-sarge/quic-go-fast/blob/0ba467db/docs/audits/2026-10-14-bbr-r9-advancement-decision/README.md#d4--the-smallest-qualification-that-would-change-an-adoption-decision-l6-coexistence-named-and-not-started). Measurement only: no code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change.

Branch `codex/bbr-l6-coexistence`, based on `abfa5456`, the candidate itself. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666) and [#748](https://github.com/the-sarge/quic-go-fast/issues/748) record, this record stays off `main`; a dev-journal entry on `main` points to it.

**Status: registration accepted by the operator on October 17, 2026 (about 22:15 UTC), before any counted observation; counted stage started unattended immediately after this commit.** The operator answered "Accept, start now" after being told the development runs' figures and the agent's expectation of an *adverse* outcome. The decisions were committed before any fixture, calibration, development or counted run of this record existed. The sections from [The candidate and the reference](#the-candidate-and-the-reference) to [Order](#order) are the registration. The development runs it discloses are excluded from every statistic. No counted observation exists.

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
  - relay2 counts reordered deliveries per flow and direction over the whole observation, warm-up included. Its samples carry no reorder timing, so the record attributes reordering to the 100 s change only through the model's structure, not by measurement.

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

`minimax` (AMD Ryzen AI MAX+ 395), with its host settings unchanged and recorded by `matrix.py hostfacts` at the start and end of the order and when a step fails:

| Process | Physical cores |
| --- | --- |
| Focal sender | 8–11 |
| Focal receiver | 12–15 |
| Relay | 4–5 |
| Competitor sender | 2–3 |
| Competitor receiver | 6–7 |
| Runner | 1 |

Every SMT sibling (n + 16) and core 0 stay out of every affinity set. Each competitor endpoint has two cores against the fixture's four Go processors; the acceptance plan asks for separate background-flow CPU, and the record reports each endpoint's CPU load (see [Usability](#observation-usability-and-reruns)). Binaries build with `GOTOOLCHAIN=go1.27.0`, `-trimpath`, `-buildvcs=false` and `CGO_ENABLED=0` for linux/amd64. [build.py](build.py) rebuilt every one byte-identically on the Mac; the aids were built identically on `minimax` from the same assembled module:

| Binary | SHA-256 | Source |
| --- | --- | --- |
| `reno-fixture` | `cda8feb1b2fb004d…` | frozen Reno `e4f322cb`, as slice 6 |
| `cand-fixture` | `d3f05ad685f92732…` | `abfa5456`, as slice 6 |
| `relay2` | `bff3ba4f86d4c71c…` | this record, frozen tree |
| `calibrate2` | `f4773dc9b53ae600…` | this record, frozen tree |
| `launch` | `a6146d62e16339f2…` | slice 6's launcher, frozen tree |

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
- **Control latency** p50 and p95 per period (ceil-index quantiles, as the readiness summaries), each reply assigned to the period of the opportunity that offered it, only when that assignment is exact: no skipped and no unresolved request. Otherwise the assignment can be ambiguous (a request pending from warm-up can skip the first measured opportunity), and that observation's per-period control latency is unusable. Whole-window control p95, replies, skipped, unresolved and missed opportunities are always reported.
- **CPU seconds per useful GiB and peak RSS** per endpoint (the fixture's `getrusage` figures, through slice 6's launcher).
- **From relay2:** per period, each flow's forward drops and mean queue occupancy, the largest sampled (100 ms) combined forward queue, and the largest forward lateness; per observation, the reorder counts and ECN codepoints per flow and direction.

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
- **Descriptive:** each participant's delivery in every period including back and absent, the combined delivery, the focal flow's delivery ratio, the relay's per-flow drops, queue occupancy, reorders and ECN codepoints, and absolute medians beside every ratio.

## Observation usability and reruns

An observation is **unusable** ([l6rules.py](l6rules.py) `health`) when any of the following holds; the reason is recorded:

- any of the five processes exits non-zero, or `summarize` rejects it: a receiver report differing between the endpoints, a controller other than configured, bins not summing to the useful bytes, relay errors, send errors, truncated reads, propagation overflow, or a relay model zero other than the measured start;
- the competitor's observed pause starts or ends more than 0.5 s from 240 s or 270 s. The fixture checks for the pause between blocking STREAM writes, so a starved competitor can observe it late: such an exclusion can depend on the arm. Every attempt's pause timing and reason is kept per arm (`summary.json` `attempts`) and reported, and a pair lost this way is never read as evidence of acceptable coexistence. The development runs observed offsets of 4–42 ms;
- relay2's largest arrival lag, or its largest forward or reverse lateness, exceeds 5 ms (Q1's native gateway gate);
- either participant's control stream reports an error;
- any endpoint's CPU time over the observation is at least 80% of its allotted cores (four focal, two competitor): a fixture limit, not controller behavior.

An observation is **contaminated** under #712's test, unchanged except for this record's CPUs: foreign CPU on the fixture cores and their siblings during the measured window above 0.10 cores mean or 0.50 cores in any second. When the test cannot be evaluated (missing host samples, or fewer than two in the window), the observation is unusable with that reason; only an affirmative uncontaminated result counts as clean (`l6rules.verdict`, `choose_triple`).

A receipt whose fixture or relay hash differs from the registered one also makes an observation unusable. So does any rise during the observation in the host's UDP `InErrors`, `RcvbufErrors`, `SndbufErrors` or `MemErrors` (the smoke rule, applied to every observation). These counters are host-wide: they show that some socket dropped or rejected datagrams outside the model's queue, not which flow. Every attempt keeps its counter deltas, and each endpoint's raw errors, pause timestamps and resources even when its summary fails (`matrix.py` `observation`, `raw_diagnostics`).

**Reruns** (the operator's #714 rule, [matrix.py](matrix.py) `follow`): a triple holding an unusable or contaminated observation is rerun once, all three arms, with the same seed, as phase `rerun`; the rerun replaces the original only when all three arms are usable and uncontaminated. At most four triples are rerun. A triple that stays unusable or contaminated leaves its pair's ratios not computable for that workload; it is never replaced by a third attempt. #738's quiet-host gate (60 s under 0.05 cores mean and 0.30 peak on the fixture cores and siblings) runs before every triple.

## Correctness gates

A transport correctness or protocol-compliance failure is never rerun, excepted or classified: the stage stops and the record reports it ([l6rules.py](l6rules.py) `correctness`). `matrix.py` checks all four endpoint reports after each observation is collected, before any usability judgment, rerun choice or classification; it does not depend on the summary succeeding. A failure is any of:

- corrupt or duplicated useful content counted at a receiver;
- one of the frozen fixture's integrity errors in any endpoint's errors, control error or top-level error: `invalid campaign payload`, `non-contiguous stream identity` and `invalid stream frame length` (receiver content checks), `control sequence mismatch` (sender), `run config mismatch` and `invalid report acknowledgment` (bytes read back on the control and report streams), and `record too large` (a length prefix beyond any record the fixture writes);
- a QUIC transport error code that states a protocol violation (RFC 9000, section 20.1), in the same places: `FLOW_CONTROL_ERROR`, `STREAM_LIMIT_ERROR`, `STREAM_STATE_ERROR`, `FINAL_SIZE_ERROR`, `FRAME_ENCODING_ERROR`, `TRANSPORT_PARAMETER_ERROR`, `CONNECTION_ID_LIMIT_ERROR`, `PROTOCOL_VIOLATION`, `CRYPTO_BUFFER_EXCEEDED` or `KEY_UPDATE_ERROR`. Codes that can reflect setup or operation (`INTERNAL_ERROR`, `CRYPTO_ERROR` TLS alerts, `AEAD_LIMIT_REACHED`, timeouts and others) are not in the list;
- when both endpoints of a participant completed their report exchange without a top-level error, a receiver report at the sender that differs from the receiver's own. This is a failed integrity check of the exchange, not proof of where it failed.

Timeouts, setup failures, incomplete exchanges and a missed pause are not correctness failures: they leave an observation unusable, and the rerun rule applies.

## Prerequisites

Run first, each excluded from every statistic; a failure ends the record as a reported prerequisite gap. First, every deployed binary is checked against the registered hashes (`matrix.py binaries`). Calibration and smoke each start after #738's quiet-host gate.

- **Path calibration** ([prereq.py](prereq.py), criteria in its docstring), relay2 driven by [calibrate2](calibrate2/main.go)'s stand-in endpoints:
  - ECN codepoints preserved per flow and direction;
  - no packet at the other flow's endpoint while both flows run;
  - one-way delays per flow and direction in warm-up (50 ms), in the short-path window (competitor 12.5 ms, focal 50 ms) and across both switch points (100 s, 200 s), each median in [D, D + 2 ms] and every probe in [D, D + 5 ms];
  - a combined delivered rate within 1% of 100 Mbit/s under overload from both flows, in each direction, with the shared queue reaching capacity less one packet and both flows losing packets.
- **Smoke** (`matrix.py smoke`): two full-length observations (STREAM `cand`, DATAGRAM `reno`). Each must be usable under the same per-observation verdict as a counted observation (correctness gate, health, registered hashes, host UDP errors, an evaluable contamination test), with every thread inside its CPU set. An affirmatively contaminated smoke observation is reported, not failed. Each observation's verdict and diagnostics are kept in `smoke.json`.

## Development runs (before this registration)

Made on `minimax`, each excluded from every statistic. Times are UTC on October 17. The decisions (`50792abf`, 05:55) and the classification, usability, contamination and rerun rules ([l6rules.py](l6rules.py) at `93314fd8`, 06:10) were committed before the first of them.

| Run | What | Result |
| --- | --- | --- |
| `prereq-dev1` (06:11) | Path calibration, first attempt | The ECN check failed because of a calibrate2 defect: its ECN mode labelled competitor probes with the wrong flow identity, so they arrived at the right endpoint but counted as foreign. The other six checks passed. Fixed in `5d8dd10d`. |
| `prereq-dev2` (06:12) | Path calibration, second attempt | All seven checks passed. Median one-way delays sat 0.3–0.9 ms above the model's (largest probe +2.2 ms), and the competitor's switches at 100 s and 200 s fell where the model puts them. The delivered rate was 99.98 (forward) and 99.99 Mbit/s (reverse), the queue reached 1,250,928 bytes, and both flows lost packets. |
| `dev1` (06:13–06:19) | STREAM, `cand` (BBRv3 focal), full length | Usable under the rules. Relay arrival lag 0.87 ms; lateness 2.85 ms forward, 2.11 ms reverse; every endpoint under 0.02 of its allotted cores; pause observed at 240.04 s and 270.00 s; control assignment exact. **Seen by the agent:** focal delivery 86.0, 83.1, 86.9, 93.7 and 87.4 Mbit/s, competitor 7.4, 11.3, 6.5, 0.0 and 5.7 Mbit/s (equal, short, back, absent, resumed). The BBRv3 flow sent 1,732,596 of its 2,791,964 forward packets as Not-ECT (its ECN was disabled during the run); the competitor kept ECT(0). |
| `dev2` (06:20–06:26) | STREAM, `reno` (Reno against Reno), full length | Usable. Relay arrival lag 0.27 ms; lateness 3.77 ms forward, 3.34 ms reverse; endpoints under 0.02 of their cores; pause at 240.00 s and 270.00 s. **Seen by the agent:** focal 47.0, 37.3, 45.9, 93.5 and 51.1 Mbit/s, competitor 48.9, 58.3, 49.6, 0.2 and 44.5 Mbit/s. Both flows kept ECT(0). |
| `dev3` (06:28–06:34) | DATAGRAM, `cand`, full length | Usable. Lag 0.40 ms; lateness 2.79 ms forward, 2.25 ms reverse; endpoints under 0.02 of their cores; pause at 240.01 s and 270.00 s. **Seen:** focal 83.8, 75.4, 85.7, 92.3 and 85.6 Mbit/s, competitor 8.4, 18.8, 6.3, 0.0 and 6.4 Mbit/s. The BBRv3 flow sent 1,109,180 of 3,085,709 forward packets as Not-ECT. |
| `dev4` (06:34–06:40) | DATAGRAM, `reno`, full length | Usable. Lag 1.53 ms; lateness 3.74 ms forward, 3.80 ms reverse; endpoints under 0.02 of their cores; pause at 240.00 s and 270.00 s. **Seen:** focal 47.4, 39.3, 49.9, 94.2 and 53.4 Mbit/s, competitor 47.7, 55.8, 45.0, 0.2 and 41.6 Mbit/s. Both flows kept ECT(0). |

Across the four full-length runs, the relay's largest forward lateness in a 100 ms interval had a median near 1 ms and exceeded 3 ms in at most three intervals per run; the largest single value was 3.80 ms.

What changed after these runs started:
- calibrate2's ECN-mode fix (`5d8dd10d`), for the defect `prereq-dev1` exposed. The same commit corrected the ECN and isolation wording in `prereq.py`'s docstring (the ECN check judges codepoints only; isolation is judged in the probe and rate checks), with `prereq.py`'s judging code unchanged;
- the correctness gate ([l6rules.py](l6rules.py) `correctness`, `matrix.py`, committed in `acc0b4f0` at 06:18, during `dev1` and before its result was read). It stops a stage on corrupt or duplicated content, which #777's "correctness gates apply unchanged" requires; no observation prompted it;
- the documentation of these sections.

No threshold, period, arm, order, estimator, usability limit or rerun rule changed after any of these observations. The relay limits (5 ms) were set before them; every run fell under them.

**The ECN observation is outside this record's question.** The L6 queue never marks, so a sender's ECN state does not change the queue's treatment of its packets. It is reported for every counted observation, per flow, from relay2's counters, and is a candidate follow-up, not part of the classification.

## Limits

- **One declared model, one host.** Results describe behavior under the frozen Q1 model's L6 on `minimax` loopback, five pairs per workload. They are not a measurement of any real path and make no universal claim.
- **One competitor schedule.** Both flows start together; the competitor's 25 ms period and its pause are the acceptance plan's fixed phases, with carryover between them. The periods are not independent steady states.
- **The competitor shares the host.** It has two cores per endpoint against the focal flow's four; the record reports each endpoint's load.
- **Pause-timing exclusions can depend on the arm** (see [Usability](#observation-usability-and-reruns)); they are reported, never counted as acceptable.
- **Not measured:** L7 (TCP CUBIC), L8, ECN marking (S8), other platforms, and any revision but `abfa5456`.

## Disclosures made before counted data

- **D2 preceded all data of this record,** and the operator chose 0.70 over the agent's 0.80 before any fixture existed.
- **Q1's cloud orchestration examples** were known (see [Known before these decisions](#known-before-these-decisions)).
- **The development runs above were seen,** including `dev1` and `dev3`, where the competitor next to BBRv3 delivered about 7–8 Mbit/s in the equal period against about 48–49 Mbit/s next to Reno in `dev2` and `dev4`. On that evidence the agent expects the counted stage to be *adverse*. Nothing in this registration was changed after it, and the expectation is not a result: the counted stage decides.
- **Interpretation known in advance.** The competitor's window reduction at 100 s is a property of the frozen model (reordering), common to every arm. Reno's growth at this path's bandwidth-delay product is slow, so a contested period is not a steady state. The candidate's ECN state can change during a run (`dev1`, `dev3`).

## Registration review

**First consideration:** RAS run `20261017T204953-20752a2689fc1b9ddaa31f4d` (`ras consider`, kind plan, default agents and adjudication), on `477f9763`. Its context was this registration, relay2 and its tests, the model overlay, `l6rules.py`, `run.py`, `matrix.py`, `prereq.py`, calibrate2 and the frozen model, with #777's binding requirements, the accepted decisions and the acceptance plan's L6 text quoted in the prompt. The synthesis queued eight items; each was checked against the code and dispositioned here.

| Item | Disposition | Change |
| --- | --- | --- |
| 1. The correctness gate missed the fixture's explicit integrity errors. STREAM identity and length failures are reported in `errors` before the corruption counter, so a corrupt observation would have been rerun. | fix-now | `l6rules.correctness` reads all four endpoint reports and stops on corrupt or duplicated content or any listed integrity error, before any usability judgment or rerun choice. Tests cover each error with zero counters, plus timeout controls. |
| 2. The clean-start check passed when only some leftovers existed (one `ls -d` over several operands). | fix-now | `driver.sh` checks each path or glob alone, including the correctness-failure report and binary check. Tested under `/bin/sh` with each category alone and with development artifacts. |
| 3. An unevaluable contamination test counted as clean. | fix-now | `l6rules.verdict` makes it unusable with a reason; `choose_triple` needs an affirmative uncontaminated result. |
| 4. The deployed binaries were not checked before execution. | fix-now | `matrix.py binaries` checks all five against the registered pins before calibration, and the driver stops on a mismatch. Each receipt's fixture and relay hashes are also checked when judging. |
| 5. `summary.json` lacked evidence the record promises to check figures against. | fix-now | It now holds every attempt's per-arm reasons, contamination, pause timing and CPU loads, and the full summary of every selected observation. |
| 6. Reorder counts are whole-observation totals, and period queue maxima are sampled. | fix-now (wording) | [Path](#path-the-frozen-q1-models-l6) and [Measures](#measures) corrected; no aid change. |
| 7. A pause-timing exclusion can depend on the arm (a starved competitor sees the pause late). | fix-now (disclosure) | The rule is unchanged. Exclusions are reported per arm and never read as acceptable ([Usability](#observation-usability-and-reruns), [Limits](#limits)). |
| 8. Prerequisites lacked the quiet-host gate; a smoke exception lost its report; an empty thread snapshot passed; a partial rerun was found only through its `reno` arm. | fix-now | `wait_quiet` runs before calibration and smoke; smoke keeps a failed observation's report and still fails; thread snapshots must be complete and error-free; any arm marks a rerun attempt. |

The synthesis also listed changes not to make. All were declined, in agreement with it:
- a goodput flag on the contested path;
- changes to A/A eligibility;
- a per-flow calibration bound;
- carryover telemetry;
- reopening settled scope.

**Replacement consideration:** RAS run `20261017T211548-5d8a87cbad5bf59d3cf09887`, on `5e431a1e`, with the first round's dispositions quoted as settled. It queued seven items, each checked against the code; no further review round is run.

| Item | Disposition | Change |
| --- | --- | --- |
| 1. QUIC protocol-violation errors (`PROTOCOL_VIOLATION`, `FLOW_CONTROL_ERROR`) would have been rerun, not stopped. A receiver report that diverged after a completed exchange was only an unusability. | fix-now | Both now stop the stage ([Correctness gates](#correctness-gates)). Operational codes and incomplete exchanges stay rerunnable. Tests cover both kinds. |
| 2. Reconstructed control-period assignments could be wrong but count-consistent (a request pending from warm-up). | fix-now | Per-period control latency is reported only when the assignment is exact, and is otherwise unusable for that observation. The review's carryover case is a test. |
| 3. The host's UDP error counters were judged only in smoke. | fix-now | A rise in any of them makes an observation unusable, disclosed as host-wide. The deltas are kept per attempt. The four development runs had none. |
| 4. Exclusion diagnostics were lost when an observation's summary failed. | fix-now | Each endpoint's raw errors, pause timestamps and resources are kept per attempt whether or not it summarizes or has a receipt. |
| 5. Smoke skipped the registered hash and contamination-evaluability checks. | fix-now | Smoke uses the counted stage's per-observation verdict; affirmative contamination is reported, not failed. |
| 6. Analysis would classify an incomplete order. | fix-now | No classification until the order completes ([Order](#order)). |
| 7. Three descriptions were inaccurate: the correctness gate's position, the host-facts timing, and `5d8dd10d`'s docstring change. | fix-now (wording) | Corrected in [Correctness gates](#correctness-gates), [Host](#host) and [Development runs](#development-runs-before-this-registration). |

The synthesis's items not to act on were declined, in agreement with it:
- a supposed classifier defect on exclusions (inconclusive is already the registered result);
- a figure checker before figures exist;
- stored derived fields;
- reopening settled scope.

**What changed in the registered rules,** across both rounds and after the development runs. Every change is stricter:
- the fixture's integrity errors, QUIC protocol-violation codes and a diverging receiver report after a completed exchange now stop the stage;
- an unevaluable contamination test, a receipt hash other than the registered one, and a rise in the host's UDP error counters each leave an observation unusable;
- per-period control latency is reported only from exact assignments;
- smoke applies the counted stage's per-observation verdict;
- no classification is emitted before the order completes.

None loosens a rule. No D2 threshold, period, arm, order, denominator floor, calibration criterion, fixture or model source, or registered hash changed. The aids' Go code is unchanged, so no binary was rebuilt and no calibration repeated. These changes are part of what the operator accepts with this registration.

## Time estimate

All aids exist and both fixture binaries are slice 6's. On a quiet host:

| Step | Estimate |
| --- | --- |
| Host facts and path calibration | about 3 minutes |
| Smoke (two observations) | about 13 minutes |
| Counted stage (30 observations of about 6.4 minutes, ten quiet-host gates of at least 1 minute) | about 3 h 25 min |
| Reruns, if triggered (up to four triples of about 20 minutes) | up to about 1 h 20 min |
| **Total** | **about 3 h 40 min without reruns; up to about 5 h** |

Foreign load extends the gates' waits.

## Order

**On the Mac:** `build.py` (every binary byte-identical to the one this registration names), `l6rules_test.py`, relay2's Go tests (also run on `minimax`), then `sync.sh`.

**On `minimax`,** sequentially under `taskset -c 1`, with nothing else built or tested there while a stage measures ([driver.sh](driver.sh)): `matrix.py hostfacts start`, `binaries`, `prereq`, `smoke`, `main`, `follow`, `hostfacts end`. The driver first checks, path by path, that no counted, rerun or smoke observation, calibration, smoke report, follow decision, correctness-failure report or binary check exists (`PRESTART-EMPTY`), since `run_case` reuses an existing clean observation; development artifacts are allowed. A failed binary check, calibration, smoke or correctness gate stops it.

**Then, anywhere:** [analyze.py](analyze.py) writes `summary.json` and prints each cell. It emits no classification unless the order completed: no correctness failure, and a completed follow decision for every triple; otherwise `summary.json` says `incomplete`, with the reasons. `summary.json` holds the classification, every period's per-pair values, the review flags, every attempt's per-arm reasons, contamination, pause timing and CPU loads, and the full summary of every selected observation; every figure the record reports is checked by script against it.
