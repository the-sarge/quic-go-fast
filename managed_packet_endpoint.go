package quic

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/protocol"
)

// NewManagedPacketEndpointV1 creates and owns a fresh UDP socket. The returned
// endpoint supports ordinary net.PacketConn operations and the closure acquires
// an exclusive lease on the same socket. Construction does not initialize t.
//
// Receive and send socket buffers are configured best-effort once, before the
// endpoint is returned, using the platform buffer policy. Failures are nonfatal
// and remain available in managed transport diagnostics. Queue limits persist
// across leases; they are not guarantees of memory consumption or throughput.
//
// Acquisition fails while parent I/O is active or another lease is held; it
// never cancels parent I/O. During a lease, parent packet and deadline operations
// fail. LocalAddr remains available. Each lease starts with the parent's logical
// deadlines. Lease deadline changes are discarded on release.
//
// Lease Close revokes the lease, interrupts and joins its active I/O, restores
// the parent's deadlines, and permits another acquisition. Old leases remain
// closed. Endpoint Close is terminal, interrupts and joins parent or lease I/O,
// and closes the socket. A failure to interrupt or restore deadlines terminates
// the endpoint and is returned by Close. Close is safe to call concurrently.
//
// The endpoint and leases expose no raw socket or descriptor. They provide
// ordinary datagrams only. Linux managed registration may retain private ECN
// metadata I/O while public reads remain ordinary datagrams; Windows and Linux
// registration install persistent receive normalization before enabling
// coalescing. Retained normalization remains across leases to decode queued
// kernel data. Lease Close discards already consumed QUIC receive storage.
// Normalized reads copy at most len(p) bytes; an oversized datagram is truncated
// without a short-buffer error.
// After ordinary establishment I/O has joined, ConfigureManagedPacketIOV1 can
// bind a lease to one transport until lease Close. Transport.Close alone does
// not return it.
func (t *Transport) NewManagedPacketEndpointV1(network string, laddr *net.UDPAddr) (net.PacketConn, func() (net.PacketConn, error), error) {
	conn, err := net.ListenUDP(network, laddr)
	if err != nil {
		return nil, nil, err
	}
	return newManagedPacketEndpoint(conn)
}

// The socket interface is the native I/O boundary. Production always supplies a
// newly created UDP socket; only the endpoint owns its deadlines and lifetime.
func newManagedPacketEndpoint(conn net.PacketConn) (net.PacketConn, func() (net.PacketConn, error), error) {
	// Complete both directions before any view can perform ordinary I/O.
	receiveErr := setReceiveBuffer(conn)
	sendErr := setSendBuffer(conn)
	e := &managedPacketEndpoint{conn: conn, buffers: inspectManagedBuffers(conn, receiveErr, sendErr)}
	warnBufferSize(managedBufferWarning(receiveErr, sendErr))
	if socket, ok := conn.(udpMessageWriter); ok {
		e.sendBatch = newUDPBatchWriter(socket)
	}
	e.idle = sync.NewCond(&e.mutex)
	return &managedPacketConn{endpoint: e}, e.acquire, nil
}

type managedPacketEndpoint struct {
	mutex             sync.Mutex
	readMutex         sync.Mutex
	receiver          managedPacketReceiver
	managedNative     rawConn
	managedRead       *managedReadOperation
	managedSend       *managedSingletonOperation
	managedECN        bool
	managedECNSetup   managedECNQualification
	receiveCoalescing bool
	receiveState      receiveCoalescingState
	idle              *sync.Cond
	conn              net.PacketConn
	buffers           managedBufferSetup
	sendBatch         func([][]byte, []byte, *net.UDPAddr) (int, error)
	lease             *managedPacketLease
	active            int
	closed            bool
	closeErr          error
	readDeadline      time.Time
	writeDeadline     time.Time
}

type managedECNQualification struct {
	qualified    bool
	admittedIPv4 bool
	admittedIPv6 bool
	ipv4Mapped   bool
	ipv6Only     bool
	receiveIPv4  bool
	receiveIPv6  bool
	sendIPv4     bool
	sendIPv6     bool
	failedFamily string
}

type managedReadOperation struct {
	lease     *managedPacketLease
	buffer    *byte
	bufferLen int
	n         int
	addr      net.Addr
	ecn       protocol.ECN
}

type managedSingletonOperation struct {
	lease     *managedPacketLease
	payload   []byte
	oob       []byte
	addr      *net.UDPAddr
	forwarded bool
	count     int
	err       error
}

