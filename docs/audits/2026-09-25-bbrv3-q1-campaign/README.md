# Q1 campaign preparation

Status: implementation candidate, **not calibrated or campaign-ready**. Q1 #598
remains open and Q2 #599 remains blocked. These sources are campaign-only
verification aids, frozen with the eventual manifest and archived after Q2.
They ship no controller behavior and are not a maintained emulator framework.

The operator extended preparation to **16 cumulative hours** on 2026-09-25;
[authorization](https://github.com/the-sarge/quic-go-fast/issues/598#issuecomment-5837538642).
The $100 cloud and 48 experiment-hour ceilings remain unchanged. Historical
acceptance records retain their original eight-hour allowance. [Infra PR638](https://github.com/the-sarge/infra/pull/638), merged as
`4fcfa6ad7f4cff923fa88d97ce82903b0e734aa9`, enforces the approved extension.

## Execution boundary

This candidate implements the existing Q1 child and one product PR, against
product source `bfa11d3f698ac98da415c7bcbd9840923180d0bb` and Q1 plan section at
`4161e1b4ca9edf6b743841245ab6e27464401a11`. The native environment, actual
calibration and current budget ledger own readiness; passing local tests or
cross compilation does not establish it. No Q2 comparisons are authorized by
this receipt. Product transport and controller code remain outside this diff.

The supported domain is explicitly configured IPv4 endpoint pairs and the
accepted scenario matrix. QUIC owns wire parsing; the native Linux adapter uses
`golang.org/x/net/ipv4` and mechanically rejects options, fragments, IP packets
over 1460 bytes and protocols other than ICMP/TCP/UDP. It needs campaign-owned
interfaces, disabled offloads and disabled kernel forwarding. The lifecycle
caller owns those reversible changes and restoration. Never run it on shared
production interfaces. Its next-hop MAC is the actual route's next hop (GCE's
virtual router), not an inferred peer MAC.

Representation guarantee: example-level evidence for the frozen native hosts,
not general platform qualification. No persisted product schema, transitional
product authority or maintained-aid exception is introduced. Contract closure
is not triggered for these bounded campaign aids. Initial dispatch context is
the Q1/common contract, named design and source seams, current frontier and
focused prior receipts, inside the accepted 35,000-token input ceiling.
One fresh review and at most one replacement apply to this product PR.

## Components and finite checks

- `fixture`: real native QUIC with the public Reno/BBRv3 selector and direct
  managed packet-I/O lease. Deterministic record identities/content, receiver
  useful-byte/duplicate/integrity accounting, 32-byte control request/response,
  fixed completion through verified EOF and bounded trace/resource observations.
  Unique content excludes the 16-byte fixture header. Stream records must have
  consecutive identities; DATAGRAM messages may reorder, duplicate or disappear.
- `model`: full-IP-byte FIFO serialization followed by independently scheduled
  propagation. Fixed finite queues, deterministic S6 loss, S8 ECT(0)-to-CE and
  Not-ECT early drop, L1 post-service delay, L2 arrival windows and L3 partial
  service/rate transitions and interruption flushing. Pending propagation has
  an explicit rate/delay-derived storage bound; overflow invalidates calibration.
- `router`: Linux AF_PACKET adapter using pinned `gopacket/afpacket` v1.7.1
  ([upstream interface](https://pkg.go.dev/github.com/gopacket/gopacket/afpacket)).
  TPACKET v3 uses an 8 MiB receive ring per direction, 1 ms block retirement
  and bounded 100 ms polling. Ring views are copied before their next read;
  canonical IP parsing remains owned by `x/net/ipv4`. Each direction has one
  model owner and bounded input/output channels. A separate emission worker
  owns each departing frame; it is joined before counters are published. Records kernel socket drops, noncanonical packets, missing kernel
  timestamps, ingress processing delay and scheduled-departure lateness. Any
  socket/send/storage failure, or maximum ingress/egress lateness over 5 ms,
  invalidates its receipt. Native rate/RTT tests must independently establish
  whether that ceiling is adequate for each scenario; it is not a calibration
  claim. CPU demand is outside endpoint allocations.
- `probe`: frozen Linux UDP offered-load source, sink and RTT echo. It counts
  complete IP bytes and received ECN bits, and exposes socket overflow. These
  are path-calibration observations, not QUIC useful delivery.

Focused evidence exercises integrity/duplicate/window accounting, actual QUIC
stream and DATAGRAM transfer, completion, one-pending-request cadence with
warm-up accounting, serialization/byte queues/CE, rate changes without queue
resizing, delay reordering, arrival-window boundaries, interruption flush and
bounded propagation storage. One focused race run covers the concurrent fixture.
Native calibration must additionally prove the packet adapter, offered/received
rates, RTT, actual marks, L3 timing, endpoint allocation and resource limits.
No whole-platform or mutation campaign is implied.

From this directory:

```sh
go test ./fixture ./model
go test -race ./fixture ./model
go vet ./fixture ./model
go mod tidy -diff
GOOS=linux GOARCH=amd64 go vet ./router ./probe
GOOS=linux GOARCH=amd64 go build -o /tmp/q1router ./router
GOOS=linux GOARCH=amd64 go build -o /tmp/q1probe ./probe
GOOS=linux GOARCH=amd64 go build -o /tmp/q1fixture-linux ./fixture
GOOS=windows GOARCH=amd64 go build -o /tmp/q1fixture.exe ./fixture
go build -o /tmp/q1fixture ./fixture
```

Release calibration binaries must also pin `-ldflags '-X main.sourceRevision=SHA'`
for the fixture and record binary hashes. Campaign mTLS keys are created in a
private staging directory and never committed. Both peers receive the same
short-lived campaign trust material; no cloud credentials reach guests.

## Remaining evidence

Native Linux and Windows fixture/model calibration, Mac CPU scheduling decision
and local model placement, actual per-scenario commands, competitor orchestration,
clock-error bounds and the costed frozen manifest remain incomplete. Queue sizes
currently use MTU 1460 with the first L3 phase as the fixed basis: forward 32120
bytes, reverse 188340 bytes. Those are selected parameters pending calibration.

Resource samples observe cumulative process CPU, heap once per second, full-run
allocations and process peak RSS. Connection setup elapsed includes listener
waiting on the server and dial setup on the sender; neither is included in the
fixed useful-delivery window. Qlog callbacks retain bounded metrics/events but
still incur instrumentation cost. Native CPU saturation must be reported as a
fixture/host limitation where applicable, not interpreted as controller capacity.

## Linux preparation receipt

[The retained preparation receipt](evidence/linux-preparation-summary.json) indexes
102 frozen native configuration/result records, including failures. Four short
Reno/BBRv3 stream/DATAGRAM native smoke cells passed integrity and controller
selection checks; these are correctness observations, not comparisons. Earlier
adapter revisions demonstrated directional rate, marking/drop and queue behavior,
but they do not certify the latest adapter revision.

The unprofiled S3 offered-load probe at `09f9d290` had no socket or send errors,
but maximum admission lateness was 9.062 ms and departure lateness 5.101 ms;
both exceed the 5 ms gate. The earlier L3 phase probe also failed its lateness
gate despite observing the four selected rates. These paths remain invalid.
A separately labeled CPU-profile diagnostic attributed about 27% of sampled CPU
to model advancement and 21% to scheduling. The next candidate reduces heap
packet copying; passing semantic tests is not native calibration.

## Frozen case ordering candidate

`case-inventory.jsonl` enumerates all 460 core and 60 completion cases, with
matched seeds, controller order within each pair, L1/L2 phases and fixed payload
sizes. Recounting the entries gives 129,600 core seconds (36 hours) and at most
3,600 completion measurement seconds. The second completion hour remains
reserved for setup/cleanup and clock checks. `manifest-candidate.json` records
the inventory digest and missing prerequisites; it is not an execution permit.

At launch, add the calibrated absolute `start_unix_ns` to the fixture config.
For a core run that is the beginning of warm-up; set the router's `StartUnixNS`
to fixture start plus warm-up, so L3 and competitor changes remain relative to
the measured interval. Completion has no warm-up and L1 uses phase 2 seconds.
Keep all run IDs, seeds and pair orders from the inventory; do not regenerate a
new ordering between controller runs or delete failed rows.
