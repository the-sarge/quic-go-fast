# OpenBSD managed ECN feasibility plan

**Date:** 2026-09-22
**Status:** O1 complete; IPv6-only native feasibility established, IPv4 receive unsupported; scoped handoff required before implementation
**Track:** O, 4 of 5 in `QGF-ECN-20260922`
**Depends on:** Nothing — safe in parallel with L1
**Related:** [Program index](2026-09-22-managed-ecn-program.md), [ADR 0001](0001-upstream-compatibility.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md)
**Normative scope:** Current feasibility outcome, evidence boundary, blockers and stop conditions
**Audit history:** [Architecture handoff audit](../audits/2026-09-22-managed-ecn-handoff/README.md)

## Goal

Produce a native, evidence-backed OpenBSD disposition for both receive ECN metadata and outgoing ECN marking without changing public authority. An unsupported result closes the platform track explicitly. A supported result records the proven native representation and returns to a scoped `$architecture-handoff` before any implementation slice is dispatched.

## Current disposition

The [native O1 receipt](../audits/2026-09-23-openbsd-ecn-o1/README.md) establishes IPv6-only receive and per-datagram send feasibility on OpenBSD 7.9/amd64 with Go 1.27.0. IPv4 receive TOS has no supported API in the tested surface; IPv4 control-message sends succeed but outgoing marks remain unproven. Managed ECN remains false. O1 is complete with no defined successor: run scoped `$architecture-handoff` before dispatching any IPv6 implementation/qualification work. The OpenBSD parent remains pending that handoff.

## Current shape (verified 2026-09-22)

OpenBSD is compiled through the generic no-OOB implementation: `newConn` returns `basicConn`, which reads ordinary datagrams, reports no ECN capability and rejects marked writes (`sys_conn_no_oob.go:1-18`; `sys_conn.go:127-167`). OpenBSD-specific code currently covers buffer limits, not ancillary ECN (`sys_conn_buffers_openbsd.go`). No repository parser, encoder or native test establishes IPv4 or IPv6 ECN on OpenBSD.

## Decision

Run a bounded native OpenBSD feasibility probe at the standard-library `*net.UDPConn` / `ReadMsgUDP` / `WriteMsgUDP` and kernel socket-option boundary. Establish separately whether the supported Go/x/sys surface can request IPv4 and IPv6 receive traffic-class metadata, whether the kernel delivers authoritative per-datagram values for Not-ECT, ECT(0), ECT(1) and CE, and whether outgoing control messages apply the requested values. API names or successful compilation are not evidence of delivery.

O1 does not add production ECN support. It commits a concise native receipt and updates ADR 0007/program status to either: unsupported, with the exact missing/failed API or kernel behavior; or feasible, with the exact authoritative representation, address-family limits and an instruction to run scoped `$architecture-handoff` for a new implementation/qualification slice. Temporary probes are deleted before merge unless a small characterization test is independently useful and proportionate; they never become a maintained blocking product by default.

**Rejected alternative (do not do this):** Do not port Linux constants or parser code by analogy, infer support from another BSD, cross-compile a capability claim, add production code while representation ownership is unresolved, or treat socket-wide default TOS alone as per-datagram managed ECN.

**Non-goals:** Implementing OpenBSD managed ECN; Linux/Darwin/FreeBSD/Windows work; raw descriptor or public metadata APIs; throughput or offload work; exhaustive external grammar analysis.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| O1 | Complete — IPv6 feasible; IPv4 receive unsupported | Native disposition recorded; no runtime capability added | None | Temporary probe retired in O1 |

## Implementation slices

### Slice O1 — Decide OpenBSD native ECN feasibility

