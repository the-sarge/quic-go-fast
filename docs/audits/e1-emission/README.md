# E1 packet-emission work in progress

E1 is incomplete. This checkpoint contains ordinary characterization and an inert prototype, not a positive feasibility disposition. The initial performance campaign is running on `minimax` using the [frozen manifest](campaign-01/manifest.json). The replacement campaign budget remains unused. E3 remains blocked. The [normative E1 contract](../../adr/2026-09-07-packet-emission-plan.md#e1--establish-packet-emission-feasibility) governs; this receipt does not amend it.

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

The compact result records progress, a stop reason, a pacing/immediate-retry deadline and an error. Its stop reasons describe only this established-send prototype. Top-level handshake, congestion/ACK-only and PTO dispatch remains in `triggerSending`; the prototype is not evidence that later modes have migrated. The policy interface exposes exactly five synchronous operations and provides no unrestricted connection access inside the module. Focused allocation observations are recorded below; the full performance comparison remains pending.

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

## Local validation checkpoint

Local environment: macOS arm64, native `go1.27.0 darwin/arm64`. The characterization/base commit passed `go test . -run '^TestEmission' -count=1`, the same focused run with `-race`, `go test ./...`, and `go vet ./...`.

The experimental candidate passed `go test -race . -run '^TestEmission|^TestConnection(GSOBatch|SendQueue|ReceivePrioritization)' -count=1`, `go test ./...`, and `go vet ./...`. These checks are local checkpoint evidence, not final exact-head certification or hosted verification.

An earlier experimental revision using method-value hooks failed `TestMITCorruptPackets/towards_the_client` in `integrationtests/self/mitm_test.go:218`: `clientTr.Dial` reached its one-second context deadline. The test randomly corrupts long- and short-header packets. Ten targeted repetitions on each of the unchanged base and that experimental revision passed; the original failure's cause remains unresolved. Do not discard the failed observation or treat the successful repetitions as evidence of a fix. No adjacent integration-test timeout change is included.

## Resume point

The disposable endpoint is archived as [endpoint.go.source](endpoint.go.source). Copy it to an explicit `endpoint.go` file outside the repository and build that file from the frozen base/candidate worktree with `go build -o /absolute/path/to/binary /absolute/path/to/endpoint.go` to opt in. Normal package enumeration cannot select it. The capture used these exact bytes under the filename `endpoint.go`; its content hash is unchanged. Hosted lint run [34088690434](https://github.com/the-sarge/quic-go-fast/actions/runs/34088690434) identified the repository's prohibition on Go files tagged `ignore`, so the archive suffix preserves the frozen fixture while satisfying that packaging rule. No capture source or binary was rebuilt for this rename.

The initial campaign is running on `minimax` with Go 1.27.1 and two/four physical cores per endpoint. The fixed endpoint, collector and initial isolation launcher were committed at `cc23153d`; the manifest records their applicable content hashes. Cores 8–15 are available to measurements; other user, system and VM workloads are restricted to cores 0–7 and 16–23 during each capture, leaving measured-core SMT siblings idle. The launcher restores the initially unrestricted settings on exit and has an independent three-hour restoration timer. The [host record](host.txt) captures topology, toolchain and the initial campaign service identity. Linux native GSO and emission tests passed before capture.

Linux allocation checks match base and prototype: 37 fixture-inclusive allocations per ordinary packet/batch and 104 per three-packet GSO batch. The embedded connection value grows from 1168 to 1216 bytes; there is no separate module allocation or close-payload change in this prototype. These counts include transparent protection, mock-adapter and assertion overhead; they do not claim native emission is allocation-free. The full paired matrix, existing handshake/churn/transfer benchmark comparisons, disposition of the earlier timeout observation, independent review and final certification remain outstanding. No performance/adoption disposition is justified yet.

Retain the one-initial/one-replacement review budget and the plan's one-campaign/one-replacement limit. Do not close E1, advance committed completion/frontier state, ready the PR for merge, append the journal or complete its OmniFocus task at this checkpoint.
