# BBRv3 qualification decision after the pacing-wake and CPU tickets

Status: draft decision set, revised after consideration, for [Decide whether the timing- and CPU-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/737), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-07. Revised after a multi-agent consideration (RAS run `20261007T154721-a9ee55e06ebcd15a7f10d802`); the [disposition table](#consideration-dispositions) records every finding. Awaiting final acceptance. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-r8-qualification-decision`, based on `3bf2245e` (the [r8 Linux re-demonstration](../2026-10-06-bbr-r8-linux-redemonstration/README.md) record). That record's history contains the [pacing-wake record](../2026-10-05-bbr-pacing-wake/README.md), the [per-packet CPU-lead record](../2026-10-06-bbr-per-packet-cpu-leads/README.md) and the [previous decision](../2026-10-05-bbr-intervention-qualification-decision/README.md). The surviving revision is **r8, `95f5b6b7`**. The earlier decisions and Mac records live on sibling record branches and are linked by commit.

## Question

On the evidence from the pacing-wake, per-packet CPU and re-demonstration tickets, with the preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration (including the connection-loop/send-queue hand-off), open a separate contract-change design decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged D1 advancement condition; preservation flags still block. Dispose of the attributed cells recorded in the previous decision's D2, any newly attributed cost and the remaining blockers, on every outcome of its terminal routing table. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability.

## Normative constraints (quoted)

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": "the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload)"; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers." D2: "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3, titled "Preservation flags stay blocking; resolve them with metric-specific evidence": "a raised preservation flag blocks advancement. Instruction agreement and A/A overlap are recorded as **noise-compatible or inconclusive**, never as noise attribution, and they never clear or except a preservation flag." The next readiness record "must register, before its data, metric-specific measurement-adequacy and discrimination rules for any raised preservation cell", including for latency "enough control replies per run for a usable tail estimate". "None may pass solely by range membership." D6: "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Any necessary contract change is a separate explicit design decision."

[BBRv3 design](../../designs/bbrv3.md), pacing and pending local work: "Bound ordinary pending local work at **2Q bytes**, including producer reservation, queue entries, worker-dequeued groups, in-service GSO/sequential fallback and already accepted prefixes until completion is accounted. … Return credit after local acceptance or known rejection, not dequeue and not peer ACK." "Tight credit can reduce throughput under worker delay, as the prototype showed. Two Q is the selected compromise; increasing it later requires evidence and a recorded profile change, not automatic platform-specific tuning."

## Evidence base

All readiness figures are from the [r8 Linux re-demonstration](../2026-10-06-bbr-r8-linux-redemonstration/README.md) and its [summary.json](../2026-10-06-bbr-r8-linux-redemonstration/summary.json) and [attribution.json](../2026-10-06-bbr-r8-linux-redemonstration/attribution.json), on r8 against frozen Reno `e4f322cb` on `minimax` (AMD Ryzen AI MAX+ 395, Linux 7.0, four isolated physical cores per endpoint): 110 readiness and 68 attribution observations, all exiting cleanly with receiver integrity, none contaminated. Ratios are medians of five per-block ratios against the same block's Reno, with blocks crossing the limit in parentheses. The decisive figures below were checked against the two JSON files.

### Advancement condition, checked on r8 (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.02× [12.18–15.51] STREAM, 14.58× [12.44–16.22] DATAGRAM, 5/5 pairs each; sender CPU per useful GiB 0.087 / 0.088× Reno | Yes |
| 2. Every flag passes, or is attributed and excepted | One cell attributed (S6 STREAM p95, Up policy), not excepted | No |
| 3. No unresolved flag, no preservation flag | Eleven unresolved candidate cells; one Reno-on-candidate cell raised in readiness, not reproduced by its registered stage, and blocking until the operator accepts D3 | No (unresolved cells) |
| 4. Mac records reviewed, platforms named | Reviewed below; any advancement would name Linux only | Reviewed |

### Flags on r8, against `d0fabc4d` on the same host (#715)

