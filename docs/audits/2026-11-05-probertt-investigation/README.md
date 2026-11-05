# ProbeRTT feedback investigation — issue #1210

**Disposition: no implementation or policy change is justified by this investigation. The original native case reproduces, and its low rate is explained principally by the actual feedback cycle being much longer than the model minimum, with a smaller contribution from window occupancy and packet granularity. Most of that cycle remains below the measured pre-submission application boundaries; its kernel, driver, NIC and network components are not separated.**

This executes the [accepted investigation brief for issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6651545107). It advances the original-path attribution beyond the [local triage verification for issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6651410158). It does not ship a fix, select a larger ProbeRTT target, change wiremux acceptance, or claim controller qualification. The issue remains open; the absence of a selected fix does not reopen triage.

The later [correction to the larger-window evidence on issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6652223418) is acknowledged: the earlier 20% deficit-bin cutoff hid shallower pauses, and a four-times target was twice the estimated BDP. The present independent timing investigation neither relies on those pause counts nor treats that larger window as evidence of retained queue-draining benefit.

## Original case and build reconciliation

The tested path is the original native Wire direction, minimax (`192.168.1.50`, `enp97s0`) → mbp128 (`192.168.1.110`, `en8`) over 10 GbE, MTU 1500. Both endpoints select BBRv3 with the ordinary conservative ACK policy. The workload is version 6, `native-wire-v031`, one connection, 1,099-byte scheduled application DATAGRAM messages at 1 Gbps, GOMAXPROCS 4 on each endpoint, 10 seconds warmup / 60 seconds measurement / 5 seconds drain, and no Go profile. The existing sender skips scheduled identities more than 1 ms late. Native admission is `legacy` with watcher `none`; absence of a watchdog closure is not stall validation.

