# Native storage prototype for N3 #1202

**Verdict:** the shared-storage prototype still fails the unchanged efficiency requirements: **67.9% of baseline goodput and 34.9% extra CPU per useful byte** in an uncontaminated pair. This continues N3 [issue #1202](https://github.com/the-sarge/quic-go-fast/issues/1202) without changing its efficiency thresholds or creating an implementation frontier. [Draft PR #1230](https://github.com/the-sarge/quic-go-fast/pull/1230) remains held. Related: [recovery representation receipt](2026-11-05-native-recovery-prototype.md), [pipeline design candidate](2026-11-05-native-pipeline-redesign.md), and [stopped plan](../adr/2026-11-04-native-endpoint-efficiency-plan.md).

The frozen source is `c8f0e2654aa4328b101e5dd8e77424b4cb8797be`, based on `a5c59becf2d89ab03f9179be6f7c6aead11c73fa`. Its source archive SHA-256 is `bba2e0603bfc24bb0b9fbafbb67e075dc9bc211697352be5a4f7553e41e5f74f`. The question is whether eliminating repeated request-storage allocation/copying and reducing canonical registration allocation frequency materially improves the complete native pipeline. The design and local semantic checks do not establish that it will meet the unchanged product requirements.

## One logical request, three storage lifetimes

| Storage | Owner and contents | Earliest reuse or reclamation |
| --- | --- | --- |
| Connection sidecar, `diagnosticLocalGroup` | Connection context; packet tokens, member spans, logical metadata and temporary flattened buffer list | After every terminal member is applied. Reentrant close drops references without clearing a sidecar still held by a consumption stack. Error-path sidecars are discarded. |
| Native envelope, `endpointRequestStorage` | Producer before admission, then endpoint/native writer; immutable member geometry and OOB, member result rows, native token and publication fact | Reuse after terminal take and complete protocol consumption. Close, cancellation, fault and timeout alone do not release an active native borrow. |
| Canonical registration chunks | Recovery and all copied token/history/ECN/rich/ACK-carrier references | Slots are never reused. GC reclaims a chunk only after its remaining references disappear. |

Embedding native rows in the reusable connection record was rejected because `nativeOrdinarySender.Close` clears that record array while deadline setup, payload submission or deadline reset may still hold native references. The separate envelope survives through endpoint retention. Reusing the record's existing inline packet array as the consumption view was also rejected: a packet callback can reenter Close and clear later tokens while the outer completion loop still needs them. The selected sidecar owns its own flat packet slice; each member records a `[first,last)` span rather than owning another copied slice. The completion loop keeps a local sidecar pointer across callbacks.

The four existing connection records bound normal sidecar/envelope caching. After successful consumption, packet slices retain at most 128 entries, and sidecar member rows, flattened buffer scratch and native member rows retain at most eight entries each; larger capacities are discarded. Used token, metadata, result, buffer and credit references are cleared before caching. These are idle-cache bounds, not live-request limits. Live slices grow to actual demand, and the existing maximum of 32 members with at most 67 packet entries per member permits 2,144 logical entries; append capacity can exceed logical length. There is no maximum-group preallocation. The old inline record arrays remain for other paths, so the new sidecar cache is additional retained storage rather than a claim of zero memory cost.

## Native transfer and terminal reuse

Building or refused admission leaves the complete envelope, OOB and payload producer-owned. Successful exact admission retains the same prepared member rows without cloning their descriptors or OOB; it still copies the flattened buffer list into the endpoint's existing record for release accounting. The producer can clear its temporary flattened list after that copy, but cannot mutate or reset the admitted envelope.

The producer builds each member's ordinary and GSO OOB forms once using exclusively owned backing. Where supported, they share an immutable base prefix; software fallback uses that prefix. The owned path reuses its envelope's token, publication fact and member result rows. Legacy request forms retain their earlier behavior. Member-specific acceptance, uncertainty, errors and timestamps remain separate from request-level deadline-control errors.

The writer's final atomic publication still follows all member operations and deadline reset. A member return, cancellation flag, watchdog decision or wake is not that fence. After publication the writer only notifies and continues; it does not dereference the old token or envelope. Endpoint reconciliation publishes terminal state and clears its current token under the same mutex used by terminal take. A taken result may still reference envelope rows, so only the connection consumer may reset them after applying the last member. Atomic fields reset through Store operations rather than copying an already used atomic-containing token.

`takeManyInto` detaches results into bounded caller scratch without borrowing endpoint-record storage or allocating a result slice. It retains the correct published count and wake when a smaller destination leaves results behind. The existing `takeMany` wrapper retains its drain-all-after-progress behavior. Never-claimed expiry synthesizes unsubmitted member facts without inventing native publication; claimed requests retain their native storage until actual return even after detach or quarantine.

## Reentrant disposition fixes retained in the prototype

An unfinished packet-buffer tail remains a cold path: `diagFinish` copies its pending tokens to a local slice, removes them from the sidecar, then invokes completion callbacks. It verifies the record's identity and group after those callbacks before continuing. This prevents a reentrant Close/reset from clearing storage still being traversed or causing the old finish operation to recycle a replacement.

Unadmitted group disposition moves the sidecar to the current stack and removes its discoverable group/cache reference before callbacks. It leaves the connection slot occupied by its existing admission identity while payload disposal is active, preventing reentrant construction from creating a fifth group. The old readiness demand is canceled before callbacks, so a callback's replacement demand cannot be withdrawn afterward. Final slot clearing requires the old identity still to match. This error path discards its storage instead of repopulating a cache that a callback may have closed or replaced.

Two red receipts are preserved. `1202-storage-reentrant-red.log` records the partial-tail reentrant-Close panic; that run exited before exercising staged expiry. `1202-storage-successor-red.log` records the assertion that old disposal withdrew replacement readiness. The final green suite covers all three `TestNativeStorageProbeReentrantDisposition` cases: replacement demand, partial tail and staged expiry. These failed runs are evidence of the corrected lifetime obligations, not omitted setup noise.

## Recovery allocation

Recovery now issues canonical registrations from fresh eight-record GC arrays. Every element belongs to the same recovery owner, although a chunk may span spaces, epochs and requests. Issuance advances before callbacks; the allocator drops its head on the eighth issue and drops a partial head on CloseLocal. Full chunks have no allocator chain, no slot is recycled, and the direct token's owner/epoch/pending validation remains unchanged. Packet pooling, immediate registration, ordered completion effects, ECN, eager loss/timer policy and the exact PTO maximum are unchanged.

On the measured 64-bit layout, eight 64-byte registrations occupy 512 bytes. One surviving record can retain seven neighbors, including consumed or unused metadata. An old token also reaches its embedded owner and potentially that owner's current partial chunk. For R distinct registrations retained by semantic token/history/ECN/rich/ACK-carrier holders and P retained owners with partial heads, a conservative backing bound is `512*(R+P)` bytes. R is not merely the pending-token count. Arbitrarily retained historical token copies remain arbitrarily numerous; the bound describes amplification under the intended caller's existing finite holder limits. Normal protocol retirement must continue clearing each record's packet reference.

Eight-record allocation reduces array construction frequency toward one per eight registrations while leaving nominal backing near 64 bytes per registration plus partial-tail waste. It does not promise an 87.5% reduction in allocated bytes or GC work. Sixteen-record chunks increase sparse retention to 1 KiB per lone record. Request-supplied recovery storage would add an issuance interface and couple request lifetime to later protocol evidence without removing that retention issue. Pools, packet embedding and reusable arena slots would require new stale-token and reclamation rules, so none is introduced.

## Corrected prior cost attribution

The preceding recovery receipt's category named “packet completion including callbacks” matched the enclosing `packetEmission.consumeLocal` wrapper. It included actual completion, some endpoint control and outer result consumption. The preserved corrected partition attributes 0.086433413 ns/useful byte to `CompleteLocalPacket`, moves 0.007869709 into endpoint control and retains 0.015205878 as outer consumption. Strict registration-plus-completion excess is therefore 0.144120655 ns/byte, or **35.8% of the prior pair's total 0.402589591 ns/byte CPU excess**. The earlier **41.5%** described the broader wrapper scope and must not be cited as pure recovery cost.

That correction changes neither the earlier measured throughput/CPU ratios nor its failed verdict. The remaining reduction needed to meet its CPU ceiling was 0.298606 ns/byte. Narrowly attributed registration/native allocation and explicit copy/clear work accounted for only about 0.03855 ns/byte; storage changes may affect locality, barriers and detached GC as well, but those indirect savings are not established in advance. Profile shares are whole-role attribution estimates scaled by measured-window CPU per useful byte, not independently removable costs or a prediction of this prototype's result.

## Semantic evidence

The full recovery race suite passed in 70.639 seconds. The final root endpoint/grouped/ordinary/storage race suite passed in 7.050 seconds, and vet/tidy checks passed. Focused evidence includes owned-versus-legacy member facts/OOB, fallback and middle failure, blocked setup/write/reset through detach and overdue inspection, actual-return release, partial drains with remaining wakes, envelope reuse, never-claimed expiry, successive groups, multi-member token spans, partial tails, reentrant disposition, chunk rollover and late mixed outcomes through ACK/Retry/close. Passing these tests does not certify the complete N3 #1202 campaign.

Local evidence is retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/shared-request-storage-v1-local-validation/`, including both red receipts, the final race logs, design/critique memos, corrected prior attribution and `manifest.json` with artifact hashes. The original attribution artifacts remain preserved alongside the explicit correction.

The native Linux build passed the grouped/storage test families and the entire recovery package before either observation. The native binary SHA-256 is `f2ded75466c8ea82bfdc281b36d6d2efbe3e22a52ee212bdb45c10dcc1b3f5a0`. Both roles in both modes used that binary. The archive commit, source hash, binary manifest and observation identities agree. Thirty-seven available artifacts, including the external source archive, were hash-checked locally. The binary remains on the remote host; its recorded identity was checked for consistency, not independently rehashed locally.

## Matched native result

One baseline/prototype pair ran on minimax using Linux amd64, Go 1.27.3, the existing server cores 8–11 and client cores 12–15, seed 1038, a 10-second warmup and 60-second measurement window. The foreign-CPU monitor includes those cores and their SMT siblings. Foreign CPU was 0.100467 seconds for baseline and 0.216162 seconds for prototype; neither observation was contaminated. CPU, block, mutex and memory profiles cover the whole role process. This is a single diagnostic cell, not the full R18 #1038 campaign.

| Metric | Unregistered baseline | Shared-storage prototype | Existing requirement / interpretation |
| --- | ---: | ---: | --- |
| Receiver-useful bytes | 98,993,445,760 | 67,195,639,296 | Same 60-second window |
| Receiver-useful MB/s | 1649.890763 | 1119.927322 | Ratio 0.678789; at least 0.95 — fail |
| Server window CPU seconds | 100.719372 | 92.201199 | Divide by each mode's useful bytes |
| Server CPU ns/useful byte | 1.017434753 | 1.372130691 | Ratio 1.348618; at most 1.10 — fail |
| Control p99 µs | 688.127 | 393.215 | Pass |
| Application send-call p99 µs | 51.199 | 106.495 | Pass; application call, not native return or peer delivery |
| Peak RSS KiB | 29,080 | 30,312 | Pass |
| Lost packets | 1,462 | 0 | Retained as observed |
| Empty seconds / DATAGRAM drops / write errors | 0 / 0 / 0 | 0 / 0 / 0 | No hidden failed role |

The native-call geometry is effectively unchanged: 61.053385 versus 61.073740 calls per useful MB and 16,767.242 versus 16,761.709 bytes per native call. The prototype formed 1,752,253 groups for 4,103,885 buffer members, averaging 2.342062 members per group, with no refused or expired groups in this observation. Different packetization does not explain the efficiency failure.

The earlier combined-recovery pair reached 67.0% of its own baseline's goodput with 38.7% extra CPU/byte. These are separate-run comparisons with different work volumes, not a matched intervention isolating storage. This result establishes that the frozen shared-storage candidate remains insufficient; it does not establish a precise causal improvement over the earlier candidate.

## Remaining cost and instrumentation limits

The following disjoint callable-owner partition uses whole-role CPU sample shares scaled by each mode's measured-window CPU/useful byte. UDP syscall takes precedence, then actual registration/completion, endpoint control and the remaining owners. Rows estimate attribution, not independently removable savings. In particular, outer result consumption is now separate from `CompleteLocalPacket`.

| Work | Baseline ns/byte | Prototype ns/byte | Excess ns/byte |
| --- | ---: | ---: | ---: |
| `CompleteLocalPacket`, including callbacks | 0 | 0.079147 | +0.079147 |
| Packet registration, including callbacks | 0.055759 | 0.123117 | +0.067358 |
| Endpoint state and exchange control | 0 | 0.061686 | +0.061686 |
| Packet construction, excluding registration | 0.428672 | 0.487243 | +0.058571 |
| Remaining runtime scheduling | 0.054374 | 0.078509 | +0.024135 |
| Outer local result consumption | 0 | 0.021539 | +0.021539 |
| Connection native handoff | 0 | 0.017588 | +0.017588 |
| Writer, excluding syscall/control | 0.019308 | 0.033137 | +0.013829 |
| UDP send syscall subtree | 0.261480 | 0.275165 | +0.013685 |
| Remaining garbage collection | 0.006147 | 0.016569 | +0.010421 |
| Other process work | 0.021040 | 0.029186 | +0.008147 |
| Remaining connection loop | 0.095847 | 0.089598 | −0.006250 |
| Other ACK processing | 0.074808 | 0.059647 | −0.015161 |

The CPU ceiling is 1.119178228 ns/byte, leaving **0.252952463 ns/byte** to remove. Registration/completion, endpoint ownership transfer and construction still contribute together. A separate, overlapping mechanism partition attributes 0.063215 ns/byte to local ECN and 0.019245 ns/byte to PTO derivation/maintenance. Those figures must not be added to the owner table or treated as enough to close the gap.

Named diagnostic helpers account for an estimated 0.008918 ns/byte baseline and 0.022559 ns/byte prototype; direct diagnostic-only clock sites add 0.000382 ns/byte in the prototype. The roughly 0.014023 ns/byte instrumentation excess is much smaller than the remaining CPU margin. Other clock work includes required deadlines and pacing and is not classified as diagnostic. Counter-implied diagnostic atomic updates are about 2.79 times denser per useful byte on the prototype, so subtracting sampled helper costs cannot establish an uninstrumented ratio. All reported performance remains instrumented diagnostic evidence.

## Allocation and retention evidence

The default-sampled memory profiles estimate cumulative allocations over each entire role and in-use memory at profile collection. They are neither exact counts nor allocations within the 60-second measurement window. The modes completed different amounts of work; lower raw object totals alone do not show lower allocation per byte.

| Role | Estimated allocated objects | Estimated allocated bytes | Estimated in-use objects | Estimated in-use bytes |
| --- | ---: | ---: | ---: | ---: |
| Baseline server | 95,019,512 | 4,056,053,255 | 9,618 | 1,574,000 |
| Prototype server | 75,026,231 | 6,942,832,532 | 4,950 | 1,594,690 |
| Baseline client | 192,462,016 | 6,707,825,920 | 256 | 525,312 |
| Prototype client | 128,274,573 | 4,194,818,429 | 513 | 1,050,625 |

The prototype's `allocateRegistration` site estimates about 7.077 million allocations of 512 bytes (3.624 GB) against about 56.27 million whole-role sent packets, consistent with eight-record issuance. This supports the intended allocation-frequency mechanism, not elimination of registration storage. The broader registration subtree includes other work and estimates 4.632 GB; it must not be labeled chunk storage alone. Native-envelope/connection-sidecar paths estimate another 164.226 MB; the native member writer still constructs a one-buffer slice and arbitration still allocates. This path is not allocation-free. Packet construction and recovery remain material allocation sources, and this profile supplies no causal comparison with the earlier prototype, which did not collect memory profiles.

The source-level idle token-cache bound is an additional 16 KiB per connection at four retained slices of 128 32-byte `ordinaryLocalPacket` entries, excluding slice headers and the separately bounded member/envelope rows. A common retained capacity of 32 entries would use 4 KiB. Live growth, allocator size classes, registration-chunk retention and the unchanged inline record arrays must also be counted; peak RSS is not a replacement for those lifetime bounds.

## Disposition and accounting

The one native build and two semantic invocations used 28.399140 measured host-seconds; the two observations used 154.098204 seconds. This adds **five counted invocations / 182.497344 measured host-seconds**, plus 4.157757 seconds of separately itemized extraction/quiet overhead. Cumulative history is **178 invocations / at least 3043.113705 measured host-seconds**, preserving prior failed/contaminated attempts, the earlier unknown setup duration and separately reported overhead. There was no replacement or repeated performance run for this source. The diagnostic exited nonzero for the two efficiency failures after both roles and semantic checks succeeded.

Raw profiles, counters, observations, identity, ledger and original artifact manifest are retained under `/Users/josh/diagnostics/quic-go-fast/issue-1202-pipeline-2026-11-05/shared-request-storage-v1/`, mirrored from the same suffix under `/home/josh/diagnostics/quic-go-fast/` on minimax. The external source archive and local-validation folder are adjacent. Supplemental analysis, its scripts and independent review are preserved without overwriting the original measured artifacts.

**Independent review:** the source/lifetime claims, measured identities, raw metrics, artifact hashes, corrected attribution and accounting were checked. No invalidating measurement issue or blocking documentation finding remains.

Recommendation: keep the prototype unshipped and redesign the per-packet recovery representation together with its construction and endpoint-transfer costs. Another isolated cache or ECN optimization has no demonstrated path to the remaining margin. Preserve immediate registration, ordered callbacks, late-token validity and the native-return fence while making the complete cost argument explicit before selecting another implementation. N3 issue #1202 and draft PR #1230 remain held; this receipt creates no prerequisite issue, accepted replacement graph or new policy-batching permission.
