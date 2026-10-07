# BBRv3 qualification decision after the pacing-wake and CPU tickets

Status: draft decision set for [Decide whether the timing- and CPU-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/737), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-07; awaiting a multi-agent consideration and final acceptance. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

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
| 3. No unresolved flag, no preservation flag | Eleven unresolved candidate cells; one Reno-on-candidate cell raised in readiness and not reproduced by its registered stage (see D3) | No (unresolved cells) |
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

Five cells cleared, five candidate cells and one preservation cell became newly raised, and one cell moved from attributed to unresolved. The frozen-Reno A/A arm crosses no limit; its widest ranges are loopback control p95 (STREAM 0.68–1.85, DATAGRAM 0.47–1.62).

### What the three tickets establish

- **The Linux S5 gap was timer delivery, and a contract-preserving change removed it.** The [pacing-wake ticket](../2026-10-05-bbr-pacing-wake/README.md) showed the Go runtime firing the pacing timer a median 107–109 µs late, with delivery holding 0.982–0.985 of the lateness. r8's netpoller kick cut the S5 deficit against frozen Reno from 0.088 / 0.113 to 0.026 / 0.030 and sender CPU per useful GiB by 22% / 13%. Its registered outcome was **negative**, because pacing-timer wakes per useful GiB rose 14–16% against the operator's no-increase requirement; the operator kept it after the outcome (its deviation 3). On readiness, every S5 goodput and CPU cell now passes and the S5 timeline reads **model behaviour** in all eight blocks: about 7 µs of lateness per event, 0.0002–0.001 of capacity.
- **The remaining S5 deficit is the selected model's own probing and margin.** The deficit against capacity is 0.029–0.036, of which the model accounts for 0.019–0.024: the pacer's 0.99 margin, Down (17–20% of the window) and ProbeRTT (3.4–4.3%). That is a passing cell.
- **Three selected CPU leads failed to clear #714's rule.** The [per-packet CPU-lead ticket](../2026-10-06-bbr-per-packet-cpu-leads/README.md) ended inconclusive, inconclusive and null and reverted all three; each cut 145–236 user instructions per packet while sender user cycles held or rose. The CPU cells those leads targeted now pass anyway. On r8 the S5 receiver shows no CPU-time excess under either harness.
- **Loopback DATAGRAM is not timing-bound.** On `d0fabc4d` the sender never reached a paced stop on loopback DATAGRAM. It spent 0.439–0.444 of the window in local credit waits, 0.404–0.407 inside opportunities and 0.095–0.097 in immediate continuations. In #715's loopback-bottleneck stage, neither BBR serial stage saturated (send queue 0.80, connection loop 0.58–0.67 cores), while frozen Reno's send queue did (0.88). That pattern fits two coupled stages waiting on each other through the 2Q send credit. D08 itself records that "tight credit can reduce throughput under worker delay". No stage has tested it.
- **Loopback STREAM goodput fell from 0.962 to 0.943 between revisions.** No stage examined it. r8 differs from `d0fabc4d` only in the Linux kick, which acts at a pacing deadline; whether loopback STREAM reaches paced stops on either revision is unmeasured.
- **The memory instrument has stopped discriminating.** All seven raised memory cells are steady-state excesses, and all are unresolved because no cell has four agreeing heap-site blocks. Two cells have a block whose heap-site excess at the heap peak is negative or near zero, so the heap peak does not carry the RSS excess there. Descriptively, bookkeeping (`recoveryEvidence.sent`, `beginCongestionFeedback`, `deliveryRecords`; about 2.0–2.6 MiB) leads the sender cells and the S6 DATAGRAM receiver; delivery data (4.0–5.3 MiB) leads the STREAM receiver cells. Receiver cells also carry about 1.1–1.4 MiB of bookkeeping, because BBRv3 is configured at both endpoints. Doubling the heap-site blocks from two to four made classification less decisive, not more. #710 found the macOS STREAM receiver heap rise to be loss-driven reassembly; r8's S5 STREAM forward overflow is 0.210% against Reno's 0.095%.
- **S6 DATAGRAM p95 descriptively matches the Up-policy pattern.** 98.3–98.9% of samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay is 0.15–0.20 ms; S5 DATAGRAM matched-load p95 is 0.987. D2's registered rule covers STREAM only, so the cell is unresolved.
- **The Reno-on-candidate latency cell did not reproduce at an adequate sample.** Readiness gives 20 control replies per loopback run, so its p95 is near the maximum; frozen Reno's own A/A arm crossed this cell's limit at 1.615 in #715. The registered `preslat` stage (six blocks, 120 replies per run, adequacy met) reads **not reproduced**: pooled ratio 0.716, 95% block-bootstrap interval 0.487–1.180, two of six blocks above 1.

