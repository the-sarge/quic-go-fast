package quic

import (
	"errors"
	"net"
	"sync"
	"time"
)

// NewManagedPacketEndpointV1 creates and owns a fresh UDP socket. The returned
// endpoint supports ordinary net.PacketConn operations and the closure acquires
// an exclusive lease on the same socket. Construction does not initialize t.
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
// ordinary datagrams only; receive coalescing is not supported. After ordinary
// establishment I/O has joined, ConfigureManagedPacketIOV1 can bind a lease to
// one transport until lease Close. Transport.Close alone does not return it.
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
	e := &managedPacketEndpoint{conn: conn}
	if socket, ok := conn.(udpMessageWriter); ok {
		e.sendBatch = newUDPBatchWriter(socket)
	}
	e.idle = sync.NewCond(&e.mutex)
	return &managedPacketConn{endpoint: e}, e.acquire, nil
}

type managedPacketEndpoint struct {
	mutex         sync.Mutex
	idle          *sync.Cond
	conn          net.PacketConn
	sendBatch     func([][]byte, []byte, *net.UDPAddr) (int, error)
	lease         *managedPacketLease
	active        int
	closed        bool
	closeErr      error
	readDeadline  time.Time
	writeDeadline time.Time
}

// Pointer identity is the generation token. It is never recycled, including
// when a lease has been returned or the endpoint has terminated.
type managedPacketLease struct {
	quic      bool
	returning bool
	done      chan struct{}
	closeErr  error
}

type managedPacketConn struct {
	endpoint *managedPacketEndpoint
	lease    *managedPacketLease // nil identifies the ordinary parent view
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
	e.lease = &managedPacketLease{done: make(chan struct{})}
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
	return c.endpoint.conn.ReadFrom(p)
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
	return c.endpoint.sendBatch(bufs, oob, addr)
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
	lease.closeErr = e.closeErr
	e.lease = nil
	close(lease.done)
	return lease.closeErr
}
