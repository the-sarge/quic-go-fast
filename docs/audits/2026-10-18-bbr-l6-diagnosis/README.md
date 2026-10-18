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
- **The agent's expectation** (stated to the operator in the plan, not a result): (a) is the most likely class. At the points read, the port follows draft-06's loss rules, and Reno at a 100 ms round trip needs very long loss-free periods to hold a large share. After the [development runs](#development-runs-before-this-registration) the agent still expects (a), with a STREAM-only port property reported separately.
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
| No cwnd cut on entering fast recovery; save on entry, restore on exit through the volume caps | 5.6.4.4 `RestoreCwnd`, 5.6.4.7, 5.5.11 | `bbr_recovery.go:21`, `:31` | Matches. Draft-06 has no packet-conservation step; PTO handling is the declared translation D07. The port restores after the event's window update, so `max_inflight` bounds a restored window from the next ACK; the draft leaves that order open (a one-ACK transient, seen in the development runs). |
| cwnd: `min(cwnd + acked, max_inflight)`, `max_inflight = quantized(cwnd_gain × BDP + extra_acked)`, then the volume caps per phase | 5.6.4.2, 5.6.4.6, 5.6.4.7 | `bbr_sender.go:296`, `:322`, `:401` | Matches. |
| Pacing: gain × min(max_bw, bw_shortterm), gains 2.77, 0.5, 0.9, 1, 1, 1.25, 1; 1% margin | 5.6.1, 5.6.2 | `bbr_sender.go:362` | Matches; the margin is applied in emission (design, Pacing). |
| max_bw: largest sample of the current and previous ProbeBW cycle; the cycle advances at the end of the probe's feedback | 5.5.3–5.5.5, 5.3.3.9 `AdaptLongTermModel` | `bbr_probe.go:23`, `:66` | Matches. |
| ProbeRTT: enter when the 5 s filter expires, hold 200 ms and one round, exit to ProbeBW (Cruise) or Startup with the short-term model reset | 5.3.4.3, 5.3.4.4 | `bbr_probe_rtt.go:14`, `:29`, `:80` | Matches, with the declared translation D02 (pre-expiration cap). |
| Application-limited samples: when cwnd and pacing would allow sending but there is no data, mark the next flight's samples application-limited; a limited crossing does not lower `inflight_longterm` | 4.1.1.3, 4.1.2.4 `CheckIfApplicationLimited`, `MarkConnectionAppLimited`; 5.5.10.2 | `delivery_sampling.go:32`, `:51`, `:104`, `:133`; `internal/ackhandler/delivery_sampler.go:222`, `:240`, `:274` | The rule matches: no data while cwnd and pacing permit and no retransmission is pending, and one flight is marked. **A property of the port's send path.** quic-go's `SendStream.Write` returns once less than one packet of a write remains (`send_stream.go:265`–`:312`), so within one packing burst the stream runs dry at every 16,384-byte record, and a bulk STREAM sender is marked application-limited almost continuously. The draft leaves the precise determination to the implementation (4.1.1.3). DATAGRAM senders are not marked this way. Seen in the development runs, and measured in every trace. |

**Not checked here:** delivery-rate sampling other than the application-limited marking (section 4.1, owned by `internal/ackhandler`'s sampler), `extra_acked` (5.5.9) beyond its use in the cwnd bound, and the offload budget (5.5.8). The [input check](#input-check-the-senders-view-against-the-relays) tests the sampler's output against the relay instead.

## The instrument: a measurement-only trace overlay

[trace/bbr_trace.go](trace/bbr_trace.go) is a verification aid in package `congestion`, built only into the `cand-trace` fixture variant. It records rows of the BBR model's state (48 integer columns, named in the file) into a fixed array outside the Go heap (65,536 rows). It writes them once, streamed row by row as JSON: when the fixture exits, after the fixture has written its own record (so the record's CPU and memory figures do not include the write), or at the first feedback event at or after `BBR_TRACE_UNTIL_UNIX_NS`, whichever comes first. It records nothing after that. A mutex orders the hooks (on the connection goroutine) and the exit write (on the main goroutine). It is enabled only in a sender-role process with both environment variables set, and only for the first BBR sender that receives feedback; the runner sets them only for the `trace` arm's focal sender, with the end time 2 s after the measured window.

Rows:
- one per packet-timed round, with that round's lost bytes and packets, largest prior flight, largest rate sample, feedback events, limited events and two-percent checks;
- a pre and a post row around every feedback event that changes the phase, a short-term bound, `inflight_longterm` (a decrease, or a change to or from unbounded), the recovery episode, the CE state or the ProbeRTT hold, or that first reports ECN failure (`max_bw` decreases too);
- a pair at every loss-round close that had loss (before and after the bounds update);
- one per two-percent check, with its inputs, before its outcome is applied;
- a pre/post pair for any such change made between feedback events (send-side idle restart), and one per model reset.

[build.py](build.py) applies eight hooks, each a text that must occur exactly once. Seven are in BBR-only controller code: `Feedback` entry and exit, the round start, both sides of the loss-round close, `Reset`, and `probeLoss` (entry and the check). The eighth is in the fixture's `main.go`, after it writes its own record: a call to a two-line exit hook ([trace/fixture_trace_exit.go](trace/fixture_trace_exit.go), through [trace/quic_trace_exit.go](trace/quic_trace_exit.go) in the root package). Reno, emission and recovery code are untouched.

Tests ([trace/bbr_trace_test.go](trace/bbr_trace_test.go)) run in the exported `cand-trace` tree, alongside the port's own `internal/congestion` tests, which also pass. They cover:
- configuration, and nothing recorded when disabled;
- a probe cycle and its loss check, and loss-round rows;
- changes between events, and a second sender ignored;
- overflow counted, and the reset row;
- the single write, at the end time and at exit.

They also pass under `-race`. Two injected faults (the check hook removed; phase changes ignored) each failed the tests, and the source was restored. `go vet` passes for `internal/congestion`, the root package and the fixture.

**A development defect, fixed before this registration.** The first version wrote the trace only at a feedback event 2 s after the measured window. The fixture exits about 1 s after the window, so the first development run left no trace ([Development runs](#development-runs-before-this-registration)). The exit write was added then, and the binary was rebuilt.

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
| C6 | cwnd never above `max(4 M, min(caps, quantized(gain × BDP + extra_acked)))`; a window restored at a recovery exit never above the volume caps | 5.6.4.2–5.6.4.7 |
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
- **Development history.** While the checker was developed on the first fixture, its first versions flagged 19, then 35, then 11 events that were checker errors, each traced to the row data and fixed. In order: a rule that forgot `max(latest, 0.7 × bound)` can raise a bound; four rules that read end-of-event values where the port decides mid-event; and a lateness allowance replaced by the first-event rule. The fixtures were regenerated with a driver whose flight follows the window, after the first one stayed in ProbeRTT because its synthetic flight never drained. After the development runs, C6 was given the recovery-exit restore rule; every event it had flagged there was such a restore ([Development runs](#development-runs-before-this-registration)).

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
- the two-percent checks: how many judged a packet marked supply-limited, how many crossed, how many crossings were limited, and where each crossing left `inflight_longterm` against the path's capacity;
- how often `bw_shortterm` was below `max_bw`;
- `inflight_longterm` and the window against the path's capacity (queue plus pipe);
- `max_bw` against BBRv3's own delivery;
- where in BBRv3's phases each flow's drops fall, and each flow's queue occupancy by BBRv3 phase;
- the time of the ECN switch, with the competitor's delivery in the 30 s before and after.

## Arms, pairs, order and host

| Arm | Focal fixture | Focal controller | Binary (SHA-256) |
| --- | --- | --- | --- |
| `trace` | `abfa5456` + this record's overlay | BBRv3 | `cand-trace-fixture` `6c25a98b…` (built on `minimax` from the exported tree) |
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

Evaluated over the selected counted observations. Terms:

- A trace **conforms** when [draft06check.py](draft06check.py) reports no mismatch in its measured window. A mismatch can be dismissed only when a test built from that event's rows shows the checker misreading it; the checker is then fixed and rerun on every trace, and the change is disclosed as post-data.
- A trace's inputs **reconcile** when [reconcile.py](reconcile.py)'s R1–R4 are within tolerance in every classified period, or each finding is shown not to change what the controller saw.
- A **port property acts** in a trace when, in its classified periods, either:
  - at least half of the two-percent crossings judged packets marked supply-limited (draft-06 then leaves `inflight_longterm` unlowered); or
  - a deviation from draft-06 or r9 is demonstrated in code and changes a transition, bound or rate in that trace. The demonstration is a unit-level reproduction in the exported tree, driving the port's sender with a mismatching event's recorded inputs.

Rules:

1. **Undiagnosed (instrument)** if D3 fails for any selected `trace` observation, or fewer than two `trace` observations per workload are usable with complete traces.
2. **(c), the fixture** if, in any selected trace, the inputs fail to reconcile in a way the fixture owns (relay or model signals that differ from the modeled path), or the competitor is limited by something other than its controller (CPU saturation, supply, flow control) in a classified period.
3. **(b), the port** if a port property acts in every selected trace of both workloads. #777 found both workloads adverse, so a cause must be present wherever the displacement is.
4. **(a), draft-06 behavior** if neither (b) nor (c) holds and, in at least one workload, every selected trace conforms, reconciles, has no port property acting, and is adverse under D3. There, draft-06's rules alone produce the displacement, and the mechanism figures say which rules. A port property acting in the other workload is reported as a separate finding, for the operator to decide whether to file. Its contribution to that workload's displacement is not measured here.
5. **Undiagnosed** otherwise, saying what was ruled out.

If (b) and (c) both hold, the record reports both and names neither alone.

**What each outcome leads to (#792):**
- **(b):** a fix ticket, then readiness and L6 again on the fixed revision.
- **(a):** the adoption question for GridCast's shared paths goes to the operator.
- **(c):** #777's validity question goes back to the operator before anything else.
- **Undiagnosed:** what was ruled out, and a decision for the operator on whether to spend more.

## Development runs (before this registration)

Made on `minimax` on October 18 (UTC), unbooked, with the wiremux sessions confirming they would not use `minimax`. Each is excluded from every statistic. None ran a quiet-host gate.

| Run | What | Result |
| --- | --- | --- |
| `dev1` (03:31–03:38) | STREAM, `trace`, full length | Usable, but **no trace**: the first overlay waited for a feedback event 2 s after the measured window, and the fixture exits about 1 s after it. Focal 85.0, 80.2, 85.3, 93.4 and 87.3 Mbit/s, competitor 8.6, 14.4, 7.9, 0.0 and 6.3 (equal, short, back, absent, resumed). This led to the exit write and the rebuilt binary (`6c25a98b…`). |
| `dev2` (03:38–03:40) | DATAGRAM, `trace` | Stopped by the agent mid-run once `dev1`'s missing trace was understood; no result. |
| `dev3` (03:41–03:48) | STREAM, `trace`, full length | Complete trace: 6,666 rows, no overflow, every two-percent check recorded. Checker: no mismatch (after the C6 change below). Input check within tolerance in every period; declared losses equal the relay's drops exactly. Focal 86.1, 83.5, 86.1, 93.3 and 87.4; competitor 7.3, 11.0, 7.1, 0.0 and 5.7. |
| `dev4` (03:48–03:54) | DATAGRAM, `trace`, full length | Complete trace: 6,823 rows. Checker: no mismatch. Input check within tolerance. Focal 83.7, 75.3, 83.9, 92.5 and 85.3; competitor 8.5, 18.8, 8.2, 0.0 and 6.8. |

**Seen by the agent** (equal period unless stated; `dev3`, then `dev4`):
- **Representative.** Both traces are adverse under D3. The competitor's delivery is close to #777's plain-port medians (7.6 and 8.3).
- **The ceiling.** `inflight_longterm` is 1.06 and 0.96 × the path's capacity (queue plus pipe) and the window 0.90 and 0.82 ×. The Cruise headroom set the window ceiling in 79% and 77% of rounds.
- **Where the competitor loses.** 99.7% and 99% of the competitor's drops fall in BBRv3's Up phase. Probes come about every 4.7 and 5.3 s (the 2–3 s waits lengthened by ProbeRTT). Up ended on loss in 19 of 22 and 19 of 20 probes.
- **The two-percent checks.** In STREAM, all 2,429 checks and all 69 crossings judged packets marked application-limited, so no crossing lowered `inflight_longterm`. In DATAGRAM, none of 2,599 checks did, and its 62 crossings set `inflight_longterm` as draft-06 computes it (at 0.96 × the path, the flight at which drop-tail loss passed 2%).
- **ECN.** The ECN failure came at 54 s and 178 s. The competitor's delivery in the 30 s before and after was 7.3 and 7.4, then 20.5 and 17.8 Mbit/s.
- **Inputs.** `min_rtt` 100.2–100.8 ms; `max_bw` 0.95–0.98 of the link's capacity in the contested periods (1.007 while the competitor paused). In rounds with any loss, BBRv3 lost a median of 9.7% and 2.0% of the round's data.

**What changed after these runs:**
- the exit write (`dev1`);
- C6's recovery-exit rule. All 66 (`dev3`) and 3 (`dev4`) events C6 had flagged were windows restored at a recovery exit, and in 65 of the 66 the window was below `max_inflight` at the next recorded row;
- the application-limited row of the code check, the two-percent-check measures in [analyze.py](analyze.py), and the classification's "port property acts" term, written to cover what `dev3` showed;
- the agent's expectation (above).

No threshold of D3, tolerance of R1–R4, checker rule other than C6, arm, pair count or order changed after these runs.

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
