# Combined grouped-request prototype for N3 #1202

**Date:** 2026-11-05. **Verdict:** the concrete combined prototype is insufficient to meet the unchanged ordinary-peer performance gate. Grouping and reduced control work occurred, but goodput reached only 53.5% of baseline and CPU/useful byte was 64.9% higher. This rejects the claim that the tested structural changes alone repair N3 #1202; it does not prove the entire requested feature impossible. No new prerequisite, ready issue or production implementation is accepted by this result.

Related: [redesign proposal](2026-11-05-native-pipeline-redesign.md), [prior whole-pipeline diagnosis](2026-11-05-native-pipeline-diagnosis.md), [stopped N-track plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md). Draft PR #1230 remains held. N3 #1202 remains stopped; downstream R17 #1037 and R18 #1038 remain blocked and R19 #1039 remains inactive.

## Hypothesis and exact source

The explicit user instruction was to build and test the combined temporary prototype. The experiment asks whether changing the unit of coordination from individual GSO buffers to bounded requests, eliminating speculative endpoint reservation, draining terminal results together and letting the writer claim its successor can restore the failed ordinary STREAM GSO-on result without changing packet recovery effects.

The frozen source is `5e1011f494ce482b618e29c8bcf700722daaf5e1` in `/Volumes/worktrees/quic-go-fast/1202-pipeline-diagnosis`, branch `codex/1202-pipeline-diagnosis`. It starts from instrumented control `ecbb35a364343f9c33b92d72389ae6ef4f7bb8d1`, reverting the preceding independent-dispatch variant to that instrumented control before applying the combination. The source archive SHA-256 is `18b530e585588dcbffdbea32cd5605a8cf39510d7ac0b229f3d6cffff3bac9b1`; the native Linux binary SHA-256 is `8414d3b218d9b0f7f133a1317cb67294bea87d42abb1c72bc946ecf08427c371`. The original production candidate remains untouched. This prototype branch is not pushed or proposed for shipping.

The adapter's declared domain is handshake-confirmed, ordinary path-zero traffic with no path-credit owner. It keeps four local lifecycle records and at most one staged head for that single endpoint; groups seal at the natural emission boundary or existing size/member limits. Exact-size `offer` transfers the native descriptors while the connection retains a packet/metadata sidecar. Refused work retains its ciphertext, tokens and original deadline. The connection's timer includes staged expiry even when hard-blocked; reset and close dispose unadmitted work before changing its generation/lifetime.

Typed native members preserve each buffer's GSO/ECN/OOB, credit and result timestamp. A separate control-error field distinguishes deadline setup/reset failures from member outcomes; an unattempted tail receives an unsubmitted settlement time without inheriting another member's size error. Recovery registration and per-packet completion still run immediately/in order through their existing implementation. No PTO cache, deferred timer policy, registration arena, scheduler yield, capacity increase or ordinary endpoint bypass is included.

Only the writer claims requests under canonical endpoint control. The independent owner still services expiry, visible native-return facts, overdue faults and terminal joining. An atomic published-result hint keeps empty `takeMany` calls out of endpoint control, while the result list and mutation authority remain mutex-owned. The actual implementation retains separate admission and drain operations, before/after progress on nonempty interface transactions and direct descriptor/sidecar allocations. These are interpretation limits, not invisible completed optimizations.

## Semantic validation and retained failure

Local endpoint and ordinary-adapter tests passed. The combined grouped families passed with the race detector. Before either performance observation, the exact Linux binary passed seven focused families: multiple members with heterogeneous GSO/fallback; middle error and untouched tail; initial/final deadline failure; early ACK plus Retry during a native borrow; staged expiry; staged reset/close; and successful two-member admission with distinct receipt timestamps and ordered ACK callbacks. These exercise real native endpoint, adapter and recovery seams with controlled native I/O. They are design-validation evidence, not full production or cross-platform qualification.

The first local staging-success fixture observed the request before simulated native sleeps had elapsed. Its assertion ran inside the locked endpoint callback, so Goexit during failure cleanup retained that mutex and hung the test. A stack dump identified the fixture error. The corrected fixture advances simulated time and captures state under the lock, then asserts outside it. The corrected suite passed. The failed local attempt and interrupted duplicate/cold-cache setup are retained as local setup history, not erased or charged as remote performance observations. Local sandbox loopback restrictions affected an agent's broad overlay run; the parent subsequently ran the focused endpoint/ordinary set with the required local access successfully.

## Matched measurement

One same-binary pair ran on minimax, Linux amd64, Go 1.27.3, the existing server/client CPU assignments and seed 1038. Each observation used 10 seconds warmup and 60 seconds measurement with a 90-second observation bound. Both were uncontaminated under the existing fixture-core/sibling criterion. CPU, block and mutex profiles were captured during the same pair, including whole-role warmup/cleanup. There was no retry or second candidate trial.

| Metric | Unregistered baseline | Grouped granted prototype | Accepted comparison |
| --- | ---: | ---: | --- |
| Receiver-useful MB/s | 1661.756079 | 889.861867 | Ratio 0.535495; required at least 0.95 — fail |
| Server CPU ns/useful byte | 1.012195 | 1.668925 | Ratio 1.648818; required at most 1.10 — fail |
| Application control-echo p99 µs | 688.127 | 1310.719 | Within existing baseline + 1 ms allowance |
| Application send-call p99 µs | 49.151 | 163.839 | Within existing baseline + 1 ms allowance |
| Peak server RSS KiB | 26668 | 27680 | No memory failure reported by the existing judge |
| Empty measurement seconds | 0 | 0 | Pass |
| Reported DATAGRAM drops / write errors | 0 / 0 | 0 / 0 | No error/drop failure |

