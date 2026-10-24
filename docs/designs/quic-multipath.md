# QUIC multipath: sizing and design exploration

**Status:** proposal for discussion, 2026-10-24. This note answers the sizing questions in [#646](https://github.com/the-sarge/quic-go-fast/issues/646); it does not accept a design, authorize implementation, change a public interface, or authorize a qualification campaign. The issue stays open for the owner's decision and, if accepted, a separately audited implementation handoff.

**Source baseline:** `f35df9d7b016a06174288901f3337ccc2fd7347c`. Production code is unchanged from `f1c5e2dfc0637c2c425a6ed30340adadb92098c6`; the intervening commits reconcile Q2 documentation and its journal. Protocol target: [draft-ietf-quic-multipath-21](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21). Current publication status, implementation pins, and external source checks are in the [research note](../audits/2026-10-24-multipath-646-research.md). Reproducible inventory and historical counts are in the [sizing data](../audits/2026-10-24-multipath-646-sizing.json).

## Recommendation and confidence

Continue toward a conditional implementation decision. The fork has a useful packet-emission owner, explicit recovery feedback, migration tests, and native packet-I/O work to build upon. The difficult change is introducing several simultaneous path contexts into mechanisms that currently have one active context. No evidence in this investigation establishes a speed benefit, correct multipath interoperability, or production readiness.

For a complete first implementation including bounded path management, ordinary STREAM and DATAGRAM use, a small built-in scheduler, BBR integration, and observability, reserve **about 7,500–15,100 production lines added or deleted, 12,000–26,000 test lines added or deleted, and roughly 16–22 implementation/review slices before native qualification**. The production forecast spans 43–63 existing files and 16–31 new files. These are engineering ranges, not measured diffs, calendar estimates, accepted slices, or a promise that the upper bounds will hold. Independent decisions about BBR, explicit payload steering, or managed socket support can change them substantially. Confidence is medium in the affected areas and low-to-medium in the forecast size; there has been no prototype or trial port.

The preferred initial product direction is an opt-in capability, two usable paths as the starting policy limit, one primary path with a validated backup by default, and a small built-in aggregation policy only after its behavior is measured. Keep the scheduling seam private initially. Preserve the existing single-path behavior when the extension is disabled or not negotiated. The path limit, policy, API spelling, and first supported controller remain proposals requiring acceptance.

There is now a third implementation option besides writing the extension ourselves or declining it: selectively adapt an existing Go implementation. [qoke/mp-quic-go at `084c13ea`](https://github.com/qoke/mp-quic-go/tree/084c13ea3834fbe58cedb63db9208951d714f839) targets draft-21 and has a picoquic test harness. It is a source reference worth evaluating, not a drop-in patch for this fork's emission and recovery ownership. Do not apply its whole diff as the implementation plan.

## What changed since the issue was filed

- The claim that no Go implementation exists is outdated. The research note verifies a draft-21 Go reference and distinguishes inspected source from the author's reported test results.
- [Packet emission #24](https://github.com/the-sarge/quic-go-fast/issues/24) and [A20 #936](https://github.com/the-sarge/quic-go-fast/issues/936) are complete. [BBRv3 #600](https://github.com/the-sarge/quic-go-fast/issues/600) has completed all transport/controller slices and Q1; [Q2 #599](https://github.com/the-sarge/quic-go-fast/issues/599) remains outstanding and not dispatched.
- Current recovery includes default-Reno registration-prefix ordering, spurious-loss undo, and adaptive application loss thresholds. The issue's September baseline predates some of those contracts. Multipath must partition their evidence correctly, not merely instantiate several BBR senders.
- The draft is in the RFC Editor queue, with final approval pending at the inspection date. Treat -21 as the pinned target and recheck the published text before implementation; a queue position is not a guarantee of immutable text.
- Application-data packet-number spaces become per path. Initial and Handshake spaces remain connection-level; 0-RTT belongs to path zero. New extension frames are carried in 1-RTT packets. PATH_ACK is recommended for path zero, but a negotiated endpoint must still process ordinary ACK for that path. A blanket replacement of every ACK is incorrect.
- A protocol path ID is distinct from this fork's current `pathID` candidate handle and `pathGeneration` migration epoch. NAT rebinding can change the tuple of an existing path ID, and several path IDs may use the same tuple. The proposed model must represent both cases.
- Backup status signals a preference to the peer. It does not prove an idle path remains reachable, mandate zero backup traffic, or guarantee sub-RTT failover. Failure detection, NAT lifetime, and scheduling determine the outcome.

## Measured surface and forecast changes

The inventory contains **73 distinct principal production files and 23,778 physical source lines** at the pinned baseline, including comments and blank lines. This is the inspected source surface, not a forecast that every line will change. Each file is counted once under its primary area; cross-cutting edits to `connection.go`, the sent-packet handler, and the packer are attributed there rather than counted repeatedly. Secondary integration sites, generated mocks, platform adapters, and tests are not an exhaustive inventory. The JSON records every counted path and blob ID.

The last column is expected production additions plus deletions, not net growth. Rows are additive for planning only. New tests are forecast separately because cross-path failures need scenarios spanning several areas.

| Area and principal files | Existing files / lines | Nature of change and integration cost | Rebase exposure | Forecast line edits |
| --- | ---: | --- | --- | ---: |
| Transport parameters and frames: `internal/wire/transport_parameters.go`, `frame_parser.go`, `frame_type.go`, ACK/CID/path frames and logging | 10 / 1,449 | Mostly additive codecs, negotiation and error gating; retain path-zero ACK compatibility, encryption-level rules and 0-RTT handling. Include PATHS_BLOCKED and PATH_CIDS_BLOCKED, omitted from the issue's initial list. | Medium: transport-parameter and parser changes routinely meet this seam. | 450–900 |
| Packet-number spaces and ACKs: `internal/ackhandler/{sent_packet_handler,received_packet_handler,*history,received_packet_tracker,packet_number_generator,interfaces,packet}.go` | 8 / 2,386 | Structural: application-space maps, per-path duplicate detection, packet keys, ACK routing and deadlines. Initial/Handshake stay unique. The sent handler is counted here although its loss logic changes too. | Very high: changes core recovery signatures and identity. | 900–1,700 |
| Loss, PTO, sampling and ECN: `internal/ackhandler/{lost_packet_tracker,congestion_dispatch,delivery_sampler,delivery_records,bbr_recovery,recovery_index,retained_index,ecn,bbr_ecn,bbr_diagnostics}.go` | 10 / 2,765 | Structural: per-path loss and sample ownership, timers, ACK-on-another-path attribution, abandonment and bounded retained evidence. | Very high: overlaps the fork's deepest recovery divergence. | 900–1,800 |
| Controllers and pacing: `internal/congestion/{bbr_*,cubic_sender,pacer,interface,feedback,packet_order}.go` as listed in JSON | 10 / 1,925 | Reuse algorithms, change their context and keys. Reno's fixed three-space prefix cannot identify independent application spaces without path scoping. BBR epochs cannot substitute for simultaneous path IDs. | High: algorithm fixes must be checked against the new adapter. | 300–750 |
| Connection-ID ownership: `conn_id_generator.go`, `conn_id_manager.go` | 2 / 543 | Structural: path-specific issuance/retirement, per-path sequence and limits, CID-to-path lookup before decryption, stateless-reset lifetime. | High: central routing and migration assumptions change. | 500–1,000 |
| Packet protection: `internal/handshake/{updatable_aead,aead,interface,crypto_setup}.go`, `packet_unpacker.go` | 5 / 1,554 | Structural: path nonce input and receive packet-number reconstruction, shared key phase with per-path progress, global AEAD usage limits and key retention. | Very high: QUIC/TLS and key-update changes require semantic review. | 600–1,100 |
| Lifecycle and MTU: `connection.go`, `path_manager.go`, `path_manager_outgoing.go`, `mtu_discoverer.go`, `path_mtu_settled.go` | 5 / 4,170 | Structural: concurrent lifecycle, aggregate timer selection, separate validation/PMTU state, close and no-usable-path behavior. Current migration resets and replaces one active context. | Very high: connection-loop changes intersect many upstream patches. | 1,400–2,600 |
| Emission and sockets: `packet_emission*.go`, `packet_packer.go`, `send_conn.go`, `send_queue.go`, `bbr_send_policy.go`, `local_send_credit.go`, `delivery_sampling.go`, `candidate_authority.go`, `transport.go` | 11 / 4,649 | Structural: select path before destructive packing, reserve and register against that path, preserve queue credit and authority on all workers, batch only compatible output. Existing emission ownership remains. | Very high: fork-specific queue, offload and BBR changes intersect. | 1,200–2,300 |
| Fixed-peer policy: `packet_policy.go` | 1 / 284 | Small if first release rejects multipath on fixed-peer transports. A mutable admitted-peer set would be a separate policy redesign outside this estimate. | Low for explicit rejection; high if the immutable contract changes. | 60–200 |
| qlog: `qlog/{frame,event,packet_header,types}.go` | 4 / 1,756 | Mostly additive: path-qualified packet/ACK identities, frame data and lifecycle events. Align with the then-current qlog schema or document a private extension. | Medium: schema and logging APIs evolve independently. | 200–500 |
| Public configuration/capability: `interface.go`, `config.go`, `congestion_control.go` | 3 / 441 | Add opt-in configuration and negotiated capability, retain copy/clone behavior and absent-safe consumer integration. | Medium: avoid replacing existing contracts. | 400–900 |
| Payload scheduling: `framer.go`, `retransmission_queue.go`, `datagram_queue.go`, `send_stream.go` | 4 / 1,856 | Separate frame delivery ownership from transmission-path ownership; handle datagram size eligibility and prevent accidental duplication. Shared stream flow control remains connection-wide. | High: ordering, retransmission and buffer ownership interact. | 600–1,300 |
| **Total** | **73 / 23,778** | Excludes tests, docs, native qualification and a general-purpose public scheduler. | | **7,510–15,050** |

The forecast includes the BBR adaptation needed for a complete supported feature. A Reno-first milestone can defer that work, but does not remove the shared recovery/credit/key-update changes. The estimate excludes FEC, duplicated application transmissions, a coupled congestion-control algorithm, arbitrary cross-transport scheduling, a fixed-peer-set API, address discovery, and a repository/module rename. It assumes no redesign of native GSO/GRO APIs: route and batch correctly at existing seams, then qualify the result.

### Comparison with completed programs

These measured counts sum `git diff --numstat COMMIT^ COMMIT` for the exact product commits listed in the JSON. They count cumulative churn, so a line edited in several slices contributes several times. File counts are unique per row. They exclude documentation, experiment/interop trees and generated `mock_` files; source and colocated tests are separate. They do not measure agent time, review effort, failed experiments, or native qualification cost.

| Historical comparison | Production + / −; unique files | Tests + / −; unique files | Coverage |
| --- | ---: | ---: | --- |
| Packet emission | 808 / 575; 5 | 1,931 / 822; 13 | E2 through E6 and the #60 repair; E1 feasibility and documentation excluded. Nine audited slices in the whole program. |
| Original BBR implementation | 3,647 / 173; 29 | 5,090 / 32; 21 | T1–T5 and B1–B6 plus #622; Q1 campaign preparation excluded. |
| Later BBR production correction port | 1,601 / 242; 25 | 5,747 / 43; 27 | Five #748 port commits only; not every later BBR/recovery change. |

Multipath's forecast is approximately 5–11 times the emission production churn, 2–4 times the original BBR production churn, or 1.3–2.7 times the original BBR plus those five correction-port commits. Those ratios describe source churn, not elapsed effort. It crosses more protocol identities and interoperability cases than either comparison. A useful planning category is **a major transport program, with BBR-scale validation risk**, rather than another small extension codec.

The Go reference supplies another check. Its name-selected multipath files add 7,440 production lines and 18,329 test lines relative to its upstream v0.63.0 parent. That selection includes optional OLIA, duplication and scheduling policies and excludes edits hidden in existing core files. Its whole-fork delta is larger and includes other features. Neither number is a required-core lower bound or a directly portable patch. This evidence widened the initial local forecast; exact classification and provenance are recorded in the research note.

## Proposed ownership model

Retain the connection goroutine as protocol-state owner and the private emission module as the owner of construction, registration and output handoff. Add a private path registry which owns path membership and delegates path-specific recovery and sending state. Keep shared application payload ownership in streams, the DATAGRAM queue and the retransmission owner. A second path does not create a second stream set, TLS session, or flow-control budget.

| Owner | State that belongs there |
| --- | --- |
| Connection | Handshake and Initial/Handshake spaces; shared key schedule and key phase; total AEAD limits; streams and flow control; connection lifetime; aggregate resource limits; application policy; globally unique registration identity where still useful. |
| Protocol path | Monotonic wire path ID; current tuple and a separate tuple/migration generation; local/remote CID sets; validation and anti-amplification; application PN/ACK history; loss/PTO/RTT; ECN and PMTU; controller and pacer; availability/backup preferences; retirement state. |
| Emission | Path selection before packing; immutable path-qualified packet registration; buffer ownership; per-path local pending-byte accounting plus an aggregate cap; compatible GSO batches; submission authority; completion routing back to the correct owner. |
| Application/consumer | Address discovery and admission policy, interface/socket choices, preferred use of expensive paths, identity policy, and definitions of useful payload delivery. |

Use `(path ID, application packet number)` for application transmission identity, with explicit Initial and Handshake identities. A tuple generation fences feedback about a rebinding or migration within one path; it is not a replacement for wire path ID. A late ACK for an abandoned path can retire retained evidence for that path without changing the live path's controller. Retransmitted STREAM bytes remain one logical delivery obligation, even when represented by transmissions on different paths.

The receive seam must recover path ID from the destination CID before packet-number reconstruction and AEAD opening. Today `updatableAEAD` has one `highestRcvdPN`, `largestAcked`, and first-sent/received key-phase progress. Making only the nonce path-aware would leave incorrect reconstruction and key-update behavior. Keep key phase shared and implement the draft's per-path transition/confirmation rules explicitly; do not duplicate the whole crypto setup per path.

On send, the scheduler sees only paths currently eligible under validation, local/peer preference, congestion, pacing, MTU, local credit and authority. It selects a path before the packer consumes stream or DATAGRAM work. The packer receives a path-scoped context for destination CID, PN, ACK/control selection, size, and sealing. Emission then registers against that same path and enqueues immutable path metadata. Socket completion must never be attributed by consulting whichever path is preferred later.

Keep send workers behind the existing `sender` seam. Per-path queues are the clearest first design, with a connection-wide byte ceiling and wakeup aggregation to avoid one full queue stalling every other path. This increases workers and memory with path count; qualify that cost. Sharing a worker across paths is an alternative optimization, not a prerequisite. Every queue must preserve candidate-group authority and release each buffer/reservation exactly once. GSO batches cannot mix destination, socket, ECN treatment or incompatible segment size. GRO splitting must preserve CID-based path attribution even for equal tuples.

### BBR and Reno implications

- `congestion.PacketKey` currently identifies only encryption space and PN; `packetPrefix` has three slots. Scope a controller's keys/prefixes to its path and explicitly handle the initial path's handshake spaces. Do not enlarge a global prefix array without defining how loss on one path may affect another controller.
- Delivery sampling, recovery episodes and retained indexes use a current path generation. Several live paths need independent live and retained evidence. Preserve connection registration ordinals only as an ordering aid; sample delivery, persistent congestion and undo membership must use the correct path's evidence.
- `localSendCredit` has one current generation, and ordinary reservations require old generations to drain. Treating another live path as an old generation could serialize multipath or misclassify pending transmissions. Separate per-path credit from the aggregate resource ceiling.
- `deliveryPendingBytes` and application-limited inference currently describe one sender. An idle path deliberately omitted by the scheduler is not evidence that the application has no data. Make scheduler-limited sending an explicit internal fact; do not train BBR using another path's queue or application-gap observation.
- ECN counters and validation are path-specific. Shared key-update counters are not. ACKs received on another path require deliberate RTT attribution; choose same-path ACKs by default where usable, while accepting valid cross-path ACKs and supporting abandonment/failure.
- [ADR 0013](../adr/0013-adaptive-loss-thresholds.md)'s adaptation and [ADR 0012](../adr/0012-reno-spurious-loss-undo.md)'s undo must stay path-scoped. Evidence for one path cannot relax loss thresholds or restore congestion state on another.

## Public interface alternatives

| Alternative | Benefit | Cost / limitation | Assessment |
| --- | --- | --- | --- |
| A: opt-in plus internal automatic scheduler only | Smallest external interface; easiest to evolve. | Insufficient control for backup cost policy, exact socket ownership and explicit path admission. | Too restrictive for the candidate consumers. |
| B: opt-in, explicit path handles, fixed policy choices | Hides wire/CID/recovery machinery while callers control lifecycle and preference. Existing exported `Transport`/`Path` types can support optional structural adapters. | Must define directional status, failure and migration compatibility carefully. | Preferred starting point. |
| C: public pluggable packet scheduler/controller | Maximum flexibility, including specialized distribution. | Exposes packet timing and eligibility contracts, callback reentrancy, stale snapshots and packetization coupling; risks freezing internals. | Defer until two concrete consumers establish the needed seam. |

Illustrative signatures for B follow. They describe a candidate interface, not an accepted API or compilable implementation. Primitive arguments and existing exported types follow the fork's [congestion-selection precedent](../../congestion_control.go); exact names, errors and state accessors must be settled during design acceptance.

```go
func (c *Config) ConfigureMultipathV1(enabled bool, maxPaths uint32, policy string) error
func (c *Conn) MultipathNegotiatedV1() bool
func (c *Conn) OpenPathV1(ctx context.Context, t *Transport, remote *net.UDPAddr) (*Path, error)
func (c *Conn) PathsV1() []*Path
func (c *Conn) PathChangesV1() <-chan struct{}
func (p *Path) PathIDV1() uint32
func (p *Path) SetLocalUseV1(use string) error
func (p *Path) AdvertiseStatusV1(status string) error
func (p *Path) AbandonV1(code uint64) error
```

Candidate contracts:

1. Configure before Dial/Listen/GetConfigForClient returns. Disabled is the default. `maxPaths` is a local simultaneous-path/resource cap including path zero; it is not a literal `initial_max_path_id` value or lifetime path-attempt budget. Validate local cap and policy without mutation on error. Copy/Clone must preserve the selection, with no process-global side table.
2. A connection exposes negotiation separately from local configuration. Without peer support it carries ordinary QUIC traffic. New path operations return an identifiable not-negotiated result rather than opening another connection or silently using migration. Before negotiation completes, report handshake-incomplete rather than an ambiguous permanent absence.
3. `OpenPathV1` is client-only, uses the caller's exact transport/socket, and accepts an explicit remote endpoint for a server address already known and admitted by the application. It returns after required validation, with context cancellation and no implicit socket ownership transfer. Cancelling an in-progress open retires its local attempt; it does not reuse a consumed wire path ID. The transport checks its own policy too.
4. `PathsV1` returns a snapshot of path handles, including path zero and peer-created server paths. Handles remain safe after abandonment. Add bounded state/address/MTU/error snapshot access before shipping; those return types remain a design decision. `PathChangesV1` is a coalescing invalidation notification, not a lossless event log; callers resnapshot. No application callback executes on the connection goroutine. Terminal context and qlog supply complementary diagnostics.
5. Local send use and advertised peer preference are separate controls. `SetLocalUseV1("backup")` governs the local scheduler. `AdvertiseStatusV1("backup")` requests how the peer should use the path; it cannot promise peer compliance or zero incoming traffic. Validation, ACK and liveness traffic may still use a backup.
6. `AbandonV1` is idempotent locally, prevents new ordinary scheduling on the path, queues required signaling, and retires/requeues eligible outstanding data through recovery. It does not claim already handed-off UDP datagrams are recalled. Define behavior for the last usable path explicitly: proposed behavior is a distinct local error unless the caller closes the connection. Remote abandonment or network failure may still leave no usable path and invoke normal connection timeout/closure behavior.
7. Existing `AddPath`/`Probe`/`Switch`/`Close` retain their documented contracts in ordinary QUIC. On a negotiated multipath connection, legacy migration methods should return an explicit mode error rather than silently redefining `Switch` or the ban on closing the active path. For locally enabled but not-yet-negotiated connections, legacy path operations wait for or report handshake completion; for an unsupported peer, legacy migration remains available after the result is known. A path handle opened through the new interface uses the new lifecycle methods.
8. Transport reuse, a path's closure, and closing the underlying socket remain distinct. Close only resources owned under existing `Transport` rules. A socket error must identify affected paths rather than unconditionally treating every path as failed; if shared transport closure is connection-fatal under an existing contract, document and resolve that conflict before implementation.

The additive interface still needs a compatibility review against [ADR 0001](../adr/0001-upstream-compatibility.md). The new mode errors affect explicitly opted-in behavior; they cannot be introduced into the default profile. Optional consumers should isolate fork calls using structural capability detection or a dedicated integration adapter, and continue to build with upstream. No module-path change is proposed.

### Scheduling, retransmission and DATAGRAM policy

Start with a private scheduler called by emission before destructive packing. Its input is a bounded immutable set of eligible path views plus work class/size requirements; its output is a path choice or the next wake deadline. It cannot mutate congestion state, admit an unauthorized address, invent credit, or retain packet/payload pointers. Keep this an internal seam so a backup policy and an aggregation policy can vary without exposing recovery internals.

Proposed initial default: prefer the primary available path; use a validated alternate when the primary is unusable under a defined failure policy. Do not equate temporary congestion-window exhaustion with path failure or promise a particular failover latency. Before shipping, specify the failure/recovery thresholds, backup promotion, all-paths-backup behavior and liveness budget. The design must distinguish a strict cost-sensitive standby policy from an opportunistic overflow policy.

For aggregation, compare earliest estimated delivery with simple round-robin among eligible paths. Round-robin is straightforward but can stall ordered STREAM delivery behind a slow path and is not a bandwidth weighting policy. Recommend earliest-delivery scheduling as an experiment candidate, not an established winner; acceptance requires mixed-delay and shared-bottleneck results. No default controller change is implied.

Retransmit lost reliable information on any eligible path by default, with same-path preference while it remains healthy. Preserve mandatory same-path controls such as PATH_CHALLENGE/PATH_RESPONSE. The retransmission owner tracks logical STREAM ranges/control obligations; each newly transmitted packet uses the destination path's PN, nonce, flight and congestion allowance. Do not retransmit an encrypted packet unchanged. An ACK for an original after retransmission must not double-retire logical work or another path's congestion flight. Application DATAGRAM messages remain unreliable and are not automatically retransmitted or duplicated.

GridCast's distinct rateless symbols fit nonduplicating DATAGRAM distribution, but ordinary packet scheduling cannot promise exact application-symbol placement or equal unique payload per path. If the consumer needs explicit per-message steering, size a later `SendDatagramOnPath`-style seam with queue/MTU/admission semantics. Do not smuggle application payload classification into a packet callback. That optional public steering feature is outside the core estimate; a small internal datagram eligibility change is included.

Each path has its own PMTU. A shared DATAGRAM queue must either admit only sizes usable by every path selected for that message or retain a set of eligible paths and define what happens when that set disappears. Prefer the conservative common eligible-path size for the first generic policy, with an explicit error/limit on admission. Existing send admission never guaranteed delivery, but a new path must not silently promise a lifetime-fixed maximum. Preserve the existing first-path-only meaning of `PathMTUSettledV1`; introduce separate per-path observations rather than rewriting that contract. Reconcile the consumer's maximum-payload contract in wiremux #1738 before offering explicit steering.

### Fixed-peer and consumer policy

Recommend that `ConfigureFixedPeerV1` and negotiated multipath be mutually exclusive in the first supported configuration, with a local configuration/initialization error before advertising the extension. It promises one immutable remote endpoint and current local socket; extending it into a mutable peer set would contradict [ADR 0006](../adr/0006-explicit-external-packet-io.md) and the existing method contract. Do not silently broaden its permitted addresses, even if the peer is authenticated.

wiremux currently retains its selected-peer wrapper. It must separately decide whether that wrapper can authorize a set of candidate pairs and preserve exact socket ownership. QUIC path validation proves reachability under transport keys; it does not supply wiremux's address-admission or identity policy. This note does not amend wiremux ADRs or select one of #1738's architecture shapes. Address Discovery #644 and ACK Frequency #645 are independent extensions, not protocol prerequisites for multipath; their path attribution/ACK policies should be reviewed at shared seams if implemented first.

## Staging and program outline

A single-sender stage can reduce debugging uncertainty, but there is no safe shortcut that negotiates multipath while ignoring its receive or key-update requirements. Advertising `initial_max_path_id=0` still negotiates the extension; it is not equivalent to omitting the parameter. Such a constrained stage can exercise path-zero framing/protection, but cannot validate concurrent paths. Once positive path credit is advertised, the endpoint must correctly handle the peer's allowed behavior even if its own scheduler sends on only one path.

Use the following as a candidate decomposition for a future handoff. These are **not audited dispatch tickets**. Several rows intentionally need more than one PR. Their exact representation contracts, dependencies and one-PR boundaries must be audited after the owner accepts the design.

| Stage | Outcome and evidence gate | Dependencies / rough slices |
| --- | --- | --- |
| D0: acceptance and source selection | Resolve controller scope, API/status/legacy-mode contracts, resource limits, upstream policy assumption and selective reuse. Pin spec and interop peer; map every proposed port to local ownership. | No production change; prerequisite to implementation packaging. |
| M1: wire and identities | Codec/transport-parameter tests, path-qualified internal identity, default-disabled equivalence. Negotiation stays off until a complete supported mode exists. | D0; 2–3 slices. |
| M2: protection and receive | CID-to-path attribution, PN reconstruction, nonce vectors, 0-RTT/path-zero behavior and connection-wide key updates. | M1; 2–3 slices. |
| M3: bounded lifecycle and recovery | Path limits, validation, IDs/CIDs, abandonment, independent ACK/loss/PTO and retained evidence. | M1/M2; 3–4 slices. |
| M4: emission with conservative sending | Path selection before packing, queue/credit/authority ownership, MTU and per-path batches; two real paths with default Reno. Correct inbound concurrent traffic even with a conservative sender. | M3; 3–4 slices. |
| M5: public lifecycle and interop | Accepted opt-in surface, fallback/legacy behavior, backup lifecycle, qlog, pinned peer tests in both roles and malformed-peer tests. | M2–M4; 2–3 slices. |
| M6: BBR and aggregation | Path-scoped sampler, ECN, local credit, supply limitation and retained history; measured aggregation policy. Explicitly reject BBR+multipath until its supported slice is complete rather than silently substituting Reno. | M4/M5 and BBR sequencing decision; 3–4 slices. |
| M7: closure and qualification preparation | Regression/default-off evidence, native case inventory, resource bounds, documentation and final ownership audit. | Prior stages; 1 slice plus a separately budgeted qualification campaign. |

The rows total 16–22 candidate implementation slices. They do not include the research/design decision itself, prototype-driven redesign, a public scheduler, or running the native qualification campaign. A first Reno milestone ends earlier, but is not a claim that the complete estimate has been delivered. No intermediate release should advertise capabilities whose receive/state machinery is unfinished.

### Sequencing with Q2

Design and source/reference inspection can proceed now. Prefer to resolve and pin Q2's candidate, reference and campaign obligations before merging multipath changes to shared recovery/emission. Completing Q2 first gives the clearest single-path baseline, but is not a protocol dependency of multipath or a reason to delay this note. If the owner chooses concurrent work, preserve Q2's immutable candidate/fixtures and audit which later changes require fresh readiness; do not transfer fast.8 or Linux exceptions to the new revision.

The [current Q2 plan](../adr/2026-09-24-bbrv3-qualification-plan.md#current-dispatch-status-and-evidence-gates) still requires execution preflight and separate dispatch. This design consumes no campaign budget and books no resources. Restoring automatic PR fuzzing via [#473](https://github.com/the-sarge/quic-go-fast/issues/473) is recommended before new parsers and state machines land; the existing workflow's PR trigger is disabled. It is an implementation-readiness recommendation, not a blocker for this design work.

## Interoperability and validation

Use picoquic as the primary non-Go interoperability candidate. The inspected Go reference pins `01e124ebdf9fc50df307e01e71f2744471ddc470` and supplies a Linux container harness in both client/server roles. Its source and reported results make it a practical starting point; **no harness was built or interoperability test run in this investigation**. Pin picoquic and its dependency tree in a future test image rather than relying on a moving tag. Re-evaluate the pin against the final RFC text at implementation start.

The reference's `go test -tags picoquic -count=1 -v ./interop/multipath/... -args -logdir=...` is a command in that repository, not a command that works in this fork today. A future local adapter would drive our opt-in client and server against the pinned endpoint, use Linux host networking/loopback aliases for two UDP paths, retain both endpoints' qlogs and application hashes, and fail if only path zero carried application data. It must exercise both roles, fallback without negotiation, unequal path delay/loss, backup signaling, abandon/reopen and key update. The fork's existing CI builds an interop image; it does not run this multipath matrix or establish membership in the public interop runner.

Known oracle gap: the inspected picoquic implementation omits a zero `initial_max_path_id` and requires positive peer values for its multipath mode. It cannot alone validate the legal negotiated-zero/path-zero stage. Cover that with normative vectors and a controlled peer fixture, and seek a second independent implementation for disputed behavior. Self-to-self tests or two forks sharing the same code are not independent interoperability evidence. If no compatible external peer is practical, retain the feature as an internal experiment and make public support contingent on resolving that gap; do not replace the claim with a passing self-suite.

| Evidence venue | What it should prove | What it cannot establish |
| --- | --- | --- |
| Unit / deterministic state-machine tests | All frame/error gates; negotiation absent/zero/positive; path-zero ACK; per-path PN including equal numbers; crypto vectors; late ACK/abandon; aggregate resource caps; timer arbitration; duplicate retire/status; key-update progress. | Real socket identity, kernel offloads or network fairness. |
| Real UDP loopback, two sockets/addresses | CID demux; both roles; data on two paths; one path lost while another continues; cancellation/close races; source/remote changes; transport ownership and independent peer compatibility. | Independent bottlenecks, realistic routing, Wi-Fi/cellular failure timing or native platform qualification. |
| Linux namespaces/netem or two controlled hosts | Different RTT/loss/rate/MTU, shared versus disjoint bottlenecks, cross-path ACKs, NAT/rebinding, drop/restore and backup ageing. Count unique useful delivery and all path overhead. | All native macOS/Windows I/O behaviors or general Internet performance. |
| Native Linux/macOS/Windows packet-I/O venues | Socket capabilities, GSO/GRO or platform batching, ECN/DF, managed leases, worker shutdown, authority revocation and memory under the selected configuration. | Benefit for an unmeasured consumer/workload or a different code revision. |

Prioritize failures that self-to-self happy paths miss: same PN on two paths; ACK on another path; path abandoned while packets remain queued/in flight; original ACK after cross-path retransmission; key update with one silent path; all remaining paths marked backup; shared socket failure; repeated consumed IDs; peer-created sparse IDs; path-zero migration; valid CID on a disallowed tuple; DATAGRAM too large for the survivor; 0-RTT accepted/rejected; unsupported peer; and fixed-peer/candidate-authority enforcement.

### Resource and anti-amplification proposal

The baseline incoming migration manager already caps candidate slots at three, tracks challenge count and validation deadlines, and reserves actual probe bytes before I/O. Preserve those properties but do not confuse that candidate cap with a negotiated multipath limit. Existing outgoing path handles and exponentially bounded probe retries do not constitute a connection-wide multipath resource policy.

Propose an initial local cap of two live protocol paths including path zero, one additional validation attempt at a time, bounded per-path CID issuance, and a separate connection-wide cap for retained/retiring paths and pending opens. Exact numeric limits beyond the initial live-path proposal need memory measurements and an accepted contract. Do not allocate arrays proportional to a peer's maximum or sparse path ID. IDs are monotonic and unreusable; increasing MAX_PATH_ID restores credit after consumption without allowing unbounded simultaneous state.

Track pre-validation sent/received bytes on the actual path and apply the draft/RFC anti-amplification rules independently there; traffic on an established path does not buy amplification credit on an unvalidated tuple. Defer expensive controller/history allocation until validation succeeds where permitted, while retaining enough bounded state to process validation and required control frames. Bound retries, idle validation, pending control frames, tombstones, retained ACK evidence and aggregate queued bytes. Rejected input must not manufacture a new full path context solely from an unauthenticated tuple or large ID.

BBR currently permits substantial bounded stores: 25,000 live delivery entries, 4,096 retained delivery entries and 32,768 recovery outcomes in the inspected source. Multiplying every maximum by path count can dominate the small path-registry cost. Establish aggregate as well as per-path limits, and measure idle, validated-backup, saturated and abandoned-path memory. Resource exhaustion must produce a controlled limit/decline/degradation outcome, never unbounded growth or silently corrupted delivery evidence.

## Maintenance and upstream relationship

The implementation forecast assumes the fork owns its extension and hand-ports necessary upstream fixes; it assigns no savings to upstream accepting multipath. That is a planning scenario, **not a change to accepted policy**. [ADR 0003](../adr/0003-follow-stable-upstream-releases.md) still bases releases on validated stable upstream releases, while [#651](https://github.com/the-sarge/quic-go-fast/issues/651) leaves the post-split relationship undecided. Accepting a permanent independent release policy requires reopening that ADR explicitly. Repository naming/detachment need not block design.

| Maintenance scenario | Expected work and planning allowance |
| --- | --- |
| Continue stable upstream integration | Reconcile a standing semantic diff across roughly 43–63 existing production files, especially connection, ACK/loss, CID and crypto interfaces. For a narrow upstream patch, allow 1–3 focused review/validation sessions; for changes to recovery, migration or packet protection, allow 3–8 sessions and possible redesign. These are untested planning allowances, not observed rebase timings. A major representation change is not bounded by them. Re-run default-off and multipath interop on the merged revision. |
| Stop rebasing releases | Avoid repeated whole-tree merge resolution but own advisory monitoring, applicability analysis and security backports; Go `crypto/tls` QUIC-hook adaptation and compiler/platform changes; dependency updates; protocol interoperability; and native regression coverage. Name an owner and cadence through #651. Skipping upstream merges does not eliminate this work or retain automatic coverage from upstream's infrastructure. |
| Selectively adapt the Go reference | Preserve MIT/upstream attribution and provenance; port cohesive protocol logic/tests only after mapping local ownership and licensing. Its upstream v0.63.0 parent differs from this fork's base, and its Conn-driven send/scheduler choices conflict with our emission design. Reconcile each patch and run independent interop; source availability reduces discovery effort, not integration obligations. |

The estimates exclude unbounded maintenance research. Before committing to implementation, a bounded paper mapping of the reference's path/crypto/recovery changes should decide whether adapting those modules is cheaper than implementing from the specification with its tests as examples. No code import is authorized by this note.

## Consumer evidence and decision gates

[wiremux #1738](https://github.com/GridSwarm/wiremux/issues/1738) owns its layering/API decision and questions 20–21 about the first consumer and benefit. [wiremux #708](https://github.com/GridSwarm/wiremux/issues/708) owns the broader multipath motivation. GridCast's rateless DATAGRAM distribution is a plausible first aggregation consumer, while a general QUIC application can exercise backup continuity without wiremux. Neither is designated as the committed first adopter here.

The transport can supply a controlled test fixture first. Ask the consumer work to select its unique-useful-delivery boundary and acceptance thresholds, then compare the same sockets/paths and total offered load across: existing single-active-path migration, multiple independent connections/Wires, and one multipath connection. Include distinct-symbol delivery, time to resume useful delivery after a path failure, duplication/repair overhead, CPU per useful byte, memory, control latency and shared-bottleneck coexistence. A throughput sum alone cannot demonstrate benefit. Define noninferiority for the disabled/single-path profile separately. This is an evidence outline, not a campaign registration or a duplicate consumer benchmark contract.

Proceed to an implementation handoff only after the owner accepts the product scope and interface direction, a protocol/interop pin and its gaps are understood, the Q2 sequence is explicit, resource accounting has a bounded design, and an independent review finds the proposed ownership model credible. Resolve whether the first milestone is Reno-only or includes BBR; reject unsupported combinations explicitly. Finish the source-adaptation comparison before freezing estimates or slice boundaries.

Reasons to decide against implementation include: no consumer benefit beyond separate connections at comparable resource cost; inability to obtain independent interoperability evidence; unacceptable regression or memory cost in the default profile; unwillingness to own crypto/recovery divergence; or a required public scheduling/authorization interface that cannot preserve accepted contracts. Record the specific reason and a revisit trigger, such as a measured unmet consumer need, upstream support, a compatible independent peer, or an accepted maintenance owner. An unfavorable comparison is a valid outcome of #646.

## Coverage of the issue's questions

| #646 question | Answer location / remaining decision |
| --- | --- |
| 1: areas, sizes, emission/BBR interactions | Measured surface, ownership and controller sections; exact inventory in JSON. |
| 2: receive/single-sender staging | Staging section; legal constrained phases do not waive negotiated receive obligations. |
| 3: comparison with completed programs | Historical churn table and its explicit exclusions. |
| 4: minimum public interface | Alternative B and illustrative signatures/contracts; final names and snapshot shape need acceptance. |
| 5: scheduler seam/default | Private emission seam; proposed backup default, aggregation measured later, public callback deferred. |
| 6: retransmission path | Any eligible path with healthy same-path preference; mandated path controls preserved; no DATAGRAM retransmission. |
| 7: migration compatibility | Ordinary mode unchanged; explicit mode errors proposed for legacy path operations after multipath negotiation. |
| 8: fixed-peer policy | First implementation rejects the combination; peer-set policy needs separate contract. |
| 9: interop partner/CI/fallback | Pinned picoquic candidate, reference harness, zero-limit oracle gap, no interop run claimed. |
| 10: test strategy | Venue matrix and adversarial scenarios; no new production/test code in this change. |
| 11: bounds | Per-path amplification, deferred heavy allocation, live/validating/retained and aggregate limits. |
| 12: upstream/rebase cost | Both scenarios and explicit ADR 0003 conflict; hand-port scenario used for forecast. |
| 13: Q2 and single-path BBR assumptions | BBR/Reno implications and Q2 sequencing; no campaign dispatch. |
| 14: first consumer/benefit | Candidate consumers and comparison outline; consumer selection and thresholds remain with #1738/#708. |
| 15: decision against | Explicit decline conditions and revisit triggers above. |

## Validation of this design-only change

Validate the inventory's blob IDs, line counts and totals against the pinned source, replay the historical commit counts, check relative links and Markdown whitespace, and independently review protocol facts and local ownership claims. The draft program's dependencies must remain acyclic and its slice ranges must total 16–22. Hosted repository checks remain applicable to a documentation PR. No local Go build, benchmark, native network experiment, or implementation compatibility claim is needed to validate these documentation changes.
