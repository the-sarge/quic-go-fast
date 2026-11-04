# R10 #1030 selected-path route re-audit

## Trigger and preserved state

Execution preflight for [R10 #1030](https://github.com/the-sarge/quic-go-fast/issues/1030), under parent [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013), at `4df82ca934d89ee0a6f4016f7083d84e7e0848d7` found material owners and seams outside the slice's named files. No implementation code exists. Branch `codex/r10-reliable-selected-path` is preserved at that base. The [current normative R10 contract](../adr/2026-10-24-multipath-reno-plan.md#r10) owns implementation and finite evidence. This receipt records the trigger and routing decision; it does not introduce a second contract.

| Accepted assumption | Current source fact | Why it cannot be avoided |
| --- | --- | --- |
| Emission can send through the selected path's endpoint context | `nativeOrdinarySender.Send` always submits through the sender's one participant, destination, OOB and write state (`native_ordinary.go:229`), and a pending reservation is taken on its one endpoint (`native_ordinary.go:198`) | A packet sealed for path 1 would leave on path zero's tuple, or be reserved on the wrong endpoint for a target transport |
| Ordinary submissions on a path respect its cutoff | Only probes carry a path's `responseEpoch` (`native_ordinary.go:344`). An ordinary request has none, so `retire` cannot revoke it and the fence query cannot see it (`native_ordinary.go:371`) | R9's abandonment signal must follow every submission on that path; ordinary traffic on a nonzero path is new here |
| Path recovery can register whatever the selected path packs | `AddPathWithRTT` leaves `local.ecn` nil while keeping `enableECN` (`internal/ackhandler/sent_path.go:27`, `:32`), so an ECT mark is refused as `errLocalPacketMode` (`internal/ackhandler/local_completion.go:144`) | The first ECN-capable STREAM packet on path 1 would fail the connection |
| The scheduler can read eligibility | Liveness, the destination CID, terminal state and the fence epoch are all owned by the registry (`multipath_admission.go:36`, `multipath_credit.go:27`) | Emission selects but cannot own that state; the plan already gives it an immutable view |
| A terminal transition leaves reliable work with its logical owner | Admission paths held only probes until now. Accepted STREAM data outstanding on a retired path has no opportunity to return to its owner, since only the emission loop queues a PTO (`packet_emission.go:497`) and a terminal path is never selected | R10's own Fixtures clause forbids excluding teardown, and the data would otherwise wait for an idle timeout |
| Admission probes keep their turn | R9's deferred C-001 and C-008: probes are selected only from `sendAny` (`packet_emission.go:177`, `connection.go:2893`), so sustained `SendAck` holds them back until their deadline | The R9 receipt on this child assigns that fix to R10, whose selection precedes dispatch at this seam |
| Registry retention snapshots | R9 declared them on the connection observation until R10 samples per-path RTT (`multipath_credit.go:184`, `:211`, `:289`, `:330`). The plan requires the largest path PTO | Real ACK samples on path 1 begin in this slice, which ends that declared seam |

The binding rule makes a newly discovered shared-state effect a slice stop until a scoped re-audit establishes that it fits the existing outcome and one-PR budget. These effects are that case. None changes an owner: each extends a seam E3 to E5 or R7 to R9 introduced for it.

## Accepted design

Keep child #1030, its private typed domain and one independently green product PR. No exported method, new platform, controller formula, wire behavior, worker or persisted state enters the slice. The BBR route stays excluded, because `enableMultipathCredit` already refuses it, so `sendBounded` is unchanged.

**Selection.** Emission calls a pure private scheduler at the top of each dispatch iteration, before recovery's opportunity is read. The registry supplies a bounded, immutable view of each eligible path: live, not terminal, with a usable destination CID. A path with a pending PTO goes first, so a probe elicits its ACK on the path that needs one. Otherwise the private primary is used, then the lowest eligible ID. Congestion, pacing and local capacity are not inputs, so backpressure cannot move traffic. R15 replaces only the primary's source.

**Route.** A nonzero selection becomes one route value: context, destination CID, recovery, tuple, target transport, epoch and write state. A nil route is path zero, so ordinary and default-off connections take the existing branch with no new allocation. Emission reads send mode, ECN, pacing and PTO from the route's recovery, and the packer takes the route for CID, packet numbers and sealer. The temporary path-zero packer adapter ends for application packets. Close and coalesced packets stay on path zero, as R14 and the handshake own them.

**Submission.** The native sender takes the same route, so reservation and submission use that path's participant, destination and OOB. Its requests carry `identity.path`, which keeps results distinct across endpoints, and the member's existing established epoch. That epoch already has a fence entry, so R9's retire, signal and release ordering covers ordinary traffic without a new owner.

**Batching.** A batch is bound to one route because selection runs only between batches. A nonzero route packs at `MinInitialPacketSize` until R11 validates its PMTU. A target transport's route never segments, because its capability is not copied from the primary. A route on the connection's own socket uses its own write state, so its `EIO` fallback stays per tuple.

**Terminal reschedule.** Each service pass drains a terminal path's outstanding packets through recovery's existing probe queue, which declares them lost for rescheduling and keeps the retained evidence. Local packets stay pending until their result. Unsubmitted results already return their frames through `CompleteLocalPacket`. A late ACK finds no outstanding frames to retire twice.

**Probe opportunity.** Admission probes move out of `prepareEmission` into their own opportunity before dispatch, with the `SendNone` stop and `CanSendPathProbe` guards kept. They keep the existing one-probe-then-retry pacing.

**Declared seams.** A nonzero-path packet carries no DATAGRAM until R11, which avoids the packer's discard-on-no-fit branch at a size that path did not admit. MTU discovery stays on path zero and runs only while path zero is selected. Registration on a nonzero path records use of that path's CID through the existing path CID owner and skips path zero's counter. Received abandonment of path zero keeps its connection close, because the root recovery, close and flow-control observations still assume path zero. R12 owns that survivor work.

**Retention snapshots.** The registry takes the largest measured PTO over path zero's observation and its members. An unmeasured path contributes nothing, so a default-valued object cannot inflate retention.

## Rejected alternatives

Splitting the route into a behavior-preserving predecessor would add a child with no reachable caller, because the route is only meaningful once a packet is sent on it. A second sender per path would duplicate reservation, record and result ownership that the endpoint and one sender already hold, and the plan requires one writer boundary. Keeping selection on recovery's root would make the root aggregate per-path modes, which contradicts R7's per-path controllers. Carrying DATAGRAMs on a nonzero path now would make R11's non-destructive selection a behavior change. Pinning nonzero paths to `ECNNon` would remove the registration failure, but it would also remove per-path ECN, which the plan's owner table assigns to the path, and the existing local evidence already fits one instance per path. Each is rejected on merit.

## Gates

Representation, the universal runtime guarantee, artifact classes, closure rows, the 16-scenario ceiling, the focused race run and the review budget are unchanged. No operator decision, adjacent slice or different PR boundary is required. Traced additions are `connection.go`, `multipath_admission.go`, `multipath_credit.go`, `packet_emission_local.go`, `native_ordinary.go` and `internal/ackhandler/sent_path.go`. `framer.go`, `retransmission_queue.go` and `send_stream.go` remain the logical owners and are expected to need no change, because a frame's handler does not depend on the path that carried it. `SetMaxDatagramSize` on a nonzero controller is left untraced, because that path stays at its admitted minimum until R11.
