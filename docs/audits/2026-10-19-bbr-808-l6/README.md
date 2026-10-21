# BBRv3 L6 on the combined revision (#808)

**Date:** October 19, 2026, America/New_York. **Scope:** step 2 of [#808](https://github.com/the-sarge/quic-go-fast/issues/808) ([campaign record](../2026-10-19-bbr-808-campaign/README.md)): L6 again, on the revision that carries #742, #800, #803, #743 and #824, reusing [#777's registration and aids](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md) and [#792's trace overlay and draft-06 checker](https://github.com/the-sarge/quic-go-fast/blob/89e71a60/docs/audits/2026-10-18-bbr-l6-diagnosis/README.md). Measurement only.

**This stage runs only after an advancing disposition** of the revision's readiness (the operator's #748 rule: no qualification runs on a new revision before its advancement disposition). If the revision is not advanced, it does not run, and the record says so.

**Status: complete.** The sections from [Question](#question) to [Order](#order) were written, with the aids adapted and the binaries built, before any observation of this stage, and before the readiness stage ran. The operator advanced `d94b3ec2` on Linux on October 19 ([campaign record](../2026-10-19-bbr-808-campaign/README.md#operator-decision)); the stage ran on October 21, and its [Results](#results) follow the registration.

## Question

When BBRv3 at `d94b3ec2` shares #777's L6 bottleneck with a competing QUIC Reno flow, does it displace that flow beyond #777's criterion? And for the requirements #800 and #803 left to this campaign: how does STREAM's displacement compare with #777's and #792's, is bulk STREAM still marked application-limited, and does ECN stay on through an L6-length run?

## Accepted decisions

From the campaign plan the operator approved on October 18, 2026 (campaign record, decisions D5–D7):

- **D5, L6:** #777's registration unchanged, plus #792's trace arm in pairs 1–3 with #800's one-line overlay update. The trace never enters the classification.
- **D6, #800's requirement:** STREAM's displacement beside #777's and #792's, and from the traces the share of two-percent checks that judged a packet marked application-limited (100% in #792), whether `inflight_longterm` is lowered and `max_bw` ages, clamp events and the draft-06 checker, judged from the trace's per-packet `Delivery.Limited` markers, not the qlog `limitation=` field.
- **D7, #803's requirement:** ECN stays on if no BBRv3 flow stops marking ECT(0) in any usable observation, read from relay2's per-flow ECN counts as #793's `crossrun.py` reads them, with the trace's ECN-failure marker.

## The candidate and the reference

- **Candidate: `cand`, `main` at `d94b3ec2`, selecting BBRv3** (re-pinned from `e7eb3f10` by the operator before any data; readiness record, [The candidate](../2026-10-19-bbr-808-readiness/README.md#the-candidate)). Its fixture binary is `e50b799c0cefa591…`, byte-identical to the readiness stage's `cand-fixture`. What the revision changes since `abfa5456`, and which changes reach this fixture, is in the readiness record's [The candidate](../2026-10-19-bbr-808-readiness/README.md#the-candidate): #800 and #803 act on every BBRv3 flow here, #824's queue on every DATAGRAM receiver; #742 and #743 cannot act, because the fixture's managed lease never GSO-batches.
- **Trace: `cand-trace`,** the same revision with #792's measurement-only overlay (`09bd3dd85820ed38…`).
- **Matched Reno, the A/A arm and the competitor: frozen Reno `e4f322cb`** (`cda8feb1b2fb004d…`), byte-identical to #777's.
- The fixture is the Q1 campaign's frozen `fixture/` with the causal-diagnosis heap patch, as in #777.

## Path, relay, endpoints, timing and host

#777's, unchanged: [Path](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#path-the-frozen-q1-models-l6) (the frozen Q1 model's L6: 100 Mbit/s each way, one shared 1,251,220-byte drop-tail FIFO, 100 ms round trip, the competitor at 25 ms during measured 100–200 s, no random loss, no marking), [the two-flow relay](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#the-two-flow-relay) (`relay2`), [Endpoints and timing](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#endpoints-and-timing) (60 s warm-up, 300 s measured, the competitor's bulk pause at 240–270 s, periods equal 0–100, short 100–200, back 200–240, absent 240–270, resumed 270–300) and [Host](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#host) (focal sender 8–11, focal receiver 12–15, relay 4–5, competitor 2–3 and 6–7, runner 1).

| Binary | SHA-256 | Source |
| --- | --- | --- |
| `reno-fixture` | `cda8feb1b2fb004d…` | frozen Reno; byte-identical to #777's |
| `cand-fixture` | `e50b799c0cefa591…` | `d94b3ec2` (new) |
| `cand-trace-fixture` | `09bd3dd85820ed38…` | `d94b3ec2` plus #792's overlay (new) |
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

- **The one changed hook.** [#806](https://github.com/the-sarge/quic-go-fast/pull/806) (`1f9ceb6e`) replaced `bbrBytes(previousFlight+prefix)` with draft-06's `bbrBytes(atLoss)` in the probe-loss assignment, so that hook's exact text changes ([build.py](build.py) `TRACE_SITES`), as #800's hand-over on #808 asks. The hook itself, the row right after the assignment, is #792's. The other eight sites are unchanged and each occurs once on `d94b3ec2`.
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

## Results

**Answer.** BBRv3 at `d94b3ec2` still displaces the competing Reno flow beyond #777's criterion: **adverse** in all six classified cells, with every pair ratio (the competitor's delivery next to BBRv3 over its delivery next to matched Reno) between 0.114 and 0.391 against the 0.70 threshold (A/A 0.802–1.302). For #800: bulk STREAM is no longer marked application-limited (0% of checks, against 100% in #792), its `max_bw` filter now cycles (19 advances per equal period, none in #792) and `inflight_longterm` sits below the path (0.91–0.98, against 1.04–1.07), a trace signature like DATAGRAM's. STREAM's competitor gets more than in #777 (short period 19.2 against 11.9 Mbit/s, about 61% more), so STREAM's displacement eased to about DATAGRAM's level, but its pair ratios stay far below 0.70. For #803: **ECN stays on**: every BBRv3 flow in every attempt sent exactly its 4 Not-ECT handshake packets and no trace recorded an ECN failure. The draft-06 checker (#792's, unchanged) reports one C8 mismatch in the measured window, which comes from the port's declared marking of ProbeRTT samples ([below](#trace-checks)).

### The run

- **Attempt 1** (October 20, from 09:00 UTC) stopped at smoke: GridCast's GARM CI (a Windows VM and Linux containers) loaded the host after the quiet-host gate passed, and the DATAGRAM smoke observation recorded 1,266 host-wide UDP receive-buffer errors, so it was unusable and smoke failed as registered ("a reported prerequisite gap"). No counted observation ran. Its files are kept with the raw archive (`observations-attempt1/`).
- **Attempt 2** (October 21, 06:13–11:19 UTC, 5 h 06 min against the registered 4 h 20 min to about 6 h), from a clean start: binary check, calibration (seven checks), smoke (all three usable; the DATAGRAM one contaminated but without UDP errors, reported as registered), the counted stage, and the follow stage. Two pair-workloads were rerun once each, as registered, and both reruns were clean and selected: DATAGRAM pair 2, whose A/A observation was unusable (relay lateness 7.011 ms forward and 6.996 ms reverse, against the 5 ms limit) and contaminated; and DATAGRAM pair 3, whose candidate and Reno observations were contaminated.

The health lines cover the 30 selected observations:

- 12 pair-workload attempts, 10 with a usable and uncontaminated triple, 2 reruns; 30 observations selected.
- Relay: largest arrival lag 3.004 ms; largest lateness 3.895 ms (limit 5 ms).
- Competitor pause observed at 240.000–240.017 s and 270.000–270.001 s (tolerance 0.5 s).
- Largest endpoint CPU load: 0.020 of its allotted cores (limit 0.80).
- Per-period control assignments: exact in every observation; host UDP error deltas: [0].

### Classification (#777's D2, unchanged)

| Workload | Period | Outcome | Pair ratios (pairs 1–5) | A/A ratios |
| --- | --- | --- | --- | --- |
| STREAM | equal | **adverse** | 0.161 0.163 0.148 0.190 0.152 | 1.025 0.975 1.000 1.194 0.852 |
| STREAM | short | **adverse** | 0.329 0.301 0.297 0.368 0.286 | 1.151 1.132 0.940 1.047 0.902 |
| STREAM | resumed | **adverse** | 0.168 0.137 0.181 0.211 0.177 | 1.187 1.015 0.962 1.064 0.866 |
| DATAGRAM | equal | **adverse** | 0.187 0.196 0.172 0.130 0.136 | 0.965 1.038 0.866 0.802 0.930 |
| DATAGRAM | short | **adverse** | 0.259 0.391 0.309 0.264 0.297 | 0.993 1.126 0.885 0.958 0.973 |
| DATAGRAM | resumed | **adverse** | 0.158 0.207 0.157 0.191 0.114 | 1.302 1.112 0.979 1.068 0.844 |

Delivery per period, medians across pairs (Mbit/s):

| Workload | Period | Competitor next to Reno | Competitor next to BBRv3 | Competitor, A/A | Focal Reno | Focal BBRv3 | Combined, BBRv3 arm |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| STREAM | equal | 52.9 | 8.6 | 54.1 | 43.0 | 84.8 | 93.4 |
| STREAM | short | 58.4 | 19.2 | 60.8 | 37.2 | 76.0 | 95.1 |
| STREAM | back | 48.4 | 7.5 | 49.0 | 47.2 | 85.7 | 93.1 |
| STREAM | absent | 0.2 | 0.0 | 0.2 | 94.9 | 93.4 | 93.4 |
| STREAM | resumed | 43.7 | 8.3 | 44.2 | 51.9 | 85.3 | 93.4 |
| DATAGRAM | equal | 49.9 | 8.6 | 45.6 | 45.3 | 83.7 | 92.2 |
| DATAGRAM | short | 61.6 | 17.5 | 61.1 | 33.6 | 76.5 | 94.0 |
| DATAGRAM | back | 45.3 | 7.2 | 49.2 | 49.7 | 84.8 | 92.0 |
| DATAGRAM | absent | 0.2 | 0.0 | 0.2 | 94.2 | 92.2 | 92.2 |
| DATAGRAM | resumed | 41.5 | 6.5 | 44.0 | 53.5 | 85.8 | 92.3 |

### #800: STREAM displacement and marking

| Workload | Period | Competitor next to BBRv3 (#808) | (#777) | (#792, plain) | (#792, traced) | Next to Reno (#808) | (#777) | Pair ratios #808 | Pair ratios #777 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| STREAM | equal | 8.6 | 7.6 | 7.4 | 7.2 | 52.9 | 50.6 | 0.161 0.163 0.148 0.190 0.152 | 0.091 0.139 0.162 0.182 0.159 |
| STREAM | short | 19.2 | 11.9 | 10.6 | 10.8 | 58.4 | 59.9 | 0.329 0.301 0.297 0.368 0.286 | 0.142 0.189 0.198 0.211 0.217 |
| STREAM | resumed | 8.3 | 6.7 | 5.9 | 5.9 | 43.7 | 45.2 | 0.168 0.137 0.181 0.211 0.177 | 0.108 0.130 0.149 0.131 0.163 |
| DATAGRAM | equal | 8.6 | 8.3 | 8.4 | 8.5 | 49.9 | 45.5 | 0.187 0.196 0.172 0.130 0.136 | 0.169 0.201 0.178 0.194 0.173 |
| DATAGRAM | short | 17.5 | 17.8 | 17.3 | 16.8 | 61.6 | 55.5 | 0.259 0.391 0.309 0.264 0.297 | 0.347 0.339 0.325 0.334 0.281 |
| DATAGRAM | resumed | 6.5 | 7.1 | 6.4 | 6.0 | 41.5 | 45.2 | 0.158 0.207 0.157 0.191 0.114 | 0.119 0.184 0.128 0.219 0.208 |

| Workload | Period | Traces | Checks on application-limited packets #808 (#792) | On any supply-limited packet #808 (#792) | Crossings, limited #808 (#792) | `inflight_longterm` / path #808 (#792) | `max_bw` cycle advances #808 (#792) | Competitor drops in Up #808 (#792) | Clamp events #808 |
| --- | --- | ---: | --- | --- | --- | --- | --- | --- | ---: |
| STREAM | equal | 3 | 0% (100%) | 0% (100%) | 57, 0 (60, 60) | 0.96 (1.04) | 19 19 19 (0 0 0) | 98% (97%) | 0 |
| STREAM | short | 3 | 0% (100%) | 0% (100%) | 59, 0 (63, 63) | 0.91 (1.05) | 19 19 19 (0 0 0) | 98% (65%) | 0 |
| STREAM | resumed | 3 | 0% (100%) | 0% (100%) | 18, 0 (19, 19) | 0.98 (1.07) | 6 6 6 (0 0 0) | 97% (93%) | 0 |
| DATAGRAM | equal | 3 | 0% (0%) | 0% (0%) | 57, 0 (57, 0) | 0.96 (0.96) | 19 19 19 (19 19 19) | 98% (99%) | 0 |
| DATAGRAM | short | 3 | 0% (0%) | 0% (0%) | 59, 0 (59, 0) | 0.90 (0.90) | 19 19 19 (18 19 19) | 98% (97%) | 0 |
| DATAGRAM | resumed | 3 | 0% (0%) | 0% (0%) | 19, 0 (19, 0) | 0.97 (0.98) | 7 6 6 (7 6 6) | 98% (98%) | 0 |

- **Marking.** In #792, every two-percent check in bulk STREAM judged a packet marked application-limited, so no crossing could lower `inflight_longterm` and the `max_bw` filter never advanced a cycle. With #800's fix (#806), bulk STREAM is marked with no supply limitation at all, crossings are unlimited, the filter cycles, and `inflight_longterm` sits below the path. That signature is like DATAGRAM's, whose figures are close to #792's (for example, short-period cycle advances 19 19 19 against 18 19 19). No clamp event occurred (C4).
- **Displacement.** STREAM's competitor gets more than in #777 in every classified period (equal 8.6 against 7.6 Mbit/s, short 19.2 against 11.9, resumed 8.3 against 6.7), so STREAM's pair ratios rose to about DATAGRAM's, still far below 0.70. #792 attributed DATAGRAM's displacement on this path to draft-06's own behavior; this record does not repeat that attribution for either workload.

### #803: ECN

| Observation | Focal forward Not-ECT | Share | Switch estimate (above 0.01% only) | Trace ECN failure | Competitor Not-ECT |
| --- | ---: | ---: | --- | --- | ---: |
| datagram/p1/cand/main | 4 of 3096636 | 0.0001% | none | – | 3 |
| datagram/p1/trace/main | 4 of 3106652 | 0.0001% | none | none | 3 |
| datagram/p2/cand/main | 4 of 3109159 | 0.0001% | none | – | 3 |
| datagram/p2/cand/rerun | 4 of 3067117 | 0.0001% | none | – | 3 |
| datagram/p2/trace/main | 4 of 3092922 | 0.0001% | none | none | 3 |
| datagram/p2/trace/rerun | 4 of 3093898 | 0.0001% | none | none | 3 |
| datagram/p3/cand/main (contaminated) | 4 of 3083153 | 0.0001% | none | – | 3 |
| datagram/p3/cand/rerun | 4 of 3105103 | 0.0001% | none | – | 3 |
| datagram/p3/trace/main | 4 of 3099139 | 0.0001% | none | none | 3 |
| datagram/p3/trace/rerun | 4 of 3109647 | 0.0001% | none | none | 3 |
| datagram/p4/cand/main | 4 of 3106964 | 0.0001% | none | – | 3 |
| datagram/p5/cand/main | 4 of 3108315 | 0.0001% | none | – | 3 |
| stream/p1/cand/main | 4 of 2685348 | 0.0001% | none | – | 3 |
| stream/p1/trace/main | 4 of 2734048 | 0.0001% | none | none | 3 |
| stream/p2/cand/main | 4 of 2705259 | 0.0001% | none | – | 3 |
| stream/p2/trace/main | 4 of 2726646 | 0.0001% | none | none | 3 |
| stream/p3/cand/main | 4 of 2694180 | 0.0001% | none | – | 3 |
| stream/p3/trace/main | 4 of 2716964 | 0.0001% | none | none | 3 |
| stream/p4/cand/main | 4 of 2692039 | 0.0001% | none | – | 3 |
| stream/p5/cand/main | 4 of 2693186 | 0.0001% | none | – | 4 |

**ECN stays on: yes.**

| Workload | Arm | Focal forward packets sent Not-ECT, per pair | Competitor Not-ECT, largest |
| --- | --- | --- | ---: |
| STREAM | Reno | 0% 0% 0% 0% 0% | 0.000% |
| STREAM | BBRv3 | 0% 0% 0% 0% 0% | 0.001% |
| STREAM | Reno (A/A) | 0% 0% 0% 0% 0% | 0.000% |
| DATAGRAM | Reno | 0% 0% 0% 0% 0% | 0.000% |
| DATAGRAM | BBRv3 | 0% 0% 0% 0% 0% | 0.001% |
| DATAGRAM | Reno (A/A) | 0% 0% 0% 0% 0% | 0.000% |

The table covers every counted and rerun observation. The two smoke observations of each attempt read the same (4 Not-ECT focal packets each; no ECN failure in either smoke trace); smoke enters no statistic.

### Trace checks

- Traces: 6, complete: yes.
- Draft-06 checker mismatches in the measured window: 1.
- Input check: every check ok.
- Competitor next to the trace ÷ next to the plain candidate, same pair: datagram/p1 0.99 1.02 1.23; datagram/p2 0.90 0.90 0.85; datagram/p3 1.00 0.90 0.84; stream/p1 1.01 0.86 0.76; stream/p2 1.01 0.88 1.01; stream/p3 1.02 0.93 0.87.

- **The one mismatch in the measured window** is C8, "`max_bw` dropped below a sample of the current or previous cycle", in the selected DATAGRAM pair 3 trace (`rerun-L6-datagram-p3-trace`, event 473470, short period): at a cycle advance BBRv3 aged `max_bw` to 11,292,921 bytes/s, while the previous cycle's largest sample was 11,335,321 (0.37% higher). Two more traces show the same pattern outside the selected measured windows: STREAM pair 3 in its warm-up (event 68403, 0.03%), and DATAGRAM pair 2's first-pass trace, which its rerun replaced (event 255116, 0.03%).
- **Where it comes from.** In each case the round holding that sample starts in Up and enters ProbeRTT partway through, and the sample is below `max_bw`. The port marks every sample it processes in ProbeRTT as limited (`Feedback` sets `SendProbeRTTLimited` while `InProbeRTT()`), and a limited sample below `max_bw` never enters its filter. That is a declared port choice ([docs/designs/bbrv3.md](../../designs/bbrv3.md): "Mark ProbeRTT samples limited intentionally"). Draft-06 marks the connection application-limited on entering ProbeRTT, which marks packets sent afterwards (sections 4.1.2 and 5.5.5); a sample for a packet sent before entry, acknowledged during ProbeRTT, is not limited and does enter the filter. The trace records each round's largest sample but not which event carried it, so the record cannot show that event directly. A sample taken before ProbeRTT entry would have entered the port's filter too and kept `max_bw` at least as high, so these samples were most probably taken after entry. The checker reads draft-06 and is right to flag this.
- **Classification.** These are **port-versus-draft deviations from a declared port choice**, not checker misreadings, so #792's dismissal rule does not dismiss them; the trace does not conform at those events. The effect is a `max_bw` 0.03–0.37% lower for at most one cycle. It changes no classification and no discharge, and draft-06's own ProbeRTT behavior is not what #808 measures. Whether to keep the port's choice is a question for a later BBR change, not for this record.

### Other L6 flags (descriptive, #777's rules)

| Workload | Period | Competitor p95, next to Reno (ms) | Next to BBRv3 (ms) | Ratio, median | Pairs above 1.20 | Flag |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| STREAM | equal | 198.3 | 200.2 | 1.008 | 0 of 5 | not raised |
| STREAM | short | 122.4 | 123.9 | 1.014 | 0 of 5 | not raised |
| STREAM | back | 193.3 | 202.1 | 1.037 | 0 of 5 | not raised |
| STREAM | absent | 119.3 | 192.2 | 1.616 | 5 of 5 | raised |
| STREAM | resumed | 186.3 | 204.1 | 1.150 | 1 of 5 | not raised |
| DATAGRAM | equal | 198.6 | 188.8 | 0.951 | 0 of 5 | not raised |
| DATAGRAM | short | 123.2 | 125.4 | 1.020 | 0 of 5 | not raised |
| DATAGRAM | back | 191.9 | 211.9 | 1.100 | 1 of 5 | not raised |
| DATAGRAM | absent | 123.9 | 191.9 | 1.573 | 5 of 5 | raised |
| DATAGRAM | resumed | 186.9 | 203.8 | 1.085 | 0 of 5 | not raised |

| Workload | Measure | Matched Reno | BBRv3 | Ratio, median (limit) | Pairs crossing | A/A ratios | Flag |
| --- | --- | ---: | ---: | ---: | ---: | --- | --- |
| STREAM | control p95 | 197.69 ms | 199.11 ms | 1.005 (1.20) | 0 of 5 | 1.002 1.000 1.001 1.001 1.000 | not raised |
| STREAM | sender CPU per useful GiB | 15.85 s | 8.97 s | 0.564 (1.10) | 0 of 5 | 1.230 1.062 0.939 1.070 0.879 | not raised |
| STREAM | receiver CPU per useful GiB | 13.08 s | 9.07 s | 0.689 (1.10) | 0 of 5 | 1.280 1.068 0.937 1.085 0.854 | not raised |
| STREAM | sender peak RSS | 16.02 MiB | 23.81 MiB | 1.517 (1.10) | 5 of 5 | 0.944 0.985 0.996 0.969 1.067 | raised |
| STREAM | receiver peak RSS | 14.84 MiB | 26.53 MiB | 1.787 (1.10) | 5 of 5 | 0.971 0.978 1.017 0.979 1.057 | raised |
| DATAGRAM | control p95 | 198.08 ms | 198.59 ms | 1.002 (1.20) | 0 of 5 | 1.004 0.999 0.996 0.996 0.997 | not raised |
| DATAGRAM | sender CPU per useful GiB | 16.07 s | 9.93 s | 0.612 (1.10) | 0 of 5 | 1.096 1.060 0.950 0.935 0.953 | not raised |
| DATAGRAM | receiver CPU per useful GiB | 13.10 s | 8.90 s | 0.673 (1.10) | 0 of 5 | 1.101 1.045 0.948 0.909 0.943 | not raised |
| DATAGRAM | sender peak RSS | 16.12 MiB | 22.96 MiB | 1.424 (1.10) | 5 of 5 | 0.957 0.987 0.967 0.986 0.991 | raised |
| DATAGRAM | receiver peak RSS | 14.69 MiB | 16.91 MiB | 1.152 (1.10) | 5 of 5 | 0.958 1.005 0.980 0.975 1.006 | raised |

| Workload | Period | Arm | Focal drops | Competitor drops | Focal mean queue (KB) | Competitor mean queue (KB) |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| STREAM | equal | Reno | 5 | 2 | 425 | 520 |
| STREAM | equal | BBRv3 | 1175 | 285 | 432 | 38 |
| STREAM | short | Reno | 7 | 6 | 392 | 613 |
| STREAM | short | BBRv3 | 1005 | 1061 | 679 | 148 |
| STREAM | resumed | Reno | 154 | 11 | 473 | 401 |
| STREAM | resumed | BBRv3 | 456 | 91 | 493 | 42 |
| DATAGRAM | equal | Reno | 4 | 2 | 480 | 524 |
| DATAGRAM | equal | BBRv3 | 1353 | 302 | 433 | 38 |
| DATAGRAM | short | Reno | 12 | 7 | 361 | 651 |
| DATAGRAM | short | BBRv3 | 1395 | 1006 | 677 | 132 |
| DATAGRAM | resumed | Reno | 167 | 2 | 491 | 372 |
| DATAGRAM | resumed | BBRv3 | 688 | 104 | 473 | 35 |

### DATAGRAM loss (campaign decision D8)

| Observation | Sent | Lost | Loss | Relay forward drops | Lost minus relay drops |
| --- | ---: | ---: | ---: | ---: | ---: |
| datagram/p1/aa | 1762627 | 830 | 0.0471% | 830 | 0 |
| datagram/p1/cand | 3085940 | 5848 | 0.1895% | 5853 | -5 |
| datagram/p1/reno | 1906305 | 738 | 0.0387% | 738 | 0 |
| datagram/p1/trace | 3096028 | 5886 | 0.1901% | 5891 | -5 |
| datagram/p2/aa | 1913285 | 874 | 0.0457% | 874 | 0 |
| datagram/p2/cand | 3056749 | 4944 | 0.1617% | 4945 | -1 |
| datagram/p2/reno | 2043079 | 868 | 0.0425% | 868 | 0 |
| datagram/p2/trace | 3084049 | 6100 | 0.1978% | 6103 | -3 |
| datagram/p3/aa | 2029205 | 606 | 0.0299% | 606 | 0 |
| datagram/p3/cand | 3095084 | 5729 | 0.1851% | 5730 | -1 |
| datagram/p3/reno | 1900980 | 846 | 0.0445% | 846 | 0 |
| datagram/p3/trace | 3098316 | 5583 | 0.1802% | 5584 | -1 |
| datagram/p4/aa | 1784118 | 752 | 0.0421% | 752 | 0 |
| datagram/p4/cand | 3097145 | 5633 | 0.1819% | 5634 | -1 |
| datagram/p4/reno | 1679143 | 689 | 0.0410% | 689 | 0 |
| datagram/p5/aa | 1756997 | 729 | 0.0415% | 729 | 0 |
| datagram/p5/cand | 3099047 | 5821 | 0.1878% | 5827 | -6 |
| datagram/p5/reno | 1672547 | 637 | 0.0381% | 637 | 0 |

BBRv3's focal DATAGRAM loss is 0.16–0.20% against Reno's 0.03–0.05%. In every observation the lost messages are at most relay2's forward drops for the focal flow, and host UDP error deltas are zero. That is consistent with all of the loss being relay drops, but it does not prove it: relay drops of packets that carried no DATAGRAM can offset losses elsewhere, and this frozen fixture does not read `DatagramReceiveDropsV1`. The [side stage](../2026-10-19-bbr-808-gso-receive/README.md) reports that counter; it is zero by median there.

### Deviations

1. **Attempt 1's prerequisite gap and the restart.** The stage ended at smoke as registered; the operator restarted it, and attempt 2 ran from a clean start. Attempt 1's files are kept, and none enters any statistic.
2. **Start times.** The operator moved the start twice before attempt 1 (from 22:00 EDT to 03:30 and then 05:00 EDT on October 20) and asked for attempt 2 to start at 02:13 EDT on October 21.

### Review

PR review (RAS run `20261021T113227-f5e0a7c774e2ff1a0f75f935`) found that the first write-up dismissed the C8 mismatches as a checker misreading after changing the checker. The change counted the port's ACK-time ProbeRTT marking as if it were draft-06's send-time marking, and it would also have hidden a genuine deviation in a round whose largest sample was eligible. The change was reverted; the checker is #792's, byte-identical, and the mismatches are reported above.

| Finding | Disposition | Change |
| --- | --- | --- |
| C-006 (high): the dismissal rested on the port's ACK-time ProbeRTT marking, not draft-06's | Fixed | Checker change reverted; the three C8 mismatches reported as deviations from a declared port choice ([Trace checks](#trace-checks)); Answer and campaign record aligned |
| C-005 (high): the changed checker would hide an eligible sample in a mixed round | Fixed by the revert | #792's checker keeps the round maximum as a floor |
| C-020 (medium): one test supported three dismissals | Moot | No dismissal is claimed |
| C-004 (low): trace inventory | Moot | The rerun inventory was part of the reverted disclosure |
| C-014 (medium): direction of the displacement change | Fixed | The competitor's delivery rose (displacement eased); the pair-ratio threshold is named |
| C-003 (low): DATAGRAM figures called unchanged | Fixed | "Close to #792's", with an example |
| C-002 (low): causal claim about draft-06 behavior | Fixed | The campaign record no longer repeats #792's attribution |
| C-001 (medium): loss called all relay drops | Fixed | Stated as consistent with relay drops, with the limits |
| C-019 (medium): the replaced observation's relay-lateness failure | Fixed | Both rerun reasons named; health lines scoped to the selected observations |
| C-008 (low): the side stage's 1.76× aggregation | Fixed | Labeled as the median of paired ratios beside the arm medians |
| C-011, C-015 (low) | Rejected as registered by the synthesis; C-015's smoke ECN reading added for completeness | — |
