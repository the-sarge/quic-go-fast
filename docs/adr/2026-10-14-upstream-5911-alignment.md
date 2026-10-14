# Upstream #5911 handshake packet-size alignment

**Status:** Proposed. Revised after [`ras consider`](#review-history), with the owner's choice of upstream's text-only error guarantee. If accepted, it supersedes the clauses of the [handshake MTU recovery plan](2026-09-06-handshake-mtu-plan.md) named under "Superseded H1 clauses". **Date:** 2026-10-14. **Related:** [Upstream compatibility](0001-upstream-compatibility.md), [H1 / PR #20](https://github.com/the-sarge/quic-go-fast/pull/20), upstream [#5911](https://github.com/quic-go/quic-go/pull/5911) (merge `99c743630f4a96a3c584ed64fb189db6ebd84f38`, parent `53a52d50e877f8d7522790da400a7a2776dbe5c4`), upstream [#5815](https://github.com/quic-go/quic-go/issues/5815). **Evidence:** [audit folder](../audits/2026-10-14-upstream-5911/README.md).

## Context

Anchors refer to `9b9fd643`. Its `connection.go`, `config.go`, `interface.go`, `send_queue.go`, `packet_emission*.go`, `mtu_discoverer.go`, `packet_packer.go` and `internal/congestion/cubic_sender.go` are byte-identical to `d3648918`, where the analysis was done. The fork's upstream base is `793f74d8`, 55 upstream commits behind #5911.

The fork's H1 fallback lowers packetization to 1200 bytes after a classified local message-size failure of a pre-confirmation handshake send. It applies whenever the effective initial size exceeds 1200. That size is initialized from `Config.InitialPacketSize`, which `populateConfig` fills with 1280 when the field is unset. So H1 lowers explicitly configured values as well as the default (`connection.go:532`, `:2590`).

- The worker classifies failures at `send_queue.go:233`.
- The run loop consumes them only inside emission (`packet_emission.go:123`, `packet_emission_bbr.go:72`). Emission is reached only after the outer send-capacity gate (`connection.go:760`).
- H1 keeps cubic packet-size accounting monotone. The ACK-growth update passes `max(mtu, config.InitialPacketSize)` (`connection.go:2242`), and the cubic setter still panics on a decrease (`internal/congestion/cubic_sender.go:322`).

Upstream #5911 makes these changes:

- `populateConfig` keeps `InitialPacketSize` at 0. The effective size comes from a new `(*Config).initialPacketSize()`.
- The run loop writes coalesced packets directly and synchronously until a packet of at least the configured Initial size has been written. The MTU finder is created at that point.
- When the field is unset, a message-size error on that write creates the finder at 1200, lowers `maxPayloadSizeEstimate`, and calls `SetMaxDatagramSize(1200)`.
- When the field is explicit, a message-size error on any direct write is returned from sending. The run loop closes through `setCloseError(&closeError{err: err})`. That non-immediate path converts the error into a local `INTERNAL_ERROR` `TransportError` whose message is the native error's text. Upstream's integration test accordingly asserts only `ErrorContains(err, syscall.EMSGSIZE.Error())`.
- After the finder exists, the send queue still swallows message-size errors.
- The cubic decrease panic is removed.
- Both migration seams start at `cmp.Or(InitialPacketSize, 1200)` and store the matching admission estimate.
- The godoc gains: "If set to 0, it defaults to 1280 bytes and falls back to 1200 bytes if the first write at that size returns a "message too long" error. An explicitly set value is kept as the lower limit; a "message too long" error at that size fails the handshake instead."

The godoc the fork ships today already reads "InitialPacketSize is the initial size (and the lower limit) for packets sent." (`interface.go:168`).

[ADR 0001](0001-upstream-compatibility.md) states: "The fork preserves quic-go's existing public API and wire compatibility while allowing optional new APIs for datagram performance." and "Existing APIs retain their caller-visible contracts; a performance improvement must not silently impose new buffer-lifetime or ownership obligations on existing callers. Breaking existing APIs for performance is outside the accepted scope."

## Decision

### D1. Public `Config.InitialPacketSize` contract follows upstream

- **Unset (0) is fallback-eligible.** Connections start at 1280. An eligible message-size failure lowers packetization to 1200.
- **An explicit value is a floor, including 1280 and 1200.** `clipConfig` already clips values from 1 to 1199 to 1200, and they become a floor of 1200. Here, "floor" means the configured effective startup and discovery size, and fallback never reduces it. It does not change existing early-server sizing: the server uses 1200 until it has the client's transport parameters. It does not make ordinary short packets, such as ACK-only Initials, larger.
- **An eligible failure under an explicit floor closes the connection** through the existing non-immediate local-error close path. That path attempts `CONNECTION_CLOSE` and records a local `INTERNAL_ERROR` `TransportError`. Its message contains the text of the native error accepted by `isSendMsgSizeErr`: EMSGSIZE on Unix, the classified `WSAEMSGSIZE` error on Windows. Matching upstream, only that text is promised. `errors.Is` identity with the native error is not promised, and no fork-only cause-preserving close is added. Where the failure is observed depends on role and phase; see "Failure observation".
- **The fork never lowers packetization below an explicit value.**
- **Readers of the effective startup size use `initialPacketSize()`.** That covers the constructors' congestion and admission initialization, `preSetup`'s effective size, and `maxPacketSize()` before the finder exists. The `max(mtu, configured)` ACK-path floor is deleted by D3. Raw-field reads remain correct, and only allowed, in three places:
  - configuration preparation (`clipConfig`, `prepareConfig`);
  - explicitness checks (`InitialPacketSize != 0`);
  - the migration start expression in D4.
- **`populateConfig` keeps the field at 0.**
- **Public documentation.** The standalone PR replaces the `InitialPacketSize` godoc with the fork-accurate text below. The `CONTEXT.md` "Handshake MTU fallback" term and the `README.md` description say that only unset configurations fall back. At the sync, the fork's godoc is kept deliberately instead of upstream's first-write wording. Upstream's first-write statement is a sufficient condition the fork also meets, but it does not describe the fork's wider window.

```go
	// InitialPacketSize is the initial size for packets sent.
	// Under most circumstances, it is not necessary to manually set this value,
	// since path MTU discovery quickly finds the path's MTU.
	// If set to 0, it defaults to 1280 bytes. If the local network stack rejects a handshake packet
	// larger than 1200 bytes with a "message too long" error before the handshake is confirmed,
	// the connection falls back to 1200 bytes.
	// An explicitly set value is never lowered by this fallback. Instead, a "message too long" error
	// for a handshake packet before the handshake is confirmed closes the connection with an error
	// whose message contains the system error's text. Dial returns that error if the handshake has
	// not completed yet; otherwise it becomes the connection's close cause.
	// If the path silently drops packets of the configured size, the handshake times out.
	// Values below 1200 are invalid.
	InitialPacketSize uint16
```

### D2. Detection stays on the fork's asynchronous, generation-fenced path

The fork keeps H1's bounded mailbox, enqueue-time classification and connection-owned decision method. It does not adopt upstream's synchronous run-loop write, and it changes no queue, authority, credit, batching or segmentation contract. Changes:

- **Eligibility.** Enqueue metadata records whether the connection's floor is explicit. The worker publishes a classified message-size failure for a handshake-marked, non-GSO send in two cases: the floor is explicit, at any payload size; or the floor is unset and the payload exceeds 1200 (H1's existing rule).
- **Mailbox.** The mailbox retains the native error with the latest event, for its text, and never retains the buffer. It keeps H1's "latest event, coalesced wakeup, non-blocking publish" semantics. No other replacement policy is introduced.
- **Guards.** The run-loop method applies H1's phase and generation guards to both branches: pending, unconfirmed, current path generation. For an unset floor, the worker's `> 1200` size filter and the consumer's `effectiveInitialPacketSize <= 1200` guard apply, followed by the D3 fallback. For an explicit floor, neither size check applies, and the method closes as D1 requires. Stale-generation and post-confirmation events are ignored for both.
- **Ordering.** The connection loop consumes handshake feedback before the outer send-capacity gate (`connection.go:760`), as well as before packing on iterations that skip waiting. A pending explicit failure is therefore acted on even while the send queue is full. After an explicit-floor event is consumed, the iteration exits ordinary emission immediately: no new ordinary packing, queue handoff or BBR reservation. Already queued packets keep their existing drain behavior. No new deadline is promised.

