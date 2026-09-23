# Windows managed ECN feasibility plan

**Date:** 2026-09-22
**Status:** W1 complete — IPv4-only and IPv6-only native ECN feasible on the tested Windows build; implementation awaits scoped handoff
**Track:** W, 5 of 5 in `QGF-ECN-20260922`
**Depends on:** Nothing — safe in parallel with L1
**Related:** [Program index](2026-09-22-managed-ecn-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md), [Windows datapath plan](2026-09-11-windows-datapath-plan.md)
**Normative scope:** Current feasibility outcome, evidence boundary, blockers and stop conditions
**Audit history:** [Architecture handoff audit](../audits/2026-09-22-managed-ecn-handoff/README.md)

## Goal

Produce a native, evidence-backed Windows disposition for receive ECN metadata and outgoing ECN marking through the existing Winsock message-I/O boundary. Unsupported closes the platform track explicitly. Feasible records the proven representation and returns to scoped `$architecture-handoff` before implementation.

## Current shape (verified 2026-09-22)

`windowsConn` owns WSARecvMsg/WSASendMsg-based packet-info, USO and URO representations, but its capabilities omit ECN and `WritePacket` panics for any ECN value other than unsupported (`sys_conn_windows.go:24-102,155-250`). Receive parsing records packet info and coalescing only. Managed Windows normalization retains that decoder for URO, but neither URO, USO nor packet-info establishes ECN (`managed_packet_receive_windows.go:5-35`).

## Decision

Run a bounded native Windows feasibility probe at the existing `*net.UDPConn.ReadMsgUDP` / `WriteMsgUDP` and Winsock control-message/socket-option boundary. Establish separately whether supported Go/x/sys and Windows versions can request IPv4/IPv6 receive ECN metadata, whether WSARecvMsg delivers authoritative per-datagram Not-ECT/ECT(0)/ECT(1)/CE values, and whether WSASendMsg applies requested outgoing marks. Distinguish these results from packet info, USO and URO.

W1 commits a concise native receipt and updates ADR 0007/program status to unsupported or feasible. A feasible result names the exact representation, minimum Windows/runtime boundary and family limits, then requires scoped `$architecture-handoff` for an implementation/qualification slice. W1 makes no production changes. Temporary probes are retired unless a small characterization test is independently useful and proportionate.

**Rejected alternative (do not do this):** Do not infer ECN from packet-info, USO/URO, Linux control messages, cross-compilation or header constants. Do not remove the current panic/false capability until a later audited implementation owns both receive and send. Do not expose a socket handle merely to make a probe convenient.

**Non-goals:** Implementing Windows managed ECN; other platforms; packet-info/USO/URO redesign; public APIs; raw handle exposure; performance work or ECN recovery changes.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| W1 | Complete — feasible | Native Windows feasible or unsupported disposition | None | Temporary probe retired in W1 |

## Implementation slices

### Slice W1 — Decide Windows native ECN feasibility

