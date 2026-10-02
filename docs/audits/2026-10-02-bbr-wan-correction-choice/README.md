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

Local Reno pacer (`internal/congestion/pacer.go`, unchanged by this decision): burst budget `max(adjustedBandwidth × (MinPacingDelay + TimerGranularity), 10 × maxDatagramSize)` with `adjustedBandwidth = bw × 5/4` and both delays 1 ms, and `TimeUntilSend` never earlier than `lastSentTime + 1 ms`. At 100 Mbit/s that is about 31 KB (~23 datagrams) per opportunity, larger than BBR's Q of about 12 KB.

## Decisions

### D1. ECN feedback: work-bounded rewrite with unchanged semantics

The correction is implementation-only. Every ledger rule quoted above is preserved, including record identity, splitting only at ACK boundaries, adjacent merge of equivalent records, prefix-only compaction, the 4,096-record cap and failure to Not-ECT before evidence loss. No ECN contract change.

Shape:

- **Merge pass.** Ledger records are sorted, non-overlapping packet-number intervals; ACK ranges arrive in descending order. One cursor over both computes the newly ACKed per-codepoint counts and the largest-acknowledged anchor in O(records touched + ACK ranges) instead of records × ranges.
- **Window-limited splice.** Locate by binary search the records overlapping `[smallest ACK range start, largest acknowledged]`, extended by one neighbour on each side so the existing adjacent-merge outcome is reproduced at the window edges. Rewrite only that window in place using tracker-owned, reused scratch. Records outside the window are not touched.
- **Failure atomicity.** Compute the post-split ledger length before mutating anything. If it would exceed 4,096, leave the ledger, accepted counts and watermark exactly as the frozen code leaves them when it discards `next` (evidence fails, state unchanged otherwise). Validation order (counter consistency, missing anchor, delta coverage) is unchanged.

Equivalence oracle:

- Retain the frozen `feedback`, `appendRange` and `compact` verbatim in a `_test.go` reference.
- Differential tests drive both implementations with identical operation sequences and compare, after every operation, the full observable tracker state (records, compacted counts and ordinal, accepted/sent counts, watermark, state, failure flags) and every returned `ECNResult`.
- Sequences: generated plus adversarial cases covering packet-number gaps and skipped numbers, Not-ECT and ECT(1) holes, non-affine ordinals, generation reset and drain, reordered, duplicate and lower-largest ACKs, heavily fragmented ACK frames, cap boundaries at 4,095/4,096/4,097 for both insertion and ACK-induced splitting, and counter/anchor failures.
- A mutation sweep over the new code must be fully caught by the oracle.
- Work evidence: zero allocations per advancing ACK in steady state, and counted record visits that stay flat as ledger occupancy grows while ACK shape is held fixed.

Rejected: a new data structure (interval tree, ring). Windowed splicing already bounds the work and allocation, and a different representation enlarges the equivalence surface.

This is the "scratch reuse or a sorted merge cursor" that #613 named for a measured cost. The experimental build is not #613's production implementation; #613 remains the production owner and receives the measurement and the chosen shape.

### D2. Pacing: release a full quantum per wake (contract amendment to D08)

When ordinary pacing credit is short, the next opportunity is the instant credit reaches Q, rather than the instant it covers the next datagram. Everything else in the pacing contract is unchanged: Q's formula, the token cap at Q and initial Q, debit by actual UDP payload bytes, no borrowing for ordinary sends, ceiling rounding to the monotonic clock unit, no 1 ms software minimum, immediate clamping on rate/quantum decreases, two-Q pending credit, and the existing probe, ACK-only and path-validation rules (the isolated oversized probe already waits for a full quantum).

Amend the design sentence "Deadline uses exact deficit/rate rounded up to the monotonic clock unit" to "When credit cannot cover the next admission, the deadline is the exact time for credit to reach Q, rounded up to the monotonic clock unit", and record the change in D08's row. Rewrite `TestBBRPacingQuantumAndDeadline` and `TestBBRPendingCreditMTUException/paced_probe` to pin the new rule.

Rationale:

- It matches Linux TCP BBR's per-aggregate pacing and draft-06's quantum pacing, and the glossary's **Send quantum** ("the amount of traffic a pacing decision permits to leave together before another pacing opportunity").
- It does not raise any permitted burst bound: credit is already capped at Q, starts at Q, and delayed wakeups already accumulate up to Q. It changes the steady-state wakeup frequency only, from about 9 to 1 per quantum at 100 Mbit/s.
- Added latency for newly ready work while continuously pacing-limited is at most Q/rate (about 1 ms where Q = rate × 1 ms; less at the 64 KB cap). Measured quantum arms keep control p95 within the 20% flag (1.091/1.005).
- Keeping D08 as is fails the CPU flag even with ECN cost removed (1.645/1.637).

Rejected:

