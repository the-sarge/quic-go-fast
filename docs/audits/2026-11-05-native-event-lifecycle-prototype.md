# Native event and lifecycle prototype for N3 #1202

**Verdict:** the composed candidate reaches **76.2% of baseline goodput with 27.0% extra CPU per useful byte** in one clean matched pair. It improves descriptively on the preceding experiment but still misses the unchanged 95% goodput and 110% CPU ceilings. [N3 #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) remains stopped and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) remains held. Continue the broader redesign with the useful components; this result neither qualifies a production replacement nor creates a new implementation prerequisite.

Related: [preceding transition/projection result](2026-11-05-native-transition-prototype.md), [pipeline design](2026-11-05-native-pipeline-redesign.md), and [stopped plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md).

## Frozen source and changes

The measured source is `d68633ed668653401520985914cda7ee831b99e9`, based on corrected checkpoint `c09a648c4fcab1f554c5e69a0294e32642a90133`. Archive SHA-256: `c70f058ddc03c47e8e2a3e0fa7089afc0c1cd21ab1df208e39d0ead1e08aec42`. Native binary SHA-256: `94d76854caa407106af929ba8e7d39663b84b63be4bfbf44093b4615794fe939`. This source includes the preceding FIFO callback-order fix and the ACK-carrier reset correction described below; the predecessor's known defects must not be attributed to this measured source.

The endpoint now derives queue deadline and authorization-presence summaries from queue membership, maintaining them at insertion/removal. Hot progress skips queue expiry scans only while no deadline is due and no externally revocable authorization requires observation. Epoch validity itself is never cached. Generic lifecycle transactions still force a sweep. An exact pending-demand count avoids participant and retry scans when there is no demand. Service selection gathers the best candidate in each existing class in one queue walk, retaining class policy, cursor ordering and FIFO ties. Native publication, final notification, timer ownership, absolute deadlines, selected grants and all capacities remain unchanged.

The writer reads each prepared member's immutable geometry directly from the native envelope. It no longer reconstructs a request and a singleton buffer list per member, and syscall helpers borrow the original token request instead of repeatedly copying it. The shared buffer core retains the legacy route's native-GSO stopping/counting behavior, persistent fallback, cancellation before each attempt, member timestamps and uncertainty attribution. Pointer borrowing ends before final atomic completion publication; no new recovery pointers enter the native envelope.

Recovery reuses the packet-number space already validated by local registration. ECN lookup handles the common front two records directly and appends through the deque's exact end operation. An empty lost-packet history bypasses vacuous local undo settlement. The exact rank projection accumulates one word's pending Fenwick delta while including that delta in every query; authoritative bit words still change per packet, and growth/rebuild/reset preserve every intermediate answer. This reduces redundant bookkeeping without deferring registration, callbacks, loss decisions or policy.

Retained ACK-carrier traversal now removes each original carrier before invoking its callback and stops traversing an old population after reset. A bounded snapshot and reset epoch prevent callbacks from restoring old survivors, erasing replacement records or replaying carriers already consumed by recursive ACK processing. This is a correctness repair with bounded cold-path cost, not a claimed performance optimization.

## Behavioral evidence

The full recovery race suite passed in 73.449 seconds. The combined native group/storage/member-writer/event/turn/Exchange race selection passed in 1.345 seconds. The final whole root package passed in 12.136 seconds; vet, tidy-diff and diff checks passed. Linux preconditions ran the grouped/storage/Exchange/turn/event/member-writer/real-loopback families and the full recovery package before either observation. Pinned source audits reported no blocking finding in the endpoint scope or recovery changes. The endpoint reviewer also authored its summary/demand/service proposal, and the recovery reviewer authored rank and carrier pieces; those scopes received further adversarial self-review. The other recovery edits were independently inspected. Separate evidence review reconstructed raw metrics, profiles and artifact hashes independently of the analysis scripts.

Two broader-test failures were retained and diagnosed. The storage probe ACKed a contiguous range despite the real packet-number generator deliberately skipping numbers; it now constructs ACK ranges from actual registrations. The loopback probe left every server-side bidirectional stream's send half open. A deterministic 101-exchange characterization failed even before migration; completing both halves corrected it. The original migration test then passed ten of ten runs, including approximately 200 ms cases matching the first path-validation retry interval. Failure snapshots showed idle healthy endpoints, matching queue/demand summaries and no stranded native work. The exact initial response loss was not traced, so this evidence does not assert a new migration defect or change migration policy. These fixture repairs preserve the original failed logs rather than treating an eventual green run as explanation.

## Matched result

The same Linux amd64 Go 1.27.3 binary served both modes and roles on minimax, server cores 8–11 and client cores 12–15, seed 1038, 10-second warmup and 60-second window. Foreign CPU monitoring included those cores and their SMT siblings. Baseline foreign CPU was 0.117945 seconds and candidate 0.270470; neither was contaminated. No observation was retried.

| Metric | Baseline | Candidate | Requirement / interpretation |
| --- | ---: | ---: | --- |
| Useful bytes | 99,537,059,840 | 75,862,026,240 | Receiver window |
| Useful MB/s | 1658.950997 | 1264.367104 | Ratio 0.762149; minimum 0.95 — fail |
| Server CPU seconds | 100.931789 | 97.669327 | Window CPU |
| Server CPU ns/useful byte | 1.014012160 | 1.287460036 | Ratio 1.269669; maximum 1.10 — fail |
| Control p99 µs | 786.431 | 524.287 | Pass |
| Application send-call p99 µs | 51.199 | 94.207 | Pass; not native return or peer delivery |
| Peak RSS KiB | 26,960 | 32,580 | Pass |
| Lost packets | 1548 | 0 | Retained as observed |
| Empty seconds / DATAGRAM drops / write errors | 0 / 0 / 0 | 0 / 0 / 0 | No hidden failed role |

Geometry remains comparable: 61.057329 versus 61.067088 native calls per useful MB, and 16,766.138 versus 16,763.521 bytes per call. The candidate formed 2,044,789 groups containing 4,632,674 members, averaging 2.265600 members, with no refusal or expiry. The preceding source reached 73.2% goodput with 31.3% extra CPU/byte against its own baseline. Separate-run differences describe progress; they do not isolate the causal contribution of each combined edit.

## Remaining cost and next work

Whole-role CPU samples, apportioned by measured-window CPU/useful byte, place registration at 0.112370 ns/byte, completion at 0.041560, endpoint control at 0.050143 and writer work excluding syscall/control at 0.032299. The preceding values were 0.124038, 0.053360, 0.062721 and 0.036626. Local undo attribution falls from 0.011000 to 0.001694 and local ECN from 0.037679 to 0.030831. Rank-related attribution is not lower in this pair (0.010531 to 0.011294), so do not claim an isolated measured rank improvement.

The current CPU ceiling is **1.115413375 ns/byte**, leaving **0.172046660 ns/byte** to remove. Construction excluding registration remains 0.473763 versus baseline 0.425339; outer local result consumption is 0.025636 and connection handoff 0.016714. The complete disjoint callable-owner table is in the preserved analysis; those rows cannot be added to overlapping mechanism or cumulative function costs.

The connection owner remains material: `Conn.run` scales to approximately 0.649374 baseline and 0.840802 candidate ns/byte. A simplified model holding baseline owner availability fixed would require at most `0.649374 / 0.95 = 0.683552` ns/byte, roughly 0.157250 below the candidate. This is a cost condition, not a saturation proof or exact throughput prediction. Writer-only savings do not automatically relieve this serial work.

The next concrete ownership proposal connects packet construction with the recovery packet that will own its metadata. Today the framer commonly allocates a one-element stream-frame array, then registration obtains the pooled recovery packet that adopts it. Preparing that existing packet before construction may let the framer use its safely cleared backing storage and registration adopt the same object. It must preserve validation after each seal, immediate policy effects, error disposal, early-ACK independence from late native completion and final callback lifetime. No implementation or measured saving is claimed here. Shared registration headers remain another candidate, but extra indirection and short-run costs need evidence; manual canonical-cell reuse remains parked.

Retain the useful endpoint, writer, exact-projection and lifetime components while testing that next composition. An individual component need not close the full gap to earn its place. Conversely, passing correctness or removing an allocation does not establish the throughput target, and improvements common to baseline and native mode must be compared against the improved baseline too.

Named diagnostic helpers plus identified diagnostic clocks account for approximately 0.008180 baseline and 0.021458 candidate ns/byte. They cannot simply be subtracted to invent an uninstrumented result. Whole-role memory profiles estimate 98,841,331 baseline server allocations / 4,242,830,806 bytes and 78,459,428 candidate / 7,841,294,008 bytes. These include warmup/shutdown at different work volumes; they are neither exact counts nor normalized window rates or peak RSS.

## Evidence and accounting

The build and two semantic invocations consumed 29.154875 host-seconds; the two observations consumed 154.114354. This adds **five invocations / 183.269229 measured host-seconds**, plus 4.157172 seconds of source preparation and quiet checks. Cumulative history becomes **188 invocations / at least 3410.872034 measured host-seconds**, retaining prior failures, contamination, unknown historical setup and separately recorded overhead. All roles and preconditions succeeded; the diagnostic exited nonzero because efficiency still failed. Thirty-seven available artifacts were hash-checked. The remote-only binary's recorded identity was checked for consistency, not independently rehashed locally.

Raw evidence is retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/event-lifecycle-v1/`, mirrored from the same suffix under `/home/josh/diagnostics/quic-go-fast/` on minimax. The exact source archive and `event-lifecycle-v1-local-validation/` are adjacent. Local evidence includes red/green characterizations, failed full-suite logs, the predecessor differential and temporary instrumentation. Supplemental source reviews, analysis scripts and results have their own manifest without replacing the original run inventory.

Recommendation: continue the larger redesign from this measured checkpoint, carrying forward the demonstrated improvements and testing construction/recovery ownership next. Keep N3 #1202 stopped and draft PR #1230 held until the complete candidate meets its existing targets and the implementation contract is audited.
