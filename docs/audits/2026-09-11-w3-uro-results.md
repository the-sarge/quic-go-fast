# W3 Windows coalesced receive (URO) adoption results

Results for the precommitted [W3 URO adoption protocol](2026-09-11-w3-uro-protocol.md) (Slice W3, Track W, program QGF-DP-2026-09).

**Disposition: Fail** — under the protocol's mechanical rule, in an uncontaminated collection: engaged-cell coalesced-datagram engagement was 0 % (fail bound: < 5 %) and the throughput point ratio was 0.914 (fail bound: < 0.90 is not met, but the pass bound's interval lower bound ≥ 0.95 is missed; the engagement criterion alone is decisive). URO never engaged on the qualified measurement surface. Per the protocol's termination rule the collection ran once and stops here; the failed disposition does not adopt and returns to the slice's stop conditions for an operator decision.

## Collection provenance

- **Candidate:** `c726ae18` (PR #248) — probe, coalesced-info parse, slab split, kill switch, harness, and precommitted protocol.
- **Collection head:** the candidate plus one workflow-only commit (`.github/workflows/w3-measure.yml`) on `scratch/w3-measure`; the measured product tree is the candidate byte for byte.
- **Official collection:** hosted run `34674085913` (workflow `w3-measure.yml`, `windows-latest`), one invocation, 11 rounds × 4 cells, round 0 discarded as warmup. No sample was discarded or repeated; the disposition rule was fixed before the data was inspected.
- **Host facts (constant across all 44 invocations):** OS build 10.0.26100, 4 CPUs, `GOMAXPROCS=4`, go1.27.1, windows/amd64. CPU affinity unavailable (hosted runner), recorded as absent.
- **Interaction matrix:** `TestWindowsUSOUROInteractionMatrix` and the URO closure classes passed on the collection head in the same run (`interaction.log` artifact): all four USO/URO combinations deliver payload intact and in order with correct destination packet info, slab-backed views exactly when URO is on, and ECN unsupported throughout. The functional half of the second-lander obligation holds.

## Cell integrity

Every invocation's asserted capabilities matched the cell table: client `gro_cap=true` in engaged and unexercised, `false` in disabled and unavailable; server `gso_cap=true` everywhere except the unexercised cell. All 40 official cell invocations completed their transfers correctly — the split path handles the traffic without corruption; it simply never received a coalesced read.

## Primary — receive syscalls per delivered datagram (engaged vs disabled)

Per-round paired ratios: exactly 1.0000 in every round; geometric mean **1.0000**, fixed-seed paired bootstrap (10,000 resamples, seed 20260912) 95 % interval **[1.0000, 1.0000]** against the ≤ 0.75 pass bound. The engaged cell issued one `WSARecvMsg` per delivered datagram — 392,002 single-datagram reads in a representative round; the per-read segment histogram contains only the bucket `1`.

## Engagement

Coalesced datagrams as a fraction of delivered datagrams: **0.0000 in every URO-on round** (engaged and unexercised cells alike; ~7.8 M delivered datagrams observed across the official collection). Not one `UDP_COALESCED_INFO` control message was delivered. This is the protocol's fail bound and the plan's silently-unengaged-offload evidence; the engagement metric exists precisely so such an adoption cannot pass.

## Throughput

Engaged vs disabled per-round paired ratio: geometric mean **0.914**, 95 % interval **[0.906, 0.923]** (noninferiority bound: lower ≥ 0.95 — missed). Raw medians: engaged 380.2 MB/s, disabled 412.3 MB/s, unavailable 412.6 MB/s (1.001 of disabled, within the 10 % context bound), unexercised 132.6 MB/s (its sender runs without USO, so it tracks the W2 disabled-cell baseline, not this bound's intent; recorded as a protocol-design imprecision — the 10 % context bound was written for a receive-side-only difference but the unexercised cell also changes the sender path). With URO enabled but never engaging, every read posts a 65,535-byte coalesced-tier buffer and wraps it in slab bookkeeping for a ~1,300-byte datagram: an ~8.6 % loopback throughput cost with zero coalescing payoff. Loopback microbenchmark throughput is not application/file/network throughput.

## Memory

Median peak working set: engaged **20.5 MB** vs disabled **20.4 MB** (+0.2 MB, within the +8 MiB budget); unavailable 20.3 MB; unexercised 13.3 MB. Allocation and GC counts showed no anomaly.

## Contamination

No cell violated the 1.30× max/median or median/min throughput rule; the collection is uncontaminated.

## Post-collection diagnostics (labeled; not part of the official collection)

Two scratch-branch diagnostic runs (`34674301236`, `34674447903`, branch `scratch/w3-diag`; the diagnostic never lands on the candidate) isolated the non-engagement after the official disposition was fixed:

- With 20 same-size same-flow datagrams **queued for 500 ms before the first read** — eliminating the reader-keeps-up explanation — a URO-enabled socket still returned 20 single-datagram reads with no `UDP_COALESCED_INFO`, in every configuration tried: option set after bind (the production shape), option set **before bind** (`net.ListenConfig.Control`), USO-segmented burst, and individually submitted sends.
- Binding both endpoints to the runner's **virtual NIC address** (10.1.0.106, Azure netvsc) instead of 127.0.0.1: identical result — zero coalescing.

Conclusion: on this surface (Windows Server build 10.0.26100, hosted runner), software URO never coalesces datagrams delivered over the local host path, regardless of bind order, backlog, or address family of the local route. Coalescing appears to require the NDIS receive-indication path that local traffic does not traverse — consistent with msquic validating its offload paths on CI with the `duonic` virtual NIC driver rather than loopback. The qualified Windows surface established by W1 (hosted `windows-latest`) is therefore structurally unable to produce URO engagement evidence, not merely noisy.

## Disposition against the mechanical rule

Engagement 0 % < 5 % in every URO-on cell of an uncontaminated collection → **Fail**. The functional interaction matrix and closure classes pass; the preserved-path cells (disabled, unavailable) behave identically to the W1/W2 foundation; memory is within budget. What failed is adoption: the offload cannot be shown to engage on the qualified surface, and while unengaged it costs ~8.6 % on that surface. Per the program's adoption rule (an engagement metric exists so a silently unengaged offload cannot pass) this collection does not adopt, and the outcome returns to the slice's stop conditions as an operator decision.
