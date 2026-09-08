# Active send-connection publication

Contract: [issue #123, implementation brief](https://github.com/the-sarge/quic-go-fast/issues/123#issuecomment-5593394707). Audited baseline: `5e9ae2ab3e46067335ea409a689b638a82877bd3`.

## Ownership audit

`Conn.conn` is the active `sendConn` interface slot. Constructors initialize it and bind `packetEmission.conn` to its address before exposing the connection. After initialization, `packetEmission.replacePath`, called by `Conn.switchToNewPath` on the connection goroutine, is its only production writer. `packetEmission.connMutex` protects that assignment and `activeConn` snapshots for public inspection. Unlocking the publisher before a reader locks safely publishes the initialized replacement as well as both interface words.

| Access | Execution owner and synchronization |
| --- | --- |
| `newConnection`, `newClientConnection`, `preSetup`, `bindPacketEmission` | Initialization before exposure; capability reads, initial queue construction, and binding the slot pointer. Binding happens before the publication mutex is used. |
| `Conn.ConnectionState`, `Conn.LocalAddr`, `Conn.RemoteAddr` | Application callers snapshot through `emission.activeConn`. `ConnectionState` retains its existing `connStateMutex` for its other state. |
| `Conn.switchToNewPath` | Connection goroutine reads the old remote address and fully initializes the new `sconn`; `replacePath` publishes under `connMutex`. |
| `Conn.handleHandshakeComplete`, `Conn.handleUnpackedLongHeaderPacket`, `Conn.prepareEmission` | Connection goroutine reads capabilities or addresses directly; serialized with the sole writer. |
| `packetEmission.emit`, `packetEmission.rebindPath`, `packetEmission.close` | Connection goroutine reads the slot directly for live capabilities, atomic remote-address rebinding, or the final synchronous write. |
| `sendQueue.Run`, `sendQueue.SendProbe` | Use each queue's own fixed `sendConn` reference, never the active slot. Queue construction and worker startup publish that reference. |

Inspection releases the publication mutex before calling into `sendConn`. An overlapping call may use either the old or new path; separate public calls do not form a combined snapshot. The observed `sconn` stays reachable through the local interface copy. Its local address and raw socket capabilities are immutable after construction; `remoteAddrInfo` and `gotGSOError` retain their existing atomic publication. Capabilities are read on every `ConnectionState` call, so later GSO fallback is visible. This audit does not claim coverage of unrelated connection-state fields or caller mutation of returned address objects.

The assignment remains before draining the old queue, matching existing behavior. The mutex is released before drain/join, socket calls, queue construction, or worker startup. `Conn.switchToNewPath` still starts the replacement worker and propagates its errors. Outgoing buffers, queue ownership, recovery, protocol state, and wire/API contracts are unchanged.

## Regression evidence

`TestConnectionMigrationConcurrentState` retains the real UDP validation, switch, data transfer, and switch-back scenario from `TestConnectionMigration`, with an application state reader overlapping both switches. `TestConnectionMigrationConcurrentAddresses` uses the same scenario for both public address accessors. Readers start after handshake and initial path validation and are joined before cleanup. Successful transfer after each switch precedes assertions of the active local address and unchanged peer address.

With only those integration regressions added, Linux/arm64 Go 1.27.1 reports active-slot and replacement-initialization races on the baseline in both tests. The same tests pass with synchronized publication. `TestConnectionStatePathCapabilities` additionally observes non-GSO to GSO path replacement through the public state accessor, then injects a socket GSO error through the real send queue and confirms that public state reports the fallback after the worker drains. Existing replacement/drain, rebinding, send-queue, and scalar GSO regressions remain applicable.

The PR validation receipt records the exact candidate SHA, commands, three independent fixed migration runs, full unit/integration checks, vet, and hosted results. The evidence domain is these supported Go interfaces and real validated migration, with Linux `-race` plus the existing hosted platform matrix; no performance or unrelated state-race guarantee is made.
