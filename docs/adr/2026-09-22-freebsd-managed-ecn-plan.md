# FreeBSD managed ECN qualification plan

**Date:** 2026-09-22
**Status:** Accepted; blocked by Linux slice L1
**Track:** F, 3 of 5 in `QGF-ECN-20260922`
**Depends on:** `QGF-ECN-20260922/L1`
**Related:** [Program index](2026-09-22-managed-ecn-program.md), [Linux plan](2026-09-22-linux-managed-ecn-plan.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Architecture handoff audit](../audits/2026-09-22-managed-ecn-handoff/README.md)

## Goal

Qualify FreeBSD against the accepted managed ECN contract using the endpoint-owned adapter delivered by Linux L1 and FreeBSD's existing native OOB representation. The slice may close FreeBSD as explicitly unsupported when native evidence disproves the contract; it must never publish partial capability.

## Current shape (verified 2026-09-22)

FreeBSD builds the shared `oobConn`, uses `IP_RECVTOS` / `IPV6_RECVTCLASS`, parses received ECN into `receivedPacket.ecn`, appends family-specific outgoing marks and reports ECN unless opted out (`sys_conn_oob.go:57-178,240-317,335-362`; `sys_conn_helper_freebsd.go:12-38`). It has no adopted GRO or GSO path. Managed setup is currently disabled by `managed_packet_receive_other.go:1-5`, and registered batch submission therefore uses the endpoint's ordinary multi-write callback rather than a separate native acceleration.

## Decision

After L1 merges, narrow the shared `managed_packet_receive_other.go` build constraint so FreeBSD alone gains the endpoint-owned platform setup needed to retain its existing `oobConn`; OpenBSD and every still-unsupported platform must continue to compile exactly one stub. Reuse L1's current-generation buffer/range/address correlation, full-datagram non-GRO single-read mode, checked-singleton result translator, family-complete capability gate and managed-only capability projection. Direct marked ordinary writes use the endpoint-owned native writer; wrapped ordinary and batch marked writes use the explicitly checked callback and existing ordinary `WriteMsgUDP` loop. No FreeBSD-specific managed adapter or acceleration is authorized, and packet info remains outside correlated metadata.

Support is accepted only if native FreeBSD `udp4`, `udp6` and any admitted `udp` dual-stack domain demonstrate receive values, full-size payload preservation, ordinary/batch outgoing marks, checked-singleton native results, selected/foreign peer policy, lease reuse/revocation, fallback and cleanup. The test records `IPV6_V6ONLY` and IPv4-mapped behavior; any usable family failure disables the endpoint capability. If the platform cannot satisfy a required family/path, remove partial capability code and publish an explicit unsupported disposition with exact evidence. Missing native host access is blocked evidence, not unsupported evidence.

**Rejected alternative (do not do this):** Do not infer support from the shared Unix build tag, port Linux coalescing, add a raw descriptor path, claim batch acceleration, or land a receive-only/send-only capability.

**Non-goals:** Linux, Darwin, OpenBSD or Windows changes; GRO/GSO or sendmmsg adoption; public APIs; throughput measurement; QUIC ECN recovery changes.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| F1 | New | Native FreeBSD supported or explicit unsupported managed ECN disposition | L1 | None introduced |

## Implementation slices

### Slice F1 — Qualify FreeBSD managed ECN

**Stable identity:** `QGF-ECN-20260922/F1`. One intended PR; GitHub child pending default-branch plan publication.

**What it delivers:** A complete supported FreeBSD managed ECN path through the L1 adapter and existing OOB/ordinary batch owners, or a docs-only explicit unsupported disposition after removing partial code.

**Existing-work disposition:** New slice. No open PR or implementation branch exists. Existing FreeBSD OOB behavior is retained evidence, not managed qualification.

**Blocked by:** L1, because F1 reuses its central correlation, policy-safe route selection and capability gate.

