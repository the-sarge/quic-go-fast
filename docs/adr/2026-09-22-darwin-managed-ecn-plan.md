# Darwin managed ECN qualification plan

**Date:** 2026-09-22
**Status:** Accepted; ready after Linux L1
**Track:** D, 2 of 5 in `QGF-ECN-20260922`
**Depends on:** `QGF-ECN-20260922/L1`
**Related:** [Program index](2026-09-22-managed-ecn-program.md), [Linux plan](2026-09-22-linux-managed-ecn-plan.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md), [Darwin batch plan](2026-09-11-darwin-batch-plan.md)
**Normative scope:** Current outcome, boundaries, invariants, acceptance evidence, blockers and stop conditions
**Audit history:** [Architecture handoff audit](../audits/2026-09-22-managed-ecn-handoff/README.md), [Darwin family-mapping re-audit](../audits/2026-09-22-darwin-managed-ecn/reaudit.md)

## Goal

Qualify Darwin against the accepted managed ECN contract using the endpoint-owned adapter delivered by Linux L1 and Darwin's existing native OOB/sendmsg_x representation. The slice may close Darwin as explicitly unsupported when native evidence disproves the contract; it must never publish a partial capability.

## Current shape (verified 2026-09-22)

Darwin's shared `oobConn` receives and parses ECN through `newConnWithSetup`, `readManagedPacket` and `decodeReadPacket`, and marks sends through `WritePacket` (`sys_conn_oob.go:98-193,259-357,374-397`; platform constants in `sys_conn_helper_darwin.go`). Registered callbacks reach `managedPacketConn.WriteBatchV1` (`managed_packet_endpoint.go:286-325`) and the existing Darwin `newUDPBatchWriter` (`send_conn_sendmsg_x_darwin.go:199-238`). The unsupported stub currently provides both `configureReceive` and `managedPacketRawFactory` (`managed_packet_receive_other.go:1-9`), so Darwin does not yet install the shared managed ECN adapter. Re-resolve these named declarations at dispatch; these ranges describe the post-L1 base.

## Decision

After L1 merges, narrow the shared `managed_packet_receive_other.go` build constraint so Darwin alone gains the endpoint-owned platform setup needed to retain its existing `oobConn`; OpenBSD and every still-unsupported platform must continue to compile exactly one stub. Do not fork the managed adapter or add a Darwin-specific authority path. Reuse L1's current-generation buffer/range/address receive correlation, full-datagram non-GRO single-read mode, checked-singleton result translator, family-complete capability gate and managed-only capability projection. Preserve Darwin's qualified native batch owner; a registered wrapper still uses its checked callback, not descriptor bypass, and packet info remains outside the correlated metadata.

Darwin family admission uses the native option that actually carries each family's metadata: an AF_INET endpoint requires `IP_RECVTOS`; an AF_INET6 endpoint requires `IPV6_RECVTCLASS`, which supplies `IPV6_TCLASS` for both IPv6 and admitted IPv4-mapped datagrams. A failed `IP_RECVTOS` on AF_INET6 is not a failed usable family when the required IPv6 option and mapped native qualification pass. Keep this platform mapping inside the endpoint-owned setup policy and retain the common adapter/correlation owner. Factoring the existing family inspection/factory declarations into a Linux/Darwin shared file is allowed without changing Linux policy or behavior.

The implementation outcome is accepted only if native Darwin `udp4`, `udp6` and any admitted `udp` dual-stack domain demonstrate receive marks, ordinary marks, batch marks, selected/foreign peer behavior, checked-singleton native results, full-size payload preservation, lease reuse/revocation, opt-out/failure fallback and cleanup. The test records `IPV6_V6ONLY` and IPv4-mapped behavior; any usable family failure disables the endpoint capability. If a supported address family or native submission path cannot satisfy the contract, remove any partial capability code and publish an explicit unsupported disposition with the exact OS/runtime/API evidence in this PR. Missing native host access is blocked evidence, not unsupported evidence.

**Rejected alternative (do not do this):** Do not generalize Linux setup blindly, infer support from shared build tags, bypass a wrapper through `SyscallConn`, treat sendmsg_x qualification as receive qualification, or keep a receive-only/send-only capability.

**Non-goals:** Linux policy or behavior changes; FreeBSD, OpenBSD or Windows changes; redesign of sendmsg_x; public APIs; raw descriptor exposure; new performance measurements; new ECN recovery policy.

## Slice graph

| Slice | Status/disposition | Delivers | Blocked by | Removes temporary seam |
| --- | --- | --- | --- | --- |
| D1 | New | Native Darwin supported or explicit unsupported managed ECN disposition | L1 | None introduced |

## Implementation slices

### Slice D1 — Qualify Darwin managed ECN

