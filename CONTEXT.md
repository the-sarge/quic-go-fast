# QUIC transport

Language for QUIC transport behavior and the fork's transmission work.

## Language

**QUIC packet**: A QUIC protocol unit; protected data and handshake packets carry frames and a packet number, while Retry and Version Negotiation have different formats. Several QUIC packets can share a UDP datagram.

**UDP datagram**: A single UDP payload sent to a network destination. Distinguish it from an application DATAGRAM message and from a batch of UDP datagrams.

**Application DATAGRAM message**: Unreliable application data carried by a QUIC DATAGRAM frame. Its admission for sending does not guarantee delivery to the peer.

**Application goodput**: Unique useful application content delivered over a declared time interval, excluding protocol overhead, repair redundancy and repeated delivery. A transport acknowledgment or local send admission does not establish application acceptance; the application must define its useful-delivery boundary.

**Send batch**: One or more UDP datagrams grouped for a single submission to the socket-writing path. A batch is not a packet-number space or an application delivery guarantee.

**Local send capacity**: Room for additional outgoing work waiting for the socket writer. This is distinct from peer flow-control credit, congestion allowance, and application credit.

**Packet registration**: Recording a constructed outgoing QUIC packet in the transport's recovery accounting. Registration, socket-write completion, and acknowledgment by the peer are distinct events.

**Transport delivery**: Receipt of transmitted data confirmed by the peer transport's acknowledgment. It does not establish that the receiving application has consumed, verified or published useful content.

**Application-limited delivery sample**: A delivery-rate observation covering a period when the sender lacked enough eligible data to fully exercise the path. A low observed rate during that period does not establish a low path capacity.

**Packet-timed round**: A round of transport feedback whose progress is determined by acknowledgments of transmissions from a recorded boundary. It is distinct from a fixed interval of wall-clock time.

**Delivery snapshot**: The delivery-accounting state associated with an outgoing packet, used with later acknowledgment feedback to measure transport delivery over an interval. It is distinct from the packet's application payload and from a socket-write completion record.

**Send quantum**: The amount of traffic a pacing decision permits to leave together before another pacing opportunity. It is distinct from the congestion window and from a socket API's maximum batch or segmentation capacity.

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

**Path ECN validation**: Evidence from sent markings and peer feedback that a network path preserves usable ECN signaling. It is distinct from platform ECN qualification and does not establish which congestion response is appropriate.

**Validated CE feedback**: An accepted increase in the peer’s cumulative Congestion Experienced count for an identified feedback interval. It reports congestion on received packets, not packet loss, and does not identify which individual acknowledged packets were marked.

**Outstanding delivery evidence**: Information about a transmission that may still receive its first transport acknowledgment. It can remain after the transmission no longer counts toward congestion-controlled flight.

**Pending local send bytes**: Outgoing datagram bytes reserved or handed to local sending work whose submission or disposal is not yet complete. They are distinct from bytes acknowledged by the peer and from bytes still queued inside the operating system or network device.

**CE response boundary**: The transmission boundary recorded when validated congestion-experienced feedback triggers a sending reduction. It suppresses repeated reductions for feedback anchored before that boundary without identifying which individual packet was marked.

**ECN counter fence**: A feedback boundary at which all previously marked transmissions are accounted for before validating marking on a new path. It separates cumulative-counter continuity from the new path's ECN capability.
