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
