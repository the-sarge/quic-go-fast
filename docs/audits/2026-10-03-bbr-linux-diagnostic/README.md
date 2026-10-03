# Linux diagnostic of the BBRv3 candidate on owned hardware

**Date:** October 3, 2026, America/New_York. **Scope:** [Measure and attribute the BBRv3 candidate's flags on owned Linux hardware](https://github.com/the-sarge/quic-go-fast/issues/712), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666). It applies D3 of the [accepted qualification decision](https://github.com/the-sarge/quic-go-fast/blob/b5f759c6/docs/audits/2026-10-03-bbr-qualification-decision/README.md#d3--next-a-linux-diagnostic-on-owned-hardware). Diagnostic only: no production merge, paid resource, campaign resumption, default-controller change or ledger change.

Branch `codex/bbr-linux-diagnostic`, based on `b5f759c6` (the decision record, which contains the [re-demonstration](../2026-10-03-bbr-wan-redemonstration/README.md) at `dc252f8a` and the candidate `fc4c1bf1`).

<!-- Results, attribution, preservation, limits and assets are added after the stages run. -->

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
