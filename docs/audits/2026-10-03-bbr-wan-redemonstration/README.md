# Re-demonstration of the WAN-corrected BBRv3 candidate

**Date:** October 3, 2026, America/New_York. **Scope:** [Re-demonstrate the WAN-corrected BBRv3 candidate in matched local comparisons](https://github.com/the-sarge/quic-go-fast/issues/711), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). Experimental candidate branch only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-wan-redemonstration`, based on `a59a9779` (the [quantum-pacing record](../2026-10-02-bbr-quantum-pacing/README.md), which contains the adopted D1+D2 candidate `fc4c1bf1`). It applies D4–D6 of the [WAN-rate correction choice](https://github.com/the-sarge/quic-go-fast/blob/15f56caf/docs/audits/2026-10-02-bbr-wan-correction-choice/README.md#d4-residual-cpu-re-measure-then-diagnose-with-a-discriminating-comparison-no-waiver) to a rerun of the [correction demonstration](https://github.com/the-sarge/quic-go-fast/blob/80466857/docs/audits/2026-10-02-bbr-correction-demonstration/README.md) harness.

## Answer

**Not yet competent across the measured local paths. No readiness is claimed.** The WAN-corrected candidate `fc4c1bf1` passes every limit on loopback. It keeps a large, repeatable useful benefit on injected-loss S6: 12.3× (STREAM) and 14.1× (DATAGRAM) Reno's goodput in all five pairs, at 0.32× Reno's sender CPU per useful GiB. On matched-load S5, goodput is within 3.1% of Reno and control p95 matches it. But flags remain raised on S5 and S6:

- **Sender CPU on S5:** 1.17× (STREAM) and 1.32× (DATAGRAM). The candidate sender executes 13–14% more instructions per packet, with the same packet count as Reno. Allocation churn is identified but too small to explain it. The leading hypothesis, extra kernel send and runtime wake work, was not discriminated. **Raised and unresolved.**
- **Peak memory:** every flagged excess is a steady-state excess, not a Startup transient. Where the excess is attributed:
  - The S5 sender's excess is design-bounded BBR bookkeeping, led by the design's 32,768-entry outcome ledger.
  - The S6 STREAM receiver's excess is loss-driven reassembly.

  The other memory flags mix both causes and stay unresolved under the registered 70% rule.
- **Control p95 on S6 (1.55/1.48):** attributed again to the selected ProbeBW Up policy. Matched-load S5 shows no p95 excess and a much lower median.
- **Default-Reno preservation:** Reno built from the candidate executes the same instructions per GiB as frozen Reno (0.99–1.00). Its S5 DATAGRAM readiness CPU ratio (1.19/1.16), however, lies outside the A/A range seen in the counters stage, so under the registered rule the preservation flag stays **raised**.

The [flag report](#flag-report-for-the-qualification-decision) lists every flag for [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672). No implementation defect met the registered fix gate, so no new revision was built: every result in this record is on `fc4c1bf1`.

## Readiness results

Plain builds, five blocks per path and workload, 80 observations, all clean. Ratios are the median [min–max] of per-block ratios against the same block's frozen Reno. Bold marks a raised flag; parentheses give the blocks crossing the limit.

| Path, workload | Arm | Goodput | Sender CPU/GiB | Receiver CPU/GiB | Sender RSS | Receiver RSS | Control p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Candidate | 0.975 [0.967–0.985] | 1.014 | 1.028 | 0.994 | 1.049 | 1.057 (1) |
| Loopback STREAM | Reno on candidate | 0.986 | 1.002 | 1.000 | 1.001 | 1.005 | 1.011 |
| Loopback DATAGRAM | Candidate | 0.985 [0.714–1.019] (2) | 1.037 (1) | 1.031 (1) | 1.021 | 1.048 | 1.053 (2) |
| Loopback DATAGRAM | Reno on candidate | 1.012 | 1.002 (1) | 1.001 (1) | 0.987 | 1.006 | 0.841 (1) |
| S5 STREAM | Candidate | 0.969 [0.936–0.973] (1) | **1.170 [1.012–1.466] (3)** | 1.089 (2) | **1.203 (4)** | **1.407 [1.350–1.424] (5)** | 1.074 (1) |
| S5 STREAM | Reno on candidate | 1.000 | 1.065 (1) | 1.083 (2) | 1.003 (2) | 1.013 | 0.994 |
| S5 DATAGRAM | Candidate | 0.979 [0.929–0.986] (1) | **1.320 [0.906–1.566] (3)** | **1.138 [0.765–1.532] (3)** | **1.132 (4)** | 0.999 | 1.003 |
| S5 DATAGRAM | Reno on candidate | 1.000 | **1.192 [0.901–1.304] (3)** | **1.158 [0.924–1.453] (3)** | 0.992 | 1.000 | 1.011 |
| S6 STREAM | Candidate | benefit 12.31× [12.10–14.94] | 0.317 | 0.405 | **1.393 (5)** | **1.574 (5)** | **1.547 [1.497–1.678] (5)** |
| S6 DATAGRAM | Candidate | benefit 14.09× [12.31–16.16] | 0.320 | 0.425 | **1.374 (5)** | 1.048 (1) | **1.481 [1.418–1.626] (5)** |

Absolute medians (Mbit/s; s/GiB; MiB; ms):

| Path, workload | Arm | Goodput (utilization) | Sender / receiver CPU | Sender / receiver RSS | Measured-window heap peak | Control p95 / p50 |
| --- | --- | --- | --- | --- | --- | --- |
| Loopback STREAM | Reno | 1316.4 | 20.46 / 20.08 | 21.12 / 20.83 | 3.26 / 3.26 | 0.209 / 0.163 |
| Loopback STREAM | Candidate | 1289.0 | 20.75 / 20.66 | 20.84 / 21.84 | 3.38 / 3.57 | 0.221 / 0.165 |
| Loopback DATAGRAM | Reno | 1179.9 | 23.45 / 21.98 | 21.20 / 21.70 | 3.16 / 3.33 | 0.325 / 0.177 |
| Loopback DATAGRAM | Candidate | 1125.8 | 24.55 / 22.67 | 21.64 / 22.61 | 3.37 / 4.03 | 0.276 / 0.173 |
| S5 STREAM | Reno | 95.9 (0.959) | 27.30 / 39.11 | 23.58 / 23.34 | 5.87 / 3.35 | 183.3 / 173.4 |
| S5 STREAM | Candidate | 92.9 (0.929) | 28.78 / 39.55 | 28.30 / 32.84 | 10.66 / 13.10 | 200.2 / 102.2 |
| S5 DATAGRAM | Reno | 94.4 (0.944) | 27.82 / 38.11 | 23.77 / 20.72 | 5.44 / 3.26 | 181.4 / 171.2 |
| S5 DATAGRAM | Candidate | 92.4 (0.924) | 36.73 / 43.36 | 26.91 / 21.00 | 9.29 / 4.12 | 183.4 / 100.7 |
| S6 STREAM | Reno | 6.1 (0.061) | 88.87 / 89.09 | 19.42 / 20.06 | 3.51 / 3.46 | 122.7 / 105.5 |
| S6 STREAM | Candidate | 85.6 (0.856) | 28.41 / 36.10 | 27.84 / 31.56 | 10.50 / 12.17 | 200.2 / 101.0 |
| S6 DATAGRAM | Reno | 5.9 (0.059) | 117.37 / 100.40 | 19.67 / 20.33 | 3.43 / 3.41 | 125.3 / 104.2 |
| S6 DATAGRAM | Candidate | 85.9 (0.859) | 38.18 / 42.92 | 27.50 / 21.31 | 9.51 / 4.15 | 185.6 / 100.6 |

**Variability.** The S5 CPU ratios swing widely in both directions. Frozen Reno's own S5 sender CPU spans 26.4–38.4 s/GiB (DATAGRAM) and 20.1–28.0 s/GiB (STREAM) across blocks. The host's non-fixture load during runs ranged from about 300% to 1,300% CPU, and that load does not correlate consistently with any arm's CPU per GiB. Loopback DATAGRAM has one candidate block at 0.714× Reno goodput. That block is disclosed, and the median passes.

**Bottleneck loss, from the relay's forward packets.**

| Path, workload | Arm | Overflow | Injected random loss |
| --- | --- | --- | --- |
| S5 STREAM | Reno | 0.068% | 0 |
| S5 STREAM | Candidate | 0.347% [0.279–0.922] | 0 |
| S5 DATAGRAM | Reno | 0.328% | 0 |
| S5 DATAGRAM | Candidate | 0.033% [0.027–0.103] | 0 |
| S6 STREAM | Reno | 0% | 0.091% |
| S6 STREAM | Candidate | 0.266% [0.226–0.461] | 0.102% |
| S6 DATAGRAM | Reno | 0% | 0.095% |
| S6 DATAGRAM | Candidate | 0.101% [0.023–0.249] | 0.099% |

On S5 STREAM the candidate overflows about 5× as often as Reno, consistent with #710's quantum-release finding. On S5 DATAGRAM it overflows about a tenth as often as Reno.

**ECN engagement.** In every WAN observation, both controllers sent all forward packets with ECT, so QUIC ECN validation succeeded and the candidate's BBR ECN tracker and ledger were active. The S5 and S6 queues never CE-mark: zero CE in every run. The CE response itself is therefore unengaged on these paths.

**Relay timing.** Median per-run maximum lateness is 2.8–20.3 ms by arm. Stalls (10 ms intervals with a wakeup more than 5 ms late) occur in both arms, at up to 87 per run, and hit Reno runs as well as the candidate's.

## Attribution

Four post-registered stages ran: `counters` (48 observations), `timeline` (16), `heapsites` (16) and `profiles` (12). All were clean and none was excluded. [attribution.py](attribution.py) applies the registered rules and writes [attribution.json](attribution.json).

### CPU (D4)

**`counters`.** These are median per-block ratios over six blocks against the same block's Reno, from `/usr/bin/time -l`.

| Workload | Arm, endpoint | CPU s/GiB | Instructions/GiB | Cycles/GiB | Switches/GiB | Rule outcome |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | Candidate sender | 1.093 | 1.132 (6/6 > 1) | 1.102 | 1.048 | executed-work excess |
| STREAM | Candidate receiver | 1.025 | 1.030 | 1.027 | 0.999 | neither |
| STREAM | Reno on candidate, sender / receiver | 1.034 / 1.032 | 1.004 / 1.001 | 1.043 / 1.039 | 1.006 / 0.998 | equal work |
| STREAM | A/A Reno, sender / receiver | 1.018 / 1.020 | 1.002 / 1.000 | 1.028 / 1.029 | 1.005 / 1.000 | A/A CPU range 0.939–1.103 / 0.945–1.099 |
| DATAGRAM | Candidate sender | 1.203 | 1.139 (6/6 > 1) | 1.206 | 1.153 | executed-work excess |
| DATAGRAM | Candidate receiver | 1.056 | 1.023 | 1.058 | 0.992 | neither |
| DATAGRAM | Reno on candidate, sender / receiver | 1.024 / 1.010 | 0.994 / 0.990 | 1.023 / 1.012 | 0.983 / 0.983 | equal work |
| DATAGRAM | A/A Reno, sender / receiver | 1.012 / 1.014 | 1.007 / 1.014 | 1.014 / 1.015 | 1.012 / 1.018 | A/A CPU range 0.988–1.088 / 1.000–1.064 |

The candidate sender's excess is per packet, not extra packets:

- Forward packets per useful GiB are 0.986–1.004× Reno, and reverse packets 0.950–1.013×.
- Frozen Reno executes about 141,000 (STREAM) and 150,000 (DATAGRAM) instructions per forward packet. On this host that is mostly kernel send and runtime wake work.
- The candidate adds about 18,700 and 23,200 instructions per packet.

**`profiles`.** Allocation profiles locate the candidate sender's extra allocation, 165–258 MiB per useful GiB, almost entirely at two BBR-only sites:

- *Delivery-record map insertion:* `d.packets[...] = info` in [congestion_dispatch.go](../../../internal/ackhandler/congestion_dispatch.go), `captureCongestionSend`, at 139–184 MiB/GiB. Each packet inserts and later deletes a record of about 150 bytes, so the map keeps rebuilding its tables.
- *Per-reservation object:* `&sendReservation{…}` in [local_send_credit.go](../../../local_send_credit.go), `reserve`, at 43–99 MiB/GiB.

Neither allocation is required by a design bound. Together they are about 245 bytes per packet. Even at several instructions per allocated byte, that bounds them to roughly a tenth of the 18,700–23,200 instruction excess. So they are an implementation-churn lead, not the attributed cause.

The CPU profiles cannot locate the rest. They sample about 71% of each process's CPU time, and in both arms 97–100% of sampled time lands in four leaf frames: `syscall.rawsyscalln` (send), `runtime.kevent`, `runtime.pthread_cond_wait` and `runtime.pthread_cond_signal`. Allocator, GC and BBR frames do not appear in either arm. Under the registered grouping, the candidate's sampled excess divides as follows:

- **STREAM:** scheduler 37%, 0% and 77% by block, with the remainder in send syscalls. No group reaches 50%.
- **DATAGRAM:** scheduler 49–52%, with the remainder in send syscalls. The registered H-sched group therefore leads by a margin smaller than its own block-to-block variation.

H-alloc never leads, so the registered fix gate was not met, and no fix was built. Building one would not change any flag's status.

**Classification.**
- **Sender CPU, S5 STREAM and DATAGRAM: raised and unresolved.** The leading hypothesis is extra kernel send and runtime wake work per packet under BBR's emission pattern. It is consistent with the profiles, but no discriminating comparison separated it from BBR computation hidden by the profiler's skew. The allocation-churn sites are recorded for production work.
- **Receiver CPU, S5 DATAGRAM: raised and unresolved.** Its readiness ratio, 1.138, lies outside the A/A range (1.000–1.064), and the counters stage shows no work or scheduling excess (instructions 1.023, CPU 1.056).
- **Default-Reno preservation, S5 DATAGRAM CPU: raised.** Executed work is equal: instructions 0.994 / 0.990 against the registered [0.97, 1.03]. Every candidate change sits behind `EnableBBR`. In the counters stage, the same comparison gives CPU ratios of 1.024 / 1.010. But the readiness ratios (1.192 / 1.158) lie outside the stage's A/A range, so the registered rule leaves this flag raised. The evidence points to host-noise exposure in the readiness blocks, not a Reno code change, but no rule converts that into a pass.

### Memory (D5)

**`timeline`.** No flagged peak is a Startup transient. Every candidate endpoint reaches 99% of its whole-run peak RSS between 7.4 and 34.6 s. That is after ProbeBW begins (1.5–4.6 s) plus one second, and after the warmup in most runs. The measured-window footprint (Go mapped-not-released memory) is above 1.10× Reno in every flagged cell:

| Path, workload | Sender footprint / live heap ratio | Receiver footprint / live heap ratio |
| --- | --- | --- |
| S5 STREAM | 1.41–1.44 / 1.66–1.80 | 1.95–2.06 / 5.18–5.37 |
| S5 DATAGRAM | 1.27–1.32 / 1.58–1.62 | 1.22 / 2.09–2.11 |
| S6 STREAM | 1.39–1.54 / 1.61–1.98 | 1.55–1.56 / 2.43–4.35 |
| S6 DATAGRAM | 1.71 / 4.59–4.62 | 1.20–1.23 / 2.26–2.34 |

This revises the earlier demonstration's reading for this revision, which classed part of the excess as a Startup transient.

**`heapsites`.** In-use bytes by allocation site come from the profile nearest each measured-window heap peak, compared with Reno in the same block. Profile sampling resolution is about 0.5 MiB per site.

- **BBR bookkeeping recurs at both endpoints:**
  - the recovery-outcome ring, `recoveryEvidence.sent`, at 1.16 MiB;
  - the feedback event buffer, `beginCongestionFeedback`, at 0.6–0.8 MiB;
  - the live delivery-record map, `captureCongestionSend`, at about 0.5 MiB;
  - the recovery slot index, at about 0.5 MiB;
  - the ECN ledger, at about 0.5 MiB.

  With Go's default GC goal, about 2.5–3 MiB of live bookkeeping accounts for the S5 sender's +4.7 MiB RSS.
- **Bookkeeping is design-bounded.** The design declares a send-outcome ledger "capped at 32,768 fixed-size entries … covering ACK-only transmissions as well as sampled packets", and live records under "the existing 25,000 tracked-packet ceiling". It calls these bounds "chosen memory/accuracy trade-offs". At about 8,900 packets/s (sender) and thousands of ACK-only registrations per second (receiver), the ring fills within seconds. That settles D5's open question: full up-front allocation is not the source of the excess, because the occupancy uses the whole bound. Removing this cost needs a design change to the bounds.
- **Delivery data:** receiver STREAM frame-pool buffers (`wire.init.0.func1`, 2.0–5.5 MiB) and `frameSorter` hold data behind losses, as in #710. S6 DATAGRAM senders hold 2–3 MiB of `SendDatagram` frames in flight at 86 Mbit/s, where Reno runs at 6 Mbit/s.

**Classification by registered rule.**

| Flag | Rule outcome | Attribution |
| --- | --- | --- |
| S5 STREAM sender RSS 1.203 | Bookkeeping in both blocks | **Design-bounded** BBR bookkeeping; removing it needs a contract change to the ledger and record bounds |
| S5 DATAGRAM sender RSS 1.132 | Bookkeeping in both blocks | **Design-bounded** BBR bookkeeping, as above |
| S6 STREAM receiver RSS 1.574 | Delivery in both blocks | **Loss-driven reassembly**: algorithm behaviour at 86 Mbit/s with 0.37% forward loss, against Reno's 6 Mbit/s |
| S5 STREAM receiver RSS 1.407 | Delivery in one block, unresolved in the other (bookkeeping 64%) | **Unresolved**; mixed reassembly and bookkeeping |
| S6 STREAM sender RSS 1.393 | Unresolved in both blocks | **Unresolved**; mixed in-flight delivery data and bookkeeping |
| S6 DATAGRAM sender RSS 1.374 | Unresolved in both blocks (delivery 66%) | **Unresolved**; mixed in-flight DATAGRAM frames and bookkeeping |

### Control latency on S6 (D6)

In the four counted S6 runs, 98–100% of forward-queue samples above 25 ms fall in ProbeBW Up or the following Down. Cruise and Refill median queue delay is 0.45–0.50 ms. On matched-load S5, candidate control p95 is 1.074/1.003× Reno, and the candidate's median control latency is 101–102 ms against Reno's 171–173 ms. Both registered conditions hold, so the S6 control p95 flags (1.547/1.481) are **attributed to the selected draft-06 ProbeBW Up policy** on a one-BDP buffer, where a 6%-utilization Reno leaves the queue empty.

## Flag report for the qualification decision

For [Decide whether corrected BBRv3 deserves further qualification](https://github.com/the-sarge/quic-go-fast/issues/672), on candidate `fc4c1bf1`:

| Status | Flags |
| --- | --- |
| **Passed** | Loopback: all limits, both workloads. S5: goodput, receiver CPU (STREAM), receiver RSS (DATAGRAM), control p95, both workloads. S6: sender and receiver CPU, receiver RSS (DATAGRAM). |
| **Raised with attribution** | S6 control p95, STREAM and DATAGRAM: selected ProbeBW Up policy. S5 sender RSS, STREAM and DATAGRAM: design-bounded BBR bookkeeping, needing a contract change to the 32,768-entry ledger and record bounds. S6 STREAM receiver RSS: loss-driven reassembly. |
| **Raised and unresolved** | S5 sender CPU, STREAM 1.170 and DATAGRAM 1.320: 13–14% more instructions per packet; leading hypothesis kernel send and runtime wake work; allocation churn of about 245 B/packet bounded to a minor share. S5 DATAGRAM receiver CPU 1.138. S5 STREAM receiver RSS, S6 STREAM sender RSS and S6 DATAGRAM sender RSS: mixed delivery data and bookkeeping. Default-Reno preservation: S5 DATAGRAM CPU 1.192/1.158 against equal executed work. |
| **Useful benefit** | S6: 12.31× (STREAM) and 14.09× (DATAGRAM) Reno goodput in 5/5 pairs each, at 0.32× Reno sender CPU per useful GiB. |

## Deviations

1. **Switch rule, amended before the stage ran.** macOS reports nearly all thread blocking as involuntary switches, so the `counters` rule uses total context switches. The amendment was recorded in the post-registration before any `counters` observation ran.
2. **Site classes, refined before stage data.** The `heapsites` site mapping was refined on an excluded smoke run, before any stage observation existed:
   - runtime frames hidden, so bytes land on the allocating caller;
   - a measurement-instrument class added for the sampler and profile writer;
   - `beginCongestionFeedback`, `spaceSlots`, the ackhandler packet pool and `runSendQueue` assigned to their groups.

   The final analysis code was committed before attribution outcomes existed (`bd21449c`).
3. **The CPU profile's precondition failed on this platform.** The registered group comparison assumed CPU samples would reach user code. They did not: 97–100% of samples fall in four syscall and scheduler leaf frames in both arms. The per-packet instruction and allocation sizing above was added post hoc to bound the allocation lead. It is not a discriminating comparison, and it converts no flag.
4. **The counters dichotomy is weaker than registered.** On Go, idle threads spin before parking, and macOS instruction counts include kernel send and wake work. "Executed-work excess" in instructions therefore does not by itself separate BBR computation from wake cost. The executed-work outcome is reported as registered, with this limit.

## Preservation

- **Payload integrity.** All 176 retained observations exited cleanly and passed receiver integrity: zero corrupt or duplicate payload, per-second totals equal to useful bytes, and matching sender and receiver reports. These are 80 readiness, 92 attribution and 4 excluded smoke runs.
- **Default Reno.** Reno built from the candidate stays within about 1% of frozen Reno on loopback, and matches it on S5 goodput, RSS and control p95. Its executed instructions per GiB are 0.990–1.004× frozen Reno. The S5 DATAGRAM CPU preservation flag above remains raised under the registered rule.
- **No code change.** No transport source changed in this ticket. The gates for `fc4c1bf1` are those recorded by #710 ([gates.log](../2026-10-02-bbr-quantum-pacing/gates.log)). No recorded QUIC translation, cap, evidence contract, controller version or default changed.

## Limits

- **Platform and topology.** One macOS host, loopback endpoints and a userspace relay, on a shared desktop with heavy, recorded, uncontrolled background load: about 300–1,300% non-fixture CPU, including Lightroom. This is not a native two-host or kernel emulator, and certifies no other platform, real carrier path or production readiness.
- **Repetition.** Five blocks per readiness cell; attribution stages ran two to six blocks. The heap-site comparison has about 0.5 MiB resolution per site.
- **Profiling.** On macOS, Go CPU profiles here are dominated by syscall and scheduler leaf frames, so per-function CPU attribution of user code was not possible. A discriminating CPU comparison would need a profiler or counters that resolve user and kernel work separately. That is a platform choice outside this ticket.
- **Engagement.** The CE response is unengaged on S5 and S6, whose queues never mark. C4 items 1 and 3 were not re-examined.
- **Pacing floor.** The floor regime (Q = 2M at low rates) remains unmeasured.

## Assets and reconstruction

- **Results:** [summary.json](summary.json) for readiness observations, groups, pairs and flags, and [attribution.json](attribution.json) for the counters, timeline, heap-site and profile analyses.
- **Raw data:** [raw.tar.gz](raw.tar.gz) with [raw-manifest.json](raw-manifest.json). It holds every observation (configs, endpoint, series, work and relay records, commands, exits, host samples, profiles), stage logs and build receipts. Binaries and credentials are omitted.
- **Aids:** [build.py](build.py), [run.py](run.py), [matrix.py](matrix.py), [analyze.py](analyze.py), [attribution.py](attribution.py), [overlay/](overlay/), [counting/](counting/), [relay/](relay/) and [model-overlay/next.go](model-overlay/next.go). These are frozen one-ticket aids, not a maintained benchmark framework.

To reconstruct, in a fresh owned worktree of this branch:

1. Run `build.py`.
2. Run `matrix.py smoke`, then `loopback`, `s5` and `s6`.
3. Run `matrix.py counters`, `timeline`, `heapsites` and `profiles`.
4. Run `analyze.py`, then `attribution.py`.

The runner refuses to overwrite a prior attempt. Copied aids are checked byte-identical to their source records at build time.

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

### Stage `profiles`: where the candidate sender's extra work is (D4 step 1 for `counters`)

Registered after the `counters` stage and before this stage ran.

`counters` found executed-work excess at the candidate sender: median instructions per useful GiB 1.132 (STREAM) and 1.139 (DATAGRAM), above 1 in all six blocks. Forward packets per useful GiB are 0.986–1.004× Reno and reverse packets 0.950–1.013×, so retransmission volume does not explain the excess. Instructions per forward packet are 1.09–1.17× Reno. The sender also allocates about 0.24–0.25 more bytes per useful byte than Reno in both workloads: 0.44–0.46 against 0.21 GiB/GiB for STREAM, and 1.94–1.95 against 1.69–1.70 for DATAGRAM.

- **Runs.** S5, both workloads, three blocks (seeds 9501–9503). Arms: Reno and candidate, plain builds with CPU and allocation profiles, under `/usr/bin/time -l`. Profiles perturb the runs, so this stage gives attribution only.
- **Hypotheses.**
  - *H-alloc:* per-packet allocation in BBR-specific code, and the allocator and GC work it causes.
  - *H-book:* the candidate's per-packet BBR bookkeeping computation, with no excess allocation.
  - *H-sched:* runtime scheduling and wakeups from pacing.
- **Comparison.** For each, sum the CPU samples per useful GiB, candidate minus Reno, over its function groups:
  - allocator and GC: `runtime.mallocgc*`, `runtime.gcBgMarkWorker` and its descendants, write barriers;
  - BBR code: `internal/congestion` BBR, plus the ackhandler sampler, recovery, retained, ECN and congestion-dispatch functions, as self time excluding allocator frames;
  - scheduler: `runtime.schedule`, `findRunnable`, `park_m`, `notesleep` and `notewakeup`, `kevent` and `pthread_cond*`.

  Also compare allocated bytes per useful GiB by allocation site.
- **Decision.** A group accounting for at least 50% of the summed positive excess is the leading hypothesis.
  - If H-alloc leads, the single largest excess allocation site is the defect candidate. It is classified as implementation churn when the allocation is not required by a design bound: a per-packet or per-opportunity heap object whose contents could live in existing owned storage without changing any contract.
  - Such a defect is fixed on a new revision, with failing-first tests where behaviour is observable. The discriminating comparison is then the `counters` arms rerun on the fixed revision. The fix is confirmed when the candidate's instructions and allocation excess fall, while receiver integrity and the existing gate tests hold.
  - Every readiness stage is then rerun on the fixed revision, so the final tables do not mix revisions.
  - If no group reaches 50%, the CPU flag stays raised and unresolved.

<!-- Results, attribution, preservation, limits and assets are added after the stages run. -->
