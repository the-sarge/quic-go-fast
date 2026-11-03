# E3 #1017 probe route re-audit

## Trigger and preserved state

Execution preflight for [E3 #1017](https://github.com/the-sarge/quic-go-fast/issues/1017) at `3671481b25646e9b1edb937695433e653f5dd2b4` found material owners outside the slice's named files. No implementation code exists. Branch `codex/multipath-e3` is preserved at that base.

| Named-file assumption | Current source fact | Why it cannot be avoided |
| --- | --- | --- |
| Recovery is consumed, not changed | `CanSendPathProbe` (`internal/ackhandler/sent_packet_handler.go:1301`) returns false on the local route; `SentPacketWithContext` (`:336`) panics there; `RegisterLocalPacket` (`internal/ackhandler/local_completion.go:133`) hard-codes non-probe flags | E2's exclusion of validation and MTU probes lives in the recovery owner, so removing it requires an additive registration surface |
| `send_queue.go` owns the borrow | The private route's exclusion is in `nativeOrdinarySender.SendProbe` and `nativeOrdinaryConn.ChangeRemoteAddr` (`native_ordinary.go:206`, `:307`), `register` (`packet_emission_local.go:29`) and the `useNativeOrdinary` MTU guard (`native_ordinary.go:57`) | The adapter is where the seam is removed; `sendQueue.SendProbe` remains the unchanged ungranted route |
| `tr.WriteTo` can carry client probes | `clientProbe` calls `Transport.WriteTo` (`packet_emission.go:655`); on a granted target E4 made that a synchronous wait for native return (`native_response.go:52`) | Using it would block the connection loop, which the route table forbids |
| A probe's path owner can stop queued work | `endpointSubmissionAllowed` (`native_endpoint.go:890`) and queued expiry (`:582`) check only a generation token or a datagram epoch; path retirement sits in the path managers | A queued probe could otherwise reach the wire after its path retired the connection ID it uses |
| An MTU loss callback means network loss | `CompleteLocalPacket` with an unsubmitted outcome calls `queueFramesForRetransmission` (`local_completion.go:182`), which reaches `mtuFinderAckHandler.OnLost` (`mtu_discoverer.go:237`) | Local disposal would otherwise be recorded as PMTU loss, which the route table forbids |

The binding rule makes a newly discovered shared-state effect a slice stop until a scoped re-audit establishes that it fits the existing outcome and one-PR budget. These effects are that case. They change no owner: each one extends a mechanism that E2 or E4 already introduced for this route.

## Accepted design

Keep child #1017, its private typed domain and one independently green product PR. No public activation, new platform, controller formula, wire behavior, goroutine or persisted state enters the slice. `sendQueue.SendProbe`, `Transport.WriteTo` and every ungranted route stay unchanged.

**MTU probes use the existing ordinary reservation.** `mtuProbe` already checks `WouldBlock` before `GetPing`, so refusal consumes no probe state and pending-record capacity is reserved before construction. Recovery gains an additive MTU registration that returns the existing token and records the `IsPathMTUProbePacket` flag. The record keeps the probe's frame handler so that the connection context can tell the finder about local outcomes before the recovery completion runs:

| Native result for an MTU probe | Finder consequence | Recovery consequence |
| --- | --- | --- |
| Accepted | None until ACK or network loss | Existing accepted transition |
| Proven unsubmitted, except for a size error (queued expiry, revocation, zero-submission native result) | Clear in-flight state with no lost entry; the existing probe delay schedules the next attempt | Existing unsubmitted transition; its `OnLost` callback is inert for this handler |
| Proven unsubmitted with a size error | Existing lost transition, because a size rejection is PMTU feedback | Existing unsubmitted transition |
| Uncertain | A later ACK still raises the size; a later loss clears in-flight state with no lost entry, because nothing proves the probe left the host | Existing uncertain transition |

The generation check still discards results from before a path reset.

**Path probes register without a completion token.** A path probe holds no flight, no ECN mark and no RTT sample, and its loss handlers have no protocol effect, so none of the consumers gated by `pending-local` depends on its transmission. It keeps the existing separate probe list and timeout. A token would add a terminal record and tombstone to protect nothing. Its rich-delivery record is registered timing-excluded on the local route, so no completion fact is needed to keep it out of rate samples. Commitment order is unchanged: amplification reservation, packet-number commitment, challenge publication and registration all precede the offer. Refusal after commitment releases the prepared buffer and keeps every charge. Results stay best effort like the baseline's ignored probe write errors; the endpoint fault channel still reports endpoint-wide failure.

**Probes take the connection-control lane and transfer storage on admission.** The offer is atomic, so successful admission cannot race ahead of commitment. Refused readiness is withdrawn immediately, with no retry state: the validation budget, the server's next received packet or the client's existing probe timer owns the next attempt.

**A client probe uses its target transport's endpoint.** The target's private endpoint is the one E4 already records on `Transport.responses`. The connection attaches one additional participant on a distinct target endpoint when first needed and detaches it at connection close. That participant has no separate wakeup: its completion records carry no protocol consequence and each run-loop pass consumes them, so a full mailbox only refuses a later probe until the existing timer retries it. An ungranted target stays rejected with `errLocalPacketRoute`, because a direct write would block the connection loop.

**Path retirement revokes queued probes.** A request may carry the existing `responseEpoch` type as an additional submission permission, checked at queued expiry and before each native submission. Client `Path.Close` and every server path retirement revoke their epoch. A current request keeps its token until actual return.

**Ordinary rebinding is a generation change.** `ChangeRemoteAddr` validates the destination, applies the existing `reset` cancellation of queued and current authority, then updates the destination and source metadata for later requests. `MigratedPath` has already retired the old registrations, so their late results reconcile terminal identity only.

## Boundaries

Client `Path.Switch` across transports stays rejected by `replacePath`. E5 owns that private exclusion, together with live CONNECTION_CLOSE and connection drain. A validated private client path can therefore be probed but not yet switched to. Public activation stays with R19.

## Gates re-run

| Gate | Result |
| --- | --- |
| Representation | Unchanged. Existing parsers own external representations, and internal events carry immutable identities. |
| Artifact classes | Unchanged. No new maintained aid, and the existing transport test suite owns the named scenarios. |
| Evidence budget | Unchanged: at most 16 deterministic scenario tests, one focused race run, one fresh review and at most one replacement. |
| Single owner | Unchanged. The connection path and MTU owners decide protocol consequences, the recovery owner keeps registrations, and the endpoint keeps native storage until return. |
| Authority completeness | Construction, validation, refusal, expiry, revocation, rebinding, retirement and close are all covered by this design. |
| Transitional seams | Narrower: this slice removes the probe and rebinding exclusions. The E5 exclusions are untouched. |
| Blast radius | Now names `native_ordinary.go`, `packet_emission_local.go`, `native_endpoint.go` (submission permission only), `connection.go` (emission intent only), `internal/ackhandler/local_completion.go`, `internal/ackhandler/sent_packet_handler.go` and `internal/ackhandler/congestion_dispatch.go`. |
| Context fit | The 24k dispatch target was exceeded by the focused reads needed to trace these seams. That read is complete, so implementation does not depend on repeating it. |
| Split or merge | No split: recovery admission without the adapters is unusable, and each adapter needs it. E4 and E5 stay separate on the existing merits. |

Contract closure stays as declared: the existing `Probe commitment / amplification` and `Known prefix / proven tail / uncertain native result` rows govern, with no new matrix.

## Design-cost decisions

A tokened path probe is rejected on merit: it would extend the completion lifecycle, its tombstones and the rich-recovery cursor to a packet whose outcome no consumer reads. Preserving `Transport.WriteTo` for client probes is rejected on merit because it blocks the loop. Pre-reserving an endpoint record before path commitment is rejected on merit: it adds a held reservation across packet processing, while the contract already defines conservative refusal after commitment. Polling path-manager state from the connection loop to revoke probes is rejected on merit: an atomic epoch already exists and needs no retirement queue. Counting an uncertain probe's later loss as PMTU loss is rejected on merit because repeated local timeouts could then shrink the path MTU with no network evidence. No revisit trigger applies inside this slice.

## Publication and resumption

Publish the amendment and this receipt in a documentation PR, with docs-only local certification and the applicable hosted checks. After merge, synchronize child #1017's exact plan pointer, reconcile `codex/multipath-e3` with main, emit the revised preflight and implement under the unchanged evidence budget. A later unsafe root, a changed owner or an inability to fit the budget stops for a new decision.
