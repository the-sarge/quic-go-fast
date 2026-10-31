# Bounded GridCast release fixture reuse

This extension supplies the packet-model and Reno-competitor portion of [Adapt the existing fixture for the bounded GridCast BBR release check](https://github.com/GridCastIO/gridcast/issues/1706). The [accepted resolution](https://github.com/GridCastIO/gridcast/issues/1705#issuecomment-6537913556) owns the topology, measurement disposition and fixed profiles. Infra retains admission, topology, process deadlines and cleanup. GridCast retains registration; wiremux retains actual-transfer execution. No traffic or calibration allowance is created here.

Historical Q1 profiles, module replacement, raw receipts and measurement windows retain their definitions. The two new model names are `gridcast-loss` and `gridcast-coexistence`. Both directions use 100 Mbit/s and a 1,251,220-IP-byte drop-tail service queue. Loss uses 40 ms modeled one-way propagation and forward-only seeded probability 0.001. Coexistence uses 50 ms modeled one-way propagation, no random loss, no marking and no L6 changing-delay schedule. Phase must be zero. Service queue and propagation accounting remain distinct. Both flows feed the same queue in each direction.

The raw router still preserves IP endpoints and enforces canonical, unfragmented IPv4 at most 1460 IP bytes. The Linux focal and competitor addresses must differ. They may share the Mac endpoint IP: classification uses the distinct source or destination address, so reverse focal traffic is not accidentally labeled as competitor traffic. Router samples now retain cumulative canonical UDP focal/competitor packet counts. These are activity evidence, not application useful-byte measurements. Existing adapter validity observations and 5 ms scheduling limits remain active.

Only the two new profiles accept an absolute `AttemptClock` file, `LifetimeSeconds: 600`, `MeasureSeconds: 600`, phase zero and `StartUnixNS: 0`. The infra owner publishes the Linux monotonic sender-launch clock once, atomically, immediately before sender fork. The router bounds preparation at 1800 seconds and, once the file appears, stops at that sender deadline, with no historical export tail. Its invocation time does not consume sender time. Invalid clock data fails the adapter. These two configuration durations declare the fixed allowance; model time starts at router startup because both new profiles are stationary. Legacy profiles reject these overrides and retain their historical 300-second measurement maximum and 20-second export tail.

Gate cancellation records queued-packet discards separately from actual `SendErrors`; historical cancellation accounting is preserved. This does not make an incomplete application or missing overlap successful. The application outcome and overlap checks remain independent.

## Pinned build boundary

Use the alternate module file `gridcast.mod`, with `GOWORK=off`, `GOTOOLCHAIN=local`, an empty `GOFLAGS`, Go 1.27.3 and no private build tags. Its replacement is the released `github.com/the-sarge/quic-go-fast v0.62.1-fast.9`; it cannot accidentally inherit a moving checkout through the historical relative replacement. Preserve `gridcast.sum` with the source. The original `go.mod`/`go.sum` remain historical inputs.

From this campaign directory, after checking out the reviewed fixture commit, execute:

```sh
export GOTOOLCHAIN=local GOWORK=off GOFLAGS='' CGO_ENABLED=0
SOURCE_REVISION=$(git rev-parse HEAD)
go mod verify -modfile=gridcast.mod
go list -m -modfile=gridcast.mod -json all > modules.json
GOOS=linux GOARCH=amd64 go build -mod=readonly -modfile=gridcast.mod -trimpath -ldflags "-X main.sourceRevision=$SOURCE_REVISION" -o router-linux-amd64 ./router
GOOS=linux GOARCH=amd64 go build -mod=readonly -modfile=gridcast.mod -trimpath -ldflags "-X main.sourceRevision=$SOURCE_REVISION" -o competitor-linux-amd64 ./fixture
GOOS=darwin GOARCH=arm64 go build -mod=readonly -modfile=gridcast.mod -trimpath -ldflags "-X main.sourceRevision=$SOURCE_REVISION" -o competitor-darwin-arm64 ./fixture
```

Record compiler, exact source revision, full module inventory, executable SHA-256 and `go version -m` for every artifact. These are cross-build recipes; they do not claim a native performance check or permission to dispatch the binaries. Configuration/certificate files and executable hashes must be admitted through infra's owner before use.

## Competitor and overlap

Use the existing fixture command engine, with `gridcast_gate: true`, `controller: "reno"`, `workload: "stream"`, `warmup_ms: 60000`, `measure_ms: 60000`, a unique ID, `payload_bytes: 16384` and a frozen start epoch in the next two minutes. Pause and completion overrides are refused. TCP mode is refused. The release mode fixes the receiver ACK threshold at two before transport initialization on both peers. Historical fixture invocations omit `gridcast_gate` and retain their behavior.

Invoke the existing `-role receive|send`, `-config`, `-local`, `-peer`, `-cert`, `-key` and `-output` flags. The Linux competitor binds `10.204.1.3:55031` and the Mac receiver binds `192.168.1.110:55031`; infra pins the exact vectors. Retain both final result files, including actual controller identity, receiver-consumption useful bytes, per-second delivery, control opportunities, latency samples and clock-offset bounds. Start both flows before the registered joint window, and reject missing overlap rather than shifting or extending the window after seeing the result.

`gridcast_receipt.py` consumes the two competitor receipts, the raw router receipt and infra's GridCast process receipt:

```text
python3 gridcast_receipt.py --sender SENDER_JSON --receiver RECEIVER_JSON --router ROUTER_JSON --application GRIDCAST_PROCESS_JSON
```

It reconciles the fixed warmup/measurement epochs, sender/receiver agreement, useful-byte totals, control-latency accounting, clock bounds, GridCast lifetime and per-direction activity observations. It requires focal activity across the clock-bounded joint window. Competitor activity uses the Linux sender/router clock, matching the engine's scheduled start/end without demanding unscheduled pre-start traffic. Both retain the same fixed warmup and overlap. Zero-delivery seconds remain measured stalls. Completed control replies delayed across later skipped opportunities remain measured latency; unresolved probes remain censored and inconclusive. The output preserves those counts and the useful-byte bins. Missing intervals, unknown overlap or adapter failures produce `inconclusive`. The observed granularity is one second, with a two-second maximum sample gap; it is not proof of continuous useful GridCast delivery. Ordinary GridCast steady-window and repair/drop counters remain unavailable optional diagnostics. The infrastructure owner and final registration still supply independent file integrity, native path, resource and completion evidence.

Reconciliation is not the release verdict. The existing paired 0.70 useful-byte floor, adverse 1.20 latency ratio, completion comparisons, integrity rules and Reno rollback remain in GridCast's accepted gate. No whole campaign analyzer, new workload engine, platform sweep or extra attempt is introduced.

## Source validation

The deterministic tests cover exact rates/queues/delays/loss, seed replay, mixed-flow FIFO capacity and serialization, the old L6 transition time, shared-Mac reverse classification, lifetime restrictions, fixed competitor config and receipt rejection on missing overlap/invalid counters. The existing model and fixture tests also run against the released dependency boundary. Linux router cross-compilation checks the adapter build. These checks do not establish macvlan/offload support, physical propagation, queue calibration or a GridCast result on the new path.
