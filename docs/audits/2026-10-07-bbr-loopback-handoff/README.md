# What limits BBRv3's loopback goodput on Linux

**Date:** October 7, 2026, America/New_York. **Scope:** [Test whether the send-credit hand-off limits BBRv3's loopback goodput](https://github.com/the-sarge/quic-go-fast/issues/738), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D4 ("Ticket F") of the [accepted decision](https://github.com/the-sarge/quic-go-fast/blob/232a6b76/docs/audits/2026-10-07-bbr-r8-qualification-decision/README.md#d4--ticket-f-what-limits-bbrv3s-loopback-goodput). Measurement, attribution and, only for a workload with an addressable hand-off finding, one contract-preserving change: no production merge, paid resource, campaign resumption, default-controller change, host-setting change or ledger change.

Branch `codex/bbr-loopback-handoff`, based on `232a6b76` (the accepted decision). The starting revision is **r8, `95f5b6b7`**.

**Status: complete. The ticket ends in Stage 0 with an instrumentation gap in both workloads**, under the registered rule; Stages 1 and 2 did not run. The sections from [What stays fixed and what changes](#what-stays-fixed-and-what-changes) to [Order](#order) were written, with the instrument, the analysis and the rules and their synthetic cases, before any counted observation. Excluded development runs that preceded the registration are disclosed in [Disclosures made before data](#disclosures-made-before-data).

## Answer

**The registered instrument failed its perturbation check in both workloads, so under D4 the ticket ends with an instrumentation gap: no Stage 1 attribution, no remedy attempted, and r8 (`95f5b6b7`) survives unchanged.** On a quiet `minimax`, the hand-off recorder delivered 0.977 (STREAM) and 0.982 (DATAGRAM) of the plain build's loopback goodput, against D4's 0.99 floor, in five clean blocks each. It did recover all five synthetic cases in both workloads.

**The diagnostic 4Q arm is inert in both workloads.** Its gates passed, including detection of an injected second violation, but in the registered activation runs no admission took pending work above 2Q: the maximum was 0.17 Q (DATAGRAM) and 0.19 Q (STREAM). Raising the 2Q bound changes nothing on this fixture.

**What the failed instrument showed, descriptively and not as attribution:** both workloads hand the send worker one packet per queue entry, the local credit was never refused, and every local wait was a full eight-entry send queue. On DATAGRAM the recorder read hand-off (queue) in all five preflight blocks; on STREAM it read inconclusive in all five, with the loop's waits split between the full queue (0.20 of the window) and the application writer (0.20). These readings match the excluded development runs, but they come from an instrument that perturbs goodput by about 2%. D4 does not let them stand as Stage 1 findings.

**Endings (D4):** credit response **inert**; remedy status **not attempted**; Stage 1 attribution **instrumentation gap** in both workloads.

## Stage 0 result: instrumentation gap

Run on October 7 (UTC) after the registration commits (`791208b5`, `c5e0916c`), every block started only after 60 s of quiet fixture cores; [results.json](results.json) `stage0`. All 32 observations exited cleanly with receiver integrity and none was contaminated.

- **Synthetic cases: all recovered in both workloads.**

  | Case | DATAGRAM | STREAM |
  | --- | --- | --- |
  | Slow worker | worker-bound; worker busy 1.00, submission mean 22.4 µs | worker-bound; 1.00, 22.3 µs |
  | Delayed local signal | hand-off; local waits 0.75, `j_hh` 0.52 | hand-off; 0.65, `j_hh` 0.45 |
  | Busy loop | loop-bound; loop running 1.00, local waits 0.00 | loop-bound; 0.99, 0.00 |
  | Congestion-window waits | window-bound; window waits 0.49 | window-bound; 0.51 |
  | Mixed | hand-off + window-bound; local 0.32, window 0.31 | hand-off + window-bound; 0.25, 0.31 |

- **Perturbation: failed in both workloads.** Instrumented ÷ plain goodput per block: STREAM 0.975, 0.977, 0.983, 0.978, 0.976 (median **0.977**; plain median 4,463 Mbit/s); DATAGRAM 0.998, 0.980, 0.995, 0.982, 0.978 (median **0.982**). The excluded `devsmoke7` runs of the build before the last two recorder changes had read 1.004 and 1.001; the registered binary differs in code as well as placement, and the registered check decides.
- **Diagnostic arm: gates passed; activation inert in both workloads** (no admission above 2Q; maximum pending 0.17 Q DATAGRAM, 0.19 Q STREAM). Under the registration it would still have run in Stage 1, which did not run.

### Descriptive only: what the failed instrument recorded on r8

Medians of the five preflight `cand-hand-timeline` blocks. These are not Stage 1 evidence.

| Measure | DATAGRAM | STREAM |
| --- | --- | --- |
| Packets per queue entry | 1.00 | 1.00 |
| Credit refusals; maximum pending | 0; 0.17 Q | 0; 0.19 Q |
| Worker busy `u_w`; loop running `u_c` | 0.757; 0.569 | 0.701; 0.583 |
| Blocked on a full queue; on the application; on window or pacing | 0.386; 0.011; 0.032 | 0.202; 0.196; 0.015 |
| `j_wb`, `j_hh`, `j_ov`, `j_lb` | 0.361, 0.025, 0.377, 0.192 | 0.187, 0.015, 0.363, 0.221 |
| Local waits per second; mean local wait | 76,300; 5.4 µs | 22,500; 9.7 µs |
| Verdict in each block | hand-off (queue), 5 of 5 | inconclusive, 5 of 5 |

## Report to the next tickets

- **Surviving revision: r8, `95f5b6b7`**, unchanged. Nothing was kept, so under D5 [Re-demonstrate the loopback-tested BBRv3 revision on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/739) closes as **not run**, and under D6 [Attribute the BBRv3 candidate's memory and S6 latency flags on the final revision](https://github.com/the-sarge/quic-go-fast/issues/740) attributes on r8 against #736's flags.
- **For the next decision:** the loopback goodput flags (STREAM 0.943, DATAGRAM 0.854) and the loopback DATAGRAM sender CPU flag stay **unresolved, missing evidence**: an instrumentation gap, not a failed intervention or a finding against BBRv3. The 4Q arm is **inert**, so this ticket gives no evidence for a D08 2Q-profile decision: on this fixture the 2Q credit never binds.
- **Leads, not findings** (development runs and source inspection, disclosed below): BBR hands off one packet per entry at loopback rates because its pacer accrues about one packet of credit between opportunities, so the eight-entry send queue binds and the two goroutines exchange every packet. A queue-side, BBR-only change (for example a deeper BBR send queue, or coalesced availability signals; in one development run, coalesced signals raised DATAGRAM goodput) is the natural candidate for any later remedy. STREAM's loop also waits on the fixture's writer for about a fifth of the window.
- **If the operator wants Stage 1 anyway**, it would be a recorded deviation that accepts a roughly 2% instrument cost, run in a quiet window of about 40 minutes. Nothing here assumes it.

## Question

On owned Linux hardware, what limits BBRv3's loopback goodput in each workload (the send-credit hand-off between the connection loop and the send worker, worker cost, loop work, window waits, or, for STREAM, r8's netpoller kick), and does one contract-preserving hand-off change remove the loopback flags without raising sender CPU or Reno cost?

## What stays fixed and what changes

- **Fixed (D4):** r8 as the starting revision; frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; #712's fixture with the causal-diagnosis heap patch, launcher, endpoint contract (GOMAXPROCS from the four-core affinity, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, reliable control stream at 1 Hz), core layout (sender 8–11, receiver 12–15, relay 4–5, runner 1–3, SMT siblings idle) and host settings on `minimax`; `GOTOOLCHAIN=go1.27.0`; #715's loopback timing (5 s warmup, 20 s measured) and S5 timing (10 s, 30 s); #712's contamination test with the operator's #714 rerun rule.
- **Builds:** [build.py](build.py), adapted from #734's. `reno`, `cand` (r8) and `prev` (`d0fabc4d`) rebuild byte-identical to the prior records' binaries (`cda8feb1…`, `b7171142…`, `b5defc02…`), and so do `relay-linux` (`24ee941d…`) and the launcher (`a6146d62…`); `build.py` asserts it. Copied byte-identical from #736's record (`3bf2245e`) and checked by `build.py`: [run.py](run.py), [stage_run.py](stage_run.py), [localize.py](localize.py), [pack.py](pack.py), [launch/](launch/), [relay/](relay/).
- **Registered binaries** (SHA-256 prefixes; receipts in `.local/bbr-loopback-handoff/bin/*-build.json`): `cand-hand-timeline` `74043bee…`, `prev-hand-timeline` `70fb8ee0…`, `cand4q-hand-timeline` `9e3c0389…`; synthetic `syn-slowworker-timeline` `ba262b80…`, `syn-busyloop-timeline` `2c28e50f…`, `syn-delaysignal-timeline` `a8a3edc2…`, `syn-cwnd-timeline` `fb5316c4…`, `syn-mixed-timeline` `16b2bc61…`. `build.py` also keeps development-only variants (`dev-*`, `syn-devmixedb-timeline`), never used by a registered stage.
- **Runner:** every fixture observation goes through #715's unchanged `run_case_x`. A variant whose name ends in `-timeline` gets `TIMELINE_OUTPUT` for its sender, which enables this record's recorder.
- **New measurement-only code:** the hand-off recorder ([hand/handrec.go](hand/handrec.go), [hand/hand_hooks.go](hand/hand_hooks.go)), the synthetic injections ([hand/synth.go](hand/synth.go)), the diagnostic arm's variant gates ([gates/pending_bound_variant_test.go](gates/pending_bound_variant_test.go), [gates.sh](gates.sh), [gates-native.sh](gates-native.sh)), and the aids [art.py](art.py), [matrix.py](matrix.py), [hand.py](hand.py), [rules.py](rules.py) with [rules_test.py](rules_test.py), and [sync.sh](sync.sh). No tracked transport source changes; every instrumented or diagnostic build is an exported tree under `.local`.

## The instrument

D4 asks for #734's loopback recorder (`cand-lb-timeline`, aggregates only) extended to observe, per refused reservation, when the connection loop began to wait for local credit; the worker's completions that returned credit, with each group's dequeue, submission start and completion; the credit signal; and when the loop resumed and reserved; plus paced stops and lateness, and the maximum pending bytes per run.

The **hand-off recorder** ([hand/handrec.go](hand/handrec.go)) does that, in the sender only, over the measured window, writing `send.hand.json`. It keeps #734's aggregates, computed the same way: time in each stop class between an opportunity's exit and the next opportunity (#734's `state_ns`), time inside opportunities, paced stops, lateness after a paced deadline (from each opportunity's own `now`), and the pacer credit discarded during it. It splits #734's "credit" class, which lumped two different waits, in two: a **credit** stop (the result waits on the local send credit's channel: `localBlocked`) and a **queue** stop (the send queue is full). And it adds the two goroutines the credit couples:

- **Connection loop.** Every blocked interval in the run-loop `select`, by the reason of the last stop (credit, queue, congestion window, paced, application supply, hard-blocked, other), and the send worker's busy time inside it.
- **Send worker.** A busy/idle clock: busy from the first dequeue after an empty queue until the queue is empty again. For one group in eight, its dequeue, submission start, submission end and credit completion.
- **Local waits.** For each credit or queue stop: whether the loop blocked, how many times it woke and blocked again before the next opportunity, and whether that opportunity handed off. For the sampled waits, the first freed capacity after the wait began (a credit completion, or a slot freed by a dequeue), the first signal after it on the channel the loop waits on (for credit the completion and its signal are one event; a queue signal before any freed slot announces no space and is not counted), and the loop's first block and its last wake before the next opportunity: a queue wait can wake on a stale token, find the queue still full and block again, so the decomposition uses the last wake and counts the earlier ones.
- **Pending local bytes.** The maximum, the largest pending ÷ Q at an admission, and the admissions that took pending above 2Q (the diagnostic arm's engagement check).
- **Entries.** Queue entries handed off, their bytes and packets.

**Cost control.** Loopback DATAGRAM hands the worker one packet per entry at about 410,000 entries a second, and the two goroutines exchange every packet, so any clock read on either side's hand-off path slows both. Counts are therefore exact, and durations are timed on a systematic sample: one exit, one opportunity, one blocked interval and one local wait in eight, plus every blocked interval of a sampled wait. Per-reason blocked time is the sampled intervals' mean duration times that reason's interval count, and the worker's busy share inside blocked time is measured on the same sample. The connection's and the worker's counters sit on separate cache lines; the only line both write holds the sampled wait in progress (two stores per sampled wait) and its first completion and signal. The aggregates are written by the first connection hook after the window, as #734's recorder hands off, because the sender can exit right after its window.

**Shares of the window W** ([rules.py](rules.py) `hand_measures`):

| Measure | Meaning |
| --- | --- |
| `u_w` | send worker busy |
| `b[r]` | connection blocked after a stop of reason r |
| `local` = `b[credit]` + `b[queue]` | connection blocked in local waits |
| `window` = `b[cwnd]` + `b[paced]` | connection blocked in congestion-window and model (paced) waits |
| `app` = `b[app]` | connection waiting for the application's data |
| `u_c` = 1 − Σ `b[r]` | connection running |
| `j_wb`, `j_hh` | local wait with the worker busy (waiting on a working worker), and with it idle (neither stage working) |
| `j_ov`, `j_lb` | connection running with the worker busy (pipelined), and with it idle (the worker starved by the loop) |
| `exposure` | lateness after paced stops ÷ W |

**Variants.** `cand-hand-timeline` is r8 with the recorder; `prev-hand-timeline` is `d0fabc4d` with it; `cand4q-hand-timeline` is the diagnostic arm with it (`handBoundQ` = 4). Plain `cand`, `prev` and `reno` carry no recorder.

## Registration: Stage 0, instrument and diagnostic preflight

No attribution. Stage 1 runs, per workload, only if that workload's Stage 0 passes; a workload whose instrument fails ends with an **instrumentation gap**, and if both fail the ticket ends there.

### Synthetic cases

Each case is the instrumented r8 build plus one measurement-only injection ([hand/synth.go](hand/synth.go); `build.py` `SYNTH`), run once per workload on loopback through `run_case_x` ([matrix.py](matrix.py) `synthetic`), and read by the Stage 1 rule ([rules.py](rules.py) `recovery`). A case is recovered in a workload when:

| Case | Injection | Recovered when |
| --- | --- | --- |
| Slow worker | the worker spins 20 µs inside every submission | verdict **worker-bound**, and the sampled submission mean is at least 20 µs |
| Delayed local signal | after the worker dequeues from a full send queue, the queue keeps reporting itself full and withholds its availability signal for 50 µs; then a helper goroutine signals (the slot itself is free on time) | verdict **hand-off**, and `j_hh` ≥ 0.25: the loop waits on local capacity while the worker, having drained the queue, idles |
| Busy loop, credit available | the connection spins 20 µs at every BBR opportunity before reserving | verdict **loop-bound** |
| Congestion-window waits | BBR's send allowance sees a congestion window of at most 12,000 bytes | verdict **window-bound** |
| Mixed | two-second phases alternate: an 8,000-byte window in even phases of the monotonic clock, the delayed local signal (100 µs) in odd ones | exactly the **hand-off** and **window-bound** states |

The delayed signal acts on the send queue's signal, not the credit's, and its recovery reads the idle-idle share rather than a decomposition component, because a hold can begin before a wait does (see the disclosures): on this fixture the local wait that binds is the queue's, and the credit is never refused (see the disclosures). It is the signal that ends the connection's local waits, which is what D4's case tests. A workload's synthetic preflight passes when all five cases are recovered in it.

### Perturbation

Arms `cand` (plain) and `cand-hand-timeline`, both workloads, five blocks, #712's rotation ([matrix.py](matrix.py) `preflight`). Per block, instrumented ÷ plain goodput. **Pass** per workload when the median ratio is at least 0.99 (D4). Contaminated or unusable preflight blocks are rerun once under the stages' rule.

### The diagnostic pending-bound arm (never kept)

r8 with ordinary pending local work bounded at 4Q instead of 2Q: both `2*e.bbr.quantum` in `packet_emission_bbr.go` become `4*e.bbr.quantum` (`build.py` `four_q`). That is its one deviation from D08. The send queue's own capacity (eight entries) is unchanged, so it can still bind first.

- **Variant-specific gates, before any measurement** ([gates.sh](gates.sh), [gates-native.sh](gates-native.sh)). #714's gate set as #734 ran it (`go vet`, ackhandler and congestion with and without `-race`, the work-count tag, the root subset with and without `-race`, the full root package; natively on `minimax`, the root subset, `-race` on the credit and variant tests, ackhandler and congestion), run in exported trees:
  - `gate-4q`: the arm, with [gates/pending_bound_variant_test.go](gates/pending_bound_variant_test.go) at `variantBoundQ = 4`, and r8's four tests that encode the 2Q number adapted to the tree's bound (`build.py` `adapt_tests_4q`: three reservation sizes restated as `variantBoundQ*2400` arithmetic, and one subtest skipped in favour of its restatement). The variant tests restate, at the tree's bound: complete pending accounting and admission (`DrainRefill`: the emission path refuses exactly at the bound with the credit's wakeup while queue slots are free, never exceeds it, and resumes on completion); Q decreasing with existing debt; unchanged pacing and congestion-window limits (#734's frozen-policy oracle with its limit generalized: no opportunity admits more than Q and no admission exceeds the bound, over 400,000 seeded opportunities); ACK, PTO and isolated-probe exemptions; ownership and exactly-once credit completion at the credit-return point; delivery sampling's local-limited reason.
  - `gate-r8`: unchanged r8 with the same variant tests at `variantBoundQ = 2`. They must pass there too: they encode the contract, not the arm.
  - `gate-m1`: the arm plus an **injected second violation** (credit returned at dequeue instead of after local acceptance). The suite must fail on it.

  The arm is **usable** only if `gate-r8` and `gate-4q` pass on the Mac, `gate-4q` passes natively, and `gate-m1` fails. Otherwise it is an **unusable diagnostic**, reported and not rerun under a different patch.
- **Activation (excluded).** One loopback run per workload of `cand4q-hand-timeline` ([matrix.py](matrix.py) `activation`). The arm is **active** in a workload when an admission took pending above 2Q; otherwise it is **inert** there, reported and not rerun under a different patch. It still runs in Stage 1, which keeps D4's inventory, and engagement is checked again in every counted run.

## Registration: Stage 1, discrimination

- **Question.** What limits BBR loopback goodput in each workload, and did r8 lower loopback STREAM goodput?
- **Hypothesis.** The local hand-off: the connection loop waits for local capacity while the worker is not saturated, so the two stages serialize through the hand-off.
- **Competitors.** The worker is saturated (per-packet worker cost); the loop is busy with capacity available (loop work); congestion-window or model waits; application supply (added here: the STREAM fixture's writer, which D4 does not list); and, for STREAM only, r8's kick.
- **Arms.** Loopback, both workloads, four blocks, #712's rotation ([matrix.py](matrix.py) `s1`): frozen Reno (plain, reference); r8 instrumented; a second r8 instrumented (the **BBR A/A** control); `d0fabc4d` instrumented; the diagnostic arm instrumented. 5 × 2 × 4 = 40 observations.
- **Usability** ([rules.py](rules.py) `hand_usable`, [hand.py](hand.py) `observation`). An observation is usable when it exits cleanly with receiver integrity (#712's `summarize`) and, if instrumented, its recorder output covers the window (within 1%), its accounting fits the window (blocked-time estimate within 5%, worker busy within 1%, worker busy inside blocked time not above it), and every reason, exit class and the opportunities have at least 100 timed samples once they have at least 800 events. A block is usable when all five observations are usable and uncontaminated.
- **States, per observation** ([rules.py](rules.py) `conditions`). With saturation at 0.90 and the other thresholds at #715's 0.25:
  - **worker-bound**: `u_w` ≥ 0.90;
  - **loop-bound**: `u_c` ≥ 0.90 and `local` < 0.25;
  - **hand-off**: `local` ≥ 0.25, with `u_w` < 0.90 and `u_c` < 0.90;
  - **window-bound**: `window` ≥ 0.25;
  - **supply-bound**: `app` ≥ 0.25.

  The verdict is the one state that holds; two or more is **mixed** (named); none is **inconclusive** ([rules.py](rules.py) `verdict`). A hand-off verdict is reported with its kind: **credit** when `b[credit]` ≥ `b[queue]`, else **queue**.
- **Workload verdict.** The r8 arm's verdict shared by at least three of four usable blocks (a mixed verdict agrees only with the same mix); fewer than three usable blocks is an **evidence gap**; otherwise **inconclusive** ([rules.py](rules.py) `majority`). The A/A arm's verdicts are reported beside it.
- **Credit response** ([rules.py](rules.py) `credit_response`), per workload. Per usable block, the arm's goodput and sender CPU per useful GiB ÷ the same block's r8 arm, read against the BBR A/A range (second r8 ÷ r8). **Unusable** when its gates failed; **inert** when its activation failed or fewer than three usable blocks engaged; **credit-responsive** when the goodput ratio exceeds the A/A maximum in at least three usable blocks and in the median; otherwise **not credit-responsive**, with the direction. A response shows only that the fixture responds to credit, not that a D08 change is necessary.
- **r8 against `d0fabc4d`, STREAM** ([rules.py](rules.py) `kick_reading`). Per usable block, `d0fabc4d` ÷ r8 goodput, read against the same A/A range. **kick** when it exceeds the A/A maximum in at least three blocks and the median, and r8 shows timing exposure (`exposure` ≥ 0.01) in at least three blocks; **r8 lower, kick not supported** when it exceeds the range without that exposure (the kick acts only at a pacing deadline); otherwise **no r8 effect detected**. These two arms are different binaries: see [Limits of the comparison](#limits-of-the-comparison).
- **Descriptive, not decisive.** Every arm's shares, joint occupancy, wait decomposition, packets per entry, maximum pending ÷ Q, and goodput and sender CPU against frozen Reno.
- **Stage 2 gate** ([rules.py](rules.py) `addressable`). Stage 2 runs for a workload whose verdict includes the hand-off state, alone or in a mix.

### Limits of the comparison

The states are whole-window shares. A pipeline that alternated between a saturated worker and a saturated loop over long phases would also read as hand-off; the mean local-wait duration and the local waits per second are reported beside every verdict to show the timescale at which the stages alternate (in development, waits of about 5 µs at about 75,000 a second: per-packet coupling, not phases).

On loopback STREAM, semantically identical recorder builds that differ only in code placement moved goodput by about 3% in development runs (see the disclosures). The BBR A/A arm runs one binary twice, so it measures run-to-run variation, not placement. Differences of that size between distinct binaries (`d0fabc4d` against r8; the diagnostic arm against r8) cannot be attributed to their semantics from goodput alone; the readings above require more than a goodput difference (timing exposure for the kick, engagement for the diagnostic arm), and this limit is reported beside them.

## Registration: Stage 2, one contract-preserving hand-off change

Runs only for workloads with an addressable hand-off finding. The mechanism is registered after Stage 1 and before any comparative data. It is BBR-only and keeps D08's 2Q bound, the credit-return points ("after local acceptance or known rejection, not dequeue and not peer ACK"), ownership, ordering and Reno's path unchanged. Registered now, from D4:

- **Gates before comparative data.** A fixed-input policy-equality oracle for admissions and credit accounting; lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown; bound checks that pending work never exceeds 2Q; and #714's inherited correctness, race and native-coverage gates.
- **Arms.** Loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r8, a second r8 (BBR A/A), the new revision and Reno on the new revision, with `perf stat` (#712's events) on both endpoints of every arm for #714's preservation metric. r8 and the new revision are instrumented, so local-wait and bottleneck shares are reported beside every outcome. 6 × 2 × 6 = 72 observations.
- **S5 non-regression screen.** r8, a second r8 and the new revision, plain, both workloads, four blocks, seeds 9881–9884. 3 × 2 × 4 = 24 observations. Usable with at least three usable blocks per workload, else **inconclusive**; a **regression** when the median goodput ratio (÷ r8) falls below the BBR A/A range or the median sender CPU per useful GiB ratio rises above it ([rules.py](rules.py) `s5_screen`).
- **Keep** ([rules.py](rules.py) `s2_workload`, `s2_outcome`), with at least five of six usable loopback blocks per workload:
  - in each workload Stage 1 attributed to the hand-off, the median deficit difference (new − r8; deficit = 1 − goodput ÷ the block's frozen Reno) falls below the BBR A/A range;
  - in the other workload, it is not above that range;
  - in both workloads, the median sender CPU per useful GiB ratio is not above the BBR A/A maximum, and every observation passes receiver integrity;
  - Reno on the new revision passes #714's preservation metric (user-plus-kernel cycles per useful GiB at each endpoint, workloads pooled, median inside frozen Reno's A/A range; `preservation_cell`);
  - the S5 screen shows no regression in either workload.

  The guard is the cost itself (CPU per useful GiB), not a wake-count proxy.
- **Other outcomes.** **Negative** when any guard regresses: integrity, preservation, sender CPU above its range, a deficit worse beyond its range in any workload, or an S5 regression. **Null** when no deficit moves beyond its range and nothing regresses. **Inconclusive** with too few usable blocks, or with partial efficacy (some but not all targeted workloads improve beyond the range). Mechanism efficacy (`efficacy`: improved, unchanged or worse, per workload) is reported separately from the keep outcome.
- **Synthetic cases** ([rules_test.py](rules_test.py) `Stage2`): DATAGRAM-only improvement with STREAM unchanged (kept when only DATAGRAM is targeted, inconclusive when both are), a non-target regression, partial improvement, a CPU regression, insufficient loopback or S5 blocks, a Reno preservation failure above and below, an S5 regression, an integrity failure, and null.

## Inventory, contamination and endings

- **Observations.** Stage 1: 40. Stage 2: 72 loopback + 24 S5 = 96. Together 136. Reruns are capped at 14, so the ticket stays within **150** Linux observations. Preflight, synthetic, activation and development runs are excluded from every statistic and from the cap, and are listed in the record: Stage 0 is 10 synthetic, 20 perturbation and 2 activation observations, plus reruns.
- **Contamination.** #712's test (localize.py `contamination`: foreign CPU on the fixture cores and siblings above 0.10 cores mean or 0.50 in any one-second sample), handled by the operator's #714 rule: a block holding a contaminated or unusable observation is rerun once under `observations-rerun`, and the rerun replaces it only if usable and uncontaminated (rules.py `choose_block`). Reruns are triggered by host state, never by outcome.
- **Endings** (D4), on two axes with the Stage 1 attribution and an identified surviving revision (r8 unless a change is kept):
  - **credit response:** credit-responsive, not credit-responsive, inert or unusable;
  - **remedy status:** kept, failed (negative or null), inconclusive, unbuildable, gates failed, or not attempted (no addressable hand-off finding).

  Stage 1 attributions (hand-off, worker-bound, loop-bound, window-bound, supply-bound, kick, mixed, inconclusive) and gaps (instrumentation gap, exhausted budget) travel with them. A credit-responsive arm with a failed or unbuildable remedy is evidence for *considering* a separate design decision on D08's 2Q profile; nothing is amended here. A credit-responsive arm with no remedy attempted shows only that the fixture responds to credit.

## Deviations

1. **Aid fix before data.** The registration commit's `matrix.py` ran three perturbation blocks; the README registered five. `c5e0916c` fixed `matrix.py` before any registered run, and five blocks ran.

2. **Stage 1 and Stage 2 run by operator decision after the registered instrumentation gap** (recorded 2026-10-07, before any Stage 1 data). The registered Stage 0 outcome stays an **instrumentation gap** in [results.json](results.json). The operator chose to run Stage 1 anyway, accepting the recorder's measured cost (instrumented ÷ plain goodput 0.977 STREAM, 0.982 DATAGRAM), and Stage 2 if Stage 1 finds an addressable hand-off. Everything else is unchanged: Stage 1's arms, usability, states, majority, credit-response and kick rules, inventory and rerun cap; Stage 2's registration after Stage 1 and before its data, its gates, arms and keep rule. Every Stage 1 reading is reported with this cost beside it. The diagnostic arm is already **inert** (Stage 0 activation), so its Stage 1 reading can only be inert. `minimax` is shared during these stages: the quiet-host wait and the registered contamination rerun rule handle foreign load.

## Limits

One Linux host and loopback endpoints. The Stage 0 result says only that this recorder perturbs loopback goodput by about 2%, more than D4 allows; it says nothing about whether the hand-off limits goodput. The descriptive readings above carry that perturbation. Loopback STREAM's sensitivity to code placement (about 3% between semantically identical builds, in development) means the perturbation check partly measures layout, not only the recorder's work; the registered rule applies regardless.

## Assets

- **Results:** [results.json](results.json) (`stage0`), [gates/](gates/).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json): every observation (the 244 development runs included), stage logs and build receipts. Binaries, credentials and exported trees are omitted; `build.py` rebuilds them.
- **To reconstruct:** in a fresh owned worktree of this branch, `build.py`, `sync.sh`, then the [Order](#order) on a Linux host with the same core layout; anywhere, `hand.py stage0` and `rules_test.py`.

## Disclosures made before data

All of these runs are excluded from every statistic and retained under `observations/` with `dev` phase names.

- **Inventory.** 244 development observations through the registered runner before the registration commit, phases `devsmoke`–`devsmoke8`, `devparts`, `devmask`, `devsyn1`, `devsync1`, `devsynb1`–`devsynb4` and `devsynm1`, on recorder and synthetic builds that the registration later replaced. None is counted, and none is used by any rule.
- **Instrument revisions and their measured cost** (instrumented ÷ plain loopback goodput, per block):
  - The first recorder ran beside #734's unchanged wake recorder. It cost STREAM about 6% (`devsmoke`: 0.936), and its STREAM output was never written, because the writer goroutine slept past the window and the sender exits as its window ends.
  - Folding #734's aggregates into one recorder and splitting the connection's and worker's counters onto separate cache lines left 2–4% (`devsmoke2`: STREAM 0.964, 0.965; DATAGRAM 0.962, 0.985; `devsmoke3`: STREAM 1.000, 0.990, 0.976; DATAGRAM 0.981, 0.977, 0.964).
  - `devparts` switched parts off: on DATAGRAM, opportunity aggregates alone cost nothing measurable (median 1.002), and blocked-interval timing, the worker hooks and the credit hooks each cost about 0.5–1%. STREAM was non-monotonic across parts (0.970–0.999).
  - Sampling the worker clock reads, moving the completion stamp out of the credit lock, integer-only admission hooks and writing from the first hook after the window gave `devsmoke4` STREAM 0.994 and DATAGRAM 0.982, and `devsmoke5` 0.994 and 0.986. Sampling blocked intervals and then exits and opportunities (one in eight) gave `devsmoke6` DATAGRAM 0.997 but STREAM 0.961.
  - `devmask` compared that sampled build, rebuilt with a constant renamed (semantically identical source, a different binary), against an always-timed build: sampled STREAM 0.992 and DATAGRAM 0.994; always-timed 0.981 and 0.989. The same source thus gave STREAM 0.961 and 0.992 in two binaries. **Loopback STREAM is sensitive to code placement at about 3%**, the basis of [Limits of the comparison](#limits-of-the-comparison).
  - `devsmoke7`, the build before the last two changes to the recorder (the last-wake decomposition and the signal definition): STREAM 1.004, DATAGRAM 1.001, all blocks clean. `devsmoke8`, the final recorder, ran while qemu VMs and CI jobs (`transfer.test`, `gridcast.test`, `golangci-lint`) loaded the fixture cores: every observation was contaminated, and its ratios (0.88–1.09) are uninformative. The final recorder's cost is therefore measured only by the registered preflight, and the runner's quiet-host wait (`matrix.py wait_quiet`) was added after `devsmoke8`.
- **Values seen for r8, clean** (`devmask`, `devsmoke7`; `cand-hand-timeline`, nine blocks). Both workloads hand the worker **one packet per queue entry** (about 410,000 entries a second), the credit is never refused (zero refusals; maximum pending 0.17–0.19 Q), and every local wait is a **full send queue**. DATAGRAM: worker busy 0.76, connection running 0.59, queue waits 0.37–0.40, `j_wb` 0.35–0.37, `j_hh` 0.02–0.03, `j_ov` 0.37–0.40, `j_lb` 0.19: **hand-off (queue)** in every block. STREAM: worker busy 0.72, running 0.61, queue waits 0.18–0.20, application waits 0.20, `j_hh` 0.01: **inconclusive** in every block. STREAM had no paced stop; DATAGRAM had 25–60 per window, with lateness under 0.001 of it. #734's loopback "credit waits" (0.44) lumped both waits; on this evidence they were queue waits.
- **Source inspection, not measurement.** At loopback rates BBR's pacer accrues about one packet of credit between consecutive opportunities, so `boundedDatagrams` builds one-packet batches (`limit` = min(budget, allowance, 20 KiB) rounded down to whole packets) and the loop hands off one entry per packet; with eight one-packet entries the send queue (`sendQueueCapacity` = 8) fills long before pending reaches 2Q = 128 KiB. The diagnostic 4Q arm may therefore never engage: its registered activation check decides.
- **Synthetic-case development** (`devsyn1`, `devsync1`, `devsynb1`–`devsynb4`, `devsynm1`). Injection strengths were raised until each case sat in its regime: at 5 µs the slow worker left STREAM's worker busy only 0.86 and the busy loop left STREAM running 0.86; a 24,000-byte window gave window waits of only 0.17–0.21. Three earlier designs of the delayed signal (delaying every credit and queue token; coalescing them; delaying only tokens sent during a wait) were not recoverable: tokens pipeline, and most queue waits end when a dequeue frees a slot that a stale token reveals. The withheld-availability design was adopted. Its delay landed in the wake component rather than the signal component, because a hold can begin before a wait does, so its recovery criterion became the hand-off verdict with `j_hh` ≥ 0.25 (dev: 0.45–0.52, against r8's 0.01–0.03). Applying the window cap and the delayed signal together let one suppress the other, so the mixed case alternates them. With one-second phases no tried strength recovered in both workloads (an 8,000-byte window: DATAGRAM only; 10,000 or 12,000 bytes: STREAM only), plausibly because after each capped second BBR re-ramps from a collapsed bandwidth estimate and the queue rarely fills; two-second phases with an 8,000-byte window and a 100 µs delay recovered in both (`devsynx2`: local waits 0.43 and 0.37, window waits 0.31 and 0.31). In the coalesced-token runs, DATAGRAM goodput rose above plain r8's (4,095–4,343 against about 3,950 Mbit/s) while queue signals were coalesced: a development hint that fewer wakes help, not evidence.
- **What the development did not change.** The Stage 1 thresholds (#715's 0.25, saturation 0.90) and the Stage 2 rules were written in [rules.py](rules.py) after `devsmoke` (whose only recorder output was the DATAGRAM run: queue waits 0.39, worker busy 0.75) and before any other recorder output was read. The supply-bound state was added after the STREAM values above were seen; with #715's 0.25 threshold it does not change r8 STREAM's development reading. The sampling of durations, the last-wake decomposition and the signal definition were changed for cost and correctness of the instrument, not to move a verdict; every development verdict for r8 is the same under every revision.
- **Gates run before data.** `gates.sh gate-r8`, `gate-4q` and `gate-m1` on the Mac ([gates/](gates/)): `gate-r8` and `gate-4q` passed every step (full root package 661 top-level passes each); `gate-m1` failed the variant tests (`DrainRefill`, `CreditReturnPoint`), the root subset with and without `-race`, and the full root package, as required.

## Order

`build.py`, `rules_test.py`, `gates.sh gate-r8`, `gates.sh gate-4q`, `gates.sh gate-m1` (Mac); `sync.sh`; `gates-native.sh gate-4q` (on `minimax`, under `taskset -c 1-3`, between stages). Then on `minimax`, under `taskset -c 1-3`, with no build or test running there: `matrix.py synthetic`, `matrix.py preflight`, `matrix.py activation`; pull the observations and run `hand.py stage0`. If a workload passes Stage 0: `matrix.py s1`, any reruns (`matrix.py rerun s1 <workload> <block>`), then `hand.py s1`. Stage 2, if it runs, is registered in its own section before its data.
