package quic

import (
	"net"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// A sendConn allows sending using a simple Write() on a non-connected packet conn.
type sendConn interface {
	Write(b []byte, gsoSize uint16, ecn protocol.ECN) error
	WriteTo([]byte, net.Addr, packetInfo) error
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	ChangeRemoteAddr(addr net.Addr, info packetInfo)

	capabilities() connCapabilities
}

type remoteAddrInfo struct {
	addr net.Addr
	oob  []byte
}

type sconn struct {
	rawConn

	localAddr net.Addr
	external  *externalPacketIO

	remoteAddrInfo atomic.Pointer[remoteAddrInfo]

	logger utils.Logger

	// If GSO enabled, and we receive a GSO error for this remote address, GSO is disabled.
	// Published by the socket writer and read by connection state and packet emission.
	gotGSOError atomic.Bool
	// Used to catch the error sometimes returned by the first sendmsg call on Linux,
	// see https://github.com/golang/go/issues/63322.
	wroteFirstPacket bool

	// The platform's batched-send state; empty on platforms without a
	// batched send path.
	sconnBatchState
}

var _ sendConn = &sconn{}

func newSendConn(c rawConn, remote net.Addr, info packetInfo, logger utils.Logger) *sconn {
	localAddr := c.LocalAddr()
	if info.addr.IsValid() {
		if udpAddr, ok := localAddr.(*net.UDPAddr); ok {
			addrCopy := *udpAddr
			addrCopy.IP = info.addr.AsSlice()
			localAddr = &addrCopy
		}
	}

	oob := info.OOB()
	// increase oob slice capacity, so we can add the UDP_SEGMENT and ECN control messages without allocating
	l := len(oob)
	oob = append(oob, make([]byte, 64)...)[:l]
	sc := &sconn{
		rawConn:   c,
		localAddr: localAddr,
		logger:    logger,
		// The platform's batched-send state starts zero; only the darwin
		// sendmsg_x path populates it, from the send worker's goroutine.
		sconnBatchState: sconnBatchState{},
	}
	if ec, ok := c.(*externalPacketConn); ok {
		sc.external = ec.config
	}
	sc.remoteAddrInfo.Store(&remoteAddrInfo{
		addr: remote,
		oob:  oob,
	})
	return sc
}

func (c *sconn) Write(p []byte, gsoSize uint16, ecn protocol.ECN) error {
	ai := c.remoteAddrInfo.Load()
	err := c.writePacket(p, ai.addr, ai.oob, gsoSize, ecn)
	if err != nil && isGSOError(err) {
		// disable GSO for future calls
		c.gotGSOError.Store(true)
		if c.logger.Debug() {
			c.logger.Debugf("GSO failed when sending to %s", ai.addr)
		}
		// send out the packets one by one
		for len(p) > 0 {
			l := min(len(p), int(gsoSize))
			if err := c.writePacket(p[:l], ai.addr, ai.oob, 0, ecn); err != nil {
				return err
			}
			p = p[l:]
		}
		return nil
	}
	return err
}

func (c *sconn) writePacket(p []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) error {
	_, err := c.WritePacket(p, addr, oob, gsoSize, ecn)
	if err != nil && !c.wroteFirstPacket && isPermissionError(err) {
		_, err = c.WritePacket(p, addr, oob, gsoSize, ecn)
	}
	c.wroteFirstPacket = true
	return err
}

func (c *sconn) WriteTo(b []byte, addr net.Addr, info packetInfo) error {
	_, err := c.WritePacket(b, addr, info.OOB(), 0, protocol.ECNUnsupported)
	return err
}

func (c *sconn) capabilities() connCapabilities {
	capabilities := c.rawConn.capabilities()
	if capabilities.GSO {
		capabilities.GSO = !c.gotGSOError.Load()
	}
	return capabilities
}

func (c *sconn) ChangeRemoteAddr(addr net.Addr, info packetInfo) {
	c.remoteAddrInfo.Store(&remoteAddrInfo{
		addr: addr,
		oob:  info.OOB(),
	})
}

func (c *sconn) RemoteAddr() net.Addr { return c.remoteAddrInfo.Load().addr }
func (c *sconn) LocalAddr() net.Addr  { return c.localAddr }

func (c *sconn) batchSendAvailable() bool {
	if c.external != nil {
		addr, ok := c.remoteAddrInfo.Load().addr.(*net.UDPAddr)
		return c.external.sendBatch != nil && ok && addr != nil
	}
	return c.nativeBatchSendAvailable()
}

func (c *sconn) sendBatch(bufs [][]byte, ecn protocol.ECN) (int, error) {
	if c.external == nil {
		return c.sendNativeBatch(bufs, ecn)
	}
	ai := c.remoteAddrInfo.Load()
	addr, ok := ai.addr.(*net.UDPAddr)
	if !ok || addr == nil || c.external.sendBatch == nil {
		return 0, nil
	}
	oob := appendExternalECN(ai.oob, addr, ecn)
	n, err := c.external.sendBatch(bufs, oob, addr)
	calls := c.external.batchCalls.Add(1)
	if n >= 0 && n <= len(bufs) {
		c.external.acceptedPackets.Add(uint64(n))
	}
	if c.logger.Debug() {
		c.logger.Debugf("external_packet_io batch_calls=%d accepted_packets=%d receive_exercised=0", calls, c.external.acceptedPackets.Load())
	}
	return n, err
}
