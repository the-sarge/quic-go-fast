# W2 Windows segmented send (USO) adoption protocol

Precommitted measurement protocol for Slice W2 of the [Windows datapath plan](../adr/2026-09-11-windows-datapath-plan.md) (Track W, program QGF-DP-2026-09), per the [datapath offload plan](../adr/2026-09-11-datapath-offload-plan.md)'s adoption rule: protocol before measurement, results before adoption. This protocol is committed on the W2 implementation branch before any collection run; the results document records the exact candidate head measured.

## Question

Does enabling UDP segmentation offload (USO) on Windows transport-owned sockets reduce send submissions per transmitted packet in a bulk QUIC transfer, with noninferior throughput and bounded memory, while the unexercised, disabled, and unavailable configurations preserve the W1 foundation behavior?

## Artifact classes

The measurement harness (`w2_measurement_test.go`, build tag `w2bench`, excluded from ordinary builds and CI) and this protocol/results pair are verification aids and process metadata for this finite investigation, not maintained product deliverables; no maintained-aid exception is requested. The shipped behavior under test is the W2 probe, segment-size encoding, capability wiring, and kill-switch code.

## Fixed configuration

- **Base commit:** `e5789d2a` (default-branch HEAD at branch creation; W1 message-I/O datapath without USO).
- **Candidate:** the W2 PR head at collection time, recorded exactly in the results document. The harness runs in-process against the candidate tree; base-vs-candidate comparison is expressed through the candidate's own disabled cell, which executes the preserved (W1-identical) send path — with the kill switch set, the capability probe never runs and no segment-size control message is encoded, so the disabled cell's send path is the base commit's byte for byte; unit suites separately assert byte-identical send behavior with USO off.
- **Host:** a GitHub-hosted `windows-latest` runner, the program's qualified Windows execution surface established by the W1 protocol. Observed OS build, CPU count, architecture, and Go version are recorded per invocation because hosted runners differ run to run. Shared-VM noise is handled by the paired same-round design and the contamination rule below, not by affinity.
- **Toolchain:** the repository's current Go 1.27.x for windows/amd64; identical flags for every cell; one binary per collection built with `go test -c -tags w2bench`.
- **CPU affinity:** not available on a hosted runner; recorded as absent. `GOMAXPROCS` left at the runner default and recorded.
- **Workload:** one QUIC connection over IPv4 loopback between two transports in one process; the server bulk-sends 512 MiB of 1071-byte application records on one unidirectional stream, generating records in 32-record batches so the sender outpaces the packetizer the way a bulk sender does and the emission path forms real segmented batches; the client reads to completion. Flow-control windows are raised (16/24 MiB max) so flow control is not the bottleneck. The unexercised cell instead paces one record per millisecond (2,000 records) so the packetizer never holds more than one packet and USO stays available but unexercised. The externally owned application sweep stays out of scope.
- **Submission counting:** the harness wraps each endpoint's `WritePacket` — every call issues exactly one `WSASendMsg` through the runtime poller, so submissions are send syscalls by construction — and counts packets as `ceil(n/gsoSize)` for segmented submissions and 1 otherwise. The per-submission batch-size distribution is emitted per cell. No external syscall counter exists on the hosted runner; the seam-level count is the declared method.
- **Cell integrity:** the harness asserts each cell's GSO capability matches the cell table before measuring, so a silently misconfigured cell cannot measure. Every cell runs the same `windowsConn` datapath and binds loopback with identical explicit socket buffers (4 MiB) and the same DF flag; the only difference under measurement is the USO capability state and, for the unexercised cell, the paced workload.

## Cells

| Cell | Send sockets | USO | Workload |
|---|---|---|---|
| engaged | transport-owned | on (probed) | bulk |
| available-but-unexercised | transport-owned | on (probed) | paced (application-limited) |
| disabled | transport-owned | off (`QUIC_GO_DISABLE_GSO=1`) | bulk |
| unavailable | caller-supplied (never probed, off by the ownership rule) | off | bulk |

Segmented-send functional correctness (payload integrity, segment sizes, single-packet submissions, error classification) is owned by the unit suites on Windows CI, not by this protocol; this protocol owns the engagement and performance cells. The four-combination USO/URO interaction matrix is owned by W3's protocol as the second lander in the W track.

