# E1 four-core replacement feasibility

**Disposition: inconclusive; no runtime adoption.** The complete four-core replacement establishes the agreed noninferiority margins for P3/P4/P5/P6, but P1's successful-probe p99 bound and the existing stream-churn time bound exceed 1.05. Every qualifying capture completed successfully; the two missed bounds are statistical qualification failures, not discarded samples or diagnosed regressions of that size. The revised interface and focused allocation provenance are complete under the finite E1 contract. This receipt exhausts the remaining campaign and diagnostic budgets authorized by the [resumption contract](../../adr/2026-09-07-packet-emission-plan.md#e1--establish-packet-emission-feasibility) and [resumption receipt](../2026-09-07-emission-four-core-resumption.md). The [original E1 receipt](../e1-emission/README.md) remains unchanged. E2 remains independently ready; E3–E6 remain blocked pending a new feasibility decision. Two-core performance has no acceptance role.

## Frozen runtime revision

The matched baseline is `e742e3ee64c79c61440061a86f56e2e223a776ef`; its production Go source and module files match original baseline `e90617366674535bcefa8f90a2e92b153be1342b`. The revised prototype is `8e61c8e8b9997204316d6009b9065943a5e8b82f`, archived as [an inert patch](prototype.patch) relative to the new baseline. Apply it only in a dedicated experimental worktree at that baseline after `git apply --check`; normal builds never select it. The endpoint source is reused byte-for-byte from [the original archive](../e1-emission/endpoint.go.source).

The same embedded emission owner and five narrow synchronous policy calls are retained. The result now carries progress, distinct stop reasons, a deadline and a queue availability channel through `triggerSending` to the connection loop. Progress avoids a redundant post-send capacity check only when established emission queued nothing; the sole producer is the connection, and the worker can only reduce occupancy. The loop rechecks capacity after progress to preserve its existing behavior if the worker drains before the loop observes the result. It uses the returned channel when present, retaining the legacy lookup for unmigrated paths and final GSO stop conditions that did not encounter an explicit queue-full branch. The initial run-loop capacity guard still precedes handshake fallback. These are bounded preserved checks, not a second capacity ledger.

No-data, queue-full, pacing, receive-yield, congestion/ACK-only recovery, hard-blocking and pending-probe outcomes are distinct. The connection consumes the deadline for pacing/receive yield and retains its original post-send timer policy for other outcomes; top-level recovery dispatch still owns ACK/PTO and blocked timers. The prototype does not migrate handshake, probes or close. A test-only `sendPackets` adapter keeps the original allocation fixture identical across variants while production consumes `emitPackets` results.

The new result tests failed first for absent capacity wakeup, absent distinct stop values and the connection's error-only return. After the revision, the result tests and retained real-queue resume/receive/output characterizations passed, as did focused race checks, the full Go suite and vet with Go 1.27.1 on darwin/arm64. Linux checks and exact measurement provenance are recorded separately; these observations do not certify a future changed head.

## Source and outcome correspondence

