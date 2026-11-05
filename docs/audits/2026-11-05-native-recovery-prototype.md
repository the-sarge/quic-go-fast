# Native recovery representation prototype for N3 #1202

**Verdict:** the redesigned recovery representation is a useful component, but the combined pipeline still fails the unchanged efficiency requirements: 67.0% of baseline goodput and 38.7% extra CPU per useful byte in an uncontaminated pair.

Related: [grouped-only result](2026-11-05-native-grouped-prototype.md), [pipeline design candidate](2026-11-05-native-pipeline-redesign.md), [stopped plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md).

This is a diagnostic continuation of N3 [issue #1202](https://github.com/the-sarge/quic-go-fast/issues/1202), built on the grouped endpoint prototype at `5e1011f494ce482b618e29c8bcf700722daaf5e1`. It tests whether removing redundant recovery identity lookup and repeated PTO history traversal materially improves the complete native pipeline. It is not a production qualification or a new implementation frontier. N3 issue #1202 remains incomplete, and [draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) remains held.

## Representation and ownership

The completion token now contains a pointer to the canonical `localRegistration` plus its original epoch. The registration retains its owner identity and existing protocol facts: packet number/space, registration time, outcome, eligibility time, ACK facts and rich-recovery provenance. A completion is valid only for the matching owner and epoch while its outcome remains pending. Time and outcome validation precede consumption. A fresh GC-managed registration is allocated for each packet; there is no pooling, slot reuse, manual reference count or second lifetime authority.

The owner maintains two counts. `pending` counts every unconsumed completion identity, including packets retired from protocol history. `live` counts pending identities that still have a packet pointer. Registration increments pending before the existing send operation and increments live exactly when that operation attaches its packet. Valid completion decrements pending and, if applicable, live before callbacks. Protocol retirement decrements live only while the registration is both pending and packet-backed, then clears the packet pointer. Completion followed by retirement, repeated retirement, and reentrant duplicate completion therefore cannot consume a count twice.

These owner counts do not replace history accounting. Capacity remains retained history spans plus pending identities outside the current spans; the existing unresolved-slot subtraction still distinguishes interior holes, trimmed prefixes, old epochs and shared 0-RTT/1-RTT history. Path quiescence uses pending rather than live, so early ACK or key disposal cannot reclaim a path while a native result is outstanding. Root-to-path routing and late completion after close retain the existing owner rules. The production map consumers were confined to `local_completion.go` and `sent_path.go`; generated mocks do not inspect token internals.

Packet history, ECN records, the rich-recovery cursor and retired ACK carriers continue sharing the same canonical registration. Their late references are not redirected into packet storage or moved into a new tombstone. A copied token can now keep registration metadata alive after consumption and can keep its packet reachable until the established protocol retirement clears that pointer. The type comment states this caveat. The intended native caller has bounded request records, but retaining arbitrary numbers of historical token copies would retain more metadata than the previous numeric tokens. The token references the small existing owner allocation, not the complete handler.

The second change belongs to each sent history: an enabled bit and a pointer to the outstanding, network-eligible packet with maximal positive recovery time. The pointer borrows the canonical packet; it does not duplicate eligibility or timestamp fields. Equal maxima may choose the last encountered packet because timer consumers observe the timestamp, not winner identity. No relationship between packet-number order and completion-time order is assumed.

History insertion and canonical completion changes maintain the maximum eagerly. Completion notifies the current epoch's history after publishing outcome/time and before congestion or ECN callbacks. Removing the winner refolds the remaining history synchronously, before packet reuse. Frame clearing during Retry and close also updates the summary before the next observable callback. Clearing `registration.packet` alone does not remove a still-retained packet from the maximum: the original fold continued to see its `Outstanding()` and network-eligibility predicates until physical removal or frame clearing. Fresh/replacement histories enable on their first local registration, existing histories are initialized when local mode is enabled, and final retained-span release clears the winner.

Queries and common updates are O(1), while changing or removing the winner costs O(H) for retained history length H. Adversarial repeated winner removal can still cost O(H²). This is an exact eager summary, not a worst-case constant-time priority index. It adds fields to the history structure, including histories on the ordinary route, although only local histories maintain the summary. It adds no per-packet index or secondary membership collection.

## Preserved semantics and alternatives

Every packet still registers immediately, including its send allowance, congestion and ECN effects. Each completion still publishes and reconciles its own facts in the existing order. All loss detection and timer updates remain eager; the summary caches no alarm, RTT, backoff, amplification state, handshake state, selected space or loss time. The handler still reads current policy state and preserves Initial/Handshake/1-RTT selection order. ECN range search/coalescing, congestion callbacks, reliable-frame retransmission, DATAGRAM disposal, early ACK contradictions, MTU/path-probe treatment and native member timestamps are unchanged. There is no group-end timer flush or deferred ECN reconciliation.

A history-sidecar or reusable arena could reduce allocation further, but history slots and completion identities have different lifetimes. Early ACK is only one case: Retry, key disposal, migration, close, rich cursor delay and uncertain ECN evidence can outlive protocol storage. An early-ACK-only tombstone is insufficient. Stable pages or generational slots would require another lookup and explicit reuse obligations for all late holders. The direct GC handle reuses the existing stable object and removes only the redundant map; it deliberately retains one registration allocation per packet.

A reverse scan with early termination is not exact for arbitrary completion timestamps. A lazy dirty maximum leaves rebuilding on observation and adds dirty-state obligations; deferring complete alarm derivation would also move reads of mutable policy state across existing observation boundaries. A heap gives O(log H) updates but needs indexed removal, while an augmented tree needs consistent indexing across holes, prefix movement and growth. The scalar eager maximum is the smallest exact experiment that removes repeated query traversal. If its refill work becomes a measured dominant cost, that would be evidence for a different index rather than a reason to claim the present worst case is solved.

Prior profile attribution identifies repeated PTO traversal and identity map work as contributors, not a complete explanation of the throughput deficit. Cumulative profile stacks overlap, and the canonical registration allocation, timer policy, ECN reconciliation and other pipeline costs remain. Even optimistic subtraction of the targeted profile categories cannot establish that the existing whole-pipeline efficiency requirements will pass. The combined measurement must decide that question.

## Semantic evidence available

The identity prototype passed the complete `internal/ackhandler` package locally. Its added public-interface tests also passed against the original pinned implementation: zero/foreign/invalid input rejection without consumption, copied handles after ACK/Retry/key disposal/close with intervening GC, same-PN replacement isolation, and duplicate completion from a reentrant frame callback. Existing tracking tests retain an independent oracle that receives the complete returned-token population, deduplicates copies, and calculates epoch/span membership without reading the production pending/live counts or unresolved summary.

The PTO prototype's focused tests compare against the original full fold across arbitrary/equal/nonpositive timestamps, pending and terminal outcomes, excluded packet kinds, holes and prefix reuse, maximum deletion, mixed removals, repeated reads, RTT/backoff changes, old-epoch completion, ACK, Retry and its intermediate callbacks, 0-RTT rejection and close. The final combined source also includes initialization of an already populated history. The separate cold-cache PTO race attempt was interrupted when validation moved to the integrated source; it supplies no pass result.

Measured Go 1.27.3 darwin/arm64 layout changes are 24→16 bytes for `LocalCompletionToken`, 80→64 for `localRegistration`, and 8→16 for the owner; `localCompletions` remains 40 bytes after removing the map and sequence counter. These are representation measurements, not measured allocation-rate or throughput improvements.

The integrated source passed the full `internal/ackhandler` race suite (70.877 seconds), the endpoint/grouped/ordinary adapter race suite (6.967 seconds), `go vet ./internal/ackhandler .`, `go mod tidy -diff` and `git diff --check`. Independent review of the frozen integrated commit found no blocking defect; the history implementation and final PTO oracle tests matched the reviewed temporary artifacts.

## Exact source and native validation

The frozen integrated prototype is `a5c59becf2d89ab03f9179be6f7c6aead11c73fa`, based on grouped prototype `5e1011f494ce482b618e29c8bcf700722daaf5e1`. Its source archive SHA-256 is `c1104232be9e665f9a4fff467f74b7c45ec26bc6d337f38ce88c4e3696eef4a2`. The clean source is retained on the unpushed diagnostic branch; the preserved production candidate is untouched. The uncontaminated replacement's native binary SHA-256 is `a35b7d80890cd1c3ed61bdb84eca2e5a90cb536f0ab5feb631d55977589c0cc5`.

Each pair built that source natively on minimax with Go 1.27.3/Linux amd64, passed the compiled root binary's grouped-endpoint semantic tests and the entire recovery package, then used one identical binary for its two observations. The retained source tree supplies the existing test certificate fixtures. The source hash stayed identical for the contamination replacement; binaries differ because each native build uses its own retained source path. This is not a second design candidate.

## Contamination and the valid paired result

The first pair, `grouped-plus-recovery-v1`, completed but both observations exceeded the existing foreign-CPU limit: baseline 13.396588 seconds and candidate 4.751898 seconds across approximately 77-second occupancies. A VM started during the baseline. Its KVM vCPU work was mostly outside the reserved cores, but its main/helper threads could use all CPUs. The evidence supports external fixture interference without attributing every foreign CPU second to that VM. The host has 16 physical cores with SMT siblings; merely having a background VM is not the contamination criterion. The driver uses CPUs 8–15 and siblings 24–31, and held the second observation until its existing fixture-share check passed. No process or host affinity was changed.

One replacement pair ran under the existing contamination-only replacement rule and scoped cumulative waiver. Both replacement observations were uncontaminated: 0.111968 and 0.220444 foreign CPU seconds. Both used 10 seconds warmup, 60 seconds measurement, the existing CPU assignments and seed 1038, with the existing 90-second per-observation bound. CPU/block/mutex profiles were collected during those same observations, not a separate performance run. No repeat followed the clean performance failure.

| Metric | Unregistered baseline | Grouped plus recovery prototype | Existing requirement |
| --- | ---: | ---: | --- |
| Receiver-useful MB/s | 1597.500075 | 1070.454758 | Ratio 0.670081; at least 0.95 — fail |
| Server CPU ns/useful byte | 1.039835 | 1.442424 | Ratio 1.387167; at most 1.10 — fail |
| Control-echo p99 µs | 720.895 | 458.751 | Pass |
| Application send-call p99 µs | 51.199 | 122.879 | Pass |
| Peak server RSS KiB | 26884 | 29524 | No failure under the existing memory-growth judge |
| Empty measurement seconds | 0 | 0 | Pass |
| Reported DATAGRAM drops / write errors | 0 / 0 | 0 / 0 | Pass |

The clean result still misses both efficiency thresholds. Earlier grouped-only observations reported 53.5% baseline goodput and 64.9% extra CPU/byte; these newer figures are descriptively better, but the two source candidates were not tested in one matched intervention pair. The valid conclusion is that this frozen combined prototype reaches 67.0% of its own baseline's goodput and uses 38.7% extra CPU per byte. It does not qualify N3 #1202 or replace the full R18 #1038 campaign.

## Mechanism and remaining cost

Grouping remained active: 1,687,500 sealed groups, 3,922,654 member buffers and 3,922,660 native calls, averaging 2.324536 buffers per group, with no group refusal or staged expiry. The small event-count differences are retained window-boundary differences. Native calls per useful MB were 61.056471 baseline versus 61.074666 candidate, only 0.0298% apart; payload per call was approximately 16.8 kB for both. Extra syscall count does not explain the deficit. Interface control-entry counters still exclude the writer's own entries, and writer-gap counters still describe request boundaries rather than individual syscalls.

The intended work removal is visible in source and profiles. The prior grouped profile attributed 12.62 of 103.97 sampled CPU seconds to PTO derivation; the new profile attributes 1.26 of 108.14 seconds to derivation plus eager summary maintenance. Winner refill itself accounts for 0.08 sampled seconds. Those are separate-run attribution observations, not a paired speedup estimate. Identity-map lookup/insertion/deletion no longer exists; residual map samples include iteration over recovery paths. The registration allocation remains, and no allocation-rate claim is inferred from type size alone.

The following table uses the replacement pair only. Each sample is assigned once in a separate callable-owner partition: UDP syscall first, registration/completion next, endpoint control, construction, ACK, writer, native handoff, remaining GC/scheduling, connection loop and other work. Whole-role CPU sample shares are scaled by measured-window CPU/useful byte. This estimates attribution; it does not make each row independently removable.

| Disjoint callable-owner delta over baseline | Estimated extra ns/useful byte |
| --- | ---: |
| Packet completion including callbacks | 0.109509 |
| Endpoint state/exchange control | 0.062824 |
| Packet registration including callbacks | 0.057687 |
| Packet construction excluding registration | 0.052713 |
| Remaining garbage collection | 0.034482 |
| Connection native handoff | 0.028411 |
| Remaining runtime scheduling | 0.017215 |
| UDP send syscall subtree | 0.016208 |
| Writer excluding syscall/control | 0.016118 |
| Remaining connection loop | 0.013207 |
| Other process work | 0.007852 |
| ACK processing excluding earlier owners | -0.013637 |

A separate mechanism partition estimates local ECN at 0.061757 ns/byte. That overlaps the callable-owner table and must not be added to it. Holding all other costs fixed, eliminating all of that attributed ECN work would leave approximately 1.380667 ns/byte, above the 1.143818 ceiling. Another isolated ECN or timer optimization is not a complete cost argument. Registration/completion, native ownership exchange and allocation/construction remain material together; a follow-up design must account for the complete remaining 0.298606 ns/byte reduction needed, including interaction costs and immediate callback semantics.

## Disposition and evidence custody

Retain direct canonical identity and the exact eager history maximum as useful candidate components. Reject this combined prototype as a sufficient repair. N3 #1202 remains stopped; draft PR #1230 remains held; no prerequisite issue, accepted replacement graph or OmniFocus state is created. The next design question is whether one bounded request-storage lifetime can remove per-member token copies, descriptor/OOB clones, result-vector allocations and transfer work across immediate registration, native handoff and ordered completion, while keeping canonical registration facts valid for every late holder. That is a question to resolve against retention and ordering obligations, not an already accepted arena or pooling design. The measured direct ECN cost alone does not account for the remaining gap; any ECN-centered proposal must explain and test additional interaction savings. This receipt does not authorize batching policy effects or changing product thresholds.

The local evidence parent is `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/`, mirrored under `/home/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/` on minimax. Preserve both `grouped-plus-recovery-v1/` and `grouped-plus-recovery-v1-replacement1/`, including identity/inventory/build/test logs, observation ledgers, role results and profiles. Source archives, the local-validation manifest, contamination diagnosis, analysis script, and independent analyses/reviews are retained beside or inside those directories. The wrapper verifies archive commit/hash and same-binary identity, records every phase, and never automatically retries.

The contaminated pair charged three build/semantic invocations and two observations: five invocations / 182.668950 measured host-seconds, plus 49.191131 seconds of extraction/quiet overhead. The replacement charged five invocations / 182.501842 seconds, plus 4.159345 seconds overhead. Added total: ten counted invocations / 365.170791 measured host-seconds, plus 53.350476 seconds separately itemized overhead. Cumulative counted history is 173 invocations / at least 2860.616361 measured host-seconds, retaining the earlier unknown setup duration and separately reported overhead. No failed/contaminated observation was discarded from accounting.

**Independent review:** the frozen source integration, lifetime/ordering claims, raw useful-byte and CPU calculations, source/archive identities, native semantic logs, both pairs’ inventoried artifacts, profile interpretations and cumulative accounting were checked. No blocking finding remains in this receipt. The prototype remains an insufficient repair.
