# ProbeRTT interface and Linux receive investigation — issue #1210

**Disposition: retain the accepted ProbeRTT and ordinary ACK policies. The investigation is complete without a justified production correction.** The original native slowdown is principally a reduced flight window operating against an actual feedback cycle much longer than the model minimum. The added measurements find roughly 140–149 µs outside the macOS software interface taps and Linux software TX/RX boundaries. They do not identify a particular NIC, driver, direction, or physical-network defect. Observed preceding-IRQ-to-poll intervals are small for the correlated ProbeRTT packets.

This completes the remaining software-boundary work in the [accepted investigation for issue #1210](https://github.com/the-sarge/quic-go-fast/issues/1210#issuecomment-6651545107), following the [userspace investigation](../2026-11-05-probertt-investigation/README.md) and [kernel timestamp investigation](../2026-11-05-probertt-kernel-timestamps/README.md). It supplies a bounded explanation and disposition, not a claim that every physical-path component is known. No controller, ACK policy, NIC setting, qualification criterion, or requalification hold changed. Issue #1210 remains open; completing this investigation does not silently select or ship a policy change.

## Original case, source and observation changes

All five native attempts use the original minimax (`192.168.1.50`, `enp97s0`) → mbp128 (`192.168.1.110`, `en8`) Ethernet path, 10 Gb/s link, MTU 1500, one native Wire connection, BBRv3 with ordinary conservative ACKs on both endpoints, 1,099-byte application DATAGRAM payloads scheduled at 1 Gb/s, GOMAXPROCS 4, and 10/60/5 seconds of warmup/measurement/drain. The sender retains its existing 1 ms late-slot skip behavior. Scheduled identities, admitted application bytes, received application goodput, and acknowledged QUIC bytes remain distinct metrics.

The historical fork base is `bc4915ea0de0c238ecfdc51d827f7da67639a789`, wiremux product `8282a84652387d3e2f644dfac1310da479fbe6b4`, and harness `b88eee5b23df2476dbe98717308e287c35d6efa0`, with the previously frozen native overlays and narrow diagnostic timestamp-metadata allowance. Go 1.27.3 builds the Linux binary `e83901cd1cca9d4dfe91ec50e47e5ba157517127fad46bd673c8f061c3042edd` and Darwin binary `5c27edcbe89ea08bb91b20d795f67420b0c2c234a22ba935f3f6b08a97d14d19`. Independent source replay reproduced both hashes. These are historical-build observations, not current-main qualification.

The diagnostic identity now reserves its sign bit for nonempty UDP payloads below 256 bytes, retaining all such packets and approximately 1/16 larger payloads. The remaining bits fingerprint payload length and the first 32 protected bytes. This increases the number of small ACK carriers available for correlation; it does not assert that every short packet is an ACK. The record capacity is eight million 128-byte rows. All arms use these same binaries and the same kernel observer settings. Actual authenticated packet-number/ACK links establish each cycle; ambiguous joins are rejected. Linux timestamp attribution still requires the first GSO segment.

A nonpromiscuous libpcap collector reads only IPv4 UDP between the two workload addresses, using en8, Ethernet framing, 128-byte snapshots, buffered output and host microsecond timestamps. It opens/configures BPF through the native administrator mechanism, immediately drops to user josh, and uses bounded nonblocking capture. The initial tcpdump activation witness opened and dropped privileges successfully but failed to stop gracefully while idle and was force-killed; no native workload ran in that witness. The replacement collector passed its idle start/stop witness and every subsequent capture stopped normally.

The later Linux observer records exact protected packet identities at `napi_gro_receive_entry`, actual `rtl8169_poll` entry, and the preceding interface IRQ. It attaches only during the selected run, with post-attachment readiness, bounded lifetime, explicit stop, and process-identity/absence verification. It does not modify the driver or its configuration.

## Native attempts and observer controls

Five attempts completed their 75-second phases, totaling 375 seconds. Every admitted application record eventually arrived; none was rejected, duplicated, invalid, or missing after admission. Three canceled application-send attempts in the first interface capture were deadline failures. Late records remain in the accounting. Every run passed the existing boundary-alignment witness; none is presented as a qualification pass.

| Execution / arm | Additional observation | Generator misses | Canceled / late | Measurement application goodput | Complete measured probes |
| --- | --- | ---: | ---: | ---: | ---: |
| `q1210-interface1 / before-interface` | Kernel/identity only | 49,018 | 0 / 2 | 992.817 Mb/s | 6 |
| `q1210-interface1 / interface` | Mac interface | 54,993 | 3 / 0 | 991.941 Mb/s | 6 |
| `q1210-interface1 / after-interface` | Kernel/identity only | 75,729 | 0 / 2 | 988.903 Mb/s | 7 |
| `q1210-napi1 / napi` | Mac interface plus Linux IRQ/NAPI | 54,851 | 0 / 3 | 991.962 Mb/s | 6 |
| `q1210-napi1 / restored-interface` | Mac interface, Linux trace removed | 46,655 | 0 / 1 | 993.163 Mb/s | 5 |

The first controls have steady ProbeRTT byte-weighted raw ACK residence of 225.3–227.7 µs before and 226.6–252.6 µs after. Five first-capture probes have 227.6–229.4 µs residence; one adverse probe reaches 454.1 µs and 371.0 Mb/s acknowledged transport rate. The Linux-observed run has 228.3–229.9 µs residence, and its restored interface-only run has 227.3–230.1 µs. Model-minimum medians in the three interface captures are approximately 84.8–99.3 µs. The characteristic low-rate cycle persists. Different probe counts, window estimates, host activity and an adverse local-service episode prevent interpreting whole-minute differences as a speedup or proof of zero observer overhead.

## Exact feedback-cycle partition

Each cycle links the sender's authenticated largest-acked packet, the receiver's copy of that packet, the emitted ACK carrier, its sender-side receipt, both Mac BPF records, and valid Linux software TX/RX records. Selection uses complete measurement ProbeRTT intervals, trimming 2 ms at each end as in the earlier analysis. No cross-host clock offset is subtracted. Mac turnaround is the difference between two Mac BPF timestamps; the other terms are Linux-local differences.

The following are **means within each run's 1,126-byte carrier cohort**, the ordinary application-size QUIC packet on this workload. Every row is an additive partition of the same matched cycles. Small control/ACK carriers remain in the detailed results but are excluded from this table.

| Capture | Complete application-size cycles | Raw RTT | Sender logical-send → software TX | Mac BPF RX → BPF TX | Linux software RX → protocol receive | Remaining outside those boundaries |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Interface | 159 | 216.485 µs | 16.412 µs | 52.723 µs | 4.390 µs | 142.961 µs |
| Linux IRQ/NAPI | 115 | 218.048 µs | 17.939 µs | 56.209 µs | 4.263 µs | 139.637 µs |
| Restored interface | 98 | 218.160 µs | 13.560 µs | 52.306 µs | 3.790 µs | 148.504 µs |

These newest-acked-packet cohorts differ from the byte-weighted residence across all acknowledged packets above. Their medians must not be added as an exact partition. Every individual retained complete cycle balances with zero integer-nanosecond arithmetic error.

Sampling remains material. There are 528 complete cycles across the three runs, including 372 application-size carriers. Of all complete cycles, 527 are unsegmented and one is the first segment of a 12-segment submission. Application-size counts by probe are `[1,1,51,48,1,57]`, `[0,50,5,5,55,0]`, and `[3,46,49,0,0]`; these are not evenly sampled phase or GSO populations. Broader turnaround/partial cohorts and every exclusion remain in the JSON. The results establish the persistent scale on the observed paths, not a confidence interval for every packet.

For the first capture's full complete cohort, Mac UDP-delivery-to-ACK-write and the combined pre-UDP/post-write edges account for the BPF turnaround. The combined edges have median 27.145 µs; optional mapped subterms have medians 19.563 µs before UDP delivery and 7.125 µs after write entry. Those separate medians are not additive. Darwin's `time.Now()` reads wall time and monotonic time sequentially, so a scheduling gap can bias the individual subterms in opposite directions. The combined edges and primary BPF turnaround cancel that mapping offset. Maximum observed neighboring offset movement is 1 µs, which is a sensitivity observation, not a certified clock-error bound. The adverse first-capture probe includes long receiver service tails; the persistent typical remainder is not explained by those tails.

BPF is a software tap, not physical-wire timing. Linux software TX precedes hardware transmission, and software RX follows driver delivery. The remainder therefore includes unmeasured post-TX/pre-RX driver and device stages in both directions, plus the network. It cannot identify one-way latency or blame either NIC. See [Linux timestamping boundaries](https://docs.kernel.org/networking/timestamping.html), [XNU BPF implementation](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/net/bpf.c), and [Apple's transmit-tap contract](https://developer.apple.com/de/documentation/kernel/1589576-bpf_tap_out). Public kernel source explains boundaries; it is not asserted byte-identical to the running kernels.

## Linux receive-service hypothesis

All 555 matched ProbeRTT turnaround cycles in the Linux-observed run have a unique, ordered IRQ/poll/GRO context on CPU 2. Of these, 492 acknowledge application-size carriers. The entire 589,269-row selected trace agrees with its exit counter, and has no malformed records, duplicate wanted identities, missing readiness/end marker, or reported lost-event/helper warning.

| Context interval, 555 matched ACKs | Median | 95th percentile | Maximum |
| --- | ---: | ---: | ---: |
| Preceding IRQ → poll entry | 2.334 µs | 2.365 µs | 5.731 µs |
| Poll entry → packet GRO entry | 0.340 µs | 0.380 µs | 1.132 µs |
| Preceding IRQ → packet GRO entry | 2.675 µs | 2.805 µs | 6.131 µs |

The preceding interrupt may concern TX completion and is not a measured packet-arrival timestamp. These contextual intervals are **not subtracted again from the RTT remainder**. They do not support a large observed interrupt-to-poll scheduling delay for this cohort; they cannot exclude waiting inside the NIC before an interrupt or other unmeasured stages.

A final read-only inventory identifies the Linux RTL8127/r8169 and the Mac AQC113/AppleEthernetAquantiaAqtion adapter. Linux reports a 20 µs NAPI repoll timeout, defer count 1, and zero global busy-poll settings. Whole-run IRQ/zero-work counts and longer whole-run repoll intervals do not establish a ProbeRTT cause: the exact ProbeRTT cohort above is different. No knob experiment is warranted by these observations. The [host-context receipt](host-context/README.md) preserves hardware, settings, source qualifications and the nontrivial restore semantics of those settings.

## Final disposition and limits

The earlier correlated window/flight/emission analysis establishes that the reduced window is occupied about 90–95% and pacing credit is available, while the actual feedback cycle is much longer than the minimum used in simplistic window/minimum arithmetic. ACK-threshold-one diagnostics did not remove the cycle. This follow-up measures the missing Mac software edges and finds short preceding-IRQ/poll intervals for the relevant packets. Together they support retaining current behavior rather than inventing an implementation correction or selecting a larger target.

The remaining roughly 140–149 µs application-size mean is deliberately unassigned within the driver/device/network path. Available timestamps do not expose physical departure/arrival at both endpoints. That visibility limit is not evidence that the remainder is propagation or an interrupt-moderation defect. A different direction or interface would also change the hardware path and would not, by itself, identify the missing component. The accepted investigation's requirement to quantify the unexplained portion is met without claiming it has vanished.

No production regression test is selected because no production defect/correction was established. The observer and analysis have meaningful deterministic tests instead. Any separately proposed shallower ProbeRTT policy still needs queue-draining, changing-RTT and coexistence evidence under the accepted design. This single-flow historical-path investigation supplies none of those policy qualifications.

## Validation, custody and replay

The retained operator reuses exclusive host admission, fresh provider/config identities, independent lease witnesses, source/binary hashes, owned staging, exact process absence, archive verification and guarded cleanup. Both executions released their leases and removed owned remote staging. Mac performance-core mean activity before/after was 5.11%/5.06% in the first execution and 9.82%/19.50% in the second; efficiency-core means were 36.18%/26.44% and 43.82%/42.74%. These are one-second boundary snapshots, not proof of continuously isolated hosts. No conclusion equates efficiency-core activity with performance-core saturation.

All native diagnostic traces report zero dropped rows. The three pcap files contain exactly 8,499,258, 8,496,937 and 8,522,075 records, equal to their collector counts, with clean EOF, no packet-format rejection, no timestamp regression and no ambiguous retained identity. All report zero BPF buffer drops. `ps_recv` can count pre-filter packets, and a zero interface-drop field may be unsupported; neither is a guarantee of zero physical NIC loss. Exact parsed counts and complete records also validate the collector's saved output. The collector's own CPU cost is not precisely estimated.

Validation includes the [native Wire fixture under the race detector](validation/checks.json); dense-identity and real prepare-TX/GSO selection tests; admitted Linux socket/metadata witnesses; 14 interface-analysis and 11 NAPI-analysis tests; independent exact-binary source replay; and published-byte replay. All active source instrumentation is removed from the owned source/replay worktrees. Inert source snapshots, commands, validation receipts, adverse outcomes and cleanup receipts are retained here.

The complete original capture roots and frozen launch inputs are archived at `/Users/josh/quic-go-fast-1210-interface-timing/native-captures.tar.gz`; [full-capture-custody.json](full-capture-custody.json) gives the verified hash and member inventory. This is same-machine custody, not independent disaster recovery. Published observations retain complete numeric traces and exact-byte pcap subsets containing every wanted identity, including any duplicates. The subsets omit unrelated packet identities, so replayed pcap inventory counts differ from the full-file counts; the correlated identities, timestamps, exclusions and timing results are identical.

From this report directory, for example:

```sh
python3 analyze-interface.py observations/q1210-interface1/interface /absolute/new-interface.json --arm interface --pcap observations/q1210-interface1/interface/interface-retained.pcap.gz
python3 analyze-interface.py observations/q1210-napi1/napi /absolute/new-napi.json --arm napi --pcap observations/q1210-napi1/napi/napi-retained.pcap.gz
python3 analyze-napi.py /absolute/new-napi.json observations/q1210-napi1/napi/linux-napi/stdout.log.gz /absolute/new-napi-context.json --stderr observations/q1210-napi1/napi/linux-napi/stderr.log
python3 analyze-interface-test.py
python3 analyze-napi-test.py
```

For source replay, create a fresh dedicated feature worktree under `/Volumes/worktrees/quic-go-fast/` at `bc4915ea0de0c238ecfdc51d827f7da67639a789`, then run `prepare.py WORKTREE` and `build.py WORKTREE`. The helpers verify source hashes and both expected binaries without generating traffic. A live rerun requires fresh owned paths and the existing host admission procedure; frozen execution roots are not reusable launch directories.