### Platform and source facts checked for this decision (source inspection, not measurement)

- **The bookkeeping capacities are fixed allocations.** On r8, the BBR recovery outcome ledger allocates `maxRecoveryOutcomes = 32768` slots on first use (`internal/ackhandler/bbr_recovery.go`), and the delivery-record ceiling is `maxDeliveryLive = 25000` (`internal/ackhandler/delivery_sampler.go`). A fixed slab's footprint does not depend on how much of it the run occupies, so its share of a memory excess can be tested by changing the capacity while staying above measured occupancy.
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
| Failed selected interventions | #735's three profile-selected CPU leads (inconclusive, inconclusive, null; reverted). #734's Stage 2 was negative by its registered rule and kept by operator decision; it is not a failed intervention in effect, since every S5 goodput and CPU cell now passes. #734's diagnostic 2Q arm never applied its treatment and is uninformative. |
| Missing evidence | Loopback STREAM goodput; loopback DATAGRAM goodput and sender CPU; S6 DATAGRAM control p95; the seven memory cells. |
| Attributed behaviour (not shown to be an algorithm necessity) | S6 STREAM control p95 (selected ProbeBW Up policy), on three records. |
| Demonstrated contract incompatibility | None. D08's statement that tight 2Q credit can reduce throughput under worker delay makes the loopback hand-off a candidate for one, untested. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception granted now

No exception is granted, and the macOS D2 exception is not extended. With eleven unresolved cells, no exception could satisfy D1. Recorded for the next decision, without clearance:

