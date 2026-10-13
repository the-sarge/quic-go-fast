# BBRv3 qualification decision after the loopback and memory tickets

Status: draft decision set for [Decide whether the loopback- and memory-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/741), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-13. Revised after a first multi-agent consideration, whose 74 adjudicated findings are dispositioned [below](#consideration-dispositions); a second consideration follows. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-r9-qualification-decision`, based on `ddbac0a3` (the [r9 memory and latency attribution](../2026-10-08-bbr-r9-memory-attribution/README.md) record, #740). That record's history contains the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) (#739), the [loopback hand-off record](../2026-10-07-bbr-loopback-handoff/README.md) (#738) and the [previous decision](../2026-10-07-bbr-r8-qualification-decision/README.md) (#737). The surviving revision is **r9, `81e9dc8c`**: r8 (`95f5b6b7`) plus a 32-entry send queue for BBR connections. The first two decisions and the Mac records live on sibling record branches and are linked by commit.

## Question

On the evidence from the loopback, re-demonstration and memory-attribution tickets, with the preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration, open a separate contract-change or design-bound decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged D1 advancement condition; preservation flags still block. Dispose of the cells recorded in the previous decision's D2, any newly attributed cost and the remaining blockers, on every outcome of its terminal routing table. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability.

## Normative constraints (quoted)

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": "the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload)"; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers." D2: "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "a raised preservation flag blocks advancement." D6: "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

[Intervention qualification decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md), D2, on a newly attributed implementation cost: "An implementation-integration cost on the declared platform, not an algorithm trade-off. It is a candidate for remedy, not for exception, while a contract-preserving remedy remains untested."

[Previous decision](../2026-10-07-bbr-r8-qualification-decision/README.md), D3, prospectively, for the Reno-on-candidate latency cell: "Any new revision re-measures the cell in readiness. A raised value there runs `preslat` again, and only its **not reproduced** outcome ends blocking. It is not an exception and covers no other cell." Its D7 routing table: "F: credit-responsive, remedy failed or unbuildable | Evidence for considering a separate design decision on D08's 2Q profile; nothing is amended"; "H: sender bookkeeping (ring) | Evidence for considering a separate design-bound decision; not an exception by itself"; "H: S6 p95 attributed to the Up policy | Attributed behaviour, eligible for an exception decision"; "H: delivery retention, with site or lifetime evidence | Attributed behaviour, eligible for an exception decision"; "H: localized, cause unresolved; mixed; inconclusive; gap; treatment inert or unusable | Unresolved; not eligible for a cause-based exception."

[#740's operator decision 2](../2026-10-08-bbr-r9-memory-attribution/README.md#operator-decisions-recorded-before-data), for the candidate's loopback STREAM control p95, accepted before data: "an adequate *not reproduced* outcome ends this cell's blocking state for r9 on Linux only, as in D3. *Regression reproduced*, *inconclusive*, *evidence gap* or unusable leave it blocking and unresolved."

This ticket's question: a separate contract-change or design-bound decision ("such as D08's 2Q pending profile or the outcome-ring bound") is "considered only on the evidence the routing table names". "An untested remedy is not a failed one, and declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Any necessary contract change is a separate explicit design decision." Out of scope: "No rewriting failed verdicts, weakening calibration/correctness gates, …".

## Evidence base

Readiness figures are from the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) ([summary.json](../2026-10-08-bbr-r9-linux-redemonstration/summary.json)): 110 observations on r9 against frozen Reno `e4f322cb` on `minimax`, all clean, none contaminated. Attribution figures are from the [r9 memory and latency attribution](../2026-10-08-bbr-r9-memory-attribution/README.md) ([memory.json](../2026-10-08-bbr-r9-memory-attribution/memory.json)): 124 counted observations, all clean, no rerun, no deviation. Ratios are medians of five per-block ratios against the same block's frozen Reno, with blocks crossing the limit in parentheses. Absolute rises are differences of arm medians. The decisive figures below were checked against both JSON files.

### Advancement condition, checked on r9 (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.53× [12.25–15.74] STREAM, 14.74× [11.34–16.32] DATAGRAM, 5/5 pairs each; sender CPU per useful GiB 0.086 / 0.090× Reno | Yes |
| 2. Every flag passes, or is attributed and excepted | Eleven raised candidate cells: two attributed (S6 control p95, both workloads) but not excepted, and nine unresolved | No |
| 3. No unresolved flag, no preservation flag | All nine unresolved cells block. #740's accepted mapping only defines how the loopback latency cell can stop blocking; it did not. No Reno-on-candidate cell is raised | No (unresolved cells) |
| 4. Mac records reviewed, platforms named | Reviewed below; any advancement would name Linux only | Reviewed |

### Raised cells on r9, with #740's endings

