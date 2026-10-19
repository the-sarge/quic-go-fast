# BBRv3 L6 on the combined revision (#808)

**Date:** October 19, 2026, America/New_York. **Scope:** step 2 of [#808](https://github.com/the-sarge/quic-go-fast/issues/808) ([campaign record](../2026-10-19-bbr-808-campaign/README.md)): L6 again, on the revision that carries #742, #800, #803, #743 and #824, reusing [#777's registration and aids](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md) and [#792's trace overlay and draft-06 checker](https://github.com/the-sarge/quic-go-fast/blob/89e71a60/docs/audits/2026-10-18-bbr-l6-diagnosis/README.md). Measurement only.

**This stage runs only after an advancing disposition** of the revision's readiness (the operator's #748 rule: no qualification runs on a new revision before its advancement disposition). If the revision is not advanced, it does not run, and the record says so.

**Status: registration.** The sections from [Question](#question) to [Order](#order) were written, with the aids adapted and the binaries built, before any observation of this stage, and before the readiness stage ran. Results are added below them afterwards.

## Question

When BBRv3 at `e7eb3f10` shares #777's L6 bottleneck with a competing QUIC Reno flow, does it displace that flow beyond #777's criterion? And for the requirements #800 and #803 left to this campaign: how does STREAM's displacement compare with #777's and #792's, is bulk STREAM still marked application-limited, and does ECN stay on through an L6-length run?

## Accepted decisions

From the campaign plan the operator approved on October 18, 2026 (campaign record, decisions D5–D7):

- **D5, L6:** #777's registration unchanged, plus #792's trace arm in pairs 1–3 with #800's one-line overlay update. The trace never enters the classification.
- **D6, #800's requirement:** STREAM's displacement beside #777's and #792's, and from the traces the share of two-percent checks that judged a packet marked application-limited (100% in #792), whether `inflight_longterm` is lowered and `max_bw` ages, clamp events and the draft-06 checker, judged from the trace's per-packet `Delivery.Limited` markers, not the qlog `limitation=` field.
- **D7, #803's requirement:** ECN stays on if no BBRv3 flow stops marking ECT(0) in any usable observation, read from relay2's per-flow ECN counts as #793's `crossrun.py` reads them, with the trace's ECN-failure marker.

## The candidate and the reference

- **Candidate: `cand`, `main` at `e7eb3f10`, selecting BBRv3.** Its fixture binary is `c9ff3595a7411f73…`, byte-identical to the readiness stage's `cand-fixture`. What the revision changes since `abfa5456`, and which changes reach this fixture, is in the readiness record's [The candidate](../2026-10-19-bbr-808-readiness/README.md#the-candidate): #800 and #803 act on every BBRv3 flow here, #824's queue on every DATAGRAM receiver; #742 and #743 cannot act, because the fixture's managed lease never GSO-batches.
- **Trace: `cand-trace`,** the same revision with #792's measurement-only overlay (`7730baef1846d58a…`).
- **Matched Reno, the A/A arm and the competitor: frozen Reno `e4f322cb`** (`cda8feb1b2fb004d…`), byte-identical to #777's.
- The fixture is the Q1 campaign's frozen `fixture/` with the causal-diagnosis heap patch, as in #777.

## Path, relay, endpoints, timing and host

#777's, unchanged: [Path](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#path-the-frozen-q1-models-l6) (the frozen Q1 model's L6: 100 Mbit/s each way, one shared 1,251,220-byte drop-tail FIFO, 100 ms round trip, the competitor at 25 ms during measured 100–200 s, no random loss, no marking), [the two-flow relay](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#the-two-flow-relay) (`relay2`), [Endpoints and timing](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#endpoints-and-timing) (60 s warm-up, 300 s measured, the competitor's bulk pause at 240–270 s, periods equal 0–100, short 100–200, back 200–240, absent 240–270, resumed 270–300) and [Host](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#host) (focal sender 8–11, focal receiver 12–15, relay 4–5, competitor 2–3 and 6–7, runner 1).

| Binary | SHA-256 | Source |
| --- | --- | --- |
| `reno-fixture` | `cda8feb1b2fb004d…` | frozen Reno; byte-identical to #777's |
| `cand-fixture` | `c9ff3595a7411f73…` | `e7eb3f10` (new) |
| `cand-trace-fixture` | `7730baef1846d58a…` | `e7eb3f10` plus #792's overlay (new) |
| `relay2`, `calibrate2`, `launch` | `bff3ba4f86d4c71c…`, `f4773dc9b53ae600…`, `a6146d62e16339f2…` | frozen tree; byte-identical to #777's |

## Arms, pairs and order

#777's triples in all five pairs, with the trace arm added in pairs 1–3 at a position drawn from its own seed (`random.Random('808-<pair>-<workload>')`); #777's triple order (`random.Random('777-<pair>-<workload>')`) is unchanged ([matrix.py](matrix.py) `order`). One quiet-host gate per pair-workload.

| Pair | First workload, arm order | Second workload, arm order |
| --- | --- | --- |
| 1 | STREAM: reno, aa, trace, cand | DATAGRAM: trace, aa, cand, reno |
| 2 | DATAGRAM: reno, aa, cand, trace | STREAM: aa, reno, trace, cand |
| 3 | STREAM: cand, aa, trace, reno | DATAGRAM: aa, trace, reno, cand |
| 4 | DATAGRAM: cand, reno, aa | STREAM: reno, aa, cand |
| 5 | STREAM: cand, aa, reno | DATAGRAM: reno, aa, cand |

Thirty-six counted observations: #777's thirty plus six traces. Seeds 7701–7705 by pair (the seed changes nothing on L6).

## Classification (#777's D2, unchanged)

For each workload, pair and classified period (equal, short, resumed), the pair ratio is the competitor's useful delivery next to `cand` ÷ next to `reno` in the same pair-workload. A period is *acceptable* when at least four of five pairs are at or above **0.70**, *adverse* when at least four are below, *inconclusive* otherwise; the overall outcome is *adverse* if any of the six cells is adverse, *acceptable* only if all six are ([l6rules.py](l6rules.py), #777's, unchanged). Reported beside it, as in #777: the A/A ratios, the competitor's control latency (flag above 1.20), and the focal flow against matched Reno under the unchanged readiness limits. No readiness exception covers any of them.

**What each outcome means (#777):** *adverse*, evidence against adopting the revision as it stands; *acceptable*, removes this one obstacle only; *inconclusive*, leaves the question open.

## The trace arm (pairs 1–3)

#792's instrument, unchanged except one hook text ([The instrument](https://github.com/the-sarge/quic-go-fast/blob/89e71a60/docs/audits/2026-10-18-bbr-l6-diagnosis/README.md#the-instrument-a-measurement-only-trace-overlay)): a measurement-only overlay in package `congestion`, rows of the BBR model's state in a fixed array outside the Go heap, written once after the fixture writes its own record, enabled only for the trace arm's focal sender until 2 s after the measured window.

- **The one changed hook.** [#806](https://github.com/the-sarge/quic-go-fast/pull/806) (`1f9ceb6e`) replaced `bbrBytes(previousFlight+prefix)` with draft-06's `bbrBytes(atLoss)` in the probe-loss assignment, so that hook's exact text changes ([build.py](build.py) `TRACE_SITES`), as #800's hand-over on #808 asks. The hook itself, the row right after the assignment, is #792's. The other eight sites are unchanged and each occurs once on `e7eb3f10`.
- **Tests,** in the exported `cand-trace` tree: the overlay's tests and the port's `internal/congestion` tests pass, also under `-race` for the overlay's; `go vet` passes for `internal/congestion`, the root package and the fixture.
- **The checker** ([draft06check.py](draft06check.py), C0–C9, 40 tests) and **the input check** ([reconcile.py](reconcile.py), R1–R4, 13 tests) are #792's, unchanged. With draft-06's arithmetic, C4 should record no clamp events (#800's hand-over).
- **Mechanism figures** come from #792's `traced` ([mechanism792.py](mechanism792.py), #792's `analyze.py` verbatim), descriptive. #792's disturbance rule (D3) was a gate for cause attribution; here it is carried as #792 computed it and decides nothing.
- **A trace observation is unusable** when its trace is missing, overflowed or saw a second sender (#792's rule).

## Readings for #800 and #803 (fixed before any counted run)

[analyze.py](analyze.py) writes them into `summary.json`; none changes the classification.

- **#800, STREAM displacement (descriptive).** Per classified period: the competitor's delivery next to BBRv3 here, next to BBRv3 in #777 and in #792 (plain and traced), and next to matched Reno here and in #777; the pair ratios here and in #777. From the traces, beside #792's: the share of two-percent checks that judged a packet marked application-limited (the per-packet `Delivery.Limited` reason the overlay records, reason application only), beside the share marked with any supply limitation (application, flow control or ProbeRTT, #792's aggregate), crossings and how many were limited, `inflight_longterm` against the path, the `max_bw` filter's cycle advances, the share of the competitor's drops in BBRv3's Up phase, and C4 clamp events. #777's and #792's figures are read from their `summary.json` at `71e88789` and `89e71a60`. A workload with no clean trace reports its trace figures as absent, never as zero or passing.
- **#803, ECN.** For every usable BBRv3 focal observation in every attempt (`cand` in all pairs and `trace` in pairs 1–3; counted and rerun; selected for the classification or not; contaminated or not), relay2's forward ECN counts ([Not-ECT, ECT(1), ECT(0), CE]) and, for traces, the first ECN-failure row. **ECN stays on** only when every such flow's forward Not-ECT count is at most 0.01% of its forward packets and no trace records an ECN failure, and at least one of them is uncontaminated. The rule is one-way: a contaminated observation that stopped marking still means ECN does not stay on (foreign CPU load creates no Not-ECT packets), but contaminated observations alone cannot show that it stays on. A flow above that share has stopped marking; for it the record gives #793's `crossrun.py` estimate of when (the first 100 ms sample at which its reconstructed arrivals reach its ECT(0) count). That estimate is computed only above the share, because for a flow that never switched it lands at the end of the run. Flows that keep ECN send a handful of Not-ECT handshake packets: 3 for every frozen-Reno flow in #777, and 4 for every BBRv3 flow that kept ECN (all twenty of slice 6's S5 and S6 candidate runs, and #792's development run `dev2`), about 0.0005% of an L6 flow's packets; the BBRv3 flows that switched in #777 and #792 sent 30–81% Not-ECT. If ECN does not stay on, the record reports in which observations and when, and the follow-up is the operator's decision.
- **DATAGRAM loss (descriptive, campaign decision D8).** Per DATAGRAM observation, the focal flow's sent messages against distinct messages received, beside relay2's forward drops for that flow. `DatagramReceiveDropsV1` is not read here; the side stage reports it.

## Usability, contamination, reruns and correctness

#777's rules, unchanged ([Observation usability and reruns](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#observation-usability-and-reruns), [Correctness gates](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#correctness-gates)), with these recorded changes:

- reruns run in two passes, each rerunning every arm a pair-workload has, once, with the same seed: first #777's pass, unchanged, for each pair-workload whose triple is unusable or contaminated (at most four); then a trace pass for each pair-workload whose triple was clean but whose trace observation is unusable, contaminated or incomplete, using only the slots the first pass left. So reruns stay within #777's cap of four, and the trace never takes a slot from a triple. A clean original triple is kept by `choose_triple` whatever its rerun shows, so a trace rerun never changes the classified data;
- the classification uses the triple chosen by #777's `choose_triple`; the trace used is the clean one from the same attempt, or else the other attempt's clean trace; a pair with no clean trace has no trace figures;
- smoke is #777's two observations (STREAM `cand`, DATAGRAM `reno`) plus #792's full-length `trace` STREAM observation, which must leave a complete trace;
- the driver holds `RUNNING` at the worktree root for the whole order. It moves the readiness order's `STAGE.DONE` into this record's artifacts at the start. On any exit (signals included) it writes `L6.DONE` with how the order ended (`complete`, `stopped at <step>` or `stopped`), writes `STAGE.DONE` again with the same text, and removes `RUNNING`. So the markers agreed with the wiremux sessions stay true throughout L6.

A transport correctness or protocol-compliance failure stops the stage and is never rerun, excepted or classified. Contamination triggers one rerun and never decides an outcome.

## Prerequisites

#777's, unchanged: the deployed-binary check against the registered hashes, path calibration through `calibrate2` (seven checks), and smoke (above), each excluded from every statistic. A failure ends the stage as a reported prerequisite gap.

## Aids

- **Copied byte-identical** (checked by [build.py](build.py) before every export, Python aids and checker fixtures included): from #777, `relay2/`, `calibrate2/`, `launch/`, `model-overlay/`, `l6rules.py` (28 tests, with this record's `matrix.py` and `analyze.py`), `l6rules_test.py`, `prereq.py`; from #792, `trace/`, `draft06check.py` and its test and `testdata/`, `reconcile.py` and its test, and `mechanism792.py` (#792's `analyze.py`).
- **Adapted, with recorded changes in each file's header:** [build.py](build.py) (#792's), [run.py](run.py) (#777's with #792's trace arm, exactly the substitutions in [runchanges.py](runchanges.py), checked by [runchanges_test.py](runchanges_test.py)), [matrix.py](matrix.py) (#777's), [driver.sh](driver.sh) (#792's), [pack.py](pack.py) (#777's), [figures.py](figures.py) (#777's, with the readings' tables added), and the new [analyze.py](analyze.py) with [analyze_test.py](analyze_test.py) (9 tests: the ECN rule and the marking shares). [go.mod](go.mod) keeps these aids out of the archived-evidence module.
- **Dry run.** Before any observation of this campaign, `matrix.follow` and `analyze.py` ran on #777's thirty counted observations with #792's six traces in pairs 1–3, judged against those records' binary pins: the classification and all thirty pair and A/A ratios reproduced #777's exactly, the trace figures reproduced #792's (STREAM application-limited share 1.0 in every period, DATAGRAM 0.0), the checker found no mismatch and every input check was `ok`, ECN read *not on* (as it was) with switch times matching #793's and the traces' failure rows, #792's one BBRv3 run that kept ECN read as on, and the focal DATAGRAM loss equalled the relay's drops within one packet.

## Time estimate

On a quiet host: host facts, binary check and calibration about 4 minutes; smoke (three observations) about 20 minutes; the counted stage, 36 observations of about 6.4 minutes with ten quiet-host gates, about 3 h 55 min; reruns, if triggered, up to four pair-workloads of 20–27 minutes. **About 4 h 20 min without reruns; up to about 6 h.**

## Order

Only after an advancing disposition. **On the Mac:** `build.py all` (done; every binary matches its pin), the Python tests and relay2's Go tests (done), then `sync.sh` (the readiness record's). **On `minimax`,** unattended under `taskset -c 1`, by a systemd user timer running [driver.sh](driver.sh): the clean-start check (no counted, rerun or smoke observation, calibration, smoke report, follow decision, correctness-failure report, binary check, `hostfacts/` directory or `L6.DONE`), then `matrix.py hostfacts start`, `binaries`, `prereq`, `smoke`, `main`, `follow`, `hostfacts end`. A failed binary check, calibration, smoke or correctness gate stops it. Booking as for readiness (readiness record, [Order](../2026-10-19-bbr-808-readiness/README.md#order)). **Then, anywhere:** `analyze.py`, which writes no classification unless the order completed.

## Registration review

Reviewed with the whole #808 registration; see the [readiness record](../2026-10-19-bbr-808-readiness/README.md#registration-review).
