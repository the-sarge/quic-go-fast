# ProbeRTT kernel-timestamp follow-up — issue #1210

**Disposition: no controller or ACK-policy change is justified. The added software timestamps locate small typical costs in Linux send scheduling, Linux ACK delivery, and macOS socket delivery. They do not locate the dominant remaining cost within the driver, NIC, network, or the still-unmeasured macOS transmit path.**

The owner authorized the recommended follow-up to the [original-path investigation for issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6652453700). This continues the [accepted investigation brief for issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6651545107). It neither ships an implementation change nor selects a shallower ProbeRTT policy. The issue remains open and its existing readiness disposition is unchanged.

## Workload and build identity

The original native Wire workload remains minimax → mbp128 over 10 GbE, MTU 1500, 1,099-byte application DATAGRAM messages scheduled at 1 Gbps, one connection, BBRv3 and ordinary conservative ACKs at both ends, GOMAXPROCS 4, and 10/60/5-second warmup/measurement/drain phases. The historical fork base is `bc4915ea0de0c238ecfdc51d827f7da67639a789`; harness and product inputs are the same frozen inputs reconciled in the [earlier evidence for issue #1210](../2026-11-05-probertt-investigation/README.md). These are observations of that historical native build, not qualification of current main.

The final capture uses Linux binary SHA-256 `9b746b6b4d134a5b87cc8d93f92b6f758683740e2eb9453add97f690da3621be` and Darwin `b3f10a8dd4d6071ab58c7322638b18c264c25a7897488fb854eabe450ac509a7`. The initial batch used Linux `92c4dde77ede3366a5710df893c4bf1d7fc900a3b8f17e3f844fdd6d05b52463`; Darwin is unchanged. Build receipts retain Go 1.27.3, flags and exact source hashes. Embedded version strings describe the historical inputs; the fork and harness overlay manifests identify the actual diagnostic executables.

The observer preserves the protocol's original receive timestamp and all congestion/ACK decisions. It adds encrypted UDP identities before decryption, software timestamp control messages, immediate read-batch return observations, and local clock calibrations. It retains approximately one in sixteen packet identities in each direction. Linux TX selection applies to the first segment of a potentially segmented submission; its timestamp is not a separately measured wire departure for each segment. Ambiguous joins and coalesced receive aggregates are excluded from precise packet attribution. Existing packet-number observations remain available.

The expanded recorder is bounded at four million 128-byte rows per process, increased from the previous two-million limit to hold the additional observations. The same recorder capacity and binary are used for all final-batch arms. The new observer has its own environment switch. Both diagnostic binaries were reproduced from the retained source package; no observation records were dropped.

## Attempts, adverse outcomes and controls

Two host-coordinated executions consumed six native workload attempts. Five completed their 75-second phases, totaling 375 seconds; one failed during setup with zero scheduled application messages. Loopback socket and metadata validation witnesses are recorded separately from physical-path workload attempts.

| Execution / arm | New observer | Generator misses | Canceled / late records | Measurement application goodput | Complete measured probes |
| --- | --- | ---: | ---: | ---: | ---: |
| Initial / before | Off | 66,132 | 26 / 6 | 990.305 Mbps | 5 |
| Initial / kernel | Setup failed | — | — | No workload measurement | — |
| Initial / after | Off | 55,194 | 0 / 14 | 991.910 Mbps | 5 |
| Final / before | Off | 70,804 | 0 / 1 | 989.625 Mbps | 8 |
| Final / kernel | On | 55,770 | 0 / 5 | 991.827 Mbps | 6 |
| Final / after | Off | 55,195 | 0 / 7 | 991.911 Mbps | 6 |

Every admitted application record eventually arrived in the five completed workloads. No application record was rejected, duplicated, invalid, or missing after admission. The initial control's 26 canceled attempts were deadline failures. Late arrivals remain in the accounting; they are not counted as on-time delivery. All five completed runs passed the existing boundary-witness alignment check. None is presented as a qualification pass.

The failed kernel arm exposed a diagnostic integration constraint: native Wire's closed send-metadata subset rejects transmit timestamp control records. The raw Linux socket witness had passed, but it did not exercise that wrapper. The retry adds an inert-by-default harness allowance for exactly the two diagnostic records, only when both observation switches are enabled. Existing framing, duplicate, routing and source/interface checks remain active. A Linux witness verifies the enabled and disabled cases, rejects malformed and unrelated metadata, and preserves rejection of foreign packet-info. The full failed attempt is retained; this is not a product defect or an omitted measurement.

All three final arms use the same rebuilt binaries. Their steady ProbeRTT raw, byte-weighted ACK residence is 225.6–227.1 µs before, 225.3–244.6 µs with the observer, and 223.9–227.2 µs after. Model-minimum medians span roughly 75–95 µs across those arms. Acknowledged transport rates remain approximately 470–665 Mbps during the observed arm. Whole-minute miss counts differ with probe counts and ordinary variation; neither an observer speedup nor precisely zero observation overhead is established. The characteristic low-rate ProbeRTT cycle remains present.

## What the new boundaries measure

Predictions were recorded before capture: receiver socket delivery would be large if it dominated the excess; sender ACK delivery would be large if it dominated; Linux scheduled/software TX residence would be large if local send queueing dominated. If these intervals were small, earlier receive stages, later transmit stages and the unavailable macOS transmit boundary would remain unresolved.

Linux `SO_TIMESTAMPING` reports software RX, TX scheduler and software TX timestamps in the system clock. Scheduler timing is before queue discipline, and software TX is before NIC transmission; RX is after delivery from the driver into the stack. These are not hardware or physical-wire timestamps. The NIC reports no PTP hardware clock or hardware TX/RX timestamp support. See the [Linux timestamping documentation](https://docs.kernel.org/networking/timestamping.html) and retained capability receipt.

On macOS, `SO_TIMESTAMP_MONOTONIC` supplies `mach_absolute_time` ticks at UDP delivery before socket append. The host's measured timebase is 125/3; `CLOCK_UPTIME_RAW` observations map it into the existing diagnostic monotonic clock. No controller timestamp is replaced. Public [XNU UDP delivery code](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/netinet/udp_usrreq.c) and [IP control-message code](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/netinet/ip_input.c) explain that boundary; public source is not asserted identical to the running kernel. Installed headers and a real socket witness, with a deliberately delayed read, validate the local timestamp and conversion behavior.

Darwin has no public equivalent of the Linux TX API used here. Its BPF interface could provide an additional software capture point, but `/dev/bpf*` is root-only on this host and noninteractive administrator access is unavailable. Therefore **macOS kernel/interface TX was not measured**. This is a concrete limit of this execution, not an inference that the missing interval is small.

Each duration is calculated within one endpoint's clock. Linux uses paired wall/monotonic observations; macOS uses bracketed uptime calibration. No unsynchronized endpoint timestamps are subtracted to infer a one-way delay.

## Measured local components

The analyzer selects complete ProbeRTT intervals within measurement and excludes the first/last 2 ms as in the preceding analysis. This is an analysis selection, not a product acceptance gate.

| Local component | Samples | Median | 95th percentile |
| --- | ---: | ---: | ---: |
| macOS UDP delivery → read-batch return | 4,961 | 9.521 µs | 19.688 µs |
| macOS batch return → protocol receive timestamp | 4,961 | 1.000 µs | 2.834 µs |
| Linux ACK software RX → read-batch return | 325 | 3.497 µs | 8.346 µs |
| Linux batch return → protocol receive timestamp | 325 | 0.080 µs | 0.140 µs |
| Linux write entry → TX scheduler timestamp | 418 | 1.638 µs | 2.755 µs |
| Linux TX scheduler → software TX | 418 | 1.843 µs | 2.845 µs |
| Linux write entry → software TX | 418 | 3.547 µs | 5.540 µs |

These are different sampled cohorts; their medians must not be added into an exact RTT partition. Their typical scale does not support any of these measured local stages as the source of the persistent roughly quarter-millisecond cycle. The earlier large post-feedback scheduling hypothesis remains unsupported.

There are real tails. macOS UDP-delivery-to-batch-return reaches 97.104 µs at the 99th percentile and 623.521 µs at the maximum. Linux ACK delivery reaches 37.881 µs, and write-entry-to-software-TX reaches 17.412 µs. This investigation does not claim that local service stalls never occur or explain every individual slow packet.

Fourteen correlated data/ACK cycles across five probes support a partial partition. Subtracting each cycle's macOS UDP-delivery-to-ACK-write interval and Linux ACK-software-RX-to-protocol interval leaves 179.771–212.171 µs, with median 196.745 µs. **That remainder still includes the sender's logical-send-to-transmit work**, in addition to the unmeasured host/device/network portions. No typical TX value from another cohort is subtracted from it.

Only two cycles include every available local boundary. Both are unsegmented transmissions in the same probe, so they are illustrations, not a representative residual distribution:

| Raw RTT | Sender logical-send → software TX | macOS UDP delivery → ACK write entry | Linux ACK software RX → protocol receive | Remaining |
| ---: | ---: | ---: | ---: | ---: |
| 215.824 µs | 11.662 µs | 26.688 µs | 4.268 µs | 173.206 µs |
| 217.929 µs | 11.813 µs | 27.729 µs | 3.677 µs | 174.710 µs |

Those remainders combine Linux post-software-TX through macOS UDP delivery and macOS pre-write-call through Linux software RX. They include the unavailable macOS transmit boundary, earlier receive processing, driver/NIC work and the network. They are not measured propagation, cannot be assigned to one endpoint, and cannot identify interrupt moderation or a NIC defect.

## Validity and disposition

The observed arm records one QUIC connection per endpoint. Setup receipts include several temporary sockets; measured traffic and periodic counters are on socket 1. There are zero dropped rows, recorded timestamp errors, missing RX timestamps or truncated reads in the retained counters, and no ambiguous identity joins. All 245,239 retained Linux TX submissions have one scheduler and one software timestamp. Last periodic counters are snapshots rather than a claim about packets after the final trace flush.

All 73 macOS calibration witnesses have uncertainty intervals compatible with initial setup; offset movement is below 0.23 µs. Linux wall-to-monotonic offset is generally stable within tens of nanoseconds, with isolated excursions up to approximately 4.4 µs; per-submission offset changes are retained. No selected residence interval is negative. Clock-offset deltas themselves may be negative and are not negative residence times.

The existing exclusive-consumer admission, independent lease witnesses, provider identity checks, owned staging and archive custody were reused. Both executions released their leases and removed owned remote staging after verification. Mac performance-core mean activity before/after was 9.76%/5.54% for the initial execution and 20.30%/10.01% for the retry; efficiency-core means were 57.81%/41.49% and 17.63%/25.10%. These are one-second boundary snapshots, not host-isolation proof or evidence that efficiency-core activity saturated performance cores.

The real native fixture and macOS socket witness passed under the race detector. The admitted Linux host passed real receive and scheduler/software transmit timestamp witnesses, including GSO, before traffic. The metadata guard witness passed before the retry. Independent analysis review found and corrected truncation-mask and ambiguous-join issues before interpreting captures; deterministic witnesses verify different endpoint clock origins, conversion consistency, exclusions and complete-cycle arithmetic. An observer maintenance hook was also moved after the original receive-time assignment before any native capture. Earlier build/test receipts and the failed setup remain retained.

**Keep the accepted ProbeRTT and ordinary ACK policies.** No implementation defect with a targeted production correction is established. No broader qualification, acceptance change, or requalification release is implied. Queue-draining, changing-RTT and Reno-coexistence obligations still apply to any separately proposed shallower ProbeRTT policy.

**Recommendation:** obtain an administrator-enabled, bounded macOS interface TX/RX capture correlated by the retained encrypted UDP identities, alongside the Linux timestamps. This would measure the missing macOS transmit and pre-UDP receive portions before choosing a host-stack, driver or network intervention. The available Linux NIC cannot supply hardware timestamps, so even that software capture must not be labeled physical wire timing.

## Evidence and replay

The `source/` and `harness-overlay/` manifests identify inert diagnostic source snapshots. `operations/` and `capture-receipts/` preserve exact launch inputs, host/custody receipts, both executions and the setup failure. `observations/manifest.json` hashes all published numeric traces, result records, logs and ledgers. The five original-analysis JSON files and `kernel-analysis.json` retain per-probe data, exclusions and counters.

The complete raw archive is `/Users/josh/quic-go-fast-1210-kernel-timestamps/native-captures.tar.gz`, SHA-256 `bdc0027de7f053a3722768108183d61ebc8c6c997d27474845cce07f179f11c5`, with 613 verified files. This is same-machine custody, not independent disaster recovery. The published observation bundles and source overlays support offline replay independently of that archive. Active temporary source is removed after validation; cleanup receipts identify the owned worktrees and restored files.

From this directory, replay the timestamp analysis with:

```sh
python3 analyze-kernel.py observations/q1210-kernel2/kernel /absolute/path/to/kernel-analysis.json --arm kernel
python3 analyze-kernel-test.py
```

Create a fresh dedicated feature worktree at `bc4915ea0de0c238ecfdc51d827f7da67639a789` under `/Volumes/worktrees/quic-go-fast/`, then reconstruct the final diagnostic binaries with:

```sh
python3 prepare.py /Volumes/worktrees/quic-go-fast/replay-kernel-1210
python3 build.py /Volumes/worktrees/quic-go-fast/replay-kernel-1210
```

The build helper uses the retained historical harness archive in the preceding evidence directory, applies the narrow harness overlay and checks both binary hashes. It launches no traffic. Live replay requires fresh owned paths and fresh host admission; the recorded capture roots are frozen evidence.
