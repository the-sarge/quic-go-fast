# BBRv3 correction demonstration

**Date:** October 2, 2026, America/New_York. **Scope:** [Demonstrate the BBRv3 correction in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/671), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666).

## Answer

The correction removes the supported bottleneck, and the full candidate is competent on local loopback: it meets all four readiness limits at the median for STREAM and DATAGRAM. On an injected-loss WAN path it delivers a large, repeatable useful benefit. It is not yet a competent candidate across the measured local paths. On a lossless 100 Mbit/s, 100 ms path, sender CPU per useful GiB is about 1.75× Reno. Two transport-integration costs outside the selected correction cause that excess: per-ACK ECN mark-range rebuilding and per-datagram pacing wakeups. Both are implementation efficiency, not BBRv3 algorithm behavior. Diagnostic interventions that remove both bring STREAM sender CPU to 1.05–1.07× Reno and DATAGRAM to 1.17–1.20×.

The injected-loss path fails the memory and control-latency limits against Reno, but Reno there delivers only 6% of capacity. At matched load, BBR's control p95 equals Reno's. The excess queueing comes from ProbeBW Up probing into a one-BDP buffer, which the accepted design selected. Peak-RSS excess is mainly the ECN tracker's allocation churn and a high-gain Startup transient, not steady-state retention.

## Conditions

| Label | Source | Contents |
| --- | --- | --- |
| Reno | frozen `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` | Default controller, matched reference |
| Frozen BBRv3 | same | Reproduced regression |
| Service layer | `c6bb13b72e2d5de55af36543aa27c8cf06812a11` | C1–C3 |
| Full candidate | `8a0e0962` | C1–C4 |
| Reno on candidate | `8a0e0962` | Preservation control for default Reno |
| Counted builds | the three BBR sources above, `bbrworkcount` tag | Diagnostic recovery patch, service layer, full candidate, with [counting overlays](counting/) |
| `full-quantum` | full candidate plus one line | Diagnostic only: when pacing credit is short, wait for a full quantum Q instead of one datagram |

Every source tree is an exported revision under the owned worktree's `.local`; no tracked transport file was edited. [build.py](build.py) records each tree, overlay and binary hash in [environment.json](environment.json).

Endpoints are separate managed IPv4 loopback processes on one Apple M4 Max, GOMAXPROCS=4 each, Go 1.27.0, tracing disabled, M=1400. CPU is full-run process time per receiver-verified useful GiB. Control p95 is nearest-rank from 20 (loopback) or 30 (WAN) replies per run. Measurements ran sequentially on a shared desktop with recorded background load (fseventsd ~175% CPU throughout); controller order rotates per block.

**Paths.** Loopback uses 5 s warmup and 20 s measurement. The WAN paths use a userspace [relay](relay/main.go) between the endpoints that applies the frozen Q1 campaign queue model, with 10 s warmup and 30 s measurement:

- **S6:** 100/100 Mbit/s, 100 ms RTT, one-BDP ordinary queue, 0.1% independent forward loss.
- **S5:** the same path without loss.

Both controllers in a pair share the declared loss seed. The relay changes no host network state and preserves ECN codepoints. Release lateness is usually small: the median per-run maximum is 2.05 ms. But 30 of 116 WAN runs saw at least one relay stall over 5 ms, the worst 249 ms, affecting at most 1.2% of a run's packets. Stalls hit Reno and BBR runs alike; per-run values are in [summary.json](summary.json). It records zero send errors, truncations and propagation overflows, and samples queue occupancy every 10 ms.

## Loopback: the bottleneck is removed

Five rotated blocks per workload. Ratios are median (blocks passing) against the same block's Reno.

| Workload | Condition | Goodput | Sender / receiver CPU per GiB | Sender / receiver peak RSS | Control p95 |
| --- | --- | --- | --- | --- | --- |
| STREAM | Frozen BBRv3 | 0.268 (0/5) | 1.888 / 0.894 | 1.373 / 1.230 | 8.109 |
| STREAM | Service layer | 0.968 (5/5) | 1.029 / 1.037 | 1.029 / 1.015 | 1.034 (4/5) |
| STREAM | Full candidate | 0.984 (5/5) | 1.008 / 1.022 (5/5) | 1.028 / 1.014 (5/5) | 1.023 (4/5) |
| STREAM | Reno on candidate | 0.996 | 1.002 / 1.004 | 1.008 / 0.996 | 0.961 |
| DATAGRAM | Frozen BBRv3 | 0.221 (0/5) | 2.248 / 1.019 | 1.323 / 1.174 | 5.986 |
| DATAGRAM | Service layer | 0.973 (3/5) | 1.074 / 1.060 | 1.004 / 1.021 | 0.901 |
| DATAGRAM | Full candidate | 0.966 (3/5) | 1.077 / 1.062 (4/5) | 1.011 / 1.020 (5/5) | 0.938 (5/5) |
| DATAGRAM | Reno on candidate | 0.987 (4/5) | 1.015 / 1.011 (4/5) | 0.996 / 0.993 | 0.830 (4/5) |

