# BBRv3 WAN-rate transport correction choice

Status: proposed decision set for [Choose the correction for BBRv3's remaining WAN-rate transport costs](https://github.com/the-sarge/quic-go-fast/issues/708), under the [BBR Wayfinder map](https://github.com/the-sarge/quic-go-fast/issues/666). Not yet accepted by the operator. Planning only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-wan-correction-choice`, based on `80466857` (the [correction demonstration](../2026-10-02-bbr-correction-demonstration/README.md) record, which contains the full C1–C4 candidate at `8a0e0962` plus its docs). The [pacing reference note](../2026-10-02-bbr-pacing-reference/README.md) is copied from `601766e8` for self-containment.

## Question

The full C1–C4 candidate is loopback-competent and shows a repeatable ~16× useful benefit on an injected-loss WAN path (S6). At matched load on a lossless 100 Mbit/s, 100 ms path (S5), sender CPU per useful GiB is about 1.75× Reno. Two transport-integration costs outside the earlier selected correction cause it. Diagnostic removal of both brings sender CPU to 1.059× (STREAM) and 1.175× (DATAGRAM). Decide:

1. The ECN correction's bounded-work shape and its equivalence oracle.
2. Whether, and how, to change pacing wakeup granularity (a contract change).
3. How to treat the STREAM receiver heap rise seen under the diagnostic quantum intervention.
4. How to treat the residual DATAGRAM sender CPU.
5. Whether the remaining peak-RSS sources need work or are accepted.
6. How the readiness limits apply where Reno runs far below capacity.

## Normative constraints (quoted)

Acceptance plan ([2026-09-23-bbrv3-acceptance-plan.md](../2026-09-23-bbrv3-acceptance-plan.md)), the thresholds table:

> | Advisory flag, compared with matched Reno | Owner-accepted attention threshold |
> | Clean WAN or LAN receiver goodput | More than 5% lower |
> | CPU time per delivered GiB | More than 10% higher |
> | Peak memory | More than 10% higher |
> | p95 control-stream response time | More than 20% higher |

The same plan defines the pairing: "Use five paired Reno/BBRv3 runs per selected scenario and workload. Within a pair, match workload demand, emulator schedule and seed; randomize controller order."

Map #666 Notes adopt these as "local candidate-readiness criteria … with a repeatable useful benefit on at least one relevant difficult path before further qualification. Report absolute values, variability and workload/platform limits. These criteria apply to a competent candidate; failure triggers diagnosis and does not by itself reject BBRv3."

Map #666 Notes also require: "Separate implementation readiness from algorithm suitability … speculative tuning and switching controller versions to conceal a shared defect are not [in scope]. Causal claims require measurements and a discriminating comparison, not source inspection or a profile alone." And: "Preserve default Reno, protocol and payload integrity, packet/buffer ownership, bounded queue and delivery evidence, pacing and congestion limits, receive fairness, ECN/loss correctness and supported-platform behavior. Any necessary contract change is a separate explicit design decision."

Design [bbrv3.md](../../designs/bbrv3.md), "Classic ECN response" (ledger semantics this decision must not change):

> Maintain a marking ledger independent of sampler tombstones, capped at 4,096 range records. A record compresses only consecutive packet numbers with the same actual sent codepoint, path generation, ACK-accounting status and an affine transmission-ordinal mapping; otherwise start or split a record. … Retain actual marking and ordinal anchors for late feedback until a prefix is completely accounted for by individual ACK evidence and accepted cumulative counts; compact only such a prefix into cumulative offsets and its ordinal boundary. If insertion or ACK-induced splitting would exceed the budget, fail marking to Not-ECT before losing evidence and disable new CE/release claims requiring the unavailable detail.

Design, "Pacing and pending local work" (the pacing contract this decision proposes to amend):

> Pace registration opportunities with a token/debt balance capped at Q, initialized to Q, debited by actual UDP payload bytes admitted and replenished at the effective rate. Deadline uses exact deficit/rate rounded up to the monotonic clock unit; do not inherit Reno's software 1ms minimum delay. At high rates the 64KiB quantum needs sub-millisecond opportunities, while actual OS wakeup precision remains a measured limitation. On rate/quantum decreases, clamp unused credit immediately. Delayed wakeups cannot accumulate more than Q. While stopped for queue capacity, retain no extra catch-up credit beyond Q. Always check both recovery flight and local credit; low actual OS wakeup precision may lower throughput but never authorizes a larger unreported burst.

Decision table row D08: "Draft quantum with two-Q pending credit, BBR pacer and no implicit Reno rate gain | Reject eight-entry-only bound or unbounded catch-up; native scheduling cost remains to measure."

## Evidence base

From the [correction demonstration](../2026-10-02-bbr-correction-demonstration/README.md) and its [summary.json](../2026-10-02-bbr-correction-demonstration/summary.json) (one macOS host, loopback endpoints, userspace relay, 3 blocks per S5 intervention arm; ratios are medians against the same block's Reno):

| S5 arm | Sender CPU STREAM / DATAGRAM | Sender alloc GiB/GiB STREAM / DATAGRAM | Receiver RSS STREAM / DATAGRAM | Receiver heap peak (measured window) STREAM / DATAGRAM | Control p95 STREAM / DATAGRAM |
| --- | --- | --- | --- | --- | --- |
| Full candidate, ECN active | 1.749 / 1.771 | 5.41 / 13.31 | 1.374 / 1.044 | 1.298 / 1.219 | 0.933 / 0.927 |
| ECN stripped | 1.645 / 1.637 | 0.44 / 1.95 | 1.148 / 1.044 | 1.262 / 1.235 | 1.021 / 1.013 |
| Quantum wakeups, ECN active | 1.781 / 1.315 | 52.2 / 13.2 | 1.403 / 1.023 | 2.977 / 1.181 | 1.091 / 0.930 |
| Quantum wakeups, ECN stripped | 1.059 / 1.175 | 0.43 / 1.92 | 1.417 / 1.039 | 3.195 / 1.238 | 1.091 / 1.005 |

Reno allocates 0.18–0.22 (STREAM) and 1.66–1.69 (DATAGRAM) GiB per useful GiB. Stripping ECN makes QUIC ECN validation fail for both controllers, so the BBR ECN tracker short-circuits and BBR sends Not-ECT; the stripped arms are discriminators, not the corrected state.

Mechanisms recorded by the demonstration:

- **ECN tracker.** `bbrECNTracker.appendRange` under `captureBBRECN` from `ReceivedAck` is 91% of sampled sender allocation on S5 STREAM and 99% in a quantum run. Each advancing ACK scans every retained mark record against every ACK range (`internal/ackhandler/bbr_ecn.go`, `feedback`), then rebuilds the whole ledger into a fresh slice grown by doubling from 8 (`appendRange`). This is O(records × ACK ranges) work plus a fresh slice per advancing ACK. Loopback measured it at 7–8%; [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613) owned it as unmeasured.
- **Pacing wakeups.** `bbrSendPolicy.deadline(size)` (`bbr_send_policy.go`) returns the instant one datagram's credit accrues. Once the bucket drains, that is one wakeup about every 112 µs at 100 Mbit/s. The S5 STREAM profile difference against Reno is scheduler park/wake (`kevent` +1.70 s, `pthread_cond_wait` +0.84–0.92 s) and `sendmsg`. At loopback rates late wakeups accumulate up to Q, so the cost disappears there.
- **Quantum intervention side effects.** Breaks `TestBBRPacingQuantumAndDeadline` and `TestBBRPendingCreditMTUException/paced_probe`, which pin the exact-deficit deadline. Raises STREAM receiver measured-window heap peaks to about 3× Reno; the demonstration attributes this to "each wakeup now carries a burst", without a discriminating measurement.
- **Memory.** STREAM receiver warmup heap peak is 2.1× Reno (about 11 MiB at t=2–3 s, then 2.5–4.4 MiB). ECN churn is a GC high-water contributor. Sender peak RSS is 1.14–1.17× Reno at matched load with ECN stripped, not uniquely attributed; forced-GC live heaps are 2–7 MB against 21–32 MiB peak RSS; the recovery ledger contributes a fixed 1.2–1.7 MB live per endpoint.
- **S6.** Reno delivers 4.9–5.9 Mbit/s (6% of capacity); the candidate 85.9–89.1 Mbit/s. Candidate control p95 183.0/167.2 ms against Reno 129.0/121.9 ms (base RTT 100 ms); peak RSS 28–32 MiB against Reno's 19–20 MiB. Queueing above 25 ms occurs in ProbeBW Up and the following Down, which the accepted design selected (draft-06 Up, cwnd gain 2.25, no QUICHE queue-threshold exit). On S5, where Reno reaches 95.9/94.4 Mbit/s and fills the same queue, candidate control p95 is 1.013/0.987× Reno.

From the [pacing reference note](../2026-10-02-bbr-pacing-reference/README.md) (pinned primary sources): Linux TCP BBR (v7.2 `8d3ae592`; `google/bbr` v3 `90210de4`) sizes each GSO/TSO aggregate as `pacing_rate >> sk_pacing_shift` (about 1 ms, at least 2 segments, at most 64 KB) and advances the next send time per aggregate. QUICHE (`f9e75eb8`) has a 1 ms alarm granularity and sends early when the next ideal send time is within it, plus optional lumpy pacing. draft-06 §5.6.2–5.6.3 defines `send_quantum` as rate × 1 ms clamped to [2 packets, 64 KB] and paces quanta. At 100 Mbit/s every reference releases about 1 ms (~12 KB) per wake.

Local Reno pacer (`internal/congestion/pacer.go`, unchanged by this decision): burst budget `max(adjustedBandwidth × (MinPacingDelay + TimerGranularity), 10 × maxDatagramSize)` with `adjustedBandwidth = bw × 5/4` and both delays 1 ms, and `TimeUntilSend` never earlier than `lastSentTime + 1 ms`. At 100 Mbit/s that credit ceiling is about 31 KB (~23 datagrams), larger than BBR's Q of about 12 KB. It is a permitted maximum, not an observed release size; Reno's actual per-opportunity releases on S5 were not measured.

## Decisions

Revised after RAS consideration `20261002T233017-68ad98d6b35fd6e4ba36c989` of `0614bb0d`; the disposition of each finding is in [Consideration disposition](#consideration-disposition).

### D1. ECN feedback: work-bounded rewrite with unchanged semantics

The correction is implementation-only. Every ledger rule quoted above is preserved, including record identity, splitting only at ACK boundaries, adjacent merge of equivalent records, prefix-only compaction, the 4,096-record cap and failure to Not-ECT before evidence loss. No ECN contract change.

**Domain and invariant.** The supported input domain is caller-valid tracker operation sequences: registrations in increasing packet-number order with the ordinals, codepoints and generations the sent-packet handler produces, ACK frames as decoded from the wire, losses, path resets and drains. Ordinal-zero cases are included only where a real registration can produce them. Under this domain the ledger is canonical: every write path goes through `appendRange`, which merges a record into an equivalent predecessor, and `compact` only removes a prefix. So no two adjacent records are mergeable before or after any call. A frozen full rebuild can therefore only change merges at the edges of the region an ACK splits, which is why extending the rewrite window by one neighbour on each side closes both edges.

**Shape.**

- **Merge pass.** Ledger records are sorted, non-overlapping packet-number intervals; ACK ranges arrive in descending order. One cursor over both computes the newly ACKed per-codepoint counts and the largest-acknowledged anchor in O(records inspected + ACK ranges) instead of records × ranges.
- **Window-limited rewrite.** Locate by binary search the records overlapping `[smallest ACK range start, largest acknowledged]`, plus one neighbour on each side. Compute the merged replacement for that window into tracker-owned, reused scratch, then splice it into the ledger in place. Records outside the window keep their semantic contents; they may be physically relocated by the splice.
- **Failure atomicity.** Compute the merged post-split ledger length before mutating anything. If it would exceed 4,096, the outcome is exactly the frozen outcome when it discards `next`: `failEvidence()` sets the evidence-lost and failed state, and the ledger, accepted counts and watermark are otherwise unchanged. Validation order (counter consistency, missing anchor, delta coverage, then splitting) is unchanged.

**Work accounting.** Physical work has five parts, measured and reported separately: binary search, record inspection, semantic rewriting, suffix relocation (bulk `copy` moves caused by a splice that changes the window's length), and prefix compaction moves. The frozen code already moves the retained suffix on compaction. For this experimental correction, bounded linear relocation under the 4,096-record cap is accepted: it is a contiguous memory move with no allocation, rescan or ACK-range multiplication. Required evidence:

- zero allocations per advancing ACK in steady state;
- record inspections and semantic rewrites that stay flat as an unaffected suffix grows while the ACK and the affected window are held fixed;
- moved records counted, including bulk copies, for both splitting and prefix compaction.

Whether relocation is acceptable in production is left to the re-demonstration's CPU result and later production slicing. Suffix occupancy is not bounded by recovery flight: individually ACKed records can stay retained behind an unresolved prefix.

**Equivalence oracle.**

- Retain the frozen `feedback`, `appendRange` and `compact` verbatim in a `_test.go` reference.
- Differential tests drive both implementations with identical sequences from the declared domain. After every operation they compare the semantic snapshot and every returned `ECNResult`, including pre-validation result fields on failure paths. The snapshot covers records (bounds, ordinal, generation, codepoint, ACK status), compacted counts and ordinal, sent and accepted counts, offsets, watermark, generation, fence, state, draining, testing slots and testing counts, evidence-lost and counter-failed flags, and each `mode()` return. It excludes function identity and representation-only storage such as slice capacity.
- Sequences: generated plus adversarial cases covering packet-number gaps and skipped numbers, Not-ECT and ECT(1) holes, non-affine ordinals, generation reset and drain, reordered, duplicate and lower-largest ACKs, heavily fragmented ACK frames, and edge merges on both window sides. Cap cases cover 4,095/4,096/4,097 for insertion and for ACK-induced splitting, merge-at-cap, overflow before compaction despite a reclaimable acked prefix, and combined invalid-counter with over-cap.
- Mutation sweep, scoped semantically: mutate the new merge cursor, window bounds, neighbour extension, merge predicate, projected-length check and splice. Every surviving mutant needs a written disposition as observationally equivalent or unreachable within the declared domain.

Rejected: a new data structure such as an interval tree or ring. Windowed rewriting already removes the per-ACK allocation and the records × ranges scan, and a different representation would enlarge the equivalence surface.

This is the "scratch reuse or a sorted merge cursor" that #613 named for a measured cost. The experimental build is not #613's production implementation; #613 remains the production owner and receives the measurement and the chosen shape.

### D2. Pacing: release a full quantum per wake (amendment to D08)

When pacing credit cannot cover the next paced admission, the next opportunity is the instant credit reaches Q, rather than the instant it covers that admission. This applies to every caller of the BBR pacing deadline: ordinary sends and DPLPMTUD probes. Sub-Q MTU probes, which today wait for their own size via `deadline(min(probeSize, Q))`, now also wait for Q; probes larger than Q already wait for Q. The extra probe delay is at most `(Q − probeSize)/rate` for an isolated probe.

Unchanged:

- Q's formula, the token cap at Q, initial credit Q and debit by actual UDP payload bytes;
- no borrowing for ordinary sends; ceiling rounding to the monotonic clock unit; no 1 ms software minimum;
- immediate clamping on rate or Q decreases, delayed wakeups accumulating at most Q, and no catch-up credit beyond Q while queue-stopped;
- two-Q pending local credit;
- isolated-probe admission (ordinary pending zero, no other queued work until it completes) and probe-debt accounting in `sentProbe`;
- the ACK-only, authorized PTO and path-validation exemptions, which wait for local credit and do not use the BBR pacing deadline for their own admission.

Amend the design sentence "Deadline uses exact deficit/rate rounded up to the monotonic clock unit" to "When credit cannot cover the next paced admission, the deadline is the exact time for credit to reach Q, rounded up to the monotonic clock unit", and record the change in D08's row. Rewrite `TestBBRPacingQuantumAndDeadline` and `TestBBRPendingCreditMTUException/paced_probe` to pin the new rule. Add deterministic deadline cases for floor (Q = 2M), rate-derived and capped (64 KB) Q, fractional credit, rate/Q decreases, and probes below, equal to and above Q including post-send credit and debt. Add emission-level checks that exempt traffic still proceeds with ordinary pacing credit depleted and under constrained local credit.

Rationale:

- It matches Linux TCP BBR's per-aggregate pacing and draft-06's quantum pacing. Both keep a two-packet floor. It also matches the glossary's **Send quantum** ("the amount of traffic a pacing decision permits to leave together before another pacing opportunity").
- It raises no permitted burst bound: credit is already capped at Q and starts at Q, delayed wakeups already accumulate up to Q, and the no-borrowing, two-Q pending and probe rules still apply. It reduces steady-state wakeups from about 9 to 1 per quantum at 100 Mbit/s. Actual release patterns can still change, which D3 addresses.
- Keeping D08 as is fails the CPU flag even with ECN cost removed (1.645/1.637).

Latency disclosure:

- Refilling from empty to Q takes Q/rate. The added delay relative to today's rule is (Q − admission size)/rate. Where Q = rate × 1 ms, that is under 1 ms; at the 64 KB cap it is less.
- In the floor regime Q = 2M, at effective rate M/T an M-byte admission waits about 2T instead of T. At low rates this added delay can be much longer than 1 ms. The cited S5 evidence does not measure this regime.
- Measured control p95 ratios to the same block's Reno (median [min–max], 3 blocks each): S5 quantum with ECN active STREAM 1.091 [1.090–1.399], DATAGRAM 0.930 [0.885–0.968]; S5 quantum with ECN stripped STREAM 1.091 [0.928–1.151], DATAGRAM 1.005 [0.930–1.259]; loopback quantum STREAM 0.902 [0.684–0.958], DATAGRAM 1.223 [1.087–1.943]. The loopback per-datagram DATAGRAM comparator is itself 1.459 [1.127–1.895], and a Reno-against-Reno control (Reno with ECN stripped) reaches 1.271 in one S5 DATAGRAM block, so these blocks are noisy. The medians do not show a quantum-caused latency regression. They also do not establish that every quantum observation stays within the 20% flag. The re-demonstration reports control p95 with absolute values, variability and flagged blocks.

Rejected:

- **QUICHE-style 1 ms early-send window.** It sends ahead of accrued credit, which "ordinary sends cannot borrow against future credit" forbids.
- **Lumpy pacing alone.** It reduces wakeups only about 2–3× and adds unpaced packets beyond credit.
- **A fraction of Q.** No reference does this; choosing a fraction is speculative tuning.

### D3. STREAM receiver heap rise: D2 is adopted only with a discriminating explanation

Under quantum wakeups with ECN stripped, STREAM receiver measured-window heap peaks are 3.195× Reno and receiver RSS 1.417× (above the 10% flag). ECN is excluded as the cause by that arm. The per-datagram baseline already shows receiver memory excess (RSS 1.148× with ECN stripped), so the incremental quantum effect needs its own attribution. The demonstration's explanation, that "each wakeup now carries a burst", is unresolved, neither confirmed nor refuted:

- Reno's ~31 KB is a credit ceiling, not an observed release size, so it does not show that Reno releases larger bursts.
- The relay assigns serialized modeled delivery deadlines but writes overdue deliveries consecutively. Its recorded stalls (30 of 116 WAN runs over 5 ms) mean modeled spacing does not establish physical spacing at the receiver.

The D2 build attributes the rise with a discriminating comparison on the D1-corrected candidate. It compares per-datagram and quantum arms on receiver heap profiles by allocation site at the peak, receive-queue and stream flow-control occupancy, and the relay's per-arm lateness and `LateOver1ms` records. It uses enough repetitions to separate the effect from GC-timing variance. Stalled runs are not excluded after their outcomes are seen.

Adoption outcomes (each yields an explicit status recorded on ticket 2):

| Attribution result | D2 status |
| --- | --- |
| Positive discriminating evidence of a measurement or fixture artifact | Adopted; artifact evidence reported |
| Implementation defect, fixed within existing contracts while preserving Reno and receive fairness | Adopted after the fix, on the identified candidate revision |
| Genuine quantum effect with whole-run receiver peak RSS within the 10% flag | Adopted; measured-window heap evidence disclosed; any unrelated RSS excess handled under D5 |
| Genuine quantum effect keeping whole-run receiver peak RSS above the flag | Stopped; reopen the pacing decision |
| Mixed causes | Adopted only if every quantum-attributed part falls in an adopting row; any unresolved or disqualifying quantum part stops |
| Inconclusive attribution | Stopped; reopen the pacing decision |

An artifact classification needs positive evidence; failure to find a cause is "inconclusive". A stop never leads to tuning Q or adopting a fraction of Q.

### D4. Residual CPU: re-measure, then diagnose with a discriminating comparison; no waiver

The 1.175× DATAGRAM residual was measured with ECN validation failed and BBR sending Not-ECT. The corrected candidate keeps ECN validation and the CE response active, so the residual is re-measured on the D1+D2 candidate. The CPU flag applies to every workload and both endpoints, not only DATAGRAM senders.

For any CPU flag raised in the authorized comparisons:

1. Use profiles or counters against Reno at the same pairing to form a hypothesis about the excess work.
2. Run a comparison or intervention that distinguishes that hypothesis from the named competing explanation.
3. Only then declare attribution or choose a fix.

A hot frame alone does not satisfy step 2. An unresolved explanation stays recorded as a raised, unattributed flag. No fix is preselected and no flag is waived.

### D5. Peak memory: decide on re-measured evidence under a fixed rule

ECN churn feeds both receiver and sender peaks through GC high water, so both are re-measured after D1. The flag remains whole-run peak RSS against matched Reno, with measured-window values reported alongside. A capacity bound alone neither proves that associated allocation is necessary nor establishes a defect. Each remaining excess is classified by evidence, following D4's discriminating-comparison workflow:

- **Implementation churn or retention:** allocation shown not to be required by a design bound. Fix it in scope, on an identified candidate revision.
- **Design-bounded retention:** shown to be required by an accepted design bound. Disclose it with absolute MiB for the qualification decision. Example candidate, not yet classified: the recovery-outcome index, whose 32,768-entry bound (`maxRecoveryOutcomes`) is allocated in full on first registration and matches the demonstration's 1.2–1.7 MB live per endpoint. Whether full up-front allocation is required is an open question for this rule, not presumed either way. The 4,096 bound belongs to the separate retained-delivery tombstones (`maxDeliveryRetained`).
- **BBR Startup transient:** shown by attribution to come from the high-gain Startup delivery pattern. Report it as algorithm behaviour with whole-run and steady-window values, weighed in the qualification decision. It is not a reason to alter Startup.
- **Contract-change cause:** a cause that would need a contract change to remove. Record it as a raised flag with the required change named, for an explicit later design decision.
- **Unresolved:** excess not attributed by a discriminating comparison, including today's 1.14–1.17× sender excess if it persists. It remains a raised flag carried to the qualification decision, with the re-demonstration ticket as owner of the attempted attribution.

No classification turns a raised flag into a pass. Design-bounded, Startup, contract-change and unresolved results all remain raised flags with disclosed attribution.

### D6. Applying the readiness flags on impaired paths

"Matched Reno" is the paired Reno run with the same workload demand, emulator schedule and seed. It does not require equal achieved throughput.

- On every measured path, including loopback, S5 and S6, report CPU per delivered GiB, peak memory and control p95 against matched Reno. Report ratios and flags with absolute values, variability and each controller's utilization.
- The goodput flag is defined for "clean WAN or LAN" paths. It applies to loopback and S5 and is inapplicable to injected-loss S6. On S6, goodput is reported as the useful-benefit comparison.
- A raised flag triggers diagnosis and does not by itself reject BBRv3. On S6 the demonstration already attributes the control p95 flag to the selected draft-06 ProbeBW Up policy on a one-BDP buffer (183.0/167.2 ms against Reno 129.0/121.9 ms at 100 ms base RTT). It partly attributes the RSS flag to loss-driven reassembly that a 5 Mbit/s Reno never accumulates. These remain raised flags with attribution and utilization caveats, carried into [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672) for explicit weighing. This decision changes neither the Up policy nor the acceptance plan.
- S5 serves as a supplemental load-comparable diagnosis, since there both controllers fill the same queue. It does not replace the S6 flags.

Glossary addition (CONTEXT.md): **Matched Reno** — the Reno reference run paired with a candidate run under the same workload demand, emulator schedule and seed. Matching is of conditions, not of achieved throughput: a loss-starved Reno is still the matched reference, and unequal utilization is reported as context rather than removing the comparison.

## Follow-up tickets (sequential)

All design-doc, test and code changes in these tickets belong to experimental candidate branches. Production incorporation stays deferred to the map's production fog. Every measurement names the exact candidate revision it ran on.

1. **Build the bounded-work BBR ECN feedback correction with frozen equivalence** (task, AFK). D1 on a new branch from the C1–C4 candidate (`8a0e0962`); comment the measurement and shape on #613. Complete when the oracle, mutation dispositions and work accounting pass on an identified revision. If equivalence cannot be achieved, the ticket closes as a negative result and returns the ECN question to the map. Ticket 2 is not unblocked on a non-equivalent candidate.
2. **Adopt quantum-release BBR pacing and explain the STREAM receiver heap rise** (task, AFK). D2, the design amendment and test rewrites, on top of ticket 1's revision, with D3's attribution and adoption table. Blocked by ticket 1. It closes in one of two ways:
   - *adopted*: an identified D1+D2 candidate revision, plus any D3 fix;
   - *stopped*: experimental branch and evidence preserved; a new grilling ticket reopens the pacing decision. That ticket replaces ticket 2 as ticket 3's blocker, and a stopped D2 does not count as adopted.
3. **Re-demonstrate the WAN-corrected BBRv3 candidate in matched local comparisons** (task). Loopback non-regression, S5 and S6 with the same harness, on ticket 2's adopted revision, applying D4–D6. Blocked by ticket 2. Residual corrections that D4/D5 classify as in-scope implementation defects are built inside this ticket on a new, identified revision. The affected comparisons are rerun on that revision, so no result mixes revisions. Contract-change causes and unresolved results are reported, not fixed. The ticket reports every flag as passed, raised with attribution, or raised and unresolved to #672, without claiming readiness while any flag is raised. Ticket 3 blocks #672.

The production path (Planit slicing against main, reuse of #613/#630/#636, the D08 amendment and the existing spurious-undo phase sentence) stays in the map's fog until the re-demonstration lands.

## Non-goals

No change to Reno, the default controller, ECN ledger semantics, the Up probing policy, Startup, the readiness thresholds or the acceptance plan. No production merge, paid resource, campaign resumption, maintained benchmark framework, platform cross-product, hosted rerun or ledger change. Negative and inconclusive results are recorded.

## Consideration disposition

RAS consideration `20261002T233017-68ad98d6b35fd6e4ba36c989` on `0614bb0d` (agents claude-fable, claude-opus, codex-astra, codex-sol, grok; adjudication and synthesis complete).

| # | Finding | Disposition | Change |
| --- | --- | --- | --- |
| 1 | D6 replaced paired-condition matching with equal-throughput matching and dropped S6 flags | Accepted; the plan pairs on demand, schedule and seed | D6 rewritten; S6 flags kept with attribution; glossary term changed to **Matched Reno** |
| 2 | Flat-work claim ignores suffix relocation and compaction moves | Accepted | Work accounting split into five parts; bounded relocation under the cap accepted explicitly; flatness required of inspections and rewrites only |
| 3 | Sub-Q MTU probes share the deadline | Accepted | Sub-Q probes adopt the Q deadline; exemptions listed; probe tests added |
| 4 | D3 outcomes not exhaustive | Accepted | Adoption table covering artifact, fixed defect, within- and above-flag effects, mixed and inconclusive results |
| 5 | Burst-discount reasoning overstated | Accepted | Burst explanation marked unresolved; credit ceiling vs observed release and modeled vs physical relay spacing distinguished; relay lateness added to the attribution |
| 6 | D4 lacked a discriminating step | Accepted | Three-step workflow applies to every CPU flag |
| 7 | D5 cited the wrong bound and lacked an unresolved outcome | Accepted; verified `maxRecoveryOutcomes = 32768` and `maxDeliveryRetained = 4096` | Example corrected and left unclassified; contract-change and unresolved categories added; no classification converts a flag into a pass |
| 8 | Ticket completion and stop paths undefined | Accepted | Completion states, stop path, revision identity and residual-fix ownership defined |
| 9 | Latency claim used medians only and omitted the floor regime | Accepted | Floor regime disclosed as unmeasured; arm-level medians, ranges and noisy comparators reported |
| 10 | Oracle domain, snapshot and mutation scope not explicit | Accepted | Domain, canonical invariant, snapshot fields, cap cases and semantic mutation scope with survivor dispositions |

The synthesis's "Do not act on" items are also accepted. They are: no proven one-neighbour semantic failure, no burst-bound increase, no new rejection gates or measured-window heap threshold, no unmeasured causal labels, no scope expansion, and C-029 as wording only.
