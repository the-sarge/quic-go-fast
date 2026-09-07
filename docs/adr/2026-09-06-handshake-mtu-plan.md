# Handshake MTU recovery implementation plan

**Date:** 2026-09-06. **Status:** Complete in [PR #20](https://github.com/the-sarge/quic-go-fast/pull/20). **Track:** H of QGF-2026-09. **Depends on:** Nothing. **Related:** [Program](2026-09-06-fork-program.md), [compatibility](0001-upstream-compatibility.md), [upstream #5815](https://github.com/quic-go/quic-go/issues/5815). **Audit history:** [Handoff audit](../audits/2026-09-06-handoff.md).

## Goal and current shape

A connection that observes a local message-size error for an oversized pre-confirmation handshake send reduces subsequent packetization to a 1200-byte UDP payload and can complete the handshake on a path accepting that size. Preserve the existing asynchronous send queue and the connection loop as the protocol-state owner.

Anchors refer to v0.62.0. `send_queue.go:77` owns asynchronous writes and suppresses `isSendMsgSizeErr`; `connection.go:587` runs it independently. `connection.go:648` can bypass its blocking select, so notification handling cannot rely solely on one select case. `connection.go:2792` registers coalesced sends before queueing at `:2840`. `connection.go:2865` uses configured packet size until an MTU finder exists; `:2443` constructs that finder during transport-parameter processing, while `:979` confirms the handshake and conditionally starts probing. `connection.go:2133` updates the application size estimate and congestion packet size on growth. `internal/congestion/cubic_sender.go:320` panics on a packet-size decrease. `connection.go:912` and `:1290` change paths by replacing the queue or changing its destination. `sys_conn_df_linux.go`, `sys_conn_df_darwin.go`, and `sys_conn_df_windows.go` own native error classification.

## Decision

Introduce private typed send metadata and bounded error feedback, without moving protocol decisions into the send goroutine. Metadata identifies a pre-confirmation handshake-flight send and the current connection-owned path generation. The sender reports only classified message-size errors for such non-GSO sends whose UDP payload exceeds 1200 bytes. A small connection-owned mailbox retains the latest eligible failure plus a coalesced wakeup; the sender must not block if another notification is pending and must not retain the packet buffer. Replacing the latest event avoids a stale-path notification occupying the only slot and discarding the next valid failure. Use Go synchronization primitives, not unsynchronized cross-goroutine flags.

The connection loop consumes feedback before packing another flight, including iterations that skip waiting. When the connection is still unconfirmed and the event belongs to its current path generation, one private connection method applies the fallback. Increment the generation at both existing path-change seams, and reject stale generations; an already-confirmed connection ignores this handshake-only feedback. Capture send classification and generation at enqueue time, not by reading mutable connection state in the queue worker. Normal packet sends, PMTU probes, path probes, and CONNECTION_CLOSE keep their existing error behavior.

The connection owns an effective initial packet-size value initialized from the existing config in both constructors. Before finder creation, fallback lowers that effective value to 1200. Transport-parameter processing uses this effective value as the finder's start size, retaining the existing peer/local upper-bound calculation. Without fallback, preserve the baseline start-size behavior; do not add a new peer-limit clamp. The fallback start of 1200 is compatible with the existing validator, which rejects `max_udp_payload_size` below 1200 (`internal/wire/transport_parameters.go:323`). If the finder already exists but confirmation has not occurred, replace it with a fresh, unstarted finder at 1200 and the existing peer/local upper limit. There are no pre-confirmation MTU probes to preserve. Do not use `Reset` here: it sets `lastProbeTime`, which could enable probing when the caller disabled discovery. The unchanged confirmation path remains the owner of starting discovery. Preserve the smaller start after confirmation; when discovery is disabled it stays smaller, and when enabled probes may grow it normally.

Update `maxPayloadSizeEstimate` downward at fallback so public datagram admission uses a conservative budget. Keep congestion-controller packet-size accounting monotone: do not call `SetMaxDatagramSize(1200)` after construction at a larger size. On later estimate growth, pass at least the configured initial size to that setter until actual discovered size exceeds it. The configured initial size is the existing congestion accounting floor during this pre-confirmation-only reduction; this is not a new congestion algorithm and does not reset bytes in flight or loss state. If inspection proves another path can violate that monotonic relation, stop for an owner-level design correction instead of removing the panic.

Already queued oversized packets may still fail; the queue stays bounded and ordinary loss/PTO mechanisms retransmit reliable handshake frames after the packing budget has changed. Do not truncate serialized encrypted bytes, replay registered packets with reused packet numbers/nonces, claim failed writes were acknowledged, manually refund bytes in flight, or inject uncontrolled retries. This slice does not promise immediate retransmission or a new deadline. An error at 1200 or below retains existing timeout/error behavior; below-minimum path support is not claimed.

**Rejected alternatives (do not do this):** Lower the global default; mutate shared `Config`; disable OOB/GSO globally; infer MTU from a generic timeout or interface name; add a blocking feedback channel; make handshake writes synchronous on the connection loop; teach the send queue to edit MTU/congestion state; lower the congestion setter directly; start discovery through a pre-confirmation reset; or add a public fallback switch.

**Non-goals:** Silent black-hole recovery, post-confirmation MTU shrink, supporting sub-1200 UDP paths, new error APIs, immediate local-loss retransmission, changes to anti-amplification or encryption/key lifetime, configurable jumbo packets, new congestion control, application repair, or operating-system/network configuration changes.

## Slice graph

| Slice | Disposition | Delivers | Blocked by | Temporary seam |
| --- | --- | --- | --- | --- |
| H1 | complete | Classified send feedback through connection-owned fallback and a completing handshake | None | None |

## Implementation slices

### H1 — Recover oversized handshake flights

**What it delivers:** The complete private metadata/mailbox/connection transition, updated packet-size initialization and admission estimate, preserved congestion-size monotonicity, and an end-to-end regression using a UDP connection wrapper that rejects oversized sends with the platform's native size error while allowing 1200-byte sends.

**Existing-work disposition:** New slice. Upstream issue #5815 is a reproduction proposal, not an implementation baseline. No open fork PR or partial local implementation exists. Preserve upstream failure accounting and key transitions; do not treat the proposal's simplified finder-lifetime explanation as source truth.

**Single owner after merge:** The `Conn` run-loop fallback method owns effective packet-size transitions; the existing finder owns post-confirmation probing; `sendQueue` owns packet buffers and only publishes immutable failure metadata. The congestion sender retains its own accounting invariant. These are separate facts, not independently writable replicas of one MTU value.

**Authority completeness:** No persisted fact or new authentication authority. Both Conn constructors, finder initialization, confirmation, application admission, native queue classifications, queue replacement, and in-place path changes are included. Existing QUIC wire parsing and native error classifiers remain authoritative.

**Transitional-seam budget:** None. The effective initial size is a constructor/handshake input, superseded as the current size source by the finder; it is not a second post-handshake PMTU state machine. The feedback mailbox is bounded event delivery, not a second MTU owner. Generated mock updates follow the private sender interface rather than introducing an adapter with its own decisions.

**Blast radius:** Cross-goroutine error delivery, queue shutdown/drain, path generations, pre/post-confirmation ordering, coalesced packet sizing, datagram admission estimates, MTU probe start, and congestion-size updates. Old queued failures remain normal losses. Packet buffer release remains exactly once on the existing message-size-error path. Non-size-error cleanup is preserved rather than opportunistically redesigned. No parser, public API, dependency, config default, cryptographic construction or network topology changes. Any additional state-mutating caller found during implementation must be classified under the bounded invariant before editing.

**Artifact classification:** Private feedback/fallback is shipped behavior; phase/path guards enforce the accepted lifecycle boundary. Tests and UDP wrappers are verification aids, not a new maintained benchmark service. The diagnosis/result receipt is process metadata. No independent harness deliverable blocks the runtime patch.

**Representation contract:** Canonical internal domain: error values accepted by the existing platform `isSendMsgSizeErr`, immutable enqueue metadata from the actual coalesced-flight caller, both endpoint roles, current/stale path generations, and configured/peer packet limits satisfying existing validation. QUIC parsers own external packets; no new grammar is scanned. The state transition is an internal invariant with finite semantic regressions; real-path performance and universal network recovery are not claimed.

**Contract closure:** Triggered by lifecycle/concurrency corruption risk across independently reachable send, initialization, confirmation and path-change states. Precise invariant: only current-path oversized handshake-send failures observed while unconfirmed may lower effective packetization, without changing key/loss ownership or starting forbidden discovery. Enforcement owner: the one Conn fallback method plus its private feedback delivery boundary. Actor model: ordinary concurrent application callers and arbitrary valid peer timing; local OS errors are classified by existing code, not assumed authenticated peer signals.

| Semantic class | Required disposition | Owner/evidence | Planning status |
| --- | --- | --- | --- |
| Client oversized Initial before transport parameters | Lower effective start to 1200; later finder inherits it | Conn fallback; completing-handshake wrapper case | Covered by H1 focused/integration regressions |
| Server oversized flight after parameters, before confirmation | Fresh unstarted finder at 1200 | Conn fallback; server wrapper case | Covered by H1 focused/integration regressions |
| Discovery disabled after fallback | Complete and remain at 1200; no probe start | Confirmation/finder initialization; focused case | Covered by H1 focused/integration regressions |
| Discovery enabled after fallback | Confirmation enables normal upward probes within peer/local limit | Existing probe owner; focused case | Covered by H1 focused/integration regressions |
| Pending feedback during busy loop, duplicates or queue drain | Observe before further packing; bounded/idempotent; shutdown joins | Mailbox/run loop; bounded race case | Covered by H1 focused/integration regressions |
| Old-path event or event observed after confirmation | Ignore; no size reset | Conn phase/generation guards; focused cases | Covered by H1 focused/integration regressions |
| Non-size error, unmarked send, GSO/PMTU/path probe, or payload <=1200 | Preserve existing disposition; no handshake fallback | Queue eligibility boundary; table case | Covered by H1 focused/integration regressions |
| Application admission and congestion growth after reduction | Estimate decreases; later growth cannot call the monotone setter with a smaller size | Conn fallback/ACK update; focused case | Covered by H1 focused/integration regressions |
| Silent drops, post-confirmation shrink, subminimum paths | Explicit non-goals; do not claim recovery | Existing handling retained | Non-goal |

**TDD and terminating evidence budget:** Write a failing local-UDP handshake test first with injected native message-size errors. Use two end-to-end cells: rejected client Initial and rejected server flight; both must establish a real connection and exchange a short stream payload once patched. Use at most eight focused logical cases matching the matrix, table-driven where behavior is shared, including queue closure without a goroutine leak and the lower-then-grow congestion regression. Existing transport-parameter, handshake, loss and MTU tests provide preservation. One source guard-bypass experiment may verify the phase/generation guard if inherited coverage is the only exercise; no mandatory mutation, syntax fuzz expansion, or cross-product matrix. A no-error control must show configured initial sizing remains unchanged. No benchmark or physical Tailscale run is required to establish the native-error transition; a later physical run is corroboration owned by the sweep.

**Platforms:** Native error wrappers differ on Unix and Windows. Use the platform constants behind test build tags, including the existing Windows WSA error, and run the two integration cells under inherited Linux/macOS/Windows CI. The connection transition and queue-race tests run locally once with `-race`. Platforms outside those existing native classifier implementations retain their existing no-recovery behavior. No OS configuration or additional infrastructure is needed.

**Dispatch context budget:** Current H1 contract and policy; Conn constructors plus named run/finder/ACK/path/packet-send methods; `send_queue.go`, `send_conn.go`, `mtu_discoverer.go`, the congestion setter, native classifiers, and relevant test helpers. Limit initial source excerpts to 2500 lines and the intended diff to roughly 500 non-generated lines including focused tests; mocks may be regenerated. One fresh context must fit implementation, local review fixes and verification. Exceeding that footprint due to another protocol mechanism requires re-audit, not a larger checklist.

**Slice decision audit:** Splitting feedback plumbing from its sole recovery consumer would leave an unused event seam and no independently useful shipped behavior; splitting client and server would leave shared initialization/confirmation invariants only partially covered. One narrow end-to-end slice is coherent and independent of receive efficiency. No genuine blocking edge to D1/D2 or external physical qualification exists.

**Approach stops:** The wrapper cannot reproduce the accepted error loop on the baseline; correctness requires changing key discard, bytes-in-flight accounting, retransmission architecture, congestion semantics beyond preserving the monotone floor, post-confirmation recovery, a new public API, or a second state owner; the feedback invariant cannot fit the declared context/evidence budget. In those cases preserve evidence and request a scoped re-audit; do not silently substitute a different MTU problem. A repeated precise invariant/owner finding follows the shared stop policy.

## Acceptance and validation

- [x] The two injected-error handshake regressions fail on the baseline and complete with stream exchange on the candidate.
- [x] The matrix's in-contract classes have dispositions and the finite focused evidence passes; no universal path or deadline guarantee is claimed.
- [x] Healthy start sizing, the existing peer/local discovery upper-bound calculation, disabled discovery, congestion monotonicity, queue shutdown, and ordinary PMTU probe handling retain their specified behavior.
- [x] The receipt identifies the reproduction, exact base/head, generated mock changes, local commands and same-head hosted outcomes.

Use names beginning `TestHandshakeMTUFallback` for the new connection/integration regressions and `TestSendQueueHandshakeMTU` for queue feedback tests. Focused local commands: `go test -count=1 -run 'TestHandshakeMTUFallback|TestSendQueueHandshakeMTU|TestConnectionHandshake|TestSendQueue|TestMTU' .`; `go test -race -count=1 -run 'TestHandshakeMTUFallback|TestSendQueueHandshakeMTU|TestSendQueue' .`; `go test -count=1 -run 'TestHandshakeMTUFallback|TestPathMTUDiscovery' ./integrationtests/self`. Run `go test -count=1 ./...` once at the final reviewed code head. Inherited same-head Linux/macOS/Windows integration and unit checks supply the justified native classifier coverage; absence is a reported missing gate, not a local substitute. No repeated complete runs absent a changed candidate or diagnosed external failure.

## Operating discipline

The shared REVIEW-LOOP.md and CONTRACT-CLOSURE.md supplied by `$implement-architecture-slice` govern, composed with the [program](2026-09-06-fork-program.md); no repository-specific overlay exists. One initial review plus at most one replacement, independent dispositions, exact-head certification/hosted checks, matched-head squash merge, post-merge journal and pointer-only tracking. The contract and its evidence budget are both floor and ceiling.