Absolute medians: Reno 1263.6/1133.4 Mbit/s, full candidate 1237.9/1107.5 Mbit/s, frozen 337.3/258.9 Mbit/s (STREAM/DATAGRAM). Full-candidate peak RSS is 21.05/21.19 MiB (STREAM sender/receiver) against Reno 20.39/20.91.

- **Margins.** The DATAGRAM margin is thin. Two blocks are 8–9% below Reno, while Reno-vs-Reno moved 4–6% in those blocks. In DATAGRAM block 5 the Reno reference was itself anomalous, and the Reno-on-candidate control shows the same swing. The isolated STREAM p95 miss (1.675) is one sparse-tail block.
- **Memory.** The earlier peak-RSS excess (+32–37%) is gone. Live heap attributes about 1.2–1.7 MB to the retained recovery ledger per BBR endpoint.
- **Mechanism.**
  - Sender CPU profiles: `Conn.handleAckFrame` falls from 14.4%/10.1% of samples (frozen STREAM/DATAGRAM) to 0.7%/below the listing threshold. `pthread_cond_signal` falls from 52.0%/54.5% to 9.8%/7.5%, matching Reno's 9.9%/7.4%.
  - Work counters: ACK transitions inspect about 3 records per call (max 33) against 45–72 for the diagnostic patch.
- **C4.** The service layer and full candidate are indistinguishable on loopback. C4 imposes no measurable cost there.

## Difficult path: a repeatable useful benefit

S6, five pairs per workload; Reno delivers 4.9–5.9 Mbit/s, consistent with loss-limited congestion avoidance.

| Workload | Condition | Goodput, Mbit/s, median (range) | Sender CPU, s/GiB | Sender / receiver RSS, MiB | Control p95, ms |
| --- | --- | --- | --- | --- | --- |
| STREAM | Reno | 5.3 (4.9–5.9) | 95.3 | 19.08 / 20.11 | 129.0 |
| STREAM | Frozen BBRv3 | 88.6 (85.5–90.4) | 103.8 | 34.83 / 36.28 | 200.4 |
| STREAM | Full candidate | 88.8 (85.9–89.1) | 50.3 | 28.09 / 32.06 | 183.0 |
| DATAGRAM | Reno | 5.4 (5.0–5.4) | 124.7 | 19.89 / 20.06 | 121.9 |
| DATAGRAM | Frozen BBRv3 | 86.6 (83.1–86.8) | 121.5 | 34.38 / 25.94 | 181.3 |
| DATAGRAM | Full candidate | 87.1 (83.9–87.7) | 59.0 | 29.02 / 21.39 | 167.2 |

- **Benefit.** The full candidate's goodput is 16.4×/16.1× Reno in all ten pairs, and CPU per useful GiB is 0.52×/0.48× Reno. The benefit is algorithmic: frozen BBRv3 achieves it too, at twice the candidate's sender CPU.
- **Loss engagement.** The relay recorded 325–384 random forward drops per BBR run and 118–207 median overflows; Reno saw no overflow.
- **Discriminating C1 check.** Counted builds on S6 show the diagnostic patch's persistent-span query inspecting about 31,400 records per call (max 32,768) after the first loss, and its ACK transitions about 1,100 (max 2,152). The service layer and full candidate inspect 0 span records (≤6 index nodes) and at most 33 transition records. Retained discovery stays within its stated bound (≤391 failed checks, below 2·64·h).
- **Engagement of C3 and C4.** Genuine idle did not leave pacing at 1 B/s. Item 2 engaged: the minimum RTT fell 215–597 times per counted run. Item 4 engaged once, a Startup loss exit, with no quantization difference. Items 1 (all-spurious undo) and 3 (rejected-rate round start) did not engage in any counted run. They are unengaged, and no effect is attributed to them.

