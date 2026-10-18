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
| The 2% check per lost packet while probing, `inflight_longterm = max(inflight at loss, 0.7 × min(BDP, cwnd))`, Up → Down | 5.5.10.2 `HandleLostPacket`, `IsInflightTooHigh`, `HandleInflightTooHigh`, `InflightAtLoss` | `bbr_probe.go:156` | Matches, with one arithmetic difference: when the losses before a packet already exceed 2%, the draft's prefix is negative and the port clamps it to zero (`:175`), so the port's `inflight_longterm` can be higher by that negative prefix. Unsigned arithmetic, not a declared translation: a **deviation demonstrated in code**. C4 records every such event with its size; the classification treats it as a port property and asks for its connection to the displacement. |
| `inflight_longterm` upward: on a sample whose loss is within 2%, raise it to the sample's `tx_in_flight`; in Up, while cwnd-limited at it, raise it with the doubling slope | 5.3.3.9 `AdaptLongTermModel`, `ProbeInflightLongtermUpward`, `RaiseInflightLongtermSlope` | `bbr_probe.go:84`–`:91`, `:188`, `:194` | Matches, with one conservative difference: the port raises to `tx_in_flight` only from a valid rate sample. **Read, not traced by a rule:** the ROUND rows record the level, and no rule bounds an increase (see [What a conforming trace shows](#classification-fixed-before-counted-data)). |
| Short-term bounds outside probing: once per loss round with loss, `bw_shortterm = max(bw_latest, 0.7 × bw_shortterm)`, the same for `inflight_shortterm` | 5.5.10.3 `AdaptLowerBoundsFromCongestion`, `InitLowerBounds`, `LossLowerBounds` | `bbr_sender.go:219`–`:251` | Matches; 0.7 × is an integer floor. |
| No cwnd cut on entering fast recovery; save on entry, restore on exit through the volume caps | 5.6.4.4 `RestoreCwnd`, 5.6.4.7, 5.5.11 | `bbr_recovery.go:21`, `:31` | Matches. Draft-06 has no packet-conservation step; PTO handling is the declared translation D07. The port restores after the event's window update, so `max_inflight` bounds a restored window from the next ACK; the draft leaves that order open (a one-ACK transient, seen in the development runs). |
| cwnd: `min(cwnd + acked, max_inflight)`, `max_inflight = quantized(cwnd_gain × BDP + extra_acked)`, then the volume caps per phase | 5.6.4.2, 5.6.4.6, 5.6.4.7 | `bbr_sender.go:296`, `:322`, `:401` | Matches. |
| Pacing: gain × min(max_bw, bw_shortterm), gains 2.77, 0.5, 0.9, 1, 1, 1.25, 1; 1% margin | 5.6.1, 5.6.2 | `bbr_sender.go:362` | Matches; the margin is applied in emission (design, Pacing). |
| max_bw: largest sample of the current and previous ProbeBW cycle; the cycle advances at the end of the probe's feedback | 5.5.3–5.5.5, 5.3.3.9 `AdaptLongTermModel` | `bbr_probe.go:23`, `:66` | Matches. |
| ProbeRTT: enter when the 5 s filter expires, hold 200 ms and one round, exit to ProbeBW (Cruise) or Startup with the short-term model reset | 5.3.4.3, 5.3.4.4 | `bbr_probe_rtt.go:14`, `:29`, `:80` | Matches, with the declared translation D02 (pre-expiration cap). |
| Application-limited samples: when cwnd and pacing would allow sending but there is no data, mark the next flight's samples application-limited; a limited crossing does not lower `inflight_longterm` | 4.1.1.3, 4.1.2.4 `CheckIfApplicationLimited`, `MarkConnectionAppLimited`; 5.5.10.2 | `delivery_sampling.go:32`, `:51`, `:104`, `:133`; `internal/ackhandler/delivery_sampler.go:222`, `:240`, `:274` | The rule matches: no data while cwnd and pacing permit and no retransmission is pending, and one flight is marked. **A property of the port's send path.** quic-go's `SendStream.Write` returns once less than one packet of a write remains (`send_stream.go:265`–`:312`), so within one packing burst the stream runs dry at every 16,384-byte record, and a bulk STREAM sender is marked application-limited almost continuously. The draft leaves the precise determination to the implementation (4.1.1.3). The design fixes it: "when otherwise permitted ordinary packing finds no eligible data" ([Delivery sampling and limitation](https://github.com/the-sarge/quic-go-fast/blob/abfa5456/docs/designs/bbrv3.md#delivery-sampling-and-limitation)). DATAGRAM senders are not marked this way. Seen in the development runs, and measured in every trace by reason. |
| Flow-control-limited samples: draft-06 treats a receiver flow-control limit like a network bottleneck (not application-limited) | 4.1.1.3.1 | `internal/congestion/bbr_sender.go:385` (`bbrLimited`) | Differs from the draft by a **declared** design choice ("flow-control blocking is supply-limited for rate filtering", the design's Delivery sampling and limitation). L6's 64/128 MiB windows do not limit; every trace reports limitation marks by reason. |

**Not checked here:** delivery-rate sampling other than the application-limited marking (section 4.1, owned by `internal/ackhandler`'s sampler), `extra_acked` (5.5.9) beyond its use in the cwnd bound, and the offload budget (5.5.8). The [input check](#input-check-the-senders-view-against-the-relays) tests the sampler's output against the relay instead.

## The instrument: a measurement-only trace overlay

[trace/bbr_trace.go](trace/bbr_trace.go) is a verification aid in package `congestion`, built only into the `cand-trace` fixture variant. It records rows of the BBR model's state (48 integer columns, named in the file) into a fixed array outside the Go heap (65,536 rows). It writes them once, streamed row by row as JSON: when the fixture exits, after the fixture has written its own record (so the record's CPU and memory figures do not include the write), or at the first feedback event at or after `BBR_TRACE_UNTIL_UNIX_NS`, whichever comes first. It records nothing after that. A mutex orders the hooks (on the connection goroutine) and the exit write (on the main goroutine). It is enabled only in a sender-role process with both environment variables set, and only for the first BBR sender that receives feedback; the runner sets them only for the `trace` arm's focal sender, with the end time 2 s after the measured window.

Rows:
- one per packet-timed round, with that round's lost bytes and packets, largest prior flight, largest rate sample, feedback events, limited events and two-percent checks;
- a pre and a post row around every feedback event that changes the phase, a short-term bound, `inflight_longterm` (a decrease, or a change to or from unbounded), the recovery episode, the CE state or the ProbeRTT hold, or that first reports ECN failure (`max_bw` decreases too);
- a pair at every loss-round close that had loss (before and after the bounds update);
- one per two-percent check, with its inputs, before its outcome is applied, and, for a check that crosses, one right after `inflight_longterm`'s assignment (before any later adjustment in the event);
- a pre/post pair for any such change made between feedback events (send-side idle restart), and one per model reset.

[build.py](build.py) applies nine hooks, each a text that must occur exactly once. Eight are in BBR-only controller code: `Feedback` entry and exit, the round start, both sides of the loss-round close, `Reset`, and `probeLoss` (entry, the check, and after the assignment). The ninth is in the fixture's `main.go`, after it writes its own record: a call to a two-line exit hook ([trace/fixture_trace_exit.go](trace/fixture_trace_exit.go), through [trace/quic_trace_exit.go](trace/quic_trace_exit.go) in the root package). Reno, emission and recovery code are untouched.

Tests ([trace/bbr_trace_test.go](trace/bbr_trace_test.go)) run in the exported `cand-trace` tree, alongside the port's own `internal/congestion` tests, which also pass. They cover:
- configuration, and nothing recorded when disabled;
- a probe cycle and its loss check, and loss-round rows;
- changes between events, and a second sender ignored;
- overflow counted, and the reset row;
- the single write, at the end time and at exit.

They also pass under `-race`. Two injected faults (the check hook removed; phase changes ignored) each failed the tests, and the source was restored. `go vet` passes for `internal/congestion`, the root package and the fixture.

**Rebuilt after the first review** (`73555c57…`) to add the post-assignment row; the review found that C4 could not otherwise tell the crossing's own assignment from a later adjustment in the same event.

**A development defect, fixed before this registration.** The first version wrote the trace only at a feedback event 2 s after the measured window. The fixture exits about 1 s after the window, so the first development run left no trace ([Development runs](#development-runs-before-this-registration)). The exit write was added then, and the binary was rebuilt.

**Disturbance.** The overlay adds work on the connection goroutine per feedback event (two struct copies and a few comparisons) and a JSON write after the measured window. Its effect on the measured flows is measured, not assumed: the `cand` arm runs beside it in every pair, and D3 decides whether the traces are representative.

## The checker: traces against draft-06

[draft06check.py](draft06check.py) recomputes, from each trace's logged inputs, what draft-06 says the state should become, and lists every mismatch with its event and measured period. It never imports or restates the port's code; where the port's arithmetic is a declared translation, the rule applies the translation.

| Rule | What it checks | Draft-06 |
| --- | --- | --- |
| C0 | Startup exits on plateau, loss or CE; Drain exits at `inflight <= Inflight(1.0)` or after three rounds | 5.3.1.2, 5.3.1.3, 5.3.2 |
| C1 | Refill only after the wall-clock or Reno-round trigger (or an undo, or precautionary acceleration), never under a CE response; no Down/Cruise round past a trigger without a Refill | 5.3.3.8.3, 5.3.3.9 |
| C2 | Refill resets the short-term model, lasts one round, and enters Up with the plateau detector reset and probe sampling on | 5.3.3.3, 5.3.3.9 |
| C3 | Up ends only on loss, precautionary deceleration, plateau (not while cwnd-limited at `inflight_longterm`), CE or ProbeRTT; and no round starts in Up with a full plateau (a missed exit) | 5.3.3.7 |
| C4 | Each two-percent crossing sets `prev_probe_too_high`, ends probe sampling and Up; its assignment, read from the row right after it, is draft-06's value within one byte, or the port's clamp (recorded as a deviation event with its size); a crossing on a supply-limited packet leaves `inflight_longterm` unchanged; every lowering has an unlimited crossing, a Startup loss exit or a reset in its own event | 5.5.10.2 |
| C5 | Each loss-round close with loss outside probing sets both short-term bounds to `max(latest, 0.7 × bound)`; no other change except resets at Refill, ProbeRTT exit or reset, and undo raises | 5.5.10.3, 5.5.11 |
| C6 | cwnd never above `max(4 M, min(caps, quantized(gain × BDP + extra_acked)))`; in ProbeRTT never above `max(4 M, 0.5 × BDP)` (D02's tighter retained cap is not recorded, so not checked); a window restored at a recovery exit never above the volume caps | 5.6.4.2–5.6.4.7 |
| C7 | Pacing rate equals the phase gain × `min(max_bw, bw_shortterm)` (at or below under CE) | 5.6.1, 5.6.2 |
| C8 | `max_bw` never above every sample of the current and previous cycle after its event's own sample, and never below one when it ages | 5.5.3–5.5.5 |
| C9 | ProbeRTT starts only when the 5 s filter has expired, holds more than 200 ms, exits to Cruise or Startup with the short-term model reset, and starts in the same event at every recorded round whose filter has expired (no allowance) | 5.3.4.3, 5.3.4.4 |

**Instrument granularity.** Rows show an event's state before and after it, and a few points inside it. The port decides some transitions mid-event, after the event's own rate sample and loss-round close and before its RTT and window updates. The checker uses the recorded mid-event values where it needs them (the BDP after the loss-round close, the pre-update window, `inflight_longterm` after the upward adjustment), and a filter value that only changes at the next sample after a cycle advance (as in the draft). An event that passes through Refill into ProbeRTT is read as a Refill.

**Tests** ([draft06check_test.py](draft06check_test.py), 37):
- **Fixtures.** Two traces of the port itself pass every rule with no mismatch. They were written by [trace/bbr_trace_test.go](trace/bbr_trace_test.go) in the exported tree, driving the real sender through synthetic paths with the port's own test driver ([testdata/](testdata/)). Between them they exercise:
  - both probe triggers;
  - loss, plateau and ProbeRTT exits from Up;
  - 114 two-percent crossings, binding short-term bounds and filter aging;
  - ProbeRTT entries and exits.
- **Injected deviations.** Each of twenty-eight is caught by its rule:
  - a Startup exit without plateau, loss or CE;
  - an early Refill, and Refills missed at either trigger;
  - a short-term model kept at Refill, and a Refill longer than a round;
  - an Up exit with no condition, and an Up kept after a full plateau;
  - for C4, with an ACK in the same event: an assignment twice the draft's, and a stale bound never lowered (while a later same-event raise is accepted), a crossing without its assignment row, a missing `prev_probe_too_high`, a crossing that stays in Up, and an unexplained decrease;
  - the clamp recorded as a deviation (and not when the 0.7 floor removes it, or for integer rounding);
  - a supply-limited crossing that lowers the bound (and one that leaves it unchanged passes);
  - a draft crossing that the port skips;
  - a wrong short-term bound, and one changed without a loss round;
  - a window above its bound, a ProbeRTT window above its cap, and a recovery-exit restore above the volume caps (one at the caps passes);
  - a wrong pacing gain;
  - a filter above its samples, and one aged below a sample;
  - a short ProbeRTT hold, an entry without expiry, and an expired filter with no entry (exempt after an idle restart).
- **Arithmetic and completeness.** The clamp arithmetic is checked, a trace with overflow is marked incomplete, and checks and crossings are counted by limitation reason.
- **Development history.** While the checker was developed on the first fixture, its first versions flagged 19, then 35, then 11 events that were checker errors, each traced to the row data and fixed. In order: a rule that forgot `max(latest, 0.7 × bound)` can raise a bound; four rules that read end-of-event values where the port decides mid-event; and a lateness allowance replaced by the first-event rule. The fixtures were regenerated with a driver whose flight follows the window, after the first one stayed in ProbeRTT because its synthetic flight never drained. After the development runs, C6 was given the recovery-exit restore rule; every event it had flagged there was such a restore ([Development runs](#development-runs-before-this-registration)).

## Input check: the sender's view against the relay's

[reconcile.py](reconcile.py) compares, per measured period, what the traced sender recorded with what relay2 recorded for the same flow. Tolerances are fixed in the file, before any counted observation:

| Check | Sender | Relay | Tolerance |
| --- | --- | --- | --- |
| R1 | Lost packets it declared (shifted 250 ms) | Forward drops of that flow | within max(2%, 15 packets) |
| R2 | Bytes it saw delivered (shifted 75 ms) | Forward delivered bytes of that flow, in QUIC bytes | ratio in [0.98, 1.02] |
| R3 | `max_bw` | The link's capacity in QUIC bytes | at most 1.05 × |
| R4 | `min_rtt` | The model's 100 ms propagation, plus up to about 100 ms of queue | in [99, 210] ms |

On the L6 path the relay's FIFO never reorders a flow, so every declared loss should be a relay drop and every drop a declared loss. Each check in each period is `ok`, `fail` or `incomplete`, with a reason. Missing evidence (no sender rounds in a window, a missing boundary record, no bandwidth or RTT samples) is `incomplete` and blocks reconciliation; it never counts as passing. Zero sender delivery against positive relay delivery is a ratio of zero, a failure. A failure is a finding to explain; its owner is decided by the [classification's evidence rule](#classification-fixed-before-counted-data). Twelve tests ([reconcile_test.py](reconcile_test.py)) cover an agreeing synthetic observation, each check failing, frozen sender delivery, missing sender rounds, a missing relay boundary, no relay delivery, and missing bandwidth and RTT.

## Mechanism figures (descriptive)

For each traced observation and period, [analyze.py](analyze.py) reports, without thresholds:
- the share of time in each phase;
- what started each probe (wall clock or rounds) and the interval between probes;
- why each Up ended;
- BBRv3's own loss per packet-timed round, and the share of rounds above 2%;
- which bound set the window ceiling (the model target, `inflight_shortterm`, `inflight_longterm` or the headroom) and how often a round was cwnd-limited;
- the two-percent checks by limitation reason: how many judged a packet marked supply-limited, how many crossed, how many crossings were limited, and where each crossing left `inflight_longterm` against the path's capacity;
- each acting port property, with its event count and its counterfactual (the Cruise ceiling without it against the ceiling it left, both over the pipe) — the registered connection to the displacement;
- how often `bw_shortterm` was below `max_bw`;
- `inflight_longterm` and the window against the path's capacity (queue plus pipe);
- `max_bw` against BBRv3's own delivery;
- where in BBRv3's phases each flow's drops fall, and each flow's queue occupancy by BBRv3 phase;
- the time of the ECN switch, with the competitor's delivery in the 30 s before and after.

## Arms, pairs, order and host

| Arm | Focal fixture | Focal controller | Binary (SHA-256) |
| --- | --- | --- | --- |
| `trace` | `abfa5456` + this record's overlay | BBRv3 | `cand-trace-fixture` `73555c57…` (built on `minimax` from the exported tree) |
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

Evaluated over the selected counted observations: first for each workload, then for the path. [analyze.py](analyze.py) `attribute` applies the mechanical parts. A class it cannot settle (an input-check failure's owner, a checker mismatch) comes back as *needs review*, and the record settles it by the rules below.

**Terms.**
- A trace **conforms** when [draft06check.py](draft06check.py) reports no mismatch in its measured window. A mismatch is dismissed only when a test built from that event's rows shows the checker misreading it. The checker is then fixed and rerun on every trace, and the record discloses the change as post-data. That disclosure cites the draft-06 text that permits the port's reading and gives the mismatch counts before and after.
- A trace's inputs **reconcile** when [reconcile.py](reconcile.py) reports `ok` for every check in every period. A `fail` or `incomplete` check does not reconcile.
- A **port property acts** in a workload when its traces show at least one of these in the classified periods:
  - a two-percent crossing on a packet marked application-limited (reason 1), after which draft-06 leaves `inflight_longterm` unlowered;
  - a clamp deviation recorded by C4;
  - a deviation from draft-06 or r9 demonstrated in code that changes a transition, bound or rate. The demonstration is a unit-level reproduction in the exported tree, driving the port's sender with an event's recorded inputs.

  Flow-control and ProbeRTT limitation marks are reported by reason. They are not the application-limited property.
- **A property's connection to the displacement** is documented by a counterfactual: the Cruise window ceiling that draft-06's assignment would give without the property, against the path's pipe (100 Mbit/s × 100 ms, in QUIC bytes). It is the median over the property's events in the classified periods ([analyze.py](analyze.py) `properties`).
  - At or below the pipe, BBRv3 could not hold a standing queue in Cruise. The standing queue is how its probes overflow the shared queue, so the property is a **cause**.
  - Above the pipe, the standing queue remains without the property, so it is a **contributor**.
  - A demonstrated deviation that has no recorded counterfactual needs its reproduction to supply one. Otherwise its connection is unresolved.
- **The owner of an input-check failure** is established by evidence, not by which check failed:
  - **The fixture's** when the calibration or relay2's own counters show the modeled path differing from its declaration (a calibration delay or rate outside its criteria, or per-flow counters that disagree with the model's totals).
  - **The port's** when calibration and relay agree and the sender's view departs from them (declared losses that are not relay drops, a delivery count the relay did not see, a bandwidth estimate above the link, a minimum RTT below propagation).
  - **Unresolved** otherwise.

  The competitor being limited by something other than its controller is evidenced only by #777's usability rule: an endpoint at 80% of its CPU allotment makes the observation unusable. No other route is registered.

**Per workload,** in order:
1. **Undiagnosed (instrument)** if D3 fails for any of its selected `trace` observations, or fewer than two of them are usable with complete traces.
2. **(c), the fixture,** if an input-check failure in a classified period is the fixture's.
3. **(b), the port,** if an acting port property is a cause, or if an input-check failure is the port's and changes what the controller saw in the classified periods.
4. **(a), draft-06 behavior,** if the traces conform and reconcile and every acting port property is a contributor. Draft-06's rules then produce the displacement in that workload; the mechanism figures say which rules, and each contributor is reported with its counterfactual.
5. **Undiagnosed** otherwise (an unresolved owner or connection, or an unexplained checker mismatch), saying what was ruled out.

If (b) and (c) both hold in one workload, that workload reports both and is undiagnosed.

**For the path:**
- When both workloads have the same class among (a), (b) and (c), the path has that class.
- Otherwise the path is **undiagnosed** (D5: mixed or missing attribution). The record still keeps each workload's supported conclusion and its #792 consequence: a fix ticket for (b), the adoption question for (a), #777's validity for (c).
- Contributors are always reported, with event counts and counterfactuals, and the operator decides whether to file them.

**The rules walked through** (the review's cases):

| Case | Workloads | Path |
| --- | --- | --- |
| DATAGRAM clean; in STREAM, a property acts and its counterfactual ceiling stays above the pipe | (a), (a) with a contributor | (a); the contributor reported |
| DATAGRAM clean; in STREAM, a property's counterfactual ceiling falls to the pipe or below | (a), (b) | undiagnosed (mixed); STREAM's (b) and its fix ticket kept, DATAGRAM's (a) kept |
| A different property is a cause in each workload | (b), (b) | (b), naming both properties |
| A demonstrated deviation whose counterfactual stays above the pipe | unchanged by it | the deviation reported as a contributor |
| An input-check failure with no established owner | undiagnosed there | undiagnosed |
| A fixture-owned input-check failure in one workload | (c) there | undiagnosed (mixed), with (c)'s consequence first |

**What a conforming trace shows.** Zero mismatches means the trace satisfies the registered rules C0–C9: a bounded comparison, not a proof that every transport behavior follows the draft. No rule validates that emission obeyed the cwnd and pacing rate, or that upward adjustments of `inflight_longterm` followed the draft. The code check covers both by reading the code, and the ROUND rows record the bound's level. A cause argument that depends on either says so.

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
| `dev5` (04:35–04:42) | STREAM, `trace`, full length, after the first review (binary `73555c57…`) | Complete trace: 6,959 rows. Checker (this registration's rules): no mismatch. Input check `ok` in every period. Focal 86.6, 84.0, 86.3, 93.4 and 87.8; competitor 6.8, 10.3, 6.8, 0.0 and 5.7. |
| `dev6` (04:42–04:48) | DATAGRAM, `trace`, full length, after the first review | Complete trace: 6,991 rows. Checker: no mismatch. Input check `ok`. Focal 83.8, 76.0, 85.3, 92.0 and 86.0; competitor 8.4, 18.1, 7.1, 0.0 and 5.9. |

**Seen by the agent** (equal period unless stated; `dev3`, then `dev4`):
- **Representative.** Both traces are adverse under D3. The competitor's delivery is close to #777's plain-port medians (7.6 and 8.3).
- **The ceiling.** `inflight_longterm` is 1.06 and 0.96 × the path's capacity (queue plus pipe) and the window 0.90 and 0.82 ×. The Cruise headroom set the window ceiling in 79% and 77% of rounds.
- **Where the competitor loses.** 99.7% and 99% of the competitor's drops fall in BBRv3's Up phase. Probes come about every 4.7 and 5.3 s (the 2–3 s waits lengthened by ProbeRTT). Up ended on loss in 19 of 22 and 19 of 20 probes.
- **The two-percent checks.** In STREAM, all 2,429 checks and all 69 crossings judged packets marked application-limited, so no crossing lowered `inflight_longterm`. In DATAGRAM, none of 2,599 checks did, and its 62 crossings set `inflight_longterm` as draft-06 computes it (at 0.96 × the path, the flight at which drop-tail loss passed 2%).
- **ECN.** The focal flow's ECN failure came at 54 s in `dev3` and 178 s in `dev4`. The competitor's delivery in the 30 s before and after the switch was 7.3 before and 7.4 after (`dev3`), and 20.5 before and 17.8 after (`dev4`).
- **`dev5` and `dev6`** (the final overlay and rules) repeat this:
  - `inflight_longterm` 1.05 and 0.96 × the path, the window 0.89 and 0.82 ×;
  - the competitor's drops in BBRv3's Up 97% and 99%;
  - STREAM's 72 crossings all on application-limited packets, DATAGRAM's 62 on none;
  - one clamp event (88 bytes) in DATAGRAM's short period.

  The counterfactual connections (median over a period's events) place the Cruise ceiling without the application-limited marking at 1.64–1.67 × the pipe, against 1.78–1.83 × with it, and the clamp's at 1.49 × either way. Each would be a contributor under the rules. The ECN switch came at 87 s and 177 s, with the competitor's delivery 7.1 before and 9.1 after (`dev5`), and 19.7 before and 14.0 after (`dev6`, whose 30 s after reach into the back period).
- **Inputs.** `min_rtt` 100.2–100.8 ms; `max_bw` 0.95–0.98 of the link's capacity in the contested periods (1.007 while the competitor paused). In rounds with any loss, BBRv3 lost a median of 9.7% and 2.0% of the round's data.

**What changed after these runs** (before the first review, `eb794a37` to `ccac7cb7`):
- the exit write (`dev1`);
- C6's recovery-exit rule. All 66 (`dev3`) and 3 (`dev4`) events C6 had flagged were windows restored at a recovery exit (draft-06 5.6.4.4: `RestoreCwnd`, then `BoundCwndForModel`'s volume caps). In 65 of the 66 the window was below `max_inflight` at the next recorded row. In the other, the next recorded row was a ROUND row, written before its event's window update; the next post-event row passes C6, because `max_inflight` had risen above the restored window. Mismatches before the change: 66 and 3; after: 0 and 0;
- the application-limited row of the code check and the two-percent-check measures in [analyze.py](analyze.py);
- **the classification, rewritten after `dev3` and `dev4` had been seen:**
  - (b) needed a port property in every trace of both workloads, and (a) needed only one clean workload;
  - the routes to (b) for a port-owned input-check failure, and for an undeclared deviation named by the code check, were removed;
  - a "port property acts" term (half of the crossings on supply-limited packets) was added;
  - the order changed from (b), (c), (a) to (c), (b), (a).

  The stated rationale was that a cause must be present wherever the displacement is. The first review found this rewrite unsound and its disclosure incomplete, and it was replaced ([Registration review](#registration-review));
- the agent's expectation (above).

No threshold of D3, tolerance of R1–R4, checker rule other than C6, arm, pair count or order changed after these runs.

## Registration review

**First consideration:** RAS run `20261018T035853-1cb9f2f3160e5599a2e5804c` (`ras consider`, kind plan, default agents and adjudication), on `ccac7cb7`. One of six reviewers (agy-argon) failed on its output format.
- **Context:** this registration; `draft06check.py`, `reconcile.py`, `analyze.py`, `matrix.py`, `build.py` and `runchanges.py`; the overlay and its tests.
- **Prompt:** #792's binding requirements, question, consequences and scope, quoted verbatim, with D1–D6 stated as settled.
- **Output:** 28 findings in 28 clusters. The synthesis queued nine items. Each was checked against the code and dispositioned:

| Item | Disposition | Change |
| --- | --- | --- |
| F1. One clean workload could name (a) for the path and hide a workload-local cause; a property counted without any connection to the displacement; an input-check failure's owner was undecided. | fix-now | The classification is per workload, then for the path. A property needs a counterfactual connection, ownership is by evidence, and mixed attribution leaves the path undiagnosed while keeping each workload's conclusion. Claims are narrowed ([What a conforming trace shows](#classification-fixed-before-counted-data)). |
| F2. C4 accepted any bound in an event with an ACK. | fix-now | The overlay records the row right after the assignment (rebuilt, `73555c57…`), and C4 checks that row. |
| F3. C4 took the port's clamp as the expected value. | fix-now | C4 expects draft-06's value. The clamp is a deviation event with its size, which the classification treats as a port property. |
| F4. Reconciliation passed on missing or contradictory evidence. | fix-now | `ok`, `fail` and `incomplete` statuses; `incomplete` blocks reconciliation; zero delivery is a failure. |
| F5. C6 lacked the ProbeRTT cap. | fix-now | `max(4 M, 0.5 × BDP)` in ProbeRTT, restores included. D02's retained cap is not recorded and so not checked. |
| F6. A limited crossing could justify a decrease. | fix-now | A limited crossing must leave the bound unchanged, and a decrease needs its own unlimited crossing. |
| F7. Limitation reasons were aggregated. | fix-now | Counts by reason. Only application-limited marks (reason 1) are the STREAM property; flow control's treatment is cited as the design's declared choice. |
| F8. The post-development rewrite's disclosure was incomplete. | fix-now | Listed under [What changed after these runs](#development-runs-before-this-registration); this section records the second rewrite. |
| F9. C9's 0.5 s allowance. | fix-now | Removed: an expired filter at a recorded round must enter ProbeRTT in that event. |

**Not acted on**, in agreement with the synthesis:
- a `trace`/`cand` acceptance band beside D3: a contract change; the paired comparison is reported;
- new rules for emission adherence or upward bound growth: disclosed as unchecked instead;
- assigning ownership automatically by check: ownership is decided by evidence;
- an ECN gate: the finding had transposed `dev3`'s and `dev4`'s figures, and the wording is now unambiguous;
- tightening R4: queue delay is legitimate, and calibration checks propagation;
- banning post-data checker corrections: the existing process already requires a demonstration, disclosure, a rerun on every trace, and the result review.

**Acted on although the synthesis made it optional:** C3's missed-plateau check, which is exact at decision time, and the omitted C0 case in the test list.

**Held** (C-023, the restore order), pending a reproduction through the next complete ACK. In all 66 `dev3` events, the next post-event row satisfies `max_inflight` ([Development runs](#development-runs-before-this-registration)).

**What changed in the registered rules in this round** (after `dev3` and `dev4`, before any counted observation):
- the classification (F1);
- C3, C4, C6 and C9;
- the reconciliation statuses;
- the overlay's assignment row;
- the analysis's per-reason counts and counterfactuals.

Every change implements a review finding. None loosens a rule, except that the classification replaces the rewritten rules the review found unsound. No D3 threshold, R1–R4 tolerance, arm, pair count, order, fixture binary or relay binary changed; the trace binary did (`73555c57…`). Two more development runs (`dev5`, `dev6`) put the new rules and the new row on real traces.

**Replacement consideration:** RAS run `20261018T045000-4354da5542ac8fa9fe336768`, on `b983b92a`, with the first round's dispositions quoted as settled. One of six reviewers (claude-fable) failed. Its 17 findings were each checked against the code:

| Finding (reviewers) | Disposition | Change |
| --- | --- | --- |
| The counterfactual rule is circular. It is evaluated at BBRv3's already-displaced flight, so on this path it can hardly return "cause" given the displacement D3 confirms (agy-argon, claude-opus, grok). | **stop-for-decision** (repeated root) | None; the decision is the operator's ([Attribution: stopped for a decision](#attribution-stopped-for-a-decision)). |
| The cause test never checks that the property changed the ceiling; a ceiling left unchanged, or already at or below the pipe, could be named the cause (claude-opus, codex-astra, codex-sol, grok). | **stop-for-decision** (same root) | As above. |
| A ceiling above the pipe permits a standing queue but does not show that the displacement persists without the property (codex-astra, codex-sol, grok). | **stop-for-decision** (same root) | As above. |
| The counterfactual covers only the `inflight_longterm` channel. A supply-limited mark also stops the plateau count and the `max_bw` filter's cycle (claude-opus). | fix-now (measured) | analyze.py reports the other channels. In `dev5` (STREAM), the filter's cycle never advanced and `max_bw` never aged in any period, and every Up round start was on a limited sample. In `dev6` (DATAGRAM), the cycle advanced 19 times per 100 s and aged 6–15 times. |
| Attribution took a median of period medians, not the registered median over events (claude-opus, codex-astra, codex-sol, grok). | fix-now | Events pooled. |
| Flow-control-limited crossings (reason 2) escape both C4 and the property list (claude-opus). | defer to the decision | None on L6's paths so far (counted by reason). |
| A stale boundary record turns missing sender rounds into a false R2 failure; figures.py fails on incomplete checks (agy-argon). | fix-now | A boundary record more than 1 s from its boundary is missing; incomplete checks have no values. Tests added. |
| C4's lowering bookkeeping counted any change, never checked the event's end against the assignment, and flagged an undo to unbounded (agy-argon). | fix-now | Counted only for an unlimited crossing that lowers or sets the bound; nothing later in the event may lower the assignment; an undo to unbounded is explained. Tests added. |

**Why stop.** The first round's F1 and these three findings are behaviorally distinct counterexamples to one invariant, at one enforcement point:
- **The invariant:** the classification names a cause only with evidence.
- **The enforcement point:** the attribution rule.
- **The counterexamples:** a workload-local cause hidden by one clean workload (round 1); a cause decided from a circular estimate; a cause named without any change; a "contributor" inferred from a permitted standing queue.

The review protocol treats two such counterexamples as an approach failure, so the attribution goes back to the operator rather than being patched again. The fixes in this round that are outside the attribution rule were applied and tested, and no further RAS round ran on them; the result consideration (D4) reviews them with the result.

### Attribution: stopped for a decision

*Pending the operator's decision; recorded after it.*

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
- **The checker covers the listed rules,** not full draft conformance. Delivery-rate sampling is tested only through its output (R2, R3). Emission's adherence to cwnd and pacing, and upward adjustments of `inflight_longterm`, are read in the code but not checked by a rule.
- **A counterfactual is an estimate.** It replaces one assignment and holds everything else as recorded; it does not simulate what the flows would then have done.
- **A conforming trace shows that the displacement follows from draft-06's rules on this path;** it does not show how another BBRv3 implementation behaves, and it is not a fairness claim about other paths.
- **Not measured:** ECN's cause (#793), L7, L8, S8, other paths and platforms.
