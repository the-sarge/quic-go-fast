# W1 Windows message-I/O foundation noninferiority protocol

Precommitted measurement protocol for Slice W1 of the [Windows datapath plan](../adr/2026-09-11-windows-datapath-plan.md) (Track W, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the W1 implementation branch before any collection run; the results document records the exact candidate head measured.

## Question

Does replacing the plain-socket Windows datapath (`basicConn`, `ReadFrom`/`WriteTo`) with the message-I/O conn (`windowsConn`, `ReadMsgUDP`/`WriteMsgUDP` with a control buffer) preserve bulk-transfer throughput and memory on Windows? W1's gate is correctness and noninferiority only: a behavior-preserving rebuild claims no syscall reduction, so no engagement or syscall metric applies — the performance gates belong to USO (W2) and URO (W3).

## Artifact classes

The measurement harness (`w1_measurement_test.go`, build tag `w1bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the W1 conn swap and control-message code.

## Fixed configuration

- **Base commit:** `8acce322` (default-branch HEAD at branch creation; `basicConn` Windows datapath).
- **Candidate:** the W1 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's preserved `basicConn` path, constructed exactly as the base commit's Windows `newConn` constructed it (`&basicConn{PacketConn: udpConn, supportsDF: true}`), so the baseline cell executes the base datapath code byte for byte.
- **Host:** a GitHub-hosted `windows-latest` runner. No dedicated Windows measurement host exists in this program; the hosted runner is the qualified Windows execution surface, and its observed OS build, CPU count, architecture, and Go version are recorded per invocation in the results. Shared-VM noise is handled by the paired same-round design and the contamination rule below, not by affinity.
- **Toolchain:** the repository's current Go 1.27.x for windows/amd64; identical flags for every cell; one binary per collection built with `go test -c -tags w1bench`.
- **CPU affinity:** not available on a hosted runner; recorded as absent. `GOMAXPROCS` left at the runner default and recorded.
- **Workload:** one QUIC connection over IPv4 loopback between two transports in one process; the server bulk-sends 512 MiB of 1071-byte application records on one unidirectional stream, generating records in 32-record batches so the sender outpaces the packetizer the way a bulk sender does; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The externally owned application sweep stays out of scope.
- **Cell integrity:** the harness asserts the concrete conn type per cell (`*windowsConn` for candidate, `*basicConn` for baseline) before measuring, so a silently misconfigured cell cannot pass. Both cells set identical explicit socket buffers (4 MiB) and the same DF flag on both endpoints.

## Cells

| Cell | Both endpoints' datapath |
|---|---|
| candidate | `windowsConn` (W1 message I/O, control buffer requested) |
| baseline | `basicConn` (the base commit's Windows datapath) |

Packet-info parity, deadline/close closure, and the caller-supplied-socket fallback are owned by the unit suites on Windows CI, not by this protocol; this protocol owns only the noninferiority cells (the W plan classifies W1's protocol/results as "noninferiority cells only").

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs both cells with the order alternating by round parity, one fresh process per cell invocation. The collection runs in one hosted job with no concurrent steps. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: wall time and throughput; Go allocation totals, GC count, heap in use; peak working set (`K32GetProcessMemoryInfo`); and the recorded host facts (OS build, CPU count, `GOMAXPROCS`, Go version, architecture).

## Metrics and predeclared bounds

- **Primary — throughput noninferiority:** candidate vs baseline per-round ratio, paired within each round. A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds, seed 20260911) gives a 95 % interval of the geometric-mean ratio; pass requires the interval's lower bound ≥ 0.95 (no more than 5 % regression). The bootstrap describes this sample set, not a universal timing guarantee.
- **Memory noninferiority:** candidate-cell median peak working set ≤ baseline-cell median peak working set + 8 MiB (W1 adds only a 128-byte control buffer per conn; the allowance covers run-to-run working-set noise, not a real growth budget). Go allocation counts and GC cycles are reported against the baseline for context.
- **Host contamination rule:** if within either cell the maximum per-round throughput exceeds 1.30× that cell's median, or the median exceeds 1.30× the minimum, the collection is contaminated by shared-host noise and the disposition is inconclusive. Paired ratios do not rescue a collection whose raw rounds are this unstable.

## Disposition rule (mechanical)

- **Pass:** primary interval lower bound ≥ 0.95 and memory within budget and no contamination.
- **Fail:** primary point estimate (geometric mean of paired ratios) < 0.90, or memory budget exceeded, in an uncontaminated collection.
- **Inconclusive:** anything else, including contamination. An inconclusive result is reported as such and does not adopt; it returns to the slice's stop conditions.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
