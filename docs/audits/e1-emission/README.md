# E1 packet-emission feasibility

**Disposition: inconclusive; no runtime adoption.** The bounded normal/GSO prototype preserves the characterized behavior and adds no focused steady-state allocation, but the initial full campaign does not establish the required latency preservation in P2 and P5, and the stream-churn benchmark misses its time margin. This PR retains ordinary characterization, opt-in evidence and an inert patch only. E3–E6 remain blocked pending a scoped re-handoff; E2 remains independently ready. The [normative E1 contract](../../adr/2026-09-07-packet-emission-plan.md#e1--establish-packet-emission-feasibility) governs; this receipt does not amend its performance margins.

## Frozen source identity

- Production parent: `effb08627fdb5210087a52c24eef79c6de9e0534`.
- Characterization/base commit: `e90617366674535bcefa8f90a2e92b153be1342b`; production sources are unchanged from the parent.
- Experimental candidate: `edea78eabf72a0ac2dacd0d55dd68a8f2cabdc3a`.
- [Inert prototype patch](prototype.patch), relative to the characterization/base commit; SHA-256 `e70fd7571063beaa2862e4ff82367ef2157dafb4025b432077da1f935d69d752`.

Normal builds and test enumeration do not apply this patch. To inspect or run the prototype, create a dedicated feature branch/worktree at the exact characterization/base commit, check `git apply --check /absolute/path/to/prototype.patch`, and then apply it explicitly. Never apply it to a worktree with unrelated changes. The prototype is disposable evidence, not production adoption or a maintained measurement tool.

## Concrete source, field and hook correspondence

| Baseline source or state | Experimental owner or binding | Preservation boundary |
| --- | --- | --- |
| `Conn.sendPacketsWithoutGSO` and `Conn.sendPacketsWithGSO` | `packetEmission.withoutGSO` / `withGSO` | Established ordinary output only; same packing, per-packet recovery queries, ECN and GSO segment boundaries. |
| `Conn.appendOneShortHeaderPacket` | `packetEmission.appendPacket` | Real packer constructs bytes; logging, activity timestamp, recovery registration and connection-ID notification precede queue submission. |
| `Conn.packer`, `sentPacketHandler`, `sendQueue` | Typed pointers to those authoritative interface slots | No copied mutable recovery/packer/queue state. Existing test substitutions and path replacement address the same slots. |
| Both production constructors | `bindPacketEmission`, once after packer initialization | One embedded concrete emission value, with one narrow policy interface. No new worker, timer, lock or completion channel. |
| Packetization/version | `policy.maxPacketSize`, frozen connection version | MTU/path policy stays connection-owned. Existing wire codecs and protection own representations. |
| `Conn.logShortHeaderPacket` | Same typed policy method | Existing qlog vocabulary and debug behavior; no event copy or per-packet closure introduced by extraction. |
| `firstAckElicitingPacketAfterIdleSentTime` | `policy.noteEmissionActivity` | Update at its original point before recovery registration. |
| `connIDManager.SentPacket` | `policy.noteEmissionRegistration` | One synchronous notification after each ordinary packet registration. |
| Receive-queue mutex and pending check | `policy.emissionReceivePending` | Connection retains the mutex and receive policy; yield between batches, preserving immediate-retry deadline. |
| Pacing deadline | Compact `emissionResult.deadline`, applied by `Conn.sendPackets` | Recovery still owns the pacing calculation. Zero deadline retains the caller's existing behavior. |
| Initial capacity check in `Conn.run` | Retained, plus an emission-entry check before destructive packing | The existing full-queue/handshake-feedback ordering is unchanged. No new reservation counter. |

The compact result records progress, a stop reason, a pacing/immediate-retry deadline and an error. Its stop reasons describe only this established-send prototype. Top-level handshake, congestion/ACK-only and PTO dispatch remains in `triggerSending`; the prototype is not evidence that later modes have migrated. The policy interface exposes exactly five synchronous operations and provides no unrestricted connection access inside the module. Focused allocation observations and the completed full-matrix comparison are recorded below.

The experimental patch also adds one expected `WouldBlock` call to the existing mock send-queue test, corresponding to the added entry guard. It retains that test and the real-queue characterization. It makes no resource-failure correction: existing partial-build cleanup, stopped-worker handling, probes, path replacement and retained close ownership remain outside this E1 experiment.

## Characterization domain

`connection_emission_test.go` uses the real packet packer, frame producers, sent-packet recovery and send queue. Protection is the existing transparent test sealer; output is decoded with the production short-header/frame parser at a controlled socket adapter. These are example-level observations of decoded internal behavior, not native encryption, kernel GSO or end-to-end throughput qualification.

| Case | Observation |
| --- | --- |
| Ordinary output | Two full-size packets and a final short packet preserve payloads and packet numbers in three writes. |
| GSO output | The same payloads and packet numbers form one batch with a 1200-byte segment size. |
| Pre-I/O registration | With the worker driven synchronously, recovery accepts each emitted packet's ACK at the socket boundary, before the write returns. |
| Full queue and resume, ordinary/GSO | A blocked socket worker plus eight queued entries prevents packing the next application DATAGRAM; worker progress wakes the real connection loop and sends it. |
| Receive fairness, ordinary/GSO | A pending receive yields after a short batch, preserves the next DATAGRAM and schedules an immediate return; clearing the pending receive permits that next batch. |

These are verification aids, not maintained product deliverables. This receipt and the source map are traceability metadata. No shipped runtime or required safety enforcement changes are present in the product branch. Contract closure is not triggered for these aids; no mutation program, universal parser claim or new lifecycle guarantee is inferred from them.

## Capture counters

For DATAGRAM cells, `offered` counts the fixed-rate generator's scheduled 1071-byte messages. A bounded 32-entry fixture queue feeds one sender. `admitted` increments only after `SendDatagram` returns successfully; that call copies the payload into QUIC and can block on QUIC's own full send queue. `ingress_drop` counts fixture-queue overflow before the call. `delivered` counts unique valid measured-phase messages received through the drain deadline. The ledger is `offered = admitted + ingress_drop`; `post_admission_drop = admitted - delivered`. These counters locate observed loss but do not independently distinguish generator scheduling from transport backpressure as its cause.

For reliable-stream cells, counters use bytes, admission is the byte count accepted by stream writes, and the admitted/delivered ledger must close. Goodput uses delivered application payload divided by the 60-second measurement window. CPU and allocated bytes include both endpoint processes and the fixture, measured from the measurement start through drain completion. Probe p50/p99 summarize successful round trips; missed schedules and failures remain separately counted across all 6000 scheduled probes.

## Validation of the frozen sources

Local environment: macOS arm64, native `go1.27.0 darwin/arm64`. The characterization/base commit passed `go test . -run '^TestEmission' -count=1`, the same focused run with `-race`, `go test ./...`, and `go vet ./...`.

The experimental candidate passed `go test -race . -run '^TestEmission|^TestConnection(GSOBatch|SendQueue|ReceivePrioritization)' -count=1`, `go test ./...`, and `go vet ./...`. These checks validate the cited experimental sources. Final product-head local certification and hosted checks are recorded separately on [PR #33](https://github.com/the-sarge/quic-go-fast/pull/33).

An earlier experimental revision using method-value hooks failed `TestMITCorruptPackets/towards_the_client` in `integrationtests/self/mitm_test.go:218`: `clientTr.Dial` reached its one-second context deadline. The test randomly corrupts long- and short-header packets. Ten targeted repetitions on each of the unchanged base and that experimental revision passed; the original failure's cause remains unresolved. Do not discard the failed observation or treat the successful repetitions as evidence of a fix. No adjacent integration-test timeout change is included.

## Capture and qualification

The disposable endpoint is archived as [endpoint.go.source](endpoint.go.source). Copy it to an explicit `endpoint.go` file outside the repository and build that file from the frozen base/candidate worktree with `go build -o /absolute/path/to/binary /absolute/path/to/endpoint.go` to opt in. Normal package enumeration cannot select it. The capture used these exact bytes under the filename `endpoint.go`; its content hash is unchanged. Hosted lint run [34088690434](https://github.com/the-sarge/quic-go-fast/actions/runs/34088690434) identified the repository's prohibition on Go files tagged `ignore`, so the archive suffix preserves the frozen fixture while satisfying that packaging rule. No capture source or binary was rebuilt for this rename.

The initial campaign ran on `minimax` with native Go 1.27.1, from 05:56 to 08:04 UTC on 2026-09-07. The [manifest](campaign-01/manifest.json) was frozen before capture. Both endpoints use QUIC v1, qlog off and GSO enabled except P4. The two/four-processor budget is interpreted per endpoint: client physical cores 8–9 or 8–11, server cores 12–13 or 12–15, with corresponding SMT siblings idle. Other user, system and VM workloads were temporarily restricted to cores 0–7 and 16–23. The [host record](host.txt) captures topology, toolchain and the initial service identity; [restoration](campaign-01/restoration.json) confirms the initially unrestricted settings were restored, the reservation marker removed and the backup timer stopped after capture.

All 120 samples passed payload, probe-timeout, ledger, GSO-capability and host checks. The largest per-run average SMT-sibling busy fraction was 0.42125%; guest CPU was zero on measured cores and siblings, within the frozen 1% and 0.1% limits respectively. GSO capability qualifies the selected native send path; individual short DATAGRAM packets need not form multi-packet GSO batches. The ordinary characterization, focused three-packet GSO allocation check, Linux native GSO test and stream cells provide their separately stated evidence.

The [raw archive](campaign-01/raw.tar.gz) retains every sample's endpoint output, invocation and CPU observations plus the capture log and restoration receipt; SHA-256 `0a3f0b8a314eb1b82f0646eadf09fc96a783ac92ae63730d4a18eee40c4b5c89`. [Sample counters](campaign-01/samples.json) and [paired analysis](campaign-01/summary.json) are directly inspectable. To recompute, extract the archive into an empty directory and run `python3 analyze.py /path/to/extracted/campaign-01 --output /path/to/summary.json`. The analysis uses 20000 whole-pair bootstrap resamples, seed 20260907, geometric paired ratios and one-sided 95% percentile bounds. No packet is treated as an independent experimental unit.

| Cell | Goodput, base / candidate Gbps | Goodput lower bound | CPU/unit upper bound | Bytes/unit upper bound | Probe p99 upper bound | Missed/failed upper difference, percentage points | Gate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| P1: DATAGRAM 1 Gbps, 2 cores/endpoint | 0.729 / 0.720 | 0.9588 | 1.0251 | 1.0038 | 1.0245 | +0.0233 | Pass |
| P2: DATAGRAM 4 Gbps, 2 cores/endpoint | 2.925 / 2.931 | 0.9981 | 1.0021 | 1.0308 | 2.0948 | +3.1017 | Not established |
| P3: DATAGRAM 4 Gbps, 4 cores/endpoint | 3.719 / 3.709 | 0.9965 | 1.0047 | 1.0016 | 0.9922 | 0 | Pass |
| P4: P2 with GSO disabled | 3.035 / 3.035 | 0.9968 | 0.9985 | 0.9984 | 0.8731 | −0.9033 | Pass |
| P5: one continuous stream | 9.350 / 9.240 | 0.9857 | 1.0113 | 1.0007 | 1.0533 | 0 | Not established |
| P6: sixteen continuous streams | 9.795 / 9.693 | 0.9883 | 1.0068 | 0.9996 | 0.9838 | 0 | Pass |

Goodput values are geometric means of whole runs. Bounds are candidate/base ratios except the last numeric column. Passing requires goodput ≥0.95, CPU/allocated bytes/probe p99 ≤1.05 and missed/failed proportion difference ≤+0.1 percentage point. P2's geometric p99 ratio is 1.5059 and its mean missed-probe difference is +1.35 percentage points. P5's geometric p99 ratio is 1.0182, but the upper bound 1.053283 exceeds the unchanged 1.05 limit. These are failures to establish the accepted noninferiority claim, not a claim that every run or all workloads regress by those upper bounds. Four cores improve this DATAGRAM workload's absolute throughput, but that does not discharge the separately required two-core gates.

Linux focused allocation checks match base and prototype: 37 fixture-inclusive allocations per ordinary packet/batch and 104 per three-packet GSO batch. The embedded connection value grows from 1168 to 1216 bytes, a 48-byte setup-size increase; there is no separate module allocation or close-payload allocation change. The focused counts include transparent protection, mock-adapter and assertion overhead and do not claim native emission is allocation-free.

The initial full campaign is the only qualifying campaign. The replacement allowance is unused: no sample had disqualifying host contamination and no in-contract candidate revision was selected. Repeating unchanged valid samples to obtain a favorable bound would not be an accepted replacement reason. The earlier experimental integration-test timeout remains an unresolved observation; successful repetitions do not establish its cause or a fix. No experimental runtime is promoted by this evidence-only outcome.

## Existing benchmark comparison

The existing `integrationtests/self` benchmarks ran in ten adjacent alternating base/candidate pairs with `-test.benchtime=1s -test.benchmem`, native Go 1.27.1, GSO enabled and GOMAXPROCS 4. These existing benchmarks keep both endpoints in one process pinned to physical cores 8, 9, 12 and 13; their existing process shape is unchanged. They are separate from the full matrix's two-process, per-endpoint processor budget. The [manifest](bench-01/manifest.json) freezes source/binary hashes and the runner; [completion](bench-01/completion.json), [paired results](bench-01/summary.json), [host observations](bench-01/host.json) and [restoration](bench-01/restoration.json) retain the outcome.

| Existing benchmark | Geometric time ratio | One-sided 95% upper bound | Mean B/op, base / candidate | Mean allocs/op, base / candidate | Time gate |
| --- | ---: | ---: | ---: | ---: | --- |
| Handshake | 0.9990 | 1.0003 | 250630.8 / 250596.5 | 2081.4 / 2081.3 | Pass |
| Stream churn | 1.0393 | 1.1061 | 3220.9 / 3210.5 | 38.4 / 38.5 | Not established |
| Transfer 500 KiB | 1.0072 | 1.0088 | 371367.3 / 371471.7 | 4031.2 / 4031.2 | Pass |
| Transfer 50 MiB | 1.0053 | 1.0106 | 4807008.0 / 4784664.5 | 159461.3 / 159404.0 | Pass |

All twenty benchmark subprocesses completed successfully. Stream churn's central time ratio is 1.0393, but its upper bound 1.106054 exceeds 1.05, so the accepted time preservation is not established. The allocation columns are arithmetic means of the ten reported `-benchmem` values, not extra focused per-packet allocation claims. The [raw archive](bench-01/raw.tar.gz), SHA-256 `19ffcc6fdb2b1c617259091852f95ea2dd048d0f1b60e4e34916a675c2735643`, includes every log and the passive CPU monitor. Host statistics use the 144 full one-second intervals inside the timestamped capture, excluding post-restoration observations; guest CPU was zero and maximum average SMT-sibling busy time was 0.08271%. Recompute paired results with `python3 analyze.py /path/to/extracted/bench-01 --benchmarks --output /path/to/summary.json`.

## Preflight and diagnostic receipts

The [preflight archive](preflight-evidence.tar.gz) retains endpoint output, host activity, invocation manifests and receipts from the short setup checks, plus the Linux focused allocation logs. These short runs are excluded from all qualifying intervals. They established the frozen fixture before the initial full campaign; no replacement campaign has been used.

| Capture | Observation and disposition |
| --- | --- |
| `smoke-01` | Client exited successfully but the server reported an error during final control exchange. The fixture needed an explicit final acknowledgment before closing the connection; corrected before freezing. |
| `smoke-02` | Host qualification failed: the unrelated VM ran on measured cores and their SMT siblings. Temporary cgroup CPU isolation removed that overlap before the next setup check. |
| `smoke-03` | All six cells completed one short base/candidate pair with valid payload, probe and host receipts under CPU isolation. |
| `qlog-smoke` | The client timed out during setup because the fixture created a qlog trace without starting its processing loop. Added the required `trace.Run()` before freezing the endpoint. |
| `qlog-smoke-02` | One qlog-on P2 base/candidate pair completed successfully. This is diagnostic coverage only. |

Both failed and successful setup observations remain in the archive. The isolation launcher restored the initially unrestricted cgroup settings after each of these captures; the full campaign also restored its reservation on completion. The launcher later gained a separate `bench` mode for the existing benchmarks; the running full campaign still uses its original launcher bytes recorded by the host checkpoint.