### D3. Congestion packet size follows upstream

- The cubic `SetMaxDatagramSize` decrease panic is removed, byte-for-byte as upstream.
- The run-loop fallback method calls `sentPacketHandler.SetMaxDatagramSize(1200)` for the unset fallback.
- ACK growth passes the discovered MTU directly. The `max(mtu, configured)` floor is deleted.
- BBR already refreshes its size from `maxPacketSize()` on every bounded emission opportunity, so no BBR change is required.
- The send queue still edits no MTU or congestion state.

### D4. Migration follows upstream

Client `switchToNewPath` (`connection.go:965`) and the server path switch (`connection.go:1345`) use two separate arguments:

- **Start size.** `cmp.Or(InitialPacketSize, 1200)` is used, unchanged, for the `MigratedPath` congestion reset, the finder's start, and `maxPayloadSizeEstimate` (through `estimateMaxPayloadSize`, which only subtracts header and tag overhead). Finder initialization does not clamp the start.
- **Search ceiling.** The existing peer/local bound, `MaxPacketBufferSize` capped by the peer's `max_udp_payload_size`, as upstream's `maxMTU()` computes it.

This closes a confirmed admission defect, which is recorded in the evidence folder.

Consequence: after a path change, every unset connection restarts at 1200 bytes, healthy or not, with the reduced datagram admission budget that size implies. Discovery can grow it back if it is enabled. Whether it stays at 1200 when discovery is disabled depends on the separately undecided upstream #5887 gating. Today the fork's `mtuFinder.Reset` (`mtu_discoverer.go:176`) sets `lastProbeTime`, and the probe gate (`connection.go:2624`) does not check `DisablePathMTUDiscovery` or DF. A connection that needed the unset fallback stays at 1200 instead of returning to 1280.