**Single owner after merge:** Managed endpoint and shared `oobConn` own metadata/socket lifecycle; L1 adapter owns correlation/capability; registered wrapper callback owns policy-safe multi-write submission; caller wrapper owns peer policy.

**Authority completeness:** No persistence. Platform setup, native qualification, generation cleanup and either complete capability or complete rollback land together.

**Transitional-seam budget:** Zero. No parallel adapter, partial capability, unused acceleration or temporary representation.

**Blast radius:** FreeBSD build-tagged setup, the shared unsupported-stub build constraint, shared adapter hooks, native tests and platform status docs. Cross-builds for Darwin, FreeBSD and OpenBSD must each select exactly one `(*managedPacketEndpoint).configureReceive` definition. Full ordinary datagrams, OOB behavior, wrapper policy, batch prefix/error semantics, lease lifecycle, managed GSO/GRO/DF projection and dependencies remain unchanged. No untraced effect is accepted.

**Artifact classification:** Supported runtime behavior is shipped behavior; correlation, generation, policy and cleanup gates are required safety enforcement. Native tests/probes are verification aids without a maintained-product exception. Dispositions, receipts and tracking are process metadata.

**Representation contract:** Factory-created FreeBSD UDP managed endpoint and current lease, direct or through the L1-supported wrapper domain whose `ReadFrom` synchronously preserves the caller buffer/range/address and whose checked callback synchronously forwards the exact borrowed singleton payload/OOB and returns its lease result unchanged. Shared `oobConn`, `unix.ParseOneSocketControlMessage` and FreeBSD control-message helpers own native representation. The L1 adapter owns correlation, checked-singleton translation, family-complete admission and managed-only capability projection. Universal guarantee inside that typed domain and kernel-emitted supported messages, including IPv4-mapped traffic only when its admitted dual-stack row passes; example-level wrapper evidence. Finite termination is the matrix and native gates below.

**Contract closure:** Triggered only for a supported result. Invariant: FreeBSD advertises managed ECN only when the current endpoint-owned native representation preserves and applies the exact value through every admitted route. Enforcement owner: existing native OOB helpers plus the single L1 adapter.

| Semantic class | Required disposition | Evidence |
| --- | --- | --- |
| IPv4/IPv6 Not-ECT, ECT(0), ECT(1), CE receive | Exact value or unsupported platform result | Native family/mark table through managed lease |
| Non-GRO full-size ordinary datagram and lease release | Whole payload, one kernel read, no stranded read-ahead | Native boundary-size/truncation/release table |
| Direct and checked-wrapper ordinary marked send | Exact mark, selected destination | Native receiver and foreign rejection |
| Checked singleton native result | Preserve success, message-size and terminal errors; FreeBSD EPERM remains terminal without Linux's retry; reject invalid callback results | Native FreeBSD result table excluding Linux retry expectations |
| Checked callback batch marked send | Exact shared mark with definite prefix/error | Native ordinary-batch receiver plus failure tests |
| Single-family, dual-family and IPv4-mapped endpoint | Capability only when every usable admitted family qualifies | `udp4`/`udp6`/`udp` setup and mapping table |
| Managed capability projection | ECN only is newly gated; GSO/packet info are not imported | Focused capability table |
| Opt-out/setup failure/absent or malformed metadata | Ordinary usable fallback, no stale mark/capability | Focused negative cases |
| Release/reacquire/stale/Close | Current generation only; joined cleanup | Lifecycle and race cases |

**Evidence budget:** At most 10 new table-driven test functions on one native FreeBSD host, covering `udp4`, `udp6`, any admitted `udp` dual-stack/mapped behavior, four receive values, full-size non-GRO payloads and representative ordinary/batch outgoing values. Reuse existing managed lifecycle/progress and L1 singleton-invalid-result structure, but assert FreeBSD EPERM is terminal without retry while message-size and other terminal results retain native behavior. At most one L1 correlation-guard mutation; no grammar mutation. One accepted `-race` gate, no repetition, benchmark or new acceleration campaign. Native family skips do not satisfy a supported disposition; exact-head evidence from another FreeBSD host is required. One review plus at most one replacement.

