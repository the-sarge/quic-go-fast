# ProbeRTT triage evidence — issue #1210

**Disposition: enhancement + needs-triage; no production policy change.** This record supports [ProbeRTT dips well below what its window should carry for paced senders (#1210)](https://github.com/the-sarge/quic-go-fast/issues/1210). The owner authorized focused verification on November 5, 2026. The inspected and exercised base is `466198e5b1a7b462cfc271ce1beb6b66299143be`, using Go 1.27.3 on Darwin arm64. These are local diagnostics and an offline reading of existing evidence, not qualification or a rerun of the original Ethernet/Thunderbolt workloads.

## Finding

The low fraction of `window / minimum RTT` is reproducible with ordinary ACKs on native loopback. The minimum RTT is substantially shorter than the actual registration-to-ACK-receive samples during ProbeRTT. With a small flight window, those longer feedback cycles and packet granularity matter. An ACK-every-packet ablation does not remove the gap. The evidence does not establish a BBR implementation defect, a benefit from changing ACK policy, or an acceptable replacement ProbeRTT target.

The accepted half-BDP target, four-packet floor, saved cap, and hold exceeding 200 ms plus a completed round remain governed by [ADR 0009](../../adr/0009-opt-in-bbrv3.md) and the [BBRv3 design](../../designs/bbrv3.md). BBR's raw RTT samples deliberately retain peer ACK delay and local residence. A minimum over those samples is still a minimum, not the contemporaneous feedback cycle. Enlarging the target would revise the accepted policy and would need evidence of its queue-draining, changing-RTT and coexistence effects.

## Native local reproduction

The disposable test creates real client/server UDP sockets on localhost and sends 1,099-byte application DATAGRAM messages at a scheduled 1 Gbps for 15 seconds, skipping slots more than 1 ms late. It uses a yielding generator, GOMAXPROCS 4, BBRv3 on the data sender, Reno on the ACK-only peer, ordinary ACKs unless explicitly overridden, a fixed 1,280-byte maximum packet size, and disabled PMTU discovery. It adds a synchronous observer to BBR feedback. The observer records existing values and changes no controller decisions. The machine was not isolated, the observer has cost, and this is neither a hardware-path comparison nor a performance acceptance test.

The table reports transport bytes acknowledged on RTT-eligible feedback, divided by elapsed ACK-receive time within each observed ProbeRTT interval. It is not receiver application goodput and is not the issue's scheduled-slot attempted rate. All windows below were 5,120 bytes. The rate calculation excludes the first observed ACK to avoid crediting bytes from before the measured interval. RTT columns are medians of eligible per-ACK observations, not byte-weighted residence estimates; dividing by either median is not an exact throughput model.

| Execution / policy | Observed interval | Acknowledged transport rate | Model minimum RTT | Actual raw RTT | Window / model minimum |
| --- | ---: | ---: | ---: | ---: | ---: |
| Baseline, ordinary ACKs | 200.08 ms | 562.9 Mbps | 21.792 µs | 51.250 µs | 1,879.6 Mbps |
| ACK every packet, first probe | 200.13 ms | 594.3 Mbps | 21.583 µs | 46.333 µs | 1,897.8 Mbps |
| ACK every packet, second probe | 156.38 ms, cut short by test end | 583.3 Mbps | 21.042 µs | 48.792 µs | 1,946.6 Mbps |
| Restored ordinary ACKs | 199.91 ms | 553.9 Mbps | 23.167 µs | 47.688 µs | 1,768.1 Mbps |
| Ordinary ACK accounting validation, first probe | 200.03 ms | 634.0 Mbps | 21.125 µs | 41.541 µs | 1,939.0 Mbps |
| Ordinary ACK accounting validation, second probe | 200.03 ms | 697.2 Mbps | 21.250 µs | 38.708 µs | 1,927.5 Mbps |

The full ordinary intervals carry about 30–36% of window / model minimum. The actual raw RTT medians are roughly 1.8–2.4 times the model minimum. This accounts for an important part of the discrepancy but does not partition the remaining cost among ACK grouping, sender/receiver scheduling, packetization and local service. In these observations, median pre-ACK flight is 4,500 bytes and median pending local bytes is zero. These are ACK-event samples, not time-weighted occupancy or proof that local work never delays a packet.

The ACK-every-packet arm is an unsupported diagnostic override, set before connection creation. Its modest increase against the adjacent ordinary arms is smaller than the variation in the subsequent ordinary-ACK validation. No reliable improvement is established. The latter validation also counted acknowledgments excluded for lack of an eligible RTT: **zero acknowledged bytes were omitted** in that run. The preceding runs did not retain that counter and use the explicitly labeled RTT-eligible rate definition above.

An initial seven-second native attempt using `time.Sleep` in the generator did not enter ProbeRTT and is excluded from the mechanism evidence; its failed `must enter ProbeRTT` check is retained in `results/native.log`. The successful native attempts use the yielding generator in the retained source. The second ACK-every-packet interval ended with the harness and is explicitly partial.

## Simulated-network control

The companion fixture uses real QUIC connections over `testutils/simnet`, 100 µs fixed RTT, virtual time, the same scheduled 1 Gbps / 1,099-byte workload, ordinary ACKs, and the accepted ProbeRTT logic. It naturally enters ProbeRTT around five seconds. This removes real socket, CPU and timer-service delays while exercising real packing, recovery, ACK tracking and bounded Datagram admission.

The initial observation-only runs read roughly 1,000 Mbps outside ProbeRTT and 440 Mbps inside, with a 6,603-byte cap. A hypothetical 6,603 bytes per 100 µs gives 528.24 Mbps, while five 1,099-byte useful payloads per 100 µs give 439.6 Mbps. Whole packets, transport overhead and unused window space explain this ideal-path scale. ACK every packet left the observed rate essentially unchanged. This control reproduces the intentional reduced-window phase, but not the native threefold gap from the optimistic arithmetic.

The first fixture polled private state from the test goroutine. A subsequent race run rejected those observation accesses; this was a diagnostic-fixture defect, not a reported production race. The corrected fixture takes snapshots on the connection owner, protects them with a mutex and uses an atomic receive counter. Its retained validation and ordinary/every-packet reruns are the authoritative simulated results. A separate fixture adapter moves the monotonic origin into the virtual-time bubble: otherwise synctest's year-2000 clock precedes the process origin and BBR rejects non-positive timestamps. Neither adapter is a product change.

The corrected ordinary and every-packet runs pass at **439.864 and 439.908 Mbps**, respectively, over 2,001 virtual 100 µs samples in ProbeRTT, with the same 6,603-byte window. The corrected ordinary fixture also passes `-race` (439.820 Mbps). The small bin-boundary variation does not change the finding. Its state columns represent the latest connection-owned send-opportunity snapshot; pending bytes can retire between that snapshot and the sampler. Its flight column is calculated from window minus nonnegative allowance and therefore clips flight above the window during initial draining. Native observations use the actual pre-ACK recovery flight instead.

## Original native capture, read offline

The existing [native ProbeRTT report for wiremux issue #1460](https://github.com/GridSwarm/wiremux/blob/01ba7d6daf554cf75beed43cbefb466f55f98e8b/docs/research/1460-residual-qualification/native-probertt-v1/README.md) retains the original on/off/on evidence on fork `bc4915ea`. Its archive SHA-256 was independently checked against the published `0c815b1f413ead6c43ca41c049aa35c79b05750538adb8b63161556a6718742e`. No traffic or remote host occupation was needed to read it. `results/original-native-samples.json` preserves the three sampled ProbeRTT states with their exact archive members and line numbers.

Those states have windows of 19,229, 11,030 and 19,165 bytes; remaining flight allowance of 1,213, 896 and 1,149 bytes; and roughly 61–65 KB of pacing credit. Their low remaining flight allowance and abundant pacing credit support the congestion-window gate at those sampled instants. The samples do not retain corresponding per-ACK raw RTT or ACK-delay timelines, so they cannot establish the detailed native cause or prove which local component adds delay. The loopback finding must not be presented as attribution of that original path, native Wire, the jumbo workload or a real GridCast transfer.

## Replay and custody

`results/summary.json` is generated from the compressed native observations by `python3 analyze.py`. A gap over 10 ms separates observed probe groups; the supplied records show distinct approximately 200 ms intervals except the disclosed final partial interval. This is an offline grouping convention, not a new product criterion. Raw observations, logs, diagnostic source and the original-capture extract remain available beside this report.

Create a separate clean feature worktree at the base SHA, under `/Volumes/worktrees/quic-go-fast/`, then run `python3 prepare.py /absolute/path/to/that/worktree`. It installs the corrected disposable fixture and instrumentation only after checking the base, branch and cleanliness. `--source source-v1` installs the source captured for the native observations and initial simulation, including the superseded polling sampler. Do not use that initial simulation sampler as race-clean evidence. The `source` directory holds the corrected sampler; the native observer is unchanged except for the additional omitted-byte accounting recorded above.

From the prepared worktree:

```sh
go test . -run '^TestTriage1210ScheduledProbe$' -count=1 -v -timeout=3m
TRIAGE1210_ACK=1 go test . -run '^TestTriage1210ScheduledProbe$' -count=1 -v -timeout=3m
go test -race . -run '^TestTriage1210ScheduledProbe$' -count=1 -v -timeout=3m
GOMAXPROCS=4 TRIAGE1210_OUTPUT=/absolute/path/to/new-observations.json go test . -run '^TestTriage1210NativeProbe$' -count=1 -v -timeout=45s
```

Set `TRIAGE1210_ACK=1` for the unsupported native ACK ablation. Output files must be new caller-chosen locations because the diagnostic writer replaces its requested output. Run only the named diagnostic: its process-level clock/observer adapters are not intended for the repository's full test suite. The retained artifacts contain no active production instrumentation; the test inputs use `.go.txt` names.

## Triage outcome

Keep issue #1210 open as `enhancement` + `needs-triage`. Existing implementation and accepted policy were checked; no already-implemented remedy or prior `.out-of-scope/` rejection was found. This verification supplies a reproducible mechanism and narrows the ACK hypothesis, but it does not define a new performance contract or select a fix. It does not advance [ACK Frequency issue #645](https://github.com/the-sarge/quic-go-fast/issues/645), launch [the separate asymmetric-uplink investigation (#619)](https://github.com/the-sarge/quic-go-fast/issues/619), restart qualification, change wiremux's 98% criterion, or remove its existing hold.

Recommendation: characterize actual ACK turnaround and send-opportunity residence on the original affected path before choosing a remedy; any proposal to make ProbeRTT shallower should explicitly retain or re-decide its queue-draining, changing-RTT and coexistence obligations.
