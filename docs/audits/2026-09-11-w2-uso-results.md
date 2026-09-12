# W2 Windows segmented send (USO) adoption results

Results for the precommitted [W2 USO adoption protocol](2026-09-11-w2-uso-protocol.md) (Slice W2, Track W, program QGF-DP-2026-09).

**Disposition: Pass.**

## Collection provenance

- **Candidate (W2 PR head):** `1af918b1d4c9b1d96f54072a72609fbb444981c5` (PR #245).
- **Collection head:** `4db67a4511dfc1cf98b7ff61905efb561b1670fa` on `scratch/w2-measure` — the candidate plus one workflow-only file (`.github/workflows/w2-measure.yml`, 62 added lines, verified as the entire diff); the measured product tree is the candidate byte for byte.
- **Official collection:** hosted run `34669428064` (workflow `w2-measure.yml`, `windows-latest`), one invocation, 11 rounds × 4 cells, round 0 discarded as warmup. Two earlier workflow invocations (`34668970613`, `34669221313`) failed in the guard-mutation step's mechanics (a patch-encoding error under the runner's CRLF checkout, then a substring-counting bug in the step's own sanity check) after their collection steps; their measurement data was never downloaded or analyzed, and the official run was declared before its data was inspected. No sample in the official collection was discarded or repeated.
- **Host facts (constant across all 44 invocations):** OS build 10.0.26100, 4 CPUs, `GOMAXPROCS=4`, go1.27.1, windows/amd64. CPU affinity unavailable (hosted runner), recorded as absent.
- **Guard mutation:** with `isSendMsgSizeErr` forced to `false` on the collection head, `TestSendQueueHandshakeMTUEligibility` failed (`/handshake` and `/unmarked_or_PMTU_probe` subtests) as the protocol requires; the mutation was then reverted. Receipt in the run's `mutation.log` artifact.

## Cell integrity

Every invocation's asserted capability matched the cell table: `gso_cap=true` in engaged and unexercised, `gso_cap=false` in disabled and unavailable. All 40 official cell invocations completed their transfers correctly.

## Primary — send submissions per transmitted packet (engaged vs disabled)

- Per-round paired ratios: 0.0808–0.0820; geometric mean **0.0815**; fixed-seed paired bootstrap (10,000 resamples, seed 20260911) 95 % interval **[0.0813, 0.0817]**.
- Predeclared bound: pass requires interval upper bound ≤ 0.50. **Passed** with a ~12.3× reduction: the engaged bulk sender needed a median 31,957 submissions for 392,021 packets (12.27 packets per submission) against the disabled cell's one submission per packet (391,671 submissions).
- Engaged-cell batch-size distribution (round 1, representative): dominated by 14-packet and 11-packet submissions (15,594 and 14,969 of them), with a small single-packet tail (133).

## Engagement

Fraction of server-transmitted packets leaving in multi-packet submissions: minimum 0.9994, median **0.9996** across rounds (bound: > 0.50). The unexercised cell reported exactly 1.0 packets per submission in every round (bound: ≤ 1.05), demonstrating the capability can be on without engaging; its paced transfer took a median 2.0 s as its timer dictates.

## Throughput

Engaged vs disabled per-round paired ratio: geometric mean **3.01**, 95 % interval **[2.93, 3.10]** (bound: lower ≥ 0.95 — noninferiority passed with a large improvement, reported but not gated). Raw medians: engaged ≈ 407 MB/s, disabled ≈ 134 MB/s. Unavailable-cell median 134.2 MB/s vs disabled 134.4 MB/s (within the 10 % context bound). Loopback microbenchmark throughput is not application/file/network throughput.

## Memory

Median peak working set: engaged **20.6 MB** vs disabled **18.8 MB** (+1.8 MB, within the +8 MiB budget); unavailable 18.8 MB, unexercised 13.3 MB. Allocation and GC counts showed no anomaly.

## Contamination

No bulk cell violated the 1.30× max/median or median/min throughput rule; the collection is uncontaminated.

## Disposition against the mechanical rule

Primary interval upper bound 0.0817 ≤ 0.50; engaged multi-packet engagement 0.9996 > 0.50; unexercised packets per submission ≤ 1.05 in every round; throughput interval lower bound 2.93 ≥ 0.95; memory within budget; no contamination → **Pass**. Per the protocol's termination rule, the collection ran once and stops here.