// The endpoint retains this decoder across generations. Only serialized reads
// or cleanup after all I/O has joined may touch its bounded receive storage.
type managedPacketReceiver interface {
	ReadPacket() (receivedPacket, error)
	releaseReadBuffers()
}

// Pointer identity is the generation token. It is never recycled, including
// when a lease has been returned or the endpoint has terminated.
type managedPacketLease struct {
	quic         bool
	returning    bool
	done         chan struct{}
	closeErr     error
	readDeadline time.Time
}

type managedPacketConn struct {
	endpoint *managedPacketEndpoint
	lease    *managedPacketLease // nil identifies the ordinary parent view
}

type managedPacketIOSetup struct {
	buffers           *managedBufferSetup
	receiveCoalescing bool
	receiveState      receiveCoalescingState
	ecn               managedECNQualification
	rawFactory        func(rawConn, *externalPacketIO) rawConn
}

func (e *managedPacketEndpoint) acquire() (net.PacketConn, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.closed {
		return nil, net.ErrClosed
	}
	if e.lease != nil || e.active != 0 {
		return nil, errors.New("quic: managed packet endpoint busy")
	}
	e.lease = &managedPacketLease{done: make(chan struct{}), readDeadline: e.readDeadline}
	return &managedPacketConn{endpoint: e, lease: e.lease}, nil
}

// checkLocked is the single admission guard for packet and deadline operations.
func (c *managedPacketConn) checkLocked() error {
	e := c.endpoint
	if e.closed || (c.lease != nil && (e.lease != c.lease || c.lease.returning)) {
		return net.ErrClosed
	}
	if c.lease == nil && e.lease != nil {
		return errors.New("quic: managed packet endpoint leased")
	}
	return nil
}

func (c *managedPacketConn) bindQUIC() (managedPacketIOSetup, error) {
	e := c.endpoint
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if err := c.checkLocked(); err != nil {
		return managedPacketIOSetup{}, err
	}
	if c.lease.quic || e.active != 0 {
		return managedPacketIOSetup{}, errors.New("quic: managed packet lease already bound or I/O active")
	}
	if err := e.configureReceive(); err != nil {
		return managedPacketIOSetup{}, err
	}
	c.lease.quic = true
	return managedPacketIOSetup{
		buffers:           &e.buffers,
		receiveCoalescing: e.receiveCoalescing,
		receiveState:      e.receiveState,
		ecn:               e.managedECNSetup,
		rawFactory:        e.managedPacketRawFactory(c),
	}, nil
}

func (c *managedPacketConn) begin() error {
	e := c.endpoint
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	e.active++
	return nil
}

func (e *managedPacketEndpoint) end() {
	e.mutex.Lock()
	e.active--
	if e.active == 0 {
		e.idle.Broadcast()
	}
	e.mutex.Unlock()
}

func (c *managedPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if err := c.begin(); err != nil {
		return 0, nil, err
	}
	defer c.endpoint.end()
	e := c.endpoint
	e.readMutex.Lock()
	defer e.readMutex.Unlock()
	// A read waiting for serialization is active I/O too. Revocation must
	// prevent it from consuming buffered data after the current reader exits.
	e.mutex.Lock()
	err := c.checkLocked()
	deadline := e.readDeadline
	if c.lease != nil {
		deadline = c.lease.readDeadline
	}
	if err == nil && e.receiver != nil && !deadline.IsZero() && !time.Now().Before(deadline) {
		err = &net.OpError{Op: "read", Net: e.conn.LocalAddr().Network(), Source: e.conn.LocalAddr(), Err: os.ErrDeadlineExceeded}
	}
	e.mutex.Unlock()
	if err != nil {
		return 0, nil, err
	}
	if e.receiver == nil {
		return e.conn.ReadFrom(p)
	}
	packet, err := e.receiver.ReadPacket()
	if err != nil {
		return 0, nil, err
	}
	defer packet.buffer.Release()
	addr := packet.remoteAddr
	if udp, ok := addr.(*net.UDPAddr); ok {
		// Coalesced siblings share their internal source. Public callers may mutate
		// the returned address, so detach it before crossing that boundary.
		cloned := *udp
		cloned.IP = append(net.IP(nil), udp.IP...)
		addr = &cloned
	}
	n := copy(p, packet.data)
	e.mutex.Lock()
	if op := e.managedRead; op != nil && op.lease == c.lease && e.lease == c.lease {
		op.buffer = unsafe.SliceData(p)
		op.bufferLen = len(p)
		op.n = n
		op.addr = addr
		op.ecn = packet.ecn
	}
	e.mutex.Unlock()
	return n, addr, nil
}

