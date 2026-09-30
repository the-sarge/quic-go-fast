# BBRv3 causal diagnosis

**Date:** September 30, 2026, America/New_York. **Scope:** [Explain the dominant causes of the BBRv3 regression](https://github.com/the-sarge/quic-go-fast/issues/669), under [Wayfinder map: explain BBR performance and decide the next qualification path](https://github.com/the-sarge/quic-go-fast/issues/666).

This investigation uses finite source interventions at frozen component `e4f322cbbfd4225a4b714e08ec19c958cccadcb0` to distinguish transport service cost from controller policy. The owned evidence branch starts at reproduction asset `2383659ce0781b358fe098f32637d41767b7e9d0`, whose transport blobs match the component. Production source is restored byte-for-byte before the evidence commit; experimental source remains in linked patches and reconstruction aids.

## Intervention contract

- **Allocation:** replace only the temporary `NewBBRSender` construction inside packet-size refresh with the same validated initial-window arithmetic. Preserve all size, window, ProbeRTT, persistent-congestion and CE behavior. This exercises [Avoid per-opportunity BBR sender allocation during packet-size refresh](https://github.com/the-sarge/quic-go-fast/issues/630).
- **Recovery:** enumerate retained ACK keys only when that enumeration is no longer than the original outcome-ring scan, otherwise retain the original scan; skip PTO confirmation while no unresolved packet has ever been PTO-retired in the path ledger; skip persistent-span reduction while no outcome has ever been marked lost in the ledger. Conservative flags stay true until the existing reset. The 32768 outcome cap, full history, packet-number spaces, duplicate witnesses, late receipts, loss classification, recovery episodes and persistent-congestion rules remain present.
- **Separate recovery scans:** retain only one of those three service changes. These quantify conditional effects with the other two costs still present, not an additive allocation of the total regression.
- **Continuation:** permit at most eight individually gated emission opportunities before ordinary connection-loop service, stopping for pending receive work, close, errors, local capacity or a non-retry result. Every opportunity retains pacing, flight and byte-credit gates. This tests loop-return overhead without replacing socket ownership or enlarging queues. It remains experimental and requires separate design/review disposition before production use.
- **ECN reuse:** alternate two bounded marking-range slices instead of allocating a fresh reconstruction slice for each advancing ACK. Mark accounting, splits, counter validation, failure and compaction remain unchanged. This is a finite cost probe for [Assess bounded ECN ACK-processing cost before optimizing](https://github.com/the-sarge/quic-go-fast/issues/613): one owner, this exact pinned baseline, three observations per workload, sender/receiver CPU and allocation metrics, no paid allowance or campaign allocation.

The original Reno path and controller model are unchanged by the allocation and recovery interventions. STREAM and application DATAGRAM observations retain the campaign fixture's original payload encoding, verification, duplicate accounting, flow-control limits, control exchange and receiver useful-delivery boundary.

## Matched causal comparisons

The table reports median (minimum–maximum) from three observations per workload/condition in the rotated comparison blocks. All these observations have fixture tracing and experiment instrumentation disabled. Sender CPU is full-run process time per measured receiver useful GiB.

| Workload | Condition | Useful goodput, Mbit/s | Sender CPU, s/GiB | Sender allocated GiB/useful GiB | Sender/receiver peak RSS, MiB | Control p95, ms |
| --- | --- | --- | --- | --- | --- | --- |
| STREAM | Reno | 1408.3 (1364.6–1420.8) | 19.1 (18.9–19.7) | 0.151 (0.150–0.152) | 20.95/20.95 | 0.203 (0.191–0.217) |
| STREAM | Frozen BBRv3 | 344.1 (343.5–354.2) | 39.8 (39.3–40.0) | 1.310 (1.301–1.315) | 28.41/25.47 | 1.423 (0.917–1.996) |
| STREAM | Allocation only | 350.3 (347.5–353.0) | 39.5 (39.2–39.5) | 0.407 (0.404–0.408) | 28.14/25.31 | 1.632 (1.498–1.722) |
| STREAM | Recovery only | 1363.9 (1310.1–1426.3) | 19.6 (19.2–20.4) | 1.336 (1.315–1.341) | 28.98/29.20 | 0.286 (0.203–0.354) |
| STREAM | Allocation + recovery | 1385.5 (1364.8–1402.1) | 19.8 (19.5–20.2) | 0.436 (0.434–0.440) | 28.48/28.89 | 0.257 (0.233–0.331) |
| DATAGRAM | Reno | 1270.3 (1259.6–1307.0) | 21.5 (20.9–22.0) | 1.546 (1.534–1.558) | 21.39/22.05 | 0.416 (0.293–0.663) |
| DATAGRAM | Frozen BBRv3 | 262.4 (262.0–264.9) | 53.3 (52.4–53.4) | 2.862 (2.859–2.892) | 28.41/25.88 | 1.502 (1.402–1.604) |
| DATAGRAM | Allocation only | 259.0 (244.7–264.9) | 53.7 (52.6–56.5) | 1.914 (1.879–1.915) | 27.95/25.50 | 1.742 (1.610–1.792) |
| DATAGRAM | Recovery only | 1257.3 (1241.4–1306.5) | 23.8 (23.4–24.1) | 2.863 (2.855–2.867) | 28.70/29.83 | 0.201 (0.195–0.282) |
| DATAGRAM | Allocation + recovery | 1264.4 (1223.7–1280.6) | 23.6 (23.3–24.0) | 1.884 (1.864–1.895) | 28.88/29.94 | 0.287 (0.204–2.626) |

Full per-run absolute sender/receiver CPU, allocated bytes, RSS, control counts and paired ratios are in [structured results](summary.json). Endpoint peaks are reported separately; their sum would not be an observed simultaneous memory peak. The structured results also report combined endpoint CPU per useful GiB.

For STREAM, recovery alone increases median useful goodput by 1019.8 Mbit/s (3.96×) and closes 95.8% of the observed median goodput deficit to Reno. With allocation removal, the closure is 97.9%. These are local, median-based conditional contrasts, not universal effect sizes or an additive division of causes.

The allocation-only condition changes median STREAM goodput by +1.8%, while reducing normalized sender allocation by 69.0%. Thus the constructor is a demonstrated allocation cause; these repetitions do not demonstrate that it drives the large throughput/CPU regression.

For DATAGRAM, recovery alone increases median useful goodput by 994.9 Mbit/s (4.79×) and closes 98.7% of the observed median goodput deficit to Reno. With allocation removal, the closure is 99.4%. These are local, median-based conditional contrasts, not universal effect sizes or an additive division of causes.

The allocation-only condition changes median DATAGRAM goodput by -1.3%, while reducing normalized sender allocation by 33.1%. Thus the constructor is a demonstrated allocation cause; these repetitions do not demonstrate that it drives the large throughput/CPU regression.

The allocation-plus-recovery condition is not a ready candidate. Peak RSS exceeds Reno by more than 10% for both endpoints in every block. STREAM control p95 exceeds its matched Reno value by more than 20% in two blocks; DATAGRAM has one retained 2.626 ms outlier. One DATAGRAM goodput observation is 6.4% below matched Reno. Sender DATAGRAM CPU per useful GiB is near the 10% boundary and exceeds it in one block. Those limited local tails, remaining resource differences and the absent difficult-path benefit belong in the correction/readiness work, not an algorithm-suitability verdict.

## Separate scan and ECN probes

Three observations per workload/isolated intervention report conditional effects while the other recovery costs remain. These probes ran in a separate series from the rotated main comparison; small numerical contrasts with the main baseline are limited by host and phase variability. They are not a unique percentage allocation of the total regression.

| Workload | Isolated intervention | Useful goodput, Mbit/s, median (range) | Sender CPU, s/GiB, median (range) | Sender allocated GiB/useful GiB, median |
| --- | --- | --- | --- | --- |
| STREAM | ACK discovery | 516.8 (510.7–555.2) | 29.8 (27.7–32.2) | 1.317 |
| STREAM | Unused PTO scan | 374.0 (373.6–416.5) | 38.7 (33.4–39.1) | 1.319 |
| STREAM | Loss-free span scan | 378.8 (372.4–390.5) | 36.9 (33.8–38.5) | 1.306 |
| STREAM | ECN slice reuse | 317.2 (317.0–348.4) | 41.3 (37.4–43.8) | 1.212 |
| DATAGRAM | ACK discovery | 427.9 (419.7–466.2) | 39.2 (36.3–39.5) | 2.875 |
| DATAGRAM | Unused PTO scan | 317.5 (315.8–329.9) | 48.7 (45.4–48.8) | 2.877 |
| DATAGRAM | Loss-free span scan | 306.4 (290.1–313.5) | 48.9 (47.0–51.7) | 2.871 |
| DATAGRAM | ECN slice reuse | 255.2 (249.0–256.7) | 54.6 (54.3–55.8) | 2.790 |

ACK discovery has the largest isolated goodput effect. PTO confirmation and persistent-span reduction show smaller gains, and their isolated CPU effects vary. Removing all three costs gives a much larger joint gain than adding the isolated median differences; service timing, feedback cadence, scheduler work and host concurrency can interact. The subsequent collector records no batch calls in any condition, so no part of the observed joint gain is attributed to increased socket batching. The available experiment does not identify a unique additive partition of that joint gain.

ECN scratch reuse reduces normalized allocation by roughly 7–8% in STREAM and 2–3% in DATAGRAM compared with the main baseline medians, but does not consistently improve goodput or CPU. This establishes a smaller exercised allocation cost on this continuous loopback workload. It does not measure worst-case fragmented marking histories, justify a broad ECN optimization, or close the existing follow-up. The cause of the severe throughput regression is not assigned to ECN.

The one-observation-per-workload continuation screening gives 329.4/252.8 Mbit/s for STREAM/DATAGRAM, leaving the large symptom intact. Combining continuation with allocation/recovery gives 1257.2/1097.8 Mbit/s in its separate screen. There is no repeated matched evidence that continuation adds a useful benefit once recovery service is repaired; it remains an unselected experimental lead.

## Causal interpretation

The dominant reproduced goodput and sender-efficiency regression is avoidable recovery-history service work in the BBR transport path. The otherwise matched intervention retains the ring's evidence and changes how ACK discovery, unused PTO confirmation and loss-free persistent-span reduction are serviced; it leaves the sampler, model, pacing gains, congestion window, fixture and socket worker unchanged. The original and intervened reducers agree in finite differential cases spanning loss-free and mixed loss/PTO/disposal histories, ring eviction, spaces, huge ACK ranges and lifecycle resets. Existing recovery, BBR, ECN, bounded emission and local-credit checks pass for the affected experiments. The [finite differential oracle](recovery-equivalence_test.go.txt) is retained as text; copy it into `internal/ackhandler` only in an owned intervention worktree. The [idle oracle](idle-oracle_test.go.txt) similarly belongs in `internal/congestion` when invoked.

The frozen ACK path repeatedly visits up to 32768 outcome registrations in each of the three scans, including acknowledged history. Limiting each storage structure does not bound its cost to an affordable ACK budget. The individual-scan probes and profiles below distinguish this service mechanism from a mere source asymmetry. Its changed service timing may alter queue occupancy and model observations; those consequences do not mean a controller policy was tuned or substituted.

The constructor removal saves allocations and deserves its existing scoped follow-up, but its isolated throughput and CPU effects are small relative to the recovery intervention and do not consistently improve both workloads. The loop-continuation screening does not remove the large regression. It is not selected as a production correction by this ticket. No causal additive decomposition across allocation, service timing, scheduler work and sampling is asserted.

## Evidence boundaries

Every performance observation uses separate managed IPv4 loopback endpoint processes, GOMAXPROCS=4 each, Go 1.27.0, M=1400, disabled PMTU discovery, 5 seconds of warmup and 20 seconds of measurement. Full-run CPU and allocation totals are divided by measured receiver useful GiB and include setup/warmup/teardown. They are not pure steady-state costs. Each per-run control p95 is a nearest-rank statistic from 20 replies; observed tail variability is not a population guarantee.

The initial sanity observations overlap preparation and are excluded from attribution statistics. All builds and tests finish before the repeated comparison series. Controller/intervention order rotates across the three blocks, and workload order alternates. Measurements run sequentially on the same desktop host, with per-observation process samples retained. There is no exclusive core reservation, separate-host control, impaired path or platform generalization.

The finite diagnostics run separately from performance comparisons. They count loop opportunities, waits, stop reasons, socket submissions and bounded sampled queue residence; capture a bounded subset of real delivery snapshots and first-receipt events; and independently recompute delivery totals, anchor, interval, limited-marker propagation and rate. This verifies arithmetic and propagation from captured transport-owned inputs; the sampled subset does not independently certify the lifecycle classification that originally created each snapshot or every excluded receipt. Queue timestamps sample handoff-to-socket-call residence at one in 256 opportunities, not wire departure. Socket counters describe offered write attempts and batch entries; they do not substitute for a wire capture, and retries can contribute more than one attempt. Delivery captures select one in 128 feedback events, retain at most 256 events with at most 512 ACKed/lost values each, and omit later captures after that cap. Their stored time span is reported explicitly. Profiles are attribution evidence, not counterfactual throughput measurements.

## Delivery, pacing, handoff and model observations

The separate collector retains CPU/allocation profiles, a forced-GC live-heap profile at measured t=10 seconds, and bounded counters for both roles of Reno, frozen BBR and recovery-only BBR. These six profiled/instrumented observations are excluded from all causal performance tables. Every endpoint exited successfully and passed the same receiver checks.

All conditions record zero batch calls and one datagram per socket-write attempt. The recovery intervention therefore restores throughput without a batching change or a send-loop continuation change. The preliminary batching explanation is rejected by this direct measurement. Sender loop counts rise from 0.83/0.97 million in frozen STREAM/DATAGRAM to 3.48/4.79 million with recovery service changed; the gain is not obtained by reducing the total number of loop passes. The raw counts, attempts, progress, waits and stop distributions are retained in [diagnostic observations](diagnostics.json).

The sampled queue-residence distributions are concentrated below 100 microseconds in both frozen and intervened runs. They have limited slow observations, not a persistent millisecond-scale local queue. The observed pacing-stop counts are only 68/664 of 796331/690796 frozen STREAM/DATAGRAM send opportunities and 16/33 of 2816901/2852166 recovery-only opportunities. Opportunity counts are not time-weighted occupancy and do not independently prove the absence of brief timer or window stalls. They do not support a dominant low-token pacing explanation on this reproduction.

The actual recovery ledgers reach 32768 entries in all BBR sender observations, with hundreds of thousands to millions of evictions. Optional delivery evidence reports no missing, expired or lost evidence; all full-run feedback events in these collectors report valid delivery samples. The independent [sample recomputation](inspect-diagnostics.py) agrees on all 1585 retained sender/receiver events: delivered/lost totals, newest eligible anchor, send/ACK interval, limited-marker propagation, sample validity and integer rate. The bulk-sender capture caps fill early: their stored spans are 0.8–3.5 seconds and cover warmup, not the full measured interval. Full-run validity counters are separate aggregate observations, not an independent arithmetic check of every later event. This checks a bounded real-input oracle and does not certify all sampling lifecycle cases.

All four BBR sender/receiver pairs report zero sampled loss bytes, no missing delivery evidence, ECN accepted ECT(0) progression, zero accepted CE and no counter/evidence failure. Genuine-idle callbacks occur, especially on receivers, but none leaves nominal pacing at 1 B/s. The [frozen idle reducer oracle](idle-oracle_test.go.txt) independently reproduces nominal pacing 51364 → 1 B/s with unmeasured bandwidth, CE-driven phase exit and genuine idle. Thus [Preserve unmeasured BBR pacing fallback on genuine idle](https://github.com/the-sarge/quic-go-fast/issues/636) is a real pinned defect, but its required trigger is absent from these observed loopback runs. It is not assigned a contribution to this severe regression.

The four differences inventoried by [Check BBRv3 model fidelity and transport integration](https://github.com/the-sarge/quic-go-fast/issues/668) are not declared harmless. Loss-driven phase repair and Startup-loss capacity learning are not exercised by these loss-free observations; RTT-before-phase ordering and valid-sample round gating remain unchanged while the dominant deficit is removed. Their impaired-path relevance remains unresolved. Registration-time versus actual-departure timing, raw RTT, application/flow limitation, ProbeRTT and CE policy remain the accepted contract, not silently replaced by another implementation.

## Profiles and remaining memory cost

The new frozen sender CPU profiles attribute 20.87%/18.75% cumulative samples to `Conn.handleAckFrame` for STREAM/DATAGRAM; the recovery-only profiles show 0.60%/1.08%. `pthread_cond_signal` falls from 44.85%/46.12% flat samples to about 10.1%/9.4%. These profile shifts accompany the controlled source intervention; the scheduler label alone does not identify a specific syscall, pacing defect or wakeup contract to change.

Allocation profiles attribute 67.06%/34.51% of frozen sender sampled allocation bytes to `NewBBRSender`, consistent with the allocation-removal counterfactual. ECN `appendRange` accounts for 6.39%/3.15%; its scratch-reuse probe measures a smaller allocation contribution without a reliable throughput benefit. The latter is coverage of this continuous loopback history, not the existing follow-up's fragmented-ledger worst case.

Live-heap profiles still attribute several MiB to recovery-evidence registration: STREAM sender frozen/recovery-only 3.63/7.74 MiB, and DATAGRAM 5.17/5.68 MiB. Corresponding total sampled live heaps are 7.79/11.96 MiB and 9.24/8.19 MiB, versus Reno 4.69/3.66 MiB. These are forced-GC sampled live-heap observations during instrumented runs, not the unprofiled RSS metric. They establish the retained recovery ledger as a major live allocation site and explain why removing temporary constructor allocation does not eliminate the metadata footprint. They do not uniquely account for every byte of peak RSS or prove that a particular representation change will meet the memory limit. Peak RSS, receiver metadata cost, sparse control tails and the remaining DATAGRAM sender CPU margin need disposition in the correction scope.

A competent implementation appears feasible: preserving the selected model and transport evidence while changing affordable recovery service removes almost the entire local goodput deficit and most excess sender CPU/GiB. A ready candidate is not yet demonstrated. No difficult-path benefit, native two-host correction, impaired-path/model oracle coverage or supported-platform readiness is established.

## Preservation and handoff

The frozen campaign worktree, private native receipts, failed campaign attempts, paused cloud heartbeat and historical ledger are untouched. No paid experiment, production merge, default-controller change, controller-version switch or qualification verdict is authorized or performed by this diagnosis.

The correction choice belongs to [Choose the correction needed for a competent BBRv3 implementation](https://github.com/the-sarge/quic-go-fast/issues/670). Its matched readiness demonstration and any difficult-path benefit belong to later map tickets. Existing model-fidelity differences remain inputs to that choice rather than assumed causes of this reproduction.

## Assets and reconstruction

- [Structured observations, main comparisons, isolated probes and paired contrasts](summary.json).
- [Diagnostic counters and sample verification](diagnostics.json), [environment and premeasurement corrections](environment.json), [validation receipt](validation.json).
- [Verified raw archive](raw.tar.gz), [member hashes](raw-manifest.json), [archive receipt](archive-receipt.json). The archive holds all 74 observations, endpoint records/configs/commands/exits, host process samples, 36 profiles and summaries, build receipts and retained test logs; temporary credentials, binaries and the copied module are omitted.
- [Intervention source hashes](interventions.json), [finite intervention builder](build.py), [run aid](run.py), [finite observation ordering](matrix.py), [analysis aid](analyze.py), [diagnostic builder](build-diagnostics.py), [bounded instrumentation aid](instrument.py), [heap/profile fixture patch](fixture-heap-diagnostics.patch), [independent sample check](inspect-diagnostics.py). These are frozen one-investigation assets, not a maintained benchmark service.

Use a fresh owned feature worktree under `/Volumes/worktrees/quic-go-fast` with this evidence branch's frozen transport tree. Do not reuse the campaign worktree or overwrite retained observations. The aids write only their owned worktree and its `.local/bbr-causal-diagnosis`; the builder deliberately edits transport source for an experiment, so the initiating checkout is unsuitable.

For performance reconstruction, copy the campaign module's `go.mod`, `go.sum` and `fixture/` from `docs/audits/2026-09-25-bbrv3-q1-campaign` into `.local/bbr-causal-diagnosis/diagnostic-source`, preserving the relative replacement of the frozen root. Apply the inherited `docs/audits/2026-09-29-bbr-local-reproduction/fixture-diagnostics.patch` inside that copied module. Build all variants with `build.py`: `allocation`, `recovery`, `continuation`, `both`, `recovery-ack`, `recovery-pto`, `recovery-feedback`, `ecn-reuse`, `combined`, and finally `baseline` to restore source. Finish affected tests before measuring. `run.py baseline` generates fresh temporary credentials and the sanity series; run `matrix.py screen`, `ablation`, `factorial`, and `ablation-repeat` sequentially afterward. Every case refuses to replace an unsuccessful prior attempt and only reanalyzes an existing successful receipt. The initial sanity binary used the original fixture-diagnostics source label; its provenance and exclusion from causal statistics are retained separately.

For diagnostics, restore only the copied fixture's `main.go` from the original frozen campaign source, then apply `fixture-heap-diagnostics.patch` in that copied module. With transport source restored, run `build-diagnostics.py`, then `matrix.py diagnostics`. The builder preserves the measured performance binaries and restores transport files even if a diagnostic build fails. The heap collector forces GC at measured t=10 seconds, joins before exit, and keeps its overhead entirely in the six diagnostic observations. Those profile observations must not be pooled into performance comparisons.

Run `analyze.py` and `inspect-diagnostics.py` only against successfully retained observations. The original main comparison and separate-probe summaries regenerate exactly; all 843 archived member hashes were verified. Nine intervention patches apply cleanly to the frozen transport, the saved heap fixture patch reconstructs the measured copied source exactly, and the transport is byte-identical to the component at publication. The finite reducer oracles are archived as text so ordinary package discovery cannot accidentally execute them.