| Cell | r8 (#736) | r9 (#739) | Absolute, Reno → candidate | Ending on r9 (#740) |
| --- | --- | --- | --- | --- |
| S6 STREAM control p95 | 1.550 | **1.562 (5)** | 127.7 → 199.5 ms (+71.7) | Attributed: selected ProbeBW Up policy |
| S6 DATAGRAM control p95 | 1.469 | **1.459 (5)** | 125.8 → 187.3 ms (+61.5) | Attributed: selected ProbeBW Up policy |
| Loopback STREAM control p95 | 1.171, passing | **1.485 (4)** | p95 0.134 → 0.199 ms; at 120 s, pooled p95 0.140 → 0.258 ms | Regression reproduced, 1.849 [1.672–2.014]; blocking, unresolved |
| S5 STREAM sender RSS | 1.261 | **1.235 (5)** | 17.97 → 22.18 MiB | Mixed: the ring removes 0.48–0.57 |
| S5 STREAM receiver RSS | 1.565 | **1.518 (5)** | 16.94 → 25.71 MiB | Inconclusive: receiver-controller arm confounded |
| S5 DATAGRAM sender RSS | 1.173 | **1.159 (4)** | 17.88 → 20.86 MiB | Evidence gap: resident memory unexplained |
| S6 STREAM sender RSS | 1.656 | **1.673 (5)** | 13.25 → 22.18 MiB | Inconclusive |
| S6 STREAM receiver RSS | 1.891 | **1.898 (5)** | 13.50 → 26.34 MiB | Inconclusive |
| S6 DATAGRAM sender RSS | 1.464 | **1.542 (5)** | 13.78 → 21.24 MiB | Inconclusive |
| Loopback STREAM receiver RSS | 1.087, passing | **1.120 (4)** | 14.48 → 16.20 MiB | Inconclusive; the sensitivity pass would have labeled it receiver-side controller state |
| Loopback DATAGRAM receiver RSS | 1.045, passing | **1.148 (5)** | 18.72 → 21.72 MiB | Instrumentation gap: perturbation 0.989 |

Every goodput and CPU cell passes on r9: loopback goodput 1.148 / 1.001, S5 goodput 0.967 / 0.972, and S5 DATAGRAM sender CPU per useful GiB 0.953 (#738's guarded cost). S6 DATAGRAM receiver RSS passes at 1.095. No Reno-on-candidate cell is raised. The frozen-Reno A/A arm crosses one cell, loopback DATAGRAM control p95 (1.599), which is context only.

### What the attribution ticket establishes and leaves open

- **The removal arms could not keep traffic comparable.** Four clean A/A pairs gave comparability bands of 1–2%, often at the 1% floor, while the treatment arms differed from the candidate by about ±1–5%. #740 reads this as scatter, not a single large shift, with one small systematic tendency: with Reno selected at the receiver, the receiver sent fewer packets per useful GiB in 14 of 16 WAN blocks, by up to 2%. The smaller ring changes eviction age; with it, S6 STREAM goodput was 1.1–5.0% lower.
- **The ring's resident cost per byte varies by endpoint.** The 4,096-entry ring removed 0.999 MiB of explicit bytes. The resident cut was about 2.2 MiB at the STREAM senders, matching the GOGC=100 doubling the registration modeled, but about 1.1 MiB at the S5 and S6 DATAGRAM receivers (1.084 and 1.092 MiB, each from one comparable block). At other receivers the ring arm's traffic was not comparable, so its removal there is not a clean multiplier.
- **Every candidate endpoint carries explicit BBR structures.** In `memory.json` (`descriptive.bbr_explicit`), each sender holds 2.00 MiB (STREAM) or 2.15 MiB (DATAGRAM) on S5 and S6 and about 1.23 MiB on loopback; each receiver holds 1.18–1.35 MiB. The outcome ring is 1.14 MiB at every endpoint.
- **The accounting places the excesses in heap memory.** File-backed pages, stacks and runtime metadata differ by little.
- **Logical receive occupancy is small.** #740 sampled connection flow-control occupancy (received minus read, holes included) at each peak. Its candidate excess is 0.0 MiB at the S5 STREAM receiver and a median of 0.30 MiB (0–0.68) at the S6 STREAM receiver, against excesses of 8.75 and 13.05 MiB. These are logical bytes at one instant, and the excess can drain before a sample or sit in pinned backing capacity, so they do not bound storage. They do weigh against unread or out-of-order stream data explaining the receiver cells.
- **Both S6 control p95 cells meet the Up-policy rule** in all eight runs per workload (STREAM 98.6–99.4% of samples above 25 ms in Up or the Down entered from Up; DATAGRAM 98.5–100%).
- **The loopback STREAM control-latency rise reproduces at an adequate sample.** All six per-block p95 ratios are above 1; the pooled p50 is 0.130 against 0.069 ms. No stage has looked for its cause.

### Fixed bookkeeping against the memory allowance (descriptive inference, not measurement)

The memory limit allows 10% above the same block's frozen Reno. Using #739's Reno medians, #740's explicit BBR bytes and the measured ring range of about 1.1–2.2 resident bytes per explicit byte:

| Raised cell | Allowance (MiB) | Explicit BBR bytes (MiB) | Resident estimate at 1.1–2.2× (MiB) | Excess E (MiB, #740) |
| --- | --- | --- | --- | --- |
| S5 STREAM sender | 1.80 | 2.00 | 2.2–4.4 | 4.05–4.82 |
| S5 STREAM receiver | 1.69 | 1.34 | 1.5–2.9 | 8.06–9.36 |
| S5 DATAGRAM sender | 1.79 | 2.15 | 2.4–4.7 | −0.01–3.35 |
| S6 STREAM sender | 1.33 | 2.00 | 2.2–4.4 | 6.55–9.80 |
| S6 STREAM receiver | 1.35 | 1.34 | 1.5–2.9 | 11.10–16.24 |
| S6 DATAGRAM sender | 1.38 | 2.15 | 2.4–4.7 | 5.76–7.38 |
| Loopback STREAM receiver | 1.45 | 1.18 | 1.3–2.6 | 1.42–1.80 |
| Loopback DATAGRAM receiver | 1.87 | 1.19 | 1.3–2.6 | 1.44–3.39 |

Even at the low multiplier, the explicit structures alone would exceed the allowance at the four S5 and S6 sender cells and the S6 STREAM receiver. At the high multiplier they would exceed it at every listed cell. This is an extrapolation, not a measurement: the loopback senders pass (0.954 / 0.956) although they carry about 1.23 MiB of the same structures, so other differences can offset them. If it holds, then for cells whose excess is mostly bookkeeping, attribution cannot make the cell pass; it can only make the cell eligible for a remedy, a design-bound decision or an exception.

### The loopback control-latency lead (descriptive)

- **What changed.** r9 differs from r8 only in `bbrSendQueueCapacity = 32` (`send_queue.go`) and its selection; Reno keeps eight entries, and `maxSendBatch` stays eight, so one batched send still takes at most eight queued packets. At loopback rates each BBR entry holds one packet. The constant is not platform-specific, and BBRv3 runs at both endpoints, so the receiver's replies also pass a 32-entry queue.
- **What the cell measures.** The fixture's sender writes a 32-byte message on a reliable control stream once per second and times the receiver's echo (`observeControl` in the [campaign fixture's session.go](../2026-09-25-bbrv3-q1-campaign/fixture/session.go), which #739 and #740 build from). The request crosses the sender's send queue behind bulk data; the reply crosses the receiver's send queue behind ACK-only packets. (Source inspection.)
- **#738 Stage 2's runs** (r8 and r9 as instrumented hand-off builds with about a 2% recorder cost, frozen Reno plain; 20 replies per run; the six clean counted blocks). [lead.py](lead.py) reads them from #738's [raw.tar.gz](../2026-10-07-bbr-loopback-handoff/raw.tar.gz), matching each block's counted attempt to its `results.json`, and writes [latency-lead.json](latency-lead.json):

  | Comparison | p95 median [range] | p50 median [range] |
  | --- | --- | --- |
  | r9 ÷ r8 | 1.42 [1.14–1.82] | 1.54 [1.35–1.74] |
  | second r8 ÷ r8 (BBR A/A) | 1.04 [0.66–1.28] | 0.94 [0.91–1.20] |
  | r8 ÷ frozen Reno | 1.45 [1.14–1.62] | 1.15 [1.14–1.42] |
  | A/A frozen Reno ÷ frozen Reno | 1.02 [0.71–1.48] | 1.05 [0.88–1.35] |

  r9's p95 exceeds the BBR A/A maximum in four of six blocks, and its p50 in all six. r8 ÷ Reno exceeds the Reno A/A maximum in only two of six blocks at p95 and one at p50, with r8 instrumented and Reno plain; r8's plain readiness read 1.171 (#736).
- **Reading.** These are instrumented, small-sample figures from a ticket that did not register them. They are a lead, not an attribution. The lead for the r8-to-r9 rise is consistent; the lead for a BBR baseline above Reno is weak.

### #738's outcomes and the D08 2Q option

- **Stage 0** ended in an instrumentation gap (its recorder cost about 2%); Stages 1 and 2 ran by operator deviations 2 and 3.
- **Stage 1:** DATAGRAM bound at the hand-off (queue) in all four blocks; STREAM inconclusive.
- **The 4Q diagnostic arm was inert** in both workloads: its activation preflight did not take pending bytes above 2Q. The previous D7's route to a D08 2Q-profile decision needs a credit-responsive arm, so it has no trigger, and no D08 2Q-profile decision is opened.
- **Stage 2's 32-entry queue was negative under its registered rule** (S5 DATAGRAM sender CPU per useful GiB +5% against r8) and kept by operator deviation 4. The kept change is the only code difference behind the new loopback latency cell.

### Source facts checked for this decision (source inspection, not measurement)

- **The outcome ring includes ACK-only registrations.** `recoveryEvidence` in `internal/ackhandler/bbr_recovery.go` allocates `maxRecoveryOutcomes = 32768` outcomes on first registration, and its comment says "The ring includes every registration, even ACK-only packets and excluded probes." A receiver therefore fills its ring with ACK-only packets at these rates, and allocating it lazily would not shrink it.
- **The bookkeeping is almost entirely scalar.** `recoveryOutcome` and `deliveryRecord` (a key and a `congestion.PacketInfo`, whose fields are values) hold no pointers; the pointer-bearing `retainedDelivery` nodes are capped at `maxDeliveryRetained = 4096`. The delivery records grow with occupancy, and their 25,000 ceiling limits insertion.

### Mac records reviewed

- **[C1–C4 correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) (#671)** and **[WAN-corrected re-demonstration](https://github.com/the-sarge/quic-go-fast/blob/dc252f8a/docs/audits/2026-10-03-bbr-wan-redemonstration/README.md) (#711, `fc4c1bf1`).** Their D2 exception covers `fc4c1bf1` on macOS only, at no more than S6 control p95 1.547 / 1.481 (+77.5 / +60.3 ms), S6 STREAM receiver RSS 1.574 and S5 sender RSS 1.203 / 1.132. It is unchanged and does not carry over.
- **New-revision Mac evidence** is #715's four S5 timeline observations on `d0fabc4d`, all "model". New-revision Mac readiness is unmeasured. r8's kick is Linux-only, while r9's queue depth applies on every platform.

## Decisions

### D1 — Verdict: continue resolving the implementation; neither advance nor decline

BBRv3 is **not advanced** on r9. Condition 3 fails with nine unresolved candidate cells, and condition 2 fails because the two attributed cells are not excepted.

BBRv3 is **not declined**. The S6 benefit is repeatable on Linux for `fc4c1bf1`, `d0fabc4d`, r8 and r9 (forty pairs, all above Reno), and stands at 14.53× / 14.74× on r9. Every goodput and CPU cell now passes, no Reno-on-candidate cell is raised, and every open cell has a measurable next step on owned hardware.

Non-advancement is forced by condition 3. Continuing rather than declining is an operator choice.

**Classification, as the question requires:**

| Category | Finding |
| --- | --- |
| Failed selected interventions | #738's 32-entry queue: **negative under its registered rule** (S5 DATAGRAM sender CPU guard), kept by operator deviation 4; its guarded cost now passes readiness (0.953), and it is the only code change behind the new loopback latency cell. |
| Inert or uninformative diagnostics | #738's 4Q arm (inert); #738's Stage 0 (instrumentation gap). #740's ring treatment engaged and passed its gates, and was never a kept change. |
| Missing evidence | Eight memory cells (one mixed, five inconclusive, one evidence gap, one instrumentation gap); #738's STREAM hand-off attribution (inconclusive); the cause of the loopback STREAM control-latency rise. |
| Attributed behaviour (not shown to be an algorithm necessity) | S6 control p95, STREAM and DATAGRAM (selected ProbeBW Up policy). |
| Implementation-integration lead | The loopback control-latency rise and r9's queue depth (descriptive, #738's runs). |
| Demonstrated contract incompatibility | None. The D08 2Q-profile route has no trigger. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception granted now

No exception is granted, and the macOS D2 exception is not extended. With nine unresolved cells, no exception could satisfy D1, and any new revision would need re-attribution. Recorded for the next decision, without clearance:

- **S6 control p95, both workloads**, attributed to the selected Up policy on Linux r9: STREAM 1.562 (+71.7 ms), DATAGRAM 1.459 (+61.5 ms), as differences of arm medians; the macOS figures use the same definition. STREAM's ratio is slightly above the macOS exception's 1.547, at a smaller absolute rise; DATAGRAM is below 1.481. Both are the leading exception candidates. An exception decision must weigh readiness's 30 replies per WAN run and #740's per-run timeline evidence.
- **#738's S5 DATAGRAM sender CPU rise** (+5% against r8) passes readiness at 0.953 and is not a flag. It stays a production-slicing question, as does #734's run-loop wake increase.

### D3 — Ticket J: what raises the loopback STREAM control latency, and one remedy if the queue is responsible

One task ticket on `minimax`, from r9, under #712's core layout and host settings, with no host-setting change. Every rule, threshold, synthetic case and inventory is committed in the ticket's record before any comparative data, with executable synthetic tests and an explicit inconclusive ending. Accepting this decision does not by itself authorize measurement; the registration does.

**Builds.** Plain r9; **r9 at depth 8** (r9's source with `bbrSendQueueCapacity = 8`, a diagnostic build that is never kept), so the depth comparison changes one constant and almost no code placement; and both with the control-path recorder.

**Stage 0 — Control-path recorder and preflight (descriptive).**
- *Per-request timing.* For every control request (about 120 per 120 s run), the recorder stores the segment times, not aggregates:
  - at the sender: write to packing, packing to queue entry, queue wait with the entries and bytes ahead, and dequeue to submission;
  - at the receiver: arrival to application read; then, for the reply, write to packing, packing to queue entry, queue wait with entries and bytes ahead, and dequeue to submission;
  - back at the sender: reply arrival to application read;
  - a residual for transit and anything unplaced.
- *Synthetic cases.* A deep sender queue, a deep receiver queue, a slow worker, a busy receiver loop, a delayed reply write, a case where the mean and the tail have different causes, and a delay the recorder cannot place, which must receive no label.
- *Perturbation, on the measured quantity.* With the recorder, loopback STREAM goodput must stay at least 0.99 of the plain build's, **and** each revision's pooled control p95 must stay within a registered tolerance of its plain arm's. If either fails, the mechanism reading ends as an **instrumentation gap**, and the plain-arm depth reading below still stands.

**Stage 1 — Attribution.**
- *Arms.* `loopback-long` STREAM (5 + 120 s), six blocks: frozen Reno, r9, a second r9 (the BBR A/A control) and r9 at depth 8, all plain; and r9 and r9 at depth 8 with the recorder.
- *Depth reading (plain arms).* The pooled p95 ratio r9 ÷ depth 8, with a 95% block-bootstrap interval. **Depth raises latency** when that interval lies above 1, at least five of six blocks exceed 1, and the point estimate exceeds the upper bound of the r9 A/A ratio's interval. **No depth effect** when the interval's upper bound lies below the A/A interval's upper bound. Otherwise **inconclusive**.
- *Mechanism reading (recorder arms).* Only if the recorder arms' r9 ÷ depth-8 p95 ratio falls inside the plain arms' interval; otherwise a gap. For the requests at or above a registered upper quantile (no lower than p90) of each arm's pooled latency, the share of the r9-minus-depth-8 rise in mean latency that sits in send-queue wait at either endpoint, with a block-bootstrap interval. **Queue wait** when the share's lower bound is at least 0.70; **load or other** when its upper bound is below 0.30; otherwise **mixed**.
- *Baseline reading.* #715's `latency_preservation` rule, unchanged, for depth 8 against frozen Reno: *not reproduced* (a depth-8 configuration would clear the cell), *regression reproduced* (a **BBR baseline** component), or *inconclusive* or a gap (baseline unresolved).
- *What Stage 1 cannot do.* It reports r9 against frozen Reno descriptively only. It cannot revise #740's *regression reproduced* verdict; only D4's route can end the cell's blocking state.
- *Inventory.* 6 arms × 6 blocks = 36 observations.
- *Disclosed before data.* #738's descriptive figures above.

**Stage 2 — One contract-preserving remedy, only if depth raises latency and the mechanism is queue wait.**
- *Admissible classes.* BBR-only changes that shorten queue wait for control-bearing packets while keeping D08's 2Q bound, the credit-return points, ownership, packet ordering and Reno's eight-entry queue unchanged. For example:
  - draining the deeper queue faster, such as letting one batched send take more of BBR's one-packet entries than `maxSendBatch`'s eight;
  - a BBR queue bound in bytes or time ahead in place of the entry count, set by a registered rule from Stage 1's recorded bytes ahead and goodput.

  A depth chosen by trial is speculative tuning and is excluded. Restoring depth 8 is not a remedy: it gives back the loopback goodput the queue bought (r8's flags were 0.943 / 0.854). The mechanism is registered after Stage 1, from its recorder evidence, before comparative data.
- *Gates before comparative data.* As the previous decision's D4 Stage 2: a fixed-input policy-equality oracle for admissions and credit accounting; lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown; bound checks; #714's correctness, race and native-coverage gates; and detection of an injected second contract violation.
- *Arms.*
  - Loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r9, a second r9, the new revision and Reno on the new revision.
  - `loopback-long` STREAM, six blocks: frozen Reno, r9, a second r9 and the new revision.
  - S5 screen, both workloads, four blocks: r9, a second r9 and the new revision.
- *Keep rule.* Kept only when all of these hold, with at least five of six usable blocks:
  - on `loopback-long`, the pooled p95 ratio new revision ÷ r9 has a 95% block-bootstrap interval whose upper bound is below 1, and a point estimate below the lower bound of the r9 A/A ratio's interval;
  - loopback goodput in each workload is not lower than r9's beyond the BBR A/A range;
  - sender CPU per useful GiB is not above the BBR A/A maximum in either workload, and receiver integrity holds;
  - Reno on the new revision passes #714's preservation metric;
  - the S5 screen shows neither goodput nor sender CPU per useful GiB moving the wrong way beyond its range, under its own usability rule.

  Efficacy is reported separately from the keep outcome. Whether the new revision's cell reads *not reproduced* against frozen Reno is also reported separately: if a baseline component holds, a kept remedy can reduce the cell without clearing it.
- *Inventory.* 72 loopback + 24 `loopback-long` + 24 S5 = 120 observations, released if Stage 2 does not run.

**Inventory and endings.** Stages 1 and 2 total 156 observations, and reruns are capped at 24, so the ticket stays within **180** Linux observations. Preflight and activation runs are excluded from statistics and listed. The ticket ends with Stage 1's depth, mechanism and baseline readings, a remedy status (kept, negative, null, inconclusive, gates failed, unbuildable, or not attempted) and a named surviving revision (r9 unless a change is kept).

### D4 — Ticket K: re-demonstrate only if Ticket J keeps a change

Blocked by Ticket J. If J keeps a change, Ticket K reruns #739's readiness stage on J's survivor, unchanged except for the revision, with its conditional preservation stages and the S5 timeline. If J keeps nothing, Ticket K closes as **not run**: a readiness rerun of unchanged r9 would duplicate #739.

**Operator interpretation, proposed for acceptance with this decision.** #740's operator decision 2 covers r9 only, and the previous decision's D3 mapping covers the Reno-on-candidate cell. For any new revision, a raised loopback STREAM candidate control p95 in readiness runs #740's `latcand-stream` stage again, with its rule unchanged. Only an adequate *not reproduced* outcome ends that cell's blocking state, for that revision on Linux only. Every other outcome leaves it blocking. It is not an exception and covers no other cell.

### D5 — Ticket L: memory attribution by candidate accounting and a bookkeeping ballast

Blocked by Ticket K, on the final survivor, against the latest readiness flags (K's, or #739's if K did not run). Its scope is every raised memory cell. If the survivor is not r9, the S6 candidate and BBR A/A arms also carry #740's fixed-size phase log and relay queue samples, and #740's Up-policy rule re-attributes both S6 control p95 cells, unchanged and with no extra arms.

**Why a new design.** #740's removal arms changed what they measured: Reno at the receiver moved receiver traffic, and the smaller ring changed eviction and S6 STREAM goodput. This ticket reads the composition inside the candidate first, on one scale, and uses a ballast added to frozen Reno as the discriminating comparison for bookkeeping. A ballast leaves Reno's controller unchanged; its traffic is still checked.

**Quantities, per block.**
- **E**: the resident excess, candidate minus frozen Reno. The primary instrument is the per-run peak of `smaps_rollup` Rss sampled at 1 Hz from outside the endpoint process within the measured window. Every label must also hold with `ru_maxrss`, the readiness instrument; #740 measured the two differing by up to 0.84 MiB.
- **ΔL**: the live-heap excess, candidate minus frozen Reno, from the runtime's `/gc/heap/live:bytes` at GC cycles aligned to the window and to each arm's resident peak.
- **K**: the candidate's explicit BBR structure bytes: the outcome ring and its indexes, the delivery records' slots and records, the retained-delivery nodes and heaps, and the feedback scratch.
- **G**: the **retained application data** excess, candidate minus Reno: at the receiver, stream data held out of order or unread, and queued DATAGRAM payloads; at the sender, stream data kept until acknowledged, and queued DATAGRAM payloads. Each is read as the backing capacity it pins, not logical length, and as a high-water mark per GC cycle so that a burst that drains before a sample still counts.
- **T**: the **in-flight transport state** excess, candidate minus Reno, as per-cycle high-water marks: the sent-packet history's per-packet metadata (payload referenced by its frames counts once, in G), the received-packet history and ACK ranges.
- **U = ΔL − K − G − T**, the unexplained live excess.
- **B**: the ballast effect, Reno-with-ballast's resident memory minus frozen Reno's.

**Stage 0 — Instruments and preflight.**
- *Calibration on the survivor.* Excluded runs measure K on the survivor itself, per endpoint, path and workload, and fix the ballast sizes. #740's r9 figures are context only.
- *Ballast build.* Frozen Reno with a measurement-only overlay that holds pointer-free memory at each endpoint, written on every page, in two parts: a fixed part sized to the ring and its indexes, allocated at first packet registration as the ring is; and an occupancy part sized to the survivor's measured delivery-record and other occupancy-dependent bytes. The bookkeeping is almost entirely scalar (source facts above), so a pointer-free ballast matches its live bytes. Timing differs from the delivery records' growth by doubling, and that difference is disclosed; the label reads the peak, where steady-state size dominates.
- *Heap regime.* Every arm reports its heap goal at the peak. Go's goal has a floor of 4 MiB × GOGC/100, and #739's frozen-Reno heap peaks sit near it while the candidate's sit well above. So B alone never assigns a label: the candidate-side condition below is always required.
- *Synthetic cases.* A known allocation whose resident effect is recovered both below and above the heap-goal floor; a structure that grows by doubling, against a one-shot ballast of its final size; a retained-data burst that drains before a 1 Hz sample but is caught by the high-water mark; a ballast that over-explains (B > E), which must not label; and an unexplained excess, which must receive no label.
- *Perturbation.* The gauge overlay must keep goodput at least 0.99 of the plain build's on every path. On a failing path, labels that need G or T end as **instrumentation gaps**, and the registration states how the ballast and K comparison stays usable there (for example, a plain arm with only external resident sampling).
- *Activation.* An excluded run shows the ballast resident and held.

**Stage 1 — Arms.** Each raised cell's path and workload, four blocks: frozen Reno; a second frozen Reno (the **Reno A/A** control); Reno with ballast; the candidate; and a second candidate (the **BBR A/A** control). The ballast arm is **comparable** when its useful delivery, forward packets per useful GiB, receiver feedback packets per useful GiB, control replies and forward overflow stay within the Reno A/A range, with #740's floors (1% for goodput and per-GiB counts, ±1 control reply, 0.0005 overflow).

**Statistics.** B's variation is the Reno A/A paired difference. E's variation combines the Reno A/A and BBR A/A paired differences, in a form fixed by the registration. ΔL's variation comes from the same pairs on the live-heap scale. A block whose E or ΔL is non-positive or below a registered detectability threshold is unusable for fractions; so is a fraction whose denominator (such as ΔL − K) falls below that threshold. Labels need agreement in **at least three usable blocks**.

**Rule, per cell.** Shares are taken on the live-heap scale, where explicit bytes and the denominator share units. The ballast supplies the resident translation for bookkeeping and the bound that excludes it for the other labels.
- **Controller bookkeeping** when K is at least 70% of ΔL with |U| below a registered share; the comparable ballast closes at least 70% of E beyond the variation; and B does not exceed E beyond the variation.
- **Retained application data** when G is at least 70% of ΔL with |U| below the registered share, and B's upper bound is below 30% of E.
- **In-flight transport state** when T is at least 70% of ΔL with |U| below the registered share, and B's upper bound is below 30% of E.
- **Composite** when no single component reaches 70% but K, G and T together reach at least 70% of ΔL with |U| below the registered share, and B's share of E agrees with K's share of ΔL within a registered tolerance. Each component's share is reported.
- Otherwise **mixed**, **inconclusive**, an **evidence gap** or an **instrumentation gap**. Accounting shares alone never assign a label: every label needs the ballast reading as well.

**Scope of retained application data, proposed for acceptance with this decision.** The previous D7's "delivery retention" route covered excess placed in delivery storage. This decision names the exception-eligible scope explicitly: receiver delivery storage and, in addition, sender data kept until acknowledged and queued DATAGRAM payloads. Packet and acknowledgment metadata are not in it; they are in-flight transport state, routed separately.

**Disclosed before data.** The allowance table predicts that K could account for most of the S5 sender excesses and much of the loopback receiver excesses, but well under half of the S6 senders' and STREAM receivers' excesses. #740's small logical receive occupancy weighs against G explaining the STREAM receivers, so those cells may end unresolved. For context, each S6 reading reports G and T against the candidate's delivered bandwidth-delay product; that comparison is descriptive and assigns no label.

**Inventory.** S5, S6 and loopback (receiver cells), each 5 arms × 2 workloads × 4 blocks = 40, so 120 observations, with reruns capped at 20, so the ticket stays within **140** Linux observations. Paths or workloads without a raised cell are dropped and their observations released. Calibration, preflight and activation runs are excluded from statistics and listed.

**Endings.** Every cell ends with one of the labels above, the revision named, and goes to the decision.

### D6 — Then a fresh decision on every outcome

A new grilling ticket, blocked by Ticket L, re-asks this question on whatever J, K and L find. Its options are the same as this ticket's. **Terminal routing:**

| Outcome | Route |
| --- | --- |
| J: depth raises latency, mechanism queue wait, remedy kept | K re-demonstrates J's survivor; L attributes on it. |
| J ends without a kept change | K closes as not run; L attributes on r9 against #739's flags. |
| J: depth raises latency, remedy negative or null | That mechanism is disposed of. The cell stays unresolved and blocking, and the cost stays a candidate for remedy: the next decision may choose another admissible class from D3. It may consider an exception for the depth trade-off only once every admissible class named in D3 has a negative or null registered outcome. |
| J: depth raises latency, remedy inconclusive, unbuildable, gates failed or not attempted | The remedy is untested. Unresolved and blocking; a candidate for remedy, not for exception. |
| J: depth raises latency, mechanism load or other, mixed, or a gap | Unresolved and blocking; a further cause stage is the next decision's choice. |
| J: BBR baseline (depth 8 reproduces against Reno) | That component stays unresolved and blocking, alongside any depth reading. A kept remedy can reduce the cell without clearing it. |
| J: no depth effect; or inconclusive | Unresolved and blocking. |
| K: a newly raised cell outside L's scope, or a raised Reno-on-candidate cell | Reported as unresolved; a preservation cell blocks, under its existing rule. No stage of L attributes it. |
| K: a raised loopback STREAM candidate control p95 | D4's interpretation applies. |
| L: S6 p95 attributed to the Up policy (new survivor only) | Attributed behaviour, eligible for an exception decision. Otherwise unresolved. |
| L: controller bookkeeping | Two parts. The share of its resident cost that is GC headroom is an integration cost: a candidate for a contract-preserving remedy in how the structures are held, not for exception, while such a remedy is untested. The bounds themselves are evidence for considering a separate design-bound decision. That decision considers a specific structure's bound only on K's split between the ring and the delivery records. This route is new for L's outcomes; it does not reuse the previous ring-specific trigger. |
| L: retained application data | Attributed behaviour, eligible for an exception decision, within the scope accepted in D5. |
| L: in-flight transport state | Attributed behaviour. No earlier route names it, so whether it is eligible for an exception is an explicit question for the next decision. |
| L: composite | Each component's route for its share. |
| L: mixed; inconclusive; gap | Unresolved; not eligible for a cause-based exception. |
| A prerequisite gap or unusable instruments | The stage ends as a gap and its ticket closes to the next. |
| Flags persist | Reported as unresolved to the decision. |

A design-bound or contract-change decision is considered only on the evidence these rows name.

### D7 — No paid experiment, no campaign, no Mac readiness

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) is unchanged. New-revision Mac readiness is not required: the accepted D1 allows declared-platform advancement with the Mac records reviewed, and any advancement would name Linux only. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D8 — BBRv1 stays parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open, and #673 is re-wired to the new decision ticket. The open costs are memory and a transport-integration latency, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the S6 benefit.** Nine cells are unresolved.
- **Decline.** No correctness or contract failure; every goodput and CPU cell passes; each open cell has a measurable next step.
- **Grant exceptions now.** They cannot unlock advancement, and a new revision would need re-attribution.
- **Revert to depth 8 to clear the latency cell.** It gives back the loopback goodput the queue bought, and the depth-8 baseline is itself untested at an adequate sample.
- **Open a D08 2Q-profile decision.** #738's 4Q arm was inert, so the previous D7's credit-responsive trigger was not met.
- **Rerun #740's removal-arm design with more blocks, or with comparability bands chosen after seeing its data.** More blocks might label one cell (the loopback STREAM receiver was comparable in three blocks and explained in three, two of them the same), but the scatter would still confound the others, and bands chosen after the data would be a post-outcome rule change. L covers that cell with arms that do not change the candidate's traffic.
- **Open the outcome-ring design-bound decision now.** The routed trigger, "H: sender bookkeeping (ring)", was not met: the ring removes about half of one cell's excess (mixed).
- **Allocate the ring lazily at receivers.** Receivers fill it with ACK-only registrations at these rates, so it would not shrink.
- **A rate-capped candidate arm on S6.** An application-limited BBR at Reno's rate changes the operating point and shrinks occupancy-dependent bookkeeping too, so it cannot isolate retained data. L reports G and T against the delivered bandwidth-delay product descriptively instead.
- **Change the memory or latency limits, add an absolute latency floor, or scale the memory limit by delivered rate.** That would weaken an accepted criterion.
- **Shrink the bookkeeping capacities now.** A design-bound change, and the cells are unattributed.
- **Run J and L in parallel.** A kept J change could change the loopback memory cells, and both share one measurement host.
- **Require new-revision Mac readiness.** Not required by D1 for Linux-only advancement.
- **Any paid run, host-setting change or BBRv1 work.**

## Map changes on acceptance

- A resolution comment on #741, the issue closed, and one Decisions-so-far line on #666.
- New tickets J (task), K (task), L (task) and a new grilling decision ticket, wired J → K → L → decision. #673 is re-wired from #741 to the new decision ticket.
- Fog updated:
  - the loopback control-latency cause becomes Ticket J;
  - the memory composition becomes Ticket L;
  - a bookkeeping representation remedy and a bookkeeping design-bound decision stay in fog, behind L's routing;
  - the production-slicing and adoption-evidence fog stays.
- Glossary: **Controller bookkeeping**, **Retained application data** and **In-flight transport state** added to `CONTEXT.md` on this branch, as the labels this decision uses.

## Consideration dispositions

**First consideration**, RAS run `20261013T161430-e30ad9d7c477c864ec020ca6` (five reviewers, five adjudicators; 74 finding clusters, 370 adjudications). Its synthesis step failed on a prompt-size limit (900,519 bytes against 786,432), so no synthesis exists; every cluster is dispositioned here from the adjudications. Of 370 adjudications, 4 disagreed. Figures behind F3, F5, F12, F15, F21 and F23 were checked against `memory.json`, #739's `summary.json` and `latency-lead.json`; the source facts behind F1, F6 and F9 were checked at r9.

| Fix | Clusters | Disposition | Where |
| --- | --- | --- | --- |
| F1 — J's recorder cannot decompose a tail, and misses the reply path | C-001, C-056, C-050, C-028, C-073 | Accepted. Per-request segment timing at both endpoints, including the receiver's 32-entry queue; a tail-conditional share with a bootstrap interval, taken on the recorder arms only, after a consistency check against the plain arms. | D3 Stage 0, Stage 1 |
| F2 — J's perturbation gate checks goodput, not latency | C-033, C-072 | Accepted. A latency perturbation check is added. If it fails, the mechanism reading is a gap and the plain-arm depth reading still stands. | D3 Stage 0 |
| F3 — J's comparison form, direction and placement | C-025, C-037 | Accepted. Interval predicates are defined with direction; r8 is replaced by r9 at depth 8, a one-constant control. The goodput guard still evaluates the actual new binary. | D3 Builds, Stage 1, keep rule |
| F4 — J's Stage 2 lacks the long-run A/A | C-018, C-020, C-044, C-045, C-047 | Accepted. A second r9 is added to Stage 2's `loopback-long` arms; inventory 156, cap 180. | D3 Stage 2 |
| F5 — Baseline against clearance, and the reroll risk | C-012, C-069, C-022 | Accepted. The baseline uses all three `latency_preservation` outcomes, Stage 1 cannot revise #740's verdict, and the lead table adds frozen Reno's A/A, which shows the baseline lead is weak. | Evidence; D3 Stage 1 |
| F6 — Remedy space and a combined queue-plus-baseline outcome | C-011, C-032 (combined part) | Partly accepted. Admissible remedy classes are named and trial-chosen depths excluded; a combined row is added. Not adopted: that no admissible remedy remains (the adjudicators found this overstated; batching is still open, since `maxSendBatch` stays eight). | D3 Stage 2; D6 |
| F7 — A failed or untested remedy routed toward exception | C-026, C-027, C-034 | Accepted. Untested remedies stay remedy candidates; an exception for the depth trade-off is considerable only once every admissible class has a negative or null outcome. | D6 |
| F8 — The latency mapping extended to a new revision | C-002 | Accepted. Stated as an operator interpretation proposed for acceptance with this decision. | D4 |
| F9 — The ballast does not transfer from Reno to the candidate | C-016, C-042, C-052, C-064, C-055, C-007, C-051 | Accepted. Labels now require a candidate-side condition on the live-heap scale (K's share of ΔL); the ballast confirms resident translation and may not over-explain; heap goals and the 4 MiB floor are reported; the ballast has a fixed and an occupancy part, sized from the survivor's own calibration. C-055's premise that the bookkeeping is pointer-bearing is not adopted (it is scalar except ≤4,096 retained-delivery nodes), but the shape difference is disclosed. | D5 |
| F10 — The unwritten-ballast "inert" case | C-017 | Accepted. Replaced by recovery of a known allocation both below and above the heap-goal floor. | D5 Stage 0 |
| F11 — Ballast comparability: floors and traffic categories | C-003, C-013 | Accepted. #740's floors; receiver feedback and control replies added; the claim that a ballast "changes nothing Reno sends" is withdrawn. | D5 Stage 1 |
| F12 — Explicit bytes compared with resident excess; denominators | C-024, C-059, C-038, C-070, C-048 | Accepted. Shares are taken on the live-heap scale (ΔL from the runtime), with guards on every denominator. | D5 Statistics, Rule |
| F13 — The rate-matched S6 arm | C-008, C-010, C-049, C-053, C-063 | Accepted. Removed; a descriptive bandwidth-delay comparison replaces it. | D5; Rejected |
| F14 — Retained-data scope and the glossary | C-031, C-061, C-006, C-065, C-035, C-057, C-043, C-058 | Accepted. Sent-packet and received-packet metadata become in-flight transport state, routed separately; payload is counted once; the exception scope is named for operator acceptance; the glossary entries are rewritten and relate to *Outstanding delivery evidence* and *Delivery snapshot*. | D5; D6; `CONTEXT.md` |
| F15 — Gauges drain; occupancy evidence undisclosed | C-040 | Accepted. Per-cycle high-water marks and pinned capacity; #740's logical occupancy is disclosed with its caveats. | Evidence; D5 |
| F16 — L's estimand, variation and inventory | C-004, C-015, C-014 | Accepted. External 1 Hz peak with `ru_maxrss` agreement required; the variation sources are named; "at least three usable blocks"; S6 p95 uses existing arms, so the inventory is closed at 120 (cap 140). Numerical thresholds stay with the registration. | D5 |
| F17 — Routing completeness | C-009, C-046, C-066, C-032 (S6 part) | Accepted. Rows added for J ending without a kept change, K's outside-scope flags and its latency cell, L's S6 re-attribution, and persisting flags. | D6 |
| F18 — The design-bound trigger and the non-binding guidance | C-023, C-030, C-067, C-029, C-062 | Partly accepted. The guidance is removed. The bookkeeping route is kept as a new route for L's outcomes, which the adjudicators found within this decision's remit, but it is restricted to K's structure split, and the table states that design-bound decisions follow only from its rows. | D6 |
| F19 — The GC-headroom share skips the remedy step | C-060 | Accepted. That share is a candidate for a contract-preserving remedy in how the structures are held, before any exception. | D6 |
| F20 — #738's outcomes and the D08 2Q option | C-021, C-074 | Accepted. Classification corrected; the D08 2Q-profile option is disposed of (no trigger). | Evidence; D1; Rejected |
| F21 — The allowance multiplier | C-054 | Accepted. Range of 1.1–2.2× from the ring arm's measurements, with predictions softened. | Evidence |
| F22 — Scatter, not a systematic shift | C-039, C-041 | Accepted. #740's reading restored; the one cell more blocks might label is named, and L covers it. | Evidence; Rejected |
| F23 — Figures and wording | C-005, C-068, C-071, C-019, C-036 | Accepted. +71.7 ms; 0.94; Reno S6 DATAGRAM p95 125.8 ms; p50 labeled; the condition rows reworded. | Evidence; D2 |

**Second consideration:** pending.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The allowance table, the queue-depth, ring and bookkeeping-shape facts, and the fixture's control path come from source inspection and arithmetic on recorded figures, and are not measurements. #738's latency figures come from instrumented builds at 20 replies per run, in a ticket that did not register them.
