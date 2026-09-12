# W1 Windows message-I/O foundation noninferiority protocol

Precommitted measurement protocol (v2) for Slice W1 of the [Windows datapath plan](../adr/2026-09-11-windows-datapath-plan.md) (Track W, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the W1 implementation branch before its official collection run; the results document records the exact candidate head measured.

**v2 amendment (pre-collection for this version):** v1 defined a single wildcard-bound cell pair. Its one collection returned a mechanically inconclusive 0.932 throughput ratio, and a labeled diagnostic decomposed that into ~1.4 % message-I/O swap cost (loopback-bound cells, 0.986, passing v1's bound) plus ~5.4 % packet-info parity cost — per-packet control-message work that is a required W1 deliverable with the same cost model the OOB platforms already pay by design, not a regression of preserved behavior. v2 therefore gates noninferiority on the preserved-behavior (swap) configuration and separately reports the parity configuration against a stop floor. The v1 collection and the diagnostic are published in the results document as history; neither is official v2 evidence, and v2's official collection is one fresh run.

## Question

Does replacing the plain-socket Windows datapath (`basicConn`, `ReadFrom`/`WriteTo`) with the message-I/O conn (`windowsConn`, `ReadMsgUDP`/`WriteMsgUDP` with a control buffer) preserve bulk-transfer throughput and memory on Windows when both paths do identical work, and what does the packet-info parity path cost where it activates? W1's gate is correctness and noninferiority only: a behavior-preserving rebuild claims no syscall reduction, so no engagement or syscall metric applies — the performance gates belong to USO (W2) and URO (W3).

## Artifact classes

The measurement harness (`w1_measurement_test.go`, build tag `w1bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the W1 conn swap and control-message code.

## Fixed configuration

- **Base commit:** `8acce322` (default-branch HEAD at branch creation; `basicConn` Windows datapath).
- **Candidate:** the W1 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's preserved `basicConn` path, constructed exactly as the base commit's Windows `newConn` constructed it (`&basicConn{PacketConn: udpConn, supportsDF: true}`), so the baseline cell executes the base datapath code byte for byte.
- **Host:** a GitHub-hosted `windows-latest` runner. No dedicated Windows measurement host exists in this program; the hosted runner is the qualified Windows execution surface, and its observed OS build, CPU count, architecture, and Go version are recorded per invocation in the results. Shared-VM noise is handled by the paired same-round design and the contamination rule below, not by affinity.
- **Toolchain:** the repository's current Go 1.27.x for windows/amd64; identical flags for every cell; one binary per collection built with `go test -c -tags w1bench`.
- **CPU affinity:** not available on a hosted runner; recorded as absent. `GOMAXPROCS` left at the runner default and recorded.
- **Workload:** one QUIC connection over IPv4 loopback between two transports in one process; the server bulk-sends 512 MiB of 1071-byte application records on one unidirectional stream, generating records in 32-record batches so the sender outpaces the packetizer the way a bulk sender does; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The externally owned application sweep stays out of scope.
- **Cell integrity:** the harness asserts the concrete conn type per cell before measuring, and a wildcard-bound candidate cell additionally fails unless both endpoints observed populated packet info during the transfer, so a silently misconfigured or control-message-inactive cell cannot pass. Every cell sets identical explicit socket buffers (4 MiB) and the same DF flag on both endpoints.

## Cells

Two cell families, each a candidate/baseline pair with both endpoints on the named datapath:

| Family | Bind | candidate | baseline | Role |
|---|---|---|---|---|
| swap | loopback (packet info inactive on both) | `windowsConn` | `basicConn` | noninferiority gate: both datapaths do identical work |
| parity | wildcard, dialed via loopback (candidate requests and processes packet info; baseline runs the base commit's wildcard behavior, no packet info) | `windowsConn` | `basicConn` | reported cost of the required packet-info parity deliverable, with a stop floor |

Packet-info functional correctness, deadline/close closure, and the caller-supplied-socket fallback are owned by the unit suites on Windows CI, not by this protocol; this protocol owns only the noninferiority cells (the W plan classifies W1's protocol/results as "noninferiority cells only").

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all four cells, family order and cell order alternating by round parity, one fresh process per cell invocation. The collection runs in one hosted job with no concurrent steps. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: the cell and its bind family; wall time and throughput (`throughput_mb_per_s`, decimal megabytes per second); Go allocation totals, GC count, heap in use; peak working set (`K32GetProcessMemoryInfo`); a wildcard-bound candidate cell's count of reads with populated packet info; and the recorded host facts (OS build, CPU count, `GOMAXPROCS`, Go version, architecture).

## Metrics and predeclared bounds

- **Primary — swap-family throughput noninferiority:** swap candidate vs swap baseline per-round ratio, paired within each round. A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds, seed 20260911) gives a 95 % interval of the geometric-mean ratio; pass requires the interval's lower bound ≥ 0.95 (no more than 5 % regression when both datapaths do identical work). The bootstrap describes this sample set, not a universal timing guarantee.
- **Parity-family reported cost with stop floor:** parity candidate vs parity baseline paired ratio with the same bootstrap. The geometric mean and interval are published as the measured price of the packet-info parity deliverable; they gate nothing above a stop floor of 0.85 — an interval lower bound below 0.85 stops adoption, because a cost that large would exceed what the OOB-platform cost model plausibly explains and would indicate a defect in the control-message path.
- **Memory noninferiority:** per family, candidate median peak working set ≤ baseline median + 8 MiB (W1 adds only a 128-byte control buffer per conn; the allowance covers run-to-run working-set noise, not a real growth budget). Go allocation counts and GC cycles are reported against the baseline for context.
- **Host contamination rule:** if within any cell the maximum per-round throughput exceeds 1.30× that cell's median, or the median exceeds 1.30× the minimum, the collection is contaminated by shared-host noise and the disposition is inconclusive. Paired ratios do not rescue a collection whose raw rounds are this unstable.

## Disposition rule (mechanical)

- **Pass:** swap interval lower bound ≥ 0.95, parity interval lower bound ≥ 0.85, memory within budget in both families, no contamination.
- **Fail:** swap point estimate (geometric mean of paired ratios) < 0.90, or parity interval lower bound < 0.85, or a memory budget exceeded, in an uncontaminated collection.
- **Inconclusive:** anything else, including contamination. An inconclusive result is reported as such and does not adopt; it returns to the slice's stop conditions.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
