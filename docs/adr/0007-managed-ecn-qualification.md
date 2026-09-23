---
status: accepted
---

# Managed ECN qualification boundary

Managed ECN support will be introduced behind the existing managed endpoint and `ConfigureManagedPacketIOV1` boundary, with no raw-socket or new public metadata authority. Linux is the first implementation and qualification target because it already has the managed receive-normalization and fallback machinery; Darwin and FreeBSD remain explicit follow-up qualification tracks. OpenBSD has no current OOB path; its completed native-feasibility study establishes IPv6-only receive/send feasibility while IPv4 receive remains unsupported through the tested API. Scoped architecture handoff is required before any OpenBSD implementation. Windows remains unsupported until an independent implementation and native qualification exist. A platform endpoint is ECN-capable only when the same managed path proves both received metadata preservation and outgoing marking across ordinary and batch sends, peer filtering, fallback, lease reuse and cleanup for every usable address family admitted by that socket. A direct registration may omit its batch callback or must synchronously forward its exact payload/OOB/destination through the exact lease under the existing general-batch result contract. Wrapper support is authority-bearing and additionally limited to synchronous exact forwarding of the lease read's buffer/range/address and checked marked-send payload/OOB plus unchanged singleton result propagation; the adapter correlates only ECN and does not inherit unrelated native capabilities or infer GRO from an ECN-only reader.

## Consequences

- The ECN umbrella issue must retain visible platform tracks so deferred platforms cannot disappear into prose.
- Existing public interfaces, ordinary datagram behavior, lease ownership, selected-peer filtering and send progress/error contracts remain stable.
- Non-GRO metadata reads must preserve full accepted UDP datagrams without read-ahead across lease release, and checked singleton marked sends must preserve native success and errors.
- Single-family endpoints qualify their admitted family; dual-family endpoints require both families and their mapped-address behavior to qualify before advertising ECN.
- Endpoint-managed setup owns the private per-family qualification record and a separate coalescing/normalization activation fact while unregistered connection behavior remains unchanged.
- Native socket-option success or cross-compilation is insufficient evidence for managed ECN capability.

## OpenBSD native feasibility disposition

The [O1 native receipt](../audits/2026-09-23-openbsd-ecn-o1/README.md) records OpenBSD 7.9/amd64 with Go 1.27.0. IPv6 `IPV6_RECVTCLASS` and per-datagram `IPV6_TCLASS` preserve Not-ECT, ECT(0), ECT(1) and CE through native UDP sockets. The representation is a kernel control message containing a native integer, parsed through Go/x/sys and x/net. IPv4 has no receive-TOS API in the tested surface; successful IPv4 `IP_TOS` control-message sends do not prove outgoing marks. Managed ECN remains false for OpenBSD. A scoped `$architecture-handoff` must define any IPv6-only implementation and qualification slice; this example-level feasibility result grants no runtime or public/raw authority.