Peak RSS (1.46/1.59 STREAM, 1.46/1.07 DATAGRAM) and control p95 (1.42/1.35) fail against Reno. Both reflect a reference running at 6% utilization; attribution follows.

## Attributing the remaining shortfalls

### Control latency is the selected probing policy

The relay's phase-correlated queue samples (sender phase timeline from the counted full build):

- **Up and Down.** Forward-queue delay exceeds 25 ms in 73–76% of ProbeBW Up samples and 79–81% of the Down samples that follow (p95 86–100 ms).
- **Cruise and Refill.** Queue delay is about 0.1 ms.
- **Cycle.** Up recurs about every 3.3 s and occupies 11–13% of the time. Down drains the queue at the 0.9 gain over a further 21–22%.

Control p50 stays at the base RTT (100.4–100.5 ms). The accepted design deliberately does not port QUICHE's queue-threshold Up exit and uses the draft's Up cwnd gain of 2.25. This queue episode is therefore selected draft-06 behavior on a one-BDP buffer, not port inefficiency.

**Load-matched control.** On S5 (three pairs per workload), Reno reaches 95.9/94.4 Mbit/s and fills the same queue. The full candidate's control p95 is 1.013/0.987× Reno, and goodput is 0.974×/0.974×.

### Sender CPU at WAN rates is transport integration

At matched load on S5 the full candidate's sender CPU per useful GiB is 1.755/1.749× Reno in every pair, versus about 1.0 on loopback. Two diagnostic discriminators, each against the same block's Reno:

| Workload | Condition | Sender CPU | Sender allocated GiB per useful GiB | Goodput |
| --- | --- | --- | --- | --- |
| STREAM | Full candidate | 1.749 | 5.41 | 0.974 |
| STREAM | ECN stripped by the relay | 1.645 | 0.44 | 0.975 |
| STREAM | Quantum wakeups only | 1.781 | 52.24 | 0.970 |
| STREAM | Quantum wakeups, ECN stripped | **1.059** | 0.42 | 0.978 |
| DATAGRAM | Full candidate | 1.771 | 13.31 | 0.974 |
| DATAGRAM | ECN stripped by the relay | 1.637 | 1.95 | 0.979 |
| DATAGRAM | Quantum wakeups only | 1.315 | 13.15 | 0.979 |
| DATAGRAM | Quantum wakeups, ECN stripped | **1.175** | 1.92 | 0.974 |

Reno allocates 0.18–0.22 (STREAM) and 1.66–1.69 (DATAGRAM) GiB per useful GiB, and stripping ECN leaves Reno unchanged (sender CPU 0.97–0.98×). Stripping makes QUIC ECN validation fail for both controllers, so the BBR ECN tracker short-circuits.

**ECN tracker.**
- *Allocation:* `bbrECNTracker.appendRange`, under `captureBBRECN` from `ReceivedAck`, accounts for 91% of sampled sender allocation (1.66 GB) in the full candidate on S5 STREAM, and 99% (16.9 GB) in a quantum run.
- *Work:* each advancing ACK scans every retained mark range against every ACK range and rebuilds the whole range slice. That is O(ledger ranges × ACK ranges) work plus a fresh slice per ACK, the same cost class C1 removed from recovery service.
- *Why it was missed:* the causal diagnosis measured it only on loopback, where it was a 7–8% allocation share.
- *Status:* [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613) is now shown to be exercised and material at WAN RTT.

**Pacing wakeups.**
- *Mechanism:* `bbrSendPolicy.deadline(size)` wakes the sender when one datagram's credit accrues. Once the bucket drains, that is about one wakeup every 112 µs at 100 Mbit/s. The S5 STREAM profile difference against Reno is scheduler park/wake (`kevent` +1.70 s, `pthread_cond_wait` +0.84–0.92 s) and `sendmsg` time.
- *Why loopback differs:* at loopback rates late wakeups accumulate up to Q, so the cost disappears there.
- *Status:* the design's decision D08 records exactly this as "native scheduling cost remains to measure".

**Interaction.** The two costs interact: in STREAM, quantum wakeups alone do not help while the ECN churn remains, but together the two discriminators close the gap.

