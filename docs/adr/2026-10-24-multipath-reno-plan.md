# Reno-first QUIC multipath implementation plan

**Status:** accepted design and audited slices; not implemented. Dispatch requires the verified default-branch publication and a frontier child issue. **Program:** `QGF-MP646-20261024`. **Track:** R, 2 of 3. **Date:** 2026-10-24. **Normative scope:** current outcomes, boundaries, invariants, evidence, blockers and stops. **Audit history:** [final consideration response](../audits/2026-10-24-multipath-646-final-consideration-response.md) and [slice audit](../audits/2026-10-24-multipath-646-slice-audit.md). **Parent issue:** pending.

## Binding contract and operating discipline

This is a current contract, not dispatch permission. Implementation occurs only when the operator dispatches a verified frontier child through `$implement-architecture-slice`. Follow that skill's shared review-loop and contract-closure baselines, composed with [the repository execution overlay](../REVIEW-LOOP.md). No repository closure overlay exists. Use the shared design-cost reference for scoped design choices. One fully briefed fresh review and at most one replacement govern each implementation PR; independently disposition findings and stop on accepted representation/ownership/boundary failures. Do not run an automatic fixer. Exact pushed-head local certification, applicable same-head hosted checks, squash merge, then append-dev-journal and tracking reconciliation are required. Diagnose failures before reruns.

The native endpoint ownership domain includes every listed ordinary QUIC v1/v2, negotiated multipath, control/stateless and public non-QUIC write on an exact granted native Linux amd64 UDP socket. The narrower negotiated multipath wire domain is opt-in draft-21 QUIC v1 with two live/validating paths. Existing parser/AEAD/configuration owners validate external representations; internal event constructors carry immutable identities. The runtime ownership and authority rules are universal within that domain. The evidence below is finite regression/qualification evidence, not a completeness proof. Unknown representations, unqualified platforms and excluded configurations cannot acquire support by passing an example.

Preserve [ADR 0001](0001-upstream-compatibility.md), [ADR 0003](0003-follow-stable-upstream-releases.md), [ADR 0004](0004-packet-emission-ownership.md), accepted BBR/ECN decisions, default Reno spurious-loss undo and adaptive-loss contracts. Do not introduce custom I/O providers/wrappers, restricted/network-admitted/candidate-group or send-thread-QoS combinations, multipath v2, FEC, duplicate DATAGRAM delivery, public scheduling callbacks, arbitrary server socket association, coupled congestion control or address discovery. Ordinary unregistered endpoints preserve their existing contracts. Local queue/native timeouts remain nonfatal backpressure for ordinary and multipath peers on granted endpoints. A qualified-native contract violation faults/quarantines the endpoint without freeing a live borrow.

**Existing-work disposition:** new runtime slices; no unmerged implementation is accepted as a dependency. Retain exploration/sizing as history; the linked slice audit records disposition of prior documents. Q2 #599 remains an external unfinished qualification slice; no result, exception or execution readiness transfers to this program.

**Evidence discipline:** write the named failing or characterization cases first. For runtime slices, at most 16 new deterministic scenario tests (R2/R16 at most 20) cover the listed semantic families, one positive per behavior and one negative per distinct supported failure owner. Run affected-package tests and vet; Go/source or dependency changes also require `go mod tidy -diff`. One focused race run is budgeted where the slice crosses execution contexts; no repeated timing stress is implied. At most one discriminating central-guard mutation per owner is allowed only if needed to prove inherited coverage reaches that guard. No recursive fixture-completeness obligation, syntax-variant matrix, new platform or additional operational campaign enters through review. The exact operational inventory/ceiling is below. If the accepted family cannot fit, stop and re-audit rather than silently grow the slice.

**Authority and preservation:** there is no persisted schema or restart round trip in this library program; each new runtime fact must cover construction, validation, all listed destructive/security consumers, cancellation/close and fresh-instance reconstruction in the same slice. A private activation gate is a transitional seam, not permission to negotiate incomplete protocol behavior with arbitrary peers. Intermediate fixtures may activate only their declared complete scenario; ordinary public construction remains unchanged. Do not let a prefactor change controller meaning or ordering. Never widen a temporary seam; only its named removal slice may delete it after every migration dependency is complete.

**Artifact classification:** runtime mechanisms are shipped behavior; native authority, CID/AEAD, generation, resource and disposal guards are required safety enforcement. Unit/integration tests, traces and bounded fixtures are verification aids. The regression coverage named by these accepted safety contracts and the pinned required peer job are maintained blocking deliverables: payoff is catching ownership/wire regressions, domain is the declared profile and semantic cases, owner is the transport test suite, and retirement requires equivalent replacement or feature removal. Qualification receipts, plans, journals and issue/task state are process metadata. No extra analyzer/harness becomes blocking by implication.

**Contained-design comparison:** changing the owner to do synchronous native I/O would remove a handoff but violate the accepted responsive connection loop. Requiring all PacketConn implementations to cancel would change a closed compatibility contract. Retain one endpoint control/writer boundary, with connection-owned policy and fact-only completion. Do not duplicate protocol decisions in route adapters. Revisit only if those public/responsiveness contracts are explicitly changed; all other rejected slice split/merge alternatives below have `none: rejected on merit` unless their stated context or contract stop is reached.

## Goal and current shape

Make one QUIC connection carry useful reliable and unreliable traffic over a sticky primary and validated backup, with path-scoped recovery and bounded lifecycle. Keep one TLS session, stream set and logical delivery owner.

Verified seams: `conn_id_manager.go:66` / `conn_id_generator.go:81` own CID sets; `internal/handshake/updatable_aead.go:178` / `:182` currently reconstruct/open one application PN context; `internal/ackhandler/sent_packet_handler.go:284`, `:417`, `:1074`, `:1293` own registration, ACK, PTO and migration; `path_manager.go:69` / `:94` commit challenge/amplification. `transport.go:974` onwards owns routing and captured callbacks. `packet_emission.go:212` packs before ownership handoff. This track changes those owners deliberately, not by importing the reference state machine.

## Identity and protocol ownership

Retain the connection goroutine as protocol-state owner and the private emission module as the owner of construction, registration and output handoff. Add a private path registry which owns path membership and delegates path-specific recovery and sending state. Keep shared application payload ownership in streams, the DATAGRAM queue and the retransmission owner. A second path does not create a second stream set, TLS session, or flow-control budget.

| Owner | State that belongs there |
| --- | --- |
| Connection | Handshake and Initial/Handshake spaces; shared key schedule and key phase; total AEAD limits; streams and flow control; connection lifetime; aggregate resource limits; application policy; globally unique registration identity where still useful. |
| Protocol path | Monotonic wire path ID; current tuple and a separate tuple/migration generation; local/remote CID sets; validation and anti-amplification; application PN/ACK history; loss/PTO/RTT; ECN and PMTU; controller and pacer; availability/backup preferences; retirement state. |
| Emission | Path selection before packing; immutable path-qualified packet registration; buffer ownership; per-path local pending-byte accounting plus an aggregate cap; compatible GSO batches; submission authority; completion routing back to the correct owner. |
| Application/consumer | Address discovery and admission policy, interface/socket choices, preferred use of expensive paths, identity policy, and definitions of useful payload delivery. |

Use `(path ID, application packet number)` for application transmission identity, with explicit Initial and Handshake identities. A tuple generation fences feedback about a rebinding or migration within one path; it is not a replacement for wire path ID. A late ACK for an abandoned path can retire retained evidence for that path without changing the live path's controller. Retransmitted STREAM bytes remain one logical delivery obligation, even when represented by transmissions on different paths.