The following map describes the frozen revised prototype, not the merged production tree. The [original characterization domain](../e1-emission/README.md#characterization-domain) is retained; new result tests extend that same real-packer/recovery/queue seam in the inert patch.

| Authoritative baseline source or state | Revised experimental binding | Preserved behavior |
| --- | --- | --- |
| `Conn.sendPacketsWithoutGSO` / `sendPacketsWithGSO` | `packetEmission.withoutGSO` / `withGSO` | Same packing, recovery queries, ECN and GSO segment boundaries for established ordinary output. |
| `Conn.appendOneShortHeaderPacket` | `packetEmission.appendPacket` | Logging, activity timestamp, recovery registration and connection-ID notification remain synchronous and precede queue submission. |
| `Conn.packer`, `sentPacketHandler`, `sendQueue` | Typed pointers to the same authoritative interface slots | No copied mutable state; existing test substitution and path replacement still address those slots. |
| Both connection constructors | `bindPacketEmission` after packer initialization | One embedded value with the connection version and one narrow policy interface. |
| MTU, logging, idle activity, connection-ID registration, receive-pending policy | Five methods on `emissionPolicy` | Same connection-owned policy effects and receive mutex, with no unrestricted connection API in the module. |
| Recovery pacing deadline and receive yield | `emissionResult.deadline` plus distinct stop reason, consumed by `Conn.emitPackets` | Existing pacing calculation and immediate-retry sentinel; other stops preserve the original post-send timer policy. |
| Queue capacity and worker wakeup | `emissionResult.available`, propagated through `triggerSending` and consumed by `Conn.run` | The existing channel signals worker progress; capacity is rechecked after a send, with the retained fallback described above. |
| Established send progress | `emissionResult.progress`, consumed by `Conn.run` | No-progress established emission skips only the redundant post-send capacity query; legacy paths retain it. |
| Top-level handshake, ACK/PTO, probes and close | Existing connection methods and explicit legacy result marker | Their ownership and execution remain outside the extraction. Successful recursive PTO dispatch marks preceding progress. |

Queue-result tests cover ordinary/GSO emission with seven or eight occupied slots, distinguish progress from no progress, and observe wakeup after draining the actual queue. Recovery-result tests cover congestion/ACK-only, hard blocking, pacing and pending probes after a real packet registration. Connection-result tests cover empty output and receive-yield progress with the immediate-retry deadline. These are the finite outcome regressions for this revision; they do not establish later-mode migration or repair the queue-lifetime obligations assigned to E2.

## Bounded churn diagnosis

The [diagnostic archive](churn-diagnostics.tar.gz) retains the four permitted unprofiled pairs, one CPU profile per original variant, exact invocations and CPU observations. The unprofiled candidate/base time ratios were 1.0269, 0.9204, 1.1500 and 1.0634, with geometric mean 1.0369. They reproduce variability and a similar central estimate to the original result; they are not acceptance samples.

The ranked hypotheses were allocation/GC cost, additional emission-call CPU cost, and scheduling/socket variability. Whole-process CPU profiles show background GC at 44.4% of baseline samples and 43.0% of candidate samples, with stream-map shutdown at 13.0% and 16.5%. The send-packet subtree accounted for 3.75% and 2.75%, respectively. These profiles include setup and teardown outside the benchmark's timed loop, and the profile runs differ in iteration count; percentages cannot establish a cause of the original timed difference. No emission-call cost increase was demonstrated by these samples, and blocked scheduling delay is not established by CPU profiles. The cause remains unresolved; no speculative churn-specific runtime fix is included.

Both diagnostic phases completed successfully. Measured cores had zero sampled guest CPU; maximum mean SMT-sibling busy time was 0.0167% for unprofiled diagnostics and 0.0827% for profiles. Temporary CPU restrictions were restored after each phase. The ten-second profile setting controls the benchmark timed loop; process profiles also include its subsequent cleanup and therefore span longer than ten seconds. The fixed diagnostic allowance is exhausted and is not extended.

## Native build and allocation checks

[Build provenance](build-receipt.json) binds native Go 1.27.1, empty GOFLAGS/GOEXPERIMENT, source commits, the identical allocation-fixture hash and endpoint/benchmark/allocation binaries. The baseline Linux full suite failed only `TestMITCorruptPackets/towards_the_client` at `integrationtests/self/mitm_test.go:218`, when its randomized-corruption connection setup reached the one-second Dial deadline. This is the same unresolved observation retained in the earlier E1 history, now observed on unchanged baseline runtime. It was not rerun or treated as passing; the build resumed only the remaining independent checks. Baseline focused race checks and vet passed. The revised candidate's full Linux suite, focused race checks and vet passed.

The one focused allocation invocation per variant recorded 37 allocations per ordinary batch and 104 per three-packet GSO batch on both variants. Connection size is 1168 bytes on baseline and 1216 bytes on candidate, a 48-byte embedded setup increase; no separately allocated module or changed cold-close payload is introduced. These fixture-inclusive observations are not a claim that native packet emission is allocation-free. The retained allocation manifest binds commands, environment, source and fixture hashes, compiled binary hashes, Go patch version, CPU placement, output and exits. `testing.AllocsPerRun(1000)` temporarily forces GOMAXPROCS=1 internally; the recorded outer process budget is four, identically for both variants.

The [preflight archive](preflight.tar.gz) retains allocation logs/manifests, one paired three-second setup run for each of the five four-core cells and the separate qlog-on paired P3 diagnostic. All setup samples passed the frozen payload, timeout, ledger, capability and host checks. Qlog-on missed schedules remain recorded and are not included in the qlog-off qualification. [Build logs](build-logs.tar.gz) retain the failed baseline suite as well as the passing remaining checks. The [host receipt](host.json) binds machine/toolchain identity and capture-source/binary hashes before the replacement campaign.

## Four-core qualification

The complete replacement ran on minimax from 14:39 to 16:26 UTC on 2026-09-07: five cells, ten adjacent alternating baseline/candidate pairs per cell, 2 s warmup, 60 s measurement and 1 s drain. All cells use four physical cores and GOMAXPROCS=4 per endpoint, with client cores 8–11, server cores 12–15 and unused SMT siblings 24–31. Other user/system/VM work was restricted to cores 0–7 and 16–23 during capture. Separate endpoint processes communicate over loopback UDP, using QUIC v1 and qlog off; GSO is enabled except P4. These observations do not qualify a physical link or non-Linux performance.

All 100 samples passed payload, duplicate, probe-timeout, ledger, GSO-capability and host checks. The [host summary](campaign-02/host.json) records zero sampled guest CPU and maximum per-run mean SMT-sibling busy time of 0.13984%, below the frozen 0.1% guest and 1% sibling limits. The [restoration receipt](campaign-02/restoration.json) records exit 0, initially unrestricted CPU settings restored, reservation marker removed and backup timer stopped. The [manifest](campaign-02/manifest.json) binds workload, source and launcher identity; the [raw archive](campaign-02/raw.tar.gz), SHA-256 `d511a7617f6ea0fffe9b65eafc6f3deb14ca4fb379cd3af180731daa3f2f11f8`, retains every sample, invocation, endpoint log, CPU observation, capture log and restoration record.

The [paired analysis](campaign-02/summary.json) uses the unchanged [original analyzer](../e1-emission/analyze.py): 20000 whole-pair bootstrap resamples, seed 20260907, geometric paired ratios and one-sided 95% percentile bounds. A whole run is the experimental unit. No original-candidate or two-core sample is pooled with this campaign. To recompute, extract the raw archive into an empty directory and run `python3 analyze.py /path/to/extracted/campaign-02 --output /path/to/summary.json`; only the reported extraction path changes.

| Cell, four cores per endpoint | Goodput, base / candidate Gbps | Goodput lower bound | CPU/unit upper bound | Bytes/unit upper bound | Probe p99 upper bound | Missed/failed upper difference, percentage points | Gate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| P1: DATAGRAM 1 Gbps | 0.996 / 0.997 | 1.0000 | 0.9950 | 1.0004 | 1.0793 | 0 | Not established |
| P3: DATAGRAM 4 Gbps | 3.724 / 3.727 | 0.9994 | 0.9996 | 1.0012 | 0.9353 | 0 | Pass |
| P4: DATAGRAM 4 Gbps, GSO disabled | 3.796 / 3.795 | 0.9989 | 1.0012 | 1.0010 | 0.9991 | 0 | Pass |
| P5: one continuous stream | 13.247 / 13.231 | 0.9973 | 1.0099 | 1.0004 | 1.0357 | 0 | Pass |
| P6: sixteen continuous streams | 10.920 / 10.912 | 0.9982 | 1.0066 | 1.0018 | 1.0306 | 0 | Pass |

Goodput values are geometric means of whole runs. Bounds are candidate/base ratios except the last numeric column. Passing requires goodput at least 0.95, CPU/allocated bytes/probe p99 at most 1.05 and missed/failed proportion difference at most +0.1 percentage point. P1's geometric p99 ratio is 0.9494, while its upper bound is 1.079320; the central estimate is lower, but the ten pairs do not exclude more than 5% worse p99. P5, which missed its historical two-core margin, passes the current four-core gate at 1.035728. This does not retroactively change any historical receipt.

### Traffic ledgers and probe p50

The endpoint and counter definitions are unchanged from the [original capture counters](../e1-emission/README.md#capture-counters). DATAGRAM rate labels describe scheduled offered traffic; admission is a successful `SendDatagram` return, and fixture-queue overflow before that call is ingress drop. Mean baseline ingress-drop fractions are 0.37% in P1, 6.90% in P3 and 5.09% in P4. These are pre-admission fixture drops, not transport loss or independent identification of a bottleneck. Reliable-stream ledgers close on accepted and delivered bytes.

The table contains arithmetic means across ten whole runs per cell/variant. DATAGRAM ledger units are 1071-byte messages; stream units are bytes. Probe p50 is the mean of the ten successful-probe run medians, not a pooled-packet percentile. CPU and allocated bytes include the fixture and both endpoint processes from measurement start through drain completion.

| Cell / variant | Offered | Admitted | Delivered | Ingress drop | Post-admission drop | Probe p50, ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| P1 base | 7,002,801.0 | 6,976,888.3 | 6,976,888.3 | 25,912.7 | 0.0 | 0.032 |
| P1 candidate | 7,002,801.0 | 6,980,342.3 | 6,980,342.3 | 22,458.7 | 0.0 | 0.032 |
| P3 base | 28,011,204.0 | 26,078,201.9 | 26,078,070.2 | 1,933,002.1 | 131.7 | 1.157 |
| P3 candidate | 28,011,204.0 | 26,096,402.5 | 26,096,308.1 | 1,914,801.5 | 94.4 | 1.021 |
| P4 base | 28,011,204.0 | 26,585,278.0 | 26,585,212.5 | 1,425,926.0 | 65.5 | 0.444 |
| P4 candidate | 28,011,204.0 | 26,575,763.2 | 26,575,688.7 | 1,435,440.8 | 74.5 | 0.423 |
| P5 base | 99,351,540,531.2 | 99,351,540,531.2 | 99,351,540,531.2 | 0.0 | 0.0 | 0.198 |
| P5 candidate | 99,235,502,489.6 | 99,235,502,489.6 | 99,235,502,489.6 | 0.0 | 0.0 | 0.206 |
| P6 base | 81,903,616,000.0 | 81,903,616,000.0 | 81,903,616,000.0 | 0.0 | 0.0 | 0.249 |
| P6 candidate | 81,843,539,148.8 | 81,843,539,148.8 | 81,843,539,148.8 | 0.0 | 0.0 | 0.253 |

## Existing benchmark comparison

The existing handshake/churn/transfer benchmarks ran from 16:26 to 16:28 UTC in ten adjacent alternating pairs with one-second `-benchmem` samples, native Go 1.27.1 and GSO enabled. Their existing shared-process shape uses GOMAXPROCS=4 and four physical cores (8, 9, 12, 13) for the whole process, with SMT siblings idle. This is separate from the matrix's per-endpoint budget. The [manifest](bench-02/manifest.json), [completion](bench-02/completion.json), [paired analysis](bench-02/summary.json), [host observations](bench-02/host.json) and [restoration](bench-02/restoration.json) bind the comparison. All twenty benchmark subprocesses exited successfully.

| Existing benchmark | Geometric time ratio | One-sided 95% upper bound | Mean B/op, base / candidate | Mean allocs/op, base / candidate | Time gate |
| --- | ---: | ---: | ---: | ---: | --- |
| Handshake | 1.0012 | 1.0032 | 250529.8 / 250596.3 | 2080.9 / 2081.4 | Pass |
| Stream churn | 1.0329 | 1.0808 | 3246.2 / 3226.9 | 38.8 / 38.6 | Not established |
| Transfer 500 KiB | 1.0028 | 1.0056 | 371271.9 / 371625.0 | 4031.3 / 4032.4 | Pass |
| Transfer 50 MiB | 0.9966 | 0.9989 | 4818519.8 / 4810223.4 | 159514.3 / 159416.0 | Pass |

Stream churn's central time ratio is 1.0329 and its upper bound is 1.080832, so the unchanged 1.05 time margin is not established. This result does not identify a cause; the bounded earlier diagnosis remains unresolved. The allocation columns are means of existing benchmark outputs, separate from focused packet/batch allocation checks. The [raw archive](bench-02/raw.tar.gz), SHA-256 `682b6d864f9d51cb9932af7679d0ca99a067ac18597b4cc42c9caf6aab1eec4f`, retains all logs and the passive CPU monitor. The 144 complete monitor intervals inside the timestamped benchmark capture show zero guest CPU and maximum mean sibling busy time of 0.055%, within the unchanged host thresholds. The monitor ran outside the measured cores; startup and post-restoration intervals are excluded only from host aggregation, not benchmark samples. Recompute with `python3 analyze.py /path/to/extracted/bench-02 --benchmarks --output /path/to/summary.json`.

## Terminating disposition and review

The revised concrete result interface and complete allocation provenance address the authorized gaps, and P3/P4/P5/P6 pass at the intended four-core budget. Overall feasibility remains inconclusive because P1 p99 and stream churn do not establish their agreed margins. All finite diagnostic, setup, focused-allocation and qualifying samples are retained. The replacement allowance is exhausted; no third campaign, changed margin or runtime adoption is implied. E2 remains the independently ready frontier. E3 and its descendants require a new feasibility decision before runtime migration can proceed, in addition to their existing slice dependencies.

The one remaining fresh replacement review covers this receipt and the inert patch under the accepted E1 contract. Review dispositions and final exact-head local/hosted certification are recorded on [PR #37](https://github.com/the-sarge/quic-go-fast/pull/37); no receipt claims a future head has already been certified.
