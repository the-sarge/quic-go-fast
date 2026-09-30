package quic

import (
	"errors"
	"net"
	"net/netip"

	"github.com/quic-go/quic-go/internal/protocol"
)

var errNetworkAdmissionDenied = errors.New("quic: network policy denied packet submission")

// packetPolicyConn owns admission; the underlying connection still owns
// reads, storage, capabilities and Close.
// Transport embeds this adapter so installing it needs no separate allocation.
type packetPolicyConn struct {
	rawConn
	policy  *fixedPeerPolicy
	network *networkAdmission
}

func (c *packetPolicyConn) ReadPacket() (receivedPacket, error) {
	// Admission belongs here, after native extraction/splitting and before any
	// QUIC, known-connection-ID or non-QUIC routing in Transport.handlePacket.
	for {
		p, err := c.rawConn.ReadPacket()
		if err != nil || c.allowsReceive(p) {
			return p, err
		}
		p.buffer.Release()
	}
}

func (c *packetPolicyConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	if err := c.checkSend(addr, oob); err != nil {
		return 0, err
	}
	return c.rawConn.WritePacket(b, addr, oob, gsoSize, ecn)
}

// checkSend is shared by ordinary/stateless writes and batch submissions. It
// borrows the destination and copies ancillary facts for the callback; packet
// buffers and retry stay with their owners.
func (c *packetPolicyConn) checkSend(addr net.Addr, oob []byte) error {
	if c.policy != nil && !c.policy.allows(addr) {
		return errors.New("quic: packet address does not match fixed peer")
	}
	if p := c.network; p != nil {
		remote, ok := packetEndpoint(addr)
		if !ok || !p.send(remote, p.local, append([]byte(nil), oob...)) {
			return errNetworkAdmissionDenied
		}
	}
	return nil
}

func (c *packetPolicyConn) allowsReceive(packet receivedPacket) bool {
	if c.policy != nil && !c.policy.allows(packet.remoteAddr) {
		return false
	}
	p := c.network
	if p == nil {
		return true
	}
	remote, ok := packetEndpoint(packet.remoteAddr)
	if !ok {
		return false
	}
	local := p.local
	if packet.info.addr.IsValid() {
		local = netip.AddrPortFrom(packet.info.addr, local.Port())
	}
	return p.receive(remote, local, packet.info.OOB())
}

func packetEndpoint(addr net.Addr) (netip.AddrPort, bool) {
	udp, ok := addr.(*net.UDPAddr)
	if !ok || udp == nil || udp.Port < 1 || udp.Port > 65535 {
		return netip.AddrPort{}, false
	}
	ep := udp.AddrPort()
	if !ep.IsValid() {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ep.Addr().Unmap(), ep.Port()), true
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

func (c *sconn) checkPacketSend(addr net.Addr, oob []byte) error {
	if pc, ok := c.rawConn.(*packetPolicyConn); ok {
		return pc.checkSend(addr, oob)
	}
	return nil
}

// checkPathPolicy is the attachment seam for both the original connection's
// socket and the target transport. It runs before target initialization or path
// registration. Origin policy is immutable once its send connection is published.
func (t *Transport) checkPathPolicy(origin sendConn) error {
	if sc, ok := origin.(*sconn); ok {
		if pc, ok := sc.rawConn.(*packetPolicyConn); ok {
			if pc.policy != nil {
				return errors.New("quic: fixed peer connection cannot add a path")
			}
			if pc.network != nil {
				return errors.New("quic: network restricted connection cannot add a path")
			}
		}
	}
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.policyConn.policy != nil {
		return errors.New("quic: fixed peer transport cannot accept a path")
	}
	if t.policyConn.network != nil {
		return errors.New("quic: network restricted transport cannot accept a path")
	}
	// Reserve the target while still holding the configuration lock. No setter
	// can install a fixed policy between successful admission and initialization.
	return t.beginPacketIOLocked()
}

// networkAdmission is immutable after configuration. Callbacks own verified
// route and adapter facts; the transport owns when they are evaluated.
type networkAdmission struct {
	conn                   net.PacketConn
	local                  netip.AddrPort
	receive, send          func(netip.AddrPort, netip.AddrPort, []byte) bool
	disableActiveMigration bool
}

// ConfigureNetworkAdmissionV1 installs connection-independent network admission
// on the exact non-nil pointer in Conn, once and before transport initialization.
// It starts no I/O and does not change socket ownership. Both callbacks are
// required. Conn and its binding must remain unchanged for the transport lifetime.
//
// Callbacks receive (remote, local, ancillaryData). Numeric endpoints are values,
// IPv4 addresses are unmapped, and UDP address zones are retained. On receive, local is
// the extracted destination when available, otherwise the socket binding. On
// send, local is the socket binding (possibly unspecified); ancillaryData carries
// the selected source/interface, including extracted IPv6 destination scope.
// Ancillary data is a private copy in the native
// platform format. Missing metadata confers no additional routing authority.
// Callbacks must use their adapter's verified facts, reject an unusable return
// route, be concurrency-safe and return promptly. The transport cannot infer
// routing authority from an IP address, descriptor, CID or successful decryption.
//
// Rejected input is discarded after datagram extraction, before any QUIC or
// non-QUIC dispatch. Every ordinary, segmented, probe and batched submission is
// checked again at the socket boundary; rejection returns an error without I/O.
// Receive rejection never closes an existing connection. Listener predicates
// must not pin the first client's tuple: admitted changes retain standard QUIC
// authentication and address validation. This does not authenticate a peer.
//
// This restricted profile disallows Conn.AddPath in either direction and does
// not adopt preferred addresses, while retaining their connection-ID handling.
// disableActiveMigration optionally advertises the server transport parameter;
// it does not disable processing of admitted rebinding. Default transports and
// separately configured auto-mode transports retain ordinary behavior.
func (t *Transport) ConfigureNetworkAdmissionV1(conn net.PacketConn, receive, send func(netip.AddrPort, netip.AddrPort, []byte) bool, disableActiveMigration bool) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: network admission configuration after initialization")
	}
	if t.policyConn.network != nil {
		return errors.New("quic: network admission already configured")
	}
	if !samePacketConn(conn, t.Conn) || receive == nil || send == nil {
		return errors.New("quic: network admission requires the exact socket and both callbacks")
	}
	local, ok := packetEndpoint(conn.LocalAddr())
	if !ok {
		return errors.New("quic: network admission requires a numeric UDP binding")
	}
	t.policyConn.network = &networkAdmission{conn: conn, local: local, receive: receive, send: send, disableActiveMigration: disableActiveMigration}
	return nil
}

func networkDisablesActiveMigration(conn sendConn) bool {
	if sc, ok := conn.(*sconn); ok {
		if pc, ok := sc.rawConn.(*packetPolicyConn); ok && pc.network != nil {
			return pc.network.disableActiveMigration
		}
	}
	return false
}

// fixedPeerPolicy is published before initialization under packetIO.mutex and
// immutable thereafter. The socket binding preserves the selected local path.
type fixedPeerPolicy struct {
	conn *net.UDPConn
	peer net.UDPAddr
}

// ConfigureFixedPeerV1 restricts this transport to one remote UDP endpoint and
// its current local socket. Call it once before any operation that initializes
// the transport, including Dial, Listen, WriteTo, ReadNonQUICPacket, Close, or
// using this transport as a Conn.AddPath target.
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