### Delivery

Land D1–D4 as one standalone fork PR before the next upstream sync, under the acceptance criteria below. At the sync, the #5911 hunks resolve to the fork side plus identical helpers, and #5887 is reconciled separately.

## Exhaustive routing

| Semantic class | Unset (0) | Explicit floor F |
| --- | --- | --- |
| Classified message-size failure; handshake-marked non-GSO send; unconfirmed; current generation; payload > 1200 | Fall back: effective size, finder start or fresh unstarted finder, admission estimate and congestion size go to 1200. The lost packet is recovered by existing loss/PTO | Close per D1, observed per "Failure observation" |
| Same, payload ≤ 1200 | Existing behavior: swallowed, loss/PTO, eventual handshake timeout | Close per D1, observed per "Failure observation" |
| Event for a stale path generation, or observed after confirmation | Ignored | Ignored |
| Unmarked send, GSO, MTU probe, path probe, CONNECTION_CLOSE, any post-confirmation send | Existing behavior: swallowed | Existing behavior: swallowed |
| Non-size send error | Existing behavior: fatal | Existing behavior: fatal |
| Event pending while the send queue is full | Consumed before the capacity gate | Consumed before the capacity gate; emission stops |
| Client migration or server path switch | Start at 1200 (congestion reset, finder start, admission estimate); ceiling is the peer/local bound | Start at F (same three); same ceiling |
| MTU growth acknowledged | Admission estimate and congestion size follow the discovered size | Same |
| No error (healthy start) | Unchanged: client starts at 1280; server uses 1200 until the client's transport parameters, then 1280 | Unchanged: client starts at F; server uses 1200 until the client's transport parameters, then F |

### Failure observation (explicit floor)

| Role and phase | What the application observes |
| --- | --- |
| Client, before handshake completion, for example a rejected first Initial | `Dial` returns the local close error. Its message contains the native error's text, and it arrives without waiting for the handshake timeout |
| Client, after handshake completion but before confirmation | `Dial` has already returned. The returned connection closes, and its close cause (`context.Cause(conn.Context())` and later stream or datagram errors) carries the same text |
| Server, during its pre-confirmation flight | The server connection closes locally and attempts `CONNECTION_CLOSE`. If that close is delivered, the client observes a remote `INTERNAL_ERROR`. Its reason phrase normally carries the server's error text, but that is not promised. The native identity and the text cannot be guaranteed across the wire, and no notification deadline is promised |

## Superseded H1 clauses (on acceptance)

Only these clauses change. Every other H1 constraint stands.

- "The connection owns an effective initial packet-size value initialized from the existing config in both constructors": the effective size is initialized from `initialPacketSize()`, and only the unset configuration may lower it.
- "The sender reports only classified message-size errors for such non-GSO sends whose UDP payload exceeds 1200 bytes": extended by D2 for explicit floors only.
- "An error at 1200 or below retains existing timeout/error behavior; below-minimum path support is not claimed": superseded only for eligible explicit-floor failures, which close per D1. Unset configurations keep this behavior, and sub-1200 paths are still unsupported. The matching table row "Non-size error, unmarked send, GSO/PMTU/path probe, or payload <=1200 | Preserve existing disposition; no handshake fallback" is superseded only in its "payload <=1200" part, and only for explicit floors.
- "Keep congestion-controller packet-size accounting monotone: do not call `SetMaxDatagramSize(1200)` after construction at a larger size. On later estimate growth, pass at least the configured initial size to that setter until actual discovered size exceeds it." and "If inspection proves another path can violate that monotonic relation, stop for an owner-level design correction instead of removing the panic": replaced by D3, which is that owner-level decision.
- The rejected alternative "lower the congestion setter directly": superseded only for D3's specified unset-fallback decrease and direct-growth update.
- The approach stop "correctness requires changing ... congestion semantics beyond preserving the monotone floor": superseded only for D3's decrease and growth behavior.
- The matrix row "Application admission and congestion growth after reduction | Estimate decreases; later growth cannot call the monotone setter with a smaller size": replaced by D3.

