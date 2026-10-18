# BBRv3 L6 diagnosis: why the production port displaces a competing Reno flow

**Date:** October 18, 2026, America/New_York. **Scope:** [#792](https://github.com/the-sarge/quic-go-fast/issues/792), the diagnosis the operator chose after [#777](https://github.com/the-sarge/quic-go-fast/issues/777)'s adverse L6 result ([#777's record](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md)). Measurement only: no product code change, adoption, default-controller change, release, CHANGELOG change, paid resource, host-setting change or ledger change.

Branch `codex/bbr-l6-diagnosis`, based on `9b5c91f2` (the tip of #777's record branch, `71e88789` plus one disclosure line in #777's README), so #777's aids are in this tree at their recorded paths. As for every [#666](https://github.com/the-sarge/quic-go-fast/issues/666), [#748](https://github.com/the-sarge/quic-go-fast/issues/748) and #777 record, this record stays off `main`; a dev-journal entry on `main` points to it.

**Status: registration, under review.** Nothing below has been run as a counted observation. The [development runs](#development-runs-before-this-registration) are disclosed with what was seen.

## Question

Why does BBRv3 at `abfa5456` displace the Reno flow on #777's L6 path? Which of these is the cause (#792):

- **(a)** draft-06 BBRv3 behavior on a one-BDP drop-tail queue;
- **(b)** a property of the port: a deviation from draft-06 or from r9, or a defect;
- **(c)** an interaction with the fixture.

## Binding requirements (#792)

- **Measurement only.** This ticket changes no product code. A fix is a separate ticket, and any fixed revision needs its own readiness measurement, advancement disposition and a fresh L6 run.
- **Reuse #777's aids unchanged where possible:** `relay2`, the runner, the build pins and the calibration. Any added instrument is a verification aid, and its perturbation must be reported.
- **Evidence for the cause class.** The record names the class (a, b or c) with evidence: state traces compared with draft-06's expected transitions, or a demonstrated deviation in code. Otherwise it ends *undiagnosed* and says what was ruled out.
- **#777's classification stands.** Nothing here reclassifies it.
- **Revision.** `abfa5456`, unless the operator picks another revision with its own readiness.

## Accepted decisions (before any data)

The agent presented one plan with six recommended decisions on October 17, 2026, at about 22:40 America/New_York. The operator answered "Approve the plan with all six recommended decisions; start now."

- **D1, record shape.** This record branch, off `main`, with a pointer entry on `main`. No product pull request.
- **D2, counted runs.** Three pairs × two workloads (STREAM, DATAGRAM) × two arms: `trace` (the port with this record's measurement-only overlay) and `cand` (the plain port, byte-identical to #777's). Twelve observations, about 1 h 35 min on `minimax`. The `cand` arm measures the overlay's disturbance in the same session.
- **D3, the disturbance rule.** The traces count only if every `trace` observation is itself adverse: in each classified period the competitor gets less than 0.70 × its median delivery next to matched Reno in #777 (same workload and period). Otherwise the record ends *undiagnosed (instrument)*. No goodput floor applies.
- **D4, reviews.** One `ras consider` of this registration before any counted observation (at most one replacement consideration after fixes), the operator's acceptance before the counted stage, then one `ras consider` of the result's cause argument (no replacement).
- **D5, the cause rules** are fixed in this registration, before any counted observation ([Classification](#classification-fixed-before-counted-data)). Mixed or missing evidence ends *undiagnosed*, with what was ruled out.
- **D6, the revision:** `main` at `abfa5456d99a9299825d04454a2e7ed12dff3326`, the revision #777 measured. `main` has since gained non-BBR production changes (#779, #787, #790); none is measured here.

## Known before these decisions

- **#777's result** (adverse; [Answer](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#answer)): next to BBRv3 the competitor delivers 6.2–17.8 Mbit/s, against 42.7–59.9 next to matched Reno. BBRv3 holds most of the shared queue and causes nearly all overflow drops. While the competitor pauses, BBRv3 alone keeps the queue near full. BBRv3's ECN switched off mid-run in every run (#793 owns that).
- **The agent's expectation** (stated to the operator in the plan, not a result): (a) is the most likely class. At the points read, the port follows draft-06's loss rules, and Reno at a 100 ms round trip needs very long loss-free periods to hold a large share.
- **Draft-06 states its own coexistence target** ([section 5.3.3.8.1](https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html#section-5.3.3.8.1)): it aims to let Reno/CUBIC keep working where they work today, at bandwidth-delay products of about 100 packets or fewer, and says that BBR flows with large BDPs deliberately probe more often than Reno/CUBIC would (every 2–3 s at high round-trip times). L6's BDP is about 857 packets. This is context for reading a trace, not evidence of a cause.
- **The port's controller is r9's code.** `git diff 81e9dc8c abfa5456` has no non-test change in `internal/congestion`; in `internal/ackhandler` the only non-test change is the port contract's declared deviation 4 (the #752 discovery-scratch release in `congestion_dispatch.go`, which changes no ACK, recovery, ECN, bound or Reno behavior). The root-package merge seams (send queue depth, candidate authority, setup admission, receive phase) are outside the controller.

## Code check: draft-06 rules on the mechanism's path

Authority: [draft-ietf-ccwg-bbr-06](https://www.ietf.org/archive/id/draft-ietf-ccwg-bbr-06.html) (pinned source `6db913f5`; the archived text read here has SHA-256 `ba4f90e93d83d62c99b7bc83b9adf2b7576a13374d857ac9fc88bd8e8f76a891`), with the declared translations of [docs/designs/bbrv3.md](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/designs/bbrv3.md) at `abfa5456`. Port lines are at `abfa5456`.

| Rule | Draft-06 | Port | Verdict |
| --- | --- | --- | --- |
| Probe timing: leave Down/Cruise for Refill after a random 2–3 s, or after `min(63, BDP in packets)` rounds | 5.3.3.8.3 `IsTimeToProbeBW`, `PickProbeWait`, `IsRenoCoexistenceProbeTime` | `bbr_probe.go:48`, `:103` | Matches; round count in packet equivalents is the declared translation D11. |
| Refill: reset the short-term model, one packet-timed round, then Up with `ResetFullBW` and `is_bw_probe_sample` | 5.3.3.3, 5.3.3.9 `StartProbeBW_REFILL`, `UpdateProbeBWCyclePhase` | `bbr_probe.go:53`, `:111` | Matches. |
| Up exits: plateau, loss above 2%, precautionary deceleration; reset the plateau detector while cwnd-limited at `inflight_longterm` | 5.3.3.7, 5.3.3.9 `IsTimeToGoDown` | `bbr_probe.go:123` | Matches. |
| The 2% check per lost packet while probing, `inflight_longterm = max(inflight at loss, 0.7 × min(BDP, cwnd))`, Up → Down | 5.5.10.2 `HandleLostPacket`, `IsInflightTooHigh`, `HandleInflightTooHigh`, `InflightAtLoss` | `bbr_probe.go:156` | Matches, with one arithmetic difference: when the losses before a packet already exceed 2%, the draft's prefix is negative and the port clamps it to zero (`:175`), so the port's `inflight_longterm` can be higher by that negative prefix. Unsigned arithmetic, not a declared translation. The traces measure how often and by how much. |
| Short-term bounds outside probing: once per loss round with loss, `bw_shortterm = max(bw_latest, 0.7 × bw_shortterm)`, the same for `inflight_shortterm` | 5.5.10.3 `AdaptLowerBoundsFromCongestion`, `InitLowerBounds`, `LossLowerBounds` | `bbr_sender.go:219`–`:251` | Matches; 0.7 × is an integer floor. |
| No cwnd cut on entering fast recovery; save on entry, restore on exit through the model bounds | 5.6.4.4, 5.5.11 | `bbr_recovery.go:21`, `:31` | Matches. (Draft-06 has no packet-conservation step; PTO handling is the declared translation D07.) |
| cwnd: `min(cwnd + acked, max_inflight)`, `max_inflight = quantized(cwnd_gain × BDP + extra_acked)`, then the volume caps per phase | 5.6.4.2, 5.6.4.6, 5.6.4.7 | `bbr_sender.go:296`, `:322`, `:401` | Matches. |
| Pacing: gain × min(max_bw, bw_shortterm), gains 2.77, 0.5, 0.9, 1, 1, 1.25, 1; 1% margin | 5.6.1, 5.6.2 | `bbr_sender.go:362` | Matches; the margin is applied in emission (design, Pacing). |
| max_bw: largest sample of the current and previous ProbeBW cycle; the cycle advances at the end of the probe's feedback | 5.5.3–5.5.5, 5.3.3.9 `AdaptLongTermModel` | `bbr_probe.go:23`, `:66` | Matches. |
| ProbeRTT: enter when the 5 s filter expires, hold 200 ms and one round, exit to ProbeBW (Cruise) or Startup with the short-term model reset | 5.3.4.3, 5.3.4.4 | `bbr_probe_rtt.go:14`, `:29`, `:80` | Matches, with the declared translation D02 (pre-expiration cap). |

**Not checked here:** delivery-rate sampling (section 4.1, owned by `internal/ackhandler`'s sampler), `extra_acked` (5.5.9) beyond its use in the cwnd bound, and the offload budget (5.5.8). The [input check](#input-check-the-senders-view-against-the-relays) tests the sampler's output against the relay instead.

## The instrument: a measurement-only trace overlay

[trace/bbr_trace.go](trace/bbr_trace.go) is a verification aid in package `congestion`, built only into the `cand-trace` fixture variant. It records rows of the BBR model's state (48 integer columns, named in the file) into a fixed array outside the Go heap (65,536 rows), and writes them once as JSON, from the connection goroutine, at its first feedback event at or after `BBR_TRACE_UNTIL_UNIX_NS`. It records nothing after that. It is enabled only in a sender-role process with both environment variables set, and only for the first BBR sender that receives feedback; the runner sets them only for the `trace` arm's focal sender, with the end time 2 s after the measured window.

Rows:
- one per packet-timed round, with that round's lost bytes and packets, largest prior flight, largest rate sample, feedback events, limited events and two-percent checks;
- a pre and a post row around every feedback event that changes the phase, a short-term bound, `inflight_longterm` (a decrease, or a change to or from unbounded), the recovery episode, the CE state or the ProbeRTT hold, or that first reports ECN failure (`max_bw` decreases too);
- a pair at every loss-round close that had loss (before and after the bounds update);
- one per two-percent check, with its inputs, before its outcome is applied;
- a pre/post pair for any such change made between feedback events (send-side idle restart), and one per model reset.

[build.py](build.py) applies seven hooks, each a text that must occur exactly once, all in BBR-only controller code: `Feedback` entry and exit, the round start, both sides of the loss-round close, `Reset`, and `probeLoss` (entry and the check). Reno, emission and recovery code are untouched.

Tests ([trace/bbr_trace_test.go](trace/bbr_trace_test.go), run in the exported `cand-trace` tree with the port's own `internal/congestion` tests, which also pass): configuration, nothing recorded when disabled, a probe cycle and its loss check, loss-round rows, changes between events, a second sender ignored, overflow counted, the reset row, and the single dump. Two injected faults (the check hook removed; phase changes ignored) each failed the tests; the source was restored. `go vet` passes.

**Disturbance.** The overlay adds work on the connection goroutine per feedback event (two struct copies and a few comparisons) and a JSON write after the measured window. Its effect on the measured flows is measured, not assumed: the `cand` arm runs beside it in every pair, and D3 decides whether the traces are representative.

## The checker: traces against draft-06

[draft06check.py](draft06check.py) recomputes, from each trace's logged inputs, what draft-06 says the state should become, and lists every mismatch with its event and measured period. It never imports or restates the port's code; where the port's arithmetic is a declared translation, the rule applies the translation.

| Rule | What it checks | Draft-06 |
| --- | --- | --- |
| C0 | Startup exits on plateau, loss or CE; Drain exits at `inflight <= Inflight(1.0)` or after three rounds | 5.3.1.2, 5.3.1.3, 5.3.2 |
| C1 | Refill only after the wall-clock or Reno-round trigger (or an undo, or precautionary acceleration), never under a CE response; no Down/Cruise round past a trigger without a Refill | 5.3.3.8.3, 5.3.3.9 |
| C2 | Refill resets the short-term model, lasts one round, and enters Up with the plateau detector reset and probe sampling on | 5.3.3.3, 5.3.3.9 |
| C3 | Up ends only on loss, precautionary deceleration, plateau (not while cwnd-limited at `inflight_longterm`), CE or ProbeRTT | 5.3.3.7 |
| C4 | Each two-percent crossing sets `prev_probe_too_high`, ends Up, and sets `inflight_longterm` as the draft computes it (the port's clamp counted separately); every lowering of `inflight_longterm` has such a cause | 5.5.10.2 |
| C5 | Each loss-round close with loss outside probing sets both short-term bounds to `max(latest, 0.7 × bound)`; no other change except resets at Refill, ProbeRTT exit or reset, and undo raises | 5.5.10.3, 5.5.11 |
| C6 | cwnd never above `max(4 M, min(caps, quantized(gain × BDP + extra_acked)))` | 5.6.4.2–5.6.4.7 |
| C7 | Pacing rate equals the phase gain × `min(max_bw, bw_shortterm)` (at or below under CE) | 5.6.1, 5.6.2 |
| C8 | `max_bw` never above every sample of the current and previous cycle after its event's own sample, and never below one when it ages | 5.5.3–5.5.5 |
| C9 | ProbeRTT starts only when the 5 s filter has expired, holds more than 200 ms, exits to Cruise or Startup with the short-term model reset, and is never overdue by more than 0.5 s | 5.3.4.3, 5.3.4.4 |

**Instrument granularity.** Rows show an event's state before and after it, and a few points inside it. The port decides some transitions mid-event, after the event's own rate sample and loss-round close and before its RTT and window updates. The checker uses the recorded mid-event values where it needs them (the BDP after the loss-round close, the pre-update window, `inflight_longterm` after the upward adjustment), and a filter value that only changes at the next sample after a cycle advance (as in the draft). An event that passes through Refill into ProbeRTT is read as a Refill.

**Tests** ([draft06check_test.py](draft06check_test.py), 24):
- **Fixtures.** Two traces of the port itself pass every rule with no mismatch. They were written by [trace/bbr_trace_test.go](trace/bbr_trace_test.go) in the exported tree, driving the real sender through synthetic paths with the port's own test driver ([testdata/](testdata/)). Between them they exercise:
  - both probe triggers;
  - loss, plateau and ProbeRTT exits from Up;
  - 114 two-percent crossings, binding short-term bounds and filter aging;
  - ProbeRTT entries and exits.
- **Injected deviations.** Each of twenty is caught by its rule:
  - an early Refill, and Refills missed at either trigger;
  - a short-term model kept at Refill, and a Refill longer than a round;
  - an Up exit with no condition;
  - a wrong `inflight_longterm`, a missing `prev_probe_too_high`, a crossing that stays in Up, and an unexplained decrease;
  - a draft crossing that the port skips;
  - a wrong short-term bound, and one changed without a loss round;
  - a window above its bound;
  - a wrong pacing gain;
  - a filter above its samples, and one aged below a sample;
  - a short ProbeRTT hold, and an entry without expiry.
- **Arithmetic and completeness.** The clamp arithmetic is checked, and a trace with overflow is marked incomplete.
- **Development history.** While the checker was developed on the first fixture, its first versions flagged 19, then 35, then 11 events that were checker errors, each traced to the row data and fixed. In order: a rule that forgot `max(latest, 0.7 × bound)` can raise a bound; four rules that read end-of-event values where the port decides mid-event; and a lateness allowance replaced by the first-event rule. The fixtures were regenerated with a driver whose flight follows the window, after the first one stayed in ProbeRTT because its synthetic flight never drained.

## Input check: the sender's view against the relay's

[reconcile.py](reconcile.py) compares, per measured period, what the traced sender recorded with what relay2 recorded for the same flow. Tolerances are fixed in the file, before any counted observation:

| Check | Sender | Relay | Tolerance |
| --- | --- | --- | --- |
| R1 | Lost packets it declared (shifted 250 ms) | Forward drops of that flow | within max(2%, 15 packets) |
| R2 | Bytes it saw delivered (shifted 75 ms) | Forward delivered bytes of that flow, in QUIC bytes | ratio in [0.98, 1.02] |
| R3 | `max_bw` | The link's capacity in QUIC bytes | at most 1.05 × |
| R4 | `min_rtt` | The model's 100 ms propagation, plus up to about 100 ms of queue | in [99, 210] ms |

On the L6 path the relay's FIFO never reorders a flow, so every declared loss should be a relay drop and every drop a declared loss. A result outside a tolerance is a finding to explain, not a verdict by itself. Seven tests ([reconcile_test.py](reconcile_test.py)) cover an agreeing synthetic observation and each check failing.

## Mechanism figures (descriptive)

For each traced observation and period, [analyze.py](analyze.py) reports, without thresholds:
- the share of time in each phase;
- what started each probe (wall clock or rounds) and the interval between probes;
- why each Up ended;
- BBRv3's own loss per packet-timed round, and the share of rounds above 2%;
- which bound set the window ceiling (the model target, `inflight_shortterm`, `inflight_longterm` or the headroom) and how often a round was cwnd-limited;
- how often `bw_shortterm` was below `max_bw`;
- `inflight_longterm` and the window against the path's capacity (queue plus pipe);
- `max_bw` against BBRv3's own delivery;
- where in BBRv3's phases each flow's drops fall, and each flow's queue occupancy by BBRv3 phase;
- the time of the ECN switch, with the competitor's delivery in the 30 s before and after.

## Arms, pairs, order and host

| Arm | Focal fixture | Focal controller | Binary (SHA-256) |
| --- | --- | --- | --- |
| `trace` | `abfa5456` + this record's overlay | BBRv3 | `cand-trace-fixture` `57d794d4…` (built on `minimax` and rebuilt identically) |
| `cand` | `abfa5456` | BBRv3 | `cand-fixture` `d3f05ad6…`, byte-identical to #777's |

The competitor is #777's: frozen Reno `e4f322cb` (`reno-fixture` `cda8feb1…`), a reliable stream, Q1's accepted bulk pause. relay2 (`bff3ba4f…`), calibrate2 (`f4773dc9…`) and the launcher (`a6146d62…`) are #777's binaries, rebuilt byte-identically from #777's sources, which [build.py](build.py) checks against `71e88789` before every export. The path, endpoint contract, timing, periods, core layout and host are #777's ([Path](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#path-the-frozen-q1-models-l6), [Endpoints and timing](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#endpoints-and-timing), [Host](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#host)).

Three pairs; each pair runs both workloads, STREAM first in odd pairs, after one quiet-host gate per pair-workload. Within each, the two arms run in a seeded random order (`random.Random('792-<pair>-<workload>')`, [matrix.py](matrix.py) `order`):

| Pair | First workload, arm order | Second workload, arm order |
| --- | --- | --- |
| 1 | STREAM: trace, cand | DATAGRAM: cand, trace |
| 2 | DATAGRAM: trace, cand | STREAM: cand, trace |
| 3 | STREAM: trace, cand | DATAGRAM: cand, trace |

Seeds 7921–7923 by pair (L6 has no random loss; the seed changes nothing).

**Runner.** [run.py](run.py) is #777's `run.py` with exactly the substitutions in [runchanges.py](runchanges.py): this record's artifact directory, the `trace` arm, and the overlay's two environment variables for that arm's focal sender. [runchanges_test.py](runchanges_test.py) checks that it is #777's file with those substitutions and nothing else. [matrix.py](matrix.py) and [driver.sh](driver.sh) are #777's, with the recorded changes listed in each file's header. #777's `l6rules.py` (usability, contamination, correctness) and `prereq.py` (path calibration) run unchanged from #777's directory.

## Usability, contamination, reruns and correctness

#777's rules, unchanged ([Observation usability and reruns](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#observation-usability-and-reruns), [Correctness gates](https://github.com/the-sarge/quic-go-fast/blob/71e88789/docs/audits/2026-10-17-bbr-l6-coexistence/README.md#correctness-gates)), with three recorded changes:
- a `trace` observation is also unusable when its trace is missing, overflowed or saw a second sender;
- a pair-workload holding an unusable or contaminated observation is rerun once, both arms, with the same seed, at most two;
- smoke is one full-length `trace` STREAM observation, which must also leave a complete trace.

While any observation runs, the file `RUNNING` exists at the worktree root on `minimax`, and `STAGE.DONE` appears when the order ends. This is the marker agreed with the wiremux sessions that share the host.

## Classification (fixed before counted data)

Evaluated over the selected counted observations, in this order:

1. **Undiagnosed (instrument)** if D3 fails for any selected `trace` observation, or fewer than two `trace` observations per workload are usable with complete traces.
2. **(b), the port** if either holds:
   - the code check above (or the counted traces) shows an undeclared deviation from draft-06 or from r9 on a rule in the mechanism's path, and the traces show it acting during the classified periods (it changes a transition, a bound or a rate that the draft would set differently);
   - the input check fails in a way the port owns (for example declared losses that are not relay drops, or a sampler rate above the link).

   A checker mismatch counts as a deviation only when a unit-level reproduction in the exported tree, driving the port's sender with the mismatching event's recorded inputs, shows the port's transition differing from draft-06's. A mismatch can be dismissed as an instrument artifact only when a test built from that event's rows shows the checker misreading it; then the checker is fixed, rerun on every trace, and the change is disclosed as post-data.
3. **(c), the fixture** if the input check fails in a way the fixture owns (the relay or model delivering signals that differ from the modeled path), or the competitor is limited by something other than its controller (CPU saturation, supply, flow control), in the classified periods.
4. **(a), draft-06 behavior** if no rule above applies: no undeclared deviation, zero unexplained checker mismatches in the measured window of every selected trace, the input check within tolerance (or each finding shown not to change the controller's inputs), and the mechanism figures showing which draft rules produce the displacement.
5. **Undiagnosed** otherwise, saying what was ruled out.

A deviation found but not acting in the classified periods does not make the class (b); it is reported separately, for the operator to decide whether to file it. More than one class can hold only through rule 2 and rule 3 together; the record then reports both and names neither alone.

**What each outcome leads to (#792):**
- **(b):** a fix ticket, then readiness and L6 again on the fixed revision.
- **(a):** the adoption question for GridCast's shared paths goes to the operator.
- **(c):** #777's validity question goes back to the operator before anything else.
- **Undiagnosed:** what was ruled out, and a decision for the operator on whether to spend more.

## Development runs (before this registration)

*To be filled from the two development observations (one STREAM, one DATAGRAM `trace` observation, made on `minimax` on October 18 before this registration's review).*

## Time estimate

All aids exist and every binary is built. On a quiet host:

| Step | Estimate |
| --- | --- |
| Host facts, binary check, path calibration | about 4 minutes |
| Smoke (one observation) | about 7 minutes |
| Counted stage (12 observations of about 6.4 minutes, six quiet-host gates of at least 1 minute) | about 1 h 23 min |
| Reruns, if triggered (up to two pair-workloads of about 14 minutes) | up to about 28 min |
| **Total** | **about 1 h 35 min without reruns; up to about 2 h** |

## Order

**On the Mac:** `build.py export` (and `compile`, or `compile` on `minimax` from the synced trees; every binary must match its pin), the Python tests (`draft06check_test.py`, `reconcile_test.py`, `runchanges_test.py`), then `sync.sh`.

**On `minimax`,** sequentially under `taskset -c 1`, with nothing else built or tested there while a stage measures ([driver.sh](driver.sh)): `matrix.py hostfacts start`, `binaries`, `prereq`, `smoke`, `main`, `follow`, `hostfacts end`. The driver first checks, path by path, that no counted, rerun or smoke observation, calibration, smoke report, follow decision, correctness-failure report, binary check or `STAGE.DONE` exists. A failed binary check, calibration, smoke or correctness gate stops it.

**Then, anywhere:** `analyze.py` writes `summary.json`; it emits no outcome unless the order completed.

## Limits

- **One declared model, one host, one revision.** Results describe the port at `abfa5456` under the frozen Q1 model's L6 on `minimax` loopback, three pairs per workload.
- **The checker covers the listed rules,** not full draft conformance; delivery-rate sampling is tested only through its output (R2, R3).
- **A conforming trace shows that the displacement follows from draft-06's rules on this path;** it does not show how another BBRv3 implementation behaves, and it is not a fairness claim about other paths.
- **Not measured:** ECN's cause (#793), L7, L8, S8, other paths and platforms.
