package quicproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

const desiredBufferSize = 2 << 20 // 2 MiB

// Connection is a UDP connection
type connection struct {
	ClientAddr *net.UDPAddr // Address of the client
	ServerAddr *net.UDPAddr // Address of the server

	mx         sync.Mutex
	ServerConn *net.UDPConn // UDP connection to server

	incomingPackets chan packetEntry
	incomingDone    chan struct{}
	originalConn    *net.UDPConn
	// All sockets accepted for this client; guarded by Proxy.mutex.
	sockets map[*net.UDPConn]struct{}

	Incoming *queue
	Outgoing *queue
}

func (c *connection) queuePacket(t monotime.Time, b []byte, shutdown <-chan struct{}) bool {
	select {
	case c.incomingPackets <- packetEntry{Time: t, Raw: b}:
		return true
	case <-c.incomingDone:
	case <-shutdown:
	}
	return false
}

func (c *connection) SwitchConn(conn *net.UDPConn) {
	c.mx.Lock()
	defer c.mx.Unlock()

	old := c.ServerConn
	old.SetReadDeadline(time.Now())
	c.ServerConn = conn
}

func (c *connection) GetServerConn() *net.UDPConn {
	c.mx.Lock()
	defer c.mx.Unlock()

	return c.ServerConn
}

// Direction is the direction a packet is sent.
type Direction int

const (
	// DirectionIncoming is the direction from the client to the server.
	DirectionIncoming Direction = iota
	// DirectionOutgoing is the direction from the server to the client.
	DirectionOutgoing
	// DirectionBoth is both incoming and outgoing
	DirectionBoth
)

type packetEntry struct {
	Time monotime.Time
	Raw  []byte
}

type queue struct {
	sync.Mutex

	timer   *time.Timer
	Packets []packetEntry // sorted by the packetEntry.Time
}

func newQueue() *queue {
	// there's no way to initialize a time.Timer that's not running
	return &queue{timer: time.NewTimer(24 * time.Hour)}
}

func (q *queue) Add(e packetEntry) {
	q.Lock()
	defer q.Unlock()

	if len(q.Packets) == 0 {
		q.Packets = append(q.Packets, e)
		q.timer.Reset(monotime.Until(e.Time))
		return
	}

	// The packets slice is sorted by the packetEntry.Time.
	// We only need to insert the packet at the correct position.
	idx := slices.IndexFunc(q.Packets, func(p packetEntry) bool {
		return p.Time.After(e.Time)
	})
	if idx == -1 {
		q.Packets = append(q.Packets, e)
	} else {
		q.Packets = slices.Insert(q.Packets, idx, e)
	}
	if idx == 0 {
		q.timer.Reset(monotime.Until(q.Packets[0].Time))
	}
}

func (q *queue) Get() []byte {
	q.Lock()
	raw := q.Packets[0].Raw
	q.Packets = q.Packets[1:]
	if len(q.Packets) > 0 {
		q.timer.Reset(monotime.Until(q.Packets[0].Time))
	}
	q.Unlock()
	return raw
}

func (q *queue) Timer() <-chan time.Time { return q.timer.C }

func (q *queue) Close() {
	q.timer.Stop()
	q.Packets = nil
}

func (d Direction) String() string {
	switch d {
	case DirectionIncoming:
		return "Incoming"
	case DirectionOutgoing:
		return "Outgoing"
	case DirectionBoth:
		return "both"
	default:
		panic("unknown direction")
	}
}

// Is says if one direction matches another direction.
// For example, incoming matches both incoming and both, but not outgoing.
func (d Direction) Is(dir Direction) bool {
	if d == DirectionBoth || dir == DirectionBoth {
		return true
	}
	return d == dir
}

// DropCallback is a callback that determines which packet gets dropped.
// It must eventually return and must not call or wait for its own proxy's Close.
type DropCallback func(dir Direction, from, to net.Addr, packet []byte) bool

// DelayCallback is a callback that determines how much delay to apply to a packet.
// It must eventually return and must not call or wait for its own proxy's Close.
// Calling SwitchConn while the proxy is running is supported.
type DelayCallback func(dir Direction, from, to net.Addr, packet []byte) time.Duration

