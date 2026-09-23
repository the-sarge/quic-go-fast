//go:build darwin || linux || freebsd || windows

package quic

import (
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/protocol"
)

// managedPacketRawConn is the sole correlation and marked-route boundary for a
// managed registration. The outer raw connection retains wrapper policy; the
// endpoint retains native metadata and socket authority.
type managedPacketRawConn struct {
	rawConn
	conn      *managedPacketConn
	config    *externalPacketIO
	direct    bool
	readMutex sync.Mutex
	sendMutex sync.Mutex
}

func newManagedPacketRawConn(base rawConn, conn *managedPacketConn, config *externalPacketIO, direct bool) rawConn {
	return &managedPacketRawConn{rawConn: base, conn: conn, config: config, direct: direct}
}

func (c *managedPacketRawConn) ReadPacket() (receivedPacket, error) {
	if err := c.conn.begin(); err != nil {
		return receivedPacket{}, err
	}
	defer c.conn.endpoint.end()

	c.readMutex.Lock()
	defer c.readMutex.Unlock()

	e := c.conn.endpoint
	op := &managedReadOperation{lease: c.conn.lease}
	e.mutex.Lock()
	if e.managedRead != nil {
		e.mutex.Unlock()
		return receivedPacket{}, errors.New("quic: concurrent managed read correlation")
	}
	if err := c.conn.checkLocked(); err != nil {
		e.mutex.Unlock()
		return receivedPacket{}, err
	}
	e.managedRead = op
	e.mutex.Unlock()

	packet, err := c.rawConn.ReadPacket()
	e.mutex.Lock()
	if e.managedRead == op {
		e.managedRead = nil
	}
	current := e.lease == c.conn.lease && !c.conn.lease.returning && !e.closed
	qualified := e.managedECN && (c.direct || c.config != nil && c.config.sendBatch != nil)
	e.mutex.Unlock()
	if err != nil {
		return receivedPacket{}, err
	}
	packet.ecn = protocol.ECNUnsupported
	if current && qualified && op.buffer == unsafe.SliceData(packet.data) && op.bufferLen >= len(packet.data) && op.n == len(packet.data) && sameManagedPacketAddr(op.addr, packet.remoteAddr) {
		packet.ecn = op.ecn
	}
	return packet, nil
}

func sameManagedPacketAddr(a, b net.Addr) bool {
	au, aok := a.(*net.UDPAddr)
	bu, bok := b.(*net.UDPAddr)
	return aok && bok && au.Port == bu.Port && au.Zone == bu.Zone && au.IP.Equal(bu.IP)
}

func (c *managedPacketRawConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	if ecn == protocol.ECNUnsupported || ecn == protocol.ECNNon {
		return c.rawConn.WritePacket(b, addr, oob, gsoSize, protocol.ECNUnsupported)
	}
	if gsoSize != 0 {
		return 0, errors.New("quic: managed packet I/O does not support segmented sends")
	}
	if !c.direct {
		return c.writeCheckedSingleton(b, addr, oob, ecn)
	}
	if err := c.conn.begin(); err != nil {
		return 0, err
	}
	defer c.conn.endpoint.end()
	e := c.conn.endpoint
	e.mutex.Lock()
	native, qualified := e.managedNative, e.managedECN
	e.mutex.Unlock()
	if native == nil || !qualified {
		return 0, errors.New("quic: managed ECN unavailable")
	}
	n, err := native.WritePacket(b, addr, oob, 0, ecn)
	if err == nil && n != len(b) {
		return n, io.ErrShortWrite
	}
	return n, err
}

func (c *managedPacketRawConn) writeCheckedSingleton(b []byte, addr net.Addr, oob []byte, ecn protocol.ECN) (int, error) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok || udpAddr == nil || c.config == nil || c.config.sendBatch == nil {
		return 0, errors.New("quic: managed ECN wrapper route unavailable")
	}
	c.sendMutex.Lock()
	defer c.sendMutex.Unlock()
	if err := c.conn.begin(); err != nil {
		return 0, err
	}
	defer c.conn.endpoint.end()

	markedOOB := appendExternalECN(oob, udpAddr, ecn)
	op := &managedSingletonOperation{lease: c.conn.lease, payload: b, oob: markedOOB, addr: udpAddr}
	e := c.conn.endpoint
	e.mutex.Lock()
	if e.managedSend != nil {
		e.mutex.Unlock()
		return 0, errors.New("quic: concurrent managed checked singleton")
	}
	e.managedSend = op
	e.mutex.Unlock()

	count, err := c.config.sendBatch([][]byte{b}, markedOOB, udpAddr)
	e.mutex.Lock()
	if e.managedSend == op {
		e.managedSend = nil
	}
	forwarded, nativeCount, nativeErr := op.forwarded, op.count, op.err
	e.mutex.Unlock()
	if !forwarded {
		if err != nil {
			return 0, err
		}
		return 0, errors.New("quic: checked singleton callback did not synchronously forward the borrowed datagram")
	}
	if count != nativeCount || !sameManagedError(err, nativeErr) {
		return 0, errors.New("quic: checked singleton callback must return the lease result unchanged")
	}
	switch {
	case count == 1 && err == nil:
		return len(b), nil
	case count == 0 && err != nil:
		return 0, err
	default:
		return 0, errors.New("quic: invalid checked singleton result")
	}
}

func sameManagedError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aType, bType := reflect.TypeOf(a), reflect.TypeOf(b)
	return aType == bType && aType.Comparable() && a == b
}

func (c *managedPacketRawConn) SetReadDeadline(t time.Time) error {
	return c.rawConn.SetReadDeadline(t)
}

func (c *managedPacketRawConn) capabilities() connCapabilities {
	cap := c.rawConn.capabilities()
	e := c.conn.endpoint
	e.mutex.Lock()
	cap.ECN = e.managedECN && (c.direct || c.config != nil && c.config.sendBatch != nil)
	cap.GRO = e.receiveCoalescing
	cap.receiveCoalescing = e.receiveState
	e.mutex.Unlock()
	cap.GSO = false
	return cap
}

func (c *managedPacketRawConn) releaseReadBuffers() {
	if reader, ok := c.rawConn.(interface{ releaseReadBuffers() }); ok {
		reader.releaseReadBuffers()
	}
}

func (c *managedPacketRawConn) packetIOConfig() *externalPacketIO { return c.config }
