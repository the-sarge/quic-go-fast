# Bounded GridCast release fixture reuse

This extension supplies the packet-model and Reno-competitor portion of [Adapt the existing fixture for the bounded GridCast BBR release check](https://github.com/GridCastIO/gridcast/issues/1706). The [accepted resolution](https://github.com/GridCastIO/gridcast/issues/1705#issuecomment-6537913556) owns the topology, measurement disposition and fixed profiles. Infra retains admission, topology, process deadlines and cleanup. GridCast retains registration; wiremux retains actual-transfer execution. No traffic or calibration allowance is created here.

Historical Q1 profiles, module replacement, raw receipts and measurement windows retain their definitions. The two new model names are `gridcast-loss` and `gridcast-coexistence`. Both directions use 100 Mbit/s and a 1,251,220-IP-byte drop-tail service queue. Loss uses 40 ms modeled one-way propagation and forward-only seeded probability 0.001. Coexistence uses 50 ms modeled one-way propagation, no random loss, no marking and no L6 changing-delay schedule. Phase must be zero. The local IP MTU is 1500, matching both physical interfaces; the service queue stays exactly 1,251,220 bytes and is not re-rounded. The propagation-storage guard uses the same rate/delay/capacity formula with two 1500-byte packet margins. Service queue and propagation accounting remain distinct. Both flows feed the same queue in each direction.

The raw router still preserves IP endpoints and enforces canonical, unfragmented IPv4 at most 1500 IP bytes for the local GridCast profiles; historical cloud profiles retain 1460. The Linux focal and competitor addresses must differ. They may share the Mac endpoint IP: classification uses the distinct source or destination address, so reverse focal traffic is not accidentally labeled as competitor traffic. Gate router Activity snapshots retain synchronized cumulative canonical UDP focal/competitor packet counts across both directions, with directional counts in the same snapshot. Per-direction observations retain queue and adapter validity. These are activity evidence, not application useful-byte measurements. Existing adapter validity observations and 5 ms scheduling limits remain active.

Only the two new profiles accept an absolute `AttemptClock` file, `LifetimeSeconds: 600`, `MeasureSeconds: 600`, phase zero and `StartUnixNS: 0`. The infra owner publishes the Linux monotonic sender-launch clock once, atomically, immediately before sender fork. The admitted Linux supervisor owns the bounded 1800-second preparation phase under its lifecycle lock. The router waits for the clock or custody cancellation and, once the file appears, independently stops at the published sender deadline, with no historical export tail. Its invocation time does not consume sender time. Invalid clock data fails the adapter. These two configuration durations declare the fixed allowance; model time starts at router startup because both new profiles are stationary. Legacy profiles reject these overrides and retain their historical 300-second measurement maximum and 20-second export tail.

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

It reconciles the fixed warmup/measurement epochs, sender/receiver agreement, useful-byte totals, control-latency accounting, clock bounds, GridCast lifetime and per-direction activity observations. It requires duplex focal activity across the clock-bounded joint window. The single synchronized Activity stream represents each flow; reverse traffic need not arrive in the same initial interval as the forward packet that causes it. Each direction must show traversal over the joint window, and interior duplex gaps remain inconclusive. Competitor activity uses the Linux sender/router clock, matching the engine's scheduled start/end without demanding unscheduled pre-start traffic. Both retain the same fixed warmup and overlap. Zero-delivery seconds remain measured stalls. Completed control replies delayed across later skipped opportunities remain measured latency; unresolved probes remain censored and inconclusive. The output preserves those counts and the useful-byte bins. Missing intervals, unknown overlap or adapter failures produce `inconclusive`. The observed granularity is one second, with a two-second maximum sample gap; it is not proof of continuous useful GridCast delivery. Ordinary GridCast steady-window and repair/drop counters remain unavailable optional diagnostics. The infrastructure owner and final registration still supply independent file integrity, native path, resource and completion evidence.

Reconciliation is not the release verdict. The existing paired 0.70 useful-byte floor, adverse 1.20 latency ratio, completion comparisons, integrity rules and Reno rollback remain in GridCast's accepted gate. No whole campaign analyzer, new workload engine, platform sweep or extra attempt is introduced.

## Source validation

The deterministic tests cover exact rates/queues/delays/loss, seed replay, mixed-flow FIFO capacity and serialization, the old L6 transition time, shared-Mac reverse classification, lifetime restrictions, fixed competitor config and receipt rejection on missing overlap/invalid counters. The existing model and fixture tests also run against the released dependency boundary. Linux router cross-compilation checks the adapter build. These checks do not establish macvlan/offload support, physical propagation, queue calibration or a GridCast result on the new path.

Pre-clock preparation expiry is owned solely by the admitted Linux supervisor under the infrastructure lifecycle lock. The gate router waits for the sender clock or custody cancellation; it does not run an independent preparation timer that could race sender admission. After publication it independently enforces the absolute Linux sender deadline. Gate mode is supported only through the registered infrastructure owner.

Reconciliation diagnostic limitation: the current lifetime-coverage reason also covers a nonzero GridCast exit. It does not establish that temporal overlap was missing. Always classify application failure from the original GridCast process receipt’s `exit`, `cancelled`, `teardown` and timestamps, alongside the retained competitor measurements. A failed application remains inconclusive regardless of temporal coverage; this wording limitation neither discards measurements nor permits a failed result.

## Local MTU amendment

The operator explicitly directed “use 1500 and let's get this done” after diagnosing the inherited cloud limit in [wiremux #1469](https://github.com/GridSwarm/wiremux/issues/1469#issuecomment-6550877003). Local native macOS UDP can emit an ordinary 1469-byte IP packet despite a locked 1460-byte route metric. GridCast retains its released ordinary binary and MTU discovery; the local fixture now admits canonical unfragmented IPv4 up to 1500 rather than classifying those normal probes as noncanonical. Only `gridcast-loss` and `gridcast-coexistence` select this limit. Historical profiles, 1460-byte cloud manifests, recorded evidence and product transport/controller code are unchanged.

The terminating source evidence covers the observed 1458/1469/1480 sizes, the 1500/1501 boundary in both local profiles, preservation of the historical 1460 boundary, unchanged non-size canonical requirements, fixed queue/propagation accounting, affected model/router/fixture tests and races, vet, module tidiness and Linux cross-build. Native path validity remains required during the six unspent registered application attempts, with zero NonCanonical and the other existing counters/lag checks. No consumed slot is replaced or reclassified. Fresh full-panel review, exact-head certification and hosted checks follow [the repository overlay](../../REVIEW-LOOP.md). This verification-aid amendment has example-level source/native evidence, authoritative IPv4 parsing by x/net, no new protocol grammar or safety mechanism, and no material multi-path closure trigger.
