# W3 Windows coalesced receive (URO) adoption results

Results for the precommitted [W3 URO adoption protocol](2026-09-11-w3-uro-protocol.md) (Slice W3, Track W, program QGF-DP-2026-09).

**Disposition: Pass** — on the protocol's v2 qualified surface (a Windows Server 2025 KVM guest receiving across a virtual NIC on the `minimax` host). URO engages on a real NDIS receive path: ~95.7 % of delivered datagrams arrive coalesced, receive syscalls per delivered datagram fall to 0.098 of the disabled baseline (≈ 10.2× fewer) against a ≤ 0.75 gate, throughput is noninferior with a 1.34× improvement, and memory is within budget. The v1 collection on the hosted `windows-latest` runner dispositioned Fail because that single-host surface cannot exercise URO at all; that record is retained below as history and motivated the v2 surface change.

## v2 collection (adopting) — minimax KVM virtual-NIC surface

### Provenance

- **Candidate:** `7eddc0f8` (PR #248) — probe, coalesced-info parse, slab split, kill switch, and the two-process v2 harness. Shipped code (`sys_conn_windows.go`) is unchanged from the v1 candidate; only the `w3bench`-tagged harness gained the cross-host split.
- **Surface:** measured receiver is the Windows Server 2025 KVM guest `w3-uro-scratch` (4 vCPU, 8 GiB, `e1000e` virtual NIC, `q35`/OVMF, QEMU 10.2 / libvirt on `minimax`), an overlay clone of the infra GARM Server 2025 golden `v2` (build 10.0.26100). Sender is the `minimax` Linux host (AMD Ryzen AI MAX+ 395, Linux 7.0, Go 1.27.1), pinned `taskset -c 12,13,28,29`, sending across the libvirt NAT bridge `virbr0` into the guest NIC.
- **Surface qualification:** a pre-collection engagement check confirmed the guest coalesces — a GSO burst of 32×1200-byte datagrams arrived as one 38,400-byte read carrying `UDP_COALESCED_INFO`. (The hosted `windows-latest` runner produced zero such reads under every diagnostic; see v1 below.)
- **Guest facts (constant across all invocations):** OS build 10.0.26100, 4 vCPU, `GOMAXPROCS=4`, go1.27.1, windows/amd64. Guest vCPUs unpinned (recorded). Host load average 1.2–1.6 on 32 threads across the run; one infra GARM CI guest (`gridcast-win64`) was also resident — a recorded shared-host fact, not a controlled variable. The 1.30× contamination rule guards against gross noise and did not trip.
- **Collection:** one fixed run, 11 rounds × 4 cells, rotating cell order, round 0 discarded as warmup, 512 MiB per cell. 44 receiver invocations completed correctly; cell integrity (client GRO and sender GSO matching the cell table) held on every one. No sample discarded or repeated; the disposition rule was fixed before the data was inspected.

### Primary — receive syscalls per delivered datagram (engaged vs disabled)

Per-round paired ratios 0.0969–0.0997; geometric mean **0.0983**, fixed-seed paired bootstrap (10,000 resamples, seed 20260912) 95 % interval **[0.0977, 0.0990]** against the ≤ 0.75 pass bound — **passed** with ≈ 10.2× fewer receive syscalls per datagram. Engaged median 0.098 reads/datagram versus disabled 1.000 (one `WSARecvMsg` per datagram, the preserved W1/W2 path). A representative engaged round delivered ~393 K datagrams in ~21.6 K reads.

### Engagement

Coalesced datagrams as a fraction of delivered datagrams in the engaged cell: minimum 0.9564, median **0.9569** (bound: > 0.50). The per-read segment histogram is bimodal — a large mass at the 24-segment cap (the kernel's coalescing ceiling for this MTU) and a single-datagram tail — with a broad spread between, exactly the opportunistic coalescing URO performs under a fast stream.

### Throughput

Engaged vs disabled per-round paired ratio: geometric mean **1.341**, 95 % interval **[1.174, 1.527]** (bound: lower ≥ 0.95 — noninferiority passed with an improvement, reported but not gated). Raw medians: engaged 497.0 MB/s, disabled 382.8 MB/s, unavailable 415.5 MB/s (1.086 of disabled), unexercised 511.8 MB/s. Loopback-vs-NIC caution does not apply here (all cells cross the same virtual NIC), but a virtual-NIC microbenchmark is still not application/file/physical-network throughput.

### Memory

Median peak working set: engaged **19.4 MB** vs disabled **19.7 MB** (−0.3 MB; the one-64-KiB-buffer-per-read prepost is amortized by far fewer reads at these rates), unexercised 18.2 MB, unavailable 20.1 MB — all within the +8 MiB budget. Allocation and GC counts showed no anomaly.

### Recorded deviation — the unexercised cell did not stay unexercised

The unexercised cell (receiver URO on, **sender GSO off** via `QUIC_GO_DISABLE_GSO`) was designed to show the capability available but idle, mirroring the G2 Linux precedent where disabling the peer's GSO removed the segments GRO needs. On Windows this assumption does not hold: URO coalesces any sufficiently back-to-back same-flow datagram stream at the receive layer regardless of whether the *sender* segmented, so this cell still coalesced ~95.8 % and tracked the engaged cell rather than the disabled one. This is a protocol-design imprecision carried over from the Linux template, not a measurement fault. It does not affect the disposition: the mechanical rule gates only on the **engaged** cell's engagement and on the engaged-vs-disabled primary, throughput, and memory comparisons, all of which are unaffected. The cell is reported for completeness and is non-informative for its intended "available but idle" purpose on this platform.

### Contamination

No cell violated the 1.30× max/median or median/min throughput rule; the collection is uncontaminated.

### Disposition against the mechanical rule

Primary interval upper bound 0.0990 ≤ 0.75; engaged engagement 0.957 > 0.50; throughput interval lower bound 1.174 ≥ 0.95; memory within budget; no contamination → **Pass**. Per the protocol's termination rule the collection ran once and stops here.

## v1 collection (historical, Fail) — hosted `windows-latest` single-host surface

The original collection on the hosted `windows-latest` runner (Azure Windows Server 2025, build 10.0.26100) ran once and dispositioned **Fail**: engaged-cell engagement was **0.0000** across ~7.8 M delivered datagrams — not one `UDP_COALESCED_INFO` control message was delivered — and the unengaged offload cost ~8.6 % throughput on that surface. The functional interaction matrix and closure classes passed, and the preserved-path cells were byte-identical to the W1/W2 foundation; only adoption failed.

Post-collection diagnostics (scratch runs, never landed on the candidate) established the cause as structural rather than noise: with 20 same-flow datagrams queued for 500 ms before the first read, a URO-enabled socket still returned only single-datagram reads — with the option set pre-bind or post-bind, with USO-segmented or individually submitted sends, and bound to loopback or to the runner's own netvsc NIC address. Windows delivers same-host traffic through an internal shortcut that bypasses the NDIS receive-indication path where coalescing happens, so no single-host surface can exercise URO (consistent with msquic validating its offload paths via the `duonic` virtual NIC driver rather than loopback). The hosted runner has no second endpoint reachable to it, so it cannot produce URO engagement evidence. This motivated the operator-directed v2 surface change to owned two-endpoint infrastructure, recorded in the protocol's v2 amendment.