The rejected alternatives "make handshake writes synchronous on the connection loop" and "teach the send queue to edit MTU/congestion state" are reaffirmed.

## Rejected alternatives

- **Adopt upstream's synchronous direct write.** It would bypass the queue's write authority (`connAuthority` and `writeAuthorized`), BBR byte-credit reservations released per queue entry, and batched or segmented submission. It would also bypass per-entry size-error and MTU-feedback attribution, which later fork contracts rely on (Darwin batch, Windows datapath). It covers only the first full-size write.
- **Keep H1's contract (lower explicit values).** It contradicts the shipped godoc ("and the lower limit") and upstream's new godoc. That violates ADR 0001's "Existing APIs retain their caller-visible contracts". It also defeats CONNECT-UDP/CONNECT-IP callers that depend on a floor.
- **A fork-only cause-preserving close, so that `errors.Is(err, EMSGSIZE)` holds.** The owner rejected it on 2026-10-14. It would be a stronger-than-upstream public promise and add a permanent divergence in the close path. Immediate destruction would preserve the cause but suppress `CONNECTION_CLOSE`.
- **Keep the cubic panic and the monotone floor.** The panic removal merges without conflict anyway. Upstream now decreases the size intentionally, so any future upstream decrease caller would crash the host process only in the fork. The accounting difference is about 7%. BBR already follows real packet size.
- **Resolve semantics inside the sync merge.** A simulated full sync to `99c74363` conflicts in 23 files. Changing a public contract inside that merge hides it from focused review.
- **Add a public option selecting either behavior.** H1 already rejected a public fallback switch, and it would add API surface without measured need.

## Non-goals

These H1 non-goals are unchanged: silent black-hole recovery, post-confirmation shrink, sub-1200 paths, immediate local-loss retransmission, and new congestion algorithms. Also out of scope:

- notification deadlines;
- `errors.Is` identity for the native error;
- the rest of the upstream sync;
- adopting upstream #5887;
- benchmarks, extra platforms and repeated hosted runs.

Release notes carry the behavior change through the normal release process; that is not an acceptance gate here.

## Acceptance criteria

- [ ] Unset config: client and server end-to-end cells complete the handshake and exchange stream data, using a UDP wrapper that rejects sends larger than 1200 bytes. The existing `TestHandshakeMTUFallback` cells are adapted from explicit 1452 to unset.
- [ ] Explicit 1280, using the same wrapper: client initial-flight rejection makes `Dial` return an error whose message contains the native classified error's text (`ErrorContains`, as in upstream's `TestHandshakePacketSizeFallback`), without waiting for the handshake timeout.
- [ ] Explicit 1200, using native-error injection on the first write at 1200 bytes: the same `Dial` result. Unset failures at or below 1200 keep their existing disposition.
- [ ] The selected close path still attempts `CONNECTION_CLOSE`.
- [ ] The existing focused and role-aware cases cover two more situations:
  - a later unconfirmed client failure after `Dial` returned: the connection closes with the same text in its close cause;
  - server-only flight rejection, with an unrestricted client and a deliverable close packet: the client observes a remote `INTERNAL_ERROR`.
- [ ] Ordering: a pending explicit failure is consumed while the send queue is full. After it is consumed, neither emission path performs new ordinary packing, handoff or BBR reservation. Queued packets keep their specified drain behavior.
- [ ] The focused `TestHandshakeMTUFallback*` and `TestSendQueueHandshakeMTU*` cases are rewritten for the new contract. Stale-path and post-confirmation guards still ignore events on both branches.
- [ ] Congestion: the lower-then-grow regression asserts that the cubic size goes 1280 → 1200 → the discovered size, without a panic. BBR behavior is unchanged.
- [ ] Migration: after discovery grows, a client migration or server path switch makes `SendDatagram` with a payload above the new estimate return `DatagramTooLargeError`. An unset config migrates at 1200; an explicit config migrates at its floor.
- [ ] Config preparation: `config_test.go` (`:136`, `:184`) and `transport_test.go` (`:580`) assert that the stored value stays 0 and the effective `initialPacketSize()` is 1280.
- [ ] A healthy-start control shows unchanged sizing: client startup at 1280 or F, and the server at 1200 before transport parameters. Same-head Linux, macOS and Windows hosted checks pass.