| Status | Cell | `d0fabc4d` (#715) | r8 (#736) | Absolute, Reno → candidate |
| --- | --- | --- | --- | --- |
| Now passes | S5 goodput, STREAM / DATAGRAM | 0.917 / 0.899, attributed to sender timing | 0.972 / 0.972 | 95.9 → 93.2 / 94.4 → 91.8 Mbit/s |
| Now passes | S5 STREAM sender CPU | 1.183, unresolved | 0.935 | 10.15 → 9.48 s/GiB |
| Now passes | S5 receiver CPU, STREAM / DATAGRAM | 1.164 / 1.108, unresolved | 1.055 / 1.007 | 8.46 → 8.93 / 8.73 → 8.78 s/GiB |
| Attributed | S6 STREAM control p95 — selected ProbeBW Up policy | 1.588 | **1.550 (5)** | 125.4 → 198.7 ms |
| Unresolved (new) | Loopback STREAM goodput | 0.962, passing | **0.943 (4)**; A/A 0.997–1.000 | 4705.1 → 4434.4 Mbit/s |
| Unresolved | Loopback DATAGRAM goodput; sender CPU | 0.858; 1.111 | **0.854 (5); 1.111 (5)** | 4655.4 → 3977.0 Mbit/s; 4.52 → 5.03 s/GiB |
| Unresolved (new) | S6 DATAGRAM control p95 | 1.167, passing | **1.469 (5)** | 121.6 → 177.9 ms |
| Unresolved | S5 STREAM sender / receiver RSS | 1.235 / 1.457 | **1.261 (5) / 1.565 (5)** | 17.82 → 22.36 / 16.82 → 25.97 MiB |
| Unresolved (new) | S5 DATAGRAM sender RSS | 1.096, passing | **1.173 (3)** | 18.00 → 20.90 MiB |
| Unresolved | S6 STREAM sender RSS | 1.640 | **1.656 (5)** | 13.50 → 22.36 MiB |
| Unresolved (was attributed) | S6 STREAM receiver RSS | 1.759, delivery over two blocks | **1.891 (5)**, unresolved over four | 13.75 → 26.01 MiB |
| Unresolved | S6 DATAGRAM sender RSS | 1.403 | **1.464 (5)** | 14.00 → 20.99 MiB |
| Unresolved (new) | S6 DATAGRAM receiver RSS | 1.076, passing | **1.118 (4)** | 13.59 → 15.20 MiB |
| Preservation, not reproduced | Loopback DATAGRAM Reno-on-candidate control p95 | 0.721, passing | **2.588 (3)**, 20 replies per run; 120 s stage 0.716 [0.487–1.180] | 0.381 → 0.606 ms readiness; 0.620 → 0.444 ms in the 120 s stage |

Five cells cleared. Four candidate cells and one preservation cell became newly raised, and one cell moved from attributed to unresolved. The frozen-Reno A/A arm crosses no limit; its widest ranges are loopback control p95 (STREAM 0.68–1.85, DATAGRAM 0.47–1.62).

### What the three tickets establish

- **The Linux S5 gap was timer delivery, and a contract-preserving change removed it.** The [pacing-wake ticket](../2026-10-05-bbr-pacing-wake/README.md) showed the Go runtime firing the pacing timer a median 107–109 µs late, with delivery holding 0.982–0.985 of the lateness. r8's netpoller kick cut the S5 deficit against frozen Reno from 0.088 / 0.113 to 0.026 / 0.030 and sender CPU per useful GiB by 22% / 13%. Its registered outcome was **negative**, because pacing-timer wakes per useful GiB rose 14–16% against the operator's no-increase requirement; the operator kept it after the outcome (its deviation 3). On readiness, every S5 goodput and CPU cell now passes and the S5 timeline reads **model behaviour** in all eight blocks: about 7 µs of lateness per event, 0.0002–0.001 of capacity.
- **The remaining S5 deficit is the selected model's own probing and margin.** The deficit against capacity is 0.029–0.036, of which the model accounts for 0.019–0.024: the pacer's 0.99 margin, Down (17–20% of the window) and ProbeRTT (3.4–4.3%). That is a passing cell.
- **Three selected CPU leads failed to clear #714's rule.** The [per-packet CPU-lead ticket](../2026-10-06-bbr-per-packet-cpu-leads/README.md) ended inconclusive, inconclusive and null and reverted all three. Leads 1 and 2 cut 145–236 user instructions per packet while STREAM sender user cycles per packet rose; lead 3 was null (instructions +14 / −165, cycle ratios 1.0005 / 0.9937). The CPU cells those leads targeted now pass anyway. On r8 the S5 receiver shows no CPU-time excess under either harness.
- **Loopback DATAGRAM is not timing-bound.** On `d0fabc4d` the sender never reached a paced stop on loopback DATAGRAM. It spent 0.439–0.444 of the window in local credit waits, 0.404–0.407 inside opportunities and 0.095–0.097 in immediate continuations. In #715's loopback-bottleneck stage, neither BBR serial stage saturated (send queue 0.80, connection loop 0.58–0.67 cores), while frozen Reno's send queue did (0.88). That pattern fits two coupled stages waiting on each other through the 2Q send credit. D08 itself records that "tight credit can reduce throughput under worker delay". No stage has tested it.
- **Loopback STREAM goodput fell from 0.962 to 0.943 between revisions.** No stage examined it. r8 differs from `d0fabc4d` only in the Linux kick, which acts at a pacing deadline; whether loopback STREAM reaches paced stops on either revision is unmeasured.
- **The memory instrument has stopped discriminating.** All seven raised memory cells are steady-state excesses, and all are unresolved because no cell has four agreeing heap-site blocks. Two cells have a block whose heap-site excess at the heap peak is negative or near zero, so the heap peak does not carry the RSS excess there. Descriptively, bookkeeping (`recoveryEvidence.sent`, `beginCongestionFeedback`, `deliveryRecords`; about 2.2–2.6 MiB) leads three of the four sender cells; the S6 STREAM sender is about evenly split (bookkeeping 2.03 MiB, delivery 2.25 MiB). Delivery data (4.0–5.3 MiB) leads the STREAM receiver cells, and bookkeeping (1.42 MiB) leads the S6 DATAGRAM receiver. The other receiver cells also carry about 1.1–1.2 MiB of bookkeeping, because BBRv3 is configured at both endpoints. Doubling the heap-site blocks from two to four made classification less decisive, not more. #710 found the macOS STREAM receiver heap rise to be loss-driven reassembly; r8's S5 STREAM forward overflow is 0.210% against Reno's 0.095%.
- **S6 DATAGRAM p95 descriptively matches the Up-policy pattern.** 98.3–98.9% of samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay is 0.15–0.20 ms; S5 DATAGRAM matched-load p95 is 0.987. D2's registered rule covers STREAM only, so the cell is unresolved.
- **The Reno-on-candidate latency cell did not reproduce at an adequate sample.** Readiness gives 20 control replies per loopback run, so its p95 is near the maximum; frozen Reno's own A/A arm crossed this cell's limit at 1.615 in #715. The registered `preslat` stage (six blocks, 120 replies per run, adequacy met) reads **not reproduced**: pooled ratio 0.716, 95% block-bootstrap interval 0.487–1.180, two of six blocks above 1.

### Platform and source facts checked for this decision (source inspection, not measurement)

- **The two bookkeeping structures allocate differently.** On r8, the BBR recovery outcome ring allocates `maxRecoveryOutcomes = 32768` outcomes on first use (`internal/ackhandler/bbr_recovery.go`). It retains classified outcomes and replaces the oldest only when full, so in a long run it fills and then evicts in steady state; its footprint is fixed, and a smaller ring changes eviction age, not just storage. The delivery records (`internal/ackhandler/delivery_records.go`) grow with occupancy: `set` appends records and doubles the slot table at seven-eighths load, and `maxDeliveryLive = 25000` (`delivery_sampler.go`) limits insertion rather than preallocating. Lowering an unexercised ceiling therefore shrinks nothing. Each structure needs its own treatment, with a predicted storage change (the ring's `recordBytes` accounting gives its explicit bytes).
- **r8's kick is Linux-only.** On other platforms `pacingKick` is a no-op type (`pacing_kick_other.go`), so the darwin build behaves as `d0fabc4d`.

### Mac records reviewed

- **[C1–C4 correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) (#671)** and **[WAN-corrected re-demonstration](https://github.com/the-sarge/quic-go-fast/blob/dc252f8a/docs/audits/2026-10-03-bbr-wan-redemonstration/README.md) (#711, `fc4c1bf1`).** On the Mac, loopback and S5 goodput passed on `fc4c1bf1`; S5 sender CPU, S5 DATAGRAM receiver CPU, three mixed memory cells and two preservation CPU cells were raised. Its D2 exception covers `fc4c1bf1` on macOS only and is unchanged.
- **New-revision Mac evidence** is #715's four S5 timeline observations on `d0fabc4d`, all "model", and r8 behaves as `d0fabc4d` on darwin. New-revision Mac readiness is unmeasured. The Mac records are consistent with the Linux finding that S5 timing was a Linux runtime effect: Mac S5 goodput passed where kqueue timers are nanosecond-precise.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced** on r8. Condition 3 fails with eleven unresolved candidate cells, and condition 2 fails because the one attributed cell is not excepted.

BBRv3 is **not declined**. The S6 benefit is repeatable on Linux for `fc4c1bf1`, `d0fabc4d` and r8 (thirty pairs, all above Reno) and grew to 14.02× / 14.58× on r8; it held historically on macOS for `fc4c1bf1`. Every S5 goodput and CPU cell now passes, and the two costs that cleared were removed by contract-preserving changes. Every open cell has a testable hypothesis or a measurement gap that owned hardware can close.

Non-advancement is forced by condition 3. Continuing rather than declining is an operator choice.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | #735's three profile-selected CPU leads (inconclusive, inconclusive, null; reverted). #734's Stage 2 was **negative under the registered rule** and kept by operator deviation 3, with a demonstrated S5 benefit; its wake increase stays a disclosed cost. #734's diagnostic 2Q arm never applied its treatment and is uninformative. |
| Missing evidence | Loopback STREAM goodput; loopback DATAGRAM goodput and sender CPU; S6 DATAGRAM control p95; the seven memory cells. |
| Attributed behaviour (not shown to be an algorithm necessity) | S6 STREAM control p95 (selected ProbeBW Up policy), on three records. |
| Demonstrated contract incompatibility | None. D08's statement that tight 2Q credit can reduce throughput under worker delay makes the loopback hand-off a candidate for one, untested. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception granted now

No exception is granted, and the macOS D2 exception is not extended. With eleven unresolved cells, no exception could satisfy D1. Recorded for the next decision, without clearance:

- **S6 STREAM control p95:** 1.550 on r8, the same Up-policy cause on three records (macOS `fc4c1bf1` 1.547, Linux `d0fabc4d` 1.588, Linux r8 1.550). It is the leading exception candidate once no unresolved cell remains.
- **S6 STREAM receiver RSS:** 1.891, now **unresolved** over four blocks, after *delivery data* over two on `d0fabc4d` (1.759). Larger and unresolved, so the earlier attribution does not carry.
- **S5 sender RSS:** STREAM 1.261 and DATAGRAM 1.173, both **unresolved**; the DATAGRAM cell is newly raised.
- **Run-loop wakes per useful GiB (#734's open cost):** not a readiness flag. #734's deviation 3 kept r8 because sender CPU per useful GiB, the cost the guard protects, fell; readiness now passes it on S5 (0.935 / 0.909). The increase (pacing-timer wakes +14–16%, all run-loop wakes +6–11% per useful GiB, measured on S5 only, with `perf` attached) stays disclosed, unmeasured on other paths, and carried into production slicing, not a ticket.

### D3 — The Reno-on-candidate loopback latency cell: resolved for r8 on Linux, by operator interpretation

**The historical measurement does not change.** #736's readiness value (2.588, three blocks crossing, 20 replies per run) and its `preslat` verdict stay exactly as recorded in its `summary.json` and `attribution.json`.

**Blocking disposition, proposed for operator acceptance.** This is an operator interpretation, proposed with r8's result already known. The Linux decision's D3 required a registered, adequate-sample, metric-specific rule. It did not say which of that rule's outcomes would end blocking. The proposal:

- `preslat`'s adequacy requirement (at least 100 replies per run, 500 per arm, four usable blocks) and its outcome rule were registered before data in #715 and reused unchanged by #736. Its **not reproduced** outcome requires the 95% block-bootstrap interval's upper bound to be at most the 1.20 limit. r8's result, pooled ratio 0.716 [0.487–1.180], meets it.
- On acceptance, an adequate **not reproduced** outcome ends the cell's blocking state under condition 3 for the named revision and platform only: r8 on Linux. **Regression reproduced**, **inconclusive**, **evidence gap** or unusable outcomes leave the cell blocking.
- The estimands differ. Readiness uses the median of five per-run ratios at 20 replies per run. `preslat` uses a pooled 120 s p95 per arm with a block-bootstrap interval. The A/A arm's crossing of this cell in #715 (1.615) is context only and plays no part in the disposition.
- **Until the operator accepts this interpretation, the cell stays blocking.** It changes nothing today: eleven unresolved cells block advancement on their own.

**Prospectively,** the same mapping applies to Ticket G. Any new revision re-measures the cell in readiness. A raised value there runs `preslat` again, and only its **not reproduced** outcome ends blocking. It is not an exception and covers no other cell.

### D4 — Ticket F: what limits BBRv3's loopback goodput

One task ticket on `minimax`, from r8, under #712's core layout and host settings, with no host-setting change. Every rule, threshold, synthetic case and inventory is committed in the ticket's record before any comparative data, with executable synthetic tests and an explicit inconclusive ending for each stage. Accepting this decision does not by itself authorize measurement; the registration does.

**Stage 0 — Instrument and diagnostic preflight (no attribution).**
- *Instrument.* Extend #734's loopback recorder (`cand-lb-timeline`, aggregates only) to observe, per refused reservation: when the connection loop began waiting for local credit; the worker's completions that returned credit, with each group's dequeue, submission start and completion; the credit signal; and when the connection loop resumed and reserved. It also reports paced stops and lateness, so loopback STREAM's timing exposure is measured on both revisions, and it outputs the maximum pending bytes per run.
- *Synthetic cases.* The preflight passes only when each case is recovered: a slow worker, a delayed credit signal, a busy connection loop with credit available, congestion-window waits and a mixed case. Instrumented goodput must stay at least 0.99 of the plain build's. If no instrument passes, the ticket ends with an **instrumentation gap**.
- *Diagnostic pending-bound build.* r8 with ordinary pending local work bounded at 4Q instead of 2Q. Its one deviation from D08 is that bound. Before any measurement it must pass variant-specific gates for every other contract:
  - complete pending-byte accounting and admission under the revised bound;
  - Q decreases with existing debt;
  - unchanged pacing and congestion-window limits;
  - ACK-only, PTO and isolated-probe exemptions;
  - ownership and exactly-once credit completion;
  - #714's race and lifecycle gates.

  The gate suite must also show that it detects an injected second contract violation. Then an excluded activation preflight must show pending bytes above 2Q. A gate failure makes the arm an **unusable diagnostic**; a failed activation check makes it **inert**. Either way it is reported and not rerun under a different patch: this is the lesson of #734's 2Q arm. Engagement is checked again in every counted run.

**Stage 1 — Discriminate the loopback limit (attribution only).**
- *Question.* What limits BBR loopback goodput in each workload, and did r8 lower loopback STREAM goodput?
- *Hypothesis.* The credit hand-off: the connection loop waits for 2Q credit while the worker is not saturated, so the two stages serialize through credit return.
- *Competitors.* The worker is saturated (per-packet worker cost); the connection loop is busy with credit available (loop work); congestion-window or model waits; and, for STREAM only, r8's kick.
- *Arms.* Loopback, both workloads, four blocks:
  - frozen Reno (plain, reference);
  - r8 instrumented;
  - a second r8 instrumented (the BBR A/A control);
  - `d0fabc4d` instrumented;
  - the diagnostic pending-bound arm, instrumented (never kept).
- *Rule.* Per workload, by a three-of-four block majority, the registration fixes share rules over the Stage 0 states. They separate hand-off, worker-bound, loop-bound and window-bound outcomes. The pending-bound arm's goodput and CPU are paired against the same block's r8 arm and read against the BBR A/A range. A response shows that the fixture responds to credit, not that a D08 change is necessary. The r8-against-`d0fabc4d` STREAM comparison is read against the same range.
- *Inventory.* 5 arms × 2 workloads × 4 blocks = 40 observations.

**Stage 2 — One contract-preserving hand-off change, only for workloads with an addressable hand-off finding.**
- *Change.* A BBR-only change that shortens the hand-off. D08's 2Q bound, the credit-return points ("after local acceptance or known rejection, not dequeue and not peer ACK"), ownership, ordering and Reno's path stay unchanged. The mechanism is registered after Stage 1 and before comparative data.
- *Gates before comparative data.* A fixed-input policy-equality oracle for admissions and credit accounting; lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown; bound checks that pending work never exceeds 2Q; and #714's inherited correctness, race and native-coverage gates.
- *Arms.* Loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r8, a second r8 (BBR A/A), the new revision and Reno on the new revision.
- *S5 non-regression screen.* r8, a second r8 and the new revision, both workloads, four blocks. It has its own registered usability rule (at least three usable blocks per workload, else inconclusive) and its own discrimination rule against the contemporaneous BBR A/A range.
- *Keep rule.* Kept only when all of these hold, with at least five of six usable loopback blocks per workload:
  - in each workload Stage 1 attributed to the hand-off, the deficit against the same block's frozen Reno falls beyond the BBR A/A range;
  - in the other workload, the deficit is not worse beyond that range;
  - in both workloads, sender CPU per useful GiB is not above the BBR A/A maximum and receiver integrity holds;
  - Reno on the new revision passes #714's preservation metric;
  - the S5 screen shows neither goodput nor sender CPU per useful GiB moving the wrong way beyond its range.

  The guard is the cost itself (CPU per useful GiB), not a wake-count proxy. Mechanism efficacy is reported per workload, separately from the keep outcome. Partial efficacy that fails the keep rule reads **inconclusive**, not negative or null, unless a guard regresses.
- *Synthetic cases.* DATAGRAM-only improvement with STREAM unchanged, a non-target regression, partial improvement, a CPU regression, insufficient blocks and a Reno preservation failure.
- *Other outcomes.* **Negative** when a guard regresses or preservation or integrity fails. **Null** when nothing moves beyond the range. Bottleneck and credit-wait shares are reported beside every outcome.
- *Inventory.* 6 × 2 × 6 = 72 loopback plus 3 × 2 × 4 = 24 S5 = 96 observations.

**Inventory.** Stages 1 and 2 total 136 observations, and reruns are capped at 14, so the ticket stays within **150** Linux observations. Preflight and activation runs are excluded from statistics and listed.

**Endings.** Each ending is reported on two independent axes, with an identified surviving revision (r8 unless a change is kept):
- **Credit response:** the pending-bound arm is credit-responsive, not credit-responsive, inert or unusable.
- **Remedy status:** kept, failed (negative or null), inconclusive, unbuildable, gates failed, or not attempted (no addressable hand-off finding).

Stage 1 attributions (hand-off, worker-bound, loop-bound, window-bound, kick, inconclusive) and gaps (instrumentation gap, exhausted budget) travel with them. A credit-responsive arm combined with a failed or unbuildable remedy is evidence for *considering* a separate design decision on D08's 2Q profile. It does not show such a change is necessary, and nothing is amended by F. A credit-responsive arm with a remedy not attempted shows only that the fixture responds to credit.

### D5 — Ticket G: re-demonstrate only if Ticket F keeps a change

Blocked by Ticket F. If F keeps a change, Ticket G reruns #736's readiness stage on F's survivor, unchanged except for the revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout. It also runs #736's conditional preservation stages (`presrss`, and `preslat` under D3's mapping) and the S5 timeline. Memory and S6 latency attribution are Ticket H's. If F keeps nothing, Ticket G closes as **not run**, because a readiness rerun of unchanged r8 would duplicate #736.

### D6 — Ticket H: attribute the memory and S6 latency cells on the final survivor

Blocked by Ticket G, on the final survivor, against the latest readiness flags (G's, or #736's if G did not run). Heap-site shares at the heap peak are not the decisive instrument here: they have left every cell unresolved on four records, and they do not carry the RSS excess in some blocks. Every rule, threshold, synthetic case and inventory is committed with executable tests before comparative data.

**Stage 0 — Memory accounting instrument (descriptive) and treatment preflight.**
- *Accounting.* A measurement-only overlay samples, every second:
  - resident memory from `/proc` (`smaps_rollup`);
  - the Go runtime's disjoint `/memory/classes/…` quantities (heap objects, heap unused, heap free, heap released, stacks, metadata and other runtime memory), which partition mapped memory;
  - the explicit bytes held by the two BBR bookkeeping structures: the outcome ring's `recordBytes`, and the delivery records' slot and record capacities.
- *Rules.* The GC goal is reported as context only. Heap-object bytes may include unswept garbage, and mapped classes are not residency. Each cell's excess is reconciled against resident memory at the same sample, aligned to the measured window and to GC cycles, with an explicit unexplained residual. A residual above a registered share is an **evidence gap** for that cell.
- *Synthetic cases.* A retained allocation of known size, a warmup-only spike, goroutine-stack growth, a higher allocation rate at a fixed live heap, and a case where runtime accounting fails to explain resident memory. The last must receive no causal label.
- *Perturbation.* Instrumented goodput must stay at least 0.99 of the plain build's with this overlay and the timeline overlay together.
- *Treatment preflight.* The ring treatment (below) must first pass its own gates. Its predicted storage reduction must exceed a registered detectability threshold derived from the BBR A/A arm's RSS range; otherwise it is not run, and the explicit structure accounting is reported descriptively. An excluded activation run must show the measured allocation reduction. A gate failure makes the treatment an **unusable diagnostic**; a missing reduction makes it **inert**. Delivery records get no capacity treatment: they already grow with occupancy, and reducing their retention would change delivery evidence. Their share is reported from the explicit accounting only.

**Stage 1 — Arms.** Each raised cell's path and workload, S5 and S6 both workloads, four blocks, every arm with the Stage 0 overlay:
- frozen Reno;
- the final candidate;
- a second final candidate (the **BBR A/A** control), identically instrumented;
- the **ring treatment** (diagnostic, never kept): the final candidate with a smaller outcome ring at a registered size. Its one deviation is earlier eviction of retained outcomes, a change of a declared bound. Before measurement it must pass recovery-equivalence and indexing tests over a declared equivalence domain, and #714's race and lifecycle gates; the suite must detect an injected second violation. Engagement is checked in every counted run. A response is evidence for a later design-bound decision and authorizes nothing;
- the **receiver-controller arm**: the final candidate build at both endpoints, with the receiver's controller set to Reno and the sender unchanged. This isolates controller selection at the receiver without changing its build. It is **comparable** only when useful delivery, packets per useful GiB, feedback and control traffic, and forward overflow and reordering stay within the BBR A/A range. Otherwise the arm is **confounded** for that block.

**Statistics.** Per block, the baseline excess is the candidate's resident memory minus frozen Reno's. A treatment's removal is the paired reduction in its arm's resident memory against the same block's candidate, as a fraction of the baseline excess. The BBR A/A arm's paired difference gives the contemporaneous variation; a removal counts only beyond it. At least three usable blocks are needed. A block whose baseline excess is non-positive or below the registered detectability threshold is unusable for fractions.

**Rule, per cell:**
- **Sender bookkeeping (ring)** when the ring treatment, engaged, removes at least 70% of the excess beyond the A/A variation, in at least three of four usable blocks.
- **Receiver-side controller state** when the comparable receiver-controller arm removes at least 70% of a receiver cell's excess on the same terms.
- **Localized to traffic-dependent live heap** when the comparable receiver-controller arm's removal is bounded below 30% by the A/A variation and the accounting places at least 70% of the excess in heap objects. Failing to detect a removal is not proof of a negligible one: the upper bound must lie below 30%. **Delivery retention** may be named only with contemporaneous site or retention-lifetime evidence (heap-site groups or reassembly-buffer bytes, sampled in the same runs) that places the excess in delivery storage. Otherwise the label stays localized, with the cause **unresolved**.
- **Runtime headroom** only when the accounting places at least 70% in heap unused and free classes and an arm or registered comparison discriminates it. Accounting shares alone never assign a cause.
- Otherwise **mixed**, **inconclusive**, an **evidence gap**, or **treatment inert or unusable**.

**S6 control p95, both workloads.** The S6 candidate and A/A arms also carry the timeline overlay and relay queue samples, so revision-matched inputs exist whether or not a memory cell is raised. D2's Up-policy rule applies unchanged to STREAM, and the same thresholds apply to DATAGRAM, using matched-load S5 p95 from the latest readiness record. Disclosed before data: #736's S6 DATAGRAM figures (98.3–98.9% in Up or Down; Cruise and Refill 0.15–0.20 ms; S5 matched-load 0.987) already meet those thresholds. A phase association neither explains the revision-to-revision rise (1.167 → 1.469) nor establishes algorithm necessity. Readiness p95 at 30 replies per WAN run is a screen; a later exception decision must weigh tail-sample sparsity.

**Inventory.** 5 arms × 2 paths × 2 workloads × 4 blocks = 80 observations, with reruns capped at 20, so the ticket stays within **100** Linux observations. If the ring treatment is not run, its 16 observations are released. Preflight and activation runs are excluded from statistics and listed.

**Endings.** Every cell ends with one of the labels above, and the revision named, and goes to the decision.

### D7 — Then a fresh decision on every outcome

A new grilling ticket, blocked by Ticket H, re-asks this question on whatever F, G and H find, including nothing kept, instrumentation gaps and inert or unusable treatments. Its options are the same as this ticket's. **Terminal routing:**

| Outcome | Route |
| --- | --- |
| F keeps a change | G re-demonstrates F's survivor; H attributes on it. |
| F ends any other way | G closes as not run; H attributes on r8 against #736's flags; F's two-axis ending goes to the decision. |
| F: credit-responsive, remedy failed or unbuildable | Evidence for considering a separate design decision on D08's 2Q profile; nothing is amended. |
| F: credit-responsive, remedy not attempted | Reported as fixture credit response only. |
| H: sender bookkeeping (ring) | Evidence for considering a separate design-bound decision; not an exception by itself. |
| H: S6 p95 attributed to the Up policy | Attributed behaviour, eligible for an exception decision. |
| H: delivery retention, with site or lifetime evidence | Attributed behaviour, eligible for an exception decision. |
| H: localized, cause unresolved; mixed; inconclusive; gap; treatment inert or unusable | Unresolved; not eligible for a cause-based exception. |
| A prerequisite gap or unusable instruments | The stage ends as a gap and its ticket closes to the next. |
| Flags persist | Reported as unresolved to the decision. |

### D8 — No paid experiment, no campaign, no Mac readiness

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) is unchanged. New-revision Mac readiness is not required: the accepted D1 allows declared-platform advancement with the Mac records reviewed, and any advancement would name Linux only. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D9 — The D08 credit-horizon amendment is dropped; BBRv1 stays parked

- **Credit horizon.** The previous decision kept a possible D08 credit-horizon amendment in fog, for consideration only after a contract-preserving remedy failed. The remedy worked, the S5 cells pass and the 2Q pacing arm measured nothing, so the question loses its trigger and leaves the map. The pending-bound question in D4 is a different part of D08 (the 2Q pending-work bound, not the one-quantum pacing credit).
- **BBRv1.** [Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open, and #673 is re-wired to the new decision ticket. The open costs are transport integration and memory, which BBRv1 would share, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the S6 benefit.** Eleven cells are unresolved.
- **Decline.** No correctness or contract failure; the two costs that cleared were removed by contract-preserving changes, and the benefit grew.
- **Grant exceptions now.** They cannot unlock advancement.
- **Keep the Reno-on-candidate latency cell blocking whatever its adequate re-measurement shows.** No evidence could then ever resolve it. The operator may still decline D3's interpretation; the cell then stays blocking, which changes nothing today.
- **Invent a new margin or rule for `preslat` after seeing r8's result.** The registered rule is applied as written.
- **Repeat heap-site attribution with more blocks or finer sampling.** Four blocks were less decisive than two; the heap peak does not carry the RSS excess in some blocks.
- **Change the memory limit, the median rule or the advancement condition.** That would weaken a gate.
- **Raise the 2Q pending bound or redesign the hand-off now.** Untested; D08 requires evidence and a recorded profile change first.
- **Reduce the bookkeeping capacities now.** A design-bound change, and the cells are unattributed.
- **Lower the delivery-record ceiling as a memory treatment.** The ceiling limits insertion and is not exercised; lowering it shrinks nothing.
- **Use a Reno-built receiver.** It changes the build as well as the controller; the receiver-controller arm isolates the controller.
- **Run the loopback and memory tickets in parallel.** A kept loopback change could change memory, and both share one measurement host.
- **Require new-revision Mac readiness.** Not required by D1 for Linux-only advancement.
- **Any paid run, host-setting change or BBRv1 work.**

## Map changes on acceptance

- A resolution comment on #737, the issue closed, and one Decisions-so-far line on #666.
- New tickets F (task), G (task), H (task) and a new grilling decision ticket, wired F → G → H → decision. #673 is re-wired from #737 to the new decision ticket.
- Fog updated:
  - the loopback hand-off hypothesis becomes Ticket F;
  - the memory composition becomes Ticket H;
  - the D08 credit-horizon amendment is removed;
  - the run-loop wake increase is recorded as a disclosed production cost;
  - the production-slicing, bookkeeping-bound and adoption-evidence fog stays.

## Consideration dispositions

RAS run `20261007T154721-a9ee55e06ebcd15a7f10d802` (adjudicated) kept the verdict: non-advancement is forced, and continuing is an operator choice. It raised twelve fixes, and all are accepted. The source facts behind F1 were checked at `95f5b6b7` (`bbr_recovery.go`, `delivery_records.go`). The figures behind F11 were checked against #736's `summary.json` and `attribution.json` (`memory.cells`, `decomposition_mib`) and #735's lead table.

| Fix | Disposition | Where | Verification and remaining dissent |
| --- | --- | --- | --- |
| F1 — High: infeasible capacity treatment | Accepted | Source facts; D6 Stage 0 and arms; Rejected | Confirmed in source: the ring is fixed, fills and evicts in steady state; delivery records grow with occupancy and the 25,000 ceiling limits insertion. The fact paragraph is corrected. A ring treatment is registered with a predicted storage reduction, a detectability threshold, gates and activation checks, and it is declared a bound deviation. Delivery records get explicit accounting instead of a treatment. An infeasible or inert treatment ends uninformative. Dissent about an RSS upper bound from ledger size is not adopted. |
| F2 — High: H controls and statistics | Accepted | D6 arms, statistics, rule, inventory | A BBR A/A arm is added. Paired per-block removal fractions, the A/A variation, a three-usable-block minimum and small-denominator handling are defined. The under-30% outcome needs an upper bound below 30%, not a missed detection. 80 observations, at most 20 reruns. |
| F3 — High: receiver substitution | Accepted | D6 receiver-controller arm and rule; D7 routing | The arm keeps the final build and selects Reno at the receiver, with comparability checks; otherwise it is confounded. Delivery retention needs site or lifetime evidence from the same runs; otherwise the cell is localized with cause unresolved and not eligible for a cause-based exception. |
| F4 — High: diagnostic gates and activation | Accepted | D4 Stage 0; D6 Stage 0 and ring arm | Each diagnostic names its one deviation and must pass variant-specific gates for every other contract, including detecting an injected second violation. An excluded activation preflight and per-run engagement checks are required. A gate failure is an unusable diagnostic, distinct from an inert treatment. |
| F5 — Medium: RSS accounting | Accepted | D6 Stage 0 | Disjoint runtime `/memory/classes` quantities, resident memory from `smaps_rollup`, GC and window alignment, an explicit residual and a failure-to-explain synthetic case. The GC goal is context only, and shares alone never assign a cause. |
| F6 — Medium: D3 semantics | Accepted | Advancement table; D3; Rejected | Restated as an operator interpretation proposed with the result known. Historical measurement is separated from blocking disposition, each outcome is mapped to a state, and the cell stays blocking until acceptance. The estimands are distinguished, and A/A is context only. The mapping applies prospectively to G. |
| F7 — Medium: latency coverage | Accepted | D5; D6 S6 control p95 | H instruments both S6 workloads on the final survivor in every route, with a combined-overlay perturbation check and matched-load input. Known DATAGRAM figures are disclosed. A phase association neither explains 1.167 → 1.469 nor establishes necessity. Tail sparsity is stated. |
| F8 — Medium: per-workload objective | Accepted | D4 Stage 2 | Stage 2 runs for workloads with an addressable hand-off finding. It keeps on improvement there and non-regression elsewhere, with guards in both. Efficacy is reported separately, and partial efficacy reads inconclusive. |
| F9 — Medium: S5 screen | Accepted | D4 Stage 2; inventory | Four S5 blocks, a registered usability and discrimination rule, 136 observations and at most 14 reruns. G's full readiness is kept. |
| F10 — Medium: credit-response inference | Accepted | D4 endings; D7 routing | Endings are reported on two axes: credit response and remedy status. An untested remedy is not a failed one, and any 2Q profile change is only for consideration. |
| F11 — Low: evidence corrections | Accepted | Flag summary; evidence bullets | Four newly raised candidate cells; leads 1–2 versus null lead 3 with its figures; S6 STREAM sender split 2.03 / 2.25 MiB; S6 DATAGRAM receiver bookkeeping 1.42 MiB. Eleven unresolved cells is unchanged. |
| F12 — Low: negative outcome and wake cost | Accepted | D1 classification; D2 | Now reads "negative under the registered rule; kept by operator deviation 3, with a demonstrated S5 benefit". The rationale is attributed to that deviation, and the wake increase stays disclosed with its S5-only, `perf`-attached scope. |

Not acted on, as the synthesis advised: changing the verdict, declaring a new `preslat` threshold, inferring an RSS bound from ledger size, imposing `preslat`'s reply minimum on readiness, historical A/A ranges in place of contemporaneous controls, automatic reversion after G, mandatory predecessor latency arms, and any paid run, host-setting change, production change or BBRv1 work.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The bookkeeping-capacity and kick facts come from source inspection and are not measurements.