**Stable identity:** `QGF-ECN-20260922/D1`. One intended product PR; GitHub child [#504](https://github.com/the-sarge/quic-go-fast/issues/504).

**What it delivers:** A complete supported Darwin managed ECN path through the L1 adapter and native OOB/sendmsg_x owners, or a docs-only explicit unsupported disposition after removing partial code.

**Existing-work disposition:** Rework the provisional uncommitted implementation on branch `codex/d1-darwin-managed-ecn` in `/Volumes/worktrees/quic-go-fast/d1-darwin-managed-ecn`, based on `0449f0bdd3f7bc75528275b9938e028022152409`; its native-validation counterpart is on m4mini at `/Volumes/worktrees/quic-go-fast/d1-native-validation`. No product PR exists. This work is evidence, not design authority: reconcile it with the current default-branch contract and corrected family row before resuming. Merged Darwin batch behavior is retained evidence, not proof of managed receive/send qualification.

**Blocked by:** L1, because D1 reuses its central metadata correlation, route selection and capability gate. D1 must not reimplement those owners.

**Single owner after merge:** The managed endpoint and shared native `oobConn` own metadata and socket lifecycle; the L1 adapter owns correlation/capability; existing sendmsg_x or registered wrapper callback owns native batch submission; the caller wrapper owns policy.

**Authority completeness:** A qualified Darwin endpoint installs a non-nil managed raw factory so capability publication includes the shared lease adapter. No persistence. Platform setup, capability publication, native receive/send evidence, generation cleanup and unsupported rollback land together. An unsupported outcome leaves ECN false and no unused platform branch.

**Transitional-seam budget:** Zero. No parallel Darwin adapter, provisional capability or lingering partial setup is allowed.

**Blast radius:** Mechanical relocation of the Linux family-inspection/factory declarations into a shared Linux/Darwin file with unchanged Linux policy, behavior and tests; Darwin build-tagged managed setup, the shared unsupported-stub build constraint, shared adapter hooks established by L1, native qualification tests and platform matrix/docs. Cross-builds for Darwin, FreeBSD and OpenBSD must each select exactly one definition each of `(*managedPacketEndpoint).configureReceive` and `(*managedPacketEndpoint).managedPacketRawFactory`. Existing sendmsg_x progress/error semantics, full ordinary datagrams, lease lifecycle, wrappers, managed GSO/GRO/DF projection and unregistered OOB behavior remain unchanged. No untraced effect is accepted.

**Artifact classification:** Supported runtime path and capability are shipped behavior; correlation, policy, generation and cleanup gates are required safety enforcement. Native tests/probes are verification aids with no maintained-product exception. The supported/unsupported disposition, plan status, receipts and tracking updates are process metadata.

**Representation contract:** Factory-created Darwin UDP managed endpoint and current lease, direct or through the L1-supported wrapper domain whose `ReadFrom` synchronously preserves the caller buffer/range/address and whose checked callback synchronously forwards the exact borrowed singleton payload/OOB and returns its lease result unchanged. Shared `oobConn`, `unix.ParseOneSocketControlMessage` and Darwin's qualified sendmsg_x encoder own native representation. The L1 adapter owns correlation, checked-singleton translation, family-complete admission and managed-only capability projection. Universal guarantee within that typed domain and kernel-emitted supported messages, including IPv4-mapped traffic only when its admitted dual-stack row passes; example-level evidence for the selected-peer wrapper. Finite termination is the semantic matrix and native gates below.

**Contract closure:** Triggered only for a supported result, for the same material congestion/policy/lifecycle consequence and independently reachable families as L1. Invariant: Darwin may advertise managed ECN only when the current endpoint-owned native representation preserves the exact receive value and applies the exact outgoing value through every admitted route. Enforcement owner: existing native OOB/sendmsg_x owners plus the single L1 adapter.

| Semantic class | Required disposition | Evidence |
| --- | --- | --- |
| IPv4/IPv6 Not-ECT, ECT(0), ECT(1), CE receive | Exact value or unsupported platform result | Native family/mark table through managed lease |
| Non-GRO full-size ordinary datagram and lease release | Whole payload, one kernel read, no stranded read-ahead | Native boundary-size/truncation/release table |
| Direct ordinary and checked-wrapper ordinary marked send | Exact requested mark and selected destination | Native receiver observation; foreign rejection |
| Checked singleton native result | Preserve success, message-size and terminal errors; Darwin EPERM remains terminal without Linux's retry; reject invalid callback results | Native Darwin result table excluding Linux retry expectations |
| Registered batch marked send | Exact shared mark with existing prefix/error semantics | Native batch receiver plus existing error tests |
| Single-family, dual-family and IPv4-mapped endpoint | AF_INET requires IP_RECVTOS; AF_INET6 requires IPV6_RECVTCLASS for both IPv6 and admitted mapped IPv4; any required option failure disables the endpoint | `udp4`/`udp6`/`udp` native setup, four-mark receive table, and required-option failure table |
| Managed capability projection | ECN only is newly gated; packet info/GSO are not imported | Focused capability table |
| Opt-out/setup failure/absent or malformed metadata | Usable ordinary fallback, no capability or stale mark | Focused negative cases |
| Release/reacquire/stale/Close | Current generation only; joined cleanup | Lifecycle and race cases |

**Evidence budget:** At most 10 new table-driven test functions on one native Darwin host, covering `udp4`, `udp6`, any admitted `udp` dual-stack/mapped behavior, four receive values, full-size non-GRO payloads and representative ordinary/batch outgoing values. Reuse existing sendmsg_x progress/error tests and L1 singleton-invalid-result structure, but assert Darwin EPERM is terminal without retry while message-size and other terminal results retain native behavior. At most one L1-adapter correlation guard mutation; no mutation of native grammar. One accepted `-race` gate, no repetitions, timing campaign or benchmarks. Native family skips do not satisfy a supported disposition; exact-head evidence from another Darwin host is required. One review plus at most one replacement. An unsupported outcome requires the exact failing native API/kernel evidence and removal of partial capability code, not more examples.

**TDD and preservation evidence:** Write the managed native receive/send/family qualification tables first, then add the smallest platform setup and build-tag narrowing. Preserve existing Darwin OOB, sendmsg_x qualification/disabled/fallback, selected-peer, full-datagram managed lease, capability projection, singleton error behavior and public compatibility tests. Cross-build Darwin, FreeBSD and OpenBSD to prove exactly one receive setup definition per target. A negative result records the failing characterization and lands only durable disposition/docs evidence.

**Dispatch context budget:** Supply the current D1 contract (Decision, exact slice, acceptance criteria and validation gates), program lines 1-80, ADRs 0006/0007, skill-supplied shared baselines and overlay; `managed_packet_receive_other.go`; `sys_conn_helper_darwin.go`; `sys_conn_oob.go:57-362`; `send_conn_sendmsg_x_darwin.go:49-140,196-234`; and, from merged L1, only the named managed adapter type plus its `ReadPacket`, `WritePacket`, `capabilities` and checked-singleton translator declarations, the endpoint per-family/setup and normalization-activation declarations, `managedPacketConn.WriteBatchV1`, and the `ConfigureManagedPacketIOV1` comment/setup block. Test input is bounded to `TestSendmsgXBatchSendEndToEnd`, `TestFixedPeerNativeBatch`, `TestExternalDarwinBatchWriterEngagement`, `TestExternalDarwinBatchWriterFallback`, `TestExternalDarwinManagedBatchDeadline`, `TestManagedPacketIOBatchDatagrams`, `TestManagedPacketIORegistration`, `TestOOBReaderAncillaryFailure` and the L1 table helpers directly reused by D1. Resolve exact post-L1 line ranges and re-measure this named manifest at dispatch; stop before implementation if `max(bytes/4, words*2)` exceeds 26,000 input tokens. The current dispatch manifest is recorded in the linked re-audit; historical publication-size estimates do not certify the current revision. Load no whole L1 diff, Linux implementation chronology, whole test files or unrelated Darwin performance history.

**Slice decision audit:** Splitting receive and send creates an unadvertisable half-capability; merging them preserves one capability decision. Merging D1 with L1 or F1 would couple native hosts/build tags and exceed a fresh context. L1 is a genuine blocker because it owns the shared managed representation; sendmsg_x alone is not. One PR can either land complete support or cleanly record unsupported status.

**Stop conditions:** Stop if native access is unavailable, if support requires a Darwin-specific second adapter owner, public/raw authority, sendmsg_x redesign, capability before both directions pass, another platform, or evidence beyond this finite budget. Unsupported is allowed only from native evidence, not cross-compilation or absence of a test machine.

## Acceptance criteria

- [ ] Native Darwin evidence records an exact supported or unsupported disposition for IPv4/IPv6 receive metadata, ordinary marked sends and batch marked sends.
- [ ] A supported result covers full-size non-GRO payloads, `udp4`/`udp6`/admitted dual-stack families, selected/foreign peers, singleton native-result preservation including terminal no-retry EPERM, opt-out/setup failure, malformed/absent metadata, lease reuse/revocation and terminal cleanup through the shared managed owner.
- [ ] An unsupported result leaves managed ECN false, removes partial capability code and records the exact native blocker.
- [ ] Darwin, FreeBSD and OpenBSD each cross-build with exactly one definition each of `(*managedPacketEndpoint).configureReceive` and `(*managedPacketEndpoint).managedPacketRawFactory`; public signatures, ordinary datagrams, managed capability projection, sendmsg_x progress/error semantics and raw-socket authority remain unchanged.

## Validation gates

Run focused native Darwin tests, the accepted package `-race` gate, affected-package tests, changed-package `go vet`, `go mod tidy -diff`, `git diff --check`, Darwin/FreeBSD/OpenBSD cross-builds and applicable exact-head hosted checks. Record Darwin version, kernel, architecture, Go version, `udp4`/`udp6`/`udp` family domain, `IPV6_V6ONLY` and IPv4-mapped availability.

## Operating discipline

Follow the shared review-loop and contract-closure baselines supplied by `$implement-architecture-slice`, composed with [the repository overlay](../REVIEW-LOOP.md). The supported and unsupported outcomes are both accepted; lack of native access is neither.