## Validation

Focused commands follow H1's shape:

```sh
GOTOOLCHAIN=go1.27.2 go test -count=1 -run 'TestConfig|TestTransportConfigPreparation|TestHandshakeMTUFallback|TestHandshakePacketSizeFallback|TestSendQueueHandshakeMTU|TestConnectionHandshake|TestSendQueue|TestMTU|TestConnectionMigrationMTUDiscovery' .
GOTOOLCHAIN=go1.27.2 go test -race -count=1 -run 'TestHandshakeMTUFallback|TestHandshakePacketSizeFallback|TestSendQueueHandshakeMTU|TestSendQueue' .
GOTOOLCHAIN=go1.27.2 go test -count=1 ./internal/congestion ./internal/qerr
GOTOOLCHAIN=go1.27.2 go test -count=1 -run 'TestHandshakeMTUFallback|TestHandshakePacketSizeFallback|TestPathMTUDiscovery' ./integrationtests/self
```

Run `GOTOOLCHAIN=go1.27.2 go test -count=1 ./...` once at the final reviewed head, then collect same-head hosted Linux, macOS and Windows checks. Repeat only for changed code or a diagnosed failure.

## Consequences at the sync

Simulating #5911 alone onto `d3648918` with `git merge-tree --write-tree --merge-base=53a52d50` gives these conflicts:

- `connection.go`, 7 blocks: the struct field, `switchToNewPath` (×2), the server path switch, `applyTransportParameters`, `sendPackedCoalescedPacket` and `maxPacketSize`.
- `config.go`, 1 block: `prepareConfig` against the new helper.
- `connection_test.go`, 4 blocks.

These merge without markers today: the cubic panic removal, the godoc paragraph, and `populateConfig` keeping 0. After that, two fork-only reads see 0 when the field is unset:

- `preSetup` sets `effectiveInitialPacketSize` (`connection.go:532`). This breaks client packing and BBR sizing loudly.
- The ACK-growth floor (`connection.go:2242`) silently loses its effect.

With this record implemented first, the #5911 hunks resolve to the fork side:

- The `interface.go` godoc is edited on both sides and conflicts; keep the fork's text.
- Drop upstream's direct-write `sendPackedCoalescedPacket`, because the fork's packing lives in `packet_emission.go`.

Test reconciliation at the sync:

- Upstream's unit test `TestConnectionInitialPacketSizeFallback` calls the removed `sendPackedCoalescedPacket` and the old `Send` signature. Drop it in favor of the fork's focused cases.
- Upstream's `connectionOptHandshakeConfirmed` now also creates a finder. Reconcile it with the fork helper.
- The fork's PR places its `TestHandshakePacketSizeFallback` cells in `integrationtests/self/mtu_fallback_test.go`, the path upstream adds. The sync then surfaces an add/add conflict on one file instead of a duplicate symbol across two files.
- Migration-hunk context from upstream #5887 (`Reset` without a start, plus explicit `Start` gating) is reconciled separately.

## Review history

`ras consider` run `20261014T203010-912fcc0e102516216627414a` reviewed the first draft. It used six reviewers, two rounds and adjudication, and no stage failed. It retained D1–D4 and queued 8 corrections. This revision dispositions them as follows:

| Queue item | Disposition | Where |
| --- | --- | --- |
| 1 — Native cause | Owner chose upstream's text-only guarantee. The close uses the existing non-immediate path; a cause-preserving close is rejected | D1, Rejected alternatives, Acceptance |
| 2 — Role and phase | Accepted | Failure observation table, D1 godoc, Acceptance |
| 3 — Feedback ordering | Accepted | D2 Ordering, routing row, Acceptance |
| 4 — Explicit 1200 | Accepted: first-write injection at 1200; size guards are unset-only | D2 Guards, Acceptance |
| 5 — Public contract | Accepted: fork-accurate godoc, qualified floor | D1, healthy routing row, Consequences |
| 6 — Sizing terminology | Accepted | D1 reader rule, D4 arguments and consequence |
| 7 — Test/sync inventory | Accepted; conflict counts unchanged | Acceptance (config tests), Consequences |
| 8 — H1 supersession | Accepted | Superseded H1 clauses |

The run also rejected some claims: a stale-overwrites-current race, conflict-count corrections, permanent stall or promptness guarantees, peer-clamping of the estimate, a mandatory CHANGELOG gate, and broader scope.