**Stable identity:** `QGF-ECN-20260922/O1`. One evidence/docs PR; GitHub child [#506](https://github.com/the-sarge/quic-go-fast/issues/506).

**What it delivers:** Native IPv4/IPv6 receive/send evidence and a current platform disposition. A feasible result names the representation but authorizes no product implementation; an unsupported result closes the track without code.

**Existing-work disposition:** New slice. No open PR, implementation branch or existing OpenBSD OOB owner exists. Shared Unix code is comparison evidence only.

**Blocked by:** None. The native API/kernel question is independent of L1; any later implementation would depend on the merged managed adapter.

**Single owner after merge:** No new runtime owner. O1's durable owner is ADR 0007/program status plus the linked native receipt. A future supported implementation must assign the native representation and enforcement owner in a scoped handoff.

**Authority completeness:** No authoritative runtime fact is introduced. The disposition includes probe construction, native execution environment, observed receive/send values, address-family limits and cleanup. A feasible finding cannot silently become advertised capability.

**Transitional-seam budget:** Zero runtime seams. Temporary probe source/binaries are verification aids retired before merge unless explicitly justified in the PR; no dead production branches or capability flags.

**Blast radius:** Documentation and bounded native probe execution only. No socket mutation persists beyond the disposable probe, no repository runtime path changes, and no public/schema/dependency/performance effect. An implementation finding is an explicit future handoff, not scope expansion.

**Artifact classification:** The disposition and native receipt are process/traceability metadata. Temporary probes are verification aids with payoff limited to identifying the native representation, supported domain limited to the recorded OpenBSD version/address families, owner the O1 operator and retirement at receipt capture. No shipped behavior or required safety enforcement changes in O1.

**Representation contract:** Supported input is a disposable native OpenBSD UDP socket on the recorded OS/kernel/architecture/Go version, tested separately for `udp4` and `udp6`. The platform's actual Go/x/sys API and kernel-delivered control messages, if any, are the candidate trusted owner. O1 makes only example-level feasibility claims for the exact tested environment; it makes no universal managed ECN guarantee. Finite evidence is the six probe classes below.

**Contract closure:** Not triggered. O1 accepts no shipped ECN invariant or runtime enforcement owner. If evidence supports implementation, the scoped handoff must apply the representation gate and contract-closure triggers to the proposed runtime contract before dispatch.

**Evidence budget:** One native host; at most six probe classes: IPv4 receive, IPv6 receive, IPv4 send, IPv6 send, absent/malformed/no-option behavior and close/cleanup. Each positive receive class exercises the four ECN values in one table. Capture compiler/API result, socket-option result, actual ancillary bytes parsed by an authoritative API where available, peer-observed traffic class and environment versions. No retries beyond diagnosis of an identified infrastructure failure, no fuzzing, performance runs, repeated kernels or cloud matrix.

**TDD and preservation evidence:** Build the smallest native characterization probe first and observe the current unsupported repository path remains unchanged. A positive result must fail if receive parsing or outgoing marking is removed from the probe. An unsupported result records the failing boundary and removes temporary code. Repository tests remain green because O1 changes no runtime path.

**Dispatch context budget:** Current O1 contract, ADRs 0006/0007, shared baselines and overlay; `sys_conn_no_oob.go`, `sys_conn.go:127-167`, OpenBSD buffer files, relevant Go/x/sys OpenBSD definitions and only comparison ranges from one shared OOB platform. Target under 18,000 input tokens plus concise native output. Do not load whole Linux/Darwin/FreeBSD histories.

**Slice decision audit:** Feasibility and implementation must split because the native representation owner is currently unknown; combining them would make the implementer design an architecture from probe results inside one PR. IPv4 and IPv6 remain one feasibility slice because one platform/API decision needs both before capability can be proposed. No blocker is needed: L1 does not answer OpenBSD's native API question.

**Stop conditions:** Stop as blocked if no native OpenBSD host is available. Stop and request scoped `$architecture-handoff` on any feasible result before production edits. Do not declare unsupported from cross-compilation, missing local headers on another OS, lack of a runner or a single unexplained probe failure. Do not add a handwritten parser claiming broad ancillary coverage.

## Acceptance criteria

- [x] A native receipt records the exact OpenBSD/kernel/architecture/Go environment and separate IPv4/IPv6 receive/send results.
- [x] The disposition distinguishes API availability, socket-option success, delivered per-datagram metadata and peer-observed outgoing marks.
- [x] Unsupported evidence updates the platform matrix and leaves ECN false; feasible evidence names the representation and routes a new implementation slice through scoped `$architecture-handoff`.
- [x] No public/raw authority or production capability is introduced by O1.

## Validation gates

Validate receipt completeness, disposable probe cleanup, relative links, `git diff --check` and the repository's documentation exact-head/hosted checks. If any maintained Go test remains, run its native focused test, changed-package `go vet` and `go mod tidy -diff`.

## Operating discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice`, composed with [the repository overlay](../REVIEW-LOOP.md). O1 terminates with a native disposition, not an implementation.
