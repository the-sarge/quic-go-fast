# W3 Windows coalesced receive (URO) adoption protocol

Precommitted measurement protocol for Slice W3 of the [Windows datapath plan](../adr/2026-09-11-windows-datapath-plan.md) (Track W, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the W3 implementation branch before any collection run; the results document records the exact candidate head measured.

## Amendment v2 (2026-09-12): qualified surface change

The v1 collection on the hosted `windows-latest` runner ran once and dispositioned **Fail** with zero engagement, and post-collection diagnostics established that any single-host surface is structurally unable to exercise URO: Windows delivers same-host traffic through a local shortcut that bypasses the NDIS receive-indication path where coalescing happens, so loopback and own-NIC-address traffic never coalesce regardless of backlog, bind order, or sender segmentation ([v1 record](2026-09-11-w3-uro-results.md)). The operator (Josh, 2026-09-12, this slice's stop-for-decision conversation) directed qualification on owned infrastructure instead.

v2 changes the qualified surface and the harness topology; it changes no cell semantics, metrics, statistical machinery, bounds, or disposition rules:

- **Surface:** the measured endpoint is a Windows Server 2025 KVM guest (`w3-uro-scratch`, 4 vCPU, 8 GiB, e1000e virtual NIC, QEMU/libvirt on the `minimax` host — an overlay clone of the infra GARM Server 2025 golden `v2`, build 10.0.26100). The sender is the `minimax` Linux host itself (AMD Ryzen AI MAX+ 395, Linux 7.0), sending across the libvirt NAT bridge (`virbr0`) into the guest's virtual NIC — a real NDIS receive path, verified URO-capable by a pre-collection engagement check (a GSO burst of 32×1200-byte datagrams arrives as one 38,400-byte coalesced read carrying `UDP_COALESCED_INFO`).
- **Topology:** the harness splits into two processes: `TestW3MeasurementServer` (Linux host, bulk sender, asserts and reports its GSO state) and `TestW3MeasurementClient` (Windows guest, measured receiver, all receiver-side metrics unchanged). The peer's GSO cell knob moves to the sender process; the sender pins its two-endpoint TLS identity exactly as the v1 harness pinned it (fixed measurement-only certificate, receiver `RootCAs`).
- **Workload:** unchanged (512 MiB of 1071-byte records, 32-record batches, raised flow-control windows), now crossing the virtual NIC instead of loopback.
- **Affinity and noise:** the sender is pinned per the repository's minimax precedent (`taskset -c 12,13,28,29`); guest vCPUs are not pinned and this is recorded. Host load is recorded before and after collection. The paired same-round design and the 1.30× contamination rule are unchanged. Other guests on the host (infra's GARM CI runners) may run during collection; this shared-host fact is recorded rather than controlled.
- **Provenance note:** the throughput noninferiority and memory bounds now bind on the virtual-NIC path. Absolute numbers are not comparable to the v1 loopback cells and are not compared against them.

## Question

Does enabling UDP receive coalescing (URO) on Windows transport-owned sockets reduce receive syscalls per delivered datagram in a bulk QUIC transfer, with noninferior throughput and bounded memory, while the unexercised, disabled, and unavailable configurations preserve the W1/W2 foundation behavior — and do all four USO/URO offload combinations validate payload and ancillary metadata?

## Artifact classes

The measurement harness (`w3_measurement_test.go`, build tag `w3bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the W3 probe, coalesced-info parsing, slab split, kill-switch, and retention-activation code.

## Fixed configuration

- **Base commit:** `6c1ab126` (default-branch HEAD at branch creation; W1+W2 message-I/O datapath without URO).
- **Candidate:** the W3 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's own disabled cell, which executes the preserved (W1/W2-identical) receive path — with the kill switch set, the capability probe never runs, ordinary-tier buffers are posted, and no split occurs, so the disabled cell's receive path is the base commit's byte for byte; unit suites separately assert byte-identical receive behavior with URO off.
- **Host:** a GitHub-hosted `windows-latest` runner, the program's qualified Windows execution surface established by the W1 protocol. Observed OS build, CPU count, architecture, and Go version are recorded per invocation because hosted runners differ run to run. Shared-VM noise is handled by the paired same-round design and the contamination rule below, not by affinity.
- **Toolchain:** the repository's current Go 1.27.x for windows/amd64; identical flags for every cell; one binary per collection built with `go test -c -tags w3bench`.
- **CPU affinity:** not available on a hosted runner; recorded as absent. `GOMAXPROCS` left at the runner default and recorded.
- **Workload:** one QUIC connection over IPv4 loopback between two transports in one process; the server bulk-sends 512 MiB of 1071-byte application records on one unidirectional stream, generating records in 32-record batches so the sender outpaces the packetizer the way a bulk sender does and the peer's USO path forms real segment bursts for URO to coalesce; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The externally owned application sweep stays out of scope.
- **Read counting:** the harness wraps the client's receive socket at the `ReadMsgUDP` seam — every call issues exactly one `WSARecvMsg` through the runtime poller, so reads are receive syscalls by construction — and counts every `ReadPacket()` return as one delivered datagram. The per-read segment distribution is derived from slab identity: consecutive views of one slab came from one coalesced read, and a non-slab packet is exactly one read. No external syscall counter exists on the hosted runner; the seam-level count is the declared method.
- **Cell integrity:** the harness asserts each cell's client GRO and server GSO capabilities match the cell table before measuring, so a silently misconfigured cell cannot measure. Every cell runs the same `windowsConn` datapath and binds loopback with identical explicit socket buffers (4 MiB) and the same DF flag; the only difference under measurement is the capability state the cell prescribes.

## Cells

| Cell | Receive socket | URO | Peer USO | Workload |
|---|---|---|---|---|
| engaged | transport-owned | on (probed) | on | bulk |
| available-but-unexercised | transport-owned | on (probed) | off (`QUIC_GO_DISABLE_GSO=1`: no segmented bursts to coalesce) | bulk |
| disabled | transport-owned | off (`QUIC_GO_DISABLE_GRO=1`) | on | bulk |
| unavailable | caller-supplied (never probed, off by the ownership rule) | off | on | bulk |

Coalesced-receive functional correctness (probe classes, split classes, packet-info inheritance, pending-view release, slab recycling) is owned by the unit suites on Windows CI, not by this protocol; this protocol owns the engagement and performance cells. The unavailable cell also demonstrates the caller-owned-socket rule operationally (no `UDP_RECV_MAX_COALESCED_SIZE` setsockopt, asserted by unit test `TestWindowsURONotEnabledOnCallerSuppliedSocket`).

## USO/URO interaction matrix

W3 is the W track's second lander, so the four-combination offload interaction matrix is this protocol's obligation. It is validated functionally by unit test `TestWindowsUSOUROInteractionMatrix` on the Windows CI matrix: for each of the four USO×URO combinations, a burst of segmented (or individually submitted) datagrams must arrive intact and in order, the destination packet info must be correct on a wildcard-bound socket, coalesced reads must split into slab-backed views only when URO is on, and ECN must remain unsupported on the Windows conn in every combination — the Server 2022 ancillary policy (TTL/DSCP receive features stay disabled while offloads are active) is satisfied structurally because the conn requests neither feature. The results document records this test passing on the exact candidate head as part of the collection run; the test remains in the ordinary suite afterward, so the matrix stays continuously enforced.

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all four cells, cell order rotating by round, one fresh process per cell invocation. The collection runs in one hosted job with no concurrent steps. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: the cell and round; wall time and throughput (`throughput_mb_per_s`, decimal megabytes per second); client socket reads, delivered datagrams, reads per delivered datagram, coalesced reads and datagrams, and the per-read segment-count histogram; Go allocation totals, GC count, heap in use; peak working set (`K32GetProcessMemoryInfo`); and the recorded host facts (OS build, CPU count, `GOMAXPROCS`, Go version, architecture).

## Metrics and predeclared bounds

- **Primary — client receive syscalls per delivered datagram**, engaged vs disabled, paired within each round. Minimum useful effect: geometric-mean ratio ≤ 0.75 (at least 25 % fewer receive syscalls per delivered datagram, matching the G2 bound for the same seam). A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds, seed 20260912) gives a 95 % interval; pass requires the interval's upper bound ≤ 0.75. The bootstrap describes this sample set, not a universal timing guarantee.
- **Engagement:** fraction of delivered datagrams arriving in coalesced (> 1 segment) reads in the engaged cell, with the per-read distribution published. Zero engagement across every URO-on cell of the fixed collection is the slice's stop-condition evidence.
- **Throughput noninferiority:** engaged vs disabled per-round ratio with the same paired bootstrap; the 95 % interval's lower bound must be ≥ 0.95 (no more than 5 % regression — URO is a syscall-reduction adoption; wall-clock improvement is reported but not gated). The unexercised and unavailable cells are reported for context and must show no material regression against disabled (medians within 10 %).
- **Memory budget:** engaged-cell median peak working set ≤ disabled-cell median + 8 MiB (declared allowance for the one-64-KiB-buffer-per-read prepost shape plus coalesced-tier pool residency and run-to-run working-set noise). Per-connection retention pinning is bounded by constants: `MaxConnRetainedCoalescedBytes` per connection, enforced by the retention queues through the shared coalesced-storage contract. Go allocation counts and GC cycles are reported against the disabled cell for context.
- **Preservation:** disabled and unavailable cells must report `gro_cap=false` and complete correctly; behavioral identity of the URO-off receive path is owned by the unit suites on Windows CI, not by timing.
- **Host contamination rule:** if within any cell the maximum per-round throughput exceeds 1.30× that cell's median, or the median exceeds 1.30× the minimum, the collection is contaminated by shared-host noise and the disposition is inconclusive. Paired ratios do not rescue a collection whose raw rounds are this unstable.

## Disposition rule (mechanical)

- **Pass:** primary interval upper bound ≤ 0.75, engaged-cell coalesced-datagram engagement > 50 % of delivered datagrams, throughput interval lower bound ≥ 0.95, memory within budget, interaction matrix green on the candidate head, no contamination.
- **Fail:** primary point ratio > 0.90, or engaged-cell engagement < 5 %, or throughput point ratio < 0.90, or a memory budget exceeded, in an uncontaminated collection.
- **Inconclusive:** anything else, including contamination. An inconclusive result is reported as such and does not adopt; it returns to the slice's stop conditions.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
