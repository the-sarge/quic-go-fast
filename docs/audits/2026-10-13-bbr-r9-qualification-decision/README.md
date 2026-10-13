# BBRv3 qualification decision after the loopback and memory tickets

Status: draft decision set for [Decide whether the loopback- and memory-tested BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/741), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Approved in outline by the operator on 2026-10-13, who asked for a multi-agent consideration before acceptance. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-r9-qualification-decision`, based on `ddbac0a3` (the [r9 memory and latency attribution](../2026-10-08-bbr-r9-memory-attribution/README.md) record, #740). That record's history contains the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) (#739), the [loopback hand-off record](../2026-10-07-bbr-loopback-handoff/README.md) (#738) and the [previous decision](../2026-10-07-bbr-r8-qualification-decision/README.md) (#737). The surviving revision is **r9, `81e9dc8c`**: r8 (`95f5b6b7`) plus a 32-entry send queue for BBR connections. The first two decisions and the Mac records live on sibling record branches and are linked by commit.

## Question

On the evidence from the loopback, re-demonstration and memory-attribution tickets, with the preserved Mac records reviewed, should BBRv3 advance to further qualification, take another resolution step, take a bounded evidence-supported redesign of BBR-specific integration, open a separate contract-change or design-bound decision, receive a declared exception, or have further work on this candidate declined? Apply the unchanged D1 advancement condition; preservation flags still block. Dispose of the cells recorded in the previous decision's D2, any newly attributed cost and the remaining blockers, on every outcome of its terminal routing table. Distinguish failed selected interventions, missing evidence, demonstrated contract incompatibility and algorithm suitability.

## Normative constraints (quoted)

[First qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md), D1: the next decision "may consider advancing only when, for one identified candidate revision on the declared platform(s)": "the repeatable useful benefit holds on S6 (candidate goodput above Reno's in all five pairs of each workload)"; "every readiness flag either passes, or is raised with a cause attribution and an explicit operator exception for that revision, platform and cell"; "no flag is raised and unresolved, and no preservation flag is raised"; and "the preserved Mac flags are reviewed alongside the new results, and any advancement names the revision and platforms it covers." D2: "The exception does **not** carry over automatically. … A larger magnitude, a different cause, or a changed composition … gets no clearance from a cause label alone."

[Linux qualification decision](https://github.com/the-sarge/quic-go-fast/blob/efdac495/docs/audits/2026-10-03-bbr-linux-qualification-decision/README.md), D3: "a raised preservation flag blocks advancement." D6: "Declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

[Intervention qualification decision](https://github.com/the-sarge/quic-go-fast/blob/42e40111/docs/audits/2026-10-05-bbr-intervention-qualification-decision/README.md), D2, on a newly attributed implementation cost: "An implementation-integration cost on the declared platform, not an algorithm trade-off. It is a candidate for remedy, not for exception, while a contract-preserving remedy remains untested."

[Previous decision](../2026-10-07-bbr-r8-qualification-decision/README.md), D3, prospectively: "Any new revision re-measures the cell in readiness. A raised value there runs `preslat` again, and only its **not reproduced** outcome ends blocking. It is not an exception and covers no other cell." Its D7 routing table: "H: sender bookkeeping (ring) | Evidence for considering a separate design-bound decision; not an exception by itself"; "H: S6 p95 attributed to the Up policy | Attributed behaviour, eligible for an exception decision"; "H: delivery retention, with site or lifetime evidence | Attributed behaviour, eligible for an exception decision"; "H: localized, cause unresolved; mixed; inconclusive; gap; treatment inert or unusable | Unresolved; not eligible for a cause-based exception."

[#740's operator decision 2](../2026-10-08-bbr-r9-memory-attribution/README.md#operator-decisions-recorded-before-data), for the candidate's loopback STREAM control p95, accepted before data: "an adequate *not reproduced* outcome ends this cell's blocking state for r9 on Linux only, as in D3. *Regression reproduced*, *inconclusive*, *evidence gap* or unusable leave it blocking and unresolved."

This ticket's question: a separate contract-change or design-bound decision ("such as D08's 2Q pending profile or the outcome-ring bound") is "considered only on the evidence the routing table names". "An untested remedy is not a failed one, and declining work on an inefficient implementation is not a finding that BBRv3 is unsuitable."

Map #666 Notes: "Separate implementation readiness from algorithm suitability." "A substantial redesign of BBR-specific integration is in scope when evidence supports it; speculative tuning and switching controller versions to conceal a shared defect are not." "Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone. Record negative and inconclusive results." "Any necessary contract change is a separate explicit design decision."

## Evidence base

Readiness figures are from the [r9 Linux re-demonstration](../2026-10-08-bbr-r9-linux-redemonstration/README.md) ([summary.json](../2026-10-08-bbr-r9-linux-redemonstration/summary.json)): 110 observations on r9 against frozen Reno `e4f322cb` on `minimax`, all clean, none contaminated. Attribution figures are from the [r9 memory and latency attribution](../2026-10-08-bbr-r9-memory-attribution/README.md) ([memory.json](../2026-10-08-bbr-r9-memory-attribution/memory.json)): 124 counted observations, all clean, no rerun, no deviation. Ratios are medians of five per-block ratios against the same block's frozen Reno, with blocks crossing the limit in parentheses. The decisive figures below were checked against both JSON files.

### Advancement condition, checked on r9 (Linux)

| D1 condition | Result | Met? |
| --- | --- | --- |
| 1. Repeatable S6 benefit | 14.53× [12.25–15.74] STREAM, 14.74× [11.34–16.32] DATAGRAM, 5/5 pairs each; sender CPU per useful GiB 0.086 / 0.090× Reno | Yes |
| 2. Every flag passes, or is attributed and excepted | Two cells attributed (S6 control p95, both workloads), neither excepted | No |
| 3. No unresolved flag, no preservation flag | Nine unresolved candidate cells, one of them blocking under #740's accepted latency mapping; no Reno-on-candidate cell raised | No (unresolved cells) |
| 4. Mac records reviewed, platforms named | Reviewed below; any advancement would name Linux only | Reviewed |

### Raised cells on r9, with #740's endings

| Cell | r8 (#736) | r9 (#739) | Absolute, Reno → candidate | Ending on r9 (#740) |
| --- | --- | --- | --- | --- |
| S6 STREAM control p95 | 1.550 | **1.562 (5)** | 127.7 → 199.5 ms | Attributed: selected ProbeBW Up policy |
| S6 DATAGRAM control p95 | 1.469 | **1.459 (5)** | 125.9 → 187.3 ms | Attributed: selected ProbeBW Up policy |
| Loopback STREAM control p95 | 1.171, passing | **1.485 (4)** | 0.134 → 0.199 ms; at 120 s, 0.140 → 0.258 ms | Regression reproduced, 1.849 [1.672–2.014]; blocking, unresolved |
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

- **The removal arms could not keep traffic comparable.** Four clean A/A pairs gave comparability bands of 1–2%, while the treatment arms differed from the candidate by about ±1–5%. With Reno selected at the receiver, the receiver sent fewer packets per useful GiB in 14 of 16 WAN blocks, by up to 2%. That shift is systematic, so it changes traffic by construction, and more blocks would not remove it. The smaller ring changes eviction age; with it, S6 STREAM goodput was 1.1–5.0% lower.
- **The outcome ring's resident cost is about twice its bytes.** The 4,096-entry ring cut the STREAM sender's live heap by 0.98–1.13 MiB, matching its explicit 0.999 MiB, and peak resident memory by 0.5–2.7 MiB (median about 2.2). That is the GOGC=100 doubling the registration modeled.
- **Every candidate endpoint carries explicit BBR structures.** In `memory.json` (`descriptive.bbr_explicit`), each sender holds 2.00 MiB (STREAM) or 2.15 MiB (DATAGRAM) on S5 and S6 and about 1.23 MiB on loopback; each receiver holds 1.18–1.35 MiB. The outcome ring is 1.14 MiB at every endpoint.
- **The accounting places the excesses in heap memory.** File-backed pages, stacks and runtime metadata differ by little.
- **Both S6 control p95 cells meet the Up-policy rule** in all eight runs per workload (STREAM 98.6–99.4% of samples above 25 ms in Up or the Down entered from Up; DATAGRAM 98.5–100%).
- **The loopback STREAM control-latency rise reproduces at an adequate sample** (all six per-block ratios above 1; median 0.130 against 0.069 ms). No stage has looked for its cause.

### Fixed bookkeeping against the memory allowance (descriptive inference, not measurement)

The memory limit allows 10% above the same block's frozen Reno. Using #739's Reno medians, #740's explicit BBR bytes and #740's measured ring multiplier of about 2:

| Raised cell | Allowance (MiB) | Explicit BBR bytes (MiB) | Resident estimate at ~2× (MiB) | Excess E (MiB, #740) |
| --- | --- | --- | --- | --- |
| S5 STREAM sender | 1.80 | 2.00 | ~4.0 | 4.05–4.82 |
| S5 STREAM receiver | 1.69 | 1.34 | ~2.7 | 8.06–9.36 |
| S5 DATAGRAM sender | 1.79 | 2.15 | ~4.3 | −0.01–3.35 |
| S6 STREAM sender | 1.33 | 2.00 | ~4.0 | 6.55–9.80 |
| S6 STREAM receiver | 1.35 | 1.34 | ~2.7 | 11.10–16.24 |
| S6 DATAGRAM sender | 1.38 | 2.15 | ~4.3 | 5.76–7.38 |
| Loopback STREAM receiver | 1.45 | 1.18 | ~2.4 | 1.42–1.80 |
| Loopback DATAGRAM receiver | 1.87 | 1.19 | ~2.4 | 1.44–3.39 |

These estimates predict that the explicit structures alone exceed the allowance at most endpoints. They are not a measurement: the multiplier was measured only for the ring on STREAM, and the loopback senders pass (0.954 / 0.956) although they carry about 1.23 MiB of the same structures, so other differences can offset them. If the prediction holds, then for cells whose excess is mostly bookkeeping, attribution cannot make the cell pass. It can only make the cell eligible for a design-bound decision or an exception.

### The loopback control-latency lead (descriptive)

- **What changed.** r9 differs from r8 only in `bbrSendQueueCapacity = 32` (`send_queue.go`); Reno keeps eight entries. At loopback rates each BBR entry holds one packet. The constant is not platform-specific.
- **What the cell measures.** The fixture's sender writes a 32-byte message on a reliable control stream once per second and times the receiver's echo (`observeControl` in the [campaign fixture's session.go](../2026-09-25-bbrv3-q1-campaign/fixture/session.go), which #739 and #740 build from). The request travels through the sender's own send queue behind bulk data. (Source inspection.)
- **#738 Stage 2's runs** (r8 and r9 as instrumented hand-off builds with about a 2% recorder cost, frozen Reno plain; 20 replies per run; the six clean counted blocks). [lead.py](lead.py) reads them from #738's [raw.tar.gz](../2026-10-07-bbr-loopback-handoff/raw.tar.gz), matching each block's counted attempt to its `results.json`, and writes [latency-lead.json](latency-lead.json):

  | Comparison | p95 median [range] | p50 median [range] |
  | --- | --- | --- |
  | r9 ÷ r8 | 1.42 [1.14–1.82] | 1.54 [1.35–1.74] |
  | second r8 ÷ r8 (BBR A/A) | 1.04 [0.66–1.28] | 0.95 [0.91–1.20] |
  | r8 ÷ frozen Reno | 1.45 [1.14–1.62] | — |

  r9's p95 exceeds the A/A maximum in four of six blocks, and its p50 in all six. r8's instrumented p95 sits above frozen Reno's, whereas r8's plain readiness read 1.171 (#736).
- **Reading.** These are instrumented, small-sample figures from a ticket that did not register them. They are a lead, not an attribution. They suggest two components: the deeper queue, and a BBR baseline the 20-reply readiness screen did not flag.

### Source facts checked for this decision (source inspection, not measurement)

- **The outcome ring includes ACK-only registrations.** `recoveryEvidence` in `internal/ackhandler/bbr_recovery.go` allocates `maxRecoveryOutcomes = 32768` outcomes on first registration, and its comment says "The ring includes every registration, even ACK-only packets and excluded probes." A receiver therefore fills its ring with ACK-only packets at these rates. Allocating it lazily would not shrink it.
- **The delivery records grow with occupancy** and the 25,000 ceiling limits insertion, as recorded in the previous decision.

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
| Failed selected interventions | None new. #740's ring treatment was a diagnostic, never kept; it engaged and passed its gates. #738's 32-entry queue was **negative under its registered rule** (the S5 DATAGRAM sender CPU guard) and kept by operator decision; that cost now passes readiness (0.953). |
| Missing evidence | Eight memory cells (one mixed, five inconclusive, one evidence gap, one instrumentation gap); the cause of the loopback STREAM control-latency rise. |
| Attributed behaviour (not shown to be an algorithm necessity) | S6 control p95, STREAM and DATAGRAM (selected ProbeBW Up policy). |
| Implementation-integration lead | The loopback control-latency rise and r9's queue depth (descriptive, #738's runs). |
| Demonstrated contract incompatibility | None. |
| Algorithm suitability | No finding against BBRv3. |

### D2 — No exception granted now

No exception is granted, and the macOS D2 exception is not extended. With nine unresolved cells, no exception could satisfy D1, and any new revision would need re-attribution. Recorded for the next decision, without clearance:

- **S6 control p95, both workloads**, attributed to the selected Up policy on Linux r9: STREAM 1.562 (+71.8 ms), DATAGRAM 1.459 (+61.5 ms). STREAM's ratio is slightly above the macOS exception's 1.547, at a smaller absolute rise; DATAGRAM is below 1.481. Both are the leading exception candidates. An exception decision must weigh readiness's 30 replies per WAN run and #740's per-run timeline evidence.
- **#738's S5 DATAGRAM sender CPU rise** (+5% against r8): passes readiness at 0.953 and is not a flag. It stays a production-slicing question, as does #734's run-loop wake increase.

### D3 — Ticket J: what raises the loopback STREAM control latency, and one remedy if the queue is responsible

One task ticket on `minimax`, from r9, under #712's core layout and host settings, with no host-setting change. Every rule, threshold, synthetic case and inventory is committed in the ticket's record before any comparative data, with executable synthetic tests and an explicit inconclusive ending. Accepting this decision does not by itself authorize measurement; the registration does.

**Stage 0 — Control-path recorder (descriptive) and preflight.**
- *Instrument.* A measurement-only recorder times each control request on the sender: from the application write to packing into a packet, from packing to send-queue entry, the queue wait until the worker dequeues that entry, and submission. It records the entries and bytes queued ahead of each control-bearing packet. At the receiver it times arrival to application read, and the reply write. Aggregates only, as #738's recorder did.
- *Synthetic cases.* A deep queue, a slow worker, a busy receiver, a delayed reply write, and a delay the recorder cannot place, which must receive no label.
- *Perturbation.* The recorder must keep loopback STREAM goodput at least 0.99 of the plain build's. Otherwise the mechanism reading ends as an **instrumentation gap**, and Stage 1's plain-arm comparisons are still reported.

**Stage 1 — Attribution.**
- *Arms.* `loopback-long` STREAM (5 + 120 s), six blocks: frozen Reno, r9, a second r9 (the BBR A/A control) and r8, all plain; and r9 and r8 with the recorder.
- *Estimators.* #715's `latency_preservation` rule, unchanged, for r9 against frozen Reno and for r8 against frozen Reno. The same pooled p95 with a 95% block-bootstrap interval for r9 against r8, read against the r9 A/A pair's interval.
- *Components, each reported:*
  - **Send-queue depth** when r9 exceeds r8 beyond the BBR A/A interval and the recorder places at least 70% of the increase in the queue wait of control-bearing packets.
  - **BBR baseline** when r8 itself reads *regression reproduced* against frozen Reno; the recorder's composition for r8 is reported.
  - **Load or other** when r9 exceeds r8 but the queue wait explains less than 30% of the increase.
  - Otherwise **inconclusive**, an **evidence gap** or an **instrumentation gap**.

  Both a queue component and a baseline component can hold.
- *Inventory.* 6 arms × 6 blocks = 36 observations.
- *Disclosed before data.* #738's descriptive figures above.

**Stage 2 — One contract-preserving remedy, only if Stage 1 finds send-queue depth.**
- *Change.* BBR-only. D08's 2Q bound, the credit-return points, ownership, packet ordering and Reno's eight-entry queue stay unchanged. The mechanism is registered after Stage 1 and before comparative data. Restoring r8's eight-entry depth is not a remedy: it is r8, whose loopback goodput flags (0.943 / 0.854) are on record.
- *Gates before comparative data.* As the previous decision's D4 Stage 2: a fixed-input policy-equality oracle for admissions and credit accounting; lifecycle tests for refusal, wakeup rearming, isolated probes, generation reset and teardown; bound checks; #714's correctness, race and native-coverage gates; and detection of an injected second contract violation.
- *Arms.*
  - Loopback, both workloads, six blocks: frozen Reno, A/A frozen Reno, r9, a second r9, the new revision and Reno on the new revision.
  - `loopback-long` STREAM, six blocks: frozen Reno, r9 and the new revision.
  - S5 screen, both workloads, four blocks: r9, a second r9 and the new revision.
- *Keep rule.* Kept only when all of these hold, with at least five of six usable blocks:
  - on `loopback-long`, the new revision's control p95 against r9 falls beyond the BBR A/A interval;
  - loopback goodput in each workload is not lower than r9's beyond the BBR A/A range;
  - sender CPU per useful GiB is not above the BBR A/A maximum in either workload, and receiver integrity holds;
  - Reno on the new revision passes #714's preservation metric;
  - the S5 screen shows neither goodput nor sender CPU per useful GiB moving the wrong way beyond its range, under its own usability rule.

  Efficacy is reported separately from the keep outcome. Whether the cell then reads *not reproduced* against frozen Reno is also reported separately: if a baseline component holds, a kept remedy can reduce the cell without clearing it.
- *Inventory.* 72 loopback + 18 `loopback-long` + 24 S5 = 114 observations, released if Stage 2 does not run.

**Inventory and endings.** Stages 1 and 2 total 150 observations, and reruns are capped at 20, so the ticket stays within **170** Linux observations. Preflight and activation runs are excluded from statistics and listed. The ticket ends with Stage 1's components, a remedy status (kept, failed, inconclusive, gates failed, unbuildable, or not attempted) and a named surviving revision (r9 unless a change is kept).

### D4 — Ticket K: re-demonstrate only if Ticket J keeps a change

Blocked by Ticket J. If J keeps a change, Ticket K reruns #739's readiness stage on J's survivor, unchanged except for the revision, with its conditional preservation stages and the S5 timeline. Under the previous decision's D3 mapping and #740's operator decision 2, a raised loopback STREAM candidate control p95 runs #740's `latcand-stream` check again, and only an adequate *not reproduced* ends its blocking state. If J keeps nothing, Ticket K closes as **not run**: a readiness rerun of unchanged r9 would duplicate #739.

### D5 — Ticket L: memory attribution by bookkeeping ballast and retained-data accounting

Blocked by Ticket K, on the final survivor, against the latest readiness flags (K's, or #739's if K did not run). Its scope is every raised memory cell. If the survivor is not r9, it also re-attributes both S6 control p95 cells with #740's Up-policy rule unchanged.

**Why a new design.** #740's removal arms changed what they measured: Reno at the receiver shifted receiver traffic systematically, and the smaller ring changed eviction and S6 STREAM goodput. Neither confound shrinks with more blocks. This ticket adds instead of removes. A ballast added to frozen Reno changes nothing Reno sends.

**Stage 0 — Instruments (descriptive) and preflight.**
- *Ballast build.* Frozen Reno with a measurement-only overlay. At the connection's first packet registration at each endpoint, it allocates pointer-free memory equal to the candidate's explicit BBR structure bytes for that endpoint, path and workload, writes every page, and holds the memory for the connection's life. Sizes are registered from #740's `bbr_explicit` medians. Each block's candidate explicit bytes are reported beside them, and a block whose bytes differ by more than a registered tolerance is unusable for the ballast fraction.
- *Retained-data gauges.* Explicit bytes, sampled with resident memory, in Reno and candidate builds alike:
  - at the receiver: stream data held out of order for reassembly, received stream data the application has not read, and queued DATAGRAM payloads;
  - at the sender: stream data retained until acknowledged, the sent-packet history, and queued DATAGRAM payloads.

  The registration names each structure and how its bytes are read.
- *Resident memory.* Sampled from outside the endpoint process where possible (`smaps_rollup` at 1 Hz), so the arm comparison does not depend on an in-process overlay. #740's runtime classes may be kept descriptively.
- *Synthetic cases.* A ballast of known size whose resident effect is recovered; an unwritten ballast, which must read inert; traffic-dependent growth that no ballast matches; retained data of known size; and an unexplained excess, which must receive no label.
- *Perturbation.* The gauge overlay must keep goodput at least 0.99 of the plain build's on every path. On a failing path, gauge-dependent labels end as **instrumentation gaps**; the registration states how the ballast comparison stays usable there.
- *Activation.* An excluded run shows the ballast resident and held.

**Stage 1 — Arms.** Each raised cell's path and workload, four blocks:
- frozen Reno;
- a second frozen Reno (the **Reno A/A** control);
- **Reno with ballast**, comparable when its useful delivery, packets per useful GiB and forward overflow stay within the Reno A/A range;
- the candidate;
- a second candidate (the **BBR A/A** control);
- on S6 only, the **rate-matched candidate**: the candidate with its sending application capped at a registered rate, frozen Reno's S6 median goodput from the latest readiness. It separates retained data that grows with delivered data from other BBR state. It is a different operating point by design, and only its registered rule reads it.

**Statistics.** Per block, the excess E is the candidate's resident memory minus frozen Reno's, and the ballast effect B is Reno-with-ballast's minus frozen Reno's. Both are read as fractions of E against the Reno A/A and BBR A/A variation. At least three usable blocks are needed. A block whose E is non-positive or below a registered detectability threshold is unusable for fractions.

**Rule, per cell:**
- **Controller bookkeeping** when B is at least 70% of E beyond the A/A variation in at least three of four usable blocks.
- **Retained application data** when B's upper bound is below 30% of E and the gauges place at least 70% of E − B in retained-data structures, with the unexplained residual below a registered share. The gauges, sampled in the same runs, are the site evidence the previous decision's D7 requires for its "delivery retention" row.
- **Controller bookkeeping and retained application data** when B lies between those bounds and B plus the gauged retained-data excess reach at least 70% of E, with the residual below the registered share.
- On S6, every reading also reports whether the rate-matched arm removes at least 70% of E − B beyond the A/A variation, that is, whether the retained data grows with delivered data.
- Otherwise **mixed**, **inconclusive**, an **evidence gap** or an **instrumentation gap**. Accounting shares alone never assign a label.

**Disclosed before data.** The allowance table above predicts that bookkeeping alone accounts for most of the S5 sender and loopback receiver excesses, about half of the S6 sender excesses, and under a third of the STREAM receiver excesses. These are predictions to be tested, not findings.

**Inventory.** S5: 5 arms × 2 workloads × 4 blocks = 40. S6: 6 × 2 × 4 = 48. Loopback (receiver cells): 5 × 2 × 4 = 40. That is 128 observations, with reruns capped at 22, so the ticket stays within **150** Linux observations. Paths or workloads without a raised cell are dropped and their observations released. Any S6 p95 re-attribution arms are added to the inventory in the registration.

**Endings.** Every cell ends with one of the labels above, and the revision named, and goes to the decision.

### D6 — Then a fresh decision on every outcome

A new grilling ticket, blocked by Ticket L, re-asks this question on whatever J, K and L find. Its options are the same as this ticket's. **Terminal routing:**

| Outcome | Route |
| --- | --- |
| J: send-queue depth, remedy kept | K re-demonstrates J's survivor; L attributes on it. |
| J: send-queue depth, remedy failed, inconclusive, unbuildable or gates failed | An attributed implementation trade-off: control latency against the loopback goodput the deeper queue bought, with r8's goodput flags as the cost of the alternative. The next decision may consider an exception for it. It is not an exception by itself. |
| J: BBR baseline | That component stays unresolved; a further cause stage is the next decision's choice. |
| J: load or other; inconclusive; gap | Unresolved and blocking. |
| L: controller bookkeeping | Evidence for a separate design-bound decision on BBR bookkeeping (the outcome ring, the delivery records, and whether an endpoint that sends mostly ACK-only packets needs them). That decision may keep the bounds and except their cost, or change them. Not an exception by itself. |
| L: retained application data | Attributed behaviour, eligible for an exception decision. On S6 the rate-matched reading informs whether it is the cost of the delivered benefit. |
| L: both | Both routes, each for its share. |
| L: mixed; inconclusive; gap | Unresolved; not eligible for a cause-based exception. |
| A prerequisite gap or unusable instruments | The stage ends as a gap and its ticket closes to the next. |

**Non-binding guidance.** L is the third memory instrument on this candidate, after heap-site shares (#712 to #736) and runtime accounting (#740). If it leaves most memory cells unresolved, the next decision should weigh a design-bound decision on the bookkeeping, or declining further work on this candidate, before commissioning a fourth instrument. This does not bind that decision.

### D7 — No paid experiment, no campaign, no Mac readiness

BBRv3 is not advanced, so no qualification is chosen and no paid experiment is proposed. The cloud heartbeat stays paused, and the ledger and its reservations are untouched. [BBRv3 Q2: Run the accepted qualification campaign and publish evidence](https://github.com/the-sarge/quic-go-fast/issues/599) is unchanged. New-revision Mac readiness is not required: the accepted D1 allows declared-platform advancement with the Mac records reviewed, and any advancement would name Linux only. The adoption-evidence sketch stays in the map's fog: coexistence (L6/L7), CE response (S8), low-rate pacing (L4/L5), and C4 items 1 and 3.

### D8 — BBRv1 stays parked

[Assess the case for a local BBRv1 investigation](https://github.com/the-sarge/quic-go-fast/issues/673) and [Decide whether to investigate BBRv1 next](https://github.com/the-sarge/quic-go-fast/issues/674) remain open, and #673 is re-wired to the new decision ticket. The open costs are memory and a transport-integration latency, and the map rules out switching versions to conceal a shared defect.

## Rejected

- **Advance on the S6 benefit.** Nine cells are unresolved.
- **Decline.** No correctness or contract failure; every goodput and CPU cell passes; each open cell has a measurable next step.
- **Grant exceptions now.** They cannot unlock advancement, and a new revision would need re-attribution.
- **Revert to r8's eight-entry queue to clear the latency cell.** It trades one raised cell for the two loopback goodput flags the queue removed.
- **Rerun #740's removal-arm design with more blocks, or with comparability bands chosen after seeing its data.** The receiver-controller shift is systematic, and bands chosen after the data would be a post-outcome rule change.
- **Open the outcome-ring design-bound decision now.** The routed trigger, "H: sender bookkeeping (ring)", was not met: the ring removes about half of one cell's excess (mixed). D6 routes a whole-bookkeeping finding to that decision instead.
- **Allocate the ring lazily at receivers.** Receivers fill it with ACK-only registrations at these rates, so it would not shrink.
- **Change the memory or latency limits, add an absolute latency floor, or scale the memory limit by delivered rate.** That would weaken an accepted criterion. The rate-matched arm is an attribution comparison; it does not change the criterion.
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
  - a bookkeeping design-bound decision stays in fog, behind L's routing;
  - the production-slicing and adoption-evidence fog stays.
- Glossary: **Controller bookkeeping** and **Retained application data** added to `CONTEXT.md` on this branch, as the labels this decision uses.

## Consideration dispositions

Pending the multi-agent consideration requested by the operator.

## Limits

This is a planning decision. It claims no readiness and no platform qualification, and it authorizes no production merge, paid resource, campaign resumption, default-controller change or ledger change. Its figures come from the records cited above. The allowance table, the queue-depth and ring facts, and the fixture's control path come from source inspection and arithmetic on recorded figures, and are not measurements. #738's latency figures come from instrumented builds at 20 replies per run, in a ticket that did not register them.
