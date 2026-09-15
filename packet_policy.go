package quic

import (
	"errors"
	"net"

	"github.com/quic-go/quic-go/internal/protocol"
)

// packetPolicyConn owns admission; the underlying connection still owns
// reads, storage, capabilities and Close.
// Transport embeds this adapter so installing it needs no separate allocation.
type packetPolicyConn struct {
	rawConn
	policy *fixedPeerPolicy
}

func (c *packetPolicyConn) ReadPacket() (receivedPacket, error) {
	// Admission belongs here, after native extraction/splitting and before any
	// QUIC, known-connection-ID or non-QUIC routing in Transport.handlePacket.
	for {
		p, err := c.rawConn.ReadPacket()
		if err != nil || c.policy == nil || c.policy.allows(p.remoteAddr) {
			return p, err
		}
		p.buffer.Release()
	}
}

func (c *packetPolicyConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	if err := c.checkSend(addr); err != nil {
		return 0, err
	}
	return c.rawConn.WritePacket(b, addr, oob, gsoSize, ecn)
}

// checkSend is shared by ordinary/stateless writes and batch submissions. It
// borrows only the destination; packet buffers and retry stay with their owners.
func (c *packetPolicyConn) checkSend(addr net.Addr) error {
	if c.policy != nil && !c.policy.allows(addr) {
		return errors.New("quic: packet address does not match fixed peer")
	}
	return nil
}

func (p *fixedPeerPolicy) allows(addr net.Addr) bool {
	peer, ok := addr.(*net.UDPAddr)
	return ok && peer != nil && peer.Port == p.peer.Port &&
		(peer.Zone == p.peer.Zone || p.peer.Zone == "") && peer.IP.Equal(p.peer.IP)
}

func (c *packetPolicyConn) releaseReadBuffers() {
	if reader, ok := c.rawConn.(interface{ releaseReadBuffers() }); ok {
		reader.releaseReadBuffers()
	}
}

// packetIOConn removes only our admission adapter for existing capability discovery.
// In particular, it never unwraps an external registration or a caller wrapper.
func packetIOConn(c rawConn) rawConn {
	if pc, ok := c.(*packetPolicyConn); ok {
		return pc.rawConn
	}
	return c
}

func (c *sconn) checkPacketSend(addr net.Addr) error {
	if pc, ok := c.rawConn.(*packetPolicyConn); ok {
		return pc.checkSend(addr)
	}
	return nil
}

// checkPathPolicy is the attachment seam for both the original connection's
// socket and the target transport. It runs before target initialization or path
// registration. Origin policy is immutable once its send connection is published.
func (t *Transport) checkPathPolicy(origin sendConn) error {
	if sc, ok := origin.(*sconn); ok {
		if pc, ok := sc.rawConn.(*packetPolicyConn); ok && pc.policy != nil {
			return errors.New("quic: fixed peer connection cannot add a path")
		}
	}
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.policyConn.policy != nil {
		return errors.New("quic: fixed peer transport cannot accept a path")
	}
	return nil
}

// fixedPeerPolicy is published before initialization under packetIO.mutex and
// immutable thereafter. The socket binding preserves the selected local path.
type fixedPeerPolicy struct {
	conn *net.UDPConn
	peer net.UDPAddr
}

// ConfigureFixedPeerV1 restricts this transport to one remote UDP endpoint and
// its current local socket. Call it once before Dial, Listen, WriteTo or Close.
// Conn must be a direct, non-nil *net.UDPConn and must not subsequently change.
// Wrappers and managed leases are unsupported. The peer must have a valid,
// non-unspecified IP and a port in [1, 65535]; its address is copied.
//
// IPv4 and IPv4-mapped addresses match. An observed IPv6 zone may match an
// unscoped peer, but a scoped peer requires the same zone. Foreign packets are
// discarded before routing; foreign output and alternate local paths fail.
// This does not authenticate a peer or change socket Close ownership.
func (t *Transport) ConfigureFixedPeerV1(peer *net.UDPAddr) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: fixed peer configuration after initialization")
	}
	if t.policyConn.policy != nil {
		return errors.New("quic: fixed peer already configured")
	}
	conn, ok := t.Conn.(*net.UDPConn)
	if !ok || conn == nil {
		return errors.New("quic: fixed peer requires a direct UDP socket")
	}
	if peer == nil || peer.IP.To16() == nil || peer.IP.IsUnspecified() || peer.Port < 1 || peer.Port > 65535 {
		return errors.New("quic: invalid fixed peer address")
	}
	copyPeer := *peer
	copyPeer.IP = append(net.IP(nil), peer.IP...)
	t.policyConn.policy = &fixedPeerPolicy{conn: conn, peer: copyPeer}
	return nil
}
