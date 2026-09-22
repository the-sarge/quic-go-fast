# QUIC transport

Language for QUIC transport behavior and the fork's transmission work.

## Language

**QUIC packet**: A QUIC protocol unit; protected data and handshake packets carry frames and a packet number, while Retry and Version Negotiation have different formats. Several QUIC packets can share a UDP datagram.

**UDP datagram**: A single UDP payload sent to a network destination. Distinguish it from an application DATAGRAM message and from a batch of UDP datagrams.

**Application DATAGRAM message**: Unreliable application data carried by a QUIC DATAGRAM frame. Its admission for sending does not guarantee delivery to the peer.

**Send batch**: One or more UDP datagrams grouped for a single submission to the socket-writing path. A batch is not a packet-number space or an application delivery guarantee.

**Local send capacity**: Room for additional outgoing work waiting for the socket writer. This is distinct from peer flow-control credit, congestion allowance, and application credit.

**Packet registration**: Recording a constructed outgoing QUIC packet in the transport's recovery accounting. Registration, socket-write completion, and acknowledgment by the peer are distinct events.

**Handshake MTU fallback**: Reduction of the handshake packetization budget in response to an eligible local message-size failure before handshake confirmation. It is distinct from recovery from a path that silently drops oversized traffic.

**Path generation**: An identity for the connection's current path epoch that distinguishes current feedback from feedback belonging to an earlier path. It does not create a new packet-number space.

**Packet emission**: The sequence that turns eligible protocol work into constructed packets, recovery registration and an owned handoff to a network send path. It does not imply rollback of protocol state after a local write failure.

**Incoming datagram storage**: The bytes backing a received UDP datagram. Several QUIC packet views can share those bytes while processing or awaiting decryption.

**Retained QUIC packet view**: A received QUIC packet kept for later decryption or replay; it remains distinct from the whole datagram whose bytes it shares.

**HTTP/3 exchange**: One request attempt and its response, including any overlapping request upload and response consumption. Receiving response headers does not by itself finish an exchange.

**Idle pooled HTTP/3 connection**: A reusable HTTP/3 connection with no active exchanges. A connection carrying an unfinished response or request upload is not idle.

**Coalesced receive**: A single socket read that delivers several consecutive equal-sized UDP datagrams from the same sender in one buffer, which the transport must split back into individual UDP datagrams before packet processing. It is distinct from several QUIC packets sharing one UDP datagram, and from a receive batch of separately delivered datagrams.

**Receive batch**: One or more separately delivered UDP datagrams collected in a single submission from the socket-reading path. Each member is a complete UDP datagram; a member may itself be a coalesced receive.

**Segmented send**: A single socket submission carrying a payload that the network stack splits into several equal-sized UDP datagrams before transmission. It is distinct from a send batch of separately submitted datagrams and from several QUIC packets sharing one UDP datagram.

**Reliable stream prefix**: The initial range of stream bytes that a partial stream reset still requires the receiver to be able to read before observing that reset. Local cancellation can abandon reading that range.

**External packet-I/O binding**: An explicit association between one transport, its exact supplied packet resource and the optimized operations its owner permits. It is distinct from discovering native support or acquiring responsibility for closing the resource.

**Receive-format permission**: Authority from a packet resource's owner to enable reads containing coalesced UDP datagrams during a controlled use interval. It does not itself guarantee that the socket can later be returned as an ordinary raw socket.

**Managed packet endpoint**: A reusable packet resource that preserves ordinary datagram behavior across transport-use intervals while retaining responsibility for its underlying socket and receive format.

**Packet-I/O lease**: The exclusive per-attempt packet resource borrowed from a managed packet endpoint. Ending the lease releases that interval; ending the parent endpoint ends the underlying resource.

**Fixed-peer transport**: A transport whose permitted remote endpoint is fixed before packet processing and remains fixed for its lifetime. The address restriction is distinct from authenticating the peer's identity.

**ECN metadata path**: The transport-preserved mapping between IP-level Explicit Congestion Notification bits on a UDP datagram and the QUIC packet's received ECN value or outgoing mark. Socket support alone does not establish an ECN metadata path.

**Managed ECN capability**: A managed endpoint's proven ability to receive ECN-marked datagrams and apply outgoing ECN marks through an exclusive lease while keeping native socket authority private.

**Platform ECN qualification**: Native evidence that one platform preserves a managed ECN capability across every address family admitted by the tested socket, ordinary and batched sends, peer filtering, fallback, lease reuse and terminal cleanup. A platform without a native metadata path remains unqualified rather than inheriting another platform's result.
