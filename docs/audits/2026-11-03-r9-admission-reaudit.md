# R9 #1029 path admission re-audit

## Trigger and preserved state

Execution preflight for [R9 #1029](https://github.com/the-sarge/quic-go-fast/issues/1029), under parent [#1013](https://github.com/the-sarge/quic-go-fast/issues/1013), at `32a4d0320df807484792c042a6f4926f29a22226` found material owners and seams outside the slice's named files. No implementation code exists. Branch `codex/r9-second-path-1029` is preserved at that base. The [current normative R9 contract](../adr/2026-10-24-multipath-reno-plan.md#r9) owns implementation and finite evidence. This receipt records the trigger and routing decision; it does not introduce a second contract.

| Accepted assumption | Current source fact | Why it cannot be avoided |
| --- | --- | --- |
| E5 supplies the bounded submission route that consumed paths use | `enableMultipathCredit` refuses a local route (`multipath_credit.go:53`) and `useNativeOrdinary` refuses path credit (`native_ordinary.go:75`), so no connection can hold both | R8 restricted its receipts to the unused ID on the promise that R9 extends them through E5's facts before granting consumed-path authority |
| An authenticated packet can request membership | Crypto admission is private to `internal/handshake` (`internal/handshake/path_aead.go:36`). A routed packet for an unadmitted ID is dropped as `ErrKeysDropped` before any frame is read (`internal/handshake/path_aead.go:72`) | Keys must exist before authentication, and packets may not allocate them |
| A challenge can be sent on the new path | `preparePathProbePacket` always uses the path-zero context (`packet_packer.go:916`), and `commitShortHeaderPacket` pops the path-zero packet number (`packet_packer.go:1110`) | A path-qualified sealer with a path-zero packet number reuses no nonce but registers against the wrong space, and the peer cannot reconstruct its packet number |
| Authenticated ingress reaches the registry | Duplicate detection and receive history use path zero (`connection.go:1405`, `:1959`). Only the server's tuple manager sees tuples, and only after an early client return (`connection.go:1447`). `PATH_RESPONSE` carries no arrival path, and the client answers every `PATH_CHALLENGE` on its default path (`connection.go:2358`) | Path 1's first packet would be discarded as a duplicate, and responses would cross paths |
| A client transport can be attached | `AddConnRunner` refuses any connection with path CIDs (`conn_id_generator.go:212`) | The target transport would never route path CIDs |
| Cutoff precedes the abandonment signal | Probes carry a revocable `responseEpoch`, but their results match no record and vanish (`native_ordinary.go:421`), so no owner can learn that a claimed write returned | The plan requires a slot to stay charged while cancellation awaits that fence, and reciprocal signaling to follow it |
| Terminal receipts survive the route change | Only `sendQueue` publishes the accepted-submission receipt. `consumeParticipant` drops it (`native_ordinary.go:421`) | The unanswered-abandonment timer could never start on the route R9 uses |

The binding rule makes a newly discovered shared-state effect a slice stop until a scoped re-audit establishes that it fits the existing outcome and one-PR budget. These effects are that case. None changes an owner: each extends a seam R4 to R8 or E3 to E5 introduced for this purpose, several with comments naming R9.

## Accepted design

Keep child #1029, its private typed domain and one independently green product PR. No exported method, new platform, controller formula, wire behavior, worker or persisted state enters the slice.

**Route.** Consumed-path authority exists only on the private endpoint route. Private activation installs the native route, then credit. Without that route, the R8 scenario is unchanged and an authenticated packet on an issued ID is declined through the terminal owner.

**Allocation order.** Keys are admitted when the local-route window is issued, which is bounded and never packet-driven. Recovery and receive history are allocated only after authentication or local commitment. An unauthenticated tuple or sparse ID allocates nothing.

**Candidates.** One candidate per protocol path reuses the existing `path` accounting for amplification and challenge records. A local operation's deadline is the earlier of five seconds and its context. Traffic and compact responses never extend it. A compact response lifts the amplification limit, but activation needs a padded response. An already validated tuple activates without a challenge, and a local open then announces the path with one PING.

**Fence.** The endpoint gains one read-only query: whether a request under a given epoch remains unresolved. The connection reads it after the existing wake, so no native wait enters the loop. Local and reciprocal `PATH_ABANDON` are queued only after it clears.

**Receipts.** Consuming a whole accepted single datagram publishes its terminal receipts. The existing separation from segmented batches continues to prevent a discriminating-prefix claim.

**Declared seams retained.** The client declines an unused ID it did not open, because this profile opens only from the client. A validated extra tuple on an established path records validation with no cutover until R13. `PATH_STATUS` is accepted after its ID is checked against credit, closing the C-009 unexpected-frame failure, and its policy stays with R15. Received abandonment of path zero keeps its connection close until R10 and R12 make a survivor useful. Per-path RTT samples begin with R10, so retention snapshots use the connection observation until then. Each admitted path reserves its retained-history unit at commitment, so its own retirement can always fence; replacement credit stays with R12.

## Rejected alternatives

Splitting candidate plumbing into a behavior-preserving predecessor would add a child without removing any named seam, because each one is reachable only from admission. The slice's stated split trigger is context fit, and comparable slices shipped as one PR ([R8 #1028](https://github.com/the-sarge/quic-go-fast/issues/1028), 1,368 changed lines). Allocating keys on first packet contradicts the no-lazy-allocation rule. Making the probe synchronous would remove the fence query but block the connection loop. Counting probe results in the connection requires per-request identity and a second accounting owner, where the endpoint already holds the records. Each is rejected on merit.

## Gates

Representation, the universal runtime guarantee, artifact classes, closure rows, the 16-scenario ceiling, the focused race run and the review budget are unchanged. No operator decision, adjacent slice or different PR boundary is required. Traced additions are `internal/handshake` key admission, `internal/ackhandler` reachability, `packet_packer.go`, `conn_id_generator.go`, `packet_emission_local.go`, `native_ordinary.go` and `native_endpoint.go`.