**Intervention side effect.** The quantum intervention is diagnostic and not a proposed fix:
- It breaks the two tests that pin the design's exact-deficit deadline (`TestBBRPacingQuantumAndDeadline`, `TestBBRPendingCreditMTUException/paced_probe`).
- It raises STREAM receiver measured-window heap peaks to about 3× Reno, because each wakeup now carries a burst.

Any change to pacing granularity is a pacing-contract decision.

DATAGRAM keeps 1.175× sender CPU after both discriminators. That remainder is unexplained.

### Memory

**Sender.** On S5, sender peak RSS is 1.16–1.17× Reno at matched load, and stripping ECN leaves it at 1.14–1.17×. On S6 the frozen BBR's larger sender peak (34.8 MiB) falls to 28.1 MiB in the candidate. Forced-GC live heaps are small, 2–7 MB against 21–32 MiB peak RSS. The recovery ledger's 1.2–1.7 MB live per endpoint is a fixed contribution. The remaining sender excess is not uniquely attributed.

**Receiver.** STREAM receiver peaks reflect Startup. In paired S5 STREAM runs the BBR receiver's warmup heap peak is 2.1× Reno, while its measured-window peak is 1.3×. Heap samples show it reaching about 11 MiB at t=2–3 s, then holding 2.5–4.4 MiB, close to Reno's steady values. On S6, retained STREAM data behind real losses also contributes: frame-sorter and packet-buffer live heap reaches about 3.6 MB at the receiver.

Peak RSS therefore mixes three things:
- the ECN allocation churn, as a GC high-water effect;
- a high-gain Startup transient;
- loss-driven reassembly that Reno's 5 Mbit/s flow never accumulates.

## Preservation

- Every one of the 203 retained observations exited cleanly. Each passed receiver integrity: zero corrupt or duplicate payload, per-second totals equal to useful bytes, and matching sender and receiver reports.
- Default Reno built from the candidate stays within about 1% of frozen Reno at the median.
- On the exact candidate tree, `internal/ackhandler` and `internal/congestion` pass with and without `-race`, the root BBR, emission, local-credit and ECN tests pass, and vet is clean ([gates.log](gates.log)).
- No recorded QUIC translation, cap, evidence contract, controller version or default changed.

## Limits

- **Platform and topology.** The paths are one macOS host, loopback endpoints and a userspace relay, not a native two-host or kernel emulator.
- **Repetition.** Pairs and blocks are few (3–5); per-run p95 uses 20–30 replies.
- **Engagement.** C4 items 1 and 3 were unengaged, and loss-dependent C4 behavior is observed only on S6.
- **Fidelity of the WAN timings.** The relay is a userspace process on a shared host. Occasional scheduling stalls (30 of 116 WAN runs above 5 ms, worst 249 ms, at most 1.2% of packets late by more than 1 ms) can inflate individual control tails for either controller.
- **Background load.** The shared-host load is recorded, not controlled.
- **Smoke runs.** These used an earlier relay without queue sampling and are excluded from every table.
- **Scope.** This does not certify other platforms, real carrier paths or production readiness. No production merge, paid resource, campaign resumption, default-controller change or ledger change occurred.

## Assets and reconstruction

- **Results:** [summary.json](summary.json) (observations, groups, paired readiness, mechanism counters, phase-queue correlation, interventions), [environment.json](environment.json) (host, toolchain, relay and build hashes), [gates.log](gates.log).
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json). It holds every observation (configs, endpoint and relay records, commands, exits, host samples, work dumps, profiles) plus the stage logs. Credentials and binaries are omitted.
- **Aids:** [build.py](build.py), [run.py](run.py), [matrix.py](matrix.py), [analyze.py](analyze.py), [relay/main.go](relay/main.go). [relay/main-v1.go.txt](relay/main-v1.go.txt) is the relay used for every non-strip WAN run; it rebuilds byte-identically. Also [model-overlay/next.go](model-overlay/next.go) and [counting/](counting/).

These are frozen one-ticket aids, not a maintained benchmark framework. To reconstruct:
1. In a fresh owned worktree of this branch, run `build.py` (all variants).
2. Run `matrix.py` stages in order: `smoke`, `loopback`, `impaired`, `loadmatched`, `mechanism`, `profiles`, `quantum`, `ecn`.
3. Build the strip-capable relay as `bin/relay-v2` from `relay/main.go` before the `ecn` stage, and the original relay from `main-v1.go.txt` if a byte-identical emulator is needed.
4. Run `analyze.py`.

The runner refuses to overwrite a prior attempt.
