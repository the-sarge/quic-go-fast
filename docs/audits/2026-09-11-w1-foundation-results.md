# W1 Windows message-I/O foundation noninferiority results

Results for the [W1 noninferiority protocol](2026-09-11-w1-foundation-protocol.md) (v2), Slice W1 of the [Windows datapath plan](../adr/2026-09-11-windows-datapath-plan.md). **Disposition: Pass.**

## Measured configuration

- **Candidate:** `206ac1e9` (PR #242 head; the last shipped-code change is `412c383e` — the two commits between it and the measured tree are the protocol/plan re-audit docs and the harness bind knob, no shipped code).
- **Measured tree:** scratch head `b609418b` = candidate `206ac1e9` plus one workflow-only commit (`.github/workflows/w1-measure.yml`, the collection driver; no Go code).
- **Collection run:** GitHub Actions run 34665708932 (workflow `w1-measure`, branch `scratch/w1-measure`), one hosted job, no concurrent steps. Raw log archived as the run's `w1bench-results` artifact; the analysis script output below is reproducible from it with seed 20260911.
- **Host:** GitHub-hosted `windows-latest` runner — Windows build 10.0.26100, 4 CPUs, `GOMAXPROCS=4`, go1.27.1, windows/amd64. CPU affinity unavailable (recorded as absent per protocol).
- **Workload:** 512 MiB of 1071-byte records per invocation; 11 rounds (round 0 discarded warmup), all four cells per round, order alternating by round parity, fresh process per invocation.

## Official v2 collection

| Family | Paired ratio (geo mean) | 95 % bootstrap CI | Bound | Outcome |
|---|---|---|---|---|
| swap (loopback bind, identical work) | 0.9894 | [0.9798, 0.9996] | lower bound ≥ 0.95 (gate) | pass |
| parity (wildcard bind, packet info active) | 0.9538 | [0.9472, 0.9617] | lower bound ≥ 0.85 (stop floor; reported cost) | pass |

- Swap cells: candidate median 192.5 MB/s (182.2–197.7), baseline median 194.4 MB/s (186.9–200.3) — stable, no contamination.
- Parity cells: candidate median 187.9 MB/s (185.5–190.3), baseline median 196.8 MB/s (190.5–200.0) — stable, no contamination.
- Memory: candidate median peak working set within 0.3 MB of baseline in both families (budget +8 MiB).
- Cell integrity: concrete conn types asserted per cell; every parity-candidate invocation observed populated packet info on ~513–517 K reads (≈ every data packet); swap-family candidates observed zero, as their loopback bind requests none.

**Mechanical disposition (protocol v2 rule): Pass** — swap CI lower bound 0.9798 ≥ 0.95, parity CI lower bound 0.9472 ≥ 0.85, memory within budget, no contamination.

The measured parity-family cost — about 4.6 % on this collection — is the published price of the packet-info parity deliverable on wildcard-bound sockets (per-read control-message parse plus per-send `IP_PKTINFO`), the same per-packet cost model the OOB platforms carry by design. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## History (not official v2 evidence)

Both prior datasets were observed before v2's bounds were finalized and are published here for provenance only, per the protocol's amendment note.

- **v1 collection** (run 34652516474, measured tree `3f32c40d` = candidate `412c383e` + workflow-only commit): single wildcard cell pair; ratio 0.932, 95 % CI [0.923, 0.939]; mechanically inconclusive under v1's single-family rule (pass ≥ 0.95, fail < 0.90). This result triggered the scoped re-audit recorded in PR #242.
- **Diagnostic collection** (run 34665436778, measured tree `4f5da009`, labeled diagnostic): loopback-bound cell pair; ratio 0.9859, 95 % CI [0.9823, 0.9890] — decomposing v1's deficit into ~1.4 % swap cost and ~5.4 % parity cost on that runner.

Absolute throughput differs across collections (~128–197 MB/s baselines) because hosted runners differ; the paired within-round design is what each disposition rests on.

## Termination

The fixed collection ran once and passed. Per the protocol, no repeated collection, workload widening, extra platforms, or additional scope follows.
