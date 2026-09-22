# Managed ECN qualification program — 2026-09-22

**Program:** `QGF-ECN-20260922`
**Status:** In progress; Linux L1 complete via [#508](https://github.com/the-sarge/quic-go-fast/pull/508), with D1, F1, O1 and W1 on the frontier
**Normative scope:** Track identity, plan pointers, slice graph, frontier and binding rules
**Audit history:** [Source, existing-work and slice audit](../audits/2026-09-22-managed-ecn-handoff/README.md)

## What this is

Five platform tracks qualify managed ECN without moving native socket or lifecycle authority out of the managed endpoint. The track plans are the normative contracts; GitHub and OmniFocus contain current state and pointers. Each audited slice maps to one intended PR and one fresh implementation context. Publication of this package does not dispatch implementation.

## Outcomes that require no implementation

Socket-option success, another platform's evidence, packet-info support, segmented/coalesced I/O support, cross-compilation, promoted native methods and raw descriptor access do not establish managed ECN capability. There is no throughput campaign, public ECN metadata API, raw-socket handoff, QUIC recovery-policy change, wiremux packet-profile change or automatic capability inference for unknown wrappers.

## Tracks, dependencies and frontier

| Track | Plan | Parent issue | Blocked by | Slices | Status |
| --- | --- | --- | --- | --- | --- |
| L | [Linux managed ECN](2026-09-22-linux-managed-ecn-plan.md) | [#455](https://github.com/the-sarge/quic-go-fast/issues/455) | None | L1 | Complete via [#508](https://github.com/the-sarge/quic-go-fast/pull/508) |
| D | [Darwin managed ECN](2026-09-22-darwin-managed-ecn-plan.md) | [#456](https://github.com/the-sarge/quic-go-fast/issues/456) | L1 | D1 | FRONTIER |
| F | [FreeBSD managed ECN](2026-09-22-freebsd-managed-ecn-plan.md) | [#457](https://github.com/the-sarge/quic-go-fast/issues/457) | L1 | F1 | FRONTIER |
| O | [OpenBSD managed ECN feasibility](2026-09-22-openbsd-managed-ecn-plan.md) | [#458](https://github.com/the-sarge/quic-go-fast/issues/458) | None | O1 | FRONTIER |
| W | [Windows managed ECN feasibility](2026-09-22-windows-managed-ecn-plan.md) | [#459](https://github.com/the-sarge/quic-go-fast/issues/459) | None | W1 | FRONTIER |

L1 delivered the central managed metadata path and capability gate. D1, F1, O1 and W1 are now parallel-safe frontier slices. O1 and W1 may close their platform tracks as unsupported. A supported feasibility result requires a scoped `$architecture-handoff` update that adds an implementation slice with the proven native representation before code implementation begins.

## Rules that bind every track

The managed endpoint and exact lease remain the native socket, ancillary setup, metadata storage, generation and cleanup owner. `ConfigureManagedPacketIOV1` remains the authority boundary. A direct registration may omit its batch callback; a non-nil direct callback participates in managed ECN only when it synchronously forwards the exact payload, OOB and destination through the exact lease under the existing registered-batch definite-prefix and terminal-error contract. A wrapper that participates in managed ECN additionally declares synchronous exact forwarding: `ReadFrom` delegates with the caller-supplied backing buffer and returns the final lease read's same range/address, while the checked callback validates the destination, forwards the exact borrowed payload/OOB and returns the singleton lease result unchanged. The adapter does not promise ECN for buffering, substitution, reordering, result normalization or synthetic wrappers. A direct lease may use the endpoint's private native send path; a caller policy wrapper may carry marked output only through the checked callback and an operation-scoped singleton route that preserves native success, message-size, platform-specific permission handling and terminal-error behavior. Concurrent singleton calls retain distinct payload/OOB/generation/result association, without a guarantee against serialization. Receive metadata may cross the wrapper only when current generation, backing buffer, returned range and source address correlate to the exact final lease read; stale, absent or uncorrelated metadata never claims ECN.

Public signatures, ordinary one-datagram reads, selected-peer filtering, complete-payload borrowing, definite-prefix progress, terminal-error semantics, `QUIC_GO_DISABLE_ECN`, usable unsupported fallback and lease Close ownership remain unchanged. Non-GRO retained receive uses full-datagram storage and no batch read-ahead; a decoder is not retained solely for disabled ECN. Endpoint-managed setup privately records per-family outcomes and coalescing/normalization activation separately from retained-reader presence while preserving unregistered behavior. The adapter correlates ECN only and projects managed facts explicitly: ECN is gated, GSO remains false, GRO and `receive_enabled` reflect actual coalescing/normalization activation, and DF/other facts retain current behavior. No platform advertises ECN until both directions qualify for every usable address family admitted by the socket, including the explicitly tested IPv4-mapped mode of any dual-stack endpoint. Single-family endpoints do not depend on an unavailable other family; one failed usable family disables ECN for a dual-family endpoint.

The active architecture skills' shared review-loop and contract-closure baselines govern with [the repository execution overlay](../REVIEW-LOOP.md), [ADR 0006](0006-explicit-external-packet-io.md), [ADR 0007](0007-managed-ecn-qualification.md) and [maintained-code conventions](../agents/conventions.md). Tests and native probes are verification aids, not recursively complete products. One child maps to one PR and one OmniFocus slice task. Per implementation slice: review loop → exact-head certification and applicable hosted checks → squash merge → append-dev-journal → reconcile live issue/frontier pointers → complete that slice task.
