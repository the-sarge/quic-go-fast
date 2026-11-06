# Canonical member geometry prototype for N3 #1202

**Verdict:** one clean matched pair reaches **78.1% of baseline goodput with 24.3% extra CPU per useful byte**, still failing the unchanged 95% and 110% limits. The ownership and traversal reductions are verified. This observation does not establish a throughput improvement: native goodput fell from 1267.420 to 1254.586 MB/s and native CPU/byte rose from 1.276196 to 1.286130; baseline also slowed, improving the ratios. Carry the simpler geometry into the larger redesign without presenting this component as sufficient.

## Source and behavior

Frozen source `990a7a4de712de0c4c4ce89b8e75f3158dd13d33` builds on `eaaea71fb5685659ba734f8923ed36f022b758a8`. Source archive SHA-256 is `62bc047f167d6bfe79b1307d6bb4a5bb01168ac5e99cc8faf7ca905a892a2003`; diagnostic wrapper SHA-256 is `209787eb987019aa23582362adc4158f055aab33e9f9948bc35c72161b3ce483`. The prior compact metadata, construction storage, event/lifecycle and FIFO corrections remain in the composition. This prototype changes private representation and immediate checked-fact reuse; it does not move native calls into the connection owner or change recovery policy.

An owned native request now uses its stable member rows as its only payload geometry. The connection no longer appends a second buffer slice, and endpoint commit no longer copies that slice into its fixed buffer array. One pure admission pass validates exact storage aliasing, count, exclusive/nonempty buffers, duplicate buffers/credits and incompatible request-wide fields while summing backing capacity. Commit consumes that total immediately under the same endpoint lock, without callbacks or intervening producer mutation. Legacy typed members retain their cloned descriptors and copied flat list. The reserved one-buffer and close-deposit entry points explicitly reject owned storage and datagram hybrids; otherwise these malformed forms could bypass validation or undercharge the reused single-buffer size. Production callers already supply only their supported one-buffer shape.

Publication still materializes missing member facts before releasing payload. Owned requests release every member buffer in one loop before any credit completion can wake a producer, then clear the member pointers. The separate native envelope remains fenced until actual native return and terminal take; the protocol sidecar remains independently reachable across recovery callbacks. Close, fault, deadline expiry and callback reentry do not authorize premature envelope reuse. Existing unadmitted `diagDrop` disposal order is unchanged. Endpoint-record full initialization remains, so this is not removal of all fixed-array clearing.

Grouped ordinary records now reset a complete 184-byte header instead of rewriting their unused 1,608-byte inline packet array. The inline array is zero whenever a record becomes free: legacy retirement still clears the whole record, and grouped construction only writes its sidecar. All header identities, admission facts and receipt-bearing metadata clear together. The occupied serial, final identity guard in unadmitted disposal and partial-tail reentry checks are unchanged. Measured 64-bit layout remains 1,792 bytes per record, 67 inline packet slots and four records. This saves grouped stores; it does not shrink the sender allocation or enlarge capacity.

## Correctness and review

The new owned-admission test first failed on the preceding implementation with an invalid-request error, then passed after the representation change. The mixed legacy→grouped→legacy characterization passed before and after the header change. It fills every inline slot through real recovery registration, checks disposal and duplicate-token rejection, exercises partial construction, unadmitted drop and published result, and verifies stale receipt exclusion and full record clearing before returning to legacy reuse.

Focused tests passed in 0.189 seconds, the full root package in 12.068 seconds, and all `TestNative` race tests in 3.398 seconds. Vet, tidy-diff and diff checks passed. The temporary layout overlay confirmed the stated sizes. Recovery source did not change in this step; the prior full recovery race result remains evidence for that unchanged source, and the existing Linux precondition reruns the full recovery package before measurement.

Owned tests retain separate test-local buffer references so a removed flat list cannot make release assertions vacuous. Coverage includes exact byte charging, successful native-return reconciliation, capacity refusal without transfer, malformed geometry without new demand, the 32-member bound, alternate-entry rejection, legacy copying, queued expiry and setup/write/reset borrows after detach. A deliberately invalid first credit is a deterministic test-only tripwire proving all buffers release before credit handling begins. Deadline setup/reset failures now run with both owned and legacy rows and check buffer/credit disposal. The existing early-ACK/Retry grouped fixture still uses legacy rows; owned borrow-after-detach and reuse tests cover the changed storage boundary. This disclosed matrix limit is not a claim that the exact owned early-ACK/Retry combination was newly exercised.

Two independent static reviewers did not author their respective header or geometry changes and found no blocking lifetime, accounting or capacity defect. They ran no workloads; root owns the reported execution evidence. These source reviews are not production or full mixed-traffic qualification.

## Matched measurement

The same Linux amd64 Go 1.27.3 binary served both observations, with SHA-256 `9ad3daaf01593cff114fbd889d4e3db5566cf17b4ae33a7c8786549d4aad893d`. The fixture retained server CPUs 8–11, client CPUs 12–15, monitoring of those CPUs and their SMT siblings, seed 1038, 10-second warmup, 60-second window and 90-second observation timeout. Both roles exited successfully in both observations. Recorded foreign CPU was 0.112581 baseline and 0.257646 candidate seconds; neither observation was contaminated. No observation was repeated.