// SocketEvent observes a completed proxy socket operation, not a forwarding
// decision. Data is borrowed and is valid only during the synchronous callback.
// Observers must not mutate Data. They must eventually return and must not
// call or wait for their own proxy's Close.
type SocketEvent struct {
	Direction Direction
	Operation string
	Started   time.Time // write start; zero for reads
	From, To  net.Addr
	Data      []byte
	N         int
	Err       error
}

// Proxy is a QUIC proxy that can drop and delay packets.
type Proxy struct {
	// Conn is the caller-owned UDP listening socket. Supply it without active
	// deadlines. From successful Start until Close returns, the proxy exclusively
	// uses its I/O and deadlines; the caller may still close it to abort I/O.
	// Close leaves an otherwise-open socket open and clears shutdown deadlines.
	// Socket buffer settings are not restored.
	Conn *net.UDPConn

	// ServerAddr is the address of the server that the proxy forwards packets to.
	ServerAddr *net.UDPAddr

	// DropPacket is a callback that determines which packet gets dropped.
	DropPacket DropCallback

	// DelayPacket is a callback that determines how much delay to apply to a packet.
	DelayPacket DelayCallback

	// ObserveSocket optionally records reads and actual forwarding write results.
	// Set before Start. It may be called concurrently by both directions.
	ObserveSocket func(SocketEvent)

	closeChan chan struct{}
	closeOnce sync.Once
	closeErr  error
	workers   sync.WaitGroup
	closing   bool // guarded by mutex
	logger    utils.Logger

	// mapping from client addresses (as host:port) to connection
	mutex      sync.Mutex
	clientDict map[string]*connection
}

// Start begins the exclusive-use interval documented on Conn.
// A proxy instance cannot be restarted. Failed setup starts no workers.
func (p *Proxy) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.closeChan != nil {
		return errors.New("proxy already started")
	}

	if err := p.Conn.SetReadBuffer(desiredBufferSize); err != nil {
		return err
	}
	if err := p.Conn.SetWriteBuffer(desiredBufferSize); err != nil {
		return err
	}

	p.clientDict = make(map[string]*connection)
	p.closeChan = make(chan struct{})
	p.logger = utils.DefaultLogger.WithPrefix("proxy")
	p.logger.Debugf("Starting UDP Proxy %s <-> %s", p.Conn.LocalAddr(), p.ServerAddr)
	p.workers.Go(func() { p.runProxy() })
	return nil
}

// SwitchConn switches the connection for a client,
// identified by the address that the client is sending from.
// A rejected switch leaves conn owned by the caller. An accepted active socket
// is closed at shutdown; retired caller-supplied sockets remain caller-owned.
func (p *Proxy) SwitchConn(clientAddr *net.UDPAddr, conn *net.UDPConn) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.closing {
		return net.ErrClosed
	}
	if err := conn.SetReadBuffer(desiredBufferSize); err != nil {
		return err
	}
	if err := conn.SetWriteBuffer(desiredBufferSize); err != nil {
		return err
	}
	c, ok := p.clientDict[clientAddr.String()]
	if !ok {
		return fmt.Errorf("client %s not found", clientAddr)
	}
	c.SwitchConn(conn)
	c.sockets[conn] = struct{}{}
	return nil
}