**TDD and preservation evidence:** Write native managed receive/send/family qualification tables first, then add the smallest platform setup and build-tag narrowing. Preserve existing FreeBSD OOB, ordinary batch callback, selected-peer, full-datagram lease, capability projection, singleton error behavior and public compatibility tests. Cross-build Darwin, FreeBSD and OpenBSD to prove exactly one receive setup definition per target. A negative result lands only durable disposition/docs after removing partial code.

**Dispatch context budget:** Supply the current F1 contract, program lines 1-80, ADRs 0006/0007, skill-supplied shared baselines and overlay; `managed_packet_receive_other.go`; `sys_conn_helper_freebsd.go`; `sys_conn_oob.go:57-362`; and, from merged L1, only the named managed adapter type plus its `ReadPacket`, `WritePacket`, `capabilities` and checked-singleton translator declarations, the endpoint per-family/setup and normalization-activation declarations, `managedPacketConn.WriteBatchV1`, and the `ConfigureManagedPacketIOV1` comment/setup block. Test input is bounded to `TestManagedPacketIOBatchDatagrams`, `TestManagedPacketIORegistration`, `TestManagedPacketIOConcurrentBatchClose`, `TestOOBReaderAncillaryFailure`, the five `TestReadECNFlags*` / `TestSendPacketsWithECNOn*` functions and the L1 table helpers directly reused by F1. The publication-revision proxy uses the current declarations at their corresponding ranges and measures 59,207 bytes / 7,127 whitespace words, or 14,802 input tokens by `max(bytes/4, words*2)`. Resolve exact post-L1 line ranges and re-measure this named manifest at dispatch; stop before implementation if it exceeds 24,000. Load no whole L1 diff, whole test files, unrelated platform material or historical plan chronology.

**Slice decision audit:** Receive/send cannot split without an unsafe half-capability. Merging with L1 or D1 couples native hosts and build tags. L1 is a real blocker because it owns the representation seam; no acceleration is required for F1 to remain independently green.

**Stop conditions:** Stop if native access is unavailable, support requires a FreeBSD-specific second adapter, public/raw authority, new batching acceleration, capability before both directions pass, another platform, or evidence beyond the finite budget. Unsupported requires native evidence.

## Acceptance criteria

- [ ] Native FreeBSD evidence records an exact supported or unsupported disposition for IPv4/IPv6 receive metadata and ordinary/batch marked output.
- [ ] A supported result covers full-size non-GRO payloads, `udp4`/`udp6`/admitted dual-stack families, selected/foreign peers, singleton native-result preservation including terminal no-retry EPERM, opt-out/setup failure, malformed/absent metadata, lease reuse/revocation and cleanup through the shared owner.
- [ ] An unsupported result leaves managed ECN false and removes partial capability code while recording the exact blocker.
- [ ] Darwin, FreeBSD and OpenBSD each cross-build with exactly one `(*managedPacketEndpoint).configureReceive` definition; public signatures, ordinary datagrams, managed capability projection, existing batch progress/error semantics and raw-socket authority remain unchanged.

## Validation gates

Run focused native FreeBSD tests, the accepted package `-race` gate, affected-package tests, changed-package `go vet`, `go mod tidy -diff`, `git diff --check`, Darwin/FreeBSD/OpenBSD cross-builds and applicable exact-head hosted checks. Record FreeBSD/kernel version, architecture, Go version, `udp4`/`udp6`/`udp` family domain, `IPV6_V6ONLY` and IPv4-mapped availability.

## Operating discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice`, composed with [the repository overlay](../REVIEW-LOOP.md). Supported and unsupported outcomes are accepted; missing native access is not a disposition.
