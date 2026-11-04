# R14 #1034 close, diagnostics and smoke re-audit

## Trigger and preserved state

Execution preflight for [R14 #1034](https://github.com/the-sarge/quic-go-fast/issues/1034), under parent [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013), at `d5c7a99f37977ec138ecb3b9de60a766a009086f` found material owners and seams outside the slice's named files. No implementation code exists. Branch `claude/1034-r14-close-survivors` is preserved at that base. The [current normative R14 contract](../adr/2026-10-24-multipath-reno-plan.md#r14) owns implementation and finite evidence. This record owns the evidence and rejected alternatives.

| Accepted assumption | Current source fact | Why it cannot be avoided |
| --- | --- | --- |
| The connection owner can pack a close for any path | `packConnectionClose` reads `pathZeroContext`, `pnManager` and `getDestConnID` only (`packet_packer.go:201`), and `emission.close` refuses once zero retires (`packet_emission.go:818`) | A survivor's close needs its own DCID, packet number and nonce |
| The endpoint retains the send lifetime of each close | A participant has one close slot (`native_endpoint.go:172`). `sendClose` uses the path-zero destination, and `drain` skips target participants (`native_ordinary.go:348`, `:359`) | A second path, or a path on a target endpoint, has nowhere to deposit |
| The closed handler owns per-path references | `ReplaceWithClosed` installs one payload per runner, and a reply goes to the trigger's source address (`transport.go:1108`, `closed_conn.go:33`) | A cached reply would carry the wrong path identity, or follow an abandoned tuple |
| Draining uses the largest path PTO | Both close paths use `3*observedRTT().PTO(false)` (`connection.go:2688`, `:2707`) | That can undercut a slower path |
| qlog needs only added fields | `frames.encode` panics on an unknown frame (`qlog/frame.go:137`), and `toQlogFrame` passes path frames through to it (`connection_logging.go:44`). A qlog-enabled private multipath connection can panic | The frame encoders are required, not optional |
| A sink cannot block the owners | `FileSeq.record` holds its mutex across a channel send (`qlogwriter/writer.go:145`), and `Recorder` is an open interface | The connection goroutine can stop on a stuck sink, so dropping needs a bounded buffer in front of it |
| The smoke needs only a driver adapter | Nothing advertises `initial_max_path_id` (`connection.go:367`, `:502`). `enableMultipathCredit` requires a confirmed handshake, a limit no higher than two issued CIDs, and is reachable only from fixtures (`multipath_credit.go:65`). R16's guard is blocked by R14 | No handshake can reach the wire profile, yet R10 assigned the first live peer to R14 |
| The named files bound the slice | `logging/frame.go` no longer exists. `multipath_*.go`, `native_*.go`, `packet_emission.go`, `packet_packer.go`, `transport.go` and the qlog writer seam are absent from the list, though the contract's own owners live there | The binding rule makes a newly discovered shared-state effect a slice stop until re-audited |

None of these changes an owner. Each extends a seam that R7 to R12 introduced for this case, except the smoke activation, which is a new transitional seam the binding discipline allows for a declared complete scenario.

## Accepted design

Keep child #1034, its private typed domain and one independently green product PR. No exported method, new platform, controller formula, wire format, persisted state or new connection worker on the grant lifetime enters the slice. The 16-scenario ceiling, focused race run, review budget and two charged smoke instances are unchanged.

**Close selection.** The registry enumerates close contexts in path ID order at one point in `handleCloseError`. A context is a member that is live, has no terminal, has a usable destination CID and whose endpoint is not quarantined. That means at most two contexts under the two-slot cap. Validating candidates are excluded, because they have no validated tuple. A connection without endpoint admission keeps today's path-zero behavior, and an ordinary connection takes no new branch.

**Packing.** Each context gets its own close packet from one packer method that takes the same identity triple as `shortHeaderIdentity`. Path zero keeps `packConnectionClose`. Each packs to its own path's size limit. A reason too long for one path costs that context, not the others. Packing still follows `drain` and precedes `CloseLocal`, so packet numbers stay valid. `emission.close` stops refusing on retired zero and reads no released CID state, because a retired zero is simply not selected.

**Live submission.** The same close goes through each selected context, since the peer may have lost any one of them. `drain` covers target participants, so each cutoff holds per endpoint. A participant holds a bounded list of close requests rather than one slot, each released under the existing deadline rule. A failed packing or deposit drops that context from the cached set. The close outcome is an error only when no context took its packet, and a connection with no eligible context sends nothing and keeps the absorb-only closed handler.

**Cached close.** One closed handler serves every runner with one shared exponential backoff counter, so two transports cannot double the response budget. Each retained response holds its own packet, tuple and target transport. A trigger routed to a path with a response uses that response. Any other trigger, such as one on an abandoned or validating path's CID, uses the lowest-ID retained response. Neither case sends to the trigger's source address or uses an abandoned context. Each transport keeps its own response-epoch child, so retirement revokes queued sends, and an endpoint that closes refuses them.

**Retention.** Multipath closing and draining retain routing for three times the largest-PTO snapshot at entry, using saturating arithmetic, and the single timer is not refreshed by arriving traffic. Reset-token and CID-routing removal stay single-shot. Ordinary connections keep their existing expression.

**Diagnostics.** The frame encoders cover every draft-21 frame the parser admits. `packet_sent`, `packet_received`, `packet_lost` and `mtu_updated` gain an optional path identity that is absent for ordinary connections, so their JSON is unchanged. Nonzero-path recovery is built with no recorder today (`internal/ackhandler/sent_path.go:30`), so it gets a decorating recorder that forwards only attributed loss events. Its other recovery events stay absent. Lifecycle events record copied facts at the registry owners. They expose no public schema promise: all path fields stay marked private.

**Sink independence.** Recovery, ECN, crypto and AEAD capture the recorder at construction (`connection.go:361`, `:407`, `:491`, `:536`), so replacing `c.qlogger` at activation would leave them calling the sink synchronously. A connection with a recorder therefore passes one stable indirection to all of them from construction. It forwards synchronously until private multipath activates, then switches to a bounded drop-on-full buffer with one goroutine. After the switch, connection-side `Close` only signals the worker. It neither joins a blocked worker nor closes the inner sink synchronously, because `run` closes the recorder before cancellation and ownership release (`connection.go:872`). The worker closes the sink when it can, and it never joins the grant lifetime.

**Smoke activation.** One unexported `Config` field is the fixture-only activation guard. Only a `picoquic` build-tagged test can set it, and R16 absorbs it. It advertises the extension, limits issued path-zero CIDs and activates credit, CID sets and the frame parser at handshake completion, only when the peer also advertised. That is after transport-parameter application on the client and after confirmation on the server (`connection.go:1145`), before 1-RTT frames can be processed. The guarded route relaxes `enableMultipathCredit`'s confirmed-handshake check to completion. Long-header retransmission and path-zero close keep their behavior until confirmation. Nonzero-path recovery admission, path opening and MTU startup stay confirmation-gated, so the client opens path 1 after confirmation. A peer that does not advertise stays ordinary. The tagged test drives a real granted endpoint with `picoquicdemo -M`, and the client role opens path 1 through `openPathPrivate`. The role's receipt must show useful payload on both paths, stream abort and close cleanup. R1's ordinary control is unchanged.

## Rejected alternatives

- **One close on the primary path only.** The primary is the path most likely to have just failed silently, and the contract names each retained identity.
- **One packet shared by all runners.** It reuses one packet number and one CID across tuples and is the defect the slice removes.
- **Reply to the trigger's source tuple.** It would reopen an abandoned or unvalidated tuple and adds amplification exposure.
- **Silently ignoring triggers on abandoned CIDs.** A peer still sending there has not seen a close, so it would wait out its idle timeout.
- **Make `FileSeq` non-blocking.** That changes ordinary qlog completeness and does nothing for caller-supplied recorders.
- **Move the smoke to R16 or R17.** R10 and this slice's audit already placed it after close authority, and moving it changes the graph, which requires `$architecture-handoff`.
- **Build the smoke on the R16 constructors.** R16 is blocked by this slice.

## Stops and untraced effects

A wire incompatibility found by the smoke is a slice stop unless its fix is inside the traced owners and the same evidence budget. Failed smoke attempts charge the shared ledger with a recorded cause. The [E6 receipt](2026-11-03-multipath-e6-receipt.md) leaves 112 instances and 28,258 host-seconds, with the scheduled inventory already 14 instances above that capacity. R14's two instances fit. The owner's stated willingness to raise the ceiling has not been recorded as a raise, so this record does not change it.

The local Docker surface stays example-level evidence, like R1. Full matrix qualification stays with R17, and path-level continuation after an endpoint fault is [R20](../adr/2026-10-24-multipath-reno-plan.md#r20), by owner decision on 2026-11-04.

R14 owns the fault-to-close transition on the private route. A primary endpoint fault reaches `destroyImpl` as `immediate: true` (`connection.go:773`, `native_ordinary.go:519`), and `handleCloseError` then removes routing without emitting. With an eligible context on another endpoint, that error closes through the survivor instead of destroying silently. Selection checks quarantine for every endpoint, including the connection's own, because `routeUsable(nil)` reports true without inspecting it. With no such context, and on every ordinary connection, immediate destruction is unchanged.
