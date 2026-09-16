# R03 raw socket handback decision

## Decision and boundary

Reject raw socket return after receive coalescing for this program. Reusable acceleration uses the lifetime-stable managed endpoint and its exclusive leases. Borrowed raw sockets retain ordinary receive; explicitly permitted disposal-guaranteed paths retain coalescing. No detach operation, restoration protocol or new implementation children are proposed.

This completes the negative outcome allowed by [R03](../adr/2026-09-15-external-packet-io-plan.md#r03--decide-raw-handback-feasibility). It is a bounded feasibility and support decision, not a universal impossibility claim about every kernel, provider or future API. The input domain is an exclusively used UDP socket with potentially queued coalesced data, including unrelated datagrams that must survive handback. Joining local I/O does not control remote senders. Native kernel/Winsock semantics own the queued representation; the existing endpoint decoder owns its supported application interpretation.

## Source-backed hypotheses

Inspection date: 2026-09-16. The common candidate is: join local readers and writers, disable coalescing with a checked option call, restore logical deadlines, then return the same socket for ordinary datagram reads without draining or discarding traffic. It is plausible only if disabling establishes ordinary semantics for already queued data as well as subsequent arrivals.

### Linux

In upstream Linux v6.8, commit `e8f897f4afef0031fe618a8e94127a0934896aba`, [`udp_lib_setsockopt`'s `UDP_GRO` case](https://github.com/torvalds/linux/blob/e8f897f4afef0031fe618a8e94127a0934896aba/net/ipv4/udp.c#L2727-L2736) changes `GRO_ENABLED` and `ACCEPT_L4` without rewriting queued buffers. [`udp_queue_rcv_skb`](https://github.com/torvalds/linux/blob/e8f897f4afef0031fe618a8e94127a0934896aba/net/ipv4/udp.c#L2180-L2200) segments unexpected aggregates on ingress. [`__skb_recv_udp` and `udp_recvmsg`](https://github.com/torvalds/linux/blob/e8f897f4afef0031fe618a8e94127a0934896aba/net/ipv4/udp.c#L1688-L1882) dequeue the stored buffer and copy its payload, truncating to the caller's capacity; they do not retroactively segment it. GRO ancillary output is conditional on the current `GRO_ENABLED` flag. Thus an already queued aggregate can lose its segment metadata after disabling without becoming separate datagrams.

The [IPv6 receive path](https://github.com/torvalds/linux/blob/e8f897f4afef0031fe618a8e94127a0934896aba/net/ipv6/udp.c#L322-L418) uses the same dequeue helper and metadata flag; its [segmentation boundary is also ingress](https://github.com/torvalds/linux/blob/e8f897f4afef0031fe618a8e94127a0934896aba/net/ipv6/udp.c#L772-L793). This source inspection rejects the candidate before experiment. It does not identify the exact patched source of the R01-L Ubuntu 6.8.0-134-generic venue or claim new native qualification there.

### Windows

Microsoft's [UDP socket option contract](https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-udp-socket-options) defines zero as no coalescing and describes `UDP_COALESCED_INFO` through a control-capable receive API. It does not specify that setting zero rewrites messages already queued or supplies a completion barrier for that conversion. The [type-safe setter](https://learn.microsoft.com/en-us/windows/win32/api/ws2tcpip/nf-ws2tcpip-wsasetudprecvmaxcoalescedsize) documents setting the maximum, not a raw-return protocol. Absence of that guarantee is an evidence limitation, not evidence that Windows necessarily behaves like Linux.

The [hardware URO contract](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/udp-rsc-offload) describes resegmentation for sockets that do not opt in and completion of outstanding indications when disabling at the NIC/NDIS layer. Those are different boundaries from a socket-level change applied after application data has queued. NIC-wide control is outside this slice and cannot substitute for an application socket handback barrier. No source-backed bounded preservation procedure is established for the qualified Windows Server 2025 domain.

## Finite disposition

| Candidate or state | Disposition |
| --- | --- |
| Linux: disable and return with an aggregate already queued | Rejected by the inspected queue/receive implementation; resetting the option is not normalization. |
| Windows: disable and return with messages already queued | Rejected for supported use: the inspected public contracts do not establish queued-message conversion or a preservation barrier. Native behavior remains unmeasured by R03. |
| Drain while coalescing remains enabled | Rejected: remote traffic can keep arriving; stopping on a count or deadline leaves unknown residue, while draining until empty has no admitted bound. |
| Disable, then drain | Rejected: it consumes traffic that belongs to the subsequent user; Linux can also withhold the segment metadata needed to interpret old aggregates. No preserving raw-queue replacement operation was established. |
| Peek, inspect queue size, or wait for apparent emptiness | Rejected as a handback guarantee: observing queued data neither converts it nor establishes a barrier against in-flight arrivals. An ordinary first message does not classify later queued messages. |
| Buffer and replay normalized data through an adapter | Already the managed ownership route; returning such an adapter is not returning the raw socket. |
| Replace, shut down or close the socket | Rejected as same-socket reuse; terminal disposal remains valid only for its separately authorized ownership path. |
| Failed option/deadline restoration | Cannot be suppressed to report successful raw return. No new recovery implementation is authorized. |

For Windows specifically, [FIONREAD](https://learn.microsoft.com/en-us/windows/win32/winsock/winsock-ioctls#fionread) reports available bytes for a datagram socket, not a normalization certificate. [`recvfrom`](https://learn.microsoft.com/en-us/windows/win32/api/winsock/nf-winsock-recvfrom) consumes the first message and loses excess UDP data on truncation; `MSG_PEEK` leaves the message queued. Neither supplies the missing preservation operation.

**Experiment disposition:** Zero R03 native experiments on either platform. The conditional experiment gate was evaluated, not waived for missing access: source inspection found no plausible bounded procedure within the accepted same-socket, unrelated-data-preserving contract. A trial of option toggling alone cannot supply the missing queue/arrival contract. No new native success, stress, performance, restoration-error or OS-wide guarantee is claimed. A distinct corrected hypothesis would require its own accepted scope before reopening this decision.

## Supported alternative and preservation

At fork base `84c8cf3d67e14ad6284419bdc1bfcb37370eb3a8`, [`managed_packet_receive_linux.go`](../../managed_packet_receive_linux.go) and [`managed_packet_receive_windows.go`](../../managed_packet_receive_windows.go) install the existing decoder before enabling coalescing. [`managed_packet_endpoint.go`](../../managed_packet_endpoint.go) retains that decoder across leases, serializes normalized reads, joins active I/O, restores logical deadlines and makes restoration failure terminal. Lease Close disposes already consumed QUIC storage; it leaves kernel-queued data for subsequent normalized reads. The endpoint exposes no raw descriptor or detach path.

The existing [R01-L receipt](2026-09-16-r01-l-managed-receive.md) covers Linux IPv4/IPv6 queued handback and the [R01-W receipt](2026-09-16-r01-w-managed-receive.md) covers a real queued Windows aggregate across ordinary endpoint and next-lease reads. Those receipts establish the working alternative in their recorded native domains; neither is evidence for disabling coalescing or raw return. Their source, tests and archived bytes remain unchanged.

R03 changes only process/traceability metadata: this decision, its design disposition and the fork's completion/frontier state. No maintained verification aid, runtime enforcement owner, persisted authority, restart format, concurrency seam or platform algorithm changes. Contract closure is not triggered for this documentation decision. Untested native raw-return behavior remains explicitly unsupported, rather than a new qualification obligation. E02 still requires E02-W; R03 completion alone makes no successor ready. Live cross-repository readiness belongs to the [program tracker](https://github.com/GridSwarm/wiremux/issues/1540).
