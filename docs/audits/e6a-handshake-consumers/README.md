# E6a handshake/lifecycle consumer preservation

Contract: [E6a](../../adr/2026-09-07-packet-emission-plan.md#e6a--migrate-handshake-and-lifecycle-packer-consumers), recorded at `24b716a20629b7456602102f1db264bf4b0d8c9b`; child [#52](https://github.com/the-sarge/quic-go-fast/issues/52), parent [#24](https://github.com/the-sarge/quic-go-fast/issues/24).

## Preservation mapping

| Existing consumer | Retained or replacement observations |
| --- | --- |
| `TestConnectionIdleTimeoutDuringHandshake` | Same idle error at exactly seven simulated seconds and same local idle-timeout qlog event. Real packer has no pending server crypto data. |
| `TestConnectionHandshakeIdleTimeout` | Same handshake timeout from the aged creation time and same qlog event. |
| `TestConnectionHandshakeServer` | Same ordered CRYPTO/TLS events, Initial key discard and confirmation; session-ticket payload, HANDSHAKE_DONE and nonempty NEW_TOKEN are decoded from real socket output instead of draining the framer in the test. Same successful teardown. |
| `testConnectionHandshakeClient` | Both preferred-address cases retained. A real Handshake flight replaces the fabricated packet and packing callback. Initial-key discard still precedes incoming CRYPTO processing. Synchronized state distinguishes complete from confirmed before and after HANDSHAKE_DONE. Preferred connection ID and reset-token add/remove obligations remain; real packing can activate the preferred ID earlier than the old empty-output mock. Same successful teardown. |
| `TestConnection0RTTTransportParameters` | Same restored transport parameters, accepted-0-RTT state, reduced stream limit, protocol-violation error text and closed-handler replacement. First-flight synchronization observes a real Handshake packet, and close output uses the real constructor. |
| `TestConnectionPacketBuffering` | Same two unavailable-key packets and exact buffering qlog records, third-packet key event, replay order `packet3, packet1, packet2`, exact received-frame/checksum records and successful teardown. Real outbound ACKs are permitted. |
| `TestConnectionVersionNegotiation` | Same Version 2 recreation result, advertised/greased versions and exact negotiation/version-information qlog events. |
| `TestConnectionVersionNegotiationNoMatch` | Same no-match error, peer version and exact negotiation qlog observations. |
| `TestHandshakeMTUFallbackBeforePacking` | ACK opportunity now uses real recovery, pending Initial ACK and real construction. Queued Initial datagram is 1,200 bytes after eligible feedback rather than asserting a 1,200-byte mock argument. |

## Boundary and evidence

One opt-in helper installs the existing packetPacker with real frame sources and recovery. Transparent protection follows connection-owned key retirement/completion state; it does not script packets or lifecycle transitions. Shared default constructors, the broad mock and unrelated consumers remain. Tests and setup are verification aids; this receipt and status transitions are metadata. No maintained verification framework or runtime change is introduced. Existing wire/TLS code owns representation. Behavioral evidence is example-level over the named scenarios; absence of assigned broad-mock consumers is checked over the finite nine-function census. Closure is not triggered.

The original focused family passed before migration. Each migrated scenario passed independently. The MTU output assertion failed with a 1,452-byte datagram when feedback publication was omitted, then passed at 1,200 bytes when eligible feedback was restored. This discriminates the accepted behavior without changing production code or adding a mutation campaign.

The focused family, one focused race run, `go test -count=1 ./...`, `go vet ./...` and `go tool gcassert ./...` passed during implementation. Exact-head local, lint, review and hosted receipts belong in the PR discussion. Source diff inspection must show only these tests and Markdown metadata, with zero production/module/CI changes. No performance capture, new platform sweep or extra repetition is required. No untraced runtime effects are admitted.

E6a completion leaves E6b and E6c ready and E6 blocked by both. Their contracts and mutual independence are retained.
