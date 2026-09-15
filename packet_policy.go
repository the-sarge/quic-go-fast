package quic

import (
	"net"

	"github.com/quic-go/quic-go/internal/protocol"
)

// packetPolicyConn is the transport-owned packet admission seam. It is inert:
// the underlying connection still owns reads, storage, capabilities and Close.
// P01-B can activate policy here without replacing socket or send-worker state.
// Transport embeds this adapter so installing it needs no separate allocation.
type packetPolicyConn struct {
	rawConn
}

func (c *packetPolicyConn) ReadPacket() (receivedPacket, error) {
	// Admission belongs here, after native extraction/splitting and before any
	// QUIC, known-connection-ID or non-QUIC routing in Transport.handlePacket.
	return c.rawConn.ReadPacket()
}

func (c *packetPolicyConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	if err := c.checkSend(addr); err != nil {
		return 0, err
	}
	return c.rawConn.WritePacket(b, addr, oob, gsoSize, ecn)
}

// checkSend is shared by ordinary/stateless writes and batch submissions. It
// borrows only the destination; packet buffers and retry stay with their owners.
func (*packetPolicyConn) checkSend(net.Addr) error { return nil }

func (c *packetPolicyConn) releaseReadBuffers() {
	if reader, ok := c.rawConn.(interface{ releaseReadBuffers() }); ok {
		reader.releaseReadBuffers()
	}
}

// packetIOConn removes only our inert adapter for existing capability discovery.
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
// registration. No transport or connection acquires policy state in this slice.
func (*Transport) checkPathPolicy(sendConn) error { return nil }