// Close stops admission, interrupts I/O, and joins all proxy workers and
// callbacks. Concurrent and repeated calls wait for the same completed shutdown.
// Pending delayed packets may be discarded. Callbacks must eventually return
// and must not synchronously call or wait for this method on their own proxy.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		p.mutex.Lock()
		p.closing = true
		close(p.closeChan)
		p.mutex.Unlock()

		record := func(err error) {
			if err != nil && !errors.Is(err, net.ErrClosed) {
				p.closeErr = errors.Join(p.closeErr, err)
			}
		}
		// Both reads and writes can be pending on the borrowed listener.
		record(p.Conn.SetDeadline(time.Now()))
		for _, c := range p.clientDict {
			active := c.GetServerConn()
			for socket := range c.sockets {
				if socket == c.originalConn || socket == active {
					record(socket.Close())
				} else {
					// A worker may still hold this retired caller-owned socket.
					// Interrupt its I/O without taking ownership of closing it.
					record(socket.SetDeadline(time.Now()))
				}
			}
		}
		// No lock needed by callbacks is held during the join. Admission registered
		// every worker under mutex before shutdown could begin waiting.
		p.workers.Wait()
		for _, c := range p.clientDict {
			c.Incoming.Close()
			c.Outgoing.Close()
			for len(c.incomingPackets) > 0 {
				<-c.incomingPackets
			}
			active := c.GetServerConn()
			for socket := range c.sockets {
				if socket != c.originalConn && socket != active {
					record(socket.SetDeadline(time.Time{}))
				}
			}
		}
		record(p.Conn.SetDeadline(time.Time{}))
		p.mutex.Lock()
		clear(p.clientDict)
		p.mutex.Unlock()
	})
	return p.closeErr
}

// LocalAddr is the address the proxy is listening on.
func (p *Proxy) LocalAddr() net.Addr { return p.Conn.LocalAddr() }

func (p *Proxy) newConnection(cliAddr *net.UDPAddr) (*connection, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	if err := conn.SetReadBuffer(desiredBufferSize); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.SetWriteBuffer(desiredBufferSize); err != nil {
		conn.Close()
		return nil, err
	}
	return &connection{
		ClientAddr:      cliAddr,
		ServerAddr:      p.ServerAddr,
		incomingPackets: make(chan packetEntry, 10),
		incomingDone:    make(chan struct{}),
		originalConn:    conn,
		sockets:         map[*net.UDPConn]struct{}{conn: {}},
		Incoming:        newQueue(),
		Outgoing:        newQueue(),
		ServerConn:      conn,
	}, nil
}

// runProxy listens on the proxy address and handles incoming packets.
func (p *Proxy) runProxy() error {
	for {
		buffer := make([]byte, protocol.MaxPacketBufferSize)
		n, cliaddr, err := p.Conn.ReadFromUDP(buffer)
		p.observeRead(DirectionIncoming, cliaddr, p.Conn.LocalAddr(), buffer[:n], n, err)
		if err != nil {
			return err
		}
		raw := buffer[:n]

		p.mutex.Lock()
		if p.closing {
			p.mutex.Unlock()
			return nil
		}
		conn, ok := p.clientDict[cliaddr.String()]

		if !ok {
			conn, err = p.newConnection(cliaddr)
			if err != nil {
				p.mutex.Unlock()
				return err
			}
			p.clientDict[cliaddr.String()] = conn
			p.workers.Go(func() { p.runIncomingConnection(conn) })
			p.workers.Go(func() { p.runOutgoingConnection(conn) })
		}
		p.mutex.Unlock()

		if p.DropPacket != nil && p.DropPacket(DirectionIncoming, cliaddr, conn.ServerAddr, raw) {
			if p.logger.Debug() {
				p.logger.Debugf("dropping incoming packet(%d bytes)", n)
			}
			continue
		}

		var delay time.Duration
		if p.DelayPacket != nil {
			delay = p.DelayPacket(DirectionIncoming, cliaddr, conn.ServerAddr, raw)
		}
		if delay == 0 {
			if p.logger.Debug() {
				p.logger.Debugf("forwarding incoming packet (%d bytes) to %s", len(raw), conn.ServerAddr)
			}
			socket := conn.GetServerConn()
			if err := p.observeWrite(DirectionIncoming, socket.LocalAddr(), conn.ServerAddr, raw, func() (int, error) { return socket.WriteTo(raw, conn.ServerAddr) }); err != nil {
				return err
			}
		} else {
			now := monotime.Now()
			if p.logger.Debug() {
				p.logger.Debugf("delaying incoming packet (%d bytes) to %s by %s", len(raw), conn.ServerAddr, delay)
			}
			if !conn.queuePacket(now.Add(delay), raw, p.closeChan) {
				return nil
			}
		}
	}
}

