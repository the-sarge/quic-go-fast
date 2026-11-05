# Native transition and projection prototype for N3 #1202

**Verdict:** the latest whole-pipeline experiment still fails the unchanged efficiency requirements: **73.2% of baseline goodput and 31.3% extra CPU per useful byte** in one uncontaminated matched pair. It does not qualify N3 [issue #1202](https://github.com/the-sarge/quic-go-fast/issues/1202). [Draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) remains held. Related: [prior shared-storage result](2026-11-05-native-storage-prototype.md), [pipeline design candidate](2026-11-05-native-pipeline-redesign.md), and [stopped plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md).

The measured source is `faac65e38251e4593b21cfd0fd2ef173cec97ef1`, based on shared-storage source `c8f0e2654aa4328b101e5dd8e77424b4cb8797be`. The source archive SHA-256 is `02b0695311f4fd1e1b7471c43490b2dd84f23a5dce7a1e120aa4dbd4bf653087`; the native binary SHA-256 is `8e4e4ca5c70d7808db0cf1277603b5afb56fc76ea752b93767063771bc007e7a`. A later callback-order correction is preserved at `c09a648c4fcab1f554c5e69a0294e32642a90133`. That corrected source has not been performance-tested, and these measurements must not be attributed to it.

## Hypothesis and change

The experiment combined two related hypotheses: redundant owner turns and endpoint progress consume time without producing work, and shifted per-packet projections repeat avoidable recovery bookkeeping. It retained immediate packet registration, ordered per-packet completion, existing policy and capacity limits, native request geometry, and the independent writer-return fence. It did not integrate reusable canonical recovery cells.

The endpoint gained a bounded Exchange operation that can detach terminal facts and admit one prepared request under its existing state mutex. Existing demand is reconsidered after detachment before new admission, preserving fairness. A full progress pass runs before the operation; the final pass repeats full work only when a newly visible native fact or crossed deadline requires it. Otherwise it settles the affected readiness and writer state. The independent timer runner records its armed deadline under the same mutex. Earlier deadlines and terminal state still wake it, and final native atomic publication still sends an unconditional notification. Lifecycle operations retain conservative progress behavior.

The connection now carries explicit local progress through its loop instead of scheduling another send turn for every consumed result. That progress survives early receive-service continuations and prevents newly available capacity from being hidden behind a stale wait. Generic readiness consumption leaves staged demands to their owner, avoiding theft of a grant arriving just after the staging service's poll. Application-originated send notifications remain separate.

Exchange may detach facts during construction, but recovery callbacks for those detached native facts execute only at the existing consumption checkpoints. Four bounded FIFO entries retain those facts; matching connection records remain occupied until consumption. The consumer freezes a bounded snapshot before callbacks. The later `c09a648` fix prevents endpoint results from overtaking another deferred result appended reentrantly during that snapshot. The measured `faac65e` source lacks that fix; it is useful timing evidence, not a correct final candidate.

Recovery's local qualification projection changed from shifted packet-number slices to 64-PN bit blocks with Fenwick population counts. Its rank query preserves the exact `(b,a]` exclusion interval, while skipped numbers retain their existing `(b,a)` rule. Physical history, unresolved membership and capacity remain authoritative and unchanged. A retired whole-block prefix still requires bounded copying/rebuilding; this is not a universal constant-time claim.

The local ECN logical record sequence changed to a circular deque. In-order coalescing moves the short prefix, prefix retirement advances the head, and arbitrary middle edits move the shorter side. The existing sorted logical sequence, 4,096-record ceiling, feedback constraints and event order remain unchanged. Local and rich-congestion ECN ledgers were not merged.

## Behavioral evidence and source limitations

Preserved red tests exposed redundant self-scheduling, a swallowed readiness grant, staging-grant theft, and reentrant deferred ordering `A,C,B` instead of `A,B,C`. The first three were corrected before the measured source was frozen. The final ordering defect was corrected afterward in `c09a648`; its focused grouped/storage/exchange/turn race suite passed in 1.241 seconds, and independent source review found no remaining blocker in that endpoint/connection scope.

The measured source passed the entire root package locally in 13.181 seconds and the entire recovery package in 21.229 seconds. Focused projection race tests passed in 3.122 seconds, and the broader endpoint/connection race selection passed in 5.547 seconds before the bounded-drain refinement; that refinement's focused race selection passed in 1.272 seconds. Vet and tidy checks passed before freezing `faac65e`. Native Linux preconditions passed the grouped, storage, Exchange and turn probe families and the entire recovery package before either observation. These selected tests are evidence for the experiment, not the full N3 #1202 qualification campaign.

A separate characterization reproduced a preexisting ACK-carrier reset defect: a callback replaces the carrier container, but the enclosing filter later restores an old survivor, yielding packet 11 instead of replacement packet 20. The red log and test are retained. Neither measured `faac65e` nor corrected `c09a648` fixes it. It is particularly relevant to manual cell reuse because resurrected references could name a recycled cell. Passing ordinary tests does not erase this known limitation.

## Matched native result

Both modes and roles used the same native binary on minimax, Linux amd64, Go 1.27.3, server cores 8–11, client cores 12–15, seed 1038, a 10-second warmup and 60-second window. The foreign-CPU monitor included those cores and their SMT siblings. Foreign CPU was 0.099673 seconds for baseline and 0.243687 seconds for the candidate; neither observation was contaminated. No replacement or repeated observation was run.

| Metric | Unregistered baseline | Transition/projection candidate | Existing requirement / interpretation |
| --- | ---: | ---: | --- |
| Receiver-useful bytes | 99,926,725,632 | 73,179,987,968 | Same 60-second window |
| Receiver-useful MB/s | 1665.445427 | 1219.666466 | Ratio 0.732336; at least 0.95 — fail |
| Server window CPU seconds | 100.988689 | 97.141769 | Divide by each mode's useful bytes |
| Server CPU ns/useful byte | 1.010627421 | 1.327436253 | Ratio 1.313477; at most 1.10 — fail |
| Control p99 µs | 753.663 | 458.751 | Pass |
| Application send-call p99 µs | 49.151 | 98.303 | Pass; application call, not native return or peer delivery |
| Peak RSS KiB | 27,284 | 29,812 | Pass |
| Lost packets | 786 | 0 | Retained as observed |
| Empty seconds / DATAGRAM drops / write errors | 0 / 0 / 0 | 0 / 0 / 0 | No hidden failed role |

Native geometry remains effectively equal: 61.054838 versus 61.068936 calls per useful MB, and 16,766.808 versus 16,763.015 bytes per call. The candidate formed 1,977,404 groups containing 4,469,022 members, averaging 2.260045 members per group. No group was refused or expired. The prior shared-storage pair reached 67.9% of its own baseline with 34.9% extra CPU/byte. These are descriptive comparisons across separate runs, not an isolated causal estimate of each source change.

## Cost result and feasibility

The following disjoint callable-owner partition assigns each whole-role CPU sample once, then scales shares by measured-window CPU/useful byte. It is attribution, not an exact window trace or a set of independently removable costs. Subtree labels include their callbacks; mechanism analyses overlap this table and cannot be added to it.

| Work | Baseline ns/byte | Candidate ns/byte | Excess ns/byte |
| --- | ---: | ---: | ---: |
| Packet registration, including callbacks | 0.050686 | 0.124038 | +0.073352 |
| Endpoint state and exchange control | 0 | 0.062721 | +0.062721 |
| `CompleteLocalPacket`, including callbacks | 0 | 0.053360 | +0.053360 |
| Packet construction, excluding registration | 0.432415 | 0.475906 | +0.043491 |
| Outer local result consumption | 0 | 0.025510 | +0.025510 |
| Connection native handoff | 0 | 0.019542 | +0.019542 |
| Remaining runtime scheduling | 0.057032 | 0.074306 | +0.017273 |
| Writer, excluding syscall/control | 0.019983 | 0.036626 | +0.016643 |
| UDP send syscall subtree | 0.252400 | 0.266798 | +0.014398 |
| Remaining garbage collection | 0.004974 | 0.016265 | +0.011291 |
| Other process work | 0.020669 | 0.026095 | +0.005426 |
| Remaining connection loop | 0.090651 | 0.085071 | −0.005580 |
| Other ACK processing | 0.081818 | 0.061200 | −0.020618 |

The CPU ceiling is 1.111690163 ns/byte, leaving **0.215746090 ns/byte** to remove. Even deleting the candidate's entire registration and completion subtrees, an impossible optimization, would remove only 0.177397 ns/byte. Their actual excess over baseline is 0.126711 ns/byte. A recovery-allocation patch alone therefore has no direct cost argument for meeting the requirement.

The connection critical path remains costly: whole-role `Conn.run` samples scale to approximately 0.655656 ns/byte baseline and 0.878444 candidate. A simplified model retaining baseline owner availability would need at most `0.655656 / 0.95 = 0.690164` ns/byte to meet the throughput floor, about 0.188280 below the candidate. This is a model condition, not proof of core saturation or a precise prediction. Off-owner savings cannot automatically be counted as relief for this serial path.

Compared descriptively with the prior source, local ECN attribution fell from 0.063215 to 0.037679 ns/byte and local history qualification/capacity from 0.016823 to 0.010531. Endpoint control was approximately unchanged, 0.061686 versus 0.062721. The mechanism improved some work while leaving substantial costs elsewhere. Reduced function-call or wake counts alone do not establish the required end-to-end result.

Named diagnostic helpers and identified diagnostic-only clock sites account for an estimated 0.006861 ns/byte baseline and 0.020478 candidate. That differential is much smaller than the remaining margin. Other clocks include required pacing and deadlines. Sample subtraction cannot establish an uninstrumented result.

Memory profiles estimate whole-role cumulative allocation, including warmup and shutdown: baseline server 96,797,508 objects / 4,287,964,196 bytes; candidate server 82,518,872 objects / 7,586,240,577 bytes. These are sampled estimates at different work volumes, not 60-second allocation rates or exact counts. They do not justify calling the candidate allocation-free.

## Decision on further representation work

Do not integrate the proposed manual canonical-cell reuse design as the next efficiency repair. Its fresh-allocation target is a small part of the remaining cost, while its correctness surface expands across pending tokens, protocol attachment, physical history, local ECN, rich qualification, retired ACK carriers and callback/iterator borrows. The temporary proposal requires issuance generations, explicit holds, precise transient borrows and separate owner allocation; a simple pool or owner-wide quarantine is insufficient.

Review also left an unresolved rooting case: a completed ACK-only attachment can survive `RemovePath.releaseRetained`; retaining its allocator page changes reclamation compared with the old GC-only representation. Clearing the attachment would require establishing a new terminal-transition argument. Together with the known carrier reset defect, that prevents treating the temporary patch as ready merely because its core tests exist. Preserve the proposal and characterization tests, but run no performance experiment on it now.

An immutable registration run can avoid some manual-reuse obligations and compress repeated member metadata, but it still cannot remove the entire cross-owner gap by itself. Sharing facts is distinct from delaying or batching policy effects. Any later proposal must preserve intermediate observable decisions or explicitly revise and audit the accepted contract; this receipt grants no policy-batching exception.

The topology appraisal found no supported replacement that removes enough work while preserving the present contract. Inline native writes can stall the protocol owner; writer-owned recovery changes callback and decision ownership; restoring a command actor adds a rendezvous that the current pump already avoids. A different queue does not eliminate shared fairness, deadline and lifetime authority. This is not a claim that the existing implementation cost is irreducible.

The next architecture question is how to remove repeated lifecycle work across construction, registration, endpoint transfer and terminal application together. A candidate needs a cost argument covering both process CPU and connection-serialized work, not a promise that less allocation will repair locality everywhere. No tested candidate yet supports an accepted replacement graph or a dispatchable implementation.

## Evidence and accounting

The one native build and two semantic invocations consumed 30.368169 measured host-seconds; the two observations consumed 154.120932. This adds **five counted invocations / 184.489101 measured host-seconds**, plus 4.158084 seconds of source preparation and quiet checks. The wrapper’s precondition total includes 0.134386 seconds of source preparation; that preparation is separated here rather than charged twice. Cumulative history is **183 invocations / at least 3227.602806 measured host-seconds**, preserving prior failures, contamination, unknown historical setup duration and separately recorded overhead. The diagnostic exited nonzero after successful roles and semantic checks because the two efficiency requirements failed. Thirty-seven available artifacts were hash-checked locally; the recorded remote-only binary identity was checked for consistency, not independently rehashed locally.

Raw observations, CPU/block/mutex/memory profiles, counters, source identity, ledger and original inventory are retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/transition-projections-v1/`, mirrored from the same suffix under `/home/josh/diagnostics/quic-go-fast/` on minimax. The source archive and local-validation folder are adjacent. Supplemental analysis and post-freeze review preserve their own hashes without replacing the original measurement inventory. The corrected diagnostic branch and preserved production candidate remain separate.

**Review scope:** independent source review cleared the corrected endpoint/connection checkpoint within its stated scope. Independent analysis recomputed the paired metrics, checked source/binary identity consistency and 37 available artifact hashes, and separated callable owners from overlapping mechanisms. This does not clear the recorded carrier/rooting limitations, certify the corrected source’s performance, or establish a viable replacement architecture.

Recommendation: retain the useful transition/projection experiment and its corrected checkpoint as diagnostic evidence, reject it as sufficient for N3 #1202, and keep draft PR #1230 held. Continue architecture design only with a concrete cross-owner cost reduction; neither another clean rerun nor manual cell reuse is justified by this result.
