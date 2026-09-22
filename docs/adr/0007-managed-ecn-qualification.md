---
status: accepted
---

# Managed ECN qualification boundary

Managed ECN support will be introduced behind the existing managed endpoint and `ConfigureManagedPacketIOV1` boundary, with no raw-socket or new public metadata authority. Linux is the first implementation and qualification target because it already has the managed receive-normalization and fallback machinery; Darwin and FreeBSD remain explicit follow-up qualification tracks, OpenBSD requires a separate native-feasibility decision because it has no current OOB path, and Windows remains unsupported until an independent implementation and native qualification exist. A platform endpoint is ECN-capable only when the same managed path proves both received metadata preservation and outgoing marking across ordinary and batch sends, peer filtering, fallback, lease reuse and cleanup for every usable address family admitted by that socket. A direct registration may omit its batch callback or must synchronously forward its exact payload/OOB/destination through the exact lease under the existing general-batch result contract. Wrapper support is authority-bearing and additionally limited to synchronous exact forwarding of the lease read's buffer/range/address and checked marked-send payload/OOB plus unchanged singleton result propagation; the adapter correlates only ECN and does not inherit unrelated native capabilities or infer GRO from an ECN-only reader.

## Consequences

- The ECN umbrella issue must retain visible platform tracks so deferred platforms cannot disappear into prose.
- Existing public interfaces, ordinary datagram behavior, lease ownership, selected-peer filtering and send progress/error contracts remain stable.
- Non-GRO metadata reads must preserve full accepted UDP datagrams without read-ahead across lease release, and checked singleton marked sends must preserve native success and errors.
- Single-family endpoints qualify their admitted family; dual-family endpoints require both families and their mapped-address behavior to qualify before advertising ECN.
- Endpoint-managed setup owns the private per-family qualification record and a separate coalescing/normalization activation fact while unregistered connection behavior remains unchanged.
- Native socket-option success or cross-compilation is insufficient evidence for managed ECN capability.