// runConnection handles packets from server to a single client
func (p *Proxy) runOutgoingConnection(conn *connection) error {
	outgoingPackets := make(chan packetEntry, 10)
	outgoingDone := make(chan struct{})
	defer close(outgoingDone)
	// This consumer remains counted while registering its nested reader.
	p.workers.Go(func() {
		for {
			select {
			case <-p.closeChan:
				return
			case <-outgoingDone:
				return
			default:
			}
			buffer := make([]byte, protocol.MaxPacketBufferSize)
			socket := conn.GetServerConn()
			n, addr, err := socket.ReadFrom(buffer)
			p.observeRead(DirectionOutgoing, addr, socket.LocalAddr(), buffer[:n], n, err)
			if err != nil {
				// when the connection is switched out, we set a deadline on the old connection,
				// in order to return it immediately
				if errors.Is(err, os.ErrDeadlineExceeded) {
					continue
				}
				return
			}
			raw := buffer[0:n]

			if p.DropPacket != nil && p.DropPacket(DirectionOutgoing, addr, conn.ClientAddr, raw) {
				if p.logger.Debug() {
					p.logger.Debugf("dropping outgoing packet(%d bytes)", n)
				}
				continue
			}

			var delay time.Duration
			if p.DelayPacket != nil {
				delay = p.DelayPacket(DirectionOutgoing, addr, conn.ClientAddr, raw)
			}
			if delay == 0 {
				if p.logger.Debug() {
					p.logger.Debugf("forwarding outgoing packet (%d bytes) to %s", len(raw), conn.ClientAddr)
				}
				if err := p.observeWrite(DirectionOutgoing, p.Conn.LocalAddr(), conn.ClientAddr, raw, func() (int, error) { return p.Conn.WriteToUDP(raw, conn.ClientAddr) }); err != nil {
					return
				}
			} else {
				now := monotime.Now()
				if p.logger.Debug() {
					p.logger.Debugf("delaying outgoing packet (%d bytes) to %s by %s", len(raw), conn.ClientAddr, delay)
				}
				select {
				case outgoingPackets <- packetEntry{Time: now.Add(delay), Raw: raw}:
				case <-outgoingDone:
					return
				case <-p.closeChan:
					return
				}
			}
		}
	})

	for {
		select {
		case <-p.closeChan:
			return nil
		case e := <-outgoingPackets:
			conn.Outgoing.Add(e)
		case <-conn.Outgoing.Timer():
			raw := conn.Outgoing.Get()
			if err := p.observeWrite(DirectionOutgoing, p.Conn.LocalAddr(), conn.ClientAddr, raw, func() (int, error) { return p.Conn.WriteTo(raw, conn.ClientAddr) }); err != nil {
				return err
			}
		}
	}
}

func (p *Proxy) runIncomingConnection(conn *connection) error {
	defer close(conn.incomingDone)
	for {
		select {
		case <-p.closeChan:
			return nil
		case e := <-conn.incomingPackets:
			// Send the packet to the server
			conn.Incoming.Add(e)
		case <-conn.Incoming.Timer():
			socket := conn.GetServerConn()
			raw := conn.Incoming.Get()
			if err := p.observeWrite(DirectionIncoming, socket.LocalAddr(), conn.ServerAddr, raw, func() (int, error) { return socket.WriteTo(raw, conn.ServerAddr) }); err != nil {
				return err
			}
		}
	}
}

func (p *Proxy) observeRead(dir Direction, from, to net.Addr, b []byte, n int, err error) {
	if p.ObserveSocket != nil {
		p.ObserveSocket(SocketEvent{Direction: dir, Operation: "read", From: from, To: to, Data: b, N: n, Err: err})
	}
}

func (p *Proxy) observeWrite(dir Direction, from, to net.Addr, b []byte, write func() (int, error)) error {
	if p.ObserveSocket == nil {
		_, err := write()
		return err
	}
	start := time.Now()
	n, err := write()
	p.ObserveSocket(SocketEvent{Direction: dir, Operation: "write", Started: start, From: from, To: to, Data: b, N: n, Err: err})
	return err
}
