# Re-demonstration of the WAN-corrected BBRv3 candidate

**Date:** October 3, 2026, America/New_York. **Scope:** [Re-demonstrate the WAN-corrected BBRv3 candidate in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/711), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). Experimental candidate branch only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-wan-redemonstration`, based on `a59a9779` (the [quantum-pacing record](../2026-10-02-bbr-quantum-pacing/README.md), which contains the adopted D1+D2 candidate `fc4c1bf1`). It applies D4–D6 of the [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md#d4-residual-cpu-re-measure-then-diagnose-with-a-discriminating-comparison-no-waiver) to a rerun of the [correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) harness.

## Pre-registration

Written and committed before the loopback, S5 and S6 readiness stages ran. Nothing below is changed after outcomes are seen; deviations are recorded as deviations.

### Arms

| Label | Build | Source | Controller | Role |
| --- | --- | --- | --- | --- |
| Reno | `reno` | frozen `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` | Reno | Matched reference |
| Candidate | `cand` | `fc4c1bf1` (C1–C4 + D1 ECN + D2 quantum pacing) | BBRv3 | Candidate under test |
| Reno on candidate | `cand` | `fc4c1bf1` | Reno | Default-Reno preservation control |

Readiness stages use plain builds: the demonstration's fixture with the causal-diagnosis heap patch, no occupancy overlay and no series sampler. [build.py](build.py) exports each tree, builds with `-trimpath -buildvcs=false` and Go 1.27.0, and records hashes. The `-diag` builds add #710's measurement-only overlay to every arm alike, and are used only in attribution stages. The [relay](relay/main.go) is #710's v3: the frozen v1 queue model ([relay/main-v1.go.src](relay/main-v1.go.src), SHA-256 `19635ff6…`) plus forward-delivery observability, used for every WAN arm. Every copied aid is checked byte-identical to the #710 record at build time. As a reproducibility check, the `-diag` builds and both relays came out byte-identical to the #710 binaries of the same revisions.

### Paths, blocks and seeds

The endpoint contract is the demonstration's: separate managed IPv4 loopback processes on one Apple M4 Max, GOMAXPROCS=4 each, tracing disabled, M=1400, STREAM payload writes of 16,384 bytes and 1,200-byte DATAGRAMs, with the reliable control stream.

| Path | Model | Timing | Arms | Blocks | Seeds |
| --- | --- | --- | --- | --- | --- |
| Loopback | none | 5 s warmup, 20 s measured | Reno, candidate, Reno on candidate | 5 per workload | — |
| S5 | 100/100 Mbit/s, 100 ms RTT, one-BDP drop-tail queue, no injected loss | 10 s warmup, 30 s measured | Reno, candidate, Reno on candidate | 5 per workload | 9001–9005 |
| S6 | S5 plus 0.1% independent forward loss | 10 s warmup, 30 s measured | Reno, candidate | 5 per workload | 9101–9105 |

Both workloads, STREAM and DATAGRAM, run in every block. Within a block, every arm shares the declared seed, and controller order rotates per block ([matrix.py](matrix.py)). Five pairs per path and workload follows the acceptance plan's pairing rule. Observations run sequentially. A failed observation is retained and reported, and no run is excluded after its outcome is seen.

### Measures

Per observation, as in the demonstration:

- receiver-verified useful goodput, and utilization against the 100 Mbit/s bottleneck on S5 and S6;
- CPU seconds per receiver-verified useful GiB, at the sender and the receiver;
- whole-run peak RSS at each endpoint (`getrusage`), with warmup and measured-window `HeapAlloc` peaks from the fixture's 1 Hz samples;
- control p95 and p50, nearest-rank, from the measured window's replies;
- sender allocated bytes per useful byte;
- on S5 and S6, from the relay: forward bottleneck overflow drops and random drops, each as a share of forward packets received by the model (the bottleneck loss rate), queue-delay distribution, release lateness and stall count (wakeups over 5 ms late);
- ECN engagement: forward and reverse codepoint counts at the relay. ECN is engaged when the candidate's forward packets carry ECT after validation. The S5 and S6 queues never CE-mark, so the CE response itself cannot engage on these paths; that is reported as a limit, not a pass.

Payload preservation is the receiver integrity check on every observation: zero corrupt or duplicate payload, per-second totals equal to useful bytes, and matching sender and receiver reports. Any failed check is a raised preservation flag.

### Flags

Each flag compares an arm with the same block's Reno. Per path, workload and measure, a flag is **raised** when the median of the five per-block ratios crosses its limit:

| Measure | Raised when | Applies to |
| --- | --- | --- |
| Receiver goodput | below 0.95 | Loopback and S5 only (clean paths) |
| CPU per useful GiB | above 1.10, sender and receiver separately | Every path |
| Whole-run peak RSS | above 1.10, sender and receiver separately | Every path |
| Control p95 | above 1.20 | Every path |

Blocks crossing a limit are counted and disclosed even when the median passes. On S6, goodput is the useful-benefit comparison and has no flag: the benefit is repeatable when the candidate's goodput exceeds Reno's in all five pairs, and its magnitude is reported. Measured-window heap peaks are reported, not flagged. Reno on candidate is held to the same limits against frozen Reno as a default-Reno preservation check. A crossing there is a raised preservation flag.

Matched Reno is the paired Reno run under the same workload demand, emulator schedule and seed. Matching does not require equal achieved throughput: on S6, Reno's low utilization is reported as context and does not remove a comparison.

### Attribution of raised flags (D4, D5)

For every raised flag:

1. Form a hypothesis from profiles, counters or the 10 ms series against Reno at the same pairing.
2. Run a comparison or intervention that distinguishes that hypothesis from a named competing explanation. These stages are designed after the readiness flags are known and are appended to [matrix.py](matrix.py) as post-registration stages, each with its hypothesis, competitor and decision rule written into this record before it runs.
3. Only then classify, under D5's categories for memory: implementation churn or retention, design-bounded retention, BBR Startup transient, contract-change cause, or unresolved. A hot frame alone is not attribution.

An in-scope implementation defect is fixed on a new, identified revision. Every affected readiness comparison is then rerun on that revision, so no result mixes revisions. Contract-change causes and unresolved excess are reported, not fixed. No classification turns a raised flag into a pass, and no readiness is claimed while any flag is raised.

## Post-registration: attribution stages

Written and committed after the readiness stages ran and before any attribution stage ran. The readiness stages raised these flags (median per-block ratio against the same block's Reno; blocks crossing out of five):

| Path, workload | Arm | Raised flags |
| --- | --- | --- |
| S5 STREAM | Candidate | sender CPU 1.170 (3), sender RSS 1.203 (4), receiver RSS 1.407 (5) |
| S5 DATAGRAM | Candidate | sender CPU 1.320 (3), receiver CPU 1.138 (3), sender RSS 1.132 (4) |
| S5 DATAGRAM | Reno on candidate | sender CPU 1.192 (3), receiver CPU 1.158 (3): a default-Reno preservation flag |
| S6 STREAM | Candidate | sender RSS 1.393 (5), receiver RSS 1.574 (5), control p95 1.547 (5) |
| S6 DATAGRAM | Candidate | sender RSS 1.374 (5), control p95 1.481 (5) |

Loopback raised none. Frozen Reno's own S5 sender CPU per GiB spans 26.4–38.4 s (DATAGRAM) and 20.1–28.0 s (STREAM) across blocks, on a host whose non-fixture load ranged from about 300% to 1,300% CPU during runs. Every candidate change from frozen Reno sits behind `EnableBBR`, the only installer of the delivery-sampling dispatch and the BBR ECN tracker. Code inspection is not attribution, so the stages below discriminate.

The stages use new seeds, retain every run and exclude none after outcomes are seen.

### Stage `counters`: CPU flags (D4)

- **Runs.** S5, both workloads, six blocks (seeds 9201–9206). Arms: Reno, a second frozen Reno run in the same block (A/A), candidate, and Reno on candidate. Order is rotated per block. Plain builds run under `/usr/bin/time -l`, which reports each endpoint's instructions retired, cycles, and voluntary and involuntary context switches.
- **Hypotheses.** For each CPU flag, *H-work* says the arm executes more instructions per useful GiB than Reno. The competitor, *H-sched*, says equal work costs more CPU time through wakeups, scheduling or core placement.
- **Decision rules,** per endpoint against the same block's Reno:
  - *Executed-work excess:* median instructions-per-GiB ratio above 1.10, with at least five of six blocks above 1. A CPU-profile stage, designed and registered here before it runs, then locates the excess by site.
  - *Wakeup or scheduling cost:* instructions within 1.10, and either total context switches (voluntary plus involuntary) per GiB above 1.5× in at least five of six blocks, or cycles per GiB above 1.10 in at least five of six blocks. It is reported against the pacing contract (D2/D08) or the platform, not fixed.
    A smoke run made before this stage, and excluded from it, showed macOS reporting about 6 voluntary and 1.68 million involuntary switches for one sender run. The voluntary count alone is degenerate, so the rule uses the total. This amendment was made before any `counters` observation ran.
  - *Noise floor:* neither of the above, with the readiness ratio inside the range of the A/A Reno CPU-seconds ratios. The flag stays raised and unresolved at this host's noise floor.
- **Default-Reno preservation.** The flag is attributed to measurement noise when Reno on candidate has a median instructions-per-GiB ratio in [0.97, 1.03], and the readiness ratio lies inside the A/A range. Otherwise it stays a raised preservation flag.

### Stage `timeline`: when each memory peak is set (D5), and S6 latency (D6)

- **Runs.** S5 and S6, both workloads, two blocks (seeds 9301–9302 on S5, 9401–9402 on S6). Arms: Reno (`reno-diag`) and candidate (`cand-diag-counted`). The candidate build adds the demonstration's phase-timeline counting overlay, under the `bbrworkcount` tag with C4 hooks, to the diag overlay. The overlay is copied byte-identical from `80466857`. Both endpoints record the 10 ms series, which includes `getrusage` peak RSS.
- **Measures.** `t_rss` is the first sample at which an endpoint's peak RSS reaches 99% of its final value. `t_probe` is the sender's first entry into a ProbeBW phase, which ends Startup and Drain. The measured-window footprint is the maximum of Go's mapped-not-released memory (`/memory/classes/total` minus heap released) within the measured window.
- **Rules,** per flagged path, workload and endpoint:
  - *Startup transient:* `t_rss ≤ t_probe + 1 s` in every candidate run, and the candidate's measured-window footprint is at most 1.10× the same block's Reno.
  - *Steady-state excess:* the measured-window footprint exceeds 1.10× Reno. It is attributed in stage `heapsites`.
  - *Mixed:* `t_rss` falls in Startup but the steady-state condition also holds. Both parts are reported.
  - *Unresolved:* anything else.
- **S6 control p95 (D6).** The flag is attributed to the selected ProbeBW Up policy when two conditions hold. In the candidate runs, at least 70% of forward-queue samples above 25 ms fall in Up or the following Down, and Cruise and Refill median queue delay is below 1 ms. Also, the readiness S5 matched-load control p95 is within 1.20. Otherwise it is unresolved.

### Stage `heapsites`: what the steady-state excess is (D5)

- **Runs.** The same paths, workloads, arms and builds as `timeline`, two blocks (seeds 9311–9312 on S5, 9411–9412 on S6). Both endpoints write a heap profile every second without forcing GC. Profile writing perturbs the run, so this stage supplies attribution only, never a flag value.
- **Comparison.** The profile nearest each endpoint's measured-window heap peak is compared with Reno's in the same block, by in-use bytes per allocation site. The excess is grouped into two named, disjoint hypotheses:
  - *Delivery data:* sent or received payload held for reliable delivery. These are packet and frame buffers, stream send data, sent-packet history and receive reassembly. Classified as algorithm behaviour scaled by BBR's in-flight or loss pattern, unless the payload is retained beyond what delivery requires.
  - *BBR bookkeeping:* recovery evidence, retained delivery, sampler, ECN ledger and BBR controller state. Classified as design-bounded when its size matches a declared design bound that the occupancy uses. Classified as implementation churn or retention when allocation exceeds what the bound and occupancy require.
- **Unresolved.** An excess outside both groups, or one that neither group explains (less than 70% of the excess), is unresolved.

An implementation defect identified here is fixed on a new revision, and the affected readiness comparisons are rerun on it. Contract-change causes are reported only.

<!-- Results, attribution, preservation, limits and assets are added after the stages run. -->
