# Multipath #646: second consideration response

This records the author's disposition of consideration `20261024T211358-95949b13efe81498dc2b5581`, which reviewed the proposed [design](../designs/quic-multipath.md) at `2d2d947aee3717e67dabc5732f4038a63fee495f`. Production evidence remains `f35df9d7b016a06174288901f3337ccc2fd7347c`. The run completed with 18 raw findings consolidated into eight document recommendations: three medium and five low. All six initial reviews and the final conflict round completed. One R2 adjudication (`agy-argon`) failed run-ID validation; its initial and R3 outputs succeeded. No high-severity finding survived adjudication.

The user authorized another manual design pass addressing these eight recommendations, still without implementation. I agree with all eight as narrowed by the synthesis. None establishes a need to replace the ownership model or add an arbitrary line/slice allowance. This revision supplies proposed contracts and bounded paper traces; it does not accept the product direction, amend an ADR, implement an API, execute a test/campaign or claim that this new revision has independently passed RAS. The [first response](2026-10-24-multipath-646-consideration-response.md) remains the record of the earlier review.

## Dispositions

| Item | Disposition and document result | Remaining acceptance / cost |
| --- | --- | --- |
| F1: target CID lengths | Addressed with equal effective local CID lengths for open/migrate, including initialized targets and custom generators; reject before attachment and credit/ID mutation. | Proposed first-profile compatibility rule; CID/emission/public rows already cover attachment and configuration gates. |
| F2: observable detachment | Addressed with proposed `Conn.WaitTransportDetachedV1(ctx, t)`, covering all this connection's uses of a transport, outstanding submissions, routing retention and close races. | D0 accepts the API and old-tuple routing-retention bound. Existing lifecycle/emission/public allowances cover the work. |
| F3: stalled submission | Addressed by explicitly proposing acceptance of indefinite retirement under existing I/O contracts, with visible pending state and no replacement-capacity escape. | D0 must accept that failover limitation or choose and resize a bounded/cancellable I/O contract or narrower profile. No universal completion deadline is claimed. |
| F4: validation contention | Addressed with one candidate per protocol path, two total, one local open/migrate operation, same-path serialization and absolute deadlines. | This increases the previous one-candidate allowance, but adds no full path context. D0 accepts the bounded-state budget; existing lifecycle forecast remains provisional. |
| F5: retirement semantics | Addressed with pre-initiation versus consumed-ID cancellation, explicit closure of initiated failures, receipt-based retention and a proposed default for last-path remote abandonment. | D0 may instead select the draft's bounded replacement/wait alternatives. No idle-timeout substitute or unconditional immediate-close requirement is invented. |
| F6: ECN accounting | Addressed by preserving cumulative counters and sender accounting with the surviving PN space, separately from tuple validation. | Preserve ADR 0009's deferred-BBR restriction while old marked traffic remains unresolved; exact adapter mechanics belong to implementation design. |
| F7: replenishment | Addressed with a two-endpoint state table: no additional unused-ID pre-grant at full capacity, fence and retention admission before slot release, then independent credit/CID exchange. | D0 accepts this policy and measured retention budgets; time to restore redundancy is a separate consumer metric. |
| F8: interop accounting | Addressed with a deliverable/category/stage map in the design and [sizing JSON](2026-10-24-multipath-646-sizing.json). Adapter, image, workflow and case accounting belong to test/support forecasts. | Numerical ranges and candidate slices are unchanged; historical colocated-test counts keep their original narrower scope. |

All “addressed” results are document dispositions. Proposed product choices remain proposed, and issue #646 remains open for acceptance and a separately audited handoff.

## Bounded paper traces

These are manual state/contract checks against the pinned code and proposed rules, not executable test results or proofs of an implementation. Future acceptance cases should reproduce the same boundaries.

### F1 — Target decoder compatibility

The baseline fixes the target routing length in `Transport.init`, parses short-header CIDs using that length, and gives `packetUnpacker` a connection-wide length; `AddConnRunner` registers existing CIDs unchanged. For a four-byte origin and an initialized eight-byte target, both open and migrate now stop at the proposed compatibility check: no target registration, ID consumption, credit change or cutover occurs. The same outcome applies to an uninitialized target whose effective configured generator length is eight; initialization cannot silently change that to four. An independent custom generator returning four passes this particular gate, while ordinary policy, capability, CID availability and validation gates still apply. Equal length does not mean every other target configuration is accepted.

### F2 — Safe transport release

Baseline `Transport.close` destroys registered handlers. Therefore “abandon A; immediately close A's transport” remains unsafe when the transport is still attached; successful `AbandonV1` only accepts retirement. Under the proposal, fence completion, required routing retention and removal of the last registration precede a successful detachment observation. Closing afterward cannot destroy this connection through a stale registration or an already-captured close callback. A concurrent close that wins first can terminate the connection and must not produce a false successful observation.

If A and B share the transport, retiring A does not detach it while B remains live. After migration from T1 to T2, cutover success likewise does not detach T1: its pending completions and necessary old-tuple routing must clear first. Protocol history retained on the connection can still process late ACKs arriving through T2. The success guarantee is per connection and remains valid until an explicit new attachment; callers must serialize reattachment against close and account for other users of the transport. A wait timeout changes none of these states.

### F3 — Non-returning and partially accepted submissions

Pause A's callback before actual submission, then receive remote abandonment over healthy B. Processing it revokes future submission permission; A remains submission-pending and charged to its slot until the in-progress call resolves. The design does not call callback entry “sent,” declare the fence complete, release borrowed buffers or promise timely reciprocal signaling. B can continue eligible work, but restoring redundancy and keeping the connection alive against peer/timeouts are not guaranteed. This is the explicit limitation requiring owner acceptance.