The existing judge reports exactly the goodput and CPU failures above. These are diagnostic observations, not the full R18 #1038 campaign. No GSO-off or mixed/multipath certification is implied.

## Did the intended structural change occur?

Yes. The granted server sealed and admitted 1,394,585 groups containing 3,261,331 member buffers: 2.338567 buffers per group. There were no group admission refusals or staged expiries during the measured window. It performed 3,261,328 native calls averaging 16,759.14 bytes per call, preserving the previous roughly 16.8 kB GSO-buffer geometry. The three-count boundary difference is retained; these exact event counters cover different points in the pipeline and need not end on the same in-flight request.

The prior instrumented single-buffer control recorded about 17.668 progress passes and 8.947 wake attempts per native call. This prototype recorded 3.323 progress passes and 1.589 wake attempts per native call. That is concrete work-count reduction, not an inferred scheduler win or a new paired speedup claim against the earlier candidate. The new interface-transaction counter records 0.734 calls per native call, but excludes the writer's newly moved control entries; it must not be advertised as the total number of mutex acquisitions.

The old writer timing counters now span request groups rather than individual buffers. Claim-to-start begins after the writer acquires control and excludes that lock wait. Group service/gap means cannot be compared directly with old singleton/native-call gaps. Window totals, actual native-call geometry, progress counts and total CPU/useful byte are the meaningful comparisons here.

## Residual CPU attribution

The following estimates apportion measured window CPU/useful byte using whole-role CPU profile shares. They locate work; nested cumulative rows overlap and must not be added as independent savings.

| Cumulative stack | Baseline estimated ns/byte | Prototype estimated ns/byte |
| --- | ---: | ---: |
| Connection run loop | 0.662362 | 1.187044 |
| Packet packer append | 0.432693 | 0.487659 |
| Packet registration | 0.055354 | 0.258437 |
| Local completion | Absent | 0.194711 |
| Loss-detection timer derivation | 0.011002 | 0.211244 |
| Local PTO selection | Absent | 0.181548 |
| Local ECN completion | Absent | 0.058429 |
| Allocation runtime | 0.008423 | 0.034993 |

About 80% of the remaining excess CPU over the matched baseline is under the connection loop. Registration's increase plus the separate completion stack accounts for approximately 61% of that excess. PTO selection is 11.31 of 103.97 sampled server CPU seconds; even removing that entire sampled cost leaves about 1.487 ns/byte, approximately 47% above the matched baseline, still well beyond the 10% allowance. This result does not reinstate the withdrawn PTO-only prerequisite.

The profile does not isolate an allocation arena or a single map operation as the cure. New descriptor allocations and retained before/after progress are real limitations, but neither an unimplemented combined Exchange nor optimistic subtraction of allocation samples establishes that they close the gap. The next design question is the connection-owned recovery lifecycle: which per-packet facts must mutate immediately, and which repeated derived work can be represented without rescanning while preserving intermediate loss/ECN/callback behavior. The earlier lazy-alarm counterexample and late-reference ownership obligations remain binding inputs to that design.

## Scope and decision

Retain the request-centric ownership simplification as a candidate component, with the actual measured limits attached. Reject this concrete prototype as a sufficient performance repair. Do not publish its provisional slice graph, turn a timer/cache/arena guess into a mandatory child, or promote the stopped N3 #1202 candidate. A revision needs a complete cost argument and evidence across the remaining recovery work, rather than another endpoint-only change justified by local timing.

This experiment does not test multi-route staged-head identity or cross-endpoint progress, a combined admission/result Exchange, production allocation strategy, all native lifecycle interleavings or the full GSO-on/off product gate. Those omissions limit claims about a finished implementation. They do not undo the observed fact that the implemented grouping, exact admission, terminal batching and writer continuation still substantially miss the covered ordinary STREAM gate.

## Evidence custody and accounting

Local evidence root: `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/group-structure-v1`. The matching remote root is under `/home/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/`. Preserve `identity.json`, `inventory.json`, `build.json`, `native-tests.log`, `ledger.json`, `summary.json`, `observations.json`, both role outputs and profiles, and the derived `analysis.json` / `profile-analysis.json`. The source archive, local validation receipt and wrapper sit in their parent diagnostic directory. The wrapper validates the git-archive commit, source hash and same binary for both observations, and records failed stages before stopping.

This adds one native build, one native semantic-test invocation and two observations: four counted invocations, 158.139027 measured host-seconds. Source verification/extraction and two quiet checks add 4.155603 seconds of separately itemized precondition overhead. The observation ledger alone charges two observations / 154.091690 seconds. No contaminated replacement or retry occurred. Using the same build/test/observation accounting categories, cumulative counted history is 163 invocations and at least 2495.445570 measured host-seconds, retaining the previously documented unknown setup duration and separately reported overhead. The user's scoped cumulative waiver remains; no new cumulative ceiling is introduced.

Prototype code and measurement helpers are temporary diagnostics retained in explicitly named diagnostic storage. The results and this receipt are traceability evidence. Nothing here creates a shipped feature, permanent verification framework, public activation or new issue/OmniFocus state. Draft PR #1230 remains held, and the current stopped plan remains authoritative until an audited replacement is actually accepted and published.

**Independent result review:** source and binary identities, native semantic execution, contamination, paired metrics, event-counter interpretation, profile denominators and invocation accounting were independently checked. Registration and completion stack populations are disjoint in these profiles; their combined excess is approximately 60.57% of the total excess over the matched baseline. The connection-loop share is approximately 79.89%. The result remains an insufficient diagnostic candidate, not a dispatch approval.