The reconstructed historical input is wiremux product `8282a84652387d3e2f644dfac1310da479fbe6b4`, harness `b88eee5b23df2476dbe98717308e287c35d6efa0` plus the frozen contextual-v4 and native-diagnostic overlays, and quic-go-fast `bc4915ea0de0c238ecfdc51d827f7da67639a789` plus its frozen diagnostics. **Both baseline binaries rebuilt byte for byte to the binaries used by the original [native ProbeRTT experiment for wiremux issue #1460](https://github.com/GridSwarm/wiremux/blob/01ba7d6daf554cf75beed43cbefb466f55f98e8b/docs/research/1460-residual-qualification/native-probertt-v1/README.md):** Linux `da7e02870878eed6234f3f9404b01a62499ea467c372a8618f8520feacef05e2`; Darwin `a9f94aa609d4f4c0adb2944776f1bc41633361326b38723a7958754afb0f9edd`. This is stronger reconciliation than matching the version string alone. The old diagnostic suppression, window multiplication and other overrides remain off in every present arm.

Pickup main was `30e47b84215bc629c3a9286070b28dc0e3c2169e`. Its BBR controller, application ACK tracker, BBR send policy and local send credit match the historical production base. Its connection, emission and recovery surroundings have changed. These runs deliberately establish the mechanism on the original historical build; they are not native reproduction or qualification of current main.

The native source requests 1,232-byte initial QUIC packets. The trace observes an effective ordinary packet budget of 1,438 bytes during the measured probes; the dominant data packet length is 1,126 QUIC bytes for 1,099 useful application bytes. The published transport receipt's 1,191-byte application ceiling is its setup observation, not a claim about later packetization. Linux socket samples show 8,388,608-byte receive and send buffer limits, zero sampled receive queue, and a `pfifo_fast` qdisc; these sparse samples do not rule out short kernel residence. Mac per-socket queue sampling returned no matching rows and is unmeasured. GSO is not publicly reported by the harness; the diagnostic handoff records retain its actual segmentation-size argument instead of inferring offload from the platform. The read-only Linux NIC inspection reports `r8169`, RTL8127 firmware, EEE enabled but inactive, and an unsupported coalescing query. None of those facts attributes the delay to a particular NIC feature.

## Native results and interventions

Four traffic attempts completed once each, with 300 seconds of workload phases in total. Every run's existing boundary-witness check passed. Every admitted application record arrived. No record was rejected, late, invalid, or missing. The threshold-one arm had two canceled, deadline-failed attempts, making its deficit union 63,288 rather than its 63,286 generator misses; the other three arms had no canceled attempts. These are diagnostic observations, not a relaxation or pass of a qualification gate.

| Arm | Binary / ACK decision | Generator misses | Application goodput over measurement | Measured ProbeRTT or deficit windows |
| --- | --- | ---: | ---: | ---: |
| `baseline1` | Exact historical binary, ordinary ACKs | 74,094 | 989.143 Mbps | Seven approximately 200 ms scheduled-slot deficit windows |
| `trace1` | Observation overlay, ordinary ACKs | 85,953 | 987.405 Mbps | Eight complete ProbeRTT intervals within measurement |
| `ack1` | Observation overlay, receiver ACK-queue threshold one | 63,286 | 990.726 Mbps | Five complete ProbeRTT intervals within measurement |
| `restored-trace` | Same binary as `ack1`, ordinary ACKs restored | 87,528 | 987.174 Mbps | Eight complete ProbeRTT intervals within measurement |

The uninstrumented original-binary baseline's deficit windows attempted approximately 0.47–0.66 Gbps by scheduled slot, reproducing the issue's symptom on its original path. This metric counts attempted scheduled identities, including an unsuccessful attempt if one occurred; it is not a transport ACK rate or an instantaneous receiver arrival rate. The minute-wide application goodput above comes from the receiving application ledger. In this experiment every admitted record eventually arrived, but that does not turn a scheduled-slot cohort into a measured receive-time interval.

The ACK intervention changes only the ACK-queue threshold on mbp128, selected by the diagnostic host environment. Retained headers confirm the sender override is false, the receiver override is true only in `ack1`, and restoration disables it. Threshold one is unsupported diagnostic policy. It does **not** force the connection to emit an ACK between every pair of packet-processing calls.

The lower whole-minute miss count in `ack1` is confounded by fewer ProbeRTT entries. Its five measured probes still carry approximately 495–561 Mbps of acknowledged QUIC bytes, with byte-weighted raw ACK residence of 244–251 µs. Ordinary traced probes carry 471–574 Mbps initially and 405–566 Mbps after restoration. This establishes no removal of the low-rate cycle and no reliable per-probe improvement. A cadence effect cannot be established or excluded by this single fixed-order intervention; the runs do not estimate its repeatability.

The exact historical binary demonstrates that the symptom exists without the new recorder. The traced arms retain the same characteristic approximately 200 ms, sub-Gbps cycles, but instrumentation cost is not precisely estimated. An optional additional exact-binary repeat was withdrawn while waiting for CI leases, before host admission, staging or any attempt; its cancellation and independently verified release/absence are retained. It is not counted as a failed traffic attempt, replacement, or fifth measurement.

## Correlated ProbeRTT timeline

All detailed quantities below come from the sender's steady ProbeRTT intervals. The analyzer excludes 2 ms at each phase boundary to avoid draining and restoration transients; that is an analysis selection, not a product or qualification requirement. The initial `trace1` interval crosses the warmup-to-measurement supply pause and lasts about 1.19 seconds; it remains in the data but is excluded from the steady measurement comparison.

| Traced arm | Effective window | Byte-weighted raw ACK residence | Model minimum RTT | Time-weighted flight / window | Useful attempts by scheduled slot |
| --- | ---: | ---: | ---: | ---: | ---: |
| Ordinary `trace1` | 11.9–19.3 KB | 187–250 µs | 82.6–91.3 µs | 90.8–94.8% | 459–560 Mbps |
| Receiver threshold one | 16.5–19.0 KB | 244–251 µs | 75.5–92.1 µs | 89.6–95.0% | 483–547 Mbps |
| Ordinary restored | 13.3–19.1 KB | 236–250 µs | 79.8–92.3 µs | 90.4–94.6% | 395–552 Mbps |

The raw residence is byte-weighted across all acknowledged packet records, not a median of the newest RTT-eligible packet in each ACK. This distinction matters when one ACK covers a batch. Sender ACK-read-to-feedback-completion time adds roughly 4–5 µs on matched ACKs. Once feedback leaves at least one maximum-packet allowance, the next observed send opportunity follows in a median 0.17–0.20 µs; per-probe 95th percentiles are below 0.8 µs. Thus the data do not support a large post-feedback connection-loop scheduling stall.

In steady `trace1` intervals, about 93.3–93.9% of elapsed time lies between an emission exit labeled congestion-limited and the next opportunity. This is wall-time classification by the previous stop, not CPU time executing congestion control. Pacing credit medians remain about 61–63 KB against a 1,438-byte packet budget. Pending local bytes are zero at every retained steady-feedback observation in all three traced arms, and those feedback records report no lost bytes. This does not prove local queues were always empty between observations; sampled registration and submission events measure their residence separately.

For a representative ordinary interval, the effective window is 19,071 bytes and the model minimum is 87.1 µs: `window / minimum` suggests 1,753 Mbps of QUIC bytes. Actual byte-weighted processing-complete residence is 251.5 µs. Replacing the minimum by that cycle reduces the arithmetic to about 607 Mbps; applying the measured 91.8% flight occupancy gives about 557 Mbps, compared with 573 Mbps actually acknowledged. This approximate reconstruction is within 3%. The clocks are deliberately different: recovery's supplied send timestamp can precede the registration callback by several microseconds of construction and batch processing, while the occupancy integral starts at the observed callback. Sampled registration-hook deltas are retained; this is not an exact conservation identity between those two clocks. Useful scheduled attempts in that interval are about 559 Mbps, consistent in scale with 1,099 / 1,126 payload efficiency, but they remain a different interval/accounting metric.

The dominant discrepancy is therefore replacing a rare minimum by a contemporaneous roughly quarter-millisecond feedback cycle. Whole-packet admission and incomplete occupancy are a further roughly 5–10% factor in these observations. The deliberate reduced window is still the mechanism that makes those cycles limit throughput; this finding does not imply that the window can be enlarged without losing ProbeRTT's benefits.

## Where the actual cycle goes, and what remains unknown

The following components use 212 ACKs matched by packet number across the first ordinary traced arm's steady measurement probes. Each duration is computed on its own endpoint's monotonic clock. Unsynchronized endpoint clock offsets are never subtracted.

| Component | Median | Mean | Boundary |
| --- | ---: | ---: | --- |
| Sender recovery timestamp → ACK socket-read timestamp | 247.3 µs | 220.9 µs | Raw round trip for matched newest packets; not the byte-weighted row above |
| Sender recovery timestamp → worker write-call start | 12.6 µs | 11.9 µs | Includes construction before the registration hook and local handoff residence |
| Receiver data socket read → ACK write-call start | 11.0 µs | 11.1 µs | Includes receive processing, ACK decision, packing and handoff |
| Remainder after subtracting those two pre-write intervals | 222.5 µs | 197.9 µs | Combined submission calls, kernel/driver/NIC/network work and socket-read service; not measured propagation |
| Sender ACK socket read → feedback completion | 4.3 µs | 4.6 µs | Additional control service after raw RTT ends |

Only means of the first three components subtract to the mean remainder; medians are not additive. The matched samples cover each measured probe, but are a deterministic packet-number sample, not a statistical confidence interval. The remainder has a similar median of about 221 µs with threshold one and 224 µs after ordinary ACK restoration. Write-call starts are userspace wrapper entry observations, not kernel-entry or wire-departure timestamps. The retained write-call durations are only a few microseconds and are included in this remainder, not additional terms to add to it. Completion proves local submission returned, not that the packet had left the NIC.

Ordinary ACK groups commonly contain 10–16 packets during ProbeRTT; changing the queue threshold to one still commonly yields 14–16. The receiver's encoded ACK delay is generally 0 or 8 µs in the initial ordinary trace, consistent with a short userspace turnaround and encoded delay quantization, not a large delayed-ACK timer wait. The real ACK tracker witness queues the first packet only under the diagnostic override, but, when 15 received packets are serviced before `GetAckFrame`, both policies emit one frame covering all 15. The witness passes under the race detector. It demonstrates why ACK decision threshold and emitted ACK frequency must remain separate concepts.

The residual is large enough to explain most of the raw turnaround, but this instrumentation cannot assign it to sender TX queueing, receiver kernel/NIC service, reverse-path delivery, sender RX service, interrupt moderation, or physical network behavior. No one-way delay or particular NIC defect is established. The unsupported coalescing query and inactive EEE state are retained null observations, not causes.

## Ranked hypotheses and disposition

The ranking was recorded before native timing capture: actual ACK turnaround; sender receive/feedback scheduling; local send service; then deliberate window and packet granularity.

1. **Long actual turnaround is supported.** Replacing minimum-RTT arithmetic by measured residence accounts for the dominant factor. The receiver's ACK decision delay is a small part of it. Threshold-one → ordinary restoration leaves the low-rate cycle and roughly quarter-millisecond residence present.
2. **A large sender post-receive scheduling stall is not supported here.** ACK processing is several microseconds, and eligible feedback reaches the next sending opportunity in well under a microsecond at the median. Earlier kernel/socket-read delay remains in the residual.
3. **A large connection-to-worker local queue is not supported here.** Sampled handoff-to-write-start medians are roughly 1.4–1.7 µs, with zero pending bytes at steady feedback observations. These samples do not measure kernel/device queues.
4. **Granularity and the deliberate cap contribute but do not explain a threefold gap alone.** Flight occupies about 90–95% of the target, while the actual feedback cycle is roughly three times the minimum in most measured intervals. Pacing credit is plentiful.

**Keep the accepted ProbeRTT and ordinary ACK policies.** No targeted implementation correction or production regression test is selected. [ADR 0009](../../adr/0009-opt-in-bbrv3.md) and the [accepted BBRv3 design](../../designs/bbrv3.md) retain their half-BDP target, saved cap and time-plus-round hold. A separately proposed shallower probe would still need evidence about queue draining, changing RTT and Reno coexistence; the present single-flow case without a controlled queue, RTT change or Reno competitor cannot supply it. This does not restart [ACK Frequency issue #645](https://github.com/the-sarge/quic-go-fast/issues/645), [the asymmetric-link investigation (#619)](https://github.com/the-sarge/quic-go-fast/issues/619), or a broad qualification campaign, and it leaves [wiremux's follow-up issue #2234](https://github.com/GridSwarm/wiremux/issues/2234) and its requalification hold unchanged.

**Recommendation:** if the remaining native cost is pursued, capture correlated UDP identities with kernel TX/RX timestamps at both endpoints on this same build and workload, alongside the retained userspace boundaries. Partition each endpoint locally and preserve clock provenance. That is the smallest justified next experiment; selecting a larger window or blaming a specific NIC from this residual is not supported.

## Host accounting, validation and custody

The existing exclusive-consumer host-admission operator, provider/config identity checks, independent lease witnesses, owned staging, worker-absence checks, remote archive verification and release were reused. Three completed operator executions contain the four traffic attempts; each released both hosts and removed its own remote staging after verified custody. The additional canceled operator never acquired either host and confirmed its owner leases absent. No shared host settings, other worktrees, or earlier capture bytes were changed.

Host load was not isolated. Mac performance-core means before/after were 10.6%/6.3% for the exact baseline, 10.5%/12.6% for the first trace, and 12.2%/5.5% around the two-arm ACK/restoration batch. Efficiency-core means were respectively 38.2%/47.9%, 74.4%/82.1%, and 61.0%/97.8%. These are one-second boundary snapshots; the high efficiency-core activity is not evidence of saturated performance cores during the probes. Linux per-core tick snapshots, process/queue samples and network state remain in the operator evidence. None proves the hosts were uncontended throughout measurement.

The observer stores fixed 16-int64 records in a bounded two-million-record buffer, samples packet/ACK identities by a deterministic hash, records all ProbeRTT feedback and sending opportunities, and writes after connection teardown. Each endpoint has exactly one recorded QUIC connection start in the native runs. Model values are read on the connection owner; local credit and the shared recorder use their locks. Every native trace reports zero dropped observations and successful flush. Clock anchors connect each endpoint's own measurement UTC to its monotonic trace; anchor drift across a run is below 1 µs on Linux and about 250 µs on Darwin. This bounds interpretation of the scheduled-slot alignment at that scale and does not synchronize endpoints.

Retained validation includes the promoted native Wire/Datagram fixture under `-race`, ProbeRTT tests under `-race`, and the ACK queue/emission witness under `-race`; focused ACK and send-queue tests also passed. The final source overlay is replayed through `prepare.py` and the promoted native fixture again. An initial installer string-count assertion and its consequent incomplete-build error occurred before any traffic; the installer was corrected and both platform builds succeeded before launch. Active diagnostic production changes and the temporary test are removed; [cleanup.json](cleanup.json) records the clean historical worktree identity.

The [observation manifest](observations/manifest.json) hashes the published compressed numeric records, per-slot send ledgers, controller results, logs and selected custody/host receipts. `baseline1.json`, `trace1.json`, `ack1.json` and `restored-trace.json` are derived analysis outputs. They retain adverse and null observations, the warmup-crossing interval, full counters, per-probe bounds, matched durations and scheduled-slot cohorts. Source overlays, toolchain/build flags and exact binary hashes are alongside them. Embedded native version strings describe the historical base; the overlay manifests and binary SHA-256 identify the actual diagnostic executables.

The complete operator roots and frozen inputs are additionally retained in `/Users/josh/quic-go-fast-1210-investigation/native-captures.tar.gz`, SHA-256 `582a380beed69f16f9b9da01e5d66c18390b4370fb9b38fb0c234509d2760c72`, with 630 verified files. [full-capture-custody.json](full-capture-custody.json) records its size and verification. This local archive is on the same machine and is not independent disaster recovery. The published numeric observation bundles and source snapshots support the offline replay independently of that local archive. The evidence branch ignores only its owned `.cache/1210/` working-capture directory; the source worktree is clean at the historical base after final replay validation.

## Replay

Offline replay reads the retained capture bytes and extracts temporary files outside the evidence tree:

```sh
python3 analyze.py observations/trace1 /absolute/path/to/new-trace1-analysis.json
python3 analyze.py observations/ack1 /absolute/path/to/new-ack1-analysis.json
python3 analyze.py observations/restored-trace /absolute/path/to/new-restored-analysis.json
```

For source replay, first create a new dedicated feature worktree at `bc4915ea0de0c238ecfdc51d827f7da67639a789` under `/Volumes/worktrees/quic-go-fast/`. From this evidence directory, run the following with that clean worktree path. `prepare.py` checks the path, branch, HEAD, cleanliness and source hashes before installing temporary diagnostic files. `build.py` requires Go 1.27.3, uses the retained harness and original relative module replacement, and checks the rebuilt binaries against their retained hashes. It launches no traffic.

```sh
python3 prepare.py /Volumes/worktrees/quic-go-fast/replay-1210 --source source-ablation
python3 build.py /Volumes/worktrees/quic-go-fast/replay-1210 --source source-ablation
```

Select `source-trace1` for the first observer version instead. The source files use `.go.txt` suffixes in this report so they cannot become active production instrumentation. The retained operator inputs under `operations/` describe the exact native commands and host coordination; a new execution needs fresh owned capture/staging paths and a newly observed provider identity, not reuse of a frozen run root. These are diagnostic replay mechanics, not authorization for unattended qualification or a production policy change.