If the callback never returns, the same bounded outstanding state remains held; a replacement cannot create another worker in that occupied slot. Connection termination does not authorize releasing memory still borrowed by the callback. If it returns a definite accepted prefix, resolve that prefix as submitted, suppress further unsent tails/retries after revocation, and release each completed/discarded reservation once before declaring the fence complete. Until the prefix is known, do not guess which buffers are free. Exact failure/partial-write handling must continue respecting the existing external-I/O terminal-error contract.

### F4 — Two paths and one candidate per path

With path 1 validating as a backup and path 0 receiving an authenticated rebinding, each uses its own candidate slot; path 0's received-byte budget does not fund path 1. With both established paths rebinding simultaneously, retain one path-qualified candidate for each, at most two total. Equal tuples on distinct wire IDs do not merge their accounting.

If another tuple arrives for a path already validating, keep the admitted candidate through its absolute deadline or earlier completion/cancellation; defer additional validation instead of adding a candidate or replacing it repeatedly. A competing local operation waits/reports contention within its context. The peer may retry after capacity becomes available. Cancellation with a pending send fence does not prematurely free the slot. This handles the cited cross-path cases without claiming progress for unlimited same-path tuple churn or increasing full path membership.

### F5 — Initiation, cancellation and last path

Before commitment to initiation, cancelling an open releases local reservations and does not require a wire abandonment. After commitment/submission may occur, the path ID stays consumed; cancellation or failed validation enters cutoff-driven retirement and signals abandonment on a usable survivor. A definite no-send after that conservative commitment does not justify reusing the ID. The context error returns before asynchronous wire cleanup is necessarily complete.

Receiving the reciprocal abandonment starts the recommended three-PTO retention interval for issued-CID/PN evidence; returning a context error or receiving an ordinary ACK is not that event. If the reciprocal signal never arrives, retaining required metadata or choosing protocol-permitted escalation remains a bounded admission/lifetime decision, not automatic deletion or ID reuse. On remote abandonment of the only open path, the proposal selects prompt closure by default and explicitly preserves the draft's optional bounded replacement/wait alternatives for D0. These distinctions follow [draft §§3.1 and 3.4](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-3.4).

### F6 — Same-ID ECN continuity

For one retained PN space, suppose receive counts are `(ECT0=40, ECT1=0, CE=2)` at tuple cutover. A late, previously unseen old-tuple ECT0 packet advances them to `(41,0,2)`; a new-tuple CE packet advances them to `(41,0,3)`. Duplicate reception does not increment them again. Changing tuple validation state cannot reset these cumulative counts. Separately, feedback for a marked transmission from the old generation updates its retained sender accounting but cannot certify the new tuple's ECN capability. This matches the separation visible in `received_packet_tracker.go`, `ecn.go` and the baseline migration handler; for BBR, unresolved old marked traffic continues to block revalidation under [ADR 0009](../adr/0009-opt-in-bbrv3.md).

### F7 — Replacement with asymmetric grants

The design's table starts with C and S both at maximum ID 1, paths 0/1 live, and no unused ID-2 window. C's pending fence keeps both slots charged. After the fence and retained-state reservation, C can advertise maximum 2, but cannot create an ID-2 CID window while S still permits only 1. S independently completes its retirement and capacity transition before raising its own limit. Usable CIDs at the common permitted ID then enable client initiation; message loss/reordering or absent peer CIDs can delay it. Old path 1 remains separately retained and can receive late feedback without occupying an active slot once the transfer is safe.

If S withholds credit, or either endpoint cannot reserve retention capacity, no assumed round trip forces progress. The surviving path may keep carrying traffic while redundancy remains reduced. A fence that never completes stalls before capacity release. This separates fence delay, control/CID availability and validation delay, without assuming all old packet ACKs must arrive first.

### F8 — Deliverable and forecast coverage

The private adapter and initial smoke are assigned to M4; M5 extends that same fixture into the required full matrix. Image/dependency pins span M4/M5, while workflow preflight, executed-case/skip accounting and evidence capture belong to M5. These are all inside Reno-first test/support line edits, including non-Go files. BBR/aggregation matrix extensions belong to M6's deferred test/support allowance. Production code used by the fixture remains in its production row, and M7 is not counted twice. Native campaign execution/bookings remain excluded. No deliverable identified by this review is left without a category or stage owner; the classification does not establish an empirical cost measurement.

## Disagreements with stronger claims

Accepting the narrowed eight recommendations does not accept the raw review's stronger premises. Different same-length generators do not establish routing incompatibility, and optional stateless resets do not mandate identical reset keys. Detachment and interop work were not proven absent from every prior allowance. Per-path candidates are a proposed contention policy, not an IETF requirement. The prior ECN wording did not order a counter reset or imply a mandatory connection error. The draft does not impose a universal additional RTT or automatic three-PTO connection failure.

The synthesis also correctly rejected the alleged acceptance-time requeue contradiction and same-ID migration prohibition. This pass does not add application-frame discard, automatic expiry of outstanding CID mappings, mandatory new fuzz targets, or an ordinary congestion-window gate for PTO probes. Existing RTT and other owner decisions remain choices rather than silently accepted changes.

## Validation and scope

Documentation whitespace, all 15 relative file links and 15 Markdown tables across the changed Markdown files, JSON parsing, unchanged pinned inventories/historical yardsticks and numerical forecasts, additive milestone/stage totals, and the new accounting references passed local checks. The inventory remains 74 principal files / 23,840 physical lines; candidate slices remain 18–41 Reno-first / 22–52 full scope. Historical measurements were compared unchanged with the reviewed commit rather than rerunning the earlier replay. The source inspection and paper traces above support the document changes; they do not validate runtime behavior. No Go build/test, interoperability execution, benchmark, native campaign, production implementation or new RAS run was performed in this pass.
