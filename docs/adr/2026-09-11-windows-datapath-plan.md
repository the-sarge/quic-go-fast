# Windows Datapath (Foundation, USO, URO) Implementation Plan

**Date:** 2026-09-11
**Status:** Accepted; W1 complete (#242); W2 and W3 not yet implemented
**Track:** W, 2 of 3 in the 2026-09-11 datapath offload program
**Depends on:** W3 requires G1 (coalesced-storage contract); W1 and W2 have no cross-track dependency
**Related:** [Datapath offload plan](2026-09-11-datapath-offload-plan.md); ADRs [0001](0001-upstream-compatibility.md), [0003](0003-follow-stable-upstream-releases.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers, and stop conditions
**Audit history:** [Handoff receipt](../audits/2026-09-11-datapath-handoff/README.md); [stack comparison](../audits/2026-09-11-quic-stack-datapath-comparison.md)

## Goal

Windows moves from the featureless plain-socket path onto a message-I/O datapath, then gains segmented send (USO) and coalesced receive (URO) through documented socket options, reusing the platform-neutral segmented-send logic and the Track G split seam.

## Current Shape (verified 2026-09-11)

Windows uses `basicConn` with no OOB, ECN, or batching (`sys_conn_windows.go:12-14`). The standard library's `net.UDPConn.ReadMsgUDP`/`WriteMsgUDP` execute `WSARecvMsg`/`WSASendMsg` through the runtime IOCP poller (verified in Go's `internal/poll/fd_windows.go` during consideration). `golang.org/x/sys@v0.47.0` ships `WSASendMsg`, `WSARecvMsg`, `UDP_SEND_MSG_SIZE`, `UDP_RECV_MAX_COALESCED_SIZE`, and `UDP_COALESCED_INFO`. The platform-neutral segmented-send logic already keys off the per-connection GSO capability flag (`sys_conn_oob.go:131`, `send_queue.go:135-147`). CI runs a Windows matrix (`.github/workflows/unit.yml`, `integration.yml`).

## Decision

Three slices with an explicit dependency and revert graph, per the [datapath offload plan](2026-09-11-datapath-offload-plan.md): a behavior-preserving foundation on standard-library message I/O, then USO, then URO. That plan is binding for probes, kill switches, the Server 2022 ancillary policy, and adoption gates; this document adds only the slice decomposition.

**Rejected alternative (do not do this):** A direct `x/sys/windows` overlapped or blocking datapath — it blocks OS threads or duplicates inaccessible runtime IOCP machinery and breaks deadline/close semantics. Version sniffing instead of runtime probes. Enabling TTL/DSCP ancillary receive features while USO/URO are active without functional qualification.

**Non-goals:** Kernel-mode or XDP datapaths; ECN as a gate for W1 (it lands only if the control-message work makes it free and correct); any public API change.

## Slice Graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
|---|---|---|---|---|
| W1 | Complete (#242) | Behavior-preserving Windows message-I/O foundation | None | Removes the Windows `basicConn` capability gap seam |
| W2 | new | Windows segmented send (USO) with adoption evidence | W1 (complete) | n/a |
| W3 | new | Windows coalesced receive (URO) with adoption evidence | W1 (complete), G1 (complete) | Closes G1's inert seam for its second producer |

## Implementation Slices

### Slice W1 — Windows message-I/O foundation

**What it delivers:** A Windows connection implementing the existing raw-connection interface over `net.UDPConn.ReadMsgUDP`/`WriteMsgUDP`, with Windows control-message encoding and parsing (IPv4/IPv6 packet-info parity with the OOB path), replacing `basicConn` on Windows with behavior otherwise preserved. Its gate is correctness and noninferiority: deadline, idle-close, and concurrent-close tests plus no throughput regression — not syscall reduction, which a behavior-preserving rebuild cannot show.

**Existing-work disposition:** New slice. No open PR or branch exists for this track.

**Blocked by:** None.

**Single owner after merge:** The Windows conn owns Windows socket reads/writes and control-message parsing for transport-created and OOB-capable sockets; Windows `newConn` (`sys_conn_windows.go:12-14`) no longer returns `basicConn`. `basicConn` itself is platform-neutral (`sys_conn.go:104-115`) and remains the fallback for caller-supplied non-OOB-capable PacketConns on every platform — that path is preserved, not removed.

**Authority completeness:** Packet-info-derived addressing becomes authoritative on Windows exactly as it is on OOB platforms; construction (parse), validation (fallback when absent), and consumers (existing address handling) are in this slice. No restart persistence; no destructive consumer.

**Transitional-seam budget:** None retained. The slice removes the Windows capability-gap seam rather than widening one.

**Blast radius:** Windows-only build-tagged files; non-Windows platforms untouched. Failure modes: deadline/close semantics are the risk surface and are the named gate. Untraced effects: none identified; the change is confined to `sys_conn_windows*.go` plus tests.

**Artifact classification:** Conn and control-message code: shipped behavior. Deadline/close/noninferiority tests: verification aid within normal CI. Protocol/results pair: process metadata per the program's adoption rule (noninferiority cells only).

**Representation contract:** Windows control messages (`WSAMSG` control buffer) owned by the encoder/parser this slice adds; guarantee universal over the messages this conn itself requests; terminating evidence is parse round-trip tests plus the correctness gate.

**Contract closure:** Triggered — close/deadline misbehavior is material (hangs, leaked OS threads) with independently reachable paths. Invariant: every blocked read observes deadline expiry, connection close, and concurrent socket close without hanging or panicking, through the runtime poller. Owner: the Windows conn's use of `net.UDPConn`. Classes: blocked read + deadline; blocked read + `Transport.Close`; concurrent close during read; sustained transfer (poller integration); write after close. Each has a Windows-CI test; dispositions covered.

**Evidence budget:** The closure tests; the protocol cells on the qualified Windows host with predeclared bounds. The noninferiority gate binds the preserved-behavior configuration, where both datapaths do identical work; the packet-info-active configuration — new behavior this slice is required to deliver, with the same per-packet control-message cost model the OOB platforms already pay — is measured and reported against a predeclared stop floor rather than the noninferiority bound (re-audited 2026-09-11 after the first collection; chronology in #242). No other platform scope.

**TDD and preservation evidence:** Closure tests written first (they pass against `basicConn` too, characterizing preserved behavior, then must keep passing). Preservation: full suite on Windows CI; behavior-identical gate is the protocol's noninferiority cell; the caller-supplied non-OOB `basicConn` fallback path keeps its existing coverage.

**Dispatch context budget:** This slice contract, the Windows rules in the datapath offload plan, `sys_conn_windows.go` (~40 lines), `sys_conn.go` interface (~60 lines), and the OOB packet-info parsing as reference. Fits one fresh context.

**Slice decision audit:** Strongest split: control-message parsing separate from the conn swap — rejected, parsing without a consumer is a banned horizontal slice and the swap without parsing delivers nothing. Strongest merge: with W2 — rejected, the foundation's behavior-preserving gate and USO's performance gate are different acceptance regimes and merging doubles the context. Blocking edges: none claimed.

**Stop conditions:** `ReadMsgUDP`/`WriteMsgUDP` proves unable to carry a control message this path requires (would force the rejected direct-Winsock alternative — operator decision); deadline/close closure classes cannot pass through the standard library.

### Slice W2 — Windows segmented send (USO)

**What it delivers:** A runtime probe for `UDP_SEND_MSG_SIZE` on transport-owned sockets, the per-send segment-size control message, wiring the probe into the existing platform-neutral segmented-send capability flag so the send path batches exactly as Linux GSO does, Windows-specific send-error classification preserving MTU-discovery feedback, governance by the existing `QUIC_GO_DISABLE_GSO` switch, and the adoption protocol/results pair (engagement metric: packets per send submission).

**Existing-work disposition:** New slice.

**Blocked by:** W1.

**Single owner after merge:** The Windows conn owns segmented-send encoding; the existing platform-neutral send logic owns batching policy. No second policy owner.

**Authority completeness:** The Windows GSO capability flag: probe constructor, kill-switch and probe-failure validation, per-connection lifetime, consumers (send path, MTU error handling) — all in this slice.

**Transitional-seam budget:** None.

**Blast radius:** Send path shared logic is exercised with a new platform flag — guarded so non-Windows is untouched; MTU discovery error mapping is Windows-specific and tested. The Server 2022 ancillary policy applies once both W2 and W3 have landed: the four-combination USO/URO interaction protocol is owned by whichever of W2/W3 merges second, and both contracts state this so the qualification is never homeless. Untraced: none identified.

**Artifact classification:** Probe/encoding/error mapping: shipped behavior. Tests: verification aid. Protocol/results: process metadata per the adoption rule.

**Representation contract:** The segment-size control message this conn emits; owner is the W1 encoder; universal over sends this path produces; terminating evidence: encoding tests plus protocol engagement cells.

**Contract closure:** Triggered — silent send misbehavior is material (loss, MTU misfeedback) across reachable paths. Invariant: every segmented submission either sends all segments or surfaces a classified error preserving per-packet MTU feedback. Classes: full acceptance; message-size error (MTU probe); non-size error; probe-false fallback; kill-switch off. Each tested; guard mutation: misclassify the size error and observe the MTU test fail.

**Evidence budget:** Closure tests; protocol cells (engaged/available-unexercised/disabled/unavailable) with predeclared statistical bounds on the qualified Windows host.

**TDD and preservation evidence:** Closure tests first. Preservation: with probe false or switch off, send behavior byte-identical (disabled protocol cell plus full suite).

**Dispatch context budget:** Slice contract, W1's merged conn, the platform-neutral send logic (`send_queue.go:135-147`, GSO flag plumbing), Windows rules from the program plan. Fits one fresh context; governing diff is W1's PR.

**Slice decision audit:** Strongest split: none viable — probe without send wiring is horizontal. Strongest merge: with W3 — rejected; send and receive offloads have independent gates, and the Server 2022 interaction argues for separately revertable slices. Blocking edge W1: genuine — the control-message encoder and conn are W1's deliverables.

**Stop conditions:** USO probe succeeds but segmented sends corrupt or reorder on a supported build (platform defect beyond the documented Server 2022 restriction — operator decision); engagement remains zero across protocol cells.

### Slice W3 — Windows coalesced receive (URO)

**What it delivers:** A runtime probe for `UDP_RECV_MAX_COALESCED_SIZE` on transport-owned sockets, parsing `UDP_COALESCED_INFO` and feeding segment sizes to the G1 split helper (one 64 KiB preposted buffer per read), governance by `QUIC_GO_DISABLE_GRO`, the Server 2022 policy (TTL/DSCP ancillary receive features stay disabled while offloads are active unless functionally qualified), and the adoption protocol/results pair. The four-combination USO/URO interaction protocol — validating payload, coalesced-info, packet-info, and ECN separately — is owned by whichever of W2/W3 merges second; if that is this slice, its protocol includes those cells.

**Existing-work disposition:** New slice.

**Blocked by:** W1, G1.

**Single owner after merge:** The Windows conn owns URO parsing and splitting via G1; the buffer pool owns slabs; retention queues own copies through G1's dormant platform-neutral wiring, which activates automatically once this slice produces slab views — no queue-side code changes here.

**Authority completeness:** The URO capability flag: probe constructor, validation, per-connection lifetime, consumers (split path) — all here.

**Transitional-seam budget:** None introduced; closes G1's inert seam for its second producer.

**Blast radius:** Same routing/concurrency surface as G2, exercised through G1's shared fixtures — race-verified on Linux CI, run without the race detector on Windows CI. Memory per the program plan's one-slab-per-read shape. Interaction with W2 covered by the second-lander's protocol cells. Untraced: none identified.

**Artifact classification:** Probe/parse/split wiring: shipped behavior. Tests: verification aid. Protocol/results: process metadata.

**Representation contract:** Kernel-delivered coalesced buffers described by `UDP_COALESCED_INFO`; kernel plus parser own it; universal over that domain; terminating evidence: closure tests plus protocol cells.

**Contract closure:** Triggered — same material concurrency consequence as G2. Two invariants with singular owners, as in G2: slab release-exactly-once (owner: G1 slab, closed in G1) and exactly-one-routing-outcome (owner: the Windows conn's split/dispatch path, closed here with G1's shared fixtures). Classes: the routing classes re-run on Windows, plus the interaction combinations per the ownership rule above. Race verification runs on the platform-neutral fixtures under Linux `-race` CI (`unit.yml` runs the race detector on ubuntu only); Windows CI runs the same tests without `-race`, and no Windows race job is claimed.

**Evidence budget:** Closure tests; protocol cells including the interaction matrix and memory budgets, statistical bounds predeclared.

**TDD and preservation evidence:** Closure tests first. Preservation: probe-false/disabled cells byte-identical; full suite.

**Dispatch context budget:** Slice contract, G1's merged API, W1's merged conn, the URO rules from the program plan. Fits one fresh context; governing diffs are G1 and W1 PRs.

**Slice decision audit:** Strongest split: interaction qualification as its own slice — rejected, the ancillary policy is this slice's safety contract, not separable evidence. Strongest merge: with W2 — rejected as in W2. Blocking edges: W1 (conn, parser) and G1 (slab, split helper) are both genuine; G2 is deliberately not an edge — the split helper is G1's deliverable, and Windows must not wait on Linux adoption.

**Stop conditions:** The documented Server 2022 restriction reproduces as more than the cited TTL/DSCP scope (payload or coalesced-info corruption — operator decision); closure invariant fails in any class; memory budgets exceeded without in-budget mitigation.

## Acceptance Criteria

- [ ] On a USO/URO-capable Windows build, segmented sends and coalesced receives engage per their protocols' predeclared thresholds; once both W2 and W3 have merged, all four offload combinations are validated for payload and ancillary metadata by the second-lander's protocol.
- [ ] With probes failing, switches set, or a caller-supplied socket, Windows behavior is behavior-identical to the W1 foundation under the disabled and probe-false protocol cells and the full suite (negative criterion: no offload socket option is set on caller-supplied sockets).
- [x] W1's deadline/close closure classes pass on Windows CI before W2 or W3 merges — delivered by #242 (`sys_conn_closure_test.go` runs on the whole CI matrix, Windows included) and enforced continuously from then on.

Universal criteria carry the per-slice domains, owners, guarantee levels, and terminating evidence above.

## Validation Gates

`go test ./...` on the Windows CI matrix; focused closure tests per slice; each slice's precommitted protocol cells on the qualified Windows host.

## Operating Discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice` for every slice/PR; no repository-specific overlay exists. Track-specific stops: the rejected direct-Winsock alternative must not be resurrected without operator decision; the Server 2022 ancillary policy is binding; reverts run W3, W2, W1 in that order. Vocabulary per the root `CONTEXT.md`.
