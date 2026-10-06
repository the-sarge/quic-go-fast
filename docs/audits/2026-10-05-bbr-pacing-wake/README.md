# Pacing-wake lateness and BBRv3's Linux goodput flags

**Date:** October 5, 2026, America/New_York. **Scope:** [Test whether pacing-wake lateness explains BBRv3's Linux goodput flags](https://github.com/the-sarge/quic-go-fast/issues/734), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D3 ("Ticket C") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md#d3--ticket-c-test-whether-pacing-wake-lateness-explains-the-linux-goodput-flags). Measurement, attribution and, only if Stage 1 finds timer delivery, one contract-preserving intervention: no production merge, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-pacing-wake-discrimination`, based on `42e40111` (the accepted decision, itself based on the [Linux re-demonstration](../2026-10-05-bbr-linux-redemonstration/README.md) record `36f7ce01`). The starting revision is `d0fabc4d` (r6).

**Status: Stage 0 passed; Stage 1 running.** The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) were written, with the instrument, the analysis and the rules and their synthetic cases, before any counted observation. Excluded smoke runs that preceded the registration are disclosed in [Disclosures made before data](#disclosures-made-before-data).

## Stage 0 result: the instrument is named

Run on October 5–6 after the registration commit (`d419a8ab`); [results.json](results.json) `stage0`.

- **Synthetic cases: all five recovered.** Delivery: verdict delivery, 99.8% of 4,746 late events show delivery-by-timer ≥ 300 µs. Scheduling: verdict scheduling (0.851 of lateness), 100% of 7,388 events ≥ 270 µs. Handler: verdict loop (0.813), 100% of 7,299. Reset: chain counts exact for 100% of 7,119 events, summed lateness exact, all three kinds present. Non-timer wake: verdict delivery, 99.9% of 7,642 events end with a packet wake. Every truth event in the window was found; no run was contaminated.
- **Perturbation: passes.** S5, instrumented ÷ plain, per block: STREAM 0.997, 1.011, 0.988 (median 0.997); DATAGRAM 0.993, 1.000, 0.991 (median 0.993). Loopback DATAGRAM with `cand-lb-timeline`: 1.023, 1.020, 1.024. S5 DATAGRAM block 3 was contaminated by a qemu VM (foreign CPU on the fixture cores); its same-seed rerun was contaminated by the same VM, so under the registered rule the original counts. Without that block the DATAGRAM median is 0.996; the check passes either way.

**Named instrument:** for S5, `cand-wake-timeline` (the recorder and the Go execution trace, with #715's overlay as the independent lateness count); for loopback, `cand-lb-timeline` (the recorder's aggregates). Stage 1 runs as registered.

## Question

On owned Linux hardware, why does the BBRv3 sender reach full-quantum pacing deadlines late, does that also limit loopback DATAGRAM goodput, and does one contract-preserving wake-path change remove the S5 goodput gap without raising wakeups, CPU or Reno cost?

## What stays fixed and what changes

- **Fixed (D3):** `d0fabc4d` as the starting revision; frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; #712's fixture with the causal-diagnosis heap patch, relay (frozen v1 queue model, #711's delivery observability, Linux ECN adapter), launcher, endpoint contract (GOMAXPROCS from the four-core affinity, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz), core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings on `minimax`; `GOTOOLCHAIN=go1.27.0`; #715's S5 and loopback timings (S5 10 s warmup and 30 s measured; loopback 5 s and 20 s).
- **Builds:** [build.py](build.py), adapted from #715's. `reno` and `cand` rebuild byte-identical to #715's binaries (`cda8feb1…`, `b5defc02…`), and so do `relay-linux` (`24ee941d…`) and the launcher (`a6146d62…`); `build.py` asserts it. Copied byte-identical from #715 and checked by `build.py`: [run.py](run.py), [stage_run.py](stage_run.py), [localize.py](localize.py), [pack.py](pack.py), [launch/](launch/), [relay/](relay/) and the [timeline overlay](timeline/).
- **Runner:** fixture observations go through #715's unchanged `run_case_x`. A variant whose name ends in `-timeline` gets `TIMELINE_OUTPUT` for its sender, which enables both #715's overlay (when built in) and this record's recorder.
- **New measurement-only code:** the wake-path recorder ([wake/wakerec.go](wake/wakerec.go), [wake/wake_hooks.go](wake/wake_hooks.go)), the analysis ([wake/ana/](wake/ana/)), the synthetic harness ([wake/harness/](wake/harness/)), and the aids [art.py](art.py), [matrix.py](matrix.py), [wake.py](wake.py), [rules.py](rules.py) with [rules_test.py](rules_test.py), and [sync.sh](sync.sh). No transport source is changed outside exported, measurement-only build trees.

## The instrument

The [timeline overlay](../2026-10-05-bbr-linux-redemonstration/timeline/) of #715 measures lateness: the time from a paced stop's deadline to the first opportunity at or after it. The connection resets one shared Go timer for several deadlines, wakes for reasons other than the timer, and processes packets and lifecycle work before it sends. Neither a `Timer.C` value nor its receipt time marks when the runtime fired the timer. D3 therefore requires an instrument that independently observes, per paced stop, the timer generation armed, the runtime's timer fire, when the connection goroutine became runnable and began running, the wake source, the loop work before the opportunity, and the opportunity itself.

The candidate instrument has two parts.

- **Recorder** ([wake/wakerec.go](wake/wakerec.go)), measurement-only, in the sender only, over the measured window. On the connection goroutine it stamps with `runtime.nanotime`:
  - every timer arm in `maybeResetTimer` (a new generation each time), with the armed deadline, the pacing deadline and the blocked mode (normal, congestion-limited, hard-blocked), so resets, superseded deadlines and deadlines folded into an earlier one are visible;
  - which run-loop `select` case returned;
  - every `sendBounded` opportunity (its `now`) and exit (paced deadline, stop reason, #715's state class, pacing rate).
  
  To keep its own cost negligible at loopback rates, it records arms and wake cases only while a paced or pacing deadline exists, opportunities only while a paced deadline is pending, and exits only when they change the pending deadline or close a chain of arms. Those are exactly the events the late-event rule reads. It also keeps window aggregates over every opportunity: time in each #715 state class between opportunities, time inside opportunities, and lateness with the pacer credit discarded during it (lateness × pacing rate). Files: `send.wake.events` (binary), `send.wake.json` (window, clock offset, aggregates).
- **Go execution trace** over the same window (`runtime/trace`, started 50 ms before the window). It supplies what the recorder cannot see: every state transition of the connection goroutine, with the blocking reason and stack, and, for each unblock, the context of the unblocker. An unblock from scheduler context (no goroutine), or from the runtime's timer code, is the runtime running the timer; an unblock from a goroutine names that goroutine's stack (for example the transport's packet handling). File: `send.wake.trace`.

The [analysis](wake/ana/main.go) finds the connection goroutine (the one blocking in a `select` inside `(*Conn).run`), converts recorder stamps to trace time through the trace's clock snapshots (both are the runtime's monotonic clock), and applies #715's pending-deadline rule to the recorder's events. It then splits each late interval `[deadline, opportunity]` by the goroutine's state:

| Part | Goroutine state in the interval |
| --- | --- |
| **delivery** | blocked in the run-loop `select` while the timer was armed at or before the deadline: the runtime had not yet woken the goroutine. Split by what ended the wait: the timer, or another wake (packet, send queue, scheduled sending, …) that came first. |
| **unarmed** | blocked in the run-loop `select` while the timer was armed after the deadline: nothing was due to wake it. |
| **scheduling** | runnable but not running. |
| **loop** | running or in a syscall (`loop_running`), or blocked anywhere other than the run-loop `select` (`loop_blocked`). |
| **ambiguous** | no trace state known. |

It also reports, per observation: the delay from each timer arm to its runtime fire (separately for pacing arms), run-loop wakes by source, `select` case counts, chain bookkeeping (arms, folded and superseded deadlines per late event) and the goroutine's state shares over the window.

**Variants.** `cand-wake-timeline` is `d0fabc4d` with #715's overlay (unchanged) and both parts: the S5 instrument. On loopback the trace itself costs about 13% goodput and #715's overlay about 7% (see the disclosures), so the loopback rule uses `cand-lb-timeline`: the recorder alone, trace off and without #715's overlay. The loopback rule needs only the recorder's aggregates. `cand-notrace-timeline` (#715's overlay and the recorder, trace off) was used only in excluded smoke runs. The registration names the instrument only after Stage 0 passes.

## Registration: Stage 0, instrument preflight

No attribution. Stage 1 runs only if Stage 0 passes; otherwise the ticket ends with an **instrumentation gap**.

### Synthetic cases

The [harness](wake/harness/main.go) runs a loop shaped like the connection's: arm one timer, block in a `select` on the timer and a packet channel fed by a UDP read goroutine (as the transport's listen loop feeds the connection), take `now`, record an opportunity and a paced exit with the next deadline one millisecond after `now`. It links the same recorder file (only the package clause differs; `build.py` writes it). It writes its own ground truth from the same pending-deadline rule. Each case runs once for 10 s on the sender's cores (8–11), with an injected delay δ = 300 µs, and the same analysis reads it.

| Case | Injection | Recovered when (rules.py `recovery`) |
| --- | --- | --- |
| Delayed delivery | The timer is armed δ after the deadline the recorder logs, so relative to the log the runtime fires it at least δ late. | Verdict **delivery**, and ≥ 95% of late events show delivery-by-timer ≥ δ. |
| Delayed scheduling | GOMAXPROCS 1. A second timer, 2 µs after the loop's, wakes a goroutine that takes the P and spins for δ, so the loop's goroutine is runnable at least δ before it runs. | Verdict **scheduling**, and ≥ 95% of late events show scheduling ≥ 0.9δ. |
| Delayed handler | After waking, the loop spins for δ before taking `now`. | Verdict **loop**, and ≥ 95% show loop running ≥ 0.9δ. |
| Reset | Repeating cycles: an early packet wake 0.5 ms before the deadline (re-arm with the same deadline); a folded arm 0.4 ms before the deadline; an early packet wake after which the deadline is superseded by one 0.3 ms later. | Arms, folded and superseded counts equal the truth for ≥ 99% of matched late events, summed lateness equals the truth within 0.1%, and all three kinds occur. |
| Non-timer wake | The timer is armed 5 ms after the logged deadline, and a packet sent δ after the deadline wakes the loop. | Verdict **delivery**, and ≥ 95% of late events end with a packet wake, with delivery-by-other ≥ 0.9 × (send time − deadline). |

In every case the analysis must be usable (trace covers the window; ≤ 0.1% of recorder opportunity and wake stamps outside a running interval of the goroutine; ≤ 2% of lateness ambiguous) and must find ≥ 99% of the truth's late events whose deadline and opportunity lie in the recorded window (the first chain after recording starts is excluded, since its earlier arms predate the window). The verdict is the Stage 1 share rule below, applied to the case's lateness. The delivery and non-timer cases lie to the analysis about the armed time on purpose: they test that the analysis recovers late delivery and non-timer wakes, which a correct runtime would not produce on demand.

### Perturbation

- **S5 (decisive):** arms `cand` (plain) and `cand-wake-timeline`, both workloads, three blocks, seeds 9851–9853, #712's rotation ([matrix.py](matrix.py) `preflight`). Per block, instrumented ÷ plain goodput. **Pass** when the median ratio is at least 0.99 in each workload (D3's perturbation check).
- **Loopback:** arms `cand` and `cand-lb-timeline`, DATAGRAM, three blocks (`preflightlb`), same criterion. A failure ends only the loopback rule, as **inconclusive (instrument perturbation)**.
- Contaminated or unusable preflight blocks are rerun once under the same rule as the stages.

## Registration: Stage 1, discrimination

### S5

- **Question.** Why does the Linux sender reach full-quantum pacing deadlines late on S5?
- **Hypothesis.** Timer delivery: the runtime fires the pacing timer late.
- **Competitors.** Goroutine scheduling (the timer fires on time, but the connection goroutine runs late); loop work (the goroutine runs on time, but other connection work precedes the opportunity); unarmed waits (the timer was not armed for the deadline). Mixed causes are allowed for.
- **Runs.** S5, both workloads, four blocks, seeds 9861–9864, `cand-wake-timeline` only ([matrix.py](matrix.py) `s1`); 8 observations.
- **Usability** (rules.py `ana_usable`). The analysis completes; the trace parses and covers the window; ≤ 0.1% of recorder stamps fall outside a running interval; ≤ 2% of lateness is ambiguous; and the analysis's late count and summed lateness agree within 1% with #715's overlay over the same window. An observation with under 0.1 s of lateness is **no lateness**.
- **Share rule** (rules.py `shares_verdict`). Over all of an observation's late intervals, the shares of total lateness held by delivery (by timer and by other wakes together), scheduling, loop (running and blocked) and unarmed. A cause **leads** with at least 0.5 while every other cause is below 0.25; two or more causes at 0.25 or more is **mixed**; anything else is **inconclusive**. These are #715's timeline thresholds.
- **Verdict.** Per workload, the verdict shared by at least three of four usable blocks (with at least three usable); fewer than three usable is an **evidence gap**. The stage verdict is **timer delivery**, **goroutine scheduling**, **loop work** or **unarmed waits** only when both workloads agree; otherwise it is **inconclusive**, reported per workload, mixed components included.
- **Escalation.** A workload with exactly two of four agreeing usable blocks gets two more blocks (seeds 9865–9866), once.
- **Descriptive, not decisive.** The split of delivery between timer-ended and other-ended waits (and the other wakes' sources), the delay from each pacing arm to its runtime fire, per-event leading parts, run-loop wakes by source, `select` cases, the goroutine's state shares, and instrumented goodput against #715's readiness candidate.
- **Stage 2 gate.** Stage 2 runs only on **timer delivery**.

### Bare Go timer loop (context only)

The harness's `bare` case: the same loop without the transport and without injection, the next deadline one millisecond after each opportunity, on cores 8–11. Five 30 s runs, plus five `barepkt` runs in which a separate peer process on cores 4–5 sends 4,000 datagrams a second (about the S5 ACK rate) to a read goroutine that counts them and never wakes the loop. It shows what the runtime delivers in isolation, and with network wakeups elsewhere in the process. It is context, never proof of the loaded transport's cause. At most ten runs.

### Loopback DATAGRAM

The S5 fluid-link decomposition needs a fixed bottleneck and relay queue samples, which loopback lacks; it is not applied here.

- **Runs.** Loopback DATAGRAM, four blocks, `cand-lb-timeline` ([matrix.py](matrix.py) `s1lb`); 4 observations.
- **Measures** (rules.py `lb_observation`, from the recorder's aggregates over the window W). The shares of W spent after paced stops (paced waits, lateness included), credit waits, congestion-window waits, immediate continuations (loop work between opportunities), application waits and inside opportunities; **exposure** = lateness ÷ W; **loss** = the pacer credit discarded during lateness (lateness × pacing rate at the stop), as a share of what frozen Reno delivers over W (#715's readiness median, 4,637.3 Mbit/s).
- **Rule.** With the deficit Δ = 0.142 (#715's readiness median ratio 0.858 for this cell): **timing-limited goodput** when loss ≥ 0.5Δ; **timing exposure** when exposure ≥ 0.01 and loss < 0.25Δ; **no timing exposure** when exposure < 0.01 and loss < 0.25Δ; otherwise **inconclusive**. The workload verdict needs three of four blocks. At about 4 Gbit/s, one quantum per millisecond cannot be the whole explanation, so a timing-exposure finding alone establishes no cause.
- **Synthetic cases** (rules_test.py `Loopback`): paced waits with discarded credit accounting for the deficit; credit waits; processing delay; exposure without loss; and a mixed case between the thresholds.
- **Perturbation.** If the loopback preflight fails, the rule ends as inconclusive. The median instrumented goodput is also reported against #715's readiness candidate (3,980.9 Mbit/s).

## Registration: Stage 2, one contract-preserving intervention

Runs only on a Stage 1 **timer delivery** verdict. The mechanism is registered after Stage 1 and before any comparative data, and it addresses the delay Stage 1 found: a BBR-only wake path with sub-millisecond precision on Linux. D08's deadline computation, one-quantum credit cap, quantum release and 2Q pending bound stay unchanged, as do Reno's path and other platforms. Registered now, from D3:

- **Equivalence.** Fixed-input policy equality: a frozen-policy oracle feeds identical timestamped model, credit and opportunity inputs to `d0fabc4d` and the new revision and requires identical admissions, deadlines, credit and recorded decisions. Live timing may differ by design; the change must keep truthful clocks, D08 deadlines and admission bounds, pending-work accounting, ownership and lifetime, cancellation, ordering and receive fairness.
- **Gates before any comparative data.** The oracle; wake-path lifecycle tests (cancellation, stale and reset wakes, non-pacing opportunities, rate and quantum decreases, authorized exemptions); bound checks separating admission, pending work and wire bursts under changing rates; and #714's inherited correctness, race and native-coverage gates. Performance never stands in for a gate.
- **Arms.** S5, both workloads, six blocks, seeds 9871–9876, #712's rotation, `perf stat` (#712's events) on both endpoints of every arm as in #714: frozen Reno; A/A frozen Reno; `d0fabc4d` instrumented (the base); a second `d0fabc4d` instrumented (the **BBR A/A** control); the new revision instrumented; Reno on the new revision (plain); and the diagnostic 2Q arm. 7 × 2 × 6 = 84 observations.
- **Metrics,** per block, paired against the same block's base: the delivery deficit (1 − goodput ÷ the block's frozen Reno) as a difference; lateness seconds per window as a ratio (undefined, so unusable for that metric, when the base has none); pacing wakes per useful GiB (run-loop wakes delivered by the pacing wake path while a paced deadline was pending; the counting for the new path is registered with the mechanism), kept separate from send opportunities, as a ratio; sender CPU per useful GiB as a ratio. The BBR A/A arm's paired statistics give each metric's noise range `[min, max]` over usable blocks.
- **Keep** (rules.py `s2_workload`, `s2_outcome`). In both workloads, with at least five of six usable blocks: the median deficit difference and the median lateness ratio both fall below the A/A minimum; the medians of wakes and sender CPU per useful GiB are not above the A/A maximum; every observation passes receiver integrity; and Reno on the new revision passes #714's preservation metric (user-plus-kernel cycles per useful GiB at each endpoint, workloads pooled, median inside frozen Reno's A/A range).
- **Other outcomes.** **Inconclusive** first, with fewer than five usable blocks for any metric in a workload, or when more than 10% of the new arm's run-loop wakes come from a source absent (under 1%) in the base and not named by the mechanism's registration. **Negative** when receiver integrity fails, Reno on the new revision lies above its A/A range, or either workload moves beyond the A/A range in the wrong direction on any metric. **Keep** as above; a kept result with Reno on the new revision below its range is inconclusive, as in #714. **Null** when every metric's median lies inside its range in both workloads. Any other combination is inconclusive. Bottleneck overflow is reported beside every outcome. Loopback is measured only diagnostically and never affects keeping.
- **Synthetic cases** (rules_test.py `Stage2`): improvement in both workloads; one-workload regression (deficit, CPU or wakes); insufficient observations; zero lateness; a changed wake-source mix, with and without a registered source; preservation failure (above and below); integrity failure; null; and a partial movement.
- **Diagnostic 2Q arm (never kept).** `d0fabc4d` with the pacing-credit cap raised from Q to 2Q, which breaks D08; the 2Q pending-work bound is unchanged. It runs only alongside Stage 2, in the same blocks, paired against the same base, and records queue-limited admissions, wire bursts and bottleneck overflow. It reports this treatment's measured response, never becomes the next ticket's input, and authorizes nothing.

## Inventory, contamination and endings

- **Observations.** Stage 1: 8 S5 + 4 loopback. Stage 2 with the diagnostic arm: 84. Together 96. Reruns and escalations together are capped at 24, so the stages stay within 120 Linux observations. The bare-timer control is bounded separately (ten 30 s runs). As in #715, preflight and smoke runs are excluded from every statistic and from the cap, and are listed in the record; the Stage 0 preflight is 12 S5 and 6 loopback observations plus its reruns.
- **Contamination.** #712's test (localize.py `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample), handled by the operator's #714 rule: a block holding a contaminated or unusable observation is rerun once with the same seed under `observations-rerun`, and the rerun replaces it only if usable and uncontaminated (rules.py `choose_block`). Harness runs report the same test.
- **Endings.** Every ending reports to [Test the remaining BBR per-packet CPU leads and the receiver CPU measure](https://github.com/the-sarge/quic-go-fast/issues/735) with an identified surviving revision (`d0fabc4d` unless a change is kept) and its unresolved evidence: kept; null; negative; inconclusive; instrumentation gap; scheduling or loop-work cause found (no intervention built); a mechanism that cannot be built contract-preservingly; failed gates; or exhausted budget. A scheduling or loop-work finding, with the loopback rule's outcome, is the evidence for or against a bounded redesign of the connection-loop/send-queue hand-off.

## Disclosures made before data

All of these runs are excluded from every statistic and retained.

- **Feasibility (scratch, not retained in this record).** On an idle synthetic loop on `minimax`, timers armed 0.2–1.5 ms ahead fired on the next whole millisecond after the deadline, and the waiting goroutine ran 0.5–2.6 µs after the fire. Timer fires showed as unblocks from scheduler context, and packet wakes as unblocks with the read goroutine's stack.
- **Instrument revisions.** The first smoke recorded every arm, wake, opportunity and exit: on loopback that grew sender RSS to 1.7 GiB. The recorder then dropped events that cannot bear on a late opportunity; a later revision added the window aggregates and moved the window-end hand-off into every hook, because a recorder that never records after the window never wrote its files. `cand-lb-timeline` was added after `lbsmoke2` showed #715's overlay costing about 7% on loopback.
- **Smoke values seen.** Development harness runs (3 s each, an earlier recorder) recovered all five synthetic cases, and the bare loop's pacing timers fired a median 53 µs late at a 1 ms cadence. S5 STREAM `cand-wake-timeline` gave 87.4, 87.0 and 88.0 Mbit/s (#715's readiness candidate: 87.9). On `smoke2` it read 24,951 late events and 2.9 s of lateness; delivery held 0.984 of it (timer 0.923, other wakes 0.061), scheduling 0.009 and loop 0.007; pacing-arm fires were a median 107 µs late (p90 165 µs). On `smoke3` the recorder's lateness matched #715's overlay (24,921 against 24,922 events; 2,940.8 against 2,940.9 ms). On loopback DATAGRAM, the traced variant gave 3,403–3,460 Mbit/s and `cand-notrace-timeline` 3,727–3,730, against 3,965–4,007 for the plain build; `cand-lb-timeline` gave 4,005–4,057, with no paced stop and no lateness, credit waits 0.44, inside opportunities 0.41 and immediate continuations 0.10 of the window. **The S5 smoke already reads as timer delivery, and the loopback smoke already shows no timing exposure.** The rules above were written after these were seen. Their thresholds repeat #715's (0.5 and 0.25) or follow from D3 (0.99); the loopback thresholds (0.5Δ, 0.25Δ, 1% exposure) were set without tuning to the smoke, which sits far from every one of them.

## Order

`build.py`, `sync.sh`; then on `minimax`, under `taskset -c 1-3`: `wake.py synthetic`; `matrix.py preflight`, `matrix.py preflightlb`; then, if Stage 0 passes, `matrix.py s1`, `wake.py analyze s1`, `matrix.py s1lb`, any reruns and the escalation, and `wake.py bare`. Then `wake.py stage0`, `s1`, `s1lb` and `bareview` anywhere, and `rules_test.py`. Stage 2, if it runs, is registered in its own section before its data.
