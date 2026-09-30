# Local BBRv3 regression reproduction and profiling

**Observed:** September 29–30, 2026, America/New_York. **Scope:** [Reproduce and profile the BBRv3 regression locally](https://github.com/the-sarge/quic-go-fast/issues/667), one child of [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666).

The frozen workload reproduces a large BBRv3 regression on one macOS host using separate managed IPv4 loopback endpoints. It remains reproducible with fixture tracing disabled and with four additional busy CPU processes. This supplies a short receiver-defined measurement loop and profiles for causal diagnosis. It does not identify the dominant cause, select a correction, or decide algorithm suitability.

## Matched results with tracing disabled

Each cell reports the median and observed minimum–maximum across three pairs. Controller order alternates between pairs. Each run has 5 seconds of warmup and 20 seconds of measurement; STREAM and application DATAGRAM results stay separate.

| Workload | Controller | Useful goodput, Mbit/s | Sender CPU, s/GiB | Receiver CPU, s/GiB | Control response p95, ms |
| --- | --- | --- | --- | --- | --- |
| STREAM | reno | 1236.2 (1223.8–1241.4) | 21.3 (21.0–21.5) | 20.7 (20.5–20.8) | 0.237 (0.214–1.032) |
| STREAM | bbrv3 | 327.4 (321.6–331.7) | 42.5 (42.2–42.9) | 20.2 (20.1–20.5) | 1.528 (1.340–1.585) |
| DATAGRAM | reno | 1114.0 (1097.1–1116.5) | 24.5 (23.6–25.3) | 22.9 (22.0–23.6) | 0.296 (0.216–0.515) |
| DATAGRAM | bbrv3 | 248.3 (240.0–267.7) | 56.5 (52.3–58.1) | 24.6 (23.5–24.7) | 1.296 (1.280–1.433) |

All six tracing-disabled pairs exceed the parent map’s goodput and sender CPU regression limits. Paired BBRv3/Reno goodput ratios are 0.259–0.271 for STREAM and 0.215–0.244 for DATAGRAM; sender CPU/GiB ratios are 1.96–2.03 and 2.13–2.39 respectively. These are symptoms requiring diagnosis, not evidence against a competent BBRv3 algorithm.

CPU and allocation costs use full-run process totals divided by useful GiB consumed during the fixed measurement window, matching the retained campaign convention. They include warmup, setup and teardown; they are not pure steady-state costs. The retained native-path cases have a different 30/180-second warmup/measurement ratio, so absolute CPU/GiB values across those topologies are not directly interchangeable. One-second CPU/heap samples are retained; the analysis does not extrapolate a missing final CPU sample.

Absolute resource totals below are medians for the same tracing-disabled runs. Peak RSS is observed separately for each process, rather than a simultaneous combined peak.

| Workload | Controller | Sender/receiver CPU, s | Sender/receiver peak RSS, MiB | Sender/receiver allocated, GiB | Sender/receiver allocated GiB per useful GiB |
| --- | --- | --- | --- | --- | --- |
| STREAM | reno | 61.40/59.32 | 21.17/21.09 | 0.44/0.91 | 0.15/0.32 |
| STREAM | bbrv3 | 32.43/15.34 | 28.20/25.66 | 0.99/0.70 | 1.31/0.91 |
| DATAGRAM | reno | 62.67/58.56 | 21.14/21.56 | 4.02/4.36 | 1.55/1.68 |
| DATAGRAM | bbrv3 | 32.58/14.20 | 27.97/26.09 | 1.67/1.47 | 2.89/2.55 |

Each tracing-disabled run has 20 measured control replies with zero skipped, unresolved or missed opportunities. The p95 is a nearest-rank statistic from just 20 replies per run; the table reports medians of those per-run p95s and their variability, not a pooled tail estimate or a population guarantee.

## Tracing and CPU-load sensitivity

| Condition | Pairs per workload | STREAM Reno/BBRv3, Mbit/s | DATAGRAM Reno/BBRv3, Mbit/s |
| --- | --- | --- | --- |
| Unmodified frozen fixture | 3 | 1271.3/322.1 | 1126.3/259.9 |
| Diagnostic binary, original tracing enabled | 1 | 1252.5/316.4 | 1105.7/246.6 |
| Same diagnostic binary, tracing disabled | 3 | 1236.2/327.4 | 1114.0/248.3 |
| Tracing disabled, four added CPU workers | 3 | 1335.5/336.5 | 1197.3/258.7 |

The diagnostic patch only adds fixture tracing/profile flags and records their activation. It changes no transport implementation or payload accounting. The original-tracing diagnostic pair agrees with the frozen-source sanity runs. Disabling tracing substantially reduces allocation totals, especially for Reno STREAM, but leaves the large goodput and sender CPU/GiB gap. The one tracing-enabled diagnostic pair and changing ambient activity limit precise attribution of the tracing effect.

With four additional bounded busy processes, paired goodput ratios are 0.243–0.261 for STREAM and 0.212–0.219 for DATAGRAM. The gap persists under this added load. This does not isolate the effect of removing desktop contention: background activity changes between phases, no CPU affinity or exclusive core reservation is established, and all endpoint processes share one host. Absolute goodput actually increased in this phase, so it would be unsupported to claim that more contention improved either controller. Owned workers were stopped and their cleanup receipt is retained.

The initial frozen sanity series overlaps a fixture build/test in some runs, documented by per-run host process samples. The later tracing-disabled, profile and added-load series run after that fixture test completes. Other desktop and unrelated test activity is recorded rather than terminated. A fully quiet, separate-host comparison remains a coverage gap.

## Profile evidence and next discriminating work

CPU and sampled allocation profiles are captured separately for both endpoints, both controllers and both workloads, with tracing disabled. Those four profiled runs are excluded from the baseline tables. Profile time covers the full process session and final allocation-accounting GC; the GC occurs after resource snapshots and workload completion. Sample percentages are attribution leads, not counterfactual performance measurements.

- BBRv3 sender STREAM: `Conn.handleAckFrame` accounts for 27.90% cumulative sampled CPU; `recoveryEvidence.ack`, `confirmPTO` and `feedback` are visible costs. `runtime.pthread_cond_signal` accounts for 37.11% flat samples. DATAGRAM has corresponding values of 26.29% and 39.40%. Scheduler/system-call labels are not proof of a particular pacing defect.
- `NewBBRSender` accounts for 665.05 MiB / 67.86% of sampled sender allocation bytes in STREAM and 603.01 MiB / 34.27% in DATAGRAM. Caller profiles attribute those sampled allocations to `SetMaxDatagramSize`, establishing that [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630) is exercised and a substantial allocation source here. Its effect on goodput, CPU or RSS still requires an otherwise matched intervention.
- DATAGRAM sender `SendDatagram` accounts for 48.02% of sampled allocation bytes. Receiver and Reno profiles are retained to distinguish workload/fixture cost from BBR-specific cost. Payload encoding, verification and duplicate accounting remain enabled in every comparison.

The ranked probe hypotheses before the sensitivity work were transport/pacing integration cost, tracing cost, fixture processing/allocation cost, and host contention. Tracing is unnecessary for reproducing the symptom. Added CPU load does not erase it. Recovery-evidence scans, wakeup behavior and packet-size-refresh allocation are now measured leads for the existing causal-diagnosis ticket; source inspection and a single profile cannot rank their throughput impact. Model-fidelity investigation is still required. Idle fallback and ECN optimization leads are not established as causes by these continuously offered, unimpaired workloads.

## Provenance, limits and artifacts

Component source is `e4f322cbbfd4225a4b714e08ec19c958cccadcb0`; campaign documentation/history is `a4ecfc8de39eaa673755bf864fb7fdefbc109e80`. Toolchain is Go 1.27.0, macOS 27.0 build 26A428, Darwin/arm64, Mac16,5 with 16 logical CPUs and 128 GiB RAM. Each separate endpoint process uses GOMAXPROCS=4, direct managed IPv4 packet I/O, initial packet size 1400, disabled path MTU discovery, and the original receive-window/control settings. STREAM payload records are 16384 bytes and DATAGRAM records 1200 bytes; their 16-byte integrity header is excluded from useful delivery.

All 44 local observations have successful endpoint exits, matching actual controller identity, receiver/sender receipt equality, zero payload corruption and duplicates, and exact agreement between per-second useful bytes and total useful bytes. These are fixture integrity checks. Tracing-disabled loss/ECN observations are unavailable, represented as unknown rather than zero. No impaired path, difficult-path benefit, native two-host behavior, ECN response, loss recovery, platform qualification or candidate readiness is established here.

The [retained S1 summary](historical-s1-summary.json) extracts the five matched native-path pairs for each workload with original receipt hashes. Median native-path Reno/BBRv3 goodput was 1636.9/307.1 Mbit/s for STREAM and 1450.7/218.5 Mbit/s for DATAGRAM. Original private receipts, failed campaign attempts and campaign accounting were read without modification; no paid resources or cloud campaign were launched.

One analysis attempt failed because it demanded a final bracketing CPU sample that the fixture does not guarantee. The endpoint run succeeded; its original records were preserved and reanalyzed using the campaign full-run metric, without rerunning the endpoints. This failure is recorded in the environment asset. No product correction was attempted.

- [Environment and binary provenance](environment.json), including binary hashes, performance environment, the patch hash and successful diagnostic-fixture tests.
- [Structured results and paired ratios](summary.json), [raw file hashes](raw-manifest.json) and [verified archive receipt](archive-receipt.json).
- [Raw observations and profiles](raw.tar.gz): endpoint JSON, configs, commands, exit codes, host process samples, CPU/allocation profiles, profile summaries, and worker cleanup. Temporary certificates and binaries are omitted.
- [Finite reproduction aid](run.py), [analysis aid](analyze.py) and [fixture-only diagnostic patch](fixture-diagnostics.patch). These are frozen investigation assets, not a maintained benchmark service.

## Reproduction

Use a fresh owned feature worktree of this evidence branch under `/Volumes/worktrees/quic-go-fast`; its transport tree remains the frozen component source. Run one phase at a time. Each phase refuses to overwrite endpoint observations, and only successfully retained runs can be reanalyzed.

Build the original fixture from `docs/audits/2026-09-25-bbrv3-q1-campaign` with `GOTOOLCHAIN=go1.27.0 go build -trimpath -ldflags "-X main.sourceRevision=e4f322cbbfd4225a4b714e08ec19c958cccadcb0" -o ../../../.local/bbr-local-reproduction/frozen-fixture ./fixture`. Keep `.local` ignored because it holds temporary credentials and binaries. From the worktree root, `python3 docs/audits/2026-09-29-bbr-local-reproduction/run.py frozen` reproduces the unmodified fixture comparison.

For diagnostics, copy that campaign module’s `go.mod`, `go.sum` and `fixture/` directory into `.local/bbr-local-reproduction/diagnostic-source/`; this preserves the module’s relative replacement of the frozen root. Apply the saved patch inside that copied module with `patch -p1`. Build there using the same toolchain and flags, `-X main.sourceRevision=e4f322cbbfd4225a4b714e08ec19c958cccadcb0+fixture-diagnostics`, and output `../diagnostic-fixture`. The copied fixture tests passed with `GOTOOLCHAIN=go1.27.0 go test ./fixture` in 81.776 seconds. Complete tests before collecting measurements.

From the root, run the same `run.py` command with `trace-on`, `trace-off`, `profile` and `contention` in order. The first use generates fresh temporary mTLS credentials. The contention phase owns four busy processes with a 480-second upper bound and joins them at cleanup. Finally run `analyze.py` to validate the 44-run inventory and regenerate the structured comparison. All retained unprofiled pairs must reproduce goodput below 95% of Reno and sender CPU/GiB above 110% of Reno; that is the current failure signal for subsequent diagnosis, not a regression test for a selected fix.