func (c *managedPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if err := c.begin(); err != nil {
		return 0, err
	}
	defer c.endpoint.end()
	return c.endpoint.conn.WriteTo(p, addr)
}

// WriteBatchV1 submits complete UDP datagrams through the endpoint's private
// writer. It has UDPBatchWriterV1's prefix/error and synchronous-buffer contract.
// Calls are generation checked and joined by Close, including concurrent calls.
// Only leases have batch authority; the ordinary parent rejects this method.
func (c *managedPacketConn) WriteBatchV1(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
	if c.lease == nil {
		return 0, errors.New("quic: batch writing requires a managed packet lease")
	}
	if err := c.begin(); err != nil {
		return 0, err
	}
	defer c.endpoint.end()
	e := c.endpoint
	e.mutex.Lock()
	op := e.managedSend
	matched := op != nil && op.lease == c.lease && !op.forwarded && len(bufs) == 1 && sameManagedBorrowedSlice(op.payload, bufs[0]) && sameManagedBorrowedSlice(op.oob, oob) && op.addr == addr
	if matched {
		op.forwarded = true
		native, qualified := e.managedNative, e.managedECN
		e.mutex.Unlock()
		count, err := 0, errors.New("quic: managed ECN unavailable")
		if native != nil && qualified {
			n, writeErr := native.WritePacket(bufs[0], addr, oob, 0, protocol.ECNUnsupported)
			switch {
			case writeErr != nil:
				err = writeErr
			case n != len(bufs[0]):
				err = io.ErrShortWrite
			default:
				count, err = 1, nil
			}
		}
		e.mutex.Lock()
		op.count, op.err = count, err
		e.mutex.Unlock()
		return count, err
	}
	sendBatch := e.sendBatch
	e.mutex.Unlock()
	if sendBatch == nil {
		return 0, errors.New("quic: managed batch writing unavailable")
	}
	return sendBatch(bufs, oob, addr)
}

func sameManagedBorrowedSlice(a, b []byte) bool {
	return len(a) == len(b) && unsafe.SliceData(a) == unsafe.SliceData(b)
}

func (c *managedPacketConn) LocalAddr() net.Addr { return c.endpoint.conn.LocalAddr() }

func (c *managedPacketConn) SetDeadline(t time.Time) error {
	return c.setDeadline(t, true, true)
}

func (c *managedPacketConn) SetReadDeadline(t time.Time) error {
	return c.setDeadline(t, true, false)
}

func (c *managedPacketConn) SetWriteDeadline(t time.Time) error {
	return c.setDeadline(t, false, true)
}

func (c *managedPacketConn) setDeadline(t time.Time, read, write bool) error {
	e := c.endpoint
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	var err error
	switch {
	case read && write:
		err = e.conn.SetDeadline(t)
	case read:
		err = e.conn.SetReadDeadline(t)
	default:
		err = e.conn.SetWriteDeadline(t)
	}
	if err != nil {
		return err
	}
	if c.lease == nil {
		if read {
			e.readDeadline = t
		}
		if write {
			e.writeDeadline = t
		}
	} else if read {
		c.lease.readDeadline = t
	}
	return nil
}

// terminateLocked revokes every view before closing the socket. Callers still
// join active operations before reporting completion.
func (e *managedPacketEndpoint) terminateLocked(cause error) {
	if !e.closed {
		e.closed = true
		e.closeErr = errors.Join(cause, e.conn.Close())
	}
}

func (c *managedPacketConn) Close() error {
	e := c.endpoint
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if c.lease == nil {
		e.terminateLocked(nil)
		for e.active != 0 {
			e.idle.Wait()
		}
		if e.receiver != nil {
			e.receiver.releaseReadBuffers()
		}
		return e.closeErr
	}
	lease := c.lease
	if lease.returning {
		e.mutex.Unlock()
		<-lease.done
		e.mutex.Lock()
		return lease.closeErr
	}
	lease.returning = true
	if !e.closed {
		if err := e.conn.SetDeadline(time.Now().Add(-time.Second)); err != nil {
			e.terminateLocked(err)
		}
	}
	for e.active != 0 {
		e.idle.Wait()
	}
	if !e.closed {
		if err := e.conn.SetReadDeadline(e.readDeadline); err != nil {
			e.terminateLocked(err)
		} else if err := e.conn.SetWriteDeadline(e.writeDeadline); err != nil {
			e.terminateLocked(err)
		}
	}
	if e.receiver != nil && (lease.quic || e.closed) {
		e.receiver.releaseReadBuffers()
	}
	lease.closeErr = e.closeErr
	e.lease = nil
	close(lease.done)
	return lease.closeErr
}
