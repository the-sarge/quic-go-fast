# Native endpoint admission contract (#1141)

**Status:** owner-approved contract, 2026-11-04; N2 implementation and N3 #1202 combined native/STREAM diagnostics complete; independent full R18 #1038 qualification remains incomplete after the [24-observation stopping prefix](../audits/2026-11-12-multipath-r18-final-receipt.md); DATAGRAM/batch-off fails goodput and CPU, and mixed/stateless evidence remains unrun. **Issue:** [Native endpoint refuses stateless and public writes under saturated data traffic (#1141)](https://github.com/the-sarge/quic-go-fast/issues/1141). **Related:** [Ordinary goodput on granted endpoints (#1193)](https://github.com/the-sarge/quic-go-fast/issues/1193). **Inspected base:** `689517410ca2f7cfc4306e9c21eb95c69574d61c`.

The owner approved the admission guarantee, protection of ordinary traffic, existing overload bounds, coordinated endpoint design and separate evidence for the two findings. This contract amends the maintained endpoint plan. [N2 (#1201)](https://github.com/the-sarge/quic-go-fast/issues/1201) implements its admission policy; issue #1141 remains open until the independent N3 and R18 evidence satisfies the remaining qualification criteria.

## Executive decision

An application write or stateless response should enter the endpoint's bounded send queue when its existing allowance is available. A data producer holding a turn must not make otherwise usable capacity inaccessible. Genuine overload, an existing short backoff, shutdown and invalid inputs retain their existing outcomes.

Protect bulk traffic through its existing capacity allowances and fair send order. Keep existing memory limits, deadlines and buffer ownership. Coordinate this improvement with the throughput repair in issue #1193, while requiring separate evidence that each problem is fixed. Run the existing downstream qualification against the final combined candidate.

## Established evidence

The [verification comment for issue #1141](https://github.com/the-sarge/quic-go-fast/issues/1141#issuecomment-6647727197) records a passing deterministic coordinator diagnostic at the inspected base. A pending data admission grant refused public and stateless submissions with zero queued/current requests and zero retained payload bytes. Their readiness was withdrawn while a second data producer remained registered. Withdrawing both data registrations allowed the identical public and stateless submissions to succeed.

The diagnostic used real coordinator and response-adapter operations with a simulated successful native writer on Darwin arm64, Go 1.27.3. It establishes the mechanism. The historical Linux refusal rates remain the observations recorded by [E6 #1020](https://github.com/the-sarge/quic-go-fast/issues/1020); they were not remeasured during this triage.

Current writer service already rotates among connection-control, stateless and public classes, with a data turn after at most two non-data turns when data is available. These are request turns, not byte shares or wall-clock bounds; the current native write is not preempted. The immediate problem is admission: one shared grant prevents a one-shot producer from entering that service order.

The [updated diagnosis for issue #1193](https://github.com/the-sarge/quic-go-fast/issues/1193#issuecomment-6647895525) identifies repeated admission scanning as a major measured throughput cost. A traversal-only probe improves throughput but remains far below the accepted floor and does not demonstrate this admission guarantee. The combined implementation must use that evidence rather than assume either finding has already been resolved.

## Accepted decisions and trade-offs

| Decision | Accepted choice and reason |
| --- | --- |
| Stronger behavior | Require admission when an eligible legal request fits available resources. A minimum success percentage would depend on offered load and request sizes, and would allow unexplained refusals even when resources are free. |
| Real overload | Preserve immediate public backpressure and best-effort stateless refusal when resources are unavailable. No waiting list or hidden retry is created for a rejected call. |
| Existing backoff | Preserve the 1 ms retry floor after local backpressure. A class inside that floor is temporarily ineligible even if its capacity is free. The guarantee does not override this protection. |
| Capacity | Keep all existing global, class and participant limits, charging backing capacity rather than payload length. Increasing buffers does not address the demonstrated refusal at zero occupancy. |
| Persistent data/control producers | Preserve their readiness, fair grant selection, selected-large-request progress, lease duration, cancellation and expiry. A one-shot admission neither consumes nor renews their grant. |
| Writer service | Preserve the existing shared writer, class rotation and data-service protection. Public/stateless admission is not priority to send immediately or a promise of network delivery. |
| Public callers | Preserve the existing synchronous result after successful admission and buffer borrow until actual native return. Concurrent callers share the existing public-class allowance; no new individual caller-order guarantee is introduced. |
| Stateless producers | Cover the existing response class, including resets, Retry, version negotiation, refusal/INVALID_TOKEN and cached close replies. Preserve response authorization, revocation, anti-amplification and existing response/drop policy. Live connection close stays in its existing connection-control route. |
| Lifecycle and errors | Preserve close/fault fences, independent queued expiry, exact-once disposal, identifiable error categories and accepted/unsubmitted/uncertain distinctions. A backpressure error or zero returned bytes alone does not prove safe replay. |
| Coordination | Carry this admission contract into the endpoint design work for issue #1193. Keep both issues and their acceptance criteria; do not assume a faster endpoint also admits these classes correctly. |
| Public scope | Apply to the granted native endpoint route. Preserve ordinary unregistered behavior and the existing public activation decision in [R19 #1039](https://github.com/the-sarge/quic-go-fast/issues/1039). |
| Validation | Use focused deterministic regressions plus affected existing E6 #1020 and R18 #1038 evidence on the final candidate. Keep the existing program accounting and product thresholds; this contract introduces no new campaign or arbitrary success-rate target. |

The alternatives considered were keeping today's best-effort admission, increasing buffers, giving responses unconditional send priority, making rejected calls wait or retry internally, and promising an acceptance percentage. They respectively preserve the demonstrated problem, spend memory without removing its cause, risk starving bulk/control traffic, change caller ownership and blocking semantics, or substitute a workload-dependent target for a precise guarantee. Direct admission within the existing allowances addresses the confirmed cause while retaining the existing limits and writer behavior.

## Exact admission contract

At the point the endpoint control owner processes a public or stateless submission, it must admit an authorized, valid request if all of the following are true:

1. The endpoint and participant admit work under the existing lifecycle and response-epoch rules, and the class's existing retry floor has elapsed.
2. The request fits its existing request-size and class byte limits. The class has a free request slot and its fixed participant has a free retained-completion allowance.
3. Global request and byte limits remain satisfied, and a completion record is available after protecting any selected persistent producer's admission.

An outstanding data/control admission grant, by itself, is not a valid refusal reason. The decision and resource transfer occur under the existing serialized admission owner; concurrent callers consume capacity in that serialized order. Successful admission reserves the completion needed for exact-once resolution. Refusal retains no caller buffer and creates no pending retry obligation for the endpoint. Check the condition at that decision, rather than relying on an earlier sampled occupancy value.

These are class-level admission conditions. They do not promise a success fraction during true overload, a fixed share of bytes, delivery to the peer, or eventual success for every individual public caller. Once admitted, requests retain the existing absolute deadline and completion rules; a blocked native writer can still cause queued expiry.

## Recommended implementation boundary

Keep one authoritative admission/resource owner and one native writer. Within that owner, give public and stateless producers a direct, atomic capacity check and admission path. Keep the readiness/grant path for persistent connection data and connection-control producers. Both paths allocate from the same bounded resource accounting, queue and terminal-result ownership; there is no second endpoint or unaccounted queue.

The existing request caps partition the global 128 requests exactly: data 104, connection control 8, stateless 8 and public 8. The byte caps likewise partition 8 MiB: 6 MiB, 1 MiB, 512 KiB and 512 KiB. Respecting those class caps already protects data/control count and byte capacity from public/stateless traffic. Do not add an unnecessary second count/byte reservation pool that would refuse otherwise usable class capacity.

The 256 completion records are shared rather than partitioned. A selected persistent grant must retain access to one record, including while accumulating its own class's capacity. Before a one-shot request could take the last free record, reconcile/reclaim existing grants and consider already registered eligible persistent producers. If one is selected, protect its record. Direct admission therefore needs two free records with a selected persistent grant, or one without it. Count every used record, including terminal results held by stalled consumers and prepared-but-uncommitted requests; an empty request queue does not imply free completion capacity. Account already materialized reservations once, rather than reserving again for a grant that has already become an admission.

Reclaim cancellation, expiry, backoff invalidation and teardown under that same owner, so a stale grant cannot retain protection indefinitely. One-shot calls must not change a persistent grant's holder, lease start/end, readiness notification or retry floor. Existing per-participant retained-completion limits still apply when global records are free; a stalled consumer must not gain an exception.

The implementation can choose a simpler equivalent representation during the combined endpoint review, but it must satisfy this contract. Its admission path must not add extra synchronous handoffs that recreate the throughput problem identified in issue #1193.

## Amendment to the endpoint plan

The earlier endpoint plan applied round-robin admission turns and the rule of at most two privileged turns before data across a shared grant. That admission-turn interpretation conflicts with direct admission of eligible one-shot work into spare capacity.

The accepted amendment is explicit: readiness/grant arbitration continues to protect persistent connection producers; public/stateless admission is governed by their existing bounded allowances and shared completion-record protection. Preserve the at-most-two non-data rule at writer-service selection when data is available. A stream of one-shot admissions may enter surplus capacity while a data producer holds a grant, but cannot consume that producer's protected capacity, displace its turn, extend its lease, or starve admitted data at the writer.

This is a deliberate policy refinement recorded in the [maintained endpoint plan](../adr/2026-10-24-multipath-endpoint-plan.md#admission-and-capacity-notification). It does not change the existing request/byte/participant limits, write duration, error identities, or public buffer-lifetime contract. Historical receipts remain evidence of their original candidate.

## Acceptance criteria for the implementation brief

- [ ] With a live data grant, free public/stateless allowances, free completion capacity beyond the protected grant record, and no applicable backoff, public and stateless submissions are admitted. The data grant and its lifetime remain intact.
- [ ] Sustained public/stateless submissions cannot consume the selected data/control producer's protected completion record or prevent a selected large request from accumulating its class capacity. Cancellation and lease expiry release protection and wake the correct successor without renewing an abandoned grant.
- [ ] Existing class, participant and global bounds remain enforced, including completion exhaustion despite an empty request queue. Public refusal returns zero bytes with identifiable backpressure, retains no caller buffer, and leaves no retry registration. Eligible zero-length public datagrams retain their existing semantics and consume a request/completion slot; invalid destinations and oversized inputs retain their existing validation behavior.
- [ ] Existing retry floors still suppress immediate re-admission following local backpressure. A class can admit again after its floor when resources permit.
- [ ] With all classes continuously queued, writer service preserves class rotation, participant/path fairness and the existing data-service bound. Stateless pressure cannot monopolize connection-control service. Multiple public callers cannot exceed the shared public allowance or lose/duplicate completions.
- [ ] The public/stateless route families preserve authorization, response epochs, cached storage lifetime, deadline/error identities, close/fault fencing, independent queued expiry and actual-native-return ownership. Reuse their existing lifecycle coverage; add a focused regression only where the admission change creates a new interaction.
- [ ] The existing E6 #1020 contention comparator is evaluated on the final candidate with attempted/admitted/completed or dropped outcomes distinguished as the existing fixture permits. Its current `stateless_sent` count means admission, whereas `public_sent` means successful synchronous completion; neither means peer delivery. Unexplained refusals with eligible unreserved capacity are a failure of this contract; a positive counter alone is insufficient evidence. Historical E6 #1020 observations are preserved.
- [ ] The affected existing R18 #1038 workload reads both ordinary-peer performance and progress of the offered stateless/background traffic. Report offered traffic, reset receive share and background useful bytes alongside ordinary costs. Throughput improvement achieved by starving or dropping that traffic does not establish success. Issue #1193 retains the accepted ordinary-peer thresholds: at least 95% baseline goodput, at most 110% baseline CPU/useful byte, and the existing p99 completion tolerance. This contract adds no reset-success percentage to the existing verdict.

These are semantic acceptance families, not a requirement to create one new test per bullet. Use existing coordinator, response, shutdown and fairness coverage wherever it demonstrates the contract. The implementation plan should choose the smallest discriminating regressions and use the existing operational ledger for affected native observations.

## Delivery and tracking

Keep issue #1141 as the admission enhancement and issue #1193 as the throughput defect. Design their shared ownership/accounting changes together before deciding the implementation split. One shared implementation may resolve both only if it independently satisfies both sets of acceptance criteria; a split must preserve a single authoritative admission model at every merge.

The owner's existing hold on [R17 #1037](https://github.com/the-sarge/quic-go-fast/issues/1037) and [R18 #1038](https://github.com/the-sarge/quic-go-fast/issues/1038) remains the coordination point. Include this accepted admission change in the final endpoint candidate before those downstream qualification results are relied upon, rather than qualifying an intermediate scheduler and changing it afterward. This uses the existing final-candidate evidence requirement, not an additional campaign or weaker thresholds.

Owner approval authorizes this contract amendment, the authoritative issue brief and matching OmniFocus updates. Issue #1193 requires a separately dispatched architecture-handoff for the combined endpoint work; issue #1141 supplies the admission contract to that design. Keep the admission issue open and preserve its acceptance criteria through that packaging. Approval of this contract does not assert that the code or qualification is complete.

The [domain glossary](../../GLOSSARY.md) defines local send admission separately from native transmission, transport delivery and the endpoint write grant over a socket.