The receive seam must recover path ID from the destination CID before packet-number reconstruction and AEAD opening. Today `updatableAEAD` has one `highestRcvdPN`, `largestAcked`, and first-sent/received key-phase progress. Making only the nonce path-aware would leave incorrect reconstruction and key-update behavior. Keep key phase shared; adapt [RFC 9001 §6](https://www.rfc-editor.org/rfc/rfc9001.html#section-6) to independent path spaces rather than duplicating crypto setup. [Draft §2.5](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-2.5) preserves the general update principles and recommends a spacing of three times the largest path PTO after confirmation before another update; it does not supply a detailed per-path confirmation state machine or an exact previous-key discard deadline.

The protection contract identifies the path and key generation of an acknowledged transmission independently of the ACK's arrival path. An old-phase high PN on A cannot confirm a new-phase low PN on B. Define previous/next receive-key selection, bounded old-key retention with silent or abandoned paths, and connection-wide AEAD usage counters. Per-path transition boundaries are one implementation option, not a requirement to obtain confirmation from every path. A packet whose phase bit matches the current key bypasses the baseline's old/next-key PN comparison; adversarial paper traces must distinguish that case from reordered previous-phase packets. Keep Initial/Handshake ownership and path-zero 0-RTT behavior explicit without prescribing separate concrete handler objects.

ACK history also has a carrier and a target. Today each sent packet stores one `LargestAcked` and its acknowledgment calls one `ignorePacketsBelow`. A packet sent on A that carries PATH_ACK for B must remember B's pruning boundary; acknowledging the carrier advances B's receive-history watermark, not A's. Bundled PATH_ACKs need one target association each, and ordinary ACK targets path zero. Keep this metadata path-qualified through carrier loss, retransmission or retirement; do not add a second payload-delivery owner. Existing receive history has a range cap, so this is a correctness/efficiency obligation rather than a claim that current history is unbounded.

Audit connection-wide RTT consumers separately from recovery: for example, `newFlowController` currently receives `c.rttStats`. Define the aggregate observation used for flow-control autotuning and connection-level timing, while handshake recovery and each path's loss/PTO stay with their correct owner. Choosing the lowest RTT globally without reviewing these callers would be an unexamined behavior change.

On send, the scheduler sees only paths currently eligible under validation, local/peer preference, congestion, pacing, MTU, local credit and authority. It selects a path before the packer consumes stream or DATAGRAM work. The packer receives a path-scoped context for destination CID, PN, ACK/control selection, size, and sealing. Emission then registers against that same path and enqueues immutable path metadata. Socket completion must never be attributed by consulting whichever path is preferred later.

Keep path scheduling and queue ownership behind the existing `sender` seam, with a connection-wide byte ceiling and wakeup aggregation. Path queues remain independently revocable, but every actual write on a bounded-I/O transport passes through the one endpoint writer below. A slow write on a shared socket can therefore delay other paths for its remaining operation budget; independent sockets can progress independently. The connection goroutine never waits synchronously for socket completion. Each queue releases buffers/reservations exactly once using its captured owner/generation. Pacing, credit and recovery remain per path; native capability/error state and deadline authority remain with the actual endpoint. GSO batches cannot mix destination, socket, ECN treatment or incompatible segment size. GRO splitting preserves CID-based path attribution even for equal tuples.

### Submission cutoffs and connection closure

Abandonment must stop submission, not merely new packing. Before submitting PATH_ABANDON for A on B, mark A unavailable, revoke its path-specific submission permission and establish that no submission still underway can follow the abandonment signal. Dispose unsent entries, batch tails and segmented retries through exact-once buffer/credit cleanup, and resolve their already-registered recovery obligations without making local disposal count as peer acknowledgment. The boundary must be rechecked at each actual socket submission, including retries; ordinary `sendQueue.Close` is insufficient because it drains. Do not block the connection goroutine waiting for a worker: completion of the worker fence enables signaling and later cleanup. An already completed kernel submission cannot be recalled and may arrive later. A syscall already in progress must finish or reach the declared fence before local abandonment is signaled; the draft does not guarantee a maximum time for this local operation. Remote abandonment immediately revokes future submissions when processed and resolves already-started work at the same boundary without pretending it was recalled.

Use path/tuple-generation permissions separately from connection-wide authority: retiring one path or migrating its tuple must not disable the survivor. A deterministic future case pauses A's worker with ordinary entries, a partial batch and a retry pending, abandons A via B, then resumes A and checks ordering, exact-once cleanup and reliable-data continuity. This is one semantic boundary across submission forms, not a new public authority interface.

Require bounded native send completion for the supported multipath profile. Retirement remains visibly submission-pending and charged to its path slot until completion, but the supported writer must resolve that pending I/O within the local budget below. This replaces the earlier proposal to accept indefinitely retiring paths. Completion supplies a definite accepted prefix where available, suppresses unsent tails/retries after revocation, and releases borrowed memory only after the native operation has returned. Cooperative cancellation can shorten this wait only if its acknowledgment has the same no-future-submission and no-borrowed-reference meaning. A context signal or timed-out wait by itself supplies neither guarantee.

This bounds local submission cleanup under the qualified Go/runtime/OS contract, not network failure detection, peer acknowledgment, CID credit, protocol retention, replacement validation or complete transport detachment. `WaitTransportDetachedV1` can still time out without detaching. An already submitted datagram can arrive late. Never declare callback entry to be submission, free in-use buffers, leak replacement workers to escape the path cap, or implicitly close a shared/caller-owned socket. A native implementation violating its qualified deadline contract is an endpoint fault outside the supported progress guarantee: stop admitting work there, report the fault, keep unresolved ownership quarantined within the existing caps and use surviving endpoints where possible. Even connection termination cannot safely free memory still borrowed by a non-returning operation. Do not disguise such a violation as successful bounded cleanup.

Local connection close chooses a still-usable path rather than assuming path zero survives. Stop ordinary scheduling, apply the appropriate submission cutoffs, and prepare a bounded retained close response for each selected open path context whose peer CID remains usable; a per-path cache is the selected simple implementation. Responses retain their own DCID/PN/nonce identity and may only use a valid associated path/tuple and socket. An abandoned context is not a fallback for later cached replies. Retain CID-routing and reset-token state only within the close/resource allowance, then remove it once; received traffic cannot extend retention indefinitely. Follow [draft §2.6](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-2.6) for closing/draining duration based on the largest path PTO, with a bounded snapshot at transition specified in the lifecycle contract. A changed tuple alone does not make an otherwise valid ciphertext fail AEAD: CID identity determines the path. Check path-zero failure, abandonment, alternate-path close and late cached-response triggers in `closed_conn.go`, the CID generator and transport routing.

## Public surface and compatibility

```go
func (c *Config) ConfigureMultipathV1(enabled bool, maxPaths uint32, policy string) error
func (t *Transport) ConfigureMultipathIOV1(maxWriteDuration time.Duration) error
func (t *Transport) WaitMultipathIOStoppedV1(ctx context.Context) error
func (c *Conn) MultipathNegotiatedV1() (bool, error)
func (c *Conn) OpenPathV1(ctx context.Context, t *Transport, remote *net.UDPAddr) (*Path, error)
func (c *Conn) PathsV1() []*Path
func (c *Conn) PathChangesV1() <-chan struct{}
func (c *Conn) WaitTransportDetachedV1(ctx context.Context, t *Transport) error
func (p *Path) PathIDV1() (uint32, error)
func (p *Path) StateV1() (MultipathPathStateV1, error)
func (p *Path) MigrateV1(ctx context.Context, t *Transport) error
func (p *Path) SetLocalUseV1(use string) error
func (p *Path) AdvertiseStatusV1(status string) error
func (p *Path) AbandonV1(code uint64) error
```

Required contracts:

1. Configure before Dial/Listen/GetConfigForClient returns. Disabled is the default. Initially accept `maxPaths` of 1 or 2 and the conservative primary/backup policy only; larger caps and aggregation need the later accepted profile. The cap counts active plus validating protocol paths including path zero; retained evidence and at most one validation tuple per protocol path need separate bounded allowances; a submission-pending retirement still occupies its protocol-path slot. It is not a literal `initial_max_path_id` value or lifetime path-attempt budget. Validate local cap and policy without mutation on error. Copy/Clone must preserve the selection, with no process-global side table. Before any advertisement, require nonzero local and peer CIDs: select that opted-in socket-owning DialAddr/DialAddrEarly select a nonzero default before initializing their single-use transport. Explicit transports with an already fixed zero-length CID generator return a local configuration error; do not relabel this as peer non-negotiation. Validate selected server configurations before advertising; an advertising peer's zero-length CID is handled as the protocol error required by [draft §2.1](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-2.1). Caller-socket Dial/DialEarly require the explicit-Transport route under the entry-point matrix. Disabled convenience dialing retains its existing behavior.
2. A connection exposes negotiation separately from local configuration. Without peer support it carries ordinary QUIC traffic. New path operations return an identifiable not-negotiated result rather than opening another connection or silently using migration. Before negotiation completes, report handshake-incomplete rather than an ambiguous permanent absence.
3. `OpenPathV1` is client-only, uses the caller's exact transport/socket, and accepts an explicit remote endpoint for a server address already known and admitted by the application. That is necessary but not sufficient: enforce the draft's handshake-address/`disable_active_migration` restriction and distinguish preferred-address migration from new-path creation. The initial scope should require the remote endpoint to reach the server's original `Transport`, for example through its wildcard/multihomed socket. Current client `AddPath` registers CIDs on another local transport; there is no corresponding public server operation for attaching an existing connection to an unrelated server transport. Arbitrary additional server sockets need a separately accepted association/routing contract and are outside this estimate. `OpenPathV1` returns after required validation, with context cancellation and no implicit socket ownership transfer. Cancellation before commitment to path initiation releases only local reservations; once initiation consumes an ID, cancellation or validation failure follows the explicit retirement procedure below. The transport checks its own policy and CID compatibility too.
4. `PathsV1` returns a snapshot of multipath handles, including path zero and peer-created server paths; before negotiation or without support it returns an empty snapshot, distinguishable through `MultipathNegotiatedV1`. Handles remain safe after abandonment. Use the immutable StateV1 snapshot specified below. `PathChangesV1` is a coalescing invalidation notification, not a lossless event log; callers resnapshot. No application callback executes on the connection goroutine. Terminal context and qlog supply complementary diagnostics.
5. Local send use and advertised peer preference are separate controls. `SetLocalUseV1("backup")` governs the local scheduler. `AdvertiseStatusV1("backup")` requests how the peer should use the path; it cannot promise peer compliance or zero incoming traffic. Validation, ACK and liveness traffic may still use a backup.
6. `AbandonV1` is idempotent locally and returns after the connection owner accepts retirement, not after peer acknowledgment, worker fencing or requeue completion. It prevents new ordinary scheduling and orders required signaling and cleanup after the submission cutoff above. Retirement/requeueing preserves existing reliable-frame ownership and per-path flight. Selected local abandonment of the last usable path returns a distinct error unless the caller closes the connection. Remote last-path abandonment follows the protocol-specific procedure below; network loss alone uses the accepted failure/timeout policy. A pending fence remains observable as retiring and charged to its existing slot; it does not create replacement capacity.
7. Existing `AddPath`/`Probe`/`Switch`/`Close` retain their documented contracts in ordinary QUIC. On a negotiated multipath connection, legacy path methods return an explicit mode error. For locally enabled but not-yet-negotiated connections, report handshake-incomplete; for an unsupported peer, legacy migration remains available after the result is known. The selected `MigrateV1` supplies the missing application operation in negotiated mode: on a client path, validate the existing remote endpoint through an exact, non-nil target transport, then request a same-wire-ID tuple transition. Preserve the PN sequence and key-generation identity; use CIDs associated with that path ID. It consumes no new wire path credit, so it works in negotiated-zero or exhausted-credit mode when the peer permits migration and the required CID is available. A spare CID or a successful validation is not guaranteed; report/wait for those prerequisites within `ctx` without abandoning the old tuple. Do not use this API to evade `disable_active_migration` or to select an arbitrary new remote address.
8. Transport reuse, path retirement and closing the underlying socket remain distinct. At the baseline, `Transport.close` destroys every registered connection handler, including a connection attached through `AddPath`. Preserve that explicit-close behavior: `AbandonV1` acceptance and `MigrateV1` success alone do not make a transport safe to close. The selected connection-level detachment observation below supplies that missing condition. Attribute network I/O errors to their affected paths/sockets rather than automatically applying explicit-close semantics to every error.

For `MigrateV1`, select at most one locally initiated open/migrate operation at a time. Its path uses the bounded validation slot described below; independent incoming rebinding on another path may proceed. Validate before retiring the old tuple, then fence old-generation submissions and publish the new tuple to emission; do not block other paths on that fence. Keep old flight and late feedback generation-qualified while initializing appropriate RTT/controller/PMTU and tuple-specific ECN validation for the new tuple. Cumulative ECN accounting and the wire PN space persist. `MigrateV1` returns success only after cutover; cancellation before the connection owner commits cutover leaves the old tuple selected, and cancellation after that commit reports success rather than claiming rollback. A simultaneous abandonment cancels the attempt. The sentinel identities below apply; the connection owner commits cutover after validation and the old submission fence, before publishing the replacement tuple.

The selected operation preserves make-before-break for a local-socket change within an existing path. It does not add a third protocol path: replacing a full slot with a different remote endpoint still requires retire-then-open and can temporarily reduce redundancy. A three-path cap or explicit replacement operation is outside this contract; revisit only on an accepted scope change. Incoming rebinding and preferred-address path-zero migration remain protocol obligations independently of this local API. Include negotiated-zero migration, exhausted credit, two occupied slots, failed/cancelled validation, repeated replacement and old-generation feedback in the design's evidence gates.



### Target transport and detachment

Both `OpenPathV1` and `MigrateV1` must require a non-nil target with the bounded native-I/O grant and the same effective local CID length as the connection. Check the exact socket, platform qualification, exclusive endpoint owner and incompatible registrations before attachment or credit mutation. For an initialized target, use its fixed routing length; for an uninitialized target, evaluate the length its supported initialization would select, including `ConnectionIDGenerator.ConnectionIDLen()`, and preserve that choice through initialization/attachment. A four-byte connection cannot attach to an eight-byte target. Reject incompatibility with a local configuration error before CID registration, wire-ID consumption, path-credit mutation or migration cutover, preserving the existing path. Do not silently override an initialized target's decoder. A different custom generator with the same effective length is not rejected solely for being different. This is a first-profile constraint on the baseline's fixed-length routing/unpacker, not a proposal for heterogeneous-length decoding.

Use `WaitTransportDetachedV1(ctx, t)` as an observation of this connection's attachment to an exact transport, usable in either negotiated endpoint role. It does not retire paths, migrate them or close the transport. Success means no live or validating path of this connection uses `t`, all submissions/completions through it have resolved, and its routing registrations can no longer dispatch connection destruction or send work for this connection. Retained protocol history may remain on the connection and receive late feedback through survivors. The conservative first proposal keeps old-socket routing until its needed late-packet/CID retention ends, then unregisters it and publishes detachment; no new terminal-handler design is assumed. For abandonment, use the receipt-based retention rule below. For migration, separately bound old-tuple routing retention under the migration contract without retiring the still-live wire path ID.

Detachment publication must be serialized with transport close and already-captured handler callbacks; removing a map entry alone cannot make a concurrent earlier `Close` harmless. If close wins and destroys the connection, report its terminal error rather than claiming safe detachment. `ctx` expiry stops waiting only. A nil target returns a local argument error; a non-nil transport never attached to this connection is already detached, with normal handshake/not-negotiated errors still applying. The observation covers all this connection's paths on `t`: abandoning one of two shared-socket paths cannot detach it. After success, existing attachments cannot spontaneously reappear; the caller must serialize new open/migrate operations using that transport against closing it. Other connections/listeners on the same transport remain the caller's responsibility. Use the selected retention and detachment contract below.

### Failed initiation and remote last-path abandonment

Distinguish an API request from protocol initiation. Cancellation before the connection commits an ID to an attempt and before any submission can occur releases reservations without requiring abandonment. Once committed for initiation, keep the ID consumed even if local submission is later uncertain or validation fails; conservatively close that attempt through the cutoff and `PATH_ABANDON` procedure on a survivor. The returned context error does not assert wire retirement is complete. [Draft §§3.1 and 3.4](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-3.4)

For received last-path abandonment, select prompt connection closure by default. The draft also permits a client to attempt a replacement and a server to wait briefly for one; those alternatives are rejected for this profile; revisit only on an accepted failure-policy change. Reply to abandonment when required, and retain issued-CID/PN evidence for the draft's recommended three PTOs measured from receipt of `PATH_ABANDON`. Local cancellation does not start that receipt-based timer. The selected unanswered-abandonment timer below bounds escalation; expiry cannot silently erase routing or authorize ID reuse. Retention capacity and safe transport detachment remain separate gates.

Use the additive V1 signatures above, including ConfigureMultipathIOV1, WaitMultipathIOStoppedV1, WaitTransportDetachedV1 and same-ID MigrateV1. Preserve existing `*Path` handles with explicit mode checks. Registration is pre-initialization and binds the exact unconnected native UDP socket. Caller-socket convenience Dial/Listen and HTTP/3 Serve reject enabled use; explicit granted Transport and library-owned Addr/default HTTP/3 paths are supported under the endpoint plan's entry-point matrix. All selected server configurations pass the same gate before advertisement.

Freeze the following public sentinel identities, tested with `errors.Is`; strings are not a contract. Local admission/resource errors do not imply packet submission, while a native result is classified only by its completion owner.

| Error identity | Meaning |
| --- | --- |
| `ErrMultipathInvalidConfigV1` | Invalid argument, budget, policy, CID compatibility or conflicting registration. |
| `ErrMultipathHandshakeIncompleteV1`, `ErrMultipathNotNegotiatedV1` | Negotiation pending versus completed without support. |
| `ErrMultipathWrongModeV1`, `ErrMultipathUnsupportedV1` | Legacy/multipath handle mismatch versus excluded role, platform or feature combination. |
| `ErrMultipathBackpressureV1` | Local capacity/deadline cause only; no delivery-certainty or safe-replay guarantee. |
| `ErrMultipathDeadlineV1` | Local queued/native deadline; also matches `os.ErrDeadlineExceeded` and backpressure. |
| `ErrMultipathUncertainV1` | Native acceptance cannot be determined; may also match deadline, but never proves non-submission. |
| `ErrMultipathResourceLimitV1`, `ErrMultipathLastPathV1` | Bounded local resource admission failure versus prohibited explicit last-path abandonment. |
| `ErrMultipathNotConfiguredV1`, `ErrMultipathNotStoppingV1` | Grant absent versus join requested before shutdown. |

Terminal connection errors retain their existing identity; closed transport operations match `net.ErrClosed`, canceled waits preserve `ctx.Err()`, and oversized public UDP writes retain an identifiable native size error. If deadline and uncertain categories both apply, expose both without asserting definite non-submission. Public backpressure describes a local cause, not delivery certainty. Internal completion enums preserve accepted prefix, uncertain remainder and unsent tail separately; the snapshot's coarse category is not recovery authority.

For observation, add an immutable value snapshot `MultipathPathStateV1` containing wire ID, tuple generation, copied local/remote addresses, lifecycle state, local use, peer preference, primary flag, validation status, RTT/PMTU and last local outcome category. Use `uint32` wire ID, `uint64` tuple generation, value `netip.AddrPort` addresses, `time.Duration` RTT and `uint32` UDP-payload PMTU (zero means not measured); lifecycle/use/preference/outcome are documented closed string sets, and primary/validated are booleans. Lifecycle is `validating`, `live`, `suspect`, `retiring`, `retained` or `closed`; local use is `active`/`backup`, peer preference is `available`/`backup`/`unknown`, and last outcome is `none`/`accepted`/`unsubmitted`/`uncertain`/`endpoint-fault`. Public numeric snapshots do not grant scheduling or deadline authority. `Path.StateV1() (MultipathPathStateV1, error)` returns it; `PathChangesV1` remains a coalescing invalidation. This additive fork type requires optional consumers to isolate it in their existing integration adapter; do not expose live internal pointers or a public scheduling callback. Connection aggregate loss stays aggregate; legacy path-zero RTT is used while zero is live, then the sticky primary's RTT, with the last snapshot retained after termination. New path snapshots provide path-specific observations.

Close seals admission/epochs and retains existing logical destruction semantics. Only successful WaitMultipathIOStoppedV1 returns write/deadline authority; it does not restore the caller's former deadline. WaitTransportDetachedV1 is separately required before Close when preserving connection survivors. An unresolved native borrow cannot produce a successful join, even after terminal connection state. Error/fault records remain observable after a late-return cleanup. No helper secretly closes a caller-owned socket.

### Primary, backup and failure policy

Initial path zero is the sticky primary; a newly validated path starts as local backup. SetLocalUseV1 accepts `active` or `backup`. Active paths are preferred to backups, while a healthy current primary stays selected among equally preferred eligible paths. All-backup state still permits useful traffic: retain the healthy current path or select the lowest wire ID among validated eligible backups. Peer status is advisory preference, subordinate to local eligibility and the application's local use choice. No automatic periodic backup traffic is added; the existing connection keepalive setting remains connection-wide.

A path becomes suspect after three consecutive application-space PTO expirations without acknowledging newly known-accepted ACK-eliciting traffic on its current tuple. Count only PTO attempts for which a packet is known accepted; local rejection, queued expiry and uncertain native submission do not increment this failure detector. ACKs returned on another path can establish delivery of the target path's packet. An authenticated matching PATH_RESPONSE also establishes current-tuple liveness. Do not use unrelated connection traffic or an old-tuple ACK to restore a changed tuple. The qualified PTO-attempt counter is distinct from existing recovery backoff: the connection loop resets it on current-tuple liveness or tuple cutover, while ordinary PTO backoff keeps its existing meaning. Endpoint-fatal errors immediately make all paths on that endpoint unavailable.

Suspect paths stop ordinary application scheduling but retain bounded probes/recovery evidence. Promote a fresh validated eligible alternative; if its last current-tuple liveness evidence is older than max(10 seconds, three current path PTOs), revalidate before application promotion. If none is eligible, keep bounded recovery/validation active until a path recovers or the ordinary connection no-progress deadline expires. Becoming suspect does not consume a new wire ID or automatically send PATH_ABANDON. A path that recovers becomes a backup; avoid automatic primary oscillation. Explicit abandonment, same-ID migration and remote last-path abandonment retain the lifecycle contracts in this plan. Remote last-path abandonment closes promptly rather than inventing a replacement grace period.

This policy makes no sub-RTT or bounded failover promise. It chooses cost-conscious primary/backup over opportunistic overflow: local endpoint backpressure alone waits for capacity. **Do not do this:** convert queue latency or ambiguous native acceptance into a remote failure signal. **Revisit:** none; a later explicitly selected overflow scheduler may use other healthy paths without calling the primary failed.

### Native error classification

Submission certainty and error cause are independent axes. Preserve existing first-send permission retry and GSO fallback only while the same absolute deadline and authority permit them (`send_conn.go:92–134`); classify the final result after those bounded operations. The connection loop, not the writer, applies the following policy to immutable endpoint/path/tuple-qualified facts.

| Final result class | Policy |
| --- | --- |
| Queued expiry, native timeout, temporary capacity exhaustion (`EAGAIN`/`EWOULDBLOCK`/`ENOBUFS`) | Nonfatal local backpressure; no failure-counter increment, path retirement, backup promotion or congestion-loss event solely for this cause. Resolve accepted/pending/uncertain portions separately. |
| Definite size rejection (`EMSGSIZE`) | Notify the affected PMTU owner and reschedule still-needed fitting reliable information; no blanket path failure. A partial/uncertain remainder retains its independent submission state. |
| Destination error (`ECONNREFUSED`, `EHOSTUNREACH`, `ENETUNREACH`, final `EACCES`/`EPERM`) or other send error without endpoint-wide failure evidence | For negotiated multipath, mark only the matching current tuple suspect/ineligible and use the selected promotion policy. Keep healthy survivors. With no survivor, retain bounded validation/recovery until recovery or ordinary no-progress termination. A late old-tuple result cannot demote its replacement. Ordinary/non-negotiated connections preserve baseline non-timeout error policy. |
| Closed/invalid endpoint (`net.ErrClosed`, `EBADF`, `ENOTSOCK`), failed deadline control or overdue qualified-native token | Quarantine that endpoint and stop admission; affect all its paths/users, preserve unresolved borrows, and retain healthy paths on other endpoints. Never infer successful join from the fault. |

A destination error does not automatically send PATH_ABANDON or spend another wire ID. The fallback class is deliberately path-scoped unless the endpoint owner has concrete endpoint-wide evidence; sharing a socket does not establish that every destination failed. Paper cases cover destination error with a survivor, without a survivor, stale tuple, size rejection, local timeout and endpoint-fatal failure within the existing deterministic slice budgets.

Primary eligibility means live, validated and authorized, neither suspect nor retiring; promotion also requires fresh current-tuple liveness or successful revalidation. It is distinct from immediate send eligibility: pacing, congestion-window exhaustion and local queue/credit shortage cannot clear or transfer primary or switch the RTT observation. The connection loop owns one designated-primary pointer and publishes the snapshot flags atomically with its transition. Promotion and explicit abandonment transfer it; there can be at most one primary, and none when no path is eligible. A suspect path's recovery changes its lifecycle to live and automatically sets local use to backup before reselection, even if it was formerly active. A later explicit SetLocalUseV1(active) overrides that demotion. Peer preference cannot override local use or validation. Local endpoint backpressure does not change the pointer.

| Event | Designated primary / local-use result |
| --- | --- |
| Initial path zero | Primary, active; newly validated alternatives are backup. |
| Current path suspect or abandoned | Select a fresh validated eligible active path, otherwise a backup, with lowest wire ID as tie-breaker; transfer primary. If none, clear primary and retain the last connection observation. |
| Suspect path recovers | Mark live + backup, retain the healthy promoted primary; no oscillation. |
| Explicit active/backup update | Reselect by local preference; retain the healthy current primary among equally preferred eligible paths, otherwise lowest wire ID. |
| All eligible paths backup | Retain healthy current primary or choose lowest wire ID; backup is not a prohibition on useful traffic. |

For the legacy connection RTT observation, "path zero is live" means lifecycle `live` and primary-eligible; while so, use its RTT. Otherwise use the designated primary's RTT, or retain the last snapshot when none is eligible/after termination. Keep the observation object used by connection and existing stream flow controllers stable; update its value through the connection owner rather than swapping a pointer that existing streams retain.

### Path handle modes

For handles, prefer retaining `*Path` with a private explicit mode and separate backing owners, so structural adapters can continue naming existing upstream types. A distinct multipath type avoids mixed method sets but couples optional consumers to a fork-only return type; this remains a D0 API choice. No server or path-zero handle may dereference a legacy outgoing-path manager. The selected mode matrix is:

| Handle / call | Legacy Probe, Switch, Close | PathIDV1 | Multipath lifecycle calls |
| --- | --- | --- | --- |
| Ordinary migration handle | Existing behavior | Explicit mode error, never a candidate ID presented as wire ID | Explicit mode error |
| Negotiated client path, including path zero | Explicit mode error | Stable wire ID | Proposed contracts, with last-path and migration constraints |
| Peer-created server path | Explicit mode error | Stable wire ID | Status/abandon permitted; local `MigrateV1` unsupported |
| Abandoned multipath handle | Explicit mode error | Retained stable ID | Abandon idempotent; other mutations report closed |

### Existing option and timing semantics

The first milestone preserves connection-created options when attaching another transport; it does not silently inherit every target option. Audit the concrete installation sites before final API acceptance:

| Existing surface | Proposed multipath rule / remaining acceptance |
| --- | --- |
| ACK threshold and receive phases | Keep the originating connection's configured threshold on its per-path application trackers and one connection-wide receive-phase state. A target transport's creation defaults do not reconfigure an existing connection. |
| Setup admission | Remains connection/handshake admission. Opening or migrating a path is not another connection setup, and gets its own bounded path-admission checks. |
| Target CID decoder | Require the connection's effective local CID length on each target, including initialized transports and custom generators. Reject before attachment/credit consumption; do not inherit a conflicting target decoder. |
| Native GSO/GRO, ECN/DF and batch capability | Use the exact qualified native socket's capability and lifetime grant; never copy capability from the preferred path. Multipath excludes external callbacks/managed wrappers. Unqualified acceleration falls back to a qualified ordinary sender before dispatch. |
| Send-thread QoS | Existing API promises connection-wide terminal status and one raised worker. Use rejecting multipath plus configured send-thread QoS on an origin or target until multiworker semantics are separately accepted. Ordinary behavior is unchanged. This exclusion matters to prospective native consumers and is included in configuration gating. |
| Explicit Transport.Close versus I/O failure | Close keeps its explicit logical destruction semantics and stops admissions; successful WaitMultipathIOStoppedV1 separately returns write authority. Normal local queue/write timeouts are nonfatal backpressure for both ordinary and multipath peers. Endpoint-fatal failure affects all users of that endpoint, while other endpoints can survive. |
| Public ConnectionStats and token RTT | Use path-zero RTT while path zero is live, otherwise the selected primary, with that selection documented for opted-in connections; aggregate connection loss counters remain aggregate. Add path-specific snapshots. The snapshot and RTT observation contract below defines the selected semantics. |
| Flow-control autotuning, idle/keepalive and CID/close lifetimes | Inventory each caller of `c.rttStats` separately. Recovery stays per path; close/drain and key-update spacing follow the draft's largest-PTO rules. Do not apply one RTT formula to every timer. Use the selected timer snapshots and backup liveness policy below; backup keepalives are not universally mandatory. |

The additive interface preserves [ADR 0001](0001-upstream-compatibility.md). The new mode errors affect explicitly opted-in behavior; they cannot be introduced into the default profile. Optional consumers should isolate fork calls using structural capability detection or a dedicated integration adapter, and continue to build with upstream. No module-path change is proposed.

A server configured to support multipath may still accept an ordinary client with a zero-length CID: omit the extension when that peer has not advertised it, preserving ordinary fallback. If the peer does advertise it with a zero-length CID, apply the mandated connection error rather than silently downgrading. This differs from a locally fixed zero-CID generator, which cannot satisfy the requested opt-in configuration.


## Payload policy

Start with a private scheduler called by emission before destructive packing. Its input is a bounded immutable set of eligible path views plus work class/size requirements; its output is a path choice or the next wake deadline. It cannot mutate congestion state, admit an unauthorized address, invent credit, or retain packet/payload pointers. Keep this an internal seam so a backup policy and an aggregation policy can vary without exposing recovery internals.

Selected initial default: prefer the primary available path; use a validated alternate when the primary is unusable under a defined failure policy. Temporary congestion-window exhaustion or local I/O backpressure does not establish path failure. The first primary/backup policy waits for endpoint capacity on a healthy primary; it does not automatically promote the backup solely because of local timeout. Independent endpoint-fatal or network-path failure can still trigger promotion. No particular failover latency is promised. The selected primary/backup policy above supplies failure/recovery thresholds, promotion, all-backup behavior and liveness cost. The design must distinguish a strict cost-sensitive standby policy from an opportunistic overflow policy.

Aggregation is owned by the BBR/aggregation plan; this track enables primary-backup only.

Retransmit lost reliable information on any eligible path by default, with same-path preference while it remains healthy. Preserve mandatory same-path controls such as PATH_CHALLENGE/PATH_RESPONSE. The retransmission owner tracks logical STREAM ranges/control obligations; each newly transmitted packet uses the destination path's PN, nonce, flight and congestion allowance. Do not retransmit an encrypted packet unchanged. An ACK for an original after retransmission must not double-retire logical work or another path's congestion flight. Application DATAGRAM messages remain unreliable and are not automatically retransmitted or duplicated.

GridCast's distinct rateless symbols fit nonduplicating DATAGRAM distribution, but ordinary packet scheduling cannot promise exact application-symbol placement or equal unique payload per path. If the consumer needs explicit per-message steering, size a later `SendDatagramOnPath`-style seam with queue/MTU/admission semantics. Do not smuggle application payload classification into a packet callback. That optional public steering feature is outside the core estimate; a small internal datagram eligibility change is included.

Each path has its own PMTU. A shared DATAGRAM queue must either admit only sizes usable by every path selected for that message or retain a set of eligible paths and define what happens when that set disappears. Prefer the conservative common eligible-path size for the first generic policy, with an explicit error/limit on admission. Existing send admission never guaranteed delivery, but a new path must not silently promise a lifetime-fixed maximum. Preserve the existing first-path-only meaning of `PathMTUSettledV1`; introduce separate per-path observations rather than rewriting that contract. Reconcile the consumer's maximum-payload contract in wiremux #1738 before offering explicit steering.

Admission alone is insufficient when eligibility changes while a message is queued. The packer's existing Pop-on-no-fit branch assumes one path and distinguishes ACK pressure through `pl.ack`; PATH_ACK representation must preserve that distinction. Do not discard a message because the selected path or current packet lacks room if another eligible path/current fresh packet can carry it. Defer to a fitting path or a fresh packet without destructively consuming the message. If no eligible path can carry it, apply a declared bounded expiry/drop policy and observability rather than retaining it indefinitely. Add focused smaller-MTU and PATH_ACK-space cases; ordinary unreliable DATAGRAM semantics do not justify knowingly dropping usable queued work at the wrong packing boundary.

## Resource admission and retention

The baseline incoming migration manager already caps candidate slots at three, tracks challenge count and validation deadlines, and reserves actual probe bytes before I/O. Preserve those properties but do not confuse that candidate cap with a negotiated multipath limit. Existing outgoing path handles and exponentially bounded probe retries do not constitute a connection-wide multipath resource policy.

Use an initial local cap of two active or validating protocol paths including path zero, with submission-pending retirement still charged to that cap. Allow at most one locally initiated open/migrate operation and at most one active validation candidate per protocol path, two connection-wide. An open consumes a protocol-path slot and validates its initial tuple there; an established path may instead validate one extra tuple for migration/rebinding without a new PN space/controller history. This deliberately replaces the former single connection-wide candidate allowance. Use separate bounded allowances for fenced retained paths and pending requests. The selected resource limits below govern; qualification measures their costs before support. Never allocate arrays proportional to a peer maximum or sparse ID. IDs are monotonic and unreusable; increasing MAX_PATH_ID restores credit without creating unbounded simultaneous state.

Key validation candidates by wire path ID, tuple and generation; equal tuples on different IDs remain distinct. A backup open on path 1 and peer rebinding on established path 0 can each use its path's candidate slot, as can simultaneous rebinding on both established paths. Local and incoming attempts for the same path serialize: retain the admitted candidate until its absolute deadline, completion or cancellation, rather than replacing it on each new tuple. Additional tuples get no extra candidate or borrowed amplification budget; resource-limited validation may be deferred and the peer must retry. Reject/defer a competing local operation within its `ctx`. When incoming work is already admitted, do not displace it for a new local operation. Bound challenge retries and pending control state, never refresh the absolute validation deadline merely because more traffic arrives, and retain a slot while cancellation awaits an outstanding submission fence. Each unvalidated tuple uses only its own received-byte allowance. This supplies bounded contention behavior without promising service for arbitrarily many simultaneous tuple changes.

For the selected cap of two, initially advertise `initial_max_path_id=1` (cap one uses zero), and proactively issue a small CID window for the lowest eligible unused path ID allowed by both endpoints. Use two CIDs per used/openable path, subject to the peer's per-path active-CID limit and aggregate allowance, so one can serve a subsequent tuple change. Do not issue for a path ID beyond either current limit. Maintain at most one unused path-ID issuance window; raise local MAX_PATH_ID one step only when consumed/abandoned membership and retention budgets permit replenishment. Because grants cannot be withdrawn, retain bounded metadata for already issued IDs and handle reordering without assuming sequential arrival. Skipped IDs and consumed failures do not justify proportional allocation. Resolve PATHS_BLOCKED/PATH_CIDS_BLOCKED by credit/issuance or a controlled decline; replenishment need not wait for every old ACK if separate retention capacity is available. This is a selected policy derived from [draft §§3.1–3.2](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-3.2), not a claim that the draft mandates eager allocation of all allowed IDs.

For full-slot replacement, select **no new unused-ID pre-grant while both protocol slots are charged**. Existing grants cannot be withdrawn. A fenced retiring path may leave the live-cap accounting only after its retained state has reserved capacity; its unresolved submissions cannot be relabeled as lightweight retained history. At that transition, replenish by one ID without waiting for all old packet ACKs, subject to both endpoints' independent limits and CID availability. If retention is full, keep admission blocked rather than increase memory or forget issued IDs.

| Replacement step (client C, server S) | Grants and usable CID sets | Membership / consequence |
| --- | --- | --- |
| Paths 0 and 1 established | C and S each advertise maximum 1; each has supplied CIDs for 0 and 1. No ID-2 grant or CID window is added while both slots are charged. | Two live slots on each endpoint. |
| C requests retirement of 1; its submission fence is pending | Grants stay 1; there is no new ID-2 window. | C keeps slot 1 charged and cannot open a replacement. Path 0 can carry traffic. Qualified native sends resolve within the local completion budget; peer credit, retention and validation remain independent gates. |
| C completes the fence, sends abandonment on 0 and reserves retained-state capacity | C can advertise maximum 2. It withholds ID-2 CIDs until S's maximum also allows 2. | C has live path 0 plus retained path 1; S may still have live path 1. No assumption of simultaneous state transitions. |
| S processes abandonment and completes its own cutoff/retention admission | S can send reciprocal abandonment and advertise maximum 2. Each endpoint issues its bounded ID-2 CID set once both maxima permit it; missing control frames/CIDs still gate opening. | Each can now hold path 0, retained path 1 and a validating path 2 within separate allowances. Receipt of abandonment starts each endpoint's retention interval. |
| Both endpoints have the usable ID-2 CIDs needed for initiation | C initiates 2; validation uses its own path slot and tuple amplification accounting. | Successful validation restores two usable paths. Late ACKs for 1 affect retained evidence only. |
| Asymmetric credit or full retention | If S stays at maximum 1, or has not supplied an ID-2 CID, C waits/reports blocked within the open context and may use the protocol's blocked signals. | No ID reuse, forced peer grant or silent third slot. Path 0 alone may continue. |

The replacement table is a state example, not a fixed packet sequence or latency promise: control frames can be bundled, lost or reordered, and receipt of one does not imply receipt of another. Measure time to restore redundancy separately from time to resume useful traffic; it includes fence progress, credit/CID exchange and replacement validation. A future pre-grant policy would need a separate admission/decline tradeoff.

Bind each locally issued CID to a wire path ID and bounded routing/reset metadata before publishing it. On ingress, use that binding for PN reconstruction and authentication without allocating a full controller/history from an unauthenticated tuple. Only authenticated input can request validating membership; apply the local live/validation/resource limits, recognize previously validated tuples where allowed, and allocate heavier state at the appropriate validated stage. The server must accept legal authenticated packets for an unused issued ID even if they contain no PATH_CHALLENGE; if it declines the path, send the required abandonment on a usable path and retain only bounded terminal metadata. Include CID issuance, reset tokens, retained close responses and tombstones in the idle-server-per-connection memory measurement, not only BBR stores. Outstanding grant, live membership, retiring evidence and tuple candidate are four different quantities.

Track pre-validation sent/received bytes on the actual path and apply the draft/RFC anti-amplification rules independently there; traffic on an established path does not buy amplification credit on an unvalidated tuple. The draft permits using an already validated tuple without repeating validation; do not claim a new path ID universally requires a new challenge. Defer expensive controller/history allocation until validation succeeds where permitted, while retaining enough bounded state to process validation and required control frames. Bound retries, idle validation, pending control frames, tombstones, retained ACK evidence and aggregate queued bytes. Rejected input must not manufacture a new full path context solely from an unauthenticated tuple or large ID.

BBR currently permits substantial bounded stores: 25,000 live delivery entries, 4,096 retained delivery entries and 32,768 recovery outcomes in the inspected source. Multiplying every maximum by path count can dominate the small path-registry cost. Establish aggregate as well as per-path limits, and measure idle, validated-backup, saturated and abandoned-path memory. Resource exhaustion must produce a controlled limit/decline/degradation outcome, never unbounded growth or silently corrupted delivery evidence.

Retain two validating/live protocol slots, at most one validation candidate per protocol path, at most one local open/migrate operation, and at most 16 committed challenge records per candidate. Share four retained-history units between fenced retired protocol-path records and retained old tuple generations of still-live paths. Reserve an old-generation unit before admitting migration/cutover; reserve a retired-path unit before releasing its live slot. Pending retirement without a history unit still occupies the live slot. Reusing a transport does not avoid the generation charge. Block/defer replacement or return ErrMultipathResourceLimitV1 before attachment when capacity is unavailable; never evict required evidence or shorten retention. Cap distinct per-connection transport-routing attachments at eight (two live, two candidate and four retained), with per-transport reference counts for shared use. Reserve attachment capacity before CID/handler registration. Release a history/attachment charge only after its retention interval, submission fence and captured callbacks have all resolved; connection close uses the same ownership rules. Retained recovery without routing continues under the separate existing connection-wide recovery limits. Preserve received-abandonment and issued-CID safety. Advertise an active-CID limit of four per path and initially issue two where peer credit permits. Bound each issued and received CID store separately to 16 active plus retiring records per path, including the single unused-ID window; never size arrays from peer maxima. If compliant churn exhausts that local allowance, stop issuing new CIDs and retire that path using PATH_RESOURCE_LIMIT_REACHED when a survivor exists. If it is the last usable path, close the connection with INTERNAL_ERROR and a local resource-exhaustion cause; do not forget a live routing/reset obligation or accuse the peer of violating its advertised limit. An actual peer limit violation retains its protocol error.

Paper trace: four serial migrations whose old generations are still retained fill the shared history pool even though only one operation runs at a time. The fifth fails before attaching another transport. A canceled admitted candidate releases its candidate charge only after its fence/callbacks resolve; it never frees an older retained unit. Expiry plus fence/callback completion of one old generation makes one unit available. Shared-transport reuse reduces distinct attachment count but not retained generations. This finite accounting bounds repeated successful migrations without inventing an earlier detachment guarantee.

### Retention and bounded deferred work

Use the existing five-second absolute validation lifetime, shortened by a local operation's context; incoming traffic does not restart it. After received abandonment, retain issued-CID/PN evidence for three times a snapshot of the largest extant path PTO at receipt. After local abandonment without a reciprocal frame, retain the obligation and retry on survivors; after max(30 seconds, three times the largest-PTO snapshot at the first known-accepted abandonment transmission), close the connection with INTERNAL_ERROR if the peer still has not reciprocated. Local backpressure does not start this peer-response timer. These choices follow the latitude in [draft §3.4](https://datatracker.ietf.org/doc/html/draft-ietf-quic-multipath-21#section-3.4); they never treat an unacknowledged local request as received abandonment. The ordinary connection no-progress timeout can terminate earlier. A close still cannot reclaim an unresolved native borrow.

On same-ID migration, fence old-generation submissions before cutover and retain old-transport routing for three times the largest-PTO snapshot at cutover, without refreshing it from arriving traffic. At expiry, generation-tagged outstanding recovery remains connection-owned; remove old routing only after its submission fence and captured-handler callbacks resolve. Subsequent late feedback through surviving transports may consume that retained recovery, but cannot restore the old tuple. Closing/draining uses three times the largest-PTO snapshot at entry. Next-key-update spacing starts only when an ACK confirms the previous update and uses three times the largest-PTO snapshot at that confirmation. Saturating timer arithmetic is mandatory, and no arbitrary short cap overrides a protocol minimum. A large peer-dependent PTO can delay detachment; native request completion is the separately bounded guarantee.

Multipath DATAGRAM admission uses the common currently eligible application-path payload limit. A queued datagram that still fits an eligible path or a fresh packet is retained for that opportunity; ACK packing pressure alone cannot discard it. If no eligible path can carry it after an eligibility/PMTU transition, drop it immediately with a bounded local drop observation. Do not wait for future path growth, retransmit it or alter ordinary unregistered DATAGRAM behavior. Reliable information remains in its logical owner for rescheduling.


The 4,096 participant cap applies to ordinary as well as multipath connections attached to a granted endpoint. Local setup refusal returns ErrMultipathResourceLimitV1 before attachment. On a server, reuse the existing bounded CONNECTION_REFUSED response path (`server.go:1035,1187`) through the capped stateless response class; when that class is full, drop the response under its existing best-effort rule. One cap-plus-one case and its response-cap-full variant cover admission without a new load campaign. Unregistered endpoints keep their existing admission behavior.

Public `ErrMultipathBackpressureV1` identifies a local capacity/deadline cause only; it says nothing about accepted bytes, safe replay or delivery certainty. Match sets are: capacity refusal → Backpressure; queued expiry → Deadline + `os.ErrDeadlineExceeded` + Backpressure; accepted prefix with deadline on the remaining request → those deadline/backpressure identities plus the internal accepted-prefix/proven-tail facts; uncertain native timeout → Uncertain + Deadline + `os.ErrDeadlineExceeded` + Backpressure. Add Uncertain whenever a remainder is ambiguous. Neither `errors.Is(Backpressure)` nor `n == 0` authorizes replay. Only the internal per-portion completion enum can prove non-submission.

## Protection decision

The one AEAD owner holds current/previous/next key generations and connection-wide usage counters. Each admitted path holds highest reconstructed receive PN and an optional first authenticated PN for the current generation; sent registrations retain path, PN and actual key generation. An ACK confirms only that sent generation, regardless of ACK carrier. Current phase selects current keys directly. Opposite phase considers retained previous keys for a PN below that path's first-current boundary or an unknown boundary; considers next keys only where update ordering permits. For an unknown boundary, a bounded authenticated previous/next trial may resolve ambiguity, without accepting a phase change before successful authentication. Failed trials count against global AEAD limits. No lazy path-key allocation from an unknown CID/path and no independent TLS session. Silent paths do not veto shared update; expired previous keys cause old packets to be dropped. Confirmation and the selected largest-PTO spacing gate the next local update. Independently derive vectors and test reordered/silent-path cases; if the finite model cannot disambiguate admitted traffic without violating RFC 9001, stop before R5 implementation rather than transplant the reference heuristic.

Connection-wide flow-control autotuning observes the legacy path-zero RTT while it is live, then the sticky primary, retaining the last observation after termination; this is an observation, never the per-path loss/PTO source. Keep ordinary idle timeout semantics connection-wide and keepalive on selected primary. Close/drain and key-update timers use their explicitly selected largest-PTO snapshots. This avoids silently treating every RTT consumer as a minimum-over-paths metric.

## Contract closure: path authority and delivery

Material crypto/delivery/lifecycle consequences plus independent ingress/egress/teardown states trigger the following bounded closure. Constructors and all destructive consumers must agree on path/tuple/key identities; tests are finite representative evidence.

| Semantic class | Invariant and central owner | Finite evidence | Slices |
| --- | --- | --- | --- |
| Ordinary zero / Initial / Handshake / path-qualified application | Identity does not alias; parser plus connection/AEAD owner | Default-off, same PN/two paths, 0-RTT and key-phase cases | R2–R7 |
| Unknown/issued/authenticated/new path | Allocation/authority follows CID binding and authentication; registry/CID owners | Sparse IDs, no challenge, two candidates, limited window | R4/R8/R9 |
| Cross-carrier ACK / retransmission / early completion | Retire target transmission and logical data once; recovery/delivery owners | Bundled target pruning, original late ACK, local disposal | R6/R7/R10 |
| Validation and tuple cutover | Per-tuple amplification and fenced transition; registry | Contention, cancellation, mismatched target, old ACK | R9/R13 |
| Retirement / replacement / close / detach | No future old submission after signal or false release; registry/transport | Unused/consumed received abandonment, queued/current cutoff, retention/credit, captured close callback | R8/R9/R12–R14 |
| Path preference / local backpressure / network evidence | Local cause never becomes path-failure evidence; connection policy | Qualified PTO, stale backup, all backup | R15 |
| Constructor and selected config | No unsupported option can advertise; one capability gate | Exact entrypoint classes and server-selected configs | R16/R19 |

Parser byte aliases stay with the authoritative parser; no alternative-encoding census. Optional guard mutation is at most one per relevant central guard, only if discriminating inherited coverage requires it.

## Selected-value precedence

The explicit parameter/error/snapshot/retention paragraphs in this plan govern over explanatory examples. Two live slots include validating and submission-pending retirement; four shared retained-history units cover retired paths and old tuple generations; at most eight distinct transport-routing attachments and one unused-ID window are separate bounded records. Each direction has at most 16 active-plus-retiring CIDs per path, advertised active limit four and initial issuance two where peer credit permits. DATAGRAMs with no currently eligible fitting path drop immediately; no future-growth wait. The endpoint plan owns endpoint count/byte/completion/diagnostic caps and qualified local time. Neither path retention nor peer cooperation inherits B.

## Accepted product thresholds

The owner accepted the following policy for the eventual program; acceptance does not dispatch implementation or experiments in this session:

1. Allow an exploratory implementation program before consumer benefit has been demonstrated. Public support remains opt-in and experimental until correctness, independent interop, native completion and ordinary-peer cost gates pass. Recommending adoption additionally requires the comparative benefit evidence; no campaign or implementation is launched by accepting this document.
2. On a matched ordinary-only unregistered versus opted-in comparison, require at least 95% of baseline useful goodput, no more than 110% of baseline CPU per useful byte, and p99 control/send-completion latency no worse than the larger of 110% of baseline or baseline plus 1 ms. Also require no timeout-only connection closes, bounded memory and the declared fair progress. These are accepted planning tolerances, not measurements or pre-existing SLOs.
3. For aggregation, require at least a 20% useful-goodput gain over the better single path on the balanced disjoint-path cell, at least 95% of two matched independent connections' useful goodput there, and no more than a 10% reduction in a competing ordinary Reno flow's useful goodput relative to the two-independent-connection comparison on the shared-bottleneck cell. Report asymmetric STREAM completion and DATAGRAM useful delivery separately; a failed cell blocks the corresponding support claim, not an automatic expansion of experiments. No claim of fairness equivalent to one congestion-controlled flow is made for uncoupled multipath.

Changing these tolerances, benefit timing or the operational ceiling requires an explicit owner decision. Failed evidence does not silently relax them.

## Program qualification ledger and sequencing

For ordinary mixed-load comparisons, the unregistered control substitutes two independent ordinary connections for one multipath connection while holding socket count, total offered load and controller selection constant. Report ordinary-peer costs separately from the background traffic. A baseline that cannot represent the same workload is an unresolved comparison, not a passing cell.

Initial native support is Linux amd64 with exact native UDP sockets on the two Go versions currently used by repository CI, 1.26.10 and 1.27.3, on one recorded supported kernel image. Record its kernel/build identity during fixture qualification; a missing image pin blocks the qualification slice, not the design of internal logic. macOS/Windows ordinary behavior remains protected by existing CI; multipath support there is deferred to separately scoped native qualification, not inferred from Linux or cross-compilation. A toolchain/kernel change requires reassessing affected native evidence, not replaying an unlimited matrix.

The native completion qualification must exercise real UDP deadline installation/reset and the native Go poll path after EAGAIN, alongside the deterministic coordinator races. Use a disposable native test process with syscall-level EAGAIN injection for the actual send syscall, retaining the real UDP socket and Go deadline/poller implementation; separately run actual socket-pressure traffic and record whether it induced waiting. Injection is evidence of the native polling deadline path, not proof that kernel pressure occurred. The ordinary expired-deadline test alone is insufficient. Establish this method's feasibility before enabling the grant publicly; if it cannot reliably reach and observe the intended path, stop and revise the qualification method rather than substitute a passing fake. Do not add production test hooks or treat a goroutine timer as cancellation. The source basis is [Go's native poll/write loop](https://go.dev/src/internal/poll/fd_unix.go) and [timeout tests](https://go.dev/src/net/timeout_test.go), plus [strace syscall-error injection](https://github.com/strace/strace/blob/686aa55405a09925a77ea0dea59a292b0fb70243/doc/strace.1.in). Exact tag source, injection-tool version and send syscall are checked during the fixture slice. Injection supplies EAGAIN, never a delayed or falsely successful syscall result; record native return and poll/deadline traversal before claiming this case executed. A debugger stop or injected syscall sleep does not qualify the deadline contract.

Use draft-21 as the protocol contract and picoquic `01e124ebdf9fc50df307e01e71f2744471ddc470` as the initial independent peer, with its PicoTLS commit and container digest recorded by the fixture. Keep the negotiated-zero oracle gap explicit and cover it with independent normative vectors plus a controlled peer; do not call self-to-self tests independent interop. The required peer job is an intentionally maintained verification deliverable: payoff is detection of wire incompatibility, domain is the declared v1 matrix, owner is the transport integration suite, and retirement requires equivalent independent coverage or feature removal. Missing prerequisites or skipped expected cases fail the job. No recursive harness-completeness program is authorized.

Budget the eventual Linux evidence package as follows: six native ownership/deadline classes on each of the two Go toolchains; eight interop scenarios in both client/server roles on the primary toolchain; six ordinary-peer workload/acceleration cells comparing unregistered and opted-in endpoints; and three aggregation topologies with the six controller-matched arms named below. Each performance comparison has three paired 60-second observations after one 10-second warmup, solely to report a paired median and range; no confidence-interval or Internet-wide claim. Cap the whole package at 180 run instances and eight host-hours, including failed attempts. Missing/inconclusive evidence is a stop, not permission to add cells or rerun until passing. Capture exact build/workload seeds and charges. The owner accepted this future qualification budget; it is not execution authorization in this session. The planned regular inventory is 118 run instances: 12 native, 16 interop, 36 ordinary comparison observations and 54 aggregation observations. The aggregation count includes the two BBR single paths, two independent BBR connections, informational Reno aggregation, BBR aggregation and the BBR fixture-only round-robin comparison, each on three topologies with three observations. Any diagnostic rerun spends the remaining ceiling, requires a recorded cause and cannot add a new semantic cell or an attempt solely to obtain a pass.

The native classes are expired-before-dispatch, EAGAIN-to-deadline completion, retry/fallback preserving the absolute deadline, queued expiry while another request is current, shutdown/grant return and shared bounded-endpoint participant/path request contention. Interop scenarios are two-path useful STREAM/DATAGRAM transfer, absent negotiation fallback, path-zero ACK/PATH_ACK compatibility, key update with a silent path, status/backup failover, abandon/reopen with late feedback, same-ID migration/rebinding and unequal-MTU/invalid-frame handling. The six ordinary cells are STREAM with GSO on/off, DATAGRAM with batching on/off, mixed ordinary/multipath bulk and ordinary traffic under stateless pressure. Aggregation topologies are balanced disjoint paths, unequal delay/rate paths and a shared bottleneck with a competing ordinary Reno connection. These are semantic distinctions, not a Cartesian cross-product.

Require Q2 #599 to complete and record its accepted code/evidence baseline before merging changes to shared recovery/congestion behavior. I/O ownership preparation, parser work and behavior-preserving prefactors may proceed independently only if their slice shows unchanged ordinary controller behavior and does not alter the candidate under qualification. No Q2 result transfers automatically to the later multipath candidate. The full handoff will show these as genuine blocking edges, not hide them in task prose. Later BBR/multipath support also depends on completed Reno-first correctness and native gates.

Fixture smoke/setup attempts also charge the same ledger. The 118 figure is the regular final-candidate inventory, not an exemption for earlier smoke or failed setup runs. Repeating an already budgeted semantic case after a candidate change requires a recorded affected-surface reason and spends remaining capacity; it cannot add a new cell. E6 owns 12 native instances, R17 the 16 final interop instances, R18 the 36 ordinary instances and B5 the 54 aggregation instances. Each execution preflight pins the exact candidate, comparison, workload and remaining budget. Missing or invalidated evidence blocks public support; extra scope or insufficient capacity returns to the owner.

### Controller-matched aggregation arms

The fixed aggregation inventory is six arms × three topologies × three observations = 54 run instances. Workload, socket count, offered load, path shaping and competing traffic are matched as applicable. All ordinary single/pair baseline arms use BBR so the aggregation support comparison isolates the selected BBR multipath policy.

| Arm | Controller / policy | Claim |
| --- | --- | --- |
| A | One ordinary BBR connection on path A | Single-path baseline. |
| B | One ordinary BBR connection on path B | Other single-path baseline; choose the better measured A/B result for the threshold. |
| C | Two independent ordinary BBR connections, one per path | Independent-connections baseline. |
| D | Reno multipath aggregate, intentional paced byte-round-robin fallback | Informational only; cannot qualify public Reno aggregation or its adoption. |
| E | BBR multipath aggregate, selected arrival estimate with fallback | Candidate for the accepted aggregation thresholds. |
| F | BBR multipath, fixture-forced byte-round-robin | Scheduler comparison only; not a public policy. |

Balanced disjoint-path thresholds compare E with max(A,B) and C. Shared-bottleneck harm compares the same ordinary Reno competitor's useful goodput alongside E versus alongside C. Unequal-delay/rate STREAM completion and DATAGRAM useful delivery are reported separately with the declared semantics; no new numeric threshold is invented. Valid negative comparative evidence completes B5 and leaves aggregation unsupported; B6 can publish qualified BBR primary/backup. Missing/invalid prerequisites stop qualification. Passing these BBR comparisons does not qualify Reno aggregation. Supporting that combination later requires a separately accepted controller-matched campaign; this handoff does not add it.

### Candidate and run ledger

| Delivery | Candidate / scope | Planned charge |
| --- | --- | ---: |
| E6 | Endpoint candidate, native request-class boundary; applicability checked against the later Reno candidate | 12 native instances |
| R17 | Complete Reno primary/backup candidate, both independent-peer roles | 16 interop instances |
| R18 | Same Reno candidate, two endpoint profiles × six ordinary cells × three observations | 36 ordinary observations |
| B5 | Complete BBR/aggregation candidate, six controller-matched arms × three topologies × three observations | 54 aggregation instances |
| R1 and R14 | One ordinary control smoke per role in R1; one two-path STREAM smoke per role after complete close handling in R14 | 4 setup/smoke instances |
| B5 affected revalidation | BBR primary/backup: useful-transfer, abandon/reopen and same-ID migration interop scenarios, each in both roles | 6 existing-cell instances |
| B5 ordinary revalidation | BBR primary/backup background in mixed ordinary/multipath bulk and ordinary-under-stateless-pressure cells, two endpoint profiles × three observations | 12 existing-cell observations |
| Remaining reserve | Cause-recorded failed attempts, affected existing native/ordinary cells or necessary repeats; never new semantic cells or pass-seeking retries | 40 instances maximum |

The regular inventory remains 118; the planned four early smokes plus 18 BBR revalidation observations make 140 scheduled instances, leaving at most 40 inside the cumulative 180-run/eight-host-hour ceiling. A "run instance" is one candidate/arm observation; paired ordinary profiles consume two, and a control warmup within an observation is included in its duration, not hidden extra capacity. Charge host-hours as summed occupied host time, including setup and failed attempts. Preflight must pin candidate/comparison SHAs, dependency/container/kernel/toolchain identities, workload seeds, remaining run/host-hour budget and per-invocation timeout; do not start an invocation whose maximum reserved cost exceeds either remainder.

The 18 listed repeats are required BBR-candidate evidence, not optional reuse of the Reno receipt. Revalidate any additionally affected existing native/ordinary cell within the 40-run reserve with its concrete source-change reason; unchanged owners/domain may retain an earlier receipt only through an explicit applicability record against the current candidate. If changed semantics require more or different evidence than the fixed inventory/remaining budget, stop for a scoped decision. BBR primary/backup support needs its own complete correctness and the applicable/revalidated native, interop and ordinary-cost evidence; aggregate support additionally needs arm E's comparative pass. Q2 evidence never automatically transfers. No execution is authorized by this ledger.

Separate native-poller traversal evidence from uninstrumented timing evidence. The EAGAIN-injection observation proves the real poll/deadline path executes; ptrace/injection elapsed time does not establish the 20 ms supported-environment tolerance. Use the already budgeted non-injected expired-before-dispatch, shutdown/join and shared request-contention observations plus the actual-pressure component to measure native return, independent expiry and nonblocking publication against that tolerance. Record injected and non-injected timings separately; lack of an observed non-injected wait is recorded, not converted into pressure evidence. Missing feasible traversal or timing evidence blocks native qualification within the same 12 instances; it grants neither an exemption nor extra runs. E6's contention is endpoint participant/path request classes, not proof of negotiated multipath; integrated mixed traffic belongs to R17/R18.

## Reference adaptation

Use the MIT-licensed qoke reference at `084c13ea3834fbe58cedb63db9208951d714f839` selectively. The following mapping is based on reading the pinned files, not treating the fork's test report as our evidence. Preserve license/provenance for copied material. The reference's public scheduler, duplication, automatic-address and coupled-controller facilities are not required dependencies.

| Reference module | Local owner and decision | Reason / verified boundary |
| --- | --- | --- |
| `internal/wire/path_*`, `max_path_id_frame.go`, transport parameters | Adapt codecs and vectors into the existing wire parser; enforce local encryption-level/negotiation and canonical field validation there. | Small protocol representation modules; local `internal/wire/frame_parser.go` and transport-parameter owner remain authoritative. Do not import unrelated extension types. |
| `internal/handshake/updatable_aead.go:43` pathKeys and `:92` receive-key selection | Adapt the path-qualified nonce/key-state model, then independently derive vectors and integrate under the existing AEAD owner. | Local `internal/handshake/updatable_aead.go:178` and `:182` currently have unqualified PN reconstruction/opening. Reference lookup/allocation/drop semantics need local admission/retention guards; no lazy allocation from an unauthenticated arbitrary path ID. |
| `internal/ackhandler/sent_packet_handler.go:71` pathRecovery and `:241` EnableMultipath | Reimplement path-context selection around the fork's current recovery owner; reuse protocol expectations/tests selectively. | Local `sent_packet_handler.go:284`, `:417`, `:1074` and `:1293` own registration, ACK, loss alarm and migration. Fork-specific Reno undo, adaptive loss, BBR and credit cannot be overwritten by the reference's controller replacement. |
| `multipath_connection.go`, `multipath_path.go`, `multipath_send.go` | Translate wire lifecycle obligations into connection-loop state and the existing emission owner; do not transplant the send state machine. | Local `packet_emission.go:546`, `path_manager.go:69`, `transport.go:540` and `:580` establish the probe commitment, output and teardown contracts that the reference does not supply. |
| `multipath_failure_detector.go:115` and schedulers | Use as comparison material only; implement the selected native-outcome-aware failure policy and private scheduling seam locally. | Reference age-of-first-unacked detection and public scheduler/quota interfaces do not encode the selected local-backpressure policy or this fork's ownership. |
| `interop/multipath/Dockerfile` and tests | Adapt the pinned peer build and endpoint driver structure, replacing API calls with the fork's accepted surface and adding strict executed-case accounting. | Reference image pins picoquic but uses a moving Debian tag; record the base digest and dependency identity before claiming reproducibility. Known oracle gaps remain. |

**Owner's design comparison:** synchronous native sends in the connection loop would remove completion handoffs but violate the accepted responsive-loop and bounded shared-endpoint requirements. Existing PacketConn/external-provider contracts cannot be rewritten to require cancellation under ADR 0001. The explicit native grant plus one control/writer boundary is therefore retained; it adds generation-qualified, late-result, quarantine and join obligations that must stay in the one endpoint owner. **Do not do this:** put timeout policy independently in each send caller. **Revisit:** only if the accepted I/O/public contract changes; no review may infer that authority.

## Slice graph

| Slice | Disposition | Delivery | Blocked by |
| --- | --- | --- | --- |
| [R1](#r1) | new | Pin the independent peer and ordinary fallback control | None |
| [R2](#r2) | new | Parse the bounded draft-21 wire profile without advertisement | None |
| [R3](#r3) | new | Make path-zero context explicit without changing recovery | R2 |
| [R4](#r4) | new | Bind CID routing to bounded protocol-path identity | R2 |
| [R5](#r5) | new | Protect path-qualified packets under one key schedule | R3, R4 |
| [R6](#r6) | new | Route receive ACK histories by target and carrier | R5 |
| [R7](#r7) | new | Give Reno paths independent recovery and pacing | #599, R6 |
| [R8](#r8) | new | Issue bounded path credit and CID windows | R4 |
| [R9](#r9) | new | Admit and validate a second authenticated path | E5, R7, R8 |
| [R10](#r10) | new | Carry reliable traffic over the selected path | R9 |
| [R11](#r11) | new | Schedule DATAGRAMs with path-qualified PMTU | R10 |
| [R12](#r12) | new | Replenish retired path credit and reconcile late delivery | R10 |
| [R13](#r13) | new | Migrate one path and observe safe transport detachment | R12 |
| [R14](#r14) | new | Close via surviving paths and publish path-qualified diagnostics | R1, R12 |
| [R15](#r15) | new | Apply sticky primary and bounded backup promotion policy | R10 |
| [R16](#r16) | new | Complete additive APIs and constructor gates behind disablement | E6, R11, R13, R14, R15 |
| [R17](#r17) | new | Complete the independent interoperability matrix | R16 |
| [R18](#r18) | new | Measure ordinary-peer costs on granted endpoints | R16 |
| [R19](#r19) | new | Enable the qualified experimental Reno profile | R17, R18 |

## Implementation slices

<a id="r1"></a>

### R1 — Pin the independent peer and ordinary fallback control

**Delivers:** Add the reproducible pinned picoquic build/driver and strict executed-case accounting. Demonstrate the ordinary no-negotiation control through current public APIs; reserve multipath scenarios for later slices. This is the maintained peer deliverable approved by the design, not a second protocol implementation.

**Blocked by:** none. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Transport integration suite owns the peer fixture and exact dependency pins. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Multipath driver calls are absent, not stubs pretending to pass; R14 adds the first complete two-path smoke and R17 finishes the matrix.

**Blast radius and context:** interop fixture/build definition; integrationtests/self ordinary driver. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Example-level evidence over the fixed scenario/toolchain domain, parsed by the pinned peer/fixture's existing protocol implementation. This is the explicitly maintained verification deliverable described above; receipts are metadata. Runtime closure is not recursively imposed on the fixture. Missing/incorrect case accounting is a failed claim, not evidence of product failure.

**TDD / evidence budget:** pinned dependency build; one ordinary transfer in each endpoint role; missing prerequisite/expected-case refusal. Build focused driver/accounting regressions first; operational attempts charge the single program ledger. Stop after the fixed cases or first unresolved prerequisite/budget failure; no repeat-until-pass.

**Context imports:** Program qualification ledger and sequencing; Reference adaptation. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Ordinary fallback is a complete reusable behavior and establishes an independent oracle before crypto integration. Merging with R14 would hide build/oracle failures behind protocol changes. No production prerequisite. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Pinned peer cannot build or claims draft behavior unsupported by source; no silent peer replacement.

<a id="r2"></a>

### R2 — Parse the bounded draft-21 wire profile without advertisement

**Delivers:** Adapt multipath codecs/transport parameter into the real parser and canonical validator, with path-zero ACK compatibility and 1-RTT-only gates. Keep public negotiation disabled; receiving unsupported extension frames follows the selected protocol error. Add wire path identity distinct from tuple generation.

**Blocked by:** none. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Existing wire parser owns bytes/varints/frame bounds; configuration/negotiation guard owns whether extension semantics are permitted. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Inert parsing representation is an expand step, not authority to activate a path. R16 integrates configuration and R19 enables the complete supported profile.

**Blast radius and context:** internal/wire/frame_parser.go, transport_parameters.go, frame_type.go; new path frame codecs. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 20 deterministic scenario tests under the binding rule. Start with: representative round trip per new semantic frame; invalid path bound; wrong encryption level; absent negotiation; ordinary ACK targets zero; default-off byte equivalence. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Public surface and compatibility; Reference adaptation (wire row). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Keep codecs plus real parser gates together; a codec-only library has no independently protected ingress. Do not merge R3: path context/recovery refactoring is independently testable. New codec files are small and share existing parser rules. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Draft revision changes required semantics or import requires unrelated extension representations.

<a id="r3"></a>

### R3 — Make path-zero context explicit without changing recovery

**Delivers:** Prefactor application packet identity/context selection so the existing ordinary path-zero flow carries explicit path and tuple identity through registration and packing. Initial/Handshake remain connection-owned. Preserve existing algorithms, ordering and one active path behavior.

**Blocked by:** R2. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Existing connection, packer and recovery owners keep all policy; explicit context replaces ambient preferred-path lookups. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** A single path-zero adapter supplies the explicit context. R7/R10 migrate multipath consumers; R19 removes any temporary unqualified context access. Ordinary path-zero construction remains permanent.

**Blast radius and context:** packet_emission.go, packet_packer.go, internal/ackhandler/interfaces.go, packet.go, sent_packet_handler.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: characterize default Reno packet prefix; ordinary migration generation; Initial/Handshake identity; unchanged ACK/loss/pacing; no multipath advertisement. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Protection decision (connection observation only). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Behavior-preserving expand is independently green and may precede Q2 only with unchanged controller semantics and untouched pinned candidate. Split at a typed interface if the governing diff exceeds the context budget; do not merge path recovery behavior into the prefactor. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Any ordinary recovery/congestion behavior changes; then Q2 becomes blocking and the slice must be re-audited.

<a id="r4"></a>

### R4 — Bind CID routing to bounded protocol-path identity

**Delivers:** Extend CID lookup/registration with a bounded wire-path binding before decryption while preserving ordinary routing, reset-token and closed-handler behavior. Unknown/unissued IDs cannot allocate path state. Exercise duplicate tuple/path-zero cases through real ingress routing.

**Blocked by:** R2. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** CID generator owns issued bindings; transport handler map owns routing references; connection admits authenticated path membership later. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Only path-zero production bindings exist. R8 introduces the bounded unused-ID window; no unauthenticated allocation authority.

**Blast radius and context:** conn_id_generator.go, conn_id_manager.go, transport.go, packet_unpacker.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: issued versus unknown CID; equal tuple distinct path binding in fixture; fixed CID-length mismatch; reset-token retirement; ordinary zero-CID fallback. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Resource admission and retention; Target transport and detachment (CID length only). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Routing identity and destructive removal must land together; split issuance windows into R8 because they add credit policy. R2 provides wire identity, not recovery dependency. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Representation, ownership, public boundary or one-PR context changes; any untraced effect or need to exceed the declared evidence budget.

<a id="r5"></a>

### R5 — Protect path-qualified packets under one key schedule

**Delivers:** Integrate path nonce/PN reconstruction and per-path transition evidence under one TLS/key-phase owner. Include previous/current/next receive-key decisions, silent/abandoned path retention, global AEAD limits and ACK confirmation by sent key generation. Public negotiation remains off. Capture actual key generation at sealing, carry it in registration, extract it before acknowledged packet records are pooled and forward it through the real connection ACK caller.

**Blocked by:** R3, R4. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** AEAD owner alone changes shared key generation and usage counters; bounded admitted path records own PN reconstruction/transition boundaries. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Private crypto fixture supplies admitted identities; no lazy state from wire path IDs. R9 connects authenticated membership.

**Blast radius and context:** internal/handshake/updatable_aead.go, aead.go, crypto_setup.go, interface.go, packet_unpacker.go; packet_packer.go:52, packet_emission.go:322–355, internal/ackhandler/packet.go:16–24, SentPacket:284–320, ReceivedAck:529–568 and connection.go:2337–2369 (focused sealing-to-ACK chain). Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: independent nonce vector; same PN on two paths; old high PN versus new low PN; reordered previous key; silent path after update; key retirement; aggregate AEAD limit; path-zero 0-RTT. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Protection decision; Retention and bounded deferred work (key timing only); Reference adaptation (AEAD row). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Nonce, PN and key update are inseparable security consumers of the identity. Do not merge lifecycle validation: authenticated membership is a different owner. R3 supplies identities and R4 trusted CID attribution. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Cannot bound previous/current/next keys or distinguish confirmation without duplicating TLS sessions.

<a id="r6"></a>

### R6 — Route receive ACK histories by target and carrier

**Delivers:** Extend receive tracking and ACK construction for independent application path spaces. Store each PATH_ACK target/watermark in its carrier registration; acknowledgment prunes target history even when carrier differs. Preserve ordinary ACK for path zero and unique Initial/Handshake spaces.

**Blocked by:** R5. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Received-packet handler owns target history; carrier packet metadata transports pruning facts; connection applies them once. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Receive path views remain fixture-only until R9; no second logical payload owner. R7 adds matching sent-space handling.

**Blast radius and context:** internal/ackhandler/received_packet_handler.go, received_packet_tracker.go, packet.go, sent_packet_handler.go, packet_packer.go; internal/ackhandler/lost_packet_tracker.go carrier receipt lifetime. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: bundled targets in one carrier; carrier ACK on another path; carrier lost/retired; ordinary ACK zero; duplicate packet per path; range cap. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership (ACK carrier/target); Resource admission and retention (history bounds). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** ACK creation and carrier-consumer pruning must ship together. Merging sent-loss algorithms would obscure separate ownership. R5 is required for authenticated PN identity. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Representation, ownership, public boundary or one-PR context changes; any untraced effect or need to exceed the declared evidence budget.

<a id="r7"></a>

### R7 — Give Reno paths independent recovery and pacing

**Delivers:** Partition application sent histories, loss/PTO, Reno registration prefix/undo and adaptive-loss state by protocol path, with aggregate logical delivery/resource ceilings. Demultiplex target ACK independent of carrier; preserve per-generation retained evidence and path-zero behavior. Create one stable connection-owned RTT observation/accessor for connection and already-created stream flow controllers; constructors/readers use it while per-path recovery retains separate RTT state. Later retirement/primary transitions update that same owner.

**Blocked by:** #599, R6. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Sent-packet handler owns per-path recovery; each Reno controller owns its own ordered registrations/pacer; shared logical delivery remains connection-owned. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Only private two-context fixtures activate additional recovery. BBR+multipath explicitly unavailable until B track. R9/R10 supply real paths and emission.

**Blast radius and context:** internal/ackhandler/sent_packet_handler.go, congestion_dispatch.go, lost_packet_tracker.go, internal/congestion/cubic_sender.go, packet_order.go; connection.go:567–577 and :2935–2942; flow_controller_base.go and constructor readers (focused RTT observation chain). Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: same PN independent loss; cross-carrier ACK target; independent PTO wake; spurious-loss undo on one path; adaptive threshold isolation; aggregate caps; ordinary Reno unchanged; stable existing-stream and connection autotune observation after path-zero retirement. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Protection decision (connection observation); Resource admission and retention; accepted Reno undo/adaptive-loss ADRs. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** A separate controller-only slice cannot be valid before registration/ACK/loss routing agrees. Keep BBR out. Q2 is a real shared-behavior prerequisite; R6 supplies correct target identities. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Reno ordering/undo needs a new algorithm or path partition exceeds bounded context; re-slice before code.

<a id="r8"></a>

### R8 — Issue bounded path credit and CID windows

**Delivers:** Implement two live slots, one unused-ID issuance window, monotonic grants, per-path CID limits and compliant-churn resource decline through real control handlers. Do not allocate arrays from sparse peer IDs or grant replacement credit while an unresolved submission still occupies a slot. Establish the common terminal-transition owner via unused-issued-ID abandonment: real control ingress queues the reciprocal frame and retains bounded CID/terminal evidence. Compliant resource decline uses this owner; no terminal cleanup is deferred to a successor.

**Blocked by:** R4. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection path registry owns membership/credit; CID managers own bounded issuance/retirement and routing obligations. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Lightweight issued-ID admission has no heavy controller authority. The unused-ID terminal owner is complete here; R9 extends it to consumed/validating/initial-live paths. Replacement credit remains fixed at the initial window until R12, and public activation remains disabled.

**Blast radius and context:** conn_id_generator.go, conn_id_manager.go, connection.go; path registry module. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: two slots and one window; asymmetric maxima; skipped/consumed ID; zero-credit fallback; active-limit violation versus compliant churn; no silent CID eviction; abandon an unused issued ID and retain routing evidence. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Resource admission and retention; Failed initiation and remote last-path abandonment (unused ID); Selected-value precedence. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Credit and CID availability define one initiation condition and must agree in the same PR. Complete unused-ID terminal handling here and consumed-path terminal handling in R9; keep replacement credit and established late-delivery integration in R12. R4 provides safe routing ownership. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Resource bounds require evicting live reset/routing evidence or treating compliant churn as a peer protocol error.

<a id="r9"></a>

### R9 — Admit and validate a second authenticated path

**Delivers:** Connect bounded path membership to authenticated ingress and client initiation in a private driver: per-path tuple candidates, anti-amplification, challenge publication, deadline/contention and validated activation. Handle legal new-path packets without PATH_CHALLENGE and previously validated tuples. Extend the same registry terminal owner through consumed/validating/initial-live abort, timeout, decline and received abandonment, including submission cutoff, reciprocal signaling, retained PN/CID obligations and connection cleanup. Keep replacement credit at the initial window until R12. Admission reserves the two live slots, shared four-history-unit budget and eight distinct transport-attachment ceiling before establishing authoritative routes.

**Blocked by:** E5, R7, R8. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection path registry owns lifecycle and candidate arbitration; emission commits probe accounting; endpoint owns native borrow. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Private owner fixtures activate only complete declared scenarios. Abort, timeout, received abandonment and cleanup are mandatory here, never fixture exclusions. Replacement credit is withheld until R12; R14 supplies path-qualified cached close before independent-peer traffic; R16/R19 own public activation.

**Blast radius and context:** connection.go, path_manager.go, path_manager_outgoing.go, packet_emission.go; path registry. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: two simultaneous candidate slots; local/incoming contention on same path; authenticated no-challenge initiation; unauthenticated sparse IDs; pre/post-ID cancellation; no cross-tuple amplification; absolute expiry; received abandon during claimed validation send, cutoff then reciprocal/PN-CID retention; retained-history saturation with live-slot reservation. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Resource admission and retention; Failed initiation and remote last-path abandonment; Submission cutoffs and connection closure; endpoint probe/result interface receipt. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Initiation through validation and mandatory teardown is one authority-complete behavior; splitting teardown afterward is unsafe. R8 already supplies the common terminal owner; E5 transitively supplies E3 probes and all endpoint fences, and R7 supplies recovery. If this does not fit 24k tokens, split behavior-preserving candidate plumbing before R9 grants live authority, not safety cleanup after it. Replacement credit and established late-delivery integration remain R12. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Second path can be reached from public config before complete supported lifecycle or controller is allocated from unauthenticated tuple.

<a id="r10"></a>

### R10 — Carry reliable traffic over the selected path

**Delivers:** Select path before destructive packing; send STREAM/control through its CID/PN/nonce/recovery/endpoint context in deterministic real-owner fixtures, preserving logical reliable delivery across retransmission. Do not yet negotiate with an independent peer; the first live-peer smoke moves to R14 after complete path-qualified close/cache behavior.

**Blocked by:** R9. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Emission owns selection/registration; streams/retransmission queue alone own logical data; path recovery owns each transmission. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Private primary-only driver over two validated paths remains; R15 supplies policy and R19 public enablement. Any temporary path-zero-only packer adapter ends here.

**Blast radius and context:** packet_emission.go, packet_packer.go, framer.go, retransmission_queue.go, send_stream.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: two-path useful STREAM transfer; original ACK after cross-path retransmission; partial packing preserves work; incompatible GSO contexts cannot batch. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Identity and protocol ownership; Payload policy (reliable portion); R9 terminal-transition interface receipt. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Reliable logical delivery and path-qualified transmission form one complete owner behavior. Keep DATAGRAM/PMTU separate because admission/disposal differ. R9 supplies complete validation and terminal transitions; an independent oracle is not consumed until R14, so R1 is not a blocker here. Fixtures cannot exclude required local teardown. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Smoke requires pretending peer behavior is supported or changing application delivery ownership.

<a id="r11"></a>

### R11 — Schedule DATAGRAMs with path-qualified PMTU

**Delivers:** Add per-path PMTU state and immutable observations, common eligible payload admission and non-destructive queue selection. Keep DATAGRAM single-shot; drop with bounded observation when no eligible path can carry an admitted message. Preserve PathMTUSettledV1 meaning.

**Blocked by:** R10. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** DATAGRAM queue owns messages; PMTU owner owns per-path limits; emission selects before pop. Connection owns the common eligible-payload observation used by public SendDatagram; queue capacity does not substitute for this size check. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** None beyond private activation; no public per-message steering.

**Blast radius and context:** datagram_queue.go, packet_packer.go, mtu_discoverer.go, path_mtu_settled.go, packet_emission.go; connection.go:3040–3057 Conn.SendDatagram and :1028–1041 resetPathPacketSize; both packet_packer.go:483–509 and :692–705 consumers. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: smaller-MTU survivor; fresh packet versus ACK/PATH_ACK pressure; another eligible path fits; eligibility loss drops once; no retransmission on uncertain send; unchanged legacy PMTU observation; public SendDatagram limit and DatagramTooLargeError agree with eligible path PMTUs. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Payload policy; Retention and bounded deferred work (DATAGRAM); immutable snapshot PMTU contract. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Queue admission and packing consumers must change together. R10 supplies path selection; retirement need not block this path-local behavior. No merge with failure policy. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Representation, ownership, public boundary or one-PR context changes; any untraced effect or need to exceed the declared evidence budget.

<a id="r12"></a>

### R12 — Replenish retired path credit and reconcile late delivery

**Delivers:** Extend the existing R8/R9 terminal owner with established-path replacement/replenishment and retained late-delivery integration. Local/remote abandonment and failed initiation already clean up completely. Admit a replacement only after the old submission fence and retained-state reservation; late ACK/original logical delivery stay exact-once. Path-zero retirement updates the stable connection RTT observation from R7. Share retained-history capacity with old migration generations and release charges only after retention plus submission/callback fences.

**Blocked by:** R10. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection registry owns lifecycle and replacement credit; endpoint token owns native cutoff; retained recovery owner disposes each flight once. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** The initial-window-only restriction from R8/R9 ends here. Mandatory terminal cleanup already has one owner; no second retirement state machine is introduced. Old-tuple migration is R13 responsibility.

**Blast radius and context:** connection.go, path registry, conn_id_generator.go, conn_id_manager.go, internal/ackhandler/sent_packet_handler.go, packet_emission.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: replacement after the inherited queued/claimed/partial-retry cutoff; remote abandon during replenishment; old ACK after replacement; retained cap blocks opening; missing reciprocal escalation; last-path handling; existing flow controllers observe surviving-primary RTT after zero retirement; retired-path and migrated-generation history compete for the same cap. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Resource admission and retention; Submission cutoffs and connection closure; Protection decision (RTT observation); R9 terminal owner receipt. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Fenced retention and replenishment are one resource transition; splitting their authority would admit unsafe replacements. R10 supplies real STREAM transmission/logical-delivery integration while R8/R9 already supply complete teardown. Keep same-ID migration separate because it preserves PN identity and has a different commit point. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Retirement frees current borrow, reuses wire IDs or requires a third live path.

<a id="r13"></a>

### R13 — Migrate one path and observe safe transport detachment

**Delivers:** Validate a target native transport with matching CID length, fence the old tuple and commit same-wire-ID cutover without resetting PN/ECN continuity. Retain old routing for the selected interval; publish detachment only after routing/captured callbacks/submissions can no longer destroy or send for this connection. Reserve retained-generation and distinct-attachment capacity before migration; reused transports still consume generation history.

**Blocked by:** R12. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection registry owns tuple commit/detachment; transport map owns captured-handler fencing; endpoint owns per-transport write grants. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Old/new tuple coexistence is bounded to one candidate and named retained generation; remove at routing expiry plus fence. No duplicate active protocol path or implicit transport close.

**Blast radius and context:** path_manager_outgoing.go, path registry, transport.go, conn_id_generator.go, connection.go, internal/ackhandler/sent_packet_handler.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: negotiated-zero and exhausted credit; mismatched CID length; cancel before/after commit; incoming rebinding contention; Close versus detach callback; shared transport still used by other path; late old-generation ACK; fifth serial migration refused with four retained generations; cancellation and delayed detachment release. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Target transport and detachment; Resource admission and retention; Retention and bounded deferred work; public migration/handle contracts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Cutover and detachment are one authority-transfer contract; splitting observation would make success unsafe. R12 supplies generation fence/retention owners. Do not merge generic close-cache work. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** CID-length decoder must be overridden, old routing cannot be fenced, or cutover cancellation falsely claims rollback.

<a id="r14"></a>

### R14 — Close via surviving paths and publish path-qualified diagnostics

**Delivers:** Route live/cached close through each retained valid path identity, including path-zero loss, and apply bounded close/drain snapshots. Add qlog frame/path/generation identity and bounded lifecycle observations at owners without blocking native progress. Once this complete close/cache contract is in place, run the first independent two-role useful STREAM smoke via R1. The fixed scenario has normal, abort and close cleanup; it does not claim the full later interoperability matrix.

**Blocked by:** R1, R12. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection close owner selects surviving contexts; closed handler owns cache references; emission/endpoint retain send lifetime; diagnostics consume copied facts. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Remove single path-zero close assumption for negotiated paths; ordinary cache behavior stays. No new sink joins the grant lifetime.

**Blast radius and context:** connection.go, closed_conn.go, conn_id_generator.go, qlog/frame.go, qlog/event.go, logging/frame.go; R1 peer driver smoke adapter. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: path-zero abandoned alternate close; cache trigger after path retirement; close reference while current; retained timer cannot refresh; distinct path IDs in qlog; blocked sink drops metadata; two-role independent STREAM smoke charged to the shared ledger. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Submission cutoffs and connection closure; Retention and bounded deferred work; Program qualification ledger and sequencing; endpoint cached-close/result receipt. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Close routing and its observations expose one small lifecycle boundary. Split diagnostics if schema compatibility needs a separate product decision; no public schema claim from private fields. R12 supplies terminal path identities. R1 is required for the independent smoke oracle; moving smoke before close/cache authority is rejected. R17 still owns full matrix qualification. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Representation, ownership, public boundary or one-PR context changes; any untraced effect or need to exceed the declared evidence budget.

<a id="r15"></a>

### R15 — Apply sticky primary and bounded backup promotion policy

**Delivers:** Implement the selected active/backup preference and qualified PTO-attempt detector. Local backpressure never promotes or marks failed; suspect paths keep bounded probes. Revalidate stale backups, preserve all-backup progress and keep recovered paths as backups. Apply the selected native destination-error table, atomic designated-primary succession and recovered-path local-use demotion; preserve local timeout as backpressure.

**Blocked by:** R10. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Connection loop owns policy, qualified PTO counter and primary; scheduler reads immutable eligibility only. Primary transitions update the stable connection RTT observation supplied by R7, never a replacement pointer that strands existing flow controllers. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Replace private primary-only choice with accepted primary-backup policy; no extra keepalive or overflow scheduler.

**Blast radius and context:** connection.go, path registry, packet_emission.go; private scheduler. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: three known-accepted PTO attempts; uncertain/local timeout does not count and preserves primary flag/RTT observation; cross-path ACK establishes target liveness; old tuple ACK does not; stale backup validation; all backup; recovery does not oscillate; destination error with/without survivor; stale destination result; recovery demotion then explicit active; all-backup primary flag and stable RTT observation. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Native error classification and primary transition table; Primary, backup and failure policy; Protection decision (RTT observation); Payload policy (scheduler interface). Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Counter and selection consumers must be one policy slice; do not overload recovery ptoCount. R10 supplies real acceptance/ACK facts. No need to block on DATAGRAM or migration implementation; their identities satisfy the same input contract. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Representation, ownership, public boundary or one-PR context changes; any untraced effect or need to exceed the declared evidence budget.

<a id="r16"></a>

### R16 — Complete additive APIs and constructor gates behind disablement

**Delivers:** Implement V1 methods, immutable snapshots/error identities, legacy handle-mode checks and the full explicit/Addr/HTTP3 constructor matrix. Public requests return unsupported until R19 certification enables the complete Linux profile. Private fixtures exercise the same constructors with one internal activation guard. Enforce the participant-cap refusal mapping for ordinary and multipath setup, including bounded server CONNECTION_REFUSED or drop.

**Blocked by:** E6, R11, R13, R14, R15. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Config/transport owns pre-init grants and compatibility; connection owns commands/snapshots; helpers own only sockets they create. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** One private activation guard remains, removed in R19 after evidence. No second implementation of public constructors in fixtures.

**Blast radius and context:** config.go, interface.go, transport.go, client.go, server.go, http3/transport.go, http3/server.go, path_manager_outgoing.go. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 20 deterministic scenario tests under the binding rule. Start with: explicit grant and Addr-owned lifecycle; caller-socket refusal; server-selected config; default HTTP3 versus caller transport; legacy modes; snapshot copies/coalescing; errors.Is categories; unsupported v2/restricted/QoS combinations; cap-plus-one setup; exhausted refusal-response class; cause-only backpressure match sets. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Public surface and compatibility; Path handle modes; compatibility matrix; endpoint Shutdown, grant return and entry points / Exact grant surface and errors. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Treat this as a mechanical constructor migration after behavior exists, not a new state machine. Split helper migration into two independently green batches if eight primary files cannot fit 24k tokens. Prerequisites are complete operations/native evidence; R17/R18 require these exact constructors. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Public enabled config can advertise early or one constructor must bypass the common capability guard.

<a id="r17"></a>

### R17 — Complete the independent interoperability matrix

**Delivers:** Run and wire the eight accepted scenarios in both endpoint roles into the pinned required Linux peer job. Include expected-case accounting, negotiated-zero normative vectors and explicit oracle limits; preserve early smoke evidence without claiming it replaces this matrix.

**Blocked by:** R16. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Transport integration suite owns maintained job, scenario driver and receipts. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Remove fixture exclusions for the supported Reno profile; retain no second protocol implementation.

**Blast radius and context:** interop fixture, integrationtests/self, dedicated interop workflow; evidence receipt. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Example-level evidence over the fixed scenario/toolchain domain, parsed by the pinned peer/fixture's existing protocol implementation. This is the explicitly maintained verification deliverable described above; receipts are metadata. Runtime closure is not recursively imposed on the fixture. Missing/incorrect case accounting is a failed claim, not evidence of product failure.

**TDD / evidence budget:** 16 scenario/role run instances; eight D0 scenarios with skips treated as missing evidence; controlled normative vectors for negotiated zero. Build focused driver/accounting regressions first; operational attempts charge the single program ledger. Stop after the fixed cases or first unresolved prerequisite/budget failure; no repeat-until-pass.

**Context imports:** Program qualification ledger and sequencing; Reference adaptation (interop row); accepted constructor and lifecycle receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** The fixed matrix is one evidence deliverable, not another feature implementation. R16 ensures public entrypoints are the tested path; R1 is the independent oracle. Split only for a newly accepted semantic venue, not arbitrary files. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Independent peer cannot exercise an expected case, prerequisites missing or fixed matrix requires expansion to make a claim.

<a id="r18"></a>

### R18 — Measure ordinary-peer costs on granted endpoints

**Delivers:** Run six accepted ordinary workload/acceleration cells against matched unregistered controls, including mixed multipath and stateless pressure. Record useful goodput, CPU/useful byte, p99 control/send completion, memory, timeout/drop rates and fair progress; publish pass/fail against owner thresholds.

**Blocked by:** R16. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Transport integration suite owns paired workload, resource ledger and qualification receipt; endpoint policy is unchanged. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** None; no performance-specific production path or widened limits.

**Blast radius and context:** bounded performance driver and receipt; existing benchmark helpers only. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Example-level evidence over the fixed scenario/toolchain domain, parsed by the pinned peer/fixture's existing protocol implementation. This is the explicitly maintained verification deliverable described above; receipts are metadata. Runtime closure is not recursively imposed on the fixture. Missing/incorrect case accounting is a failed claim, not evidence of product failure.

**TDD / evidence budget:** 36 run instances: six cells, two profiles, three paired observations; 60 seconds after 10-second warmup; paired median/range. Build focused driver/accounting regressions first; operational attempts charge the single program ledger. Stop after the fixed cases or first unresolved prerequisite/budget failure; no repeat-until-pass.

**Context imports:** Accepted product thresholds; Program qualification ledger and sequencing; endpoint limits and exact tested constructors. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** One matched comparison contract fits one evidence PR; do not split metrics into independently tuned workloads. R16 provides exact constructors, native qualification and both traffic types. No dependence on R17 results. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Matched baseline cannot be represented, ordinary tolerance fails, or evidence budget would be exceeded.

<a id="r19"></a>

### R19 — Enable the qualified experimental Reno profile

**Delivers:** Remove private activation for the exact supported Linux native v1 Reno profile, publish opt-in usage/support boundaries, and delete temporary fixture selectors/unqualified context access. Record evidence reachability and default-off preservation. Keep BBR+multipath and aggregate rejected until the B track.

**Blocked by:** R17, R18. **Existing work:** new slice; no unmerged implementation accepted.

**Single owner and authority completeness:** Common configuration capability gate alone authorizes advertisement/public operations; all runtime owners remain as qualified. Cover constructors, validation and the destruction/cancellation consumers named by this behavior together; no persisted state is introduced.

**Temporary seams:** Contract step removes every temporary Reno activation seam; permanent ordinary/unregistered route stays by design.

**Blast radius and context:** config.go, transport.go, API docs, integration fixture activation; source audit of callers. Receive only this slice, binding discipline, its referenced owner/closure sections, direct blocker interface receipts and the bounded governing diff; omit complete historical plans. Target at most 24k input tokens, leaving the fresh context room for implementation, review fixes and verification. If the named source cannot fit after focused symbol reads, re-slice before editing. The touched owners and their tests bound shared state, concurrency, API, failure, performance/security and dependency effects; any newly discovered effect outside them stops the slice.

**Representation, artifacts and closure:** Universal runtime invariants within the binding profile and this slice's typed event/configuration/wire domain; the named owner validates the representation. Runtime is shipped behavior, ownership guards required safety enforcement, named tests maintained regression aids, receipts process metadata. Apply only this slice's rows of the track closure table; no Cartesian matrix or parser-alias census.

**TDD / evidence budget:** At most 16 deterministic scenario tests under the binding rule. Start with: public two-path smoke via both roles; default-disabled upstream-compatible build; v2/unsupported combinations refuse or ordinary fallback as specified; no private bypass remains. One focused race run where these owners cross execution contexts; preserve ordinary/default-off behavior on the changed seam.

**Context imports:** Public surface and compatibility; Accepted product thresholds; Program qualification ledger and sequencing; direct blocker evidence receipts. Include the binding discipline and only the matching closure rows, plus this current slice and direct blocker interfaces; do not load every slice contract.

**Slice decision audit:** Enablement is separate from evidence so a failed campaign cannot accidentally ship support. Both R17 and R18 are necessary; their transitive blockers supply native/lifecycle gates. No merge with BBR. Rejected split/merge alternatives have no revisit trigger except the stated context/contract stop. Direct blockers supply the interfaces or evidence named here; transitive blockers need not be repeated and convenience ordering is excluded.

**Stop:** Any named evidence gate missing, changed code invalidates its candidate, or enabling requires a new capability.

## Acceptance and validation

The track is complete only when every slice's narrow end-to-end outcome, owner invariant, removal seam and named evidence are satisfied, with no open stop-for-decision. Universal statements range only over the binding runtime domain and the semantic classes owned by each slice; source inspection must map every supported producer/consumer to that owner. No unsupported platform/peer/provider claim follows from a passing fixture. Finite tests terminate as specified; missing evidence stays missing.

Default-disabled ordinary traffic, opted-in ordinary traffic and negotiated multipath are separate populations. Preserve unchanged default behavior with characterization/regression coverage; measure the changed opted-in ordinary cost against its explicit tolerances. Hosted repository workflows remain authoritative on the exact live PR head. A receipt for an earlier candidate or a mirror's plan SHA is not code-head certification.

A newly discovered untraced shared-state, concurrency, public API, failure, performance, security or dependency effect is a slice stop until scoped re-audit establishes that it fits the existing outcome and one-PR budget. Do not silently merge adjacent work. Public support and adoption claims require their distinct named gates; the owner permits exploratory implementation before consumer benefit, not unsupported claims.