## Collection

Eleven rounds; round 0 is a discarded warmup, rounds 1–10 are the fixed collection. Each round runs all four cells, cell order rotating by round, one fresh process per cell invocation. The collection runs in one hosted job with no concurrent steps. No repetition beyond the fixed set, and no selective deletion of samples.

Per cell invocation the harness emits one JSON line: the cell and round; wall time and throughput (`throughput_mb_per_s`, decimal megabytes per second); server and client submission and packet counts, segmented-submission counts, packets carried in multi-packet submissions, and the per-submission batch-size histogram; Go allocation totals, GC count, heap in use; peak working set (`K32GetProcessMemoryInfo`); and the recorded host facts (OS build, CPU count, `GOMAXPROCS`, Go version, architecture).

## Metrics and predeclared bounds

- **Primary — server send submissions per transmitted packet**, engaged vs disabled, paired within each round. Minimum useful effect: geometric-mean ratio ≤ 0.50 (at least a halving of send submissions per packet; an engaged bulk sender batching correctly should sit far below this). A fixed-seed paired bootstrap (10,000 resamples of the 10 paired rounds, seed 20260911) gives a 95 % interval; pass requires the interval's upper bound ≤ 0.50. The bootstrap describes this sample set, not a universal timing guarantee.
- **Engagement:** fraction of server-transmitted packets that left in multi-packet submissions in the engaged cell, with the per-submission batch-size distribution published. Zero engagement across the fixed collection is the slice's stop-condition evidence. The unexercised cell must report per-round server packets per submission ≤ 1.05, demonstrating the capability can be on without engaging.
- **Throughput noninferiority:** engaged vs disabled per-round ratio with the same paired bootstrap; the 95 % interval's lower bound must be ≥ 0.95 (no more than 5 % regression — loopback USO is a syscall-reduction adoption; wall-clock improvement is reported but not gated). The unavailable cell is reported for context and must show no material regression against disabled (medians within 10 %).
- **Memory budget:** engaged-cell median peak working set ≤ disabled-cell median + 8 MiB (W2 adds only a 24-byte control message per segmented submission; the allowance covers run-to-run working-set noise, not a real growth budget). Go allocation counts and GC cycles are reported against the disabled cell for context.
- **Preservation:** disabled and unavailable cells must report `gso_cap=false` and complete correctly; behavioral identity of the USO-off send path is owned by the unit suites on Windows CI, not by timing. The paced unexercised cell's wall time is timer-bound by construction and is reported without a timing bound.
- **Host contamination rule:** if within any bulk cell the maximum per-round throughput exceeds 1.30× that cell's median, or the median exceeds 1.30× the minimum, the collection is contaminated by shared-host noise and the disposition is inconclusive. Paired ratios do not rescue a collection whose raw rounds are this unstable. The paced cell is exempt (timer-bound).

## Disposition rule (mechanical)

- **Pass:** primary interval upper bound ≤ 0.50, engaged-cell multi-packet engagement > 50 % of packets, unexercised-cell packets per submission ≤ 1.05 in every round, throughput interval lower bound ≥ 0.95, memory within budget, no contamination.
- **Fail:** primary point ratio > 0.75, or engaged-cell multi-packet engagement < 5 %, or throughput point ratio < 0.90, or a memory budget exceeded, in an uncontaminated collection.
- **Inconclusive:** anything else, including contamination. An inconclusive result is reported as such and does not adopt; it returns to the slice's stop conditions.

Lack of statistical significance is not evidence of equivalence. Loopback microbenchmark throughput is not application/file/network throughput; one connection and one stream characterize this workload, not the supported caller contract.

## Guard-mutation evidence

The slice's contract closure requires one guard mutation: misclassify the Windows message-size error (`isSendMsgSizeErr` in `sys_conn_df_windows.go` forced to `false`) and observe the accepted MTU-feedback test fail on Windows. The collection workflow runs this mutation as a separate step against the candidate head and the results document records the observed failure. The mutation never lands on the candidate branch.

## Termination

Run the fixed collection once, publish the results document with the disposition, and stop. A fail or inconclusive disposition returns to the slice's stop conditions; no repeated collection until significance, no workload widening, no extra platforms, timing demonstrations, or fixture cross products without a separately approved evidence scope.
