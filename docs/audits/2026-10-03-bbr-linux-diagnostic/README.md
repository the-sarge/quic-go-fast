# Linux diagnostic of the BBRv3 candidate on owned hardware

**Date:** October 3, 2026, America/New_York. **Scope:** [Measure and attribute the BBRv3 candidate's flags on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/712), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D3 of the [accepted qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md#d3--next-a-linux-diagnostic-on-owned-hardware). Diagnostic only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-diagnostic`, based on `b5f759c6` (the decision record, which contains the [re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) at `dc252f8a` and the candidate `fc4c1bf1`).

## Answer

**Not ready on Linux either; no readiness is claimed, and no fix revision was built.** On `minimax`, the unchanged candidate `fc4c1bf1` keeps its useful benefit on S6: 12.07× (STREAM) and 11.09× (DATAGRAM) Reno's goodput in all five pairs, at about 0.13× Reno's sender CPU per useful GiB. But Linux raises more flags than the Mac, including new loopback and S5 goodput flags.

- **The candidate sender's extra work is user-space work.** On S5 it executes 1.188× (STREAM) and 1.123× (DATAGRAM) Reno's instructions per useful GiB, in every block. User instructions are 1.358×/1.267× and user cycles 1.494×/1.336×, while kernel instructions are 0.978×/0.944×. Wakeups issued, context switches and futex calls are lower than Reno's. Per forward packet that is about 11,000 extra user instructions on Reno's 31,600, with kernel work unchanged. On Linux this rules out the Mac record's leading hypothesis, extra kernel send and runtime wake work.
- **No single group holds it.** Under the registered rule, localization is **inconclusive** at both endpoints and in both workloads. BBR and bookkeeping code holds 0.39–0.44 of the excess, other user code 0.29–0.37, runtime scheduling 0.14–0.21 at the sender, and allocation and GC only 0.03–0.16. With no lead, no discrimination stage was triggered and no defect was demonstrated. The CPU flags stay **raised and unresolved**.
- **New goodput flags.** On loopback the candidate reaches 0.770/0.795 of Reno's goodput, and on S5 0.913/0.901. Both hold in 5/5 blocks, with A/A at 1.00. No registered stage attributes them.
- **Memory.** Every flagged peak is a steady-state excess, not a Startup transient. S5 DATAGRAM sender RSS is design-bounded bookkeeping, and S5 STREAM receiver RSS is delivery data. Every other memory flag is unresolved under the unchanged 70% rule. That includes S6 STREAM receiver RSS, which the Mac attributed to delivery data.
- **Preservation.** Reno on candidate matches frozen Reno's counters within 0.2% and passes every CPU limit; the Mac's preservation CPU flag does not recur. Two other preservation flags are raised under the registered rule: S5 DATAGRAM sender RSS 1.152, and loopback DATAGRAM control p95 1.226. In both cells, the A/A arm's own range reaches or exceeds that value.

## Readiness results

Plain builds, five blocks per path and workload, 110 observations. All exited cleanly and passed receiver integrity. None was contaminated: the largest foreign-CPU mean was 0.002 cores and the largest one-second sample 0.05 cores. There were no UDP buffer or memory errors, and no relay stalls. Ratios are the median [min–max] of per-block ratios against the same block's frozen Reno. Bold marks a raised flag; parentheses give the blocks crossing the limit. The A/A arm's per-block ranges are in [summary.json](summary.json) beside every ratio. No flag is contamination-sensitive.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | **0.770 [0.768–0.777] (5)** | **1.261 [1.256–1.267] (5)** | **1.189 (5)** | 0.945 | 1.073 | 1.063 (2) |
| Loopback STREAM | Reno on candidate | 0.996 | 1.003 | 1.006 | 0.983 | 0.990 | 1.068 (2) |
| Loopback DATAGRAM | Candidate | **0.795 [0.788–0.801] (5)** | **1.208 [1.200–1.211] (5)** | 1.094 (2) | 0.932 | 1.038 | 0.699 (2) |
| Loopback DATAGRAM | Reno on candidate | 1.001 | 0.999 | 1.005 | 1.025 | 1.000 | **1.226 [0.936–5.375] (3)** |
| S5 STREAM | Candidate | **0.913 [0.904–0.925] (5)** | **1.237 [1.228–1.256] (5)** | **1.155 (5)** | **1.202 (5)** | **1.441 [1.409–1.483] (5)** | 1.030 |
| S5 STREAM | Reno on candidate | 1.000 | 0.994 | 0.977 | 1.002 | 0.999 | 0.998 |
| S5 DATAGRAM | Candidate | **0.901 [0.887–0.904] (5)** | **1.108 [1.092–1.124] (4)** | **1.131 (5)** | **1.144 [0.971–1.178] (3)** | 1.057 | 0.691 |
| S5 DATAGRAM | Reno on candidate | 1.000 | 0.987 | 0.994 | **1.152 [0.861–1.154] (3)** | 1.004 | 1.026 |
| S6 STREAM | Candidate | benefit 12.07× [9.52–13.37] | 0.125 | 0.085 | **1.571 (5)** | **1.754 (5)** | **1.541 [1.311–1.572] (5)** |
| S6 DATAGRAM | Candidate | benefit 11.09× [8.94–12.40] | 0.129 | 0.096 | **1.411 (5)** | 1.089 (1) | 1.134 (1) |

A/A context for the two raised preservation cells:
- *Loopback DATAGRAM control p95:* the A/A ratio ranges 0.36–4.27, and on loopback STREAM the A/A arm itself crosses the limit (1.294).
- *S5 DATAGRAM sender RSS:* the A/A ratio ranges 0.92–1.12, with a median of 1.055. Reno on candidate's values fall at two levels, about 18.4 and 20.9 MiB, as frozen Reno's do.

Absolute medians (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput | Sender / receiver CPU | Sender / receiver RSS | Control p95 / p50 | Forward overflow |
| --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 4683.2 | 3.97 / 2.74 | 16.95 / 15.23 | 0.135 / 0.068 | — |
| Loopback STREAM | Candidate | 3611.4 | 5.02 / 3.25 | 15.96 / 16.34 | 0.139 / 0.077 | — |
| Loopback DATAGRAM | Reno | 4627.7 | 4.55 / 3.03 | 17.88 / 19.82 | 0.279 / 0.092 | — |
| Loopback DATAGRAM | Candidate | 3669.1 | 5.49 / 3.32 | 16.76 / 19.95 | 0.219 / 0.111 | — |
| S5 STREAM | Reno | 95.9 | 10.18 / 8.50 | 18.88 / 17.63 | 184.0 / 177.7 | 0.100% |
| S5 STREAM | Candidate | 87.5 | 12.63 / 9.86 | 22.58 / 25.27 | 188.4 / 102.2 | 0.254% |
| S5 DATAGRAM | Reno | 94.4 | 11.33 / 8.71 | 18.38 / 14.88 | 176.4 / 169.9 | 0.386% |
| S5 DATAGRAM | Candidate | 85.0 | 12.56 / 9.89 | 20.91 / 15.85 | 121.3 / 101.8 | 0.034% |
| S6 STREAM | Reno | 6.2 | 104.85 / 117.95 | 14.38 / 14.63 | 129.5 / 104.3 | 0% (+0.091% injected) |
| S6 STREAM | Candidate | 72.4 | 13.36 / 10.07 | 22.89 / 25.69 | 199.6 / 102.3 | 0.319% (+0.100%) |
| S6 DATAGRAM | Reno | 5.9 | 121.60 / 118.39 | 14.61 / 14.54 | 126.1 / 103.1 | 0% (+0.095%) |
| S6 DATAGRAM | Candidate | 65.8 | 15.67 / 11.32 | 20.77 / 15.75 | 143.0 / 102.0 | 0.034% (+0.099%) |

**Against the Mac record.** The Linux host is far quieter: A/A Reno CPU ratios stay within about ±1% on loopback and S5 (±6% on S6), against swings of up to 45% on the Mac. Linux loopback runs 3.5× faster than the Mac's, where both arms sat near 1.3 Gbit/s. Reno's whole-run RSS is lower (about 14–19 MiB against 19–24 MiB), which inflates the RSS ratios. The candidate's absolute S5 and S6 goodput is lower than on the Mac: 85–88 Mbit/s against 92–93 on S5, and 66–72 against 86 on S6.

**ECN.** All forward packets after the handshake carry ECT(0) in every WAN observation, and no queue CE-marks, so the CE response stays unengaged.

**Packet I/O.** Measured in the counters stage: both arms make one send syscall and one `net_dev_xmit` per forward packet (0.999–1.000). Neither `sendmmsg` batching nor GSO is engaged on these paced paths. Receivers make 0.60–0.67 receive syscalls per packet.

## Attribution

All four registered stages ran: `counters` (48 observations), `callgraphs` (12), `timeline` (16) and `heapsites` (16). All were clean and none was excluded. [localize.py](localize.py) writes [localization.json](localization.json), and [attribution.py](attribution.py) writes [attribution.json](attribution.json).

### CPU: counters

Median per-block ratios over six S5 blocks against the same block's Reno, measured-window `perf stat`. Every run counted every event at 100% running time.

| Workload | Arm, endpoint | Instr./GiB (user / kernel) | Cycles/GiB (user / kernel) | User share of excess, instr. / cycles | Switches / wakeups issued / futex |
| --- | --- | --- | --- | --- | --- |
| STREAM | Candidate sender | 1.188 [1.184–1.192] (1.358 / 0.978) | 1.227 (1.494 / 1.020) | 1.00 / 0.95 | 0.898 / 0.762 / 0.888 |
| STREAM | Candidate receiver | 1.031 (1.055 / 0.980) | 1.039 (1.078 / 0.992) | 1.00 / 1.00 | 0.956 / 1.001 / 0.974 |
| STREAM | Reno on candidate, sender / receiver | 1.001 / 1.000 | 0.998 / 1.000 | — | ≈1.00 |
| STREAM | A/A Reno, sender / receiver | 1.000 / 1.001 | 0.999 / 1.001 | — | ≈1.00 |
| DATAGRAM | Candidate sender | 1.123 [1.116–1.126] (1.267 / 0.944) | 1.136 (1.336 / 0.968) | 1.00 / 1.00 | 0.829 / 0.380 / 0.858 |
| DATAGRAM | Candidate receiver | 1.030 (1.068 / 0.952) | 1.027 (1.083 / 0.967) | 1.00 / 1.00 | 0.920 / 1.055 / 0.936 |
| DATAGRAM | Reno on candidate, sender / receiver | 1.000 / 1.002 | 1.003 / 1.004 | — | ≈1.00 |
| DATAGRAM | A/A Reno, sender / receiver | 1.000 / 0.999 | 1.000 / 1.002 | — | ≈1.00 |

Per forward packet, frozen Reno's sender executes about 31,600 (STREAM) and 33,300 (DATAGRAM) user instructions, plus 25,600 and 26,500 kernel instructions. The candidate executes about 42,900 and 42,000 user instructions, plus 24,900 kernel instructions in both workloads. User cycles per packet are 21,600 against 14,400 (STREAM) and 20,900 against 15,700 (DATAGRAM). The receiver's whole-run CPU-time ratio (1.08–1.11) is larger than its measured-window cycle ratio (1.03–1.04), so part of the receiver flag lies outside the measured window.

### CPU: call-graph localization

Cycles per useful GiB by group: the median share of the positive excess over three blocks, and the median change.

| Endpoint, workload | BBR + bookkeeping | Other user | Runtime scheduling | Alloc + GC | Kernel send | Kernel wake/sched | Kernel recv / other | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Sender STREAM | 0.44 (+2.47 G) | 0.29 (+1.64 G) | 0.14 (+0.78 G) | 0.05 (+0.26 G) | 0.02 (+0.09 G) | 0 (−0.57 G) | 0 / 0.01 | inconclusive |
| Sender DATAGRAM | 0.42 (+2.31 G) | 0.29 (+1.61 G) | 0.21 (+1.15 G) | 0.03 (+0.18 G) | 0 (−0.21 G) | 0 (−0.63 G) | 0 / 0 | inconclusive |
| Receiver STREAM | 0.40 (+0.46 G) | 0.37 (+0.43 G) | 0 (−0.13 G) | 0.12 (+0.17 G) | 0 (−0.11 G) | 0 (−0.32 G) | 0 / 0.17 | inconclusive |
| Receiver DATAGRAM | 0.39 (+0.45 G) | 0.37 (+0.45 G) | 0 (−0.13 G) | 0.16 (+0.18 G) | 0 (−0.06 G) | 0 (−0.50 G) | 0 / 0 | inconclusive |

The total excess is positive in every block: about +4.8–5.0 Gcycles per GiB over Reno's 26.2–30.0 at the sender. The call-graph lead and the counter domains agree on user space, but no group reaches half the excess. Under the registered rule, the CPU flags stay **raised and unresolved**. No discrimination stage was triggered, and no defect is demonstrated.

**Descriptive, not a registered outcome.** The largest positive leaf-function changes at the candidate sender are spread widely across the BBR-enabled per-packet path. In each of the following, 0.1–0.3 Gcycles/GiB:
- runtime and `sync` mutex lock and unlock, and channel `selectgo` — about 0.9–1.1 G together;
- map hashing, probing and assignment under `captureCongestionSend` — about 0.3 G;
- `recoveryEvidence.lowerBound`, `.sent` and `.ack`;
- per-packet `externalPacketConn.capabilities` and `managedPacketRawConn.capabilities`;
- `sendReservation.resize` and `localSendCredit.reserve`;
- `beginCongestionFeedback` and BBR ECN feedback.

Kernel copy and lock work is slightly lower than Reno's. Allocation and GC as a whole are only 0.03–0.05 of the sender excess. That bounds the Mac record's two allocation sites to a minor share; their exact cycle cost was not isolated. These are production-slicing leads for the next decision, not causes.

### Memory: timeline and heap sites

Every candidate endpoint reaches 99% of its whole-run peak RSS at 23–40 s, against ProbeBW entry at 1.6–1.7 s. There is one exception, an S5 DATAGRAM sender block at 1.9 s. Measured-window footprint is above 1.10× Reno in every cell, so no flag is a Startup transient. Heap-site classification under the unchanged 70% rule:

| Flag | Ratio | Block 1 / block 2 | Classification |
| --- | --- | --- | --- |
| S5 DATAGRAM sender RSS | 1.144 | bookkeeping / bookkeeping | **Design-bounded bookkeeping** (2.5–3.0 MiB) |
| S5 STREAM receiver RSS | 1.441 | delivery / delivery | **Delivery data**: reassembly behind 0.25% overflow loss (Mac: unresolved) |
| S5 STREAM sender RSS | 1.202 | unresolved (bookkeeping 2.4, delivery 2.0 MiB) / bookkeeping | **Unresolved**, mixed |
| S6 STREAM sender RSS | 1.571 | unresolved / unresolved | **Unresolved**, mixed delivery, bookkeeping and other |
| S6 STREAM receiver RSS | 1.754 | unresolved (delivery 64%) / unresolved (delivery 65%) | **Unresolved**, mixed (Mac: delivery) |
| S6 DATAGRAM sender RSS | 1.411 | bookkeeping / unresolved | **Unresolved** |

The S5 DATAGRAM receiver and S6 DATAGRAM receiver cells did not raise flags. Their heap-site rows are in [attribution.json](attribution.json).

### D2's three cells, re-attributed on Linux

| Cell | Mac (covered) | Linux | Same cause and magnitude? |
| --- | --- | --- | --- |
| S6 control p95 | 1.547 / 1.481, Up policy | STREAM 1.541. Both conditions hold: 99–100% of forward-queue samples above 25 ms fall in Up or the following Down; Cruise and Refill median queue delay is about 0 ms; matched-load S5 p95 is 1.030/0.691. DATAGRAM passes at 1.134. | Same cause, same or smaller magnitude |
| S6 STREAM receiver RSS | 1.574, delivery | 1.754, unresolved (delivery 64–65%, bookkeeping 35%) | Larger, and composition changed |
| S5 sender RSS | 1.203 / 1.132, bookkeeping | DATAGRAM 1.144, bookkeeping; STREAM 1.202, one block unresolved | DATAGRAM same cause, slightly larger; STREAM composition changed |

D2 does not carry over, so whether to extend any exception to Linux is for the next decision.

### Goodput (not attributed)

No registered stage targets the new goodput flags, and no stage was added. Descriptively, the counted S5 runs spend 59–80% of the measured window in Cruise, 9–26% in Up, 5–10% in Down, 2–3% in Refill and 2–4.5% in ProbeRTT. They deliver 84–88 Mbit/s against Reno's 94–96. On loopback, both arms use about 2.3 cores of sender CPU, and the candidate costs about 26% more per GiB. A send loop limited by per-packet user work would fit, but it is not tested. Both goodput flags stay **raised and unresolved**.

## Flag report for the next decision

For [Decide whether the Linux-measured BBRv3 candidate deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/713), on candidate `fc4c1bf1`, Linux (`minimax`):

| Status | Flags |
| --- | --- |
| **Useful benefit** | S6: 12.07× (STREAM) and 11.09× (DATAGRAM) Reno goodput in 5/5 pairs each, at 0.125/0.129× Reno sender CPU per useful GiB. |
| **Passed** | S6 sender and receiver CPU, S6 DATAGRAM receiver RSS and control p95. S5 control p95 and S5 DATAGRAM receiver RSS. Loopback RSS and control p95 (candidate), and loopback DATAGRAM receiver CPU. |
| **Raised with attribution** | S6 STREAM control p95 1.541: selected ProbeBW Up policy. S5 DATAGRAM sender RSS 1.144: design-bounded bookkeeping. S5 STREAM receiver RSS 1.441: delivery data, loss-driven reassembly. |
| **Raised and unresolved** | Goodput: loopback 0.770/0.795, S5 0.913/0.901. Sender CPU: loopback 1.261/1.208, S5 1.237/1.108 — user-space per-packet work in the BBR-enabled path, localization inconclusive. Receiver CPU: loopback STREAM 1.189, S5 1.155/1.131. Memory: S5 STREAM sender 1.202, S6 STREAM sender 1.571, S6 STREAM receiver 1.754, S6 DATAGRAM sender 1.411. |
| **Raised (preservation)** | Reno on candidate: S5 DATAGRAM sender RSS 1.152, loopback DATAGRAM control p95 1.226. Both unattributed under the registered rule, with A/A ranges that reach or exceed them. Executed work is equal to frozen Reno's within 0.2%. |

## Deviations

1. **Endpoint launcher.** Found and fixed on excluded smoke runs before any comparative observation; see [prerequisites](#endpoint-peak-rss-on-linux).
2. **Descriptive analyses added after the outcomes.** The leaf-function list, the per-packet figures and the phase-time shares are descriptive, added after the registered outcomes were known. They change no classification. The per-packet reporting helper, `packet_io` in [localize.py](localize.py), was added after the readiness stages began and before any counters data was analyzed. It classifies nothing; the registered rule functions are unchanged from `a540ac6e`.
3. **New flags without a registered attribution stage.** The loopback and S5 goodput flags and the loopback CPU flags were not anticipated by the registration, which localized only S5. No stage was added for them, so they are reported as unresolved. Localizing the loopback CPU excess would need a new post-registered stage.

## Preservation

- **Payload integrity.** All 213 retained observations exited cleanly and passed receiver integrity: 110 readiness, 92 attribution and 11 excluded smoke and prerequisite runs.
- **Default Reno.** Reno built from the candidate matches frozen Reno on every counter within 0.2%, on goodput within 0.4%, and on CPU within 2.3%. Two preservation flags are raised under the registered rule, as reported above.
- **No code change.** No transport source changed, and no fix revision was built. The gates for `fc4c1bf1` are those recorded by #710. No recorded QUIC translation, cap, evidence contract, controller version or default changed.
- **No host change.** `perf_event_paranoid`, socket-buffer limits, offloads, the governor and every other host setting were left unchanged. `perf` and `tcpdump` ran with `sudo`, attached passively.

## Limits

- **Platform and topology.** One Linux host, loopback endpoints and a userspace relay. This is not a native two-host topology or a kernel emulator. It certifies no other platform, no real carrier path and no production readiness, and it does not reinterpret the Mac results.
- **Paths.** The paced paths engage neither GSO nor `sendmmsg` batching, so the comparison measures the unbatched send path. The endpoints' 8 MiB effective socket buffers reflect the host's 4 MiB `rmem_max`/`wmem_max`.
- **Repetition.** Five blocks per readiness cell. The attribution stages ran two to six blocks. Heap-site resolution is about 0.5 MiB per site.
- **Engagement.** The CE response is unengaged on S5 and S6. C4 items 1 and 3 and the low-rate pacing floor were not examined.

## Assets and reconstruction

- **Results:**
  - [summary.json](summary.json): readiness observations, groups, pairs, flags with A/A, and contamination.
  - [localization.json](localization.json): counters, packet I/O and call-graph localization.
  - [attribution.json](attribution.json): timeline and heap sites.
- **Raw data:** [raw.tar.gz](raw.tar.gz), with [raw-manifest.json](raw-manifest.json). It holds every observation, the prerequisites, the stage log and the build receipts, plus folded call stacks in place of the root-owned `perf.data` files. Binaries and credentials are omitted.
- **Aids:** [build.py](build.py), [run.py](run.py), [matrix.py](matrix.py), [prereq.py](prereq.py), [analyze.py](analyze.py), [localize.py](localize.py) with [localize_test.py](localize_test.py), [attribution.py](attribution.py), [pack.py](pack.py), [calibrate/](calibrate/), [launch/](launch/), [relay/](relay/), [overlay/](overlay/), [counting/](counting/) and [model-overlay/](model-overlay/). These are frozen one-ticket aids, not a maintained benchmark framework.

To reconstruct, in a fresh owned worktree of this branch:

1. Run `build.py`, then rsync the worktree, without `.git`, to a Linux host with the same CPU layout.
2. Under `taskset -c 1-3`, run `prereq.py`, then `matrix.py smoke`, `perfsmoke` and `ecnsmoke`.
3. Run `loopback`, `s5`, `s6`, `counters` and `callgraphs`, then `localize.py fold`, then `timeline` and `heapsites`.
4. Run `analyze.py`, `localize.py` and `attribution.py`.


## What stays fixed and what changes

- **Fixed (D3):** candidate `fc4c1bf1` and frozen Reno `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; the fixture and workloads with the causal-diagnosis heap patch; the relay's frozen v1 queue model with #710's delivery observability; paths, durations and readiness seeds; the flag limits; the registered memory and preservation rules; `GOTOOLCHAIN=go1.27.0`.
- **Changed, all recorded:**
  - *Host:* `minimax`, AMD Ryzen AI MAX+ 395 (16 physical cores in two 8-core L3 complexes, SMT sibling of CPU n at n+16), Linux 7.0.0-30-generic, `powersave` governor, owned hardware with no cloud charge.
  - *Builds:* cross-compiled `linux/amd64` with `CGO_ENABLED=0`, `-trimpath -buildvcs=false`, Go 1.27.0 ([build.py](build.py)).
  - *Relay:* #711's v3 relay plus the Linux ECN adapter ([relay/linux-ecn.patch](relay/linux-ecn.patch)), applied at build time. The queue model is unchanged.
  - *Endpoint launch:* a small launcher ([launch/main.go](launch/main.go)) between the runner and each endpoint; see [prerequisites](#endpoint-peak-rss-on-linux).
  - *Instruments:* `perf` 7.0.14 attached by PID with `sudo` for the measured window only, instead of `/usr/bin/time -l` and the macOS Go profiler.
  - *Arms:* an A/A frozen-Reno arm joins every readiness path.
- A Linux pass does not show that the Mac excess was noise, and a Linux attribution does not transfer to macOS without its own evidence.

## Prerequisites

Each was checked before any comparative observation. All passed. Every check run is excluded from every statistic and retained under `prereq/` and the `smoke*`, `perfsmoke*` and `ecnsmoke` observations.

### Resources

| Role | CPUs (physical cores) | SMT siblings, never used |
| --- | --- | --- |
| Sender | 8–11 | 24–27 |
| Receiver | 12–15 | 28–31 |
| Relay (WAN paths) | 4, 5 | 20, 21 |
| Runner and `perf` | 1–3 | — |

Both endpoints share one L3 complex (CPUs 8–15), and the relay sits on the other. Allocations are identical for every arm. `lscpu -e` and the L3 sharing lists are recorded in `prereq/host-facts.json`. Each observation records every thread's allowed CPU list and last CPU at mid-run (`host-facts.json`), and smoke runs confirm all endpoint and relay threads stay inside their sets. Host contention is sampled every second from `/proc/stat` per CPU and from the fixture processes' own CPU time (`host-cpu.json`), and the top foreign processes by CPU over each run are listed.

### Privilege and socket buffers

- Endpoints and the relay run as the ordinary user (UID 1000, `CapEff` 0). `kernel.perf_event_paranoid` stays 4.
- `perf` runs as `sudo perf stat` or `sudo perf record`, attached by PID at the start of the measured window and stopped with SIGINT at its end, on the runner CPUs. `perf` re-raises SIGINT after writing its output, so exit status −2 is a normal stop.
- The endpoints request 7 MiB socket buffers and the relay 8 MiB. Unprivileged `SO_RCVBUFFORCE` fails, so the kernel caps the request at `net.core.rmem_max`/`wmem_max` (4 MiB) and doubles it: `ss -m` shows `rb8388608`/`tb8388608` on all four sockets. Each observation records `ss -uampn` at mid-run and the UDP error counters (`RcvbufErrors`, `SndbufErrors`, `InErrors`, `MemErrors`) before and after.
- The kernel has no `IRQ_TIME_ACCOUNTING`. Softirq work done in a process's context is billed to that process's system time. On these paths, delivering a sent datagram to the peer's socket and waking the peer happen in the sender's context, so they count in the sender's CPU time, as kernel send and wake work.

### ECN portability

- **Calibration** (`prereq/calibrate-*.json`, aid [calibrate/main.go](calibrate/main.go)). Two plain UDP sockets send 200 packets of each codepoint through the relay in each direction on S5:
  - *Negative control, unadapted #711 relay:* all 800 packets per direction arrive Not-ECT, and the relay counts every packet as Not-ECT. This confirms the decision's code-inspection finding: unadapted on Linux, the relay strips ECN.
  - *Adapted relay:* every packet arrives with the codepoint it was sent with, in both directions — Not-ECT, ECT(1), ECT(0) and CE, 200 each.
- **Endpoint engagement.**
  - *S5 and S6:* the relay counts all forward packets but 4, and all reverse packets but 2, as ECT(0) — the handshake packets before validation. So QUIC ECN validation succeeds in both directions.
  - *Loopback:* a passive `sudo tcpdump` of 20,000 packets mid-run (`prereq/loopback-tcpdump.txt`) shows ECT(0) on every packet in both directions.
  - *BBR ECN tracker:* the candidate sender's call graphs contain `(*bbrECNTracker).feedback`, `.mode` and `captureBBRECN` samples on loopback, S5 and S6.

### Instruments

- `perf stat -p` counts `instructions:u`, `instructions:k`, `cycles:u` and `cycles:k`, plus context switches, migrations, page faults, wakeups issued (`sched:sched_wakeup`), send, receive, futex, epoll and sleep syscalls and `net:net_dev_xmit`. All events count at 100% running time, with no multiplexing.
- `perf record -e cycles --call-graph fp -p` resolves both kernel and Go frames, using Go's frame pointers.
- Unlike the macOS profiles, Linux samples reach user code. In a candidate-only smoke run, the S5 sender's cycles fall about 8% in BBR code, 25% in kernel send, 17% in kernel wake and scheduling, 16% in runtime scheduling and 28% in other user code. Those smoke figures are an instrument check and enter no comparison.

### Endpoint peak RSS on Linux

The first smoke runs reported identical peak RSS at both endpoints: 17.77/17.77 MiB, then 30.27/30.27 MiB. On Linux, `ru_maxrss` survives `execve`: an endpoint spawned directly by the Python runner inherits the runner's resident high-water mark, which exceeded the endpoint's own. The readiness rule needs each endpoint's own peak. So each endpoint is now forked by a small launcher from its own few-MiB image. The launcher records the endpoint's PID and forwards termination signals. With it, each endpoint's `getrusage` peak equals its own `VmHWM` exactly, and sender and receiver values differ (for example 21.15/15.80 MiB). The runner samples `VmHWM` every second as a cross-check. The measure is unchanged — whole-run peak RSS from the fixture's `getrusage` — and only the spawning changed. This was found and fixed on excluded smoke runs, before any comparative observation.

## Registration: readiness

Written and committed before any readiness observation. Nothing below changes after outcomes are seen; deviations are recorded as deviations.

### Arms, paths and blocks

| Label | Build | Controller | Role |
| --- | --- | --- | --- |
| Reno | `reno` (frozen `e4f322cb…`) | Reno | Matched reference |
| A/A Reno | `reno` | Reno | Second frozen-Reno run in the block, tag `aa` |
| Candidate | `cand` (`fc4c1bf1`) | BBRv3 | Candidate under test |
| Reno on candidate | `cand` | Reno | Default-Reno preservation control |

| Path | Model | Timing | Arms | Blocks | Seeds |
| --- | --- | --- | --- | --- | --- |
| Loopback | none | 5 s warmup, 20 s measured | Reno, A/A, candidate, Reno on candidate | 5 per workload | — |
| S5 | 100/100 Mbit/s, 100 ms RTT, one-BDP drop-tail queue | 10 s warmup, 30 s measured | Reno, A/A, candidate, Reno on candidate | 5 per workload | 9001–9005 |
| S6 | S5 plus 0.1% independent forward loss | 10 s warmup, 30 s measured | Reno, A/A, candidate | 5 per workload | 9101–9105 |

That is 110 observations: 40 loopback and 70 WAN. Both workloads run in every block. Controller order rotates per block ([matrix.py](matrix.py)), every arm in a block shares its seed, and observations run sequentially. Plain builds; no profiler is attached. The endpoint contract is #711's: GOMAXPROCS=4, tracing disabled, M=1400, 16,384-byte STREAM writes, 1,200-byte DATAGRAMs, and the reliable control stream. A failed observation is retained and reported, and none is excluded after its outcome is seen.

### Measures and flags

The measures are #711's: receiver-verified goodput; CPU seconds per useful GiB at each endpoint (`getrusage`, whole run); whole-run peak RSS at each endpoint; control p95 and p50; and the relay's bottleneck loss, queue delay, lateness and ECN counts. Payload integrity is checked on every observation.

The flag rule is unchanged. Per path, workload and measure, a flag is **raised** when the median of the five per-block ratios against the same block's Reno crosses its limit: goodput below 0.95 (loopback and S5), CPU per useful GiB above 1.10, peak RSS above 1.10, control p95 above 1.20. On S6, goodput is the useful benefit, repeatable when the candidate beats Reno in all five pairs. Reno on candidate is held to the same limits as a preservation check.

**A/A.** The A/A arm's per-block ratios are reported beside every ratio. They never override the median rule.

**Contamination.** Per observation, *foreign CPU* is the busy time on the fixture CPUs and their SMT siblings (4, 5, 8–15, 20, 21, 24–31) during the measured window, minus the fixture processes' own CPU time, in cores. An observation is **contaminated** when its foreign CPU averages above 0.10 cores, or any one-second sample exceeds 0.50 cores. Contaminated observations are retained and counted in the registered medians. A sensitivity table recomputes every flag without the blocks that contain one, and a flag whose status changes is reported as contamination-sensitive. The rule is [localize.py](localize.py) `contamination`, exercised on synthetic cases.

**Packet I/O.** The counters stage measures the native path rather than assuming it: send syscalls and `net_dev_xmit` per forward packet show whether sends are batched or segmented (GSO), and receive syscalls per packet show receive batching.

## Registration: attribution stages

Written, with the analysis code in [localize.py](localize.py) and its synthetic tests in [localize_test.py](localize_test.py) (14 cases, all passing), before any observation of these stages existed. These stages give attribution only, never flag values. New seeds are used, and every run is retained.

### Stage `counters`

- **Runs.** S5, both workloads, six blocks (seeds 9201–9206). Arms: Reno, A/A Reno, candidate and Reno on candidate, plain builds. `perf stat` attaches to both endpoints for the measured window.
- **Measures,** per endpoint and per useful GiB of the measured window: user and kernel instructions and cycles, context switches, migrations, page faults, wakeups issued, and the syscall and transmit counts above. A run's counters are usable only when every event counted at ≥ 95% running time. Unusable runs are reported, and their blocks are left out of the counter analysis.
- **Outputs.** Per endpoint and block, the ratio against the same block's Reno for each measure, and the user/kernel split of the excess: `user share = max(Δuser, 0) / (max(Δuser, 0) + max(Δkernel, 0))`, separately for instructions and cycles. Medians are taken over blocks. A/A ratios give the stage's noise range.

### Stage `callgraphs` and the localization rule

- **Runs.** S5, both workloads, three blocks (seeds 9501–9503). Arms: Reno and candidate. `perf record -e cycles -F 4999 --call-graph fp` attaches to both endpoints for the measured window.
- **Primary metric.** Cycles per receiver-verified useful GiB of the measured window: the sum of sample periods (cycles) in each group, divided by useful GiB. Cycles are primary because CPU time is what the flags measure.
- **Groups.** Every sample falls in exactly one group ([localize.py](localize.py) `classify_stack`):

  | Group | Rule, in precedence order |
  | --- | --- |
  | `kernel_wake_sched` | A kernel sample with any wake or scheduling frame: `try_to_wake_up`, `__wake_up*`, `ep_poll_callback`, `futex_wake*`/`futex_wait*`, `__schedule`, `schedule*`, `finish_task_switch`, `ep_poll`, sleeps, enqueue and dequeue. A wakeup inside a send — waking the peer — counts here. |
  | `kernel_send` | Other kernel samples under `__sys_sendmsg`, `__sys_sendmmsg`, `udp_sendmsg` or `udp_send_skb`, including loopback delivery to the peer's socket. |
  | `kernel_recv` | Other kernel samples under `__sys_recvmsg`, `__sys_recvmmsg` or `udp_recvmsg`. |
  | `kernel_other` | All other kernel samples: page faults, timers, interrupts. |
  | `alloc_gc` | User samples with any allocator or GC frame: `runtime.mallocgc*`, `gcBgMarkWorker`, `bgsweep`, `gcAssistAlloc*`, `gcDrain*`, `scanobject` and kin. |
  | `user_sched` | Other user samples with a runtime scheduling, parking, spinning or wake frame: `schedule`, `findRunnable`, `park_m`, `stopm`, `notesleep`, `futexsleep`, `netpoll`, `procyield` and kin. |
  | `bbr` | Other user samples with a BBR or bookkeeping frame: `internal/congestion` BBR, the ackhandler recovery, retained, sampler, congestion-dispatch and BBR-ECN functions, and `(*localSendCredit)`. Their callees count here unless they allocate or schedule. |
  | `user_other` | All other user samples: packing, framing, crypto, application. |

- **Excess.** Per block, each group's excess is the candidate's cycles per GiB minus Reno's in the same block. A group with no samples has zero. Shares are taken of the **positive pool**, the sum of the positive group excesses. Negative excesses are reported as offsets and left out of the pool.
- **Repeatability.** Localization needs a positive total excess in at least two of the three blocks. Otherwise the outcome is **no repeatable excess**.
- **Lead.** A group **leads** when its median share across blocks is at least 0.5 and its excess is positive in every block.
- **Instructions and cycles.** The counters stage gives the user share of the candidate's excess for cycles and for instructions. When the two put the majority in different domains, the disagreement is reported, the lead is still taken from cycles, and the discrimination step must name the instruction-domain alternative as a competitor. When the call-graph lead's domain (user or kernel) differs from the domain holding the counters stage's cycles majority, the outcome is **inconclusive: conflict**.
- **Outcomes.** *Localized* (a lead), *no repeatable excess*, or *inconclusive* (no lead, or a conflict). This is applied to the candidate sender. It is also applied to the receiver for any raised receiver CPU flag. Localization says where work executes. It does not classify a cause.

### Stages `timeline` and `heapsites`

These are #711's stages, runs, seeds (9301–9302 and 9401–9402; 9311–9312 and 9411–9412) and rules, unchanged ([attribution.py](attribution.py)). Arms are `reno-diag` and `cand-diag-counted`, S5 and S6, both workloads, two blocks. They classify every raised memory flag under the unchanged rules: Startup transient, steady-state excess and mixed, then delivery data against BBR bookkeeping by heap site, with 70% to lead. They re-attribute D2's three cells on Linux: S6 control p95 by queue delay per BBR phase, S6 STREAM receiver RSS, and S5 sender RSS.

### Preservation

#711's preservation rule is unchanged. A raised Reno-on-candidate CPU flag is attributed to measurement noise when the counters stage's Reno-on-candidate median instructions per GiB lie in [0.97, 1.03] and the readiness ratio lies inside the counters stage's A/A CPU range. The flag stays raised either way. Any raised Reno-on-candidate goodput, RSS or latency flag is an unattributed preservation flag.

### Discrimination and disposition

- **Discrimination.** For a localized lead, a discrimination stage is designed after localization. It is appended to [matrix.py](matrix.py) and registered here before it runs. It names the hypothesis and a competing explanation, and a bounded comparison or intervention that separates them. A hot function, a kernel majority or a declared bound alone never satisfies this step.
- **Disposition.** Only a demonstrated, contract-preserving implementation defect is fixed in this ticket, on one new identified revision. The attribution comparison and every readiness stage are then rerun on that revision. A fix is confirmed only when the targeted excess falls while receiver integrity, the existing gate tests and the preservation checks hold. The two known allocation sites are eligible only through this route. A pacing or platform cost, a design bound, a contract-change cause or an unresolved cause is reported to the next decision, not fixed or pre-accepted.
- **Stopping.** At most one fix revision. A second candidate defect, a needed contract change, or an unmet prerequisite stops the ticket and is reported.

### Order

`loopback`, `s5`, `s6`, then `counters`, `callgraphs`, `timeline` and `heapsites`, all sequential, under `taskset -c 1-3`. Any discrimination stage follows its registration.