- **QUICHE-style 1 ms early-send window.** It sends ahead of accrued credit, which the design's "ordinary sends cannot borrow against future credit" forbids, and D08 already rejected it.
- **Lumpy pacing alone.** Reduces wakeups only about 2–3× and adds unpaced packets beyond credit.
- **A fraction of Q.** No reference does this; choosing a fraction is speculative tuning.

### D3. STREAM receiver heap rise: adoption of D2 is conditional on a discriminating explanation

Under quantum wakeups with ECN stripped, STREAM receiver measured-window heap peaks are 3.195× Reno and receiver RSS 1.417× (fails the 10% flag). ECN is excluded as the cause by that arm. The recorded burst attribution is untested and doubtful: Reno's pacer permits larger opportunities (~23 vs ~9 datagrams), and the relay emulates the bottleneck and re-spaces packets before the receiver.

The D2 build must attribute the rise with a discriminating comparison: receiver heap profiles by allocation site at the peak in per-datagram versus quantum arms on the D1-corrected candidate, plus receive-queue and stream flow-control occupancy, enough repetitions to separate it from GC-timing variance.

Decision rule:

- If the cause is a measurement artifact, report it and proceed.
- If it is an implementation defect fixable within existing contracts, fix it in scope while preserving Reno and receive fairness.
- If it is genuinely caused by the quantum release shape and keeps receiver peak RSS above the flag, stop and reopen the pacing decision as a new grilling ticket with that evidence. Do not tune Q or adopt a fraction of Q to hide it.

### D4. Residual DATAGRAM sender CPU: re-measure, then diagnose; no waiver

The 1.175× residual was measured with ECN disabled entirely. The corrected candidate keeps ECN validation and the CE response active, so the residual must be re-measured on the D1+D2 candidate. If DATAGRAM sender CPU per useful GiB still exceeds 1.10× matched Reno, the re-demonstration must attribute it with a profile/counter comparison against Reno at matched load before any fix is chosen. No fix is preselected and the flag is not waived.

### D5. Peak memory: decide on re-measured evidence under a fixed rule

ECN churn feeds both the receiver and sender peaks through GC high water, so both are re-measured after D1. The gate remains whole-run peak RSS against matched Reno; measured-window values are reported alongside. For any remaining excess:

- **Implementation churn or retention** (allocation not required by a design bound): fix in scope.
- **Fixed design-bounded retention** (for example the recovery ledger's 1.2–1.7 MB per endpoint under its 4,096-tombstone budget): disclose with absolute MiB as a known cost for the qualification decision; not silently accepted.
- **BBR Startup transient** (high-gain Startup delivery pattern, confirmed by attribution rather than assumed): report as algorithm behaviour with whole-run and steady-window values, weighed in the qualification decision. Not a readiness waiver and not a reason to alter Startup.

### D6. Applying the readiness flags where Reno runs far below capacity

- The four flags apply against Reno at **matched load**: loopback and lossless S5 (clean WAN), where both controllers deliver comparable useful throughput. The acceptance plan defines each flag "compared with matched Reno" and the goodput flag for "clean WAN or LAN" paths.
- On an impaired path where Reno delivers far below capacity (S6), there is no matched Reno. That path tests the "repeatable useful benefit on at least one relevant difficult path" criterion. The candidate's absolute peak RSS and control p95 are reported beside Reno's with their attribution, without pass/fail ratios.
- The S6 queueing cost of draft-06 ProbeBW Up into a one-BDP buffer (control p95 183/167 ms at 100 ms base RTT) is algorithm-suitability evidence. It is carried into [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672) for explicit weighing. This decision does not change the Up policy.

Glossary addition (CONTEXT.md): **Matched load** — a comparison condition in which the candidate and its Reno reference deliver comparable useful throughput on the same path, so per-unit and peak costs compare equal work. A path where Reno delivers far less than the candidate is not at matched load.

## Follow-up tickets (sequential)

1. **Build the bounded-work BBR ECN feedback correction with frozen equivalence** (task, AFK). D1 on a new branch from the C1–C4 candidate; comment the measurement and shape on #613.
2. **Adopt quantum-release BBR pacing and explain the STREAM receiver heap rise** (task, AFK, with D3's stop rule). D2 plus the design amendment and test rewrites, on top of ticket 1; D3's attribution on the composed candidate. Blocked by ticket 1.
3. **Re-demonstrate the WAN-corrected BBRv3 candidate in matched local comparisons** (task). Loopback non-regression, S5 matched load and S6 benefit with the same harness, applying D4–D6. Blocked by ticket 2. #672 becomes blocked by ticket 3.

The production path (Planit slicing against main, reuse of #613/#630/#636, the D08 amendment and the existing spurious-undo phase sentence) stays in the map's fog until the re-demonstration lands.

## Non-goals

No change to Reno, the default controller, ECN ledger semantics, the Up probing policy, Startup, the readiness thresholds or the acceptance plan. No production merge, paid resource, campaign resumption, maintained benchmark framework or ledger change. Negative and inconclusive results are recorded.
