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

<!-- Results, attribution, preservation, limits and assets are added after the stages run. -->