- **S6 STREAM control p95:** 1.550 on r8, the same Up-policy cause on three records (macOS `fc4c1bf1` 1.547, Linux `d0fabc4d` 1.588, Linux r8 1.550). It is the leading exception candidate once no unresolved cell remains.
- **S6 STREAM receiver RSS:** 1.891, now **unresolved** over four blocks, after *delivery data* over two on `d0fabc4d` (1.759). Larger and unresolved, so the earlier attribution does not carry.
- **S5 sender RSS:** STREAM 1.261 and DATAGRAM 1.173, both **unresolved**; the DATAGRAM cell is newly raised.
- **Run-loop wakes per useful GiB (#734's open cost):** not a readiness flag. The cost the operator's no-increase guard protected, sender CPU per useful GiB, now passes on S5 (0.935 / 0.909). It is carried as a disclosed production cost, not a ticket.

### D3 — The Reno-on-candidate loopback latency cell is resolved for r8 on Linux

The cell's readiness value (2.588) stays as recorded. The decision dispositions it as **resolved by its registered metric-specific stage**, for r8 on Linux only:

- D3 of the Linux decision required exactly this: a latency rule with "enough control replies per run for a usable tail estimate", registered before data and exercised on synthetic cases, that cannot pass by range membership. `preslat` is that rule, and #736 ran it as registered.
- Its outcome, **not reproduced**, requires the interval's upper bound to be at most the 1.20 limit. That is evidence against a regression above the limit at an adequate sample, not A/A overlap.
- The readiness measurement gives 20 replies per run, a tail estimate that frozen Reno's own A/A arm has crossed. Keeping a flag blocking when its adequate re-measurement excludes the regression would leave no path by which evidence could resolve it.

The disposition is not an exception and covers no other cell. Any new revision re-measures the cell in readiness, and a raised value there runs `preslat` again. A *regression reproduced* or *inconclusive* outcome would keep the cell blocking.

### D4 — Ticket F: what limits BBRv3's loopback goodput

One task ticket on `minimax`, from r8, under #712's core layout and host settings, with no host-setting change. Every rule, threshold, synthetic case and inventory is committed in the ticket's record before any comparative data, with an explicit inconclusive ending for each stage.

**Stage 0 — Instrument preflight (no attribution).** Extend #734's loopback recorder (`cand-lb-timeline`, aggregates only) to observe, per refused reservation: when the connection loop began waiting for local credit; the worker's completions that returned credit, with each group's dequeue, submission start and completion; the credit signal; and when the connection loop resumed and reserved. It also reports paced stops and lateness, so loopback STREAM's timing exposure is measured on both revisions. The preflight passes only when synthetic cases are each recovered: a slow worker, a delayed credit signal, a busy connection loop with credit available, congestion-window waits, and a mixed case. Instrumented goodput must stay at least 0.99 of the plain build's. If no instrument passes, the ticket ends with an **instrumentation gap**.

**Stage 1 — Discriminate the loopback limit (attribution only).**
- *Question.* What limits BBR loopback goodput in each workload, and did r8 lower loopback STREAM goodput?
- *Hypothesis.* The credit hand-off: the connection loop waits for 2Q credit while the worker is not saturated, so the two stages serialize through credit return.
- *Competitors.* The worker is saturated (per-packet worker cost); the connection loop is busy with credit available (loop work); congestion-window or model waits; for STREAM only, r8's kick.
- *Arms.* Loopback, both workloads, four blocks: frozen Reno (plain, reference), r8 instrumented, a second r8 instrumented (the BBR A/A control), `d0fabc4d` instrumented, and a **diagnostic pending-bound arm**: r8 with ordinary pending local work bounded at 4Q instead of 2Q, instrumented.
- *Diagnostic pending-bound arm (never kept).* It breaks D08's 2Q bound and exists only to discriminate. Before any comparative use, a treatment check must show that pending bytes exceed 2Q in the arm's own runs; otherwise the arm is declared **not applied** and uninformative, and is not rerun under a different patch (the lesson of #734's 2Q arm). A response shows that credit tightness limits this fixture, not that a D08 change is necessary.
- *Rule.* The registration fixes share rules over the Stage 0 states that separate hand-off, worker-bound, loop-bound and window-bound outcomes, per workload, by a three-of-four block majority. The pending-bound arm's goodput and CPU are paired against the same block's r8 arm and read against the BBR A/A range. The r8 against `d0fabc4d` STREAM comparison is read against the same range.
- *Inventory.* 5 arms × 2 workloads × 4 blocks = 40 observations.

**Stage 2 — One contract-preserving hand-off change, only on a hand-off finding that such a change could address.**
- *Change.* A BBR-only change that shortens the hand-off. The 2Q bound, the credit-return points ("after local acceptance or known rejection, not dequeue and not peer ACK"), ownership, ordering and Reno's path stay unchanged. The mechanism is registered after Stage 1 and before comparative data.
- *Gates before comparative data.* A fixed-input policy-equality oracle for admissions and credit accounting; lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown; bound checks that pending work never exceeds 2Q; and #714's inherited correctness, race and native-coverage gates.
- *Arms.* Loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r8, a second r8 (BBR A/A), the new revision and Reno on the new revision; plus an S5 non-regression check (r8, BBR A/A, new; both workloads; two blocks).
- *Keep rule.* Kept only when, in both loopback workloads, with at least five of six usable blocks: the deficit against the same block's frozen Reno falls beyond the BBR A/A range; sender CPU per useful GiB is not above the BBR A/A maximum; receiver integrity holds; Reno on the new revision passes #714's preservation metric; and on S5 neither goodput nor sender CPU per useful GiB moves the wrong way beyond the BBR A/A range. The guard is the cost itself (CPU per useful GiB), not a wake-count proxy.
- *Other outcomes.* Negative, null and inconclusive as in #734, with bottleneck and credit-wait shares reported beside every outcome.
- *Inventory.* 6 × 2 × 6 = 72 loopback plus 3 × 2 × 2 = 12 S5 = 84 observations.

**Inventory and endings.** Stages 1 and 2 total 124 observations; reruns are capped at 26, so the ticket stays within **150** Linux observations. The Stage 0 preflight is excluded from statistics and listed. Every ending reports to the next ticket with an identified surviving revision (r8 unless a change is kept):
- kept;
- null, negative or inconclusive;
- instrumentation gap;
- a worker-bound or loop-bound finding (no hand-off change built; evidence for a later per-packet or integration decision);
- a **credit-bound finding**: the pending-bound arm lifts goodput beyond the BBR A/A range and no contract-preserving hand-off change addresses the delay. This is the evidence D08 asks for before any "recorded profile change", and it goes to the decision as a candidate for a separate contract-change design decision;
- a kick finding on loopback STREAM (reported as a cost of r8);
- an unbuildable mechanism, failed gates or an exhausted budget.

### D5 — Ticket G: re-demonstrate only if Ticket F keeps a change

Blocked by Ticket F. If F keeps a change, Ticket G reruns #736's readiness stage on F's survivor, unchanged except for the revision: the same arms, paths, seeds, limits, median rule, contamination rule and core layout, with #736's conditional preservation stages (`presrss`, `preslat`) and the S5 timeline. It runs no memory attribution; that is Ticket H's. If F keeps nothing, Ticket G closes as **not run**, because a readiness rerun of unchanged r8 would duplicate #736.

### D6 — Ticket H: attribute the memory cells by intervention, and the S6 DATAGRAM latency cell

Blocked by Ticket G, on the final survivor, against the latest readiness flags (Ticket G's, or #736's if G did not run). Heap-site shares at the heap peak are not the decisive instrument here: they have left every cell unresolved on four records, and they do not carry the RSS excess in some blocks.

**Stage 0 — RSS decomposition instrument (descriptive).** A measurement-only overlay that samples, every second, the process RSS and the Go runtime's own memory classes (live heap objects, free heap retained, heap released, the GC goal, goroutine stacks and other runtime memory). It reports each raised cell's excess as live heap, GC headroom, retained free heap and non-heap. It passes only when synthetic cases are recovered: a retained allocation of known size, a warmup-only spike, goroutine-stack growth and a higher allocation rate at a fixed live heap. Instrumented goodput must stay at least 0.99 of plain. The decomposition is descriptive and classifies nothing.

**Stage 1 — Causal arms.** Each raised cell's path and workload (S5 and S6), four blocks, with the Stage 0 overlay:
- frozen Reno and the candidate;
- a **diagnostic bookkeeping-capacity arm** (never kept): the candidate with its fixed bookkeeping capacities (the recovery outcome ledger and the delivery-record ceiling) reduced to a registered size above the largest occupancy measured in the candidate's own runs. Before comparative use, occupancy counters must show the reduced bound never bound: no forced eviction or shed record, and goodput within the BBR A/A range. Otherwise the arm is **not applied**. A reduction of declared bounds is a design-bound change, so a response is evidence for a later design decision and authorizes nothing;
- a **Reno-receiver arm**: the candidate sender against a frozen-Reno-built receiver. If a receiver excess remains, the sender's traffic causes it; if it disappears, receiver-side candidate state does.

**Rule, per cell** (registered with synthetic cases before data; at least three usable blocks, else an evidence gap):
- **sender bookkeeping capacity** when the capacity arm removes at least 70% of a sender cell's median excess, with the treatment applied;
- **receiver-side candidate state** when the Reno-receiver arm removes at least 70% of a receiver cell's median excess;
- **traffic-driven delivery retention** when the Reno-receiver arm removes under 30% of a receiver cell's excess and the overlay places at least 70% of it in live heap;
- **runtime headroom** when the overlay places at least 70% of the excess in GC headroom and retained free heap, with no arm removing it;
- otherwise **mixed** or **inconclusive**.

**S6 DATAGRAM control p95.** Applied to the S6 DATAGRAM candidate runs with the timeline overlay and relay queue samples: D2's Up-policy rule, unchanged except that it now covers DATAGRAM, registered before data.

**Inventory.** 4 arms × 2 paths × 2 workloads × 4 blocks = 64 causal observations. The S6 DATAGRAM candidate arm carries the timeline overlay and relay queue samples, so the latency rule adds no runs. With reruns, the ticket stays within **100** Linux observations; the Stage 0 preflight is excluded from statistics and listed.

**Endings.** Every cell ends classified, mixed, inconclusive, an evidence gap, or with its arm not applied; every ending goes to the decision with the revision named.

### D7 — Then a fresh decision on every outcome

A new grilling ticket, blocked by Ticket H, re-asks this question on whatever F, G and H find, including nothing kept, instrumentation gaps and treatments not applied. Its options are the same as this ticket's. **Terminal routing:**

| Outcome | Route |
| --- | --- |
| F keeps a change | G re-demonstrates F's survivor; H attributes on it. |
| F ends any other way | G closes as not run; H attributes on r8 against #736's flags; F's finding goes to the decision. |
| F finds a credit bound | Evidence for a separate contract-change design decision on D08's 2Q profile, considered by the decision; nothing is changed by F. |
| H attributes a sender cell to bookkeeping capacity | Evidence for a separate design-bound change decision; not an exception by itself. |
| H attributes a cell to traffic-driven retention or Up policy | An attributed-behaviour cell, eligible for an exception decision. |
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
- **Keep the Reno-on-candidate latency cell blocking.** Its registered adequate-sample stage excludes the regression; no evidence could ever resolve it otherwise.
- **Repeat heap-site attribution with more blocks or finer sampling.** Four blocks were less decisive than two; the heap peak does not carry the RSS excess in some blocks.
- **Change the memory limit, the median rule or the advancement condition.** That would weaken a gate.
- **Raise the 2Q pending bound or redesign the hand-off now.** Untested; D08 requires evidence and a recorded profile change first.
- **Reduce the bookkeeping capacities now.** A design-bound change, and the cells are unattributed.
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

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The bookkeeping-capacity and kick facts come from source inspection and are not measurements.