**Stable identity:** `QGF-ECN-20260922/W1`. One evidence/docs PR; GitHub child [#507](https://github.com/the-sarge/quic-go-fast/issues/507).

**What it delivers:** Native IPv4/IPv6 receive/send evidence and a current Windows disposition, without changing runtime capability.

**Existing-work disposition:** New slice. No open PR or implementation branch exists. Merged Windows packet-info/USO/URO code is retained and explicitly not an ECN baseline.

**Blocked by:** None. Winsock feasibility is independent of L1; any later implementation would reuse the merged managed adapter.

**Single owner after merge:** No new runtime owner. ADR 0007/program status and the linked native receipt own the disposition. A future supported implementation must name the Windows representation and enforcement owner in a scoped handoff.

**Authority completeness:** No authoritative runtime fact is introduced. The receipt includes probe construction, native OS build, Winsock/Go boundary, observed values, family limits and cleanup. Feasible cannot silently enable capability.

**Transitional-seam budget:** Zero runtime seams. Temporary probe source/binaries are retired unless explicitly justified; no dormant encoder/parser or capability flag.

**Blast radius:** Documentation and disposable native probing only. Existing Windows packet-info, USO/URO, managed normalization, runtime behavior, public surface and dependencies remain byte-for-byte unaffected except status docs. No untraced effect is accepted.

**Artifact classification:** Disposition and native receipt are process metadata. Temporary probes are verification aids with payoff limited to identifying the Winsock representation, supported domain limited to the recorded Windows build/address families, owner the W1 operator and retirement at receipt capture. No shipped behavior or safety enforcement change.

**Representation contract:** Supported input is a disposable native Windows UDP socket on the recorded Windows build/architecture/Go version, separately `udp4` and `udp6`. The actual supported Winsock/Go control-message API is the candidate trusted representation owner. W1 makes example-level feasibility claims only for that environment, not a universal managed ECN guarantee. Finite evidence is the six probe classes below.

**Contract closure:** Not triggered because W1 accepts no runtime ECN invariant. A feasible result requires the scoped handoff to apply the representation gate and closure triggers to the implementation contract.

**Evidence budget:** One native Windows host; at most six probe classes: IPv4 receive, IPv6 receive, IPv4 send, IPv6 send, absent/malformed/no-option behavior and close/cancellation. Each positive receive class covers four ECN values in one table. Capture API availability, option result, authoritative ancillary parse, peer-observed traffic class and exact OS/Go versions. No performance run, repeated builds, cloud matrix, fuzzing or unbounded header research.

**TDD and preservation evidence:** Build the smallest native characterization probe first. A positive result must discriminate removal of receive extraction or outgoing marking. An unsupported result records the failed boundary and removes temporary code. Existing Windows tests confirm packet-info/USO/URO and ECN=false remain unchanged.

**Dispatch context budget:** Current W1 contract, ADRs 0006/0007, shared baselines and overlay; `sys_conn_windows.go:24-250`, Windows managed receive setup, focused Windows tests, relevant Go/x/sys definitions and only a small comparison range from one OOB platform. Target under 18,000 input tokens plus concise native output. Do not load whole offload histories.

**Slice decision audit:** Feasibility and implementation must split because the authoritative Winsock representation and supported Windows boundary are unresolved. IPv4/IPv6 belong in one feasibility decision. Merging with L1 would make Linux wait for unrelated native access and would not answer the Windows API question.

**Stop conditions:** Stop as blocked without a native Windows host. Stop and request scoped `$architecture-handoff` on a feasible result before production edits. Do not declare unsupported from cross-compilation, missing constants on another OS, absent runner or one unexplained failure. Do not conflate packet-info, USO/URO and ECN.

## Acceptance criteria

- [x] A native receipt records exact Windows build/architecture/Go environment and separate IPv4/IPv6 receive/send results.
- [x] The disposition distinguishes API availability, socket-option success, delivered per-datagram metadata and peer-observed outgoing marks from packet-info, USO and URO.
- [x] Unsupported leaves `windowsConn` ECN false; feasible names the representation and routes implementation through scoped `$architecture-handoff`.
- [x] No public/raw authority or production capability is introduced by W1.

## Validation gates

Validate receipt completeness, temporary-probe cleanup, relative links, `git diff --check` and repository documentation exact-head/hosted checks. If a maintained Go characterization remains, run its native focused test, changed-package `go vet` and `go mod tidy -diff`.

## Operating discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice`, composed with [the repository overlay](../REVIEW-LOOP.md). W1 terminates with a native disposition, not an implementation.

## Current disposition

The [W1 native receipt](../audits/2026-09-23-windows-ecn-w1/README.md) establishes example-level IPv4-only and IPv6-only receive/send feasibility on Windows Server 2025 build 26100.32230, Go 1.27.0 and x/sys 0.48.0. Windows ECN capability remains false. The next action is scoped `$architecture-handoff` to define implementation and qualification; no successor slice is dispatchable. The receipt records native representation, family limits, zero-byte successful send results, preservation evidence and probe retirement.
