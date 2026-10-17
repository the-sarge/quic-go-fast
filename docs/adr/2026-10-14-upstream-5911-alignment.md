# Explicit InitialPacketSize floor for the handshake MTU fallback

**Status:** Accepted on 2026-10-16 and implemented for [#756](https://github.com/the-sarge/quic-go-fast/issues/756). **Date:** 2026-10-14. This revision follows the owner's statement that there will be no upstream sync. It narrows the record to the fork's own documented contract. It supersedes the clauses of the [handshake MTU recovery plan](2026-09-06-handshake-mtu-plan.md) named under "Superseded H1 clauses". **Related:** [Upstream compatibility](0001-upstream-compatibility.md), [H1 / PR #20](https://github.com/the-sarge/quic-go-fast/pull/20), [#756](https://github.com/the-sarge/quic-go-fast/issues/756). The trigger was upstream [#5911](https://github.com/quic-go/quic-go/pull/5911) (merge `99c743630f4a96a3c584ed64fb189db6ebd84f38`), but no upstream code is adopted. **Evidence:** [audit folder](../audits/2026-10-14-upstream-5911/README.md).

## Context

Anchors refer to `9b9fd643`. Its `connection.go`, `config.go`, `interface.go`, `send_queue.go`, `packet_emission*.go`, `mtu_discoverer.go`, `packet_packer.go` and `internal/congestion/cubic_sender.go` are byte-identical to `d3648918`, where the analysis was done. The fork's base is upstream `793f74d8` (v0.62 line). There will be no upstream sync, so this record is measured against the contract the fork ships, not against upstream's later changes.

The `InitialPacketSize` godoc the fork ships (`interface.go:168-172`) reads:

> InitialPacketSize is the initial size (and the lower limit) for packets sent. Under most circumstances, it is not necessary to manually set this value, since path MTU discovery quickly finds the path's MTU. If set too high, the path might not support packets of that size, leading to a timeout of the QUIC handshake. Values below 1200 are invalid.

[ADR 0001](0001-upstream-compatibility.md) states: "Existing APIs retain their caller-visible contracts; a performance improvement must not silently impose new buffer-lifetime or ownership obligations on existing callers. Breaking existing APIs for performance is outside the accepted scope."

H1 (shipped in `v0.62.1-fast.1` through `fast.7`) lowers packetization to 1200 bytes after a classified local message-size failure of a pre-confirmation handshake send. It applies whenever the effective initial size exceeds 1200. That size is initialized from `Config.InitialPacketSize`, which `populateConfig` fills with 1280 when unset (`connection.go:532`, `:2590`). H1 therefore also lowers explicitly configured values below the documented lower limit.

- The worker publishes failures for sends whose enqueue metadata is marked `handshake` (`send_queue.go:232`). That flag is set only by `coalescedSendMetadata` (`connection.go:2671`) and is used for nothing else.
- H1 keeps cubic packet-size accounting monotone. ACK growth passes `max(mtu, config.InitialPacketSize)` (`connection.go:2242`), and the cubic setter panics on a decrease (`internal/congestion/cubic_sender.go:322`).

Both path-change seams have two problems:

- They reset congestion and the finder to the raw configured size (`connection.go:965`, `:1345`), so a connection that needed the fallback returns to 1280 on a path change.
- They never lower `maxPayloadSizeEstimate`. This is a confirmed defect: after discovery grows and the path changes, `SendDatagram` admits datagrams that no longer fit, and the packer then silently discards them (evidence folder).

## Decision

### D1. Explicit sizes are a floor; only unset sizes fall back

- **Unset (0).** The effective size is 1280, and H1's fallback to 1200 stays available. This is an addition to the documented contract: it only changes the outcome of handshakes that would otherwise time out.
- **Explicit value.** The value, after `clipConfig`'s existing 1–1199 → 1200 clipping, is a floor that the fallback never lowers. Local message-size failures keep the documented behavior: the send queue swallows them, loss/PTO retransmits at the same size, and the handshake eventually times out. No early close is added.
- **Meaning of "floor".** The configured effective startup and discovery size. Existing early-server sizing (1200 until the client's transport parameters arrive) and ordinary short packets are unchanged.
- **Telling unset from explicit.** `populateConfig` keeps the stored field at 0. A private `(*Config).initialPacketSize()` returns the effective size (1280 when unset). Readers of the effective startup size use the helper: the constructors' congestion and admission initialization, `preSetup`'s effective size, and H1's ACK-growth floor. Raw-field reads remain only for configuration preparation (`clipConfig`, `prepareConfig`) and the unset check.
- **Public documentation.** The godoc keeps every existing sentence and adds one paragraph:

```go
	// InitialPacketSize is the initial size (and the lower limit) for packets sent.
	// Under most circumstances, it is not necessary to manually set this value,
	// since path MTU discovery quickly finds the path's MTU.
	// If set too high, the path might not support packets of that size, leading to a timeout of the QUIC handshake.
	// If left at 0, it defaults to 1280 bytes. Only in that case, if the local network stack rejects a
	// handshake packet larger than 1200 bytes with a "message too long" error before the handshake is
	// confirmed, the connection falls back to 1200 bytes.
	// Values below 1200 are invalid.
	InitialPacketSize uint16
```

The `CONTEXT.md` "Handshake MTU fallback" term and the `README.md` description say the fallback applies only when the setting is unset.

### D2. Detection keeps H1's design; eligibility is classified at enqueue

H1's bounded mailbox, worker, wakeup and run-loop consumption are unchanged. They keep the same ordering, the same `> 1200` size filter, and the same phase and path-generation guards.

- `coalescedSendMetadata` marks a send `handshake` only when the configured size is unset. The configuration is immutable, so this is a connection-owned classification captured at enqueue time, as H1 requires. Explicit-size connections therefore never publish feedback or receive fallback wakeups.
- `applyHandshakeMTUFallback`'s guard set also includes "configured size unset". No publication path, including direct test publication, can lower an explicit floor.

### D3. Congestion: H1's monotone rule stays

Unchanged from H1:

- The fallback does not call `SetMaxDatagramSize`.
- ACK growth passes `max(mtu, initialPacketSize())`.
- The cubic decrease panic stays.
- BBR keeps refreshing its size from `maxPacketSize()` on every bounded emission opportunity.

The construction sizes below keep the panic unreachable from fork code: the connection constructors use `initialPacketSize()`; a migrated cubic sender is created at the D4 start size, which never exceeds that floor.

### D4. Path changes start at the connection's effective size and lower admission

At both seams, client `switchToNewPath` (`connection.go:965`) and the server path switch (`connection.go:1345`):

- **Start size.** `effectiveInitialPacketSize` is used for the `MigratedPath` congestion reset, the finder reset, and `maxPayloadSizeEstimate` (through `estimateMaxPayloadSize`). It is 1200 if this connection took the unset fallback, and otherwise `initialPacketSize()`: 1280 when unset, or the explicit floor.
- **Search ceiling.** The existing peer/local bound: `MaxPacketBufferSize` capped by the peer's `max_udp_payload_size`.
- **Unchanged.** Path-generation increments and queue replacement or rebinding.

Healthy connections keep their configured size across path changes, a connection that recovered through the fallback stays at 1200, and datagram admission matches the packet size.

### Delivery

One standalone fork PR builds on this branch, so the record merges with the code. There is no timing constraint.

## Exhaustive routing

| Semantic class | Unset (0) | Explicit floor F |
| --- | --- | --- |
| Classified message-size failure; pre-confirmation coalesced handshake send; non-GSO; current generation; payload > 1200 | H1 fallback: effective size, finder start or fresh unstarted finder, and admission estimate go to 1200. Congestion size unchanged; the lost packet is recovered by loss/PTO | Not published (no `handshake` mark). Documented behavior: swallowed, loss/PTO at size F, eventual handshake timeout |
| Same, payload ≤ 1200 | H1 behavior: not published; swallowed; loss/PTO | Not published; swallowed; loss/PTO |
| Event for a stale path generation, or observed after confirmation | Ignored | Not applicable (nothing is published) |
| Unmarked send, GSO, MTU probe, path probe, CONNECTION_CLOSE, any post-confirmation send | Existing behavior: swallowed | Existing behavior: swallowed |
| Non-size send error | Existing behavior: fatal | Existing behavior: fatal |
| Client migration or server path switch | Start at the effective size (1200 after fallback, else 1280) for the congestion reset, finder reset and admission estimate; peer/local ceiling | Start at F for the same three; same ceiling |
| MTU growth acknowledged | Admission estimate follows the discovered size; congestion size is `max(mtu, 1280)` | Admission estimate follows; congestion size is `max(mtu, F)` |
| No error (healthy start) | Unchanged: client starts at 1280; server uses 1200 until the client's transport parameters, then 1280 | Unchanged: client starts at F; server uses 1200 until transport parameters, then F |

## Superseded H1 clauses (on acceptance)

Only these clauses change. Every other H1 constraint stands, including its congestion clauses and approach stops.

- "Metadata identifies a pre-confirmation handshake-flight send and the current connection-owned path generation": the `handshake` mark is also conditioned on an unset configured size (D2).
- "The connection owns an effective initial packet-size value initialized from the existing config in both constructors": it is initialized from `initialPacketSize()`, and only an unset configuration may lower it.
- The matrix rows "Client oversized Initial before transport parameters" and "Server oversized flight after parameters, before confirmation" apply only to unset configurations. Explicit configurations keep the documented timeout behavior.

H1 did not specify path-change sizing. D4 adds it, consistent with H1's "Preserve the smaller start after confirmation".

## Rejected alternatives

- **Fail fast and close on an explicit-size failure (upstream #5911's behavior).** Rejected by the owner on 2026-10-14. The fork's documented contract specifies a handshake timeout. Without an upstream sync there is no parity reason to add a new close path. That path also carried most of the design risk the review found: error identity, role and phase observation, ordering under a full send queue, and special 1200-byte tests.
- **Keep H1's lowering of explicit values.** It contradicts the documented "lower limit", which violates ADR 0001.
- **Upstream's synchronous direct write.** It would bypass the fork's write authority, BBR byte credits, batched and segmented submission, and per-entry size-error attribution. H1 already rejected synchronous handshake writes.
- **Remove the cubic panic and lower the congestion size (upstream #5911).** Rejected. Only fork code calls the setter, none of it decreases the size, and H1's monotone rule is validated.
- **Restart every unset connection at 1200 after a path change (upstream #5911).** Rejected. It shrinks packets on healthy connections, while the connection's own effective size already carries the fallback evidence.
- **A public option selecting the behavior.** H1 already rejected a public fallback switch.

## Non-goals

These H1 non-goals are unchanged: silent black-hole recovery, post-confirmation shrink, sub-1200 paths, immediate local-loss retransmission, and new congestion algorithms. Also out of scope:

- an early close or a new error for explicit sizes;
- silent-drop recovery, tracked by the separate handshake silent-drop plan, which depends on this record;
- the probing-after-path-change defect below.

A related defect was confirmed and kept separate. After a path change, `mtuFinder.Reset` (`mtu_discoverer.go:176`) sets `lastProbeTime`, and the probe gate (`connection.go:2624`) does not check `DisablePathMTUDiscovery` or DF. A connection with discovery disabled and no DF therefore starts MTU probing after migrating. Evidence is in the audit folder. Upstream fixed the equivalent in #5887. The fork fixed it separately in [#761](https://github.com/the-sarge/quic-go-fast/issues/761) by [#762](https://github.com/the-sarge/quic-go-fast/pull/762) (`4d89a66e`), before this record was implemented.

## Acceptance criteria

- [ ] Unset config: client and server end-to-end cells complete the handshake and exchange stream data, using a UDP wrapper that rejects sends larger than 1200 bytes. The existing `TestHandshakeMTUFallback` cells are adapted from explicit 1452 to unset.
- [ ] Explicit config (1452) with the same wrapper and a short handshake idle timeout: no size reduction occurs and the dial fails with the handshake timeout. A focused case shows that an explicit-size connection marks no send `handshake`, and that a directly published event leaves the effective size, finder and admission estimate unchanged.
- [ ] The focused `TestHandshakeMTUFallback*` and `TestSendQueueHandshakeMTU*` cases are rewritten for unset configurations. Stale-path and post-confirmation guards still ignore events.
- [ ] Congestion: the existing lower-then-grow regression still passes with an unset configuration (monotone, no panic). BBR behavior is unchanged.
- [ ] Path change: after discovery grows, a client migration or server path switch makes `SendDatagram` with a payload above the new estimate return `DatagramTooLargeError`. The start size is 1280 for a healthy unset connection, 1200 for one that took the fallback, and F for an explicit one.
- [ ] Config preparation: `config_test.go` (`:136`, `:184`) and `transport_test.go` (`:580`) assert that the stored value stays 0 and the effective `initialPacketSize()` is 1280.
- [ ] A healthy-start control shows unchanged sizing: client startup at 1280 or F, and the server at 1200 before transport parameters. Same-head Linux, macOS and Windows hosted checks pass.

## Validation

```sh
GOTOOLCHAIN=go1.27.2 go test -count=1 -run 'TestConfig|TestTransportConfigPreparation|TestHandshakeMTUFallback|TestSendQueueHandshakeMTU|TestConnectionHandshake|TestSendQueue|TestMTU|TestConnectionMigration' .
GOTOOLCHAIN=go1.27.2 go test -race -count=1 -run 'TestHandshakeMTUFallback|TestSendQueueHandshakeMTU|TestSendQueue' .
GOTOOLCHAIN=go1.27.2 go test -count=1 ./internal/congestion
GOTOOLCHAIN=go1.27.2 go test -count=1 -run 'TestHandshakeMTUFallback|TestPathMTUDiscovery' ./integrationtests/self
```

Run `GOTOOLCHAIN=go1.27.2 go test -count=1 ./...` once at the final reviewed head, then collect same-head hosted Linux, macOS and Windows checks. Repeat only for changed code or a diagnosed failure.

## Review history

`ras consider` run `20261014T203010-912fcc0e102516216627414a` reviewed the first draft. That draft adopted upstream #5911's fail-fast close and congestion changes ahead of an expected sync. The run retained the decisions and queued 8 corrections. After the owner confirmed there will be no upstream sync, this revision narrowed the scope:

| Queue item | Disposition in this revision |
| --- | --- |
| 1 — Native cause | Moot: no close path is added |
| 2 — Role and phase | Moot: explicit failures keep the documented timeout |
| 3 — Feedback ordering | Moot: H1 ordering is unchanged and there are no fatal events |
| 4 — Explicit 1200 | Moot: explicit sizes are never acted on |
| 5 — Public contract | Applied: existing godoc retained plus an unset-only paragraph; floor qualified (D1) |
| 6 — Sizing terminology | Applied: reader rule (D1); D4 uses the connection's effective size instead of a blanket 1200 |
| 7 — Test/sync inventory | Config-test adaptations kept (Acceptance); sync reconciliation dropped |
| 8 — H1 supersession | Applied to the narrower change |