| Metric | Baseline | Candidate |
| --- | ---: | ---: |
| Useful bytes | 96,444,743,680 | 75,275,173,874 |
| Useful MB/s | 1607.412395 | 1254.586231 |
| Server window CPU seconds | 99.824585 | 96.813685 |
| CPU ns/useful byte | 1.035044329 | 1.286130340 |
| Control p99 µs | 720.895 | 442.367 |
| Application send-call p99 µs | 51.199 | 90.111 |
| Peak RSS KiB | 26,720 | 32,368 |
| Lost packets | 877 | 0 |
| Empty seconds / DATAGRAM drops / write errors | 0 / 0 / 0 | 0 / 0 / 0 |

The exact goodput ratio is 0.780500533; CPU/byte ratio is 1.242584790. Other fixture checks pass. Native geometry remains comparable at 61.057936 versus 61.069271 calls/useful MB and 16765.996 versus 16762.904 bytes/native call. Candidate counters show 2,034,359 groups and 4,596,992 members, averaging 2.259676, with zero refusal or expiry. The 4,597,000 native calls and member count are separately recorded window counters and differ by eight; they are not interchangeable counts.

The current CPU ceiling is **1.138548762 ns/useful byte**, leaving **0.147581579 ns/byte**. Disjoint callable-owner estimates are:

| Owner | Baseline ns/B | Candidate ns/B |
| --- | ---: | ---: |
| packet registration including callbacks | 0.055365 | 0.101534 |
| construction metadata acquisition and cleanup outside registration | 0.000000 | 0.016505 |
| packet construction excluding registration | 0.446206 | 0.470674 |
| CompleteLocalPacket including callbacks | 0.000000 | 0.040181 |
| outer local result consumption excluding completion and endpoint control | 0.000000 | 0.023562 |
| endpoint state and exchange control | 0.000000 | 0.047807 |
| connection native handoff excluding earlier owners | 0.000000 | 0.016505 |
| writer excluding syscall and state control | 0.019018 | 0.032782 |
| UDP send syscall subtree | 0.256473 | 0.261916 |

These scaled whole-role samples locate work; they do not prove independently removable costs, a critical path, or causation. Registration plus acquisition must be considered together because packet acquisition moved before construction in an earlier prototype. Cumulative `Conn.run` is 0.848011 candidate versus 0.676729 baseline ns/byte, compared with 0.851999 versus 0.658771 previously. That modest change does not establish a material reduction in connection-owned work. Endpoint control is 0.047807 versus the prior 0.045518; removing geometry passes is real source-level simplification, but no isolated endpoint CPU gain is demonstrated by these observations.

Whole-role server memory profiles estimate 14,243,976 candidate allocated objects / 5,570,510,790 allocated bytes, versus 13,709,599 / 5,439,484,083 in the preceding candidate at a different work volume. These sampled totals include warmup and shutdown; they are neither exact allocation counts nor window rates. No reduction in total allocation volume is claimed from this run. Named diagnostic helpers plus identified diagnostic clocks account for approximately 0.010131 baseline and 0.018098 candidate ns/byte; subtracting them would not produce a valid uninstrumented counterfactual.

## Evidence and next decision

Raw artifacts are retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/canonical-geometry-v1/`, mirrored from minimax; the exact source archive and `canonical-geometry-v1-local-validation/` are adjacent. Twenty-one local validation files preserve proposals, independent source reviews, red/green receipts and layout checks. Analysis and the independent evidence review have a separate supplemental manifest. Independent reconstruction verified all 36 measurement artifacts, the source archive and all 21 local-validation artifacts, and reproduced the ratios, accounting and cumulative connection profile estimates. The binary's recorded identity is consistent across the pair; the remote-only binary has not been independently rehashed locally. Raw scheduler snapshots are not separately retained, so contamination review can inspect the fixture's scoped calculation and recorded result, not reconstruct that subtraction from snapshots.

Build and semantic invocations consumed 28.969054073 host-seconds; observations consumed 154.093944233. This adds **five invocations / 183.062998306 measured host-seconds**, plus 4.161893765 seconds of preparation and quiet checks. Cumulative history is **203 invocations / at least 3960.420260018 measured host-seconds**, including earlier failures and contamination. Exit status one reflects the two efficiency failures, not an unreported role or precondition failure.

Retain canonical member geometry, checked-size reuse and complete grouped header resets. The next substantive design input is construction/registration and ordered completion ownership. An API that merely wraps the unchanged per-packet primitive does not establish removed work. Any proposed transition-local shortcut must preserve callback-visible state, per-packet ECN/loss/undo settlement and fresh eager alarm calculation; neither group-end policy batching nor cached RTT/deadline assumptions are justified. Tuple completion identity remains deferred because its ECN, timing and ACK-carrier lifetimes require a broader retention model than pending-token tombstones alone.

Recommendation: continue the larger redesign at that lifecycle boundary, retaining useful components and the independent native writer. Keep [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) stopped and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) held; this diagnostic is neither final qualification nor a dispatchable implementation contract.
